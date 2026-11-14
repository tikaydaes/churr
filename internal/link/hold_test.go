package link_test

import (
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/link"
	"github.com/tikaydaes/churr/internal/link/wire"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/testkit"
)

// reactionsOn returns how many reactions a client sees on the message with
// this text, -1 if it does not see the message.
func reactionsOn(c *testkit.Client, conv, text string) int {
	for _, m := range arr(c.Must(200, "GET", "/api/v1/conversations/"+conv+"/messages?limit=100", nil)["messages"]) {
		if m := m.(map[string]any); m["text"] == text {
			return len(arr(m["reactions"]))
		}
	}
	return -1
}

// missedBy returns the envelopes of the hub's log that server i has not been
// delivered, in the hub's order.
func (s *shared) missedBy(i int) []wire.Envelope {
	s.t.Helper()
	held := s.link(i).TestHave(s.stream)
	var out []wire.Envelope
	for _, env := range s.Hub.Hub.TestLog(string(s.stream)) {
		if env.N > held[env.Origin] {
			out = append(out, env)
		}
	}
	return out
}

// An event that refers to another origin's event reaches a server before the
// event it refers to (docs/LINK-PROTOCOL.md section 13, "Events that
// refer to others"). It is held, and applied when its message arrives:
// every server ends with the same state. Before, it was rejected at once
// (unknown_message), and the server that got things in that order showed a
// message without its reaction, without its edit, without its reply.
func TestEventsWaitForWhatTheyReferTo(t *testing.T) {
	s := share(t, 3, true, nil)
	a, b, c := s.Servers[0], s.Servers[1], s.Servers[2]
	s.settle("before")
	restore := s.cut(2)

	// While c is away: ann (on a) posts; b reacts to it, replies to it, and
	// reacts to its own reply; ann edits her message.
	posted := s.ann.Post(s.conv[0], "the message")
	onB := b.Owner.HasMessage(s.conv[1], "the message")
	b.Owner.Must(200, "PUT", "/api/v1/messages/"+onB["id"].(string)+"/reactions/👍", nil)
	reply := b.Owner.Must(201, "POST", "/api/v1/conversations/"+s.conv[1]+"/messages", map[string]any{"text": "the reply", "parent_id": onB["id"]})["message"].(map[string]any)
	a.Owner.HasMessage(s.conv[0], "the reply")
	a.Owner.Must(200, "PUT", "/api/v1/messages/"+reply["id"].(string)+"/reactions/🎉", nil)
	testkit.Eventually(t, "b sees a's reaction to its reply", func() bool { return reactionsOn(b.Owner, s.conv[1], "the reply") == 1 })
	testkit.Eventually(t, "a sees b's reaction", func() bool { return reactionsOn(a.Owner, s.conv[0], "the message") == 1 })

	missed := s.missedBy(2)
	var fromA, fromB []wire.Envelope
	for _, env := range missed {
		if env.Origin == testkit.ServerName(0) {
			fromA = append(fromA, env)
		} else {
			fromB = append(fromB, env)
		}
	}
	// a: the message, the reaction to the reply. b: the reaction, the reply.
	if len(fromA) != 2 || len(fromB) != 2 {
		t.Fatalf("the hub's log holds %d envelopes of a and %d of b that c has not got", len(fromA), len(fromB))
	}
	// c is handed them in the worst order, each origin's in its own order:
	// a's reaction to the reply before the reply exists, b's reaction and
	// reply before the message exists, and the message last.
	held := func() int { return s.link(2).TestHeld(s.stream) }
	deliver := func(env wire.Envelope, prev int64) { s.link(2).TestDeliver(s.stream, env, env.Cursor, prev) }
	deliver(fromB[0], fromB[0].Prev) // b's reaction to the message
	if held() != 1 {
		t.Fatalf("c holds %d events after a reaction to a message it has not got, want 1", held())
	}
	if got := s.link(2).StatsNow().Rejected["unknown_message"]; got != 0 {
		t.Fatalf("c rejected the reaction instead of holding it (%d)", got)
	}
	// c's counter of b has not passed the held event: were c to restart
	// now, the hub's log would give it again.
	if have := s.link(2).TestHave(s.stream)[testkit.ServerName(1)]; have >= fromB[0].N {
		t.Fatalf("c's counter of b is %d, at or past the held event's %d", have, fromB[0].N)
	}
	deliver(fromB[1], fromB[1].Prev) // b's reply to the message: no gap is seen, though the counter did not move
	deliver(fromA[1], 0)             // a's reaction to b's reply (out of a's own order too, as a hub may deliver)
	if held() != 3 {
		t.Fatalf("c holds %d events, want 3: the reaction, the reply, the reaction to the reply", held())
	}
	if gaps := s.link(2).StatsNow().Gaps; gaps != 0 {
		t.Fatalf("c took %d deliveries for gaps", gaps)
	}
	deliver(fromA[0], fromA[0].Prev) // and now the message itself
	if held() != 0 {
		t.Fatalf("c still holds %d events after the message arrived", held())
	}
	if n := reactionsOn(c.Owner, s.conv[2], "the message"); n != 1 {
		t.Fatalf("c shows %d reactions on the message, want 1", n)
	}
	if n := reactionsOn(c.Owner, s.conv[2], "the reply"); n != 1 {
		t.Fatalf("c shows %d reactions on the reply, want 1", n)
	}
	if m := c.Owner.HasMessage(s.conv[2], "the reply"); m["parent_id"] != posted["id"] {
		t.Fatalf("c's copy of the reply: %v", m)
	}
	// The counters have caught up: nothing is asked for again.
	have := s.link(2).TestHave(s.stream)
	if have[testkit.ServerName(1)] != fromB[1].N || have[testkit.ServerName(0)] != fromA[1].N {
		t.Fatalf("c's counters after the hold emptied: %v, want b %d and a %d", have, fromB[1].N, fromA[1].N)
	}
	if st := s.link(2).StatsNow(); st.Held != 3 || st.HeldExpired != 0 || st.Rejected["unknown_message"] != 0 {
		t.Fatalf("c: held %d, expired %d, rejected %v", st.Held, st.HeldExpired, st.Rejected)
	}
	restore()
	s.settle("after")
	s.sameEverywhere("before", "the message", "the reply", "after")
}

// The bounds of holding: an event whose message never comes is rejected
// after the wait, as it would have been at once, and the origin's counter
// then passes it; and a stream holds no more than its limit.
func TestHeldEventsAreBounded(t *testing.T) {
	defer link.SetHoldWait(400 * time.Millisecond)()
	s := share(t, 3, true, nil)
	b := s.Servers[1]
	s.settle("before")
	restore := s.cut(2)
	// b reacts to a message nobody has: one the test makes up.
	nowhere := model.NewID[string]()
	env := s.submit(1, "reaction.added", map[string]any{"message_id": nowhere, "account_id": b.Owner.ID, "emoji": "👍"})
	s.link(2).TestDeliver(s.stream, env, env.Cursor, env.Prev)
	if got := s.link(2).TestHeld(s.stream); got != 1 {
		t.Fatalf("c holds %d events, want 1", got)
	}
	testkit.Eventually(t, "the held event is given up", func() bool { return s.link(2).TestHeld(s.stream) == 0 })
	s.rejected(2, "unknown_message", 1)
	if st := s.link(2).StatsNow(); st.HeldExpired != 1 {
		t.Fatalf("c gave up %d held events, want 1", st.HeldExpired)
	}
	if have := s.link(2).TestHave(s.stream)[testkit.ServerName(1)]; have != env.N {
		t.Fatalf("c's counter of b is %d after the hold let go, want %d", have, env.N)
	}
	restore()
	s.settle("after")
	s.sameEverywhere("before", "after")
}

// What is held is in memory. A server that restarts while it holds an event
// is given it again by the hub's log, with the message it was waiting for,
// and ends as every other server did.
func TestHeldEventsSurviveARestart(t *testing.T) {
	s := share(t, 3, true, nil)
	a, b, c := s.Servers[0], s.Servers[1], s.Servers[2]
	s.settle("before")
	restore := s.cut(2)
	s.ann.Post(s.conv[0], "the message")
	onB := b.Owner.HasMessage(s.conv[1], "the message")
	b.Owner.Must(200, "PUT", "/api/v1/messages/"+onB["id"].(string)+"/reactions/👍", nil)
	b.Owner.Post(s.conv[1], "and b goes on")
	a.Owner.HasMessage(s.conv[0], "and b goes on")
	var fromB []wire.Envelope
	for _, env := range s.missedBy(2) {
		if env.Origin == testkit.ServerName(1) {
			fromB = append(fromB, env)
		}
	}
	if len(fromB) != 2 {
		t.Fatalf("the hub's log holds %d envelopes of b that c has not got", len(fromB))
	}
	// c is handed b's two, and not a's message: the reaction is held, and
	// what b said after it is applied.
	for _, env := range fromB {
		s.link(2).TestDeliver(s.stream, env, env.Cursor, env.Prev)
	}
	c.Owner.HasMessage(s.conv[2], "and b goes on")
	if got := s.link(2).TestHeld(s.stream); got != 1 {
		t.Fatalf("c holds %d events, want 1", got)
	}
	if have := s.link(2).TestHave(s.stream)[testkit.ServerName(1)]; have >= fromB[0].N {
		t.Fatalf("c's counter of b is %d, at or past the held event's %d", have, fromB[0].N)
	}
	// c restarts. What it held is gone; its counters are what it wrote down.
	c.Restart()
	if got := s.link(2).TestHeld(s.stream); got != 0 {
		t.Fatalf("a restarted server holds %d events", got)
	}
	restore()
	testkit.Eventually(t, "c ends with the message and its reaction", func() bool { return reactionsOn(c.Owner, s.conv[2], "the message") == 1 })
	s.settle("after")
	s.sameEverywhere("before", "the message", "and b goes on", "after")
	if n := countText(c.Owner, s.conv[2], "and b goes on"); n != 1 {
		t.Fatalf("what was applied before the restart is there %d times", n)
	}
}
