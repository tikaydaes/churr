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
// starts with none, watches the one sign the kernel gives an unprivileged
// program that packets are being lost on the way out, the share of TCP
// segments it had to send again (tcpstat_linux.go), and sets a pace when
// that share is high while this program is writing a great deal. It lowers
// the pace while the loss goes on and the pace is what is being written
// at, raises it slowly when the loss has stopped, and lets go of it
// altogether when the server no longer writes anywhere near it.
//
// Segments are also sent again because of the networks clients are on,
// which no pace cures. Three things keep that from holding a server back:
// nothing is paced below paceLossSegments segments a second, where a
// transmit queue cannot be what overflows; the pace is lowered only while
// the server is writing at it, so it settles just under what the server
// writes and holds little; and it never goes below paceFloor.
//
// Where the kernel's count cannot be read (not Linux) there is no pace
// unless one is set, and nothing fails.

const (
	// paceLossShare is the share of segments sent again from which packets
	// are taken to be lost on this machine's way out, and paceCleanShare
	// the share under which they are taken not to be.
	paceLossShare  = 0.01
	paceCleanShare = 0.002
	// paceLossSegments is how many segments a second the machine must be
	// sending, and paceFloor how many writes a second this program must be
	// making, before any of it is of interest; paceFloor is also the
	// lowest pace ever set.
	paceLossSegments = 20000
	paceFloor        = 20000
	// A pace is set at paceFirst of what was being written when loss was
	// first seen, lowered to paceDown of itself while loss goes on, and
	// raised by paceUp a second when there is none, up to paceBelow of the
	// pace loss was last seen at.
	paceFirst = 0.7
	paceDown  = 0.8
	paceUp    = 1.02
	paceBelow = 0.9
	// pacePatience is how many seconds after a change loss is not acted
	// on again: segments lost before it are still being sent again.
	pacePatience = 5
	// paceForget is how many seconds without loss it takes to forget
	// where loss was last seen, and to try above it once more.
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
	clean   int     // seconds in a row without loss
	unused  int     // seconds in a row of writing under half the pace
	engaged bool
}

// step takes one second's counts: the writes this program made, the TCP
// segments the machine sent and how many of those were sent again. It
// returns the pace to write at from now on, 0 for none, and a word for the
// log if it changed for a reason worth one.
func (c *paceControl) step(writes, segments, again float64) (rate float64, said string) {
	c.since++
	loss := segments >= paceLossSegments && again >= paceLossShare*segments
	clean := segments < paceLossSegments || again < paceCleanShare*segments
	switch {
	case loss && c.rate == 0:
		c.clean = 0
		if writes < paceFloor {
			break // not this program's doing
		}
		c.lostAt = writes
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
		if c.since >= pacePatience && writes >= 0.8*c.rate {
			up := c.rate * paceUp
			if c.lostAt > 0 {
				up = min(up, paceBelow*c.lostAt)
			}
			c.rate = max(c.rate, up)
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
// It ends at once where the kernel's counts cannot be read.
func runPace() {
	lastOut, lastAgain, ok := tcpSegments()
	if !ok {
		return
	}
	var c paceControl
	lastWrites := pacing.writes.Load()
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for range t.C {
		out, again, ok := tcpSegments()
		writes := pacing.writes.Load()
		if !ok || pacing.pinned.Load() {
			lastOut, lastAgain, lastWrites = out, again, writes
			continue
		}
		rate, said := c.step(float64(writes-lastWrites), float64(out-lastOut), float64(again-lastAgain))
		if said != "" {
			if log := pacing.log.Load(); log != nil {
				share := 0.0
				if out > lastOut {
					share = float64((again-lastAgain)*1000/(out-lastOut)) / 10
				}
				log.Info("live: the pace of writes to connections was "+said+": the machine was sending TCP segments again, so packets were being lost on its way out (a full transmit queue, usually). Frames that wait for the pace leave together, in order",
					"writes_per_second_allowed", int64(rate), "writes_per_second", writes-lastWrites,
					"segments_per_second", out-lastOut, "sent_again_percent", share)
			}
		}
		writePace.rate.Store(int64(rate))
		lastOut, lastAgain, lastWrites = out, again, writes
	}
}
