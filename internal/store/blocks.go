package store

import (
	"context"

	"github.com/tikaydaes/churr/internal/model"
)

// Blocks is the `blocks` table: account_id has blocked blocked_id. Rows are
// deleted when unblocked and, in both directions, when either account is
// removed (Accounts.Remove does that).
type Blocks interface {
	// Add stores a block. Blocking someone already blocked changes nothing.
	Add(ctx context.Context, account, blocked model.AccountID, at model.Time) error
	// Remove deletes a block. Unblocking someone who is not blocked changes
	// nothing and is not an error.
	Remove(ctx context.Context, account, blocked model.AccountID) error
	// Has reports whether account has blocked blocked.
	Has(ctx context.Context, account, blocked model.AccountID) (bool, error)
	// List returns everyone an account has blocked, most recently blocked
	// first.
	List(ctx context.Context, account model.AccountID) ([]model.AccountID, error)
	// Blockers returns, among the accounts given, the ones that have
	// blocked `blocked`. It lets a fan-out leave out the people who do not
	// want to hear from someone with one query.
	Blockers(ctx context.Context, blocked model.AccountID, among []model.AccountID) ([]model.AccountID, error)
	// Blocking returns everyone an account has blocked, in no particular
	// order, kept in memory like BlockersOf: presence asks it of every
	// change it announces (ruling R77).
	Blocking(ctx context.Context, blocker model.AccountID) ([]model.AccountID, error)
	// BlockersOf returns everyone who has blocked an account.
	BlockersOf(ctx context.Context, blocked model.AccountID) ([]model.AccountID, error)
}
