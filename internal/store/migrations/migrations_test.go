package migrations

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite the generated migration files from docs/schema.sql")

const schemaPath = "../../../docs/schema.sql"

// TestGeneratedFilesMatchSchema regenerates migration 0001 from
// docs/schema.sql and fails if either checked-in file differs, so the
// document and the migrations cannot drift apart.
func TestGeneratedFilesMatchSchema(t *testing.T) {
	schema, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatal(err)
	}
	pg, lite, err := Generate(schema)
	if err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string][]byte{
		filepath.Join("postgres", "0001_schema.sql"): pg,
		filepath.Join("sqlite", "0001_schema.sql"):   lite,
	} {
		if *update {
			if err := os.WriteFile(path, want, 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%s: %v (run: go test ./internal/store/migrations -update)", path, err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s is not what docs/schema.sql generates; run: go test ./internal/store/migrations -update", path)
		}
	}
}

var (
	createTable = regexp.MustCompile(`(?m)^CREATE TABLE (\w+) \(`)
	createIndex = regexp.MustCompile(`(?m)^CREATE (?:UNIQUE )?INDEX (\w+) ON`)
	columnLine  = regexp.MustCompile(`^\s+"?(\w+)"?\s+\w+`)
)

// shape lists the tables, their columns and the indexes a schema file
// declares, leaving out the search objects, which differ on purpose.
func shape(sql string) []string {
	var out []string
	for _, m := range createIndex.FindAllStringSubmatch(sql, -1) {
		if m[1] != "messages_tsv" {
			out = append(out, "index "+m[1])
		}
	}
	lines := strings.Split(sql, "\n")
	table := ""
	for _, l := range lines {
		if m := createTable.FindStringSubmatch(l); m != nil {
			table = m[1]
			out = append(out, "table "+table)
			continue
		}
		if strings.HasPrefix(l, ");") {
			table = ""
			continue
		}
		if table == "" {
			continue
		}
		code := l
		if i := strings.Index(code, "--"); i >= 0 {
			code = code[:i]
		}
		m := columnLine.FindStringSubmatch(code)
		if m == nil {
			continue
		}
		switch strings.ToUpper(m[1]) {
		case "PRIMARY", "UNIQUE", "FOREIGN", "CHECK", "GENERATED":
			continue
		}
		out = append(out, "column "+table+"."+m[1])
	}
	sort.Strings(out)
	return out
}

// TestDialectsAgree fails if the two schemas differ in tables, columns or
// indexes apart from the search block (docs/SCHEMA.md section 16).
func TestDialectsAgree(t *testing.T) {
	pgs, err := List(Postgres)
	if err != nil {
		t.Fatal(err)
	}
	lites, err := List(SQLite)
	if err != nil {
		t.Fatal(err)
	}
	if len(pgs) != len(lites) {
		t.Fatalf("postgres has %d migrations and sqlite has %d", len(pgs), len(lites))
	}
	for i := range pgs {
		if pgs[i].Name != lites[i].Name {
			t.Errorf("migration %d is named %s for postgres and %s for sqlite", i+1, pgs[i].Name, lites[i].Name)
		}
		a, b := shape(pgs[i].SQL), shape(lites[i].SQL)
		if strings.Join(a, "\n") != strings.Join(b, "\n") {
			t.Errorf("migration %s: the two dialects do not declare the same tables, columns and indexes\npostgres only: %v\nsqlite only: %v",
				pgs[i].Name, minus(a, b), minus(b, a))
		}
		if i == 0 && len(a) < 300 {
			t.Errorf("migration 0001 declares only %d tables, columns and indexes; the shape reader has gone wrong", len(a))
		}
	}
}

func minus(a, b []string) []string {
	in := map[string]bool{}
	for _, s := range b {
		in[s] = true
	}
	var out []string
	for _, s := range a {
		if !in[s] {
			out = append(out, s)
		}
	}
	return out
}

// TestSQLiteHasNoPostgresTypes checks the substitutions left nothing behind.
func TestSQLiteHasNoPostgresTypes(t *testing.T) {
	ms, err := List(SQLite)
	if err != nil {
		t.Fatal(err)
	}
	bad := regexp.MustCompile(`\b(uuid|jsonb|boolean|bytea|bigint|tsvector|gin)\b`)
	for _, m := range ms {
		for n, l := range strings.Split(m.SQL, "\n") {
			code := l
			if i := strings.Index(code, "--"); i >= 0 {
				code = code[:i]
			}
			if bad.MatchString(code) {
				t.Errorf("%s line %d still has a PostgreSQL type: %s", m.Name, n+1, strings.TrimSpace(l))
			}
		}
	}
}
