package calls

import "time"

// downlink follows what one subscriber's connection can take.
//
// docs/CALLS.md section 20 asks whether Pion's send-side estimator is good
// enough for the layer picker, or whether loss reports must stand in for
// it. Tried here, they must:
//
//   - Its target bit rate is never more than one and a half times what is
//     being received. A server that sends only what subscribers ask for,
//     and no padding to probe with, sees the target sink to the size of
//     the audio and stay there, with every camera paused.
//   - Its delay signal ("overuse") was raised more than half the time with
//     twenty-five clients on the loopback address, where nothing is
//     congested: packets forwarded in bursts look to it like a queue.
//
// What it reports dependably is loss, taken from the subscriber's
// transport-wide feedback and smoothed. So the limit is worked out here
// from loss alone, by the rule the congestion control drafts use for it.
// With little loss there is no limit, and the picker goes by size. When
// more than a tenth of the packets are lost, the limit becomes a little
// less than what is being sent now, which makes the picker lower or pause
// tracks. While loss stays under a fiftieth after that the limit grows
// again, and once it has grown past anything a call sends it is dropped.
//
// What this cannot see is a path that queues for a long time before it
// drops anything. Whether that matters in practice needs a test on real
// networks, which has not been done.
type downlink struct {
	// limit is the most the connection is taken to carry, in bits a
	// second; 0 is no limit.
	limit   int
	lastCut time.Time
	lastAt  time.Time
	// lastBytes is the count of bytes sent at lastAt; rate is what was
	// being sent, in bits a second, over the stretch that ended then.
	lastBytes int64
	rate      int
}

const (
	// downlinkEvery is how often the limit is looked at again.
	downlinkEvery = 500 * time.Millisecond
	// downlinkCutEvery is the least time between two cuts: the effect of
	// one must be seen before the next.
	downlinkCutEvery = 1500 * time.Millisecond
	// downlinkKeep is the share of the current rate kept on a cut.
	downlinkKeep = 0.85
	// downlinkGrow is how fast the limit grows on a clear path, a second:
	// this share of itself, and downlinkStep on top so that a low limit
	// does not take a minute to mean anything again.
	downlinkGrow = 0.08
	downlinkStep = 100_000
	// downlinkFloor is the least a limit is ever cut to: the audio of a
	// full call and one small picture. Loss while little is being sent
	// says little about what the path can carry.
	downlinkFloor = 300_000
	// downlinkOpen is the limit above which there is none.
	downlinkOpen = 30_000_000
	// downlinkLoss is the share of packets lost that counts as
	// congestion; downlinkClear the share under which the path is clear.
	// Between the two the limit stays where it is.
	downlinkLoss  = 0.10
	downlinkClear = 0.02
)

// update takes the count of bytes sent so far and the share of packets the
// subscriber reports lost, and returns the limit in bits a second, 0 for
// none.
func (d *downlink) update(now time.Time, sentBytes int64, loss float64) int {
	congested, clear := loss > downlinkLoss, loss < downlinkClear
	if d.lastAt.IsZero() {
		d.lastAt, d.lastBytes = now, sentBytes
		return d.limit
	}
	dt := now.Sub(d.lastAt)
	if dt < downlinkEvery {
		return d.limit
	}
	d.rate = int(float64(sentBytes-d.lastBytes) * 8 / dt.Seconds())
	d.lastAt, d.lastBytes = now, sentBytes
	switch {
	case congested && now.Sub(d.lastCut) >= downlinkCutEvery:
		base := d.rate
		if d.limit != 0 && d.limit < base {
			base = d.limit
		}
		d.limit = int(downlinkKeep * float64(base))
		if d.limit < downlinkFloor {
			d.limit = downlinkFloor
		}
		d.lastCut = now
	case clear && d.limit != 0:
		d.limit += int((float64(d.limit)*downlinkGrow + downlinkStep) * dt.Seconds())
		if d.limit >= downlinkOpen {
			d.limit = 0
		}
	}
	return d.limit
}
