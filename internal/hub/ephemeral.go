package hub

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/tikaydaes/churr/internal/link/wire"
	"github.com/tikaydaes/churr/internal/ratelimit"
)

// Ephemeral frames under pressure (docs/LINK-PROTOCOL.md sections 17 and
// 23, ruling R62).
//
// An ephemeral frame is typing or presence. It is never stamped, logged or
// stored; nothing is ordered after it, and whatever it says is said again
// soon (typing repeats, presence is sent whole in the next change or
// snapshot). So it is the one thing a hub may throw away, and it does in two
// places, counting each:
//
//   - on arrival, when its servers together send more than
//     ephemeralPerSecond: the frame is dropped before the hub spends a
//     signature check and a database read on it, so that a burst of presence
//     from every server at once (all of them reconnecting) does not make the
//     messages on the same connections wait;
//   - on the way out, when the frames waiting to be written to a server
//     pass ephemeralRoom: the frame is dropped for that server instead of
//     being put behind them.
//
// The hub cannot read these frames, so it cannot merge them as a server
// does for its own clients; dropping is all it can do.
//
// Envelopes in deliver frames, control frames and everything else a server
// is owed never come this way and are never dropped.

// ephemeralRoom is how many bytes may be waiting to be written to a server
// before ephemeral frames for it are dropped: an eighth of what closes the
// connection altogether.
const ephemeralRoom = sendQueueBytes / 8

// ephemeralPerSecond is how many ephemeral frames a second a hub takes from
// all its servers together. A burst of as many again is let through first.
// Each server has its own allowance besides (1,200 a minute, section 23);
// this one is the hub looking after itself.
const ephemeralPerSecond = 2000

// ephemeralCounts is what the hub has done with ephemeral frames since it
// started. passed and dropped are counted once for each server a frame was
// meant for, shed once for each frame dropped on arrival.
type ephemeralCounts struct {
	passed, dropped, shed atomic.Int64
	once                  sync.Once
	budget                *ratelimit.Limiter
	// room, when a test has set it, replaces ephemeralRoom.
	room atomic.Int64
}

// EphemeralStats counts ephemeral frames since the hub started. Passed is
// how many were sent on to a server and Dropped how many were not because
// that server's connection was backed up, both counted once for each server
// a frame was meant for. Shed is how many frames were dropped as they
// arrived because the hub was being sent more than it takes.
type EphemeralStats struct {
	Passed, Dropped, Shed int64
}

// EphemeralStats returns the counts.
func (h *Hub) EphemeralStats() EphemeralStats {
	return EphemeralStats{Passed: h.eph.passed.Load(), Dropped: h.eph.dropped.Load(), Shed: h.eph.shed.Load()}
}

// takeEphemeral reports whether the hub takes one more ephemeral frame now.
// When it does not, the frame is counted as shed and the caller drops it
// without an answer: its sender is doing nothing wrong.
func (h *Hub) takeEphemeral() bool {
	h.eph.once.Do(func() {
		h.eph.budget = ratelimit.New(ratelimit.Per(ephemeralPerSecond, time.Second), ephemeralPerSecond)
	})
	if h.eph.budget.Take("").OK {
		return true
	}
	h.eph.shed.Add(1)
	// Nobody is told to slow down here: the hub as a whole is over what
	// it passes on, and drops (ruling R62).
	h.refused.Note("ephemeral frames, all servers together (dropped)", "", 0)
	return false
}

// sendEphemeral passes an ephemeral frame to the named servers that are
// connected, leaving out any whose connection is backed up.
func (h *Hub) sendEphemeral(names []string, f wire.Frame) {
	if len(names) == 0 {
		return
	}
	raw, err := wire.EncodeFrame(f)
	if err != nil {
		return
	}
	room := h.eph.room.Load()
	if room == 0 {
		room = ephemeralRoom
	}
	for _, n := range names {
		c := h.connOf(n)
		if c == nil {
			continue
		}
		if int64(c.conn.Queued()) > room {
			h.eph.dropped.Add(1)
			continue
		}
		c.conn.SendRaw(raw)
		h.eph.passed.Add(1)
	}
}
