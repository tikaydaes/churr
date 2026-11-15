package wire

import (
	"context"
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/secret"
)

// A connection whose sends are queued: Send returns at once, frames reach
// the other side in the order they were given from one goroutine, the last
// word of CloseWith follows everything queued before it, and a connection
// whose other side does not read is closed when too much waits for it,
// instead of holding its sender.
func TestQueuedSends(t *testing.T) {
	a, b := ident(t, "a.example", KindHub), ident(t, "b.example", KindServer)
	got := make(chan *Conn, 2)
	url := listen(t, a, func(Frame, []byte, []byte) error { return nil }, got)
	dial := func() (client, server *Conn) {
		c, err := Dial(context.Background(), DialOptions{URL: url, Identity: b})
		if err != nil {
			t.Fatal(err)
		}
		return c, <-got
	}

	client, server := dial()
	server.Queue(1 << 20)
	const n = 500
	stream := model.NewID[string]()
	for i := 1; i <= n; i++ {
		if err := server.Send(Frame{Frame: FAck, StreamID: stream, Cursor: int64(i)}); err != nil {
			t.Fatalf("send %d: %v", i, err)
		}
	}
	server.CloseWith(Frame{Frame: FBye, Reason: "shutdown"})
	for i := 1; i <= n; i++ {
		f, _, err := client.Read(context.Background())
		if err != nil || f.Frame != FAck || f.Cursor != int64(i) {
			t.Fatalf("frame %d arrived as %+v (%v)", i, f, err)
		}
	}
	if f, _, err := client.Read(context.Background()); err != nil || f.Frame != FBye || f.Reason != "shutdown" {
		t.Fatalf("after everything queued, the last word: %+v (%v)", f, err)
	}
	if err := server.Send(Frame{Frame: FAck}); err == nil {
		t.Fatal("a frame was taken after the last word")
	}
	client.Close()

	// The other side reads nothing. The sender is never held, and once more
	// waits than the queue may hold, the connection is closed.
	client, server = dial()
	defer client.Close()
	server.Queue(256 << 10)
	// Bytes that do not compress, as a sealed body's do not: the socket
	// fills.
	big := Frame{Frame: FDirect, Body: secret.RandomBytes(48 << 10)}
	deadline := time.Now().Add(10 * time.Second)
	closed := false
	for i := 0; i < 5000 && time.Now().Before(deadline); i++ {
		began := time.Now()
		err := server.Send(big)
		if took := time.Since(began); took > 2*time.Second {
			t.Fatalf("a queued send took %v", took)
		}
		if err != nil {
			closed = true
			break
		}
	}
	if !closed {
		t.Fatal("a connection nobody reads took everything it was given")
	}
	select {
	case <-server.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("the connection that could not keep up was not closed")
	}
}
