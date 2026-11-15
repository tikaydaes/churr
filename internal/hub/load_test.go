package hub_test

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/link/wire"
	"github.com/tikaydaes/churr/internal/testkit"
)

// TestLoad measures one hub alone and three hubs in a line under a steady
// rate of messages from simulated servers, stepping the rate up until it
// bends. It runs only when CHURR_HUB_LOAD is set:
//
//	CHURR_HUB_LOAD=1 go test -count=1 -run TestLoad -timeout 30m -v ./internal/hub/
//
// Everything runs in one process on one machine: the hubs, their SQLite
// databases, and the simulated servers that sign every envelope they send.
// The figures are what this machine did and say nothing about another.
func TestLoad(t *testing.T) {
	if os.Getenv("CHURR_HUB_LOAD") == "" {
		t.Skip("set CHURR_HUB_LOAD=1 to run the load measurement")
	}
	for _, hubs := range []int{1, 3} {
		t.Run(fmt.Sprintf("%d-hubs", hubs), func(t *testing.T) { runLoad(t, hubs) })
	}
}

const (
	loadServers    = 30
	loadPerChannel = 5 // so every envelope is delivered to four servers
	loadStep       = 5 * time.Second
	loadPayload    = 200 // bytes of text in each message
)

func runLoad(t *testing.T, hubs int) {
	c := testkit.NewCluster(t, "line", hubs)
	for _, h := range c.Hubs {
		if err := h.Hub.SetLimits("", 100000000, 0, 0); err != nil {
			t.Fatal(err)
		}
		h.Hub.Reconcile()
	}
	var sims []*sim
	for i := 0; i < loadServers; i++ {
		sims = append(sims, newSim(t, c.Hubs[i%hubs], fmt.Sprintf("s%02d.sim", i)))
	}
	channelOf := map[*sim]string{}
	for j := 0; j < loadServers/loadPerChannel; j++ {
		group := sims[j*loadPerChannel : (j+1)*loadPerChannel]
		id := shared(t, fmt.Sprintf("load-%d", j), group...)
		for _, s := range group {
			channelOf[s] = id
		}
	}
	text := strings.Repeat("x", loadPayload)

	// When each envelope was meant to leave, by its event id, and how long
	// each delivery of it took from then.
	var born sync.Map
	var mu sync.Mutex
	var latencies []time.Duration
	var delivered atomic.Int64
	for _, s := range sims {
		s.mu.Lock()
		s.light = true
		s.got = map[string][]wire.Envelope{}
		s.onDeliver = func(env wire.Envelope) {
			if v, ok := born.Load(env.OriginEventID); ok {
				d := time.Since(v.(time.Time))
				mu.Lock()
				latencies = append(latencies, d)
				mu.Unlock()
				delivered.Add(1)
			}
		}
		s.mu.Unlock()
	}
	heap := func() (inUse, sys float64) {
		runtime.GC()
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		return float64(m.HeapAlloc) / (1 << 20), float64(m.Sys) / (1 << 20)
	}
	disk := func() float64 {
		var n int64
		for _, h := range c.Hubs {
			filepath.Walk(h.Dir, func(_ string, info os.FileInfo, err error) error {
				if err == nil && !info.IsDir() {
					n += info.Size()
				}
				return nil
			})
		}
		return float64(n) / (1 << 20)
	}
	idleHeap, idleSys := heap()
	t.Logf("%d hub(s), %d simulated servers, %d channels of %d servers, %d-byte messages; idle: heap %.0f MiB, from the system %.0f MiB",
		hubs, loadServers, loadServers/loadPerChannel, loadPerChannel, loadPayload, idleHeap, idleSys)
	t.Logf("%8s %10s %11s %9s %9s %9s %9s %8s %9s %8s", "offered", "accepted", "delivered", "p50 ms", "p95 ms", "p99 ms", "max ms", "refused", "heap MiB", "log MiB")

	bent := ""
	for _, rate := range []int{250, 500, 1000, 2000, 3000, 4000, 6000, 8000, 12000} {
		mu.Lock()
		latencies = latencies[:0]
		mu.Unlock()
		delivered.Store(0)
		var accepted, refused, made atomic.Int64
		var wg sync.WaitGroup
		start := time.Now()
		stop := start.Add(loadStep)
		perServer := float64(rate) / loadServers
		for _, s := range sims {
			wg.Add(1)
			go func(s *sim) {
				defer wg.Done()
				id := channelOf[s]
				queue := make(chan wire.Envelope, 100000)
				sent := make(chan struct{})
				// The sender: as a server does, one submit at a time for a
				// stream, everything that has queued up in the meantime
				// in the next frame.
				go func() {
					defer close(sent)
					for env := range queue {
						batch := []wire.Envelope{env}
					more:
						for len(batch) < 64 {
							select {
							case e, ok := <-queue:
								if !ok {
									break more
								}
								batch = append(batch, e)
							default:
								break more
							}
						}
						r := s.submit(batch...)
						accepted.Add(int64(len(r.IDs)))
						refused.Add(int64(len(batch) - len(r.IDs)))
					}
				}()
				// The generator: a steady rate, whatever the sender manages.
				produced := 0
				tick := time.NewTicker(5 * time.Millisecond)
				defer tick.Stop()
				for now := range tick.C {
					if now.After(stop) {
						break
					}
					due := int(now.Sub(start).Seconds() * perServer)
					for ; produced < due; produced++ {
						env := s.seal(id, 0, text)
						born.Store(env.OriginEventID, time.Now())
						made.Add(1)
						queue <- env
					}
				}
				close(queue)
				<-sent
			}(s)
		}
		wg.Wait()
		took := time.Since(start)
		// What is still on its way is given a moment.
		want := accepted.Load() * (loadPerChannel - 1)
		for wait := time.Now().Add(5 * time.Second); delivered.Load() < want && time.Now().Before(wait); {
			time.Sleep(20 * time.Millisecond)
		}
		mu.Lock()
		sorted := append([]time.Duration(nil), latencies...)
		mu.Unlock()
		sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
		pct := func(p float64) float64 {
			if len(sorted) == 0 {
				return 0
			}
			return float64(sorted[int(float64(len(sorted)-1)*p)]) / float64(time.Millisecond)
		}
		inUse, _ := heap()
		acceptedRate := float64(accepted.Load()) / took.Seconds()
		t.Logf("%8d %10.0f %11.0f %9.1f %9.1f %9.1f %9.1f %8d %9.0f %8.0f", rate, acceptedRate, float64(delivered.Load())/took.Seconds(),
			pct(0.50), pct(0.95), pct(0.99), pct(1), refused.Load(), inUse, disk())
		born.Range(func(k, _ any) bool { born.Delete(k); return true })
		if bent == "" && (acceptedRate < 0.9*float64(rate) || pct(0.99) > 1000 || delivered.Load() < want) {
			bent = fmt.Sprintf("it bent at %d a second offered: %.0f a second were taken, the slowest one in a hundred deliveries took %.0f ms, %d of %d deliveries arrived",
				rate, acceptedRate, pct(0.99), delivered.Load(), want)
		}
		if acceptedRate < 0.6*float64(rate) {
			break // well past the bend; more offered load only queues
		}
		time.Sleep(time.Second)
	}
	if bent == "" {
		bent = "it did not bend within the rates tried"
	}
	t.Log(bent)
	for i, h := range c.Hubs {
		if i+1 < len(c.Hubs) {
			s := h.Hub.TrunkStats(testkit.HubName(i + 1))
			t.Logf("trunk %d-%d: %d envelopes in %d batches (largest %d), most unanswered %d, waits for room %d, refused there %d",
				i, i+1, s.Envelopes, s.Batches, s.MaxBatch, s.MaxInflight, s.Waits, s.RefusedThere)
		}
	}
}

// TestLoadPresenceStorm measures one hub carrying a steady rate of messages
// while every server on it sends presence as fast as the hub allows, as
// they would if all of them reconnected at once and sent their snapshots
// without spreading them (ruling R62 has servers spread them; this is the
// worst case). It reports what became of the ephemeral frames and whether
// the messages noticed. It runs only when CHURR_HUB_LOAD is set:
//
//	CHURR_HUB_LOAD=1 go test -count=1 -run TestLoadPresenceStorm -timeout 30m -v ./internal/hub/
func TestLoadPresenceStorm(t *testing.T) {
	if os.Getenv("CHURR_HUB_LOAD") == "" {
		t.Skip("set CHURR_HUB_LOAD=1 to run the load measurement")
	}
	const (
		rate     = 1000 // messages a second, over all servers
		perStorm = 1200 // ephemeral frames from each server: its allowance for a minute, at once
	)
	c := testkit.NewCluster(t, "line", 1)
	h := c.Hubs[0].Hub
	if err := h.SetLimits("", 100000000, 0, 0); err != nil {
		t.Fatal(err)
	}
	h.Reconcile()
	var sims []*sim
	for i := 0; i < loadServers; i++ {
		sims = append(sims, newSim(t, c.Hubs[0], fmt.Sprintf("s%02d.sim", i)))
	}
	channelOf := map[*sim]string{}
	for j := 0; j < loadServers/loadPerChannel; j++ {
		group := sims[j*loadPerChannel : (j+1)*loadPerChannel]
		id := shared(t, fmt.Sprintf("storm-%d", j), group...)
		for _, s := range group {
			channelOf[s] = id
		}
	}
	text := strings.Repeat("x", loadPayload)
	var born sync.Map
	var mu sync.Mutex
	var latencies []time.Duration
	var delivered atomic.Int64
	for _, s := range sims {
		s.mu.Lock()
		s.light = true
		s.got = map[string][]wire.Envelope{}
		s.onDeliver = func(env wire.Envelope) {
			if v, ok := born.Load(env.OriginEventID); ok {
				d := time.Since(v.(time.Time))
				mu.Lock()
				latencies = append(latencies, d)
				mu.Unlock()
				delivered.Add(1)
			}
		}
		s.mu.Unlock()
	}
	told := func() (n int) {
		for _, s := range sims {
			n += s.told(channelOf[s])
		}
		return n
	}
	refusals := func() (n int) {
		for _, s := range sims {
			s.mu.Lock()
			for _, e := range s.errs {
				if e.Code == wire.ERateLimited {
					n++
				}
			}
			s.mu.Unlock()
		}
		return n
	}
	t.Logf("1 hub, %d simulated servers, %d channels of %d servers, %d messages a second of %d bytes for %s; in the storm each server also sends %d ephemeral frames at once",
		loadServers, loadServers/loadPerChannel, loadPerChannel, rate, loadPayload, loadStep, perStorm)
	t.Logf("%-10s %9s %10s %8s %8s %8s %8s | %9s %8s %9s %8s %8s %9s", "", "accepted", "delivered", "p50 ms", "p95 ms", "p99 ms", "max ms",
		"eph sent", "shed", "passed", "dropped", "refused", "received")

	for _, phase := range []string{"calm", "storm", "calm again"} {
		mu.Lock()
		latencies = latencies[:0]
		mu.Unlock()
		delivered.Store(0)
		before, toldBefore, refusedBefore := h.EphemeralStats(), told(), refusals()
		var accepted, sentEph atomic.Int64
		var wg sync.WaitGroup
		start := time.Now()
		stop := start.Add(loadStep)
		perServer := float64(rate) / loadServers
		for _, s := range sims {
			if phase == "storm" {
				wg.Add(1)
				go func(s *sim) {
					defer wg.Done()
					for i := 0; i < perStorm; i++ {
						s.tell(channelOf[s])
						sentEph.Add(1)
					}
				}(s)
			}
			wg.Add(1)
			go func(s *sim) {
				defer wg.Done()
				id := channelOf[s]
				produced := 0
				tick := time.NewTicker(5 * time.Millisecond)
				defer tick.Stop()
				for now := range tick.C {
					if now.After(stop) {
						break
					}
					due := int(now.Sub(start).Seconds() * perServer)
					var batch []wire.Envelope
					for ; produced < due && len(batch) < 64; produced++ {
						env := s.seal(id, 0, text)
						born.Store(env.OriginEventID, time.Now())
						batch = append(batch, env)
					}
					if len(batch) > 0 {
						accepted.Add(int64(len(s.submit(batch...).IDs)))
					}
				}
			}(s)
		}
		wg.Wait()
		took := time.Since(start)
		want := accepted.Load() * (loadPerChannel - 1)
		for wait := time.Now().Add(10 * time.Second); delivered.Load() < want && time.Now().Before(wait); {
			time.Sleep(20 * time.Millisecond)
		}
		// Every ephemeral frame has been passed on, dropped or refused.
		for wait := time.Now().Add(10 * time.Second); time.Now().Before(wait); {
			st := h.EphemeralStats()
			if st.Passed-before.Passed == int64(told()-toldBefore) &&
				st.Passed+st.Dropped-before.Passed-before.Dropped >= (sentEph.Load()-(st.Shed-before.Shed)-int64(refusals()-refusedBefore))*(loadPerChannel-1) {
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
		mu.Lock()
		sorted := append([]time.Duration(nil), latencies...)
		mu.Unlock()
		sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
		pct := func(p float64) float64 {
			if len(sorted) == 0 {
				return 0
			}
			return float64(sorted[int(float64(len(sorted)-1)*p)]) / float64(time.Millisecond)
		}
		st := h.EphemeralStats()
		t.Logf("%-10s %9.0f %10.0f %8.1f %8.1f %8.1f %8.1f | %9d %8d %9d %8d %8d %9d", phase, float64(accepted.Load())/took.Seconds(), float64(delivered.Load())/took.Seconds(),
			pct(0.50), pct(0.95), pct(0.99), pct(1), sentEph.Load(), st.Shed-before.Shed, st.Passed-before.Passed, st.Dropped-before.Dropped, refusals()-refusedBefore, told()-toldBefore)
		if delivered.Load() != want {
			t.Errorf("%s: %d of %d message deliveries arrived", phase, delivered.Load(), want)
		}
		born.Range(func(k, _ any) bool { born.Delete(k); return true })
		time.Sleep(time.Second)
	}
}
