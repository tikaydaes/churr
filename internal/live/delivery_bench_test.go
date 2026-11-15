package live

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"runtime"
	"runtime/metrics"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/tikaydaes/churr/internal/model"
)

// TestDeliveryOnOneProcessor measures what delivery over TLS costs a server
// with one processor, and what it leaves for requests: a few hundred TLS
// connections are sent a steady stream of events while one goroutine makes
// round trips over a socket, as a request does to a database. It reports
// frames delivered for each second of processor time, frames to a write,
// how long the round trips took and how long goroutines waited for the
// processor, for sockets the flushers can be offered (Offerable) and for
// sockets written by a goroutine each, which is how TLS connections were
// written before.
//
// It is a measurement, so it runs only when asked:
//
//	CHURR_LIVE_MEASURE=150 go test ./internal/live -run TestDeliveryOnOneProcessor -v
func TestDeliveryOnOneProcessor(t *testing.T) {
	if os.Getenv("CHURR_LIVE_MEASURE") == "" {
		t.Skip("a measurement: set CHURR_LIVE_MEASURE=1")
	}
	trustTestCertificates(t)
	defer runtime.GOMAXPROCS(runtime.GOMAXPROCS(1))
	for _, offerable := range []bool{false, true} {
		name := "a goroutine for each connection"
		if offerable {
			name = "flushers through TLS"
		}
		// Events a second: the variable's value, or 150, which a few
		// hundred connections keep up with; several hundred is more
		// than one processor delivers, and shows what gives then.
		rate, _ := strconv.Atoi(os.Getenv("CHURR_LIVE_MEASURE"))
		if rate < 10 {
			rate = 150
		}
		t.Run(name, func(t *testing.T) { measureDelivery(t, offerable, 300, rate, 3*time.Second) })
	}
}

func cpuTime() time.Duration {
	var ru syscall.Rusage
	syscall.Getrusage(syscall.RUSAGE_SELF, &ru)
	return time.Duration(ru.Utime.Nano() + ru.Stime.Nano())
}

func measureDelivery(t *testing.T, offerable bool, conns, eventsPerSecond int, d time.Duration) {
	was := overTLS
	overTLS = true
	defer func() { overTLS = was }()
	wasOffer := offerableInTests
	offerableInTests = offerable
	defer func() { offerableInTests = wasOffer }()
	modes := make([]websocket.CompressionMode, conns)
	g := newGroup(t, Options{QueueFrames: 1 << 16, QueueBytes: 1 << 28}, modes...)

	// The clients read and count.
	var got atomic.Int64
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	for _, c := range g.conns {
		go func(c *websocket.Conn) {
			for {
				if _, _, err := c.Read(ctx); err != nil {
					return
				}
				got.Add(1)
			}
		}(c)
	}
	// The round trips: an echo over loopback.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		c, err := ln.Accept()
		if err == nil {
			io.Copy(c, c)
		}
	}()
	echo, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer echo.Close()
	var trips []time.Duration
	var tripsMu sync.Mutex
	go func() {
		buf := make([]byte, 8)
		for ctx.Err() == nil {
			start := time.Now()
			if _, err := echo.Write(buf); err != nil {
				return
			}
			if _, err := io.ReadFull(echo, buf); err != nil {
				return
			}
			tripsMu.Lock()
			trips = append(trips, time.Since(start))
			tripsMu.Unlock()
			time.Sleep(2 * time.Millisecond)
		}
	}()

	sched := func() *metrics.Float64Histogram {
		s := []metrics.Sample{{Name: "/sched/latencies:seconds"}}
		metrics.Read(s)
		h := s[0].Value.Float64Histogram()
		return &metrics.Float64Histogram{Counts: append([]uint64(nil), h.Counts...), Buckets: h.Buckets}
	}
	payload := json.RawMessage(fmt.Sprintf(`{"type":"test.sent","text":%q}`, "The deploy went out at nine and the dashboards look fine, so far as anyone can tell."))
	to := []model.AccountID{g.acc.ID}
	writes0, cpu0, sched0, start := pacing.writes.Load(), cpuTime(), sched(), time.Now()
	sent := 0
	tick := time.NewTicker(time.Second / time.Duration(eventsPerSecond))
	for time.Since(start) < d {
		<-tick.C
		g.h.SendAll(to, payload)
		sent++
	}
	tick.Stop()
	want := int64(sent * conns)
	for deadline := time.Now().Add(10 * time.Second); got.Load() < want && time.Now().Before(deadline); {
		time.Sleep(5 * time.Millisecond)
	}
	took, cpu, sched1 := time.Since(start), cpuTime()-cpu0, sched()
	writes := pacing.writes.Load() - writes0
	tripsMu.Lock()
	sort.Slice(trips, func(i, j int) bool { return trips[i] < trips[j] })
	p := func(q float64) time.Duration {
		if len(trips) == 0 {
			return 0
		}
		return trips[int(q*float64(len(trips)-1))]
	}
	trip50, trip99 := p(0.5), p(0.99)
	tripsMu.Unlock()
	var n, seen uint64
	for i := range sched1.Counts {
		n += sched1.Counts[i] - sched0.Counts[i]
	}
	wait99 := 0.0
	for i := range sched1.Counts {
		seen += sched1.Counts[i] - sched0.Counts[i]
		if n > 0 && float64(seen) >= 0.99*float64(n) {
			wait99 = sched1.Buckets[i+1]
			break
		}
	}
	perWrite := "n/a (not counted on this path)"
	if writes > 0 {
		perWrite = fmt.Sprintf("%.1f", float64(got.Load())/float64(writes))
	}
	t.Logf("%d of %d frames to %d connections in %v; processor time %v (%.0f frames a processor-second, both ends of every connection); frames to a write: %s; round trip p50 %v p99 %v; goroutines made ready %d, waited p99 %.2f ms",
		got.Load(), want, conns, took.Round(time.Millisecond), cpu.Round(time.Millisecond), float64(got.Load())/cpu.Seconds(),
		perWrite, trip50.Round(10*time.Microsecond), trip99.Round(10*time.Microsecond), n, wait99*1000)
	if got.Load() < want {
		t.Errorf("only %d of %d frames arrived", got.Load(), want)
	}
}
