package sqlstore

import (
	"context"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

type bookmarks struct{ *queries }

// Bookmarks returns the bookmarks area.
func (q *queries) Bookmarks() store.Bookmarks { return bookmarks{q} }

func (b bookmarks) Add(ctx context.Context, account model.AccountID, msg model.MessageID, at model.Time) (model.Bookmark, bool, error) {
	out := model.Bookmark{MessageID: msg}
	added := false
	err := b.atomic(ctx, func(q *queries) error {
		// Looked up first, so a message that does not exist is answered
		// without a failed statement inside the transaction.
		var one int
		if err := q.wr.QueryRowContext(ctx, `SELECT 1 FROM messages WHERE id = ?`, string(msg)).Scan(&one); err != nil {
			return q.mapErr(err)
		}
		n, err := q.affected(q.wr.ExecContext(ctx, `
			INSERT INTO bookmarks (account_id, message_id, created_at) VALUES (?, ?, ?)
			ON CONFLICT (account_id, message_id) DO NOTHING`, string(account), string(msg), int64(at)))
		if err != nil {
			return err
		}
		added = n > 0
		return q.mapErr(q.wr.QueryRowContext(ctx, `SELECT created_at FROM bookmarks WHERE account_id = ? AND message_id = ?`,
			string(account), string(msg)).Scan(tm(&out.CreatedAt)))
	})
	return out, added, err
}

func (b bookmarks) Remove(ctx context.Context, account model.AccountID, msg model.MessageID) (bool, error) {
	n, err := b.affected(b.wr.ExecContext(ctx, `DELETE FROM bookmarks WHERE account_id = ? AND message_id = ?`,
		string(account), string(msg)))
	return n > 0, err
}

type bookmarkCursor struct {
	At int64  `json:"a"`
	ID string `json:"i"`
}

func (b bookmarks) List(ctx context.Context, account model.AccountID, scope store.Scope, p store.Page) ([]model.Bookmark, string, error) {
	// The same readability rule as search: channels the account is in,
	// and every public channel when the scope says so.
	// A member reads from their first join in a private channel (ruling
	// R87): visible_from is 0 everywhere else.
	where := `b.account_id = ? AND (EXISTS (SELECT 1 FROM members mb WHERE mb.conversation_id = m.conversation_id AND mb.account_id = ? AND mb.visible_from <= m.created_at)`
	args := []any{string(account), id(scope.AccountID)}
	if scope.PublicChannels {
		nb, nbArgs := notBanned("c.id", scope.AccountID, model.Now())
		where += ` OR (c.kind = 'channel' AND c.private = ? AND c.name IS NOT NULL AND ` + nb + `)`
		args = append(append(args, false), nbArgs...)
	}
	where += `)`
	if p.Cursor != "" {
		var c bookmarkCursor
		if err := store.DecodeCursor(p.Cursor, &c); err != nil {
			return nil, "", err
		}
		where += ` AND (b.created_at < ? OR (b.created_at = ? AND b.message_id < ?))`
		args = append(args, c.At, c.At, c.ID)
	}
	limit := p.Clamp()
	args = append(args, limit+1)
	rows, err := b.rd.QueryContext(ctx, `
		SELECT b.message_id, b.created_at FROM bookmarks b
		JOIN messages m ON m.id = b.message_id JOIN conversations c ON c.id = m.conversation_id
		WHERE `+where+` ORDER BY b.created_at DESC, b.message_id DESC LIMIT ?`, args...)
	out, err := collect(rows, err, func(s scanner) (model.Bookmark, error) {
		var bm model.Bookmark
		err := s.Scan(str(&bm.MessageID), tm(&bm.CreatedAt))
		return bm, err
	})
	if err != nil {
		return nil, "", err
	}
	next := ""
	if len(out) > limit {
		out = out[:limit]
		last := out[limit-1]
		next = store.EncodeCursor(bookmarkCursor{At: int64(last.CreatedAt), ID: string(last.MessageID)})
	}
	return out, next, nil
}
