package storetest

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
	"github.com/tikaydaes/churr/internal/store/postgres"
	"github.com/tikaydaes/churr/internal/store/sqlite"
)

// PostgresEnv names the environment variable that holds the address of a
// PostgreSQL server tests may create databases on. tools/pgtest.sh starts
// one and sets it.
const PostgresEnv = "CHURR_TEST_POSTGRES"

// OpenSQLite opens a fresh SQLite store in a temporary directory that is
// removed when the test ends.
func OpenSQLite(t testing.TB) store.Store {
	t.Helper()
	s, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), sqlite.FileName))
	if err != nil {
		t.Fatalf("opening SQLite: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// PostgresURL makes a fresh, empty database on the test server and returns
// its address. The database is dropped when the test ends. The test is
// skipped when CHURR_TEST_POSTGRES is not set.
func PostgresURL(t testing.TB) string {
	t.Helper()
	base := os.Getenv(PostgresEnv)
	if base == "" {
		t.Skipf("%s is not set; run the tests through tools/pgtest.sh to include PostgreSQL", PostgresEnv)
	}
	u, err := url.Parse(base)
	if err != nil {
		t.Fatalf("%s: %v", PostgresEnv, err)
	}
	// Each test gets a database of its own, so tests can run in parallel
	// and never see each other's rows.
	name := "t_" + strings.ReplaceAll(model.NewID[string](), "-", "")
	admin, err := sql.Open("pgx", base)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	if _, err := admin.Exec(`CREATE DATABASE ` + name); err != nil {
		t.Fatalf("creating a test database: %v", err)
	}
	t.Cleanup(func() {
		admin, err := sql.Open("pgx", base)
		if err != nil {
			return
		}
		defer admin.Close()
		admin.Exec(fmt.Sprintf(`DROP DATABASE IF EXISTS %s WITH (FORCE)`, name))
	})
	u.Path = "/" + name
	return u.String()
}

// OpenPostgres opens a fresh PostgreSQL store, or skips the test when
// CHURR_TEST_POSTGRES is not set.
func OpenPostgres(t testing.TB) store.Store {
	t.Helper()
	s, err := postgres.Open(context.Background(), PostgresURL(t))
	if err != nil {
		t.Fatalf("opening PostgreSQL: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// UsePostgres reports whether tests that run on one backend should use
// PostgreSQL: when CHURR_TEST_POSTGRES is set. The API tests use this, so
// `go test ./...` covers SQLite and the same command under tools/pgtest.sh
// covers PostgreSQL.
func UsePostgres() bool { return os.Getenv(PostgresEnv) != "" }
