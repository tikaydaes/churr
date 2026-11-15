package live

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
	"github.com/tikaydaes/churr/internal/store/storetest"
)

// room is a crowd whose accounts are all members of one conversation that
// a test can post to, the way the chat service does: the event is written
// to the log with the conversation's next seq and published after.
type room struct {
	*crowd
	store store.Store
	conv  model.ConversationID
	// posting keeps posts one after another, as publishing in seq order
	// requires.
	posting sync.Mutex
}

func newRoom(t *testing.T, n int, opts Options) *room {
	t.Helper()
	c := newCrowd(t, n, opts)
	s := c.hub.store
	ctx := context.Background()
	now := model.Now()
	conv := model.Conversation{Kind: model.KindChannel, Name: "room", CreatedByID: c.ids[0], CreatedAt: now}
	if err := s.Conversations().Create(ctx, &conv); err != nil {
		t.Fatal(err)
	}
	for _, id := range c.ids {
		if _, err := s.Members().AddDirect(ctx, conv.ID, id, model.LevelMember, now); err != nil {
			t.Fatal(err)
		}
	}
	return &room{crowd: c, store: s, conv: conv.ID}
}

// post writes one event and publishes it, and returns its seq.
func (r *room) post() int64 {
	r.posting.Lock()
	defer r.posting.Unlock()
	ctx := context.Background()
	var e model.Event
	err := r.store.Tx(ctx, func(tx store.Tx) error {
		seq, err := tx.Conversations().NextSeq(ctx, r.conv)
		if err != nil {
			return err
		}
		e = model.Event{Type: "test.posted", At: model.Now(), ConversationID: r.conv, Seq: seq, Payload: map[string]any{"n": seq}}
		return tx.Events().Append(ctx, e)
	})
	if err != nil {
		r.t.Error(err)
		return 0
	}
	r.hub.Publish(ctx, e)
	return e.Seq
}

// resumer is the far end of one connection that has sent `resume` with
// positions of its own. It keeps the seq of every event of the room in the
// order they came, and the type of every frame.
type resumer struct {
	ws    *websocket.Conn
	hello map[string]any

	mu     sync.Mutex
	seqs   []int64
	types  []string
	closed error
	ready  chan struct{}
	gone   chan struct{}
}

// open connects account i and reads `hello`, and sends nothing.
func (r *room) open(i int) *resumer {
	r.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	ws, _, err := websocket.Dial(ctx, r.url+"?a="+strconv.Itoa(i), &websocket.DialOptions{Subprotocols: []string{Subprotocol}})
	if err != nil {
		r.t.Fatal(err)
	}
	ws.SetReadLimit(maxFrame)
	r.t.Cleanup(func() { ws.CloseNow() })
	_, data, err := ws.Read(ctx)
	if err != nil {
		r.t.Fatal(err)
	}
	cl := &resumer{ws: ws, ready: make(chan struct{}), gone: make(chan struct{})}
	json.Unmarshal(data, &cl.hello)
	if cl.hello["type"] != "hello" {
		r.t.Fatalf("first frame: %s", data)
	}
	return cl
}

// resume sends `resume` from a position in the room and starts reading.
func (r *room) resume(cl *resumer, from int64) {
	r.t.Helper()
	frame := fmt.Sprintf(`{"type":"resume","positions":{%q:%d}}`, r.conv, from)
	if err := cl.ws.Write(context.Background(), websocket.MessageText, []byte(frame)); err != nil {
		r.t.Fatal(err)
	}
	go cl.read()
}

func (cl *resumer) read() {
	for {
		_, data, err := cl.ws.Read(context.Background())
		if err != nil {
			cl.mu.Lock()
			cl.closed = err
			cl.mu.Unlock()
			close(cl.gone)
			return
		}
		var f struct {
			Type string `json:"type"`
			Seq  int64  `json:"seq"`
		}
		if json.Unmarshal(data, &f) != nil {
			continue
		}
		cl.mu.Lock()
		cl.types = append(cl.types, f.Type)
		if f.Type == "test.posted" {
			cl.seqs = append(cl.seqs, f.Seq)
		}
		cl.mu.Unlock()
		if f.Type == "ready" {
			close(cl.ready)
		}
	}
}

func (cl *resumer) got() []int64 {
	cl.mu.Lock()
	defer cl.mu.Unlock()
	return append([]int64(nil), cl.seqs...)
}

func (cl *resumer) saw(frameType string) bool {
	cl.mu.Lock()
	defer cl.mu.Unlock()
	for _, t := range cl.types {
		if t == frameType {
			return true
		}
	}
	return false
}

func (cl *resumer) waitReady(t *testing.T) {
	t.Helper()
	select {
	case <-cl.ready:
	case <-cl.gone:
		t.Fatalf("closed before ready: %v", cl.closed)
	case <-time.After(20 * time.Second):
		t.Fatal("no ready")
	}
}

// gate stands in for the chat service's Tailor. It counts how many
// catch-ups are reading at the same moment, and can hold every catch-up
// where it is until released.
type gate struct {
	now, most atomic.Int64
	slow      time.Duration
	hold      chan struct{} // nil lets catch-ups through
	entered   chan struct{}
}

func (g *gate) Tailor(ctx context.Context, e model.Event, to model.AccountID, live bool) map[string]any {
	if live {
		return nil
	}
	n := g.now.Add(1)
	for {
		most := g.most.Load()
		if n <= most || g.most.CompareAndSwap(most, n) {
			break
		}
	}
	if g.entered != nil {
		select {
		case g.entered <- struct{}{}:
		default:
		}
	}
	if g.hold != nil {
		<-g.hold
	}
	time.Sleep(g.slow)
	g.now.Add(-1)
	return nil
}

// TestResumesAreBoundedAndComplete: many clients resume in the same moment
// while messages are being posted. Never more than the bound catch up at
// once, everyone reaches ready, and every client is sent every event of the
// conversation exactly once, in order: the ones it had missed, the ones
// posted while it waited in the queue, and the ones after.
func TestResumesAreBoundedAndComplete(t *testing.T) {
	const n, bound, before, during, after = 60, 3, 5, 25, 3
	r := newRoom(t, n, Options{resumeInFlight: bound})
	g := &gate{slow: 2 * time.Millisecond}
	r.hub.SetTailor(g)
	r.everyoneSeesEveryone()
	for i := 0; i < before; i++ {
		r.post()
	}
	// Everyone is connected and has `hello` before anyone resumes, so the
	// resumes really do arrive together.
	clients := make([]*resumer, n)
	for i := range clients {
		clients[i] = r.open(i)
	}
	var posts sync.WaitGroup
	posts.Add(1)
	go func() {
		defer posts.Done()
		for i := 0; i < during; i++ {
			r.post()
			time.Sleep(3 * time.Millisecond)
		}
	}()
	// Half hold nothing of the room and half are two events behind.
	for i, cl := range clients {
		r.resume(cl, int64(i%2)*(before-2))
	}
	for _, cl := range clients {
		cl.waitReady(t)
	}
	posts.Wait()
	for i := 0; i < after; i++ {
		r.post()
	}
	total := int64(before + during + after)
	for i, cl := range clients {
		from := int64(i%2) * (before - 2)
		eventually(t, fmt.Sprintf("client %d has every event", i), func() bool { return int64(len(cl.got())) >= total-from })
		got := cl.got()
		if int64(len(got)) != total-from {
			t.Fatalf("client %d was sent %d events, want %d: %v", i, len(got), total-from, got)
		}
		for k, seq := range got {
			if seq != from+int64(k)+1 {
				t.Fatalf("client %d: event %d has seq %d, want %d (missed, doubled or out of order): %v", i, k, seq, from+int64(k)+1, got)
			}
		}
	}
	if most := g.most.Load(); most > bound {
		t.Fatalf("%d catch-ups ran at once; the bound is %d", most, bound)
	} else if most < bound {
		t.Errorf("only %d catch-ups ever ran at once; the bound of %d was never reached", most, bound)
	}
	st := r.hub.ResumeStats()
	if st.Served != n || st.Refused != 0 || st.Waiting != 0 || st.Bound != bound {
		t.Fatalf("after the storm: %+v", st)
	}
	eventually(t, "every place is given back", func() bool { return r.hub.ResumeStats().InFlight == 0 })
}

// TestQueuedConnectionIsAnsweredPings: a connection whose resume is waiting
// its turn gets `pong` for its `ping`, so a client that expects one within a
// heartbeat does not give up, and is not closed for having resumed late.
func TestQueuedConnectionIsAnsweredPings(t *testing.T) {
	r := newRoom(t, 2, Options{resumeInFlight: 1, ResumeWithin: 300 * time.Millisecond})
	g := &gate{hold: make(chan struct{}), entered: make(chan struct{}, 1)}
	r.hub.SetTailor(g)
	r.post()
	first := r.open(0)
	r.resume(first, 0)
	<-g.entered // the one place is taken, and stays taken
	second := r.open(1)
	r.resume(second, 0)
	eventually(t, "the second resume is queued", func() bool { return r.hub.ResumeStats().Waiting == 1 })
	for i := 0; i < 3; i++ {
		id := fmt.Sprintf("p%d", i)
		if err := second.ws.Write(context.Background(), websocket.MessageText, []byte(`{"type":"ping","id":"`+id+`"}`)); err != nil {
			t.Fatal(err)
		}
		eventually(t, "a pong while queued", func() bool {
			second.mu.Lock()
			defer second.mu.Unlock()
			pongs := 0
			for _, typ := range second.types {
				if typ == "pong" {
					pongs++
				}
			}
			return pongs == i+1
		})
	}
	// Longer than the server waits for a resume: the queued connection
	// has resumed, and is not closed for the time it spends waiting.
	time.Sleep(450 * time.Millisecond)
	if second.saw("ready") || second.saw("test.posted") {
		t.Fatal("the queued connection was caught up while the only place was taken")
	}
	select {
	case <-second.gone:
		t.Fatalf("the queued connection was closed: %v", second.closed)
	default:
	}
	close(g.hold)
	first.waitReady(t)
	second.waitReady(t)
	if got := second.got(); len(got) != 1 || got[0] != 1 {
		t.Fatalf("the queued connection's catch-up: %v", got)
	}
	if wait := r.hub.ResumeStats().LastWait; wait < 400*time.Millisecond {
		t.Fatalf("the queue reports a wait of %v for a resume that waited longer", wait)
	}
}

// TestFullResumeQueueRefuses: past the queue's limit a resume is refused
// with 4029, which clients answer by coming back in thirty seconds; the
// connections already waiting are not disturbed, and one that goes away
// gives its place in the queue to the next.
func TestFullResumeQueueRefuses(t *testing.T) {
	r := newRoom(t, 6, Options{resumeInFlight: 1, resumeQueue: 2})
	g := &gate{hold: make(chan struct{}), entered: make(chan struct{}, 1)}
	r.hub.SetTailor(g)
	r.post()
	working := r.open(0)
	r.resume(working, 0)
	<-g.entered
	queued := []*resumer{r.open(1), r.open(2)}
	for _, cl := range queued {
		r.resume(cl, 0)
	}
	eventually(t, "two are queued", func() bool { return r.hub.ResumeStats().Waiting == 2 })

	refused := r.open(3)
	r.resume(refused, 0)
	select {
	case <-refused.gone:
	case <-time.After(10 * time.Second):
		t.Fatal("the resume past the limit was not refused")
	}
	if code := int(websocket.CloseStatus(refused.closed)); code != CloseTooManySends {
		t.Fatalf("refused with %d (%v), want %d", code, refused.closed, CloseTooManySends)
	}
	if refused.saw("ready") || refused.saw("test.posted") {
		t.Fatal("the refused connection was sent a catch-up")
	}
	if st := r.hub.ResumeStats(); st.Refused != 1 || st.Waiting != 2 || st.Limit != 2 {
		t.Fatalf("after the refusal: %+v", st)
	}

	// One of the waiting goes away; its place is free for someone else.
	queued[0].ws.Close(websocket.StatusNormalClosure, "")
	eventually(t, "the one that left is out of the queue", func() bool { return r.hub.ResumeStats().Waiting == 1 })
	late := r.open(4)
	r.resume(late, 0)
	eventually(t, "the next takes the freed place", func() bool { return r.hub.ResumeStats().Waiting == 2 })

	close(g.hold)
	for _, cl := range []*resumer{working, queued[1], late} {
		cl.waitReady(t)
		if got := cl.got(); len(got) != 1 || got[0] != 1 {
			t.Fatalf("catch-up after the queue moved: %v", got)
		}
	}
	if st := r.hub.ResumeStats(); st.Served != 3 || st.Refused != 1 {
		t.Fatalf("at the end: %+v", st)
	}
}

// TestSingleResumeDoesNotWait: with a free place a resume starts the
// moment it arrives. Connection after connection, none waits in the queue.
func TestSingleResumeDoesNotWait(t *testing.T) {
	r := newRoom(t, 20, Options{})
	for i := 0; i < 50; i++ {
		r.post()
	}
	var slowest time.Duration
	for i := 0; i < 20; i++ {
		cl := r.open(i)
		began := time.Now()
		r.resume(cl, 0)
		cl.waitReady(t)
		if took := time.Since(began); took > slowest {
			slowest = took
		}
		if st := r.hub.ResumeStats(); st.LastWait != 0 || st.Waiting != 0 {
			t.Fatalf("connection %d waited in the queue: %+v", i, st)
		}
		if got := cl.got(); len(got) != 50 {
			t.Fatalf("connection %d was sent %d of 50 events", i, len(got))
		}
	}
	// A whole catch-up of fifty events, database and all, takes a few
	// milliseconds; a queue that held anything back would show here.
	if slowest > 500*time.Millisecond {
		t.Fatalf("the slowest of twenty lone resumes took %v", slowest)
	}
	t.Logf("slowest of twenty lone resumes, fifty events each: %v", slowest)
	if st := r.hub.ResumeStats(); st.Served != 20 {
		t.Fatalf("served %d of 20", st.Served)
	}
}

// TestLeavingWaitsBehindResumes: telling others an account went offline
// takes its turn behind the resumes, and by then an account that has come
// back is not announced at all.
func TestLeavingWaitsBehindResumes(t *testing.T) {
	r := newRoom(t, 3, Options{resumeInFlight: 1})
	var mu sync.Mutex
	told := map[model.AccountID][]model.Presence{}
	r.hub.OnPresence(func(account model.AccountID) {
		mu.Lock()
		told[account] = append(told[account], r.hub.Presence(account))
		mu.Unlock()
	})
	tellings := func(i int) []model.Presence {
		mu.Lock()
		defer mu.Unlock()
		return append([]model.Presence(nil), told[r.ids[i]]...)
	}
	r.post()
	// Accounts 1 and 2 are connected and known to be online.
	one, two := r.open(1), r.open(2)
	r.resume(one, 1)
	r.resume(two, 1)
	one.waitReady(t)
	two.waitReady(t)
	eventually(t, "both are announced online", func() bool { return len(tellings(1)) == 1 && len(tellings(2)) == 1 })

	// The one place is taken; both then drop, and one comes straight back.
	g := &gate{hold: make(chan struct{}), entered: make(chan struct{}, 1)}
	r.hub.SetTailor(g)
	busy := r.open(0)
	r.resume(busy, 0)
	<-g.entered
	one.ws.Close(websocket.StatusNormalClosure, "")
	two.ws.Close(websocket.StatusNormalClosure, "")
	eventually(t, "both connections are gone", func() bool { return r.hub.Connections(r.ids[1])+r.hub.Connections(r.ids[2]) == 0 })
	back := r.open(1)
	r.resume(back, 1)
	time.Sleep(100 * time.Millisecond)
	if len(tellings(1)) != 1 || len(tellings(2)) != 1 {
		t.Fatalf("someone was announced while the only place was taken: %v %v", tellings(1), tellings(2))
	}
	close(g.hold)
	back.waitReady(t)
	eventually(t, "the one that stayed away is announced offline", func() bool {
		p := tellings(2)
		return len(p) == 2 && p[1] == model.PresenceOffline
	})
	if p := tellings(1); len(p) != 1 {
		t.Fatalf("the account that came straight back was announced again: %v", p)
	}
}

// TestHelloCarriesTheReconnectSpread: `hello` tells the client how long to
// spread its next reconnect over, from how many connections the server has.
func TestHelloCarriesTheReconnectSpread(t *testing.T) {
	for _, c := range []struct{ connections, want int }{
		{0, 2}, {1, 2}, {20, 2}, {500, 2}, {501, 3}, {750, 3}, {751, 4},
		{5000, 20}, {5001, 21}, {14999, 60}, {15000, 60}, {15001, 60}, {1000000, 60},
	} {
		if got := reconnectSpread(c.connections); got != c.want {
			t.Errorf("with %d connections the spread is %d seconds, want %d", c.connections, got, c.want)
		}
	}
	r := newRoom(t, 1, Options{})
	cl := r.open(0)
	if got, ok := cl.hello["reconnect_spread_seconds"].(float64); !ok || got != 2 {
		t.Fatalf("hello on a server with one connection: reconnect_spread_seconds = %v", cl.hello["reconnect_spread_seconds"])
	}
}

// TestResumeBound: how many resumes are worked at once, from the database
// pool and the processors.
func TestResumeBound(t *testing.T) {
	for _, c := range []struct{ pool, cpus, want int }{
		{20, 4, 8}, {20, 6, 10}, {20, 64, 10}, {8, 2, 4}, {8, 16, 4}, {1, 1, 2}, {0, 0, 2},
	} {
		if got := resumeBound(c.pool, c.cpus); got != c.want {
			t.Errorf("pool %d, %d processors: %d at once, want %d", c.pool, c.cpus, got, c.want)
		}
	}
	// A real store says how large its pool is, and the hub believes it.
	s := storetest.OpenSQLite(t)
	p, ok := s.(readPool)
	if !ok || p.ReadPool() <= 0 {
		t.Fatalf("the store does not say how large its read pool is")
	}
}
