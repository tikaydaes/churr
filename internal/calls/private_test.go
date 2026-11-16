package calls

import (
	"bytes"
	"context"
	"log/slog"
	"net/netip"
	"strings"
	"testing"
)

func addrs(t *testing.T, list ...string) []netip.Addr {
	t.Helper()
	out := make([]netip.Addr, len(list))
	for i, s := range list {
		out[i] = netip.MustParseAddr(s)
	}
	return out
}

func TestPrivateOnly(t *testing.T) {
	for _, c := range []struct {
		addrs []string
		want  bool
	}{
		{nil, false},
		{[]string{"203.0.113.7"}, false},
		{[]string{"192.168.1.20"}, true},
		{[]string{"10.240.0.5", "fd00::5"}, true},
		{[]string{"100.100.1.1"}, true}, // shared address space
		{[]string{"192.168.1.20", "203.0.113.7"}, false},
		{[]string{"2001:db8::1"}, false},
		{[]string{"127.0.0.1"}, false}, // a trial on one computer
		{[]string{"127.0.0.1", "192.168.1.20"}, true},
	} {
		if got := privateOnly(addrs(t, c.addrs...)); got != c.want {
			t.Errorf("privateOnly(%v) = %v, want %v", c.addrs, got, c.want)
		}
	}
}

// A media address of private addresses only is logged once, and again only
// if it changes; the Overview has a sentence for it while it lasts. The
// host name's lookup is the one the server makes anyway, here a stand-in.
func TestPrivateMediaAddressWarned(t *testing.T) {
	var logged bytes.Buffer
	answer := addrs(t, "192.168.1.20")
	m := &media{cfg: Config{Hostname: "chat.example.com"}, port: 24877, log: slog.New(slog.NewTextHandler(&logged, nil))}
	m.lookup = func(context.Context, string) ([]netip.Addr, error) { return answer, nil }
	ctx := context.Background()

	m.addresses(ctx)
	m.addrs = nil // look again
	m.addresses(ctx)
	if n := strings.Count(logged.String(), "only private addresses"); n != 1 {
		t.Fatalf("logged %d times, want once:\n%s", n, logged.String())
	}
	if !strings.Contains(logged.String(), "192.168.1.20") || !strings.Contains(logged.String(), "CHURR_MEDIA_ADDRESS") {
		t.Errorf("the line does not name the address and the setting:\n%s", logged.String())
	}
	if w := m.privateWarning(ctx); !strings.Contains(w, "192.168.1.20") || !strings.Contains(w, "24877") {
		t.Errorf("the Overview warning is %q", w)
	}

	// The public address added: no warning.
	logged.Reset()
	answer = addrs(t, "192.168.1.20", "203.0.113.7")
	m.addrs = nil
	if w := m.privateWarning(ctx); w != "" || logged.Len() != 0 {
		t.Errorf("with a public address: warning %q, logged %q", w, logged.String())
	}
	// Another private address is warned about again.
	answer = addrs(t, "10.0.0.5")
	m.addrs = nil
	m.addresses(ctx)
	if !strings.Contains(logged.String(), "10.0.0.5") {
		t.Errorf("a changed private address was not logged:\n%s", logged.String())
	}
}
