package calls_test

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"sort"
	"strconv"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/calls"
	"github.com/tikaydaes/churr/internal/calls/calltest"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/secret"
	"github.com/tikaydaes/churr/internal/testkit"
)

// BenchmarkCallCapacity pushes one call toward the target in BRIEF.md: 25
// people on audio and 8 of them on video, everyone showing all eight
// cameras at layer h, with media as heavy as docs/CALLS.md sections 6 and 7
// say it is. Server and clients run in this one process on the loopback
// address, so the CPU and memory it reports are for both together and say
// nothing about a network. Run it with:
//
//	go test ./internal/calls/ -run '^$' -bench CallCapacity -benchtime 1x
//
// CHURR_CALL_PEOPLE, CHURR_CALL_CAMERAS and CHURR_CALL_SECONDS change the
// size of the call and how long the steady stretch is measured for.
func BenchmarkCallCapacity(b *testing.B) {
	people, cameras, seconds := envInt("CHURR_CALL_PEOPLE", 25), envInt("CHURR_CALL_CAMERAS", 8), envInt("CHURR_CALL_SECONDS", 10)
	for i := 0; i < b.N; i++ {
		capacity(b, people, cameras, time.Duration(seconds)*time.Second)
	}
}

// scripted makes a member and a personal token for it straight in the
// store. Registering through the API is limited per address, rightly, and a
// load test needs more people than that allows from one address.
func scripted(t testing.TB, ts *testkit.TestServer, name string) *testkit.Client {
	t.Helper()
	ctx := context.Background()
	acc := model.Account{Username: name, DisplayName: name, Role: model.RoleMember}
	if _, err := ts.Server.Store.Accounts().Create(ctx, &acc); err != nil {
		t.Fatal(err)
	}
	token, hash := secret.NewToken("churr_pat_")
	ses := model.Session{Kind: "personal", Label: "load test", AccountID: acc.ID}
	if err := ts.Server.Store.Sessions().Create(ctx, &ses, hash, nil); err != nil {
		t.Fatal(err)
	}
	c := ts.Browser()
	c.Bearer, c.ID, c.Username = token, acc.ID, name
	return c
}

func envInt(name string, def int) int {
	if n, err := strconv.Atoi(os.Getenv(name)); err == nil && n > 0 {
		return n
	}
	return def
}

func cpuTime() time.Duration {
	var ru syscall.Rusage
	syscall.Getrusage(syscall.RUSAGE_SELF, &ru)
	return time.Duration(ru.Utime.Nano() + ru.Stime.Nano())
}

func capacity(b *testing.B, people, cameras int, steady time.Duration) {
	ts := testkit.NewServer(b, testkit.Options{CallTimings: calls.Timings{}})
	accounts := make([]*testkit.Client, people)
	for i := range accounts {
		accounts[i] = scripted(b, ts, fmt.Sprintf("p%02d", i))
	}
	ch := accounts[0].Channel("all-hands", false, accounts[1:]...)
	clients := make([]*calltest.Client, people)
	joinTimes := make([]time.Duration, people)

	// Everyone joins, a few at a time, as people arriving at a meeting do.
	b.ResetTimer()
	var wg sync.WaitGroup
	gate := make(chan struct{}, 5)
	failed := make(chan string, people)
	for i := range accounts {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			gate <- struct{}{}
			defer func() { <-gate }()
			stream, _ := accounts[i].Events(nil)
			c := calltest.New(b, stream, calltest.Options{Simulcast: true, Realistic: true, Wait: 30 * time.Second})
			clients[i] = c
			at := time.Now()
			status, out := accounts[i].Do("POST", "/api/v1/conversations/"+ch+"/call/join", nil)
			if status != 200 {
				failed <- fmt.Sprintf("joinCall = %d %v", status, out)
				return
			}
			call := out["call"].(map[string]any)
			if f := c.Join(call["id"].(string), out["ticket"].(string)); f["type"] != "call.ok" {
				failed <- fmt.Sprintf("call.join: %v", f)
				return
			}
			sources := []string{"microphone"}
			if i < cameras {
				sources = append(sources, "camera")
			}
			if f := c.Publish(sources...); f["type"] != "call.answer" {
				failed <- fmt.Sprintf("call.offer: %v", f)
				return
			}
			c.SetLevel(30) // everyone talks at once: the worst case for audio
			// Joined, for a person, is when their own media is connected.
			for c.SendState().String() != "connected" && time.Since(at) < 30*time.Second {
				time.Sleep(5 * time.Millisecond)
			}
			joinTimes[i] = time.Since(at)
		}(i)
	}
	wg.Wait()
	select {
	case msg := <-failed:
		b.Fatal(msg)
	default:
	}
	defer func() {
		for _, c := range clients {
			c.Close()
		}
	}()
	// Everyone shows all the cameras in a grid: 640x360 tiles, layer h.
	var cams [][3]any
	for i := 0; i < cameras; i++ {
		cams = append(cams, [3]any{clients[i].TrackIDs["camera"], 640, 360})
	}
	for i, c := range clients {
		var mine [][3]any
		for j, cam := range cams {
			if j != i {
				mine = append(mine, cam)
			}
		}
		c.Subscribe(mine...)
	}
	// Let the layers settle, then measure a steady stretch.
	time.Sleep(4 * time.Second)
	id := callID(clients[0])
	type streamOf struct {
		c     *calltest.Client
		track string
		video bool
	}
	var streams []streamOf
	for i, c := range clients {
		for j, other := range clients {
			if i == j {
				continue
			}
			streams = append(streams, streamOf{c, other.TrackIDs["microphone"], false})
			if j < cameras {
				streams = append(streams, streamOf{c, other.TrackIDs["camera"], true})
			}
		}
	}
	count := func() (packets, bytes int64) {
		for _, s := range streams {
			st := s.c.Stream(s.track)
			packets += st.Packets.Load()
			bytes += st.Bytes.Load()
		}
		return
	}
	for _, s := range streams {
		s.c.Stream(s.track).ResetStats()
	}
	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	p0, b0 := count()
	f0 := ts.Server.Calls.Forwarded(id)
	cpu0, at := cpuTime(), time.Now()
	time.Sleep(steady)
	wall := time.Since(at)
	cpu := cpuTime() - cpu0
	p1, b1 := count()
	f1 := ts.Server.Calls.Forwarded(id)
	var after runtime.MemStats
	runtime.ReadMemStats(&after)

	var lost int64
	var worst, sum time.Duration
	var delays int64
	for _, s := range streams {
		st := s.c.Stream(s.track)
		lost += st.Lost()
		mean, max := st.Delay()
		if max > worst {
			worst = max
		}
		if mean > 0 {
			sum += mean
			delays++
		}
	}
	// What each video tile was sent over the last two seconds.
	tiles := func(of *calltest.Client, not bool) map[string]int {
		type key struct {
			c     *calltest.Client
			track string
		}
		first := map[key]map[byte]int{}
		for _, s := range streams {
			if s.video && (s.c == of) != not {
				first[key{s.c, s.track}] = s.c.Stream(s.track).Layers()
			}
		}
		time.Sleep(2 * time.Second)
		out := map[string]int{}
		for k, was := range first {
			now, got := k.c.Stream(k.track).Layers(), "nothing"
			for _, mark := range []byte{'q', 'h', 'f'} {
				if now[mark] > was[mark] {
					got = string(mark)
				}
			}
			out[got]++
		}
		return out
	}
	all := tiles(nil, true)
	sort.Slice(joinTimes, func(i, j int) bool { return joinTimes[i] < joinTimes[j] })
	received := p1 - p0
	secs := wall.Seconds()
	b.StopTimer()
	b.ReportMetric(float64(f1-f0)/secs, "forwarded-pkt/s")
	b.ReportMetric(float64(b1-b0)*8/secs/1e6, "received-Mbit/s")
	b.ReportMetric(100*float64(lost)/float64(received+lost), "loss-%")
	b.ReportMetric(float64(sum.Microseconds())/float64(max(delays, 1))/1000, "mean-delay-ms")
	b.ReportMetric(float64(worst.Microseconds())/1000, "worst-delay-ms")
	b.ReportMetric(100*cpu.Seconds()/secs, "cpu-%-of-one-core")
	b.ReportMetric(float64(after.HeapInuse)/1e6, "heap-MB")
	b.ReportMetric(float64(after.Sys)/1e6, "from-system-MB")
	b.ReportMetric(float64(joinTimes[len(joinTimes)/2].Milliseconds()), "median-join-ms")
	b.ReportMetric(float64(joinTimes[len(joinTimes)-1].Milliseconds()), "slowest-join-ms")
	held := 0
	for i := range accounts {
		if ts.Server.Calls.Limit(id, accounts[i].ID) != 0 {
			held++
		}
	}
	b.Logf("%d people, %d cameras, %v steady: of the video tiles being shown, by the layer each was sent: %v; %d of %d downlinks held back for loss; %d goroutines; %d logical CPUs",
		people, cameras, wall.Round(time.Millisecond), all, held, people, runtime.NumGoroutine(), runtime.NumCPU())

	// One subscriber's connection turns poor: 1.5 Mbit/s. Its own tiles
	// are lowered; nobody else's are touched.
	poor := clients[people-1]
	ts.Server.Calls.SetEstimate(id, accounts[people-1].ID, 1_500_000)
	time.Sleep(3 * time.Second)
	b.Logf("one subscriber held to 1.5 Mbit/s: its tiles, by the layer each was sent: %v; the other %d subscribers' tiles: %v",
		tiles(poor, false), people-1, tiles(poor, true))
}
