// Package wire is the link protocol itself, version 1, as
// docs/LINK-PROTOCOL.md fixes it: the encodings, a server's published
// identity, invitations, frames, the envelope, sealed direct messages, the
// handshake and the connection. It knows nothing about accounts, channels or
// storage, which is why the hub can be built from it and still be unable to
// do what a server does.
package wire

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base32"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"strings"

	"github.com/google/uuid"
)

// The numbers of sections 2, 6 and 23.
const (
	// Version is the one protocol version this code speaks. Version 2 is
	// the protocol with events stamped by a shared clock (ruling R57),
	// channels a hub owns (R56) and the move onto a hub (R55): the
	// envelope and the frames differ from version 1's, so a side that
	// speaks only version 1 is refused at the handshake.
	Version = 2
	// Subprotocol is the WebSocket subprotocol a dialler offers.
	Subprotocol = "churr-link.v2"
	// oldSubprotocol is what a version 1 dialler offers. A listener takes
	// the WebSocket under it only to say version_unsupported in the
	// link's own words (Accept).
	oldSubprotocol = "churr-link.v1"
	// MaxFrameBytes is the largest frame.
	MaxFrameBytes = 1 << 20
	// MaxEnvelopeBytes is the most ciphertext one envelope carries.
	MaxEnvelopeBytes = 262144
	// ChunkBytes is how much of a file one blob_chunk holds.
	ChunkBytes = 262144
)

// b64 is base64url without padding, strict: a decoder rejects padding and
// characters outside the alphabet (section 2).
var b64 = base64.RawURLEncoding.Strict()

// Encode writes bytes the way they travel inside JSON.
func Encode(b []byte) string { return b64.EncodeToString(b) }

// Decode reads what Encode wrote, and refuses padding, line breaks and
// anything outside the alphabet.
func Decode(s string) ([]byte, error) {
	if strings.ContainsAny(s, "=\r\n") {
		return nil, errors.New("wire: bytes must be base64url without padding")
	}
	return b64.DecodeString(s)
}

// Bytes is a byte string inside a frame: base64url without padding in JSON.
type Bytes []byte

// MarshalJSON writes the bytes as a base64url string.
func (b Bytes) MarshalJSON() ([]byte, error) { return json.Marshal(Encode(b)) }

// UnmarshalJSON reads a base64url string, strictly.
func (b *Bytes) UnmarshalJSON(raw []byte) error {
	if string(raw) == "null" {
		*b = nil
		return nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return errors.New("wire: bytes must be a string")
	}
	out, err := Decode(s)
	if err != nil {
		return err
	}
	*b = out
	return nil
}

// layout builds the byte strings that are signed. Nothing is ever signed as
// JSON; each signature is over one of these.
type layout struct{ bytes.Buffer }

// label writes ASCII followed by one zero byte.
func (l *layout) label(s string) *layout {
	l.WriteString(s)
	l.WriteByte(0)
	return l
}

func (l *layout) u16(n int) *layout {
	var b [2]byte
	binary.BigEndian.PutUint16(b[:], uint16(n))
	l.Write(b[:])
	return l
}

func (l *layout) u32(n int) *layout {
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], uint32(n))
	l.Write(b[:])
	return l
}

// i64 writes a signed 64-bit number, big-endian, two's complement.
func (l *layout) i64(n int64) *layout {
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], uint64(n))
	l.Write(b[:])
	return l
}

// lp writes a string preceded by the u16 of its length in bytes.
func (l *layout) lp(s string) *layout {
	l.u16(len(s))
	l.WriteString(s)
	return l
}

func (l *layout) raw(b []byte) *layout {
	l.Write(b)
	return l
}

// id16 writes an identifier as its 16 raw bytes. An identifier that is not a
// UUID writes sixteen zero bytes; nothing signed that way verifies against a
// real one, and callers check identifiers before they get here.
func (l *layout) id16(id string) *layout {
	u, err := uuid.Parse(id)
	if err != nil {
		u = uuid.UUID{}
	}
	l.Write(u[:])
	return l
}

// ValidID reports whether s is a UUID written the canonical way.
func ValidID(s string) bool {
	u, err := uuid.Parse(s)
	return err == nil && u.String() == s
}

// Fingerprint is the first 20 bytes of SHA-256("churr-fp-v1" 0x00 ||
// sign_key) (section 3).
func Fingerprint(signKey []byte) []byte {
	var l layout
	l.label("churr-fp-v1").raw(signKey)
	sum := sha256.Sum256(l.Bytes())
	return sum[:20]
}

// FingerprintText shows a fingerprint as 32 characters of base32 in eight
// groups of four: K7QF-2M3X-PLDA-6TZN-W4HB-EJ5R-QC2V-YU7G.
func FingerprintText(fp []byte) string {
	s := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(fp)
	var groups []string
	for len(s) > 4 {
		groups = append(groups, s[:4])
		s = s[4:]
	}
	if s != "" {
		groups = append(groups, s)
	}
	return strings.Join(groups, "-")
}

// KeyFingerprint is FingerprintText of a signing key's fingerprint: what an
// admin console shows.
func KeyFingerprint(signKey []byte) string { return FingerprintText(Fingerprint(signKey)) }

// Signer is the private half of a server's identity. secret.Keys is one.
type Signer interface {
	Sign(msg []byte) []byte
	SignPublic() ed25519.PublicKey
	BoxPublic() []byte
	OpenBoxBetween(sealed []byte, from, to string) ([]byte, error)
}

// verify checks an Ed25519 signature and never panics on a key of the wrong
// length, which a peer could send.
func verify(pub, msg, sig []byte) bool {
	return len(pub) == ed25519.PublicKeySize && len(sig) == ed25519.SignatureSize && ed25519.Verify(pub, msg, sig)
}
