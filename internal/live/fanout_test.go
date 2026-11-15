package live

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/perm"
	"github.com/tikaydaes/churr/internal/store"
	"github.com/tikaydaes/churr/internal/store/storetest"
)

// One event is handed out on several goroutines (publish.go). Every
// connection must still be handed a conversation's events in seq order,
// each once, whatever part of the recipients it fell into for each event;
// and readers with the same `notify` must still share one encoding.
func TestFanOutInPartsKeepsOrder(t *testing.T) {
	const members, posts = 24, 400
	r := newRoom(t, members, Options{fanoutWorkers: 8, fanoutEach: 2, QueueFrames: 4 * posts})
	if got := fanoutParts(members, members, 8, 2); got != 8 {
		t.Fatalf("twenty-four connections are handed out in %d parts, want 8", got)
	}
	by := map[model.AccountID]string{}
	for i, id := range r.ids {
		by[id] = []string{"none", "badge", "notify"}[i%3]
	}
	r.hub.SetTailor(&levels{by: by, asked: map[model.AccountID]int{}})
	clients := make([]*resumer, members)
	for i := range clients {
		clients[i] = r.open(i)
		r.resume(clients[i], 0)
		clients[i].waitReady(t)
	}
	// As fast as they can be written, one after another, as chat.Tx
	// publishes them.
	for i := 0; i < posts; i++ {
		r.post()
	}
	for i, cl := range clients {
		eventually(t, fmt.Sprintf("client %d has every event", i), func() bool { return len(cl.got()) >= posts })
		got := cl.got()
		if len(got) != posts {
			t.Fatalf("client %d was sent %d events, want %d", i, len(got), posts)
		}
		for j, seq := range got {
			if seq != int64(j+1) {
				t.Fatalf("client %d: event %d has seq %d: out of order, missing or twice; around it %v", i, j+1, seq, got[max(0, j-3):min(len(got), j+3)])
			}
		}
	}
}

func TestFanOutParts(t *testing.T) {
	for _, c := range []struct{ targets, conns, workers, each, want int }{
		{10, 10, 16, 0, 1},        // too few to be worth a second goroutine
		{600, 1100, 16, 0, 2},     // one for every 512 connections
		{50000, 50000, 16, 0, 16}, // at most one for each processor
		{50000, 50000, 4, 0, 4},
		{3, 5000, 16, 0, 3}, // never more than there are recipients
		{0, 0, 16, 0, 1},
	} {
		if got := fanoutParts(c.targets, c.conns, c.workers, c.each); got != c.want {
			t.Errorf("fanoutParts(%d, %d, %d, %d) = %d, want %d", c.targets, c.conns, c.workers, c.each, got, c.want)
		}
	}
}

// What several parts share: the same difference gives the same bytes.
func TestSharedFormsGiveOneEncoding(t *testing.T) {
	sh := &sharedForms{base: []byte(`{"type":"message.created","seq":3}`), log: func(error) {}}
	a := sh.spliced("notify", "badge", true, map[string]any{"notify": "badge"}, nil)
	b := sh.spliced("notify", "badge", true, map[string]any{"notify": "badge"}, nil)
	c := sh.spliced("notify", "none", true, map[string]any{"notify": "none"}, nil)
	if &a[0] != &b[0] || &a[0] == &c[0] || string(a) == string(c) {
		t.Fatal("readers with the same notify do not share one encoding, or readers with another do")
	}
	bad := &sharedForms{base: []byte(`not json`), log: func(error) {}}
	if bad.spliced("notify", "none", true, map[string]any{"notify": "none"}, nil) != nil {
		t.Fatal("an event that cannot be read was handed out")
	}
}

// fixedLevels is a Tailor that answers from a map nobody writes, so it
// does not make the goroutines of a fan-out wait for one another.
type fixedLevels map[model.AccountID]string

func (l fixedLevels) Tailor(_ context.Context, e model.Event, to model.AccountID, _ bool) map[string]any {
	return levelFields[l[to]]
}

// TailorFanout answers for many at once, as the chat service does.
func (l fixedLevels) TailorFanout(_ context.Context, e model.Event, to []model.AccountID) []map[string]any {
	out := make([]map[string]any, len(to))
	for i, id := range to {
		out[i] = levelFields[l[id]]
	}
	return out
}

// BenchmarkPublish hands one message to every member of a channel, each
// with one connection that takes everything at once, on one goroutine and
// on one for each processor. ns/op is one message to everyone.
func BenchmarkPublish(b *testing.B) {
	ctx := context.Background()
	for _, n := range []int{2000, 20000} {
		s := storetest.OpenSQLite(b)
		conv := model.Conversation{Kind: model.KindChannel, Name: "big"}
		accs := make([]model.Account, n)
		if err := s.Tx(ctx, func(tx store.Tx) error {
			if err := tx.Conversations().Create(ctx, &conv); err != nil {
				return err
			}
			for i := range accs {
				accs[i] = model.Account{Username: fmt.Sprintf("b%06d", i), DisplayName: "B", Role: model.RoleMember}
				if _, err := tx.Accounts().Create(ctx, &accs[i]); err != nil {
					return err
				}
				if _, err := tx.Members().AddDirect(ctx, conv.ID, accs[i].ID, model.LevelMember, model.Now()); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			b.Fatal(err)
		}
		for _, workers := range []int{1, runtime.GOMAXPROCS(0)} {
			b.Run(fmt.Sprintf("conns=%d/goroutines=%d", n, workers), func(b *testing.B) {
				h := New(perm.New(s), s, Options{fanoutWorkers: workers, QueueFrames: 1 << 30, QueueBytes: 1 << 40})
				h.SetLimits(0, 0)
				by := map[model.AccountID]string{}
				for i, a := range accs {
					by[a.ID] = []string{"none", "none", "none", "badge"}[i%4]
					c := &conn{hub: h, account: a, flusher: flusherFor(), started: true, deflate: true,
						nc: &wireConn{Conn: discard{}, sent: &h.wire.bytes, try: func(p []byte) (int, error) { return len(p), nil }}}
					h.conns[a.ID] = map[*conn]struct{}{c: {}}
				}
				h.SetTailor(fixedLevels(by))
				e := model.Event{Type: model.EventMessageCreated, ConversationID: conv.ID, At: model.Now(), MessageID: model.NewID[model.MessageID](),
					Payload: map[string]any{"message": map[string]any{"text": "Morning all. The deploy went out at nine and the dashboards look fine so far."}}}
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					e.Seq = int64(i + 1)
					h.Publish(ctx, e)
				}
			})
		}
	}
}

// manyLevels is a FanoutTailor: it answers for all the readers of one part
// of a fan-out together, and counts how it was asked.
type manyLevels struct {
	fixedLevels
	together, alone atomic.Int64
}

func (l *manyLevels) Tailor(ctx context.Context, e model.Event, to model.AccountID, live bool) map[string]any {
	l.alone.Add(1)
	if e.Type != model.EventMessageCreated {
		return nil
	}
	return l.fixedLevels.Tailor(ctx, e, to, live)
}

func (l *manyLevels) TailorFanout(ctx context.Context, e model.Event, to []model.AccountID) []map[string]any {
	l.together.Add(1)
	return l.fixedLevels.TailorFanout(ctx, e, to)
}

// Messages are judged for all the readers of a part at once, the readers
// are looked up by the goroutine that hands the message to them, and the
// flushers are woken once for a run of them (publish.go, flush.go). None of
// that may change what a connection is sent: every event of the
// conversation once, in seq order, messages and other events alike, on
// every connection of an account, each message with that reader's own
// `notify`.
func TestFanOutJudgedTogetherKeepsOrder(t *testing.T) {
	const members, posts = 24, 300
	r := newRoom(t, members, Options{fanoutWorkers: 8, fanoutEach: 2, QueueFrames: 4 * posts})
	by := fixedLevels{}
	for i, id := range r.ids {
		by[id] = []string{"none", "badge", "notify"}[i%3]
	}
	tailor := &manyLevels{fixedLevels: by}
	r.hub.SetTailor(tailor)
	// Two connections for every third account, so some readers are more
	// than one connection.
	var clients []*resumer
	var whose []int
	for i := 0; i < members; i++ {
		for n := 0; n < 1+(i+1)%3/2; n++ {
			cl := r.open(i)
			r.resume(cl, 0)
			cl.waitReady(t)
			clients = append(clients, cl)
			whose = append(whose, i)
		}
	}
	if len(clients) <= members {
		t.Fatalf("%d connections for %d accounts: none has two", len(clients), members)
	}
	// Two writers at once, as two requests are: the room's own lock puts
	// them in seq order, as chat.Tx does.
	var wg sync.WaitGroup
	for w := 0; w < 2; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < posts/2; i++ {
				if (i+w)%3 == 0 {
					r.say("test.posted")
				} else {
					r.say(model.EventMessageCreated)
				}
			}
		}()
	}
	wg.Wait()
	for i, cl := range clients {
		eventually(t, fmt.Sprintf("connection %d has every event", i), func() bool { return len(cl.all()) >= posts })
		got := cl.all()
		if len(got) != posts {
			t.Fatalf("connection %d was sent %d events, want %d", i, len(got), posts)
		}
		for j, seq := range got {
			if seq != int64(j+1) {
				t.Fatalf("connection %d: event %d has seq %d: out of order, missing or twice; around it %v", i, j+1, seq, got[max(0, j-3):min(len(got), j+3)])
			}
		}
		want := by[r.ids[whose[i]]]
		for _, level := range cl.levels() {
			if level != want {
				t.Fatalf("connection %d of account %d was sent a message with notify %q, want %q", i, whose[i], level, want)
			}
		}
	}
	if tailor.together.Load() == 0 || tailor.alone.Load() >= int64(posts*members/2) {
		t.Fatalf("messages were judged %d times for a part at once and %d times reader by reader: messages are not judged together",
			tailor.together.Load(), tailor.alone.Load())
	}
}
