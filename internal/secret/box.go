package secret

import (
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"

	"golang.org/x/crypto/chacha20poly1305"
)

// ErrOpenBox is returned when a box cannot be opened: it was changed, it
// was sealed to another key, or it was sealed between other names. The
// cases are deliberately not told apart.
var ErrOpenBox = errors.New("secret: the box cannot be opened")

// boxLabel begins the info string of docs/LINK-PROTOCOL.md section 7.
const boxLabel = "churr-seal-v1\x00"

// boxKeyLen is the length of an X25519 public key, and so of the ephemeral
// key in front of every box.
const boxKeyLen = 32

// SealBoxBetween seals plaintext to the server whose X25519 public key is
// recipientPublic, exactly as docs/LINK-PROTOCOL.md section 7 lays out,
// steps 1 to 6:
//
//	(e_priv, e_pub) = a fresh X25519 key pair
//	shared = X25519(e_priv, recipientPublic)
//	info   = "churr-seal-v1" 0x00 || lp(from) || lp(to)
//	key    = HKDF-SHA-256(ikm = shared, salt = e_pub || recipientPublic, info), 32 bytes
//	ct     = XChaCha20-Poly1305(key, nonce = 24 zero bytes, plaintext, associated data = info)
//	body   = e_pub || ct
//
// from and to are the names of the sending and receiving servers. They are
// part of the key, so a box opens only for the pair it was made for. Each
// box has its own key, which is why the fixed nonce is safe.
//
// The result is body. Signing it (step 7) is the caller's work, since the
// signature belongs to the frame the box travels in.
func SealBoxBetween(recipientPublic, plaintext []byte, from, to string) ([]byte, error) {
	recipient, err := ecdh.X25519().NewPublicKey(recipientPublic)
	if err != nil {
		return nil, fmt.Errorf("secret: the recipient's box key is not an X25519 public key: %w", err)
	}
	e, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("secret: making a key pair for the box: %w", err)
	}
	ePub := e.PublicKey().Bytes()
	// ECDH fails when the shared secret is all zero bytes, which is the
	// "stop" of step 2: such a public key is not one a real server has.
	shared, err := e.ECDH(recipient)
	if err != nil {
		return nil, fmt.Errorf("secret: the recipient's box key cannot be used: %w", err)
	}
	aead, info, err := boxCipher(shared, ePub, recipientPublic, from, to)
	if err != nil {
		return nil, err
	}
	out := make([]byte, boxKeyLen, boxKeyLen+len(plaintext)+aead.Overhead())
	copy(out, ePub)
	return aead.Seal(out, make([]byte, aead.NonceSize()), plaintext, info), nil
}

// OpenBoxBetween opens a box that SealBoxBetween made for this server's box
// key and the same two names. Any other input gives ErrOpenBox.
func (k *Keys) OpenBoxBetween(sealed []byte, from, to string) ([]byte, error) {
	if len(sealed) < boxKeyLen+chacha20poly1305.Overhead {
		return nil, ErrOpenBox
	}
	ePub := sealed[:boxKeyLen]
	sender, err := ecdh.X25519().NewPublicKey(ePub)
	if err != nil {
		return nil, ErrOpenBox
	}
	shared, err := k.box.ECDH(sender)
	if err != nil {
		return nil, ErrOpenBox
	}
	aead, info, err := boxCipher(shared, ePub, k.box.PublicKey().Bytes(), from, to)
	if err != nil {
		return nil, ErrOpenBox
	}
	plain, err := aead.Open(nil, make([]byte, aead.NonceSize()), sealed[boxKeyLen:], info)
	if err != nil {
		return nil, ErrOpenBox
	}
	return plain, nil
}

// SealBox is SealBoxBetween with both names empty: a box for one key, tied
// to no pair of servers.
//
// Nothing in the link protocol is sealed this way. Direct messages, stream
// keys and form answers all name the two servers and must use
// SealBoxBetween, or the other side will not be able to open them.
func SealBox(recipientPublic, plaintext []byte) ([]byte, error) {
	return SealBoxBetween(recipientPublic, plaintext, "", "")
}

// OpenBox opens what SealBox made for this server's box key. A box sealed
// with names needs OpenBoxBetween.
func (k *Keys) OpenBox(sealed []byte) ([]byte, error) {
	return k.OpenBoxBetween(sealed, "", "")
}

// boxCipher carries out steps 3 and 4: it builds the info string and derives
// the one-time key, and returns the cipher under that key with the info to
// use as associated data.
func boxCipher(shared, ePub, recipientPublic []byte, from, to string) (cipher.AEAD, []byte, error) {
	// lp() writes a length in two bytes, so a longer name cannot be written.
	// Server names are host names and never come near this.
	if len(from) > 0xFFFF || len(to) > 0xFFFF {
		return nil, nil, errors.New("secret: a server name is too long to seal with")
	}
	info := make([]byte, 0, len(boxLabel)+4+len(from)+len(to))
	info = append(info, boxLabel...)
	info = binary.BigEndian.AppendUint16(info, uint16(len(from)))
	info = append(info, from...)
	info = binary.BigEndian.AppendUint16(info, uint16(len(to)))
	info = append(info, to...)

	salt := make([]byte, 0, 2*boxKeyLen)
	salt = append(salt, ePub...)
	salt = append(salt, recipientPublic...)

	key, err := hkdf.Key(sha256.New, shared, salt, string(info), chacha20poly1305.KeySize)
	if err != nil {
		return nil, nil, fmt.Errorf("secret: deriving the box key: %w", err)
	}
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, nil, fmt.Errorf("secret: %w", err)
	}
	return aead, info, nil
}
