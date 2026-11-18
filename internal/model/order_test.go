package model

import "testing"

// An order string gives back the stamp it was made from.
func TestOrderStamp(t *testing.T) {
	o := OrderString(1794557400532, "b.example", 118)
	origin, n, ok := OrderStamp(o)
	if !ok || origin != "b.example" || n != 118 || OrderAt(o) != 1794557400532 {
		t.Fatalf("%q read back as %q %d %v", o, origin, n, ok)
	}
	if origin, n, ok := OrderStamp(LocalOrder(0, 7)); !ok || origin != "" || n != 7 {
		t.Fatalf("a local order read back as %q %d %v", origin, n, ok)
	}
	for _, bad := range []string{"", "short", "0000019ee41c2a14!x"} {
		if _, _, ok := OrderStamp(bad); ok {
			t.Errorf("%q was read as an order", bad)
		}
	}
}
