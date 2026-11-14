package sqlstore

import (
	"context"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

type sections struct{ *queries }

// Sections returns the sections area.
func (q *queries) Sections() store.Sections { return sections{q} }

func scanSection(s scanner) (model.Section, error) {
	var out model.Section
	err := s.Scan(str(&out.ID), &out.Name, &out.Position)
	return out, err
}

func (s sections) Create(ctx context.Context, sec *model.Section) error {
	if sec.ID == "" {
		sec.ID = model.NewID[model.SectionID]()
	}
	_, err := s.wr.ExecContext(ctx, `INSERT INTO sections (id, name, position) VALUES (?, ?, ?)`,
		string(sec.ID), sec.Name, sec.Position)
	return s.mapErr(err)
}

func (s sections) Get(ctx context.Context, section model.SectionID) (model.Section, error) {
	out, err := scanSection(s.rd.QueryRowContext(ctx, `SELECT id, name, position FROM sections WHERE id = ?`, string(section)))
	return out, s.mapErr(err)
}

func (s sections) List(ctx context.Context) ([]model.Section, error) {
	// The id last, so two sections with the same position and name
	// still come back in the same order every time.
	rows, err := s.rd.QueryContext(ctx, `SELECT id, name, position FROM sections ORDER BY position, name, id`)
	return collect(rows, err, scanSection)
}

func (s sections) Update(ctx context.Context, sec model.Section) error {
	return s.changed(s.wr.ExecContext(ctx, `UPDATE sections SET name = ?, position = ? WHERE id = ?`,
		sec.Name, sec.Position, string(sec.ID)))
}

func (s sections) Delete(ctx context.Context, section model.SectionID) error {
	// The foreign key sets conversations.section_id to null.
	return s.changed(s.wr.ExecContext(ctx, `DELETE FROM sections WHERE id = ?`, string(section)))
}
