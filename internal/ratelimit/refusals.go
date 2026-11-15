package ratelimit

import (
	"log/slog"
	"sync"
	"time"
)

// A limit that refuses in silence looks, from outside, like a server that
// is slow for no reason: messages arrive seconds late while every
// processor idles, and nothing says why. So refusals are logged. They come
// in floods, though (a limit that is hit is hit hundreds of times a
// minute), and a line for each would bury the log. Refusals writes one line
// for the first refusal of a kind from a sender, counts the ones that
// follow, and says how many there were when its interval is up, for as long
// as they keep coming.

// Refusals logs refusals, a line at most every interval for each limit and
// sender. The zero value is not usable; make one with NewRefusals.
type Refusals struct {
	log   *slog.Logger
	every time.Duration
	msg   string

	mu   sync.Mutex
	open map[refusalKey]*refusalCount
}

type refusalKey struct{ limit, from string }

type refusalCount struct {
	n     int64 // refused since the last line
	total int64 // refused since the first line of this run of refusals
}

// RefusalsEvery is how often a limit that keeps refusing one sender is
// written to the log.
const RefusalsEvery = 10 * time.Second

// NewRefusals makes a log of refusals. msg is the line's message; every is
// the interval, RefusalsEvery if zero.
func NewRefusals(log *slog.Logger, msg string, every time.Duration) *Refusals {
	if log == nil {
		log = slog.Default()
	}
	if every <= 0 {
		every = RefusalsEvery
	}
	return &Refusals{log: log, every: every, msg: msg, open: map[refusalKey]*refusalCount{}}
}

// Note records one refusal by the limit named, of the sender named. The
// first of a run is logged at once; the rest are counted and logged
// together. retryAfter is what the sender was told to wait, in seconds, or
// 0. A nil *Refusals notes nothing.
func (r *Refusals) Note(limit, from string, retryAfter int) {
	if r == nil {
		return
	}
	key := refusalKey{limit, from}
	r.mu.Lock()
	c := r.open[key]
	if c != nil {
		c.n++
		c.total++
		r.mu.Unlock()
		return
	}
	r.open[key] = &refusalCount{total: 1}
	r.mu.Unlock()
	r.log.Warn(r.msg, "limit", limit, "from", from, "refused", 1, "retry_after_seconds", retryAfter)
	time.AfterFunc(r.every, func() { r.report(key) })
}

// report writes how many refusals followed the last line, and comes back
// after another interval; when none did, the run is over.
func (r *Refusals) report(key refusalKey) {
	r.mu.Lock()
	c := r.open[key]
	if c == nil || c.n == 0 {
		delete(r.open, key)
		r.mu.Unlock()
		return
	}
	n, total := c.n, c.total
	c.n = 0
	r.mu.Unlock()
	r.log.Warn(r.msg, "limit", key.limit, "from", key.from, "refused", n, "in_seconds", int(r.every/time.Second), "refused_in_all", total)
	time.AfterFunc(r.every, func() { r.report(key) })
}
