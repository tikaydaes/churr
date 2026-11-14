package calls

import (
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/model"
)

// Tickets: sixty seconds, one use, tied to the account, the session and the
// call (docs/CALLS.md section 2).
func TestTickets(t *testing.T) {
	tk := newTickets()
	now := time.Now()
	want := ticket{call: "call-1", account: "ann", session: "ses-1", expires: now.Add(time.Minute)}
	secret := tk.issue(want)
	if len(secret) != 43 {
		t.Fatalf("a ticket is 256 bits in base64url, 43 characters; got %d", len(secret))
	}
	if other := tk.issue(want); other == secret {
		t.Fatal("two tickets were the same")
	}
	got, ok := tk.use(secret, now)
	if !ok || got != want {
		t.Fatalf("a fresh ticket: %v %v", got, ok)
	}
	if _, ok := tk.use(secret, now); ok {
		t.Fatal("a ticket worked twice")
	}
	if _, ok := tk.use("not-a-ticket", now); ok {
		t.Fatal("an unknown ticket worked")
	}
	old := tk.issue(ticket{call: "call-1", account: "ann", expires: now.Add(time.Minute)})
	if _, ok := tk.use(old, now.Add(61*time.Second)); ok {
		t.Fatal("a ticket worked after it ran out")
	}
	// Running out also uses it up.
	if _, ok := tk.use(old, now); ok {
		t.Fatal("a ticket that was refused worked afterwards")
	}
	// Tickets that ran out are swept when another is issued.
	stale := newTickets()
	stale.issue(ticket{expires: now.Add(-time.Second)})
	stale.issue(ticket{expires: now.Add(time.Minute)})
	if len(stale.open) != 1 {
		t.Fatalf("%d tickets kept, want 1", len(stale.open))
	}
}

func camera(id string, height int, available ...string) wanted {
	w := wanted{id: id, height: height}
	for _, l := range cameraLayers {
		o := option{name: l.name, height: l.height, kbps: l.kbps}
		for _, a := range available {
			o.available = o.available || a == l.name
		}
		w.options = append(w.options, o)
	}
	return w
}

func all(id string, height int) wanted { return camera(id, height, "q", "h", "f") }

// Step 1 and 2 of docs/CALLS.md section 7: by size, then by what exists.
func TestLayerBySizeAndByWhatExists(t *testing.T) {
	now := time.Now()
	for _, c := range []struct {
		name  string
		w     wanted
		layer string
		state string
		why   string
	}{
		{"a 150-pixel tile gets q", all("t", 150), "q", stateForwarding, reasonSize},
		{"a 225-pixel tile still gets q: 180 is 80% of it", all("t", 225), "q", stateForwarding, reasonSize},
		{"a 226-pixel tile gets h", all("t", 226), "h", stateForwarding, reasonSize},
		{"a 400-pixel tile gets h", all("t", 400), "h", stateForwarding, reasonSize},
		{"a 451-pixel tile gets f", all("t", 451), "f", stateForwarding, reasonSize},
		{"a full-window tile gets f", all("t", 1080), "f", stateForwarding, reasonSize},
		{"taller than anything gets the tallest", all("t", 4000), "f", stateForwarding, reasonSize},
		{"a hidden tile gets the smallest", all("t", 0), "q", stateForwarding, reasonSize},
		{"a phone has no f: the nearest below", camera("t", 1080, "q", "h"), "h", stateForwarding, reasonPublisher},
		{"the uplink dropped h: the nearest below", camera("t", 400, "q", "f"), "q", stateForwarding, reasonPublisher},
		{"nothing below: the nearest above", camera("t", 150, "h", "f"), "h", stateForwarding, reasonPublisher},
		{"nothing is being sent", camera("t", 400), "", stateUnavailable, reasonPublisher},
		{"muted by the publisher", func() wanted { w := all("t", 400); w.muted = true; return w }(), "", stateMuted, reasonPublisher},
		{"a codec this client cannot decode", wanted{id: "t", height: 400, unavailable: true}, "", stateUnavailable, reasonPublisher},
		{"one layer, whatever size was wanted", wanted{id: "t", height: 90, options: []option{{name: "f", height: 720, kbps: 1200, available: true}}}, "f", stateForwarding, reasonSize},
	} {
		got := newPicker().pick([]wanted{c.w}, 0, now)["t"]
		if got.layer != c.layer || got.state != c.state || got.reason != c.why {
			t.Errorf("%s: got %+v, want %s %s %s", c.name, got, c.state, c.layer, c.why)
		}
	}
}

// Step 3: by bandwidth. Smallest tiles first, the current speaker last, a
// shared screen never; a track at its lowest is paused; back up one step
// every five seconds.
func TestLayerByBandwidth(t *testing.T) {
	now := time.Now()
	screen := wanted{id: "screen", height: 1080, screen: true, options: []option{{name: "f", height: 1080, kbps: screenKbps, available: true}}}
	speaker := all("speaker", 720)
	speaker.speaker = true
	list := []wanted{all("big", 720), all("small", 360), speaker, screen}
	layers := func(m map[string]choice) string {
		var b strings.Builder
		for _, id := range []string{"big", "small", "speaker", "screen"} {
			c := m[id]
			if c.state == statePaused {
				b.WriteString(id + "=paused ")
			} else {
				b.WriteString(id + "=" + c.layer + " ")
			}
		}
		return strings.TrimSpace(b.String())
	}
	p := newPicker()
	// Everything fits: f 1200 + h 500 + f 1200 + screen 2500 = 5400.
	if got := layers(p.pick(list, 6000, now)); got != "big=f small=h speaker=f screen=f" {
		t.Fatalf("plenty of room: %s", got)
	}
	// 5400 does not fit in 5000: the smallest tile goes down first, h to q
	// saves 350, not enough; then the big one, f to h, saves 700.
	got := p.pick(list, 5000, now)
	if s := layers(got); s != "big=h small=q speaker=f screen=f" {
		t.Fatalf("a little short: %s", s)
	}
	if got["small"].reason != reasonBandwidth || got["speaker"].reason != reasonSize {
		t.Fatalf("reasons: %+v", got)
	}
	// Much shorter: cameras pause before the screen is touched, and the
	// speaker goes last.
	if s := layers(p.pick(list, 2700, now)); s != "big=paused small=paused speaker=q screen=f" {
		t.Fatalf("very short: %s", s)
	}
	if s := layers(p.pick(list, 100, now)); s != "big=paused small=paused speaker=paused screen=f" {
		t.Fatalf("nothing left: the screen is never lowered: %s", s)
	}
	// The estimate recovers. Nothing comes back at once...
	p.lastRaise = now
	if s := layers(p.pick(list, 6000, now.Add(time.Second))); s != "big=paused small=paused speaker=paused screen=f" {
		t.Fatalf("recovered a second ago: %s", s)
	}
	// ...then one step every five seconds, in the reverse order: the
	// speaker first.
	steps := []string{
		"big=paused small=paused speaker=q screen=f",
		"big=paused small=paused speaker=h screen=f",
		"big=paused small=paused speaker=f screen=f",
		"big=q small=paused speaker=f screen=f",
		"big=h small=paused speaker=f screen=f",
		"big=f small=paused speaker=f screen=f",
		"big=f small=q speaker=f screen=f",
		"big=f small=h speaker=f screen=f",
		"big=f small=h speaker=f screen=f",
	}
	at := now
	for i, want := range steps {
		at = at.Add(5 * time.Second)
		if s := layers(p.pick(list, 6000, at)); s != want {
			t.Fatalf("step %d back up: got %s, want %s", i+1, s, want)
		}
		if s := layers(p.pick(list, 6000, at.Add(time.Second))); s != want {
			t.Fatalf("step %d moved again within five seconds: %s", i+1, s)
		}
	}
	if len(p.cap) != 0 {
		t.Fatalf("nothing should be held down any more: %v", p.cap)
	}
	// A step up is not taken if it would not fit.
	p = newPicker()
	p.pick([]wanted{all("big", 720)}, 400, now) // down to q
	if c := p.pick([]wanted{all("big", 720)}, 400, now.Add(time.Minute))["big"]; c.layer != "q" {
		t.Fatalf("raised into an estimate it does not fit: %+v", c)
	}
	// No estimate yet: nothing is held down.
	if c := newPicker().pick([]wanted{all("big", 720)}, 0, now)["big"]; c.layer != "f" {
		t.Fatalf("without an estimate: %+v", c)
	}
	// A track that leaves the list is forgotten.
	p.pick(nil, 400, now)
	if len(p.cap) != 0 {
		t.Fatalf("a track no longer subscribed is still held down: %v", p.cap)
	}
}

func TestVideoBudget(t *testing.T) {
	if got := videoBudget(0, 5); got != 0 {
		t.Fatalf("no estimate: %d", got)
	}
	// 2 Mbit/s, less four microphones at 32, less a tenth held back.
	if got := videoBudget(2_000_000, 4); got != 2000-128-200 {
		t.Fatalf("budget: %d", got)
	}
	if got := videoBudget(50_000, 24); got != 1 {
		t.Fatalf("an estimate audio alone fills leaves nothing for video: %d", got)
	}
}

// Active-speaker detection (docs/CALLS.md section 8).
func TestSpeakers(t *testing.T) {
	const ann, bob, cy, dee = model.AccountID("ann"), model.AccountID("bob"), model.AccountID("cy"), model.AccountID("dee")
	start := time.Unix(1_800_000_000, 0)
	s := newSpeakers()
	// talk feeds one account's packets, one every 20 ms, at a level.
	talk := func(who model.AccountID, level uint8, from, to time.Duration) {
		for at := from; at < to; at += 20 * time.Millisecond {
			s.observe(who, level, start.Add(at))
		}
	}
	ms := func(n int) time.Duration { return time.Duration(n) * time.Millisecond }
	ids := func(list []model.AccountID) string {
		parts := make([]string, len(list))
		for i, id := range list {
			parts[i] = string(id)
		}
		return strings.Join(parts, ",")
	}

	// Louder than -40 dBov for one window is not yet speaking.
	talk(ann, 20, 0, ms(320))
	if got := s.current(start.Add(ms(320))); len(got) != 0 {
		t.Fatalf("after one loud window: %v", got)
	}
	// Two windows running is.
	talk(ann, 20, ms(320), ms(620))
	if got := ids(s.current(start.Add(ms(620)))); got != "ann" {
		t.Fatalf("after two loud windows: %q", got)
	}
	// Quieter than the threshold never counts, however long.
	talk(bob, 55, 0, ms(1500))
	if got := ids(s.current(start.Add(ms(1500)))); strings.Contains(got, "bob") {
		t.Fatalf("someone below the threshold is a speaker: %q", got)
	}
	// A loud window, a quiet one, a loud one: not two running.
	talk(cy, 10, 0, ms(300))
	talk(cy, 90, ms(300), ms(600))
	talk(cy, 10, ms(600), ms(900))
	talk(cy, 90, ms(900), ms(920))
	if got := ids(s.current(start.Add(ms(920)))); strings.Contains(got, "cy") {
		t.Fatalf("loud, quiet, loud counted as speaking: %q", got)
	}

	// Ann stops at 620 ms. She is still a speaker a second later, and not
	// after 1.5 seconds below the threshold.
	if got := ids(s.current(start.Add(ms(1600)))); got != "ann" {
		t.Fatalf("one second after stopping: %q", got)
	}
	if got := s.current(start.Add(ms(2200))); len(got) != 0 {
		t.Fatalf("more than 1.5 seconds after stopping: %v", got)
	}

	// Loudest first, three at most.
	s = newSpeakers()
	talk(ann, 30, 0, ms(700))
	talk(bob, 10, 0, ms(700))
	talk(cy, 20, 0, ms(700))
	talk(dee, 35, 0, ms(700))
	if got := ids(s.current(start.Add(ms(700)))); got != "bob,cy,ann" {
		t.Fatalf("loudest first, three at most: %q", got)
	}

	// Reporting: when the set changes, at most three times a second.
	s = newSpeakers()
	if _, changed := s.changed(start); changed {
		t.Fatal("nobody speaking, and a report")
	}
	talk(ann, 20, 0, ms(700))
	list, changed := s.changed(start.Add(ms(700)))
	if !changed || ids(list) != "ann" {
		t.Fatalf("the first report: %v %v", list, changed)
	}
	if s.loudest() != ann {
		t.Fatalf("the current speaker: %q", s.loudest())
	}
	if _, changed := s.changed(start.Add(ms(720))); changed {
		t.Fatal("the same speakers reported twice")
	}
	talk(bob, 5, ms(100), ms(800))
	if _, changed := s.changed(start.Add(ms(800))); changed {
		t.Fatal("a second report within a third of a second")
	}
	list, changed = s.changed(start.Add(ms(1040)))
	if !changed || ids(list) != "bob,ann" {
		t.Fatalf("the second report: %v %v", list, changed)
	}
	// A muted microphone is forgotten at once.
	s.forget(bob)
	if got := ids(s.current(start.Add(ms(1050)))); got != "ann" {
		t.Fatalf("after bob muted: %q", got)
	}
}

// What the forwarder reads of a VP8 payload, and what it rewrites.
func TestVP8Descriptor(t *testing.T) {
	// No extension: one byte, then the frame.
	d, ok := parseVP8([]byte{0x10, 0x00, 0xaa})
	if !ok || d.size != 1 || !d.start || !d.keyframe || d.hasPicture || d.hasTL0 {
		t.Fatalf("the shortest descriptor, key frame: %+v", d)
	}
	d, _ = parseVP8([]byte{0x10, 0x01})
	if d.keyframe {
		t.Fatal("a frame with the P bit set is not a key frame")
	}
	// Not the first packet of a frame: no frame header to read.
	d, _ = parseVP8([]byte{0x00, 0x00})
	if d.start || d.keyframe {
		t.Fatalf("a later packet of a frame: %+v", d)
	}
	// Everything: 15-bit picture number, TL0PICIDX, TID and KEYIDX.
	full := []byte{0x90, 0xf0, 0x80 | 0x12, 0x34, 0x07, 0x60, 0x00, 0xbb}
	d, ok = parseVP8(full)
	if !ok || d.size != 6 || !d.wide || d.picture != 0x1234 || !d.hasTL0 || d.tl0 != 7 || !d.keyframe {
		t.Fatalf("the full descriptor: %+v", d)
	}
	rewriteVP8(full, d, 0x7fff, 200)
	again, _ := parseVP8(full)
	if again.picture != 0x7fff || again.tl0 != 200 || full[5] != 0x60 || full[6] != 0x00 || full[7] != 0xbb {
		t.Fatalf("after rewriting: %+v %x", again, full)
	}
	// A 7-bit picture number keeps its width.
	short := []byte{0x90, 0x80, 0x11, 0x01}
	d, _ = parseVP8(short)
	if d.wide || d.picture != 0x11 || d.size != 3 || d.keyframe {
		t.Fatalf("a 7-bit picture number: %+v", d)
	}
	rewriteVP8(short, d, 0x7f, 0)
	if short[2] != 0x7f || short[3] != 0x01 {
		t.Fatalf("rewritten: %x", short)
	}
	// Flags that promise more than the payload holds.
	for _, bad := range [][]byte{{}, {0x90}, {0x90, 0x80}, {0x90, 0x80, 0x80}, {0x90, 0x40}, {0x90, 0x20}} {
		if _, ok := parseVP8(bad); ok {
			t.Errorf("a truncated descriptor was accepted: %x", bad)
		}
	}
}

func TestH264Keyframe(t *testing.T) {
	for _, c := range []struct {
		name string
		p    []byte
		key  bool
	}{
		{"an IDR slice", []byte{0x65, 0x88}, true},
		{"a sequence parameter set", []byte{0x67, 0x42}, true},
		{"an ordinary slice", []byte{0x41, 0x9a}, false},
		{"STAP-A holding SPS, PPS and IDR", []byte{0x78, 0x00, 0x02, 0x67, 0x42, 0x00, 0x01, 0x68, 0x00, 0x02, 0x65, 0x88}, true},
		{"STAP-A holding only ordinary slices", []byte{0x78, 0x00, 0x02, 0x41, 0x9a, 0x00, 0x02, 0x41, 0x9b}, false},
		{"FU-A, the start of an IDR slice", []byte{0x7c, 0x85, 0x88}, true},
		{"FU-A, the middle of an IDR slice", []byte{0x7c, 0x05, 0x88}, false},
		{"FU-A, the start of an ordinary slice", []byte{0x7c, 0x81, 0x9a}, false},
		{"empty", nil, false},
		{"a truncated FU-A", []byte{0x7c}, false},
	} {
		if got := h264Keyframe(c.p); got != c.key {
			t.Errorf("%s: got %v", c.name, got)
		}
	}
}

// The server's candidates are fixed and are in every SDP it sends; a
// client's candidates are never taken from its SDP.
func TestCandidates(t *testing.T) {
	sdp := "v=0\r\nm=audio 9 UDP/TLS/RTP/SAVPF 111\r\na=ice-ufrag:abcd\r\na=ice-pwd:secret\r\na=candidate:1 1 udp 1 10.0.0.5 4444 typ host\r\na=end-of-candidates\r\na=mid:0\r\n" +
		"m=video 9 UDP/TLS/RTP/SAVPF 96\r\na=ice-ufrag:abcd\r\na=ice-pwd:secret\r\na=mid:1\r\n"
	out := withCandidates(sdp, []netip.Addr{netip.MustParseAddr("203.0.113.7"), netip.MustParseAddr("2001:db8::1")}, 24877)
	if strings.Contains(out, "10.0.0.5") {
		t.Fatal("a gathered candidate was left in")
	}
	for _, want := range []string{
		"203.0.113.7 24877 typ host\r\n", "203.0.113.7 24877 typ host tcptype passive\r\n",
		"2001:db8::1 24877 typ host\r\n", "2001:db8::1 24877 typ host tcptype passive\r\n",
	} {
		if strings.Count(out, want) != 2 {
			t.Errorf("each media section should carry %q once:\n%s", want, out)
		}
	}
	if strings.Count(out, "a=end-of-candidates") != 2 || !strings.HasSuffix(out, "\r\n") {
		t.Fatalf("the rewritten SDP:\n%s", out)
	}
	// UDP is preferred to TCP, and the first address to the second.
	if strings.Index(out, " udp ") > strings.Index(out, " tcp ") {
		t.Fatal("TCP is listed before UDP")
	}

	client := "v=0\na=ice-lite\nm=audio 9 UDP/TLS/RTP/SAVPF 111\na=candidate:1 1 udp 1 198.51.100.9 5555 typ host\na=end-of-candidates\na=mid:0\n"
	got := withoutCandidates(client)
	if strings.Contains(got, "candidate") || strings.Contains(got, "ice-lite") || !strings.Contains(got, "a=mid:0\r\n") {
		t.Fatalf("a client's SDP after cleaning:\n%s", got)
	}
}

// The limit on a subscriber's downlink comes from the loss it reports, not
// from an estimate of capacity (downlink.go).
func TestDownlink(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	var d downlink
	var sent int64
	// step moves on by a duration at a sending rate, in kbit/s, with a
	// share of packets reported lost.
	step := func(dur time.Duration, kbps int, loss float64) int {
		now = now.Add(dur)
		sent += int64(kbps) * 1000 / 8 * int64(dur) / int64(time.Second)
		return d.update(now, sent, loss)
	}
	// A clear path: no limit, whatever is sent. A little loss is not
	// congestion.
	for i := 0; i < 10; i++ {
		if got := step(time.Second, 4000, 0.05); got != 0 {
			t.Fatalf("a path losing a twentieth is held to %d", got)
		}
	}
	// More than a tenth lost: a little less than what is being sent.
	if got := step(time.Second, 4000, 0.2); got != 3_400_000 {
		t.Fatalf("the first cut: %d", got)
	}
	// Not again at once: the cut must be given time to show.
	if got := step(600*time.Millisecond, 3000, 0.2); got != 3_400_000 {
		t.Fatalf("a second cut too soon: %d", got)
	}
	// Still losing later: cut from what is sent now, which is less.
	if got := step(time.Second, 3000, 0.2); got != 2_550_000 {
		t.Fatalf("the second cut: %d", got)
	}
	// Sending more than the limit does not raise a cut above the limit.
	if got := step(2*time.Second, 9000, 0.2); got != int(0.85*2_550_000) {
		t.Fatalf("a cut while sending over the limit: %d", got)
	}
	// Never below the floor.
	d.limit = 310_000
	if got := step(2*time.Second, 50, 0.5); got != downlinkFloor {
		t.Fatalf("the floor: %d", got)
	}
	// Between the two marks the limit stays where it is.
	d.limit = 1_000_000
	if got := step(time.Second, 800, 0.05); got != 1_000_000 {
		t.Fatalf("holding: %d", got)
	}
	// A clear path again: the limit grows, eight percent a second and a
	// fixed step.
	if got := step(time.Second, 800, 0.01); got != 1_180_000 {
		t.Fatalf("growing: %d", got)
	}
	// And is dropped once it has grown past anything a call sends.
	d.limit = 29_000_000
	if got := step(time.Second, 800, 0); got != 0 {
		t.Fatalf("a limit past the top should go: %d", got)
	}
	// Looked at no more than twice a second.
	d.limit = 1_000_000
	if got := step(100*time.Millisecond, 800, 0.5); got != 1_000_000 {
		t.Fatalf("looked at too often: %d", got)
	}
}
