package hub_test

import (
	"fmt"
	"testing"

	"github.com/tikaydaes/churr/internal/hub"
	"github.com/tikaydaes/churr/internal/testkit"
)

// stoppedFor reports the reason a sim's hub gave it for a stream that has
// stopped for it, "" if it gave none.
func stoppedFor(s *sim, id string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stopped[id]
}

// A hub that leaves its cluster tells its own servers that the channels
// homed on other hubs have stopped for them: the ones connected at once, and
// one that was away when it returns. Before, nothing told them: their copies
// went quiet without a word, while the home hub took them out of the channel.
func TestLeavingHubTellsItsServers(t *testing.T) {
	c := testkit.NewCluster(t, "line", 2)
	a, b := c.Hubs[0], c.Hubs[1]
	a1 := newSim(t, a, "a1.sim")
	b1, b2 := newSim(t, b, "b1.sim"), newSim(t, b, "b2.sim")
	id := shared(t, "far-home", a1, b1, b2)
	// A channel homed on the leaving hub itself, with nobody elsewhere, is
	// nobody's business but its own: it goes on.
	own := shared(t, "own-home", b1, b2)
	b2.close() // away when its hub leaves

	if err := b.Hub.ClusterLeave(); err != nil {
		t.Fatal(err)
	}
	b.Hub.Reconcile()
	testkit.Eventually(t, "hub b stands alone and hub a knows", func() bool {
		return fmt.Sprint(b.Hub.TestMembers()) == "[hub-b.localhost]" && fmt.Sprint(a.Hub.TestMembers()) == "[hub-a.localhost]"
	})
	testkit.Eventually(t, "b1 is told the channel has stopped for it", func() bool { return stoppedFor(b1, id) == "hub_left" })
	if stoppedFor(b1, own) != "" {
		t.Fatal("a channel homed on the hub that left was stopped too")
	}
	// The home hub takes the servers out, as before.
	testkit.Eventually(t, "the home hub takes hub b's servers out", func() bool {
		a.Hub.Reconcile()
		rec := a1.record(id)
		return !rec.Has("b1.sim") && !rec.Has("b2.sim")
	})
	// Nothing more of the channel is taken from them.
	if code := b1.post(id); code == "" {
		t.Fatal("hub b still took an envelope of a channel it has stopped carrying")
	}
	if code := b1.post(own); code != "" {
		t.Fatalf("the hub's own channel: %s", code)
	}
	// The one that was away hears it when it comes back.
	b2.reconnect()
	testkit.Eventually(t, "b2 is told when it returns", func() bool { return stoppedFor(b2, id) == "hub_left" })
	if stoppedFor(b2, own) != "" {
		t.Fatal("a channel homed on the hub that left was stopped for the server that was away")
	}
}

// `cluster leave` run while the hub is not serving: the neighbour is told
// when the hub next runs. And run while the trunk is down: when the trunk
// next comes up. Before, the hub marked its trunk as over at once in both
// cases, and the neighbour went on listing it until an operator removed it
// there.
func TestLeaveWhileNotServingOrCutOff(t *testing.T) {
	t.Run("not serving", func(t *testing.T) {
		c := testkit.NewCluster(t, "line", 2)
		a, b := c.Hubs[0], c.Hubs[1]
		b.Stop()
		if err := b.Offline(func(hb *hub.Hub) error { return hb.ClusterLeave() }); err != nil {
			t.Fatal(err)
		}
		if got := fmt.Sprint(a.Hub.TestMembers()); got != "[hub-a.localhost hub-b.localhost]" {
			t.Fatalf("hub a's cluster before hub b has run again: %s", got)
		}
		b.Start()
		testkit.Eventually(t, "hub a hears that hub b has left", func() bool {
			return fmt.Sprint(a.Hub.TestMembers()) == "[hub-a.localhost]" && fmt.Sprint(b.Hub.TestMembers()) == "[hub-b.localhost]"
		})
		// And the trunk is over on both sides: nothing dials any more.
		testkit.Eventually(t, "the trunk is over", func() bool {
			for _, pair := range [][2]*testkit.TestHub{{a, b}, {b, a}} {
				list, err := pair[0].Hub.Cluster()
				if err != nil {
					return false
				}
				for _, info := range list {
					if info.Name == pair[1].Name && (info.Trunk != "removed" || info.Connected) {
						return false
					}
				}
			}
			return true
		})
	})
	t.Run("cut off", func(t *testing.T) {
		c := testkit.NewCluster(t, "line", 2)
		a, b := c.Hubs[0], c.Hubs[1]
		c.Cut(0, 1)
		if err := b.Hub.ClusterLeave(); err != nil {
			t.Fatal(err)
		}
		b.Hub.Reconcile()
		testkit.Eventually(t, "hub b stands alone", func() bool { return fmt.Sprint(b.Hub.TestMembers()) == "[hub-b.localhost]" })
		if got := fmt.Sprint(a.Hub.TestMembers()); got != "[hub-a.localhost hub-b.localhost]" {
			t.Fatalf("hub a's cluster while the trunk is down: %s", got)
		}
		a.Hub.TestCutTrunk(b.Name, false)
		b.Hub.TestCutTrunk(a.Name, false)
		testkit.Eventually(t, "hub a hears that hub b has left", func() bool { return fmt.Sprint(a.Hub.TestMembers()) == "[hub-a.localhost]" })
	})
}
