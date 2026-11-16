package sqlstore

import (
	"context"
	"database/sql"
	"errors"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

type sidebars struct{ *queries }

// Sidebars returns the sidebars area.
func (q *queries) Sidebars() store.Sidebars { return sidebars{q} }

func (s sidebars) read(ctx context.Context, q *queries, account model.AccountID, lock string) (model.Sidebar, error) {
	var out model.Sidebar
	var version int64
	err := q.wr.QueryRowContext(ctx, `SELECT version, layout FROM sidebars WHERE account_id = ?`+lock, string(account)).Scan(&version, js(&out))
	if errors.Is(err, sql.ErrNoRows) {
		return model.Sidebar{}, nil
	}
	out.Version = version
	return out, q.mapErr(err)
}

func (s sidebars) Get(ctx context.Context, account model.AccountID) (model.Sidebar, error) {
	l, err := s.read(ctx, s.queries, account, "")
	return tidySidebar(l), err
}

func (s sidebars) Update(ctx context.Context, account model.AccountID, fn func(*model.Sidebar) error) (model.Sidebar, error) {
	var out model.Sidebar
	err := s.atomic(ctx, func(q *queries) error {
		l, err := s.read(ctx, q, account, q.forUpdate())
		if err != nil {
			return err
		}
		l = tidySidebar(l)
		was := l.Version
		if err := fn(&l); err != nil {
			return err
		}
		l.Version = was + 1
		body := l
		body.Version = 0
		// The WHERE keeps a write that raced in between from being lost:
		// two first writes, which no row lock can order, meet here.
		n, err := q.affected(q.wr.ExecContext(ctx, `
			INSERT INTO sidebars (account_id, version, layout) VALUES (?, ?, ?)
			ON CONFLICT (account_id) DO UPDATE SET version = excluded.version, layout = excluded.layout
			WHERE sidebars.version = ?`, string(account), l.Version, jsonArg(body), was))
		if err != nil {
			return err
		}
		if n == 0 {
			return model.ErrConflict
		}
		out = l
		return nil
	})
	return out, err
}

// tidySidebar gives every list of a layout a value, so none is sent as null.
func tidySidebar(l model.Sidebar) model.Sidebar {
	if l.Sections == nil {
		l.Sections = []model.SidebarSection{}
	}
	if l.Headings == nil {
		l.Headings = []string{}
	}
	if l.Groups == nil {
		l.Groups = []model.SidebarGroup{}
	}
	for i := range l.Groups {
		if l.Groups[i].Order == nil {
			l.Groups[i].Order = []model.ConversationID{}
		}
	}
	if l.Placements == nil {
		l.Placements = []model.SidebarPlacement{}
	}
	if l.Stars == nil {
		l.Stars = []model.ConversationID{}
	}
	return l
}
