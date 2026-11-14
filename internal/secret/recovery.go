package secret

import (
	"crypto/sha256"
	"encoding/base32"
	"strings"
)

// recoveryAlphabet is Crockford's base32: the digits and the capital
// letters without I, L, O and U, so that no two characters look alike and
// a code read aloud or copied by hand survives.
const recoveryAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// recoveryLen is the number of characters in a code. Each carries five
// random bits, so a code holds 80.
const recoveryLen = 16

var recoveryEncoding = base32.NewEncoding(recoveryAlphabet).WithPadding(base32.NoPadding)

// NewRecoveryCode makes one recovery code as it is shown to its owner:
// sixteen characters in four groups, such as 7K2M-9QXD-04TV-HB3N
// (docs/AUTH.md section 6.1). Store HashRecoveryCode of it, not the code.
func NewRecoveryCode() string {
	// Ten bytes are exactly sixteen base32 characters, so there is no
	// padding and no bias.
	raw := recoveryEncoding.EncodeToString(RandomBytes(recoveryLen * 5 / 8))
	return raw[0:4] + "-" + raw[4:8] + "-" + raw[8:12] + "-" + raw[12:16]
}

// NormalizeRecoveryCode turns what a person typed into the sixteen
// characters that are hashed: letters are upper-cased, spaces and hyphens
// are dropped, O is read as 0, and I and L are read as 1. It reports false
// when what is left is not sixteen characters of the alphabet, so the
// caller can skip the lookup; the caller must still answer exactly as it
// does for a well-formed code that matches nothing.
func NormalizeRecoveryCode(typed string) (string, bool) {
	var b strings.Builder
	b.Grow(recoveryLen)
	for _, r := range typed {
		switch {
		case r == '-' || r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\u00a0':
			// Hyphens, and the kinds of space a paste brings along.
			continue
		case r >= 'a' && r <= 'z':
			r -= 'a' - 'A'
		}
		switch r {
		case 'O':
			r = '0'
		case 'I', 'L':
			r = '1'
		}
		// Only ASCII is looked at. A letter from another script that merely
		// resembles one of ours is not a character of the code.
		if r > 0x7f || strings.IndexByte(recoveryAlphabet, byte(r)) < 0 {
			return "", false
		}
		if b.Len() == recoveryLen {
			return "", false
		}
		b.WriteByte(byte(r))
	}
	if b.Len() != recoveryLen {
		return "", false
	}
	return b.String(), true
}

// HashRecoveryCode is the SHA-256 hash of the normalised code, which is
// what recovery_codes.code_hash holds. It reports false for input that
// cannot be a code. With 80 random bits a plain hash is enough.
func HashRecoveryCode(typed string) ([]byte, bool) {
	code, ok := NormalizeRecoveryCode(typed)
	if !ok {
		return nil, false
	}
	sum := sha256.Sum256([]byte(code))
	return sum[:], true
}
