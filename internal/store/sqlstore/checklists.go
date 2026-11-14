package sqlstore

import (
	"context"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

type checklists struct{ *queries }

// Checklists returns the checklists area.
func (q *queries) Checklists() store.Checklists { return checklists{q} }

func scanChecklistItem(s scanner) (store.ChecklistItem, error) {
	var c store.ChecklistItem
	err := s.Scan(&c.ItemID, &c.Position, &c.Text, flag(&c.Checked), str(&c.CheckedBy), tm(&c.CheckedAt), tm(&c.RemovedAt))
	return c, err
}

func (c checklists) Replace(ctx context.Context, msg model.MessageID, items []store.ChecklistItem, at model.Time) error {
	return c.atomic(ctx, func(q *queries) error {
		// Everything is marked removed, then the items given are put
		// back: an item named again returns with the state it had.
		if _, err := q.wr.ExecContext(ctx,
			`UPDATE checklist_items SET removed_at = ? WHERE message_id = ? AND removed_at IS NULL`, int64(at), string(msg)); err != nil {
			return err
		}
		for _, it := range items {
			_, err := q.wr.ExecContext(ctx, `
				INSERT INTO checklist_items (message_id, item_id, position, text, checked) VALUES (?, ?, ?, ?, ?)
				ON CONFLICT (message_id, item_id) DO UPDATE SET position = excluded.position, text = excluded.text, removed_at = NULL`,
				string(msg), it.ItemID, it.Position, it.Text, it.Checked)
			if err != nil {
				return q.mapErr(err)
			}
		}
		return nil
	})
}

func (c checklists) Items(ctx context.Context, msg model.MessageID, includeRemoved bool) ([]store.ChecklistItem, error) {
	where := ""
	if !includeRemoved {
		where = " AND removed_at IS NULL"
	}
	rows, err := c.rd.QueryContext(ctx, `
		SELECT item_id, position, text, checked, checked_by, checked_at, removed_at FROM checklist_items
		WHERE message_id = ?`+where+` ORDER BY position, item_id`, string(msg))
	return collect(rows, err, scanChecklistItem)
}

func (c checklists) Tick(ctx context.Context, msg model.MessageID, itemID string, account model.AccountID, checked bool, at model.Time) (model.Task, error) {
	var out model.Task
	err := c.atomic(ctx, func(q *queries) error {
		var by any
		var when any
		if checked {
			by, when = string(account), int64(at)
		}
		err := q.changed(q.wr.ExecContext(ctx, `
			UPDATE checklist_items SET checked = ?, checked_by = ?, checked_at = ?
			WHERE message_id = ? AND item_id = ? AND removed_at IS NULL`, checked, by, when, string(msg), itemID))
		if err != nil {
			return err
		}
		_, err = q.wr.ExecContext(ctx, `
			INSERT INTO checklist_history (id, message_id, item_id, account_id, checked, at) VALUES (?, ?, ?, ?, ?, ?)`,
			model.NewID[string](), string(msg), itemID, string(account), checked, int64(at))
		if err != nil {
			return q.mapErr(err)
		}
		out = model.Task{ID: itemID, Checked: checked}
		if checked {
			out.CheckedByID, out.CheckedAt = account, at
		}
		return nil
	})
	return out, err
}

func (c checklists) History(ctx context.Context, msg model.MessageID, itemID string) ([]model.TaskChange, error) {
	rows, err := c.rd.QueryContext(ctx, `
		SELECT item_id, account_id, checked, at FROM checklist_history
		WHERE message_id = ? AND item_id = ? ORDER BY at, id`, string(msg), itemID)
	return collect(rows, err, func(s scanner) (model.TaskChange, error) {
		var t model.TaskChange
		err := s.Scan(&t.ItemID, str(&t.AccountID), flag(&t.Checked), tm(&t.At))
		return t, err
	})
}
