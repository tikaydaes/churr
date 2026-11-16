package api

import (
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"sort"
	"strings"
	"sync"
)

// untrustedProxies remembers the networks a forwarding header came from
// without being believed: a peer on a private or loopback address that is
// not in the trusted proxies. That is nearly always the operator's own
// reverse proxy on a network the setting does not cover, and then every
// client looks like the proxy. Each network is logged once and shown on the
// admin Overview; nothing is trusted because of it.
type untrustedProxies struct {
	mu   sync.Mutex
	seen map[netip.Prefix]netip.Addr
}

// untrustedKept is how many networks are remembered. Past it the warning is
// already on the Overview and in the log, and more of the same adds nothing.
const untrustedKept = 16

// clientAddr is the address limits are counted against: the connection's,
// or, only behind a trusted proxy, the client its forwarding header names.
// The header is read from the right, past every trusted proxy, so a chain of
// trusted proxies (a front proxy, then one on the host) gives the client and
// not the front proxy.
func (a *API) clientAddr(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if len(a.TrustedProxies) == 0 {
		return host
	}
	peer, err := netip.ParseAddr(host)
	if err != nil {
		return host
	}
	peer = peer.Unmap()
	xff := r.Header.Get("X-Forwarded-For")
	if !a.trusted(peer) {
		if xff != "" && (peer.IsPrivate() || peer.IsLoopback()) {
			a.untrustedProxy(peer)
		}
		return host
	}
	if xff == "" {
		return host
	}
	parts := strings.Split(xff, ",")
	for i := len(parts) - 1; i >= 0; i-- {
		hop := strings.TrimSpace(parts[i])
		addr, err := netip.ParseAddr(hop)
		if i == 0 || err != nil || !a.trusted(addr.Unmap()) {
			return hop
		}
	}
	return host
}

// trusted reports whether an address is one of the trusted proxies.
func (a *API) trusted(addr netip.Addr) bool {
	for _, p := range a.TrustedProxies {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}

// peerNetwork is the network a peer is counted in for the warning: its /24,
// or its /64 for IPv6. A Docker network is at least that wide.
func peerNetwork(peer netip.Addr) netip.Prefix {
	bits := 24
	if peer.Is6() {
		bits = 64
	}
	p, _ := peer.Prefix(bits)
	return p
}

// untrustedProxy notes a peer whose forwarding header was ignored, and logs
// it the first time its network is seen.
func (a *API) untrustedProxy(peer netip.Addr) {
	network := peerNetwork(peer)
	u := &a.untrusted
	u.mu.Lock()
	if _, ok := u.seen[network]; ok || len(u.seen) >= untrustedKept {
		u.mu.Unlock()
		return
	}
	if u.seen == nil {
		u.seen = map[netip.Prefix]netip.Addr{}
	}
	u.seen[network] = peer
	u.mu.Unlock()
	if a.Log != nil {
		a.Log.Warn("a forwarding header came from an address that is not a trusted proxy and was ignored: "+
			"every client behind it looks like that address. If it is your reverse proxy, add its network to CHURR_TRUSTED_PROXIES (-trusted-proxies)",
			"peer", peer.String(), "network", network.String(), "trusted_proxies", fmt.Sprint(a.TrustedProxies))
	}
}

// untrustedWarnings are the Overview's sentences for the peers
// untrustedProxy noted, one per network, in address order.
func (a *API) untrustedWarnings() []string {
	u := &a.untrusted
	u.mu.Lock()
	peers := make([]netip.Addr, 0, len(u.seen))
	for _, p := range u.seen {
		peers = append(peers, p)
	}
	u.mu.Unlock()
	sort.Slice(peers, func(i, j int) bool { return peers[i].Less(peers[j]) })
	out := make([]string, 0, len(peers))
	for _, p := range peers {
		out = append(out, fmt.Sprintf("Requests from %s carry a forwarding header, but %s is not a trusted proxy, so every client behind it looks like %s. "+
			"If it is your reverse proxy, add its network, such as %s, to CHURR_TRUSTED_PROXIES (-trusted-proxies) and restart.", p, p, p, peerNetwork(p)))
	}
	return out
}
