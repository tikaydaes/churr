package store

import (
	"context"

	"github.com/tikaydaes/churr/internal/model"
)

// Accounts is the `accounts` table: every person, bot, webhook account and
// remote shadow, with profile, status, availability and do not disturb.
//
// An account row is never deleted (docs/SCHEMA.md decision S2); removal is a
// state. Usernames are stored lower-case; the caller lower-cases them.
type Accounts interface {
	// Create inserts a new account. An empty ID, a zero CreatedAt and, for
	// a local account, a zero ClaimedAt are filled in (pass a ClaimedAt of
	// 0 through CreateUnclaimed for an imported account). A local username
	// must be free among local usernames and group handles and must not be
	// reserved, or the answer is model.ErrNameTaken; a remote username
	// must be free on its server. An account is created owner only while
	// the instance has none, by first-run setup (model.ErrConflict
	// otherwise); later owners are made by SetRole.
	//
	// A new owner, admin or member is at once a member of every channel
	// the built-in groups are in; those joins are returned.
	Create(ctx context.Context, a *model.Account) (MembershipResult, error)
	// CreateUnclaimed is Create for an account made by the importer:
	// ClaimedAt stays zero, so it is in no built-in group yet.
	CreateUnclaimed(ctx context.Context, a *model.Account) error
	// Claim marks an imported account claimed, which puts it in the
	// built-in groups its role belongs to.
	Claim(ctx context.Context, id model.AccountID, at model.Time) (MembershipResult, error)

	// Get returns one account, removed ones included.
	Get(ctx context.Context, id model.AccountID) (model.Account, error)
	// GetMany returns the accounts that exist among ids, in no set order.
	GetMany(ctx context.Context, ids []model.AccountID) ([]model.Account, error)
	// NotifyFacts returns, for each id in order, the account with only
	// the columns the notification rule reads filled in (its role,
	// whether it is active, and what decides do not disturb), or nil if
	// there is no such account. They come from memory once read
	// (roster.go) and are shared: read them only.
	NotifyFacts(ctx context.Context, ids []model.AccountID) ([]*model.Account, error)
	// ByUsername finds a local account by its username.
	ByUsername(ctx context.Context, username string) (model.Account, error)
	// ByWebAuthnHandle finds the account a passkey's user handle names.
	ByWebAuthnHandle(ctx context.Context, handle []byte) (model.Account, error)
	// Owner returns the first owner, the one setup made unless it has
	// stepped down. found is false before first-run setup.
	Owner(ctx context.Context) (a model.Account, found bool, err error)
	// List returns accounts ordered by username, then id.
	List(ctx context.Context, f AccountFilter, p Page) (accounts []model.Account, next string, err error)

	// Update reads the account, lets fn change it, and writes back the
	// columns a person or the server may change: username (checked as in
	// Create), display name, avatar, title, pronouns, time zone, WebAuthn
	// handle, profile version, status, availability, appear away, do not
	// disturb, quiet hours, allow_notify_anyway and prefs. Role, claiming
	// and removal have their own methods and are ignored here. If fn
	// returns an error nothing is written and that error is returned.
	Update(ctx context.Context, id model.AccountID, fn func(a *model.Account) error) (model.Account, error)
	// SetRole changes the role of a person's account between owner,
	// admin, member and guest (model.ErrConflict for any other) and works
	// out its channel memberships through the built-in groups again. The
	// instance's last active owner cannot be given another role
	// (model.ErrConflict): there is always an owner. Who may ask is
	// perm's (CheckSetRole).
	SetRole(ctx context.Context, id model.AccountID, role model.Role) (model.Account, MembershipResult, error)
	// Remove carries out account removal as docs/SCHEMA.md section 14.3
	// lists it, in one go: marks the account removed, clears its profile,
	// deletes its credentials and personal rows, takes it out of every
	// group and conversation, and applies the last-manager and last-member
	// rules to each conversation it was in. The account row, its messages
	// and what it did stay. model.ErrConflict if it is already removed.
	Remove(ctx context.Context, id, by model.AccountID, at model.Time) (RemovedAccount, error)

	// SharesConversation reports whether two accounts are members of at
	// least one conversation together, which is what lets a guest or bot
	// see another account.
	SharesConversation(ctx context.Context, a, b model.AccountID) (bool, error)

	// Bans, what admins see of an account, and who an account shares a
	// conversation with: bans.go.
	Bans
}

// AccountFilter narrows Accounts.List. Zero fields do not filter.
type AccountFilter struct {
	// Query matches a substring of the username or display name, without
	// regard to case.
	Query string
	Role  model.Role
	// GroupID keeps only members of this group (built-in groups included).
	GroupID model.GroupID
	// IDs keeps only these accounts.
	IDs []model.AccountID
	// IncludeRemoved also returns former members.
	IncludeRemoved bool
	// State keeps only accounts in one state: "active" (claimed and not
	// removed), "removed" or "unclaimed" (imported and not yet claimed,
	// and not removed). "removed" implies IncludeRemoved.
	State string
	// SharedWith keeps only accounts that share a conversation with this
	// one (and the account itself): what a guest or bot can see.
	SharedWith model.AccountID
	// LocalOrSharedWith keeps local accounts, and remote accounts only if
	// they share a conversation with this account: what an owner, admin
	// or member can see.
	LocalOrSharedWith model.AccountID
}

// RemovedAccount is what Accounts.Remove did, so the service can close
// connections, send events and remove blobs.
type RemovedAccount struct {
	Account model.Account
	// SessionIDs are the sessions that were deleted; their live
	// connections must be closed.
	SessionIDs []model.SessionID
	// BlobKeys are the blobs of the account's avatar, to remove from the
	// file store.
	BlobKeys []string
	MembershipResult
}
