package store

import (
	"context"

	"github.com/tikaydaes/churr/internal/model"
)

// Scheduled is the `scheduled` and `saved_replies` tables.
//
// An item's state is read from its columns (docs/SCHEMA.md section 8):
// cancelled when cancelled_at is set, failed when failed_at is set, waiting
// when next_run_at is set, done otherwise.
type Scheduled interface {
	// Create stores a scheduled item. An empty ID and zero CreatedAt are
	// filled in.
	Create(ctx context.Context, s *model.ScheduledItem) error
	// Get returns one item, cancelled ones included.
	Get(ctx context.Context, id string) (model.ScheduledItem, error)
	// List returns items that are not cancelled, newest first.
	List(ctx context.Context, f ScheduledFilter, p Page) (items []model.ScheduledItem, next string, err error)
	// Update reads an item (locking it on PostgreSQL), lets fn change its
	// owner, content, about-message, next run, repeat, last run, failure
	// and cancelled time, and writes it back.
	Update(ctx context.Context, id string, fn func(s *model.ScheduledItem) error) (model.ScheduledItem, error)
	// Delete removes an item for good.
	Delete(ctx context.Context, id string) error
	// CountWaiting returns how many items of an account are waiting: not
	// cancelled, with a next run.
	CountWaiting(ctx context.Context, account model.AccountID) (int, error)
	// Due returns waiting items whose next run is at or before now,
	// earliest first.
	Due(ctx context.Context, now model.Time, limit int) ([]model.ScheduledItem, error)
	// NextRun returns the earliest next run among waiting items. found is
	// false if nothing is waiting.
	NextRun(ctx context.Context) (at model.Time, found bool, err error)
	// DeleteFinishedBefore removes items that were cancelled before the
	// given moment, or that are done and last ran before it, and returns
	// how many went. A failed item stays until its owner deletes it.
	DeleteFinishedBefore(ctx context.Context, before model.Time) (int64, error)

	// CreateReply stores a saved reply; an empty OwnerID makes a
	// community one. An empty ID and zero CreatedAt are filled in.
	// model.ErrNameTaken if the owner (or the community) already has one
	// of that name.
	CreateReply(ctx context.Context, r *model.SavedReply) error
	// Reply returns one saved reply.
	Reply(ctx context.Context, id string) (model.SavedReply, error)
	// UpdateReply changes a saved reply's name and text.
	// model.ErrNameTaken as for CreateReply.
	UpdateReply(ctx context.Context, id, name, text string) (model.SavedReply, error)
	// DeleteReply removes a saved reply. model.ErrNotFound if there is
	// none.
	DeleteReply(ctx context.Context, id string) error
	// Replies returns the community's saved replies and one account's
	// own, by name, the community's first where names are equal. An empty
	// owner lists only the community's.
	Replies(ctx context.Context, owner model.AccountID) ([]model.SavedReply, error)
	// ReplyNamed finds a saved reply by name among one account's own, or
	// among the community's when owner is empty.
	ReplyNamed(ctx context.Context, owner model.AccountID, name string) (r model.SavedReply, found bool, err error)
}

// ScheduledFilter narrows Scheduled.List. Empty fields match everything.
type ScheduledFilter struct {
	AccountID      model.AccountID
	ConversationID model.ConversationID
	Kind           string
	// AlsoForms widens AccountID: recurring forms are listed whoever
	// owns them. A channel's managers see its recurring forms this way,
	// with ConversationID set.
	AlsoForms bool
}
