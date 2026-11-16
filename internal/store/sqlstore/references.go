package sqlstore

import (
	"context"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

type referencePatterns struct{ *queries }

// ReferencePatterns returns the reference patterns area.
func (q *queries) ReferencePatterns() store.ReferencePatterns { return referencePatterns{q} }

const refPatternCols = `id, conversation_id, name, prefix, expression, url_template, created_by, created_at`

func scanRefPattern(s scanner) (model.ReferencePattern, error) {
	var p model.ReferencePattern
	err := s.Scan(&p.ID, str(&p.ConversationID), &p.Name, &p.Prefix, &p.Expression, &p.URLTemplate, str(&p.CreatedByID), tm(&p.CreatedAt))
	return p, err
}

func (r referencePatterns) Create(ctx context.Context, p *model.ReferencePattern) error {
	if p.ID == "" {
		p.ID = model.NewID[string]()
	}
	if p.CreatedAt.IsZero() {
		p.CreatedAt = model.Now()
	}
	_, err := r.wr.ExecContext(ctx, `INSERT INTO reference_patterns (`+refPatternCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID, id(p.ConversationID), p.Name, p.Prefix, p.Expression, p.URLTemplate, id(p.CreatedByID), int64(p.CreatedAt))
	r.touchRefPatterns()
	return r.mapErr(err)
}

func (r referencePatterns) Get(ctx context.Context, pattern string) (model.ReferencePattern, error) {
	p, err := scanRefPattern(r.rd.QueryRowContext(ctx, `SELECT `+refPatternCols+` FROM reference_patterns WHERE id = ?`, pattern))
	return p, r.mapErr(err)
}

func (r referencePatterns) Update(ctx context.Context, p model.ReferencePattern) error {
	// Marked after the write: a reader in between would keep the old list.
	defer r.touchRefPatterns()
	return r.changed(r.wr.ExecContext(ctx, `UPDATE reference_patterns SET name = ?, prefix = ?, expression = ?, url_template = ? WHERE id = ?`,
		p.Name, p.Prefix, p.Expression, p.URLTemplate, p.ID))
}

func (r referencePatterns) Delete(ctx context.Context, pattern string) error {
	defer r.touchRefPatterns()
	return r.changed(r.wr.ExecContext(ctx, `DELETE FROM reference_patterns WHERE id = ?`, pattern))
}

func (r referencePatterns) All(ctx context.Context) ([]model.ReferencePattern, error) {
	return r.allRefPatterns(ctx)
}

// touchRefPatterns marks the reference patterns as changed.
func (q *queries) touchRefPatterns() {
	if q.touched != nil {
		q.touched.refPatterns = true
		return
	}
	q.db.kept.refPatterns.drop()
}

// allRefPatterns is every reference pattern, from memory when it may be.
// The list is shared: callers get a copy.
func (q *queries) allRefPatterns(ctx context.Context) ([]model.ReferencePattern, error) {
	read := func() ([]model.ReferencePattern, error) {
		rows, err := q.rd.QueryContext(ctx, `SELECT `+refPatternCols+` FROM reference_patterns ORDER BY created_at, id`)
		return collect(rows, err, scanRefPattern)
	}
	if q.touched != nil && q.touched.refPatterns {
		return read()
	}
	c := &q.db.kept.refPatterns
	val, ver, ok := c.get()
	if !ok {
		var err error
		if val, err = read(); err != nil {
			return nil, err
		}
		if !q.inTx {
			c.put(val, ver)
		}
	}
	if len(val) == 0 {
		return nil, nil
	}
	return append([]model.ReferencePattern(nil), val...), nil
}
