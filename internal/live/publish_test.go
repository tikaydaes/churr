package live

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/model"
)

// levels is a Tailor that gives each reader its own `notify`, from maps
// shared between readers as the chat service's are.
type levels struct {
	mu    sync.Mutex
	by    map[model.AccountID]string
	asked map[model.AccountID]int
}

var levelFields = map[string]map[string]any{"none": {"notify": "none"}, "badge": {"notify": "badge"}, "notify": {"notify": "notify"}}

func (l *levels) Tailor(_ context.Context, e model.Event, to model.AccountID, _ bool) map[string]any {
	if e.Type != model.EventMessageCreated {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.asked[to]++
	return levelFields[l.by[to]]
}

// got returns the message.created frames a client has been sent, as
// "seq:notify" each.
func (cl *client) got() []string {
	cl.mu.Lock()
	defer cl.mu.Unlock()
	var out []string
	for _, f := range cl.others {
		if f["type"] == model.EventMessageCreated {
			out = append(out, fmt.Sprintf("%v:%v", f["seq"], f["notify"]))
		}
	}
	return out
}

// TestPublishReachesMembersOnce checks the fan-out of a conversation event:
// each connected member and each account in AlsoTo gets it exactly once
// with its own `notify`, nobody else gets it, and someone who joins or
// leaves is in or out from the very next event.
func TestPublishReachesMembersOnce(t *testing.T) {
	c := newCrowd(t, 7, Options{})
	ctx := context.Background()
	s := c.hub.store
	conv := model.Conversation{Kind: model.KindChannel, Name: "town", CreatedByID: c.ids[0]}
	if err := s.Conversations().Create(ctx, &conv); err != nil {
		t.Fatal(err)
	}
	for _, i := range []int{0, 1, 2, 3} {
		if _, err := s.Members().AddDirect(ctx, conv.ID, c.ids[i], model.LevelMember, model.Now()); err != nil {
			t.Fatal(err)
		}
	}
	tl := &levels{by: map[model.AccountID]string{c.ids[0]: "none", c.ids[1]: "badge", c.ids[2]: "notify", c.ids[4]: "notify", c.ids[5]: "none"},
		asked: map[model.AccountID]int{}}
	c.hub.SetTailor(tl)
	// 3 is a member who is not connected; 6 is connected and no member.
	clients := map[int]*client{}
	for _, i := range []int{0, 1, 2, 4, 5, 6} {
		clients[i] = c.dial(i)
	}
	second := c.dial(1) // a second device of the same member
	seq := int64(0)
	publish := func(also ...model.AccountID) {
		seq++
		c.hub.Publish(ctx, model.Event{Type: model.EventMessageCreated, ConversationID: conv.ID, Seq: seq, At: model.Now(),
			MessageID: model.NewID[model.MessageID](), AlsoTo: also, Payload: map[string]any{"message": map[string]any{"text": "hello"}}})
	}
	expect := func(what string, want map[int][]string) {
		t.Helper()
		// Let anything that should not arrive arrive, so it is seen.
		time.Sleep(150 * time.Millisecond)
		for i, cl := range clients {
			eventually(t, fmt.Sprintf("%s: client %d has %v", what, i, want[i]), func() bool { return fmt.Sprint(cl.got()) == fmt.Sprint(want[i]) })
		}
	}

	// A member named in AlsoTo too, an outsider named twice.
	publish(c.ids[1], c.ids[4], c.ids[4])
	expect("the first event", map[int][]string{0: {"1:none"}, 1: {"1:badge"}, 2: {"1:notify"}, 4: {"1:notify"}})
	eventually(t, "the second device has it", func() bool { return fmt.Sprint(second.got()) == "[1:badge]" })
	tl.mu.Lock()
	if tl.asked[c.ids[3]] != 0 || tl.asked[c.ids[6]] != 0 || tl.asked[c.ids[1]] != 1 || tl.asked[c.ids[4]] != 1 {
		t.Errorf("the tailor was asked about people the event does not reach, or twice: %v", tl.asked)
	}
	tl.mu.Unlock()

	// Someone joins, someone leaves: the next event knows.
	if _, err := s.Members().AddDirect(ctx, conv.ID, c.ids[5], model.LevelMember, model.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Members().RemoveDirect(ctx, conv.ID, c.ids[2]); err != nil {
		t.Fatal(err)
	}
	publish()
	expect("after a join and a leave", map[int][]string{0: {"1:none", "2:none"}, 1: {"1:badge", "2:badge"}, 2: {"1:notify"}, 4: {"1:notify"}, 5: {"2:none"}})

	// ConnectedAccounts and SendAll, which the people service tells others
	// through.
	on := c.hub.ConnectedAccounts(func(id model.AccountID) bool { return id == c.ids[0] || id == c.ids[3] })
	if len(on) != 1 || on[0] != c.ids[0] {
		t.Errorf("ConnectedAccounts returned %v, want only the connected one asked for", on)
	}
	c.hub.SendAll([]model.AccountID{c.ids[0], c.ids[3], c.ids[6]}, AccountEvent("status.changed", map[string]any{"account_id": c.ids[1]}))
	for _, i := range []int{0, 6} {
		cl := clients[i]
		eventually(t, "an event sent to several accounts arrives", func() bool {
			cl.mu.Lock()
			defer cl.mu.Unlock()
			n := 0
			for _, f := range cl.others {
				if f["type"] == "status.changed" {
					n++
				}
			}
			return n == 1
		})
	}
}
