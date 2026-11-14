package store

import (
	"context"
	"encoding/json"

	"github.com/tikaydaes/churr/internal/model"
)

// Passkeys is the `passkeys` table.
type Passkeys interface {
	// Add inserts a passkey. An empty ID and zero CreatedAt are filled in.
	// model.ErrAlreadyExists if the credential id is already registered.
	Add(ctx context.Context, p *model.Passkey) error
	// Get returns one passkey.
	Get(ctx context.Context, id model.PasskeyID) (model.Passkey, error)
	// ByCredentialID finds a passkey by the identifier the authenticator
	// gave it.
	ByCredentialID(ctx context.Context, credentialID []byte) (model.Passkey, error)
	// ForAccount returns an account's passkeys, oldest first.
	ForAccount(ctx context.Context, account model.AccountID) ([]model.Passkey, error)
	// Count returns how many passkeys an account has.
	Count(ctx context.Context, account model.AccountID) (int64, error)
	// Used records a successful use: the library's updated record (new
	// signature counter and flags) and the time.
	Used(ctx context.Context, id model.PasskeyID, credential json.RawMessage, at model.Time) error
	// Rename changes the name the person sees.
	Rename(ctx context.Context, id model.PasskeyID, name string) error
	// SetPRF records whether the passkey can unlock sealed history, and
	// the unlock public key derived from it once a page has given it
	// (nil leaves the key as it is).
	SetPRF(ctx context.Context, id model.PasskeyID, hasPRF bool, unlockPublic []byte) error
	// Delete removes a passkey; its locked copy of the backup key goes
	// with it by cascade.
	Delete(ctx context.Context, id model.PasskeyID) error
}
