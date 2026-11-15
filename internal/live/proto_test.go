package live

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/perm"
	"github.com/tikaydaes/churr/internal/store/storetest"
	"github.com/tikaydaes/churr/internal/wirepb"
)

// Protobuf on the live connection (ruling R65, prepared.go).

var protoSubprotocol = wirepb.SubprotocolFor(wirepb.SchemaVersion)

// wireClient is one client of a hub under test, speaking JSON or protobuf.
type wireClient struct {
	ws    *websocket.Conn
	proto bool
}

// read returns the next frame as JSON text, whichever way it travelled.
func (c *wireClient) read(ctx context.Context) ([]byte, error) {
	typ, data, err := c.ws.Read(ctx)
	if err != nil {
		return nil, err
	}
	if c.proto != (typ == websocket.MessageBinary) {
		return nil, fmt.Errorf("a %v message on a connection with protobuf=%v", typ, c.proto)
	}
	if c.proto {
		return wirepb.DecodeFrameJSON(data)
	}
	return data, nil
}

// write sends a frame given as JSON text.
func (c *wireClient) write(ctx context.Context, text string) error {
	if c.proto {
		b, typed, why := wirepb.EncodeFrameJSON([]byte(text))
		if !typed {
			return fmt.Errorf("%s has no protobuf wireForm: %v", text, why)
		}
		return c.ws.Write(ctx, websocket.MessageBinary, b)
	}
	return c.ws.Write(ctx, websocket.MessageText, []byte(text))
}

// wireGroup is a hub and connections of one account in several forms.
type wireGroup struct {
	h     *Hub
	acc   model.Account
	url   string
	conns []*wireClient
}

// wireForm is how one client connects.
type wireForm struct {
	proto bool
	mode  websocket.CompressionMode
}

var (
	jsonPlain    = wireForm{false, websocket.CompressionDisabled}
	jsonDeflate  = wireForm{false, websocket.CompressionNoContextTakeover}
	protoDeflate = wireForm{true, websocket.CompressionNoContextTakeover}
)

func newWireGroup(t testing.TB, opts Options, forms ...wireForm) *wireGroup {
	t.Helper()
	s := storetest.OpenSQLite(t)
	ctx := context.Background()
	g := &wireGroup{acc: model.Account{Username: "ann", DisplayName: "Ann", Role: model.RoleMember}}
	if _, err := s.Accounts().Create(ctx, &g.acc); err != nil {
		t.Fatal(err)
	}
	g.h = New(perm.New(s), s, opts)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.h.Serve(w, r, model.Session{ID: "s1", AccountID: g.acc.ID}, g.acc)
	}))
	t.Cleanup(srv.Close)
	g.url = "ws" + strings.TrimPrefix(srv.URL, "http")
	for _, f := range forms {
		offer := []string{Subprotocol}
		if f.proto {
			offer = []string{protoSubprotocol, Subprotocol}
		}
		ws, resp, err := websocket.Dial(ctx, g.url, &websocket.DialOptions{Subprotocols: offer, CompressionMode: f.mode})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { ws.CloseNow() })
		if got := resp.Header.Get("Sec-WebSocket-Protocol"); got != offer[0] {
			t.Fatalf("the server chose %q of %v", got, offer)
		}
		ws.SetReadLimit(maxFrame)
		c := &wireClient{ws: ws, proto: f.proto}
		rctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		if b, err := c.read(rctx); err != nil || !bytes.Contains(b, []byte(`"hello"`)) {
			t.Fatalf("hello: %s %v", b, err)
		}
		if err := c.write(ctx, `{"type":"resume","positions":{}}`); err != nil {
			t.Fatal(err)
		}
		if b, err := c.read(rctx); err != nil || !bytes.Contains(b, []byte(`"ready"`)) {
			t.Fatalf("ready: %s %v", b, err)
		}
		cancel()
		g.conns = append(g.conns, c)
	}
	return g
}

// sameJSON reports whether two JSON texts say the same thing: key order
// aside, and a whole number the same whether written 3 or 3.0.
func sameJSON(t testing.TB, a, b []byte) bool {
	t.Helper()
	parse := func(text []byte) any {
		dec := json.NewDecoder(bytes.NewReader(text))
		dec.UseNumber()
		var v any
		if err := dec.Decode(&v); err != nil {
			t.Fatalf("not JSON: %v: %s", err, text)
		}
		var norm func(v any) any
		norm = func(v any) any {
			switch x := v.(type) {
			case json.Number:
				if i, err := x.Int64(); err == nil {
					return i
				}
				f, _ := x.Float64()
				if float64(int64(f)) == f {
					return int64(f)
				}
				return f
			case []any:
				for i := range x {
					x[i] = norm(x[i])
				}
			case map[string]any:
				for k := range x {
					x[k] = norm(x[k])
				}
			}
			return v
		}
		return norm(v)
	}
	return reflect.DeepEqual(parse(a), parse(b))
}

// frameVectors is every frame of testdata/wirepb/frames.jsonl: each frame
// type there is, three made-up values of each and the realistic ones.
func frameVectors(t testing.TB) (names []string, frames [][]byte) {
	t.Helper()
	f, err := os.Open("../../testdata/wirepb/frames.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(nil, 16<<20)
	for sc.Scan() {
		var v struct {
			Name string          `json:"name"`
			JSON json.RawMessage `json:"json"`
		}
		if err := json.Unmarshal(sc.Bytes(), &v); err != nil {
			t.Fatal(err)
		}
		names, frames = append(names, v.Name), append(frames, v.JSON)
	}
	if len(frames) < 100 {
		t.Fatalf("only %d vectors", len(frames))
	}
	return names, frames
}

// TestProtobufEveryFrame: every frame type, sent through the hub to a JSON
// connection and a protobuf one at once, arrives at both, in order, and the
// protobuf one reads back equal to what the JSON one was sent.
func TestProtobufEveryFrame(t *testing.T) {
	names, frames := frameVectors(t)
	g := newWireGroup(t, Options{QueueFrames: 4096, QueueBytes: 64 << 20}, jsonPlain, protoDeflate, jsonDeflate)
	go func() {
		for _, f := range frames {
			g.h.SendAll([]model.AccountID{g.acc.ID}, json.RawMessage(f))
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for i, want := range frames {
		for j, c := range g.conns {
			got, err := c.read(ctx)
			if err != nil {
				t.Fatalf("%s, connection %d: %v", names[i], j, err)
			}
			if !sameJSON(t, got, want) {
				t.Fatalf("%s, connection %d (protobuf=%v):\n got %s\nwant %s", names[i], j, c.proto, got, want)
			}
		}
	}
}

// TestProtobufMixedClients: JSON connections, compressed and not, and
// protobuf connections in one fan-out are each sent every frame whole and
// in order, small and large, in bursts that leave several to a write.
func TestProtobufMixedClients(t *testing.T) {
	g := newWireGroup(t, Options{}, jsonPlain, protoDeflate, jsonDeflate, protoDeflate, jsonPlain, protoDeflate)
	const events = 400
	text := strings.Repeat("The deploy went out at nine and the dashboards look fine. ", 8)
	var want [][]byte
	for i := 0; i < events; i++ {
		// A typed frame and one the schema does not know, by turns.
		v := map[string]any{"type": "typing.started", "at": "2026-11-13T09:30:00.000Z",
			"conversation_id": "0193a6f0-1111-7b3a-9f0e-2a4d5c6b7e8f", "account_id": fmt.Sprintf("0193a001-0000-7000-8000-%012d", i)}
		if i%3 != 0 {
			v = map[string]any{"type": "test.sent", "n": i, "text": fmt.Sprint(i, " ", text)}
		}
		b, _ := json.Marshal(v)
		want = append(want, b)
	}
	go func() {
		for _, b := range want {
			g.h.SendAll([]model.AccountID{g.acc.ID}, json.RawMessage(b))
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	for i, c := range g.conns {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n, w := range want {
				got, err := c.read(ctx)
				if err != nil {
					t.Errorf("connection %d, frame %d: %v", i, n, err)
					return
				}
				if !sameJSON(t, got, w) {
					t.Errorf("connection %d, frame %d:\n got %s\nwant %s", i, n, got, w)
					return
				}
			}
		}()
	}
	wg.Wait()
}

// TestProtobufClientFrames: a protobuf connection's own frames are read
// from protobuf and answered in it, and a text message on it is refused.
func TestProtobufClientFrames(t *testing.T) {
	g := newWireGroup(t, Options{}, protoDeflate)
	c := g.conns[0]
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	expect := func(what string, has ...string) {
		t.Helper()
		got, err := c.read(ctx)
		if err != nil {
			t.Fatalf("%s: %v", what, err)
		}
		for _, h := range has {
			if !bytes.Contains(got, []byte(h)) {
				t.Fatalf("%s: got %s, want %s in it", what, got, h)
			}
		}
	}
	if err := c.write(ctx, `{"type":"ping","id":"p1"}`); err != nil {
		t.Fatal(err)
	}
	expect("ping", `"type":"pong"`, `"reply_to":"p1"`, `"server_time"`)
	send := func(text string) {
		t.Helper()
		if err := c.write(ctx, text); err != nil {
			t.Fatal(err)
		}
	}
	send(`{"type":"activity","id":"a1"}`)
	expect("activity", `"type":"ack"`, `"reply_to":"a1"`)
	send(`{"type":"conversations.open","id":"o1","positions":{}}`)
	expect("conversations.open", `"type":"ack"`, `"reply_to":"o1"`)
	send(`{"type":"presence.subscribe","id":"s1","account_ids":[]}`)
	expect("presence.subscribe", `"reply_to":"s1"`)
	// JSON text on a protobuf connection, and bytes that are no Envelope.
	c.ws.Write(ctx, websocket.MessageText, []byte(`{"type":"ping"}`))
	expect("text", `"type":"error"`, `invalid_request`)
	c.ws.Write(ctx, websocket.MessageBinary, []byte{0xff, 0xff, 0xff})
	expect("garbage", `"type":"error"`, `invalid_request`)
}

// TestProtobufNeedsCompression: protobuf is never sent uncompressed, so a
// client that offers it without permessage-deflate is refused the upgrade,
// even if it offers JSON beside it and even where the server is set not to
// compress; with compression it is served, compressed, on that same server.
func TestProtobufNeedsCompression(t *testing.T) {
	g := newWireGroup(t, Options{NoCompression: true})
	ctx := context.Background()
	_, resp, err := websocket.Dial(ctx, g.url, &websocket.DialOptions{
		Subprotocols: []string{protoSubprotocol, Subprotocol}, CompressionMode: websocket.CompressionDisabled})
	if err == nil || resp == nil || resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("protobuf without compression: %v %v, want 400", resp, err)
	}
	if r := Refusal(&http.Request{Header: http.Header{"Sec-Websocket-Protocol": {"nothing.known"}}}); r == nil {
		t.Fatal("an unknown subprotocol was not refused")
	}
	ws, resp, err := websocket.Dial(ctx, g.url, &websocket.DialOptions{
		Subprotocols: []string{protoSubprotocol, Subprotocol}, CompressionMode: websocket.CompressionNoContextTakeover})
	if err != nil {
		t.Fatal(err)
	}
	defer ws.CloseNow()
	if !strings.Contains(resp.Header.Get("Sec-WebSocket-Extensions"), "permessage-deflate") {
		t.Fatalf("protobuf was accepted without compression: %v", resp.Header)
	}
	// And JSON on that server stays uncompressed.
	ws2, resp, err := websocket.Dial(ctx, g.url, &websocket.DialOptions{
		Subprotocols: []string{Subprotocol}, CompressionMode: websocket.CompressionNoContextTakeover})
	if err != nil {
		t.Fatal(err)
	}
	defer ws2.CloseNow()
	if resp.Header.Get("Sec-WebSocket-Extensions") != "" {
		t.Fatal("NoCompression no longer holds for JSON")
	}
}

// TestProtobufSmallFrames: on a protobuf connection a frame goes
// uncompressed exactly when it is under compressFrom bytes or compressing
// it would not make it smaller, the header's bit says which, and the frame
// is built once for everyone.
func TestProtobufSmallFrames(t *testing.T) {
	_, frames := frameVectors(t)
	a, b := &conn{proto: true, deflate: true}, &conn{proto: true, deflate: true}
	plain, packed := 0, 0
	for _, f := range frames {
		pb, _, _ := wirepb.EncodeFrameJSON(f)
		z := deflated(pb)
		ba := &batch{frames: [][]byte{f}}
		got := a.frameBytes(ba)
		if got[0]&0x0f != opBinary || got[0]&0x80 == 0 {
			t.Fatalf("not one final binary frame: %x", got[0])
		}
		compressed := got[0]&0x40 != 0
		if compressed != (len(pb) >= compressFrom && len(z) < len(pb)) {
			t.Fatalf("compressed=%v for a frame of %d bytes that deflates to %d", compressed, len(pb), len(z))
		}
		body := pb
		if compressed {
			body = z
			packed++
		} else {
			plain++
		}
		if want := append(appendHeader(nil, opBinary, len(body), compressed), body...); !bytes.Equal(got, want) {
			t.Fatalf("the frame is not its header and payload")
		}
		if ba.raw != len(pb) {
			t.Fatalf("counted %d bytes before compression, want %d", ba.raw, len(pb))
		}
		// The second connection is handed the very same bytes.
		if again := b.frameBytes(&batch{frames: [][]byte{f}}); &again[0] != &got[0] {
			t.Fatal("the protobuf frame was built again for a second connection")
		}
		// Several to a write are the same frames one after another.
		many := a.frameBytes(&batch{frames: [][]byte{f, f}})
		if !bytes.Equal(many, append(append([]byte{}, got...), got...)) {
			t.Fatal("two frames in one write are not the two frames")
		}
	}
	if plain == 0 || packed == 0 {
		t.Fatalf("%d frames went plain and %d compressed: the vectors no longer test both", plain, packed)
	}
	// A connection's own frame is finished when queued and goes as it is.
	own := a.own([]byte(`{"type":"pong","server_time":"2026-11-13T09:30:00.000Z"}`))
	if !finished(own) || own[0]&0x40 != 0 || !bytes.Equal(a.frameBytes(&batch{frames: [][]byte{own}}), own) {
		t.Fatalf("a pong is not sent as it was made: %x", own)
	}
	if j := (&conn{}).own([]byte(`{}`)); finished(j) {
		t.Fatal("a JSON connection's own frame is not its JSON")
	}
}

// TestWireSizes prints what the common frames take on the socket, WebSocket
// header and all, in each form a connection can be sent them
// (go test -v -run TestWireSizes ./internal/live). "Sent" is what the hub
// sends: JSON compressed from compressFrom bytes, protobuf compressed
// when that is smaller.
func TestWireSizes(t *testing.T) {
	want := []struct{ name, label string }{
		{"fixture message.created a", "message.created, 200-byte text"},
		{"fixture reaction.added e", "reaction.added"},
		{"fixture presence.changed g", "presence.changed"},
		{"fixture presence.batch h", "presence.batch, 3 accounts"},
		{"fixture typing.started i", "typing.started"},
		{"fixture read_position.moved k", "read_position.moved"},
	}
	names, frames := frameVectors(t)
	var out strings.Builder
	fmt.Fprintf(&out, "\n%-32s %6s %10s %10s %6s %10s %10s\n", "frame", "JSON", "JSON defl", "JSON sent", "proto", "proto defl", "proto sent")
	header := func(n int) int { return len(appendHeader(nil, opBinary, n, false)) }
	for _, w := range want {
		found := false
		for i, name := range names {
			if name != w.name {
				continue
			}
			found = true
			f := frames[i]
			pb, _, _ := wirepb.EncodeFrameJSON(f)
			jz, pz := deflated(f), deflated(pb)
			jsonSent := len((&conn{deflate: true}).frameBytes(&batch{frames: [][]byte{f}}))
			protoSent := len((&conn{deflate: true, proto: true}).frameBytes(&batch{frames: [][]byte{f}}))
			fmt.Fprintf(&out, "%-32s %6d %10d %10d %6d %10d %10d\n", w.label, len(f)+header(len(f)), len(jz)+header(len(jz)), jsonSent,
				len(pb)+header(len(pb)), len(pz)+header(len(pz)), protoSent)
			if protoSent > jsonSent || protoSent > len(pb)+header(len(pb)) {
				t.Errorf("%s: protobuf as sent (%d) is not the smallest", w.label, protoSent)
			}
		}
		if !found {
			t.Errorf("no vector named %q", w.name)
		}
	}
	t.Log(out.String())
}

// BenchmarkBuildFrame is the work done once for each distinct frame, by the
// first writer to need it: compressing the JSON, or encoding the Envelope
// and compressing that. Every later recipient shares the result.
func BenchmarkBuildFrame(b *testing.B) {
	names, frames := frameVectors(b)
	var frame []byte
	for i, n := range names {
		if n == "fixture message.created a" {
			frame = frames[i]
		}
	}
	b.Run("json+deflate", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			compressedFrame(frame)
		}
	})
	b.Run("protobuf+deflate", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			protoFrame(frame)
		}
	})
}

// TestSmallFramesAreNeverCompressed: the one rule of both encodings. A
// frame under 64 bytes is sent as it is without being tried; any other is
// sent compressed or not, whichever is smaller.
func TestSmallFramesAreNeverCompressed(t *testing.T) {
	if compressFrom != 64 {
		t.Fatalf("frames are compressed from %d bytes, want 64", compressFrom)
	}
	for _, c := range []struct {
		name       string
		payload    []byte
		compressed bool
	}{
		{"63 bytes", payloadOf(63, true), false},
		{"64 bytes", payloadOf(64, true), len(deflated(payloadOf(64, true))) < 64},
		{"300 bytes that shrink", payloadOf(300, true), true},
		{"70 bytes that do not", payloadOf(70, false), len(deflated(payloadOf(70, false))) < 70},
	} {
		j := &conn{deflate: true}
		got := j.frameBytes(&batch{frames: [][]byte{c.payload}})
		if compressed := got[0]&0x40 != 0; compressed != c.compressed {
			t.Errorf("JSON, %s: compressed %v", c.name, compressed)
		}
		// Among others in one write the frame is the same.
		many := j.frameBytes(&batch{frames: [][]byte{c.payload, c.payload}})
		if !bytes.Equal(many, append(append([]byte(nil), got...), got...)) {
			t.Errorf("JSON, %s: the frame differs when written with another", c.name)
		}
	}
	// Protobuf: an envelope under 64 bytes that would shrink.
	small := []byte(`{"type":"typing.started","conversation_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`)
	pb, _, _ := wirepb.EncodeFrameJSON(small)
	wire, _ := protoFrame(small)
	if len(pb) < compressFrom && wire[0]&0x40 != 0 {
		t.Errorf("protobuf: an envelope of %d bytes was compressed", len(pb))
	}
}
