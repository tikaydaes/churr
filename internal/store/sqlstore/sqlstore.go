// Package sqlstore implements store.Store on SQL databases. One set of
// queries serves SQLite and PostgreSQL; the few places where the two differ
// go through the Dialect.
//
// It imports no database driver. The two openers, store/sqlite and
// store/postgres, import theirs, open the connections and hand them to New.
//
// Each area of store.Areas is one file here, named like its interface file
// in the store package. ../README.md explains the pattern.
package sqlstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
	"github.com/tikaydaes/churr/internal/store/migrations"
)

// Dialect is what an opener tells sqlstore about its database.
type Dialect struct {
	// Name is migrations.SQLite or migrations.Postgres.
	Name migrations.Dialect
	// UniqueViolation reports whether err is the database refusing a
	// duplicate, and names the constraint or columns if it can. Each
	// driver has its own error type, so the opener supplies this.
	UniqueViolation func(err error) (what string, ok bool)
	// ForeignKeyViolation reports whether err is the database refusing a
	// row that points at something missing.
	ForeignKeyViolation func(err error) bool
}

// Postgres reports whether the dialect is PostgreSQL.
func (d Dialect) Postgres() bool { return d.Name == migrations.Postgres }

// execer is what *sql.DB and *sql.Tx have in common.
type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// bound wraps a handle so every query passes through rebind. Queries are
// written once with "?" placeholders.
type bound struct {
	ex execer
	pg bool
}

func (b bound) ExecContext(ctx context.Context, q string, args ...any) (sql.Result, error) {
	return b.ex.ExecContext(ctx, rebind(b.pg, q), args...)
}

func (b bound) QueryContext(ctx context.Context, q string, args ...any) (*sql.Rows, error) {
	return b.ex.QueryContext(ctx, rebind(b.pg, q), args...)
}

func (b bound) QueryRowContext(ctx context.Context, q string, args ...any) *sql.Row {
	return b.ex.QueryRowContext(ctx, rebind(b.pg, q), args...)
}

// rebind turns "?" placeholders into PostgreSQL's "$1", "$2", …, leaving
// question marks inside quoted strings alone.
func rebind(pg bool, q string) string {
	if !pg || !strings.Contains(q, "?") {
		return q
	}
	var b strings.Builder
	b.Grow(len(q) + 16)
	n := 0
	inString := false
	for i := 0; i < len(q); i++ {
		c := q[i]
		switch {
		case c == '\'':
			inString = !inString
			b.WriteByte(c)
		case c == '?' && !inString:
			n++
			b.WriteByte('$')
			b.WriteString(strconv.Itoa(n))
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// queries is the implementation of every area. It runs either directly on
// the database (rd for reads, wr for writes) or inside one transaction (rd
// and wr are then the same transaction).
type queries struct {
	db *DB
	// rd is where reads go. On SQLite, outside a transaction, it is the
	// pool of read connections, so readers never wait for the writer.
	// Inside a transaction it is the transaction, so a read always sees
	// what the transaction has written. Every method that only reads uses
	// rd; one that used wr would wait for the single SQLite writer, which
	// can deadlock a fan-out that reads while a writer waits on it.
	rd execer
	// wr is where writes go, and the reads of a read-then-write method
	// (always inside atomic). On SQLite it is the single write connection.
	wr   execer
	inTx bool
}

func (q *queries) pg() bool { return q.db.dialect.Postgres() }

// forUpdate is appended to a SELECT that reads a row in order to change it.
// PostgreSQL then locks the row until the transaction ends; on SQLite the
// single writer gives the same guarantee and the clause does not exist.
func (q *queries) forUpdate() string {
	if q.pg() {
		return " FOR UPDATE"
	}
	return ""
}

// atomic runs fn so that everything it does happens together: in the
// transaction q is already in, or in a new one. Area methods that need more
// than one statement use it, so they are safe whether or not the caller
// opened a transaction.
func (q *queries) atomic(ctx context.Context, fn func(q *queries) error) error {
	if q.inTx {
		return fn(q)
	}
	return q.db.tx(ctx, fn)
}

// DB is a store on one database.
type DB struct {
	*queries
	dialect Dialect
	w, r    *sql.DB
	// path is the SQLite file, for snapshots; empty on PostgreSQL.
	path string
}

var _ store.Store = (*DB)(nil)

// Options are the connections an opener made.
type Options struct {
	Dialect Dialect
	// Write is the handle writes and transactions go through. On SQLite
	// it must be limited to one connection.
	Write *sql.DB
	// Read is the handle plain reads go through. On PostgreSQL it is the
	// same pool as Write.
	Read *sql.DB
	// Path is the SQLite file.
	Path string
}

// New runs the migrations and returns the store. It takes over the handles:
// Close closes them.
func New(ctx context.Context, o Options) (*DB, error) {
	if o.Read == nil {
		o.Read = o.Write
	}
	if err := migrations.Apply(ctx, o.Write, o.Dialect.Name); err != nil {
		return nil, err
	}
	d := &DB{dialect: o.Dialect, w: o.Write, r: o.Read, path: o.Path}
	pg := o.Dialect.Postgres()
	d.queries = &queries{db: d, rd: bound{o.Read, pg}, wr: bound{o.Write, pg}}
	return d, nil
}

// Dialect is "sqlite" or "postgres".
func (d *DB) Dialect() string { return string(d.dialect.Name) }

// Ping answers once the database has run a trivial query.
func (d *DB) Ping(ctx context.Context) error {
	var one int
	return d.r.QueryRowContext(ctx, `SELECT 1`).Scan(&one)
}

// Close releases the database.
func (d *DB) Close() error {
	err := d.w.Close()
	if d.r != d.w {
		if e := d.r.Close(); err == nil {
			err = e
		}
	}
	return err
}

// Tx runs fn in one transaction.
func (d *DB) Tx(ctx context.Context, fn func(tx store.Tx) error) error {
	return d.tx(ctx, func(q *queries) error { return fn(q) })
}

func (d *DB) tx(ctx context.Context, fn func(q *queries) error) (err error) {
	// On SQLite the write handle has one connection and is opened so that
	// a transaction takes the write lock when it begins. Transactions
	// therefore run one after another, which is what makes "read next_seq,
	// add one" safe there. On PostgreSQL the row lock does that job.
	sqlTx, err := d.w.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	done := false
	defer func() {
		// A panic in fn must not leave the transaction open: on SQLite
		// that would hold the only write connection for good.
		if !done {
			sqlTx.Rollback()
		}
	}()
	b := bound{sqlTx, d.dialect.Postgres()}
	if err := fn(&queries{db: d, rd: b, wr: b, inTx: true}); err != nil {
		done = true
		sqlTx.Rollback()
		return err
	}
	done = true
	return d.mapErr(sqlTx.Commit())
}

// Snapshot writes a consistent copy of a SQLite database to path while the
// server runs. PostgreSQL is backed up with its own tools.
func (d *DB) Snapshot(ctx context.Context, path string) error {
	if d.dialect.Postgres() {
		return fmt.Errorf("a PostgreSQL database is backed up with pg_dump, not by Churr")
	}
	_, err := d.r.ExecContext(ctx, `VACUUM INTO ?`, path)
	return err
}

// mapErr turns the database's refusals into the store's errors. A duplicate
// is model.ErrAlreadyExists unless the caller says otherwise with taken.
func (d *DB) mapErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return model.ErrNotFound
	}
	if _, ok := d.dialect.UniqueViolation(err); ok {
		return model.ErrAlreadyExists
	}
	return err
}

func (q *queries) mapErr(err error) error { return q.db.mapErr(err) }

// taken is mapErr for an insert or update whose unique column is a name
// people choose: a duplicate is model.ErrNameTaken.
func (q *queries) taken(err error) error {
	if err == nil {
		return nil
	}
	if _, ok := q.db.dialect.UniqueViolation(err); ok {
		return model.ErrNameTaken
	}
	return q.mapErr(err)
}

// changed returns model.ErrNotFound when a statement touched no row.
func (q *queries) changed(res sql.Result, err error) error {
	if err != nil {
		return q.mapErr(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return model.ErrNotFound
	}
	return nil
}

// affected returns how many rows a statement touched.
func (q *queries) affected(res sql.Result, err error) (int64, error) {
	if err != nil {
		return 0, q.mapErr(err)
	}
	n, err := res.RowsAffected()
	return n, err
}
