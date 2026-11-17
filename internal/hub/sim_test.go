package hub_test

import (
	"context"
	"crypto/rand"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/link/wire"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/secret"
	"github.com/tikaydaes/churr/internal/testkit"
)

// sim is a server as a hub sees one: it joins a hub with an invitation,
// answers pings, shares and joins channels, and submits envelopes it seals
// with a key the hub never sees. It is no Churr server: it stores nothing
// and shows nobody anything. The tests of the hubs by themselves, and the
// load measurement, drive hubs with these.
type sim struct {
	t    testing.TB
	name string
	keys *secret.Keys
	id   wire.Identity
	url  string
	inv  wire.Invitation
	// skew is added to the sim's clock, in milliseconds.
	skew int64

	mu      sync.Mutex
	conn    *wire.Conn
	clock   *wire.Clock
	homes   map[string]model.HomeClock
	records map[string]wire.ChannelRecord
	got     map[string][]wire.Envelope // delivered, by stream, in arrival order
	errs    []wire.Frame
	replies map[int64]chan wire.Frame
	n       map[string]int64
	lastAt  map[string]int64
	epoch   map[string]int64
	started map[string]int64 // the epoch this sim has begun, by stream
	// stopped is, by stream, the reason of an unsigned stream_close the hub
	// sent: the stream is over for this sim, and its hub says why.
	stopped map[string]string
	// onDeliver, if set, is called for every envelope delivered.
	onDeliver func(env wire.Envelope)
	// quiet stops the sim from starting key epochs it is named for.
	quiet bool
	// light stops the sim from keeping what it is delivered: the load
	// measurement counts and times deliveries and has no use for them.
	light bool
	// ephemeral counts the ephemeral frames delivered, by stream.
	ephemeral map[string]int
	// stalled, while not nil, stops the sim reading: a server that has
	// fallen behind. Closing it lets the sim read again.
	stalled chan struct{}
}

// streamKey is the one stream key every sim uses. A hub never sees it.
var streamKey = make([]byte, 32)

func init() { rand.Read(streamKey) }

func newSim(t testing.TB, th *testkit.TestHub, name string) *sim {
	t.Helper()
	s := newSimSkewed(t, th, name, 0)
	return s
}

func newSimSkewed(t testing.TB, th *testkit.TestHub, name string, skew time.Duration) *sim {
	t.Helper()
	keys, err := secret.Open(filepath.Join(t.TempDir(), "keys"))
	if err != nil {
		t.Fatal(err)
	}
	inv, err := wire.ParseInvitation(th.Invite(name))
	if err != nil {
		t.Fatal(err)
	}
	s := &sim{t: t, name: name, keys: keys, inv: inv, skew: int64(skew / time.Millisecond),
		id:      wire.Identity{Name: name, Kind: wire.KindServer, Keys: keys, URL: "wss://" + name + "/link/v1", Software: "sim"},
		records: map[string]wire.ChannelRecord{}, got: map[string][]wire.Envelope{}, replies: map[int64]chan wire.Frame{},
		n: map[string]int64{}, lastAt: map[string]int64{}, epoch: map[string]int64{}, started: map[string]int64{}}
	s.connect(true)
	t.Cleanup(s.close)
	return s
}

func (s *sim) now() int64 { return int64(model.Now()) + s.skew }

func (s *sim) connect(first bool) {
	s.t.Helper()
	opts := wire.DialOptions{URL: s.inv.URL, Identity: s.id, HTTPClient: testkit.LoopbackClient()}
	if first {
		opts.InviteSecret = s.inv.Secret
	}
	var conn *wire.Conn
	var err error
	for try := 0; try < 100; try++ {
		if conn, err = wire.Dial(context.Background(), opts); err == nil {
			break
		}
		time.Sleep(30 * time.Millisecond)
	}
	if err != nil {
		s.t.Fatalf("%s could not connect: %v", s.name, err)
	}
	s.mu.Lock()
	s.conn = conn
	s.mu.Unlock()
	go s.read(conn)
}

func (s *sim) close() {
	s.mu.Lock()
	conn := s.conn
	s.conn = nil
	s.mu.Unlock()
	if conn != nil {
		conn.Close()
	}
}

// reconnect drops the connection and dials again, as a server does after a
// hub restarts.
func (s *sim) reconnect() {
	s.t.Helper()
	s.close()
	s.connect(false)
}

func (s *sim) read(conn *wire.Conn) {
	for {
		s.mu.Lock()
		stalled := s.stalled
		s.mu.Unlock()
		if stalled != nil {
			<-stalled
		}
		f, _, err := conn.Read(context.Background())
		if err != nil {
			return
		}
		switch f.Frame {
		case wire.FEphemeral:
			if f.Envelope != nil {
				s.mu.Lock()
				if s.ephemeral == nil {
					s.ephemeral = map[string]int{}
				}
				s.ephemeral[f.Envelope.StreamID]++
				s.mu.Unlock()
			}
		case wire.FPing:
			t1 := s.now()
			conn.AnswerPing(f, t1, s.now)
			s.mu.Lock()
			if f.Clock != nil {
				s.clock = f.Clock
			}
			if f.Homes != nil {
				s.homes = f.Homes
			}
			s.mu.Unlock()
		case wire.FChannel:
			var rec wire.ChannelRecord
			if wire.PeekBlock(f, &rec) != nil {
				continue
			}
			s.mu.Lock()
			if rec.Version > s.records[rec.StreamID].Version {
				s.records[rec.StreamID] = rec
			}
			begin := !s.quiet && rec.KeyMaker == s.name && rec.Has(s.name) && rec.Epoch > s.started[rec.StreamID] && !rec.Closed
			if begin {
				s.started[rec.StreamID] = rec.Epoch
			}
			s.mu.Unlock()
			if begin {
				// Named to make the key: the first envelope under the new
				// epoch tells the hub the epoch has begun.
				go s.post(rec.StreamID)
			}
		case wire.FDeliver:
			envs := f.Envelopes
			if f.Envelope != nil {
				envs = append([]wire.Envelope{*f.Envelope}, envs...)
			}
			s.mu.Lock()
			for _, env := range envs {
				if !s.light {
					s.got[env.StreamID] = append(s.got[env.StreamID], env)
				}
				if env.KeyEpoch > s.epoch[env.StreamID] {
					s.epoch[env.StreamID] = env.KeyEpoch
				}
				// As a server does: a time more than 30 seconds ahead is
				// not believed, and does not push this sim's own stamps on.
				if env.At > s.lastAt[env.StreamID] && env.At <= s.now()+30000+600000*boolInt(s.clock == nil)-s.offsetLocked(env.StreamID) {
					s.lastAt[env.StreamID] = env.At
				}
			}
			cb := s.onDeliver
			s.mu.Unlock()
			if cb != nil {
				for _, env := range envs {
					cb(env)
				}
			}
		case wire.FStreamClose:
			if len(f.Signed) == 0 {
				s.mu.Lock()
				if s.stopped == nil {
					s.stopped = map[string]string{}
				}
				s.stopped[f.StreamID] = f.Reason
				s.mu.Unlock()
			}
		case wire.FAccepted, wire.FError, wire.FDirectory, wire.FCaughtUp:
			s.mu.Lock()
			ch := s.replies[f.Re]
			delete(s.replies, f.Re)
			if f.Frame == wire.FError {
				s.errs = append(s.errs, f)
			}
			s.mu.Unlock()
			if ch != nil {
				ch <- f
			}
		}
	}
}

// ask sends a frame and waits for the frame that answers it, or gives up
// after a moment and returns false.
func (s *sim) ask(f wire.Frame, wait time.Duration) (wire.Frame, bool) {
	s.mu.Lock()
	conn := s.conn
	if conn == nil {
		s.mu.Unlock()
		return wire.Frame{}, false
	}
	f.ID = conn.NextID()
	ch := make(chan wire.Frame, 1)
	s.replies[f.ID] = ch
	s.mu.Unlock()
	if conn.Send(f) != nil {
		return wire.Frame{}, false
	}
	select {
	case r := <-ch:
		return r, true
	case <-time.After(wait):
		return wire.Frame{}, false
	}
}

func (s *sim) me() wire.ChannelAccount {
	return wire.ChannelAccount{AccountID: "0193d100-0000-7000-8000-" + fmt.Sprintf("%012x", len(s.name)*7919+int(s.name[0])), Server: s.name, Username: "owner"}
}

// create shares a channel to the sim's hub and waits for its record.
func (s *sim) create(name, listing string) string {
	s.t.Helper()
	id := model.NewID[string]()
	f, err := wire.SignBlock(wire.FChannelCreate, wire.ChannelCreate{StreamID: id, Name: name, Listing: listing, Creator: s.me(), At: model.Now()}, s.keys)
	if err != nil {
		s.t.Fatal(err)
	}
	s.mu.Lock()
	s.started[id], s.epoch[id] = 1, 1
	s.mu.Unlock()
	if r, ok := s.ask(f, 300*time.Millisecond); ok && r.Frame == wire.FError {
		s.t.Fatalf("%s could not create a channel: %s %s", s.name, r.Code, r.Message)
	}
	testkit.Eventually(s.t, "the new channel's record reaches "+s.name, func() bool { return s.record(id).Version >= 1 })
	return id
}

func (s *sim) record(stream string) wire.ChannelRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.records[stream]
}

// act sends a moderator's action and returns the hub's refusal, if it
// refuses: "" means the hub did not say no within the wait.
func (s *sim) act(stream, action string, fill func(a *wire.ChannelAction)) string {
	s.t.Helper()
	a := wire.ChannelAction{StreamID: stream, ActionID: model.NewID[string](), Action: action, By: s.me(), At: model.Now()}
	if fill != nil {
		fill(&a)
	}
	f, err := wire.SignBlock(wire.FChannelAction, a, s.keys)
	if err != nil {
		s.t.Fatal(err)
	}
	if r, ok := s.ask(f, 400*time.Millisecond); ok && r.Frame == wire.FError {
		return r.Code
	}
	return ""
}

// join asks to be in a channel and returns the hub's refusal, if any.
func (s *sim) join(stream string) string {
	s.t.Helper()
	f, err := wire.SignBlock(wire.FJoin, wire.ChannelAction{StreamID: stream, ActionID: model.NewID[string](), Server: s.name, At: model.Now()}, s.keys)
	if err != nil {
		s.t.Fatal(err)
	}
	if r, ok := s.ask(f, 400*time.Millisecond); ok && r.Frame == wire.FError {
		return r.Code
	}
	return ""
}

// in waits until the sim is in a channel and the epoch that began when it
// joined has started.
func (s *sim) in(stream string) {
	s.t.Helper()
	testkit.Eventually(s.t, s.name+" is in the channel", func() bool { return s.record(stream).Has(s.name) })
}

func boolInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

// offsetLocked is offset for a caller that holds the sim's lock; 0 when the
// sim has not been told.
func (s *sim) offsetLocked(stream string) int64 {
	if s.clock == nil {
		return 0
	}
	home := s.records[stream].HomeHub
	if home == "" || home == s.inv.Name {
		return s.clock.Offset
	}
	return s.homes[home].Offset
}

// offset is the sim's clock against the clock of a channel's home hub, as
// its own hub last told it.
func (s *sim) offset(stream string) (int64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.clock == nil {
		return 0, false
	}
	home := s.records[stream].HomeHub
	if home == "" || home == s.inv.Name {
		return s.clock.Offset, true
	}
	hc, ok := s.homes[home]
	return hc.Offset, ok
}

// seal makes the sim's next envelope in a stream, stamped at a time by the
// home hub's clock (0: now, as the sim reads it).
func (s *sim) seal(stream string, at int64, text string) wire.Envelope {
	s.t.Helper()
	var offset int64
	testkit.Eventually(s.t, s.name+" has been told its clock", func() bool {
		var ok bool
		offset, ok = s.offset(stream)
		return ok
	})
	s.mu.Lock()
	if at == 0 {
		at = s.now() - offset
		if at <= s.lastAt[stream] {
			at = s.lastAt[stream] + 1
		}
	}
	s.lastAt[stream] = max(s.lastAt[stream], at)
	s.n[stream]++
	n := s.n[stream]
	epoch := max(s.epoch[stream], s.started[stream], 1)
	s.mu.Unlock()
	env, err := wire.SealEnvelope(streamKey, stream, epoch, s.name, model.NewID[string](), wire.Stamp{At: at, N: n, Offset: offset},
		[]byte(`{"type":"message.created","text":"`+text+`"}`), s.keys)
	if err != nil {
		s.t.Fatal(err)
	}
	return env
}

// submit sends envelopes and returns the hub's answer.
func (s *sim) submit(envs ...wire.Envelope) wire.Frame {
	s.t.Helper()
	r, ok := s.ask(wire.Frame{Frame: wire.FSubmit, Envelopes: envs}, 5*time.Second)
	if !ok {
		return wire.Frame{Frame: wire.FError, Code: "no_answer"}
	}
	return r
}

// post makes and submits one envelope now, and returns the refusal code, ""
// if it was taken.
func (s *sim) post(stream string) string {
	return s.say(stream, "")
}

// say is post with a text to find again (or to prove a hub never held).
func (s *sim) say(stream, text string) string {
	for try := 0; ; try++ {
		r := s.submit(s.seal(stream, 0, text))
		if r.Code == wire.EEarlyEpoch && try < 200 {
			// The key-maker has not begun the epoch yet.
			s.mu.Lock()
			s.n[stream]-- // the counter was not used
			s.epoch[stream] = 0
			s.mu.Unlock()
			time.Sleep(20 * time.Millisecond)
			continue
		}
		if r.Code == wire.EStaleEpoch && try < 200 {
			// A newer epoch has begun: this one is sealed under it.
			s.mu.Lock()
			s.n[stream]--
			s.epoch[stream] = s.records[stream].Epoch
			s.mu.Unlock()
			continue
		}
		return r.Code
	}
}

// received is what the sim has been delivered of a stream, as "origin/n".
func (s *sim) received(stream string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for _, env := range s.got[stream] {
		out = append(out, fmt.Sprintf("%s/%d", env.Origin, env.N))
	}
	return out
}

func (s *sim) count(stream string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.got[stream])
}

// catchUp asks the hub for everything of a stream above what the sim says
// it holds.
func (s *sim) catchUp(stream string, have map[string]int64) (wire.Frame, bool) {
	return s.ask(wire.Frame{Frame: wire.FCatchUp, StreamID: stream, Have: have}, 5*time.Second)
}

// stall stops the sim reading what the hub sends it, as a server that has
// fallen behind; the function returned lets it read again.
func (s *sim) stall() (resume func()) {
	ch := make(chan struct{})
	s.mu.Lock()
	s.stalled = ch
	s.mu.Unlock()
	return func() {
		s.mu.Lock()
		s.stalled = nil
		s.mu.Unlock()
		close(ch)
	}
}

// tell sends one ephemeral frame in a stream: typing or presence, sealed
// like anything else and carrying no stamp.
func (s *sim) tell(stream string) {
	s.t.Helper()
	s.mu.Lock()
	epoch := max(s.epoch[stream], s.started[stream], 1)
	conn := s.conn
	s.mu.Unlock()
	env, err := wire.SealEnvelope(streamKey, stream, epoch, s.name, model.NewID[string](), wire.Stamp{},
		[]byte(`{"type":"presence.changed","account_id":"0193a001-0000-7000-8000-000000000001","presence":"online"}`), s.keys)
	if err != nil {
		s.t.Fatal(err)
	}
	if err := conn.Send(wire.Frame{Frame: wire.FEphemeral, Envelope: &env}); err != nil {
		s.t.Fatal(err)
	}
}

// told is how many ephemeral frames of a stream the sim has been delivered.
func (s *sim) told(stream string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ephemeral[stream]
}
