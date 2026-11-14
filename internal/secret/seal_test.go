package secret

import (
	"bytes"
	"errors"
	"path/filepath"
	"testing"
)

func TestSealRoundTrip(t *testing.T) {
	k := open(t)
	for _, plain := range [][]byte{nil, {}, []byte("x"), []byte("JBSWY3DPEHPK3PXP"), bytes.Repeat([]byte{0xAB}, 5000)} {
		sealed, err := k.Seal(plain, "totp", "secret", "0193a6f2-7c1e-7b3a-9f0e-2a4d5c6b7e8f")
		if err != nil {
			t.Fatalf("Seal: %v", err)
		}
		// Twelve bytes of nonce in front, sixteen of tag behind.
		if want := 12 + len(plain) + 16; len(sealed) != want {
			t.Errorf("sealed %d bytes into %d, want %d", len(plain), len(sealed), want)
		}
		got, err := k.Unseal(sealed, "totp", "secret", "0193a6f2-7c1e-7b3a-9f0e-2a4d5c6b7e8f")
		if err != nil {
			t.Fatalf("Unseal: %v", err)
		}
		if !bytes.Equal(got, plain) {
			t.Errorf("round trip changed the value: %q became %q", plain, got)
		}
	}
}

func TestSealHidesThePlaintextAndNeverRepeats(t *testing.T) {
	k := open(t)
	plain := []byte("a client secret that must not be readable")
	a, _ := k.Seal(plain, "t", "c", "r")
	b, _ := k.Seal(plain, "t", "c", "r")
	if bytes.Contains(a, plain) {
		t.Error("the sealed value contains the plaintext")
	}
	if bytes.Equal(a, b) {
		t.Error("sealing the same value twice gave the same bytes")
	}
	if bytes.Equal(a[:12], b[:12]) {
		t.Error("two seals used the same nonce")
	}
}

func TestUnsealRefusesAnotherPlace(t *testing.T) {
	k := open(t)
	sealed, _ := k.Seal([]byte("value"), "oidc_providers", "secret", "row-1")
	places := [][3]string{
		{"saml_providers", "secret", "row-1"}, // another table
		{"oidc_providers", "other", "row-1"},  // another column
		{"oidc_providers", "secret", "row-2"}, // another row
		{"", "", ""},
		// The same characters split differently between the three names.
		{"oidc_providerss", "ecret", "row-1"},
		{"oidc_providers", "secretr", "ow-1"},
		{"oidc_providerssecret", "", "row-1"},
		{"oidc_providers", "", "secretrow-1"},
	}
	for _, p := range places {
		if _, err := k.Unseal(sealed, p[0], p[1], p[2]); !errors.Is(err, ErrUnseal) {
			t.Errorf("opened under %q: err = %v, want ErrUnseal", p, err)
		}
	}
	if _, err := k.Unseal(sealed, "oidc_providers", "secret", "row-1"); err != nil {
		t.Errorf("did not open in its own place: %v", err)
	}
}

func TestUnsealRefusesTampering(t *testing.T) {
	k := open(t)
	sealed, _ := k.Seal([]byte("value that matters"), "t", "c", "r")
	// Every single bit, in the nonce, the ciphertext and the tag alike.
	for i := range sealed {
		for bit := 0; bit < 8; bit++ {
			bad := bytes.Clone(sealed)
			bad[i] ^= 1 << bit
			if _, err := k.Unseal(bad, "t", "c", "r"); !errors.Is(err, ErrUnseal) {
				t.Fatalf("a flipped bit in byte %d went unnoticed: err = %v", i, err)
			}
		}
	}
	for _, bad := range [][]byte{nil, {}, sealed[:5], sealed[:12], sealed[:27], sealed[:len(sealed)-1], append(bytes.Clone(sealed), 0)} {
		if _, err := k.Unseal(bad, "t", "c", "r"); !errors.Is(err, ErrUnseal) {
			t.Errorf("a value of %d bytes was accepted: err = %v", len(bad), err)
		}
	}
}

func TestUnsealRefusesAnotherServersKey(t *testing.T) {
	a, b := open(t), open(t)
	sealed, _ := a.Seal([]byte("value"), "t", "c", "r")
	if _, err := b.Unseal(sealed, "t", "c", "r"); !errors.Is(err, ErrUnseal) {
		t.Errorf("another seal key opened it: err = %v", err)
	}
}

func TestMACIsStable(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "keys")
	a, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	first := a.MAC("wake-route", []byte("device-1"))
	if len(first) != 32 {
		t.Fatalf("MAC is %d bytes, want 32", len(first))
	}
	if !bytes.Equal(first, a.MAC("wake-route", []byte("device-1"))) {
		t.Error("the same input gave two different MACs")
	}
	// The same after a restart: a route handed to a relay must keep working.
	b, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, b.MAC("wake-route", []byte("device-1"))) {
		t.Error("the MAC changed after reopening the keys")
	}
}

func TestMACSeparation(t *testing.T) {
	k := open(t)
	base := k.MAC("wake-collapse", []byte("device"), []byte("conversation"))
	different := map[string][]byte{
		"another label":         k.MAC("wake-route", []byte("device"), []byte("conversation")),
		"label moved into part": k.MAC("wake-", []byte("collapsedevice"), []byte("conversation")),
		"another part":          k.MAC("wake-collapse", []byte("device"), []byte("conversatiom")),
		"parts split elsewhere": k.MAC("wake-collapse", []byte("deviceconv"), []byte("ersation")),
		"parts joined":          k.MAC("wake-collapse", []byte("deviceconversation")),
		"parts swapped":         k.MAC("wake-collapse", []byte("conversation"), []byte("device")),
		"an extra empty part":   k.MAC("wake-collapse", []byte("device"), []byte("conversation"), nil),
		"another key":           open(t).MAC("wake-collapse", []byte("device"), []byte("conversation")),
	}
	for what, mac := range different {
		if bytes.Equal(mac, base) {
			t.Errorf("%s gave the same MAC", what)
		}
	}
	if bytes.Equal(k.MAC("x"), k.MAC("x", nil)) {
		t.Error("no parts and one empty part gave the same MAC")
	}
	if bytes.Equal(k.MAC("x"), k.MAC("y")) {
		t.Error("two labels with no parts gave the same MAC")
	}
}

func TestMACDoesNotUseTheSealKeyDirectly(t *testing.T) {
	// If the MAC key were seal.key itself, a MAC over chosen bytes would be a
	// value under the very key that seals columns. It must be a derived key.
	k := open(t)
	if bytes.Equal(k.MAC("x", []byte("p")), macWith(k.seal, []byte("p"))) {
		t.Error("the MAC is keyed with seal.key itself")
	}
}
