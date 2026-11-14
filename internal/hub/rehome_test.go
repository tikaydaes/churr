package hub_test

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/hub"
	"github.com/tikaydaes/churr/internal/link/wire"
	"github.com/tikaydaes/churr/internal/testkit"
)

// homeOf returns the hub a hub takes to be a channel's home, "" if it does
// not hold the channel.
func homeOf(t *testing.T, h *testkit.TestHub, id string) string {
	t.Helper()
	for _, ch := range mustChannels(t, h) {
		if ch.ID == id {
			return ch.Home
		}
	}
	return ""
}

// handsOver reports whether a frame is one by which a channel's home is
// offered to another hub: the offer itself, or (as the home was moved before)
// a record that names another hub than the one that signed it.
func handsOver(f wire.Frame) bool {
	if f.Frame == wire.FRehome && f.Step == "offer" {
		return true
	}
	var rec wire.ChannelRecord
	return f.Frame == wire.FChannel && wire.PeekBlock(f, &rec) == nil && rec.Hub != rec.HomeHub
}

// Moving a channel's home is all or nothing (section 23b). The offer is lost
// on its way to the new home: nothing has happened. The old home is still
// the home; once it has had the other hub's word that the offer is dead it
// signs for the channel again, and a late copy of the offer is refused.
// Before, the old home stepped down when it sent the handover, and a
// handover that did not arrive left the channel with no home at all.
func TestRehomeOfferLost(t *testing.T) {
	defer hub.SetRehomeWait(400 * time.Millisecond)()
	c := testkit.NewCluster(t, "line", 2)
	a, b := c.Hubs[0], c.Hubs[1]
	a1, b1 := newSim(t, a, "a1.sim"), newSim(t, b, "b1.sim")
	id := shared(t, "moving", a1, b1)

	// Hub b loses the offer, and keeps what it lost.
	var mu sync.Mutex
	var lost []wire.Frame
	b.Hub.TestTrunkDrop(func(_ string, f wire.Frame) bool {
		if handsOver(f) {
			mu.Lock()
			lost = append(lost, f)
			mu.Unlock()
			return true
		}
		return false
	})
	if err := a.Hub.Rehome(id, b.Name); err != nil {
		t.Fatal(err)
	}
	a.Hub.Reconcile()
	// While the offer is out, the old home is the home and changes nothing.
	if got := homeOf(t, a, id); got != a.Name {
		t.Fatalf("with its offer unanswered, hub a takes the home to be %q", got)
	}
	text := "changed while the offer was out"
	if code := a1.act(id, wire.ModDescribe, func(m *wire.ModAction) { m.Description = &text }); code != wire.EUnavailable {
		t.Fatalf("a change while the home is on offer was answered %q, want unavailable", code)
	}
	// No answer comes. Hub a calls the offer off; hub b says it is dead.
	testkit.Eventually(t, "the move fails, and the home stays", func() bool {
		a.Hub.Reconcile()
		got := a.Hub.RehomeResult(id)
		return strings.HasPrefix(got, "failed:") && strings.Contains(got, "still this hub")
	})
	if got := homeOf(t, a, id); got != a.Name {
		t.Fatalf("after the failed move hub a takes the home to be %q", got)
	}
	if got := homeOf(t, b, id); got != a.Name {
		t.Fatalf("after the failed move hub b takes the home to be %q", got)
	}
	// The channel has its home: a moderator's action is answered, by hub a.
	text = "changed after the failed move"
	testkit.Eventually(t, "the old home signs for the channel again", func() bool {
		return a1.act(id, wire.ModDescribe, func(m *wire.ModAction) { m.Description = &text }) == "" && b1.record(id).Description == text
	})
	if rec := b1.record(id); rec.Hub != a.Name || rec.HomeHub != a.Name {
		t.Fatalf("the record after the failed move is signed by %s for home %s", rec.Hub, rec.HomeHub)
	}
	// The offer turns up late (a trunk that held it back): it is dead.
	b.Hub.TestTrunkDrop(nil)
	mu.Lock()
	late := append([]wire.Frame(nil), lost...)
	mu.Unlock()
	if len(late) == 0 {
		t.Fatal("no offer was ever sent")
	}
	if late[0].Frame == wire.FRehome {
		a.Hub.TestSendOverTrunk(b.Name, late[0])
		time.Sleep(300 * time.Millisecond)
		if got := homeOf(t, b, id); got != a.Name {
			t.Fatalf("hub b took an offer that had been called off: home %q", got)
		}
	}
	// And a move made again, with nothing in its way, goes through.
	if err := a.Hub.Rehome(id, b.Name); err != nil {
		t.Fatal(err)
	}
	testkit.Eventually(t, "the second move is done", func() bool {
		a.Hub.Reconcile()
		return a.Hub.RehomeResult(id) == "done"
	})
	testkit.Eventually(t, "both hubs and both servers know the new home", func() bool {
		return homeOf(t, a, id) == b.Name && homeOf(t, b, id) == b.Name && a1.record(id).Hub == b.Name && b1.record(id).Hub == b.Name
	})
}

// The offer arrives and the answer is lost. The new home is the home from
// the transaction in which it took the offer, and acts as it at once; the
// old home signs nothing until it learns, and then steps down. At no moment
// do both sign, and at no moment is the channel without a home.
func TestRehomeAnswerLost(t *testing.T) {
	defer hub.SetRehomeWait(400 * time.Millisecond)()
	c := testkit.NewCluster(t, "line", 2)
	a, b := c.Hubs[0], c.Hubs[1]
	a1, b1 := newSim(t, a, "a1.sim"), newSim(t, b, "b1.sim")
	id := shared(t, "moving", a1, b1)
	before := a1.record(id).Version

	// Hub a loses everything hub b says about the channel.
	a.Hub.TestTrunkDrop(func(_ string, f wire.Frame) bool { return f.Frame == wire.FRehome || f.Frame == wire.FChannel })
	if err := a.Hub.Rehome(id, b.Name); err != nil {
		t.Fatal(err)
	}
	a.Hub.Reconcile()
	// Hub b has taken the channel, and is its home in every way: its own
	// server holds a record it signed, and its operator can change it.
	testkit.Eventually(t, "hub b is the home", func() bool { return homeOf(t, b, id) == b.Name && b1.record(id).Hub == b.Name })
	if err := b.Hub.Appoint(id, "01940000-0000-7000-8000-000000000001", "bo@b1.sim"); err != nil {
		t.Fatalf("the new home's operator: %v", err)
	}
	testkit.Eventually(t, "the new home's change reaches its server", func() bool { return len(b1.record(id).Moderators) == 2 })
	// Hub a has not heard. It signs nothing: its server's request is put off.
	text := "from the old home's side"
	if code := a1.act(id, wire.ModDescribe, func(m *wire.ModAction) { m.Description = &text }); code != wire.EUnavailable {
		t.Fatalf("the old home, not knowing, answered a change %q, want unavailable", code)
	}
	if rec := a1.record(id); rec.Version != before || rec.Hub != a.Name {
		t.Fatalf("the old home signed version %d (it held %d) while its offer was out", rec.Version, before)
	}
	// It calls the offer off, and is told the offer was taken.
	time.Sleep(600 * time.Millisecond)
	a.Hub.Reconcile()
	if got := a.Hub.RehomeResult(id); !strings.HasPrefix(got, "cancelling") {
		t.Fatalf("with no answer in time the move stands at %q", got)
	}
	a.Hub.TestTrunkDrop(nil)
	testkit.Eventually(t, "the old home learns that the offer was taken, and steps down", func() bool {
		a.Hub.Reconcile()
		return a.Hub.RehomeResult(id) == "done" && homeOf(t, a, id) == b.Name
	})
	// Its server follows the chain: the handover hub a signed, then hub b's.
	testkit.Eventually(t, "the old home's server holds the new home's record", func() bool {
		rec := a1.record(id)
		return rec.Hub == b.Name && len(rec.Moderators) == 2
	})
	testkit.Eventually(t, "a change asked on the old home's side is made by the new home", func() bool {
		return a1.act(id, wire.ModDescribe, func(m *wire.ModAction) { m.Description = &text }) == "" && b1.record(id).Description == text
	})
}
