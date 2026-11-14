package wire

import (
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tikaydaes/churr/internal/model"
)

// This file is how a time reference (a hub, or the owner server on a direct
// link) measures the clock of a server connected to it, in the connection's
// own ping and pong (docs/LINK-PROTOCOL.md section 5).
//
//	reference                               server
//	   │ ── ping  t0 ───────────────────────▶ │  arrives at t1 by the server's clock
//	   │ ◀──────────────── pong  t0, t1, t2 ── │  leaves at t2
//	   │  arrives at t3 by the reference's clock
//
// From the four times:
//
//	offset     = ((t1 − t0) + (t2 − t3)) / 2      the server's clock minus the reference's
//	round trip = (t3 − t0) − (t2 − t1)
//
// The offset is exact when the two directions take equally long. A constant
// difference between them cannot be told from a clock offset by any exchange
// of this kind: it moves the measured offset by half the difference. What is
// certain is that neither direction took less than no time, so the true
// offset lies within half the round trip of the measured one. That is the
// floor of the uncertainty reported here, and nothing in this file pretends
// to do better without being told the clocks are synchronised (Synced).

// The sizes and waits of the measurement.
const (
	// ClockWindow is how many recent samples are kept per connected server.
	ClockWindow = 16
	// ClockBurst pings, ClockBurstEvery apart, follow a new connection, so
	// an offset is known within a second or two.
	ClockBurst      = 6
	ClockBurstEvery = 150 * time.Millisecond
	// ClockSyncedBound is how far apart, in milliseconds, two clocks may be
	// that an operator has declared synchronised. A measured offset beyond
	// it is put down to the path and reported, not applied.
	ClockSyncedBound = 10
	// clockRecent is how many of the newest samples must agree before a
	// change in one direction counts as having happened.
	clockRecent = 4
	// clockStepFloor is the smallest change in a one-way figure, in
	// milliseconds, that is treated as a change and not as noise.
	clockStepFloor = 5
	// clockDriftPPM is how fast two clocks are allowed to drift apart, in
	// parts per million. An old sample is trusted less by this much.
	clockDriftPPM = 50
)

// Clock is what a reference knows about the clock of one server and the path
// to it. It travels on a ping, is stored with the peer and is shown to the
// owner, so it lives in model.
type Clock = model.PeerClock

// ClockSample is the four times of one exchange, in milliseconds: T0 and T3
// by the reference's clock, T1 and T2 by the server's.
type ClockSample struct{ T0, T1, T2, T3 int64 }

func (s ClockSample) roundTrip() int64 { return (s.T3 - s.T0) - (s.T2 - s.T1) }
func (s ClockSample) forward() int64   { return s.T1 - s.T0 }
func (s ClockSample) back() int64      { return s.T3 - s.T2 }

// offset is ((t1 − t0) + (t2 − t3)) / 2, rounded to the nearest millisecond.
func (s ClockSample) offset() int64 { return half((s.T1 - s.T0) + (s.T2 - s.T3)) }

// half divides by two, rounding halves away from zero, so that an offset and
// its negative come out as each other's negatives.
func half(v int64) int64 {
	if v < 0 {
		return -((-v + 1) / 2)
	}
	return (v + 1) / 2
}

func abs64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

// ClockTrack turns the samples of one connection into the figures. It is the
// reference's; the server it measures only answers pings.
type ClockTrack struct {
	mu     sync.Mutex
	synced bool
	win    []ClockSample
	// baseF and baseB are the lowest one-way figures established so far in
	// each direction. A direction that moves away from its base while the
	// other stays is the path changing; both moving by the same amount in
	// opposite senses is a clock being set.
	baseF, baseB int64
	based        bool
	// tolF and tolB are how far a direction's figure may stray from its
	// base before that counts as a change and not as jitter.
	tolF, tolB int64
	// asym is the forward-minus-back delay put down to the path from
	// changes seen in one direction.
	asym int64
	// seed is what was known before this connection.
	seed *Clock
	// unsettled: the newest sample is off its base and the next few will
	// say whether something changed.
	unsettled bool
	clock     Clock
	have      bool
}

// NewClockTrack starts a window. synced is the operator's declaration that
// both clocks are synchronised. prior, if not nil, is what was measured for
// this server before the connection began: an asymmetry estimate is carried
// over when the first measurements agree with it.
func NewClockTrack(synced bool, prior *Clock) *ClockTrack {
	return &ClockTrack{synced: synced, seed: prior, tolF: clockStepFloor, tolB: clockStepFloor}
}

// Clock returns the figures, and whether there are any yet.
func (t *ClockTrack) Clock() (Clock, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.clock, t.have
}

// Unsettled reports whether the newest sample looked unlike the ones before
// it. The pinger then asks again at once instead of in half a minute.
func (t *ClockTrack) Unsettled() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.unsettled
}

// Add takes one sample and returns the new figures. A sample that cannot be
// true (an answer that left before the question arrived, a negative round
// trip) is dropped, and ok is false.
func (t *ClockTrack) Add(s ClockSample) (c Clock, ok bool) {
	if s.T3 < s.T0 || s.T2 < s.T1 || s.roundTrip() < 0 {
		return Clock{}, false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.win = append(t.win, s)
	if len(t.win) > ClockWindow {
		t.win = t.win[len(t.win)-ClockWindow:]
	}
	if !t.synced {
		t.watchDirections()
	}
	t.clock, t.have = t.figures(), true
	return t.clock, true
}

// minOf returns the lowest value of f over samples.
func minOf(samples []ClockSample, f func(ClockSample) int64) int64 {
	m := f(samples[0])
	for _, s := range samples[1:] {
		if v := f(s); v < m {
			m = v
		}
	}
	return m
}

// maxOf returns the highest value of f over samples.
func maxOf(samples []ClockSample, f func(ClockSample) int64) int64 {
	m := f(samples[0])
	for _, s := range samples[1:] {
		if v := f(s); v > m {
			m = v
		}
	}
	return m
}

// excess returns the mean by which f exceeds its lowest over samples.
func excess(samples []ClockSample, f func(ClockSample) int64) int64 {
	m, sum := minOf(samples, f), int64(0)
	for _, s := range samples {
		sum += f(s) - m
	}
	return (sum + int64(len(samples))/2) / int64(len(samples))
}

// watchDirections looks at each direction by itself. The one-way figures
// t1 − t0 and t3 − t2 each hold the clock offset as well as the delay, so
// neither means anything alone; but the offset is in both with opposite
// signs, and a path's delay never goes below nothing. So:
//
//   - one direction's lowest figure moves and the other's stays: that
//     direction's path changed. The clocks did not, and the offset must not
//     follow. The change is added to the asymmetry estimate.
//   - both move by the same amount in opposite senses: a clock was set. The
//     samples from before are of a clock that no longer exists.
//
// A mixture is split the same way: what the two have in common with opposite
// signs is the clock, the rest is the path.
func (t *ClockTrack) watchDirections() {
	if len(t.win) < clockRecent {
		return
	}
	recent := t.win[len(t.win)-clockRecent:]
	rF, rB := minOf(recent, ClockSample.forward), minOf(recent, ClockSample.back)
	if !t.based {
		t.baseF, t.baseB, t.based = minOf(t.win, ClockSample.forward), minOf(t.win, ClockSample.back), true
		if p := t.seed; p != nil && p.Asymmetry != 0 && !p.Synced {
			// The same lopsided path as before the connection dropped, if
			// what is measured now still agrees with what was applied then.
			// The offset applied then is the best guess at the truth now, so
			// the estimate is kept if taking it out lands nearer to it than
			// leaving it in.
			raw := t.best().offset()
			if abs64(raw-half(p.Asymmetry)-p.Offset) < abs64(raw-p.Offset) {
				t.asym = p.Asymmetry
			}
		}
		return
	}
	older := t.win[:len(t.win)-clockRecent]
	// How far a direction must move to count: three times its own jitter,
	// as the samples from before the newest few show it. The newest few are
	// left out so that a change cannot hide by looking like jitter.
	tolF, tolB := int64(clockStepFloor), int64(clockStepFloor)
	if len(older) > 0 {
		if j := 3 * excess(older, ClockSample.forward); j > tolF {
			tolF = j
		}
		if j := 3 * excess(older, ClockSample.back); j > tolB {
			tolB = j
		}
	}
	t.tolF, t.tolB = tolF, tolB
	newest := t.win[len(t.win)-1]
	t.unsettled = newest.forward()-t.baseF > tolF || newest.forward()-t.baseF < -tolF ||
		newest.back()-t.baseB > tolB || newest.back()-t.baseB < -tolB
	// A direction has moved when every one of the newest few samples says
	// so: all above the base, or all below it. One sample is not enough
	// either way, or a clock set forward would first look like the way back
	// having become faster.
	dF, dB := rF-t.baseF, rB-t.baseB
	movedF := dF > tolF || maxOf(recent, ClockSample.forward)-t.baseF < -tolF
	movedB := dB > tolB || maxOf(recent, ClockSample.back)-t.baseB < -tolB
	// What the two directions share with opposite signs is a clock that was
	// set; the rest is the path.
	var step int64
	if movedF && movedB && (dF > 0) != (dB > 0) {
		step = abs64(dF)
		if abs64(dB) < step {
			step = abs64(dB)
		}
		if dF < 0 {
			step = -step
		}
	}
	pathF, pathB := int64(0), int64(0)
	if movedF {
		pathF = dF - step
	}
	if movedB {
		pathB = dB + step
	}
	// A change of path must stand well clear of the direction's jitter, and
	// of enough samples from before, to be believed: a path that jitters by
	// a lot in one direction must not look like one that keeps changing. A
	// clock that was set moves both directions at once in opposite senses,
	// which jitter does not do, and needs neither.
	pathMoved := (abs64(pathF) > 2*tolF || abs64(pathB) > 2*tolB) && len(older) >= 2*clockRecent
	if !pathMoved && step == 0 {
		movedF, movedB = false, false
	}
	if !movedF && !movedB {
		// Nothing changed. The base follows the lowest seen, a little at a
		// time: a large drop waits to be confirmed.
		if rF < t.baseF && t.baseF-rF <= tolF {
			t.baseF = rF
		}
		if rB < t.baseB && t.baseB-rB <= tolB {
			t.baseB = rB
		}
		return
	}
	if pathMoved {
		t.asym += pathF - pathB
		// One direction cannot take longer than the other by more than the
		// whole round trip.
		if rt := minOf(recent, ClockSample.roundTrip); t.asym > rt {
			t.asym = rt
		} else if t.asym < -rt {
			t.asym = -rt
		}
	}
	// Either way the older samples describe a clock or a path that is gone.
	t.win = append([]ClockSample(nil), recent...)
	t.baseF, t.baseB = rF, rB
	t.unsettled = false
}

// best is the sample to take the offset from: the one with the lowest round
// trip, an old sample counting for less by the drift two clocks are allowed.
func (t *ClockTrack) best() ClockSample {
	newest := t.win[len(t.win)-1].T3
	score := func(s ClockSample) int64 {
		// In microseconds: half the round trip, plus the drift since. The
		// age is taken either way round, for a reference whose own clock
		// was set back.
		return s.roundTrip()*500 + abs64(newest-s.T3)*clockDriftPPM/1000
	}
	// A sample whose one-way figure is well below the base belongs to a
	// change that the next few samples will confirm or not. Until they do
	// it is left out: the asymmetry estimate still describes the path as it
	// was, and the two must not be mixed.
	settled := func(s ClockSample) bool {
		return !t.based || t.synced || (s.forward() >= t.baseF-t.tolF && s.back() >= t.baseB-t.tolB)
	}
	var best *ClockSample
	for i := range t.win {
		if s := t.win[i]; settled(s) && (best == nil || score(s) <= score(*best)) {
			best = &t.win[i]
		}
	}
	if best == nil {
		best = &t.win[0]
		for i := range t.win {
			if score(t.win[i]) <= score(*best) {
				best = &t.win[i]
			}
		}
	}
	return *best
}

func (t *ClockTrack) figures() Clock {
	best := t.best()
	newest := t.win[len(t.win)-1]
	raw, rt := best.offset(), best.roundTrip()
	lowest := minOf(t.win, ClockSample.roundTrip)
	c := Clock{Latency: lowest, Jitter: excess(t.win, ClockSample.roundTrip), Samples: len(t.win), MeasuredAt: model.Time(newest.T3),
		ForwardJitter: excess(t.win, ClockSample.forward), BackJitter: excess(t.win, ClockSample.back), Synced: t.synced}
	// Half the round trip of the sample used (rounded up), what the clocks
	// may have drifted since it, and one for the rounding of four times.
	bound := (rt+1)/2 + abs64(newest.T3-best.T3)*clockDriftPPM/1_000_000 + 1
	if t.synced {
		// The clocks are within the bound of each other by the operator's
		// word. Whatever more is measured is the path's doing.
		c.Offset = raw
		if c.Offset > ClockSyncedBound {
			c.Offset = ClockSyncedBound
		} else if c.Offset < -ClockSyncedBound {
			c.Offset = -ClockSyncedBound
		}
		c.Asymmetry = 2 * (raw - c.Offset)
		c.Uncertainty = bound + abs64(raw-c.Offset)
		if c.Uncertainty > 2*ClockSyncedBound {
			c.Uncertainty = 2 * ClockSyncedBound
		}
	} else {
		// The asymmetry seen is taken out, but never further than the round
		// trip allows: the true offset is within half of it of the raw one.
		c.Asymmetry = t.asym
		if c.Asymmetry > rt {
			c.Asymmetry = rt
		} else if c.Asymmetry < -rt {
			c.Asymmetry = -rt
		}
		c.Offset = raw - half(c.Asymmetry)
		c.Uncertainty = bound + abs64(raw-c.Offset)
	}
	c.Forward = minOf(t.win, ClockSample.forward) - c.Offset
	c.Back = minOf(t.win, ClockSample.back) + c.Offset
	return c
}

// ---------------------------------------------------------------- the frames

// pingEvery is PingEvery unless a test has shortened it.
var pingEvery atomic.Int64

func init() { pingEvery.Store(int64(PingEvery)) }

// SetPingEvery changes how often a reference pings, and returns a function
// that puts it back. Tests of clocks that change shorten it.
func SetPingEvery(d time.Duration) func() {
	old := pingEvery.Swap(int64(d))
	return func() { pingEvery.Store(old) }
}

// Pinger is the reference's side of a connection's heartbeat. Run sends the
// pings; the connection's reader hands each pong to Pong.
type Pinger struct {
	Conn *Conn
	// Now is the reference's clock, in milliseconds.
	Now   func() int64
	Track *ClockTrack
	// OnClock is called with the figures after every sample, so they can
	// be stored with the peer.
	OnClock func(Clock)
	// Extra, if set, may add to a ping before it leaves: a hub adds the
	// server's clock against the other hubs of its cluster.
	Extra func(f *Frame)

	mu   sync.Mutex
	sent map[int64]int64 // frame id to t0
	more chan struct{}
	// told is signalled when the first sample is in: the next ping, which
	// tells the server its offset, leaves at once.
	told chan struct{}
}

// NewPinger makes the pinger of one connection.
func NewPinger(c *Conn, now func() int64, track *ClockTrack, onClock func(Clock)) *Pinger {
	return &Pinger{Conn: c, Now: now, Track: track, OnClock: onClock, sent: map[int64]int64{}, more: make(chan struct{}, 1),
		told: make(chan struct{}, 1)}
}

// ping sends one ping. It carries what the reference has worked out so far,
// which is how the server learns its offset.
func (p *Pinger) ping() error {
	f := Frame{Frame: FPing, ID: p.Conn.NextID()}
	if c, ok := p.Track.Clock(); ok {
		f.Clock = &c
		if p.Extra != nil {
			p.Extra(&f)
		}
	}
	p.mu.Lock()
	if len(p.sent) > 4*ClockWindow {
		// Pings nobody answered.
		for id := range p.sent {
			if id < f.ID-2*ClockWindow {
				delete(p.sent, id)
			}
		}
	}
	// The time is read last, as near to the write as it can be.
	t0 := p.Now()
	f.T0 = &t0
	p.sent[f.ID] = t0
	p.mu.Unlock()
	return p.Conn.sendNow(f)
}

// Run pings until the connection ends: a short burst at once (the second
// ping, which carries the first offset, as soon as the first is answered),
// then every PingEvery, and a few close together whenever a sample looks
// unlike the ones before it.
func (p *Pinger) Run() {
	left := ClockBurst
	var extra time.Time
	for {
		if err := p.ping(); err != nil {
			return
		}
		every := time.Duration(pingEvery.Load())
		wait := every
		if left > 1 {
			left--
			wait = ClockBurstEvery
		}
		timer := time.NewTimer(wait)
	waiting:
		for {
			select {
			case <-p.Conn.Done():
				timer.Stop()
				return
			case <-p.told:
				timer.Stop()
				break waiting
			case <-p.more:
				// At most one extra burst per ordinary ping, so a path that
				// is simply bad is not pinged without end.
				if left > 1 || time.Since(extra) < every {
					continue
				}
				timer.Stop()
				left, extra = clockRecent+1, time.Now()
				break waiting
			case <-timer.C:
				break waiting
			}
		}
	}
}

// Pong takes the answer to a ping. The arrival time is read at once.
func (p *Pinger) Pong(f Frame) {
	t3 := p.Now()
	p.mu.Lock()
	t0, asked := p.sent[f.Re]
	delete(p.sent, f.Re)
	p.mu.Unlock()
	if !asked || f.T0 == nil || f.T1 == nil || f.T2 == nil || *f.T0 != t0 {
		return // not an answer to anything this side asked
	}
	c, ok := p.Track.Add(ClockSample{T0: t0, T1: *f.T1, T2: *f.T2, T3: t3})
	if !ok {
		return
	}
	if c.Samples == 1 {
		// The first measurement of this connection. A server that has
		// never been told its offset can make nothing until it is, so it
		// is told now and not at the next tick.
		select {
		case p.told <- struct{}{}:
		default:
		}
	}
	if p.Track.Unsettled() {
		select {
		case p.more <- struct{}{}:
		default:
		}
	}
	if p.OnClock != nil {
		p.OnClock(c)
	}
}

// AnswerPing sends the pong for a ping. t1 is the server's clock when the
// ping arrived, read by the caller as early as it could; now is read again
// for t2 as the answer leaves.
func (c *Conn) AnswerPing(f Frame, t1 int64, now func() int64) error {
	if f.T0 == nil {
		return Errf(EBadFrame, "A ping carries t0.")
	}
	t2 := now()
	if t2 < t1 {
		t2 = t1 // the clock was set back between the two readings
	}
	return c.sendNow(Frame{Frame: FPong, Re: f.ID, T0: f.T0, T1: &t1, T2: &t2})
}

// Median returns the middle value of a list, for the totals a status page
// shows; 0 for an empty one.
func Median(values []int64) int64 {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]int64(nil), values...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	return sorted[len(sorted)/2]
}
