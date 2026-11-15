package live

import (
	"context"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/tikaydaes/churr/internal/wirepb"
)

// TestRefusedWhenShortOfMemory: while the server says it has no room, a
// new connection is answered with a 4029 close and nothing else, nothing
// is kept for it, and the connections already there carry on. When there
// is room again the next one is taken.
func TestRefusedWhenShortOfMemory(t *testing.T) {
	var full atomic.Bool
	r := newRoom(t, 3, Options{Admit: func() bool { return !full.Load() }})
	first := r.open(0)
	r.resume(first, 0)
	first.waitReady(t)

	full.Store(true)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// Every kind of client must get as far as reading the close: JSON
	// with and without compression, and protobuf, which takes a
	// handshake without compression for a broken server.
	for name, opts := range clientKinds() {
		ws, _, err := websocket.Dial(ctx, r.url+"?a=1", opts)
		if err != nil {
			t.Fatalf("%s: the upgrade of a connection to be refused failed: %v", name, err)
		}
		_, data, err := ws.Read(ctx)
		ws.CloseNow()
		if code := int(websocket.CloseStatus(err)); code != CloseTooManySends {
			t.Fatalf("%s: a connection with no room for it got %q and %v, want a close with %d", name, data, err, CloseTooManySends)
		}
		var ce websocket.CloseError
		if !asCloseError(err, &ce) || ce.Reason != shortOfMemory {
			t.Fatalf("%s: closed with reason %q, want %q", name, ce.Reason, shortOfMemory)
		}
	}
	if st := r.hub.Stats(); st.RefusedForMemory != 3 || st.Connections != 1 {
		t.Fatalf("after the refusal: refused %d, connections %d", st.RefusedForMemory, st.Connections)
	}
	// The one that was there is untouched.
	seq := r.post()
	eventually(t, "the connected client still hears", func() bool { g := first.got(); return len(g) == 1 && g[0] == seq })

	full.Store(false)
	second := r.open(1)
	r.resume(second, seq)
	second.waitReady(t)
}

// clientKinds is the three ways a client asks for a live connection.
func clientKinds() map[string]*websocket.DialOptions {
	return map[string]*websocket.DialOptions{
		"JSON":                  {Subprotocols: []string{Subprotocol}},
		"JSON with compression": {Subprotocols: []string{Subprotocol}, CompressionMode: websocket.CompressionNoContextTakeover},
		"protobuf":              {Subprotocols: []string{protoSubprotocol}, CompressionMode: websocket.CompressionNoContextTakeover},
	}
}

// TestFullResumeQueueRefusesEveryKindOfClient: the 4029 of a full resume
// queue, with its reason, reaches protobuf clients as it does JSON ones.
func TestFullResumeQueueRefusesEveryKindOfClient(t *testing.T) {
	r := newRoom(t, 6, Options{resumeInFlight: 1, resumeQueue: 1})
	g := &gate{hold: make(chan struct{}), entered: make(chan struct{}, 1)}
	r.hub.SetTailor(g)
	defer close(g.hold)
	r.post()
	working := r.open(0)
	r.resume(working, 0)
	<-g.entered
	r.resume(r.open(1), 0)
	eventually(t, "one is queued", func() bool { return r.hub.ResumeStats().Waiting == 1 })

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	i := 2
	for name, opts := range clientKinds() {
		ws, _, err := websocket.Dial(ctx, r.url+"?a="+strconv.Itoa(i), opts)
		i++
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if _, _, err := ws.Read(ctx); err != nil { // hello
			t.Fatalf("%s: no hello: %v", name, err)
		}
		resume, typ := []byte(`{"type":"resume","positions":{}}`), websocket.MessageText
		if name == "protobuf" {
			resume, _, _ = wirepb.EncodeFrameJSON(resume)
			typ = websocket.MessageBinary
		}
		if err := ws.Write(ctx, typ, resume); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		_, data, err := ws.Read(ctx)
		ws.CloseNow()
		var ce websocket.CloseError
		if !asCloseError(err, &ce) || int(ce.Code) != CloseTooManySends || !strings.Contains(ce.Reason, "try again shortly") {
			t.Fatalf("%s: a resume past the queue's limit got %q and %v, want a close with %d and its reason", name, data, err, CloseTooManySends)
		}
	}
}

func asCloseError(err error, ce *websocket.CloseError) bool {
	for err != nil {
		if e, ok := err.(websocket.CloseError); ok {
			*ce = e
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

// TestShedTakesTheLargestBacklogsFirst: asked to close two of five, the hub
// closes the two holding the most unsent, with 4029, and the rest go on.
func TestShedTakesTheLargestBacklogsFirst(t *testing.T) {
	r := newRoom(t, 5, Options{})
	clients := make([]*resumer, 5)
	for i := range clients {
		clients[i] = r.open(i)
		r.resume(clients[i], 0)
		clients[i].waitReady(t)
	}
	// What each holds unsent: account 3 the most, then 1, then 4.
	backlog := map[int]int{3: 9000, 1: 5000, 4: 100}
	for i, n := range backlog {
		for _, c := range r.hub.connsOf(r.ids[i]) {
			c.mu.Lock()
			c.held = append(c.held, heldEvent{frame: make([]byte, n)})
			c.mu.Unlock()
		}
	}
	if n := r.hub.Shed(2); n != 2 {
		t.Fatalf("shed %d, want 2", n)
	}
	for _, i := range []int{3, 1} {
		select {
		case <-clients[i].gone:
		case <-time.After(10 * time.Second):
			t.Fatalf("client %d, with %d bytes waiting, was not closed", i, backlog[i])
		}
		if code := int(websocket.CloseStatus(clients[i].closed)); code != CloseTooManySends {
			t.Fatalf("client %d closed with %d (%v), want %d", i, code, clients[i].closed, CloseTooManySends)
		}
	}
	eventually(t, "the two are gone from the hub", func() bool { return r.hub.Connected() == 3 })
	seq := r.post()
	for _, i := range []int{0, 2, 4} {
		eventually(t, "client "+strconv.Itoa(i)+" was kept and still hears", func() bool {
			g := clients[i].got()
			return len(g) == 1 && g[0] == seq
		})
	}
	if st := r.hub.Stats(); st.ShedForMemory != 2 {
		t.Fatalf("counted %d shed, want 2", st.ShedForMemory)
	}
	// With nothing queued anywhere, the one idle longest goes first.
	for _, c := range r.hub.connsOf(r.ids[4]) {
		c.mu.Lock()
		c.held = nil
		c.mu.Unlock()
	}
	for _, c := range r.hub.connsOf(r.ids[2]) {
		c.mu.Lock()
		c.lastActive = c.lastActive.Add(-time.Hour)
		c.mu.Unlock()
	}
	if n := r.hub.Shed(1); n != 1 {
		t.Fatalf("shed %d, want 1", n)
	}
	select {
	case <-clients[2].gone:
	case <-time.After(10 * time.Second):
		t.Fatal("the connection idle longest was not the one closed")
	}
	r.hub.Trim() // and tidying up breaks nothing
	seq = r.post()
	for _, i := range []int{0, 4} {
		eventually(t, "client "+strconv.Itoa(i)+" still hears after a trim", func() bool {
			g := clients[i].got()
			return len(g) == 2 && g[1] == seq
		})
	}
}
