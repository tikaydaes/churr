package link_test

import (
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/calls/calltest"
	"github.com/tikaydaes/churr/internal/testkit"
)

// joinTicket asks a member's own server to join the conversation's call and
// returns the call's identifier and the ticket.
func joinTicket(t *testing.T, c *testkit.Client, conv string) (string, string) {
	t.Helper()
	out := c.Must(200, "POST", "/api/v1/conversations/"+conv+"/call/join", nil)
	return out["call"].(map[string]any)["id"].(string), out["ticket"].(string)
}

func callOn(c *testkit.Client, conv string) map[string]any {
	status, out := c.Do("GET", "/api/v1/conversations/"+conv+"/call", nil)
	if status != 200 {
		return nil
	}
	call, _ := out["call"].(map[string]any)
	return call
}

// Case 52 with two real servers and the real calls package: a member of B
// joins a call hosted on A. B's server gets the pass from A over the link
// as a sealed direct message, relays the member's signalling the same way,
// and the call's events reach B in the stream; a pass works once.
func TestCallAcrossTwoServers(t *testing.T) {
	s := share(t, 2, false, nil)
	a, b := s.Servers[0], s.Servers[1]

	// Ann starts the call on her server, which hosts it.
	annStream, _ := s.ann.Events(nil)
	ann := calltest.New(t, annStream, calltest.Options{})
	t.Cleanup(ann.Close)
	id, ticket := joinTicket(t, s.ann, s.conv[0])
	ann.MustJoin(id, ticket)

	// B hears of it in the stream, under its own conversation, naming the
	// host.
	testkit.Eventually(t, "b knows of the call", func() bool {
		call := callOn(b.Owner, s.conv[1])
		return call != nil && call["id"] == id && call["hosted_by"] == "a.localhost" && call["conversation_id"] == s.conv[1]
	})
	if call := callOn(s.ann, s.conv[0]); call == nil || call["hosted_by"] != nil {
		t.Fatalf("the host does not name itself: %v", call)
	}

	// B's owner joins through B. The ticket is the host's pass.
	bobStream, _ := b.Owner.Events(nil)
	bob := calltest.New(t, bobStream, calltest.Options{})
	t.Cleanup(bob.Close)
	sameID, pass := joinTicket(t, b.Owner, s.conv[1])
	if sameID != id || pass == "" {
		t.Fatalf("the pass: call %s (want %s), pass %q", sameID, id, pass)
	}
	bob.MustJoin(id, pass)
	for i, c := range []*testkit.Client{s.ann, b.Owner} {
		testkit.Eventually(t, "both are in the call as "+testkit.ServerName(i)+" shows it", func() bool {
			call := callOn(c, s.conv[i])
			return call != nil && len(arr(call["participants"])) == 2
		})
	}
	// He leaves; the host says so in the stream.
	bob.Leave()
	testkit.Eventually(t, "a shows one participant again", func() bool {
		call := callOn(s.ann, s.conv[0])
		return call != nil && len(arr(call["participants"])) == 1
	})
	testkit.Eventually(t, "and so does b", func() bool {
		call := callOn(b.Owner, s.conv[1])
		return call != nil && len(arr(call["participants"])) == 1
	})
	// The pass worked once: it does not let him, or anyone, in again.
	again, _ := b.Owner.Events(nil)
	second := calltest.New(t, again, calltest.Options{})
	t.Cleanup(second.Close)
	if answer := second.Join(id, pass); answer["type"] == "call.ok" {
		t.Fatalf("a used pass let someone in again: %v", answer)
	}
	// Nothing about the call was rejected on either side.
	for i := range s.Servers {
		if r := s.link(i).StatsNow().Rejected; len(r) != 0 {
			t.Fatalf("%s rejected something: %v", testkit.ServerName(i), r)
		}
	}
	_ = a
}

// Section 18: two people on two servers start a call in one conversation at
// the same moment, each before the other's call.started has arrived. Nobody
// numbers events; the call.started with the earlier stamp is the call, on
// every server. Here the two servers cannot hear each other while both
// start, the earlier start is on b (which is neither the stream's owner nor
// its time reference), and when the link returns both servers show b's
// call and a's has ended.
func TestTwoCallsStartedAtOnce(t *testing.T) {
	for _, hub := range []bool{false, true} {
		name, n := "direct", 2
		if hub {
			name, n = "hub", 3
		}
		t.Run(name, func(t *testing.T) {
			s := share(t, n, hub, nil)
			a, b := s.Servers[0], s.Servers[1]
			rejoin := s.cut(1)
			bobStream, _ := b.Owner.Events(nil)
			bob := calltest.New(t, bobStream, calltest.Options{})
			t.Cleanup(bob.Close)
			idB, ticketB := joinTicket(t, b.Owner, s.conv[1])
			bob.MustJoin(idB, ticketB)
			time.Sleep(80 * time.Millisecond)
			annStream, _ := s.ann.Events(nil)
			ann := calltest.New(t, annStream, calltest.Options{})
			t.Cleanup(ann.Close)
			idA, ticketA := joinTicket(t, s.ann, s.conv[0])
			ann.MustJoin(idA, ticketA)
			if idA == idB {
				t.Fatal("the two servers started one call while cut off from each other")
			}
			if call := callOn(s.ann, s.conv[0]); call == nil || call["id"] != idA {
				t.Fatalf("a's own call before the link returns: %v", call)
			}
			rejoin()
			for i, c := range []*testkit.Client{s.ann, b.Owner} {
				testkit.Eventually(t, "the earlier call is the call on "+testkit.ServerName(i), func() bool {
					call := callOn(c, s.conv[i])
					return call != nil && call["id"] == idB
				})
			}
			if call := callOn(s.ann, s.conv[0]); call["hosted_by"] != "b.localhost" {
				t.Fatalf("a does not show b as the host: %v", call)
			}
			if call := callOn(b.Owner, s.conv[1]); call["hosted_by"] != nil {
				t.Fatalf("b's call is not its own: %v", call)
			}
			testkit.Eventually(t, "a gave way and b refused the later start", func() bool {
				return s.link(0).StatsNow().Replaced["call"] == 1 && s.link(1).StatsNow().Rejected["call_lost"] == 1
			})
			if s.link(1).StatsNow().Replaced["call"] != 0 || s.link(0).StatsNow().Rejected["call_lost"] != 0 {
				t.Fatalf("the wrong call gave way: a %v, b %v", s.link(0).StatsNow().Rejected, s.link(1).StatsNow().Replaced)
			}
			if hub {
				// A third server takes the same one, whichever reached it
				// first.
				c := s.Servers[2]
				testkit.Eventually(t, "c shows the earlier call", func() bool {
					call := callOn(c.Owner, s.conv[2])
					return call != nil && call["id"] == idB && call["hosted_by"] == "b.localhost"
				})
			}
			_ = a
		})
	}
}
