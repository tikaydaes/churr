package link_test

import (
	"testing"

	"github.com/tikaydaes/churr/internal/testkit"
)

// Case 3: a direct link made from an invitation ends with both peers active
// and each holding the other's keys. Case 2: the fingerprint each console
// shows is the other's own.
func TestDirectLink(t *testing.T) {
	l := testkit.NewLinked(t, 2, false)
	a, b := l.Servers[0], l.Servers[1]
	pa := a.Owner.Must(200, "GET", "/api/v1/admin/peers/"+l.Peers[0], nil)["peer"].(map[string]any)
	pb := b.Owner.Must(200, "GET", "/api/v1/admin/peers/"+l.Peers[1], nil)["peer"].(map[string]any)
	if pa["name"] != "b.localhost" || pb["name"] != "a.localhost" || pa["dials"] != false || pb["dials"] != true {
		t.Fatalf("peers: %v %v", pa, pb)
	}
	if pa["fingerprint"] != b.Server.Link.Fingerprint() || pb["fingerprint"] != a.Server.Link.Fingerprint() {
		t.Fatalf("each side should show the other's fingerprint: %v %v", pa["fingerprint"], pb["fingerprint"])
	}
}

// Case 13: a channel shared over a direct link: a message posted on A
// appears on B with the same message identifier and in the same place (the
// same `order`), and the other way. Each server's seq is its own arrival
// number and need not agree.
func TestShareOverDirectLink(t *testing.T) {
	l := testkit.NewLinked(t, 2, false)
	a, b := l.Servers[0], l.Servers[1]
	ann := a.Member("ann")
	ch := a.Owner.Channel("launch", false, ann)
	before := ann.Post(ch, "before sharing")
	_, local := l.Share(0, ch, "launch-here", map[string]any{"history_days": 30}, 1)

	posted := ann.Post(ch, "hello from a")
	got := b.Owner.HasMessage(local[1], "hello from a")
	if got["id"] != posted["id"] || got["order"] != posted["order"] || got["order"] == nil || got["late"] == true {
		t.Fatalf("on b: id %v order %v; on a: id %v order %v", got["id"], got["order"], posted["id"], posted["order"])
	}
	if got["author_id"] != string(ann.ID) {
		t.Fatalf("the author is %v, not ann", got["author_id"])
	}
	// History came with the place it has on a, before everything stamped.
	old := b.Owner.HasMessage(local[1], "before sharing")
	if old["id"] != before["id"] || old["order"] != before["order"] || old["order"].(string) >= got["order"].(string) {
		t.Fatalf("history on b: %v, on a: %v", old, before)
	}

	// The other way: b's owner posts. The message is stored and shown at
	// once, with a seq and its place, and is unsent until a has taken it.
	reply := b.Owner.Post(local[1], "hello from b")
	if reply["seq"] == nil || reply["order"] == nil || reply["unsent"] != true {
		t.Fatalf("b's own message as posted: %v", reply)
	}
	onA := ann.HasMessage(ch, "hello from b")
	if onA["id"] != reply["id"] || onA["order"] != reply["order"] || onA["unsent"] == true {
		t.Fatalf("b's message: on a %v, as posted %v", onA, reply)
	}
	testkit.Eventually(t, "b's message is acknowledged", func() bool {
		return b.Owner.HasMessage(local[1], "hello from b")["unsent"] != true
	})
	// a is the time reference: its own messages are never unsent.
	if posted["unsent"] == true {
		t.Fatalf("a message of the reference's own is marked unsent: %v", posted)
	}
}
