package secret

import (
	"bytes"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tikaydaes/churr/internal/ecvrf"
)

// The VRF key is made once, at first start, and the same key and the same
// start time come back on every later start.
func TestVRFKeyMadeOnceAndKeptAcrossRestarts(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "keys")
	a, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	pk := a.VRFPublic()
	if len(pk) != 33 || (pk[0] != 2 && pk[0] != 3) {
		t.Fatalf("VRFPublic is %x, want a 33-byte compressed point", pk)
	}
	sk, err := os.ReadFile(filepath.Join(dir, "vrf.key"))
	if err != nil {
		t.Fatal(err)
	}
	if derived, err := ecvrf.DerivePublicKey(sk); err != nil || !bytes.Equal(derived, pk) {
		t.Fatalf("vrf.key does not give VRFPublic: %v", err)
	}
	h := a.VRFHistory()
	if len(h) != 1 || !bytes.Equal(h[0].Key, pk) || h[0].Began.IsZero() {
		t.Fatalf("history after first start = %+v", h)
	}
	for i := 0; i < 2; i++ {
		b, err := Open(dir)
		if err != nil {
			t.Fatalf("start %d: %v", i+2, err)
		}
		hb := b.VRFHistory()
		if !bytes.Equal(b.VRFPublic(), pk) || len(hb) != 1 || hb[0].Began != h[0].Began {
			t.Fatalf("start %d: the VRF key or its start changed: %+v", i+2, hb)
		}
	}
	if bytes.Equal(open(t).VRFPublic(), pk) {
		t.Error("two fresh key folders hold the same VRF key")
	}
	// What is handed out is a copy.
	h[0].Key[0] ^= 0xff
	if !bytes.Equal(a.VRFHistory()[0].Key, pk) {
		t.Error("the history is handed out by reference")
	}
}

// Once the history names a key, a missing vrf.key is not made afresh: the
// server refuses to start and leaves the folder as it was.
func TestVRFKeyMissingIsRefusedNotReplaced(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "keys")
	if _, err := Open(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "vrf.key")); err != nil {
		t.Fatal(err)
	}
	_, err := Open(dir)
	if err == nil || !strings.Contains(err.Error(), "vrf.key") || !strings.Contains(err.Error(), "restore") {
		t.Fatalf("Open with vrf.key missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "vrf.key")); err == nil {
		t.Error("a new vrf.key was made although the history names another")
	}
}

// A vrf.key that is not the key the history ends with (a folder put
// together from two backups) is refused.
func TestVRFKeyNotTheHistorysIsRefused(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "keys")
	if _, err := Open(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "vrf.key"), newVRFKey(), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(dir); err == nil || !strings.Contains(err.Error(), "vrf.history") {
		t.Fatalf("Open with another vrf.key: %v", err)
	}
}

// A 32-byte file that is not a scalar below the group order is refused.
func TestVRFKeyOutOfRangeIsRefused(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "keys")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "vrf.key"), bytes.Repeat([]byte{0xff}, 32), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(dir); err == nil || !strings.Contains(err.Error(), "vrf.key") {
		t.Fatalf("Open with an out-of-range vrf.key: %v", err)
	}
}

// A first start interrupted after vrf.key and before vrf.history: the next
// start writes the history for the key that is there.
func TestVRFHistoryMadeForAKeyWithout(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "keys")
	a, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "vrf.history")); err != nil {
		t.Fatal(err)
	}
	b, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a.VRFPublic(), b.VRFPublic()) || len(b.VRFHistory()) != 1 {
		t.Fatalf("history = %+v", b.VRFHistory())
	}
}

// The history's shape: several keys, oldest first, the last being vrf.key's.
// This is what an operator's new key leaves behind.
func TestVRFHistoryOfSeveralKeys(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "keys")
	a, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	old := a.VRFHistory()[0]
	sk := newVRFKey()
	pk, _ := ecvrf.DerivePublicKey(sk)
	if err := os.WriteFile(filepath.Join(dir, "vrf.key"), sk, 0o600); err != nil {
		t.Fatal(err)
	}
	hist := `[{"key": "` + hex.EncodeToString(old.Key) + `", "began": "2026-01-01T00:00:00.000Z"},` +
		`{"key": "` + hex.EncodeToString(pk) + `", "began": "2026-06-01T00:00:00.000Z"}]`
	if err := os.WriteFile(filepath.Join(dir, "vrf.history"), []byte(hist), 0o600); err != nil {
		t.Fatal(err)
	}
	b, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	h := b.VRFHistory()
	if len(h) != 2 || !bytes.Equal(h[0].Key, old.Key) || !bytes.Equal(h[1].Key, pk) || !bytes.Equal(b.VRFPublic(), pk) || h[0].Began >= h[1].Began {
		t.Fatalf("history = %+v", h)
	}
}

func TestVRFHistoryDamagedIsRefused(t *testing.T) {
	for what, content := range map[string]string{
		"not JSON":   "{",
		"empty list": "[]",
		"bad key":    `[{"key": "00", "began": "2026-01-01T00:00:00.000Z"}]`,
		"no start":   `[{"key": "` + strings.Repeat("02", 33) + `"}]`,
		"out of order": `[{"key": "` + strings.Repeat("02", 33) + `", "began": "2026-06-01T00:00:00.000Z"},` +
			`{"key": "` + strings.Repeat("03", 33) + `", "began": "2026-01-01T00:00:00.000Z"}]`,
	} {
		dir := filepath.Join(t.TempDir(), "keys")
		if _, err := Open(dir); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "vrf.history"), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := Open(dir); err == nil || !strings.Contains(err.Error(), "vrf.history") {
			t.Errorf("%s: %v", what, err)
		}
	}
}
