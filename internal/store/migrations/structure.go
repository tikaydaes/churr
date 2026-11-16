package migrations

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"sort"
)

// A database from before names were recorded has rows with numbers only,
// and the runner takes each to be this build's migration of that number. A
// migration that an older build skipped, or ran under the same number with
// different SQL, cannot be told from the record there. To narrow that gap,
// such a database is checked once, before its names are filled in: every
// table and column that this build's migrations up to its recorded number
// create must exist. The expected set is found by running those migrations
// on a scratch database of the same kind: an in-memory SQLite database, or
// PostgreSQL's temporary schema inside a transaction that is rolled back.
// Columns are compared by name only; types, indexes and constraints are not.

// columns is a set of "table.column".
type columns map[string]bool

// checkStructure returns a *MismatchError naming every table and column the
// first n migrations create that the database lacks.
func checkStructure(ctx context.Context, db *sql.DB, d Dialect, ms []Migration) error {
	want, err := expectedColumns(ctx, db, d, ms)
	if err != nil {
		return fmt.Errorf("working out the tables this build's migrations create: %w", err)
	}
	have, err := readColumns(ctx, db, d)
	if err != nil {
		return fmt.Errorf("reading the database's tables: %w", err)
	}
	tables := map[string]bool{}
	for tc := range have {
		tables[tableOf(tc)] = true
	}
	var missing []string
	gone := map[string]bool{}
	for tc := range want {
		t := tableOf(tc)
		switch {
		case have[tc]:
		case !tables[t]:
			if !gone[t] {
				gone[t] = true
				missing = append(missing, fmt.Sprintf("table %s is missing", t))
			}
		default:
			missing = append(missing, fmt.Sprintf("column %s is missing", tc))
		}
	}
	if len(missing) == 0 {
		return nil
	}
	sort.Strings(missing)
	return &MismatchError{Structure: true, Problems: missing}
}

func tableOf(tc string) string {
	for i := 0; i < len(tc); i++ {
		if tc[i] == '.' {
			return tc[:i]
		}
	}
	return tc
}

// expectedColumns runs ms on a scratch database of the same kind as db and
// reads back its tables and columns.
func expectedColumns(ctx context.Context, db *sql.DB, d Dialect, ms []Migration) (columns, error) {
	if d == SQLite {
		scratch := sql.OpenDB(connector{db.Driver(), ":memory:"})
		defer scratch.Close()
		// Every connection to :memory: is a database of its own.
		scratch.SetMaxOpenConns(1)
		for _, m := range ms {
			if _, err := scratch.ExecContext(ctx, m.SQL); err != nil {
				return nil, fmt.Errorf("%s: %w", m.Name, err)
			}
		}
		return readColumns(ctx, scratch, d)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	// Unqualified names then create and find temporary tables, which the
	// rollback removes with everything else.
	if _, err := tx.ExecContext(ctx, `SET LOCAL search_path TO pg_temp`); err != nil {
		return nil, err
	}
	for _, m := range ms {
		if _, err := tx.ExecContext(ctx, m.SQL); err != nil {
			return nil, fmt.Errorf("%s: %w", m.Name, err)
		}
	}
	return readColumns(ctx, tx, d)
}

// readColumns lists the tables and columns of the current schema, leaving
// out schema_migrations, whose name column is the runner's own.
func readColumns(ctx context.Context, q queryer, d Dialect) (columns, error) {
	sel := `SELECT table_name, column_name FROM information_schema.columns WHERE table_schema = current_schema()`
	if d == SQLite {
		sel = `SELECT m.name, p.name FROM sqlite_master m, pragma_table_info(m.name) p WHERE m.type = 'table' AND m.name NOT LIKE 'sqlite_%'`
	}
	rows, err := q.QueryContext(ctx, sel)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := columns{}
	for rows.Next() {
		var t, c string
		if err := rows.Scan(&t, &c); err != nil {
			return nil, err
		}
		if t != "schema_migrations" {
			out[t+"."+c] = true
		}
	}
	return out, rows.Err()
}

// connector opens connections of a driver to one address.
type connector struct {
	d   driver.Driver
	dsn string
}

func (c connector) Connect(context.Context) (driver.Conn, error) { return c.d.Open(c.dsn) }
func (c connector) Driver() driver.Driver                       { return c.d }
