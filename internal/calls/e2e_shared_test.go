package calls_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/calls"
	"github.com/tikaydaes/churr/internal/calls/calltest"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
	"github.com/tikaydaes/churr/internal/testkit"
)

// fakeLink stands in for the link between two servers: what one side hands
// it, it hands to the other side's calls service, as the link package will
// with sealed direct frames and the stream. It is one end; two of them,
// pointing at each other, are a link.
type fakeLink struct {
	// self is this server's name; other is the server at the far end.
	self  string
	other *calls.Server
	// selfID is this server's row in the other server's `servers` table.
	selfID model.ServerID
	down   atomic.Bool
	heard  atomic.Int64

	mu     sync.Mutex
	queue  chan func()
	passes int
	asked  []model.AccountID
	// signals counts call_signal frames by their `type`.
	signals map[string]int
}

func newFakeLink(self string) *fakeLink {
	l := &fakeLink{self: self, queue: make(chan func(), 1024), signals: map[string]int{}}
	l.heard.Store(time.Now().UnixNano())
	// One at a time, in order, like frames on a connection.
	go func() {
		for fn := range l.queue {
			fn()
		}
	}()
	return l
}

var errLinkDown = errors.New("the link is down")

func (l *fakeLink) RequestPass(ctx context.Context, host string, req calls.PassRequest) (calls.Pass, error) {
	if l.down.Load() {
		return calls.Pass{}, errLinkDown
	}
	l.mu.Lock()
	l.passes++
	l.asked = append(l.asked, req.AccountID)
	l.mu.Unlock()
	return l.other.PassRequested(ctx, l.self, req)
}

func (l *fakeLink) Signal(_ context.Context, to string, sig calls.Signal) error {
	if l.down.Load() {
		return errLinkDown
	}
	l.queue <- func() { l.other.Signalled(context.Background(), l.self, sig) }
	return nil
}

func (l *fakeLink) Submit(_ context.Context, conv model.ConversationID, e calls.StreamEvent) error {
	if l.down.Load() {
		return errLinkDown
	}
	l.queue <- func() { l.other.StreamEvent(context.Background(), l.self, l.selfID, conv, e) }
	return nil
}

func (l *fakeLink) LastHeard(string) time.Time {
	if l.down.Load() {
		return time.Unix(0, l.heard.Load())
	}
	return time.Now()
}

// cut takes the link down, or with false brings it back.
func (l *fakeLink) cut(down bool) {
	l.heard.Store(time.Now().UnixNano())
	l.down.Store(down)
}

// twoServers is two servers that share one channel: ann lives on a; bob, bix
// and bea (a reader) on b; and each server holds the conversation under the same
// identifier with shadows of the other's people, as the link leaves them.
type twoServers struct {
	a, b       *testkit.TestServer
	ann        *testkit.Client
	bob, bea   *testkit.Client
	bix        *testkit.Client
	conv       string
	toB, toA   *fakeLink
	aOnB, bOnA model.ServerID
}

func shareAChannel(t *testing.T, tm calls.Timings) *twoServers {
	t.Helper()
	ctx := context.Background()
	w := &twoServers{a: server(t, tm), b: server(t, tm)}
	w.ann, w.bob, w.bea, w.bix = w.a.Member("ann"), w.b.Member("bob"), w.b.Member("bea"), w.b.Member("bix")
	pin := func(ts *testkit.TestServer, name string) model.ServerID {
		s, _, err := ts.Server.Store.Peers().PinServer(ctx, store.Server{Name: name, SignKey: make([]byte, 32), BoxKey: make([]byte, 32)})
		if err != nil {
			t.Fatal(err)
		}
		return s.ID
	}
	w.bOnA, w.aOnB = pin(w.a, "b.example"), pin(w.b, "a.example")
	shadow := func(ts *testkit.TestServer, of *testkit.Client, home model.ServerID) {
		acc := model.Account{ID: of.ID, Username: of.Username, DisplayName: of.Username, Role: model.RoleRemote, ServerID: home, ProfileVersion: 1}
		if _, err := ts.Server.Store.Streams().PutShadow(ctx, &acc); err != nil {
			t.Fatal(err)
		}
	}
	shadow(w.a, w.bob, w.bOnA)
	shadow(w.a, w.bea, w.bOnA)
	shadow(w.a, w.bix, w.bOnA)
	shadow(w.b, w.ann, w.aOnB)
	id := model.NewID[model.ConversationID]()
	stream := model.NewID[model.StreamID]()
	for _, ts := range []*testkit.TestServer{w.a, w.b} {
		c := model.Conversation{ID: id, Kind: model.KindChannel, Name: "shared", Private: true, CreatedByID: w.ann.ID, StreamID: stream}
		if err := ts.Server.Store.Conversations().Create(ctx, &c); err != nil {
			t.Fatal(err)
		}
		// The manager first: a channel is never left without one, and
		// whoever is added to an empty channel would be made it.
		for _, m := range []struct {
			who   model.AccountID
			level model.Level
		}{{w.ann.ID, model.LevelManager}, {w.bob.ID, model.LevelMember}, {w.bix.ID, model.LevelMember}, {w.bea.ID, model.LevelReader}} {
			if _, err := ts.Server.Store.Members().AddDirect(ctx, id, m.who, m.level, model.Now()); err != nil {
				t.Fatal(err)
			}
		}
	}
	w.conv = string(id)
	w.toB, w.toA = newFakeLink("a.example"), newFakeLink("b.example")
	w.toB.other, w.toB.selfID = w.b.Server.Calls, w.aOnB
	w.toA.other, w.toA.selfID = w.a.Server.Calls, w.bOnA
	w.a.Server.Calls.SetLink(w.toB)
	w.b.Server.Calls.SetLink(w.toA)
	return w
}

// A call in a shared channel lives on the starter's server. A member of
// another server joins with a pass got over the link, its signalling is
// relayed between the two servers, and its media goes straight to the host.
func TestCallInASharedChannel(t *testing.T) {
	w := shareAChannel(t, fast())
	ctx := context.Background()
	// Bob chose Any activity for the channel on his own server, so his
	// own server rings him.
	w.bob.Must(200, "PATCH", convPath+w.conv+"/membership", map[string]any{"mode": "any"})
	bobWatch, _ := w.bob.Token().Events(nil)

	// Ann starts the call on her server: the host.
	a := enter(t, w.ann, w.conv, calltest.Options{})
	a.Publish("microphone")
	// Bob's server hears of it on the stream and tells its own members.
	started := bobWatch.Expect("call.started")
	call := obj(started, "call")
	if call["id"] != a.CallID || call["hosted_by"] != "a.example" || call["started_by_id"] != string(w.ann.ID) || call["conversation_id"] != w.conv {
		t.Fatalf("call.started on the other server: %v", started)
	}
	if got := obj(w.bob.Must(200, "GET", convPath+w.conv+"/call", nil), "call"); got["hosted_by"] != "a.example" || len(got["participants"].([]any)) != 1 {
		t.Fatalf("the call as the other server shows it: %v", got)
	}
	if got := obj(w.ann.Must(200, "GET", convPath+w.conv+"/call", nil), "call"); got["hosted_by"] != nil {
		t.Fatalf("the host does not name itself: %v", got)
	}
	// Each server keeps its own row; only the other's names a host.
	row, err := w.b.Server.Store.Calls().Get(ctx, model.CallID(a.CallID))
	if err != nil || row.HostServerID != w.aOnB || row.HostName != "a.example" || !row.Live() {
		t.Fatalf("the row on the other server: %+v %v", row, err)
	}
	if row, _ := w.a.Server.Store.Calls().Get(ctx, model.CallID(a.CallID)); row.HostServerID != "" {
		t.Fatalf("the row on the host: %+v", row)
	}
	// Ringing is each server's own decision.
	bobWatch.ExpectWhere("call.ring_started", is("call_id", a.CallID))

	// Bob asks his own server to join. It gets a pass from the host, and
	// the pass is the ticket.
	stream, _ := w.bob.Events(nil)
	b := calltest.New(t, stream, calltest.Options{})
	t.Cleanup(b.Close)
	id, pass := ticket(t, w.bob, w.conv)
	if id != a.CallID || len(pass) != 43 || w.toA.passes != 1 {
		t.Fatalf("the pass: call %s, %d characters, %d requests", id, len(pass), w.toA.passes)
	}
	// A reader is refused by their own server, which never asks the host.
	w.bea.Refused(403, model.CodeForbidden, "POST", convPath+w.conv+"/call/join", nil)
	if w.toA.passes != 1 {
		t.Fatalf("a pass was asked for on behalf of a reader: asked for %v", w.toA.asked)
	}
	// And by the host, were their server to ask: its own copy says reader.
	if _, err := w.a.Server.Calls.PassRequested(ctx, "b.example", calls.PassRequest{CallID: model.CallID(a.CallID), AccountID: w.bea.ID}); model.CodeOf(err) != model.CodeForbidden {
		t.Fatalf("a pass for a reader: %v", err)
	}
	// The host gives a pass only to an account's own server, only for a
	// live call, only for a member.
	host := w.a.Server.Calls
	req := calls.PassRequest{ConversationID: model.ConversationID(w.conv), CallID: model.CallID(a.CallID), AccountID: w.bob.ID}
	if _, err := host.PassRequested(ctx, "c.example", req); model.CodeOf(err) != model.CodeForbidden {
		t.Fatalf("a pass asked for by a server that is not the account's own: %v", err)
	}
	if _, err := host.PassRequested(ctx, "b.example", calls.PassRequest{CallID: model.NewID[model.CallID](), AccountID: w.bob.ID}); model.CodeOf(err) != model.CodeNotFound {
		t.Fatalf("a pass for a call that is not live: %v", err)
	}
	if _, err := host.PassRequested(ctx, "b.example", calls.PassRequest{CallID: model.CallID(a.CallID), AccountID: w.ann.ID}); model.CodeOf(err) != model.CodeForbidden {
		t.Fatalf("a pass for a local account: %v", err)
	}
	good, err := host.PassRequested(ctx, "b.example", req)
	if err != nil || good.CallID != model.CallID(a.CallID) || good.AccountID != w.bob.ID || good.Media.UDPPort != host.Port() || good.Media.TCPPort != host.Port() ||
		good.Media.Host == "" || good.ExpiresAt.Before(model.Now()) {
		t.Fatalf("a pass: %+v %v", good, err)
	}

	// call.join travels to the host as a call_signal and its answer comes
	// back the same way. A wrong pass gets the host's refusal.
	if f := b.Join(id, "not-the-pass"); f["code"] != "ticket_invalid" {
		t.Fatalf("joining with a wrong pass: %v", f)
	}
	ok := b.MustJoin(id, pass)
	if len(obj(ok, "call")["participants"].([]any)) != 2 || obj(ok, "limits")["max_participants"] != 50.0 {
		t.Fatalf("the host's answer to call.join, relayed: %v", ok)
	}
	if f := b.Join(id, pass); f["code"] != "ticket_invalid" {
		t.Fatalf("a pass used twice: %v", f)
	}
	// Both servers tell their own members that Bob joined.
	bobWatch.ExpectWhere("call.participant_joined", func(f map[string]any) bool { return obj(f, "participant")["account_id"] == string(w.bob.ID) })
	if f := bobWatch.Expect("call.ring_stopped"); f["reason"] != "joined_elsewhere" {
		t.Fatalf("bob's other device when he joined: %v", f)
	}
	if got := obj(w.ann.Must(200, "GET", convPath+w.conv+"/call", nil), "call"); len(got["participants"].([]any)) != 2 {
		t.Fatalf("the host's participants: %v", got)
	}

	// Offers, answers and subscriptions are relayed; media goes straight
	// to the host's media port.
	if f := b.Publish("microphone", "camera"); f["type"] != "call.answer" {
		t.Fatalf("an offer relayed to the host: %v", f)
	}
	a.Expect("call.track_published", func(f map[string]any) bool {
		return f["account_id"] == string(w.bob.ID) && obj(f, "track")["source"] == "camera"
	})
	a.Subscribe([3]any{b.TrackIDs["camera"], 640, 360})
	eventually(t, "media both ways through the host", func() bool {
		return a.Stream(b.TrackIDs["microphone"]).Packets.Load() > 10 && b.Stream(a.TrackIDs["microphone"]).Packets.Load() > 10 &&
			a.Stream(b.TrackIDs["camera"]).Packets.Load() > 5
	})
	if _, addr, err := b.Selected(); err != nil || addr != w.a.Server.Calls.MediaAddress(ctx) {
		t.Fatalf("bob's media goes to %s, the host is at %s (%v)", addr, w.a.Server.Calls.MediaAddress(ctx), err)
	}

	// Bob's connection to his own server drops. The host holds him while
	// his media is alive, and he resumes through his server.
	b.Detach()
	stream.Close()
	time.Sleep(1800 * time.Millisecond)
	if got := obj(w.ann.Must(200, "GET", convPath+w.conv+"/call", nil), "call"); len(got["participants"].([]any)) != 2 {
		t.Fatalf("a relayed participant with live media was dropped: %v", got)
	}
	again, _ := w.bob.Events(nil)
	b.Attach(again)
	if f := b.Resume(); f["type"] != "call.ok" {
		t.Fatalf("call.resume relayed: %v", f)
	}
	b.OK(map[string]any{"type": "call.mute", "source": "camera", "muted": true})
	a.Expect("call.mute_changed", is("account_id", string(w.bob.ID)))

	// The link goes down: signalling waits, media already flowing carries
	// on, and a new join cannot be had.
	w.toA.cut(true)
	b.Refused("host_unreachable", map[string]any{"type": "call.mute", "source": "camera", "muted": false})
	w.bix.Refused(503, model.CodeUnavailable, "POST", convPath+w.conv+"/call/join", nil)
	n := a.Stream(b.TrackIDs["microphone"]).Packets.Load()
	eventually(t, "media while the link is down", func() bool { return a.Stream(b.TrackIDs["microphone"]).Packets.Load() > n+20 })
	w.toA.cut(false)

	// Bob leaves with the operation, for a client whose connection went.
	w.bob.Must(204, "POST", convPath+w.conv+"/call/leave", nil)
	if f := bobWatch.ExpectWhere("call.participant_left", is("account_id", string(w.bob.ID))); f["reason"] != "left" {
		t.Fatalf("call.participant_left on the other server: %v", f)
	}
	w.bob.Refused(404, model.CodeNotFound, "POST", convPath+w.conv+"/call/leave", nil)
	// The host ends the call; the other server ends its copy.
	a.Leave()
	if f := bobWatch.Expect("call.ended"); f["reason"] != "empty" || f["call_id"] != a.CallID {
		t.Fatalf("call.ended on the other server: %v", f)
	}
	eventually(t, "the other server's row is closed", func() bool {
		row, err := w.b.Server.Store.Calls().Get(ctx, model.CallID(a.CallID))
		return err == nil && !row.Live()
	})
	if out := w.bob.Must(200, "GET", convPath+w.conv+"/call", nil); out["call"] != nil {
		t.Fatalf("the other server still shows the call: %v", out)
	}
}

// A server that has heard nothing from the host for the documented time
// ends the call locally.
func TestHostGoesSilent(t *testing.T) {
	tm := fast()
	tm.HostSilence = 700 * time.Millisecond
	w := shareAChannel(t, tm)
	watch, _ := w.bob.Events(nil)
	a := enter(t, w.ann, w.conv, calltest.Options{})
	watch.Expect("call.started")
	// While the link is up the call stays, however quiet it is.
	time.Sleep(1200 * time.Millisecond)
	if out := w.bob.Must(200, "GET", convPath+w.conv+"/call", nil); out["call"] == nil {
		t.Fatal("a quiet call on a live link was ended")
	}
	w.toA.cut(true)
	w.toB.cut(true)
	if f := watch.Expect("call.ended"); f["reason"] != "server" || f["call_id"] != a.CallID {
		t.Fatalf("call.ended after the host went silent: %v", f)
	}
	// The host knows nothing of it and carries on.
	if out := w.ann.Must(200, "GET", convPath+w.conv+"/call", nil); out["call"] == nil {
		t.Fatal("the host's own call ended")
	}
}

// Two people on different servers start a call at the same moment: the one
// the stream puts first is the call. The other server ends its local call
// and offers its member the real one.
func TestTwoServersStartAtOnce(t *testing.T) {
	w := shareAChannel(t, fast())
	// Bob's server has not heard of Ann's call when Bob starts his own.
	w.toB.cut(true)
	a := enter(t, w.ann, w.conv, calltest.Options{})
	w.toA.cut(true) // and Bob's start does not reach Ann's server either
	watch, _ := w.bob.Token().Events(nil)
	b := enter(t, w.bob, w.conv, calltest.Options{})
	if b.CallID == a.CallID {
		t.Fatal("the two servers should each have started a call of their own")
	}
	w.toB.cut(false)
	w.toA.cut(false)
	// The stream delivers Ann's call.started to Bob's server: hers was
	// first.
	w.b.Server.Calls.StreamEvent(context.Background(), "a.example", w.aOnB, model.ConversationID(w.conv), calls.StreamEvent{
		Type: calls.EventStarted, At: model.Now(),
		Call: &model.Call{ID: model.CallID(a.CallID), ConversationID: model.ConversationID(w.conv), StartedByID: w.ann.ID, StartedAt: model.Now(),
			Participants: []model.CallParticipant{{AccountID: w.ann.ID, JoinedAt: model.Now()}}}})
	if f := watch.ExpectWhere("call.ended", is("call_id", b.CallID)); f["reason"] != "server" {
		t.Fatalf("the local call that lost: %v", f)
	}
	real := watch.ExpectWhere("call.started", func(f map[string]any) bool { return obj(f, "call")["id"] == a.CallID })
	if obj(real, "call")["hosted_by"] != "a.example" {
		t.Fatalf("the real call, offered to the member: %v", real)
	}
	// Bob joins the real one.
	stream, _ := w.bob.Events(nil)
	b2 := calltest.New(t, stream, calltest.Options{})
	t.Cleanup(b2.Close)
	id, pass := ticket(t, w.bob, w.conv)
	if id != a.CallID {
		t.Fatalf("joining after the race gave call %s, want %s", id, a.CallID)
	}
	b2.MustJoin(id, pass)
}
