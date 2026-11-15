package sqlstore

import (
	"context"
	"encoding/json"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

type settings struct{ *queries }

// Settings returns the settings area.
func (q *queries) Settings() store.Settings { return settings{q} }

func (s settings) Get(ctx context.Context, key string) (json.RawMessage, bool, error) {
	all, err := s.allSettings(ctx)
	if err != nil {
		return nil, false, err
	}
	raw, found := all[key]
	return raw, found, nil
}

func (s settings) All(ctx context.Context) (map[string]json.RawMessage, error) {
	all, err := s.allSettings(ctx)
	if err != nil {
		return nil, err
	}
	// What is kept is shared; the caller's map is its own.
	out := make(map[string]json.RawMessage, len(all))
	for k, v := range all {
		out[k] = v
	}
	return out, nil
}

func (s settings) Set(ctx context.Context, key string, value any, by model.AccountID) error {
	_, err := s.wr.ExecContext(ctx, `
		INSERT INTO settings (key, value, updated_at, updated_by) VALUES (?, ?, ?, ?)
		ON CONFLICT (key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at, updated_by = excluded.updated_by`,
		key, jsonArg(value), int64(model.Now()), id(by))
	s.touchSettings()
	return s.mapErr(err)
}

func (s settings) Delete(ctx context.Context, key string) error {
	_, err := s.wr.ExecContext(ctx, `DELETE FROM settings WHERE key = ?`, key)
	s.touchSettings()
	return err
}
