package sqlstore

import (
	"context"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

type themes struct{ *queries }

// Themes returns the themes area.
func (q *queries) Themes() store.Themes { return themes{q} }

const themeCols = `id, owner_id, name, kind, colours, published_at, created_at, updated_at`

func scanTheme(s scanner) (model.Theme, error) {
	var t model.Theme
	err := s.Scan(str(&t.ID), str(&t.OwnerID), &t.Name, &t.Kind, js(&t.Colours), tm(&t.PublishedAt), tm(&t.CreatedAt), tm(&t.UpdatedAt))
	t.Published = !t.PublishedAt.IsZero()
	if t.Colours == nil {
		t.Colours = map[string]string{}
	}
	return t, err
}

func (th themes) Create(ctx context.Context, t *model.Theme, max int) error {
	return th.atomic(ctx, func(q *queries) error {
		// Lock the owner so the count and the insert are one step.
		var owner string
		if err := q.wr.QueryRowContext(ctx, `SELECT id FROM accounts WHERE id = ?`+q.forUpdate(), string(t.OwnerID)).Scan(str(&owner)); err != nil {
			return q.mapErr(err)
		}
		var n int
		if err := q.wr.QueryRowContext(ctx, `SELECT count(*) FROM themes WHERE owner_id = ?`, string(t.OwnerID)).Scan(&n); err != nil {
			return q.mapErr(err)
		}
		if n >= max {
			return model.ErrConflict.With("You have as many themes as there can be. Delete one first.")
		}
		if t.ID == "" {
			t.ID = model.NewID[model.ThemeID]()
		}
		if t.UpdatedAt.IsZero() {
			t.UpdatedAt = t.CreatedAt
		}
		t.Published = !t.PublishedAt.IsZero()
		_, err := q.wr.ExecContext(ctx, `INSERT INTO themes (`+themeCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			string(t.ID), string(t.OwnerID), t.Name, t.Kind, jsonObject(t.Colours), ms(t.PublishedAt), int64(t.CreatedAt), int64(t.UpdatedAt))
		return q.mapErr(err)
	})
}

func (th themes) Get(ctx context.Context, theme model.ThemeID) (model.Theme, error) {
	t, err := scanTheme(th.rd.QueryRowContext(ctx, `SELECT `+themeCols+` FROM themes WHERE id = ?`, string(theme)))
	return t, th.mapErr(err)
}

func (th themes) Update(ctx context.Context, theme model.ThemeID, at model.Time, fn func(*model.Theme) error) (model.Theme, error) {
	var out model.Theme
	err := th.atomic(ctx, func(q *queries) error {
		t, err := scanTheme(q.wr.QueryRowContext(ctx, `SELECT `+themeCols+` FROM themes WHERE id = ?`+q.forUpdate(), string(theme)))
		if err != nil {
			return q.mapErr(err)
		}
		if err := fn(&t); err != nil {
			return err
		}
		if !t.Published {
			t.PublishedAt = 0
		}
		t.Published = !t.PublishedAt.IsZero()
		t.UpdatedAt = at
		_, err = q.wr.ExecContext(ctx, `UPDATE themes SET name = ?, kind = ?, colours = ?, published_at = ?, updated_at = ? WHERE id = ?`,
			t.Name, t.Kind, jsonObject(t.Colours), ms(t.PublishedAt), int64(t.UpdatedAt), string(theme))
		out = t
		return q.mapErr(err)
	})
	return out, err
}

func (th themes) Delete(ctx context.Context, theme model.ThemeID) error {
	return th.changed(th.wr.ExecContext(ctx, `DELETE FROM themes WHERE id = ?`, string(theme)))
}

func (th themes) ByOwner(ctx context.Context, owner model.AccountID) ([]model.Theme, error) {
	rows, err := th.rd.QueryContext(ctx, `SELECT `+themeCols+` FROM themes WHERE owner_id = ? ORDER BY created_at, id`, string(owner))
	return collect(rows, err, scanTheme)
}

// themeCursor is the sort key of the last row of a library page: most
// recently published first, the id breaking ties.
type themeCursor struct {
	At int64  `json:"a"`
	ID string `json:"i"`
}

func (th themes) Library(ctx context.Context, query string, p store.Page) ([]model.Theme, string, error) {
	where := `published_at IS NOT NULL`
	var args []any
	if query != "" {
		where += ` AND LOWER(name) LIKE ?` + likeEscape
		args = append(args, like(query))
	}
	if p.Cursor != "" {
		var c themeCursor
		if err := store.DecodeCursor(p.Cursor, &c); err != nil {
			return nil, "", err
		}
		where += ` AND (published_at < ? OR (published_at = ? AND id < ?))`
		args = append(args, c.At, c.At, c.ID)
	}
	limit := p.Clamp()
	args = append(args, limit+1)
	rows, err := th.rd.QueryContext(ctx, `SELECT `+themeCols+` FROM themes WHERE `+where+`
		ORDER BY published_at DESC, id DESC LIMIT ?`, args...)
	out, err := collect(rows, err, scanTheme)
	if err != nil {
		return nil, "", err
	}
	next := ""
	if len(out) > limit {
		out = out[:limit]
		last := out[limit-1]
		next = store.EncodeCursor(themeCursor{At: int64(last.PublishedAt), ID: string(last.ID)})
	}
	return out, next, nil
}
