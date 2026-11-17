package auth

import (
	"fmt"
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/ratelimit"
)

// TestLimitsCountAnIPv6NetworkAsOne holds review finding H8: every limit
// counted per address counts an IPv6 /64 as one address, since whoever has
// one address of a /64 usually has all of them. Another /64, and an IPv4
// address, are counted on their own; a key that is not an address is used
// as it is.
func TestLimitsCountAnIPv6NetworkAsOne(t *testing.T) {
	l := ratelimit.New(ratelimit.Per(3, time.Hour), 3)
	for i := 1; i <= 3; i++ {
		if err := limited(l, fmt.Sprintf("2001:db8:1:2::%x", i)); err != nil {
			t.Fatalf("step %d from the /64: %v", i, err)
		}
	}
	if limited(l, "2001:db8:1:2:ffff:ffff:ffff:ffff") == nil {
		t.Fatal("a fourth step from the same /64 was let through")
	}
	if err := limited(l, "2001:db8:1:3::1"); err != nil {
		t.Fatalf("the next /64: %v", err)
	}
	if err := limited(l, "198.51.100.7"); err != nil {
		t.Fatalf("an IPv4 address: %v", err)
	}
	// Failures counted on their own (failed, blocked) likewise.
	f := ratelimit.New(ratelimit.Per(2, time.Hour), 2)
	failed(f, "2001:db8:9:9::1")
	failed(f, "2001:db8:9:9::2")
	if blocked(f, "2001:db8:9:9::3") == nil {
		t.Fatal("failures from one /64 were counted apart")
	}
	if err := blocked(f, "ann"); err != nil {
		t.Fatalf("a key that is not an address: %v", err)
	}
}
