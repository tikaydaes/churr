package sqlstore

import (
	"context"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

type emoji struct{ *queries }

// Emoji returns the emoji area.
func (q *queries) Emoji() store.Emoji { return emoji{q} }

// emojiSelect reads an emoji with the name of the server it came from and
// the name of its file, which its address ends in.
const emojiSelect = `
	SELECT e.id, e.name, e.file_id, e.created_by, e.created_at, s.name, f.name
	FROM emoji e LEFT JOIN servers s ON s.id = e.server_id LEFT JOIN files f ON f.id = e.file_id`

func scanEmoji(s scanner) (model.Emoji, error) {
	var e model.Emoji
	var fileName string
	err := s.Scan(str(&e.ID), &e.Name, str(&e.FileID), str(&e.CreatedByID), tm(&e.CreatedAt), str(&e.ServerName), str(&fileName))
	if err == nil {
		f := model.File{ID: e.FileID, Name: fileName, BlobKey: "set"}
		f.FillURLs()
		e.URL = f.URL
	}
	return e, err
}

func (e emoji) one(ctx context.Context, ex execer, where string, args ...any) (model.Emoji, error) {
	out, err := scanEmoji(ex.QueryRowContext(ctx, emojiSelect+` WHERE `+where, args...))
	return out, e.mapErr(err)
}

func (e emoji) Create(ctx context.Context, em *model.Emoji) error {
	if em.ID == "" {
		em.ID = model.NewID[model.EmojiID]()
	}
	if em.CreatedAt.IsZero() {
		em.CreatedAt = model.Now()
	}
	return e.atomic(ctx, func(q *queries) error {
		// Checked first: a refused insert would end a PostgreSQL
		// transaction the caller may still need. The index decides a race.
		var n int
		if err := q.wr.QueryRowContext(ctx, `SELECT count(*) FROM emoji WHERE name = ? AND server_id IS NULL`, em.Name).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			return model.ErrNameTaken
		}
		_, err := q.wr.ExecContext(ctx, `INSERT INTO emoji (id, name, file_id, created_by, created_at) VALUES (?, ?, ?, ?, ?)`,
			string(em.ID), em.Name, string(em.FileID), string(em.CreatedByID), int64(em.CreatedAt))
		if err != nil {
			return q.taken(err)
		}
		out, err := emoji{q}.one(ctx, q.wr, "e.id = ?", string(em.ID))
		*em = out
		return err
	})
}

func (e emoji) Get(ctx context.Context, em model.EmojiID) (model.Emoji, error) {
	return e.one(ctx, e.rd, "e.id = ?", string(em))
}

func (e emoji) ByName(ctx context.Context, name string) (model.Emoji, error) {
	return e.one(ctx, e.rd, "e.name = ? AND e.server_id IS NULL", name)
}

// emojiCursor is the sort key of List.
type emojiCursor struct {
	Name string `json:"n"`
	ID   string `json:"i"`
}

func (e emoji) List(ctx context.Context, p store.Page) ([]model.Emoji, string, error) {
	where := "1 = 1"
	var args []any
	if p.Cursor != "" {
		var c emojiCursor
		if err := store.DecodeCursor(p.Cursor, &c); err != nil {
			return nil, "", err
		}
		where = "(e.name > ? OR (e.name = ? AND e.id > ?))"
		args = append(args, c.Name, c.Name, c.ID)
	}
	limit := p.Clamp()
	args = append(args, limit+1)
	rows, err := e.rd.QueryContext(ctx, emojiSelect+` WHERE `+where+` ORDER BY e.name, e.id LIMIT ?`, args...)
	out, err := collect(rows, err, scanEmoji)
	if err != nil {
		return nil, "", err
	}
	next := ""
	if len(out) > limit {
		out = out[:limit]
		next = store.EncodeCursor(emojiCursor{Name: out[limit-1].Name, ID: string(out[limit-1].ID)})
	}
	return out, next, nil
}

func (e emoji) Rename(ctx context.Context, em model.EmojiID, name string) (model.Emoji, error) {
	var out model.Emoji
	err := e.atomic(ctx, func(q *queries) error {
		var n int
		if err := q.wr.QueryRowContext(ctx, `SELECT count(*) FROM emoji WHERE name = ? AND server_id IS NULL AND id <> ?`,
			name, string(em)).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			return model.ErrNameTaken
		}
		res, err := q.wr.ExecContext(ctx, `UPDATE emoji SET name = ? WHERE id = ? AND server_id IS NULL`, name, string(em))
		if err != nil {
			return q.taken(err)
		}
		if err := q.changed(res, nil); err != nil {
			return err
		}
		out, err = emoji{q}.one(ctx, q.wr, "e.id = ?", string(em))
		return err
	})
	return out, err
}

func (e emoji) Delete(ctx context.Context, em model.EmojiID) (model.Emoji, error) {
	var out model.Emoji
	err := e.atomic(ctx, func(q *queries) error {
		var err error
		if out, err = (emoji{q}).one(ctx, q.wr, "e.id = ?", string(em)); err != nil {
			return err
		}
		return q.changed(q.wr.ExecContext(ctx, `DELETE FROM emoji WHERE id = ?`, string(em)))
	})
	return out, err
}
