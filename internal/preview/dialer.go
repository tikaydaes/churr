// Package preview fetches link previews: it reads a page somebody linked
// to and makes a card out of its title, description, site name and image
// (docs/FEATURES.md section 23).
//
// Fetching an address a member typed is the most dangerous thing the
// server does on its own initiative, so every connection this package
// makes goes through the guarded dialer in this file, and the limits of
// section 23.3 are constants in fetch.go, in one place.
package preview

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"time"
)

// ErrRefused is returned for an address the guarded dialer will not connect
// to. The reason is in the error's text, for the operator's log; members
// are never shown it.
var ErrRefused = errors.New("preview: address refused")

// blocked are the ranges that are not the public internet and that the
// standard library's own tests (loopback, private, link-local, multicast,
// unspecified) do not cover.
var blocked = func() []netip.Prefix {
	var out []netip.Prefix
	for _, s := range []string{
		"0.0.0.0/8",       // "this network"
		"100.64.0.0/10",   // carrier-grade NAT
		"192.0.0.0/24",    // protocol assignments
		"192.0.2.0/24",    // documentation
		"198.18.0.0/15",   // benchmarking
		"198.51.100.0/24", // documentation
		"203.0.113.0/24",  // documentation
		"240.0.0.0/4",     // reserved, and the broadcast address
		"64:ff9b::/96",    // NAT64: an IPv4 address in disguise
		"64:ff9b:1::/48",  // local NAT64
		"2001:db8::/32",   // documentation
		"fec0::/10",       // site-local, deprecated but still routed in places
		"2002::/16",       // 6to4: an IPv4 address in disguise
	} {
		out = append(out, netip.MustParsePrefix(s))
	}
	return out
}()

// Public reports whether an address is on the public internet: not
// loopback, private, link-local, carrier-grade NAT, unique-local or
// site-local, multicast or unspecified, nor an IPv4-mapped form of any of
// these.
func Public(a netip.Addr) bool {
	if !a.IsValid() {
		return false
	}
	// ::ffff:10.0.0.1 is 10.0.0.1.
	a = a.Unmap().WithZone("")
	if a.IsLoopback() || a.IsPrivate() || a.IsLinkLocalUnicast() || a.IsLinkLocalMulticast() || a.IsInterfaceLocalMulticast() ||
		a.IsMulticast() || a.IsUnspecified() {
		return false
	}
	for _, p := range blocked {
		if p.Contains(a) {
			return false
		}
	}
	return true
}

// Dialer connects only to public addresses. It resolves the name itself,
// refuses unless every address the name resolves to is public, and then
// connects to an address it checked, never resolving again, so a name
// cannot change between the check and the use. Because it is the
// transport's dialer, the same check is made for the first request and
// again for every redirect.
type Dialer struct {
	// Own are the server's own addresses, refused like private ones.
	Own []netip.Addr
	// Ports are the ports that may be dialled; nil means 80 and 443.
	Ports []string
	// Resolve looks a name up; nil means the system resolver.
	Resolve func(ctx context.Context, host string) ([]netip.Addr, error)
	// Connect opens the connection to a checked address; nil means a
	// plain TCP connection. Tests replace it to reach a local listener
	// under a public address.
	Connect func(ctx context.Context, network, address string) (net.Conn, error)
}

// SafeDialer returns the guarded dialer with the server's own interface
// addresses refused. preview uses it for pages and images; push uses it for
// UnifiedPush endpoints.
func SafeDialer() *Dialer {
	d := &Dialer{}
	if addrs, err := net.InterfaceAddrs(); err == nil {
		for _, a := range addrs {
			if ipn, ok := a.(*net.IPNet); ok {
				if ip, ok := netip.AddrFromSlice(ipn.IP); ok {
					d.Own = append(d.Own, ip.Unmap())
				}
			}
		}
	}
	return d
}

// allowed reports whether one resolved address may be connected to.
func (d *Dialer) allowed(a netip.Addr) bool {
	if !Public(a) {
		return false
	}
	a = a.Unmap().WithZone("")
	for _, own := range d.Own {
		if own == a {
			return false
		}
	}
	return true
}

// Check resolves a host and returns the addresses it may be reached at, or
// ErrRefused if any address it resolves to is not public.
func (d *Dialer) Check(ctx context.Context, host string) ([]netip.Addr, error) {
	var addrs []netip.Addr
	if ip, err := netip.ParseAddr(host); err == nil {
		addrs = []netip.Addr{ip}
	} else {
		resolve := d.Resolve
		if resolve == nil {
			resolve = func(ctx context.Context, host string) ([]netip.Addr, error) {
				return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
			}
		}
		if addrs, err = resolve(ctx, host); err != nil {
			return nil, fmt.Errorf("preview: looking up %s: %w", host, err)
		}
	}
	if len(addrs) == 0 {
		return nil, fmt.Errorf("%w: %s has no address", ErrRefused, host)
	}
	// Every address, not just the one that would be used: a name that
	// answers with one public and one private address is up to something.
	for _, a := range addrs {
		if !d.allowed(a) {
			return nil, fmt.Errorf("%w: %s resolves to %s, which is not a public address", ErrRefused, host, a)
		}
	}
	return addrs, nil
}

// DialContext is the DialContext of an http.Transport.
func (d *Dialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	ports := d.Ports
	if ports == nil {
		ports = []string{"80", "443"}
	}
	ok := false
	for _, p := range ports {
		ok = ok || p == port
	}
	if !ok {
		return nil, fmt.Errorf("%w: port %s", ErrRefused, port)
	}
	addrs, err := d.Check(ctx, host)
	if err != nil {
		return nil, err
	}
	connect := d.Connect
	if connect == nil {
		connect = (&net.Dialer{Timeout: 5 * time.Second}).DialContext
	}
	var last error
	for _, a := range addrs {
		// The address that was checked, as a literal: nothing is looked
		// up a second time.
		conn, err := connect(ctx, network, net.JoinHostPort(a.Unmap().String(), port))
		if err == nil {
			return conn, nil
		}
		last = err
	}
	return nil, last
}
