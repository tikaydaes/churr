package api

import (
	"bytes"
	"log/slog"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
)

// A forwarding header is believed from a trusted proxy only. From any other
// private or loopback peer it is ignored, and that is logged once for the
// peer's network and shown as an Overview warning; from a public peer it is
// ignored without a word, as anyone on the internet may send one.
func TestUntrustedProxyWarning(t *testing.T) {
	var logged bytes.Buffer
	a := &API{Deps: Deps{
		Log:            slog.New(slog.NewTextHandler(&logged, nil)),
		TrustedProxies: []netip.Prefix{netip.MustParsePrefix("172.16.0.0/12")},
	}}
	ask := func(peer, xff string) string {
		r := httptest.NewRequest("GET", "/api/v1/server", nil)
		r.RemoteAddr = peer
		if xff != "" {
			r.Header.Set("X-Forwarded-For", xff)
		}
		return a.clientAddr(r)
	}

	if got := ask("172.18.0.1:5000", "198.51.100.7"); got != "198.51.100.7" {
		t.Errorf("through a trusted proxy the client is %q, want 198.51.100.7", got)
	}
	if got := ask("172.18.0.1:5000", "198.51.100.7, 172.20.0.5"); got != "198.51.100.7" {
		t.Errorf("through two trusted proxies the client is %q, want 198.51.100.7", got)
	}
	if got := ask("172.18.0.1:5000", "192.0.2.1, 198.51.100.7, 172.20.0.5"); got != "198.51.100.7" {
		t.Errorf("an address before an untrusted hop was believed: %q", got)
	}
	if got := ask("172.18.0.1:5000", ""); got != "172.18.0.1" {
		t.Errorf("a trusted peer without the header is %q", got)
	}
	if got := ask("10.240.0.1:5000", ""); got != "10.240.0.1" || logged.Len() != 0 {
		t.Errorf("a private peer without the header: %q, logged %q", got, logged.String())
	}
	if got := ask("203.0.113.9:5000", "198.51.100.7"); got != "203.0.113.9" || logged.Len() != 0 {
		t.Errorf("a public peer with the header: %q, logged %q", got, logged.String())
	}
	if w := a.untrustedWarnings(); len(w) != 0 {
		t.Fatalf("warnings before any untrusted proxy: %q", w)
	}

	for i := 0; i < 3; i++ {
		if got := ask("10.240.0.1:5000", "198.51.100.7"); got != "10.240.0.1" {
			t.Fatalf("an untrusted private peer's header was believed: %q", got)
		}
	}
	ask("10.240.0.9:5000", "198.51.100.8") // the same /24
	ask("[::ffff:127.0.0.1]:5000", "198.51.100.9")
	if n := strings.Count(logged.String(), "not a trusted proxy"); n != 2 {
		t.Errorf("logged %d times, want once for each of two networks:\n%s", n, logged.String())
	}
	if !strings.Contains(logged.String(), "peer=10.240.0.1") || !strings.Contains(logged.String(), "CHURR_TRUSTED_PROXIES") {
		t.Errorf("the log line does not name the peer and the setting:\n%s", logged.String())
	}
	w := a.untrustedWarnings()
	if len(w) != 2 || !strings.Contains(w[0], "10.240.0.1") || !strings.Contains(w[0], "10.240.0.0/24") ||
		!strings.Contains(w[1], "127.0.0.1") || !strings.Contains(w[1], "CHURR_TRUSTED_PROXIES") {
		t.Errorf("the Overview warnings are %q", w)
	}

	// Not behind a proxy, nothing is believed and nothing is said.
	logged.Reset()
	a.TrustedProxies = nil
	a.untrusted = untrustedProxies{}
	if got := ask("10.240.0.1:5000", "198.51.100.7"); got != "10.240.0.1" || logged.Len() != 0 {
		t.Errorf("without -behind-proxy: %q, logged %q", got, logged.String())
	}
}

// The networks remembered are bounded, whatever reaches the server.
func TestUntrustedProxyBounded(t *testing.T) {
	a := &API{Deps: Deps{TrustedProxies: []netip.Prefix{netip.MustParsePrefix("127.0.0.1/8")}}}
	for i := 0; i < 200; i++ {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = netip.AddrFrom4([4]byte{10, byte(i), 0, 1}).String() + ":1"
		r.Header.Set("X-Forwarded-For", "198.51.100.7")
		a.clientAddr(r)
	}
	if n := len(a.untrustedWarnings()); n != untrustedKept {
		t.Errorf("%d networks remembered, want %d", n, untrustedKept)
	}
}
