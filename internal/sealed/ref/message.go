package ref

import (
	"crypto/ed25519"
	"encoding/binary"
	"errors"
	"fmt"
)

// The sealed message of docs/SEALED.md section 8.1: an 82-byte header, a
// 16-byte salt, the encrypted content with its tag, and the sending
// device's signature.

const (
	// HeaderSize is bytes 0 to 81 of a sealed message.
	HeaderSize = 82
	// MaxContent is the most content one message may carry.
	MaxContent = 128 * 1024
	// FlagQuiet is bit 0 of the flags: not an ordinary message, so it does
	// not notify or count as unread.
	FlagQuiet byte = 1
)

// Header is the readable front of a sealed message.
type Header struct {
	Format         byte
	Flags          byte
	ConversationID UUID
	KeyEpoch       uint64
	AccountID      UUID // the sender's account
	DeviceID       UUID // the sender's device
	Counter        uint64
	Reference      UUID // also sent to the server as the message's client_id
}

// Quiet reports whether the quiet flag is set.
func (h Header) Quiet() bool { return h.Flags&FlagQuiet != 0 }

// Encode writes the 82 bytes.
func (h Header) Encode() []byte {
	b := make([]byte, 0, HeaderSize)
	b = append(b, h.Format, h.Flags)
	b = append(b, h.ConversationID[:]...)
	b = binary.BigEndian.AppendUint64(b, h.KeyEpoch)
	b = append(b, h.AccountID[:]...)
	b = append(b, h.DeviceID[:]...)
	b = binary.BigEndian.AppendUint64(b, h.Counter)
	return append(b, h.Reference[:]...)
}

// ErrMessage is the refusal of a sealed message. A message that fails is
// shown as "A message could not be checked" and its content is never drawn.
var ErrMessage = errors.New("ref: the message could not be checked")

func messageErr(why string) error { return fmt.Errorf("%w: %s", ErrMessage, why) }

// DecodeHeader reads the header of a sealed message.
func DecodeHeader(sealed []byte) (Header, error) {
	var h Header
	if len(sealed) < HeaderSize {
		return h, messageErr("it is shorter than a header")
	}
	h.Format, h.Flags = sealed[0], sealed[1]
	copy(h.ConversationID[:], sealed[2:18])
	h.KeyEpoch = binary.BigEndian.Uint64(sealed[18:26])
	copy(h.AccountID[:], sealed[26:42])
	copy(h.DeviceID[:], sealed[42:58])
	h.Counter = binary.BigEndian.Uint64(sealed[58:66])
	copy(h.Reference[:], sealed[66:82])
	if h.Format != 1 {
		return h, messageErr("its format is not 1")
	}
	if h.Flags&^FlagQuiet != 0 {
		return h, messageErr("it sets flags that do not exist")
	}
	return h, nil
}

// SealMessage encrypts and signs one message.
//
// senderKey is the sending device's sender key for the header's epoch,
// salt 16 random bytes, content the UTF-8 JSON object of section 8.3. The
// message key and nonce are HKDF(sender key, salt, "churr lock message v1"
// || 0x00 || header, 44), so no AES-GCM key ever encrypts two messages.
func SealMessage(h Header, senderKey, salt, content []byte, sign ed25519.PrivateKey) ([]byte, error) {
	if len(content) > MaxContent {
		return nil, fmt.Errorf("ref: a message's content is at most 128 KB")
	}
	header := h.Encode()
	body := append(header, Lock(senderKey, LabelLockMessage, header, content, salt)...)
	return append(body, Sign(sign, LabelMessage, body)...), nil
}

// OpenMessage checks the signature of a sealed message under the sending
// device's signing key and decrypts its content. It makes none of the
// checks that need the row or the history; Chat.Receive does those.
func OpenMessage(sealed, senderKey, signKey []byte) (Header, []byte, error) {
	h, err := DecodeHeader(sealed)
	if err != nil {
		return h, nil, err
	}
	if len(sealed) < HeaderSize+SaltSize+16+SignatureSize {
		return h, nil, messageErr("it is too short")
	}
	body, sig := sealed[:len(sealed)-SignatureSize], sealed[len(sealed)-SignatureSize:]
	if !Verify(signKey, LabelMessage, body, sig) {
		return h, nil, messageErr("the signature does not verify")
	}
	content, err := Unlock(senderKey, LabelLockMessage, body[:HeaderSize], body[HeaderSize:])
	if err != nil {
		return h, nil, messageErr("the content does not decrypt")
	}
	return h, content, nil
}
