package live

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// closeCode reads until the connection ends and returns the code it was
// closed with.
func closeCode(t *testing.T, c *websocket.Conn) int {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for {
		if _, _, err := c.Read(ctx); err != nil {
			return int(websocket.CloseStatus(err))
		}
	}
}

// TestPongOvertakesTheQueue: the answer to a ping is sent before the events
// already waiting, so a connection with a long queue still hears that it is
// alive.
func TestPongOvertakesTheQueue(t *testing.T) {
	h, acc, c := dial(t, Options{writeDelay: 20 * time.Millisecond})
	const events = 100
	for i := 0; i < events; i++ {
		h.Send(acc.ID, AccountEvent("test.sent", map[string]any{"n": i}))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := c.Write(ctx, websocket.MessageText, []byte(`{"type":"ping","id":"p1"}`)); err != nil {
		t.Fatal(err)
	}
	before := 0
	for {
		_, data, err := c.Read(ctx)
		if err != nil {
			t.Fatalf("no pong after %d events: %v", before, err)
		}
		if strings.Contains(string(data), `"pong"`) {
			if !strings.Contains(string(data), `"reply_to":"p1"`) {
				t.Fatalf("pong without the ping's id: %s", data)
			}
			break
		}
		before++
	}
	// The writer may have had a few frames out before the ping arrived;
	// the pong must not have waited for the hundred.
	if before > events/2 {
		t.Fatalf("the pong waited behind %d of %d queued events", before, events)
	}
	// The events themselves still arrive, every one and in order.
	for seen := before; seen < events; seen++ {
		_, data, err := c.Read(ctx)
		if err != nil {
			t.Fatalf("after the pong, %d of %d events arrived: %v", seen, events, err)
		}
		if !strings.Contains(string(data), `"test.sent"`) {
			t.Fatalf("unexpected frame: %s", data)
		}
	}
}

// TestSilentClientIsClosedForNoHeartbeat: a client that sends nothing and
// is owed nothing is closed with 4008.
func TestSilentClientIsClosedForNoHeartbeat(t *testing.T) {
	h, _, c := dial(t, Options{Heartbeat: 100 * time.Millisecond})
	if code := closeCode(t, c); code != CloseNoHeartbeat {
		t.Fatalf("closed with %d, want %d", code, CloseNoHeartbeat)
	}
	if s := h.Stats(); s.ClosedSilent != 1 || s.ClosedSlow != 0 {
		t.Fatalf("counted %d silent and %d slow, want 1 and 0", s.ClosedSilent, s.ClosedSlow)
	}
}

// TestSilentClientThatIsBehindIsClosedAsSlow: a client nothing is heard
// from while frames wait for it is not taken for dead. It is closed by the
// slow-consumer rule, with that rule's code.
func TestSilentClientThatIsBehindIsClosedAsSlow(t *testing.T) {
	h, acc, c := dial(t, Options{Heartbeat: 100 * time.Millisecond, writeDelay: 100 * time.Millisecond})
	for i := 0; i < 50; i++ {
		h.Send(acc.ID, AccountEvent("test.sent", map[string]any{"n": i}))
	}
	if code := closeCode(t, c); code != CloseQueueFull {
		t.Fatalf("closed with %d, want %d", code, CloseQueueFull)
	}
	if s := h.Stats(); s.ClosedSlow != 1 || s.ClosedSilent != 0 {
		t.Fatalf("counted %d slow and %d silent, want 1 and 0", s.ClosedSlow, s.ClosedSilent)
	}
}

// TestClientThatPingsStaysWhileBehind: pings are heard and answered while
// the queue is long, so a slow connection that is alive is left alone until
// the queue's own limit says otherwise.
func TestClientThatPingsStaysWhileBehind(t *testing.T) {
	h, acc, c := dial(t, Options{Heartbeat: 100 * time.Millisecond, writeDelay: 30 * time.Millisecond})
	for i := 0; i < 40; i++ { // 1.2 seconds of writing: four times the silence allowed
		h.Send(acc.ID, AccountEvent("test.sent", map[string]any{"n": i}))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	go func() {
		for ctx.Err() == nil {
			c.Write(ctx, websocket.MessageText, []byte(`{"type":"ping"}`))
			time.Sleep(60 * time.Millisecond)
		}
	}()
	events := 0
	for events < 40 {
		_, data, err := c.Read(ctx)
		if err != nil {
			t.Fatalf("closed after %d of 40 events: %v", events, err)
		}
		if strings.Contains(string(data), `"test.sent"`) {
			events++
		}
	}
}

// TestWriteThatTimesOutIsSlow: a client whose socket takes nothing for the
// whole write timeout is counted and closed as a slow consumer.
func TestWriteThatTimesOutIsSlow(t *testing.T) {
	h, acc, _ := dial(t, Options{writeTimeout: 200 * time.Millisecond, QueueFrames: 1 << 20, QueueBytes: 1 << 30})
	big := strings.Repeat("x", 512<<10)
	for i := 0; i < 128; i++ { // 64 MB to a client that reads none of it
		h.Send(acc.ID, AccountEvent("test.sent", map[string]any{"text": big}))
	}
	deadline := time.Now().Add(10 * time.Second)
	for h.Stats().ClosedSlow == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if s := h.Stats(); s.ClosedSlow != 1 {
		t.Fatalf("counted %d slow closes, want 1: %+v", s.ClosedSlow, s)
	}
}

// TestCompression: a client that asks for compression gets its larger
// frames compressed and reads them as they were; one that does not ask is
// sent plain frames; and the hub counts the bytes that left either way.
func TestCompression(t *testing.T) {
	text := strings.Repeat("Morning all. The deploy went out at nine and the dashboards look fine. ", 8)
	sent := func(mode websocket.CompressionMode, opts Options) (wire, raw int64) {
		h, acc, c := dialWith(t, opts, mode)
		// The writer counts a frame a moment after the socket has it, so
		// wait until the frames of connecting are all counted.
		before := h.Stats()
		for settled := time.Now(); time.Since(settled) < 150*time.Millisecond; {
			time.Sleep(10 * time.Millisecond)
			if now := h.Stats(); now.FramesOut != before.FramesOut {
				before, settled = now, time.Now()
			}
		}
		h.Send(acc.ID, AccountEvent("test.sent", map[string]any{"text": text}))
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, data, err := c.Read(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), text) {
			t.Fatalf("the frame did not arrive whole: %s", data)
		}
		// The writer counts a frame once the socket has it, which may be
		// a moment after the client read it.
		deadline := time.Now().Add(2 * time.Second)
		for h.Stats().FramesOut == before.FramesOut && time.Now().Before(deadline) {
			time.Sleep(5 * time.Millisecond)
		}
		after := h.Stats()
		if after.FramesOut != before.FramesOut+1 {
			t.Fatalf("counted %d frames, want 1", after.FramesOut-before.FramesOut)
		}
		return after.BytesOut - before.BytesOut, after.RawBytesOut - before.RawBytesOut
	}
	plain, raw := sent(websocket.CompressionDisabled, Options{})
	if plain < raw || plain > raw+16 {
		t.Fatalf("a client that did not ask was sent %d bytes for a frame of %d", plain, raw)
	}
	for _, mode := range []websocket.CompressionMode{websocket.CompressionNoContextTakeover, websocket.CompressionContextTakeover} {
		small, raw := sent(mode, Options{})
		if small*2 > raw {
			t.Fatalf("mode %d: a client that asked was sent %d bytes for a frame of %d", mode, small, raw)
		}
	}
	// The switch turns it off for everyone.
	if off, raw := sent(websocket.CompressionNoContextTakeover, Options{NoCompression: true}); off < raw {
		t.Fatalf("with compression off, %d bytes were sent for a frame of %d", off, raw)
	}
}

// TestStatsRate: bytes out a second follows what was sent.
func TestStatsRate(t *testing.T) {
	h, acc, c := dial(t, Options{})
	h.Stats()
	for i := 0; i < 20; i++ {
		h.Send(acc.ID, AccountEvent("test.sent", map[string]any{"n": i}))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for i := 0; i < 20; i++ {
		if _, _, err := c.Read(ctx); err != nil {
			t.Fatal(err)
		}
	}
	time.Sleep(1100 * time.Millisecond)
	s := h.Stats()
	if s.Connections != 1 || s.BytesOutPerSecond <= 0 || s.BytesOut < s.RawBytesOut {
		t.Fatalf("stats: %+v", s)
	}
}

// BenchmarkWrite measures what sending one message.created of a 200-byte
// message costs the server, to a client that asked for compression and to
// one that did not: the difference is what compression costs a delivery.
func BenchmarkWrite(b *testing.B) {
	frame := []byte(`{"at":"2026-11-15T06:15:21.897Z","conversation_id":"01a1ddd8-9827-75b1-95d6-754d0b18310e","message":{"id":"01a1ddd8-9829-7b98-af7e-707785455722","conversation_id":"01a1ddd8-9827-75b1-95d6-754d0b18310e","seq":3,"author_id":"01a1ddd8-9823-7412-a446-9b7311414642","text":"Morning all. The deploy went out at nine and the dashboards look fine so far; I will keep an eye on the queue depth until lunch. If anyone sees odd latency in search, tell me before rolling back, thanks","order":"0000000000000000!!0000000000000003","created_at":"2026-11-15T06:15:21.897Z"},"notify":"none","seq":3,"type":"message.created"}`)
	for _, mode := range []struct {
		name string
		mode websocket.CompressionMode
	}{{"plain", websocket.CompressionDisabled}, {"compressed", websocket.CompressionNoContextTakeover}} {
		b.Run(mode.name, func(b *testing.B) {
			h, acc, c := dialWith(b, Options{}, mode.mode)
			conns := h.connsOf(acc.ID)
			ctx := context.Background()
			done := make(chan struct{})
			go func() {
				defer close(done)
				for i := 0; i < b.N; i++ {
					if _, _, err := c.Read(ctx); err != nil {
						return
					}
				}
			}()
			start := h.Stats()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := conns[0].ws.Write(ctx, websocket.MessageText, frame); err != nil {
					b.Fatal(err)
				}
			}
			b.StopTimer()
			end := h.Stats()
			b.ReportMetric(float64(end.BytesOut-start.BytesOut)/float64(b.N), "wire-bytes/op")
			c.CloseNow()
			<-done
		})
	}
}
