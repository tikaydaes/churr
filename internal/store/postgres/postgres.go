// Package postgres opens the PostgreSQL backend.
//
// Importing this package makes store.Open able to open PostgreSQL.
package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/stdlib" // registers the "pgx" driver

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
			Batch:               batch,
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

// batch sends several statements on one connection as one round trip
// (pgx's pipeline) and reads their answers in order. The statements run as
// if sent one after another: one that fails stops the ones after it, and an
// explicit BEGIN among them leaves its transaction open afterwards.
func batch(ctx context.Context, conn *sql.Conn, stmts []sqlstore.BatchStmt) error {
	return conn.Raw(func(driverConn any) error {
		pc, ok := driverConn.(*stdlib.Conn)
		if !ok {
			return fmt.Errorf("postgres: a connection of type %T cannot batch", driverConn)
		}
		b := &pgx.Batch{}
		for _, s := range stmts {
			b.Queue(s.SQL, s.Args...)
		}
		res := pc.Conn().SendBatch(ctx, b)
		var first error
		for _, s := range stmts {
			var err error
			if len(s.Scan) > 0 {
				err = res.QueryRow().Scan(s.Scan...)
				if errors.Is(err, pgx.ErrNoRows) {
					err = sql.ErrNoRows
				}
			} else {
				_, err = res.Exec()
			}
			if err != nil && first == nil {
				first = err
			}
		}
		if err := res.Close(); err != nil && first == nil {
			first = err
		}
		return first
	})
}
