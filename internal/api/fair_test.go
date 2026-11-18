package api_test

import (
	"bytes"
	"encoding/json"
	"sync"
	"testing"

	"github.com/tikaydaes/churr/internal/fair"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/testkit"
)

// Provably fair dice (docs/FAIR.md): a roll's message carries the proof of
// its draw, which checks out under the server's published key with no
// server asked; the conversation's chain advances by one per message that
// draws and each draw names the proof before it; messages posted at once
// get consecutive draws; a deleted roll and an edited one keep their proof.

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
	if err := fair.CheckBlock(&b, ts.Server.Config.Hostname, conv, created, keysOf(ts)); err != nil {
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

	first := verified(t, ts, room, rolled(sam, room, "roll", "4d6kh3, d20"))
	if first.N != 1 || first.Owner != ts.Server.Config.Hostname || first.Derive != 1 {
		t.Fatalf("the first draw: %+v", first)
	}
	// A plain post draws nothing and moves nothing.
	if plain := owner.Post(room, "no dice here"); plain["fair"] != nil {
		t.Fatalf("a plain post carries a block: %v", plain)
	}
	second := verified(t, ts, room, rolled(owner, room, "dice", "3"))
	if second.N != 2 || !bytes.Equal(second.Alpha, fair.Alpha(ts.Server.Config.Hostname, room, 2)) {
		t.Fatalf("the second draw does not follow the first: %+v", second)
	}
	// Inline dice: one draw for the message, each roll its own object.
	m := owner.Post(room, "I hit for [[2d6+3]] and [[d20]]")
	b := blockOf(t, m)
	if b.N != 3 || !bytes.Equal(b.Alpha, fair.Alpha(ts.Server.Config.Hostname, room, 3)) {
		t.Fatalf("the third draw: %+v", b)
	}
	for i, e := range diceIn(m) {
		var v model.ValueEnvelope
		remarshal(t, e["object"], &v)
		if v.Draw == nil || v.Draw.I != i {
			t.Fatalf("inline roll %d took %+v", i, v.Draw)
		}
	}
	// Another conversation has a chain of its own.
	other := owner.Channel("other", false)
	if b := blockOf(t, rolled(owner, other, "roll", "")); b.N != 1 {
		t.Fatalf("another conversation's first draw is %d", b.N)
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

// Rolls posted at once take consecutive draws, none twice and none skipped.
func TestRacingRollsTakeConsecutiveDraws(t *testing.T) {
	ts := testkit.NewServer(t)
	owner := ts.Owner
	room := owner.Channel("race", false)
	const n = 6
	got := make([]map[string]any, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, out, err := owner.Try("POST", convPath(room)+"/commands", map[string]any{"name": "roll", "text": "d6"})
			if err == nil {
				got[i], _ = out["message"].(map[string]any)
			}
		}(i)
	}
	wg.Wait()
	byN := map[int64]model.FairBlock{}
	for _, m := range got {
		if m == nil {
			t.Fatal("a roll was not made")
		}
		b := verified(t, ts, room, m)
		if _, twice := byN[b.N]; twice {
			t.Fatalf("draw %d was used twice", b.N)
		}
		byN[b.N] = b
	}
	for k := int64(1); k <= n; k++ {
		b, ok := byN[k]
		if !ok {
			t.Fatalf("draw %d was skipped: %v", k, byN)
		}
		if !bytes.Equal(b.Alpha, fair.Alpha(ts.Server.Config.Hostname, room, k)) {
			t.Fatalf("draw %d is not proved over its place in the chain", k)
		}
	}
}
