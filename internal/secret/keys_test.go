package secret

import (
	"bytes"
	"crypto/ed25519"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// open makes a fresh set of keys in a temporary folder.
func open(t *testing.T) *Keys {
	t.Helper()
	k, err := Open(filepath.Join(t.TempDir(), "keys"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return k
}

func skipWithoutModes(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no permission bits to check")
	}
}

func TestOpenMakesKeysWithTightModes(t *testing.T) {
	skipWithoutModes(t)
	// A parent that does not exist yet, as on the very first start.
	dir := filepath.Join(t.TempDir(), "data", "keys")
	if _, err := Open(dir); err != nil {
		t.Fatalf("Open: %v", err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o700 {
		t.Errorf("keys directory mode = %04o, want 0700", got)
	}
	for name, size := range map[string]int64{"seal.key": 32, "sign.key": 32, "box.key": 32} {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Errorf("%s mode = %04o, want 0600", name, got)
		}
		if info.Size() != size {
			t.Errorf("%s is %d bytes, want %d", name, info.Size(), size)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Errorf("keys directory holds %d entries, want exactly the three key files", len(entries))
	}
}

func TestOpenAgainGivesTheSameKeys(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "keys")
	a, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Open(dir)
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	if !a.SignPublic().Equal(b.SignPublic()) {
		t.Error("the signing key changed between starts")
	}
	if !bytes.Equal(a.BoxPublic(), b.BoxPublic()) {
		t.Error("the box key changed between starts")
	}
	sealed, err := a.Seal([]byte("secret"), "t", "c", "r")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.Unseal(sealed, "t", "c", "r"); err != nil {
		t.Errorf("a value sealed before a restart did not open after it: %v", err)
	}
}

func TestOpenMakesDifferentKeysEachTime(t *testing.T) {
	a, b := open(t), open(t)
	if a.SignPublic().Equal(b.SignPublic()) || bytes.Equal(a.BoxPublic(), b.BoxPublic()) {
		t.Error("two fresh key folders hold the same keys")
	}
	if bytes.Equal(a.seal, b.seal) {
		t.Error("two fresh key folders hold the same seal key")
	}
	if bytes.Equal(a.seal, a.sign.Seed()) || bytes.Equal(a.seal, a.box.Bytes()) {
		t.Error("two of the three keys in one folder are the same bytes")
	}
}

func TestOpenRefusesKeysOthersCanRead(t *testing.T) {
	skipWithoutModes(t)
	for _, name := range []string{"seal.key", "sign.key", "box.key"} {
		for _, mode := range []os.FileMode{0o640, 0o604, 0o644, 0o660, 0o620} {
			dir := filepath.Join(t.TempDir(), "keys")
			if _, err := Open(dir); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, name)
			if err := os.Chmod(path, mode); err != nil {
				t.Fatal(err)
			}
			_, err := Open(dir)
			if err == nil {
				t.Errorf("%s with mode %04o was accepted", name, mode)
				continue
			}
			if !strings.Contains(err.Error(), name) || !strings.Contains(err.Error(), "0600") {
				t.Errorf("%s with mode %04o: the error does not say what to fix: %v", name, mode, err)
			}
		}
	}
}

func TestOpenRefusesAnOpenDirectory(t *testing.T) {
	skipWithoutModes(t)
	for _, mode := range []os.FileMode{0o750, 0o705, 0o755, 0o770} {
		dir := filepath.Join(t.TempDir(), "keys")
		if _, err := Open(dir); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(dir, mode); err != nil {
			t.Fatal(err)
		}
		_, err := Open(dir)
		if err == nil {
			t.Errorf("a keys directory with mode %04o was accepted", mode)
			continue
		}
		if !strings.Contains(err.Error(), "0700") {
			t.Errorf("mode %04o: the error does not say what to fix: %v", mode, err)
		}
	}
}

func TestOpenRefusesDamagedKeyFiles(t *testing.T) {
	cases := map[string][]byte{
		"empty":     {},
		"too short": bytes.Repeat([]byte{7}, 31),
		"too long":  bytes.Repeat([]byte{7}, 33),
		"all zero":  make([]byte, 32),
	}
	for _, name := range []string{"seal.key", "sign.key", "box.key"} {
		for what, content := range cases {
			dir := filepath.Join(t.TempDir(), "keys")
			if _, err := Open(dir); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, name), content, 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := Open(dir)
			if err == nil {
				t.Errorf("%s (%s) was accepted", name, what)
				continue
			}
			if !strings.Contains(err.Error(), name) {
				t.Errorf("%s (%s): the error does not name the file: %v", name, what, err)
			}
			// The damaged file must be left as it was found, never replaced.
			after, rerr := os.ReadFile(filepath.Join(dir, name))
			if rerr != nil || !bytes.Equal(after, content) {
				t.Errorf("%s (%s): Open changed the damaged file", name, what)
			}
		}
	}
}

func TestOpenRefusesAFileWhereTheDirectoryShouldBe(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keys")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path); err == nil {
		t.Error("a plain file was accepted as the keys directory")
	}
	if _, err := Open(""); err == nil {
		t.Error("an empty path was accepted")
	}
}

func TestOpenMakesAMissingFileAndKeepsTheOthers(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "keys")
	a, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "box.key")); err != nil {
		t.Fatal(err)
	}
	b, err := Open(dir)
	if err != nil {
		t.Fatalf("Open with box.key missing: %v", err)
	}
	if !a.SignPublic().Equal(b.SignPublic()) {
		t.Error("sign.key was replaced although it was present")
	}
	if bytes.Equal(a.BoxPublic(), b.BoxPublic()) {
		t.Error("box.key was missing, yet the old key came back")
	}
}

func TestSignVerifiesUnderSignPublic(t *testing.T) {
	k := open(t)
	msg := []byte("churr-box-key-v1\x00example")
	sig := k.Sign(msg)
	if len(sig) != ed25519.SignatureSize {
		t.Fatalf("signature is %d bytes", len(sig))
	}
	if !ed25519.Verify(k.SignPublic(), msg, sig) {
		t.Error("the signature does not verify under SignPublic")
	}
	if ed25519.Verify(k.SignPublic(), append(msg, 'x'), sig) {
		t.Error("the signature verifies for another message")
	}
	if ed25519.Verify(open(t).SignPublic(), msg, sig) {
		t.Error("the signature verifies under another server's key")
	}
}

func TestPublicKeysAreCopies(t *testing.T) {
	k := open(t)
	if len(k.SignPublic()) != 32 || len(k.BoxPublic()) != 32 {
		t.Fatalf("public keys are %d and %d bytes, want 32 and 32", len(k.SignPublic()), len(k.BoxPublic()))
	}
	// A caller scribbling on what it was given must not change the keys.
	sp, bp := k.SignPublic(), k.BoxPublic()
	sp[0] ^= 0xff
	bp[0] ^= 0xff
	if bytes.Equal(sp, k.SignPublic()) || bytes.Equal(bp, k.BoxPublic()) {
		t.Error("a public key is handed out by reference")
	}
}

func TestRandomBytes(t *testing.T) {
	for _, n := range []int{0, 1, 16, 32, 100} {
		if got := len(RandomBytes(n)); got != n {
			t.Errorf("RandomBytes(%d) gave %d bytes", n, got)
		}
	}
	if bytes.Equal(RandomBytes(32), RandomBytes(32)) {
		t.Error("two calls gave the same 32 bytes")
	}
}
