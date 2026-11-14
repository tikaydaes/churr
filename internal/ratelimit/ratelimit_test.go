package ratelimit

import (
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// clock is a time source a test moves by hand.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func newClock() *clock {
	return &clock{t: time.Date(2026, 11, 13, 9, 30, 0, 0, time.UTC)}
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func TestPer(t *testing.T) {
	cases := []struct {
		n      int
		window time.Duration
		want   float64
	}{
		{30, 5 * time.Minute, 0.1},
		{60, time.Minute, 1},
		{5, time.Hour, 5.0 / 3600},
		{50, time.Second, 50},
		{10, 24 * time.Hour, 10.0 / 86400},
	}
	for _, c := range cases {
		if got := Per(c.n, c.window); math.Abs(got-c.want) > 1e-12 {
			t.Errorf("Per(%d, %v) = %v, want %v", c.n, c.window, got, c.want)
		}
	}
}

func TestBurstThenRefusal(t *testing.T) {
	c := newClock()
	l := New(1, 5, WithClock(c.Now))
	for i := 0; i < 5; i++ {
		ok, retry := l.Allow("a")
		if !ok || retry != 0 {
			t.Fatalf("event %d of the burst: ok = %v, retry = %v", i+1, ok, retry)
		}
	}
	ok, retry := l.Allow("a")
	if ok {
		t.Fatal("a sixth event got through a burst of five")
	}
	if retry != time.Second {
		t.Errorf("retry after = %v, want 1s", retry)
	}
}

func TestKeysAreSeparate(t *testing.T) {
	c := newClock()
	l := New(1, 2, WithClock(c.Now))
	l.Allow("a")
	l.Allow("a")
	if ok, _ := l.Allow("a"); ok {
		t.Fatal("a third event got through for a")
	}
	if ok, _ := l.Allow("b"); !ok {
		t.Error("b was refused because a had used its bucket up")
	}
	if ok, _ := l.Allow(""); !ok {
		t.Error("the empty key was refused; it is a key like any other")
	}
}

func TestRefill(t *testing.T) {
	c := newClock()
	// Thirty in five minutes: one token every ten seconds.
	l := New(Per(30, 5*time.Minute), 30, WithClock(c.Now))
	for i := 0; i < 30; i++ {
		if ok, _ := l.Allow("a"); !ok {
			t.Fatalf("event %d of 30 was refused", i+1)
		}
	}
	ok, retry := l.Allow("a")
	if ok {
		t.Fatal("event 31 got through")
	}
	if retry < 10*time.Second || retry > 10*time.Second+time.Millisecond {
		t.Errorf("retry after = %v, want 10s", retry)
	}

	// Not quite long enough.
	c.Advance(9 * time.Second)
	ok, retry = l.Allow("a")
	if ok {
		t.Fatal("got through a second early")
	}
	if retry < time.Second || retry > time.Second+time.Millisecond {
		t.Errorf("retry after = %v, want 1s", retry)
	}

	// Waiting exactly as long as told is always enough.
	c.Advance(retry)
	if ok, _ := l.Allow("a"); !ok {
		t.Fatal("refused after waiting exactly the retry-after time")
	}
	if ok, _ := l.Allow("a"); ok {
		t.Fatal("ten seconds bought more than one event")
	}

	// A long quiet spell fills the bucket and no further.
	c.Advance(24 * time.Hour)
	for i := 0; i < 30; i++ {
		if ok, _ := l.Allow("a"); !ok {
			t.Fatalf("after a day, event %d of 30 was refused", i+1)
		}
	}
	if ok, _ := l.Allow("a"); ok {
		t.Fatal("a day's quiet gave more than one burst")
	}
}

func TestWaitingTheRetryAfterAlwaysWorks(t *testing.T) {
	// Rates whose arithmetic does not come out even.
	for _, rate := range []float64{Per(30, 5*time.Minute), Per(5, time.Hour), 1.0 / 3, 7, 0.37, Per(10, 24*time.Hour)} {
		c := newClock()
		l := New(rate, 3, WithClock(c.Now))
		for i := 0; i < 3; i++ {
			l.Allow("a")
		}
		for i := 0; i < 200; i++ {
			ok, retry := l.Allow("a")
			if ok {
				t.Fatalf("rate %v: got through with an empty bucket", rate)
			}
			if retry <= 0 {
				t.Fatalf("rate %v: refused with no retry-after", rate)
			}
			c.Advance(retry)
			if ok, _ := l.Allow("a"); !ok {
				t.Fatalf("rate %v, round %d: refused after waiting the %v it asked for", rate, i, retry)
			}
		}
	}
}

func TestARefusalCostsNothing(t *testing.T) {
	c := newClock()
	l := New(1, 1, WithClock(c.Now))
	l.Allow("a")
	// Hammering while refused must not push the wait further out.
	for i := 0; i < 100; i++ {
		c.Advance(5 * time.Millisecond)
		if ok, _ := l.Allow("a"); ok {
			t.Fatal("got through early")
		}
	}
	c.Advance(500 * time.Millisecond)
	if ok, _ := l.Allow("a"); !ok {
		t.Error("refused a second after the last event that went through")
	}
}

func TestTakeGivesTheHeaderNumbers(t *testing.T) {
	c := newClock()
	// 120 a minute: two tokens a second.
	l := New(Per(120, time.Minute), 120, WithClock(c.Now))

	r := l.Take("a")
	if !r.OK || r.Limit != 120 || r.Remaining != 119 || r.RetryAfter != 0 {
		t.Errorf("first take = %+v", r)
	}
	// One token is missing, and one comes back every half second.
	if r.Reset != 500*time.Millisecond || r.ResetSeconds() != 1 {
		t.Errorf("reset = %v (%d s), want 500ms (1 s)", r.Reset, r.ResetSeconds())
	}

	l.Take("a")
	r = l.Take("a")
	if r.Remaining != 117 || r.ResetSeconds() != 2 {
		t.Errorf("third take: remaining = %d, reset = %d s; want 117 and 2", r.Remaining, r.ResetSeconds())
	}

	for i := 3; i < 120; i++ {
		r = l.Take("a")
	}
	if !r.OK || r.Remaining != 0 || r.Reset != time.Minute || r.ResetSeconds() != 60 {
		t.Errorf("last of the burst = %+v", r)
	}

	r = l.Take("a")
	if r.OK || r.Limit != 120 || r.Remaining != 0 {
		t.Errorf("past the limit = %+v", r)
	}
	if r.RetryAfter != 500*time.Millisecond || r.RetryAfterSeconds() != 1 {
		t.Errorf("retry after = %v (%d s), want 500ms (1 s)", r.RetryAfter, r.RetryAfterSeconds())
	}
	if r.Reset != time.Minute {
		t.Errorf("reset past the limit = %v, want 1m", r.Reset)
	}

	// Half a minute later half the bucket is back.
	c.Advance(30 * time.Second)
	r = l.Take("a")
	if !r.OK || r.Remaining != 59 || r.ResetSeconds() != 31 {
		t.Errorf("after 30s = %+v (reset %d s); want remaining 59, reset 31 s", r, r.ResetSeconds())
	}
}

func TestSecondsRoundUp(t *testing.T) {
	cases := map[time.Duration]int{
		0:                              0,
		-time.Second:                   0,
		time.Nanosecond:                1,
		999 * time.Millisecond:         1,
		time.Second:                    1,
		time.Second + time.Nanosecond:  2,
		42 * time.Second:               42,
		time.Duration(math.MaxInt64):   int(time.Duration(math.MaxInt64)/time.Second) + 1,
		90*time.Minute + 1:             5401,
		59*time.Second + 1*time.Second: 60,
	}
	for d, want := range cases {
		if got := (Result{RetryAfter: d}).RetryAfterSeconds(); got != want {
			t.Errorf("RetryAfterSeconds(%v) = %d, want %d", d, got, want)
		}
		if got := (Result{Reset: d}).ResetSeconds(); got != want {
			t.Errorf("ResetSeconds(%v) = %d, want %d", d, got, want)
		}
	}
}

func TestPeekTakesNothing(t *testing.T) {
	c := newClock()
	// Five failures an hour.
	l := New(Per(5, time.Hour), 5, WithClock(c.Now))

	for i := 0; i < 100; i++ {
		if r := l.Peek("addr"); !r.OK || r.Remaining != 5 || r.Reset != 0 {
			t.Fatalf("peek %d at an untouched key = %+v", i, r)
		}
	}
	if l.Len() != 0 {
		t.Errorf("peeking left %d entries behind", l.Len())
	}

	for i := 0; i < 5; i++ {
		if r := l.Peek("addr"); !r.OK {
			t.Fatalf("attempt %d was not allowed", i+1)
		}
		l.Take("addr") // the attempt failed
	}
	r := l.Peek("addr")
	if r.OK || r.Remaining != 0 || r.RetryAfter != 12*time.Minute {
		t.Errorf("after five failures peek = %+v; want refused with 12m to wait", r)
	}
	if again := l.Peek("addr"); again != r {
		t.Errorf("a second peek differs from the first: %+v then %+v", r, again)
	}
	c.Advance(12 * time.Minute)
	if r := l.Peek("addr"); !r.OK || r.Remaining != 1 {
		t.Errorf("twelve minutes later peek = %+v", r)
	}
}

func TestEntriesExpireWhenFull(t *testing.T) {
	c := newClock()
	l := New(1, 10, WithClock(c.Now)) // an empty bucket fills in 10s
	for i := 0; i < 50; i++ {
		l.Allow(fmt.Sprintf("key-%d", i))
	}
	if l.Len() != 50 {
		t.Fatalf("Len = %d, want 50", l.Len())
	}
	c.Advance(9 * time.Second)
	l.Allow("late")
	if l.Len() != 51 {
		t.Errorf("Len = %d after 9s, want 51: nothing is due yet", l.Len())
	}
	c.Advance(time.Second)
	if l.Len() != 1 {
		t.Errorf("Len = %d after 10s, want 1: only the late key is left", l.Len())
	}
	c.Advance(time.Hour)
	if l.Len() != 0 {
		t.Errorf("Len = %d after an hour, want 0", l.Len())
	}
	// A key that expired behaves exactly as one never seen.
	if r := l.Take("key-0"); !r.OK || r.Remaining != 9 {
		t.Errorf("an expired key came back as %+v", r)
	}
}

func TestSizeCapDropsTheOldestFirst(t *testing.T) {
	c := newClock()
	l := New(Per(1, time.Hour), 1, WithClock(c.Now), WithMaxKeys(3))
	for _, k := range []string{"a", "b", "c"} {
		l.Allow(k)
		c.Advance(time.Second)
	}
	for _, k := range []string{"a", "b", "c"} {
		if l.Peek(k).OK {
			t.Fatalf("%s has a token left; the test's own setup is wrong", k)
		}
	}

	// A fourth key pushes out a, the one unused longest.
	l.Allow("d")
	if l.Len() != 3 {
		t.Errorf("Len = %d, want the cap of 3", l.Len())
	}
	if !l.Peek("a").OK {
		t.Error("a was not the one dropped")
	}
	for _, k := range []string{"b", "c", "d"} {
		if l.Peek(k).OK {
			t.Errorf("%s was dropped, though a was older", k)
		}
	}

	// Never more than the cap, however many keys arrive.
	for i := 0; i < 1000; i++ {
		l.Allow(fmt.Sprintf("flood-%d", i))
		if n := l.Len(); n > 3 {
			t.Fatalf("Len = %d, over the cap of 3", n)
		}
	}
}

func TestSizeCapGoesByLastUseNotFirst(t *testing.T) {
	c := newClock()
	l := New(Per(10, time.Hour), 10, WithClock(c.Now), WithMaxKeys(2))
	l.Allow("a")
	c.Advance(time.Second)
	l.Allow("b")
	c.Advance(time.Second)
	l.Allow("a") // a is now the fresher of the two
	c.Advance(time.Second)
	l.Allow("c") // so b goes

	if r := l.Peek("a"); r.Remaining != 8 {
		t.Errorf("a was dropped: remaining = %d, want 8", r.Remaining)
	}
	if r := l.Peek("b"); r.Remaining != 10 {
		t.Errorf("b was kept: remaining = %d, want a full 10", r.Remaining)
	}
}

func TestDefaultCap(t *testing.T) {
	l := New(1, 1)
	if l.max != DefaultMaxKeys {
		t.Errorf("max = %d, want %d", l.max, DefaultMaxKeys)
	}
	// A cap that makes no sense is ignored, not obeyed.
	l = New(1, 1, WithMaxKeys(0), WithMaxKeys(-5))
	if l.max != DefaultMaxKeys {
		t.Errorf("max = %d after WithMaxKeys(0), want the default", l.max)
	}
}

func TestClockSteppingBackwards(t *testing.T) {
	c := newClock()
	l := New(1, 2, WithClock(c.Now))
	l.Allow("a")
	c.Advance(-time.Hour)
	if r := l.Take("a"); !r.OK || r.Remaining != 0 {
		t.Errorf("after the clock went back = %+v; want the one token that was left", r)
	}
	if ok, _ := l.Allow("a"); ok {
		t.Error("the clock going back refilled the bucket")
	}
}

func TestNewRefusesNonsense(t *testing.T) {
	for name, f := range map[string]func(){
		"zero rate":     func() { New(0, 1) },
		"negative rate": func() { New(-1, 1) },
		"NaN rate":      func() { New(math.NaN(), 1) },
		"infinite rate": func() { New(math.Inf(1), 1) },
		"zero burst":    func() { New(1, 0) },
		"negative":      func() { New(1, -3) },
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s: New did not panic", name)
				}
			}()
			f()
		}()
	}
}

func TestRealClock(t *testing.T) {
	// No injected clock: the limiter must work on the system's.
	l := New(1000, 2)
	if ok, _ := l.Allow("a"); !ok {
		t.Fatal("first event refused")
	}
	if ok, _ := l.Allow("a"); !ok {
		t.Fatal("second event refused")
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		if ok, _ := l.Allow("a"); ok {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("no token came back in five seconds at a thousand a second")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestConcurrentUse(t *testing.T) {
	c := newClock()
	const burst = 100
	l := New(Per(1, time.Hour), burst, WithClock(c.Now), WithMaxKeys(64))

	var shared atomic.Int64
	var wg sync.WaitGroup
	for g := 0; g < 16; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 500; i++ {
				// Everyone fights over one key...
				if ok, _ := l.Allow("shared"); ok {
					shared.Add(1)
				}
				// ...and churns through keys of their own, past the cap.
				own := fmt.Sprintf("g%d-%d", g, i%50)
				l.Take(own)
				l.Peek(own)
				l.Len()
			}
		}(g)
	}
	wg.Wait()

	// The clock never moved. Without evictions exactly one burst gets
	// through; each time the churn pushed the shared key out it came back
	// full, so more can, but never fewer.
	if got := shared.Load(); got < burst {
		t.Errorf("%d events got through for the shared key, want at least the burst of %d", got, burst)
	}
	if n := l.Len(); n > 64 {
		t.Errorf("Len = %d, over the cap of 64", n)
	}
}

func TestConcurrentUseIsExact(t *testing.T) {
	// With room for every key, a burst is a burst however many goroutines
	// ask at once: not one event more, not one fewer.
	c := newClock()
	const burst = 250
	l := New(Per(1, time.Hour), burst, WithClock(c.Now))

	var through atomic.Int64
	var wg sync.WaitGroup
	for g := 0; g < 20; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				if ok, _ := l.Allow("k"); ok {
					through.Add(1)
				}
			}
		}()
	}
	wg.Wait()
	if got := through.Load(); got != burst {
		t.Errorf("%d events got through, want exactly %d", got, burst)
	}
}
