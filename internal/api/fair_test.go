package api_test

import (
	"bytes"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/fair"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/testkit"
)

// Provably fair dice (docs/FAIR.md): a roll's message carries the proof of
// its draw, over the input its client gave, its second and its own
// client_id, which checks out under the server's published key with no
// server asked; a pair draws once, the same post sent again gets the same
// roll and anyone else's use of the pair is refused; a post that draws needs
// the input; a proof moved to another message fails; a deleted roll and an
// edited one keep their proof.

// blockOf reads a message's fair block.
func blockOf(t *testing.T, m map[string]any) model.FairBlock {
	t.Helper()
	var b model.FairBlock
	if m["fair"] == nil {
		t.Fatalf("no fair block: %v", m)
	}
	remarshal(t, m["fair"], &b)
	return b
}

// keysOf is the server's VRF key history, as anyone reads it.
func keysOf(ts *testkit.TestServer) []fair.Key {
	var out []fair.Key
	for _, k := range ts.Server.Keys.VRFHistory() {
		out = append(out, fair.Key{Key: k.Key, Began: k.Began})
	}
	return out
}

// verified checks a roll as a client does: the block, and every die of the
// view re-derived from it.
func verified(t *testing.T, ts *testkit.TestServer, conv string, m map[string]any) model.FairBlock {
	t.Helper()
	b := blockOf(t, m)
	var created model.Time
	remarshal(t, m["created_at"], &created)
	clientID, _ := m["client_id"].(string)
	if err := fair.CheckBlock(&b, ts.Server.Config.Hostname, clientID, created, fair.Window, keysOf(ts)); err != nil {
		t.Fatalf("the block does not check out: %v", err)
	}
	var view model.ValueEnvelope
	remarshal(t, m["view"], &view)
	if view.Draw == nil {
		t.Fatalf("the view does not say it took the draw: %v", m["view"])
	}
	var stored struct {
		Rows []fair.DiceRow `json:"rows"`
	}
	json.Unmarshal(view.Data, &stored)
	again, err := fair.Roll(fair.NewStream(b.Beta, uint32(view.Draw.I)), stored.Rows)
	a, _ := json.Marshal(again)
	want, _ := json.Marshal(stored.Rows)
	if err != nil || !bytes.Equal(a, want) {
		t.Fatalf("the dice are not the draw's: %s, want %s", a, want)
	}
	return b
}

func TestARollCarriesAProofThatChecksOut(t *testing.T) {
	ts := testkit.NewServer(t)
	owner, sam := ts.Owner, ts.Member("sam")
	room := owner.Channel("fair", false, sam)

	firstMsg := rolled(sam, room, "roll", "4d6kh3, d20")
	first := verified(t, ts, room, firstMsg)
	if first.Owner != ts.Server.Config.Hostname || first.Derive != 1 || first.Alpha != fair.Alpha(first.T, firstMsg["client_id"].(string)) {
		t.Fatalf("the first draw: %+v", first)
	}
	// A plain post draws nothing and moves nothing.
	if plain := owner.Post(room, "no dice here"); plain["fair"] != nil {
		t.Fatalf("a plain post carries a block: %v", plain)
	}
	if second := verified(t, ts, room, rolled(owner, room, "dice", "3")); second.Alpha == first.Alpha {
		t.Fatalf("two messages share an alpha: %+v", second)
	}
	// Inline dice: one draw for the message, each roll its own object.
	m := owner.Post(room, "I hit for [[2d6+3]] and [[d20]]")
	b := blockOf(t, m)
	if b.Alpha != fair.Alpha(b.T, m["client_id"].(string)) {
		t.Fatalf("the inline draw: %+v", b)
	}
	for i, e := range diceIn(m) {
		var v model.ValueEnvelope
		remarshal(t, e["object"], &v)
		if v.Draw == nil || v.Draw.I != i {
			t.Fatalf("inline roll %d took %+v", i, v.Draw)
		}
	}

	// An edit that marks the roll removed keeps the message's proof.
	edited := obj(owner.Must(200, "PATCH", "/api/v1/messages/"+m["id"].(string), map[string]any{"text": "I hit for [[2d6+4]] and [[d20]]"}), "message")
	if e := blockOf(t, edited); !bytes.Equal(e.Proof, b.Proof) {
		t.Fatal("an edit changed the proof")
	}
	// A deleted roll keeps its proof, so the chain stays whole.
	roll := rolled(sam, room, "roll", "")
	kept := blockOf(t, roll)
	del := obj(sam.Must(200, "DELETE", "/api/v1/messages/"+roll["id"].(string), nil), "message")
	var got map[string]any
	for _, m := range owner.Messages(room) {
		if m["id"] == roll["id"] {
			got = m
		}
	}
	for _, d := range []map[string]any{del, got} {
		if d == nil || d["view"] != nil || d["deleted_at"] == nil || !bytes.Equal(blockOf(t, d).Proof, kept.Proof) {
			t.Fatalf("the deleted roll: %v", d)
		}
	}
	sameOverProtobuf(t, ts, owner.Token().Bearer, "listMessages", convPath(room)+"/messages")
}

// A draw's input is the second and the client_id the client gives it. The
// same command sent again with them is given the message it made, the same
// roll, however many times and at once; the pair used by anyone else, here
// or in another conversation, is refused; a second outside the window of the
// server's clock is refused with the server's time, and so is one no later
// than the server's start just after it started; a post that draws with no
// client_id or second is refused.
func TestADrawsInputIsTakenOnce(t *testing.T) {
	ts := testkit.NewServer(t)
	owner, sam := ts.Owner, ts.Member("sam")
	room := owner.Channel("once", false, sam)
	other := owner.Channel("elsewhere", false, sam)
	cmds := convPath(room) + "/commands"
	// Just after the start: a second no later than the start's.
	owner.Refused(409, model.CodeClock, "POST", cmds, map[string]any{"name": "roll", "text": "d6", "client_id": model.NewID[string](), "draw_t": time.Now().Unix() - 1})
	now := time.Now().Unix() + 1
	id := model.NewID[string]()
	body := map[string]any{"name": "roll", "text": "3d20", "client_id": id, "draw_t": now}
	first := owner.Must(200, "POST", cmds, body)["message"].(map[string]any)
	var wg sync.WaitGroup
	again := make([]map[string]any, 4)
	for i := range again {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if _, out, err := owner.Try("POST", cmds, body); err == nil {
				again[i], _ = out["message"].(map[string]any)
			}
		}(i)
	}
	wg.Wait()
	for _, m := range again {
		if m == nil || m["id"] != first["id"] || m["text"] != first["text"] || !bytes.Equal(blockOf(t, m).Proof, blockOf(t, first).Proof) {
			t.Fatalf("the same roll sent again: %v, first %v", m, first)
		}
	}
	verified(t, ts, room, first)
	if n := len(owner.Messages(room)); n != 1 {
		t.Fatalf("%d messages for one roll sent five times", n)
	}
	sam.Refused(409, model.CodeConflict, "POST", cmds, body)
	owner.Refused(409, model.CodeConflict, "POST", convPath(other)+"/commands", body)
	owner.Refused(409, model.CodeConflict, "POST", convPath(other)+"/messages", map[string]any{"text": "[[3d20]]", "client_id": id, "draw_t": now})
	// A client_id draws once in the whole window, under any second.
	owner.Refused(409, model.CodeConflict, "POST", convPath(other)+"/commands", map[string]any{"name": "roll", "text": "3d20", "client_id": id, "draw_t": now + 1})
	// The clock.
	for _, off := range []int64{-11 - 2, 11 + 2} {
		e := owner.Refused(409, model.CodeClock, "POST", cmds, map[string]any{"name": "roll", "text": "d6", "client_id": model.NewID[string](), "draw_t": time.Now().Unix() + off})
		if at, _ := e["server_time"].(string); at == "" {
			t.Fatalf("a clock refusal without the server's time: %v", e)
		}
	}
	// No input.
	owner.Refused(400, model.CodeInvalidValue, "POST", cmds, map[string]any{"name": "roll", "text": "d6", "client_id": ""})
	owner.Refused(400, model.CodeInvalidValue, "POST", cmds, map[string]any{"name": "roll", "text": "d6", "client_id": model.NewID[string](), "draw_t": 0})
	owner.Refused(400, model.CodeInvalidValue, "POST", convPath(room)+"/messages", map[string]any{"text": "I hit for [[2d6]]", "client_id": ""})
}

// A proof is bound to its message: moved to another message, with another
// client_id, it fails, here and in the clients alike.
func TestAProofMovedToAnotherMessageFails(t *testing.T) {
	ts := testkit.NewServer(t)
	owner := ts.Owner
	room := owner.Channel("moved", false)
	one, two := rolled(owner, room, "roll", "d20"), rolled(owner, room, "roll", "d20")
	verified(t, ts, room, one)
	b := blockOf(t, one)
	var created model.Time
	remarshal(t, two["created_at"], &created)
	if err := fair.CheckBlock(&b, ts.Server.Config.Hostname, two["client_id"].(string), created, fair.Window, keysOf(ts)); err == nil {
		t.Fatal("a proof moved to another message checked out")
	}
}

// Every chance command posts a message whose fair block checks out under the
// server's published key, whose view says which share of the draw it took
// and what it asked, and whose outcome is what derive 1 gives for it from
// that share (docs/FAIR.md section 5a): /roll, /dice, /flip, /pick,
// /shuffle, /draw, /tarot, /mahjongg and /mahjong.
func TestEveryChanceCommandIsProved(t *testing.T) {
	ts := testkit.NewServer(t)
	owner, sam := ts.Owner, ts.Member("sam")
	room := owner.Channel("table", false, sam)
	for _, c := range []struct{ name, text, kind, by string }{
		{"roll", "2d6+3", model.KindDiceRoll, "dice"}, {"dice", "4", model.KindDiceRoll, "dice"},
		{"flip", "", model.KindCoinFlip, "dice"},
		{"pick", "tea | coffee | water", model.KindPick, "choose"}, {"shuffle", "a | b | c | d", model.KindShuffle, "choose"},
		{"draw", "5", model.KindCardDraw, "cards"}, {"draw", "3 tarot", model.KindCardDraw, "cards"},
		{"tarot", "celtic", model.KindTarotReading, "cards"},
		{"mahjongg", "13", model.KindCardDraw, "cards"}, {"mahjong", "", model.KindCardDraw, "cards"},
	} {
		m := rolled(sam, room, c.name, c.text)
		b := blockOf(t, m)
		var created model.Time
		remarshal(t, m["created_at"], &created)
		if err := fair.CheckBlock(&b, ts.Server.Config.Hostname, m["client_id"].(string), created, fair.Window, keysOf(ts)); err != nil {
			t.Fatalf("/%s %s: the block does not check out: %v", c.name, c.text, err)
		}
		var view model.ValueEnvelope
		remarshal(t, m["view"], &view)
		if view.Kind != c.kind || view.By != c.by || view.Draw == nil || view.Draw.I != 0 || m["text"] == "" {
			t.Fatalf("/%s %s: %v", c.name, c.text, m)
		}
		if ok, err := fair.Rederive(view.Kind, view.Data, b.Beta, uint32(view.Draw.I)); !ok || err != nil {
			t.Fatalf("/%s %s: the outcome is not the draw's (%v): %s", c.name, c.text, err, view.Data)
		}
		// What the value says it asked is what its kind asks of its data.
		asks, _ := fair.KindAsks(view.Kind, view.Data)
		if view.Kind != model.KindDiceRoll {
			a, _ := json.Marshal(asks)
			w, _ := json.Marshal(view.Draw.Primitives)
			if !bytes.Equal(a, w) {
				t.Fatalf("/%s %s: recorded %s, asks %s", c.name, c.text, w, a)
			}
		}
	}
}
