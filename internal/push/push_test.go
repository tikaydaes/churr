package push

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/perm"
	"github.com/tikaydaes/churr/internal/secret"
	"github.com/tikaydaes/churr/internal/store"
	"github.com/tikaydaes/churr/internal/store/storetest"
)

var ctx = context.Background()

// fakeHub says which accounts have an active client.
type fakeHub struct {
	mu     sync.Mutex
	active map[model.AccountID]bool
}

func (h *fakeHub) Active(a model.AccountID) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.active[a]
}

// fakeNotify answers the one rule from a table.
type fakeNotify struct {
	wake map[model.AccountID]bool
	high bool
	b    int64
}

func (n *fakeNotify) Wake(_ context.Context, _ model.Message, to model.AccountID) (bool, bool) {
	return n.wake[to], n.high
}
func (n *fakeNotify) Totals(context.Context, model.AccountID) (model.Totals, error) {
	return model.Totals{Badge: n.b}, nil
}

// fakeRelay is a relay that records what it was sent and answers from a
// script: one status per request, the last one repeating.
type fakeRelay struct {
	mu       sync.Mutex
	requests []map[string]any
	script   []int
	body     map[int]string
	srv      *httptest.Server
}

func newFakeRelay(t *testing.T, script ...int) *fakeRelay {
	r := &fakeRelay{script: script, body: map[int]string{
		200: `{"result":"sent"}`,
		410: `{"error":{"code":"gone","message":"That address is no longer valid."}}`,
		400: `{"error":{"code":"invalid_request","message":"bad"}}`,
		502: `{"error":{"code":"unavailable","message":"upstream"}}`,
	}}
	r.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		var got map[string]any
		json.NewDecoder(req.Body).Decode(&got)
		got["_path"], got["_method"], got["_type"] = req.URL.Path, req.Method, req.Header.Get("Content-Type")
		r.mu.Lock()
		r.requests = append(r.requests, got)
		status := 200
		if len(r.script) > 0 {
			status = r.script[0]
			if len(r.script) > 1 {
				r.script = r.script[1:]
			}
		}
		body := r.body[status]
		r.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		io.WriteString(w, body)
	}))
	t.Cleanup(r.srv.Close)
	return r
}

func (r *fakeRelay) got() []map[string]any {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]map[string]any{}, r.requests...)
}

type world struct {
	t      *testing.T
	s      *Service
	store  store.Store
	hub    *fakeHub
	notify *fakeNotify
	ann    model.Account
	conv   model.Conversation
}

func newWorld(t *testing.T, relay string, d Deps) *world {
	t.Helper()
	st := storetest.OpenSQLite(t)
	keys, err := secret.Open(filepath.Join(t.TempDir(), "keys"))
	if err != nil {
		t.Fatal(err)
	}
	w := &world{t: t, store: st, hub: &fakeHub{active: map[model.AccountID]bool{}}, notify: &fakeNotify{wake: map[model.AccountID]bool{}, b: 7}}
	d.Store, d.Perm, d.Hub, d.Notify, d.Keys = st, perm.New(st), w.hub, w.notify, keys
	if d.Retry == nil {
		d.Retry = []time.Duration{10 * time.Millisecond, 10 * time.Millisecond, 10 * time.Millisecond}
	}
	w.s = New(d)
	t.Cleanup(w.s.Close)
	w.ann = model.Account{Username: "ann", DisplayName: "Ann", Role: model.RoleMember}
	if _, err := st.Accounts().Create(ctx, &w.ann); err != nil {
		t.Fatal(err)
	}
	w.conv = model.Conversation{Kind: model.KindChannel, Name: "ops", CreatedByID: w.ann.ID}
	if err := st.Conversations().Create(ctx, &w.conv); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Members().AddDirect(ctx, w.conv.ID, w.ann.ID, model.LevelMember, model.Now()); err != nil {
		t.Fatal(err)
	}
	if relay != "" {
		if err := st.Settings().Set(ctx, SettingRelayURL, relay, ""); err != nil {
			t.Fatal(err)
		}
	}
	return w
}

func (w *world) session(kind string) model.Session {
	w.t.Helper()
	ses := model.Session{AccountID: w.ann.ID, Kind: kind, Label: "Ann's phone"}
	if err := w.store.Sessions().Create(ctx, &ses, secret.RandomBytes(32), secret.RandomBytes(32)); err != nil {
		w.t.Fatal(err)
	}
	return ses
}

func ptr(s string) *string { return &s }

func (w *world) phone(kind, address string) (model.Device, model.Session) {
	w.t.Helper()
	ses := w.session(model.SessionApp)
	d, err := w.s.Register(ctx, w.ann, ses, DeviceInput{Name: ptr("Phone"), Platform: ptr("ios"), PushKind: ptr(kind), PushAddress: ptr(address)})
	if err != nil {
		w.t.Fatal(err)
	}
	return d, ses
}

// wait waits for a condition that background delivery brings about.
func wait(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func keysOf(m map[string]any) []string {
	var out []string
	for k := range m {
		if k[0] != '_' {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

var (
	eventIDForm  = regexp.MustCompile(`^[A-Za-z0-9_-]{11}\.[A-Za-z0-9_-]+$`)
	collapseForm = regexp.MustCompile(`^[A-Za-z0-9_-]{22}$`)
)

func TestWakeUpIdentifiers(t *testing.T) {
	w := newWorld(t, "http://relay.invalid", Deps{})
	d1, s1 := w.phone(model.PushAPNs, "aa")
	d2, _ := w.phone(model.PushFCM, "bb")
	if len(d1.WakeRoute) != 11 || d1.WakeRoute != w.s.Route(d1.ID) || d1.WakeRoute == d2.WakeRoute {
		t.Fatalf("routes: %q %q", d1.WakeRoute, d2.WakeRoute)
	}
	other := model.NewID[model.ConversationID]()
	k := w.s.CollapseKey(d1.ID, w.conv.ID)
	if !collapseForm.MatchString(k) || k != w.s.CollapseKey(d1.ID, w.conv.ID) || k == w.s.CollapseKey(d2.ID, w.conv.ID) || k == w.s.CollapseKey(d1.ID, other) {
		t.Fatalf("collapse keys: %q", k)
	}
	ref := Ref{DeviceID: d1.ID, Kind: KindMessage, ConversationID: w.conv.ID, Seq: 41, At: model.Now()}
	id, err := w.s.EventID(ref)
	if err != nil || !eventIDForm.MatchString(id) || len(id) > 256 || id[:11] != d1.WakeRoute {
		t.Fatalf("event id: %q %v", id, err)
	}
	// Two identifiers for the same event differ: the token has a nonce.
	if again, _ := w.s.EventID(ref); again == id {
		t.Fatal("two event identifiers for one event are the same")
	}
	got, err := w.s.Resolve(ctx, s1.ID, id)
	if err != nil || got != ref {
		t.Fatalf("resolve: %+v %v", got, err)
	}
	call := model.NewID[model.CallID]()
	ring := Ref{DeviceID: d1.ID, Kind: KindRing, ConversationID: w.conv.ID, CallID: call, At: model.Now()}
	ringID, _ := w.s.EventID(ring)
	if got, err := w.s.Resolve(ctx, s1.ID, ringID); err != nil || got != ring {
		t.Fatalf("resolve a ring: %+v %v", got, err)
	}
	// Only from the session of the device it names, only if this server
	// made it, and only for seven days.
	for name, try := range map[string]func() error{
		"another session": func() error { _, err := w.s.Resolve(ctx, w.session(model.SessionApp).ID, id); return err },
		"a changed token": func() error { _, err := w.s.Resolve(ctx, s1.ID, id[:len(id)-2]+"AA"); return err },
		"no dot":          func() error { _, err := w.s.Resolve(ctx, s1.ID, "nonsense"); return err },
		"not base64url":   func() error { _, err := w.s.Resolve(ctx, s1.ID, d1.WakeRoute+".!!!"); return err },
	} {
		if err := try(); model.CodeOf(err) != model.CodeNotFound {
			t.Errorf("%s: %v", name, err)
		}
	}
	w.s.now = func() model.Time { return model.Now().Add(8 * 24 * time.Hour) }
	if _, err := w.s.Resolve(ctx, s1.ID, id); model.CodeOf(err) != model.CodeNotFound {
		t.Errorf("an identifier older than seven days: %v", err)
	}
}

func TestRelayRequestAndDecision(t *testing.T) {
	relay := newFakeRelay(t)
	w := newWorld(t, relay.srv.URL, Deps{})
	iphone, _ := w.phone(model.PushAPNs, "a1b2c3")
	android, _ := w.phone(model.PushFCM, "fcm-token")
	w.phone(model.PushNone, "") // a desktop app: never woken
	m := model.Message{ID: model.NewID[model.MessageID](), ConversationID: w.conv.ID, Seq: 5, AuthorID: model.NewID[model.AccountID]()}

	// The rule says no: nothing is sent at all.
	w.s.MessageCreated(m)
	// A quiet sealed message and a system notice never wake a phone,
	// whatever the rule would say.
	w.notify.wake[w.ann.ID] = true
	quiet := m
	quiet.Quiet = true
	w.s.MessageCreated(quiet)
	system := m
	system.System = map[string]any{"kind": "member_joined"}
	w.s.MessageCreated(system)
	// A client that is connected and active shows it itself (ruling R6).
	w.hub.active[w.ann.ID] = true
	w.s.MessageCreated(m)
	w.s.RingStarted(Ring{CallID: model.NewID[model.CallID](), ConversationID: w.conv.ID, AccountID: w.ann.ID})
	w.s.wg.Wait()
	if got := relay.got(); len(got) != 0 {
		t.Fatalf("wake-ups were sent that the rule forbids: %v", got)
	}

	// Connected but idle: the phones are woken, one wake-up per device.
	w.hub.active[w.ann.ID] = false
	w.s.MessageCreated(m)
	wait(t, "two wake-ups", func() bool { return len(relay.got()) == 2 })
	byAddress := map[string]map[string]any{}
	for _, r := range relay.got() {
		byAddress[r["push_address"].(string)] = r
	}
	ios, droid := byAddress["a1b2c3"], byAddress["fcm-token"]
	if ios == nil || droid == nil {
		t.Fatalf("requests: %v", relay.got())
	}
	// Exactly the fields of PUSH.md section 5, and no others: no text, no
	// name, no conversation identifier.
	wantKeys := []string{"badge", "collapse_key", "event_id", "platform", "priority", "push_address"}
	if !reflect.DeepEqual(keysOf(ios), wantKeys) || !reflect.DeepEqual(keysOf(droid), wantKeys) {
		t.Fatalf("request fields: %v and %v", keysOf(ios), keysOf(droid))
	}
	if ios["_path"] != "/v1/wake" || ios["_method"] != "POST" || ios["_type"] != "application/json" {
		t.Fatalf("the request: %v", ios)
	}
	if ios["platform"] != "ios" || droid["platform"] != "android" || ios["priority"] != "normal" || ios["badge"] != float64(7) {
		t.Fatalf("request values: %v %v", ios, droid)
	}
	if !eventIDForm.MatchString(ios["event_id"].(string)) || !collapseForm.MatchString(ios["collapse_key"].(string)) {
		t.Fatalf("identifiers: %v", ios)
	}
	if ios["collapse_key"] != w.s.CollapseKey(iphone.ID, w.conv.ID) || droid["collapse_key"] != w.s.CollapseKey(android.ID, w.conv.ID) ||
		ios["collapse_key"] == droid["collapse_key"] {
		t.Fatalf("collapse keys: %v %v", ios["collapse_key"], droid["collapse_key"])
	}
	raw, _ := json.Marshal(relay.got())
	for _, secret := range []string{string(w.conv.ID), string(m.ID), string(w.ann.ID), "ops", "Ann"} {
		if regexp.MustCompile(regexp.QuoteMeta(secret)).Match(raw) {
			t.Fatalf("the relay was told %q", secret)
		}
	}

	// A ring looks the same to the relay as any other wake-up: the same
	// fields, the conversation's collapse key, high priority.
	relay.mu.Lock()
	relay.requests = nil
	relay.mu.Unlock()
	call := model.NewID[model.CallID]()
	w.s.RingStarted(Ring{CallID: call, ConversationID: w.conv.ID, AccountID: w.ann.ID})
	wait(t, "the ring's wake-ups", func() bool { return len(relay.got()) == 2 })
	for _, r := range relay.got() {
		if !reflect.DeepEqual(keysOf(r), wantKeys) || r["priority"] != "high" {
			t.Fatalf("a ring as the relay sees it: %v", r)
		}
		if r["push_address"] == "a1b2c3" && r["collapse_key"] != w.s.CollapseKey(iphone.ID, w.conv.ID) {
			t.Fatalf("a ring uses the conversation's collapse key: %v", r)
		}
	}
	// When the ring stops, the phones that were woken get one more
	// wake-up with the same collapse key and no badge.
	w.s.RingStopped(Ring{CallID: call, ConversationID: w.conv.ID, AccountID: w.ann.ID})
	wait(t, "the ring stopping", func() bool { return len(relay.got()) == 4 })
	for _, r := range relay.got()[2:] {
		if _, has := r["badge"]; has || !reflect.DeepEqual(keysOf(r), []string{"collapse_key", "event_id", "platform", "priority", "push_address"}) {
			t.Fatalf("the wake-up for a stopped ring: %v", r)
		}
	}
	w.s.RingStopped(Ring{CallID: call, ConversationID: w.conv.ID, AccountID: w.ann.ID})
	w.s.wg.Wait()
	if len(relay.got()) != 4 {
		t.Fatal("a ring was reported stopped twice")
	}

	// High priority when the rule says so; no badge when the admin has
	// switched badge numbers off.
	relay.mu.Lock()
	relay.requests = nil
	relay.mu.Unlock()
	w.notify.high = true
	if err := w.store.Settings().Set(ctx, SettingSendBadge, false, ""); err != nil {
		t.Fatal(err)
	}
	w.s.NotifiedAnyway(m, w.ann.ID)
	wait(t, "the wake-ups", func() bool { return len(relay.got()) == 2 })
	if r := relay.got()[0]; r["priority"] != "high" || r["badge"] != nil {
		t.Fatalf("priority and badge: %v", r)
	}

	// The relay switched off: no request is ever made.
	relay.mu.Lock()
	relay.requests = nil
	relay.mu.Unlock()
	if err := w.store.Settings().Set(ctx, store.SettingPushRelayEnabled, false, ""); err != nil {
		t.Fatal(err)
	}
	w.s.MessageCreated(m)
	w.s.wg.Wait()
	if len(relay.got()) != 0 {
		t.Fatal("a wake-up went to the relay while it is switched off")
	}
}

func TestRelayAnswersAndForgetting(t *testing.T) {
	m := model.Message{ID: model.NewID[model.MessageID](), Seq: 5, AuthorID: model.NewID[model.AccountID]()}
	run := func(t *testing.T, script []int, bodies map[int]string) (*world, *fakeRelay, model.Device) {
		relay := newFakeRelay(t, script...)
		for k, v := range bodies {
			relay.body[k] = v
		}
		w := newWorld(t, relay.srv.URL, Deps{})
		d, _ := w.phone(model.PushAPNs, "a1b2c3")
		w.notify.wake[w.ann.ID] = true
		msg := m
		msg.ConversationID = w.conv.ID
		w.s.MessageCreated(msg)
		w.s.wg.Wait()
		got, err := w.store.Devices().Get(ctx, d.ID)
		if err != nil {
			t.Fatal(err)
		}
		return w, relay, got
	}
	t.Run("sent clears the failure mark", func(t *testing.T) {
		_, relay, d := run(t, []int{502, 200}, nil)
		if len(relay.got()) != 2 || !d.PushFailedAt.IsZero() || d.PushAddress == "" {
			t.Fatalf("%d requests, device %+v", len(relay.got()), d)
		}
	})
	t.Run("gone forgets the address at once", func(t *testing.T) {
		w, relay, d := run(t, []int{410}, nil)
		if len(relay.got()) != 1 || d.PushAddress != "" || d.PushFailedAt.IsZero() {
			t.Fatalf("%d requests, device %+v", len(relay.got()), d)
		}
		// No more wake-ups go to that device, until the app registers
		// again.
		w.s.MessageCreated(model.Message{ID: model.NewID[model.MessageID](), ConversationID: w.conv.ID, Seq: 6})
		w.s.wg.Wait()
		if len(relay.got()) != 1 {
			t.Fatal("a wake-up went to a forgotten address")
		}
	})
	t.Run("failures are retried three times, then dropped", func(t *testing.T) {
		_, relay, d := run(t, []int{502}, nil)
		if len(relay.got()) != 4 || d.PushFailedAt.IsZero() || d.PushAddress == "" {
			t.Fatalf("%d requests, device %+v", len(relay.got()), d)
		}
	})
	t.Run("overloaded is retried", func(t *testing.T) {
		_, relay, _ := run(t, []int{503, 200}, map[int]string{503: `{"error":{"code":"unavailable","message":"busy"}}`})
		if len(relay.got()) != 2 {
			t.Fatalf("%d requests", len(relay.got()))
		}
	})
	t.Run("platform off is not retried", func(t *testing.T) {
		_, relay, _ := run(t, []int{503}, map[int]string{503: `{"error":{"code":"unavailable","message":"no key","platform_off":true}}`})
		if len(relay.got()) != 1 {
			t.Fatalf("%d requests", len(relay.got()))
		}
	})
	t.Run("malformed is not retried", func(t *testing.T) {
		_, relay, d := run(t, []int{400}, nil)
		if len(relay.got()) != 1 || d.PushAddress == "" {
			t.Fatalf("%d requests, device %+v", len(relay.got()), d)
		}
	})
	t.Run("too large is not retried", func(t *testing.T) {
		_, relay, _ := run(t, []int{413}, map[int]string{413: `{"error":{"code":"too_large","message":"big"}}`})
		if len(relay.got()) != 1 {
			t.Fatalf("%d requests", len(relay.got()))
		}
	})
	t.Run("a rate limit is tried once more after the stated time", func(t *testing.T) {
		start := time.Now()
		_, relay, _ := run(t, []int{429, 429, 200}, map[int]string{429: `{"error":{"code":"rate_limited","message":"slow down","retry_after_seconds":1,"scope":"device"}}`})
		if len(relay.got()) != 2 || time.Since(start) < time.Second {
			t.Fatalf("%d requests in %v", len(relay.got()), time.Since(start))
		}
	})
	t.Run("a long rate limit drops the wake-up", func(t *testing.T) {
		_, relay, _ := run(t, []int{429}, map[int]string{429: `{"error":{"code":"rate_limited","message":"slow down","retry_after_seconds":300,"scope":"address"}}`})
		if len(relay.got()) != 1 {
			t.Fatalf("%d requests", len(relay.got()))
		}
	})
}

func TestUnifiedPush(t *testing.T) {
	var mu sync.Mutex
	var got []*http.Request
	var bodies [][]byte
	status := 201
	endpoint := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		got, bodies = append(got, r), append(bodies, body)
		s := status
		mu.Unlock()
		w.WriteHeader(s)
	}))
	defer endpoint.Close()
	w := newWorld(t, "", Deps{EndpointHTTP: endpoint.Client(), Retry: []time.Duration{}})
	ses := w.session(model.SessionApp)
	in := DeviceInput{Name: ptr("Phone"), Platform: ptr("android"), PushKind: ptr(model.PushUnifiedPush), PushAddress: ptr(endpoint.URL + "/up/abc")}
	// Off by default, with its own switch.
	if _, err := w.s.Register(ctx, w.ann, ses, in); model.CodeOf(err) != model.CodeFeatureOff {
		t.Fatalf("UnifiedPush while switched off: %v", err)
	}
	if kinds := w.s.Kinds(ctx); !reflect.DeepEqual(kinds, []string{"none"}) {
		t.Fatalf("kinds with everything off: %v", kinds)
	}
	if err := w.store.Settings().Set(ctx, store.SettingUnifiedPushEnabled, true, ""); err != nil {
		t.Fatal(err)
	}
	plainHTTP := in
	plainHTTP.PushAddress = ptr("http://push.example.org/up/abc")
	if _, err := w.s.Register(ctx, w.ann, ses, plainHTTP); model.CodeOf(err) != model.CodeFeatureOff {
		t.Fatalf("an endpoint that is not https: %v", err)
	}
	d, err := w.s.Register(ctx, w.ann, ses, in)
	if err != nil {
		t.Fatal(err)
	}
	if kinds := w.s.Kinds(ctx); !reflect.DeepEqual(kinds, []string{"unifiedpush", "none"}) {
		t.Fatalf("kinds: %v", kinds)
	}
	w.notify.wake[w.ann.ID] = true
	m := model.Message{ID: model.NewID[model.MessageID](), ConversationID: w.conv.ID, Seq: 5}
	w.s.MessageCreated(m)
	w.s.wg.Wait()
	// The same wake-up with the address and platform left off.
	var body map[string]any
	if len(bodies) != 1 || json.Unmarshal(bodies[0], &body) != nil || got[0].URL.Path != "/up/abc" || got[0].Header.Get("Content-Type") != "application/json" {
		t.Fatalf("the request to the endpoint: %d %s", len(bodies), bodies)
	}
	if !reflect.DeepEqual(keysOf(body), []string{"badge", "collapse_key", "event_id", "priority"}) || body["collapse_key"] != w.s.CollapseKey(d.ID, w.conv.ID) {
		t.Fatalf("the body: %v", body)
	}

	// With a key registered the body is encrypted as Web Push.
	ua, _ := ecdh.P256().GenerateKey(rand.Reader)
	auth := secret.RandomBytes(16)
	in.PushPublicKey, in.PushAuth = ua.PublicKey().Bytes(), auth
	if _, err := w.s.Register(ctx, w.ann, ses, in); err != nil {
		t.Fatal(err)
	}
	w.s.MessageCreated(m)
	w.s.wg.Wait()
	if len(bodies) != 2 || got[1].Header.Get("Content-Encoding") != "aes128gcm" || got[1].Header.Get("TTL") != "259200" {
		t.Fatalf("the encrypted request: %d %v", len(bodies), got[len(got)-1].Header)
	}
	plain := webPushDecrypt(t, ua, auth, bodies[1])
	if json.Unmarshal(plain, &body) != nil || !reflect.DeepEqual(keysOf(body), []string{"badge", "collapse_key", "event_id", "priority"}) {
		t.Fatalf("the decrypted body: %s", plain)
	}

	// 404 and 410 forget the address.
	mu.Lock()
	status = 404
	mu.Unlock()
	w.s.MessageCreated(m)
	w.s.wg.Wait()
	if after, _ := w.store.Devices().Get(ctx, d.ID); after.PushAddress != "" {
		t.Fatalf("a 404 from the endpoint did not forget the address: %+v", after)
	}
}

// The guarded dialer: an endpoint on a private address is never connected
// to, unless the operator allowed private addresses.
func TestUnifiedPushRefusesPrivateAddresses(t *testing.T) {
	hits := 0
	endpoint := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits++ }))
	defer endpoint.Close()
	w := newWorld(t, "", Deps{Retry: []time.Duration{}})
	if err := w.store.Settings().Set(ctx, store.SettingUnifiedPushEnabled, true, ""); err != nil {
		t.Fatal(err)
	}
	d, _ := w.phone(model.PushUnifiedPush, endpoint.URL+"/up")
	w.notify.wake[w.ann.ID] = true
	w.s.MessageCreated(model.Message{ID: model.NewID[model.MessageID](), ConversationID: w.conv.ID, Seq: 5})
	w.s.wg.Wait()
	after, _ := w.store.Devices().Get(ctx, d.ID)
	if hits != 0 || after.PushFailedAt.IsZero() {
		t.Fatalf("a loopback endpoint was reached %d times; device %+v", hits, after)
	}
}

// webPushDecrypt is the receiving side of RFC 8291, written apart from the
// sending side.
func webPushDecrypt(t *testing.T, ua *ecdh.PrivateKey, auth, body []byte) []byte {
	t.Helper()
	salt, rs, idLen := body[:16], binary.BigEndian.Uint32(body[16:20]), int(body[20])
	asPublic := body[21 : 21+idLen]
	as, err := ecdh.P256().NewPublicKey(asPublic)
	if err != nil || rs != 4096 {
		t.Fatalf("header: %v, record size %d", err, rs)
	}
	shared, err := ua.ECDH(as)
	if err != nil {
		t.Fatal(err)
	}
	key, nonce, err := webPushKeys(shared, auth, ua.PublicKey().Bytes(), asPublic, salt)
	if err != nil {
		t.Fatal(err)
	}
	block, _ := aes.NewCipher(key)
	gcm, _ := cipher.NewGCM(block)
	plain, err := gcm.Open(nil, nonce, body[21+idLen:], nil)
	if err != nil || plain[len(plain)-1] != 2 {
		t.Fatalf("the record does not open: %v", err)
	}
	return plain[:len(plain)-1]
}

func TestRegisterAndReattach(t *testing.T) {
	w := newWorld(t, "", Deps{})
	ses := w.session(model.SessionWeb)
	// With no relay set, apns and fcm cannot be served.
	if _, err := w.s.Register(ctx, w.ann, ses, DeviceInput{Platform: ptr("ios"), PushKind: ptr(model.PushAPNs), PushAddress: ptr("aa")}); model.CodeOf(err) != model.CodeFeatureOff {
		t.Fatalf("apns with no relay: %v", err)
	}
	if err := w.store.Settings().Set(ctx, SettingRelayURL, "https://relay.example.org", ""); err != nil {
		t.Fatal(err)
	}
	if kinds := w.s.Kinds(ctx); !reflect.DeepEqual(kinds, []string{"apns", "fcm", "none"}) {
		t.Fatalf("kinds: %v", kinds)
	}
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	box := secret.RandomBytes(32)
	for name, in := range map[string]DeviceInput{
		"an unknown platform":     {Platform: ptr("amiga")},
		"an empty name":           {Name: ptr("")},
		"an unknown push kind":    {PushKind: ptr("pigeon"), PushAddress: ptr("x")},
		"no address":              {PushKind: ptr(model.PushFCM)},
		"an address too long":     {PushKind: ptr(model.PushFCM), PushAddress: ptr(string(make([]byte, 4097)))},
		"a short sign key":        {SignKey: []byte("short")},
		"a short box key":         {BoxKey: []byte("short")},
		"a push key without auth": {PushKind: ptr(model.PushNone), PushPublicKey: make([]byte, 65)},
	} {
		if _, err := w.s.Register(ctx, w.ann, ses, in); model.CodeOf(err) != model.CodeInvalidValue {
			t.Errorf("%s: %v", name, err)
		}
	}
	d, err := w.s.Register(ctx, w.ann, ses, DeviceInput{Name: ptr("Ann's browser"), Platform: ptr("web"), PushKind: ptr(model.PushNone), SignKey: pub, BoxKey: box})
	if err != nil || !d.Current || d.SessionID != ses.ID || d.Platform != "web" || len(d.WakeRoute) != 11 {
		t.Fatalf("registered: %+v %v", d, err)
	}
	// Idempotent, and one device per session.
	again, err := w.s.Register(ctx, w.ann, ses, DeviceInput{Name: ptr("Renamed")})
	if err != nil || again.ID != d.ID || again.Name != "Renamed" || string(again.SignKey) != string(pub) {
		t.Fatalf("registered again: %+v %v", again, err)
	}
	// The keys are set once; the platform stays once there are keys.
	otherKey, _, _ := ed25519.GenerateKey(rand.Reader)
	if _, err := w.s.Register(ctx, w.ann, ses, DeviceInput{SignKey: otherKey}); model.CodeOf(err) != model.CodeInvalidValue {
		t.Fatalf("a second sign key: %v", err)
	}
	if _, err := w.s.Register(ctx, w.ann, ses, DeviceInput{Platform: ptr("linux")}); model.CodeOf(err) != model.CodeInvalidValue {
		t.Fatalf("a changed platform: %v", err)
	}
	// A bot or a script has no device.
	bot := model.Account{Username: "bot", DisplayName: "Bot", Role: model.RoleBot}
	if _, err := w.store.Accounts().Create(ctx, &bot); err != nil {
		t.Fatal(err)
	}
	if _, err := w.s.Register(ctx, bot, model.Session{Kind: model.SessionBot}, DeviceInput{}); model.CodeOf(err) != model.CodeForbidden {
		t.Fatalf("a bot registering a device: %v", err)
	}
	if _, err := w.s.Register(ctx, w.ann, model.Session{ID: ses.ID, Kind: model.SessionPersonal}, DeviceInput{}); model.CodeOf(err) != model.CodeForbidden {
		t.Fatalf("a personal token registering a device: %v", err)
	}

	// Reattaching: the session ends by itself; the same app signs in
	// again and proves the signing key.
	next := w.session(model.SessionWeb)
	if _, err := w.s.BeginReattach(ctx, w.ann, next, d.ID); model.CodeOf(err) != model.CodeForbidden {
		t.Fatalf("reattaching a device whose session is live: %v", err)
	}
	if err := w.store.Sessions().Touch(ctx, ses.ID, model.Now().Add(-2*time.Hour), model.Now().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	challenge, err := w.s.BeginReattach(ctx, w.ann, next, d.ID)
	if err != nil || len(challenge) != 32 {
		t.Fatalf("begin: %v", err)
	}
	sign := func(c []byte) []byte { return ed25519.Sign(priv, append([]byte("churr device attach v1\x00"), c...)) }
	// A signature without the label, or by another key, is refused, and
	// the challenge is spent.
	if _, err := w.s.FinishReattach(ctx, w.ann, next, d.ID, ed25519.Sign(priv, challenge)); model.CodeOf(err) != model.CodeForbidden {
		t.Fatalf("a signature without the label: %v", err)
	}
	if _, err := w.s.FinishReattach(ctx, w.ann, next, d.ID, sign(challenge)); model.CodeOf(err) != model.CodeForbidden {
		t.Fatalf("a spent challenge: %v", err)
	}
	challenge, _ = w.s.BeginReattach(ctx, w.ann, next, d.ID)
	other := model.Account{Username: "bob", DisplayName: "Bob", Role: model.RoleMember}
	if _, err := w.store.Accounts().Create(ctx, &other); err != nil {
		t.Fatal(err)
	}
	if _, err := w.s.BeginReattach(ctx, other, model.Session{ID: model.NewID[model.SessionID](), Kind: model.SessionWeb}, d.ID); model.CodeOf(err) != model.CodeNotFound {
		t.Fatalf("another account's device: %v", err)
	}
	got, err := w.s.FinishReattach(ctx, w.ann, next, d.ID, sign(challenge))
	if err != nil || got.ID != d.ID || got.SessionID != next.ID || !got.Current {
		t.Fatalf("finish: %+v %v", got, err)
	}
	// An app's new session comes with a keyless row, which gives way.
	if err := w.store.Sessions().Revoke(ctx, next.ID, model.Now()); err != nil {
		t.Fatal(err)
	}
	// Revoking retires the device: it cannot be attached again.
	if _, _, err := w.store.Devices().RetireForSession(ctx, next.ID, model.Now()); err != nil {
		t.Fatal(err)
	}
	app := w.session(model.SessionApp)
	if _, err := w.s.BeginReattach(ctx, w.ann, app, d.ID); model.CodeOf(err) != model.CodeForbidden {
		t.Fatalf("reattaching a retired device: %v", err)
	}
	list, err := w.s.Devices(ctx, w.ann, app)
	if err != nil || len(list) != 1 || list[0].RetiredAt.IsZero() || list[0].Current {
		t.Fatalf("the list: %+v %v", list, err)
	}
}
