package live

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/perm"
	"github.com/tikaydaes/churr/internal/store/storetest"
)

// crowd is a hub with n accounts, any of which a test can connect.
type crowd struct {
	t   *testing.T
	hub *Hub
	ids []model.AccountID
	url string
}

func newCrowd(t *testing.T, n int, opts Options) *crowd {
	t.Helper()
	s := storetest.OpenSQLite(t)
	ctx := context.Background()
	accs := make([]model.Account, n)
	ids := make([]model.AccountID, n)
	for i := range accs {
		accs[i] = model.Account{Username: fmt.Sprintf("p%04d", i), DisplayName: "P", Role: model.RoleMember}
		if _, err := s.Accounts().Create(ctx, &accs[i]); err != nil {
			t.Fatal(err)
		}
		ids[i] = accs[i].ID
	}
	h := New(perm.New(s), s, opts)
	srv := newTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		i, _ := strconv.Atoi(r.URL.Query().Get("a"))
		h.Serve(w, r, model.Session{ID: model.SessionID(fmt.Sprintf("s%d", i)), AccountID: ids[i]}, accs[i])
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(h.Shutdown)
	return &crowd{t: t, hub: h, ids: ids, url: "ws" + strings.TrimPrefix(srv.URL, "http")}
}

// everyoneSeesEveryone stands in for the people service: each change in
// what the connections say of an account is sent to every account.
func (c *crowd) everyoneSeesEveryone() {
	c.hub.OnPresence(func(account model.AccountID) {
		c.hub.SendPresence(c.ids, account, presenceEvent(account, c.hub.Presence(account)))
	})
}

func presenceEvent(account model.AccountID, p model.Presence) model.Event {
	return model.Event{Type: model.EventPresenceChanged, At: model.Now(),
		Payload: map[string]any{"account_id": account, "presence": p, "in_call": false}}
}

// client is the far end of one connection. It reads every frame as it
// comes and keeps what a real client would: the newest presence of each
// account, from presence.changed and presence.batch alike.
type client struct {
	ws *websocket.Conn

	mu       sync.Mutex
	presence map[string]string
	// frames counts presence frames of each type; arrived is when each
	// came; others holds every other event, in order.
	frames  map[string]int
	arrived []time.Time
	others  []map[string]any
	closed  error
	// dialled and ready are when the connection was begun and when its
	// `ready` had been read.
	dialled, ready time.Time
}

func (c *crowd) dial(i int) *client {
	c.t.Helper()
	cl, err := c.tryDial(i)
	if err != nil {
		c.t.Fatal(err)
	}
	return cl
}

func (c *crowd) tryDial(i int) (*client, error) {
	began := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	ws, _, err := websocket.Dial(ctx, c.url+"?a="+strconv.Itoa(i), &websocket.DialOptions{Subprotocols: []string{Subprotocol}})
	if err != nil {
		return nil, err
	}
	ws.SetReadLimit(maxFrame)
	c.t.Cleanup(func() { ws.CloseNow() })
	cl := &client{ws: ws, presence: map[string]string{}, frames: map[string]int{}}
	if _, _, err := ws.Read(ctx); err != nil { // hello
		return nil, err
	}
	if err := ws.Write(ctx, websocket.MessageText, []byte(`{"type":"resume","positions":{}}`)); err != nil {
		return nil, err
	}
	ready := make(chan struct{})
	go cl.read(ready)
	select {
	case <-ready:
	case <-ctx.Done():
		return nil, fmt.Errorf("no ready: %w", ctx.Err())
	}
	cl.dialled, cl.ready = began, time.Now()
	return cl, nil
}

func (cl *client) read(ready chan struct{}) {
	for {
		_, data, err := cl.ws.Read(context.Background())
		if err != nil {
			cl.mu.Lock()
			cl.closed = err
			cl.mu.Unlock()
			return
		}
		var f map[string]any
		if json.Unmarshal(data, &f) != nil {
			continue
		}
		typ, _ := f["type"].(string)
		cl.mu.Lock()
		switch typ {
		case "ready":
			close(ready)
		case model.EventPresenceChanged:
			cl.frames[typ]++
			cl.arrived = append(cl.arrived, time.Now())
			cl.presence[f["account_id"].(string)] = f["presence"].(string)
		case EventPresenceBatch:
			cl.frames[typ]++
			cl.arrived = append(cl.arrived, time.Now())
			for _, raw := range f["changes"].([]any) {
				p := raw.(map[string]any)
				if p["type"] != model.EventPresenceChanged {
					cl.others = append(cl.others, map[string]any{"type": "bad batch entry"})
				}
				cl.presence[p["account_id"].(string)] = p["presence"].(string)
			}
		case "ack", "pong":
		default:
			cl.others = append(cl.others, f)
		}
		cl.mu.Unlock()
	}
}

func (cl *client) presenceFrames() int {
	cl.mu.Lock()
	defer cl.mu.Unlock()
	return cl.frames[model.EventPresenceChanged] + cl.frames[EventPresenceBatch]
}

func (cl *client) of(id model.AccountID) string {
	cl.mu.Lock()
	defer cl.mu.Unlock()
	return cl.presence[string(id)]
}

// settle waits until the hub has had nothing to send for a while: no
// presence waiting anywhere and the frame count standing still.
func (c *crowd) settle() PresenceStats {
	c.t.Helper()
	quiet := c.hub.opts.presenceCap + 300*time.Millisecond
	last, since := c.hub.PresenceStats(), time.Now()
	for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); {
		time.Sleep(20 * time.Millisecond)
		if now := c.hub.PresenceStats(); now != last {
			last, since = now, time.Now()
		} else if time.Since(since) > quiet {
			return last
		}
	}
	c.t.Fatal("presence never settled")
	return last
}

func eventually(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); {
		if ok() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("never happened: " + what)
}

// storm connects n accounts, evenly over the same stretch of time whatever
// n is, with everyone told of everyone, and returns what the hub counted
// once it was over and what the clients held.
func storm(t *testing.T, n int, over time.Duration) (PresenceStats, []*client, *crowd) {
	t.Helper()
	// Tight settings, so that a few hundred connections in a test are a
	// storm: pressure from 100 frames a second, the longest wait reached at
	// 500 a second.
	c := newCrowd(t, n, Options{presenceCalm: 100, presenceStep: 100 * time.Millisecond, presenceCap: 500 * time.Millisecond})
	c.everyoneSeesEveryone()
	clients := make([]*client, n)
	var wg sync.WaitGroup
	start := time.Now()
	for i := 0; i < n; i++ {
		if wait := time.Until(start.Add(over * time.Duration(i) / time.Duration(n))); wait > 0 {
			time.Sleep(wait)
		}
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			cl, err := c.tryDial(i)
			if err != nil {
				t.Errorf("connection %d: %v", i, err)
				return
			}
			clients[i] = cl
		}(i)
	}
	wg.Wait()
	if t.Failed() {
		t.FailNow()
	}
	return c.settle(), clients, c
}

// TestPresenceStormIsLinear: when many accounts connect in a short time and
// each is announced to all the others, the presence frames the server sends
// grow roughly with the number of connections, not with its square. Frames
// are counted; no time is measured.
func TestPresenceStormIsLinear(t *testing.T) {
	const small, large = 75, 300 // four times as many
	over := 1500 * time.Millisecond
	a, _, _ := storm(t, small, over)
	b, clients, c := storm(t, large, over)
	t.Logf("%4d connections: %6d deliveries asked for, %5d frames sent (%d batches, %d merged away)", small, a.Deliveries, a.Frames, a.Batches, a.Merged)
	t.Logf("%4d connections: %6d deliveries asked for, %5d frames sent (%d batches, %d merged away)", large, b.Deliveries, b.Frames, b.Batches, b.Merged)

	// What was asked for is quadratic: four times the connections, some
	// sixteen times the deliveries.
	if b.Deliveries < 10*a.Deliveries {
		t.Fatalf("the storm was not the quadratic one this test is about: %d and %d deliveries", a.Deliveries, b.Deliveries)
	}
	// What was sent is not: four times the connections is about four times
	// the frames. Eight leaves room for a slow machine; sixteen is the
	// square.
	if b.Frames > 8*a.Frames {
		t.Fatalf("frames grew from %d to %d for four times the connections: more than linearly", a.Frames, b.Frames)
	}
	if b.Frames*8 > b.Deliveries {
		t.Fatalf("%d frames carried %d deliveries: presence was not merged", b.Frames, b.Deliveries)
	}
	if b.Batches == 0 {
		t.Fatal("no presence.batch frame was sent")
	}
	// The clients counted the same frames the hub did.
	got := 0
	for _, cl := range clients {
		got += cl.presenceFrames()
	}
	if int64(got) != b.Frames {
		t.Fatalf("clients read %d presence frames, the hub sent %d", got, b.Frames)
	}
	// Nothing was lost by merging: whoever was connected before someone
	// arrived knows they are online. (Someone who connected later, or at
	// the same moment, loads that person with everyone else, over HTTP.)
	checked := 0
	for i, cl := range clients {
		for j, other := range clients {
			if !cl.ready.Before(other.dialled) {
				continue
			}
			checked++
			if p := cl.of(c.ids[j]); p != "online" {
				t.Fatalf("connection %d holds %q for account %d, who connected after it was ready", i, p, j)
			}
		}
	}
	if checked < large*large/4 {
		t.Fatalf("only %d pairs of connections were one after the other", checked)
	}
}

// TestPresenceIsImmediateUnderNormalLoad: with nothing much going on, a
// presence change is sent the moment it happens, as presence.changed and
// never as a batch, with the documented settings.
func TestPresenceIsImmediateUnderNormalLoad(t *testing.T) {
	c := newCrowd(t, 10, Options{})
	c.everyoneSeesEveryone()
	watcher := c.dial(0)
	// Its own arrival is told to it too; that one is let through first.
	eventually(t, "the watcher hears of itself", func() bool { return watcher.of(c.ids[0]) == "online" })
	for i := 1; i < 10; i++ {
		before := watcher.presenceFrames()
		sent := time.Now()
		c.dial(i)
		eventually(t, "the watcher hears of the newcomer", func() bool { return watcher.of(c.ids[i]) == "online" })
		watcher.mu.Lock()
		took := watcher.arrived[len(watcher.arrived)-1].Sub(sent)
		watcher.mu.Unlock()
		// The longest wait is a second and a half; "at once" is nowhere
		// near it. The bound is loose only for slow machines.
		if took > 400*time.Millisecond {
			t.Fatalf("presence took %v to arrive under normal load", took)
		}
		if n := watcher.presenceFrames() - before; n != 1 {
			t.Fatalf("one change came as %d frames", n)
		}
	}
	if w := c.hub.PresenceWait(); w != 0 {
		t.Fatalf("the hub holds presence back by %v under normal load", w)
	}
	st := c.settle()
	if st.Batches != 0 || st.Merged != 0 {
		t.Fatalf("presence was batched under normal load: %+v", st)
	}
	watcher.mu.Lock()
	defer watcher.mu.Unlock()
	if watcher.frames[EventPresenceBatch] != 0 || watcher.frames[model.EventPresenceChanged] == 0 {
		t.Fatalf("frames under normal load: %v", watcher.frames)
	}
}

// TestNewestPresenceWinsAfterAStorm: accounts whose presence flips many
// times during a storm end, for every reader, at the last value sent; other
// events sent meanwhile arrive one frame each and in order; and when the
// storm is over presence is immediate again.
func TestNewestPresenceWinsAfterAStorm(t *testing.T) {
	const accounts, rounds = 40, 25
	c := newCrowd(t, accounts+1, Options{presenceCalm: 50, presenceStep: 50 * time.Millisecond, presenceCap: 200 * time.Millisecond})
	reader := c.dial(0)
	me := c.ids[0]
	values := []model.Presence{model.PresenceOnline, model.PresenceAway, model.PresenceOffline}
	want := map[model.AccountID]string{}
	events := 0
	for r := 0; r < rounds; r++ {
		for i := 1; i <= accounts; i++ {
			p := values[(r*7+i*3+r*i)%len(values)]
			c.hub.SendPresence([]model.AccountID{me}, c.ids[i], presenceEvent(c.ids[i], p))
			want[c.ids[i]] = string(p)
		}
		// Something that is not presence, between the rounds.
		c.hub.Send(me, AccountEvent("test.sent", map[string]any{"n": events}))
		events++
		time.Sleep(4 * time.Millisecond)
	}
	st := c.settle()
	for id, p := range want {
		if got := reader.of(id); got != p {
			t.Fatalf("after the storm the reader holds %q for an account whose last value was %q", got, p)
		}
	}
	if st.Batches == 0 || st.Merged == 0 {
		t.Fatalf("the storm was not merged: %+v", st)
	}
	if got := reader.presenceFrames(); got >= accounts*rounds/4 {
		t.Fatalf("%d changes came as %d frames", accounts*rounds, got)
	}
	// Every other event came, one frame each, in the order sent.
	reader.mu.Lock()
	if len(reader.others) != events {
		t.Fatalf("%d other events were sent and %d arrived", events, len(reader.others))
	}
	for i, f := range reader.others {
		if f["type"] != "test.sent" || int(f["n"].(float64)) != i {
			t.Fatalf("other event %d arrived as %v", i, f)
		}
	}
	reader.mu.Unlock()

	// A second after the last of it the server is calm, and a change is
	// one presence.changed frame again.
	eventually(t, "the wait falls back to nothing", func() bool { return c.hub.PresenceWait() == 0 })
	before := c.hub.PresenceStats()
	c.hub.SendPresence([]model.AccountID{me}, c.ids[1], presenceEvent(c.ids[1], model.PresenceOnline))
	after := c.hub.PresenceStats()
	if after.Frames != before.Frames+1 || after.Batches != before.Batches {
		t.Fatalf("after the storm a change was not sent at once: before %+v, after %+v", before, after)
	}
	eventually(t, "the last change arrives", func() bool { return reader.of(c.ids[1]) == "online" })
}

// TestBatchesKeepToTheirRecipients: under pressure two connections flushed
// together are each sent only the changes that were addressed to them, and
// connections addressed the same changes hold the same values.
func TestBatchesKeepToTheirRecipients(t *testing.T) {
	const n = 60
	c := newCrowd(t, n+3, Options{presenceCalm: 20, presenceStep: 50 * time.Millisecond, presenceCap: 200 * time.Millisecond})
	a, b, b2 := c.dial(0), c.dial(1), c.dial(2)
	both := []model.AccountID{c.ids[0], c.ids[1], c.ids[2]}
	onlyA := []model.AccountID{c.ids[0]}
	for round := 0; round < 3; round++ {
		for i := 3; i < n+3; i++ {
			to := both
			if i%2 == 1 {
				to = onlyA // b and b2 may not see the odd accounts
			}
			c.hub.SendPresence(to, c.ids[i], presenceEvent(c.ids[i], model.PresenceOnline))
		}
		time.Sleep(30 * time.Millisecond)
	}
	if st := c.settle(); st.Batches == 0 {
		t.Fatalf("nothing was batched: %+v", st)
	}
	for i := 3; i < n+3; i++ {
		if a.of(c.ids[i]) != "online" {
			t.Fatalf("a was not told of account %d", i)
		}
		for name, cl := range map[string]*client{"b": b, "b2": b2} {
			got := cl.of(c.ids[i])
			if i%2 == 1 && got != "" {
				t.Fatalf("%s was sent the presence of account %d, which was addressed to a alone", name, i)
			}
			if i%2 == 0 && got != "online" {
				t.Fatalf("%s was not told of account %d", name, i)
			}
		}
	}
}

// TestSlowClientGetsPresenceMerged: a client that reads slowly is sent
// fewer, larger presence frames as its queue fills, instead of being cut
// off for a queue that presence filled; and it ends with every value.
func TestSlowClientGetsPresenceMerged(t *testing.T) {
	const n = 300
	// The server-wide load stays calm (the documented settings); only this
	// connection's queue says there is pressure.
	c := newCrowd(t, n+1, Options{QueueFrames: 40, writeDelay: 5 * time.Millisecond, presenceCap: 200 * time.Millisecond})
	slow := c.dial(0)
	for i := 1; i <= n; i++ {
		c.hub.SendPresence([]model.AccountID{c.ids[0]}, c.ids[i], presenceEvent(c.ids[i], model.PresenceOnline))
	}
	st := c.settle()
	eventually(t, "the slow client reads everything", func() bool { return slow.of(c.ids[n]) == "online" })
	slow.mu.Lock()
	closed := slow.closed
	slow.mu.Unlock()
	if closed != nil {
		t.Fatalf("the slow client was cut off: %v", closed)
	}
	for i := 1; i <= n; i++ {
		if slow.of(c.ids[i]) != "online" {
			t.Fatalf("the slow client never heard of account %d", i)
		}
	}
	if st.Batches == 0 || st.Frames >= n {
		t.Fatalf("%d changes for a slow client went as %d frames (%d batches)", n, st.Frames, st.Batches)
	}
}

// TestPresenceWhileCatchingUpComesOnceAfterReady: what changes while a
// client is between hello and ready is not streamed at it; it arrives after
// ready as one frame holding the newest value of each account.
func TestPresenceWhileCatchingUpComesOnceAfterReady(t *testing.T) {
	c := newCrowd(t, 6, Options{})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ws, _, err := websocket.Dial(ctx, c.url+"?a=0", &websocket.DialOptions{Subprotocols: []string{Subprotocol}})
	if err != nil {
		t.Fatal(err)
	}
	defer ws.CloseNow()
	read := func() map[string]any {
		_, data, err := ws.Read(ctx)
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
	// Before resume the connection is still catching up.
	me := []model.AccountID{c.ids[0]}
	for i := 1; i <= 5; i++ {
		c.hub.SendPresence(me, c.ids[i], presenceEvent(c.ids[i], model.PresenceAway))
	}
	c.hub.SendPresence(me, c.ids[1], presenceEvent(c.ids[1], model.PresenceOnline))
	ws.Write(ctx, websocket.MessageText, []byte(`{"type":"resume","positions":{}}`))
	if f := read(); f["type"] != "ready" {
		t.Fatalf("after resume, before anything else: %v", f)
	}
	f := read()
	if f["type"] != EventPresenceBatch {
		t.Fatalf("after ready: %v", f)
	}
	got := map[string]string{}
	for _, raw := range f["changes"].([]any) {
		p := raw.(map[string]any)
		if _, twice := got[p["account_id"].(string)]; twice {
			t.Fatalf("an account is in the batch twice: %v", f)
		}
		got[p["account_id"].(string)] = p["presence"].(string)
	}
	if len(got) != 5 || got[string(c.ids[1])] != "online" || got[string(c.ids[2])] != "away" {
		t.Fatalf("the batch after ready: %v", got)
	}
}

// TestPresenceLoadForgets: the measure of load counts the last second and
// nothing older, so the wait falls back by itself.
func TestPresenceLoadForgets(t *testing.T) {
	var l presenceLoad
	t0 := time.Unix(1_800_000_000, 0)
	l.add(t0, 700)
	l.add(t0.Add(450*time.Millisecond), 300)
	if n := l.perSecond(t0.Add(900 * time.Millisecond)); n != 1000 {
		t.Fatalf("within the second: %d", n)
	}
	if n := l.perSecond(t0.Add(1200 * time.Millisecond)); n != 300 {
		t.Fatalf("after the first has aged out: %d", n)
	}
	if n := l.perSecond(t0.Add(5 * time.Second)); n != 0 {
		t.Fatalf("long after: %d", n)
	}
	h := &Hub{}
	h.opts.defaults()
	now := time.Now()
	if w := h.presenceWait(now); w != 0 {
		t.Fatalf("idle: %v", w)
	}
	h.load.add(now, presenceCalm)
	if w := h.presenceWait(now); w != 0 {
		t.Fatalf("at the calm rate: %v", w)
	}
	h.load.add(now, presenceCalm)
	if w := h.presenceWait(now); w != 2*presenceStep {
		t.Fatalf("at twice the calm rate: %v", w)
	}
	h.load.add(now, 100*presenceCalm)
	if w := h.presenceWait(now); w != presenceCap {
		t.Fatalf("far past it: %v", w)
	}
}
