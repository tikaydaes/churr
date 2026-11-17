package ratelimit

import "net/netip"

// AddressKey is the key a limit counted per address uses for addr: the
// address itself for IPv4, and its /64 for IPv6, since whoever has one
// address of a /64 usually has all of them (docs/AUTH.md section 14). A key
// that is not an address is returned as it is.
func AddressKey(addr string) string {
	a, err := netip.ParseAddr(addr)
	if err != nil {
		return addr
	}
	a = a.Unmap()
	if !a.Is6() {
		return a.String()
	}
	p, err := a.Prefix(64)
	if err != nil {
		return addr
	}
	return p.String()
}
