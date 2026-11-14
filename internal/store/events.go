package store

import (
	"context"

	"github.com/tikaydaes/churr/internal/model"
)

// Events is the `events` table: the catch-up log, one row per event that
// belongs to a conversation, under the seq it was given. The payload column
// holds the event exactly as model.Event marshals it.
type Events interface {
	// Append writes an event whose Seq was taken with
	// Conversations.NextSeq in the same transaction as the change it
	// describes. Event.MessageID fills the message_id column. Only and
	// AlsoTo are not stored.
	Append(ctx context.Context, e model.Event) error
	// List returns events with seq greater than after and, if until is
	// not 0, at most until, oldest first, and whether more follow.
	List(ctx context.Context, conv model.ConversationID, after, until int64, limit int) (events []model.Event, hasMore bool, err error)
	// Oldest returns the lowest seq the log still holds for a
	// conversation, or 0 if it holds none.
	Oldest(ctx context.Context, conv model.ConversationID) (int64, error)
	// DeleteForMessage deletes every event that carries a message, so a
	// row never outlives the content it holds.
	DeleteForMessage(ctx context.Context, msg model.MessageID) error
	// DeleteBefore deletes events older than the given moment, for the
	// janitor.
	DeleteBefore(ctx context.Context, before model.Time) (int64, error)
}
