package sqlstore

import (
	"context"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

type notices struct{ *queries }

// Notices returns the notices area.
func (q *queries) Notices() store.Notices { return notices{q} }

type noticeCursor struct {
	At int64  `json:"a"`
	ID string `json:"i"`
}

func (q notices) Add(ctx context.Context, n *model.Notice) error {
	if n.ID == "" {
		n.ID = model.NewID[string]()
	}
	if n.CreatedAt.IsZero() {
		n.CreatedAt = model.Now()
	}
	_, err := q.wr.ExecContext(ctx, `INSERT INTO notices (id, account_id, kind, by_id, subject_id, conversation_id, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		n.ID, string(n.AccountID), n.Kind, id(n.ByID), id(n.SubjectID), id(n.ConversationID), int64(n.CreatedAt))
	return q.mapErr(err)
}

func (q notices) List(ctx context.Context, account model.AccountID, p store.Page) ([]model.Notice, string, error) {
	where := `account_id = ?`
	args := []any{string(account)}
	if p.Cursor != "" {
		var c noticeCursor
		if err := store.DecodeCursor(p.Cursor, &c); err != nil {
			return nil, "", err
		}
		where += ` AND (created_at < ? OR (created_at = ? AND id < ?))`
		args = append(args, c.At, c.At, c.ID)
	}
	limit := p.Clamp()
	args = append(args, limit+1)
	rows, err := q.rd.QueryContext(ctx, `SELECT id, account_id, kind, by_id, subject_id, conversation_id, created_at, seen_at
		FROM notices WHERE `+where+` ORDER BY created_at DESC, id DESC LIMIT ?`, args...)
	out, err := collect(rows, err, func(s scanner) (model.Notice, error) {
		var n model.Notice
		err := s.Scan(&n.ID, str(&n.AccountID), &n.Kind, str(&n.ByID), str(&n.SubjectID), str(&n.ConversationID), tm(&n.CreatedAt), tm(&n.SeenAt))
		return n, err
	})
	if err != nil {
		return nil, "", q.mapErr(err)
	}
	next := ""
	if len(out) > limit {
		out = out[:limit]
		last := out[limit-1]
		next = store.EncodeCursor(noticeCursor{At: int64(last.CreatedAt), ID: last.ID})
	}
	return out, next, nil
}

func (q notices) Unseen(ctx context.Context, account model.AccountID) (int, error) {
	var n int
	err := q.rd.QueryRowContext(ctx, `SELECT COUNT(*) FROM notices WHERE account_id = ? AND seen_at IS NULL`, string(account)).Scan(&n)
	return n, q.mapErr(err)
}

func (q notices) MarkSeen(ctx context.Context, account model.AccountID, at model.Time) error {
	_, err := q.wr.ExecContext(ctx, `UPDATE notices SET seen_at = ? WHERE account_id = ? AND seen_at IS NULL`, int64(at), string(account))
	return q.mapErr(err)
}
