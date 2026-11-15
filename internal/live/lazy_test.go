package live

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

// lazyRoom is a room delivered lazily, posted to with real message
// traffic: message.created events written to the log and then published.
func newLazyRoom(t *testing.T, n int, opts Options) *room {
	t.Helper()
	r := newRoom(t, n, opts)
	_, err := r.store.Conversations().Update(context.Background(), r.conv, func(c *model.Conversation) error {
		c.LazyDelivery = model.LazyOn
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// say posts one message or, with another type, one event of that type.
func (r *room) say(eventType string) int64 {
	r.posting.Lock()
	defer r.posting.Unlock()
	ctx := context.Background()
	var e model.Event
	err := r.store.Tx(ctx, func(tx store.Tx) error {
		seq, err := tx.Conversations().NextSeq(ctx, r.conv)
		if err != nil {
			return err
		}
		e = model.Event{Type: eventType, At: model.Now(), ConversationID: r.conv, Seq: seq, Payload: map[string]any{"n": seq}}
		if eventType == model.EventMessageCreated {
			e.MessageID = model.NewID[model.MessageID]()
			e.Payload = map[string]any{"message": map[string]any{"id": e.MessageID, "text": "hello"}}
		}
		return tx.Events().Append(ctx, e)
	})
	if err != nil {
		r.t.Error(err)
		return 0
	}
	r.hub.Publish(ctx, e)
	return e.Seq
}

// viewer is the far end of a connection of a client that says what it has
// open. It keeps, of the room, the seq of every event sent in full, and
// every conversation.changed.
type viewer struct {
	t    testing.TB
	ws   *websocket.Conn
	conv model.ConversationID

	mu      sync.Mutex
	full    []int64
	signals []map[string]any
	at      []time.Time
	reloads int
	counts  []map[string]any
	acks    map[string]chan struct{}
	next    int
	ready   chan struct{}
}

// watch connects account i. open says whether the resume frame carries
// `open` at all (a client from before lazy delivery sends none), and with
// what; from is its position in the room.
func (r *room) watch(i int, declared bool, open []model.ConversationID, from int64) *viewer {
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
	var hello map[string]any
	json.Unmarshal(data, &hello)
	if hello["conversations_open_max"] != float64(MaxOpenConversations) {
		r.t.Fatalf("hello does not say how many conversations may be open: %s", data)
	}
	f := map[string]any{"type": "resume", "positions": map[string]int64{string(r.conv): from}}
	if declared {
		if open == nil {
			open = []model.ConversationID{}
		}
		f["open"] = open
	}
	b, _ := json.Marshal(f)
	if err := ws.Write(ctx, websocket.MessageText, b); err != nil {
		r.t.Fatal(err)
	}
	v := &viewer{t: r.t, ws: ws, conv: r.conv, acks: map[string]chan struct{}{}, ready: make(chan struct{})}
	go v.read()
	select {
	case <-v.ready:
	case <-ctx.Done():
		r.t.Fatal("no ready")
	}
	return v
}

func (v *viewer) read() {
	for {
		_, data, err := v.ws.Read(context.Background())
		if err != nil {
			return
		}
		var f map[string]any
		if json.Unmarshal(data, &f) != nil {
			continue
		}
		v.mu.Lock()
		switch f["type"] {
		case "ready":
			close(v.ready)
		case "ack":
			id, _ := f["reply_to"].(string)
			if ch := v.acks[id]; ch != nil {
				close(ch)
			}
		case "error":
			v.t.Errorf("the server answered with an error: %s", data)
		case "reload":
			v.reloads++
		case EventConversationChanged:
			v.signals = append(v.signals, f)
			v.at = append(v.at, time.Now())
		case EventPresenceCounts:
			v.counts = append(v.counts, f)
		default:
			if seq, ok := f["seq"].(float64); ok && f["conversation_id"] == string(v.conv) {
				v.full = append(v.full, int64(seq))
			}
		}
		v.mu.Unlock()
	}
}

// open says which conversations are open, with the seq the client holds of
// each, and waits for the server's answer.
func (v *viewer) open(positions map[model.ConversationID]int64) {
	v.t.Helper()
	v.mu.Lock()
	v.next++
	id := "o" + strconv.Itoa(v.next)
	ch := make(chan struct{})
	v.acks[id] = ch
	v.mu.Unlock()
	if positions == nil {
		positions = map[model.ConversationID]int64{}
	}
	b, _ := json.Marshal(map[string]any{"type": "conversations.open", "id": id, "positions": positions})
	if err := v.ws.Write(context.Background(), websocket.MessageText, b); err != nil {
		v.t.Fatal(err)
	}
	select {
	case <-ch:
	case <-time.After(20 * time.Second):
		v.t.Fatal("conversations.open was not answered")
	}
}

// last is the seq where what the client holds of the room ends.
func (v *viewer) last() int64 {
	v.mu.Lock()
	defer v.mu.Unlock()
	if len(v.full) == 0 {
		return 0
	}
	return v.full[len(v.full)-1]
}

func (v *viewer) fulls() []int64 {
	v.mu.Lock()
	defer v.mu.Unlock()
	return append([]int64(nil), v.full...)
}

func (v *viewer) signalled() []map[string]any {
	v.mu.Lock()
	defer v.mu.Unlock()
	return append([]map[string]any(nil), v.signals...)
}

// A lazy conversation goes in full to the connections that have it open
// and to clients that never say what they have open; the others are told
// that it changed, at most once a second, with nothing lost from the count;
// and a message that notifies its reader, or an event that is not message
// traffic, reaches a reader who is not looking at once, inside a signal.
func TestLazyDeliveryByWhatIsOpen(t *testing.T) {
	r := newLazyRoom(t, 5, Options{})
	tl := &levels{by: map[model.AccountID]string{r.ids[0]: "none", r.ids[1]: "none", r.ids[2]: "none", r.ids[3]: "notify", r.ids[4]: "badge"},
		asked: map[model.AccountID]int{}}
	r.hub.SetTailor(tl)
	looking := r.watch(0, true, []model.ConversationID{r.conv}, 0)
	away := r.watch(1, true, nil, 0)
	old := r.watch(2, false, nil, 0)
	named := r.watch(3, true, nil, 0)
	counted := r.watch(4, true, nil, 0)

	began := time.Now()
	for i := 0; i < 5; i++ {
		r.say(model.EventMessageCreated)
		time.Sleep(20 * time.Millisecond)
	}
	want := "[1 2 3 4 5]"
	eventually(t, "the connection that has it open has every message", func() bool { return fmt.Sprint(looking.fulls()) == want })
	eventually(t, "a client that never says what it has open has every message", func() bool { return fmt.Sprint(old.fulls()) == want })

	// The one who is away: a signal at once, and one more a second later
	// with the rest. Between them the five messages are all counted.
	total := func(v *viewer) (messages int, last int64, mentioned bool) {
		for _, s := range v.signalled() {
			if _, urgent := s["event"]; urgent {
				continue
			}
			n, _ := s["messages"].(float64)
			messages += int(n)
			l, _ := s["last_seq"].(float64)
			last = max(last, int64(l))
			m, _ := s["mentioned"].(bool)
			mentioned = mentioned || m
		}
		return
	}
	eventually(t, "the one who is away is told of all five", func() bool {
		n, last, _ := total(away)
		return n == 5 && last == 5
	})
	if got := away.fulls(); len(got) != 0 {
		t.Errorf("a connection that is not showing the conversation was sent %v in full", got)
	}
	if n := len(away.signalled()); n != 2 {
		t.Errorf("five messages in a tenth of a second made %d signals, want 2 (one at once, one a second later)", n)
	}
	away.mu.Lock()
	if len(away.at) == 2 {
		if gap := away.at[1].Sub(away.at[0]); gap < signalEvery-50*time.Millisecond {
			t.Errorf("two signals for one conversation came %v apart, want at least a second", gap)
		}
		if first := away.at[0].Sub(began); first > 500*time.Millisecond {
			t.Errorf("the first signal took %v", first)
		}
	}
	away.mu.Unlock()
	if _, _, mentioned := total(away); mentioned {
		t.Error("a reader the messages mean nothing to was told they count")
	}
	eventually(t, "a reader whose badge they count for is told so", func() bool {
		n, _, mentioned := total(counted)
		return n == 5 && mentioned
	})

	// The one the messages notify has each of them at once, inside a
	// signal, and none as a frame of its own.
	eventually(t, "the one who is notified has all five at once", func() bool {
		var seqs []int64
		for _, s := range named.signalled() {
			if ev, ok := s["event"].(map[string]any); ok && ev["type"] == model.EventMessageCreated && ev["notify"] == "notify" {
				seq, _ := ev["seq"].(float64)
				seqs = append(seqs, int64(seq))
			}
		}
		return fmt.Sprint(seqs) == want
	})
	if got := named.fulls(); len(got) != 0 {
		t.Errorf("a notified reader who is not showing the conversation was sent %v outside a signal", got)
	}

	// An event that is not message traffic is not held back.
	before := len(away.signalled())
	seq := r.say(model.EventConversationUpdated)
	eventually(t, "a change to the conversation reaches the one who is away at once", func() bool {
		for _, s := range away.signalled()[before:] {
			if ev, ok := s["event"].(map[string]any); ok && ev["type"] == model.EventConversationUpdated && ev["seq"] == float64(seq) {
				return true
			}
		}
		return false
	})

	// A conversation that is not lazy is everyone's in full, as before.
	if _, err := r.store.Conversations().Update(context.Background(), r.conv, func(c *model.Conversation) error {
		c.LazyDelivery = model.LazyOff
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	seq = r.say(model.EventMessageCreated)
	eventually(t, "once it is not lazy the one who is away is sent the message itself", func() bool { return away.last() == seq })

	st := r.hub.LazyStats()
	if st.Full == 0 || st.Signals == 0 || st.Urgent == 0 {
		t.Errorf("the counts are %+v", st)
	}
}

// The size decides when the channel has no answer of its own.
func TestLazyFollowsTheLimit(t *testing.T) {
	r := newRoom(t, 4, Options{})
	r.hub.SetLimits(3, 50)
	away := r.watch(1, true, nil, 0)
	r.say(model.EventMessageCreated)
	eventually(t, "four members above a limit of three: a signal", func() bool { return len(away.signalled()) == 1 })
	if len(away.fulls()) != 0 {
		t.Fatal("sent in full above the limit")
	}
	r.hub.SetLimits(4, 50)
	seq := r.say(model.EventMessageCreated)
	eventually(t, "four members at a limit of four: in full", func() bool { return away.last() == seq })
	r.hub.SetLimits(0, 50)
	seq = r.say(model.EventMessageCreated)
	eventually(t, "a limit of 0 is no limit", func() bool { return away.last() == seq })
}

// A client that opens and closes a conversation while people post in it
// ends up holding every event exactly once, in order: opening fetches the
// difference from where the client says it is, and closing ends the stream
// at one seq.
func TestOpeningAndClosingWhilePostingLosesNothing(t *testing.T) {
	r := newLazyRoom(t, 3, Options{})
	v := r.watch(1, true, nil, 0)
	steady := r.watch(2, true, []model.ConversationID{r.conv}, 0)

	const posts = 600
	var posted atomic.Int64
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < posts; i++ {
			posted.Store(r.say(model.EventMessageCreated))
			time.Sleep(3 * time.Millisecond)
		}
	}()
	rng := rand.New(rand.NewSource(1))
	toggles := 0
	for open := false; ; open = !open {
		select {
		case <-done:
		default:
		}
		if posted.Load() >= posts {
			break
		}
		if open {
			v.open(nil)
		} else {
			// Where what the client holds ends is known for certain only
			// while the conversation is closed: nothing more arrives.
			v.open(map[model.ConversationID]int64{r.conv: v.last()})
		}
		toggles++
		// A client may send twenty frames a second.
		time.Sleep(time.Duration(60+rng.Intn(40)) * time.Millisecond)
	}
	<-done
	v.open(nil)
	v.open(map[model.ConversationID]int64{r.conv: v.last()})
	eventually(t, "the client that opened and closed has the last event", func() bool { return v.last() == posts })
	eventually(t, "the client that stayed has the last event", func() bool { return steady.last() == posts })
	for name, cl := range map[string]*viewer{"opened and closed": v, "stayed": steady} {
		got := cl.fulls()
		if len(got) != posts {
			t.Errorf("the client that %s holds %d events, want %d", name, len(got), posts)
		}
		for i, seq := range got {
			if seq != int64(i+1) {
				t.Fatalf("the client that %s: event %d has seq %d (a gap or one sent twice); around it %v", name, i+1, seq, got[max(0, i-3):min(len(got), i+3)])
			}
		}
	}
	if v.reloads != 0 {
		t.Errorf("%d reloads", v.reloads)
	}
	if toggles < 6 {
		t.Fatalf("the conversation was opened or closed only %d times while posting", toggles)
	}
	t.Logf("%d posts, opened or closed %d times; %+v", posts, toggles, r.hub.LazyStats())
}

// A client far behind when it opens is told to load the conversation again,
// and is then sent what follows.
func TestOpeningFarBehindReloads(t *testing.T) {
	r := newLazyRoom(t, 2, Options{ReplayLimit: 5})
	v := r.watch(1, true, nil, 0)
	for i := 0; i < 8; i++ {
		r.say(model.EventMessageCreated)
	}
	v.open(map[model.ConversationID]int64{r.conv: 0})
	eventually(t, "a reload", func() bool { v.mu.Lock(); defer v.mu.Unlock(); return v.reloads == 1 })
	seq := r.say(model.EventMessageCreated)
	eventually(t, "what follows arrives", func() bool { return fmt.Sprint(v.fulls()) == fmt.Sprint([]int64{seq}) })
}

// Who may open what: only members, at most eight, and after `ready`.
func TestOpenTakesOnlyMembersConversations(t *testing.T) {
	r := newLazyRoom(t, 3, Options{})
	ctx := context.Background()
	other := model.Conversation{Kind: model.KindChannel, Name: "elsewhere", CreatedByID: r.ids[0], LazyDelivery: model.LazyOn}
	if err := r.store.Conversations().Create(ctx, &other); err != nil {
		t.Fatal(err)
	}
	if _, err := r.store.Members().AddDirect(ctx, other.ID, r.ids[0], model.LevelMember, model.Now()); err != nil {
		t.Fatal(err)
	}
	v := r.watch(1, true, nil, 0)
	v.open(map[model.ConversationID]int64{other.ID: -1, r.conv: -1})
	r.hub.mu.RLock()
	var c *conn
	for x := range r.hub.conns[r.ids[1]] {
		c = x
	}
	r.hub.mu.RUnlock()
	c.mu.Lock()
	_, in := c.open[r.conv]
	_, out := c.open[other.ID]
	c.mu.Unlock()
	if !in || out {
		t.Fatalf("open holds the member's conversation: %v, someone else's: %v", in, out)
	}
	r.hub.lazy.viewMu.Lock()
	watching := len(r.hub.lazy.viewers[r.conv])
	r.hub.lazy.viewMu.Unlock()
	if watching != 1 {
		t.Fatalf("%d connections are noted as showing the room", watching)
	}
	v.ws.CloseNow()
	eventually(t, "a closed connection is forgotten", func() bool {
		r.hub.lazy.viewMu.Lock()
		defer r.hub.lazy.viewMu.Unlock()
		return len(r.hub.lazy.viewers[r.conv]) == 0
	})
}

// presence.counts: the people online on the server, and in each open
// conversation above the large_feature_limit, never one by one.
func TestOnlineCounts(t *testing.T) {
	r := newRoom(t, 6, Options{CountsEvery: 100 * time.Millisecond})
	r.hub.SetLimits(1000, 4)
	v := r.watch(0, true, []model.ConversationID{r.conv}, 0)
	latest := func() (server float64, room any) {
		v.mu.Lock()
		defer v.mu.Unlock()
		if len(v.counts) == 0 {
			return -1, nil
		}
		f := v.counts[len(v.counts)-1]
		server, _ = f["online_count"].(float64)
		convs, _ := f["conversations"].(map[string]any)
		return server, convs[string(r.conv)]
	}
	eventually(t, "one person online, here and in the room", func() bool {
		s, n := latest()
		return s == 1 && n == float64(1)
	})
	r.watch(1, false, nil, 0)
	r.watch(2, true, nil, 0)
	eventually(t, "three online", func() bool {
		s, n := latest()
		return s == 3 && n == float64(3)
	})
	// A conversation at or below the limit shows no count.
	r.hub.SetLimits(1000, 6)
	eventually(t, "no count for a small conversation", func() bool {
		s, n := latest()
		return s == 3 && n == nil
	})
	// Nor does an announcement channel, whatever its size.
	r.hub.SetLimits(1000, 4)
	eventually(t, "a count again", func() bool { _, n := latest(); return n == float64(3) })
	if _, err := r.store.Conversations().Update(context.Background(), r.conv, func(c *model.Conversation) error {
		c.Announcement = true
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "no count in an announcement channel", func() bool { _, n := latest(); return n == nil })
	if got := r.hub.OnlineCount(); got != 3 {
		t.Errorf("OnlineCount is %d, want 3", got)
	}
}
