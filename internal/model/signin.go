package model

import "encoding/json"

// SsoProvider is a company sign-in provider as an admin sees it (schema
// SsoProvider). The secret is write-only: only HasSecret is ever sent.
type SsoProvider struct {
	ID          string          `json:"id"`
	Kind        string          `json:"kind"` // oidc | saml
	Name        string          `json:"name"`
	Config      json.RawMessage `json:"config"`
	HasSecret   bool            `json:"has_secret"`
	GroupClaim  string          `json:"group_claim,omitempty"`
	AdminGroup  string          `json:"admin_group,omitempty"`
	Enabled     bool            `json:"enabled"`
	CallbackURL string          `json:"callback_url"`
	CreatedAt   Time            `json:"created_at"`

	// Secret is the sealed client secret or signing key, as stored.
	Secret []byte `json:"-"`
}

// The two kinds of company sign-in provider.
const (
	ProviderOIDC = "oidc"
	ProviderSAML = "saml"
)

// Identity links an account to its subject at a provider (schema Identity).
// The subject itself is never sent to the person.
type Identity struct {
	ProviderID   string `json:"provider_id"`
	ProviderName string `json:"provider_name"`
	LinkedAt     Time   `json:"linked_at"`
	LastUsedAt   Time   `json:"last_used_at,omitempty"`

	Subject   string    `json:"-"`
	AccountID AccountID `json:"-"`
}

// Totp is the state of the authenticator app on the caller's account
// (schema Totp).
type Totp struct {
	Enabled     bool `json:"enabled"`
	ConfirmedAt Time `json:"confirmed_at,omitempty"`
}

// AccessKey is the caller's access key (schema AccessKey): when it was made
// and last used. Never the key, which the server does not have.
type AccessKey struct {
	CreatedAt  Time `json:"created_at"`
	LastUsedAt Time `json:"last_used_at,omitempty"`
	// UnlockPublic is the public half of the key pair derived from the
	// key, to which its locked copy of the sealed-chat backup key is
	// sealed (docs/SEALED.md section 13.3).
	UnlockPublic Bytes `json:"unlock_public,omitempty"`
}

// SecondFactorState is what a pending session may give as its second step
// (schema SecondFactorState).
type SecondFactorState struct {
	Accepted []string `json:"accepted"`
	SetUp    []string `json:"set_up"`
}

// Integration is a credential for a program that works on the server as a
// whole (schema Integration).
type Integration struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Allows      []string  `json:"allows"`
	CreatedByID AccountID `json:"created_by_id"`
	CreatedAt   Time      `json:"created_at"`
	LastUsedAt  Time      `json:"last_used_at,omitempty"`
	RevokedAt   Time      `json:"revoked_at,omitempty"`
}

// Token is a personal or bot token as listed (schema Token).
type Token struct {
	ID         SessionID `json:"id"`
	Label      string    `json:"label"`
	CreatedAt  Time      `json:"created_at"`
	LastSeenAt Time      `json:"last_seen_at,omitempty"`
	ExpiresAt  Time      `json:"expires_at,omitempty"`
}

// The events of this area (docs/EVENTS.md section 9).
const (
	EventSessionCreated = "session.created"
	EventSessionEnded   = "session.ended"
	EventServerUpdated  = "server.updated"
	EventDeviceRetired  = "device.retired"
)
