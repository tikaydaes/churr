package secret

import (
	"bytes"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"testing"

	"golang.org/x/crypto/chacha20poly1305"
)

// macWith is HMAC-SHA-256 over one length-prefixed part, written out by hand
// for the test that the MAC key is not seal.key itself.
func macWith(key, part []byte) []byte {
	h := hmac.New(sha256.New, key)
	var n [4]byte
	binary.BigEndian.PutUint32(n[:], uint32(len(part)))
	h.Write(n[:])
	h.Write(part)
	return h.Sum(nil)
}

func TestBoxRoundTrip(t *testing.T) {
	k := open(t)
	for _, plain := range [][]byte{nil, []byte("x"), []byte(`{"kind":"key","epoch":3}`), bytes.Repeat([]byte{1}, 70000)} {
		sealed, err := SealBoxBetween(k.BoxPublic(), plain, "a.example", "b.example")
		if err != nil {
			t.Fatalf("SealBoxBetween: %v", err)
		}
		// An ephemeral public key in front, a sixteen-byte tag behind.
		if want := 32 + len(plain) + 16; len(sealed) != want {
			t.Errorf("sealed %d bytes into %d, want %d", len(plain), len(sealed), want)
		}
		got, err := k.OpenBoxBetween(sealed, "a.example", "b.example")
		if err != nil {
			t.Fatalf("OpenBoxBetween: %v", err)
		}
		if !bytes.Equal(got, plain) {
			t.Error("round trip changed the value")
		}
	}
}

func TestBoxWithoutNames(t *testing.T) {
	k := open(t)
	sealed, err := SealBox(k.BoxPublic(), []byte("hello"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := k.OpenBox(sealed)
	if err != nil || string(got) != "hello" {
		t.Fatalf("OpenBox = %q, %v", got, err)
	}
	// The two forms do not open each other's boxes.
	if _, err := k.OpenBoxBetween(sealed, "a.example", "b.example"); !errors.Is(err, ErrOpenBox) {
		t.Errorf("a box without names opened with names: %v", err)
	}
	named, _ := SealBoxBetween(k.BoxPublic(), []byte("hello"), "a.example", "b.example")
	if _, err := k.OpenBox(named); !errors.Is(err, ErrOpenBox) {
		t.Errorf("a box with names opened without them: %v", err)
	}
}

func TestBoxIsDifferentEveryTime(t *testing.T) {
	k := open(t)
	a, _ := SealBox(k.BoxPublic(), []byte("same"))
	b, _ := SealBox(k.BoxPublic(), []byte("same"))
	if bytes.Equal(a[:32], b[:32]) {
		t.Error("two boxes used the same ephemeral key, and so the same key and nonce")
	}
	if bytes.Equal(a[32:], b[32:]) {
		t.Error("two boxes of the same plaintext have the same ciphertext")
	}
}

func TestBoxRefusesOtherNamesAndKeys(t *testing.T) {
	k, other := open(t), open(t)
	sealed, _ := SealBoxBetween(k.BoxPublic(), []byte("stream key"), "a.example", "b.example")

	if _, err := other.OpenBoxBetween(sealed, "a.example", "b.example"); !errors.Is(err, ErrOpenBox) {
		t.Errorf("another server opened the box: %v", err)
	}
	names := [][2]string{
		{"evil.example", "b.example"}, // passed off as another sender
		{"a.example", "c.example"},
		{"b.example", "a.example"},
		{"a.exampleb", ".example"}, // the same characters, split elsewhere
		{"", "a.exampleb.example"},
		{"", ""},
	}
	for _, n := range names {
		if _, err := k.OpenBoxBetween(sealed, n[0], n[1]); !errors.Is(err, ErrOpenBox) {
			t.Errorf("opened as from %q to %q: %v", n[0], n[1], err)
		}
	}
}

func TestBoxRefusesTampering(t *testing.T) {
	k := open(t)
	sealed, _ := SealBoxBetween(k.BoxPublic(), []byte("stream key"), "a", "b")
	for i := range sealed {
		bad := bytes.Clone(sealed)
		bad[i] ^= 0x01
		if _, err := k.OpenBoxBetween(bad, "a", "b"); !errors.Is(err, ErrOpenBox) {
			t.Fatalf("a changed byte %d went unnoticed: %v", i, err)
		}
	}
	for _, bad := range [][]byte{nil, sealed[:31], sealed[:32], sealed[:47], sealed[:len(sealed)-1], append(bytes.Clone(sealed), 0)} {
		if _, err := k.OpenBoxBetween(bad, "a", "b"); !errors.Is(err, ErrOpenBox) {
			t.Errorf("a box of %d bytes was accepted: %v", len(bad), err)
		}
	}
}

func TestBoxRefusesBadRecipientKeys(t *testing.T) {
	for what, pub := range map[string][]byte{
		"nil":       nil,
		"too short": make([]byte, 31),
		"too long":  make([]byte, 33),
		// The all-zero point makes the shared secret all zero bytes, which
		// step 2 of the construction says to stop on.
		"low order": make([]byte, 32),
	} {
		if _, err := SealBox(pub, []byte("x")); err == nil {
			t.Errorf("sealed to a %s public key", what)
		}
	}
	// And on the way in: a box whose ephemeral key is the all-zero point.
	k := open(t)
	if _, err := k.OpenBox(make([]byte, 32+16)); !errors.Is(err, ErrOpenBox) {
		t.Errorf("opened a box with a low-order ephemeral key: %v", err)
	}
}

func TestBoxRefusesNamesTooLongToWrite(t *testing.T) {
	k := open(t)
	long := string(bytes.Repeat([]byte{'a'}, 0x10000))
	if _, err := SealBoxBetween(k.BoxPublic(), []byte("x"), long, "b"); err == nil {
		t.Error("sealed with a name whose length does not fit in two bytes")
	}
}

// TestBoxFollowsTheLinkProtocol opens a box with the construction written
// out here step by step from docs/LINK-PROTOCOL.md section 7, sharing no
// code with box.go, and seals one the same way for OpenBoxBetween to open.
// If box.go drifts from the document, this fails.
func TestBoxFollowsTheLinkProtocol(t *testing.T) {
	const from, to = "a.example", "chat.b.example"
	plain := []byte(`{"kind":"offer"}`)

	// info = "churr-seal-v1" 0x00 || lp(F.name) || lp(T.name)
	info := []byte("churr-seal-v1\x00")
	info = binary.BigEndian.AppendUint16(info, uint16(len(from)))
	info = append(info, from...)
	info = binary.BigEndian.AppendUint16(info, uint16(len(to)))
	info = append(info, to...)
	if want := "churr-seal-v1\x00\x00\x09a.example\x00\x0echat.b.example"; string(info) != want {
		t.Fatalf("the test's own info string is wrong: %q", info)
	}

	derive := func(shared, ePub, tPub []byte) []byte {
		// key = HKDF-SHA-256(ikm = shared, salt = e_pub || T.box_key, info), 32 bytes
		key, err := hkdf.Key(sha256.New, shared, append(bytes.Clone(ePub), tPub...), string(info), 32)
		if err != nil {
			t.Fatal(err)
		}
		return key
	}
	zeroNonce := make([]byte, 24)

	t.Run("open by hand what SealBoxBetween made", func(t *testing.T) {
		recipient, err := ecdh.X25519().GenerateKey(testRand{})
		if err != nil {
			t.Fatal(err)
		}
		tPub := recipient.PublicKey().Bytes()
		body, err := SealBoxBetween(tPub, plain, from, to)
		if err != nil {
			t.Fatal(err)
		}
		// body = e_pub || ct
		ePub, ct := body[:32], body[32:]
		e, err := ecdh.X25519().NewPublicKey(ePub)
		if err != nil {
			t.Fatal(err)
		}
		shared, err := recipient.ECDH(e)
		if err != nil {
			t.Fatal(err)
		}
		aead, err := chacha20poly1305.NewX(derive(shared, ePub, tPub))
		if err != nil {
			t.Fatal(err)
		}
		got, err := aead.Open(nil, zeroNonce, ct, info)
		if err != nil {
			t.Fatalf("the box does not open by the document's steps: %v", err)
		}
		if !bytes.Equal(got, plain) {
			t.Errorf("opened to %q", got)
		}
	})

	t.Run("OpenBoxBetween opens what was sealed by hand", func(t *testing.T) {
		k := open(t)
		tPub := k.BoxPublic()
		e, err := ecdh.X25519().GenerateKey(testRand{})
		if err != nil {
			t.Fatal(err)
		}
		ePub := e.PublicKey().Bytes()
		recipient, err := ecdh.X25519().NewPublicKey(tPub)
		if err != nil {
			t.Fatal(err)
		}
		shared, err := e.ECDH(recipient)
		if err != nil {
			t.Fatal(err)
		}
		aead, err := chacha20poly1305.NewX(derive(shared, ePub, tPub))
		if err != nil {
			t.Fatal(err)
		}
		body := append(bytes.Clone(ePub), aead.Seal(nil, zeroNonce, plain, info)...)
		got, err := k.OpenBoxBetween(body, from, to)
		if err != nil {
			t.Fatalf("OpenBoxBetween refused a box made by the document's steps: %v", err)
		}
		if !bytes.Equal(got, plain) {
			t.Errorf("opened to %q", got)
		}
	})
}

// testRand is a source of bytes for making throwaway keys in tests.
type testRand struct{}

func (testRand) Read(p []byte) (int, error) {
	copy(p, RandomBytes(len(p)))
	return len(p), nil
}
