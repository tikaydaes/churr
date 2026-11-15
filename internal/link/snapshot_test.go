package link_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/testkit"
)

// Section 17, ruling R62: after a connection comes back a server sends a
// presence snapshot on each stream, but not all in the same moment: each
// goes after its own random delay, so a hub that restarts is not sent every
// server's snapshots at once. They do all arrive, and say what is true when
// they are sent.
func TestSnapshotsAreSpreadAfterAReconnection(t *testing.T) {
	const channels = 6
	const spread = 1500 * time.Millisecond
	l := testkit.NewLinked(t, 2, false)
	a, b := l.Servers[0], l.Servers[1]
	ann := a.Member("ann")
	var streams []model.StreamID
	for i := 0; i < channels; i++ {
		ch := a.Owner.Channel(fmt.Sprintf("room-%d", i), false, ann)
		stream, _ := l.Share(0, ch, fmt.Sprintf("room-%d", i), nil, 1)
		streams = append(streams, model.StreamID(stream))
	}
	// ann is connected, so every snapshot has someone to name.
	ann.Events(nil)
	testkit.Eventually(t, "b hears that ann is online", func() bool {
		p, ok := b.Server.Link.PresenceOf(ann.ID)
		return ok && p.Presence == "online"
	})
	// It hears so once on every stream, and has heard the first by now. The
	// rest must be in before the count below is taken: one that came after
	// it was taken for that stream's snapshot, and the test went on to
	// count what a had sent while a was still sending.
	testkit.Eventually(t, "b has heard it on every stream", func() bool {
		heard := b.Server.Link.StatsNow().Ephemeral
		for _, id := range streams {
			if heard[id] == 0 {
				return false
			}
		}
		return true
	})

	a.Server.Link.TestSnapshotSpread(spread)
	b.Server.Link.TestSnapshotSpread(-1) // b's own snapshots are not what is watched
	before := b.Server.Link.StatsNow().Ephemeral
	sentBefore := a.Server.Link.StatsNow().Snapshots
	peerA, peerB := model.PeerID(l.Peers[0]), model.PeerID(l.Peers[1])
	a.Server.Link.TestDrop(peerA)
	testkit.Eventually(t, "the connection is back", func() bool {
		return a.Server.Link.TestConnected(peerA) && b.Server.Link.TestConnected(peerB)
	})
	back := time.Now()

	// When each stream's snapshot reached b, measured from the reconnection.
	arrived := map[model.StreamID]time.Duration{}
	for deadline := back.Add(spread + 5*time.Second); len(arrived) < channels && time.Now().Before(deadline); {
		now := b.Server.Link.StatsNow().Ephemeral
		for _, id := range streams {
			if _, seen := arrived[id]; !seen && now[id] > before[id] {
				arrived[id] = time.Since(back)
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	if len(arrived) != channels {
		t.Fatalf("%d of %d snapshots arrived after the reconnection", len(arrived), channels)
	}
	// a counts a snapshot after it has handed it to the connection, which
	// may be a moment after b has it.
	testkit.Eventually(t, "a has counted every snapshot it sent", func() bool {
		return a.Server.Link.StatsNow().Snapshots-sentBefore >= channels
	})
	if got := a.Server.Link.StatsNow().Snapshots - sentBefore; got != channels {
		t.Fatalf("a sent %d snapshots for %d streams", got, channels)
	}
	first, last := spread*10, time.Duration(0)
	for _, d := range arrived {
		first, last = min(first, d), max(last, d)
	}
	t.Logf("%d snapshots arrived between %v and %v after the reconnection (spread %v)", channels, first.Round(time.Millisecond), last.Round(time.Millisecond), spread)
	// Six random delays over a second and a half that all fell within a
	// tenth of it would be a chance of a few in a hundred thousand; sent
	// at once, as before, they arrive within a few milliseconds of each
	// other.
	if last-first < spread/10 {
		t.Fatalf("the snapshots arrived within %v of each other: they were not spread", last-first)
	}
	if last > spread+2*time.Second {
		t.Fatalf("a snapshot took %v, with a spread of %v", last, spread)
	}
}
