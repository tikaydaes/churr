package secret

import (
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"math/big"
	"strconv"
	"strings"
)

// Access keys (docs/AUTH.md section 5.6): the long random code an account
// signs in with when its person cannot use a passkey. Nobody chooses one;
// the page makes it, shows it once, and the server never sees it. What the
// server is sent and keeps is derived from it.
//
// The grammar is shared with the web pages (static/shared/accesskey.js) and
// the app (app/lib/core/access_key.dart), and all three are tested against
// testdata/accesskey/vectors.json, which accesskey_test.go writes.
//
//	35 bytes = version (1) || 32 random bytes || first 2 bytes of SHA-256 of the first 33
//
// read as one big-endian number and written in base 58 with the alphabet
// below, left-padded with its zero digit ("2") to 48 characters, and shown
// as 12 groups of 4 separated by spaces. The version shows through as the
// first character, which is always "2" for version 1.

// AccessKeyAlphabet is the 58 characters an access key is written in: the
// digits and letters without 0, 1, O and I. Lower-case l is kept.
const AccessKeyAlphabet = "23456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

// The sizes of an access key.
const (
	AccessKeyVersion = 1
	AccessKeyRandom  = 32
	AccessKeyChars   = 48
	accessKeyBytes   = 1 + AccessKeyRandom + 2
)

// The derivations of docs/AUTH.md section 5.6. The salt is the same for
// every key; each use has its own info.
const (
	AccessKeySalt     = "churr access key v1"
	AccessKeyAuthInfo = "churr access key auth v1"
	// AccessKeyWrapInfo is reserved for locking device or sealed-chat
	// keys under an access key. Nothing uses it yet.
	AccessKeyWrapInfo = "churr access key wrap v1"
)

// AccessKeyError says what is wrong with typed text, in words to show.
// Kind is one of length, character, checksum and version; the shared
// vectors name it.
type AccessKeyError struct {
	Kind    string
	Message string
}

func (e *AccessKeyError) Error() string { return e.Message }

// The messages are the same in the three implementations.
const (
	msgCharacter = "An access key never has 0, 1, capital O or capital I. Check that character: it may be a small o or l."
	msgOther     = "That character is not part of an access key."
	msgChecksum  = "That key has a typing mistake. Check each character: capital and small letters are different."
	msgVersion   = "That is not an access key from this version of Churr."
)

var accessKeyBase = big.NewInt(58)

// NewAccessKey makes 32 random bytes for a new access key.
func NewAccessKey() []byte {
	b := make([]byte, AccessKeyRandom)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return b
}

func accessKeyChecksum(head []byte) []byte {
	sum := sha256.Sum256(head)
	return sum[:2]
}

// FormatAccessKey writes 32 random bytes as an access key, in 12 groups of
// 4 separated by spaces.
func FormatAccessKey(random []byte) string {
	if len(random) != AccessKeyRandom {
		panic("secret: an access key holds 32 random bytes")
	}
	raw := make([]byte, 0, accessKeyBytes)
	raw = append(raw, AccessKeyVersion)
	raw = append(raw, random...)
	raw = append(raw, accessKeyChecksum(raw)...)
	n := new(big.Int).SetBytes(raw)
	digits := make([]byte, AccessKeyChars)
	mod := new(big.Int)
	for i := AccessKeyChars - 1; i >= 0; i-- {
		n.DivMod(n, accessKeyBase, mod)
		digits[i] = AccessKeyAlphabet[mod.Int64()]
	}
	var b strings.Builder
	for i := 0; i < AccessKeyChars; i += 4 {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.Write(digits[i : i+4])
	}
	return b.String()
}

// ParseAccessKey reads typed text back to the 32 random bytes. Spaces,
// other whitespace and hyphens anywhere are ignored; case matters.
func ParseAccessKey(text string) ([]byte, error) {
	var digits []byte
	for _, r := range text {
		switch {
		case r == '-' || r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\f' || r == '\v' || r == 0xa0:
			continue
		case r == '0' || r == '1' || r == 'O' || r == 'I':
			return nil, &AccessKeyError{Kind: "character", Message: msgCharacter}
		case r < 128 && strings.IndexByte(AccessKeyAlphabet, byte(r)) >= 0:
			digits = append(digits, byte(r))
		default:
			return nil, &AccessKeyError{Kind: "character", Message: msgOther}
		}
	}
	if len(digits) != AccessKeyChars {
		return nil, &AccessKeyError{Kind: "length", Message: lengthMessage(len(digits))}
	}
	n := new(big.Int)
	for _, d := range digits {
		n.Mul(n, accessKeyBase)
		n.Add(n, big.NewInt(int64(strings.IndexByte(AccessKeyAlphabet, d))))
	}
	if n.BitLen() > accessKeyBytes*8 {
		return nil, &AccessKeyError{Kind: "checksum", Message: msgChecksum}
	}
	raw := n.FillBytes(make([]byte, accessKeyBytes))
	head, sum := raw[:accessKeyBytes-2], raw[accessKeyBytes-2:]
	want := accessKeyChecksum(head)
	if sum[0] != want[0] || sum[1] != want[1] {
		return nil, &AccessKeyError{Kind: "checksum", Message: msgChecksum}
	}
	if head[0] != AccessKeyVersion {
		return nil, &AccessKeyError{Kind: "version", Message: msgVersion}
	}
	return append([]byte(nil), head[1:]...), nil
}

func lengthMessage(n int) string {
	return "An access key is 48 characters; this has " + strconv.Itoa(n) + "."
}

func accessKeyDerive(random []byte, info string) []byte {
	if len(random) != AccessKeyRandom {
		panic("secret: an access key holds 32 random bytes")
	}
	out, err := hkdf.Key(sha256.New, random, []byte(AccessKeySalt), info, 32)
	if err != nil {
		panic(err)
	}
	return out
}

// AccessKeyAuth is the value a page sends to sign in with an access key:
// HKDF-SHA-256 of the 32 random bytes. The server keeps only its SHA-256.
func AccessKeyAuth(random []byte) []byte { return accessKeyDerive(random, AccessKeyAuthInfo) }

// AccessKeyWrap is the second derivation, reserved for locking keys under
// an access key. Nothing uses it yet; the vectors fix it.
func AccessKeyWrap(random []byte) []byte { return accessKeyDerive(random, AccessKeyWrapInfo) }

// ErrAccessKeyAuth is returned for a sent value that is not 32 bytes.
var ErrAccessKeyAuth = errors.New("secret: an access key's sign-in value is 32 bytes")

// HashAccessKeyAuth is what the server stores and compares: SHA-256 of the
// 32-byte sign-in value. A slow hash adds nothing to 256 random bits.
func HashAccessKeyAuth(auth []byte) ([]byte, error) {
	if len(auth) != 32 {
		return nil, ErrAccessKeyAuth
	}
	sum := sha256.Sum256(auth)
	return sum[:], nil
}
