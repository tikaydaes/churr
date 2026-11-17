package store

import (
	"context"

	"github.com/tikaydaes/churr/internal/model"
)

// Recovery is the `recovery_codes` and `recovery_links` tables.
type Recovery interface {
	// ReplaceCodes deletes an account's recovery codes and stores a new
	// set, given as hashes.
	ReplaceCodes(ctx context.Context, account model.AccountID, hashes [][]byte) error
	// UseCode marks a code used, with an update that only succeeds while
	// it is unused. ok is false if no unused code has that hash.
	UseCode(ctx context.Context, account model.AccountID, hash []byte, at model.Time) (ok bool, err error)
	// CountCodes returns how many codes an account has and how many are
	// still unused.
	CountCodes(ctx context.Context, account model.AccountID) (total, unused int, err error)

	// CreateLink stores a one-time recovery link by the hash of its value.
	// createdBy is "" for the command line. Making a link withdraws the
	// account's earlier links that were never used, so only the newest one
	// works.
	CreateLink(ctx context.Context, tokenHash []byte, account, createdBy model.AccountID, expiresAt model.Time) error
	// UseLink marks a link used if it is unused and not expired, and
	// returns the account it recovers and who made the link ("" for the
	// command line). ok is false otherwise.
	UseLink(ctx context.Context, tokenHash []byte, at model.Time) (account, createdBy model.AccountID, ok bool, err error)
	// DeleteLinksEnded deletes links used or expired before the given
	// moment, for the janitor.
	DeleteLinksEnded(ctx context.Context, before model.Time) (int64, error)
}
