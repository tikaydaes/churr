package calls_test

import (
	"fmt"
	"runtime"
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/calls/calltest"
	"github.com/tikaydaes/churr/internal/testkit"
)

// A subscriber that says it lost packets is sent them again by the server,
// unchanged, after a change of layer as before it; audio is not kept.
func TestLostPacketsAreSentAgain(t *testing.T) {
	ts := server(t, fast())
	ann, bob := ts.Member("ann"), ts.Member("bob")
	ch := ann.Channel("standup", false, bob)
	a := enter(t, ann, ch, calltest.Options{Simulcast: true})
	b := enter(t, bob, ch, calltest.Options{})
	a.Publish("microphone", "camera")
	b.Publish("microphone")
	cam, mic := a.TrackIDs["camera"], a.TrackIDs["microphone"]
	b.Subscribe([3]any{cam, 320, 180})
	s := b.Stream(cam)
	eventually(t, "bob sees ann at q", func() bool { return s.Layers()['q'] > 10 })
	b.Subscribe([3]any{cam, 1280, 720})
	eventually(t, "bob sees ann at f", func() bool { return s.Last() == 'f' && s.Layers()['f'] > 20 })

	id := callID(a)
	seq := s.Seq()
	b.Nack(cam, seq-1, seq-3, seq-6)
	eventually(t, "three packets arrive again", func() bool { again, _ := s.Resent(); return again == 3 })
	if _, wrong := s.Resent(); wrong != 0 {
		t.Fatalf("%d packets sent again were not what was sent the first time", wrong)
	}
	if got := ts.Server.Calls.Resent(id); got != 3 {
		t.Fatalf("the server sent %d packets again, want 3", got)
	}
	// Audio is not asked for again, and a request for it is not answered.
	eventually(t, "bob hears ann", func() bool { return b.Stream(mic).Packets.Load() > 10 })
	b.Nack(mic, b.Stream(mic).Seq()-1)
	// A packet from long before what is kept is not sent either.
	b.Nack(cam, s.Seq()-5000)
	time.Sleep(300 * time.Millisecond)
	if got := ts.Server.Calls.Resent(id); got != 3 {
		t.Fatalf("the server sent %d packets again, want still 3", got)
	}
	if again, _ := b.Stream(mic).Resent(); again != 0 {
		t.Fatal("an audio packet was sent again")
	}
}

// What a call holds is let go: a person who leaves takes their tracks,
// what they were being forwarded and the packets kept for them out of the
// room, and when everyone has left the server is back where it began, in
// goroutines and in memory.
func TestACallLeavesNothingBehind(t *testing.T) {
	ts := server(t, fast())
	const people, cameras = 6, 3
	accounts := make([]*testkit.Client, people)
	for i := range accounts {
		accounts[i] = scripted(t, ts, fmt.Sprintf("p%02d", i))
	}

	held := func() (uint64, int) {
		runtime.GC()
		runtime.GC()
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		return m.HeapAlloc, runtime.NumGoroutine()
	}
	calls := 0
	hold := func() {
		// A channel of its own for each call: the one before may still
		// be ending in the last.
		calls++
		ch := accounts[0].Channel(fmt.Sprintf("all-hands-%d", calls), false, accounts[1:]...)
		var in []*calltest.Client
		var streams []*testkit.Stream
		for i, acc := range accounts {
			stream, _ := acc.Events(nil)
			streams = append(streams, stream)
			c := calltest.New(t, stream, calltest.Options{Simulcast: true, Realistic: true})
			callID, tk := ticket(t, acc, ch)
			c.MustJoin(callID, tk)
			sources := []string{"microphone"}
			if i < cameras {
				sources = append(sources, "camera")
			}
			c.Publish(sources...)
			c.SetLevel(30)
			in = append(in, c)
		}
		var cams [][3]any
		for i := 0; i < cameras; i++ {
			cams = append(cams, [3]any{in[i].TrackIDs["camera"], 640, 360})
		}
		for i, c := range in {
			var mine [][3]any
			for j, cam := range cams {
				if j != i {
					mine = append(mine, cam)
				}
			}
			c.Subscribe(mine...)
		}
		last := in[people-1]
		eventually(t, "the last person sees every camera and hears everyone", func() bool {
			for i, c := range in[:people-1] {
				if last.Stream(c.TrackIDs["microphone"]).Packets.Load() < 20 || i < cameras && last.Stream(c.TrackIDs["camera"]).Packets.Load() < 50 {
					return false
				}
			}
			return true
		})
		call := callID(in[0])
		// Everything the room holds, with everyone in: a microphone
		// each and the cameras; every microphone forwarded to everyone
		// else and every camera to everyone but its owner.
		wantTracks, wantForwardings := people+cameras, people*(people-1)+cameras*(people-1)
		eventually(t, "the room holds every track and forwarding", func() bool {
			tracks, forwardings, kept := ts.Server.Calls.Holding(call)
			return tracks == wantTracks && forwardings == wantForwardings && kept > 0
		})
		// The first person, who has a camera, leaves: their two tracks,
		// the forwardings of those, and the forwardings to them, go.
		in[0].Leave()
		in[0].Close()
		streams[0].Close()
		wantTracks -= 2
		wantForwardings -= 2*(people-1) + (people - 1) + (cameras - 1)
		eventually(t, "the room lets go of the one who left", func() bool {
			tracks, forwardings, _ := ts.Server.Calls.Holding(call)
			return tracks == wantTracks && forwardings == wantForwardings
		})
		// A layer keeps at most its count of packets, none longer than a
		// packet: two cameras of three layers are under a megabyte.
		if _, _, kept := ts.Server.Calls.Holding(call); kept > (64+128+256)*1500*(cameras-1) {
			t.Fatalf("%d bytes of packets are kept for %d cameras", kept, cameras-1)
		}
		for i, c := range in[1:] {
			c.Leave()
			c.Close()
			streams[i+1].Close()
		}
		eventually(t, "the call is over", func() bool {
			tracks, forwardings, kept := ts.Server.Calls.Holding(call)
			return tracks == 0 && forwardings == 0 && kept == 0
		})
	}
	// One call first, so that what is made once and kept (pools, caches,
	// the database's connections) is not taken for a leak.
	hold()
	var heap0 uint64
	var routines0 int
	eventually(t, "the first call is let go", func() bool {
		h, g := held()
		if routines0 == 0 || g < routines0 {
			heap0, routines0 = h, g
			return false
		}
		return true
	})
	for i := 0; i < 3; i++ {
		hold()
	}
	var heap, routines = uint64(0), 0
	deadline := time.Now().Add(20 * time.Second)
	for {
		heap, routines = held()
		if routines <= routines0+3 && heap <= heap0+512<<10 || time.Now().After(deadline) {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Logf("after the first call: %d goroutines, %d KiB of heap; after three more: %d goroutines, %d KiB", routines0, heap0>>10, routines, heap>>10)
	if routines > routines0+3 {
		buf := make([]byte, 1<<20)
		t.Logf("%s", buf[:runtime.Stack(buf, true)])
		t.Fatalf("%d goroutines after three more calls, %d after the first", routines, routines0)
	}
	if heap > heap0+512<<10 {
		t.Fatalf("the heap holds %d bytes after three more calls, %d after the first", heap, heap0)
	}
}
