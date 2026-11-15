package live

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/tikaydaes/churr/internal/model"
)

// Every test of this package that serves connections does it through
// newTestServer. With CHURR_LIVE_TEST_TLS set they are all served over TLS
// ended in the process, on sockets from Offerable, which is how a server
// runs that has its own certificate: the flushers then write through the
// TLS layer (offer.go), and everything the tests hold of plain sockets
// (order, whole frames, slow consumers, heartbeats, memory for each
// connection) is held of those too.
//
//	CHURR_LIVE_TEST_TLS=1 go test ./internal/live
var overTLS = os.Getenv("CHURR_LIVE_TEST_TLS") != ""

func init() {
	if overTLS {
		// The clients of the tests dial with the default client.
		http.DefaultTransport.(*http.Transport).TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}
}

func newTestServer(h http.Handler) *httptest.Server {
	if !overTLS {
		return httptest.NewServer(h)
	}
	return newTLSServer(h)
}

// offerableInTests is false only while a measurement wants TLS connections
// written the old way, by a goroutine each.
var offerableInTests = true

func newTLSServer(h http.Handler) *httptest.Server {
	srv := httptest.NewUnstartedServer(h)
	if offerableInTests {
		srv.Listener = Offerable(srv.Listener)
	}
	srv.StartTLS()
	return srv
}

// trustTestCertificates has the default client, which the helpers dial
// with, trust a test server's certificate until the test ends.
func trustTestCertificates(t testing.TB) {
	if overTLS {
		return // already, for the whole run
	}
	tr := http.DefaultTransport.(*http.Transport)
	was := tr.TLSClientConfig
	tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	t.Cleanup(func() { tr.TLSClientConfig = was; tr.CloseIdleConnections() })
}

// tlsGroup is newGroup over TLS whatever the environment says.
func tlsGroup(t testing.TB, opts Options, modes ...websocket.CompressionMode) *group {
	t.Helper()
	was := overTLS
	overTLS = true
	defer func() { overTLS = was }()
	g := newGroup(t, opts, modes...)
	return g
}

// TestTLSConnectionsAreWrittenByTheFlushers: a connection whose TLS ends in
// the process is offered its writes by a flusher, a burst to it goes out
// several frames to a write, and every frame arrives whole and in order,
// compressed or not.
func TestTLSConnectionsAreWrittenByTheFlushers(t *testing.T) {
	trustTestCertificates(t)
	g := tlsGroup(t, Options{QueueFrames: 1 << 16, QueueBytes: 1 << 28},
		websocket.CompressionDisabled, websocket.CompressionNoContextTakeover)
	g.h.mu.RLock()
	for _, set := range g.h.conns {
		for c := range set {
			if !c.offered() {
				t.Fatal("a TLS connection from an Offerable listener is not written by the flushers")
			}
		}
	}
	g.h.mu.RUnlock()
	const events = 2000
	writesBefore, framesBefore := pacing.writes.Load(), g.h.wire.frames.Load()
	for i := 0; i < events; i++ {
		g.h.SendAll([]model.AccountID{g.acc.ID}, json.RawMessage(fmt.Sprintf(`{"type":"test.sent","n":%d,"text":%q}`, i, strings.Repeat("x", i%300))))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for ci, c := range g.conns {
		for i := 0; i < events; i++ {
			_, data, err := c.Read(ctx)
			if err != nil {
				t.Fatalf("connection %d, frame %d: %v", ci, i, err)
			}
			var f struct {
				N    int
				Text string
			}
			if json.Unmarshal(data, &f) != nil || f.N != i || len(f.Text) != i%300 {
				t.Fatalf("connection %d: frame %d arrived as %.80s", ci, i, data)
			}
		}
	}
	frames, writes := g.h.wire.frames.Load()-framesBefore, pacing.writes.Load()-writesBefore
	t.Logf("%d frames in %d writes", frames, writes)
	if writes*2 > frames {
		t.Errorf("%d frames took %d writes: a burst should go several frames to a write", frames, writes)
	}
}

// TestTLSSlowReaderGetsWholeFramesAndIsShed: a TLS client that does not
// read fills its socket; what TLS had sealed and the socket did not take is
// finished by a goroutine of the connection's own, no flusher waits, the
// frames arrive whole and in order when it reads, and one that never reads
// is closed as a slow consumer.
func TestTLSSlowReaderGetsWholeFramesAndIsShed(t *testing.T) {
	trustTestCertificates(t)
	g := tlsGroup(t, Options{QueueFrames: 1 << 20, QueueBytes: 1 << 30}, websocket.CompressionDisabled)
	c := g.conns[0]
	const events = 400
	before := handovers.Load()
	var want []string
	for i := 0; i < events; i++ {
		b, _ := json.Marshal(map[string]any{"type": "test.sent", "n": i, "text": string(payloadOf(30000, false))})
		want = append(want, string(b))
		g.h.SendAll([]model.AccountID{g.acc.ID}, json.RawMessage(b))
	}
	// A second connection is still written at once while the first is
	// behind: no flusher is waiting for it.
	time.Sleep(200 * time.Millisecond)
	if handovers.Load() == before {
		t.Fatal("no write was left unfinished, so this tested nothing")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for n, w := range want {
		_, data, err := c.Read(ctx)
		if err != nil {
			t.Fatalf("frame %d: %v", n, err)
		}
		if string(data) != w {
			t.Fatalf("frame %d arrived damaged", n)
		}
	}
	g.h.Send(g.acc.ID, AccountEvent("test.sent", map[string]any{"n": "last"}))
	if _, data, err := c.Read(ctx); err != nil || !strings.Contains(string(data), `"last"`) {
		t.Fatalf("after catching up: %s %v", data, err)
	}

	// One that never reads is closed as slow.
	slow := tlsGroup(t, Options{QueueFrames: 1 << 20, QueueBytes: 1 << 30, writeTimeout: 300 * time.Millisecond}, websocket.CompressionDisabled)
	closedBefore := slow.h.wire.slow.Load()
	for i := 0; i < events; i++ {
		slow.h.SendAll([]model.AccountID{slow.acc.ID}, json.RawMessage(want[i]))
	}
	// Counted as slow when the time allowed runs out; gone once the
	// library has given up on the close handshake with a client that is
	// not reading, which takes it some seconds on any kind of socket.
	deadline := time.Now().Add(5 * time.Second)
	for slow.h.wire.slow.Load() == closedBefore && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if slow.h.wire.slow.Load() == closedBefore {
		t.Fatal("a TLS client that never read was not closed as slow")
	}
	deadline = time.Now().Add(30 * time.Second)
	for slow.h.Connected() != 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if slow.h.Connected() != 0 {
		t.Fatal("a TLS client that never read is still connected")
	}
}

// TestDroppedTLSConnectionsAreLetGo: when every TLS connection goes at once
// while events are still being sent, the hub has let go of all of them
// within moments: none is registered, their goroutines have ended, and a
// collection gets back what they held. The memory guard counts on that when
// a crowd drops and comes straight back (internal/memguard).
func TestDroppedTLSConnectionsAreLetGo(t *testing.T) {
	trustTestCertificates(t)
	const n = 300
	heap := func() int64 {
		var m runtime.MemStats
		runtime.GC()
		runtime.ReadMemStats(&m)
		return int64(m.HeapAlloc)
	}
	base, baseGoroutines := heap(), runtime.NumGoroutine()
	g := tlsGroup(t, Options{}, make([]websocket.CompressionMode, n)...)
	payload := json.RawMessage(fmt.Sprintf(`{"type":"test.sent","text":%q}`, strings.Repeat("x", 500)))
	to := []model.AccountID{g.acc.ID}
	for i := 0; i < 50; i++ {
		g.h.SendAll(to, payload)
		time.Sleep(time.Millisecond)
	}
	time.Sleep(200 * time.Millisecond)
	held := heap() - base
	for _, c := range g.conns {
		c.CloseNow()
	}
	g.conns = nil // the clients' own ends are not the hub's to let go of
	// Events go on being sent to them as they go.
	for i := 0; i < 20; i++ {
		g.h.SendAll(to, payload)
	}
	deadline := time.Now().Add(3 * time.Second)
	for (g.h.Connected() != 0 || runtime.NumGoroutine() > baseGoroutines+20) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if g.h.Connected() != 0 {
		t.Fatalf("%d of %d dropped connections are still registered", g.h.Connected(), n)
	}
	if extra := runtime.NumGoroutine() - baseGoroutines; extra > 20 {
		t.Errorf("%d goroutines are left over from %d dropped connections", extra, n)
	}
	// The clients' own ends are still held by the test, and the hub and
	// its store stay: what must come back is the hub's share for each
	// connection, which is 10 KB and more. (Written by a goroutine
	// each, as TLS connections were before, the same test gets the same
	// amount back.)
	left := heap() - base
	t.Logf("%d connections held %d KB; %d KB is held after they dropped", n, held/1024, left/1024)
	if freed := held - left; freed < n*8<<10 {
		t.Errorf("only %d KB came back when %d connections dropped: the hub still holds them", freed/1024, n)
	}
}
