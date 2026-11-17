package store

import (
	"context"

	"github.com/tikaydaes/churr/internal/model"
)

// Janitor is what the hourly janitor needs that no single area owns: the
// deletions of docs/SCHEMA.md sections 14.1 and 14.4 that cut across tables.
// Uploads never attached, old events and stale previews are reached through
// their own areas (Files.Unattached, Events.DeleteBefore, Previews.Stale).
type Janitor interface {
	// ExpiredMessages returns messages whose created_at is older than
	// their conversation's retention period at `now`: the channel's own
	// period for a named channel that has one (0 meaning never), otherwise
	// serverSeconds (0 meaning never). Replies come before parents, oldest
	// first. A parent already emptied to a bare record that still has
	// replies is left out, so it does not fill every batch until they go.
	ExpiredMessages(ctx context.Context, serverSeconds int64, now model.Time, limit int) ([]model.MessageID, error)
	// ExpireMessage removes one message as retention does (section 14.1):
	// its files, the events that carry it, its search entry and the call it
	// announced. A parent that still has replies is emptied to a bare
	// record, with deleted_by null, and kept; anything else is deleted with
	// all that hangs from it. It returns the blobs to remove, and whether
	// the row itself went.
	ExpireMessage(ctx context.Context, id model.MessageID, now model.Time) (blobKeys []string, deleted bool, err error)
	// Tidy deletes the rows of section 14.4 that have outlived their use
	// (sessions, sign-in codes and links, invites, dialogs, time-outs,
	// scheduled items, link logs, unlock requests) and
	// clears client ids older than a day and marks lapsed the approvals
	// that waited past their end. It returns how many rows went,
	// by table.
	Tidy(ctx context.Context, now model.Time) (map[string]int64, error)
}
