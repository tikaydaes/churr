// Package timing is a cheap way to see, on a running server, where the
// time of a request goes: how long each part of the post path took, how
// long the database pool and the scheduler kept goroutines waiting.
//
// It is off unless the server is started with CHURR_DEBUG_TIMING set to
// anything. Then every Observe is kept, and once a minute one line is
// logged at info level for each name seen, with the count, the median, the
// 99th percentile and the largest, in milliseconds, followed by a line of
// what the Go runtime and the database pool say about the same minute.
// Off, an Observe is one atomic read.
package timing

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"os"
	"runtime"
	"runtime/metrics"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// Env is the environment variable that turns timing on.
const Env = "CHURR_DEBUG_TIMING"

var (
	on      atomic.Bool
	mu      sync.Mutex
	samples = map[string]*series{}
)

// keep bounds the samples kept of one name in one minute; beyond it the
// count and the largest go on, and the percentiles are of the first ones.
const keep = 50000

type series struct {
	n   int
	max time.Duration
	d   []time.Duration
}

// On reports whether timing is on. Callers use it to skip reading the
// clock.
func On() bool { return on.Load() }

// Observe notes that the part of the work called name took d.
func Observe(name string, d time.Duration) {
	if !on.Load() {
		return
	}
	mu.Lock()
	s := samples[name]
	if s == nil {
		s = &series{}
		samples[name] = s
	}
	s.n++
	if d > s.max {
		s.max = d
	}
	if len(s.d) < keep {
		s.d = append(s.d, d)
	}
	mu.Unlock()
}

// Since is Observe for a part that began at start, and does nothing with a
// zero start: `var t time.Time; if timing.On() { t = time.Now() }`.
func Since(name string, start time.Time) {
	if !start.IsZero() {
		Observe(name, time.Since(start))
	}
}

// Now is time.Now when timing is on and the zero time otherwise.
func Now() time.Time {
	if !on.Load() {
		return time.Time{}
	}
	return time.Now()
}

func ms(d time.Duration) string { return fmt.Sprintf("%.2f", float64(d)/float64(time.Millisecond)) }

// Start turns timing on if the environment asks for it and logs the
// summary once a minute until ctx ends. more returns further pairs for the
// runtime line (the database pool's numbers).
func Start(ctx context.Context, log *slog.Logger, more func() []any) {
	if os.Getenv(Env) == "" {
		return
	}
	on.Store(true)
	every := time.Minute
	if d, err := time.ParseDuration(os.Getenv(Env)); err == nil && d >= time.Second {
		every = d
	}
	log.Info("timing is on: a summary of where time goes is logged at this interval", "every", every.String())
	go func() {
		t := time.NewTicker(every)
		defer t.Stop()
		prev := readRuntime()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
			mu.Lock()
			got := samples
			samples = map[string]*series{}
			mu.Unlock()
			names := make([]string, 0, len(got))
			for name := range got {
				names = append(names, name)
			}
			sort.Strings(names)
			for _, name := range names {
				s := got[name]
				sort.Slice(s.d, func(i, j int) bool { return s.d[i] < s.d[j] })
				var sum time.Duration
				for _, d := range s.d {
					sum += d
				}
				log.Info("timing", "part", name, "count", s.n, "p50_ms", ms(s.d[len(s.d)/2]),
					"p99_ms", ms(s.d[len(s.d)*99/100]), "max_ms", ms(s.max), "sum_ms", ms(sum))
			}
			now := readRuntime()
			args := now.since(prev)
			if more != nil {
				args = append(args, more()...)
			}
			log.Info("timing runtime", args...)
			prev = now
		}
	}()
}

// runtimeNow is what the runtime counts, read at one moment.
type runtimeNow struct {
	sched     *metrics.Float64Histogram
	gcPause   *metrics.Float64Histogram
	mutexWait float64
	cpuTotal  float64
	cpuIdle   float64
	cpuGC     float64
}

const (
	mSched   = "/sched/latencies:seconds"
	mGCPause = "/sched/pauses/total/gc:seconds"
	mMutex   = "/sync/mutex/wait/total:seconds"
	mCPU     = "/cpu/classes/total:cpu-seconds"
	mIdle    = "/cpu/classes/idle:cpu-seconds"
	mGCCPU   = "/cpu/classes/gc/total:cpu-seconds"
)

func readRuntime() runtimeNow {
	s := []metrics.Sample{{Name: mSched}, {Name: mGCPause}, {Name: mMutex}, {Name: mCPU}, {Name: mIdle}, {Name: mGCCPU}}
	metrics.Read(s)
	var r runtimeNow
	if s[0].Value.Kind() == metrics.KindFloat64Histogram {
		r.sched = copyHist(s[0].Value.Float64Histogram())
	}
	if s[1].Value.Kind() == metrics.KindFloat64Histogram {
		r.gcPause = copyHist(s[1].Value.Float64Histogram())
	}
	f := func(i int) float64 {
		if s[i].Value.Kind() == metrics.KindFloat64 {
			return s[i].Value.Float64()
		}
		return 0
	}
	r.mutexWait, r.cpuTotal, r.cpuIdle, r.cpuGC = f(2), f(3), f(4), f(5)
	return r
}

func copyHist(h *metrics.Float64Histogram) *metrics.Float64Histogram {
	return &metrics.Float64Histogram{Counts: append([]uint64(nil), h.Counts...), Buckets: h.Buckets}
}

// quantiles returns the upper bounds of the buckets that hold the median,
// the 99th percentile and the largest of what was counted between two
// reads of a histogram, in milliseconds, and how many were counted.
func quantiles(now, prev *metrics.Float64Histogram) (n uint64, p50, p99, max string) {
	if now == nil || prev == nil || len(now.Counts) != len(prev.Counts) {
		return 0, "", "", ""
	}
	diff := make([]uint64, len(now.Counts))
	for i := range diff {
		diff[i] = now.Counts[i] - prev.Counts[i]
		n += diff[i]
	}
	if n == 0 {
		return 0, "0", "0", "0"
	}
	at := func(q float64) string {
		want := uint64(math.Ceil(q * float64(n)))
		var seen uint64
		for i, c := range diff {
			seen += c
			if seen >= want && c > 0 {
				up := now.Buckets[i+1]
				if math.IsInf(up, 1) {
					up = now.Buckets[i]
				}
				return fmt.Sprintf("%.3f", up*1000)
			}
		}
		return ""
	}
	return n, at(0.5), at(0.99), at(1)
}

func (r runtimeNow) since(prev runtimeNow) []any {
	n, p50, p99, max := quantiles(r.sched, prev.sched)
	gn, _, g99, gmax := quantiles(r.gcPause, prev.gcPause)
	return []any{
		// How long goroutines that were ready to run waited for a
		// processor: on one processor, the price of everything else
		// that was ready at the same time.
		"sched_wait_count", n, "sched_wait_p50_ms", p50, "sched_wait_p99_ms", p99, "sched_wait_max_ms", max,
		"gc_pauses", gn, "gc_pause_p99_ms", g99, "gc_pause_max_ms", gmax,
		"mutex_wait_ms", fmt.Sprintf("%.1f", (r.mutexWait-prev.mutexWait)*1000),
		"cpu_s", fmt.Sprintf("%.1f", r.cpuTotal-prev.cpuTotal), "cpu_idle_s", fmt.Sprintf("%.1f", r.cpuIdle-prev.cpuIdle),
		"cpu_gc_s", fmt.Sprintf("%.1f", r.cpuGC-prev.cpuGC),
		"goroutines", runtime.NumGoroutine(), "gomaxprocs", runtime.GOMAXPROCS(0),
	}
}
