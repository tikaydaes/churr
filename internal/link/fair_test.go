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
	stream, local := l.Share(0, ch, "tavern-here", map[string]any{"history_days": 30}, 1)

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
	var hb model.FairBlock
	raw, _ := json.Marshal(honest["fair"])
	json.Unmarshal(raw, &hb)
	if chain, _ := fair.ChainOfAlpha(hb.Alpha); chain != stream {
		t.Fatalf("a roll in a shared channel is drawn on the stream's chain, not %q", chain)
	}

	a.Server.Chat.SetFair("a.localhost", newForger(t))
	forged := roll("d20")
	got := rollOn(b.Owner, local[1], forged["text"].(string))
	if got["view"] != nil || got["fair"] != nil || !strings.HasPrefix(got["text"].(string), "Rolled 1d20:") {
		t.Fatalf("a forged roll arrived as a roll: %v", got)
	}
	audit := b.Owner.Must(200, "GET", "/api/v1/admin/audit?action=link.fair_block_dropped", nil)
	entries, _ := audit["entries"].([]any)
	if len(entries) != 1 {
		t.Fatalf("the drop was audited %d times: %v", len(entries), audit)
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
