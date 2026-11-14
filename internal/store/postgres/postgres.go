// Package postgres opens the PostgreSQL backend.
//
// Importing this package makes store.Open able to open PostgreSQL.
package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" driver

	"github.com/tikaydaes/churr/internal/store"
	"github.com/tikaydaes/churr/internal/store/migrations"
	"github.com/tikaydaes/churr/internal/store/sqlstore"
)

func init() {
	store.Register("postgres", func(ctx context.Context, _, dbURL string) (store.Store, error) {
		return Open(ctx, dbURL)
	})
}

// connections is the size of the pool.
const connections = 20

// Open connects to the database at url (postgres://user:password@host/name)
// and runs the migrations.
func Open(ctx context.Context, url string) (store.Store, error) {
	db, err := sql.Open("pgx", url)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(connections)
	db.SetMaxIdleConns(connections)
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("connecting to PostgreSQL: %w", err)
	}
	s, err := sqlstore.New(ctx, sqlstore.Options{
		Dialect: sqlstore.Dialect{
			Name:                migrations.Postgres,
			UniqueViolation:     uniqueViolation,
			ForeignKeyViolation: foreignKeyViolation,
		},
		Write: db, Read: db,
	})
	if err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func uniqueViolation(err error) (string, bool) {
	var e *pgconn.PgError
	if errors.As(err, &e) && e.Code == "23505" {
		return e.ConstraintName, true
	}
	return "", false
}

func foreignKeyViolation(err error) bool {
	var e *pgconn.PgError
	return errors.As(err, &e) && e.Code == "23503"
}
