package link_test

import (
	"fmt"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/link"
	"github.com/tikaydaes/churr/internal/link/wire"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/testkit"
)

// The cases of order under ruling R57: nobody numbers events. Each event
// carries a stamp its origin signed, a conversation is shown by the stamps,
// and servers that were cut off from each other end up showing the same
// thing in the same order.

// isMember reports whether an account is a member of server i's copy.
func (s *shared) isMember(i int, account model.AccountID) bool {
	_, found, err := s.Servers[i].Server.Store.Members().Get(ctx, model.ConversationID(s.conv[i]), account)
	return err == nil && found
}

// cut takes server i off its link (a direct link, or its connection to the
// hub) and returns the function that puts it back.
func (s *shared) cut(i int) func() {
	s.t.Helper()
	path := "/api/v1/admin/peers/" + s.Peers[i]
	s.Servers[i].Owner.Must(200, "PATCH", path, map[string]any{"state": "paused"})
	testkit.Eventually(s.t, testkit.ServerName(i)+" is cut off", func() bool { return !s.link(i).TestConnected(model.PeerID(s.Peers[i])) })
	return func() {
		s.t.Helper()
		s.Servers[i].Owner.Must(200, "PATCH", path, map[string]any{"state": "active"})
		testkit.Eventually(s.t, testkit.ServerName(i)+" is back", func() bool { return s.link(i).TestConnected(model.PeerID(s.Peers[i])) })
	}
}

// skewHub puts the hub's clock, and so the reference clock, 40 seconds on, and
// waits until every server has been told so. A clock does not jump like
// that; a server that has not heard of it yet takes an event stamped by the
// new time for one 40 seconds ahead of the reference, and throws it away.
func (s *shared) skewHub() {
	s.t.Helper()
	var was []int64
	for i := range s.Servers {
		told, _ := s.link(i).TestTold(model.PeerID(s.Peers[i]))
		was = append(was, told.Offset)
	}
	s.Hub.Skew(40 * time.Second)
	testkit.Eventually(s.t, "every server is told its clock against the hub's new time", func() bool {
		for i := range s.Servers {
			told, _ := s.link(i).TestTold(model.PeerID(s.Peers[i]))
			if d := told.Offset - was[i]; d > -30000 && d < 30000 {
				return false
			}
		}
		return true
	})
}

// among returns the texts of `all` that are in `of`, in the order of `all`.
func among(all []string, of ...string) []string {
	want := map[string]bool{}
	for _, t := range of {
		want[t] = true
	}
	var out []string
	for _, t := range all {
		if want[t] {
			out = append(out, t)
		}
	}
	return out
}

// sameEverywhere waits until every server shows these texts, in this order
// by `order`, and then checks that each message has one id and one order
// string on all of them.
func (s *shared) sameEverywhere(want ...string) {
	s.t.Helper()
	for i := range s.Servers {
		var got []string
		ok := false
		deadline := time.Now().Add(10 * time.Second)
		for !ok && time.Now().Before(deadline) {
			got = among(s.order(i), want...)
			if ok = reflect.DeepEqual(got, want); !ok {
				time.Sleep(20 * time.Millisecond)
			}
		}
		if !ok {
			s.t.Fatalf("%s shows\n  %q\nwant\n  %q", testkit.ServerName(i), got, want)
		}
	}
	for _, text := range want {
		first := s.Servers[0].Owner.HasMessage(s.conv[0], text)
		for i := 1; i < len(s.Servers); i++ {
			m := s.Servers[i].Owner.HasMessage(s.conv[i], text)
			if m["id"] != first["id"] || m["order"] != first["order"] {
				s.t.Fatalf("%q is %v at %v on a and %v at %v on %s", text, first["id"], first["order"], m["id"], m["order"], testkit.ServerName(i))
			}
		}
	}
}

func (s *shared) unread(c *testkit.Client, conv string) float64 {
	m := c.Must(200, "GET", "/api/v1/conversations/"+conv, nil)["conversation"].(map[string]any)["membership"].(map[string]any)
	n, _ := m["unread_count"].(float64)
	return n
}

// Cases 30, 31 and 34, as they are without a sequencer. A server is cut off
// from the others, directly or through a hub. Both sides go on writing: each
// shows its own messages at once, in place, the cut-off side's marked
// unsent. When the link returns every server shows the same messages in the
// same order, the order of the stamps; a message that arrives below what a
// server had already shown is marked late there and is unread; what was
// edited or deleted while cut off arrives edited or does not arrive.
func TestCutOffServersConvergeOnOneOrder(t *testing.T) {
	for _, hub := range []bool{false, true} {
		t.Run(fmt.Sprintf("hub=%v", hub), func(t *testing.T) {
			defer link.SetDialBase(20 * time.Millisecond)()
			n := 2
			if hub {
				n = 3
			}
			s := share(t, n, hub, nil)
			z := n - 1 // the server that is cut off: b on a direct link, c through a hub
			zo := s.Servers[z].Owner
			s.settle("before the cut")
			first := zo.HasMessage(s.conv[z], "before the cut")
			restore := s.cut(z)

			a1 := s.ann.Post(s.conv[0], "a, first")
			time.Sleep(30 * time.Millisecond)
			kept := zo.Post(s.conv[z], "written while cut off")
			gone := zo.Post(s.conv[z], "deleted before it was sent")
			// Stored and placed at once, with a seq of its own, and unsent.
			if kept["seq"] == nil || kept["order"] == nil || kept["unsent"] != true || kept["late"] == true {
				t.Fatalf("a message posted while cut off: %v", kept)
			}
			shown := s.order(z)
			if len(shown) < 2 || shown[len(shown)-2] != "written while cut off" || shown[len(shown)-1] != "deleted before it was sent" {
				t.Fatalf("the cut-off server does not show its own messages in place at once: %q", shown)
			}
			// Edited and deleted while they wait, and a reaction to an older
			// message: all made at once here, all to be sent.
			zo.Must(200, "PATCH", "/api/v1/messages/"+kept["id"].(string), map[string]any{"text": "written while cut off, then corrected"})
			zo.Must(200, "DELETE", "/api/v1/messages/"+gone["id"].(string), nil)
			zo.Must(200, "PUT", "/api/v1/messages/"+first["id"].(string)+"/reactions/👍", nil)
			time.Sleep(30 * time.Millisecond)
			a2 := s.ann.Post(s.conv[0], "a, second")
			s.ann.Must(200, "PUT", "/api/v1/conversations/"+s.conv[0]+"/read", map[string]any{"seq": a2["seq"]})
			if s.unread(s.ann, s.conv[0]) != 0 {
				t.Fatal("ann has unread messages before anything arrived")
			}
			if s.link(z).TestOutbox(s.stream) == 0 {
				t.Fatal("nothing waits on the cut-off server")
			}

			restore()
			want := []string{"before the cut", "a, first", "written while cut off, then corrected", "a, second"}
			s.sameEverywhere(want...)
			s.settle("after the cut")
			for i := range s.Servers {
				if i == z {
					continue
				}
				o := s.Servers[i].Owner
				absent(t, o, s.conv[i], "deleted before it was sent")
				absent(t, o, s.conv[i], "written while cut off")
				// Arrived after "a, second", which is ordered after it: late.
				m := o.HasMessage(s.conv[i], "written while cut off, then corrected")
				if m["id"] != kept["id"] || m["late"] != true || m["unsent"] == true {
					t.Fatalf("the cut-off server's message on %s: %v", testkit.ServerName(i), m)
				}
				if o.HasMessage(s.conv[i], "a, second")["late"] == true || o.HasMessage(s.conv[i], "a, first")["late"] == true {
					t.Fatalf("messages that arrived in order are marked late on %s", testkit.ServerName(i))
				}
			}
			// Late, and unread like anything new: its seq on a is above
			// everything a had, and ann's count includes it.
			onA := s.ann.HasMessage(s.conv[0], "written while cut off, then corrected")
			if onA["seq"].(float64) <= a2["seq"].(float64) {
				t.Fatalf("the late message's arrival number %v is not above %v", onA["seq"], a2["seq"])
			}
			if n := s.unread(s.ann, s.conv[0]); n != 1 {
				t.Fatalf("ann has %v unread messages, want the one that arrived late", n)
			}
			// On the cut-off server it is the other way round: "a, first"
			// arrived below its own message.
			if zo.HasMessage(s.conv[z], "a, first")["late"] != true || zo.HasMessage(s.conv[z], "a, second")["late"] == true {
				t.Fatalf("late on the cut-off server: %v, %v", zo.HasMessage(s.conv[z], "a, first"), zo.HasMessage(s.conv[z], "a, second"))
			}
			if zo.HasMessage(s.conv[z], "a, first")["id"] != a1["id"] {
				t.Fatal("ids differ")
			}
			// Acknowledged: nothing waits, nothing is unsent.
			testkit.Eventually(t, "the cut-off server's outbox is empty", func() bool { return s.link(z).TestOutbox(s.stream) == 0 })
			if zo.HasMessage(s.conv[z], "written while cut off, then corrected")["unsent"] == true {
				t.Fatal("still unsent after it was acknowledged")
			}
			// The reaction made while cut off arrived too.
			testkit.Eventually(t, "the reaction reaches a", func() bool {
				for _, m := range s.ann.Messages(s.conv[0]) {
					if m["id"] == first["id"] {
						return len(arr(m["reactions"])) == 1
					}
				}
				return false
			})
		})
	}
}

// Case 32: the same envelope submitted twice is taken once. Case 33: an
// envelope delivered twice is applied once. Cases 35 and 37: a server that
// is handed an envelope while it lacks an earlier one of the same origin
// does not take it; it catches up, and ends with both, in order.
func TestDuplicatesAndGaps(t *testing.T) {
	defer link.SetDialBase(20 * time.Millisecond)()
	s := share(t, 2, false, nil)
	a, b := s.Servers[0], s.Servers[1]
	// 32: b submits one envelope twice.
	fields, _ := message(b.Owner.ID, "submitted twice")
	env, _ := s.link(1).TestSealNext(s.stream, "message.created", fields, 0)
	for i := 0; i < 2; i++ {
		if answer, err := s.link(1).TestSubmit(s.stream, env); err != nil || !link.Took(answer, env) {
			t.Fatalf("submit %d: %+v %v", i, answer, err)
		}
	}
	s.settle("after the double submit")
	if n := countText(a.Owner, s.conv[0], "submitted twice"); n != 1 {
		t.Fatalf("a holds it %d times", n)
	}
	logged := 0
	for _, e := range s.link(0).TestLog(s.stream) {
		if e.OriginEventID == env.OriginEventID {
			logged++
		}
	}
	if logged != 1 {
		t.Fatalf("the log holds the envelope %d times", logged)
	}

	// 33: a's log delivers one envelope to b a second time.
	s.ann.Post(s.conv[0], "delivered twice")
	b.Owner.HasMessage(s.conv[1], "delivered twice")
	log := s.link(0).TestLog(s.stream)
	last := log[len(log)-1]
	s.link(1).TestDeliver(s.stream, last, last.Cursor+50, last.Prev)
	s.rejected(1, "duplicate", 1)
	if n := countText(b.Owner, s.conv[1], "delivered twice"); n != 1 {
		t.Fatalf("an envelope delivered twice was applied %d times", n)
	}

	// 35, 37: b is cut off; a writes two messages; b is handed the second
	// without the first.
	restore := s.cut(1)
	s.ann.Post(s.conv[0], "the one b missed")
	s.ann.Post(s.conv[0], "the one after it")
	log = s.link(0).TestLog(s.stream)
	second := log[len(log)-1]
	held := s.link(1).TestHave(s.stream)["a.localhost"]
	if second.Prev <= held {
		t.Fatalf("the log says a's envelope before this one is %d and b holds %d: no gap to see", second.Prev, held)
	}
	s.link(1).TestDeliver(s.stream, second, second.Cursor, second.Prev)
	absent(t, b.Owner, s.conv[1], "the one after it") // not taken: something is missing before it
	if s.link(1).StatsNow().Gaps < 1 || s.link(1).TestHave(s.stream)["a.localhost"] != held {
		t.Fatalf("b took an envelope past a gap: holds %d, held %d", s.link(1).TestHave(s.stream)["a.localhost"], held)
	}
	restore()
	s.sameEverywhere("delivered twice", "the one b missed", "the one after it")
	if s.link(1).TestHave(s.stream)["a.localhost"] != second.N {
		t.Fatalf("b holds a's events up to %d, the log up to %d", s.link(1).TestHave(s.stream)["a.localhost"], second.N)
	}
}

// Case 36: a server offline longer than the log is told the log no longer
// reaches back to what it holds, asks the owner for what was made since, and
// ends with every message, each in the place it has on the owner.
func TestOfflineLongerThanTheLog(t *testing.T) {
	defer link.SetDialBase(20 * time.Millisecond)()
	s := share(t, 2, false, nil)
	a, b := s.Servers[0], s.Servers[1]
	s.settle("level")
	restore := s.cut(1)
	want := []string{"level"}
	for i := 0; i < 4; i++ {
		want = append(want, fmt.Sprintf("long ago %d", i))
		s.ann.Post(s.conv[0], want[len(want)-1])
	}
	// The log's thirty days pass for everything so far.
	if n, err := a.Server.Store.Streams().PruneLog(ctx, model.Now().Add(time.Hour)); err != nil || n == 0 {
		t.Fatal(n, err)
	}
	s.ann.Post(s.conv[0], "still in the log")
	restore()
	s.sameEverywhere(append(want, "still in the log")...)
	audit := fmt.Sprint(b.Owner.Must(200, "GET", "/api/v1/admin/audit?action=link.log_gap", nil))
	if !strings.Contains(audit, "a.localhost") || !strings.Contains(audit, "link.log_gap") {
		t.Fatalf("the audit log does not say the log had a gap: %v", audit)
	}
}

// What a time reference refuses, on a direct link (the owner server is the
// reference) and through a hub (the hub is): a stamp from the future, a
// stamp from the past while the origin was connected all along, a stamp
// made with an offset that is not the one the reference measured, and a
// stamp that does not come after the origin's last. Nothing refused is
// shown anywhere or kept in the log. A stamp from the past is taken when it
// falls in a stretch the origin really was disconnected for.
func TestStampsAreCheckedByTheReference(t *testing.T) {
	for _, hub := range []bool{false, true} {
		t.Run(fmt.Sprintf("hub=%v", hub), func(t *testing.T) {
			defer link.SetDialBase(20 * time.Millisecond)()
			n := 2
			if hub {
				n = 3
			}
			s := share(t, n, hub, nil)
			b := s.Servers[1]
			s.settle("before")
			// Every envelope below is made by hand, with b's keys, around a
			// stamp b really took: b's own counter and clock move with them,
			// so what b makes afterwards in the ordinary way follows on.
			stamp := func() wire.Stamp {
				t.Helper()
				st, err := s.link(1).TestStamp(s.stream)
				if err != nil {
					t.Fatal(err)
				}
				return st
			}
			try := func(text string, stamp wire.Stamp) (wire.Frame, wire.Envelope) {
				t.Helper()
				fields, _ := message(b.Owner.ID, text)
				env, err := s.link(1).TestSeal(s.stream, "message.created", fields, stamp, 0)
				if err != nil {
					t.Fatal(err)
				}
				answer, err := s.link(1).TestSubmit(s.stream, env)
				if err != nil {
					t.Fatal(err)
				}
				return answer, env
			}
			var refused []string
			refusedIDs := map[string]string{}
			refuse := func(text string, stamp wire.Stamp, code string) {
				t.Helper()
				answer, env := try(text, stamp)
				if answer.Code != code || link.Took(answer, env) || answer.OriginEventID != env.OriginEventID {
					t.Fatalf("%q: want it refused with %s, got %+v", text, code, answer)
				}
				if code != wire.ENotMonotonic && (answer.Clock == nil || answer.Clock.Samples == 0) {
					t.Fatalf("%q: the refusal does not carry the reference's figures for the origin's clock: %+v", text, answer)
				}
				refused = append(refused, text)
				refusedIDs[env.OriginEventID] = text
			}
			st := stamp()
			refuse("postdated", wire.Stamp{At: st.At + 5*60_000, N: st.N, Offset: st.Offset}, wire.EClockAhead)
			st = stamp()
			refuse("backdated", wire.Stamp{At: st.At - 5*60_000, N: st.N, Offset: st.Offset}, wire.EClockBehind)
			// The time is honest but the offset it claims to rest on is not
			// what the reference measured: two seconds out, far beyond the
			// tolerance on a path this short.
			st = stamp()
			refuse("forged offset", wire.Stamp{At: st.At, N: st.N, Offset: st.Offset + 2000}, wire.EClockOffset)
			st = stamp()
			refuse("forged offset, the other way", wire.Stamp{At: st.At, N: st.N, Offset: st.Offset - 2000}, wire.EClockOffset)
			// One that is sound is taken, and shown.
			sound := stamp()
			if answer, env := try("sound", sound); !link.Took(answer, env) || answer.Code != "" {
				t.Fatalf("a sound stamp was refused: %+v", answer)
			}
			s.ann.HasMessage(s.conv[0], "sound")
			// After it, the origin's time and counter only go up.
			st = stamp()
			refuse("the same counter again", wire.Stamp{At: st.At, N: sound.N, Offset: st.Offset}, wire.ENotMonotonic)
			refuse("time going back", wire.Stamp{At: sound.At - 1, N: st.N, Offset: st.Offset}, wire.ENotMonotonic)
			refuse("the same time again", wire.Stamp{At: sound.At, N: st.N, Offset: st.Offset}, wire.ENotMonotonic)

			s.settle("after the refusals")
			var log []wire.Envelope
			if hub {
				log = s.Hub.Hub.TestLog(string(s.stream))
			} else {
				log = s.link(0).TestLog(s.stream)
			}
			if len(log) == 0 {
				t.Fatal("the reference's log is empty")
			}
			for _, e := range log {
				if text, was := refusedIDs[e.OriginEventID]; was {
					t.Fatalf("%q was refused and is in the reference's log all the same", text)
				}
			}
			for i := range s.Servers {
				if i == 1 {
					continue
				}
				for _, text := range refused {
					absent(t, s.Servers[i].Owner, s.conv[i], text)
				}
			}

			// A server that has never been told its offset by the
			// conversation's time reference cannot make events there at all.
			s.link(1).TestHideClock(model.PeerID(s.Peers[1]), true)
			b.Owner.Refused(503, model.CodeUnavailable, "POST", "/api/v1/conversations/"+s.conv[1]+"/messages", map[string]any{"text": "with no offset"})
			absent(t, b.Owner, s.conv[1], "with no offset")
			s.link(1).TestHideClock(model.PeerID(s.Peers[1]), false)

			// The excuse. b is cut off, writes, and two minutes pass for
			// everyone. When it returns its message is two minutes old, far
			// behind the reference clock, and is taken all the same: it
			// falls in the stretch b was away for.
			restore := s.cut(1)
			made := b.Owner.Post(s.conv[1], "made while away")
			restamped := s.link(1).StatsNow().Restamped
			for i := range s.Servers {
				s.Skew(i, 2*time.Minute)
			}
			if hub {
				s.Hub.Skew(2 * time.Minute)
			}
			restore()
			// ("sound" was never b's own to show: the test made its envelope
			// by hand.)
			s.sameEverywhere("before", "after the refusals", "made while away")
			// Taken as it was stamped, two minutes ago: not refused first and
			// given a newer time.
			if got := s.ann.HasMessage(s.conv[0], "made while away"); got["order"] != made["order"] || s.link(1).StatsNow().Restamped != restamped {
				t.Fatalf("the message made while away was stamped again: %v, as made %v", got["order"], made["order"])
			}
			// The stretch excuses only what was made inside it: a stamp from
			// before it began is as late as it ever was.
			st = stamp()
			answer, env := try("from before it went away", wire.Stamp{At: st.At - 10*60_000, N: st.N, Offset: st.Offset})
			if link.Took(answer, env) || answer.Code != wire.EClockBehind {
				t.Fatalf("a stamp from before the stretch of disconnection: %+v", answer)
			}
		})
	}
}

// A server's clock is set five minutes forward while it is connected. For a
// moment its events are stamped five minutes ahead: the reference refuses
// them. As soon as the reference has measured the clock again and told the
// server, the server stamps the event again, the message moves to where the
// new stamp puts it, and it is taken: the refusal is reported to the origin,
// which sends again with a corrected time.
func TestRefusedStampIsCorrectedAndSentAgain(t *testing.T) {
	for _, hub := range []bool{false, true} {
		t.Run(fmt.Sprintf("hub=%v", hub), func(t *testing.T) {
			defer wire.SetPingEvery(250 * time.Millisecond)()
			n := 2
			if hub {
				n = 3
			}
			s := share(t, n, hub, nil)
			b := s.Servers[1]
			s.settle("before")
			before := s.ann.HasMessage(s.conv[0], "before")
			s.Skew(1, 5*time.Minute)
			began := time.Now()
			posted := b.Owner.Post(s.conv[1], "after my clock jumped")
			if posted["unsent"] != true || posted["order"].(string) <= before["order"].(string) {
				t.Fatalf("as posted: %v", posted)
			}
			wild := model.OrderAt(posted["order"].(string))
			if ahead := wild - int64(model.Now()); ahead < 4*60_000 {
				t.Fatalf("the first stamp is %d ms ahead of the true time; the test means it to be five minutes", ahead)
			}
			// Refused for being ahead of the reference clock, or, if the
			// reference had measured the jump by the time the envelope
			// reached it, for resting on an offset that is no longer true.
			testkit.Eventually(t, "the reference refuses the stamp from the future", func() bool {
				st := s.link(1).StatsNow()
				return st.Dropped["error:"+wire.EClockAhead]+st.Dropped["error:"+wire.EClockOffset] >= 1
			})
			s.sameEverywhere("before", "after my clock jumped")
			// Not by waiting out the outbox's five seconds: as soon as the
			// reference had measured the clock again.
			if took := time.Since(began); took > 4*time.Second {
				t.Fatalf("the corrected stamp took %v to be sent", took)
			}
			final := b.Owner.HasMessage(s.conv[1], "after my clock jumped")
			at := model.OrderAt(final["order"].(string))
			if final["id"] != posted["id"] || final["order"] == posted["order"] || at >= wild {
				t.Fatalf("the message was not stamped again: first %v, now %v", posted["order"], final["order"])
			}
			if off := at - int64(model.Now()); off > 30_000 || off < -30_000 {
				t.Fatalf("the corrected stamp is %d ms from the true time", off)
			}
			if s.link(1).StatsNow().Restamped < 1 {
				t.Fatal("nothing was stamped again")
			}
			testkit.Eventually(t, "it is acknowledged", func() bool {
				return b.Owner.HasMessage(s.conv[1], "after my clock jumped")["unsent"] != true && s.link(1).TestOutbox(s.stream) == 0
			})
			// And what b writes next is not dragged five minutes ahead by
			// the stamp that was refused.
			nextMsg := b.Owner.Post(s.conv[1], "and the next")
			if d := model.OrderAt(nextMsg["order"].(string)) - int64(model.Now()); d > 30_000 {
				t.Fatalf("the next message is stamped %d ms ahead", d)
			}
			s.sameEverywhere("before", "after my clock jumped", "and the next")
		})
	}
}

// A time reference that lets an event from the future through does not get
// it shown: every server that can read the reference clock refuses, for
// itself, an event stamped more than 30 seconds ahead of it. And a time it
// refused does not push its own stamps forward.
func TestReceiversRefuseTheFuture(t *testing.T) {
	s := share(t, 2, false, nil)
	b := s.Servers[1]
	s.settle("before")
	// a is the reference here, and nobody checks what a reference puts in
	// its own log.
	stamp, err := s.link(0).TestStamp(s.stream)
	if err != nil {
		t.Fatal(err)
	}
	fields, _ := message(s.ann.ID, "from five minutes ahead")
	env, _ := s.link(0).TestSeal(s.stream, "message.created", fields, wire.Stamp{At: stamp.At + 5*60_000, N: stamp.N + 1, Offset: 0}, 0)
	if answer, err := s.link(0).TestSubmit(s.stream, env); err != nil || !link.Took(answer, env) {
		t.Fatal(answer, err)
	}
	s.rejected(1, "clock_ahead", 1)
	s.settle("after")
	absent(t, b.Owner, s.conv[1], "from five minutes ahead")
	absent(t, s.ann, s.conv[0], "from five minutes ahead")
	// b held the envelope (its counter for a moved) without taking its time.
	if s.link(1).TestHave(s.stream)["a.localhost"] < env.N {
		t.Fatalf("b holds a's events up to %d, the refused one was %d", s.link(1).TestHave(s.stream)["a.localhost"], env.N)
	}
	next := b.Owner.Post(s.conv[1], "b carries on")
	if ahead := model.OrderAt(next["order"].(string)) - int64(model.Now()); ahead > 30_000 {
		t.Fatalf("b's next message is stamped %d ms ahead: the refused time pushed it", ahead)
	}
	s.sameEverywhere("before", "after", "b carries on")
}

// A hub decides nothing about order. It may deliver what it carries in any
// order it likes: the servers show the same thing. It may not move an event
// in time or renumber it: the stamp is under the origin's signature, and a
// changed one is rejected by whoever receives it.
func TestAHubCannotReorder(t *testing.T) {
	defer link.SetDialBase(20 * time.Millisecond)()
	s := share(t, 3, true, nil)
	b, c := s.Servers[1], s.Servers[2]
	s.settle("before")
	restore := s.cut(2)
	want := []string{"before"}
	for i := 0; i < 3; i++ {
		want = append(want, fmt.Sprintf("from a %d", i), fmt.Sprintf("from b %d", i))
		s.ann.Post(s.conv[0], fmt.Sprintf("from a %d", i))
		time.Sleep(15 * time.Millisecond)
		b.Owner.Post(s.conv[1], fmt.Sprintf("from b %d", i))
		time.Sleep(15 * time.Millisecond)
	}
	s.Servers[0].Owner.HasMessage(s.conv[0], "from b 2")
	b.Owner.HasMessage(s.conv[1], "from a 2")
	log := s.Hub.Hub.TestLog(string(s.stream))
	held := s.link(2).TestHave(s.stream)
	var missed []wire.Envelope
	for _, env := range log {
		if env.N > held[env.Origin] {
			missed = append(missed, env)
		}
	}
	if len(missed) < 6 {
		t.Fatalf("the hub's log holds %d envelopes c has not got", len(missed))
	}
	// A hub that tampers: the newest envelope with its time moved back a
	// minute, then with its counter changed. Neither is applied.
	tampered := missed[len(missed)-1]
	tampered.At -= 60_000
	s.link(2).TestDeliver(s.stream, tampered, tampered.Cursor, 0)
	renumbered := missed[len(missed)-1]
	renumbered.N += 1000
	s.link(2).TestDeliver(s.stream, renumbered, renumbered.Cursor, 0)
	s.rejected(2, "bad_signature", 2)
	absent(t, c.Owner, s.conv[2], "from b 2")
	// A hub that reorders: c is handed everything it missed backwards.
	for i := len(missed) - 1; i >= 0; i-- {
		s.link(2).TestDeliver(s.stream, missed[i], missed[i].Cursor, 0)
	}
	if got := among(s.order(2), want...); !reflect.DeepEqual(got, want) {
		t.Fatalf("handed the log backwards, c shows\n  %q\nwant\n  %q", got, want)
	}
	// Arrival order is still c's own business: its seq runs the other way
	// from its order, and all but the newest arrived late.
	late := 0
	for _, text := range want[1:] {
		if c.Owner.HasMessage(s.conv[2], text)["late"] == true {
			late++
		}
	}
	newest, oldest := c.Owner.HasMessage(s.conv[2], "from b 2"), c.Owner.HasMessage(s.conv[2], "from a 0")
	if late != len(want)-2 || newest["seq"].(float64) >= oldest["seq"].(float64) || newest["late"] == true {
		t.Fatalf("%d of %d arrived late; newest %v, oldest %v", late, len(want)-1, newest, oldest)
	}
	restore()
	s.settle("after")
	s.sameEverywhere(append(want, "after")...)
}

// Two edits to one message are settled by the later stamp, whichever
// arrives first.
func TestTwoEditsAreSettledByTheLaterStamp(t *testing.T) {
	s := share(t, 2, false, nil)
	b := s.Servers[1]
	posted := s.ann.Post(s.conv[0], "as first written")
	b.Owner.HasMessage(s.conv[1], "as first written")
	edit := func(text string) wire.Envelope {
		stamp, err := s.link(0).TestStamp(s.stream)
		if err != nil {
			t.Fatal(err)
		}
		env, err := s.link(0).TestSeal(s.stream, "message.edited", map[string]any{"message": map[string]any{"id": posted["id"], "author_id": s.ann.ID,
			"text": text, "created_at": model.Now(), "edited_at": model.Now()}, "blobs": []any{}}, stamp, 0)
		if err != nil {
			t.Fatal(err)
		}
		return env
	}
	earlier, later := edit("the earlier edit"), edit("the later edit")
	// The later one arrives first.
	s.link(1).TestDeliver(s.stream, later, 1000, 0)
	b.Owner.HasMessage(s.conv[1], "the later edit")
	s.link(1).TestDeliver(s.stream, earlier, 1001, 0)
	s.rejected(1, "superseded", 1)
	absent(t, b.Owner, s.conv[1], "the earlier edit")
	if m := b.Owner.HasMessage(s.conv[1], "the later edit"); m["id"] != posted["id"] || m["order"] != posted["order"] {
		t.Fatalf("an edit moved the message or made another: %v, posted %v", m, posted)
	}
}

// One call that makes several events (an owner adds three members at once)
// while another server writes. The call succeeds at once, without waiting
// for anybody; its events reach the reference in the order they were made,
// however the frames fall; and every server ends up with the same members
// and the same conversation. Directly and through a hub.
func TestInterleavedTransactionIsPublishedInOrder(t *testing.T) {
	for _, hub := range []bool{false, true} {
		t.Run(fmt.Sprintf("hub=%v", hub), func(t *testing.T) {
			n := 2
			if hub {
				n = 3
			}
			s := share(t, n, hub, nil)
			b := s.Servers[1]
			s.settle("before")
			bea, bob, cy := b.Member("bea"), b.Member("bob"), b.Member("cy")
			keeper := s.keeper(1)
			live, _ := b.Owner.Events(nil)
			defer live.Close()
			// b's frames leave one event at a time, and a writes between
			// the first and the second.
			var sent atomic.Int32
			b.Server.Link.TestSubmitting(func(_ model.StreamID, f wire.Frame) wire.Frame {
				if len(f.Envelopes) > 1 {
					f.Envelopes = f.Envelopes[:1]
				}
				if sent.Add(1) == 2 {
					s.ann.Post(s.conv[0], "in between")
				}
				return f
			})
			defer b.Server.Link.TestSubmitting(nil)
			keeper.Add(s.conv[1], "member", bea, bob, cy)
			// Made at once on b, in one piece, in seq order.
			last := float64(0)
			for i := 0; i < 3; i++ {
				e := live.Expect("member.joined")
				if seq := e["seq"].(float64); seq <= last {
					t.Fatalf("b published seq %v after %v", seq, last)
				} else {
					last = seq
				}
			}
			for _, who := range []*testkit.Client{bea, bob, cy} {
				if !s.isMember(1, who.ID) {
					t.Fatal("the call returned before its members were members")
				}
			}
			for i := range s.Servers {
				for _, who := range []*testkit.Client{bea, bob, cy} {
					testkit.Eventually(t, "everyone is a member on "+testkit.ServerName(i), func() bool { return s.isMember(i, who.ID) })
				}
			}
			// The three are accepted as members everywhere: what they say
			// is shown everywhere.
			cy.Post(s.conv[1], "cy is in")
			s.sameEverywhere("before", "in between", "cy is in")
			testkit.Eventually(t, "nothing waits on b", func() bool { return s.link(1).TestOutbox(s.stream) == 0 })
		})
	}
}

// A submit is cut short: of a call's two events the reference is sent only
// the first. The call has succeeded already, at once. The second event
// waits in the outbox and goes when the outbox is next tried; the outcome is
// the same everywhere. Directly and through a hub.
func TestSubmissionThatIsCutShortPartWay(t *testing.T) {
	for _, hub := range []bool{false, true} {
		t.Run(fmt.Sprintf("hub=%v", hub), func(t *testing.T) {
			n := 2
			if hub {
				n = 3
			}
			s := share(t, n, hub, nil)
			b := s.Servers[1]
			s.settle("before")
			bea, bob := b.Member("bea"), b.Member("bob")
			keeper := s.keeper(1)
			// The connection loses everything but the first envelope of the
			// first frame, and every frame after it.
			var frames atomic.Int32
			b.Server.Link.TestSubmitting(func(_ model.StreamID, f wire.Frame) wire.Frame {
				if frames.Add(1) == 1 {
					f.Envelopes = f.Envelopes[:1]
					return f
				}
				f.Envelopes = nil
				return f
			})
			keeper.Add(s.conv[1], "member", bea, bob)
			if !s.isMember(1, bea.ID) || !s.isMember(1, bob.ID) {
				t.Fatal("the call did not make its change at once")
			}
			for i := range s.Servers {
				testkit.Eventually(t, "bea is a member on "+testkit.ServerName(i), func() bool { return s.isMember(i, bea.ID) })
			}
			time.Sleep(100 * time.Millisecond)
			if s.isMember(0, bob.ID) || s.link(1).TestOutbox(s.stream) != 1 {
				t.Fatalf("the event that was never sent: a member on a %v, %d waiting on b", s.isMember(0, bob.ID), s.link(1).TestOutbox(s.stream))
			}
			// The connection mends, and the outbox is tried again.
			b.Server.Link.TestSubmitting(nil)
			s.link(1).TestKick(s.stream)
			for i := range s.Servers {
				testkit.Eventually(t, "bob is a member on "+testkit.ServerName(i), func() bool { return s.isMember(i, bob.ID) })
			}
			b.Owner.Post(s.conv[1], "still working")
			s.sameEverywhere("before", "still working")
			testkit.Eventually(t, "nothing waits on b", func() bool { return s.link(1).TestOutbox(s.stream) == 0 })
		})
	}
}

// A server that has been given the key of an epoch the owner has not started
// yet goes on under the current one until it has. An envelope that is ahead
// of its epoch all the same is refused by the time reference, and taken once
// the epoch has started, with the stamp it had.
func TestEnvelopeAheadOfItsEpochWaitsForIt(t *testing.T) {
	s := share(t, 2, false, nil)
	a, b := s.Servers[0], s.Servers[1]
	s.settle("before")
	epoch, err := s.link(0).TestNewEpoch(s.stream)
	if err != nil {
		t.Fatal(err)
	}
	testkit.Eventually(t, "b holds the new key", func() bool { return s.link(1).TestKeys(s.stream)[epoch] != nil })
	// b behaves: the epoch has not started, so it does not use the key.
	b.Owner.Post(s.conv[1], "under the epoch that is current")
	a.Owner.HasMessage(s.conv[0], "under the epoch that is current")
	// One that does not wait.
	fields, _ := message(b.Owner.ID, "ahead of its epoch")
	stamp, _ := s.link(1).TestStamp(s.stream)
	early, err := s.link(1).TestSeal(s.stream, "message.created", fields, stamp, epoch)
	if err != nil || early.KeyEpoch != epoch {
		t.Fatal(early.KeyEpoch, err)
	}
	if answer, err := s.link(1).TestSubmit(s.stream, early); err != nil || answer.Code != wire.EBadFrame || link.Took(answer, early) {
		t.Fatalf("an envelope ahead of its epoch: %+v %v", answer, err)
	}
	if s.link(0).StatsNow().Refused[wire.EBadFrame] < 1 {
		t.Fatal("the refusal was not counted")
	}
	absent(t, a.Owner, s.conv[0], "ahead of its epoch")
	for _, e := range s.link(0).TestLog(s.stream) {
		if e.OriginEventID == early.OriginEventID {
			t.Fatal("the refused envelope is in a's log")
		}
	}
	if err := s.link(0).TestStartEpoch(s.stream, epoch); err != nil {
		t.Fatal(err)
	}
	if answer, err := s.link(1).TestSubmit(s.stream, early); err != nil || !link.Took(answer, early) {
		t.Fatalf("the same envelope once its epoch has started: %+v %v", answer, err)
	}
	onA := a.Owner.HasMessage(s.conv[0], "ahead of its epoch")
	if onA["order"] != model.OrderString(stamp.At, "b.localhost", stamp.N) {
		t.Fatalf("it does not stand where its stamp put it: %v", onA["order"])
	}
	// From here b uses the new epoch, and the old one is refused.
	testkit.Eventually(t, "b is on the new epoch", func() bool {
		st, _ := s.link(1).TestStream(s.stream)
		return st.KeyEpoch == epoch
	})
	b.Owner.Post(s.conv[1], "under the new epoch")
	a.Owner.HasMessage(s.conv[0], "under the new epoch")
	s.settle("after")
}

// What a member says in a channel between its server accepting an offer and
// the owner adding that server is not lost and does not arrive before its
// author is known: it waits, and is sent after the server's members have
// been announced.
func TestSaidBeforeJoiningIsSentAfterTheMembersAreAnnounced(t *testing.T) {
	defer link.SetDialBase(20 * time.Millisecond)()
	l := testkit.NewLinked(t, 2, false)
	a, b := l.Servers[0], l.Servers[1]
	ann := a.Member("ann")
	ch := a.Owner.Channel("launch", false, ann)
	stream := a.Owner.Must(201, "POST", "/api/v1/admin/streams", map[string]any{"conversation_id": ch, "peer_id": l.Peers[0]})["stream"].(map[string]any)["id"].(string)
	testkit.Eventually(t, "the offer reaches b", func() bool { return l.StreamOn(1, stream) != nil })
	// b is told its clock by a before the link goes: it can stamp.
	testkit.Eventually(t, "b knows its offset", func() bool {
		_, ok := b.Server.Link.TestTold(model.PeerID(l.Peers[1]))
		return ok
	})
	peer := "/api/v1/admin/peers/" + l.Peers[1]
	b.Owner.Must(200, "PATCH", peer, map[string]any{"state": "paused"})
	testkit.Eventually(t, "the link is down", func() bool { return !a.Server.Link.TestConnected(model.PeerID(l.Peers[0])) })
	local := l.Accept(1, stream, "launch")
	said := b.Owner.Post(local, "said before joining")
	if said["unsent"] != true || said["seq"] == nil {
		t.Fatalf("as posted: %v", said)
	}
	b.Owner.Must(200, "PATCH", peer, map[string]any{"state": "active"})
	l.Joined(1, stream)
	got := ann.HasMessage(ch, "said before joining")
	if got["id"] != said["id"] {
		t.Fatalf("on a: %v", got)
	}
	// It was stamped again on the way, after the announcements; both
	// servers agree where it stands now.
	testkit.Eventually(t, "both servers place it alike, and it is acknowledged", func() bool {
		m := b.Owner.HasMessage(local, "said before joining")
		return m["order"] == ann.HasMessage(ch, "said before joining")["order"] && m["unsent"] != true
	})
	if n := a.Server.Link.StatsNow().Rejected["not_a_member"]; n != 0 {
		t.Fatalf("a rejected %d events for an author it had not been told of", n)
	}
}

// A long queue drains at once when the reference takes what it is sent. The
// wait an event carries after an attempt that got no answer is for trying
// again after a silence or a no; once a frame has been taken, what waits
// behind it goes, a frame after a frame, and does not sit out each event's
// own wait in turn.
func TestQueueDrainsAfterARestamp(t *testing.T) {
	s := share(t, 2, false, nil)
	b := s.Servers[1]
	back := s.cut(1)
	const n = 80 // more than two frames' worth
	for i := 0; i < n; i++ {
		b.Owner.Post(s.conv[1], fmt.Sprintf("queued %03d", i))
	}
	// Long enough for the outbox to have tried each of them once, in vain:
	// each now carries a wait of ten seconds.
	time.Sleep(6500 * time.Millisecond)
	if got := s.link(1).TestOutbox(s.stream); got != n {
		t.Fatalf("%d events wait on b, want %d", got, n)
	}
	back()
	start := time.Now()
	testkit.Eventually(t, "the whole queue reaches a", func() bool { return s.link(1).TestOutbox(s.stream) == 0 })
	if took := time.Since(start); took > 4*time.Second {
		t.Fatalf("the queue took %s to drain: it sat out the events' own waits", took.Round(time.Millisecond))
	}
	s.ann.HasMessage(s.conv[0], fmt.Sprintf("queued %03d", n-1))
	var said []string
	for i := 0; i < n; i++ {
		said = append(said, fmt.Sprintf("queued %03d", i))
	}
	if got := among(s.order(0), said...); !reflect.DeepEqual(got, said) {
		t.Fatalf("a shows the queue out of order: %v", got)
	}
}

// A server that makes events faster than its hub takes them is told to slow
// down, and what waits grows old. Once it is more than 30 seconds old the hub
// refuses it for its time, and the server stamps it again and goes on: being
// told to slow down does not use up the tries an event has to be stamped
// again in, and neither does waiting behind an event that was stamped
// again. Both did, on the first real hub: two thousand members added to a
// shared channel in one go left the channel sending nothing, for good.
func TestSlowedDownEventsThatGrowOldAreStampedAgain(t *testing.T) {
	defer wire.SetPingEvery(250 * time.Millisecond)()
	s := share(t, 3, true, nil)
	b := s.Servers[1]
	s.settle("before")
	if err := s.Hub.Hub.SetLimits(testkit.ServerName(1), 60, 0, 0); err != nil {
		t.Fatal(err)
	}
	time.Sleep(1500 * time.Millisecond) // the serving hub takes up new limits within a second
	want := []string{"before"}
	for i := 0; i < 90; i++ {
		text := fmt.Sprintf("burst %02d", i)
		b.Owner.Post(s.conv[1], text)
		want = append(want, text)
	}
	// The hub's clock, and so the reference clock, goes 40 seconds on, six
	// times over: each time what still waits is too old, is refused for it
	// and is stamped again, as a long queue is every 30 seconds. As in a
	// real queue, the event that was refused is taken with its new stamp
	// before the rest grow old again: the clock moving on at once, before
	// the hub had a token for it, had the same event refused six times
	// running, which is not a long queue but a clock that will not agree.
	for i := 0; i < 6; i++ {
		restamped, left := s.link(1).StatsNow().Restamped, 0
		s.skewHub()
		testkit.Eventually(t, "what waits is stamped again", func() bool {
			left = s.link(1).TestOutbox(s.stream)
			return s.link(1).StatsNow().Restamped > restamped
		})
		testkit.Eventually(t, "the event stamped again is taken", func() bool { return s.link(1).TestOutbox(s.stream) < left })
	}
	if left := s.link(1).TestOutbox(s.stream); left < 3 {
		t.Fatalf("%d events are waiting; the test means some still to be", left)
	}
	s.skewHub()
	deadline := time.Now().Add(40 * time.Second)
	for s.link(1).TestOutbox(s.stream) != 0 {
		if time.Now().After(deadline) {
			t.Fatalf("%d events still wait, the last refusal %+v", s.link(1).TestOutbox(s.stream), s.link(1).TestLastError())
		}
		time.Sleep(100 * time.Millisecond)
	}
	s.sameEverywhere(want...)
}

// An event keeps its right to a new stamp however often it was sent. The
// tries of an outbox row count every sending after a wait, and an event that
// had been told to slow down five times, or had gone unanswered five times,
// was never stamped again once it grew old: it was offered with its old stamp
// every five minutes, and nothing of the stream was sent behind it. And an
// event refused for its stamp five times is not given up either: it is
// stamped again after its wait, not at once.
func TestAnEventOutOfTriesIsStillStampedAgain(t *testing.T) {
	defer wire.SetPingEvery(250 * time.Millisecond)()
	s := share(t, 3, true, nil)
	b := s.Servers[1]
	s.settle("before")
	// held keeps b's events in its outbox while the test arranges things.
	hold := func() {
		s.link(1).TestSubmitting(func(model.StreamID, wire.Frame) wire.Frame { return wire.Frame{} })
	}
	let := func() {
		s.link(1).TestSubmitting(nil)
		s.link(1).TestKick(s.stream)
	}

	hold()
	b.Owner.Post(s.conv[1], "sent seven times")
	if err := s.link(1).TestOutboxHistory(s.stream, 7, 0); err != nil {
		t.Fatal(err)
	}
	restamped := s.link(1).StatsNow().Restamped
	s.skewHub() // its stamp is 40 seconds old by the reference clock
	let()
	testkit.Eventually(t, "an event out of tries is stamped again and taken", func() bool {
		return s.link(1).StatsNow().Restamped > restamped && s.link(1).TestOutbox(s.stream) == 0
	})

	hold()
	b.Owner.Post(s.conv[1], "refused five times")
	if err := s.link(1).TestOutboxHistory(s.stream, 5, 5); err != nil {
		t.Fatal(err)
	}
	restamped = s.link(1).StatsNow().Restamped
	s.skewHub()
	let()
	// Not at once: it waits five seconds from when it was last stamped.
	time.Sleep(time.Second)
	if got := s.link(1).StatsNow().Restamped; got != restamped {
		t.Fatalf("an event refused five times was stamped again at once")
	}
	testkit.Eventually(t, "an event refused five times is stamped again after its wait and taken", func() bool {
		return s.link(1).StatsNow().Restamped > restamped && s.link(1).TestOutbox(s.stream) == 0
	})
	s.sameEverywhere("before", "sent seven times", "refused five times")
}
