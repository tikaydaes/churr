package push

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"io"
)

// Web Push message encryption (RFC 8291, with the aes128gcm content coding
// of RFC 8188), for a UnifiedPush distributor that accepts nothing else.
// The device registered a P-256 public key and a 16-byte shared secret.

// webPushRecordSize is the record size written in the header. The wake-up
// is far smaller than one record.
const webPushRecordSize = 4096

// webPushEncrypt encrypts body to a device's public key and shared secret
// and returns the whole request body: the aes128gcm header (salt, record
// size, the sender's one-time public key) followed by the one record.
func webPushEncrypt(rnd io.Reader, uaPublic, auth, body []byte) ([]byte, error) {
	ua, err := ecdh.P256().NewPublicKey(uaPublic)
	if err != nil {
		return nil, errors.New("push: the device's Web Push key is not a P-256 public key")
	}
	if len(body)+17 > webPushRecordSize {
		return nil, errors.New("push: the wake-up is too large for one Web Push record")
	}
	as, err := ecdh.P256().GenerateKey(rnd)
	if err != nil {
		return nil, err
	}
	salt := make([]byte, 16)
	if _, err := io.ReadFull(rnd, salt); err != nil {
		return nil, err
	}
	shared, err := as.ECDH(ua)
	if err != nil {
		return nil, err
	}
	asPublic := as.PublicKey().Bytes()
	key, nonce, err := webPushKeys(shared, auth, uaPublic, asPublic, salt)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	out := make([]byte, 0, 16+4+1+len(asPublic)+len(body)+1+16)
	out = append(out, salt...)
	out = binary.BigEndian.AppendUint32(out, webPushRecordSize)
	out = append(out, byte(len(asPublic)))
	out = append(out, asPublic...)
	// The padding delimiter of the last record is 0x02.
	return gcm.Seal(out, nonce, append(append([]byte{}, body...), 2), nil), nil
}

// webPushKeys derives the content encryption key and nonce of RFC 8291
// section 3.4 from the ECDH secret, the shared secret, the two public keys
// and the salt.
func webPushKeys(shared, auth, uaPublic, asPublic, salt []byte) (key, nonce []byte, err error) {
	info := append(append([]byte("WebPush: info\x00"), uaPublic...), asPublic...)
	ikm, err := hkdf.Key(sha256.New, shared, auth, string(info), 32)
	if err != nil {
		return nil, nil, err
	}
	if key, err = hkdf.Key(sha256.New, ikm, salt, "Content-Encoding: aes128gcm\x00", 16); err != nil {
		return nil, nil, err
	}
	nonce, err = hkdf.Key(sha256.New, ikm, salt, "Content-Encoding: nonce\x00", 12)
	return key, nonce, err
}
