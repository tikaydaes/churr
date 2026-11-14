package wire

import (
	"context"
	"math/rand"
	"sync/atomic"
	"testing"
	"time"
)

// sim is a reference and a server with clocks and a path between them that a
// test sets. Time is in milliseconds. The reference's clock is the truth;
// the server's is the truth plus off(truth).
type sim struct {
	t     *testing.T
	track *ClockTrack
	now   int64                        // true time of the next ping
	off   func(now int64) int64        // the server's clock minus the reference's
	fwd   func(i int, now int64) int64 // delay from the reference to the server
	back  func(i int, now int64) int64 // and from the server to the reference
	i     int
	last  Clock
}

func newSim(t *testing.T, synced bool, prior *Clock) *sim {
	return &sim{t: t, track: NewClockTrack(synced, prior), now: 1_700_000_000_000,
		off: func(int64) int64 { return 0 }, fwd: func(int, int64) int64 { return 20 }, back: func(int, int64) int64 { return 20 }}
}

// exchange is one ping and pong: the four times as each side reads them.
func (s *sim) exchange() Clock {
	s.t.Helper()
	df, db := s.fwd(s.i, s.now), s.back(s.i, s.now)
	arrives := s.now + df // true time the ping reaches the server
	leaves := arrives + 1 // the server takes a millisecond to answer
	sample := ClockSample{T0: s.now, T1: arrives + s.off(arrives), T2: leaves + s.off(leaves), T3: leaves + db}
	c, ok := s.track.Add(sample)
	if !ok {
		s.t.Fatalf("sample %d was dropped: %+v", s.i, sample)
	}
	s.i++
	s.now += 30_000
	s.last = c
	return c
}

// run makes n exchanges and checks after each that the reported uncertainty
// covers the true error: the one promise the figures make.
func (s *sim) run(n int) Clock {
	s.t.Helper()
	for k := 0; k < n; k++ {
		at := s.now
		c := s.exchange()
		if err := abs64(c.Offset - s.off(at)); err > c.Uncertainty {
			s.t.Fatalf("exchange %d: offset %d, true %d: the error %d is beyond the uncertainty %d", s.i-1, c.Offset, s.off(at), err, c.Uncertainty)
		}
		if !c.Synced && c.Uncertainty < (c.Latency+1)/2 {
			s.t.Fatalf("exchange %d: uncertainty %d is below half the lowest round trip %d", s.i-1, c.Uncertainty, c.Latency)
		}
	}
	return s.last
}

func near(t *testing.T, what string, got, want, within int64) {
	t.Helper()
	if abs64(got-want) > within {
		t.Errorf("%s: got %d, want %d (within %d)", what, got, want, within)
	}
}

func TestClockWrongByMinutes(t *testing.T) {
	for _, off := range []int64{7 * 60_000, -13 * 60_000, 0, 1, -1} {
		s := newSim(t, false, nil)
		s.off = func(int64) int64 { return off }
		c := s.run(1)
		near(t, "the offset after one exchange", c.Offset, off, 1)
		c = s.run(20)
		near(t, "the offset", c.Offset, off, 1)
		near(t, "the lowest round trip", c.Latency, 40, 0)
		near(t, "jitter on a steady path", c.Jitter, 0, 0)
		near(t, "the uncertainty on a steady 40 ms path", c.Uncertainty, 21, 1)
		near(t, "the forward one-way time", c.Forward, 20, 1)
		near(t, "the back one-way time", c.Back, 20, 1)
		if c.Asymmetry != 0 || c.Synced || c.Samples != ClockWindow {
			t.Errorf("offset %d: asymmetry %d, synced %v, samples %d", off, c.Asymmetry, c.Synced, c.Samples)
		}
	}
}

func TestClockSymmetricDelayThatVaries(t *testing.T) {
	// Both directions wander together. The offset comes from the sample
	// with the lowest round trip, where the two directions are closest to
	// equal, so the error stays small and inside the uncertainty.
	r := rand.New(rand.NewSource(1))
	s := newSim(t, false, nil)
	s.off = func(int64) int64 { return 90_000 }
	extra := make([]int64, 400)
	for i := range extra {
		extra[i] = r.Int63n(80)
	}
	s.fwd = func(i int, _ int64) int64 { return 15 + extra[i] }
	s.back = func(i int, _ int64) int64 { return 15 + extra[i] + r.Int63n(3) }
	c := s.run(200)
	near(t, "the offset", c.Offset, 90_000, 2)
	if c.Jitter < 10 {
		t.Errorf("jitter %d on a path that wanders by up to 160 ms", c.Jitter)
	}
}

func TestClockConstantAsymmetryCannotBeSeen(t *testing.T) {
	// 80 ms one way and 20 the other, for ever. No exchange of four times
	// can tell this from a clock that is 30 ms off: the measured offset is
	// wrong by half the difference, and all that can be said is that the
	// error is within half the round trip. The test holds the code to
	// saying exactly that, and to not pretending otherwise.
	for _, off := range []int64{0, 5000} {
		s := newSim(t, false, nil)
		s.off = func(int64) int64 { return off }
		s.fwd = func(int, int64) int64 { return 80 }
		c := s.run(40)
		near(t, "the offset, wrong by half the asymmetry", c.Offset, off+30, 1)
		if c.Asymmetry != 0 {
			t.Errorf("an asymmetry of %d was claimed from a path that never changed", c.Asymmetry)
		}
		if c.Uncertainty < 50 {
			t.Errorf("uncertainty %d does not cover half the 100 ms round trip", c.Uncertainty)
		}
	}
}

func TestClockJitterInOneDirection(t *testing.T) {
	for seed := int64(1); seed <= 25; seed++ {
		r := rand.New(rand.NewSource(seed))
		s := newSim(t, false, nil)
		s.off = func(int64) int64 { return -42_000 }
		s.fwd = func(int, int64) int64 { return 20 + r.Int63n(120) }
		var worst, asym int64
		for k := 0; k < 300; k++ {
			c := s.run(1)
			if a := abs64(c.Asymmetry); a > asym {
				asym = a
			}
			if k >= ClockWindow {
				if e := abs64(c.Offset + 42_000); e > worst {
					worst = e
				}
			}
		}
		c := s.last
		// The direction that jitters is known, and the quiet one is seen to
		// be quiet.
		if c.ForwardJitter < 20 || c.BackJitter > 1 {
			t.Errorf("seed %d: forward jitter %d, back jitter %d: the forward direction alone jitters by up to 120 ms", seed, c.ForwardJitter, c.BackJitter)
		}
		// Jitter is not a change of path: nothing is put down to asymmetry,
		// and the offset stays near the truth because the lowest round trip
		// in a window is a sample the jitter hardly touched.
		if asym != 0 {
			t.Errorf("seed %d: jitter in one direction was taken for a change of path: asymmetry reached %d", seed, asym)
		}
		if worst > 45 {
			t.Errorf("seed %d: the offset strayed %d ms from the truth under one-way jitter", seed, worst)
		}
	}
}

func TestClockSuddenChangeInOneDirection(t *testing.T) {
	// The forward path goes from 20 ms to 90 ms and stays there; the way
	// back does not change, and neither clock is touched. Looking at the
	// round trip alone, the offset would move by 35 ms once the old samples
	// had left the window. Each direction is watched by itself, so the
	// change is seen for what it is: the offset holds and the 70 ms are
	// reported as asymmetry.
	const off = 250_000
	s := newSim(t, false, nil)
	s.off = func(int64) int64 { return off }
	s.run(20)
	s.fwd = func(int, int64) int64 { return 90 }
	for k := 0; k < 3*ClockWindow; k++ {
		c := s.run(1)
		near(t, "the offset while and after the path changed", c.Offset, off, 1)
	}
	c := s.last
	near(t, "the asymmetry estimate", c.Asymmetry, 70, 1)
	near(t, "the round trip after the change", c.Latency, 110, 0)
	near(t, "the forward one-way time", c.Forward, 90, 2)
	near(t, "the back one-way time", c.Back, 20, 2)

	// And back again: the estimate returns to nothing.
	s.fwd = func(int, int64) int64 { return 20 }
	for k := 0; k < 3*ClockWindow; k++ {
		c = s.run(1)
		near(t, "the offset when the path recovered", c.Offset, off, 1)
	}
	near(t, "the asymmetry estimate after recovery", c.Asymmetry, 0, 1)

	// The same in the other direction has the other sign.
	s.back = func(int, int64) int64 { return 60 }
	c = s.run(3 * ClockWindow)
	near(t, "the offset when the way back slowed", c.Offset, off, 1)
	near(t, "the asymmetry estimate, back slower", c.Asymmetry, -40, 1)

	// A new connection to the same server over the same lopsided path
	// starts from what was known and does not jump.
	again := newSim(t, false, &c)
	again.off, again.back = s.off, s.back
	c2 := again.run(ClockBurst)
	near(t, "the offset on a new connection", c2.Offset, off, 1)
	near(t, "the asymmetry carried over", c2.Asymmetry, -40, 1)
	// Unless the path is no longer like that.
	fresh := newSim(t, false, &c)
	fresh.off = s.off
	c3 := fresh.run(ClockBurst)
	near(t, "the offset on a new connection over a new path", c3.Offset, off, 1)
	near(t, "the asymmetry not carried over", c3.Asymmetry, 0, 0)
}

func TestClockSetWhileConnected(t *testing.T) {
	// The server's clock is set forward half a second. Both one-way figures
	// move by that much in opposite senses, which a path cannot do: it is
	// the clock. The samples from before are dropped as soon as a few agree,
	// not kept until they age out of the window.
	s := newSim(t, false, nil)
	var step int64
	base := s.now
	s.off = func(now int64) int64 {
		if now >= base+20*30_000 {
			return 1000 + step
		}
		return 1000
	}
	step = 500
	c := s.run(20)
	near(t, "the offset before the clock was set", c.Offset, 1000, 1)
	for k := 0; k < clockRecent; k++ {
		c = s.exchange() // the figures lag the truth for these few; nothing is promised of them
	}
	near(t, "the offset once a few samples agree", c.Offset, 1500, 1)
	if c.Asymmetry != 0 {
		t.Errorf("a clock that was set was taken for a change of path: asymmetry %d", c.Asymmetry)
	}
	c = s.run(40)
	near(t, "the offset afterwards", c.Offset, 1500, 1)
}

func TestClockSyncedMode(t *testing.T) {
	// The operator has said the clocks are synchronised. The path is 80 ms
	// one way and 20 the other, and the server's clock is in truth 3 ms
	// ahead. Unsynced, the offset would be measured as 33. Synced, no more
	// than the bound is applied and the rest is reported as asymmetry.
	s := newSim(t, true, nil)
	s.off = func(int64) int64 { return 3 }
	s.fwd = func(int, int64) int64 { return 80 }
	c := s.run(30)
	if !c.Synced {
		t.Fatal("the figures do not say they were made in synced mode")
	}
	near(t, "the offset applied", c.Offset, ClockSyncedBound, 0)
	near(t, "the asymmetry estimate", c.Asymmetry, 2*(33-ClockSyncedBound), 2)
	if c.Uncertainty > 2*ClockSyncedBound {
		t.Errorf("uncertainty %d is beyond what two synchronised clocks can differ by", c.Uncertainty)
	}
	near(t, "the forward one-way time", c.Forward, 80, 2*ClockSyncedBound)
	near(t, "the back one-way time", c.Back, 20, 2*ClockSyncedBound)

	// A small offset on a symmetric path is applied as measured.
	s = newSim(t, true, nil)
	s.off = func(int64) int64 { return -4 }
	c = s.run(30)
	near(t, "a small offset", c.Offset, -4, 1)
	near(t, "no asymmetry", c.Asymmetry, 0, 2)

	// The mode is the operator's to choose and nothing turns it on: the
	// same path unsynced reports the 33 and claims no asymmetry.
	s = newSim(t, false, nil)
	s.off = func(int64) int64 { return 3 }
	s.fwd = func(int, int64) int64 { return 80 }
	c = s.run(30)
	if c.Synced || c.Asymmetry != 0 {
		t.Errorf("unsynced: synced %v, asymmetry %d", c.Synced, c.Asymmetry)
	}
	near(t, "the offset unsynced", c.Offset, 33, 1)
}

func TestClockImpossibleSamples(t *testing.T) {
	tr := NewClockTrack(false, nil)
	for _, s := range []ClockSample{
		{T0: 100, T1: 50, T2: 40, T3: 120},  // answered before it was asked
		{T0: 100, T1: 50, T2: 60, T3: 90},   // arrived before it left
		{T0: 100, T1: 50, T2: 150, T3: 120}, // the server held it longer than the whole round trip
	} {
		if _, ok := tr.Add(s); ok {
			t.Errorf("an impossible sample was taken: %+v", s)
		}
	}
	if _, ok := tr.Clock(); ok {
		t.Error("figures exist though no sample was taken")
	}
	c, ok := tr.Add(ClockSample{T0: 100, T1: 5110, T2: 5112, T3: 122})
	if !ok || c.Offset != 5000 || c.Latency != 20 || c.Samples != 1 {
		t.Errorf("a sound sample: %+v, %v", c, ok)
	}
	// An offset that has not been measured for a while is trusted less.
	if later := c.UncertaintyAt(c.MeasuredAt.Add(time.Hour)); later < c.Uncertainty+150 {
		t.Errorf("an hour without measuring: uncertainty %d, was %d", later, c.Uncertainty)
	}
}

// TestPingPongOverAConnection runs the frames themselves over a real
// connection between a reference and a server whose clock is eleven minutes
// behind.
func TestPingPongOverAConnection(t *testing.T) {
	a, b := ident(t, "a.example", KindServer), ident(t, "b.example", KindServer)
	got := make(chan *Conn, 1)
	url := listen(t, a, func(Frame, []byte, []byte) error { return nil }, got)
	dialled, err := Dial(context.Background(), DialOptions{URL: url, Identity: b})
	if err != nil {
		t.Fatal(err)
	}
	defer dialled.Close()
	ref := <-got
	defer ref.Close()

	refNow := func() int64 { return time.Now().UnixMilli() }
	const behind = 11 * 60_000
	serverNow := func() int64 { return time.Now().UnixMilli() - behind }

	// The server answers pings and remembers what it is told.
	var told atomic.Pointer[Clock]
	go func() {
		for {
			f, _, err := dialled.Read(context.Background())
			if err != nil {
				return
			}
			if f.Frame == FPing {
				t1 := serverNow()
				if f.Clock != nil {
					told.Store(f.Clock)
				}
				dialled.AnswerPing(f, t1, serverNow)
			}
		}
	}()
	var stored atomic.Pointer[Clock]
	p := NewPinger(ref, refNow, NewClockTrack(false, nil), func(c Clock) { stored.Store(&c) })
	go p.Run()
	go func() {
		for {
			f, _, err := ref.Read(context.Background())
			if err != nil {
				return
			}
			if f.Frame == FPong {
				p.Pong(f)
			}
		}
	}()
	// The burst after connecting gives a usable offset well inside two
	// seconds, and the server has been told it by then.
	deadline := time.Now().Add(2 * time.Second)
	for {
		c := told.Load()
		if c != nil && c.Samples >= 3 {
			near(t, "the offset the server was told", c.Offset, -behind, 3)
			if c.Uncertainty < 1 || c.Uncertainty > 50 {
				t.Errorf("uncertainty on loopback: %d", c.Uncertainty)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no usable offset within two seconds of connecting: told %+v", c)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if c := stored.Load(); c == nil || abs64(c.Offset+behind) > 3 {
		t.Errorf("what the reference stored: %+v", c)
	}
	// A pong that answers nothing is ignored.
	before, _ := p.Track.Clock()
	zero := int64(0)
	p.Pong(Frame{Frame: FPong, Re: 9999, T0: &zero, T1: &zero, T2: &zero})
	if after, _ := p.Track.Clock(); after.Samples != before.Samples && after.MeasuredAt == before.MeasuredAt {
		t.Error("a pong nobody asked for was taken as a sample")
	}
}

// The reference's own clock may be the one that is set. Its samples then
// carry times that run backwards; the figures must still be sound.
func TestClockOfTheReferenceSetBack(t *testing.T) {
	tr := NewClockTrack(false, nil)
	now := int64(1_700_000_000_000)
	var c Clock
	for i := 0; i < 6; i++ {
		c, _ = tr.Add(ClockSample{T0: now, T1: now + 20 + 5000, T2: now + 21 + 5000, T3: now + 41})
		now += 150
	}
	near(t, "the offset before", c.Offset, 5000, 1)
	// Ten minutes back: to the reference the server is now ten minutes
	// further ahead.
	ref := now - 600_000
	for i := 0; i < clockRecent; i++ {
		c, _ = tr.Add(ClockSample{T0: ref, T1: now + 20 + 5000, T2: now + 21 + 5000, T3: ref + 41})
		now, ref = now+150, ref+150
	}
	near(t, "the offset after the reference's clock was set back", c.Offset, 605_000, 1)
	if c.Uncertainty < 20 || c.Uncertainty > 30 || c.Asymmetry != 0 || c.ForwardJitter > 1 || c.BackJitter > 1 {
		t.Errorf("the figures after the reference's clock was set back: %+v", c)
	}
}
