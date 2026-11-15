package main

import (
	"encoding/json"
	"fmt"
	"math/bits"
	"sync/atomic"
	"time"
)

// A histogram of durations that several generators can each fill and that
// can then be added together, so the percentiles of a whole run are worked
// out from every sample and not averaged from each generator's own.
//
// Values are counted in microseconds. Up to 64 µs every value has its own
// bucket; above that each power of two is cut into 64, so a bucket is never
// wider than 1.6% of what it holds. The largest value it tells apart is over
// an hour; anything longer lands in the last bucket. Adding a sample is a
// few atomic additions and no allocation, because seventeen thousand
// connections do it a hundred thousand times a second.

const (
	histSub     = 64 // buckets to a power of two
	histBuckets = 27 * histSub
)

type hist struct {
	n, sum   atomic.Int64 // samples, and their total in µs
	min, max atomic.Int64 // in µs; min is kept one too high so that zero means "none yet"
	b        [histBuckets]atomic.Int64
}

// bucketOf is the bucket a value in µs is counted in.
func bucketOf(us int64) int {
	if us < histSub {
		if us < 0 {
			return 0
		}
		return int(us)
	}
	e := bits.Len64(uint64(us)) - 1 // 6 or more
	i := (e-6)*histSub + int(us>>(e-6))
	if i >= histBuckets {
		return histBuckets - 1
	}
	return i
}

// bucketTop is the largest value in µs that bucket i holds.
func bucketTop(i int) int64 {
	if i < histSub {
		return int64(i)
	}
	e := i/histSub + 5
	sub := int64(i%histSub + histSub)
	return sub<<(e-6) + 1<<(e-6) - 1
}

func (h *hist) add(d time.Duration) {
	us := int64(d / time.Microsecond)
	if us < 0 {
		us = 0
	}
	h.b[bucketOf(us)].Add(1)
	h.n.Add(1)
	h.sum.Add(us)
	for {
		cur := h.max.Load()
		if us <= cur || h.max.CompareAndSwap(cur, us) {
			break
		}
	}
	for {
		cur := h.min.Load()
		if (cur != 0 && us+1 >= cur) || h.min.CompareAndSwap(cur, us+1) {
			break
		}
	}
}

// merge adds another histogram's samples to this one.
func (h *hist) merge(o *hist) {
	if o.n.Load() == 0 {
		return
	}
	for i := range o.b {
		if c := o.b[i].Load(); c != 0 {
			h.b[i].Add(c)
		}
	}
	h.n.Add(o.n.Load())
	h.sum.Add(o.sum.Load())
	if m := o.max.Load(); m > h.max.Load() {
		h.max.Store(m)
	}
	if m := o.min.Load(); m != 0 && (h.min.Load() == 0 || m < h.min.Load()) {
		h.min.Store(m)
	}
}

func (h *hist) count() int64 { return h.n.Load() }

// quantile is the value that the share p of the samples do not exceed: the
// top of the bucket that sample falls in, and never more than the largest
// value seen.
func (h *hist) quantile(p float64) time.Duration {
	n := h.n.Load()
	if n == 0 {
		return 0
	}
	rank := int64(p * float64(n)) // the sample at this place, counting from 0
	if rank >= n {
		rank = n - 1
	}
	var seen int64
	for i := range h.b {
		seen += h.b[i].Load()
		if seen > rank {
			return time.Duration(min(bucketTop(i), h.max.Load())) * time.Microsecond
		}
	}
	return time.Duration(h.max.Load()) * time.Microsecond
}

func (h *hist) report(name string) {
	n := h.n.Load()
	if n == 0 {
		fmt.Printf("%-28s no samples\n", name)
		return
	}
	r := func(d time.Duration) string { return d.Round(100 * time.Microsecond).String() }
	fmt.Printf("%-28s n=%d  p50=%s  p95=%s  p99=%s  max=%s\n", name, n,
		r(h.quantile(0.50)), r(h.quantile(0.95)), r(h.quantile(0.99)), r(time.Duration(h.max.Load())*time.Microsecond))
}

// histJSON is a histogram in a results file: the counts of the buckets that
// have any, as [bucket, count] pairs.
type histJSON struct {
	Count   int64      `json:"count"`
	SumUS   int64      `json:"sum_us"`
	MinUS   int64      `json:"min_us"`
	MaxUS   int64      `json:"max_us"`
	Sub     int        `json:"buckets_per_power_of_two"`
	Buckets [][2]int64 `json:"buckets"`
}

func (h *hist) MarshalJSON() ([]byte, error) {
	out := histJSON{Count: h.n.Load(), SumUS: h.sum.Load(), MaxUS: h.max.Load(), Sub: histSub, Buckets: [][2]int64{}}
	if m := h.min.Load(); m > 0 {
		out.MinUS = m - 1
	}
	for i := range h.b {
		if c := h.b[i].Load(); c != 0 {
			out.Buckets = append(out.Buckets, [2]int64{int64(i), c})
		}
	}
	return json.Marshal(out)
}

func (h *hist) UnmarshalJSON(data []byte) error {
	var in histJSON
	if err := json.Unmarshal(data, &in); err != nil {
		return err
	}
	if in.Sub != histSub && in.Count != 0 {
		return fmt.Errorf("a histogram with %d buckets to a power of two, where this program uses %d", in.Sub, histSub)
	}
	var total int64
	for _, bc := range in.Buckets {
		if bc[0] < 0 || bc[0] >= histBuckets || bc[1] < 0 {
			return fmt.Errorf("a histogram bucket out of range: %v", bc)
		}
		h.b[bc[0]].Store(bc[1])
		total += bc[1]
	}
	if total != in.Count {
		return fmt.Errorf("a histogram that says %d samples and holds %d", in.Count, total)
	}
	h.n.Store(in.Count)
	h.sum.Store(in.SumUS)
	h.max.Store(in.MaxUS)
	if in.Count > 0 {
		h.min.Store(in.MinUS + 1)
	}
	return nil
}
