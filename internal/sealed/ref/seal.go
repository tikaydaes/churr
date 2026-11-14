package ref

import "fmt"

// Seal and lock: the two ways anything is encrypted (docs/SEALED.md
// sections 2.5 and 2.6). Each gives AES-256-GCM a key that is used for
// exactly one encryption.

// Seal encrypts plaintext to a recipient's X25519 public key.
//
// oneTime is the private half of a fresh key pair, 32 random bytes, used
// for this one seal and then discarded. The result is the one-time public
// key followed by the ciphertext with its tag: sealing 32 bytes gives 80.
// A seal says nothing about who made it.
func Seal(recipient []byte, label string, context, plaintext, oneTime []byte) ([]byte, error) {
	if len(recipient) != 32 || len(oneTime) != 32 {
		return nil, fmt.Errorf("ref: seal needs a 32-byte recipient key and a 32-byte one-time key")
	}
	shared, err := X25519(oneTime, recipient)
	if err != nil {
		return nil, err
	}
	e := X25519Public(oneTime)
	ad := labelled(label, context)
	okm := HKDF(shared, append(append([]byte{}, e...), recipient...), ad, 44)
	return append(e, gcmSeal(okm[:32], okm[32:], plaintext, ad)...), nil
}

// Open opens what Seal made for this private key, label and context. Any
// failure refuses the whole value.
func Open(private []byte, label string, context, sealed []byte) ([]byte, error) {
	if len(private) != 32 || len(sealed) < 32+16 {
		return nil, ErrOpen
	}
	e, ct := sealed[:32], sealed[32:]
	shared, err := X25519(private, e)
	if err != nil {
		return nil, ErrOpen
	}
	ad := labelled(label, context)
	okm := HKDF(shared, append(append([]byte{}, e...), X25519Public(private)...), ad, 44)
	return gcmOpen(okm[:32], okm[32:], ct, ad)
}

// SaltSize is the length of the random salt in front of a locked value.
const SaltSize = 16

// Lock encrypts plaintext under 32 secret bytes. salt is 16 random bytes;
// the result is the salt followed by the ciphertext with its tag.
func Lock(key []byte, label string, context, plaintext, salt []byte) []byte {
	if len(key) != 32 || len(salt) != SaltSize {
		panic("ref: lock needs a 32-byte key and a 16-byte salt")
	}
	ad := labelled(label, context)
	okm := HKDF(key, salt, ad, 44)
	return append(append([]byte{}, salt...), gcmSeal(okm[:32], okm[32:], plaintext, ad)...)
}

// Unlock opens what Lock made under this key, label and context.
func Unlock(key []byte, label string, context, locked []byte) ([]byte, error) {
	if len(key) != 32 || len(locked) < SaltSize+16 {
		return nil, ErrOpen
	}
	ad := labelled(label, context)
	okm := HKDF(key, locked[:SaltSize], ad, 44)
	return gcmOpen(okm[:32], okm[32:], locked[SaltSize:], ad)
}
