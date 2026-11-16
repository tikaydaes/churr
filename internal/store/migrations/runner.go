package migrations

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// The runner keeps one row per applied migration in schema_migrations:
// version, name and applied_at. Migration 0001 creates the table with
// version and applied_at only, and 0001 cannot change, so the name column
// is the runner's own: it adds the column, in the same transaction, the
// first time it finds the table without it.

// Applied is one row of schema_migrations.
type Applied struct {
	Version   int
	Name      string // "" for a row written before names were recorded
	AppliedAt int64
}

// Status is where a database stands against this build.
type Status struct {
	// Version is the highest migration number recorded, 0 for an empty
	// database.
	Version int
	// Pending are the migrations this build would apply, in order.
	Pending []Migration
	// Unnamed is true when some recorded migrations have no name yet;
	// Apply fills them in before anything else.
	Unnamed bool
}

// Current says the database needs nothing from Apply.
func (s Status) Current() bool { return len(s.Pending) == 0 && !s.Unnamed }

// MismatchError is the refusal to touch a database whose record of applied
// migrations does not fit this build. Nothing has been changed when it is
// returned.
type MismatchError struct {
	// Problems names each migration that does not fit, one line each.
	Problems []string
	// Newer is true when the database has migrations this build lacks.
	Newer bool
	// Structure is true when the record fits but tables or columns its
	// migrations create are missing (structure.go).
	Structure bool
}

func (e *MismatchError) Error() string {
	var b strings.Builder
	if e.Structure {
		b.WriteString("the database lacks tables or columns that the migrations it records as applied create, so it was left exactly as it was:")
	} else {
		b.WriteString("the database's applied migrations do not match this build of Churr, so it was left exactly as it was:")
	}
	for _, p := range e.Problems {
		b.WriteString("\n  - ")
		b.WriteString(p)
	}
	if e.Structure {
		b.WriteString("\nAn older build of Churr skipped or changed a migration on this database. Restore a backup taken before that build ran.")
	} else if e.Newer {
		b.WriteString("\nRun the build of Churr that last ran on this database, or restore the backup taken before it was upgraded.")
	} else {
		b.WriteString("\nRestore the backup taken before the upgrade, or run the build of Churr that matches this database.")
	}
	return b.String()
}

// queryer is a *sql.DB or a *sql.Tx.
type queryer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Inspect reads the database's record of applied migrations and checks it
// against this build without changing anything. It returns a
// *MismatchError when the database must not be migrated or used. A
// database whose rows have numbers only is also checked for the tables and
// columns those migrations create (structure.go).
func Inspect(ctx context.Context, db *sql.DB, d Dialect) (Status, error) {
	ms, err := List(d)
	if err != nil {
		return Status{}, err
	}
	return inspect(ctx, db, d, ms)
}

func inspect(ctx context.Context, db *sql.DB, d Dialect, ms []Migration) (Status, error) {
	rec, err := Recorded(ctx, db, d)
	if err != nil {
		return Status{}, fmt.Errorf("reading the applied migrations: %w", err)
	}
	st, err := check(ms, rec)
	if err != nil || !st.Unnamed {
		return st, err
	}
	if err := checkStructure(ctx, db, d, ms[:st.Version]); err != nil {
		return Status{}, err
	}
	return st, nil
}

// check compares the recorded migrations with the build's. Every recorded
// number must be a migration of this build with the same name; every
// migration of this build below the highest recorded number must be
// recorded; and nothing may be recorded beyond the build's last migration.
// A row with no name is taken to be the build's migration of that number:
// that is the one assumption made, for databases created before names were
// recorded.
func check(ms []Migration, rec []Applied) (Status, error) {
	var st Status
	e := &MismatchError{}
	seen := map[int]bool{}
	for _, r := range rec {
		seen[r.Version] = true
		if r.Version > st.Version {
			st.Version = r.Version
		}
		label := r.Name
		if label == "" {
			label = fmt.Sprintf("%04d", r.Version)
		}
		switch {
		case r.Version > len(ms):
			e.Newer = true
			e.Problems = append(e.Problems, fmt.Sprintf("%s is applied to the database, but this build's migrations end at %s", label, last(ms)))
		case r.Version < 1:
			e.Problems = append(e.Problems, fmt.Sprintf("%s is recorded as applied, but no migration has that number", label))
		case r.Name == "":
			st.Unnamed = true
		case r.Name != ms[r.Version-1].Name:
			e.Problems = append(e.Problems, fmt.Sprintf("%04d: the database has %s applied, but this build has %s under that number", r.Version, r.Name, ms[r.Version-1].Name))
		}
	}
	for _, m := range ms {
		if m.Version < st.Version && !seen[m.Version] {
			e.Problems = append(e.Problems, fmt.Sprintf("%s has not been applied, but later migrations have", m.Name))
		}
	}
	if len(e.Problems) > 0 {
		return Status{}, e
	}
	if st.Version < len(ms) {
		st.Pending = ms[st.Version:]
	}
	return st, nil
}

func last(ms []Migration) string {
	if len(ms) == 0 {
		return "0000"
	}
	return ms[len(ms)-1].Name
}

// Recorded returns the rows of schema_migrations in order, or none for a
// database without the table.
func Recorded(ctx context.Context, q queryer, d Dialect) ([]Applied, error) {
	// Before migration 0001 the table does not exist. Asking the catalogue
	// first avoids a failed statement, which on PostgreSQL would poison a
	// surrounding transaction.
	exists := `SELECT count(*) FROM information_schema.tables WHERE table_schema = current_schema() AND table_name = 'schema_migrations'`
	if d == SQLite {
		exists = `SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'schema_migrations'`
	}
	var n int
	if err := q.QueryRowContext(ctx, exists).Scan(&n); err != nil {
		return nil, err
	}
	if n == 0 {
		return nil, nil
	}
	named, err := hasNameColumn(ctx, q, d)
	if err != nil {
		return nil, err
	}
	sel := `SELECT version, NULL, applied_at FROM schema_migrations ORDER BY version`
	if named {
		sel = `SELECT version, name, applied_at FROM schema_migrations ORDER BY version`
	}
	rows, err := q.QueryContext(ctx, sel)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Applied
	for rows.Next() {
		var a Applied
		var name sql.NullString
		if err := rows.Scan(&a.Version, &name, &a.AppliedAt); err != nil {
			return nil, err
		}
		a.Name = name.String
		out = append(out, a)
	}
	return out, rows.Err()
}

func hasNameColumn(ctx context.Context, q queryer, d Dialect) (bool, error) {
	c := `SELECT count(*) FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = 'schema_migrations' AND column_name = 'name'`
	if d == SQLite {
		c = `SELECT count(*) FROM pragma_table_info('schema_migrations') WHERE name = 'name'`
	}
	var n int
	err := q.QueryRowContext(ctx, c).Scan(&n)
	return n > 0, err
}

// addNameColumn gives schema_migrations its name column if it lacks it.
func addNameColumn(ctx context.Context, tx *sql.Tx, d Dialect) error {
	named, err := hasNameColumn(ctx, tx, d)
	if err != nil || named {
		return err
	}
	_, err = tx.ExecContext(ctx, `ALTER TABLE schema_migrations ADD COLUMN name text`)
	return err
}

// Apply checks the database against this build (Inspect) and, only if it
// fits, brings it up to date: it first records the names of migrations
// recorded by number only, then runs each pending migration in a
// transaction of its own together with its row in schema_migrations, so a
// migration that fails leaves neither its changes nor its record behind.
// Both databases run DDL inside transactions.
func Apply(ctx context.Context, db *sql.DB, d Dialect) error {
	ms, err := List(d)
	if err != nil {
		return err
	}
	return apply(ctx, db, d, ms)
}

func apply(ctx context.Context, db *sql.DB, d Dialect, ms []Migration) error {
	st, err := inspect(ctx, db, d, ms)
	if err != nil {
		return err
	}
	// One line says what was checked and done.
	var done []string
	switch {
	case st.Version == 0:
		done = append(done, "new database")
	case st.Unnamed:
		done = append(done, fmt.Sprintf("names recorded for %d migrations applied before names were kept, assumed from their numbers; their tables and columns checked", st.Version))
	default:
		done = append(done, fmt.Sprintf("database verified against this build (%d migrations, names match)", st.Version))
	}
	if len(st.Pending) > 0 {
		var names []string
		for _, m := range st.Pending {
			names = append(names, m.Name)
		}
		done = append(done, "applied "+strings.Join(names, ", "))
	}
	ph := func(n int) string { return fmt.Sprintf("$%d", n) }
	if d == SQLite {
		ph = func(int) string { return "?" }
	}
	inTx := func(what string, f func(tx *sql.Tx) error) error {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("%s: %w", what, err)
		}
		if err := f(tx); err != nil {
			tx.Rollback()
			return fmt.Errorf("%s: %w", what, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("%s: %w", what, err)
		}
		return nil
	}
	if st.Unnamed {
		err := inTx("recording the names of applied migrations", func(tx *sql.Tx) error {
			if err := addNameColumn(ctx, tx, d); err != nil {
				return err
			}
			for _, m := range ms[:st.Version] {
				if _, err := tx.ExecContext(ctx, `UPDATE schema_migrations SET name = `+ph(1)+` WHERE version = `+ph(2)+` AND name IS NULL`, m.Name, m.Version); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	ins := `INSERT INTO schema_migrations (version, name, applied_at) VALUES (` + ph(1) + `, ` + ph(2) + `, ` + ph(3) + `)`
	for _, m := range st.Pending {
		err := inTx("migration "+m.Name, func(tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, m.SQL); err != nil {
				return err
			}
			// 0001 has just created the table, without the column.
			if err := addNameColumn(ctx, tx, d); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, ins, m.Version, m.Name, time.Now().UnixMilli()); err != nil {
				return fmt.Errorf("recording it: %w", err)
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	slog.Info("migrations: "+strings.Join(done, "; "), "dialect", string(d), "schema", len(ms))
	return nil
}
