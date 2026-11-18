package wire

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/tikaydaes/churr/internal/model"
)

// The two kinds of party on a link.
const (
	KindServer = "server"
	KindHub    = "hub"
)

// Identity is who this program is on the link: its name, its kind and its
// keys. A server and a hub alike publish a box key: a hub is sealed the
// direct messages that ask it for a channel's key (section 7).
type Identity struct {
	Name string
	Kind string
	Keys Signer
	// URL is where others dial this party: wss://name/link/v1.
	URL string
	// Software is what the hello says, such as churr/1.0.0.
	Software string
}

// WellKnown is the document served at /.well-known/churr (section 3).
type WellKnown struct {
	Churr     int           `json:"churr"`
	Kind      string        `json:"kind"`
	Name      string        `json:"name"`
	SignKey   Bytes         `json:"sign_key"`
	BoxKey    Bytes         `json:"box_key,omitempty"`
	BoxKeySig Bytes         `json:"box_key_sig,omitempty"`
	Link      WellKnownLink `json:"link"`
}

// WellKnownLink says which protocol versions are spoken and where.
type WellKnownLink struct {
	Versions []int  `json:"versions"`
	URL      string `json:"url"`
}

// boxKeyMessage is what box_key_sig signs:
// "churr-box-key-v1" 0x00 || lp(name) || box_key.
func boxKeyMessage(name string, boxKey []byte) []byte {
	var l layout
	l.label("churr-box-key-v1").lp(name).raw(boxKey)
	return l.Bytes()
}

// BoxKeySig signs this identity's box key with its signing key, which ties
// the two together so that pinning the signing key pins both.
func (id Identity) BoxKeySig() []byte {
	return id.Keys.Sign(boxKeyMessage(id.Name, id.Keys.BoxPublic()))
}

// VerifyBoxKey checks a box_key_sig.
func VerifyBoxKey(name string, signKey, boxKey, sig []byte) bool {
	return len(boxKey) == 32 && verify(signKey, boxKeyMessage(name, boxKey), sig)
}

// WellKnown builds the published document.
func (id Identity) WellKnown() WellKnown {
	w := WellKnown{Churr: 1, Kind: id.Kind, Name: id.Name, SignKey: Bytes(id.Keys.SignPublic()),
		Link: WellKnownLink{Versions: []int{Version}, URL: id.URL}}
	w.BoxKey = id.Keys.BoxPublic()
	w.BoxKeySig = id.BoxKeySig()
	return w
}

// Check says whether a fetched document is sound and belongs to host.
func (w WellKnown) Check(host string) error {
	switch {
	case w.Churr != 1:
		return errors.New("wire: unknown document format")
	case w.Name != host:
		return fmt.Errorf("wire: the document names %q, not the host it came from", w.Name)
	case len(w.SignKey) != 32:
		return errors.New("wire: the signing key is not 32 bytes")
	case !VerifyBoxKey(w.Name, w.SignKey, w.BoxKey, w.BoxKeySig):
		return errors.New("wire: the box key signature does not verify")
	case w.Kind != KindServer && w.Kind != KindHub:
		return errors.New("wire: unknown kind")
	}
	return nil
}

// InvitePrefix begins every invitation string (ruling R24).
const InvitePrefix = "churr_link_"

// Invitation is the one string an owner hands to another (section 4).
type Invitation struct {
	V         int        `json:"v"`
	Kind      string     `json:"kind"`
	Name      string     `json:"name"`
	URL       string     `json:"url"`
	FP        Bytes      `json:"fp"`
	Secret    Bytes      `json:"secret"`
	ExpiresAt model.Time `json:"expires_at"`
	For       *string    `json:"for"`
}

// String is the invitation as pasted: the prefix and the base64url of its
// JSON.
func (inv Invitation) String() string {
	b, _ := json.Marshal(inv)
	return InvitePrefix + Encode(b)
}

// ParseInvitation reads an invitation string and checks its shape. It does
// not check the expiry; the caller has the clock.
func ParseInvitation(s string) (Invitation, error) {
	var inv Invitation
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, InvitePrefix) {
		return inv, errors.New("an invitation starts with churr_link_")
	}
	raw, err := Decode(strings.TrimPrefix(s, InvitePrefix))
	if err != nil {
		return inv, errors.New("the invitation is damaged")
	}
	if err := json.Unmarshal(raw, &inv); err != nil {
		return inv, errors.New("the invitation is damaged")
	}
	switch {
	case inv.V != 1:
		return inv, errors.New("the invitation is of a format this server does not know")
	case inv.Kind != KindServer && inv.Kind != KindHub:
		return inv, errors.New("the invitation names neither a server nor a hub")
	case inv.Name == "" || inv.URL == "" || len(inv.FP) != 20 || len(inv.Secret) != 32 || inv.ExpiresAt.IsZero():
		return inv, errors.New("the invitation is incomplete")
	}
	return inv, nil
}

// InviteHash is what the issuer keeps of an invitation: SHA-256(secret).
func InviteHash(secret []byte) []byte {
	sum := sha256.Sum256(secret)
	return sum[:]
}

// InviteProof is HMAC-SHA-256 with key SHA-256(secret) over
// "churr-link-invite-v1" 0x00 || th. The issuer, who kept only the hash,
// can check it; an eavesdropper who saw it can use it on no other
// connection, because th is this handshake's transcript.
func InviteProof(inviteHash, th []byte) []byte {
	var l layout
	l.label("churr-link-invite-v1").raw(th)
	m := hmac.New(sha256.New, inviteHash)
	m.Write(l.Bytes())
	return m.Sum(nil)
}

// ProofMatches compares a proof in constant time.
func ProofMatches(inviteHash, th, proof []byte) bool {
	return hmac.Equal(InviteProof(inviteHash, th), proof)
}
