package live

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/perm"
	"github.com/tikaydaes/churr/internal/store/storetest"
)

// dial starts a hub over a fresh store, serves one account's connection and
// returns the client end after hello, resume and ready.
func dial(t *testing.T, opts Options) (*Hub, model.Account, *websocket.Conn) {
	t.Helper()
	s := storetest.OpenSQLite(t)
	ctx := context.Background()
	acc := model.Account{Username: "ann", DisplayName: "Ann", Role: model.RoleMember}
	if _, err := s.Accounts().Create(ctx, &acc); err != nil {
		t.Fatal(err)
	}
	h := New(perm.New(s), s, opts)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.Serve(w, r, model.Session{ID: "s1", AccountID: acc.ID}, acc)
	}))
	t.Cleanup(srv.Close)
	c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http"), &websocket.DialOptions{Subprotocols: []string{Subprotocol}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.CloseNow() })
	read := func() map[string]any {
		rctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		_, data, err := c.Read(rctx)
		if err != nil {
			t.Fatal(err)
		}
		var f map[string]any
		json.Unmarshal(data, &f)
		return f
	}
	if f := read(); f["type"] != "hello" {
		t.Fatalf("first frame: %v", f)
	}
	c.Write(ctx, websocket.MessageText, []byte(`{"type":"resume","positions":{}}`))
	if f := read(); f["type"] != "ready" {
		t.Fatalf("after resume: %v", f)
	}
	return h, acc, c
}

// TestSlowClientIsDropped: when a client reads too slowly and its queue
// fills, the connection is closed with 4009 and the hub is not held up.
func TestSlowClientIsDropped(t *testing.T) {
	h, acc, c := dial(t, Options{QueueFrames: 5, writeDelay: 200 * time.Millisecond})
	start := time.Now()
	for i := 0; i < 20; i++ {
		h.Send(acc.ID, AccountEvent("test.sent", map[string]any{"n": i}))
	}
	if took := time.Since(start); took > 100*time.Millisecond {
		t.Fatalf("sending to a slow client held the sender up for %v", took)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for {
		_, _, err := c.Read(ctx)
		if err != nil {
			if code := int(websocket.CloseStatus(err)); code != CloseQueueFull {
				t.Fatalf("closed with %d (%v), want %d", code, err, CloseQueueFull)
			}
			break
		}
	}
	deadline := time.Now().Add(2 * time.Second)
	for h.Connected() != 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if h.Connected() != 0 {
		t.Fatal("the dropped connection is still registered")
	}
}

// TestFloodingClientIsClosed: past twenty frames a second a frame is
// dropped with an error, and a client that keeps going is closed with 4029.
func TestFloodingClientIsClosed(t *testing.T) {
	_, _, c := dial(t, Options{})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	send := func(n int) {
		for i := 0; i < n; i++ {
			if c.Write(ctx, websocket.MessageText, []byte(`{"type":"activity"}`)) != nil {
				return
			}
		}
	}
	// A few frames over the rate: each one over gets an error and is
	// dropped, and the connection stays open.
	send(framesPerSecond + 3)
	for {
		_, data, err := c.Read(ctx)
		if err != nil {
			t.Fatalf("the connection ended before any rate_limited error: %v", err)
		}
		if strings.Contains(string(data), `"rate_limited"`) {
			break
		}
	}
	// A client that keeps going is closed. Frames still queued for it
	// then are dropped, so only the close code is checked here.
	go send(floodFrames + framesPerSecond + 5)
	for {
		_, _, err := c.Read(ctx)
		if err != nil {
			if code := int(websocket.CloseStatus(err)); code != CloseTooManySends {
				t.Fatalf("closed with %d (%v), want %d", code, err, CloseTooManySends)
			}
			break
		}
	}
}

// TestUnreadableFramesClose: ten frames the server cannot read in a minute
// close the connection with 4004.
func TestUnreadableFramesClose(t *testing.T) {
	_, _, c := dial(t, Options{})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	go func() {
		for i := 0; i < unreadablePerMin; i++ {
			c.Write(ctx, websocket.MessageText, []byte(`not json`))
			time.Sleep(60 * time.Millisecond) // stay under the frame rate
		}
	}()
	errors := 0
	for {
		_, data, err := c.Read(ctx)
		if err != nil {
			if code := int(websocket.CloseStatus(err)); code != CloseUnreadable {
				t.Fatalf("closed with %d (%v), want %d", code, err, CloseUnreadable)
			}
			break
		}
		if strings.Contains(string(data), `"error"`) {
			errors++
		}
	}
	if errors < unreadablePerMin-1 {
		t.Fatalf("got %d error frames before the close", errors)
	}
}

// TestPresence: online while a connection is active, offline with none.
func TestPresence(t *testing.T) {
	h, acc, c := dial(t, Options{})
	if p := h.Presence(acc.ID); p != model.PresenceOnline {
		t.Fatalf("presence while connected: %s", p)
	}
	if !h.Active(acc.ID) {
		t.Fatal("a client that just connected is active")
	}
	c.Close(websocket.StatusNormalClosure, "")
	deadline := time.Now().Add(2 * time.Second)
	for h.Connected() != 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if p := h.Presence(acc.ID); p != model.PresenceOffline {
		t.Fatalf("presence after disconnecting: %s", p)
	}
}
