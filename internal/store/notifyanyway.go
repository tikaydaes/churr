package store

import (
	"context"

	"github.com/tikaydaes/churr/internal/model"
)

// NotifyAnyway is the `notify_anyway` table and the public mark on the
// message, `messages.notified_anyway`.
//
// `delivered` is for the server's own use (ruling R4). Nothing here may be
// returned by an API operation, put in an event or written to the export.
type NotifyAnyway interface {
	// Record sets the message's public mark and stores who it was pushed
	// through to, by whom, and whether the server then notified them.
	// Asking again for the same message and person changes nothing: the
	// first row stays as it was. marked says whether this call set the
	// mark (false if the message already had it); first says whether a row
	// was written. model.ErrNotFound if the message or the account does
	// not exist.
	Record(ctx context.Context, msg model.MessageID, target, by model.AccountID, at model.Time, delivered bool) (marked, first bool, err error)
	// Delivered reports whether a message was pushed through to this
	// account and it applied. The notification rule asks it; nothing else
	// may.
	Delivered(ctx context.Context, msg model.MessageID, target model.AccountID) (bool, error)
}
