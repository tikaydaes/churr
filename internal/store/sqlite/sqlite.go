// Package sqlite opens the SQLite backend: the default database, one file
// in the data directory, with FTS5 for search and no cgo.
//
// Importing this package makes store.Open able to open SQLite.
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"

	"github.com/tikaydaes/churr/internal/store"
	"github.com/tikaydaes/churr/internal/store/migrations"
	"github.com/tikaydaes/churr/internal/store/sqlstore"
)

// FileName is the database file inside the data directory.
const FileName = "churr.db"

func init() {
	store.Register("sqlite", func(ctx context.Context, dataDir, _ string) (store.Store, error) {
		return Open(ctx, filepath.Join(dataDir, FileName))
	})
}

// readConnections is the size of the pool plain reads use. Writes always
// share one connection.
const readConnections = 8

// Open opens (creating if needed) the database file at path and runs the
// migrations.
func Open(ctx context.Context, path string) (store.Store, error) {
	// Settings every connection needs, given in the address so the pool
	// applies them to each connection it opens:
	//   foreign_keys   the schema relies on cascades (docs/SCHEMA.md 1)
	//   journal_mode   WAL, so readers do not wait for the writer
	//   secure_delete  erased text must not linger in freed pages
	//   busy_timeout   wait for a lock instead of failing at once
	//   synchronous    NORMAL is safe with WAL and much faster than FULL
	pragmas := []string{"foreign_keys(ON)", "journal_mode(WAL)", "secure_delete(ON)", "busy_timeout(10000)", "synchronous(NORMAL)"}
	dsn := func(extra ...string) string {
		v := url.Values{}
		for _, p := range pragmas {
			v.Add("_pragma", p)
		}
		for i := 0; i+1 < len(extra); i += 2 {
			v.Add(extra[i], extra[i+1])
		}
		return "file:" + filepath.ToSlash(path) + "?" + v.Encode()
	}

	// One connection for writes, opened so that a transaction takes the
	// write lock as it begins. Two transactions then never interleave,
	// and none fails halfway because another got the lock first.
	w, err := sql.Open("sqlite", dsn("_txlock", "immediate"))
	if err != nil {
		return nil, err
	}
	w.SetMaxOpenConns(1)
	if err := w.PingContext(ctx); err != nil {
		w.Close()
		return nil, fmt.Errorf("opening %s: %w", path, err)
	}
	r, err := sql.Open("sqlite", dsn())
	if err != nil {
		w.Close()
		return nil, err
	}
	r.SetMaxOpenConns(readConnections)

	s, err := sqlstore.New(ctx, sqlstore.Options{
		Dialect: sqlstore.Dialect{
			Name:                migrations.SQLite,
			UniqueViolation:     uniqueViolation,
			ForeignKeyViolation: foreignKeyViolation,
		},
		Write: w, Read: r, Path: path,
	})
	if err != nil {
		w.Close()
		r.Close()
		return nil, err
	}
	return s, nil
}

func uniqueViolation(err error) (string, bool) {
	var e *sqlite.Error
	if !errors.As(err, &e) {
		return "", false
	}
	switch e.Code() {
	case sqlite3.SQLITE_CONSTRAINT_UNIQUE, sqlite3.SQLITE_CONSTRAINT_PRIMARYKEY:
		// The message reads "… UNIQUE constraint failed: table.column".
		msg := e.Error()
		if i := strings.LastIndex(msg, "failed: "); i >= 0 {
			msg = msg[i+len("failed: "):]
			if j := strings.Index(msg, " ("); j >= 0 {
				msg = msg[:j]
			}
		}
		return msg, true
	}
	return "", false
}

func foreignKeyViolation(err error) bool {
	var e *sqlite.Error
	return errors.As(err, &e) && e.Code() == sqlite3.SQLITE_CONSTRAINT_FOREIGNKEY
}
