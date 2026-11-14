package model

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
)

// Bytes is a byte string that is written in JSON as base64url without
// padding, which is how every key, record and ciphertext of sealed chats and
// devices travels (docs/SEALED.md section 2.2). encoding/json would write a
// plain []byte as padded standard base64.
type Bytes []byte

// EncodeBytes writes bytes the way JSON carries them here.
func EncodeBytes(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

// DecodeBytes reads base64url. Padding is accepted on the way in, since some
// libraries add it; nothing else is.
func DecodeBytes(s string) ([]byte, error) {
	b, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(s, "="))
	if err != nil {
		return nil, fmt.Errorf("model: bytes must be base64url")
	}
	return b, nil
}

// MarshalJSON writes base64url without padding.
func (b Bytes) MarshalJSON() ([]byte, error) {
	return json.Marshal(EncodeBytes(b))
}

// UnmarshalJSON reads base64url. null leaves the value empty.
func (b *Bytes) UnmarshalJSON(raw []byte) error {
	if string(raw) == "null" {
		*b = nil
		return nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return fmt.Errorf("model: bytes must be a base64url string")
	}
	out, err := DecodeBytes(s)
	if err != nil {
		return err
	}
	*b = out
	return nil
}

// The types below hold their bytes as plain []byte, because the storage
// layer scans straight into them. These methods give them the same JSON
// form as Bytes.

// MarshalJSON writes the ciphertext as base64url.
func (p SealedPayload) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		KeyEpoch   int64 `json:"key_epoch"`
		Ciphertext Bytes `json:"ciphertext"`
	}{p.KeyEpoch, p.Ciphertext})
}

// UnmarshalJSON reads the ciphertext from base64url. Unknown fields are
// refused, as for any request body.
func (p *SealedPayload) UnmarshalJSON(raw []byte) error {
	var in struct {
		KeyEpoch   *int64 `json:"key_epoch"`
		Ciphertext *Bytes `json:"ciphertext"`
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		return err
	}
	if in.KeyEpoch == nil || in.Ciphertext == nil {
		return fmt.Errorf("model: sealed needs key_epoch and ciphertext")
	}
	p.KeyEpoch, p.Ciphertext = *in.KeyEpoch, *in.Ciphertext
	return nil
}

// MarshalJSON writes the device's two public keys as base64url.
func (d Device) MarshalJSON() ([]byte, error) {
	type plain Device
	return json.Marshal(struct {
		plain
		SignKey Bytes `json:"sign_key,omitempty"`
		BoxKey  Bytes `json:"box_key,omitempty"`
	}{plain(d), d.SignKey, d.BoxKey})
}

// MarshalJSON writes the unlock public key as base64url.
func (p Passkey) MarshalJSON() ([]byte, error) {
	type plain Passkey
	return json.Marshal(struct {
		plain
		UnlockPublic Bytes `json:"unlock_public,omitempty"`
	}{plain(p), p.UnlockPublic})
}
