// Package migrations holds the numbered schema migrations for both
// databases, embedded in the binary, and the runner that applies them.
//
// Migration 0001 is docs/schema.sql. The PostgreSQL file is that document as
// it stands. The SQLite file is produced from it by Generate, which applies
// the substitutions listed at the top of schema.sql and swaps the search
// block. Both files are checked in; a test regenerates them and fails if
// they differ from what is here, so the document and the migrations cannot
// drift (go test ./internal/store/migrations -update rewrites them).
//
// Later migrations are written by hand as a pair of files with the same
// number, one per dialect. Every file is listed with its SHA-256 in
// manifest.txt, and a test fails if a listed file changes, is renamed or
// goes away: a database that has run a migration must never meet different
// SQL under the same name. That includes 0001, so docs/schema.sql does not
// change either.
package migrations

import (
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
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
// numbers have a gap or do not start at 1: the runner checks a database
// against this list by number, so the numbers must be 1 to N.
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
