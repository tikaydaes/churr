package store

import (
	"context"

	"github.com/tikaydaes/churr/internal/model"
)

// Bans is the `bans` table, and what admins see about an account's sign-in.
// It is part of the Accounts area, because a ban is a removal plus rows that
// stop the same person coming back (docs/FEATURES.md section 19).
type Bans interface {
	// Ban removes an account as Accounts.Remove does and, in the same
	// transaction, writes its ban rows: one per identity the account had
	// in `identities`, with the provider and subject copied before those
	// rows are deleted, or one row with neither for an account that had
	// none. model.ErrNotFound if the account does not exist. An account
	// that is already removed is not removed again; it still gets its
	// rows (one with neither, since its identities are gone).
	Ban(ctx context.Context, account, by model.AccountID, reason string, at model.Time) ([]model.Ban, RemovedAccount, error)
	// BanIdentity bans a sign-in identity that has no account. An empty ID
	// and zero CreatedAt are filled in.
	BanIdentity(ctx context.Context, b *model.Ban) error
	// GetBan returns one ban.
	GetBan(ctx context.Context, id string) (model.Ban, error)
	// ListBans returns bans newest first, lifted ones included.
	ListBans(ctx context.Context, p Page) (bans []model.Ban, next string, err error)
	// LiftBan sets lifted_at. model.ErrNotFound if there is no such ban;
	// lifting one that is already lifted changes nothing.
	LiftBan(ctx context.Context, id string, at model.Time) error
	// IdentityBanned reports whether a ban that is not lifted names this
	// provider and subject. Sign-in and registration ask it.
	IdentityBanned(ctx context.Context, providerID, subject string) (bool, error)

	// AdminDetails returns what admins see beside each of the accounts
	// given: sign-in details and whether a ban that is not lifted names
	// it. An account without an entry in the result has none of them.
	AdminDetails(ctx context.Context, ids []model.AccountID) (map[model.AccountID]AdminDetail, error)
	// ClaimLinkFor withdraws the unused claim links of an unclaimed
	// imported account and stores the new one given, together.
	// model.ErrConflict if the account is claimed, removed or not local.
	ClaimLinkFor(ctx context.Context, i *model.Invite) error
	// SharedWith returns the accounts that are in at least one
	// conversation with this one, the account itself left out: who is
	// told about its presence, status and profile.
	SharedWith(ctx context.Context, account model.AccountID) ([]model.AccountID, error)
	// Timed returns the accounts, not removed, whose shown state can
	// change with the clock alone: a status, an availability, appear away
	// or a manual do not disturb that has an end, a quiet-hours schedule,
	// or a skip. The sweep that announces such changes reads it.
	Timed(ctx context.Context) ([]model.Account, error)
}

// AdminDetail is what admins see about one account's sign-in. Never its
// sessions.
type AdminDetail struct {
	PasskeyCount int64
	HasTotp      bool
	ProviderIDs  []string
	LastSeenAt   model.Time
	Banned       bool
}
