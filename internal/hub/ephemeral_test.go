package hub_test

import (
	"strings"
	"testing"

	"github.com/tikaydaes/churr/internal/testkit"
)

// Section 17, ruling R62: a hub under pressure may drop ephemeral frames
// (typing and presence) for a server that has fallen behind, and counts
// them. It never drops an envelope: every message still reaches that
// server, in order, when it reads again. A server that keeps up is passed
// every ephemeral frame as before.
func TestHubDropsPresenceUnderPressureAndNeverMessages(t *testing.T) {
	c := testkit.NewCluster(t, "line", 1)
	h := c.Hubs[0].Hub
	if err := h.SetLimits("", 100000000, 0, 0); err != nil {
		t.Fatal(err)
	}
	h.Reconcile()
	a, b, slow := newSim(t, c.Hubs[0], "a.sim"), newSim(t, c.Hubs[0], "b.sim"), newSim(t, c.Hubs[0], "slow.sim")
	id := shared(t, "storm", a, b, slow)

	// Without pressure everything is passed on to both.
	for i := 0; i < 5; i++ {
		a.tell(id)
	}
	testkit.Eventually(t, "ephemeral frames reach both servers", func() bool { return b.told(id) == 5 && slow.told(id) == 5 })
	if st := h.EphemeralStats(); st.Passed != 10 || st.Dropped != 0 {
		t.Fatalf("with no pressure: %+v", st)
	}
	// What each holds already differs with when it joined; what follows is
	// counted from here.
	had, hadB := slow.count(id), b.count(id)

	// One server stops reading. Messages go on; what is waiting to be
	// written to it grows past the room the test allows.
	const room = 256 << 10
	h.TestEphemeralRoom(room)
	resume := slow.stall()
	text := strings.Repeat("m", 100<<10)
	messages, tells := 0, 5
	say := func() {
		if code := a.say(id, text); code != "" {
			t.Fatalf("message %d was refused: %s", messages, code)
		}
		messages++
	}
	for h.TestQueued("slow.sim") <= room {
		if messages > 400 {
			t.Fatalf("after %d messages only %d bytes wait for the server that is not reading", messages, h.TestQueued("slow.sim"))
		}
		say()
		a.tell(id)
		tells++
	}
	// Now it is backed up: presence for it is dropped, messages are not.
	dealt := func() bool {
		st := h.EphemeralStats()
		return st.Passed+st.Dropped == int64(2*tells)
	}
	testkit.Eventually(t, "the hub has dealt with what was sent so far", dealt)
	before := h.EphemeralStats()
	const more = 40
	for i := 0; i < more; i++ {
		a.tell(id)
		tells++
		if i%4 == 0 {
			say()
		}
	}
	testkit.Eventually(t, "the server that keeps up is passed every ephemeral frame", func() bool { return b.told(id) == tells })
	testkit.Eventually(t, "the hub has dealt with every ephemeral frame", dealt)
	after := h.EphemeralStats()
	if dropped := after.Dropped - before.Dropped; dropped != more {
		t.Fatalf("%d ephemeral frames were sent while a server was backed up and %d were dropped for it", more, dropped)
	}
	if got := h.Status().Ephemeral; got != after {
		t.Fatalf("the status says %+v, the counts are %+v", got, after)
	}
	if !strings.Contains(h.Status().Text(), "dropped under pressure") {
		t.Fatal("the status text does not say what was dropped")
	}

	// It reads again: every message is there, in the order the hub gave
	// them, and only presence is missing.
	resume()
	testkit.Eventually(t, "the slow server is delivered every message", func() bool { return slow.count(id) == had+messages })
	testkit.Eventually(t, "the other server has them too", func() bool { return b.count(id) == hadB+messages })
	if got, want := strings.Join(slow.received(id)[had:], " "), strings.Join(b.received(id)[hadB:], " "); got != want {
		t.Fatalf("the slow server was delivered\n%s\nand the one that kept up\n%s", got, want)
	}
	if got := slow.told(id); int64(got) != int64(tells)-after.Dropped {
		t.Fatalf("the slow server was passed %d ephemeral frames of %d, with %d dropped", got, tells, after.Dropped)
	}
	t.Logf("%d messages of 100 KiB and %d ephemeral frames: the server that fell behind got every message and %d ephemeral frames; %d were dropped for it",
		messages, tells, slow.told(id), after.Dropped)
	a.mu.Lock()
	errs := len(a.errs)
	a.mu.Unlock()
	if errs != 0 {
		t.Fatalf("the hub answered the sender with %d errors", errs)
	}
}
