package sqlstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

type settings struct{ *queries }

// Settings returns the settings area.
func (q *queries) Settings() store.Settings { return settings{q} }

func (s settings) Get(ctx context.Context, key string) (json.RawMessage, bool, error) {
	var raw json.RawMessage
	err := s.rd.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(js(&raw))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	return raw, err == nil, err
}

func (s settings) All(ctx context.Context) (map[string]json.RawMessage, error) {
	rows, err := s.rd.QueryContext(ctx, `SELECT key, value FROM settings`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]json.RawMessage{}
	for rows.Next() {
		var k string
		var v json.RawMessage
		if err := rows.Scan(&k, js(&v)); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}

func (s settings) Set(ctx context.Context, key string, value any, by model.AccountID) error {
	_, err := s.wr.ExecContext(ctx, `
		INSERT INTO settings (key, value, updated_at, updated_by) VALUES (?, ?, ?, ?)
		ON CONFLICT (key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at, updated_by = excluded.updated_by`,
		key, jsonArg(value), int64(model.Now()), id(by))
	return s.mapErr(err)
}

func (s settings) Delete(ctx context.Context, key string) error {
	_, err := s.wr.ExecContext(ctx, `DELETE FROM settings WHERE key = ?`, key)
	return err
}
