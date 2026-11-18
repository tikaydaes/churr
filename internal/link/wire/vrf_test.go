package wire

import (
	"context"
	"testing"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/secret"
)

// noVRF is a party whose keys show no VRF key history.
type noVRF struct{ *secret.Keys }

func (noVRF) VRFHistory() []secret.VRFKey { return nil }

// The signed identity carries the VRF key history: each side of a
// handshake holds the other's, signed by its signing key, and a hello
// without a sound one is refused.
func TestHelloCarriesTheVRFKeyHistory(t *testing.T) {
	a, b := ident(t, "a.example", KindServer), ident(t, "b.example", KindServer)
	got := make(chan *Conn, 1)
	url := listen(t, a, func(Frame, []byte, []byte) error { return nil }, got)
	c, err := Dial(context.Background(), DialOptions{URL: url, Identity: b})
	if err != nil {
		t.Fatal(err)
	}
	server := <-got
	for _, side := range []struct {
		seen Frame
		of   Identity
	}{{c.Remote, a}, {server.Remote, b}} {
		k := side.seen.VRFKeys
		if len(k) != 1 || string(k[0].Key) != string(side.of.Keys.VRFHistory()[0].Key) || k[0].Began != side.of.Keys.VRFHistory()[0].Began {
			t.Fatalf("%s's hello carries %+v", side.of.Name, k)
		}
		if err := CheckVRFKeys(side.of.Name, side.of.Keys.SignPublic(), k); err != nil {
			t.Fatal(err)
		}
	}
	c.Close()

	bare := b
	bare.Keys = noVRF{b.Keys.(*secret.Keys)}
	if _, err := Dial(context.Background(), DialOptions{URL: url, Identity: bare}); AsError(err).Code != EAuthFailed {
		t.Fatalf("a hello with no VRF key: %v", err)
	}
}

func TestCheckVRFKeys(t *testing.T) {
	id := ident(t, "a.example", KindServer)
	good := id.VRFKeys()
	if err := CheckVRFKeys("a.example", id.Keys.SignPublic(), good); err != nil {
		t.Fatal(err)
	}
	other := ident(t, "b.example", KindServer)
	cases := map[string]func([]VRFKey) ([]VRFKey, string, []byte){
		"empty":          func([]VRFKey) ([]VRFKey, string, []byte) { return nil, "a.example", id.Keys.SignPublic() },
		"another name":   func(k []VRFKey) ([]VRFKey, string, []byte) { return k, "b.example", id.Keys.SignPublic() },
		"another signer": func(k []VRFKey) ([]VRFKey, string, []byte) { return k, "a.example", other.Keys.SignPublic() },
		"a changed key": func(k []VRFKey) ([]VRFKey, string, []byte) {
			k[0].Key[5] ^= 1
			return k, "a.example", id.Keys.SignPublic()
		},
		"a changed start": func(k []VRFKey) ([]VRFKey, string, []byte) { k[0].Began++; return k, "a.example", id.Keys.SignPublic() },
		"not a point": func(k []VRFKey) ([]VRFKey, string, []byte) {
			k[0].Key[0] = 4
			return k, "a.example", id.Keys.SignPublic()
		},
		"out of order": func(k []VRFKey) ([]VRFKey, string, []byte) {
			return append(k, k[0]), "a.example", id.Keys.SignPublic()
		},
	}
	for what, mk := range cases {
		k, name, key := mk(id.VRFKeys())
		if err := CheckVRFKeys(name, key, k); err == nil {
			t.Errorf("%s was accepted", what)
		}
	}
}

func TestVRFKeysExtend(t *testing.T) {
	k := func(b byte, began model.Time) VRFKey { return VRFKey{Key: Bytes{2, b}, Began: began, Sig: Bytes{b}} }
	a, b, c := k(1, 1000), k(2, 2000), k(3, 2000)
	for _, tc := range []struct {
		what          string
		pinned, shown []VRFKey
		added         int
		ok            bool
	}{
		{"nothing pinned", nil, []VRFKey{a}, 1, true},
		{"the same", []VRFKey{a}, []VRFKey{a}, 0, true},
		{"one appended", []VRFKey{a}, []VRFKey{a, b}, 1, true},
		{"the last replaced", []VRFKey{a, b}, []VRFKey{a, c}, 0, false},
		{"the first replaced", []VRFKey{a}, []VRFKey{c}, 0, false},
		{"one taken away", []VRFKey{a, b}, []VRFKey{a}, 0, false},
		{"a start changed", []VRFKey{a}, []VRFKey{{Key: a.Key, Began: a.Began + 1, Sig: a.Sig}}, 0, false},
	} {
		added, ok := VRFKeysExtend(tc.pinned, tc.shown)
		if added != tc.added || ok != tc.ok {
			t.Errorf("%s: %d %v, want %d %v", tc.what, added, ok, tc.added, tc.ok)
		}
	}
}
