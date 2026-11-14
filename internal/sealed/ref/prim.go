// Package ref is the reference implementation of the client side of sealed
// chats: every construction of docs/SEALED.md, written in Go on the five
// primitives that document allows and nothing else.
//
// The server never links this package. It is imported only from tests and
// from testkit, where it plays the part of a device, and it writes the test
// vectors in testdata/sealed that the Dart, JavaScript and Swift
// implementations are built against (vectors.go, transcript.go).
//
// Layout: prim.go the primitives; enc.go the record encoding; seal.go seal
// and lock; sign.go signatures, hashes and every label; one file per kind
// of record after that; device.go a whole device, which is what tests
// drive.
//
// Nothing here reads the system's random generator directly. Every
// function that needs random bytes is handed them, so the vectors can fix
// them; a Device draws them from its Rand.
package ref

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/hkdf"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
)

// The five primitives of docs/SEALED.md section 2.2: X25519, Ed25519,
// HKDF-SHA-256, AES-256-GCM and SHA-256.

// ErrZeroShared is returned for an X25519 result of 32 zero bytes, which
// every construction refuses.
var ErrZeroShared = errors.New("ref: the X25519 result is all zero")

// ErrOpen is returned when something encrypted does not open: a changed
// byte, another label or context, another key. The cases are not told
// apart.
var ErrOpen = errors.New("ref: the value does not open")

// HKDF is HKDF-SHA-256, extract then expand, giving n bytes.
func HKDF(ikm, salt, info []byte, n int) []byte {
	out, err := hkdf.Key(sha256.New, ikm, salt, string(info), n)
	if err != nil {
		// Only an output longer than 255 hashes gets here, and nothing in
		// the scheme asks for more than 44 bytes.
		panic("ref: hkdf: " + err.Error())
	}
	return out
}

// X25519 is the function of RFC 7748 applied to a private key and a public
// key. A result of 32 zero bytes is refused.
func X25519(private, public []byte) ([]byte, error) {
	priv, err := ecdh.X25519().NewPrivateKey(private)
	if err != nil {
		return nil, fmt.Errorf("ref: an X25519 private key is 32 bytes")
	}
	pub, err := ecdh.X25519().NewPublicKey(public)
	if err != nil {
		return nil, fmt.Errorf("ref: an X25519 public key is 32 bytes")
	}
	shared, err := priv.ECDH(pub)
	if err != nil {
		// The standard library refuses the all-zero result itself.
		return nil, ErrZeroShared
	}
	zero := true
	for _, b := range shared {
		if b != 0 {
			zero = false
		}
	}
	if zero {
		return nil, ErrZeroShared
	}
	return shared, nil
}

// X25519Public computes the public key of a private key: the function
// applied to the base point, 9.
func X25519Public(private []byte) []byte {
	priv, err := ecdh.X25519().NewPrivateKey(private)
	if err != nil {
		panic("ref: an X25519 private key is 32 bytes")
	}
	return priv.PublicKey().Bytes()
}

// gcmSeal is AES-256-GCM with a 12-byte nonce; the 16-byte tag is appended.
func gcmSeal(key, nonce, plaintext, ad []byte) []byte {
	return gcm(key).Seal(nil, nonce, plaintext, ad)
}

// gcmOpen is the inverse. Any failure is ErrOpen.
func gcmOpen(key, nonce, ciphertext, ad []byte) ([]byte, error) {
	if len(ciphertext) < 16 {
		return nil, ErrOpen
	}
	plain, err := gcm(key).Open(nil, nonce, ciphertext, ad)
	if err != nil {
		return nil, ErrOpen
	}
	if plain == nil {
		plain = []byte{}
	}
	return plain, nil
}

func gcm(key []byte) cipher.AEAD {
	if len(key) != 32 {
		panic("ref: an AES-256 key is 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		panic(err)
	}
	g, err := cipher.NewGCM(block)
	if err != nil {
		panic(err)
	}
	return g
}

// SigningKey makes an Ed25519 private key from its 32-byte seed.
func SigningKey(seed []byte) ed25519.PrivateKey {
	if len(seed) != ed25519.SeedSize {
		panic("ref: an Ed25519 seed is 32 bytes")
	}
	return ed25519.NewKeyFromSeed(seed)
}

// Public returns the 32-byte public half of an Ed25519 private key.
func Public(k ed25519.PrivateKey) []byte {
	return []byte(k.Public().(ed25519.PublicKey))
}

// UUID is an identifier as its 16 bytes.
type UUID [16]byte

// ParseUUID reads the lowercase hyphenated form.
func ParseUUID(s string) (UUID, error) {
	var u UUID
	if len(s) != 36 || s[8] != '-' || s[13] != '-' || s[18] != '-' || s[23] != '-' {
		return u, fmt.Errorf("ref: not an identifier: %q", s)
	}
	raw, err := hex.DecodeString(s[0:8] + s[9:13] + s[14:18] + s[19:23] + s[24:36])
	if err != nil {
		return u, fmt.Errorf("ref: not an identifier: %q", s)
	}
	copy(u[:], raw)
	return u, nil
}

// MustUUID is ParseUUID for constants.
func MustUUID(s string) UUID {
	u, err := ParseUUID(s)
	if err != nil {
		panic(err)
	}
	return u
}

// String writes the lowercase hyphenated form.
func (u UUID) String() string {
	h := hex.EncodeToString(u[:])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}

// IsZero reports whether u is 16 zero bytes.
func (u UUID) IsZero() bool { return u == UUID{} }

// compare orders two byte strings as the record encoding orders set keys:
// byte by byte, ascending.
func compare(a, b []byte) int {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			if a[i] < b[i] {
				return -1
			}
			return 1
		}
	}
	return len(a) - len(b)
}
