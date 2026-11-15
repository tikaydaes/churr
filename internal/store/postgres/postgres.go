// Package postgres opens the PostgreSQL backend.
//
// Importing this package makes store.Open able to open PostgreSQL.
package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	neturl "net/url"
	"strconv"

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

// connections is the size of the pool unless the address says otherwise.
const connections = 20

// poolSize takes pool_max_conns out of the address, where
// docs/OPERATIONS.md section 9 says a different pool size goes, and returns
// the address without it. Left in, the parameter went to PostgreSQL as a
// setting of the session, which refused the connection: "unrecognized
// configuration parameter".
func poolSize(address string) (string, int, error) {
	u, err := neturl.Parse(address)
	if err != nil {
		return address, connections, nil // not a URL: the driver says what is wrong with it
	}
	q := u.Query()
	v := q.Get("pool_max_conns")
	if v == "" {
		return address, connections, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		return "", 0, fmt.Errorf("pool_max_conns in the database address is %q; it takes a number from 1 up", v)
	}
	q.Del("pool_max_conns")
	u.RawQuery = q.Encode()
	return u.String(), n, nil
}

// Open connects to the database at url (postgres://user:password@host/name)
// and runs the migrations.
func Open(ctx context.Context, url string) (store.Store, error) {
	url, pool, err := poolSize(url)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("pgx", url)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(pool)
	db.SetMaxIdleConns(pool)
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
