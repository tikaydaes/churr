package hub

import (
	"bytes"
	"database/sql"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/config"
	"github.com/tikaydaes/churr/internal/link/wire"
)

// The hub with servers on it is tested from internal/link, where real
// servers dial it (TestShareThroughHub, TestHubCannotRead and the rest).
// These are the operator's commands by themselves.

func newHub(t *testing.T) *Hub {
	t.Helper()
	h, err := New(config.Hub{Common: config.Common{Data: t.TempDir(), Hostname: "hub.example", LogLevel: "error"}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { h.Close() })
	return h
}

func TestOperatorCommands(t *testing.T) {
	h := newHub(t)
	if h.Name() != "hub.example" || len(strings.Split(h.Fingerprint(), "-")) != 8 {
		t.Fatalf("name %q fingerprint %q", h.Name(), h.Fingerprint())
	}
	// An invitation names the hub, its address and its fingerprint, and
	// keeps only the hash of its secret.
	text, err := h.Invite("A.Example", 0)
	if err != nil {
		t.Fatal(err)
	}
	inv, err := wire.ParseInvitation(text)
	if err != nil {
		t.Fatal(err)
	}
	if inv.Kind != wire.KindHub || inv.Name != "hub.example" || inv.URL != "wss://hub.example/link/v1" || inv.For == nil || *inv.For != "a.example" ||
		wire.FingerprintText(inv.FP) != h.Fingerprint() {
		t.Fatalf("the invitation: %+v", inv)
	}
	if d := inv.ExpiresAt.Sub(h.now()); d < 6*24*time.Hour || d > 7*24*time.Hour+time.Minute {
		t.Fatalf("an invitation lasts seven days unless told otherwise, not %v", d)
	}
	stored, err := os.ReadFile(filepath.Join(h.cfg.Data, "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	wal, _ := os.ReadFile(filepath.Join(h.cfg.Data, "hub.db-wal"))
	if bytes.Contains(append(stored, wal...), inv.Secret) {
		t.Fatal("the hub kept the invitation's secret")
	}
	if _, err := h.Invite("", time.Hour); err != nil {
		t.Fatal(err)
	}
	list, err := h.Invites()
	if err != nil || len(list) != 2 || list[0].For != "a.example" || list[1].For != "" {
		t.Fatalf("invites: %+v %v", list, err)
	}
	if err := h.RevokeInvite(list[1].ID); err != nil {
		t.Fatal(err)
	}
	if err := h.RevokeInvite(list[1].ID); err == nil {
		t.Fatal("an invitation was revoked twice")
	}
	if list, _ = h.Invites(); len(list) != 1 {
		t.Fatalf("after revoking: %+v", list)
	}

	// Limits: the defaults of the protocol, settable as defaults and per
	// server.
	l, err := h.Limits("")
	if err != nil || l.Submits != 3000 || l.BlobBytes != 1<<30 || l.Streams != 1000 {
		t.Fatalf("default limits: %+v %v", l, err)
	}
	if _, err := h.Limits("nobody.example"); err == nil {
		t.Fatal("limits of a server that is not on the hub")
	}
	// -server-rate is that default, in envelopes a second, until a
	// default is set with `limits`.
	h.cfg.ServerRate = 200
	if l, _ = h.Limits(""); l.Submits != 12000 {
		t.Fatalf("with -server-rate 200 a server may submit %d envelopes a minute, want 12000", l.Submits)
	}
	if err := h.SetLimits("", 300, 0, 0); err != nil {
		t.Fatal(err)
	}
	// A server put there by hand: joining is the handshake's business.
	if err := h.tx(func(tx *sql.Tx) error {
		_, err := tx.Exec(`INSERT INTO servers (name, sign_key, box_key, box_key_sig, joined_at, state) VALUES ('a.example', x'01', x'02', x'03', 1, 'active')`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := h.SetLimits("a.example", 0, 0, 5); err != nil {
		t.Fatal(err)
	}
	if l, _ = h.Limits("a.example"); l.Submits != 300 || l.Streams != 5 {
		t.Fatalf("a server's limits: %+v", l)
	}
	servers, err := h.Servers()
	if err != nil || len(servers) != 1 || servers[0].Name != "a.example" || servers[0].Connected {
		t.Fatalf("servers: %+v %v", servers, err)
	}
	// Removing a server closes the streams it owned and changes the roster.
	h.db.Exec(`INSERT INTO streams (id, owner, version, next_seq, state, created_at) VALUES ('s1', 'a.example', 1, 1, 'open', 1)`)
	before := h.meta("roster_version")
	if err := h.Remove("A.example"); err != nil {
		t.Fatal(err)
	}
	if err := h.Remove("a.example"); err == nil {
		t.Fatal("a server was removed twice")
	}
	if streams, _ := h.Streams(); len(streams) != 0 || h.meta("roster_version") != before+1 {
		t.Fatalf("after removing: streams %v, roster %d then %d", streams, before, h.meta("roster_version"))
	}
	if servers, _ = h.Servers(); len(servers) != 0 {
		t.Fatalf("a removed server is still listed: %+v", servers)
	}
	// A removed server is refused from then on, whatever it presents.
	err = h.authorize(wire.Frame{Name: "a.example", SignKey: []byte{1}, BoxKey: []byte{2}}, nil, make([]byte, 32))
	if wire.AsError(err).Code != wire.EBlocked {
		t.Fatalf("a removed server came back: %v", err)
	}

	// The status, as text and as a page: totals only.
	status := h.Status().Text()
	if !strings.Contains(status, "Servers joined: 0") || !strings.Contains(status, h.Fingerprint()) {
		t.Fatalf("status: %s", status)
	}
	page := httptest.NewRecorder()
	h.StatusHandler().ServeHTTP(page, httptest.NewRequest("GET", "/", nil))
	if page.Code != 200 || strings.Contains(page.Body.String(), "<script") || strings.Contains(page.Body.String(), "a.example") {
		t.Fatalf("status page: %d %s", page.Code, page.Body.String())
	}
	other := httptest.NewRecorder()
	h.StatusHandler().ServeHTTP(other, httptest.NewRequest("GET", "/servers", nil))
	if other.Code != 404 {
		t.Fatalf("the status page serves one page, not %d for another", other.Code)
	}
}

// A hub publishes its name and signing key, and no box key: nothing is ever
// encrypted to a hub.
func TestHubWellKnown(t *testing.T) {
	h := newHub(t)
	rec := httptest.NewRecorder()
	h.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/.well-known/churr", nil))
	body := rec.Body.String()
	if rec.Code != 200 || !strings.Contains(body, `"kind":"hub"`) || strings.Contains(body, "box_key") || rec.Header().Get("Cache-Control") != "max-age=3600" {
		t.Fatalf("%d %s", rec.Code, body)
	}
}
