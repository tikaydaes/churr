package relay

import (
	"sort"
	"sync"
	"time"
)

// The relay's counters (docs/PUSH.md section 7): requests received, and
// results by platform and outcome, since start and for the current hour.
// Nothing else is counted, and there is no way to read them from outside:
// they go to the log, one line an hour.

type counters struct {
	mu    sync.Mutex
	start time.Time
	// hour is the hour the hourly numbers are for.
	hour       time.Time
	total      map[string]int64
	thisHour   map[string]int64
	requests   int64
	requestsHr int64
}

// roll starts a new hour's numbers when the hour has changed.
func (c *counters) roll() {
	now := time.Now().Truncate(time.Hour)
	if c.total == nil {
		c.total, c.thisHour, c.hour = map[string]int64{}, map[string]int64{}, now
	}
	if !now.Equal(c.hour) {
		c.hour, c.thisHour, c.requestsHr = now, map[string]int64{}, 0
	}
}

func (c *counters) received() {
	c.mu.Lock()
	c.roll()
	c.requests++
	c.requestsHr++
	c.mu.Unlock()
}

// result counts one outcome: sent, gone, rate_limited, invalid or
// upstream_error. platform is "" when the request named none.
func (c *counters) result(platform, outcome string) {
	if platform == "" {
		platform = "none"
	}
	c.mu.Lock()
	c.roll()
	c.total[platform+"."+outcome]++
	c.thisHour[platform+"."+outcome]++
	c.mu.Unlock()
}

// Counts is a copy of the counters.
type Counts struct {
	Requests, RequestsThisHour int64
	// Results and ResultsThisHour are keyed "<platform>.<outcome>".
	Results, ResultsThisHour map[string]int64
}

// Counts returns the counters as they stand, for the hourly log line and
// for tests.
func (r *Relay) Counts() Counts {
	c := &r.counters
	c.mu.Lock()
	defer c.mu.Unlock()
	c.roll()
	out := Counts{Requests: c.requests, RequestsThisHour: c.requestsHr, Results: map[string]int64{}, ResultsThisHour: map[string]int64{}}
	for k, v := range c.total {
		out.Results[k] = v
	}
	for k, v := range c.thisHour {
		out.ResultsThisHour[k] = v
	}
	return out
}

// LogCounters writes the one line of counters. It names no device, no
// event and no caller.
func (r *Relay) LogCounters() {
	c := r.Counts()
	args := []any{"requests", c.Requests, "requests_this_hour", c.RequestsThisHour}
	keys := make([]string, 0, len(c.Results))
	for k := range c.Results {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		args = append(args, k, c.Results[k])
	}
	r.log.Info("counters", args...)
}
