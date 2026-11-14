package migrations

import (
	"fmt"
	"regexp"
	"strings"
)

// header is put at the top of each generated file.
const header = "-- Generated from docs/schema.sql by internal/store/migrations. Do not edit.\n" +
	"-- Change docs/schema.sql and run: go test ./internal/store/migrations -update\n\n"

// typeSubstitutions are the ones listed at the top of docs/schema.sql.
var typeSubstitutions = []struct {
	re *regexp.Regexp
	to string
}{
	{regexp.MustCompile(`\buuid\b`), "TEXT"},
	{regexp.MustCompile(`\bjsonb\b`), "TEXT"},
	{regexp.MustCompile(`\bboolean\b`), "INTEGER"},
	{regexp.MustCompile(`\bbytea\b`), "BLOB"},
	{regexp.MustCompile(`\bbigint\b`), "INTEGER"},
}

// A boolean column's default is written false or true in the document.
// SQLite stores 0 and 1, so the defaults are written that way too.
var (
	defaultFalse = regexp.MustCompile(`\bDEFAULT false\b`)
	defaultTrue  = regexp.MustCompile(`\bDEFAULT true\b`)
)

// sqliteKeywordColumns are column names that SQLite reads as keywords and
// will not accept bare, as it will not accept a column called commit. They
// are quoted in the SQLite file, and a query that names one must quote it on
// both databases. The schema has none at present; the list is kept so that
// the next one is a one-line change here and not a change to the document.
var sqliteKeywordColumns = []*regexp.Regexp{
	regexp.MustCompile(`^(\s+)(commit)(\s+)`),
}

const (
	searchMarker = "-- SQLite (in place of the two statements above):"
	pgMarker     = "-- PostgreSQL:"
)

// Generate turns docs/schema.sql into the two files of migration 0001.
func Generate(schema []byte) (postgres, sqlite []byte, err error) {
	src := strings.ReplaceAll(string(schema), "\r\n", "\n")
	postgres = []byte(header + src)

	lines := strings.Split(src, "\n")
	pgAt, liteAt := -1, -1
	for i, l := range lines {
		switch strings.TrimSpace(l) {
		case pgMarker:
			pgAt = i
		case searchMarker:
			liteAt = i
		}
	}
	if pgAt < 0 || liteAt < pgAt {
		return nil, nil, fmt.Errorf("docs/schema.sql: the search block markers %q and %q were not found in that order", pgMarker, searchMarker)
	}

	var b strings.Builder
	b.WriteString(header)
	for _, l := range lines[:pgAt] {
		b.WriteString(sqliteLine(l))
		b.WriteByte('\n')
	}
	// The PostgreSQL statements between the two markers are left out. The
	// SQLite statements follow their marker as comment lines, up to the
	// first empty comment line; they are written out as statements.
	b.WriteString("-- SQLite:\n\n")
	wrote := false
	rest := liteAt + 1
	for ; rest < len(lines); rest++ {
		l := lines[rest]
		t := strings.TrimSpace(l)
		if t == "--" {
			if wrote {
				break
			}
			continue
		}
		if !strings.HasPrefix(t, "--") {
			return nil, nil, fmt.Errorf("docs/schema.sql line %d: expected the SQLite search statements as comment lines", rest+1)
		}
		b.WriteString(strings.TrimPrefix(strings.TrimPrefix(l, "--"), "   "))
		b.WriteByte('\n')
		wrote = true
	}
	if !wrote {
		return nil, nil, fmt.Errorf("docs/schema.sql: no SQLite search statements after %q", searchMarker)
	}
	// What follows is commentary; keep it so the file reads like the document.
	for _, l := range lines[rest:] {
		b.WriteString(l)
		b.WriteByte('\n')
	}
	out := strings.TrimRight(b.String(), "\n") + "\n"
	return postgres, []byte(out), nil
}

// sqliteLine applies the substitutions to the SQL part of one line and
// leaves its comment alone, so a comment that mentions "bytea" still does.
func sqliteLine(l string) string {
	code, comment := l, ""
	if i := strings.Index(l, "--"); i >= 0 {
		code, comment = l[:i], l[i:]
	}
	if strings.TrimSpace(code) == "" {
		return l
	}
	for _, s := range typeSubstitutions {
		code = s.re.ReplaceAllString(code, s.to)
	}
	code = defaultFalse.ReplaceAllString(code, "DEFAULT 0")
	code = defaultTrue.ReplaceAllString(code, "DEFAULT 1")
	for _, re := range sqliteKeywordColumns {
		code = re.ReplaceAllString(code, `$1"$2"$3`)
	}
	return code + comment
}
