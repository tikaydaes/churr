package live

import (
	"log/slog"
	"sync/atomic"
	"time"
)

// Finding the pace.
//
// How many packets a second a machine can send is not something an
// operator knows, and it differs by an order of magnitude between a virtual
// network card with one queue and a real one with sixteen. So the pace of
// writes (flush.go) is not a setting unless someone makes it one: the hub
// starts with none and watches this machine's own transmit queues
// (qdisc_linux.go): the packets they drop because they are full, and the
// packets standing in them. When they drop, or stay full, while this
// program is writing a great deal, it sets a pace under what it was
// writing. It lowers the pace while that goes on and the pace is what is
// being written at, raises it slowly when it has stopped, and lets go of
// it altogether when the server no longer writes anywhere near it.
//
// The share of TCP segments sent again is not what is watched, though it
// is the first thing one sees. Segments are sent again when a packet is
// lost anywhere: in this machine's queue, but as well on the path and at
// the far end, on a phone in a tunnel or a load generator whose own
// network card is full. A pace cures only the first. Set for the others
// it slowed a server that was doing nothing wrong: 10,000 connections that
// had been served in 34 ms at the median took 182 ms. So loss that this
// machine's queues do not show is warned of (wire.go) and nothing is
// slowed for it.
//
// Where the kernel's queues cannot be read (not Linux) there is no pace
// unless one is set, and nothing fails.

const (
	// paceDrops is how many packets a second the transmit queues must
	// drop, and paceDropShare the share of the writes that must be, for
	// the machine to be taken as sending more than it can.
	paceDrops     = 100
	paceDropShare = 0.005
	// paceWaiting is how many packets standing in the queues, second
	// after second for paceStanding seconds, say the same of a queue too
	// deep to drop: a burst to every connection passes through in a
	// fraction of a second, and only more than the card can carry stays.
	paceWaiting  = 4096
	paceStanding = 3
	// Under paceQuietDrops dropped and paceQuietWaiting waiting, the
	// queues are at ease.
	paceQuietDrops   = 10
	paceQuietWaiting = 1024
	// paceFloor is how many writes a second this program must be making
	// before any of it is of interest, and the lowest pace ever set.
	paceFloor = 20000
	// A pace is set at paceFirst of what was being written when the
	// queues first overflowed, lowered to paceDown of itself while they
	// still do, and raised by paceUp a second when they do not, up to
	// paceBelow of the pace they last overflowed at.
	// Until they have overflowed at a pace, where that is is not known,
	// and the pace seeks it by paceSeek a second.
	paceFirst = 0.7
	paceDown  = 0.8
	paceUp    = 1.02
	paceSeek  = 1.25
	paceBelow = 0.9
	// paceSeekAfter is how many seconds after a pace is first set the
	// seeking starts.
	paceSeekAfter = 2
	// pacePatience is how many seconds after a change the queues are not
	// acted on again: they are still emptying.
	pacePatience = 5
	// paceForget is how many seconds at ease it takes to forget where
	// the queues last overflowed, and to try above it once more.
	paceForget = 600
	// paceIdle is how many seconds of writing at under half the pace it
	// takes to let go of it.
	paceIdle = 120
)

// paceControl decides the pace from what one second showed. It is not
// safe for use by several goroutines; one runs it.
type paceControl struct {
	rate    float64 // the pace set, or 0 for none
	lostAt  float64 // the pace loss was last seen at, or 0
	since   int     // seconds since the pace last changed
	clean   int     // seconds in a row with the queues at ease
	full    int     // seconds in a row with the queues standing full
	unused  int     // seconds in a row of writing under half the pace
	engaged bool
}

// step takes one second's counts: the writes this program made, the
// packets this machine's transmit queues dropped in it, and the packets
// waiting in them at its end. It returns the pace to write at from now on,
// 0 for none, and a word for the log if it changed for a reason worth one.
// Nothing else is asked: segments sent again that the queues do not
// account for are somebody else's loss.
func (c *paceControl) step(writes, dropped, waiting float64) (rate float64, said string) {
	c.since++
	if waiting >= paceWaiting {
		c.full++
	} else {
		c.full = 0
	}
	loss := dropped >= max(paceDrops, paceDropShare*writes) || c.full >= paceStanding
	clean := dropped < paceQuietDrops && waiting < paceQuietWaiting
	switch {
	case loss && c.rate == 0:
		c.clean = 0
		if writes < paceFloor {
			break // not this program's doing
		}
		// Where the queues overflow is not known yet: what was being
		// written in this second may be the first moment of a burst
		// or its height. The pace starts low and seeks upward.
		c.lostAt = 0
		c.rate = max(paceFloor, paceFirst*writes)
		c.since, c.unused = 0, 0
		said = "set"
	case loss:
		c.clean = 0
		// Only while the pace is what holds the writes, and not for
		// loss that an earlier pace caused.
		if c.since >= pacePatience && writes >= 0.8*c.rate && c.rate > paceFloor {
			c.lostAt = c.rate
			c.rate = max(paceFloor, paceDown*c.rate)
			c.since = 0
			said = "lowered"
		}
	case clean && c.rate > 0:
		c.clean++
		if c.clean >= paceForget {
			c.lostAt = 0
		}
		if writes < 0.5*c.rate {
			c.unused++
		} else {
			c.unused = 0
		}
		if c.unused >= paceIdle {
			c.rate, c.lostAt, c.unused = 0, 0, 0
			said = "lifted"
			break
		}
		// Raised only while it holds something back, and not past
		// where it went wrong before.
		switch {
		case writes < 0.8*c.rate:
		case c.lostAt == 0 && c.since >= paceSeekAfter:
			c.rate *= paceSeek
		case c.lostAt > 0 && c.since >= pacePatience:
			c.rate = max(c.rate, min(c.rate*paceUp, paceBelow*c.lostAt))
		}
	}
	return c.rate, said
}

// What the pace is run from: the writes the flushers made, the log of the
// hub made last, and what an operator pinned.
var pacing struct {
	writes atomic.Int64
	log    atomic.Pointer[slog.Logger]
	// pinned is true when the pace was set by hand (Options): it is
	// then left alone.
	pinned atomic.Bool
}

// runPace adjusts the pace once a second for as long as the process runs.
// It ends at once where the kernel's queues cannot be read.
func runPace() {
	lastDropped, _, ok := sendQueues()
	if !ok {
		return
	}
	var c paceControl
	lastWrites := pacing.writes.Load()
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for range t.C {
		dropped, waiting, ok := sendQueues()
		writes := pacing.writes.Load()
		if !ok || pacing.pinned.Load() {
			if ok {
				lastDropped = dropped
			}
			lastWrites = writes
			continue
		}
		rate, said := c.step(float64(writes-lastWrites), float64(dropped-lastDropped), float64(waiting))
		if said != "" {
			if log := pacing.log.Load(); log != nil {
				log.Info("live: the pace of writes to connections was "+said+": this machine's transmit queues say whether it is sending more packets than its network card carries (`tc -s qdisc`). Frames that wait for the pace leave together, in order",
					"writes_per_second_allowed", int64(rate), "writes_per_second", writes-lastWrites,
					"packets_dropped_by_the_queues", dropped-lastDropped, "packets_waiting_in_the_queues", waiting)
			}
		}
		writePace.rate.Store(int64(rate))
		lastDropped, lastWrites = dropped, writes
	}
}
