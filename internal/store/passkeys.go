package store

import (
	"context"
	"encoding/json"

	"github.com/tikaydaes/churr/internal/model"
)

// Passkeys is the `passkeys` and `access_keys` tables: the two ways a person
// signs in by themselves.
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

	// SetAccessKey stores an account's access key by the hash of its
	// sign-in value, replacing the one it had, if any.
	SetAccessKey(ctx context.Context, account model.AccountID, authHash []byte, at model.Time) error
	// AccessKey returns an account's access key; found is false when it
	// has none.
	AccessKey(ctx context.Context, account model.AccountID) (key AccessKey, found bool, err error)
	// AccessKeyUsed records a sign-in, check or unlock with the key.
	AccessKeyUsed(ctx context.Context, account model.AccountID, at model.Time) error
	// DeleteAccessKey removes an account's access key. removed is false
	// when it had none.
	DeleteAccessKey(ctx context.Context, account model.AccountID) (removed bool, err error)
}

// AccessKey is an account's access key as stored: never the key, only the
// hash of the value derived from it (docs/AUTH.md section 5.6).
type AccessKey struct {
	AuthHash   []byte
	CreatedAt  model.Time
	LastUsedAt model.Time
}
