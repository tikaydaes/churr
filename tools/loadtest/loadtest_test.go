package main

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"github.com/tikaydaes/churr/internal/wirepb"
	"math/rand"
	"net"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestShards(t *testing.T) {
	members := make([]fileMember, 1003)
	for i := range members {
		members[i].ID = string(rune('a'+i%26)) + "-" + strings.Repeat("x", i%7) + "-" + string(rune('0'+i%10)) + "-" + time.Duration(i).String()
	}
	for _, online := range []int{1, 2, 500, 1003} {
		for _, n := range []int{1, 2, 3, 7} {
			seen := map[string]int{}
			smallest, largest := len(members), 0
			for i := 1; i <= n; i++ {
				part := shardOf(members, online, i, n)
				smallest, largest = min(smallest, len(part)), max(largest, len(part))
				for _, m := range part {
					seen[m.ID]++
				}
			}
			if len(seen) != online {
				t.Errorf("online %d in %d shards: %d members covered", online, n, len(seen))
			}
			for id, times := range seen {
				if times != 1 {
					t.Errorf("online %d in %d shards: %s is in %d shards", online, n, id, times)
				}
			}
			if largest-smallest > 1 {
				t.Errorf("online %d in %d shards: sizes from %d to %d", online, n, smallest, largest)
			}
			// Only the first `online` members are anyone's.
			for _, m := range members[online:] {
				if seen[m.ID] != 0 {
					t.Errorf("online %d: a member past the online ones is in a shard", online)
				}
			}
		}
	}
}

func TestParseShard(t *testing.T) {
	if i, n, err := parseShard("2/3"); err != nil || i != 2 || n != 3 {
		t.Errorf("2/3: %d %d %v", i, n, err)
	}
	for _, bad := range []string{"", "0/3", "4/3", "1", "1/0", "a/b", "1/3x", "-1/3"} {
		if _, _, err := parseShard(bad); err == nil {
			t.Errorf("%q was taken", bad)
		}
	}
}

func TestOnlineCount(t *testing.T) {
	for _, c := range []struct {
		n     int
		share float64
		want  int
	}{{500, 1, 500}, {500, 0.1, 50}, {50000, 0.3, 15000}, {3, 0.01, 1}, {7, 0.5, 4}} {
		if got := onlineCount(c.n, c.share); got != c.want {
			t.Errorf("%d members, %g online: %d, want %d", c.n, c.share, got, c.want)
		}
	}
}

func TestChannelsFor(t *testing.T) {
	in := channelsFor(5000, 200, 5)
	size := make([]int, 200)
	for i, cs := range in {
		if len(cs) != 5 {
			t.Fatalf("member %d is in %d channels", i, len(cs))
		}
		seen := map[int]bool{}
		for _, c := range cs {
			if seen[c] {
				t.Fatalf("member %d is in channel %d twice", i, c)
			}
			seen[c] = true
			size[c]++
		}
	}
	// About 125 each.
	for c, n := range size {
		if n < 70 || n > 190 {
			t.Errorf("channel %d has %d members", c, n)
		}
	}
	if got := channelsFor(10, 2, 5); len(got[0]) != 2 {
		t.Errorf("more channels for a member than there are: %v", got[0])
	}
	if got := channelsFor(10, 0, 3); len(got[0]) != 0 {
		t.Errorf("channels where there are none: %v", got[0])
	}
}

func TestMembersFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "members.json")
	f := &membersFile{Version: membersFileVersion, Prefix: "p", PerMember: 2, General: "g", Channels: []string{"c0", "c1", "c2"},
		Members: []fileMember{{ID: "a", Token: "ta", In: []int{0, 2}}, {ID: "b", Token: "tb", In: []int{1, 2}}, {ID: "c", Token: "tc", In: []int{}}}}
	if err := f.write(path); err != nil {
		t.Fatal(err)
	}
	got, err := readMembersFile(path)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := json.Marshal(f)
	b, _ := json.Marshal(got)
	if !bytes.Equal(a, b) {
		t.Errorf("read back\n%s\nwrote\n%s", b, a)
	}
	ms := membersOf(got, shardOf(got.Members, 2, 1, 1))
	if len(ms) != 2 || strings.Join(ms[0].convs, ",") != "g,c0,c2" || strings.Join(ms[1].convs, ",") != "g,c1,c2" || ms[1].token != "tb" || len(ms[0].pos) != 3 {
		t.Errorf("members from the file: %+v %+v", ms[0], ms[1])
	}

	// A file that would send a generator wrong is refused.
	f.Members[0].In = []int{3}
	f.write(path)
	if _, err := readMembersFile(path); err == nil {
		t.Error("a member in a channel that is not there was read")
	}
	f.Members[0].In = nil
	f.Version = 99
	f.write(path)
	if _, err := readMembersFile(path); err == nil {
		t.Error("a file of another version was read")
	}
}

func TestHistBuckets(t *testing.T) {
	// Every value lands in a bucket whose top is at or above it and within
	// 1.6%, and the buckets are in order.
	last := -1
	for _, us := range []int64{0, 1, 63, 64, 65, 127, 128, 129, 1000, 4095, 4096, 123456, 1e6, 59999999, 3.6e9} {
		i := bucketOf(us)
		if i < last {
			t.Errorf("%d µs is in bucket %d, below a smaller value's %d", us, i, last)
		}
		last = i
		top := bucketTop(i)
		if top < us || float64(top-us) > 0.016*float64(us)+0.5 {
			t.Errorf("%d µs is in bucket %d, whose top is %d", us, i, top)
		}
		if i > 0 && bucketTop(i-1) >= us {
			t.Errorf("%d µs is in bucket %d but fits the one before (top %d)", us, i, bucketTop(i-1))
		}
	}
	if i := bucketOf(1 << 62); i != histBuckets-1 {
		t.Errorf("a huge value is in bucket %d", i)
	}
}

// exact is the percentile of sorted samples as the report defines it.
func exact(sorted []time.Duration, p float64) time.Duration {
	return sorted[min(len(sorted)-1, int(p*float64(len(sorted))))]
}

func TestHistMerge(t *testing.T) {
	// Three generators see very different latencies. The percentiles of
	// the merged histogram are those of all the samples together, which the
	// average of the three generators' percentiles is nowhere near.
	rng := rand.New(rand.NewSource(7))
	var all []time.Duration
	parts := []*hist{{}, {}, {}}
	scale := []float64{1, 20, 400} // milliseconds
	count := []int{50000, 5000, 500}
	for g, h := range parts {
		for i := 0; i < count[g]; i++ {
			d := time.Duration(rng.ExpFloat64() * scale[g] * float64(time.Millisecond))
			h.add(d)
			all = append(all, d)
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i] < all[j] })

	// Through the file format, as -merge reads them.
	merged := &hist{}
	for _, h := range parts {
		data, err := json.Marshal(h)
		if err != nil {
			t.Fatal(err)
		}
		back := &hist{}
		if err := json.Unmarshal(data, back); err != nil {
			t.Fatal(err)
		}
		if back.count() != h.count() || back.quantile(0.99) != h.quantile(0.99) || back.max.Load() != h.max.Load() || back.min.Load() != h.min.Load() || back.sum.Load() != h.sum.Load() {
			t.Fatal("a histogram came back from its file changed")
		}
		merged.merge(back)
	}
	if merged.count() != int64(len(all)) {
		t.Fatalf("merged %d samples of %d", merged.count(), len(all))
	}
	for _, p := range []float64{0.5, 0.95, 0.99, 0.999} {
		want, got := exact(all, p), merged.quantile(p)
		if got < want.Truncate(time.Microsecond) || float64(got-want) > 0.016*float64(want)+float64(time.Microsecond) {
			t.Errorf("p%g of the merged histogram is %s; of all samples, %s", p*100, got, want)
		}
	}
	averaged := (parts[0].quantile(0.99) + parts[1].quantile(0.99) + parts[2].quantile(0.99)) / 3
	if p99 := merged.quantile(0.99); averaged < 2*p99 {
		t.Errorf("this test no longer tells merging from averaging: p99 merged %s, averaged %s", p99, averaged)
	}
	if got, want := time.Duration(merged.max.Load())*time.Microsecond, all[len(all)-1].Truncate(time.Microsecond); got != want {
		t.Errorf("max %s, want %s", got, want)
	}
	if got := merged.quantile(1); got != time.Duration(merged.max.Load())*time.Microsecond {
		t.Errorf("p100 %s is not the max", got)
	}

	// Merging in an empty one changes nothing; a file that contradicts
	// itself is refused.
	before := merged.quantile(0.5)
	merged.merge(&hist{})
	if merged.quantile(0.5) != before || merged.count() != int64(len(all)) {
		t.Error("an empty histogram changed the merge")
	}
	if err := json.Unmarshal([]byte(`{"count":3,"buckets_per_power_of_two":64,"buckets":[[5,1]]}`), &hist{}); err == nil {
		t.Error("a histogram whose buckets do not add up was read")
	}
	var empty hist
	if empty.quantile(0.5) != 0 {
		t.Error("an empty histogram has a median")
	}
}

func resultOf(shard, of int) *result {
	r := newResult()
	r.Shard, r.Of, r.StartAt = shard, of, 1000
	r.Shape = shape{Members: 100, Online: 60, Channels: 2, PerMember: 1, Rate: 3, Duration: time.Minute}
	r.Generators = []genStats{{Shard: shard, Of: of, Host: "h", CPUs: 8}}
	return r
}

func TestMergeResults(t *testing.T) {
	a, b, c := resultOf(1, 3), resultOf(2, 3), resultOf(3, 3)
	// Generator 1 posted twice in #general and once in a channel only
	// generators 2 and 3 have members of.
	a.Posted, a.PostsByConv = 3, map[string]int64{"general": 2, "x": 1}
	a.Connected, a.ConnectedByConv = 20, map[string]int64{"general": 20}
	b.Connected, b.ConnectedByConv = 20, map[string]int64{"general": 20, "x": 7}
	c.Connected, c.ConnectedByConv = 19, map[string]int64{"general": 19, "x": 5}
	c.ConnectFailed = 1
	a.Received, b.Received, c.Received = 40, 47, 43
	a.Lost["closed by the server: 4009 slow"] = 2
	c.Lost["closed by the server: 4009 slow"] = 1
	c.Lost["reset"] = 4
	a.Delivery.add(time.Millisecond)
	b.Delivery.add(3 * time.Millisecond)
	c.Delivery.add(900 * time.Millisecond)
	b.Storm = newStormResult()
	b.Storm.AllReady, b.Storm.Posted, b.Storm.PostsByConv = 4*time.Second, 1, map[string]int64{"x": 1}
	c.Storm = newStormResult()
	c.Storm.AllReady = 9 * time.Second

	m, notes, err := merge([]*result{c, a, b})
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 0 {
		t.Errorf("notes for a whole run: %v", notes)
	}
	if m.Shard != 0 || m.Of != 3 || m.Posted != 3 || m.Connected != 59 || m.ConnectFailed != 1 || m.Received != 130 {
		t.Errorf("merged counts: %+v", m)
	}
	// 2 in #general to 59 connections, 1 in x to 12.
	if got := expected(m.PostsByConv, m.ConnectedByConv); got != 2*59+12 {
		t.Errorf("expected deliveries %d", got)
	}
	if m.Lost["closed by the server: 4009 slow"] != 3 || m.Lost["reset"] != 4 {
		t.Errorf("connections lost: %v", m.Lost)
	}
	if p50 := m.Delivery.quantile(0.5); m.Delivery.count() != 3 || p50 < 3*time.Millisecond || p50 > 3050*time.Microsecond {
		t.Errorf("merged delivery: n=%d p50=%s", m.Delivery.count(), m.Delivery.quantile(0.5))
	}
	if m.Storm == nil || m.Storm.AllReady != 9*time.Second || expected(m.Storm.PostsByConv, m.ConnectedByConv) != 12 {
		t.Errorf("merged storm: %+v", m.Storm)
	}
	if len(m.Generators) != 3 || m.Generators[0].Shard != 1 || m.Generators[2].Shard != 3 {
		t.Errorf("generators: %+v", m.Generators)
	}

	// Through files, as -merge does it.
	dir := t.TempDir()
	var again []*result
	for i, r := range []*result{a, b, c} {
		path := filepath.Join(dir, string(rune('a'+i))+".json")
		if err := r.write(path); err != nil {
			t.Fatal(err)
		}
		back, err := readResult(path)
		if err != nil {
			t.Fatal(err)
		}
		again = append(again, back)
	}
	m2, _, err := merge(again)
	if err != nil {
		t.Fatal(err)
	}
	j1, _ := json.Marshal(m)
	j2, _ := json.Marshal(m2)
	if !bytes.Equal(j1, j2) {
		t.Errorf("merging the files gave\n%s\nand merging what was written to them\n%s", j2, j1)
	}

	// What does not fit together is refused or said.
	if _, notes, _ := merge([]*result{a, b}); len(notes) != 1 || !strings.Contains(notes[0], "missing: [3]") {
		t.Errorf("a run with a generator missing: %v", notes)
	}
	if _, _, err := merge([]*result{a, a}); err == nil {
		t.Error("the same shard twice was merged")
	}
	other := resultOf(2, 3)
	other.Shape.Rate = 4
	if _, _, err := merge([]*result{a, other}); err == nil {
		t.Error("runs of different shapes were merged")
	}
	if _, _, err := merge([]*result{m}); err == nil {
		t.Error("a merged result was merged again")
	}
	late := resultOf(2, 3)
	late.StartAt = 5000
	if _, notes, _ := merge([]*result{a, late, c}); len(notes) != 1 || !strings.Contains(notes[0], "-start-at") {
		t.Errorf("generators with different starts: %v", notes)
	}
}

const createdFrame = `{"at":1763174400000,"conversation_id":"01a1dd53-0686-778d-a90e-5c7ab74f2f40","message":{"id":"m1","seq":41,"author":{"name":"a \"quoted\" } name"},"text":"loadtest 1763174400123456789 3 from 01a1","reactions":[{"emoji":"{","n":1}]},"seq":42,"type":"message.created"}`

func TestScanFrame(t *testing.T) {
	h, ok := scanFrame([]byte(createdFrame))
	if !ok || string(h.typ) != "message.created" || string(h.conv) != "01a1dd53-0686-778d-a90e-5c7ab74f2f40" || h.seq != 42 {
		t.Fatalf("scanned %q %q %d %v", h.typ, h.conv, h.seq, ok)
	}
	nanos, shard, ok := sentAt(h.message, false)
	if !ok || nanos != 1763174400123456789 || shard != 3 {
		t.Errorf("sent at %d by %d, %v", nanos, shard, ok)
	}
	// What this program posts is what it reads back.
	text := postText(nil, 1763174400123456789, 12, "someone")
	if n, s, ok := sentAt([]byte(`{"text":"`+string(text)+`"}`), false); !ok || n != 1763174400123456789 || s != 12 {
		t.Errorf("%s read back as %d %d %v", text, n, s, ok)
	}
	for frame, typ := range map[string]string{
		`{"type":"ready"}`: "ready",
		` { "type" : "hello" , "heartbeat_seconds" : 30 } `:  "hello",
		`{"type":"presence.changed","account_id":"x","n":1}`: "presence.changed",
		`{}`: "",
	} {
		if h, ok := scanFrame([]byte(frame)); !ok || string(h.typ) != typ {
			t.Errorf("%s: type %q, %v", frame, h.typ, ok)
		}
	}
	// A message that only talks about the test is not one of its own, and
	// a nested seq is not the event's.
	h, _ = scanFrame([]byte(`{"message":{"seq":9,"text":"about loadtest 5 1 from x"},"type":"message.created"}`))
	if _, _, ok := sentAt(h.message, false); ok || h.seq != 0 {
		t.Errorf("another message was taken for the test's own (seq %d)", h.seq)
	}
	for _, bad := range []string{``, `[1]`, `{"type":"x"`, `{"type":}`, `{"a":"unterminated}`, `{"a":{"b":1}`, `nonsense`} {
		if _, ok := scanFrame([]byte(bad)); ok {
			t.Errorf("%q was read as a frame", bad)
		}
	}
}

// A frame is read, checked and timed without allocating: a generator does
// this a hundred thousand times a second.
func TestFrameWithoutAllocating(t *testing.T) {
	g := &generator{res: newResult(), cfg: config{shard: 3, of: 3}}
	m := &member{convs: []string{"other", "01a1dd53-0686-778d-a90e-5c7ab74f2f40"}, pos: make([]int64, 2)}
	frame := []byte(createdFrame)
	allocs := testing.AllocsPerRun(1000, func() {
		m.pos[1] = 41
		g.arrived(m, frame)
	})
	if allocs != 0 {
		t.Errorf("taking a frame allocates %.1f times", allocs)
	}
	if g.res.Delivery.count() == 0 || g.res.DeliveryOwn.count() != g.res.Delivery.count() || g.gaps.Load() != 0 || g.repeats.Load() != 0 || m.pos[1] != 42 {
		t.Errorf("deliveries %d (own %d), gaps %d, repeats %d, position %d", g.res.Delivery.count(), g.res.DeliveryOwn.count(), g.gaps.Load(), g.repeats.Load(), m.pos[1])
	}
	// Out of order is noticed.
	m.pos[1] = 40
	g.arrived(m, frame)
	m.pos[1] = 42
	g.arrived(m, frame)
	if g.gaps.Load() != 1 || g.repeats.Load() != 1 {
		t.Errorf("gaps %d, repeats %d", g.gaps.Load(), g.repeats.Load())
	}
}

// The same of a frame that came as protobuf: the envelope says what the
// generator wants to know, and the body is searched, not decoded.
func TestProtobufFrameWithoutAllocating(t *testing.T) {
	const conv = "01a1dd53-0686-778d-a90e-5c7ab74f2f40"
	text := postText(nil, time.Now().UnixNano()-int64(time.Millisecond), 3, "someone")
	frame, typed, why := wirepb.EncodeFrameJSON([]byte(`{"type":"message.created","at":"2026-11-13T09:30:00.000Z","conversation_id":"` + conv +
		`","seq":42,"message":{"id":"0193a6f2-7c1e-7b3a-9f0e-2a4d5c6b7e8f","conversation_id":"` + conv + `","seq":42,"text":"` + string(text) + `"}}`))
	if !typed {
		t.Fatal(why)
	}
	var room [36]byte
	h, ok := scanAny(true, frame, &room)
	if !ok || string(h.typ) != "message.created" || string(h.conv) != conv || h.seq != 42 || !h.packed {
		t.Fatalf("scanned %q %q %d %v", h.typ, h.conv, h.seq, ok)
	}
	if _, shard, ok := sentAt(h.message, true); !ok || shard != 3 {
		t.Fatalf("the post's moment was not found in the body")
	}
	g := &generator{res: newResult(), cfg: config{shard: 3, of: 3, proto: true}}
	m := &member{convs: []string{"other", conv}, pos: make([]int64, 2)}
	allocs := testing.AllocsPerRun(1000, func() {
		m.pos[1] = 41
		g.arrived(m, frame)
	})
	if allocs != 0 {
		t.Errorf("taking a protobuf frame allocates %.1f times", allocs)
	}
	if g.res.Delivery.count() == 0 || g.gaps.Load() != 0 || g.repeats.Load() != 0 || m.pos[1] != 42 {
		t.Errorf("deliveries %d, gaps %d, repeats %d, position %d", g.res.Delivery.count(), g.gaps.Load(), g.repeats.Load(), m.pos[1])
	}
	// The frames the generator sends have a protobuf form, and one the
	// schema cannot hold is read through the JSON it travels as.
	for _, f := range []string{`{"type":"ping"}`, `{"type":"activity"}`, `{"type":"resume","positions":{"` + conv + `":7},"open":["` + conv + `"]}`, `{"type":"resume","positions":{},"open":[]}`} {
		if _, typed, why := wirepb.EncodeFrameJSON([]byte(f)); !typed {
			t.Errorf("%s: %v", f, why)
		}
	}
	odd := wirepb.EncodeFrame(map[string]any{"type": "ready", "something_new": 1})
	if h, ok := scanAny(true, odd, &room); !ok || string(h.typ) != "ready" {
		t.Errorf("a frame carried as JSON was read as %q, %v", h.typ, ok)
	}
	if body, err := postBody([]byte(`{"text":"hello"}`)); err != nil || len(body) < 2 || body[0] != 0x1f || body[1] != 0x8b {
		t.Errorf("a post's body is not gzip: %x %v", body, err)
	}
}

// serverFrame is a frame as a server sends it: not masked.
func serverFrame(fin bool, opcode byte, payload []byte) []byte {
	b := []byte{opcode}
	if fin {
		b[0] |= 0x80
	}
	switch {
	case len(payload) < 126:
		b = append(b, byte(len(payload)))
	case len(payload) < 1<<16:
		b = binary.BigEndian.AppendUint16(append(b, 126), uint16(len(payload)))
	default:
		b = binary.BigEndian.AppendUint64(append(b, 127), uint64(len(payload)))
	}
	return append(b, payload...)
}

func TestWSRead(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	small := []byte(`{"type":"hello"}`)
	medium := bytes.Repeat([]byte("m"), 3000)
	large := bytes.Repeat([]byte("L"), 70000)
	go func() {
		// A pong the client need not answer, then messages of every size,
		// one of them in pieces, then the close.
		var out []byte
		out = append(out, serverFrame(true, 10, []byte("pong"))...)
		out = append(out, serverFrame(true, 1, small)...)
		out = append(out, serverFrame(true, 1, medium)...)
		out = append(out, serverFrame(true, 1, large)...)
		out = append(out, serverFrame(false, 1, []byte("in "))...)
		out = append(out, serverFrame(false, 0, []byte("three "))...)
		out = append(out, serverFrame(true, 0, []byte("pieces"))...)
		out = append(out, serverFrame(true, 1, small)...)
		out = append(out, serverFrame(true, 1, nil)...)
		out = append(out, serverFrame(true, 8, append([]byte{0x0f, 0xa9}, "slow"...))...) // 4009
		server.Write(out)
	}()
	c := &wsConn{conn: client, r: bufio.NewReaderSize(client, readBuffer)}
	for i, want := range [][]byte{small, medium, large, []byte("in three pieces"), small, {}} {
		got, err := c.read()
		if err != nil {
			t.Fatalf("message %d: %v", i, err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("message %d: read %d bytes %.20q, want %d bytes %.20q", i, len(got), got, len(want), want)
		}
	}
	_, err := c.read()
	if !errors.Is(err, errClosed) || closeReason(err) != "closed by the server: 4009 slow" {
		t.Errorf("the close read as %v (%q)", err, closeReason(err))
	}
}

// -viewing: at 1 nobody says what they have open; below it everyone does,
// and that share of the members names #general.
func TestViewingShare(t *testing.T) {
	g := &generator{general: "gen", cfg: config{viewing: 1}}
	for i := 0; i < 200; i++ {
		g.members = append(g.members, &member{})
	}
	g.markViewers()
	if got := string(g.firstResume(g.members[0])); got != `{"type":"resume","positions":{}}` {
		t.Fatalf("at -viewing 1 the resume frame is %s", got)
	}
	g.cfg.viewing = 0.05
	g.markViewers()
	viewers := 0
	for _, m := range g.members {
		want := `{"type":"resume","positions":{},"open":[]}`
		if m.viewer {
			viewers++
			want = `{"type":"resume","positions":{},"open":["gen"]}`
		}
		if got := string(g.firstResume(m)); got != want {
			t.Fatalf("resume is %s, want %s", got, want)
		}
	}
	if viewers != 10 {
		t.Fatalf("%d of 200 members have #general open at 5%%, want 10", viewers)
	}
}
