package calls_test

import (
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/calls"
	"github.com/tikaydaes/churr/internal/calls/calltest"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/testkit"
)

// fast are the waits of docs/CALLS.md section 12, shortened so a test of
// "thirty seconds" takes a second.
func fast() calls.Timings {
	return calls.Timings{Hold: 1200 * time.Millisecond, Tick: 40 * time.Millisecond, Watch: 150 * time.Millisecond, Ring: 1500 * time.Millisecond,
		MediaTimeout: 1500 * time.Millisecond, MediaLost: 900 * time.Millisecond}
}

func server(t *testing.T, tm calls.Timings) *testkit.TestServer {
	return testkit.NewServer(t, testkit.Options{CallTimings: tm})
}

// ticket asks to join the conversation's call and returns the call's
// identifier and the ticket.
func ticket(t *testing.T, c *testkit.Client, conv string) (string, string) {
	t.Helper()
	out := c.Must(200, "POST", "/api/v1/conversations/"+conv+"/call/join", nil)
	return out["call"].(map[string]any)["id"].(string), out["ticket"].(string)
}

// enter opens a live connection for an account and joins the
// conversation's call on it.
func enter(t *testing.T, c *testkit.Client, conv string, o calltest.Options) *calltest.Client {
	t.Helper()
	stream, _ := c.Events(nil)
	cc := calltest.New(t, stream, o)
	t.Cleanup(cc.Close)
	id, tk := ticket(t, c, conv)
	cc.MustJoin(id, tk)
	return cc
}

// eventually waits for a condition, failing the test with what is true
// instead if it never holds.
func eventually(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("never happened: %s", what)
}

// Two people in a call hear and see each other: audio always, video once
// subscribed, over UDP on the loopback address.
func TestTwoPeopleExchangeAudioAndVideo(t *testing.T) {
	ts := server(t, fast())
	ann, bob := ts.Member("ann"), ts.Member("bob")
	ch := ann.Channel("standup", false, bob)
	a := enter(t, ann, ch, calltest.Options{Simulcast: true})
	b := enter(t, bob, ch, calltest.Options{Simulcast: true})

	ans := a.Publish("microphone", "camera")
	if ans["type"] != "call.answer" || ans["pc"] != "send" {
		t.Fatalf("the answer to an offer: %v", ans)
	}
	b.Publish("microphone", "camera")
	amic, acam := a.TrackIDs["microphone"], a.TrackIDs["camera"]
	bmic, bcam := b.TrackIDs["microphone"], b.TrackIDs["camera"]
	if amic == "" || acam == "" || bmic == "" || bcam == "" {
		t.Fatalf("track identifiers: %v %v", a.TrackIDs, b.TrackIDs)
	}
	// Audio needs no subscription.
	eventually(t, "each hears the other", func() bool {
		return b.Stream(amic).Packets.Load() > 20 && a.Stream(bmic).Packets.Load() > 20
	})
	if a.Stream(acam).Packets.Load() != 0 || b.Stream(acam).Packets.Load() != 0 {
		t.Fatal("video was forwarded before anyone subscribed")
	}
	if proto, addr, err := a.Selected(); err != nil || proto != "udp" || addr[:10] != "127.0.0.1:" {
		t.Fatalf("the path in use: %s %s %v", proto, addr, err)
	}
	// Video once subscribed: a 300-pixel tile gets h.
	got := b.Subscribe([3]any{acam, 533, 300})
	if len(got) != 1 || got[0]["state"] != "forwarding" || got[0]["layer"] != "h" {
		t.Fatalf("the answer to call.subscribe: %v", got)
	}
	eventually(t, "bob sees ann's camera at h", func() bool { return b.Stream(acam).Layers()['h'] > 20 })
	if l := b.Stream(acam).Layers(); l['q'] != 0 || l['f'] != 0 {
		t.Fatalf("layers nobody asked for were forwarded: %v", l)
	}
	if a.Stream(bcam).Packets.Load() != 0 {
		t.Fatal("ann was sent video she did not subscribe to")
	}
}

// callID is the call a client is in, as the service names it.
func callID(c *calltest.Client) model.CallID { return model.CallID(c.CallID) }

func is(field string, want any) func(map[string]any) bool {
	return func(f map[string]any) bool { return f[field] == want }
}

// On a network that blocks UDP, ICE over TCP on the same port number
// carries the call.
func TestCallOverTCPWhenUDPIsBlocked(t *testing.T) {
	ts := server(t, fast())
	ann, bob := ts.Member("ann"), ts.Member("bob")
	ch := ann.Channel("standup", false, bob)
	// Ann's network lets no UDP through; Bob's is ordinary.
	a := enter(t, ann, ch, calltest.Options{TCPOnly: true, Simulcast: true})
	b := enter(t, bob, ch, calltest.Options{})
	a.Publish("microphone", "camera")
	b.Publish("microphone", "camera")
	a.Subscribe([3]any{b.TrackIDs["camera"], 1280, 720})
	b.Subscribe([3]any{a.TrackIDs["camera"], 1280, 720})
	eventually(t, "audio and video both ways", func() bool {
		return a.Stream(b.TrackIDs["microphone"]).Packets.Load() > 20 && b.Stream(a.TrackIDs["microphone"]).Packets.Load() > 20 &&
			a.Stream(b.TrackIDs["camera"]).Packets.Load() > 10 && b.Stream(a.TrackIDs["camera"]).Layers()['f'] > 10
	})
	if proto, addr, err := a.Selected(); err != nil || proto != "tcp" {
		t.Fatalf("ann's path: %s %s %v", proto, addr, err)
	}
	if proto, _, err := b.Selected(); err != nil || proto != "udp" {
		t.Fatalf("bob's path: %s %v", proto, err)
	}
	// The same number for both.
	_, aAddr, _ := a.Selected()
	_, bAddr, _ := b.Selected()
	if aAddr != bAddr {
		t.Fatalf("TCP and UDP use different addresses: %s %s", aAddr, bAddr)
	}
}

// Three people. What a subscriber asks for decides what it is forwarded:
// which tracks, and which layer from the size it says it is showing.
func TestSubscriptionsDecideWhatIsForwarded(t *testing.T) {
	ts := server(t, fast())
	ann, bob, cy := ts.Member("ann"), ts.Member("bob"), ts.Member("cy")
	ch := ann.Channel("standup", false, bob, cy)
	a := enter(t, ann, ch, calltest.Options{Simulcast: true})
	b := enter(t, bob, ch, calltest.Options{Simulcast: true})
	c := enter(t, cy, ch, calltest.Options{})
	a.Publish("microphone", "camera")
	b.Publish("microphone", "camera")
	c.Publish("microphone")
	acam, bcam := a.TrackIDs["camera"], b.TrackIDs["camera"]

	// The others learn of each track as it is published.
	pub := c.Expect("call.track_published", func(f map[string]any) bool { return f["track"].(map[string]any)["id"] == acam })
	tr := pub["track"].(map[string]any)
	if pub["account_id"] != string(ann.ID) || tr["source"] != "camera" || tr["kind"] != "video" || tr["codec"] != "vp8" || tr["muted"] != false ||
		len(tr["layers"].([]any)) != 3 || tr["layers"].([]any)[0] != "q" || pub["call_id"] != a.CallID || pub["conversation_id"] != ch || pub["at"] == nil {
		t.Fatalf("call.track_published: %v", pub)
	}
	// Everyone hears everyone, with no subscription.
	eventually(t, "cy hears ann and bob", func() bool {
		return c.Stream(a.TrackIDs["microphone"]).Packets.Load() > 10 && c.Stream(b.TrackIDs["microphone"]).Packets.Load() > 10
	})

	// Cy shows Ann small and Bob large.
	got := c.Subscribe([3]any{acam, 320, 180}, [3]any{bcam, 1280, 720})
	if got[0]["layer"] != "q" || got[1]["layer"] != "f" || got[0]["track_id"] != acam {
		t.Fatalf("call.subscribe: %v", got)
	}
	eventually(t, "ann at q and bob at f", func() bool {
		return c.Stream(acam).Layers()['q'] > 10 && c.Stream(bcam).Layers()['f'] > 10
	})
	// Each publisher is told which of its layers anyone is receiving.
	demand := a.Expect("call.demand_changed", func(f map[string]any) bool { return len(f["layers"].([]any)) == 1 })
	if demand["track_id"] != acam || demand["layers"].([]any)[0] != "q" {
		t.Fatalf("call.demand_changed: %v", demand)
	}

	// The window is resized: Ann's tile grows. The server moves Cy to f
	// at one of f's key frames, which it asks Ann for, and Cy sees one
	// continuous stream.
	before := a.PLIs("camera", "f")
	if got := c.Subscribe([3]any{acam, 1280, 720}, [3]any{bcam, 1280, 720}); got[0]["layer"] != "f" {
		t.Fatalf("call.subscribe after growing: %v", got)
	}
	eventually(t, "ann moves to f", func() bool { return c.Stream(acam).Last() == 'f' && c.Stream(acam).Layers()['f'] > 10 })
	if a.PLIs("camera", "f") == before {
		t.Fatal("the publisher was not asked for a key frame of the new layer")
	}
	a.Expect("call.demand_changed", func(f map[string]any) bool {
		l := f["layers"].([]any)
		return len(l) == 1 && l[0] == "f"
	})
	// And back down.
	c.Subscribe([3]any{acam, 320, 180}, [3]any{bcam, 1280, 720})
	eventually(t, "ann moves back to q", func() bool { return c.Stream(acam).Last() == 'q' })
	if seq, pic, ts, bad := c.Stream(acam).Breaks(); seq != 0 || pic != 0 || ts != 0 || bad != 0 {
		t.Fatalf("the stream did not carry on across layer changes: %d sequence breaks, %d picture breaks, %d timestamps back, %d switches off a key frame", seq, pic, ts, bad)
	}
	if l := c.Stream(acam).Layers(); l['h'] != 0 {
		t.Fatalf("a layer nobody chose was forwarded: %v", l)
	}

	// A track not in the list is not forwarded.
	c.Subscribe([3]any{bcam, 1280, 720})
	a.Expect("call.demand_changed", func(f map[string]any) bool { return len(f["layers"].([]any)) == 0 })
	time.Sleep(150 * time.Millisecond)
	n := c.Stream(acam).Packets.Load()
	m := c.Stream(bcam).Packets.Load()
	time.Sleep(400 * time.Millisecond)
	if c.Stream(acam).Packets.Load() != n {
		t.Fatal("a track taken out of the list was still forwarded")
	}
	if c.Stream(bcam).Packets.Load() == m {
		t.Fatal("the track left in the list stopped")
	}
	// The app goes to the background: an empty list. Audio carries on.
	c.Subscribe()
	time.Sleep(150 * time.Millisecond)
	m = c.Stream(bcam).Packets.Load()
	heard := c.Stream(b.TrackIDs["microphone"]).Packets.Load()
	time.Sleep(400 * time.Millisecond)
	if c.Stream(bcam).Packets.Load() != m || c.Stream(b.TrackIDs["microphone"]).Packets.Load() == heard {
		t.Fatal("an empty list should stop all video and no audio")
	}

	// Sixteen video tracks at most; and a list names a track once.
	var many [][3]any
	for i := 0; i < 17; i++ {
		many = append(many, [3]any{"t" + string(rune('a'+i)), 100, 100})
	}
	list := []map[string]any{}
	for _, v := range many {
		list = append(list, map[string]any{"track_id": v[0], "width": v[1], "height": v[2]})
	}
	c.Refused("too_many_subscriptions", map[string]any{"type": "call.subscribe", "video": list})
	c.Refused("invalid_request", map[string]any{"type": "call.subscribe", "video": []map[string]any{
		{"track_id": acam, "width": 1, "height": 1}, {"track_id": acam, "width": 2, "height": 2}}})
	// A track that does not exist, a microphone, and one's own camera
	// are unavailable.
	got = b.Subscribe([3]any{"t999", 100, 100}, [3]any{a.TrackIDs["microphone"], 100, 100}, [3]any{bcam, 100, 100})
	for _, v := range got {
		if v["state"] != "unavailable" || v["layer"] != nil {
			t.Fatalf("should be unavailable: %v", got)
		}
	}
}

// The server keeps what it forwards inside the subscriber's downlink
// estimate, lowering and pausing in the documented order, and says so.
func TestBandwidthLowersAndPauses(t *testing.T) {
	ts := server(t, fast())
	ann, bob, cy := ts.Member("ann"), ts.Member("bob"), ts.Member("cy")
	ch := ann.Channel("standup", false, bob, cy)
	a := enter(t, ann, ch, calltest.Options{Simulcast: true})
	b := enter(t, bob, ch, calltest.Options{Simulcast: true})
	c := enter(t, cy, ch, calltest.Options{})
	a.Publish("microphone", "camera")
	b.Publish("microphone", "camera", "screen")
	acam, bcam, screen := a.TrackIDs["camera"], b.TrackIDs["camera"], b.TrackIDs["screen"]
	c.Subscribe([3]any{acam, 1280, 720}, [3]any{bcam, 640, 360}, [3]any{screen, 1920, 1080})
	eventually(t, "all three at full size", func() bool {
		return c.Stream(acam).Last() == 'f' && c.Stream(bcam).Last() == 'h' && c.Stream(screen).Packets.Load() > 10
	})
	// The real estimator runs: on the loopback address it must not be
	// what holds video back.
	if limit := ts.Server.Calls.Limit(callID(a), cy.ID); limit != 0 {
		t.Fatalf("the downlink on the loopback address is held to %d bit/s", limit)
	}

	// 4.6 Mbit/s: less two microphones and a tenth, 4,076 for video,
	// against 1,200 + 500 + 2,500 wanted. The smaller tile goes down
	// first (h to q), which is enough.
	ts.Server.Calls.SetEstimate(callID(a), cy.ID, 4_600_000)
	ch1 := c.Expect("call.layer_changed", is("track_id", bcam))
	if ch1["state"] != "forwarding" || ch1["layer"] != "q" || ch1["reason"] != "bandwidth" || ch1["call_id"] != a.CallID {
		t.Fatalf("call.layer_changed: %v", ch1)
	}
	eventually(t, "bob's camera at q", func() bool { return c.Stream(bcam).Last() == 'q' })
	if c.Stream(acam).Last() != 'f' {
		t.Fatal("the larger tile was lowered before the smaller")
	}

	// 2.9 Mbit/s: 2,546 for video. The screen alone takes 2,500, and a
	// screen is never lowered: both cameras pause.
	ts.Server.Calls.SetEstimate(callID(a), cy.ID, 2_900_000)
	c.Expect("call.layer_changed", func(f map[string]any) bool { return f["track_id"] == bcam && f["state"] == "paused" })
	paused := c.Expect("call.layer_changed", func(f map[string]any) bool { return f["track_id"] == acam && f["state"] == "paused" })
	if paused["layer"] != nil || paused["reason"] != "bandwidth" {
		t.Fatalf("a paused track: %v", paused)
	}
	time.Sleep(200 * time.Millisecond)
	cam, scr := c.Stream(acam).Packets.Load(), c.Stream(screen).Packets.Load()
	time.Sleep(400 * time.Millisecond)
	if c.Stream(acam).Packets.Load() != cam {
		t.Fatal("a paused camera was still forwarded")
	}
	if c.Stream(screen).Packets.Load() == scr {
		t.Fatal("the screen stopped")
	}
	// Nobody receives Ann's camera now, and she is told.
	a.Expect("call.demand_changed", func(f map[string]any) bool { return f["track_id"] == acam && len(f["layers"].([]any)) == 0 })

	// The estimate recovers, and the layers come back a step at a time.
	ts.Server.Calls.QuickRaise(callID(a), cy.ID)
	ts.Server.Calls.SetEstimate(callID(a), cy.ID, 20_000_000)
	c.Expect("call.layer_changed", func(f map[string]any) bool { return f["track_id"] == acam && f["layer"] == "f" })
	c.Expect("call.layer_changed", func(f map[string]any) bool { return f["track_id"] == bcam && f["layer"] == "h" })
	eventually(t, "back at full size", func() bool { return c.Stream(acam).Last() == 'f' && c.Stream(bcam).Last() == 'h' })
	if _, _, _, bad := c.Stream(acam).Breaks(); bad != 0 {
		t.Fatalf("%d layer changes did not begin at a key frame", bad)
	}
}
