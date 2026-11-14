// Package ratelimit counts how often something happens, per key, and says
// when it has happened too often.
//
// Each key (an account, a token, an address) has a token bucket: it holds
// up to burst tokens, an event takes one, and tokens come back at a steady
// rate. Everything is in memory and is forgotten on restart, which
// docs/AUTH.md section 14 accepts. As that document's section 1 requires of
// anything held in memory, every entry expires and the number of entries is
// capped, with the oldest dropped first.
package ratelimit

import (
	"container/list"
	"math"
	"sync"
	"time"
)

// DefaultMaxKeys is how many keys a Limiter remembers unless told otherwise.
// An entry is about a hundred bytes plus its key, so a full limiter is in
// the tens of megabytes at worst.
const DefaultMaxKeys = 100_000

// slack forgives the rounding of float arithmetic, so that a caller who
// waits exactly as long as it was told finds its token there.
const slack = 1e-9

// Limiter is a set of token buckets, one per key. It is safe for use from
// many goroutines.
type Limiter struct {
	rate  float64 // tokens a second
	burst float64 // bucket size
	limit int     // burst as given
	max   int     // most keys remembered
	now   func() time.Time
	// fill is how long an empty bucket takes to fill. A bucket untouched for
	// that long is full, whatever it held, and can be forgotten.
	fill time.Duration

	mu      sync.Mutex
	buckets map[string]*list.Element
	// order holds every bucket, least recently used first. Since a bucket
	// expires a fixed time after its last use, it is also the order in which
	// they expire.
	order *list.List
}

// bucket is the state of one key.
type bucket struct {
	key    string
	tokens float64   // as of last
	last   time.Time // when tokens was worked out
}

// Option changes how a Limiter is made.
type Option func(*Limiter)

// WithClock makes the limiter read the time from now instead of the system
// clock. It is for tests.
func WithClock(now func() time.Time) Option {
	return func(l *Limiter) { l.now = now }
}

// WithMaxKeys sets how many keys are remembered at once. When a new key
// arrives and the limiter is full, the key that has gone unused longest is
// forgotten, and starts again with a full bucket if it comes back. That
// loosens the limit for that key, which is the price of a bound on memory;
// it only happens when more than n keys are in play within one refill time.
func WithMaxKeys(n int) Option {
	return func(l *Limiter) {
		if n > 0 {
			l.max = n
		}
	}
}

// Per writes "n in window" as a rate for New: Per(30, 5*time.Minute) is
// thirty in five minutes, as tokens a second.
func Per(n int, window time.Duration) float64 {
	return float64(n) / window.Seconds()
}

// New makes a limiter that lets each key do burst things at once and
// perSecond things a second from then on. "Thirty in five minutes" is
// New(Per(30, 5*time.Minute), 30).
//
// It panics if perSecond or burst is not positive: a limit that never lets
// anything through, or never refills, is a mistake in the program and not
// something to discover from a locked-out person.
func New(perSecond float64, burst int, opts ...Option) *Limiter {
	if !(perSecond > 0) || math.IsInf(perSecond, 0) {
		panic("ratelimit: the rate must be a positive number of events a second")
	}
	if burst < 1 {
		panic("ratelimit: the burst must be at least 1")
	}
	l := &Limiter{
		rate:    perSecond,
		burst:   float64(burst),
		limit:   burst,
		max:     DefaultMaxKeys,
		now:     time.Now,
		fill:    seconds(float64(burst) / perSecond),
		buckets: make(map[string]*list.Element),
		order:   list.New(),
	}
	for _, opt := range opts {
		opt(l)
	}
	return l
}

// Result is the answer to one Take or Peek, with what the API layer needs
// for the RateLimit-Limit, RateLimit-Remaining and RateLimit-Reset headers.
type Result struct {
	// OK is whether the event may go ahead.
	OK bool
	// Limit is the size of the bucket: the burst the limiter was made with.
	Limit int
	// Remaining is how many more events could go ahead right now.
	Remaining int
	// RetryAfter is, for an event that was refused, how long until it may
	// go ahead. It is zero when OK is true.
	RetryAfter time.Duration
	// Reset is how long until the bucket is full again, if nothing more is
	// taken from it.
	Reset time.Duration
}

// RetryAfterSeconds is RetryAfter in whole seconds, rounded up, for
// retry_after_seconds and the Retry-After header. Rounding up means a
// client that waits that long is never turned away again for being early.
func (r Result) RetryAfterSeconds() int { return ceilSeconds(r.RetryAfter) }

// ResetSeconds is Reset in whole seconds, rounded up, for RateLimit-Reset.
func (r Result) ResetSeconds() int { return ceilSeconds(r.Reset) }

// Allow takes one token for key. It reports whether there was one, and if
// not, how long until there will be.
func (l *Limiter) Allow(key string) (ok bool, retryAfter time.Duration) {
	r := l.Take(key)
	return r.OK, r.RetryAfter
}

// Take takes one token for key, if there is one, and reports the state of
// the bucket afterwards. A refused Take takes nothing, so being turned away
// does not push the wait further out.
func (l *Limiter) Take(key string) Result {
	return l.use(key, true)
}

// Peek reports the state of key's bucket without taking anything. It is for
// limits that count failures only (docs/AUTH.md section 14, "5 failures an
// hour"): Peek before the attempt to see whether it may be made, and Take
// after it has failed.
func (l *Limiter) Peek(key string) Result {
	return l.use(key, false)
}

// Len is how many keys the limiter remembers at the moment.
func (l *Limiter) Len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.expire(l.now())
	return len(l.buckets)
}

func (l *Limiter) use(key string, take bool) Result {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	l.expire(now)

	tokens := l.burst
	el, known := l.buckets[key]
	if known {
		b := el.Value.(*bucket)
		// A clock that steps backwards must not take tokens away, so time
		// that has not passed counts as none.
		if elapsed := now.Sub(b.last).Seconds(); elapsed > 0 {
			tokens = math.Min(l.burst, b.tokens+elapsed*l.rate)
		} else {
			tokens = b.tokens
		}
	}

	ok := tokens >= 1-slack
	if take && ok {
		tokens = math.Max(0, tokens-1)
	}

	// Only a Take that went through changes anything worth remembering. A
	// refusal leaves the bucket as it was, and a Peek at a key not seen
	// before leaves no entry, so asking about many keys cannot fill the
	// limiter.
	if take && ok {
		if known {
			b := el.Value.(*bucket)
			b.tokens, b.last = tokens, now
			l.order.MoveToBack(el)
		} else {
			if len(l.buckets) >= l.max {
				l.forget(l.order.Front())
			}
			l.buckets[key] = l.order.PushBack(&bucket{key: key, tokens: tokens, last: now})
		}
	}

	r := Result{
		OK:        ok,
		Limit:     l.limit,
		Remaining: int(math.Floor(tokens + slack)),
		Reset:     seconds((l.burst - tokens) / l.rate),
	}
	if !ok {
		r.RetryAfter = seconds((1 - tokens) / l.rate)
	}
	return r
}

// expire forgets buckets nobody has used for a whole refill time. Such a
// bucket is full, and a full bucket is the same as no bucket.
func (l *Limiter) expire(now time.Time) {
	for el := l.order.Front(); el != nil; el = l.order.Front() {
		if now.Sub(el.Value.(*bucket).last) < l.fill {
			return
		}
		l.forget(el)
	}
}

func (l *Limiter) forget(el *list.Element) {
	delete(l.buckets, el.Value.(*bucket).key)
	l.order.Remove(el)
}

// seconds turns a number of seconds into a Duration, rounded up to the next
// nanosecond so that waiting that long is always long enough, and held
// within what a Duration can say.
func seconds(s float64) time.Duration {
	if s <= 0 {
		return 0
	}
	ns := math.Ceil(s * float64(time.Second))
	if ns >= float64(math.MaxInt64) {
		return time.Duration(math.MaxInt64)
	}
	return time.Duration(ns)
}

// ceilSeconds is d in whole seconds, rounded up.
func ceilSeconds(d time.Duration) int {
	if d <= 0 {
		return 0
	}
	s := d / time.Second
	if d%time.Second != 0 {
		s++
	}
	return int(s)
}
