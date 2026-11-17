// Package testkit starts whole servers for tests and gives them clients:
// browsers with a virtual passkey authenticator and a cookie jar, bearer
// tokens, and live connections. It is imported only from _test.go files.
//
// A server runs in the test's process on a random port with a temporary
// data directory, on SQLite, or on PostgreSQL when CHURR_TEST_POSTGRES is
// set (tools/pgtest.sh sets it). Accounts are made through the real API.
package testkit

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/cookiejar"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/descope/virtualwebauthn"

	"github.com/tikaydaes/churr/internal/bots"
	"github.com/tikaydaes/churr/internal/calls"
	"github.com/tikaydaes/churr/internal/config"
	"github.com/tikaydaes/churr/internal/live"
	"github.com/tikaydaes/churr/internal/memguard"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/server"
	"github.com/tikaydaes/churr/internal/store/storetest"
	"github.com/tikaydaes/churr/internal/wirepb"
)

// Options change how NewServer starts a server.
type Options struct {
	// NoOwner leaves first-run setup undone, for tests of setup itself.
	NoOwner bool
	// Live tunes the WebSocket hub: tests of heartbeats and back-pressure
	// shorten its times and its queue.
	Live live.Options
	// BotLimits replaces the limits on bots and webhooks, for tests that
	// reach them.
	BotLimits *bots.Limits
	// CallTimings shortens the waits of calls (tickets, the hold after a
	// drop, the ring) for tests that reach them.
	CallTimings calls.Timings
	// Hostname is the server's name, ending in .localhost, for tests that
	// run several servers and link them (linked.go). Empty means
	// localhost.
	Hostname string
	// LinkSkew, if set, is added to the clock the server's link uses, in
	// milliseconds, each time the clock is read: a server whose clock is
	// wrong, as the servers it is linked to see it.
	LinkSkew *atomic.Int64
	// Memory replaces where the server's memory guard reads how much
	// memory there is, for tests of a server that is short of it.
	Memory memguard.Source
	// ChatRequests keeps every account's preferences as a new account's:
	// new direct and sealed chats are chat requests (ruling R78). Without
	// it each account made here turns both auto-approve preferences on, so
	// the tests of everything else open chats as they always did.
	ChatRequests bool
}

// TestServer is a whole server running in the test process.
type TestServer struct {
	t testing.TB
	// Server gives tests the services directly, for what the API cannot
	// do yet (making an admin).
	Server *server.Server
	// URL is where the server listens; Origin is the origin it believes
	// it has, which is what passkeys and the origin check are tied to.
	URL, Origin string
	// Owner is the owner's browser, unless Options.NoOwner was set.
	Owner *Client
	rp    virtualwebauthn.RelyingParty
	// What Restart needs to start the same server again: its
	// configuration, its options, its port, and how to stop it.
	cfg  config.Serve
	opts server.Options
	port int
	stop func()
	// chatRequests is Options.ChatRequests.
	chatRequests bool
}

// NewServer starts a server and, unless told not to, runs first-run setup
// with an owner called "owner". The server stops when the test ends.
func NewServer(t testing.TB, opts ...Options) *TestServer {
	t.Helper()
	var o Options
	if len(opts) > 0 {
		o = opts[0]
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	args := []string{"-dev", "-data", t.TempDir(), "-http", fmt.Sprintf("127.0.0.1:%d", port), "-log-level", "error",
		// Call media on a spare port of the loopback address, so servers
		// of tests running side by side never ask for the same one.
		"-media", "127.0.0.1:0", "-media-address", "127.0.0.1"}
	if storetest.UsePostgres() {
		args = append(args, "-db", storetest.PostgresURL(t))
	}
	if o.Hostname != "" {
		args = append(args, "-hostname", o.Hostname)
	}
	cfg, err := config.ParseServe(args, func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	sopts := server.Options{
		Listener: ln, Live: o.Live, BotLimits: o.BotLimits, CallTimings: o.CallTimings,
		// Only failures of the server itself are worth a test's log.
		Log: slog.New(slog.NewTextHandler(logWriter{t}, &slog.HandlerOptions{Level: logLevel()})),
		// Tests make requests far faster than a person does.
		RatePerSecond: 1000, RateBurst: 100000,
		// Identity providers run in the test process (idp.go).
		HTTPClient: idpHTTPClient(),
		// Linked servers have made-up names; every one is on loopback.
		LinkHTTPClient: LoopbackClient(),
		LinkNow:        linkClock(o.LinkSkew),
		MemorySource:   o.Memory,
	}
	srv, err := server.NewWith(cfg, sopts)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		srv.Run(ctx)
		close(done)
	}()
	ts := &TestServer{t: t, Server: srv, URL: "http://" + ln.Addr().String(), Origin: cfg.Origin(), cfg: cfg, opts: sopts, port: port,
		chatRequests: o.ChatRequests}
	ts.stop = func() {
		cancel()
		<-done
	}
	t.Cleanup(func() { ts.stop() })
	ts.rp = virtualwebauthn.RelyingParty{Name: "Churr", ID: cfg.Hostname, Origin: ts.Origin}
	if !o.NoOwner {
		ts.Owner = ts.Browser()
		ts.Owner.Setup("owner")
	}
	return ts
}

// Client is one browser (a cookie jar and a passkey authenticator) or one
// script (a bearer token).
type Client struct {
	ts   *TestServer
	t    testing.TB
	http *http.Client
	// Bearer, when set, is sent instead of the cookie.
	Bearer string
	auth   virtualwebauthn.Authenticator
	cred   virtualwebauthn.Credential
	// ID and Username are the account, once it has registered.
	ID       model.AccountID
	Username string
	// NoOrigin makes requests leave the Origin header out, to test the
	// origin check.
	NoOrigin bool
	// accessKey is the 32 random bytes of this browser's access key, as a
	// person would keep them (accesskey.go).
	accessKey []byte
}

// Browser returns a new browser that is not signed in.
func (ts *TestServer) Browser() *Client {
	jar, _ := cookiejar.New(nil)
	return &Client{ts: ts, t: ts.t, http: &http.Client{Jar: jar}, auth: virtualwebauthn.NewAuthenticator(),
		cred: virtualwebauthn.NewCredential(virtualwebauthn.KeyTypeEC2)}
}

// Do sends a request and decodes the JSON answer. body may be nil.
func (c *Client) Do(method, path string, body any) (int, map[string]any) {
	c.t.Helper()
	status, _, out := c.DoFull(method, path, body)
	return status, out
}

// DoFull is Do that also returns the response headers.
func (c *Client) DoFull(method, path string, body any) (int, http.Header, map[string]any) {
	c.t.Helper()
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			c.t.Fatal(err)
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.ts.URL+path, rd)
	if err != nil {
		c.t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.Bearer != "" {
		req.Header.Set("Authorization", "Bearer "+c.Bearer)
	} else if !c.NoOrigin {
		// A browser sends the page's origin with every request that
		// could change something.
		req.Header.Set("Origin", c.ts.Origin)
	}
	if wireProtobuf {
		var sent []byte
		if body != nil {
			sent, _ = json.Marshal(body)
		}
		req = protoRequest(req, sent) // see protobuf.go
	}
	hc := c.http
	if c.Bearer != "" {
		hc = &http.Client{} // a script has no cookie jar
	}
	resp, err := hc.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	out := map[string]any{}
	raw, _ := io.ReadAll(resp.Body)
	if wireProtobuf {
		if raw, err = protoResponse(req, resp, raw); err != nil {
			c.t.Fatalf("%s %s: the protobuf answer could not be read: %v", method, path, err)
		}
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &out); err != nil {
			out = map[string]any{"_raw": string(raw)}
		}
	}
	return resp.StatusCode, resp.Header, out
}

// Must is Do, failing the test unless the status is the one wanted.
func (c *Client) Must(want int, method, path string, body any) map[string]any {
	c.t.Helper()
	got, out := c.Do(method, path, body)
	if got != want {
		c.t.Fatalf("%s %s = %d %v, want %d", method, path, got, out, want)
	}
	return out
}

// Refused is Do, failing the test unless the request is refused with this
// status and error code.
func (c *Client) Refused(status int, code model.Code, method, path string, body any) map[string]any {
	c.t.Helper()
	got, out := c.Do(method, path, body)
	e, _ := out["error"].(map[string]any)
	if got != status || e["code"] != string(code) {
		c.t.Fatalf("%s %s = %d %v, want %d %s", method, path, got, out, status, code)
	}
	return e
}

// create runs a passkey creation against a begin answer and returns the
// body for the matching finish request.
func (c *Client) create(begin map[string]any) map[string]any {
	c.t.Helper()
	pk := begin["public_key"].(map[string]any)
	raw, _ := json.Marshal(map[string]any{"publicKey": pk})
	opts, err := virtualwebauthn.ParseAttestationOptions(string(raw))
	if err != nil {
		c.t.Fatal(err)
	}
	handle := pk["user"].(map[string]any)["id"].(string)
	c.auth.Options.UserHandle, _ = base64.RawURLEncoding.DecodeString(handle)
	resp := virtualwebauthn.CreateAttestationResponse(c.ts.rp, c.auth, c.cred, *opts)
	c.auth.AddCredential(c.cred)
	return map[string]any{"ceremony_id": begin["ceremony_id"], "credential": json.RawMessage(resp), "passkey_name": "Test passkey", "prf_enabled": true}
}

func (c *Client) signedIn(out map[string]any) {
	acc := out["account"].(map[string]any)
	c.ID = model.AccountID(acc["id"].(string))
	c.Username = acc["username"].(string)
	if !c.ts.chatRequests {
		// Straight into the store, so the account's first moments send
		// no event a test of something else would have to step over.
		_, err := c.ts.Server.Store.Accounts().Update(context.Background(), c.ID, func(a *model.Account) error {
			var prefs map[string]any
			_ = json.Unmarshal(a.PrefsJSON, &prefs)
			if prefs == nil {
				prefs = map[string]any{}
			}
			prefs["auto_approve_direct"], prefs["auto_approve_sealed"] = true, true
			a.PrefsJSON, _ = json.Marshal(prefs)
			return nil
		})
		if err != nil {
			c.t.Fatal(err)
		}
	}
}

// SetupToken is the one-time value of the server's setup link.
func (ts *TestServer) SetupToken() string {
	link := ts.Server.SetupLink
	return link[strings.LastIndex(link, "/")+1:]
}

// Setup runs first-run setup in this browser, making the owner.
func (c *Client) Setup(username string) map[string]any {
	c.t.Helper()
	begin := c.Must(200, "POST", "/api/v1/setup/begin", map[string]any{"setup_token": c.ts.SetupToken(), "username": username, "display_name": username})
	out := c.Must(201, "POST", "/api/v1/setup/finish", c.create(begin))
	c.signedIn(out)
	return out
}

// Register registers a new account in this browser with an invite code.
func (c *Client) Register(username, inviteCode string) map[string]any {
	c.t.Helper()
	begin := c.Must(200, "POST", "/api/v1/passkey/register/begin", map[string]any{"invite_code": inviteCode, "username": username, "display_name": username})
	out := c.Must(201, "POST", "/api/v1/passkey/register/finish", c.create(begin))
	c.signedIn(out)
	return out
}

// Apply registers without an invite while registrations wait for approval
// (docs/FEATURES.md section 31): the account is made with this browser's
// passkey and nobody is signed in. It returns the finish request's status
// and answer, and fills in the browser's username.
func (c *Client) Apply(username, note string) (int, map[string]any) {
	c.t.Helper()
	begin := c.Must(200, "POST", "/api/v1/passkey/register/begin", map[string]any{"username": username, "display_name": username, "note": note})
	c.Username = username
	return c.Do("POST", "/api/v1/passkey/register/finish", c.create(begin))
}

// SignedIn takes a sign-in answer as this browser's: its account and, for
// a later Login, nothing more.
func (c *Client) SignedIn(out map[string]any) { c.signedIn(out) }

// Login signs this browser in with its passkey and returns the status and
// the answer.
func (c *Client) Login() (int, map[string]any) {
	c.t.Helper()
	begin := c.Must(200, "POST", "/api/v1/passkey/login/begin", nil)
	raw, _ := json.Marshal(map[string]any{"publicKey": begin["public_key"]})
	opts, err := virtualwebauthn.ParseAssertionOptions(string(raw))
	if err != nil {
		c.t.Fatal(err)
	}
	resp := virtualwebauthn.CreateAssertionResponse(c.ts.rp, c.auth, c.cred, *opts)
	return c.Do("POST", "/api/v1/passkey/login/finish", map[string]any{"ceremony_id": begin["ceremony_id"], "credential": json.RawMessage(resp)})
}

// invite makes an invite as the owner and returns its code.
func (ts *TestServer) invite(body map[string]any) string {
	ts.t.Helper()
	out := ts.Owner.Must(201, "POST", "/api/v1/invites", body)
	return out["invite"].(map[string]any)["code"].(string)
}

// Member makes an account with the role member, through an invite and the
// real registration, and returns its browser.
func (ts *TestServer) Member(name string) *Client {
	ts.t.Helper()
	c := ts.Browser()
	c.Register(name, ts.invite(map[string]any{}))
	return c
}

// Guest makes a guest who starts in the given channels.
func (ts *TestServer) Guest(name string, channels ...string) *Client {
	ts.t.Helper()
	c := ts.Browser()
	c.Register(name, ts.invite(map[string]any{"role": "guest", "conversation_ids": channels}))
	return c
}

// Admin makes a member and then gives it the admin role. The role is set in
// the store, because the operation that changes roles is not built yet.
func (ts *TestServer) Admin(name string) *Client {
	ts.t.Helper()
	c := ts.Member(name)
	if _, _, err := ts.Server.Store.Accounts().SetRole(context.Background(), c.ID, model.RoleAdmin); err != nil {
		ts.t.Fatal(err)
	}
	return c
}

// Token makes a personal token for this browser's account and returns a
// client that uses it as a bearer token.
func (c *Client) Token() *Client {
	c.t.Helper()
	out := c.Must(201, "POST", "/api/v1/me/tokens", map[string]any{"label": "test"})
	return &Client{ts: c.ts, t: c.t, http: &http.Client{}, Bearer: out["secret"].(string), ID: c.ID, Username: c.Username}
}

// Channel creates a named channel as this account and returns its id.
func (c *Client) Channel(name string, private bool, members ...*Client) string {
	c.t.Helper()
	ids := []string{}
	for _, m := range members {
		ids = append(ids, string(m.ID))
	}
	out := c.Must(201, "POST", "/api/v1/channels", map[string]any{"name": name, "private": private, "account_ids": ids})
	id := out["conversation"].(map[string]any)["id"].(string)
	// The people a channel is made with are invited (ruling R103); the
	// ones a test names accept.
	for _, m := range members {
		m.AcceptInvitation(id)
	}
	return id
}

// Post posts a message as this account and returns the message.
func (c *Client) Post(conversation, text string) map[string]any {
	c.t.Helper()
	out := c.Must(201, "POST", "/api/v1/conversations/"+conversation+"/messages", map[string]any{"text": text})
	return out["message"].(map[string]any)
}

// Stream is one live connection.
type Stream struct {
	t    testing.TB
	conn *websocket.Conn
	// Hello is the first frame the server sent.
	Hello map[string]any
}

// Dial opens the WebSocket without sending anything, and reads `hello`.
func (c *Client) Dial() (*Stream, *http.Response, error) {
	h := http.Header{}
	if c.Bearer != "" {
		h.Set("Authorization", "Bearer "+c.Bearer)
	} else {
		if !c.NoOrigin {
			h.Set("Origin", c.ts.Origin)
		}
		for _, ck := range c.http.Jar.Cookies(mustURL(c.ts.URL)) {
			h.Add("Cookie", ck.Name+"="+ck.Value)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// In the protobuf mode (protobuf.go) the connection is the one an app
	// opens: the protobuf subprotocol, with compression.
	opts := &websocket.DialOptions{HTTPHeader: h, Subprotocols: []string{live.Subprotocol}}
	if wireProtobuf {
		opts.Subprotocols = []string{wirepb.SubprotocolFor(wirepb.SchemaVersion)}
		opts.CompressionMode = websocket.CompressionNoContextTakeover
	}
	conn, resp, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(c.ts.URL, "http")+"/api/v1/events", opts)
	if err != nil {
		return nil, resp, err
	}
	conn.SetReadLimit(1 << 20)
	s := &Stream{t: c.t, conn: conn}
	c.t.Cleanup(func() { conn.CloseNow() })
	hello, ok := s.Next(5 * time.Second)
	if !ok || hello["type"] != "hello" {
		return nil, resp, fmt.Errorf("expected hello, got %v", hello)
	}
	s.Hello = hello
	return s, resp, nil
}

// DialRefused opens the WebSocket expecting the server to close it before
// `hello`, and returns the close code and reason; 0 if it was sent a frame
// or the upgrade failed. It waits longer than Dial does: a server short of
// memory may keep a connection waiting some seconds before it refuses it.
func (c *Client) DialRefused() (code int, reason string) {
	h := http.Header{}
	if c.Bearer != "" {
		h.Set("Authorization", "Bearer "+c.Bearer)
	} else {
		if !c.NoOrigin {
			h.Set("Origin", c.ts.Origin)
		}
		for _, ck := range c.http.Jar.Cookies(mustURL(c.ts.URL)) {
			h.Add("Cookie", ck.Name+"="+ck.Value)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	opts := &websocket.DialOptions{HTTPHeader: h, Subprotocols: []string{live.Subprotocol}}
	if wireProtobuf {
		opts.Subprotocols = []string{wirepb.SubprotocolFor(wirepb.SchemaVersion)}
		opts.CompressionMode = websocket.CompressionNoContextTakeover
	}
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(c.ts.URL, "http")+"/api/v1/events", opts)
	if err != nil {
		return 0, ""
	}
	defer conn.CloseNow()
	_, _, err = conn.Read(ctx)
	var ce websocket.CloseError
	if errors.As(err, &ce) {
		return int(ce.Code), ce.Reason
	}
	return 0, ""
}

// Events opens a live connection, resumes with the positions given (nil for
// a client that holds nothing) and returns once `ready` has arrived. Frames
// that arrived before `ready` are returned too.
func (c *Client) Events(positions map[string]int64) (*Stream, []map[string]any) {
	c.t.Helper()
	s, _, err := c.Dial()
	if err != nil {
		c.t.Fatalf("opening the live connection: %v", err)
	}
	if positions == nil {
		positions = map[string]int64{}
	}
	s.Send(map[string]any{"type": "resume", "positions": positions})
	var before []map[string]any
	for {
		f, ok := s.Next(5 * time.Second)
		if !ok {
			c.t.Fatalf("no ready frame; got %v", before)
		}
		before = append(before, f)
		if f["type"] == "ready" {
			return s, before
		}
	}
}

// Send sends one frame.
func (s *Stream) Send(v any) {
	s.t.Helper()
	b, _ := json.Marshal(v)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	typ := websocket.MessageText
	if wireProtobuf {
		// A frame the schema cannot hold (a test's deliberately wrong
		// one) travels as JSON inside its envelope.
		b, _, _ = wirepb.EncodeFrameJSON(b)
		typ = websocket.MessageBinary
	}
	if err := s.conn.Write(ctx, typ, b); err != nil {
		s.t.Fatalf("sending a frame: %v", err)
	}
}

// Next reads the next frame, or reports false if none arrives in time or
// the connection has closed.
func (s *Stream) Next(wait time.Duration) (map[string]any, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), wait)
	defer cancel()
	_, data, err := s.conn.Read(ctx)
	if err != nil {
		return nil, false
	}
	if wireProtobuf {
		if data, err = wirepb.DecodeFrameJSON(data); err != nil {
			s.t.Errorf("a frame is not an Envelope: %v", err)
			return nil, false
		}
	}
	var f map[string]any
	if json.Unmarshal(data, &f) != nil {
		return nil, false
	}
	checkFrame(s.t, data) // protobuf.go
	return f, true
}

// Expect reads frames until one of the wanted type arrives and returns it,
// failing the test if none does within half of Patience. Other frames are
// skipped.
func (s *Stream) Expect(frameType string) map[string]any {
	s.t.Helper()
	deadline := time.Now().Add(Patience / 2)
	for time.Now().Before(deadline) {
		f, ok := s.Next(time.Until(deadline))
		if !ok {
			break
		}
		if f["type"] == frameType {
			return f
		}
	}
	s.t.Fatalf("no %s frame arrived", frameType)
	return nil
}

// ExpectNone fails the test if any frame other than a pong or a
// presence.changed arrives within the wait. Reading with a deadline ends the connection, so it is the last
// thing a test does with a stream.
func (s *Stream) ExpectNone(wait time.Duration) {
	s.t.Helper()
	deadline := time.Now().Add(wait)
	for time.Now().Before(deadline) {
		f, ok := s.Next(time.Until(deadline))
		if !ok {
			return
		}
		// Presence is in the air on any server where people connect: it
		// is about accounts, not about what the test is watching for.
		if f["type"] != "pong" && f["type"] != "presence.changed" && f["type"] != "presence.batch" {
			s.t.Fatalf("a frame arrived that should not have: %v", f)
		}
	}
}

// CloseCode waits for the server to close the connection and returns the
// close code.
func (s *Stream) CloseCode(wait time.Duration) int {
	ctx, cancel := context.WithTimeout(context.Background(), wait)
	defer cancel()
	for {
		_, _, err := s.conn.Read(ctx)
		if err != nil {
			return int(websocket.CloseStatus(err))
		}
	}
}

// logWriter sends the server's log to the test's.
type logWriter struct{ t testing.TB }

func (w logWriter) Write(p []byte) (int, error) {
	w.t.Log("server: " + strings.TrimSpace(string(p)))
	return len(p), nil
}

// linkClock is the system's clock moved by a skew a test may change.
func linkClock(skew *atomic.Int64) func() model.Time {
	if skew == nil {
		return nil
	}
	return func() model.Time { return model.Now() + model.Time(skew.Load()) }
}

// logLevel is what a test's servers log: errors, or with CHURR_TEST_LOG set
// to warn or debug, more, for looking into a test that fails.
func logLevel() slog.Level {
	switch os.Getenv("CHURR_TEST_LOG") {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	}
	return slog.LevelError
}

// Restart stops the server and starts it again on the same data and the
// same address, as a restart of the process does: everything it held in
// memory is gone, and what it does next it does from what it wrote down.
// Clients made before keep working: their sessions are in the database.
func (ts *TestServer) Restart() {
	ts.t.Helper()
	ts.stop()
	var ln net.Listener
	var err error
	for try := 0; try < 50; try++ {
		if ln, err = net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", ts.port)); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		ts.t.Fatalf("the server's port could not be taken again: %v", err)
	}
	opts := ts.opts
	opts.Listener = ln
	srv, err := server.NewWith(ts.cfg, opts)
	if err != nil {
		ts.t.Fatalf("the server did not start again: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		srv.Run(ctx)
		close(done)
	}()
	ts.Server = srv
	ts.stop = func() {
		cancel()
		<-done
	}
}
