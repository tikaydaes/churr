package sqlstore

import (
	"context"
	"errors"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

type integrations struct{ *queries }

// Integrations returns the integrations area.
func (q *queries) Integrations() store.Integrations { return integrations{q} }

const integrationCols = `id, name, allows, created_by, created_at, last_used_at, revoked_at`

func scanIntegration(s scanner) (model.Integration, error) {
	var out model.Integration
	err := s.Scan(str(&out.ID), &out.Name, js(&out.Allows), str(&out.CreatedByID), tm(&out.CreatedAt), tm(&out.LastUsedAt), tm(&out.RevokedAt))
	if out.Allows == nil {
		out.Allows = []string{}
	}
	return out, err
}

func (i integrations) Create(ctx context.Context, in *model.Integration, tokenHash []byte) error {
	if len(tokenHash) == 0 {
		return errors.New("sqlstore: an integration needs the hash of its token")
	}
	if in.ID == "" {
		in.ID = model.NewID[string]()
	}
	if in.CreatedAt.IsZero() {
		in.CreatedAt = model.Now()
	}
	if in.Allows == nil {
		in.Allows = []string{}
	}
	_, err := i.wr.ExecContext(ctx, `
		INSERT INTO integrations (id, name, token_hash, allows, created_by, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		in.ID, in.Name, tokenHash, jsonList(in.Allows), string(in.CreatedByID), int64(in.CreatedAt))
	return i.mapErr(err)
}

func (i integrations) Get(ctx context.Context, integration string) (model.Integration, error) {
	if !model.ValidID(integration) {
		return model.Integration{}, model.ErrNotFound
	}
	out, err := scanIntegration(i.rd.QueryRowContext(ctx, `SELECT `+integrationCols+` FROM integrations WHERE id = ?`, integration))
	return out, i.mapErr(err)
}

func (i integrations) ByTokenHash(ctx context.Context, tokenHash []byte) (model.Integration, error) {
	if len(tokenHash) == 0 {
		return model.Integration{}, model.ErrNotFound
	}
	out, err := scanIntegration(i.rd.QueryRowContext(ctx, `SELECT `+integrationCols+` FROM integrations WHERE token_hash = ?`, tokenHash))
	return out, i.mapErr(err)
}

func (i integrations) List(ctx context.Context) ([]model.Integration, error) {
	rows, err := i.rd.QueryContext(ctx, `SELECT `+integrationCols+` FROM integrations ORDER BY created_at DESC, id DESC`)
	return collect(rows, err, scanIntegration)
}

func (i integrations) Used(ctx context.Context, integration string, at model.Time) error {
	return i.changed(i.wr.ExecContext(ctx, `UPDATE integrations SET last_used_at = ? WHERE id = ?`, int64(at), integration))
}

func (i integrations) Revoke(ctx context.Context, integration string, at model.Time) error {
	if !model.ValidID(integration) {
		return model.ErrNotFound
	}
	return i.atomic(ctx, func(q *queries) error {
		n, err := q.affected(q.wr.ExecContext(ctx,
			`UPDATE integrations SET revoked_at = ? WHERE id = ? AND revoked_at IS NULL`, int64(at), integration))
		if err != nil || n > 0 {
			return err
		}
		var one int
		return q.mapErr(q.wr.QueryRowContext(ctx, `SELECT 1 FROM integrations WHERE id = ?`, integration).Scan(&one))
	})
}
