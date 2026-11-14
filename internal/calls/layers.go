package calls

import (
	"sort"
	"time"
)

// The three sizes a camera is sent in (docs/CALLS.md section 7).
const (
	layerQ = "q"
	layerH = "h"
	layerF = "f"
)

// layerSpec is what the documents say of one layer.
type layerSpec struct {
	name   string
	height int
	kbps   int
}

// cameraLayers are the simulcast layers, smallest first.
var cameraLayers = []layerSpec{{layerQ, 180, 150}, {layerH, 360, 500}, {layerF, 720, 1200}}

// Bit rates of the single-layer tracks, in kbit/s.
const (
	audioKbps  = 32
	screenKbps = 2500
)

// layerIndex is a layer's place in cameraLayers, or -1.
func layerIndex(name string) int {
	for i, l := range cameraLayers {
		if l.name == name {
			return i
		}
	}
	return -1
}

// Forwarding states of a subscribed track (docs/CALLS.md section 10).
const (
	stateForwarding  = "forwarding"
	statePaused      = "paused"
	stateMuted       = "muted"
	stateUnavailable = "unavailable"
)

// Reasons of call.layer_changed.
const (
	reasonSize      = "size"
	reasonBandwidth = "bandwidth"
	reasonPublisher = "publisher"
)

// option is one layer a subscriber could be sent of a track.
type option struct {
	name   string
	height int
	kbps   int
	// available is false for a layer the publisher is not sending.
	available bool
}

// wanted is one video track in a subscriber's list, with what the layer
// picker needs to know about it.
type wanted struct {
	id string
	// height is the tile's height in physical pixels.
	height int
	// options are the track's layers, smallest first. A track sent in one
	// size has one.
	options []option
	screen  bool
	// speaker marks the current speaker's camera, lowered last.
	speaker bool
	// muted and unavailable settle the state before any layer is picked.
	muted       bool
	unavailable bool
}

// choice is what the picker decided for one track.
type choice struct {
	state string
	// layer is the name of the layer to forward, or "" when none is.
	layer string
	// level is the layer's index in the track's options, or -1.
	level  int
	reason string
}

// bySize is step 1: the smallest layer whose height is at least 80% of the
// tile's, or the tallest there is.
func bySize(options []option, tile int) int {
	for i, o := range options {
		if o.height*5 >= tile*4 {
			return i
		}
	}
	return len(options) - 1
}

// byWhatExists is step 2: the layer itself if the publisher sends it, else
// the nearest one below, else the nearest above. -1 if none is sent.
func byWhatExists(options []option, want int) int {
	for i := want; i >= 0; i-- {
		if options[i].available {
			return i
		}
	}
	for i := want + 1; i < len(options); i++ {
		if options[i].available {
			return i
		}
	}
	return -1
}

// raiseEvery is how long the picker waits between steps up once the
// estimate has recovered, so the picture does not flap.
const raiseEvery = 5 * time.Second

// picker chooses what one subscriber is forwarded. It remembers which
// tracks it lowered for bandwidth, because they come back up slowly.
type picker struct {
	// cap is, per track, the highest level bandwidth allows: -1 is
	// paused. A track without an entry is not held down.
	cap       map[string]int
	lastRaise time.Time
	// raiseEvery replaces the documented five seconds in tests.
	raiseEvery time.Duration
}

func newPicker() *picker { return &picker{cap: map[string]int{}, raiseEvery: raiseEvery} }

// pick runs the three steps for a subscriber's list. budgetKbps is what the
// subscriber's downlink can take for video: the estimate, less audio, less
// a tenth held back. Zero or less means there is no estimate yet, and
// nothing is held down.
func (p *picker) pick(list []wanted, budgetKbps int, now time.Time) map[string]choice {
	out := make(map[string]choice, len(list))
	type cand struct {
		w     wanted
		ideal int
		level int
	}
	var cands []*cand
	for _, w := range list {
		switch {
		case w.unavailable || len(w.options) == 0:
			out[w.id] = choice{state: stateUnavailable, level: -1, reason: reasonPublisher}
			continue
		case w.muted:
			out[w.id] = choice{state: stateMuted, level: -1, reason: reasonPublisher}
			continue
		}
		size := bySize(w.options, w.height)
		ideal := byWhatExists(w.options, size)
		if ideal < 0 {
			out[w.id] = choice{state: stateUnavailable, level: -1, reason: reasonPublisher}
			continue
		}
		c := &cand{w: w, ideal: ideal, level: ideal}
		if ideal != size {
			out[w.id] = choice{reason: reasonPublisher} // the reason is kept below
		}
		cands = append(cands, c)
	}
	// Forget what was held down for tracks no longer in the list.
	for id := range p.cap {
		keep := false
		for _, c := range cands {
			keep = keep || c.w.id == id
		}
		if !keep {
			delete(p.cap, id)
		}
	}
	// The order tracks are lowered in: the smallest tiles first, the
	// current speaker last. A shared screen is never lowered.
	order := make([]*cand, 0, len(cands))
	for _, c := range cands {
		if !c.w.screen {
			order = append(order, c)
		}
	}
	sort.SliceStable(order, func(i, j int) bool {
		a, b := order[i], order[j]
		if a.w.speaker != b.w.speaker {
			return !a.w.speaker
		}
		if a.w.height != b.w.height {
			return a.w.height < b.w.height
		}
		return a.w.id < b.w.id
	})
	// below is the next level down that the publisher sends, or -1.
	below := func(c *cand, level int) int {
		for i := level - 1; i >= 0; i-- {
			if c.w.options[i].available {
				return i
			}
		}
		return -1
	}
	above := func(c *cand, level int) int {
		for i := level + 1; i <= c.ideal; i++ {
			if c.w.options[i].available {
				return i
			}
		}
		return c.ideal
	}
	total := func() int {
		sum := 0
		for _, c := range cands {
			if c.level >= 0 {
				sum += c.w.options[c.level].kbps
			}
		}
		return sum
	}
	// Start from what was held down before.
	for _, c := range cands {
		if cp, held := p.cap[c.w.id]; held && cp < c.level {
			c.level = cp
			if cp >= 0 && !c.w.options[cp].available {
				c.level = below(c, cp+1)
			}
		}
	}
	if budgetKbps > 0 {
		// Step 3: one step at a time, in order, until it fits.
		for total() > budgetKbps {
			lowered := false
			for _, c := range order {
				if total() <= budgetKbps {
					break
				}
				if c.level < 0 {
					continue
				}
				c.level = below(c, c.level)
				p.cap[c.w.id] = c.level
				lowered = true
			}
			if !lowered {
				break
			}
		}
		// Back up: one step every five seconds, in the reverse order,
		// and only if the result still fits.
		if now.Sub(p.lastRaise) >= p.raiseEvery {
			for i := len(order) - 1; i >= 0; i-- {
				c := order[i]
				if c.level >= c.ideal {
					continue
				}
				next := above(c, c.level)
				add := c.w.options[next].kbps
				if c.level >= 0 {
					add -= c.w.options[c.level].kbps
				}
				if total()+add <= budgetKbps {
					c.level = next
					p.cap[c.w.id] = next
					p.lastRaise = now
				}
				break
			}
		}
	} else {
		for _, c := range cands {
			c.level = c.ideal
		}
	}
	for _, c := range cands {
		if c.level >= c.ideal {
			delete(p.cap, c.w.id)
		}
		reason := reasonSize
		if out[c.w.id].reason == reasonPublisher {
			reason = reasonPublisher
		}
		switch {
		case c.level < 0:
			out[c.w.id] = choice{state: statePaused, level: -1, reason: reasonBandwidth}
		case c.level < c.ideal:
			out[c.w.id] = choice{state: stateForwarding, layer: c.w.options[c.level].name, level: c.level, reason: reasonBandwidth}
		default:
			out[c.w.id] = choice{state: stateForwarding, layer: c.w.options[c.level].name, level: c.level, reason: reason}
		}
	}
	return out
}

// videoBudget is what is left of a downlink estimate for video: less the
// audio being forwarded, less a tenth held back. Zero means "no estimate".
func videoBudget(estimateBps int, audioTracks int) int {
	if estimateBps <= 0 {
		return 0
	}
	kbps := estimateBps/1000 - audioTracks*audioKbps
	kbps -= estimateBps / 10000
	if kbps < 1 {
		kbps = 1
	}
	return kbps
}
