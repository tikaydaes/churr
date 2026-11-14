// Package migrations holds the numbered schema migrations for both
// databases, embedded in the binary, and the runner that applies them.
//
// Migration 0001 is docs/schema.sql. The PostgreSQL file is that document as
// it stands. The SQLite file is produced from it by Generate, which applies
// the substitutions listed at the top of schema.sql and swaps the search
// block. Both files are checked in; a test regenerates them and fails if
// they differ from what is here, so the document and the migrations cannot
// drift. After changing docs/schema.sql run
//
//	go test ./internal/store/migrations -update
//
// Later migrations are written by hand as a pair of files with the same
// number, one per dialect.
package migrations

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Dialect names one of the two databases.
type Dialect string

// The two dialects.
const (
	Postgres Dialect = "postgres"
	SQLite   Dialect = "sqlite"
)

//go:embed postgres/*.sql sqlite/*.sql
var files embed.FS

// Migration is one numbered step.
type Migration struct {
	Version int
	Name    string
	SQL     string
}

// List returns the migrations for a dialect in order. It fails if the
// numbers have a gap or do not start at 1, because the runner relies on
// "highest applied number" meaning "everything up to it".
func List(d Dialect) ([]Migration, error) {
	entries, err := fs.ReadDir(files, string(d))
	if err != nil {
		return nil, err
	}
	var out []Migration
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".sql") || len(name) < 5 {
			continue
		}
		v, err := strconv.Atoi(name[:4])
		if err != nil {
			return nil, fmt.Errorf("migration %q: the name must start with four digits", name)
		}
		b, err := fs.ReadFile(files, string(d)+"/"+name)
		if err != nil {
			return nil, err
		}
		out = append(out, Migration{Version: v, Name: strings.TrimSuffix(name, ".sql"), SQL: string(b)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Version < out[j].Version })
	for i, m := range out {
		if m.Version != i+1 {
			return nil, fmt.Errorf("migrations for %s: expected number %04d, found %s", d, i+1, m.Name)
		}
	}
	return out, nil
}

// Latest is the highest migration number this binary knows for a dialect.
func Latest(d Dialect) (int, error) {
	ms, err := List(d)
	return len(ms), err
}

// Version returns the highest migration number applied to the database, or 0
// for an empty one.
func Version(ctx context.Context, db *sql.DB, d Dialect) (int, error) {
	// Before migration 0001 the table does not exist. Asking the catalogue
	// first avoids a failed statement, which on PostgreSQL would poison a
	// surrounding transaction.
	q := `SELECT count(*) FROM information_schema.tables WHERE table_schema = current_schema() AND table_name = 'schema_migrations'`
	if d == SQLite {
		q = `SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'schema_migrations'`
	}
	var n int
	if err := db.QueryRowContext(ctx, q).Scan(&n); err != nil {
		return 0, err
	}
	if n == 0 {
		return 0, nil
	}
	var v sql.NullInt64
	if err := db.QueryRowContext(ctx, `SELECT max(version) FROM schema_migrations`).Scan(&v); err != nil {
		return 0, err
	}
	return int(v.Int64), nil
}

// Apply runs every migration the database has not had yet, each in its own
// transaction, and records it in schema_migrations. Both databases run DDL
// inside transactions, so a failed migration leaves nothing half done. A
// database that is newer than this binary is refused: there are no down
// migrations, and running old code on a new schema would corrupt it.
func Apply(ctx context.Context, db *sql.DB, d Dialect) error {
	ms, err := List(d)
	if err != nil {
		return err
	}
	have, err := Version(ctx, db, d)
	if err != nil {
		return fmt.Errorf("reading the schema version: %w", err)
	}
	if have > len(ms) {
		return fmt.Errorf("the database is at schema %d and this version of Churr knows only up to %d; use a newer Churr or restore a backup", have, len(ms))
	}
	for _, m := range ms[have:] {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, m.SQL); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %s: %w", m.Name, err)
		}
		ins := `INSERT INTO schema_migrations (version, applied_at) VALUES ($1, $2)`
		if d == SQLite {
			ins = `INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`
		}
		if _, err := tx.ExecContext(ctx, ins, m.Version, time.Now().UnixMilli()); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %s: recording it: %w", m.Name, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("migration %s: %w", m.Name, err)
		}
	}
	return nil
}
