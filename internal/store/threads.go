package store

import (
	"context"

	"github.com/tikaydaes/churr/internal/model"
)

// Threads is the `thread_states` table: one row per account per thread it
// follows or ignores. No row means "neither".
//
// A ThreadState returned from here has ConversationID and UnreadCount filled
// in: the replies in the thread above its read position that are counted
// (docs/FEATURES.md section 2.1): not deleted, not system notices, not
// written by the account itself unless a reminder, and not written by
// someone the account has blocked.
type Threads interface {
	// Get returns an account's state for a thread. found is false for
	// "neither".
	Get(ctx context.Context, parent model.MessageID, account model.AccountID) (st model.ThreadState, found bool, err error)
	// States returns the state ("following" or "ignored") of every account
	// that has one for a thread, in one read: for a fan-out of a reply.
	States(ctx context.Context, parent model.MessageID) (map[model.AccountID]string, error)
	// ForParents returns an account's states for the given parents, for
	// the thread_states beside a page of messages.
	ForParents(ctx context.Context, account model.AccountID, parents []model.MessageID) ([]model.ThreadState, error)
	// ForAccount returns the threads an account follows or ignores in one
	// state ("" for both), most recently updated first.
	ForAccount(ctx context.Context, account model.AccountID, state string, p Page) (states []model.ThreadState, next string, err error)
	// Set stores a state chosen by the person: following or ignored. A new
	// row starts with the read position given; an existing row keeps its
	// own.
	Set(ctx context.Context, parent model.MessageID, account model.AccountID, state string, readSeq int64, at model.Time) (model.ThreadState, error)
	// Follow makes an account follow a thread because of something that
	// happened: it replied, the thread got its first reply, or a reply
	// mentioned it by name. With evenIfIgnored false an account that
	// ignores the thread is left alone. A new row starts with readSeq. If
	// raiseTo is above the row's read position the position is raised to
	// it (the replier has read up to its own reply). changed is false if
	// nothing was written.
	Follow(ctx context.Context, parent model.MessageID, account model.AccountID, readSeq, raiseTo int64, evenIfIgnored bool, at model.Time) (st model.ThreadState, changed bool, err error)
	// SetRead moves the read position of an existing row. found is false
	// for "neither", for which nothing is stored.
	SetRead(ctx context.Context, parent model.MessageID, account model.AccountID, seq int64, at model.Time) (st model.ThreadState, found bool, err error)
	// Delete takes an account back to "neither".
	Delete(ctx context.Context, parent model.MessageID, account model.AccountID) error

	// Unread returns the Threads view (docs/FEATURES.md section 1.5): the
	// threads an account follows that have at least one unread counted
	// reply, latest reply first. With all set, every followed thread.
	Unread(ctx context.Context, account model.AccountID, all bool, p Page) (states []model.ThreadState, next string, err error)
	// CountUnread returns how many threads Unread would list without all:
	// the unread_threads of the status endpoint.
	CountUnread(ctx context.Context, account model.AccountID) (int64, error)
}
