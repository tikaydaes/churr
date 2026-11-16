package migrations

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"strconv"
	"strings"
	"testing"
)

// file is one migration file as it is in the repository.
type file struct {
	number int
	name   string // 0007_knocks
	sha    string
}

// files reads a dialect's directory without List, which would stop at the
// first problem; these tests want to name every one.
func dialectFiles(t *testing.T, d Dialect) []file {
	t.Helper()
	entries, err := fs.ReadDir(files, string(d))
	if err != nil {
		t.Fatal(err)
	}
	var out []file
	for _, e := range entries {
		n := e.Name()
		if !strings.HasSuffix(n, ".sql") {
			continue
		}
		v, err := strconv.Atoi(n[:min(4, len(n))])
		if err != nil || len(n) < 6 || n[4] != '_' {
			t.Errorf("%s/%s: a migration is named NNNN_name.sql", d, n)
			continue
		}
		b, err := fs.ReadFile(files, string(d)+"/"+n)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(b)
		out = append(out, file{v, strings.TrimSuffix(n, ".sql"), hex.EncodeToString(sum[:])})
	}
	return out
}

// TestMigrationNumbering fails if two migrations share a number, if the
// numbers do not run 1, 2, 3 … without a gap, or if the two dialects do not
// have the same migrations under the same names.
func TestMigrationNumbering(t *testing.T) {
	byDialect := map[Dialect]map[int]string{}
	for _, d := range []Dialect{SQLite, Postgres} {
		byNum := map[int]string{}
		for _, f := range dialectFiles(t, d) {
			if other, ok := byNum[f.number]; ok {
				t.Errorf("%s: %s and %s share the number %04d", d, other, f.name, f.number)
			}
			byNum[f.number] = f.name
		}
		for i := 1; i <= len(byNum); i++ {
			if _, ok := byNum[i]; !ok {
				t.Errorf("%s: there is no migration %04d; numbers run from 0001 without gaps", d, i)
			}
		}
		byDialect[d] = byNum
	}
	lite, pg := byDialect[SQLite], byDialect[Postgres]
	for n, name := range lite {
		if pg[n] != name {
			t.Errorf("migration %04d is %q for sqlite and %q for postgres; each migration is a pair of files with the same name", n, name, pg[n])
		}
	}
	for n, name := range pg {
		if _, ok := lite[n]; !ok {
			t.Errorf("migration %s exists for postgres only", name)
		}
	}
}

const manifestPath = "manifest.txt"

// TestManifest fails if a migration listed in manifest.txt has changed, been
// renamed or gone away. A database that ran a migration has it recorded by
// name and will never run it again, so a listed file must stay byte for
// byte as it is; a change to the schema is a new migration. A migration not
// yet listed fails too, with the line to add.
func TestManifest(t *testing.T) {
	f, err := os.Open(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	type entry struct{ name, sqlite, postgres string }
	listed := map[int]entry{}
	sc := bufio.NewScanner(f)
	line := 0
	for sc.Scan() {
		line++
		l := strings.TrimSpace(sc.Text())
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		parts := strings.Fields(l)
		if len(parts) != 4 {
			t.Fatalf("%s line %d: want: number name sqlite-sha256 postgres-sha256", manifestPath, line)
		}
		n, err := strconv.Atoi(parts[0])
		if err != nil {
			t.Fatalf("%s line %d: %q is not a number", manifestPath, line, parts[0])
		}
		if _, dup := listed[n]; dup {
			t.Fatalf("%s line %d: %04d is listed twice", manifestPath, line, n)
		}
		listed[n] = entry{parts[1], parts[2], parts[3]}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}

	have := map[Dialect]map[int]file{}
	for _, d := range []Dialect{SQLite, Postgres} {
		have[d] = map[int]file{}
		for _, f := range dialectFiles(t, d) {
			have[d][f.number] = f
		}
	}
	for n, e := range listed {
		for d, sha := range map[Dialect]string{SQLite: e.sqlite, Postgres: e.postgres} {
			f, ok := have[d][n]
			switch {
			case !ok:
				t.Errorf("%s/%s.sql is listed in %s but is gone. A released migration is never removed: databases have it recorded and this build would refuse them", d, e.name, manifestPath)
			case f.name != e.name:
				t.Errorf("%s/%s.sql is listed in %s as %s. A released migration is never renamed: databases have it recorded by name and this build would refuse them", d, f.name, manifestPath, e.name)
			case f.sha != sha:
				t.Errorf("%s/%s.sql has changed since it was listed in %s. Databases that ran it will not run it again; put the change in a new migration and restore this file (for 0001, docs/schema.sql too)", d, f.name, manifestPath)
			}
		}
	}
	for n, f := range have[SQLite] {
		if _, ok := listed[n]; ok {
			continue
		}
		p, ok := have[Postgres][n]
		if !ok {
			continue // TestMigrationNumbering says so
		}
		t.Errorf("%s is not listed in %s. Add this line to the end of the file (and write it again if the migration changes before it is released):\n%s", f.name, manifestPath, manifestLine(n, f, p))
	}
}

func manifestLine(n int, lite, pg file) string {
	return fmt.Sprintf("%04d %s %s %s", n, lite.name, lite.sha, pg.sha)
}
