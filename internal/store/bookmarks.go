package store

import (
	"context"

	"github.com/tikaydaes/churr/internal/model"
)

// Bookmarks is the `bookmarks` table: a person's saved messages. Rows go
// when unsaved, with the message, and with the account.
type Bookmarks interface {
	// Add saves a message for an account and returns the row. Saving one
	// that is already saved keeps the first row and its time; added says
	// whether a row was written. model.ErrNotFound if the message does not
	// exist.
	Add(ctx context.Context, account model.AccountID, msg model.MessageID, at model.Time) (b model.Bookmark, added bool, err error)
	// Remove unsaves a message and reports whether there was a row.
	Remove(ctx context.Context, account model.AccountID, msg model.MessageID) (removed bool, err error)
	// List returns an account's bookmarks newest first, leaving out
	// messages outside the scope: ones in conversations the account can
	// no longer read. Those rows are kept, not shown (docs/SCHEMA.md).
	// Message is not filled in; the caller loads the messages.
	List(ctx context.Context, account model.AccountID, scope Scope, p Page) (bookmarks []model.Bookmark, next string, err error)
}
