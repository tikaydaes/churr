package api_test

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/sealed/ref"
	"github.com/tikaydaes/churr/internal/testkit"
)

// Devices, phone wake-ups through a relay, the wake-up fetch, and the
// hand-over of the backup key to an app (docs/PUSH.md; docs/SEALED.md
// sections 4.1 and 13.3).

// testRelay stands in for the relay: it records every request and accepts
// it.
type testRelay struct {
	mu   sync.Mutex
	got  []map[string]any
	raw  []string
	srv  *httptest.Server
	stop func()
}

func newTestRelay(t *testing.T, ts *testkit.TestServer) *testRelay {
	r := &testRelay{}
	r.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		body, _ := io.ReadAll(req.Body)
		var m map[string]any
		json.Unmarshal(body, &m)
		r.mu.Lock()
		r.got, r.raw = append(r.got, m), append(r.raw, string(body))
		r.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"result":"sent"}`)
	}))
	t.Cleanup(r.srv.Close)
	ts.Set("push.relay_url", r.srv.URL)
	return r
}

// take waits for n wake-ups and returns them, emptying the record.
func (r *testRelay) take(t *testing.T, n int) []map[string]any {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		r.mu.Lock()
		if len(r.got) >= n {
			out := r.got
			r.got = nil
			r.mu.Unlock()
			if len(out) != n {
				t.Fatalf("%d wake-ups, want %d: %v", len(out), n, out)
			}
			return out
		}
		r.mu.Unlock()
		if time.Now().After(deadline) {
			t.Fatalf("no wake-up arrived at the relay (want %d)", n)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// none fails if a wake-up arrives within a short while.
func (r *testRelay) none(t *testing.T, why string) {
	t.Helper()
	time.Sleep(150 * time.Millisecond)
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.got) != 0 {
		t.Fatalf("%s, yet a wake-up was sent: %v", why, r.got)
	}
}

func TestDevicesAndWakeUps(t *testing.T) {
	ts := testkit.NewServer(t)
	ann, bob := ts.Member("ann"), ts.Member("bob")
	current := "/api/v1/me/devices/current"

	// With no relay set and UnifiedPush off, only `none` can be served.
	if kinds := ts.Owner.Must(200, "GET", "/api/v1/server", nil)["server"].(map[string]any)["push_kinds"]; len(kinds.([]any)) != 1 || kinds.([]any)[0] != "none" {
		t.Fatalf("push_kinds with nothing switched on: %v", kinds)
	}
	bob.Refused(403, model.CodeFeatureOff, "PUT", current, map[string]any{"platform": "ios", "push_kind": "apns", "push_address": "a1b2"})
	bob.Refused(403, model.CodeFeatureOff, "PUT", current, map[string]any{"platform": "android", "push_kind": "unifiedpush", "push_address": "https://push.example.org/x"})
	relay := newTestRelay(t, ts)
	if kinds := ts.Owner.Must(200, "GET", "/api/v1/server", nil)["server"].(map[string]any)["push_kinds"].([]any); len(kinds) != 3 || kinds[0] != "apns" || kinds[1] != "fcm" {
		t.Fatalf("push_kinds with a relay: %v", kinds)
	}
	bob.Refused(400, model.CodeInvalidValue, "PUT", current, map[string]any{"platform": "amiga"})
	bob.Refused(400, model.CodeInvalidRequest, "PUT", current, map[string]any{"platform": "ios", "push_token": "x"})
	bob.Token().Refused(403, model.CodeForbidden, "PUT", current, map[string]any{"platform": "ios"})
	dev := bob.Must(200, "PUT", current, map[string]any{"name": "Bob's iPhone", "platform": "ios", "push_kind": "apns", "push_address": "a1b2c3d4"})["device"].(map[string]any)
	route := dev["wake_route"].(string)
	if len(route) != 11 || dev["current"] != true || dev["push_kind"] != "apns" || dev["push_address"] != "a1b2c3d4" || dev["session_id"] == nil {
		t.Fatalf("the registered device: %v", dev)
	}
	// Registering is idempotent: the same device, the same route.
	again := bob.Must(200, "PUT", current, map[string]any{"push_address": "a1b2c3d4"})["device"].(map[string]any)
	if again["id"] != dev["id"] || again["wake_route"] != route || again["name"] != "Bob's iPhone" {
		t.Fatalf("registered again: %v", again)
	}
	list := bob.Must(200, "GET", "/api/v1/me/devices", nil)["devices"].([]any)
	if len(list) != 1 || list[0].(map[string]any)["id"] != dev["id"] {
		t.Fatalf("the list of devices: %v", list)
	}
	ann.Refused(404, model.CodeNotFound, "DELETE", "/api/v1/me/devices/"+dev["id"].(string), nil)

	// A mention wakes the phone: one wake-up, of high priority, that
	// says nothing about what happened.
	ch := ann.Channel("ops", false, bob)
	ann.Post(ch, "chatter that does not mention anyone")
	relay.none(t, "the channel's mode is Mentions")
	first := ann.Post(ch, "@bob the server room is flooding")
	wake := relay.take(t, 1)[0]
	keys := []string{}
	for k := range wake {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if strings.Join(keys, ",") != "badge,collapse_key,event_id,platform,priority,push_address" {
		t.Fatalf("the relay request carries %v", keys)
	}
	if wake["platform"] != "ios" || wake["push_address"] != "a1b2c3d4" || wake["priority"] != "high" || wake["badge"] != float64(1) ||
		!strings.HasPrefix(wake["event_id"].(string), route+".") || len(wake["collapse_key"].(string)) != 22 {
		t.Fatalf("the wake-up: %v", wake)
	}
	for _, leak := range []string{ch, "flooding", "ann", "ops", string(ann.ID), string(bob.ID), first["id"].(string)} {
		if strings.Contains(relay.raw[len(relay.raw)-1], leak) {
			t.Fatalf("the relay was told %q: %s", leak, relay.raw[len(relay.raw)-1])
		}
	}

	// The wake-up fetch: what the phone needs to draw one notification.
	eventID := wake["event_id"].(string)
	fetched := bob.Must(200, "GET", "/api/v1/wake-ups/"+eventID, nil)["wake_up"].(map[string]any)
	msgs := fetched["messages"].([]any)
	if fetched["kind"] != "message" || fetched["read"] != false || fetched["more"] != float64(0) || fetched["badge"] != float64(1) || len(msgs) != 1 ||
		fetched["conversation"].(map[string]any)["id"] != ch || fetched["conversation"].(map[string]any)["name"] != "ops" {
		t.Fatalf("the fetched wake-up: %v", fetched)
	}
	if m := msgs[0].(map[string]any); m["author_name"] != "ann" || m["message"].(map[string]any)["text"] != "@bob the server room is flooding" {
		t.Fatalf("the message in the wake-up: %v", m)
	}
	// It does not move the read position.
	if c := bob.Must(200, "GET", "/api/v1/conversations/"+ch, nil)["conversation"].(map[string]any)["membership"].(map[string]any); c["read_seq"] != float64(0) && c["unread_count"] == float64(0) {
		t.Fatalf("the fetch moved the read position: %v", c)
	}
	// Only the session of the device it was sent to may fetch it.
	ann.Refused(404, model.CodeNotFound, "GET", "/api/v1/wake-ups/"+eventID, nil)
	bob.NewSession().Refused(404, model.CodeNotFound, "GET", "/api/v1/wake-ups/"+eventID, nil)
	bob.Refused(404, model.CodeNotFound, "GET", "/api/v1/wake-ups/"+eventID[:len(eventID)-3]+"AAA", nil)
	bob.Refused(404, model.CodeNotFound, "GET", "/api/v1/wake-ups/nonsense", nil)
	ts.Browser().Refused(401, model.CodeUnauthenticated, "GET", "/api/v1/wake-ups/"+eventID, nil)

	// Two wake-ups for one conversation on one device share a collapse
	// key; another conversation has another.
	ann.Post(ch, "@bob and the lights are out")
	second := relay.take(t, 1)[0]
	other := ann.Channel("lab", true, bob)
	ann.Post(other, "@bob come here")
	third := relay.take(t, 1)[0]
	if second["collapse_key"] != wake["collapse_key"] || third["collapse_key"] == wake["collapse_key"] || second["event_id"] == wake["event_id"] {
		t.Fatalf("collapse keys: %v %v %v", wake["collapse_key"], second["collapse_key"], third["collapse_key"])
	}
	if got := bob.Must(200, "GET", "/api/v1/wake-ups/"+second["event_id"].(string), nil)["wake_up"].(map[string]any); len(got["messages"].([]any)) != 2 {
		t.Fatalf("both unread mentions are in the fetch: %v", got)
	}
	// Once read, the fetch says so, and Android shows nothing.
	bob.Must(200, "PUT", "/api/v1/conversations/"+ch+"/read", map[string]any{"seq": 100})
	if got := bob.Must(200, "GET", "/api/v1/wake-ups/"+eventID, nil)["wake_up"].(map[string]any); got["read"] != true || len(got["messages"].([]any)) != 0 {
		t.Fatalf("a wake-up for something already read: %v", got)
	}

	// A client that is connected and active shows the notification
	// itself: no phone is woken (ruling R6). A connection counts as
	// active from the moment it opens and for five minutes after each
	// report of activity; that a connected but idle client does not hold
	// the phone back is the hub's Active, tested in the live and push
	// packages, where the clock can be moved.
	stream, _ := bob.Events(nil)
	ann.Post(ch, "@bob active now")
	relay.none(t, "an active client is connected")
	stream.Close()
	time.Sleep(100 * time.Millisecond)
	ann.Post(ch, "@bob gone again")
	relay.take(t, 1)

	// Do not disturb stops it; notify anyway is the one way through, and
	// the wake-up it causes looks like any other.
	bob.Must(200, "PUT", "/api/v1/me/dnd", nil)
	quiet := ann.Post(ch, "@bob while you rest")
	relay.none(t, "Bob is in do not disturb")
	ann.Must(200, "POST", "/api/v1/messages/"+quiet["id"].(string)+"/notify-anyway", map[string]any{"account_id": bob.ID})
	through := relay.take(t, 1)[0]
	if through["collapse_key"] != wake["collapse_key"] || through["priority"] != "high" {
		t.Fatalf("the wake-up of a notify-anyway request: %v", through)
	}
	bob.Must(200, "DELETE", "/api/v1/me/dnd", nil)

	// Someone who can no longer read the conversation is told nothing.
	ann.Must(204, "DELETE", "/api/v1/conversations/"+other+"/members/"+string(bob.ID), nil)
	bob.Refused(404, model.CodeNotFound, "GET", "/api/v1/wake-ups/"+third["event_id"].(string), nil)

	// The relay switched off: no request is made, and apns is refused.
	ts.Set("push.relay_enabled", false)
	ann.Post(ch, "@bob with the relay off")
	relay.none(t, "the relay is switched off")
	bob.Refused(403, model.CodeFeatureOff, "PUT", current, map[string]any{"push_kind": "fcm", "push_address": "x"})
	ts.Set("push.relay_enabled", true)

	// Removing the device ends its session.
	bob.Must(204, "DELETE", "/api/v1/me/devices/"+dev["id"].(string), nil)
	bob.Refused(401, model.CodeUnauthenticated, "GET", "/api/v1/me/devices", nil)
}

// A sealed chat wakes phones like any conversation, but the fetch carries
// ciphertext, and a quiet message wakes nobody.
func TestSealedWakeUps(t *testing.T) {
	w := newSealedWorld(t)
	relay := newTestRelay(t, w.ts)
	a1 := w.annPhone.Open(w.bob.ID)
	b1 := w.bobPhone.Chat(a1.ID, w.ann.ID)
	b1.Sync()
	w.bob.Must(200, "PUT", "/api/v1/me/devices/current", map[string]any{"push_kind": "apns", "push_address": "bobs-token"})
	_, reference := a1.Send(ref.MessageContent("a secret the relay never sees", nil, nil, nil, nil))
	wake := relay.take(t, 1)[0]
	// A sealed chat is high priority. The relay learns nothing of it.
	if wake["priority"] != "high" || strings.Contains(relay.raw[0], a1.ID) || strings.Contains(relay.raw[0], "secret") {
		t.Fatalf("the wake-up for a sealed message: %s", relay.raw[0])
	}
	fetched := w.bob.Must(200, "GET", "/api/v1/wake-ups/"+wake["event_id"].(string), nil)["wake_up"].(map[string]any)
	conv := fetched["conversation"].(map[string]any)
	m := fetched["messages"].([]any)[0].(map[string]any)["message"].(map[string]any)
	if conv["kind"] != "sealed" || conv["title"] != "ann" || m["text"] != nil || m["client_id"] != reference.String() || m["sealed"] == nil {
		t.Fatalf("the fetched wake-up of a sealed chat: %v", fetched)
	}
	// The phone decrypts what it fetched with the keys the app shares.
	ct, _ := model.DecodeBytes(m["sealed"].(map[string]any)["ciphertext"].(string))
	got, err := b1.C.Receive(ref.Row{Seq: int64(m["seq"].(float64)), Author: ref.MustUUID(string(w.ann.ID)), KeyEpoch: 1, Ciphertext: ct})
	if err != nil || got.Content["text"] != "a secret the relay never sees" {
		t.Fatalf("decrypting the fetched message: %v", err)
	}
	// A reaction is quiet: it wakes nobody.
	a1.Send(ref.ReactionContent(reference, "👍", true))
	relay.none(t, "a quiet sealed message")
	noPlaintext(t, w.ts, "a secret the relay never sees")
}

// The same app signing in again after its session ended by itself carries
// on as the same device, by proving its signing key.
func TestReattachDevice(t *testing.T) {
	ts := testkit.NewServer(t)
	ann := ts.Member("ann")
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	box := make([]byte, 32)
	rand.Read(box)
	dev := ann.Must(200, "PUT", "/api/v1/me/devices/current", map[string]any{"name": "Browser", "platform": "web",
		"sign_key": model.EncodeBytes(pub), "box_key": model.EncodeBytes(box)})["device"].(map[string]any)
	id := dev["id"].(string)
	if dev["sign_key"] != model.EncodeBytes(pub) || dev["box_key"] != model.EncodeBytes(box) {
		t.Fatalf("the device's keys: %v", dev)
	}
	begin, finish := "/api/v1/me/devices/reattach/begin", "/api/v1/me/devices/reattach/finish"
	next := ann.NewSession()
	// While the old session lives the device is not up for grabs.
	next.Refused(403, model.CodeForbidden, "POST", begin, map[string]any{"device_id": id})
	next.Refused(404, model.CodeNotFound, "POST", begin, map[string]any{"device_id": string(model.NewID[model.DeviceID]())})
	next.Refused(400, model.CodeInvalidValue, "POST", begin, map[string]any{"device_id": "nope"})
	// The session ends by itself: it expires.
	ts.Exec(`UPDATE sessions SET expires_at = ? WHERE id = ?`, int64(model.Now().Add(-time.Hour)), string(ann.SessionID()))
	other := ts.Member("bob")
	other.Refused(404, model.CodeNotFound, "POST", begin, map[string]any{"device_id": id})
	challenge, _ := model.DecodeBytes(next.Must(200, "POST", begin, map[string]any{"device_id": id})["challenge"].(string))
	sig := ed25519.Sign(priv, append([]byte("churr device attach v1\x00"), challenge...))
	_, wrongKey, _ := ed25519.GenerateKey(rand.Reader)
	next.Refused(403, model.CodeForbidden, "POST", finish, map[string]any{"device_id": id, "signature": model.EncodeBytes(ed25519.Sign(wrongKey, append([]byte("churr device attach v1\x00"), challenge...)))})
	// A challenge is answered once.
	next.Refused(403, model.CodeForbidden, "POST", finish, map[string]any{"device_id": id, "signature": model.EncodeBytes(sig)})
	challenge, _ = model.DecodeBytes(next.Must(200, "POST", begin, map[string]any{"device_id": id})["challenge"].(string))
	sig = ed25519.Sign(priv, append([]byte("churr device attach v1\x00"), challenge...))
	got := next.Must(200, "POST", finish, map[string]any{"device_id": id, "signature": model.EncodeBytes(sig)})["device"].(map[string]any)
	if got["id"] != id || got["current"] != true || got["session_id"] != string(next.SessionID()) {
		t.Fatalf("the reattached device: %v", got)
	}
	// One device per session: the list holds the one device still.
	if list := next.Must(200, "GET", "/api/v1/me/devices", nil)["devices"].([]any); len(list) != 1 {
		t.Fatalf("devices after reattaching: %v", list)
	}
}

// The hand-over: the page that approves an app after a passkey sign-in
// seals the backup key to the app's box key; the server stores it with the
// one-time code, returns it with the tokens, and cannot open it.
func TestSealedHandover(t *testing.T) {
	ts := testkit.NewServer(t)
	ann := ts.Member("ann")
	web := ann.Sealed("web") // the unlocked page
	web.CreateBackup()
	annID := ref.MustUUID(string(ann.ID))

	// The app makes its keys before it signs in and sends its box key.
	appDev := ref.NewDevice(annID, ts.Server.Config.Hostname, ref.PlatformAndroid, rand.Reader, func() uint64 { return uint64(time.Now().UnixMilli()) })
	verifier, challenge := pkce("a-verifier-of-at-least-forty-three-characters-long")
	q := authorizeQuery(challenge, func(q url.Values) { q.Set("box_key", model.EncodeBytes(appDev.BoxPublic())) })
	handover, err := web.D.HandoverFor(appDev.BoxPublic(), challenge)
	if err != nil {
		t.Fatal(err)
	}
	// A hand-over of the wrong size, or with no box key to seal to, is
	// refused before any code is issued.
	form := url.Values{"decision": {"approve"}, "handover": {model.EncodeBytes(handover[:40])}}
	for k, v := range q {
		form[k] = v
	}
	if p := ann.Fetch("POST", "/oauth/authorize", form); p.Status != 400 || p.Location() != "" {
		t.Fatalf("a short hand-over: %d %s", p.Status, p.Location())
	}
	noBox := url.Values{"decision": {"approve"}, "handover": {model.EncodeBytes(handover)}}
	for k, v := range authorizeQuery(challenge, nil) {
		noBox[k] = v
	}
	if p := ann.Fetch("POST", "/oauth/authorize", noBox); p.Status != 400 {
		t.Fatalf("a hand-over with no box key: %d", p.Status)
	}
	q.Set("handover", model.EncodeBytes(handover))
	code := approve(t, ann, q)
	tok := token(ts.Browser(), url.Values{"grant_type": {"authorization_code"}, "code": {code}, "code_verifier": {verifier}, "redirect_uri": {appRedirect}}).JSON()
	if tok["handover"] != model.EncodeBytes(handover) || tok["device_id"] == nil {
		t.Fatalf("the token answer: %v", tok)
	}
	// The app opens it, checks the backup key against the account record
	// and the device list, registers its keys and joins.
	app := ts.App(tok["access_token"].(string))
	app.ID, app.Username = ann.ID, "ann"
	out := app.Must(200, "PUT", "/api/v1/me/devices/current", map[string]any{"sign_key": model.EncodeBytes(appDev.SignPublic()), "box_key": model.EncodeBytes(appDev.BoxPublic())})
	if out["device"].(map[string]any)["id"] != tok["device_id"] {
		t.Fatalf("the app's device: %v", out)
	}
	appDev.ID = ref.MustUUID(tok["device_id"].(string))
	lists := app.Must(200, "GET", "/api/v1/accounts/"+string(ann.ID)+"/device-lists", nil)["versions"].([]any)[0].(map[string]any)
	listBytes, _ := model.DecodeBytes(lists["list"].(string))
	sigBytes, _ := model.DecodeBytes(lists["sig"].(string))
	if err := appDev.LearnLists(annID, []ref.SignedList{{List: listBytes, Sig: sigBytes}}); err != nil {
		t.Fatal(err)
	}
	b := app.Must(200, "GET", "/api/v1/me/backup", nil)["backup"].(map[string]any)
	rec := b["account_record"].(map[string]any)
	recBytes, _ := model.DecodeBytes(rec["record"].(string))
	stored := ref.StoredBackup{KeyVersion: 1, Revision: uint64(rec["revision"].(float64)), AccountRecord: recBytes}
	// The hand-over is bound to the code challenge of this request.
	if err := appDev.UnlockWithHandover(handover, "another-challenge", stored); err == nil {
		t.Fatal("a hand-over opened under another code challenge")
	}
	if err := appDev.UnlockWithHandover(handover, challenge, stored); err != nil || !appDev.Unlocked() {
		t.Fatalf("the app could not unlock with the hand-over: %v", err)
	}
	if string(appDev.BackupKey) != string(web.D.BackupKey) {
		t.Fatal("the app unlocked to another backup key")
	}
	// The server held 80 bytes it could not open. Nothing of the key
	// itself is in the database: not as text in either encoding and, on
	// SQLite where the file can be read, not as raw bytes.
	noPlaintext(t, ts, model.EncodeBytes(web.D.BackupKey), hex.EncodeToString(web.D.BackupKey))
	if !ts.Server.Config.Postgres() {
		matches, _ := filepath.Glob(ts.Server.Config.DBPath() + "*")
		for _, path := range matches {
			if raw, err := os.ReadFile(path); err == nil && bytes.Contains(raw, web.D.BackupKey) {
				t.Errorf("%s holds the backup key", filepath.Base(path))
			}
		}
	}

	// QR pairing: the two-layer hand-over, locked under the secret from
	// the QR code, which the server never sees.
	pairDev := ref.NewDevice(annID, ts.Server.Config.Hostname, ref.PlatformIOS, rand.Reader, func() uint64 { return 0 })
	phone := ts.Browser()
	pair := func(form url.Values) testkit.Page {
		form.Set("grant_type", "urn:churr:params:oauth:grant-type:pairing-code")
		return token(phone, form)
	}
	ann.Check()
	made := ann.Must(201, "POST", "/oauth/pair", nil)
	pairing := made["pairing_code"].(string)
	pair(url.Values{"pairing_code": {pairing}, "device_name": {"Ann's iPhone"}, "platform": {"ios"}, "box_key": {model.EncodeBytes(pairDev.BoxPublic())}})
	secret := make([]byte, 32)
	rand.Read(secret)
	twoLayer, err := web.D.PairingHandoverFor(pairDev.BoxPublic(), secret)
	if err != nil || len(twoLayer) != 112 {
		t.Fatalf("the pairing hand-over: %d bytes, %v", len(twoLayer), err)
	}
	path := "/oauth/pair/" + pairing + "/confirm"
	ann.Refused(400, model.CodeInvalidValue, "POST", path, map[string]any{"handover": model.EncodeBytes(twoLayer[:80])})
	ann.Must(204, "POST", path, map[string]any{"handover": model.EncodeBytes(twoLayer)})
	time.Sleep(2100 * time.Millisecond)
	paired := pair(url.Values{"pairing_code": {pairing}}).JSON()
	if paired["handover"] != model.EncodeBytes(twoLayer) {
		t.Fatalf("the paired app's tokens: %v", paired)
	}
	if err := pairDev.LearnLists(annID, []ref.SignedList{{List: listBytes, Sig: sigBytes}}); err != nil {
		t.Fatal(err)
	}
	if err := pairDev.UnlockWithPairingHandover(twoLayer, make([]byte, 32), stored); err == nil {
		t.Fatal("the pairing hand-over opened without the secret from the QR code")
	}
	if err := pairDev.UnlockWithPairingHandover(twoLayer, secret, stored); err != nil || string(pairDev.BackupKey) != string(web.D.BackupKey) {
		t.Fatalf("the paired app could not unlock: %v", err)
	}
}
