package store

import (
	"context"

	"github.com/tikaydaes/churr/internal/model"
)

// Checklists is the `checklist_items` and `checklist_history` tables.
type Checklists interface {
	// Replace sets the checklist of a message, when it is posted and each
	// time it is edited. Items given are inserted, or updated in place
	// when their ItemID exists (position and text change; the ticked state
	// stays, and a removed item named again comes back). Items the message
	// had that are not given get removed_at, and are kept for history.
	// For a new item, Checked is the state its author typed.
	Replace(ctx context.Context, msg model.MessageID, items []ChecklistItem, at model.Time) error
	// Items returns a message's items in position order. Removed ones are
	// left out unless asked for.
	Items(ctx context.Context, msg model.MessageID, includeRemoved bool) ([]ChecklistItem, error)
	// Tick sets an item ticked or unticked and writes the history row.
	// model.ErrNotFound if the message has no such item or it was removed.
	Tick(ctx context.Context, msg model.MessageID, itemID string, account model.AccountID, checked bool, at model.Time) (model.Task, error)
	// History returns every tick and untick of an item, oldest first.
	History(ctx context.Context, msg model.MessageID, itemID string) ([]model.TaskChange, error)
}

// ChecklistItem is one tick-box line of a message.
type ChecklistItem struct {
	ItemID    string
	Position  int
	Text      string
	Checked   bool
	CheckedBy model.AccountID
	CheckedAt model.Time
	RemovedAt model.Time
}

// Task is the item's state in the shape a message carries it.
func (c ChecklistItem) Task() model.Task {
	return model.Task{ID: c.ItemID, Checked: c.Checked, CheckedByID: c.CheckedBy, CheckedAt: c.CheckedAt}
}
