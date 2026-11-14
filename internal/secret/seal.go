package secret

import (
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"errors"
)

// ErrUnseal is returned when a sealed value cannot be opened: it was
// changed, it was sealed for another table, column or row, or it was sealed
// under another seal.key. The cases are deliberately not told apart.
var ErrUnseal = errors.New("secret: the sealed value cannot be opened")

// nonceLen is the length of the random nonce in front of a sealed value.
const nonceLen = 12

// sealLabel begins the associated data of every sealed column. It keeps a
// sealed column apart from anything else ever encrypted under seal.key.
const sealLabel = "churr-column-v1\x00"

// Seal encrypts plaintext for one place in the database. The result is a
// random 96-bit nonce followed by the AES-256-GCM ciphertext and tag.
//
// The table name, column name and row identifier go in as associated data,
// so a sealed value copied into another row, column or table does not open
// there (docs/AUTH.md section 1). row is whatever names the row for good:
// its identifier, not something that can be edited later.
func (k *Keys) Seal(plaintext []byte, table, column, row string) ([]byte, error) {
	// The nonce is the start of the output, and Seal appends to it, so the
	// result is one allocation of exactly the right size.
	out := make([]byte, nonceLen, nonceLen+len(plaintext)+k.sealing.Overhead())
	copy(out, RandomBytes(nonceLen))
	return k.sealing.Seal(out, out[:nonceLen], plaintext, place(table, column, row)), nil
}

// Unseal opens what Seal made for the same table, column and row. Any other
// input gives ErrUnseal.
func (k *Keys) Unseal(sealed []byte, table, column, row string) ([]byte, error) {
	if len(sealed) < nonceLen+k.sealing.Overhead() {
		return nil, ErrUnseal
	}
	plain, err := k.sealing.Open(nil, sealed[:nonceLen], sealed[nonceLen:], place(table, column, row))
	if err != nil {
		return nil, ErrUnseal
	}
	return plain, nil
}

// place is the associated data for a sealed column: a label, then each of
// the three names with its length in front. The lengths are what make it
// unambiguous: ("ab", "c") and ("a", "bc") give different bytes.
func place(table, column, row string) []byte {
	b := make([]byte, 0, len(sealLabel)+12+len(table)+len(column)+len(row))
	b = append(b, sealLabel...)
	for _, s := range []string{table, column, row} {
		b = binary.BigEndian.AppendUint32(b, uint32(len(s)))
		b = append(b, s...)
	}
	return b
}

// macLabel begins the HKDF info for every MAC key.
const macLabel = "churr-mac-v1\x00"

// MAC returns the 32-byte HMAC-SHA-256 of parts under a key that belongs to
// label alone. It is for values that must be the same every time for the
// same input yet mean nothing to anyone without seal.key, such as the route
// and collapse key of a push wake-up; callers cut the result to the length
// they need.
//
// Each label gets its own key, derived from seal.key with HKDF-SHA-256, so
// a value made for one purpose can never stand in for another. Each part is
// written with its length in front, so ("ab", "c") and ("a", "bc") differ,
// and so do one empty part and no parts.
func (k *Keys) MAC(label string, parts ...[]byte) []byte {
	key, err := hkdf.Key(sha256.New, k.seal, nil, macLabel+label, sha256.Size)
	if err != nil {
		// Only a key length HKDF cannot produce gets here, and 32 is not one.
		panic("secret: deriving a MAC key: " + err.Error())
	}
	h := hmac.New(sha256.New, key)
	var n [4]byte
	for _, p := range parts {
		binary.BigEndian.PutUint32(n[:], uint32(len(p)))
		h.Write(n[:])
		h.Write(p)
	}
	return h.Sum(nil)
}
