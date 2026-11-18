package link_test

import (
	"crypto/sha256"
	"encoding/json"
	"strings"
	"testing"

	"github.com/tikaydaes/churr/internal/ecvrf"
	"github.com/tikaydaes/churr/internal/fair"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/testkit"
)

// forger proves with a VRF key its server never published.
type forger struct{ sk, pk []byte }

func newForger(t *testing.T) forger {
	sum := sha256.Sum256([]byte("a key nobody saw begin"))
	pk, err := ecvrf.DerivePublicKey(sum[:])
	if err != nil {
		t.Fatal(err)
	}
	return forger{sk: sum[:], pk: pk}
}

func (f forger) VRFProve(alpha []byte) ([]byte, []byte, error) {
	pi, err := ecvrf.Prove(f.sk, alpha)
	if err != nil {
		return nil, nil, err
	}
	beta, err := ecvrf.ProofToHash(pi)
	return pi, beta, err
}

func (f forger) VRFPublic() []byte { return f.pk }

// rollOn waits for a roll to arrive on a server and returns it.
func rollOn(c *testkit.Client, conv, text string) map[string]any {
	return c.HasMessage(conv, text)
}

// A roll crosses a direct link with the proof of its draw, which the other
// server checks under the key it pinned for the sender, against the stream:
// one drawn before the channel was shared too, which came as history. A
// roll whose proof is under a key the sender never published arrives as its
// text, its view and proof dropped, and the drop is audited.
func TestAForgedDrawIsDroppedOnReceipt(t *testing.T) {
	l := testkit.NewLinked(t, 2, false)
	a, b := l.Servers[0], l.Servers[1]
	ch := a.Owner.Channel("tavern", false)
	roll := func(text string) map[string]any {
		return a.Owner.Must(200, "POST", "/api/v1/conversations/"+ch+"/commands", map[string]any{"name": "roll", "text": text})["message"].(map[string]any)
	}
	before := roll("2d6")
	gone := roll("d4")
	// A roll whose proof checks out but whose value claims another object
	// index than the one rule gives: the server could have tried indexes
	// until an outcome suited it. It arrives as its text, and is audited.
	renumbered := roll("3d6")
	if a.Server.Store.Dialect() == "postgres" {
		a.Exec(`UPDATE messages SET view = replace(view::text, '"i":0', '"i":1')::jsonb WHERE id = ?`, renumbered["id"])
	} else {
		a.Exec(`UPDATE messages SET view = replace(view, '"i":0', '"i":1') WHERE id = ?`, renumbered["id"])
	}
	a.Owner.Must(200, "DELETE", "/api/v1/messages/"+gone["id"].(string), nil)
	_, local := l.Share(0, ch, "tavern-here", map[string]any{"history_days": 30}, 1)

	honest := roll("4d6kh3")
	for _, sent := range []map[string]any{before, honest} {
		got := rollOn(b.Owner, local[1], sent["text"].(string))
		if got["view"] == nil || got["fair"] == nil {
			t.Fatalf("an honest roll arrived without its view or proof: %v", got)
		}
		a1, _ := json.Marshal(sent["fair"])
		b1, _ := json.Marshal(got["fair"])
		if string(a1) != string(b1) {
			t.Fatalf("the proof changed on the way: %s, sent %s", b1, a1)
		}
	}
	if got := rollOn(b.Owner, local[1], renumbered["text"].(string)); got["view"] != nil || got["fair"] != nil {
		t.Fatalf("a roll with an index not the canonical one arrived as a roll: %v", got)
	}
	// A deleted roll came in the history as its marker, with its proof.
	b.Owner.HasMessage(local[1], honest["text"].(string))
	var marker map[string]any
	for _, m := range b.Owner.Messages(local[1]) {
		if m["id"] == gone["id"] {
			marker = m
		}
	}
	if marker == nil || marker["deleted_at"] == nil || marker["fair"] == nil {
		t.Fatalf("the deleted roll on b: %v", marker)
	}
	var hb model.FairBlock
	raw, _ := json.Marshal(honest["fair"])
	json.Unmarshal(raw, &hb)
	if hb.Alpha != fair.Alpha(hb.T, honest["client_id"].(string)) {
		t.Fatalf("a roll in a shared channel is drawn for its own second and client_id, not %q", hb.Alpha)
	}

	a.Server.Chat.SetFair("a.localhost", newForger(t))
	forged := roll("d20")
	got := rollOn(b.Owner, local[1], forged["text"].(string))
	if got["view"] != nil || got["fair"] != nil || !strings.HasPrefix(got["text"].(string), "Rolled 1d20:") {
		t.Fatalf("a forged roll arrived as a roll: %v", got)
	}
	audit := b.Owner.Must(200, "GET", "/api/v1/admin/audit?action=link.fair_block_dropped", nil)
	entries, _ := audit["entries"].([]any)
	if len(entries) != 2 {
		t.Fatalf("the drops were audited %d times, not for the renumbered roll and the forged one: %v", len(entries), audit)
	}
	// Inline dice of a forged draw are text too.
	a.Owner.Post(ch, "and [[1d6]]")
	inl := rollOn(b.Owner, local[1], "and [[1d6]]")
	if ents, _ := inl["entities"].([]any); inl["fair"] != nil || len(ents) != 0 {
		t.Fatalf("inline dice of a forged draw: %v", inl)
	}
}

// In a hub channel a roll is drawn by its author's instance, as dice are
// made today, and the other instance checks it under the key history the
// channel's record carries for that instance.
func TestARollInAHubChannelKeepsItsProof(t *testing.T) {
	l := testkit.NewLinked(t, 2, true)
	a, b := l.Servers[0], l.Servers[1]
	ch := a.Owner.Channel("commons", false)
	_, local := l.Share(0, ch, "commons", map[string]any{"listing": "open", "history_days": 30}, 1)
	sent := a.Owner.Must(200, "POST", "/api/v1/conversations/"+ch+"/commands", map[string]any{"name": "roll", "text": "3d8"})["message"].(map[string]any)
	got := rollOn(b.Owner, local[1], sent["text"].(string))
	if got["view"] == nil || got["fair"] == nil || got["fair"].(map[string]any)["owner"] != "a.localhost" {
		t.Fatalf("a hub channel's roll arrived as %v", got)
	}
	audit := b.Owner.Must(200, "GET", "/api/v1/admin/audit?action=link.fair_block_dropped", nil)
	if entries, _ := audit["entries"].([]any); len(entries) != 0 {
		t.Fatalf("an honest roll was dropped: %v", audit)
	}
}

// convKeys is the VRF key history the conversation gives a client for each
// server, by name (docs/FAIR.md section 9), as fair.Keys.
func convKeys(t *testing.T, c *testkit.Client, conv string) map[string][]fair.Key {
	t.Helper()
	raw, _ := json.Marshal(c.Must(200, "GET", "/api/v1/conversations/"+conv, nil)["conversation"])
	var got model.Conversation
	if err := json.Unmarshal(raw, &got); err != nil || got.Shared == nil {
		t.Fatalf("the conversation %s: %v %s", conv, err, raw)
	}
	out := map[string][]fair.Key{}
	for _, s := range got.Shared.VRFKeys {
		for _, k := range s.Keys {
			out[s.Server] = append(out[s.Server], fair.Key{Key: k.Key, Began: k.Began})
		}
	}
	return out
}

// sameKeys says whether a history a conversation gives is the one the
// server publishes.
func sameKeys(got []fair.Key, ts *testkit.TestServer) bool {
	want := ts.Server.Link.Identity().VRFKeys()
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if string(got[i].Key) != string(want[i].Key) || got[i].Began != want[i].Began {
			return false
		}
	}
	return true
}

// checksUnder checks a message's draw as a client does, under the history a
// conversation gave, against the chain named.
func checksUnder(t *testing.T, m map[string]any, chain string, keys map[string][]fair.Key) error {
	t.Helper()
	raw, _ := json.Marshal(m)
	var msg model.Message
	if err := json.Unmarshal(raw, &msg); err != nil || msg.Fair == nil {
		t.Fatalf("a message with no draw: %v %s", err, raw)
	}
	return fair.CheckBlock(msg.Fair, msg.Fair.Owner, msg.ClientID, msg.CreatedAt, fair.LinkWindow, keys[msg.Fair.Owner])
}

// A hub channel's conversation gives its clients the key history of every
// server its record has named, from the record the hub keeps for good: a
// roll made by an instance that has since left can still be checked by an
// instance that joined after it went, with nothing asked of the one gone.
func TestTheConversationGivesTheKeysOfAServerThatLeft(t *testing.T) {
	l := testkit.NewLinked(t, 3, true)
	a, b, c := l.Servers[0], l.Servers[1], l.Servers[2]
	ch := a.Owner.Channel("dicehall", false)
	stream, local := l.Share(0, ch, "dicehall", map[string]any{"listing": "open", "history_days": 30}, 1)
	roll := b.Owner.Must(200, "POST", "/api/v1/conversations/"+local[1]+"/commands", map[string]any{"name": "roll", "text": "3d6"})["message"].(map[string]any)
	a.Owner.HasMessage(ch, roll["text"].(string))
	b.Owner.Must(204, "DELETE", "/api/v1/admin/streams/"+stream, nil)
	testkit.Eventually(t, "the hub has taken b out", func() bool { return names(hubChannel(a.Owner, stream)["servers"]) == "a.localhost" })
	convC := joinListed(l, 2, stream, "dicehall-c")
	l.Joined(2, stream)
	got := c.Owner.HasMessage(convC, roll["text"].(string))
	keys := convKeys(t, c.Owner, convC)
	for _, ts := range []*testkit.TestServer{a, b, c} {
		if !sameKeys(keys[ts.Server.Link.Identity().Name], ts) {
			t.Fatalf("the conversation on c gives %v for %s", keys, ts.Server.Link.Identity().Name)
		}
	}
	if len(keys[l.Hub.Name]) == 0 {
		t.Fatalf("the conversation gives no history for the hub: %v", keys)
	}
	if err := checksUnder(t, got, stream, keys); err != nil {
		t.Fatalf("b's roll does not check under the history c was given: %v", err)
	}
}

// A conversation shared over a direct link gives its clients this server's
// pin of the other server's key history, beside its own.
func TestADirectLinkGivesThePinnedKeys(t *testing.T) {
	l := testkit.NewLinked(t, 2, false)
	a, b := l.Servers[0], l.Servers[1]
	ch := a.Owner.Channel("parlour", false)
	stream, local := l.Share(0, ch, "parlour-here", map[string]any{"history_days": 30}, 1)
	roll := a.Owner.Must(200, "POST", "/api/v1/conversations/"+ch+"/commands", map[string]any{"name": "roll", "text": "d20"})["message"].(map[string]any)
	got := b.Owner.HasMessage(local[1], roll["text"].(string))
	keys := convKeys(t, b.Owner, local[1])
	if !sameKeys(keys["a.localhost"], a) || !sameKeys(keys["b.localhost"], b) || len(keys) != 2 {
		t.Fatalf("the conversation on b gives %v", keys)
	}
	if err := checksUnder(t, got, stream, keys); err != nil {
		t.Fatalf("a's roll does not check under the history b was given: %v", err)
	}
}
