package sqlstore

import (
	"context"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

type pins struct{ *queries }

// Pins returns the pins area.
func (q *queries) Pins() store.Pins { return pins{q} }

func (p pins) Add(ctx context.Context, conv model.ConversationID, msg model.MessageID, by model.AccountID, at model.Time) (model.Pin, error) {
	pin := model.Pin{MessageID: msg, PinnedByID: by, PinnedAt: at}
	err := p.atomic(ctx, func(q *queries) error {
		// Lock the conversation so two pins cannot both be the hundredth.
		if _, err := q.conversationState(ctx, conv); err != nil {
			return err
		}
		var n int
		if err := q.wr.QueryRowContext(ctx, `SELECT count(*) FROM pins WHERE conversation_id = ?`, string(conv)).Scan(&n); err != nil {
			return err
		}
		done, err := q.affected(q.wr.ExecContext(ctx, `
			INSERT INTO pins (conversation_id, message_id, pinned_by, pinned_at) VALUES (?, ?, ?, ?)
			ON CONFLICT (conversation_id, message_id) DO NOTHING`, string(conv), string(msg), string(by), int64(at)))
		if err != nil {
			return err
		}
		if done == 0 {
			return model.ErrAlreadyExists.With("That message is already pinned.")
		}
		if n >= model.MaxPinsPerConversation {
			return model.ErrConflict.With("This conversation already has 100 pins. Unpin one first.")
		}
		return nil
	})
	return pin, err
}

func (p pins) Remove(ctx context.Context, conv model.ConversationID, msg model.MessageID) error {
	return p.changed(p.wr.ExecContext(ctx, `DELETE FROM pins WHERE conversation_id = ? AND message_id = ?`, string(conv), string(msg)))
}

func (p pins) List(ctx context.Context, conv model.ConversationID) ([]model.Pin, error) {
	rows, err := p.rd.QueryContext(ctx, `
		SELECT message_id, pinned_by, pinned_at FROM pins WHERE conversation_id = ? ORDER BY pinned_at DESC, message_id DESC`, string(conv))
	return collect(rows, err, func(s scanner) (model.Pin, error) {
		var pin model.Pin
		err := s.Scan(str(&pin.MessageID), str(&pin.PinnedByID), tm(&pin.PinnedAt))
		return pin, err
	})
}
