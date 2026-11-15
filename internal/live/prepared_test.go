package live

import (
	"bufio"
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/rand"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/perm"
	"github.com/tikaydaes/churr/internal/store/storetest"
)

// recorded keeps what the library wrote to a socket.
type recorded struct {
	http.ResponseWriter
	mu  *sync.Mutex
	buf *bytes.Buffer
}

type recordedConn struct {
	net.Conn
	mu  *sync.Mutex
	buf *bytes.Buffer
}

func (c recordedConn) Write(p []byte) (int, error) {
	c.mu.Lock()
	c.buf.Write(p)
	c.mu.Unlock()
	return c.Conn.Write(p)
}

func (r recorded) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	nc, brw, err := http.NewResponseController(r.ResponseWriter).Hijack()
	if err != nil {
		return nil, nil, err
	}
	rc := recordedConn{nc, r.mu, r.buf}
	brw.Writer.Reset(rc)
	return rc, brw, nil
}

// libraryBytes is what the WebSocket library itself puts on the wire for a
// payload, accepted with the options Serve uses, to a client that asked for
// compression or did not.
func libraryBytes(t *testing.T, mode websocket.CompressionMode, payload []byte) []byte {
	t.Helper()
	var (
		mu   sync.Mutex
		buf  bytes.Buffer
		done = make(chan error, 1)
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := websocket.Accept(recorded{w, &mu, &buf}, r, &websocket.AcceptOptions{
			CompressionMode:      websocket.CompressionNoContextTakeover,
			CompressionThreshold: compressFrom,
			InsecureSkipVerify:   true,
		})
		if err != nil {
			done <- err
			return
		}
		done <- ws.Write(context.Background(), websocket.MessageText, payload)
		ws.Read(context.Background()) // until the client goes
	}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http"), &websocket.DialOptions{CompressionMode: mode})
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	c.SetReadLimit(-1)
	_, got, err := c.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatal("the library's own frame did not read back as the payload")
	}
	mu.Lock()
	defer mu.Unlock()
	return append([]byte(nil), buf.Bytes()...)
}

// payloadOf is n bytes of JSON-like text: repetitive when easy, hard to
// compress when not.
func payloadOf(n int, easy bool) []byte {
	if easy {
		return []byte(strings.Repeat(`{"type":"message.created","conversation_id":"01a1ddd8-9827-75b1-95d6-754d0b18310e"},`, n/80+1)[:n])
	}
	raw := make([]byte, n/2+1)
	rand.New(rand.NewSource(int64(n))).Read(raw)
	return []byte(hex.EncodeToString(raw)[:n])
}

// TestPreparedFramesAreTheLibrarys: the frame the hub builds for a payload
// is byte for byte the one the library writes for it, compressed and not,
// at every size of header.
func TestPreparedFramesAreTheLibrarys(t *testing.T) {
	for _, n := range []int{1, 125, 126, compressFrom - 1, compressFrom, 474, 4000, 65535, 65536, 200000} {
		for _, easy := range []bool{true, false} {
			payload := payloadOf(n, easy)
			for _, mode := range []websocket.CompressionMode{websocket.CompressionDisabled, websocket.CompressionNoContextTakeover, websocket.CompressionContextTakeover} {
				want := libraryBytes(t, mode, payload)
				c := &conn{deflate: mode != websocket.CompressionDisabled}
				b := &batch{frames: [][]byte{payload}}
				got := c.frameBytes(b)
				if !bytes.Equal(got, want) {
					t.Fatalf("%d bytes (easy %v), mode %d: the hub's frame is %d bytes and the library's %d; they differ", n, easy, mode, len(got), len(want))
				}
				if !oneFrame(got) {
					t.Fatalf("%d bytes, mode %d: not taken for one whole frame", n, mode)
				}
				if compressed := got[0]&0x40 != 0; compressed != (c.deflate && n >= compressFrom) {
					t.Fatalf("%d bytes, mode %d: compressed %v", n, mode, compressed)
				}
				// Several at once are the same frames end to end.
				b = &batch{frames: [][]byte{payload, []byte(`{"type":"ack"}`), payload}}
				many := c.frameBytes(b)
				small := appendHeader(nil, opText, 14, false)
				small = append(small, `{"type":"ack"}`...)
				if !bytes.Equal(many, bytes.Join([][]byte{want, small, want}, nil)) {
					t.Fatalf("%d bytes, mode %d: a batch is not its frames one after another", n, mode)
				}
			}
		}
	}
}

// TestOneFrame: the socket takes from the library whole frames only.
func TestOneFrame(t *testing.T) {
	whole := appendHeader(nil, opText, 300, false)
	whole = append(whole, make([]byte, 300)...)
	for name, c := range map[string]struct {
		p    []byte
		want bool
	}{
		"pong":           {[]byte{0x8a, 0x00}, true},
		"close":          {[]byte{0x88, 0x02, 0x03, 0xe8}, true},
		"data":           {whole, true},
		"header alone":   {whole[:4], false},
		"short":          {whole[:len(whole)-1], false},
		"two":            {append(append([]byte{}, whole...), 0x8a, 0x00), false},
		"not final":      {[]byte{0x01, 0x00}, false},
		"masked":         {[]byte{0x81, 0x80, 1, 2, 3, 4}, false},
		"empty":          {nil, false},
		"long, cut":      {[]byte{0x81, 127, 0, 0}, false},
		"sixteen bit, 0": {[]byte{0x81, 126, 0, 0}, true},
	} {
		if got := oneFrame(c.p); got != c.want {
			t.Errorf("%s: %v, want %v", name, got, c.want)
		}
	}
	// And a part of a frame is refused, not sent.
	w := &wireConn{Conn: discard{}, sent: new(atomic.Int64)}
	if _, err := w.Write(whole[:4]); err != errNotOneFrame {
		t.Fatalf("part of a frame: %v", err)
	}
	// Nothing follows a close frame.
	if _, err := w.Write([]byte{0x88, 0x00}); err != nil {
		t.Fatal(err)
	}
	if err := w.finish(whole, false); err != net.ErrClosed {
		t.Fatalf("a frame after close: %v", err)
	}
}

// group is one hub with several connections of one account, some asking
// for compression and some not.
type group struct {
	h     *Hub
	acc   model.Account
	conns []*websocket.Conn
}

func newGroup(t testing.TB, opts Options, modes ...websocket.CompressionMode) *group {
	t.Helper()
	s := storetest.OpenSQLite(t)
	ctx := context.Background()
	g := &group{acc: model.Account{Username: "ann", DisplayName: "Ann", Role: model.RoleMember}}
	if _, err := s.Accounts().Create(ctx, &g.acc); err != nil {
		t.Fatal(err)
	}
	g.h = New(perm.New(s), s, opts)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.h.Serve(w, r, model.Session{ID: "s1", AccountID: g.acc.ID}, g.acc)
	}))
	t.Cleanup(srv.Close)
	for _, mode := range modes {
		c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http"), &websocket.DialOptions{Subprotocols: []string{Subprotocol}, CompressionMode: mode})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { c.CloseNow() })
		c.SetReadLimit(maxFrame)
		rctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		if _, _, err := c.Read(rctx); err != nil { // hello
			t.Fatal(err)
		}
		c.Write(ctx, websocket.MessageText, []byte(`{"type":"resume","positions":{}}`))
		if _, _, err := c.Read(rctx); err != nil { // ready
			t.Fatal(err)
		}
		cancel()
		g.conns = append(g.conns, c)
	}
	return g
}

// TestMixedClients: connections that asked for compression and ones that
// did not are sent the same payloads at once, small and large, in bursts
// that go out several to a write, and each reads every one whole and in
// order; the compressed ones cost fewer bytes.
func TestMixedClients(t *testing.T) {
	modes := []websocket.CompressionMode{
		websocket.CompressionDisabled, websocket.CompressionNoContextTakeover,
		websocket.CompressionDisabled, websocket.CompressionContextTakeover,
	}
	g := newGroup(t, Options{}, modes...)
	const events = 400
	text := strings.Repeat("The deploy went out at nine and the dashboards look fine. ", 8)
	var want [][]byte
	for i := 0; i < events; i++ {
		v := map[string]any{"type": "test.sent", "n": i}
		if i%3 != 0 {
			v["text"] = fmt.Sprint(i, " ", text)
		}
		b, _ := json.Marshal(v)
		want = append(want, b)
	}
	go func() {
		for _, b := range want {
			g.h.SendAll([]model.AccountID{g.acc.ID}, json.RawMessage(b))
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	for i, c := range g.conns {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n, w := range want {
				typ, data, err := c.Read(ctx)
				if err != nil {
					t.Errorf("connection %d, frame %d: %v", i, n, err)
					return
				}
				if typ != websocket.MessageText || !bytes.Equal(data, w) {
					t.Errorf("connection %d, frame %d: got %.80s, want %.80s", i, n, data, w)
					return
				}
			}
		}()
	}
	wg.Wait()
	// A ping is still answered, by the hub and by the library both.
	var pings []chan error
	for i, c := range g.conns {
		// The library's pong is seen by a read, so the two go together.
		pinged := make(chan error, 1)
		go func() { pinged <- c.Ping(ctx) }()
		c.Write(ctx, websocket.MessageText, []byte(`{"type":"ping","id":"p"}`))
		if _, data, err := c.Read(ctx); err != nil || !strings.Contains(string(data), `"pong"`) {
			t.Fatalf("connection %d: after ping: %s %v", i, data, err)
		}
		pings = append(pings, pinged)
	}
	if s := g.h.Stats(); s.BytesOut >= s.RawBytesOut {
		t.Fatalf("sent %d bytes for %d: nothing was compressed", s.BytesOut, s.RawBytesOut)
	}
	// And a close still carries its code to each.
	for _, c := range g.h.connsOf(g.acc.ID) {
		c.close(CloseCredential, "the session ended")
	}
	for i, c := range g.conns {
		if code := closeCode(t, c); code != CloseCredential {
			t.Fatalf("connection %d closed with %d, want %d", i, code, CloseCredential)
		}
		// The library's pong came before the close, if not sooner.
		if err := <-pings[i]; err != nil {
			t.Fatalf("connection %d: protocol ping: %v", i, err)
		}
	}
}

// TestWritersShareUnderLoad is for the race detector: many connections of
// both kinds are sent the same frames from several goroutines while some
// ping and some leave.
func TestWritersShareUnderLoad(t *testing.T) {
	var modes []websocket.CompressionMode
	for i := 0; i < 24; i++ {
		modes = append(modes, websocket.CompressionMode(i%2*int(websocket.CompressionNoContextTakeover)))
	}
	g := newGroup(t, Options{}, modes...)
	const senders, each = 4, 150
	text := strings.Repeat("Morning all. The queue depth is fine. ", 12)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var got atomic.Int64
	var wg sync.WaitGroup
	for i, c := range g.conns {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n := 0; ; n++ {
				if i%6 == 5 && n == 100 {
					c.Close(websocket.StatusNormalClosure, "") // leaves part way
					return
				}
				if n%50 == 10 {
					c.Write(ctx, websocket.MessageText, []byte(`{"type":"ping"}`))
				}
				_, data, err := c.Read(ctx)
				if err != nil {
					return
				}
				if bytes.Contains(data, []byte(`"test.sent"`)) {
					if !bytes.Contains(data, []byte(text)) {
						t.Errorf("connection %d: a frame arrived damaged: %.100s", i, data)
						return
					}
					got.Add(1)
				}
			}
		}()
	}
	var sending sync.WaitGroup
	for s := 0; s < senders; s++ {
		sending.Add(1)
		go func() {
			defer sending.Done()
			for n := 0; n < each; n++ {
				g.h.SendAll([]model.AccountID{g.acc.ID}, AccountEvent("test.sent", map[string]any{"s": s, "n": n, "text": text}))
			}
		}()
	}
	sending.Wait()
	stay := int64(0)
	for i := range g.conns {
		if i%6 != 5 {
			stay++
		}
	}
	eventually(t, "every connection that stayed has every frame", func() bool { return got.Load() >= stay*senders*each })
	for _, c := range g.conns {
		c.CloseNow()
	}
	wg.Wait()
}

// discard is a socket that takes everything at once.
type discard struct{ net.Conn }

func (discard) Write(p []byte) (int, error) { return len(p), nil }
func (discard) Close() error                { return nil }

// BenchmarkFanOut measures one delivery of a message.created of 474 bytes
// to connections that take everything at once, so what is left is the
// hub's own work: queueing, waking the writer, building the frame.
//
//	each      the frame is also compressed for every connection, which is
//	          the work the library did when it wrote the frames
//	prepared  it is compressed once and shared
//	plain     the connections did not ask for compression
func BenchmarkFanOut(b *testing.B) {
	base := []byte(`{"at":"2026-11-15T06:15:21.897Z","conversation_id":"01a1ddd8-9827-75b1-95d6-754d0b18310e","message":{"id":"01a1ddd8-9829-7b98-af7e-707785455722","conversation_id":"01a1ddd8-9827-75b1-95d6-754d0b18310e","seq":3,"author_id":"01a1ddd8-9823-7412-a446-9b7311414642","text":"Morning all. The deploy went out at nine and the dashboards look fine so far; I will keep an eye on the queue depth until lunch. If anyone sees odd latency in search, tell me before rolling back, thanks","order":"0000000000000000!!0000000000000003","created_at":"2026-11-15T06:15:21.897Z"},"notify":"none","seq":3,"type":"message.created"}`)
	for _, n := range []int{1000, 10000, 50000} {
		for _, mode := range []string{"each", "prepared", "plain"} {
			b.Run(fmt.Sprintf("%s/conns=%d", mode, n), func(b *testing.B) {
				s := storetest.OpenSQLite(b)
				h := New(perm.New(s), s, Options{})
				conns := make([]*conn, n)
				for i := range conns {
					// A socket that takes everything at once.
					try := func(p []byte) (int, error) { return len(p), nil }
					if mode == "each" {
						try = func(p []byte) (int, error) {
							compressedFrame(base)
							return len(p), nil
						}
					}
					conns[i] = &conn{hub: h, flusher: flusherFor(), started: true, deflate: mode != "plain",
						nc: &wireConn{Conn: discard{}, sent: &h.wire.bytes, try: try}}
				}
				b.ReportAllocs()
				b.ResetTimer()
				sent := int64(0)
				for i := 0; i < b.N; {
					// A new message for every round of the connections.
					frame := append([]byte(nil), base...)
					for _, c := range conns {
						c.sendEvent("", 0, frame)
						if i++; i == b.N {
							break
						}
					}
					sent = int64(i)
					// As fast as the writers take it, no faster.
					for h.wire.frames.Load() < sent-int64(n)/2 {
						time.Sleep(20 * time.Microsecond)
					}
				}
				for h.wire.frames.Load() < sent {
					time.Sleep(20 * time.Microsecond)
				}
				b.StopTimer()
				b.ReportMetric(float64(sent)/b.Elapsed().Seconds(), "frames/s")
			})
		}
	}
}

// TestSlowReaderGetsWholeFrames: a client that reads later than it is sent
// to fills its socket, so writes to it are cut part way through a frame and
// finished by a goroutine of its own (flush.go). Every frame still arrives
// whole and in order, with the library's pongs between frames and never
// inside one, and the connection goes back to being written at once.
func TestSlowReaderGetsWholeFrames(t *testing.T) {
	for _, mode := range []websocket.CompressionMode{websocket.CompressionDisabled, websocket.CompressionNoContextTakeover} {
		g := newGroup(t, Options{QueueFrames: 1 << 20, QueueBytes: 1 << 30}, mode)
		c := g.conns[0]
		const events = 600
		before := handovers.Load()
		var want [][]byte
		for i := 0; i < events; i++ {
			// Hard to compress, so the socket fills either way.
			b, _ := json.Marshal(map[string]any{"type": "test.sent", "n": i, "text": string(payloadOf(30000, false))})
			want = append(want, b)
		}
		for _, b := range want {
			g.h.SendAll([]model.AccountID{g.acc.ID}, json.RawMessage(b))
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		pinged := make(chan error, 1)
		go func() { pinged <- c.Ping(ctx) }() // answered by the library, among the frames
		time.Sleep(300 * time.Millisecond)    // 18 MB is waiting; the socket took a little
		for n, w := range want {
			_, data, err := c.Read(ctx)
			if err != nil {
				t.Fatalf("mode %d, frame %d: %v", mode, n, err)
			}
			if !bytes.Equal(data, w) {
				t.Fatalf("mode %d, frame %d arrived damaged", mode, n)
			}
		}
		if handovers.Load() == before {
			t.Fatalf("mode %d: no write was cut short, so this tested nothing", mode)
		}
		// Caught up, it is sent to as anyone is.
		g.h.Send(g.acc.ID, AccountEvent("test.sent", map[string]any{"n": "last"}))
		if _, data, err := c.Read(ctx); err != nil || !strings.Contains(string(data), `"last"`) {
			t.Fatalf("mode %d: after catching up: %s %v", mode, data, err)
		}
		if err := <-pinged; err != nil {
			t.Fatalf("mode %d: protocol ping: %v", mode, err)
		}
		cancel()
	}
}
