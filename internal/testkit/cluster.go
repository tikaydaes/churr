package testkit

import (
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/hub"
)

// Cluster is several hubs in the test process, joined by trunks.
type Cluster struct {
	t testing.TB
	// Hubs are named hub-a.localhost, hub-b.localhost and so on.
	Hubs []*TestHub
	// via[i] is the index of the hub that hub i joined through; -1 for the
	// first.
	via []int
}

// HubName is the name of the i-th hub of a cluster.
func HubName(i int) string { return "hub-" + string(rune('a'+i)) + ".localhost" }

// NewCluster starts n hubs and joins them. shape is "line" (each hub joins
// through the one before it: a - b - c) or "star" (every hub joins through
// the first: b - a - c).
func NewCluster(t testing.TB, shape string, n int) *Cluster {
	t.Helper()
	c := NewHubs(t, n)
	for i := 1; i < n; i++ {
		through := 0
		if shape == "line" {
			through = i - 1
		}
		c.Join(i, through)
	}
	return c
}

// NewHubs starts n hubs that are not yet joined: a test that wants their
// clocks or their trunks a certain way from the first measurement sets that
// up and then calls Join.
func NewHubs(t testing.TB, n int) *Cluster {
	t.Helper()
	// A trunk that drops in a test comes back at once when it is let.
	t.Cleanup(hub.SetTrunkRetry(50 * time.Millisecond))
	c := &Cluster{t: t}
	for i := 0; i < n; i++ {
		c.Hubs = append(c.Hubs, NewHubNamed(t, HubName(i)))
		c.via = append(c.via, -1)
	}
	return c
}

// Join makes hub i join the cluster of hub j, by a trunk between the two,
// and waits until every hub of the cluster lists it.
func (c *Cluster) Join(i, j int) {
	c.t.Helper()
	inv, err := c.Hubs[j].Hub.TrunkInvite("", 0)
	if err != nil {
		c.t.Fatal(err)
	}
	if err := c.Hubs[i].Hub.TrunkJoin(inv); err != nil {
		c.t.Fatal(err)
	}
	c.Hubs[i].Hub.Reconcile()
	c.Joined(i, j)
}

// Joined waits until hub i, which has been told to join the cluster of hub
// j in some other way than Join (the churr-hub program, run on its data),
// is in it, and every hub of the cluster lists it.
func (c *Cluster) Joined(i, j int) {
	c.t.Helper()
	c.via[i] = j
	want := len(c.Hubs[j].Hub.TestMembers()) + 1
	Eventually(c.t, fmt.Sprintf("%s is in the cluster", HubName(i)), func() bool {
		for k, h := range c.Hubs {
			if k != i && c.via[k] < 0 && k != 0 {
				continue // not joined yet
			}
			if len(h.Hub.TestMembers()) != want {
				return false
			}
		}
		return c.Up(i, j)
	})
}

// Up reports whether the trunk between hubs i and j is connected at both
// ends and each has said what it holds.
func (c *Cluster) Up(i, j int) bool {
	return c.Hubs[i].Hub.TestTrunkUp(HubName(j)) && c.Hubs[j].Hub.TestTrunkUp(HubName(i))
}

// Cut drops the trunk between hubs i and j and holds it down.
func (c *Cluster) Cut(i, j int) {
	c.t.Helper()
	c.Hubs[i].Hub.TestCutTrunk(HubName(j), true)
	c.Hubs[j].Hub.TestCutTrunk(HubName(i), true)
	Eventually(c.t, "the trunk is down", func() bool {
		return !c.Hubs[i].Hub.TestTrunkUp(HubName(j)) && !c.Hubs[j].Hub.TestTrunkUp(HubName(i))
	})
}

// Restore lets the trunk between hubs i and j come back, and waits for it.
func (c *Cluster) Restore(i, j int) {
	c.t.Helper()
	c.Hubs[i].Hub.TestCutTrunk(HubName(j), false)
	c.Hubs[j].Hub.TestCutTrunk(HubName(i), false)
	Eventually(c.t, "the trunk is back", func() bool { return c.Up(i, j) })
}

// Whole waits until every trunk of the cluster is up.
func (c *Cluster) Whole() {
	c.t.Helper()
	Eventually(c.t, "every trunk is up", func() bool {
		for i, j := range c.via {
			if j >= 0 && !c.Up(i, j) {
				return false
			}
		}
		return true
	})
}

// ClusterLinked is a cluster with servers on its hubs.
type ClusterLinked struct {
	*Linked
	Cluster *Cluster
	// On[i] is the index of the hub server i is on.
	On []int
}

// NewClusterLinked starts a cluster and servers on its hubs: on[i] is the
// hub server i joins. Every server ends up holding the roster that lists
// all the others, whichever hub they are on. skews, if given, are added to
// the servers' link clocks.
func NewClusterLinked(t testing.TB, shape string, hubs int, on []int, skews ...time.Duration) *ClusterLinked {
	t.Helper()
	return LinkCluster(t, NewCluster(t, shape, hubs), on, skews...)
}

// LinkCluster is NewClusterLinked for a cluster the test has already made,
// in whatever way it wanted to make it.
func LinkCluster(t testing.TB, c *Cluster, on []int, skews ...time.Duration) *ClusterLinked {
	t.Helper()
	l := &Linked{t: t, Peers: make([]string, len(on)), Hub: c.Hubs[0]}
	for i, at := range on {
		l.skews = append(l.skews, &atomic.Int64{})
		if i < len(skews) {
			l.Skew(i, skews[i])
		}
		ts := NewServer(t, Options{Hostname: ServerName(i), LinkSkew: l.skews[i]})
		l.Servers = append(l.Servers, ts)
		peer := ts.Owner.Must(201, "POST", "/api/v1/admin/peers", map[string]any{"invitation": c.Hubs[at].Invite("")})["peer"].(map[string]any)
		l.Peers[i] = peer["id"].(string)
	}
	for i, ts := range l.Servers {
		Eventually(t, "the roster of the whole cluster reaches "+ServerName(i), func() bool {
			peer := ts.Owner.Must(200, "GET", "/api/v1/admin/peers/"+l.Peers[i], nil)["peer"].(map[string]any)
			servers, _ := peer["servers"].([]any)
			return len(servers) == len(on)-1
		})
	}
	return &ClusterLinked{Linked: l, Cluster: c, On: append([]int{}, on...)}
}
