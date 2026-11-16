package migrations

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"
)

// The runner's tests run on SQLite always and on PostgreSQL under
// tools/pgtest.sh. This package cannot import storetest (storetest imports
// the store, which imports this package), so it makes its own databases.
const postgresEnv = "CHURR_TEST_POSTGRES"

// eachDB runs f once per database, each time on an empty one.
func eachDB(t *testing.T, f func(t *testing.T, db *sql.DB, d Dialect)) {
	t.Run("sqlite", func(t *testing.T) {
		// Not t.TempDir: a second subtest's name, and so its directory,
		// ends in #01, which a file: address reads as a fragment.
		dir, err := os.MkdirTemp("", "churr-migrations")
		if err != nil {
			t.Fatal(err)
		}
		db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(filepath.Join(dir, "m.db"))+"?_pragma=foreign_keys(ON)&_txlock=immediate")
		if err != nil {
			t.Fatal(err)
		}
		db.SetMaxOpenConns(1)
		t.Cleanup(func() {
			db.Close()
			os.RemoveAll(dir)
		})
		f(t, db, SQLite)
	})
	t.Run("postgres", func(t *testing.T) {
		base := os.Getenv(postgresEnv)
		if base == "" {
			t.Skipf("%s is not set; run the tests through tools/pgtest.sh to include PostgreSQL", postgresEnv)
		}
		u, err := url.Parse(base)
		if err != nil {
			t.Fatal(err)
		}
		var b [8]byte
		rand.Read(b[:])
		name := "m_" + hex.EncodeToString(b[:])
		admin, err := sql.Open("pgx", base)
		if err != nil {
			t.Fatal(err)
		}
		defer admin.Close()
		if _, err := admin.Exec(`CREATE DATABASE ` + name); err != nil {
			t.Fatal(err)
		}
		u.Path = "/" + name
		db, err := sql.Open("pgx", u.String())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			db.Close()
			if a, err := sql.Open("pgx", base); err == nil {
				a.Exec(`DROP DATABASE IF EXISTS ` + name + ` WITH (FORCE)`)
				a.Close()
			}
		})
		f(t, db, Postgres)
	})
}

// small is a stand-in list with the same first step as the real one: 0001
// creates schema_migrations with version and applied_at only.
func small() []Migration {
	return []Migration{
		{1, "0001_base", `CREATE TABLE schema_migrations (version integer PRIMARY KEY, applied_at bigint NOT NULL);
CREATE TABLE t (id integer PRIMARY KEY);`},
		{2, "0002_a", `ALTER TABLE t ADD COLUMN a integer;`},
		{3, "0003_b", `CREATE TABLE b (id integer PRIMARY KEY);`},
	}
}

func exec(t *testing.T, db *sql.DB, qs ...string) {
	t.Helper()
	for _, q := range qs {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
}

// numbersOnly makes the database a runner before names were recorded would
// have left: the given migrations run and recorded by number.
func numbersOnly(t *testing.T, db *sql.DB, ms []Migration, versions ...int) {
	t.Helper()
	for _, v := range versions {
		exec(t, db, ms[v-1].SQL, fmt.Sprintf(`INSERT INTO schema_migrations (version, applied_at) VALUES (%d, 1)`, v))
	}
}

// snapshot is every table and column of the database and every row of
// schema_migrations, as text: what "nothing was changed" compares.
func snapshot(t *testing.T, db *sql.DB, d Dialect) []string {
	t.Helper()
	q := `SELECT table_name, column_name FROM information_schema.columns WHERE table_schema = current_schema()`
	if d == SQLite {
		q = `SELECT m.name, p.name FROM sqlite_master m, pragma_table_info(m.name) p WHERE m.type = 'table'`
	}
	rows, err := db.Query(q)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for rows.Next() {
		var a, b string
		if err := rows.Scan(&a, &b); err != nil {
			t.Fatal(err)
		}
		out = append(out, "column "+a+"."+b)
	}
	rows.Close()
	rec, err := Recorded(context.Background(), db, d)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rec {
		out = append(out, fmt.Sprintf("row %d %q %d", r.Version, r.Name, r.AppliedAt))
	}
	sort.Strings(out)
	return out
}

func names(t *testing.T, db *sql.DB, d Dialect) []string {
	t.Helper()
	rec, err := Recorded(context.Background(), db, d)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, r := range rec {
		if r.AppliedAt <= 0 {
			t.Errorf("%s has applied_at %d", r.Name, r.AppliedAt)
		}
		out = append(out, r.Name)
	}
	return out
}

// refused checks that err is the runner's refusal, that it names each of
// want, and that the database is as it was before.
func refused(t *testing.T, err error, db *sql.DB, d Dialect, before []string, want ...string) {
	t.Helper()
	var me *MismatchError
	if !errors.As(err, &me) {
		t.Fatalf("got %v, want a refusal", err)
	}
	for _, w := range want {
		if !strings.Contains(err.Error(), w) {
			t.Errorf("the refusal does not name %s:\n%v", w, err)
		}
	}
	if !strings.Contains(err.Error(), "backup") {
		t.Errorf("the refusal does not say what to do:\n%v", err)
	}
	if after := snapshot(t, db, d); !reflect.DeepEqual(before, after) {
		t.Fatalf("the database changed although it was refused\nbefore: %v\nafter:  %v", before, after)
	}
}

func TestApplyFresh(t *testing.T) {
	eachDB(t, func(t *testing.T, db *sql.DB, d Dialect) {
		ctx := context.Background()
		ms := small()
		if err := apply(ctx, db, d, ms); err != nil {
			t.Fatal(err)
		}
		if got := names(t, db, d); !reflect.DeepEqual(got, []string{"0001_base", "0002_a", "0003_b"}) {
			t.Fatalf("recorded %v", got)
		}
		before := snapshot(t, db, d)
		if err := apply(ctx, db, d, ms); err != nil {
			t.Fatal(err)
		}
		if after := snapshot(t, db, d); !reflect.DeepEqual(before, after) {
			t.Fatalf("a second run changed the database\nbefore: %v\nafter:  %v", before, after)
		}
	})
}

// A database whose rows have numbers only gets the names of this build's
// migrations of those numbers, then the rest.
func TestApplyBackfillsNames(t *testing.T) {
	eachDB(t, func(t *testing.T, db *sql.DB, d Dialect) {
		ms := small()
		numbersOnly(t, db, ms, 1, 2)
		st, err := inspect(context.Background(), db, d, ms)
		if err != nil || !st.Unnamed || st.Version != 2 || len(st.Pending) != 1 {
			t.Fatalf("inspecting a numbers-only database: %+v, %v", st, err)
		}
		if err := apply(context.Background(), db, d, ms); err != nil {
			t.Fatal(err)
		}
		if got := names(t, db, d); !reflect.DeepEqual(got, []string{"0001_base", "0002_a", "0003_b"}) {
			t.Fatalf("recorded %v", got)
		}
	})
}

// A recorded migration whose name this build has under another number's
// file, as when a placeholder was replaced, is refused.
func TestApplyRefusesRenamed(t *testing.T) {
	eachDB(t, func(t *testing.T, db *sql.DB, d Dialect) {
		ms := small()
		if err := apply(context.Background(), db, d, ms[:2]); err != nil {
			t.Fatal(err)
		}
		before := snapshot(t, db, d)
		renamed := small()
		renamed[1].Name = "0002_other"
		err := apply(context.Background(), db, d, renamed)
		refused(t, err, db, d, before, "0002_a", "0002_other")
	})
}

// A migration below the highest recorded one that was never applied is
// refused, not skipped; on a numbers-only database the name column is not
// added either.
func TestApplyRefusesGap(t *testing.T) {
	eachDB(t, func(t *testing.T, db *sql.DB, d Dialect) {
		ms := small()
		numbersOnly(t, db, ms, 1, 3)
		before := snapshot(t, db, d)
		err := apply(context.Background(), db, d, ms)
		refused(t, err, db, d, before, "0002_a has not been applied")
	})
}

func TestApplyRefusesNewer(t *testing.T) {
	eachDB(t, func(t *testing.T, db *sql.DB, d Dialect) {
		ms := small()
		if err := apply(context.Background(), db, d, ms); err != nil {
			t.Fatal(err)
		}
		before := snapshot(t, db, d)
		err := apply(context.Background(), db, d, ms[:2])
		refused(t, err, db, d, before, "0003_b is applied to the database", "end at 0002_a")
		var me *MismatchError
		if errors.As(err, &me) && !me.Newer {
			t.Error("the refusal does not say the database is newer")
		}
	})
	eachDB(t, func(t *testing.T, db *sql.DB, d Dialect) {
		ms := small()
		numbersOnly(t, db, ms, 1, 2, 3)
		before := snapshot(t, db, d)
		err := apply(context.Background(), db, d, ms[:2])
		refused(t, err, db, d, before, "0003 is applied to the database")
	})
}

// A migration that fails after its first statement leaves neither that
// statement's change nor a record of itself, and the next run applies the
// corrected migration.
func TestApplyFailureRollsBack(t *testing.T) {
	eachDB(t, func(t *testing.T, db *sql.DB, d Dialect) {
		ctx := context.Background()
		ms := small()
		bad := small()
		bad[2].SQL = `CREATE TABLE b (id integer PRIMARY KEY);
CREATE TABLE half (id integer);
ALTER TABLE t ADD COLUMN c integer;
INSERT INTO no_such_table VALUES (1);`
		if err := apply(ctx, db, d, bad[:2]); err != nil {
			t.Fatal(err)
		}
		before := snapshot(t, db, d)
		err := apply(ctx, db, d, bad)
		if err == nil || !strings.Contains(err.Error(), "0003_b") {
			t.Fatalf("a failing migration gave %v", err)
		}
		if after := snapshot(t, db, d); !reflect.DeepEqual(before, after) {
			t.Fatalf("a failed migration left changes behind\nbefore: %v\nafter:  %v", before, after)
		}
		if err := apply(ctx, db, d, ms); err != nil {
			t.Fatal(err)
		}
		if got := names(t, db, d); len(got) != 3 {
			t.Fatalf("recorded %v", got)
		}
	})
	// The very first migration fails: the database stays empty, without
	// even schema_migrations.
	eachDB(t, func(t *testing.T, db *sql.DB, d Dialect) {
		bad := small()
		bad[0].SQL += "\nINSERT INTO no_such_table VALUES (1);"
		if err := apply(context.Background(), db, d, bad); err == nil {
			t.Fatal("a failing first migration was accepted")
		}
		if s := snapshot(t, db, d); len(s) != 0 {
			t.Fatalf("a failed first migration left %v", s)
		}
	})
}

// The real migrations: a database at 0006 recorded by number only (as every
// database was before names were recorded) is brought to the end with
// names; one where 0007 is recorded under another name, the upgrade that
// prompted the check, is refused untouched.
func TestApplyRealMigrations(t *testing.T) {
	eachDB(t, func(t *testing.T, db *sql.DB, d Dialect) {
		ms, err := List(d)
		if err != nil {
			t.Fatal(err)
		}
		numbersOnly(t, db, ms, 1, 2, 3, 4, 5, 6)
		if err := Apply(context.Background(), db, d); err != nil {
			t.Fatal(err)
		}
		got := names(t, db, d)
		if len(got) != len(ms) || got[6] != "0007_knocks" || got[0] != "0001_schema" {
			t.Fatalf("recorded %v", got)
		}
		st, err := Inspect(context.Background(), db, d)
		if err != nil || !st.Current() || st.Version != len(ms) {
			t.Fatalf("after Apply: %+v, %v", st, err)
		}
	})
	eachDB(t, func(t *testing.T, db *sql.DB, d Dialect) {
		ms, err := List(d)
		if err != nil {
			t.Fatal(err)
		}
		if err := apply(context.Background(), db, d, ms[:6]); err != nil {
			t.Fatal(err)
		}
		exec(t, db, `INSERT INTO schema_migrations (version, name, applied_at) VALUES (7, '0007_reserved', 1)`)
		before := snapshot(t, db, d)
		err = Apply(context.Background(), db, d)
		refused(t, err, db, d, before, "0007_reserved", "0007_knocks")
		if _, err := Inspect(context.Background(), db, d); err == nil {
			t.Fatal("Inspect accepted the database")
		}
	})
}
