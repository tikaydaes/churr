package sqlstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

type previews struct{ *queries }

// Previews returns the link-preview cache.
func (q *queries) Previews() store.Previews { return previews{q} }

func scanPreview(s scanner) (store.PreviewEntry, error) {
	var e store.PreviewEntry
	var card json.RawMessage
	if err := s.Scan(blob(&e.Hash), &e.URL, js(&card), tm(&e.FetchedAt)); err != nil {
		return e, err
	}
	if len(card) > 0 && string(card) != "null" {
		e.Card = &model.Card{}
		if err := json.Unmarshal(card, e.Card); err != nil {
			return e, err
		}
	}
	return e, nil
}

func (p previews) get(ctx context.Context, ex execer, hash []byte) (store.PreviewEntry, bool, error) {
	e, err := scanPreview(ex.QueryRowContext(ctx, `SELECT url_hash, url, card, fetched_at FROM preview_cache WHERE url_hash = ?`, hash))
	if errors.Is(err, sql.ErrNoRows) {
		return e, false, nil
	}
	return e, err == nil, err
}

func (p previews) Get(ctx context.Context, hash []byte) (store.PreviewEntry, bool, error) {
	return p.get(ctx, p.rd, hash)
}

func (p previews) Put(ctx context.Context, e store.PreviewEntry) (store.PreviewEntry, bool, error) {
	var old store.PreviewEntry
	var replaced bool
	err := p.atomic(ctx, func(q *queries) error {
		var err error
		if old, replaced, err = (previews{q}).get(ctx, q.wr, e.Hash); err != nil {
			return err
		}
		var card any
		if e.Card != nil {
			card = jsonArg(e.Card)
		}
		_, err = q.wr.ExecContext(ctx, `
			INSERT INTO preview_cache (url_hash, url, card, fetched_at) VALUES (?, ?, ?, ?)
			ON CONFLICT (url_hash) DO UPDATE SET url = excluded.url, card = excluded.card, fetched_at = excluded.fetched_at`,
			e.Hash, e.URL, card, int64(e.FetchedAt))
		return q.mapErr(err)
	})
	return old, replaced, err
}

func (p previews) Stale(ctx context.Context, cardBefore, emptyBefore model.Time, limit int) ([]store.PreviewEntry, error) {
	rows, err := p.rd.QueryContext(ctx, `
		SELECT url_hash, url, card, fetched_at FROM preview_cache
		WHERE fetched_at < ? OR (card IS NULL AND fetched_at < ?) ORDER BY fetched_at LIMIT ?`,
		int64(cardBefore), int64(emptyBefore), store.Page{Limit: limit}.Clamp())
	return collect(rows, err, scanPreview)
}

func (p previews) Delete(ctx context.Context, hash []byte) error {
	_, err := p.wr.ExecContext(ctx, `DELETE FROM preview_cache WHERE url_hash = ?`, hash)
	return p.mapErr(err)
}
