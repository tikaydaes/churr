package link_test

import (
	"testing"

	"github.com/tikaydaes/churr/internal/testkit"
)

// Case 14: a channel shared through a hub with three servers: all three
// hold the same events under the same seq.
func TestShareThroughHub(t *testing.T) {
	l := testkit.NewLinked(t, 3, true)
	a, b, c := l.Servers[0], l.Servers[1], l.Servers[2]
	ann := a.Member("ann")
	ch := a.Owner.Channel("launch", false, ann)
	_, local := l.Share(0, ch, "launch", nil, 1, 2)

	fromA := ann.Post(ch, "from a")
	fromB := b.Owner.Post(local[1], "from b")
	fromC := c.Owner.Post(local[2], "from c")
	for i, text := range []string{"from a", "from b", "from c"} {
		id := []map[string]any{fromA, fromB, fromC}[i]["id"]
		onA := ann.HasMessage(ch, text)
		onB := b.Owner.HasMessage(local[1], text)
		onC := c.Owner.HasMessage(local[2], text)
		if onA["id"] != id || onB["id"] != id || onC["id"] != id {
			t.Fatalf("%q: ids differ: %v %v %v, want %v", text, onA["id"], onB["id"], onC["id"], id)
		}
		if onA["order"] != onB["order"] || onB["order"] != onC["order"] || onA["order"] == nil {
			t.Fatalf("%q: order differs: a %v b %v c %v", text, onA["order"], onB["order"], onC["order"])
		}
	}
}
