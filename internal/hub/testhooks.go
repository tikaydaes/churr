package hub

import (
	"database/sql"
	"time"

	"github.com/tikaydaes/churr/internal/link/wire"
)

// What tests do to a hub's trunks. None of it is reachable from a command.

// TestCutTrunk holds the trunk to a neighbour down (cut true) or lets it
// come back (cut false): the connection is closed, this hub does not dial
// the neighbour and refuses its dial. A test cuts both ends.
func (h *Hub) TestCutTrunk(name string, cut bool) {
	h.mu.Lock()
	h.testCut[name] = cut
	tc := h.trunks[name]
	h.mu.Unlock()
	if cut && tc != nil {
		tc.conn.Close()
	}
}

// TestTrunkUp reports whether the trunk to a neighbour is connected and the
// two have exchanged what they hold.
func (h *Hub) TestTrunkUp(name string) bool {
	tc := h.trunkOf(name)
	if tc == nil {
		return false
	}
	tc.mu.Lock()
	defer tc.mu.Unlock()
	return tc.up && tc.synced
}

// TestTrunkDelay makes everything that arrives over a trunk arrive this much
// later: a slow path, in one direction. It is latency and nothing else:
// each frame is handled d after it arrived, in the order they came, and the
// trunk carries as many in that time as are sent on it.
func (h *Hub) TestTrunkDelay(name string, d time.Duration) {
	h.mu.Lock()
	h.testDelay[name] = d
	h.mu.Unlock()
	h.testDelayed.Store(true)
}

// TestTrunkTap shows fn every frame this hub sends over a trunk (out true)
// or receives from one. Nil stops it.
func (h *Hub) TestTrunkTap(fn func(trunk string, out bool, f wire.Frame)) {
	if fn == nil {
		h.testTrunkTap.Store(nil)
		return
	}
	h.testTrunkTap.Store(&fn)
}

// TestSkipStampChecks makes this hub take envelopes whatever their time: a
// hub that does not do its duty, for tests of the hubs that check after it.
func (h *Hub) TestSkipStampChecks(skip bool) { h.testNoStamp.Store(skip) }

// TestNoWindow lifts the bound on unanswered batches, to show what the bound
// is for.
func (h *Hub) TestNoWindow(off bool) { h.testNoWindow.Store(off) }

// TestSendCluster sends every connected neighbour and server a roster of the
// test's making: what a hub that forges an entry looks like.
func (h *Hub) TestSendCluster(entries []wire.ClusterEntry) {
	f := wire.Frame{Frame: wire.FCluster, Hub: h.id.Name, Cluster: entries}
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, t := range h.trunks {
		t.conn.Send(f)
	}
	for _, c := range h.conns {
		c.conn.Send(f)
	}
}

// TestSendToServer sends one server a frame of the test's making.
func (h *Hub) TestSendToServer(server string, f wire.Frame) {
	if c := h.connOf(server); c != nil {
		c.conn.Send(f)
	}
}

// TestClusterEntries returns the roster as this hub holds it, and
// TestMembers the names of its live members in order.
func (h *Hub) TestClusterEntries() []wire.ClusterEntry {
	return append([]wire.ClusterEntry{}, h.topology().entries...)
}

func (h *Hub) TestMembers() []string { return h.topology().cluster.Names() }

// TestSign signs with the hub's key, for a test that plays the hub.
func (h *Hub) TestSign(msg []byte) []byte { return h.keys.Sign(msg) }

// TestKeys returns the hub's keys as a signer.
func (h *Hub) TestKeys() wire.Signer { return h.keys }

// TestHubClock returns this hub's clock minus another hub's and how sure it
// is, in milliseconds.
func (h *Hub) TestHubClock(hub string) (offset, uncertainty int64, ok bool) {
	c, ok := h.hubClockOf(hub)
	return c.offset, c.uncertaintyAt(int64(h.now())), ok
}

// TestStored reports how many envelopes of a stream this hub's log holds.
func (h *Hub) TestStored(streamID string) int {
	var n int
	h.db.QueryRow(`SELECT count(*) FROM log WHERE stream_id = ?`, streamID).Scan(&n)
	return n
}

// SetRelayWait shortens how long a sender waits for room before it gives a
// trunk up, and returns a function that puts it back. It is for tests.
func SetRelayWait(d time.Duration) func() {
	old := relayWait.Swap(int64(d))
	return func() { relayWait.Store(old) }
}

// TestTrunkDrop makes the hub lose, without a word, the frames arriving over
// a trunk that fn claims. Nil puts it back. It is for tests.
func (h *Hub) TestTrunkDrop(fn func(trunk string, f wire.Frame) bool) {
	if fn == nil {
		h.testTrunkDrop.Store(nil)
		return
	}
	h.testTrunkDrop.Store(&fn)
}

// TestSendOverTrunk sends a frame over the trunk to a neighbour, as if this
// hub had made it. It is for tests.
func (h *Hub) TestSendOverTrunk(name string, f wire.Frame) {
	if tc := h.trunkOf(name); tc != nil {
		tc.send(f)
	}
}

// TestForgetStream deletes everything the hub holds of one stream, as a
// database lost, or put back from an older copy, would have. It is for
// tests.
func (h *Hub) TestForgetStream(id string) {
	m := h.lock(id)
	defer m.Unlock()
	h.tx(func(tx *sql.Tx) error {
		for _, q := range []string{`DELETE FROM stream_servers WHERE stream_id = ?`, `DELETE FROM channels WHERE stream_id = ?`,
			`DELETE FROM origins WHERE stream_id = ?`, `DELETE FROM log WHERE stream_id = ?`, `DELETE FROM stream_keys WHERE stream_id = ?`,
			`DELETE FROM streams WHERE id = ?`} {
			if _, err := tx.Exec(q, id); err != nil {
				return err
			}
		}
		return nil
	})
	h.forgetRoute(id)
}

// TestRelayHold makes this hub take this long over every batch that arrives
// by trunk: a hub that cannot keep up.
func (h *Hub) TestRelayHold(d time.Duration) { h.testHold.Store(int64(d)) }

// TestEphemeralRoom sets how many bytes may be waiting for a server before
// ephemeral frames for it are dropped; 0 puts the documented figure back.
func (h *Hub) TestEphemeralRoom(bytes int) { h.eph.room.Store(int64(bytes)) }

// TestQueued is how many bytes are waiting to be written to a server, or -1
// if it is not connected.
func (h *Hub) TestQueued(server string) int {
	if c := h.connOf(server); c != nil {
		return c.conn.Queued()
	}
	return -1
}
