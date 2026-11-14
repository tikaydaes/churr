package sqlstore

import (
	"context"
	"strings"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

type audit struct{ *queries }

// Audit returns the audit log area.
func (q *queries) Audit() store.Audit { return audit{q} }

func (a audit) Append(ctx context.Context, e model.AuditEntry) error {
	if e.ID == "" {
		e.ID = model.NewID[string]()
	}
	if e.At.IsZero() {
		e.At = model.Now()
	}
	if e.ActorKind == "" {
		e.ActorKind = "server"
	}
	_, err := a.wr.ExecContext(ctx, `
		INSERT INTO audit_log (id, at, actor_id, actor_kind, action, target_kind, target_id, detail)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		e.ID, int64(e.At), id(e.ActorID), e.ActorKind, e.Action, id(e.TargetKind), id(e.TargetID), jsonObject(e.Detail))
	return a.mapErr(err)
}

// auditCursor is the sort key of the last row of a page: newest first, with
// the id to break ties between entries written in the same millisecond.
type auditCursor struct {
	At int64  `json:"a"`
	ID string `json:"i"`
}

func (a audit) List(ctx context.Context, f store.AuditFilter, p store.Page) ([]model.AuditEntry, string, error) {
	where := []string{"1 = 1"}
	var args []any
	add := func(clause string, v string) {
		if v != "" {
			where = append(where, clause)
			args = append(args, v)
		}
	}
	add("action = ?", f.Action)
	add("actor_id = ?", f.ActorID)
	add("target_kind = ?", f.TargetKind)
	add("target_id = ?", f.TargetID)
	if f.ActionPrefix != "" {
		// like() matches anywhere; the pattern here must match from the
		// start, so its leading wildcard is taken off.
		where = append(where, "LOWER(action) LIKE ?"+likeEscape)
		args = append(args, strings.TrimPrefix(like(f.ActionPrefix), "%"))
	}
	if !f.Since.IsZero() {
		where = append(where, "at >= ?")
		args = append(args, int64(f.Since))
	}
	if !f.Until.IsZero() {
		where = append(where, "at < ?")
		args = append(args, int64(f.Until))
	}
	if p.Cursor != "" {
		var c auditCursor
		if err := store.DecodeCursor(p.Cursor, &c); err != nil {
			return nil, "", err
		}
		where = append(where, "(at < ? OR (at = ? AND id < ?))")
		args = append(args, c.At, c.At, c.ID)
	}
	limit := p.Clamp()
	args = append(args, limit+1)
	rows, err := a.rd.QueryContext(ctx, `
		SELECT id, at, actor_id, actor_kind, action, target_kind, target_id, detail
		FROM audit_log WHERE `+strings.Join(where, " AND ")+`
		ORDER BY at DESC, id DESC LIMIT ?`, args...)
	out, err := collect(rows, err, func(s scanner) (model.AuditEntry, error) {
		var e model.AuditEntry
		err := s.Scan(str(&e.ID), tm(&e.At), str(&e.ActorID), &e.ActorKind, &e.Action, str(&e.TargetKind), str(&e.TargetID), js(&e.Detail))
		if e.Detail == nil {
			e.Detail = map[string]any{}
		}
		return e, err
	})
	if err != nil {
		return nil, "", err
	}
	next := ""
	if len(out) > limit {
		out = out[:limit]
		last := out[limit-1]
		next = store.EncodeCursor(auditCursor{At: int64(last.At), ID: last.ID})
	}
	return out, next, nil
}
