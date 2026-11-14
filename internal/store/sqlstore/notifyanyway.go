package sqlstore

import (
	"context"
	"database/sql"
	"errors"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

type notifyAnyway struct{ *queries }

// NotifyAnyway returns the notify anyway area.
func (q *queries) NotifyAnyway() store.NotifyAnyway { return notifyAnyway{q} }

func (n notifyAnyway) Record(ctx context.Context, msg model.MessageID, target, by model.AccountID, at model.Time, delivered bool) (bool, bool, error) {
	marked, first := false, false
	err := n.atomic(ctx, func(q *queries) error {
		var had bool
		if err := q.wr.QueryRowContext(ctx, `SELECT notified_anyway FROM messages WHERE id = ?`+q.forUpdate(), string(msg)).Scan(flag(&had)); err != nil {
			return q.mapErr(err)
		}
		var one int
		if err := q.wr.QueryRowContext(ctx, `SELECT 1 FROM accounts WHERE id = ?`, string(target)).Scan(&one); err != nil {
			return q.mapErr(err)
		}
		if !had {
			if _, err := q.wr.ExecContext(ctx, `UPDATE messages SET notified_anyway = ? WHERE id = ?`, true, string(msg)); err != nil {
				return err
			}
			marked = true
		}
		rows, err := q.affected(q.wr.ExecContext(ctx, `
			INSERT INTO notify_anyway (message_id, account_id, by_id, at, delivered) VALUES (?, ?, ?, ?, ?)
			ON CONFLICT (message_id, account_id) DO NOTHING`, string(msg), string(target), string(by), int64(at), delivered))
		first = rows > 0
		return err
	})
	return marked, first, err
}

func (n notifyAnyway) Delivered(ctx context.Context, msg model.MessageID, target model.AccountID) (bool, error) {
	var delivered bool
	err := n.rd.QueryRowContext(ctx, `SELECT delivered FROM notify_anyway WHERE message_id = ? AND account_id = ?`,
		string(msg), string(target)).Scan(flag(&delivered))
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return delivered, err
}
