package calls_test

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/calls"
	"github.com/tikaydaes/churr/internal/calls/calltest"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/testkit"
)

// Mute state, one shared screen at a time, and unpublishing.
func TestMuteAndScreenSharing(t *testing.T) {
	ts := server(t, fast())
	ann, bob := ts.Member("ann"), ts.Member("bob")
	ch := ann.Channel("standup", false, bob)
	a := enter(t, ann, ch, calltest.Options{Simulcast: true})
	b := enter(t, bob, ch, calltest.Options{})
	a.Publish("microphone", "camera")
	b.Publish("microphone")
	amic, acam := a.TrackIDs["microphone"], a.TrackIDs["camera"]
	b.Subscribe([3]any{acam, 640, 360})
	eventually(t, "bob sees and hears ann", func() bool { return b.Stream(acam).Packets.Load() > 5 && b.Stream(amic).Packets.Load() > 5 })

	// Turning the camera off is a mute: everyone is told, the subscriber
	// is told the track's new state, and nothing more is forwarded even
	// if the client keeps sending.
	a.OK(map[string]any{"type": "call.mute", "source": "camera", "muted": true})
	mc := b.Expect("call.mute_changed", nil)
	if mc["account_id"] != string(ann.ID) || mc["track_id"] != acam || mc["source"] != "camera" || mc["muted"] != true || mc["call_id"] != a.CallID {
		t.Fatalf("call.mute_changed: %v", mc)
	}
	lc := b.Expect("call.layer_changed", is("track_id", acam))
	if lc["state"] != "muted" || lc["layer"] != nil || lc["reason"] != "publisher" {
		t.Fatalf("call.layer_changed for a muted track: %v", lc)
	}
	time.Sleep(150 * time.Millisecond)
	n := b.Stream(acam).Packets.Load()
	time.Sleep(300 * time.Millisecond)
	if b.Stream(acam).Packets.Load() != n {
		t.Fatal("a muted camera was still forwarded")
	}
	if got := b.Subscribe([3]any{acam, 640, 360}); got[0]["state"] != "muted" {
		t.Fatalf("subscribing to a muted track: %v", got)
	}
	// The call object says so to someone who joins now.
	tracks := obj(b.OK(map[string]any{"type": "call.resume", "call_id": b.CallID, "resume_token": b.ResumeToken}), "call")["participants"].([]any)[0].(map[string]any)["tracks"].([]any)
	if len(tracks) != 2 || tracks[1].(map[string]any)["muted"] != true || tracks[0].(map[string]any)["source"] != "microphone" {
		t.Fatalf("tracks in the call object: %v", tracks)
	}
	a.OK(map[string]any{"type": "call.mute", "source": "camera", "muted": false})
	b.Expect("call.layer_changed", func(f map[string]any) bool { return f["track_id"] == acam && f["state"] == "forwarding" })
	eventually(t, "the camera is back", func() bool { return b.Stream(acam).Packets.Load() > n+5 })
	// Only one's own, published tracks can be muted.
	b.Refused("invalid_request", map[string]any{"type": "call.mute", "source": "camera", "muted": true})
	b.Refused("invalid_request", map[string]any{"type": "call.mute", "source": "microphone"})

	// One screen at a time.
	if f := a.Publish("microphone", "camera", "screen"); f["type"] != "call.answer" {
		t.Fatalf("sharing a screen: %v", f)
	}
	screen := a.TrackIDs["screen"]
	pub := b.Expect("call.track_published", func(f map[string]any) bool { return obj(f, "track")["source"] == "screen" })
	if tr := obj(pub, "track"); tr["id"] != screen || tr["layers"] != nil || tr["kind"] != "video" {
		t.Fatalf("a screen's track: %v", pub)
	}
	refused := b.Publish("microphone", "screen")
	if refused["type"] != "call.error" || refused["code"] != "screen_in_use" || refused["account_id"] != string(ann.ID) {
		t.Fatalf("a second screen: %v", refused)
	}
	if got := b.Subscribe([3]any{acam, 640, 360}, [3]any{screen, 1920, 1080}); got[1]["state"] != "forwarding" || got[1]["layer"] != "f" {
		t.Fatalf("subscribing to a screen: %v", got)
	}
	eventually(t, "bob sees the screen", func() bool { return b.Stream(screen).Layers()['s'] > 5 })
	// Stopping the share is an unpublish: a new offer without the line.
	a.Publish("microphone", "camera")
	un := b.Expect("call.track_unpublished", nil)
	if un["track_id"] != screen || un["account_id"] != string(ann.ID) {
		t.Fatalf("call.track_unpublished: %v", un)
	}
	b.Expect("call.layer_changed", func(f map[string]any) bool { return f["track_id"] == screen && f["state"] == "unavailable" })
	// Now Bob may share, and Ann's media line for it is used again
	// rather than a new one added for ever.
	if f := b.Publish("microphone", "screen"); f["type"] != "call.answer" {
		t.Fatalf("sharing once the first screen stopped: %v", f)
	}
	// A screen is one layer; sources are published once; lines must exist.
	a.Refused("invalid_request", map[string]any{"type": "call.offer", "pc": "send", "sdp": "v=0\r\n", "ice_restart": false,
		"tracks": []map[string]any{{"mid": "9", "source": "camera"}}})
	a.Refused("invalid_request", map[string]any{"type": "call.offer", "pc": "recv", "sdp": "v=0\r\n", "ice_restart": false, "tracks": []any{}})
	a.Refused("invalid_request", map[string]any{"type": "call.answer", "pc": "recv", "offer_id": 999, "sdp": "v=0\r\n"})
	a.OK(map[string]any{"type": "call.candidate", "pc": "send", "candidate": "candidate:842163049 1 udp 1677729535 203.0.113.7 51234 typ srflx", "sdp_mid": "0", "sdp_mline_index": 0})
	a.OK(map[string]any{"type": "call.candidate", "pc": "recv", "candidate": nil})
	a.Refused("invalid_request", map[string]any{"type": "call.candidate", "pc": "both", "candidate": nil})
	a.Refused("invalid_request", map[string]any{"type": "call.nonsense"})
}

// Who is speaking is worked out from the loudness each client states.
func TestActiveSpeaker(t *testing.T) {
	ts := server(t, fast())
	ann, bob, cy := ts.Member("ann"), ts.Member("bob"), ts.Member("cy")
	ch := ann.Channel("standup", false, bob, cy)
	a := enter(t, ann, ch, calltest.Options{})
	b := enter(t, bob, ch, calltest.Options{})
	c := enter(t, cy, ch, calltest.Options{})
	a.Publish("microphone")
	b.Publish("microphone")
	c.Publish("microphone")
	// Everyone is silent, then Ann talks.
	a.SetLevel(20)
	sp := c.Expect("call.speakers_changed", nil)
	if ids := sp["account_ids"].([]any); len(ids) != 1 || ids[0] != string(ann.ID) || sp["call_id"] != a.CallID || sp["conversation_id"] != ch {
		t.Fatalf("call.speakers_changed: %v", sp)
	}
	// Bob talks over her, louder: loudest first.
	b.SetLevel(5)
	c.Expect("call.speakers_changed", func(f map[string]any) bool {
		ids := f["account_ids"].([]any)
		return len(ids) == 2 && ids[0] == string(bob.ID) && ids[1] == string(ann.ID)
	})
	// Both stop: after a second and a half, nobody.
	a.SetLevel(127)
	b.SetLevel(127)
	c.Expect("call.speakers_changed", func(f map[string]any) bool { return len(f["account_ids"].([]any)) == 0 })
	// Muting the microphone ends it at once. (The two may have stopped a
	// window apart, leaving a report of one speaker unread: forget those.)
	for c.Find("call.speakers_changed", nil, 0) != nil {
	}
	a.SetLevel(10)
	c.Expect("call.speakers_changed", func(f map[string]any) bool { return len(f["account_ids"].([]any)) == 1 })
	a.OK(map[string]any{"type": "call.mute", "source": "microphone", "muted": true})
	c.Expect("call.speakers_changed", func(f map[string]any) bool { return len(f["account_ids"].([]any)) == 0 })
}

// An account is in a call on one device. Joining from a second moves it.
func TestOneDevicePerAccount(t *testing.T) {
	ts := server(t, fast())
	ann, bob := ts.Member("ann"), ts.Member("bob")
	ch := ann.Channel("standup", false, bob)
	watch, _ := bob.Events(nil)
	b := enter(t, bob, ch, calltest.Options{})
	first := enter(t, ann, ch, calltest.Options{})
	first.Publish("microphone")
	watch.ExpectWhere("call.participant_joined", func(f map[string]any) bool { return obj(f, "participant")["account_id"] == string(ann.ID) })
	mic := first.TrackIDs["microphone"]
	eventually(t, "bob hears ann's first device", func() bool { return b.Stream(mic).Packets.Load() > 5 })

	phone := ann.Token()
	second := enter(t, phone, ch, calltest.Options{})
	// The first device is told it left, for itself alone.
	left := first.Expect("call.participant_left", nil)
	if left["account_id"] != string(ann.ID) || left["reason"] != "joined_elsewhere" {
		t.Fatalf("the first device: %v", left)
	}
	first.Refused("not_in_call", map[string]any{"type": "call.subscribe", "video": []any{}})
	// Its track went with it; the others are told.
	b.Expect("call.track_unpublished", is("track_id", mic))
	second.Publish("microphone")
	eventually(t, "bob hears ann's second device", func() bool { return b.Stream(second.TrackIDs["microphone"]).Packets.Load() > 5 })
	// Still two people, and nobody was told Ann left or joined again.
	if got := obj(bob.Must(200, "GET", convPath+ch+"/call", nil), "call"); len(got["participants"].([]any)) != 2 {
		t.Fatalf("after the move: %v", got)
	}
	second.Leave()
	if f := watch.ExpectWhere("call.participant_left", is("account_id", string(ann.ID))); f["reason"] != "left" {
		t.Fatalf("the only call.participant_left the conversation should see: %v", f)
	}
}

// Signalling and media fail separately and are mended separately
// (docs/CALLS.md section 11).
func TestReconnection(t *testing.T) {
	ts := server(t, fast())
	ann, bob := ts.Member("ann"), ts.Member("bob")
	ch := ann.Channel("standup", false, bob)
	watch, _ := bob.Events(nil)
	b := enter(t, bob, ch, calltest.Options{})
	annStream, _ := ann.Events(nil)
	a := calltest.New(t, annStream, calltest.Options{Simulcast: true})
	t.Cleanup(a.Close)
	id, tk := ticket(t, ann, ch)
	a.MustJoin(id, tk)
	a.Publish("microphone", "camera")
	b.Publish("microphone")
	amic, bmic := a.TrackIDs["microphone"], b.TrackIDs["microphone"]
	b.Subscribe([3]any{a.TrackIDs["camera"], 640, 360})
	eventually(t, "media both ways", func() bool { return b.Stream(amic).Packets.Load() > 5 && a.Stream(bmic).Packets.Load() > 5 })

	// The WebSocket drops; media still flows. People keep hearing each
	// other, for longer than the hold time, because media is alive.
	a.Detach()
	annStream.Close()
	time.Sleep(2 * time.Second)
	n, m := b.Stream(amic).Packets.Load(), a.Stream(bmic).Packets.Load()
	time.Sleep(300 * time.Millisecond)
	if b.Stream(amic).Packets.Load() == n || a.Stream(bmic).Packets.Load() == m {
		t.Fatal("media stopped when signalling dropped")
	}
	if got := obj(bob.Must(200, "GET", convPath+ch+"/call", nil), "call"); len(got["participants"].([]any)) != 2 {
		t.Fatalf("a participant whose media is alive was dropped: %v", got)
	}
	// While Ann has no connection, Bob publishes a camera, which needs a
	// new offer to her.
	oldToken := a.ResumeToken
	offers := a.Offers.Load()
	// She reconnects and resumes on the new connection.
	again, _ := ann.Events(nil)
	a.Attach(again)
	res := a.Resume()
	if res["type"] != "call.ok" || res["resume_token"] == oldToken || len(obj(res, "call")["participants"].([]any)) != 2 {
		t.Fatalf("call.resume: %v", res)
	}
	// Signalling works again on the new connection.
	b.Publish("microphone", "camera")
	a.Expect("call.track_published", func(f map[string]any) bool { return obj(f, "track")["source"] == "camera" })
	a.Subscribe([3]any{b.TrackIDs["camera"], 640, 360})
	eventually(t, "ann sees bob's new camera", func() bool { return a.Stream(b.TrackIDs["camera"]).Packets.Load() > 5 })
	if a.Offers.Load() == offers {
		t.Fatal("no offer reached the resumed connection")
	}
	// A resume token works once, and only for its own account.
	a.Refused("ticket_invalid", map[string]any{"type": "call.resume", "call_id": a.CallID, "resume_token": oldToken})
	b.Refused("ticket_invalid", map[string]any{"type": "call.resume", "call_id": a.CallID, "resume_token": a.ResumeToken})

	// Media stalls; the WebSocket is fine: ICE restarts on both
	// connections, and the tracks survive it.
	if f := a.RestartSend(); f["type"] != "call.answer" {
		t.Fatalf("an ICE restart on send: %v", f)
	}
	before := a.Offers.Load()
	a.OK(map[string]any{"type": "call.restart", "pc": "recv"})
	fresh := a.Expect("call.offer", is("ice_restart", true))
	if fresh["pc"] != "recv" || !strings.Contains(fresh["sdp"].(string), "a=ice-lite") || len(fresh["tracks"].([]any)) == 0 {
		t.Fatalf("the offer after call.restart: %v", fresh)
	}
	if a.Offers.Load() == before {
		t.Fatal("call.restart brought no offer")
	}
	a.Refused("invalid_request", map[string]any{"type": "call.restart", "pc": "send"})
	n, m = b.Stream(amic).Packets.Load(), a.Stream(bmic).Packets.Load()
	eventually(t, "media after the restarts", func() bool {
		return b.Stream(amic).Packets.Load() > n+20 && a.Stream(bmic).Packets.Load() > m+20
	})
	if left := watchFind(watch, "call.participant_left", 300*time.Millisecond); left != nil {
		t.Fatalf("a restart looked like a leave: %v", left)
	}
}

// watchFind reads a stream for a while and returns the first frame of a
// type, or nil.
func watchFind(s *testkit.Stream, frameType string, wait time.Duration) map[string]any {
	found := make(chan map[string]any, 1)
	go func() {
		for {
			f, ok := s.Next(time.Hour)
			if !ok {
				return
			}
			if f["type"] == frameType {
				found <- f
				return
			}
		}
	}()
	select {
	case f := <-found:
		return f
	case <-time.After(wait):
		return nil
	}
}

// Signalling that works with media that never connects is reported, and a
// participant with neither for the hold time has left.
func TestMediaTimeoutAndHold(t *testing.T) {
	ts := server(t, fast())
	ann, bob := ts.Member("ann"), ts.Member("bob")
	ch := ann.Channel("standup", false, bob)
	watch, _ := bob.Events(nil)
	enter(t, bob, ch, calltest.Options{}).Publish("microphone")
	stream, _ := ann.Events(nil)
	a := calltest.New(t, stream, calltest.Options{NoAnswer: true})
	t.Cleanup(a.Close)
	id, tk := ticket(t, ann, ch)
	a.MustJoin(id, tk)
	// Ann never makes a media connection: she publishes nothing and
	// answers none of the server's offers.
	e := a.Expect("call.error", nil)
	if e["code"] != "media_timeout" || e["reply_to"] != nil {
		t.Fatalf("the error for media that never came: %v", e)
	}
	// She is still in the call while her signalling is alive.
	if got := obj(bob.Must(200, "GET", convPath+ch+"/call", nil), "call"); len(got["participants"].([]any)) != 2 {
		t.Fatalf("participants: %v", got)
	}
	// Then her connection goes too.
	a.Detach()
	stream.Close()
	if left := watch.ExpectWhere("call.participant_left", is("account_id", string(ann.ID))); left["reason"] != "timeout" {
		t.Fatalf("call.participant_left: %v", left)
	}
}

// The limits an admin can set: participants and cameras.
func TestLimits(t *testing.T) {
	ts := server(t, fast())
	ann, bob, cy := ts.Member("ann"), ts.Member("bob"), ts.Member("cy")
	ch := ann.Channel("standup", false, bob, cy)
	ts.Owner.Must(200, "PATCH", "/api/v1/admin/settings", map[string]any{"calls.max_participants": 2, "calls.max_cameras": 1})
	a := enter(t, ann, ch, calltest.Options{})
	// A ticket had before the call filled is refused when it is used.
	_, early := ticket(t, cy, ch)
	b := enter(t, bob, ch, calltest.Options{})
	cy.Refused(409, model.CodeConflict, "POST", convPath+ch+"/call/join", nil)
	stream, _ := cy.Events(nil)
	c := calltest.New(t, stream, calltest.Options{})
	t.Cleanup(c.Close)
	if f := c.Join(a.CallID, early); f["code"] != "call_full" {
		t.Fatalf("joining a full call: %v", f)
	}
	// Someone already in it can always ask again.
	ticket(t, ann, ch)
	if ok := a.OK(map[string]any{"type": "call.resume", "call_id": a.CallID, "resume_token": a.ResumeToken}); obj(ok, "limits")["max_participants"] != 2.0 {
		t.Fatalf("the limits a client is told: %v", ok["limits"])
	}

	// One camera on at once. The microphone still works.
	if f := a.Publish("microphone", "camera"); f["type"] != "call.answer" {
		t.Fatalf("the first camera: %v", f)
	}
	if f := b.Publish("microphone", "camera"); f["code"] != "cameras_full" {
		t.Fatalf("a camera over the limit: %v", f)
	}
	if f := b.Publish("microphone"); f["type"] != "call.answer" {
		t.Fatalf("the microphone after the camera was refused: %v", f)
	}
	eventually(t, "ann hears bob", func() bool { return a.Stream(b.TrackIDs["microphone"]).Packets.Load() > 5 })
	// A camera that is off does not count: Ann turns hers off, Bob's goes
	// on, and then Ann's cannot come back.
	a.OK(map[string]any{"type": "call.mute", "source": "camera", "muted": true})
	if f := b.Publish("microphone", "camera"); f["type"] != "call.answer" {
		t.Fatalf("a camera once another was turned off: %v", f)
	}
	a.Refused("cameras_full", map[string]any{"type": "call.mute", "source": "camera", "muted": false})
	// The admin's list shows numbers, not people.
	admin := ts.Owner.Must(200, "GET", "/api/v1/admin/calls", nil)
	list := admin["calls"].([]any)
	if len(list) != 1 || list[0].(map[string]any)["participant_count"] != 2.0 || list[0].(map[string]any)["camera_count"] != 1.0 {
		t.Fatalf("the admin's list of calls: %v", admin)
	}
}

// H.264 is the fallback: a client that can only encode it can still
// publish, and a subscriber that cannot decode it is told so.
func TestH264Fallback(t *testing.T) {
	ts := server(t, fast())
	ann, bob, cy := ts.Member("ann"), ts.Member("bob"), ts.Member("cy")
	ch := ann.Channel("standup", false, bob, cy)
	a := enter(t, ann, ch, calltest.Options{H264: true, Decodes: []string{"h264"}})
	b := enter(t, bob, ch, calltest.Options{})                        // decodes both
	c := enter(t, cy, ch, calltest.Options{Decodes: []string{"vp8"}}) // VP8 only
	a.Publish("microphone", "camera")
	cam := a.TrackIDs["camera"]
	pub := b.Expect("call.track_published", func(f map[string]any) bool { return obj(f, "track")["source"] == "camera" })
	if obj(pub, "track")["codec"] != "h264" {
		t.Fatalf("the track's codec: %v", pub)
	}
	if got := b.Subscribe([3]any{cam, 640, 360}); got[0]["state"] != "forwarding" || got[0]["layer"] != "f" {
		t.Fatalf("a subscriber that decodes H.264: %v", got)
	}
	if got := c.Subscribe([3]any{cam, 640, 360}); got[0]["state"] != "unavailable" {
		t.Fatalf("a subscriber that cannot decode H.264: %v", got)
	}
	eventually(t, "bob sees the H.264 camera", func() bool { return b.Stream(cam).Layers()['f'] > 10 })
	time.Sleep(200 * time.Millisecond)
	if c.Stream(cam).Packets.Load() != 0 {
		t.Fatal("a track was forwarded to a client that cannot decode it")
	}
	// Cy still hears Ann.
	eventually(t, "cy hears ann", func() bool { return c.Stream(a.TrackIDs["microphone"]).Packets.Load() > 5 })
	// The server prefers VP8: it is first in what it sends.
	ans := b.Publish("camera")
	sdp := ans["sdp"].(string)
	if v, h := strings.Index(sdp, "VP8/90000"), strings.Index(sdp, "H264/90000"); v < 0 || h < 0 || v > h {
		t.Fatalf("VP8 should be listed before H.264 in the server's answer:\n%s", sdp)
	}
}

// sealedChat makes a sealed chat between two accounts in the store, as the
// sealed package will through its own operation.
func sealedChat(t *testing.T, ts *testkit.TestServer, a, b model.AccountID) string {
	t.Helper()
	ctx := context.Background()
	c := model.Conversation{Kind: model.KindSealed, Private: true, CreatedByID: a, MemberKey: "s:" + string(a) + ":" + string(b)}
	if err := ts.Server.Store.Conversations().Create(ctx, &c); err != nil {
		t.Fatal(err)
	}
	for _, id := range []model.AccountID{a, b} {
		if _, err := ts.Server.Store.Members().AddDirect(ctx, c.ID, id, model.LevelMember, model.Now()); err != nil {
			t.Fatal(err)
		}
	}
	return string(c.ID)
}

// A sealed chat's call: the client must say it encrypts frames, and the
// server forwards frames it cannot read, changing nothing in them and
// still choosing layers and switching at key frames.
func TestSealedChatCall(t *testing.T) {
	ts := server(t, fast())
	ann, bob := ts.Member("ann"), ts.Member("bob")
	chat := sealedChat(t, ts, ann.ID, bob.ID)

	stream, _ := ann.Events(nil)
	plain := calltest.New(t, stream, calltest.Options{})
	t.Cleanup(plain.Close)
	out := ann.Must(200, "POST", convPath+chat+"/call/join", nil)
	if obj(out, "call")["sealed"] != true {
		t.Fatalf("a sealed chat's call: %v", out["call"])
	}
	id := obj(out, "call")["id"].(string)
	if f := plain.Join(id, out["ticket"].(string)); f["code"] != "frame_encryption_required" {
		t.Fatalf("joining without frame encryption: %v", f)
	}

	// A stand-in for the devices' frame encryption: every byte after the
	// readable leading bytes is turned into something the server has no
	// key for. What arrives must be exactly what was sent.
	var mu sync.Mutex
	sent := map[string]bool{}
	seal := func(frame []byte) {
		for i := range frame {
			frame[i] = frame[i]*31 + 0xa7 ^ byte(i)
		}
		mu.Lock()
		sent[string(frame)] = true
		mu.Unlock()
	}
	a := enter(t, ann, chat, calltest.Options{Simulcast: true, FrameEncryption: true, Encrypt: seal})
	b := enter(t, bob, chat, calltest.Options{FrameEncryption: true})
	a.Publish("microphone", "camera")
	mic, cam := a.TrackIDs["microphone"], a.TrackIDs["camera"]
	b.Subscribe([3]any{cam, 320, 180})
	eventually(t, "bob receives the sealed audio and video", func() bool { return b.Stream(mic).Packets.Load() > 10 && b.Stream(cam).Layers()['q'] > 10 })
	// Layer choice and switching at key frames still work.
	b.Subscribe([3]any{cam, 1280, 720})
	eventually(t, "the layer changes in a sealed call", func() bool { return b.Stream(cam).Last() == 'f' && b.Stream(cam).Layers()['f'] > 10 })
	if seq, pic, tsb, bad := b.Stream(cam).Breaks(); seq != 0 || pic != 0 || tsb != 0 || bad != 0 {
		t.Fatalf("the sealed stream did not carry on across a layer change: %d %d %d %d", seq, pic, tsb, bad)
	}
	// The bytes after the readable ones arrive as they were sent: VP8's
	// five leading bytes here (descriptor and first frame byte) and the
	// test's one-byte layer mark, then the sealed rest.
	for _, check := range []struct {
		track string
		skip  int
	}{{cam, 6}, {mic, 1}} {
		got := b.Stream(check.track).LastPayload()
		mu.Lock()
		known := len(got) > check.skip && sent[string(got[check.skip:])]
		mu.Unlock()
		if !known {
			t.Fatalf("a sealed frame of %s was changed on the way: % x", check.track, got[:min(len(got), 24)])
		}
	}
	// The rewritten part is the packetizer's descriptor alone: the frame
	// header byte after it is as sent.
	if got := b.Stream(cam).LastPayload(); got[0] != 0x90 || got[1] != 0x80 || got[4]&0xfe != 0 || !bytes.Equal(got[5:6], []byte{'f'}) {
		t.Fatalf("the readable bytes of a sealed video frame: % x", got[:8])
	}
}

// Ringing: who is rung for which kind of conversation, do not disturb, and
// how a ring ends (docs/CALLS.md section 15).
func TestRinging(t *testing.T) {
	ts := server(t, fast())
	ann, bob, cy, dee := ts.Member("ann"), ts.Member("bob"), ts.Member("cy"), ts.Member("dee")
	// What the push package would hear.
	var mu sync.Mutex
	var rang []calls.Ring
	var stopped []calls.RingStop
	ts.Server.Calls.OnRing(func(r calls.Ring) { mu.Lock(); rang = append(rang, r); mu.Unlock() })
	ts.Server.Calls.OnRingStopped(func(r calls.RingStop) { mu.Lock(); stopped = append(stopped, r); mu.Unlock() })

	// A named channel starts in Mentions: nobody is rung, except a member
	// who chose Any activity for it.
	named := ann.Channel("standup", false, bob, cy)
	cy.Must(200, "PATCH", convPath+named+"/membership", map[string]any{"mode": "any"})
	bobDesk, _ := bob.Events(nil)
	bobPhone, _ := bob.Token().Events(nil)
	cyDesk, _ := cy.Events(nil)
	a := enter(t, ann, named, calltest.Options{})
	ring := cyDesk.Expect("call.ring_started")
	if ring["call_id"] != a.CallID || ring["conversation_id"] != named || ring["from_account_id"] != string(ann.ID) || ring["until"] == nil || ring["seq"] != nil {
		t.Fatalf("call.ring_started: %v", ring)
	}
	if st := cy.Must(200, "GET", "/api/v1/status", nil); st["ringing_call_id"] != a.CallID {
		t.Fatalf("the status summary while ringing: %v", st)
	}
	if st := bob.Must(200, "GET", "/api/v1/status", nil); st["ringing_call_id"] != nil {
		t.Fatalf("bob is on Mentions and should not be rung: %v", st)
	}
	// Nobody answers: the ring gives up, and the call goes on.
	if f := cyDesk.Expect("call.ring_stopped"); f["reason"] != "timeout" || f["call_id"] != a.CallID {
		t.Fatalf("call.ring_stopped: %v", f)
	}
	if st := cy.Must(200, "GET", "/api/v1/status", nil); st["ringing_call_id"] != nil {
		t.Fatalf("still ringing after the ring gave up: %v", st)
	}
	if out := ann.Must(200, "GET", convPath+named+"/call", nil); out["call"] == nil {
		t.Fatal("an unanswered ring ended the call")
	}
	a.Leave()

	// An ad-hoc channel starts in Any activity: every other member is
	// rung, on every connection, unless they are in do not disturb.
	_, chatOut := ann.Do("POST", "/api/v1/chats", map[string]any{"account_ids": []string{string(bob.ID), string(cy.ID), string(dee.ID)}})
	adhoc := obj(chatOut, "conversation")["id"].(string)
	dee.Must(200, "PUT", "/api/v1/me/dnd", nil)
	deeDesk, _ := dee.Events(nil)
	a = enter(t, ann, adhoc, calltest.Options{})
	bobDesk.ExpectWhere("call.ring_started", is("call_id", a.CallID))
	bobPhone.ExpectWhere("call.ring_started", is("call_id", a.CallID))
	cyDesk.ExpectWhere("call.ring_started", is("call_id", a.CallID))
	if st := dee.Must(200, "GET", "/api/v1/status", nil); st["ringing_call_id"] != nil {
		t.Fatalf("someone in do not disturb was rung: %v", st)
	}
	// Do not disturb never blocks joining; and joining a call that is
	// already going rings nobody.
	d := enter(t, dee, adhoc, calltest.Options{})
	// Bob joins on his desk: his phone stops ringing.
	b := calltest.New(t, bobDesk, calltest.Options{})
	t.Cleanup(b.Close)
	id, tk := ticket(t, bob, adhoc)
	b.MustJoin(id, tk)
	if f := bobPhone.Expect("call.ring_stopped"); f["reason"] != "joined_elsewhere" {
		t.Fatalf("the other device when the person joins: %v", f)
	}
	// Cy declines on her desk: the participants are told.
	cyPhone, _ := cy.Token().Events(nil)
	c := calltest.New(t, cyDesk, calltest.Options{})
	t.Cleanup(c.Close)
	c.OK(map[string]any{"type": "call.decline", "call_id": a.CallID})
	if f := a.Expect("call.declined", nil); f["account_id"] != string(cy.ID) || f["call_id"] != a.CallID {
		t.Fatalf("call.declined: %v", f)
	}
	if st := cy.Must(200, "GET", "/api/v1/status", nil); st["ringing_call_id"] != nil {
		t.Fatalf("still ringing after declining: %v", st)
	}
	cyPhone.ExpectWhere("call.ring_stopped", is("reason", "declined_elsewhere"))
	deeDesk.Close()
	d.Leave()

	// The call ends while someone is still being rung.
	eve := ts.Member("eve")
	_, chatOut = ann.Do("POST", "/api/v1/chats", map[string]any{"account_ids": []string{string(eve.ID)}})
	direct := obj(chatOut, "conversation")["id"].(string)
	eveDesk, _ := eve.Events(nil)
	a2 := enter(t, ann.Token(), direct, calltest.Options{})
	eveDesk.ExpectWhere("call.ring_started", is("call_id", a2.CallID))
	a2.Leave()
	if f := eveDesk.Expect("call.ring_stopped"); f["reason"] != "ended" {
		t.Fatalf("a ring when the call ends: %v", f)
	}

	// What the push package was told: each person rung once per call, and
	// each ring's end.
	eventually(t, "the hooks have run", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(rang) == 4 && len(stopped) == 4
	})
	mu.Lock()
	defer mu.Unlock()
	for _, r := range rang {
		if r.FromAccountID != ann.ID || r.AccountID == dee.ID || r.AccountID == ann.ID || r.Until.IsZero() || r.CallID == "" || r.ConversationID == "" {
			t.Fatalf("a ring as the push package hears it: %+v", r)
		}
	}
	reasons := map[string]int{}
	for _, r := range stopped {
		reasons[r.Reason]++
	}
	if reasons["timeout"] != 1 || reasons["joined_elsewhere"] != 1 || reasons["declined_elsewhere"] != 1 || reasons["ended"] != 1 {
		t.Fatalf("how the rings ended: %v", reasons)
	}
	// Someone who blocked the starter is not rung.
	eve.Must(204, "PUT", "/api/v1/me/blocks/"+string(ann.ID), nil)
	a3 := enter(t, ann.Token(), direct, calltest.Options{})
	time.Sleep(200 * time.Millisecond)
	if st := eve.Must(200, "GET", "/api/v1/status", nil); st["ringing_call_id"] != nil {
		t.Fatalf("someone who blocked the starter was rung: %v", st)
	}
	a3.Leave()
}
