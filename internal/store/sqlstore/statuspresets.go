package sqlstore

import (
	"context"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

type statusPresets struct{ *queries }

// StatusPresets returns the status presets area.
func (q *queries) StatusPresets() store.StatusPresets { return statusPresets{q} }

const presetCols = `id, owner_id, emoji, text, duration_seconds, dnd, away, availability, position`

func scanPreset(s scanner) (model.StatusPreset, error) {
	var p model.StatusPreset
	err := s.Scan(&p.ID, str(&p.OwnerID), str(&p.Emoji), &p.Text, numPtr(&p.DurationSeconds), flag(&p.PauseNotifications),
		flag(&p.AppearAway), str(&p.Availability), &p.Position)
	p.Community = p.OwnerID == ""
	return p, err
}

func (sp statusPresets) Create(ctx context.Context, p *model.StatusPreset) error {
	if p.ID == "" {
		p.ID = model.NewID[string]()
	}
	p.Community = p.OwnerID == ""
	_, err := sp.wr.ExecContext(ctx, `
		INSERT INTO status_presets (`+presetCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID, id(p.OwnerID), id(p.Emoji), p.Text, nullInt(p.DurationSeconds), p.PauseNotifications, p.AppearAway,
		id(p.Availability), p.Position)
	return sp.mapErr(err)
}

func (sp statusPresets) Get(ctx context.Context, preset string) (model.StatusPreset, error) {
	p, err := scanPreset(sp.rd.QueryRowContext(ctx, `SELECT `+presetCols+` FROM status_presets WHERE id = ?`, preset))
	return p, sp.mapErr(err)
}

func (sp statusPresets) Update(ctx context.Context, preset string, fn func(*model.StatusPreset) error) (model.StatusPreset, error) {
	var out model.StatusPreset
	err := sp.atomic(ctx, func(q *queries) error {
		p, err := scanPreset(q.wr.QueryRowContext(ctx, `SELECT `+presetCols+` FROM status_presets WHERE id = ?`+q.forUpdate(), preset))
		if err != nil {
			return q.mapErr(err)
		}
		if err := fn(&p); err != nil {
			return err
		}
		_, err = q.wr.ExecContext(ctx, `
			UPDATE status_presets SET emoji = ?, text = ?, duration_seconds = ?, dnd = ?, away = ?, availability = ?, position = ?
			WHERE id = ?`,
			id(p.Emoji), p.Text, nullInt(p.DurationSeconds), p.PauseNotifications, p.AppearAway, id(p.Availability), p.Position, preset)
		out = p
		return q.mapErr(err)
	})
	return out, err
}

func (sp statusPresets) Delete(ctx context.Context, preset string) error {
	return sp.changed(sp.wr.ExecContext(ctx, `DELETE FROM status_presets WHERE id = ?`, preset))
}

func (sp statusPresets) List(ctx context.Context, owner model.AccountID) ([]model.StatusPreset, error) {
	// Community presets first; the CASE puts a row with no owner ahead of
	// one with, on both databases, which sort NULL differently.
	rows, err := sp.rd.QueryContext(ctx, `
		SELECT `+presetCols+` FROM status_presets WHERE owner_id IS NULL OR owner_id = ?
		ORDER BY CASE WHEN owner_id IS NULL THEN 0 ELSE 1 END, position, id`, id(owner))
	return collect(rows, err, scanPreset)
}
