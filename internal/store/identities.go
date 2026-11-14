package store

import (
	"context"

	"github.com/tikaydaes/churr/internal/model"
)

// Identities is the `sso_providers`, `identities`, `totp`, `oauth_codes`
// and `import_claims` tables: everything company sign-in, the authenticator
// app and app sign-in store (docs/AUTH.md sections 7, 9 and 11). Whether a
// sign-in identity is banned is Accounts().IdentityBanned.
type Identities interface {
	// CreateProvider stores a company sign-in provider. An empty ID and
	// zero CreatedAt are filled in.
	CreateProvider(ctx context.Context, p *model.SsoProvider) error
	// GetProvider returns one provider, with its sealed secret.
	GetProvider(ctx context.Context, id string) (model.SsoProvider, error)
	// ListProviders returns every provider, oldest first.
	ListProviders(ctx context.Context) ([]model.SsoProvider, error)
	// UpdateProvider writes back name, config, secret, group claim, admin
	// group and enabled. The kind never changes.
	UpdateProvider(ctx context.Context, p model.SsoProvider) error
	// DeleteProvider removes a provider; its identities go with it by
	// cascade, and groups filled from it keep their members.
	DeleteProvider(ctx context.Context, id string) error

	// Link stores an account's identity at a provider.
	// model.ErrAlreadyExists if the subject is already linked, to this
	// account or another: a subject belongs to one account.
	Link(ctx context.Context, i model.Identity) error
	// BySubject finds the identity for a provider's subject.
	BySubject(ctx context.Context, provider, subject string) (model.Identity, error)
	// ForAccount returns an account's identities with their providers'
	// names, oldest first.
	ForAccount(ctx context.Context, account model.AccountID) ([]model.Identity, error)
	// CountForAccount returns how many identities an account has.
	CountForAccount(ctx context.Context, account model.AccountID) (int64, error)
	// Touch records a sign-in through an identity.
	Touch(ctx context.Context, provider, subject string, at model.Time) error
	// LastUsed returns the account's newest identities.last_used_at, or
	// zero if it never signed in through a provider (ruling R42).
	LastUsed(ctx context.Context, account model.AccountID) (model.Time, error)

	// PutTotp stores a sealed authenticator secret for an account,
	// unconfirmed, replacing any earlier row.
	PutTotp(ctx context.Context, account model.AccountID, sealed []byte) error
	// GetTotp returns the account's row. found is false if it has none.
	GetTotp(ctx context.Context, account model.AccountID) (t Totp, found bool, err error)
	// ConfirmTotp marks an unconfirmed row confirmed and records the time
	// step of the code that proved it. ok is false if there was no
	// unconfirmed row.
	ConfirmTotp(ctx context.Context, account model.AccountID, at model.Time, step int64) (ok bool, err error)
	// AdvanceTotp records an accepted time step with an update that only
	// succeeds while the stored step is smaller, so a code works once.
	AdvanceTotp(ctx context.Context, account model.AccountID, step int64) (ok bool, err error)
	// DeleteTotp removes the account's row. ok is false if it had none.
	DeleteTotp(ctx context.Context, account model.AccountID) (ok bool, err error)
	// DeleteTotpUnconfirmed deletes rows that were stored before the given
	// moment and never confirmed, for the janitor: a set-up that was begun
	// and abandoned leaves no secret behind. It returns how many went.
	DeleteTotpUnconfirmed(ctx context.Context, before model.Time) (int64, error)

	// CreateCode stores a one-time authorization or pairing code by its
	// hash. A pairing code starts with no account and no client name.
	CreateCode(ctx context.Context, hash []byte, c OAuthCode) error
	// GetCode returns a code's row, whatever its state.
	GetCode(ctx context.Context, hash []byte) (OAuthCode, error)
	// UseCode marks a code used with an update that only succeeds while
	// it is unused, unexpired and (for a pairing code) confirmed, and
	// returns the row. ok is false otherwise.
	UseCode(ctx context.Context, hash []byte, at model.Time) (c OAuthCode, ok bool, err error)
	// ClaimPairing records the device that scanned a pairing code. Only
	// the first device to post an unexpired, unclaimed code is recorded;
	// ok is false for any other.
	ClaimPairing(ctx context.Context, hash []byte, clientName, platform string, boxKey []byte, at model.Time) (ok bool, err error)
	// ConfirmPairing sets the account on a claimed, unconfirmed,
	// unexpired pairing code. ok is false otherwise.
	ConfirmPairing(ctx context.Context, hash []byte, account model.AccountID, at model.Time) (ok bool, err error)
	// DeleteCodesEnded deletes codes that expired before the given
	// moment, for the janitor.
	DeleteCodesEnded(ctx context.Context, before model.Time) (int64, error)

	// AddImportClaim lets company sign-in claim an imported account by
	// the keyed hash of its email address.
	AddImportClaim(ctx context.Context, account model.AccountID, emailHash []byte) error
	// HasImportClaims reports whether any imported account still waits.
	HasImportClaims(ctx context.Context) (bool, error)
	// TakeImportClaim deletes the claim with this hash and returns its
	// account. found is false if there is none.
	TakeImportClaim(ctx context.Context, emailHash []byte) (account model.AccountID, found bool, err error)
}

// Totp is one row of `totp`.
type Totp struct {
	AccountID model.AccountID
	// Secret is sealed (docs/AUTH.md section 1).
	Secret      []byte
	ConfirmedAt model.Time
	// LastStep is the last accepted time step; 0 before the first.
	LastStep int64
}

// The kinds of one-time code in `oauth_codes`.
const (
	CodeAuthorization = "authorization"
	CodePairing       = "pairing"
)

// OAuthCode is one row of `oauth_codes`.
type OAuthCode struct {
	// AccountID is empty on a pairing code until the person confirms.
	AccountID     model.AccountID
	Kind          string
	ClientName    string
	RedirectURI   string
	PKCEChallenge string
	Platform      string
	// BoxKey and Handover belong to the sealed hand-over; the store keeps
	// them as bytes it cannot read.
	BoxKey    []byte
	Handover  []byte
	ExpiresAt model.Time
	UsedAt    model.Time
}
