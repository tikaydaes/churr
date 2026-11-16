package link_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/link"
	"github.com/tikaydaes/churr/internal/link/wire"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
	"github.com/tikaydaes/churr/internal/testkit"
)

// fakeCalls plays the calls package on one server.
type fakeCalls struct {
	mu      sync.Mutex
	refuse  error
	asked   []string
	signals []link.CallSignal
	events  []string
}

func (f *fakeCalls) PassRequested(ctx context.Context, from string, req link.CallPassRequest) (link.CallPass, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked = append(f.asked, from+" for "+string(req.AccountID))
	if f.refuse != nil {
		return link.CallPass{}, f.refuse
	}
	return link.CallPass{CallID: req.CallID, AccountID: req.AccountID, Pass: "a-pass", ExpiresAt: model.Now().Add(time.Minute),
		Media: link.CallMedia{Host: "a.localhost", UDPPort: 24877, TCPPort: 24877}}, nil
}

func (f *fakeCalls) Signalled(ctx context.Context, from string, sig link.CallSignal) {
	f.mu.Lock()
	f.signals = append(f.signals, sig)
	f.mu.Unlock()
}

func (f *fakeCalls) StreamEvent(ctx context.Context, host string, hostID model.ServerID, conv model.ConversationID, eventType string, at model.Time, fields json.RawMessage) {
	f.mu.Lock()
	f.events = append(f.events, host+" "+eventType+" "+string(conv))
	f.mu.Unlock()
}

func (f *fakeCalls) count() (asked, signals, events int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.asked), len(f.signals), len(f.events)
}

// Case 52, the link's half: a member of B gets a pass for a call hosted on
// A through its own server, as a sealed direct message; the host's refusal
// comes back as the refusal it was; signalling is relayed; and the call's
// events reach the other server only from the server entitled to send them.
func TestCallsOverTheLink(t *testing.T) {
	s := share(t, 3, true, nil)
	a, b := s.Servers[0], s.Servers[1]
	hostA, hostB := &fakeCalls{}, &fakeCalls{}
	a.Server.Link.SetCalls(hostA)
	b.Server.Link.SetCalls(hostB)
	call := model.NewID[model.CallID]()
	req := link.CallPassRequest{ConversationID: model.ConversationID(s.conv[1]), CallID: call, AccountID: b.Owner.ID}
	pass, err := b.Server.Link.RequestPass(ctx, "a.localhost", req)
	if err != nil || pass.Pass != "a-pass" || pass.Media.UDPPort != 24877 || pass.AccountID != b.Owner.ID {
		t.Fatalf("pass: %+v %v", pass, err)
	}
	hostA.mu.Lock()
	hostA.refuse = model.ErrForbidden.With("That account does not belong to the server asking.")
	hostA.mu.Unlock()
	if _, err := b.Server.Link.RequestPass(ctx, "a.localhost", req); model.CodeOf(err) != model.CodeForbidden {
		t.Fatalf("the host's refusal came back as %v", err)
	}
	// A server that is not in the conversation cannot be asked.
	if _, err := b.Server.Link.RequestPass(ctx, "nobody.localhost", req); model.CodeOf(err) != model.CodeNotFound {
		t.Fatalf("an unknown host: %v", err)
	}
	sig := link.CallSignal{ConversationID: req.ConversationID, CallID: call, AccountID: b.Owner.ID, Pass: "a-pass", Signal: json.RawMessage(`{"type":"call.join","id":"1"}`)}
	if err := b.Server.Link.Signal(ctx, "a.localhost", sig); err != nil {
		t.Fatal(err)
	}
	testkit.Eventually(t, "the signal reaches the host", func() bool { _, n, _ := hostA.count(); return n == 1 })
	hostA.mu.Lock()
	got := hostA.signals[0]
	hostA.mu.Unlock()
	if got.Pass != "a-pass" || got.AccountID != b.Owner.ID || string(got.ConversationID) != s.conv[0] || !strings.Contains(string(got.Signal), "call.join") {
		t.Fatalf("the signal as the host got it: %+v", got)
	}
	// The host's events travel in the stream.
	fields := map[string]any{"call_id": call, "started_by_id": s.ann.ID, "host": "a.localhost"}
	if err := a.Server.Link.SubmitCallEvent(ctx, model.ConversationID(s.conv[0]), "call.started", model.Now(), fields); err != nil {
		t.Fatal(err)
	}
	a.Server.Link.SubmitCallEvent(ctx, model.ConversationID(s.conv[0]), "call.participant_joined", model.Now(), map[string]any{"call_id": call, "account_id": s.ann.ID})
	testkit.Eventually(t, "b hears of the call", func() bool { _, _, n := hostB.count(); return n == 2 })
	hostB.mu.Lock()
	first := hostB.events[0]
	hostB.mu.Unlock()
	if first != "a.localhost call.started "+s.conv[1] {
		t.Fatalf("the event as b got it: %q", first)
	}
	// b cannot announce a call in a's name, nor one started by a's member.
	s.submit(1, "call.started", map[string]any{"call_id": model.NewID[string](), "started_by_id": b.Owner.ID, "host": "a.localhost"})
	s.rejected(0, "not_the_host", 1)
	s.submit(1, "call.started", map[string]any{"call_id": model.NewID[string](), "started_by_id": s.ann.ID, "host": "b.localhost"})
	s.rejected(0, "forged_author", 1)
	if _, _, n := hostA.count(); n != 0 {
		t.Fatalf("a was handed %d call events it should have rejected", n)
	}
	// The hub passed all of it along and could read none of it.
	if a.Server.Link.LastHeard("b.localhost").IsZero() {
		t.Error("a has not heard from b")
	}
}

// fakeSealed plays the sealed package on one server.
type fakeSealed struct {
	mu     sync.Mutex
	lists  []store.SignedDeviceList
	stored []string
}

func (f *fakeSealed) StoreRemoteList(ctx context.Context, account model.AccountID, signed []byte) error {
	f.mu.Lock()
	f.stored = append(f.stored, string(account)+":"+string(signed))
	f.mu.Unlock()
	return nil
}

func (f *fakeSealed) RemoteLists(ctx context.Context, account model.AccountID, after int64) ([]store.SignedDeviceList, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []store.SignedDeviceList
	for _, l := range f.lists {
		if l.AccountID == account && l.Version > after {
			out = append(out, l)
		}
	}
	return out, nil
}

func (f *fakeSealed) CheckRemoteEpoch(ctx context.Context, a store.Areas, conv model.ConversationID, record []byte) (model.EpochRecord, bool, error) {
	return model.EpochRecord{}, true, nil
}

func (f *fakeSealed) CheckRemoteEpochFor(ctx context.Context, a store.Areas, conv model.ConversationID, record []byte, keyEpoch int64) (model.EpochRecord, bool, error) {
	return f.CheckRemoteEpoch(ctx, a, conv, record)
}

func (f *fakeSealed) CheckRemoteStart(ctx context.Context, starter, local model.Account) error {
	return model.ErrFeatureOff
}

func (f *fakeSealed) AcceptRemote(ctx context.Context, starter, local model.Account, stream model.StreamID, record []byte, deaf bool) (model.Conversation, error) {
	return model.Conversation{}, model.ErrFeatureOff
}

func (f *fakeSealed) CheckRemoteMessage(ctx context.Context, a store.Areas, conv model.ConversationID, author model.AccountID, keyEpoch int64, quiet bool, ciphertext []byte) (bool, error) {
	return true, nil
}

// Case 57b: device list versions are carried between the two home servers,
// and a device_list_request is refused once the two people share no
// channel.
func TestDeviceListsOverTheLink(t *testing.T) {
	s := share(t, 2, false, nil)
	a, b := s.Servers[0], s.Servers[1]
	onA, onB := &fakeSealed{}, &fakeSealed{}
	onA.lists = []store.SignedDeviceList{{AccountID: s.ann.ID, Version: 1, Signed: []byte("list-v1")}, {AccountID: s.ann.ID, Version: 2, Signed: []byte("list-v2")}}
	a.Server.Link.SetSealed(onA)
	b.Server.Link.SetSealed(onB)
	if err := b.Server.Link.RequestDeviceLists(ctx, s.ann.ID, b.Owner.ID, 1); err != nil {
		t.Fatal(err)
	}
	testkit.Eventually(t, "the newer version is stored on b", func() bool {
		onB.mu.Lock()
		defer onB.mu.Unlock()
		return len(onB.stored) == 1 && onB.stored[0] == string(s.ann.ID)+":list-v2"
	})
	// A list for an account that is not the sender's is not stored.
	if _, err := b.Server.Link.TestDirect(model.PeerID(s.Peers[1]), "a.localhost", "device_list", map[string]any{"account_id": s.ann.ID,
		"versions": []any{map[string]any{"version": 9, "signed_list": wire.Bytes("forged")}}}, model.Now(), nil); err != nil {
		t.Fatal(err)
	}
	s.dropped(0, "device_list_not_from_home")
	// Someone on b who shares no channel with ann is refused, by b before
	// it asks and by a if it is asked anyway.
	stranger := b.Member("stranger")
	if err := b.Server.Link.RequestDeviceLists(ctx, s.ann.ID, stranger.ID, 0); model.CodeOf(err) != model.CodeForbidden {
		t.Fatalf("no shared channel, asked on b: %v", err)
	}
	err := b.Server.Link.TestAsk(model.PeerID(s.Peers[1]), "a.localhost", "device_list_request", map[string]any{"account_id": s.ann.ID,
		"by_account_id": b.Owner.ID, "after_version": 0})
	if err != nil {
		t.Fatalf("while they share a channel: %v", err)
	}
	// The last shared channel ends.
	b.Owner.Must(204, "DELETE", "/api/v1/admin/streams/"+string(s.stream), nil)
	if err := b.Server.Link.RequestDeviceLists(ctx, s.ann.ID, b.Owner.ID, 0); model.CodeOf(err) != model.CodeForbidden {
		t.Fatalf("after the channel ended, asked on b: %v", err)
	}
	a.Owner.Channel("another", false, s.ann)
	testkit.Eventually(t, "a has taken b out of the stream", func() bool {
		_, found, _ := a.Server.Store.Members().Get(ctx, model.ConversationID(s.conv[0]), b.Owner.ID)
		return !found
	})
	err = b.Server.Link.TestAsk(model.PeerID(s.Peers[1]), "a.localhost", "device_list_request", map[string]any{"account_id": s.ann.ID,
		"by_account_id": b.Owner.ID, "after_version": 0})
	if wire.AsError(err).Code != wire.EForbidden {
		t.Fatalf("after the channel ended, asked of a: %v", err)
	}
}

// Case 12: a roster that changes a pinned server's keys is refused and the
// owner is told. The hub here is the test: it signs what it likes.
func TestRosterThatChangesPinnedKeys(t *testing.T) {
	hub := stranger(t, "badhub.localhost")
	hub.Kind = wire.KindHub
	conns := make(chan *wire.Conn, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := wire.Accept(w, r, hub, func(wire.Frame, []byte, []byte) error { return nil })
		if err != nil {
			return
		}
		conns <- c
		for {
			if _, _, err := c.Read(r.Context()); err != nil {
				return
			}
		}
	}))
	defer srv.Close()
	inv := wire.Invitation{V: 1, Kind: wire.KindHub, Name: hub.Name, URL: "ws://badhub.localhost:" + srv.URL[strings.LastIndex(srv.URL, ":")+1:] + "/link/v1",
		FP: wire.Fingerprint(hub.Keys.SignPublic()), Secret: make([]byte, 32), ExpiresAt: model.Now().Add(time.Hour)}
	a := testkit.NewServer(t, testkit.Options{Hostname: "a.localhost"})
	peer := a.Owner.Must(201, "POST", "/api/v1/admin/peers", map[string]any{"invitation": inv.String()})["peer"].(map[string]any)
	if peer["kind"] != "hub" || peer["name"] != hub.Name {
		t.Fatalf("the hub as a peer: %v", peer)
	}
	conn := <-conns
	roster := func(version int64, victim wire.Identity) {
		f, err := wire.SignBlock(wire.FRoster, wire.Roster{Hub: hub.Name, Version: version, At: model.Now(), Servers: []wire.RosterServer{{
			Name: victim.Name, SignKey: wire.Bytes(victim.Keys.SignPublic()), BoxKey: victim.Keys.BoxPublic(), BoxKeySig: victim.BoxKeySig(), JoinedAt: model.Now()}}}, hub.Keys)
		if err != nil {
			t.Fatal(err)
		}
		conn.Send(f)
	}
	honest := stranger(t, "x.localhost")
	roster(1, honest)
	var pinned string
	testkit.Eventually(t, "x is pinned", func() bool {
		list := a.Owner.Must(200, "GET", "/api/v1/admin/servers", nil)["servers"].([]any)
		if len(list) == 1 {
			pinned = list[0].(map[string]any)["fingerprint"].(string)
		}
		return pinned != ""
	})
	// The hub now gives x its own keys, to sit in the middle.
	roster(2, stranger(t, "x.localhost"))
	testkit.Eventually(t, "the roster is refused", func() bool { return a.Server.Link.StatsNow().Dropped["roster_refused"] == 1 })
	list := a.Owner.Must(200, "GET", "/api/v1/admin/servers", nil)["servers"].([]any)
	if list[0].(map[string]any)["fingerprint"] != pinned || pinned != wire.KeyFingerprint(honest.Keys.SignPublic()) {
		t.Fatalf("the pinned keys changed: %v", list)
	}
	audit := fmt.Sprint(a.Owner.Must(200, "GET", "/api/v1/admin/audit?action=link.roster_refused", nil))
	if !strings.Contains(audit, "x.localhost") {
		t.Fatalf("the owner was not told: %v", audit)
	}
	// A roster signed by someone else is refused outright.
	other := stranger(t, "badhub.localhost")
	f, _ := wire.SignBlock(wire.FRoster, wire.Roster{Hub: hub.Name, Version: 3, At: model.Now()}, other.Keys)
	conn.Send(f)
	roster(3, honest)
	testkit.Eventually(t, "the honest roster is applied", func() bool {
		p := a.Owner.Must(200, "GET", "/api/v1/admin/peers/"+peer["id"].(string), nil)["peer"].(map[string]any)
		return len(arr(p["servers"])) == 1
	})
}
