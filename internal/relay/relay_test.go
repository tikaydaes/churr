package relay

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/config"
)

// fakeApple is Apple's push service in this process: HTTPS with HTTP/2,
// recording each request and answering from a table by device token.
type fakeApple struct {
	mu       sync.Mutex
	requests []*http.Request
	bodies   []string
	answers  map[string]struct {
		status int
		reason string
	}
	srv *httptest.Server
}

func newFakeApple(t *testing.T) *fakeApple {
	a := &fakeApple{answers: map[string]struct {
		status int
		reason string
	}{}}
	a.srv = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		a.mu.Lock()
		a.requests, a.bodies = append(a.requests, r), append(a.bodies, string(body))
		ans, ok := a.answers[strings.TrimPrefix(r.URL.Path, "/3/device/")]
		a.mu.Unlock()
		if !ok {
			w.WriteHeader(200)
			return
		}
		w.WriteHeader(ans.status)
		io.WriteString(w, `{"reason":"`+ans.reason+`"}`)
	}))
	a.srv.EnableHTTP2 = true
	a.srv.StartTLS()
	t.Cleanup(a.srv.Close)
	return a
}

func (a *fakeApple) answer(token string, status int, reason string) {
	a.mu.Lock()
	a.answers[token] = struct {
		status int
		reason string
	}{status, reason}
	a.mu.Unlock()
}

// fakeGoogle is Google's token address and FCM in this process.
type fakeGoogle struct {
	mu       sync.Mutex
	tokens   int
	asserted []string
	sends    []*http.Request
	bodies   []string
	answers  map[string]string // device token -> the error to answer with
	srv      *httptest.Server
}

func newFakeGoogle(t *testing.T) *fakeGoogle {
	g := &fakeGoogle{answers: map[string]string{}}
	g.srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		g.mu.Lock()
		defer g.mu.Unlock()
		if r.URL.Path == "/token" {
			g.tokens++
			g.asserted = append(g.asserted, string(body))
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"access_token":"ya29.access-`+strconv.Itoa(g.tokens)+`","expires_in":3600,"token_type":"Bearer"}`)
			return
		}
		g.sends, g.bodies = append(g.sends, r), append(g.bodies, string(body))
		var msg struct {
			Message struct {
				Token string `json:"token"`
			} `json:"message"`
		}
		json.Unmarshal(body, &msg)
		w.Header().Set("Content-Type", "application/json")
		switch g.answers[msg.Message.Token] {
		case "":
			io.WriteString(w, `{"name":"projects/churr-test/messages/1"}`)
		case "UNREGISTERED":
			w.WriteHeader(404)
			io.WriteString(w, `{"error":{"code":404,"message":"Requested entity was not found.","status":"NOT_FOUND","details":[{"@type":"type.googleapis.com/google.firebase.fcm.v1.FcmError","errorCode":"UNREGISTERED"}]}}`)
		case "INVALID_TOKEN":
			w.WriteHeader(400)
			io.WriteString(w, `{"error":{"code":400,"message":"The registration token is not a valid FCM registration token","status":"INVALID_ARGUMENT","details":[{"@type":"type.googleapis.com/google.firebase.fcm.v1.FcmError","errorCode":"INVALID_ARGUMENT"}]}}`)
		case "INVALID_OTHER":
			w.WriteHeader(400)
			io.WriteString(w, `{"error":{"code":400,"message":"Invalid value at 'message.android.ttl'","status":"INVALID_ARGUMENT","details":[{"@type":"type.googleapis.com/google.rpc.BadRequest","fieldViolations":[{"field":"message.android.ttl"}]}]}}`)
		case "SENDER_ID_MISMATCH":
			w.WriteHeader(403)
			io.WriteString(w, `{"error":{"code":403,"message":"SenderId mismatch","status":"PERMISSION_DENIED","details":[{"@type":"type.googleapis.com/google.firebase.fcm.v1.FcmError","errorCode":"SENDER_ID_MISMATCH"}]}}`)
		case "QUOTA_EXCEEDED":
			w.Header().Set("Retry-After", "30")
			w.WriteHeader(429)
			io.WriteString(w, `{"error":{"code":429,"message":"Quota exceeded","status":"RESOURCE_EXHAUSTED","details":[{"@type":"type.googleapis.com/google.firebase.fcm.v1.FcmError","errorCode":"QUOTA_EXCEEDED"}]}}`)
		case "UNAVAILABLE":
			w.WriteHeader(503)
			io.WriteString(w, `{"error":{"code":503,"message":"The service is currently unavailable.","status":"UNAVAILABLE","details":[{"@type":"type.googleapis.com/google.firebase.fcm.v1.FcmError","errorCode":"UNAVAILABLE"}]}}`)
		case "INTERNAL":
			w.WriteHeader(500)
			io.WriteString(w, `{"error":{"code":500,"message":"Internal error","status":"INTERNAL","details":[{"@type":"type.googleapis.com/google.firebase.fcm.v1.FcmError","errorCode":"INTERNAL"}]}}`)
		case "UNAUTHENTICATED":
			w.WriteHeader(401)
			io.WriteString(w, `{"error":{"code":401,"message":"Request had invalid authentication credentials.","status":"UNAUTHENTICATED"}}`)
		}
	}))
	t.Cleanup(g.srv.Close)
	return g
}

type harness struct {
	t      *testing.T
	relay  *Relay
	srv    *httptest.Server
	apple  *fakeApple
	google *fakeGoogle
	log    *bytes.Buffer
	apnsK  *ecdsa.PrivateKey
	fcmK   *rsa.PrivateKey
	now    time.Time
	nowMu  sync.Mutex
}

func (h *harness) clock() time.Time {
	h.nowMu.Lock()
	defer h.nowMu.Unlock()
	return h.now
}

func (h *harness) advance(d time.Duration) {
	h.nowMu.Lock()
	h.now = h.now.Add(d)
	h.nowMu.Unlock()
}

func newHarness(t *testing.T, cfg config.Relay, withApple, withGoogle bool) *harness {
	t.Helper()
	h := &harness{t: t, log: &bytes.Buffer{}, now: time.Unix(1_800_000_000, 0)}
	opts := Options{Log: slog.New(slog.NewTextHandler(h.log, &slog.HandlerOptions{Level: slog.LevelDebug})), Now: h.clock}
	if withApple {
		h.apple = newFakeApple(t)
		h.apnsK, _ = ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		apns, err := NewAPNs(APNsConfig{Key: h.apnsK, KeyID: "KEY1234567", TeamID: "TEAM123456", Topic: "org.example.churr",
			URL: h.apple.srv.URL, HTTP: h.apple.srv.Client(), Now: h.clock})
		if err != nil {
			t.Fatal(err)
		}
		opts.APNs = apns
	}
	if withGoogle {
		h.google = newFakeGoogle(t)
		h.fcmK, _ = rsa.GenerateKey(rand.Reader, 2048)
		der, _ := x509.MarshalPKCS8PrivateKey(h.fcmK)
		creds, _ := json.Marshal(map[string]string{"type": "service_account", "project_id": "churr-test", "client_email": "relay@churr-test.iam.gserviceaccount.com",
			"private_key": string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})), "token_uri": h.google.srv.URL + "/token"})
		fcm, err := NewFCM(FCMConfig{Credentials: creds, URL: h.google.srv.URL, HTTP: h.google.srv.Client(), Now: h.clock})
		if err != nil {
			t.Fatal(err)
		}
		opts.FCM = fcm
	}
	r, err := NewWith(cfg, opts)
	if err != nil {
		t.Fatal(err)
	}
	h.relay = r
	h.srv = httptest.NewServer(r.Handler())
	t.Cleanup(h.srv.Close)
	return h
}

// post sends a body to /v1/wake and returns the status and the answer.
func (h *harness) post(body string) (int, map[string]any) {
	h.t.Helper()
	resp, err := http.Post(h.srv.URL+"/v1/wake", "application/json", strings.NewReader(body))
	if err != nil {
		h.t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func wakeBody(address, platform string, more map[string]any) string {
	m := map[string]any{"push_address": address, "platform": platform, "event_id": "q83vEjRWeJA.AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8",
		"collapse_key": "Zm9vYmFyYmF6cXV4MTIzNA", "priority": "high"}
	for k, v := range more {
		if v == nil {
			delete(m, k)
		} else {
			m[k] = v
		}
	}
	b, _ := json.Marshal(m)
	return string(b)
}

func errOf(out map[string]any) map[string]any {
	e, _ := out["error"].(map[string]any)
	if e == nil {
		return map[string]any{}
	}
	return e
}

func TestRequestShape(t *testing.T) {
	h := newHarness(t, config.Relay{}, true, true)
	token := strings.Repeat("ab", 32)
	for name, c := range map[string]struct {
		body   string
		status int
		code   string
		field  string
	}{
		"not JSON":                  {"hello", 400, "invalid_request", ""},
		"an unknown field":          {wakeBody(token, "ios", map[string]any{"text": "the message"}), 400, "invalid_request", ""},
		"a title":                   {wakeBody(token, "ios", map[string]any{"title": "Sam"}), 400, "invalid_request", ""},
		"a conversation":            {wakeBody(token, "ios", map[string]any{"conversation_id": "0193a0e0-0000-7000-8000-0000000000c0"}), 400, "invalid_request", ""},
		"two objects":               {wakeBody(token, "ios", nil) + wakeBody(token, "ios", nil), 400, "invalid_request", ""},
		"no push address":           {wakeBody(token, "ios", map[string]any{"push_address": nil}), 400, "invalid_request", ""},
		"no priority":               {wakeBody(token, "ios", map[string]any{"priority": nil}), 400, "invalid_request", ""},
		"an empty push address":     {wakeBody("", "ios", nil), 400, "invalid_value", "push_address"},
		"a push address too long":   {wakeBody(strings.Repeat("a", 4097), "ios", nil), 400, "invalid_value", "push_address"},
		"another platform":          {wakeBody(token, "windows", nil), 400, "invalid_value", "platform"},
		"an event id with a space":  {wakeBody(token, "ios", map[string]any{"event_id": "a b"}), 400, "invalid_value", "event_id"},
		"an event id too long":      {wakeBody(token, "ios", map[string]any{"event_id": strings.Repeat("a", 257)}), 400, "invalid_value", "event_id"},
		"a collapse key with a dot": {wakeBody(token, "ios", map[string]any{"collapse_key": "a.b"}), 400, "invalid_value", "collapse_key"},
		"a collapse key too long":   {wakeBody(token, "ios", map[string]any{"collapse_key": strings.Repeat("a", 65)}), 400, "invalid_value", "collapse_key"},
		"another priority":          {wakeBody(token, "ios", map[string]any{"priority": "urgent"}), 400, "invalid_value", "priority"},
		"a negative badge":          {wakeBody(token, "ios", map[string]any{"badge": -1}), 400, "invalid_value", "badge"},
		"a badge too large":         {wakeBody(token, "ios", map[string]any{"badge": 100000}), 400, "invalid_value", "badge"},
		"a badge that is not whole": {wakeBody(token, "ios", map[string]any{"badge": 1.5}), 400, "invalid_value", "badge"},
		"a badge that is text":      {wakeBody(token, "ios", map[string]any{"badge": "7"}), 400, "invalid_value", "badge"},
		"a body over 8 KB":          {`{"push_address":"` + strings.Repeat("a", 9000) + `"}`, 413, "too_large", ""},
	} {
		status, out := h.post(c.body)
		e := errOf(out)
		if status != c.status || e["code"] != c.code || (c.field != "" && e["field"] != c.field) {
			t.Errorf("%s: %d %v, want %d %s %s", name, status, out, c.status, c.code, c.field)
		}
	}
	// Another content type is refused too.
	resp, err := http.Post(h.srv.URL+"/v1/wake", "text/plain", strings.NewReader(wakeBody(token, "ios", nil)))
	if err != nil || resp.StatusCode != 400 {
		t.Fatalf("text/plain: %v %v", resp, err)
	}
	resp.Body.Close()
	if len(h.apple.requests) != 0 || len(h.google.sends) != 0 {
		t.Fatal("a malformed request was forwarded")
	}
	// What is accepted: with a badge, with none, with null, and the
	// largest values.
	for name, body := range map[string]string{
		"a badge":      wakeBody(token, "ios", map[string]any{"badge": 7}),
		"badge zero":   wakeBody(token+"1", "ios", map[string]any{"badge": 0}),
		"no badge":     wakeBody(token+"2", "ios", nil),
		"a null badge": strings.Replace(wakeBody(token+"3", "ios", map[string]any{"badge": 1}), `"badge":1`, `"badge":null`, 1),
		"the largest": wakeBody(strings.Repeat("a", 4096), "ios", map[string]any{"badge": 99999, "priority": "normal",
			"event_id": strings.Repeat("a", 256), "collapse_key": strings.Repeat("b", 64)}),
	} {
		if status, out := h.post(body); status != 200 || out["result"] != "sent" {
			t.Errorf("%s: %d %v", name, status, out)
		}
	}
	// The other endpoints: healthz, the one paragraph, and nothing else.
	get := func(path string) (int, string) {
		resp, err := http.Get(h.srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b)
	}
	if status, body := get("/healthz"); status != 200 || body != "ok" {
		t.Errorf("/healthz: %d %q", status, body)
	}
	if status, body := get("/"); status != 200 || !strings.Contains(body, "push relay") {
		t.Errorf("/: %d %q", status, body)
	}
	for _, path := range []string{"/status", "/counters", "/v1/wake", "/metrics"} {
		if status, _ := get(path); status != 404 && status != 405 {
			t.Errorf("GET %s = %d", path, status)
		}
	}
}

func TestAPNs(t *testing.T) {
	h := newHarness(t, config.Relay{}, true, false)
	token := strings.Repeat("8a1f", 16)
	status, out := h.post(wakeBody(token, "ios", map[string]any{"badge": 7}))
	if status != 200 || out["result"] != "sent" {
		t.Fatalf("sent: %d %v", status, out)
	}
	r := h.apple.requests[0]
	if r.ProtoMajor != 2 {
		t.Errorf("Apple was reached over HTTP/%d", r.ProtoMajor)
	}
	if r.Method != "POST" || r.URL.Path != "/3/device/"+token {
		t.Errorf("the request line: %s %s", r.Method, r.URL.Path)
	}
	wantHeaders := map[string]string{
		"apns-topic": "org.example.churr", "apns-push-type": "alert", "apns-priority": "10",
		"apns-collapse-id": "Zm9vYmFyYmF6cXV4MTIzNA", "apns-expiration": strconv.FormatInt(h.now.Add(72*time.Hour).Unix(), 10),
	}
	for k, v := range wantHeaders {
		if r.Header.Get(k) != v {
			t.Errorf("%s: %q, want %q", k, r.Header.Get(k), v)
		}
	}
	// The exact payload.
	var payload any
	json.Unmarshal([]byte(h.apple.bodies[0]), &payload)
	want := map[string]any{
		"aps": map[string]any{"alert": map[string]any{"loc-key": "WAKE_FALLBACK"}, "mutable-content": float64(1), "sound": "default", "badge": float64(7)},
		"e":   "q83vEjRWeJA.AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8", "c": "Zm9vYmFyYmF6cXV4MTIzNA",
	}
	if !reflect.DeepEqual(payload, want) {
		t.Errorf("the payload: %s", h.apple.bodies[0])
	}
	// The token: ES256, the key identifier in the header, the team and
	// the issue time in the body, signed by the key.
	auth := r.Header.Get("authorization")
	parts := strings.Split(strings.TrimPrefix(auth, "bearer "), ".")
	if !strings.HasPrefix(auth, "bearer ") || len(parts) != 3 {
		t.Fatalf("authorization: %q", auth)
	}
	var header, claims map[string]any
	hb, _ := base64.RawURLEncoding.DecodeString(parts[0])
	cb, _ := base64.RawURLEncoding.DecodeString(parts[1])
	sig, _ := base64.RawURLEncoding.DecodeString(parts[2])
	json.Unmarshal(hb, &header)
	json.Unmarshal(cb, &claims)
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if header["alg"] != "ES256" || header["kid"] != "KEY1234567" || claims["iss"] != "TEAM123456" || claims["iat"] != float64(h.now.Unix()) ||
		len(sig) != 64 || !ecdsa.Verify(&h.apnsK.PublicKey, digest[:], new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:])) {
		t.Errorf("the token: %v %v", header, claims)
	}

	// Normal priority, and no badge when the request had none.
	h.post(wakeBody(token+"00", "ios", map[string]any{"priority": "normal"}))
	if got := h.apple.requests[1].Header.Get("apns-priority"); got != "5" || strings.Contains(h.apple.bodies[1], "badge") {
		t.Errorf("normal priority, no badge: %q %s", got, h.apple.bodies[1])
	}
	// One token is reused for fifty minutes and then replaced.
	if h.apple.requests[1].Header.Get("authorization") != auth {
		t.Error("a new token was signed for the second request")
	}
	h.advance(51 * time.Minute)
	h.post(wakeBody(token+"01", "ios", nil))
	if h.apple.requests[2].Header.Get("authorization") == auth {
		t.Error("the token was not replaced after fifty minutes")
	}

	// The answers.
	for name, c := range map[string]struct {
		status       int
		reason       string
		want         int
		code         string
		counter      string
		scope        string
		loud         bool
		retryAfter   bool
		platformOff  bool
		fromFakeOnly bool
	}{
		"unregistered":            {status: 410, reason: "Unregistered", want: 410, code: "gone", counter: "ios.gone"},
		"a bad device token":      {status: 400, reason: "BadDeviceToken", want: 410, code: "gone", counter: "ios.gone"},
		"a token for another app": {status: 400, reason: "DeviceTokenNotForTopic", want: 410, code: "gone", counter: "ios.gone"},
		"too many requests":       {status: 429, reason: "TooManyRequests", want: 429, code: "rate_limited", counter: "ios.rate_limited", scope: "device", retryAfter: true},
		"the relay's key refused": {status: 403, reason: "InvalidProviderToken", want: 503, code: "unavailable", counter: "ios.upstream_error", loud: true},
		"a server error":          {status: 500, reason: "InternalServerError", want: 502, code: "unavailable", counter: "ios.upstream_error"},
		"shutting down":           {status: 503, reason: "Shutdown", want: 502, code: "unavailable", counter: "ios.upstream_error"},
		"another bad request":     {status: 400, reason: "BadCollapseId", want: 502, code: "unavailable", counter: "ios.upstream_error"},
	} {
		tok := strings.Repeat("c", 60) + strconv.Itoa(len(name)) + strconv.Itoa(c.status)
		h.apple.answer(tok, c.status, c.reason)
		before := h.relay.Counts().Results[c.counter]
		h.log.Reset()
		status, out := h.post(wakeBody(tok, "ios", nil))
		e := errOf(out)
		if status != c.want || e["code"] != c.code || (c.scope != "" && e["scope"] != c.scope) || (c.retryAfter && e["retry_after_seconds"] == nil) {
			t.Errorf("%s: %d %v", name, status, out)
		}
		if e["platform_off"] == true {
			t.Errorf("%s: platform_off on a relay that has the key", name)
		}
		if got := h.relay.Counts().Results[c.counter]; got != before+1 {
			t.Errorf("%s: counter %s went from %d to %d", name, c.counter, before, got)
		}
		if c.loud != strings.Contains(h.log.String(), "level=ERROR") {
			t.Errorf("%s: log %q", name, h.log.String())
		}
	}
	// Apple unreachable is a 502.
	h.apple.srv.Close()
	if status, out := h.post(wakeBody(token+"02", "ios", nil)); status != 502 || errOf(out)["code"] != "unavailable" {
		t.Errorf("Apple unreachable: %d %v", status, out)
	}
	// With no Google file, Android is "platform off".
	status, out = h.post(wakeBody("fcm-token", "android", nil))
	if e := errOf(out); status != 503 || e["code"] != "unavailable" || e["platform_off"] != true {
		t.Errorf("android with no key: %d %v", status, out)
	}
}

func TestFCM(t *testing.T) {
	h := newHarness(t, config.Relay{}, false, true)
	status, out := h.post(wakeBody("fcm-registration-token", "android", map[string]any{"badge": 7}))
	if status != 200 || out["result"] != "sent" {
		t.Fatalf("sent: %d %v", status, out)
	}
	r := h.google.sends[0]
	if r.Method != "POST" || r.URL.Path != "/v1/projects/churr-test/messages:send" || r.Header.Get("Authorization") != "Bearer ya29.access-1" {
		t.Errorf("the request: %s %s %q", r.Method, r.URL.Path, r.Header.Get("Authorization"))
	}
	// The exact body: a data message with no notification part.
	var body any
	json.Unmarshal([]byte(h.google.bodies[0]), &body)
	want := map[string]any{"message": map[string]any{
		"token":   "fcm-registration-token",
		"data":    map[string]any{"e": "q83vEjRWeJA.AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8", "c": "Zm9vYmFyYmF6cXV4MTIzNA", "b": "7"},
		"android": map[string]any{"priority": "HIGH", "ttl": "259200s"},
	}}
	if !reflect.DeepEqual(body, want) {
		t.Errorf("the body: %s", h.google.bodies[0])
	}
	// The assertion: RS256, the firebase.messaging scope, signed by the
	// service account's key.
	form := h.google.asserted[0]
	if !strings.Contains(form, "grant_type=urn%3Aietf%3Aparams%3Aoauth%3Agrant-type%3Ajwt-bearer") {
		t.Fatalf("the token request: %s", form)
	}
	assertion := form[strings.Index(form, "assertion=")+len("assertion="):]
	if i := strings.Index(assertion, "&"); i >= 0 {
		assertion = assertion[:i]
	}
	parts := strings.Split(assertion, ".")
	var claims map[string]any
	cb, _ := base64.RawURLEncoding.DecodeString(parts[1])
	sig, _ := base64.RawURLEncoding.DecodeString(parts[2])
	json.Unmarshal(cb, &claims)
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if claims["iss"] != "relay@churr-test.iam.gserviceaccount.com" || claims["scope"] != "https://www.googleapis.com/auth/firebase.messaging" ||
		claims["aud"] != h.google.srv.URL+"/token" || claims["exp"].(float64)-claims["iat"].(float64) != 3600 ||
		rsa.VerifyPKCS1v15(&h.fcmK.PublicKey, 5 /* crypto.SHA256 */, digest[:], sig) != nil {
		t.Errorf("the assertion: %v", claims)
	}

	// Normal priority, no badge.
	h.post(wakeBody("another-token", "android", map[string]any{"priority": "normal"}))
	if !strings.Contains(h.google.bodies[1], `"priority":"NORMAL"`) || strings.Contains(h.google.bodies[1], `"b"`) {
		t.Errorf("normal priority, no badge: %s", h.google.bodies[1])
	}
	// The access token is reused, and renewed before it runs out.
	if h.google.tokens != 1 {
		t.Errorf("%d token exchanges for two sends", h.google.tokens)
	}
	h.advance(56 * time.Minute)
	h.post(wakeBody("a-third-token", "android", nil))
	if h.google.tokens != 2 || h.google.sends[2].Header.Get("Authorization") != "Bearer ya29.access-2" {
		t.Errorf("the token was not renewed before it ran out: %d exchanges", h.google.tokens)
	}

	for name, c := range map[string]struct {
		answer  string
		want    int
		code    string
		counter string
		loud    bool
	}{
		"unregistered":                  {"UNREGISTERED", 410, "gone", "android.gone", false},
		"an invalid token":              {"INVALID_TOKEN", 410, "gone", "android.gone", false},
		"a token of another app":        {"SENDER_ID_MISMATCH", 410, "gone", "android.gone", false},
		"quota exceeded":                {"QUOTA_EXCEEDED", 429, "rate_limited", "android.rate_limited", false},
		"unavailable":                   {"UNAVAILABLE", 502, "unavailable", "android.upstream_error", false},
		"internal":                      {"INTERNAL", 502, "unavailable", "android.upstream_error", false},
		"an invalid argument elsewhere": {"INVALID_OTHER", 502, "unavailable", "android.upstream_error", false},
		"the relay's token refused":     {"UNAUTHENTICATED", 503, "unavailable", "android.upstream_error", true},
	} {
		tok := "token-for-" + c.answer
		h.google.mu.Lock()
		h.google.answers[tok] = c.answer
		h.google.mu.Unlock()
		before := h.relay.Counts().Results[c.counter]
		h.log.Reset()
		status, out := h.post(wakeBody(tok, "android", nil))
		e := errOf(out)
		if status != c.want || e["code"] != c.code {
			t.Errorf("%s: %d %v", name, status, out)
		}
		if c.answer == "QUOTA_EXCEEDED" && (e["scope"] != "device" || e["retry_after_seconds"] != float64(30)) {
			t.Errorf("%s: %v", name, e)
		}
		if got := h.relay.Counts().Results[c.counter]; got != before+1 {
			t.Errorf("%s: counter %s went from %d to %d", name, c.counter, before, got)
		}
		if c.loud != strings.Contains(h.log.String(), "level=ERROR") {
			t.Errorf("%s: log %q", name, h.log.String())
		}
	}
	// With no Apple key, iPhones are "platform off".
	status, out = h.post(wakeBody(strings.Repeat("ab", 32), "ios", nil))
	if e := errOf(out); status != 503 || e["platform_off"] != true {
		t.Errorf("ios with no key: %d %v", status, out)
	}
}

func TestLimits(t *testing.T) {
	h := newHarness(t, config.Relay{DeviceRate: 3, DeviceDaily: 5, AddressRate: 12, MaxInflight: 2}, true, false)
	one, two := strings.Repeat("a1", 32), strings.Repeat("b2", 32)
	for i := 0; i < 3; i++ {
		if status, out := h.post(wakeBody(one, "ios", nil)); status != 200 {
			t.Fatalf("wake-up %d: %d %v", i, status, out)
		}
	}
	// Per push address: so many a minute.
	status, out := h.post(wakeBody(one, "ios", nil))
	if e := errOf(out); status != 429 || e["code"] != "rate_limited" || e["scope"] != "device" || e["retry_after_seconds"].(float64) < 1 {
		t.Fatalf("over the per-minute limit: %d %v", status, out)
	}
	// Another device is not affected.
	if status, _ := h.post(wakeBody(two, "ios", nil)); status != 200 {
		t.Fatalf("another device: %d", status)
	}
	// The daily limit: a minute later the minute's bucket has refilled,
	// but only five a day go through.
	h.advance(time.Minute)
	for i := 0; i < 2; i++ {
		if status, out := h.post(wakeBody(one, "ios", nil)); status != 200 {
			t.Fatalf("wake-up %d of the next minute: %d %v", i, status, out)
		}
	}
	status, out = h.post(wakeBody(one, "ios", nil))
	if e := errOf(out); status != 429 || e["scope"] != "device" || e["retry_after_seconds"].(float64) < 3600 {
		t.Fatalf("over the daily limit: %d %v", status, out)
	}
	// Per calling address: twelve a minute, whatever the device.
	h.advance(time.Minute)
	limited := 0
	for i := 0; i < 14; i++ {
		status, out := h.post(wakeBody(strings.Repeat("c", 62)+strconv.Itoa(10+i), "ios", nil))
		if status == 429 {
			if errOf(out)["scope"] != "address" {
				t.Fatalf("the calling address's limit: %v", out)
			}
			limited++
		}
	}
	if limited != 2 {
		t.Fatalf("%d of 14 requests were limited by calling address, want 2", limited)
	}
	if h.relay.Counts().Results["ios.rate_limited"] != 2 || h.relay.Counts().Results["none.rate_limited"] != 2 {
		t.Fatalf("the counters: %v", h.relay.Counts().Results)
	}
	// The buckets are keyed by a salted hash: no push address is held.
	for _, key := range []string{h.relay.hashed("device", one), h.relay.hashed("address", "127.0.0.1")} {
		if strings.Contains(key, one) || strings.Contains(key, "127.0.0.1") || len(key) != 32 {
			t.Fatalf("a bucket key: %q", key)
		}
	}
	other, _ := NewWith(config.Relay{}, Options{Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if other.hashed("device", one) == h.relay.hashed("device", one) {
		t.Fatal("two relays hash a push address the same way: the salt is not random")
	}
}

func TestCallingAddress(t *testing.T) {
	r, err := NewWith(config.Relay{Common: config.Common{TrustedProxies: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}}},
		Options{Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ remote, forwarded, want string }{
		{"203.0.113.9:4000", "", "203.0.113.9"},
		{"203.0.113.9:4000", "198.51.100.1", "203.0.113.9"},          // not a trusted proxy: the header is not believed
		{"10.1.2.3:4000", "198.51.100.1", "198.51.100.1"},            // a trusted proxy: it is
		{"10.1.2.3:4000", "192.0.2.1, 198.51.100.7", "198.51.100.7"}, // the address the proxy added
		{"[2001:db8:1:2:3:4:5:6]:4000", "", "2001:db8:1:2::/64"},     // IPv6 is counted by /64
		{"10.1.2.3:4000", "2001:db8:aa:bb:1:2:3:4", "2001:db8:aa:bb::/64"},
	} {
		req := httptest.NewRequest("POST", "/v1/wake", nil)
		req.RemoteAddr = c.remote
		if c.forwarded != "" {
			req.Header.Set("X-Forwarded-For", c.forwarded)
		}
		if got := r.caller(req); got != c.want {
			t.Errorf("caller(%s, %q) = %s, want %s", c.remote, c.forwarded, got, c.want)
		}
	}
}

// The development flag: accept and count, send nothing. Without it a relay
// with no keys answers "platform off".
func TestDryRunAndNoKeys(t *testing.T) {
	plain := newHarness(t, config.Relay{}, false, false)
	for _, platform := range []string{"ios", "android"} {
		status, out := plain.post(wakeBody("some-token-"+platform, platform, nil))
		if e := errOf(out); status != 503 || e["code"] != "unavailable" || e["platform_off"] != true {
			t.Errorf("%s with no keys: %d %v", platform, status, out)
		}
	}
	dry := newHarness(t, config.Relay{DryRun: true}, true, true)
	for _, platform := range []string{"ios", "android"} {
		if status, out := dry.post(wakeBody("some-token-"+platform, platform, map[string]any{"badge": 3})); status != 200 || out["result"] != "sent" {
			t.Errorf("%s in a dry run: %d %v", platform, status, out)
		}
	}
	// The shape is still checked in a dry run.
	if status, _ := dry.post(wakeBody("some-token", "ios", map[string]any{"text": "hello"})); status != 400 {
		t.Errorf("an unknown field in a dry run: %d", status)
	}
	if len(dry.apple.requests) != 0 || len(dry.google.sends) != 0 || dry.google.tokens != 0 {
		t.Fatal("a dry run sent something")
	}
	c := dry.relay.Counts()
	if c.Requests != 3 || c.Results["ios.sent"] != 1 || c.Results["android.sent"] != 1 || c.Results["none.invalid"]+c.Results["ios.invalid"] != 1 {
		t.Fatalf("the counters of a dry run: %+v", c)
	}
}

// Nothing that names a device, an event or a caller is ever logged, and
// the counters are all there is.
func TestNothingIsLogged(t *testing.T) {
	h := newHarness(t, config.Relay{}, true, true)
	address, eventID, collapse := strings.Repeat("d4", 32), "SECRETroute.SECRETtokenSECRETtoken", "SECRETcollapseKEY12345"
	body := func(platform, addr string) string {
		return wakeBody(addr, platform, map[string]any{"event_id": eventID, "collapse_key": collapse})
	}
	h.apple.answer(address+"gone", 410, "Unregistered")
	h.apple.answer(address+"key", 403, "InvalidProviderToken")
	h.post(body("ios", address))
	h.post(body("ios", address+"gone"))
	h.post(body("ios", address+"key"))
	h.post(body("android", address))
	h.post(body("android", address) + "junk")
	h.relay.LogCounters()
	logged := h.log.String()
	for _, secret := range []string{address, eventID, collapse, "SECRET", "127.0.0.1"} {
		if strings.Contains(logged, secret) {
			t.Fatalf("the log names %q:\n%s", secret, logged)
		}
	}
	if !strings.Contains(logged, "counters") || !strings.Contains(logged, "requests=5") || !strings.Contains(logged, "ios.sent=1") ||
		!strings.Contains(logged, "ios.gone=1") || !strings.Contains(logged, "android.sent=1") {
		t.Fatalf("the counters line: %s", logged)
	}
	c := h.relay.Counts()
	if c.Requests != 5 || c.RequestsThisHour != 5 || !reflect.DeepEqual(c.Results, c.ResultsThisHour) {
		t.Fatalf("the counters: %+v", c)
	}
	for k := range c.Results {
		parts := strings.SplitN(k, ".", 2)
		if (parts[0] != "ios" && parts[0] != "android" && parts[0] != "none") ||
			(parts[1] != Sent && parts[1] != Gone && parts[1] != RateLimited && parts[1] != invalid && parts[1] != UpstreamError) {
			t.Errorf("a counter that is not in the document: %s", k)
		}
	}
}

// The relay reads its keys from the files the flags name, and runs.
func TestKeysFromFilesAndRun(t *testing.T) {
	dir := t.TempDir()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	der, _ := x509.MarshalPKCS8PrivateKey(key)
	p8 := filepath.Join(dir, "AuthKey.p8")
	os.WriteFile(p8, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), 0o600)
	rsaKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	rsaDER, _ := x509.MarshalPKCS8PrivateKey(rsaKey)
	creds, _ := json.Marshal(map[string]string{"project_id": "p", "client_email": "e@p", "token_uri": "https://oauth2.googleapis.com/token",
		"private_key": string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: rsaDER}))})
	fcm := filepath.Join(dir, "fcm.json")
	os.WriteFile(fcm, creds, 0o600)
	cfg := config.Relay{APNsKey: p8, APNsKeyID: "K", APNsTeamID: "T", APNsTopic: "org.example.churr", FCMCredentials: fcm}
	r, err := New(cfg)
	if err != nil || r.apns == nil || r.fcm == nil {
		t.Fatalf("New: %v", err)
	}
	// Files that are not keys are refused at start, in plain words.
	os.WriteFile(p8, []byte("not a key"), 0o600)
	if _, err := New(cfg); err == nil {
		t.Fatal("a .p8 file that is not a key was accepted")
	}
	cfg.APNsKey = ""
	os.WriteFile(fcm, []byte(`{"project_id":"p"}`), 0o600)
	if _, err := New(cfg); err == nil {
		t.Fatal("a service account file with no key was accepted")
	}
	// Run serves until its context ends.
	ln := httptest.NewUnstartedServer(nil).Listener
	var log bytes.Buffer
	run, err := NewWith(config.Relay{DryRun: true}, Options{Listener: ln, Log: slog.New(slog.NewTextHandler(&log, nil))})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- run.Run(ctx) }()
	var resp *http.Response
	for i := 0; i < 100; i++ {
		if resp, err = http.Get("http://" + ln.Addr().String() + "/healthz"); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(log.String(), "started") || !strings.Contains(log.String(), "dry run") || !strings.Contains(log.String(), "stopped") {
		t.Fatalf("the log of a run: %s", log.String())
	}
}
