package server

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/tikaydaes/churr/internal/store"
	"github.com/tikaydaes/churr/internal/store/migrations"
	"github.com/tikaydaes/churr/internal/store/postgres"
	"github.com/tikaydaes/churr/internal/store/sqlite"
)

// isPostgres says whether a -from or -to argument names a PostgreSQL
// database. Anything else is the path of a SQLite file.
func isPostgres(where string) bool {
	return strings.HasPrefix(where, "postgres://") || strings.HasPrefix(where, "postgresql://")
}

// MigrateDB is `churr migrate-db`: it copies a community from one database
// to another (docs/SCHEMA.md section 17). from and to are each the path of
// a SQLite file or a postgres:// address. The source must be at the schema
// version this program knows and is only read; the destination is given the
// schema and must hold nothing else. The data directory is not touched:
// files and keys serve both databases unchanged. It returns what was copied,
// table by table.
func MigrateDB(ctx context.Context, from, to string) ([]store.TableCount, error) {
	if from == "" || to == "" {
		return nil, errors.New("give both -from and -to: each is the path of a SQLite file or a postgres:// address")
	}
	if from == to {
		return nil, errors.New("-from and -to name the same database")
	}
	// The source's version is read before it is opened, because opening a
	// database brings its schema up to date, and the source must be left
	// exactly as it was.
	var have int
	var err error
	dialect := migrations.SQLite
	if isPostgres(from) {
		dialect = migrations.Postgres
		have, err = postgres.SchemaVersion(ctx, from)
	} else {
		have, err = sqlite.SchemaVersion(ctx, from)
	}
	if err != nil {
		return nil, fmt.Errorf("the source: %w", err)
	}
	latest, err := migrations.Latest(dialect)
	if err != nil {
		return nil, err
	}
	switch {
	case have == 0:
		return nil, errors.New("the source is not a Churr database: it has no schema")
	case have < latest:
		return nil, fmt.Errorf("the source is at schema %d and this version of Churr is at %d. Start the server on it once to bring it up to date, stop it, and run this again", have, latest)
	case have > latest:
		return nil, fmt.Errorf("the source is at schema %d and this version of Churr knows only up to %d; use a newer Churr", have, latest)
	}
	open := func(where string) (store.Store, error) {
		if isPostgres(where) {
			return postgres.Open(ctx, where)
		}
		return sqlite.Open(ctx, where)
	}
	src, err := open(from)
	if err != nil {
		return nil, fmt.Errorf("opening the source: %w", err)
	}
	defer src.Close()
	dst, err := open(to)
	if err != nil {
		return nil, fmt.Errorf("opening the destination: %w", err)
	}
	defer dst.Close()
	return store.CopyCounted(ctx, src, dst)
}
