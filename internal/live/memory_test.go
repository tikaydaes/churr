package live

import (
	"context"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
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
	ws, _, err := websocket.Dial(ctx, r.url+"?a=1", &websocket.DialOptions{Subprotocols: []string{Subprotocol}})
	if err != nil {
		t.Fatal(err)
	}
	defer ws.CloseNow()
	_, data, err := ws.Read(ctx)
	if code := int(websocket.CloseStatus(err)); code != CloseTooManySends {
		t.Fatalf("a connection with no room for it got %q and %v, want a close with %d", data, err, CloseTooManySends)
	}
	var ce websocket.CloseError
	if !asCloseError(err, &ce) || ce.Reason != shortOfMemory {
		t.Fatalf("closed with reason %q, want %q", ce.Reason, shortOfMemory)
	}
	if st := r.hub.Stats(); st.RefusedForMemory != 1 || st.Connections != 1 {
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
