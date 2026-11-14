package store

import (
	"context"

	"github.com/tikaydaes/churr/internal/model"
)

// Reactions is the `reactions` table: one emoji on one message by one
// account.
type Reactions interface {
	// Add stores a reaction. added is false if the account had already
	// reacted with that emoji. model.ErrConflict if it would be the
	// twenty-first different emoji on the message.
	Add(ctx context.Context, msg model.MessageID, account model.AccountID, emoji string, at model.Time) (added bool, err error)
	// Remove takes a reaction back. removed is false if there was none.
	Remove(ctx context.Context, msg model.MessageID, account model.AccountID, emoji string) (removed bool, err error)
	// List returns every reaction on a message, emoji in the order they
	// first appeared, each with all of its account ids in the order they
	// reacted, and Me for the reader.
	List(ctx context.Context, msg model.MessageID, reader model.AccountID) ([]model.Reaction, error)
	// One returns the message's entry for one emoji as a message carries
	// it: the count, at most the first ten account ids, and Me. A count of
	// 0 means the entry is gone.
	One(ctx context.Context, msg model.MessageID, emoji string, reader model.AccountID) (model.Reaction, error)
	// Has reports whether an account has reacted to a message with an
	// emoji.
	Has(ctx context.Context, msg model.MessageID, emoji string, account model.AccountID) (bool, error)
}
