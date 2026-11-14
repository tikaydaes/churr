package server_test

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/tikaydaes/churr/internal/server"
	"github.com/tikaydaes/churr/internal/store"
	"github.com/tikaydaes/churr/internal/store/storetest"
	"github.com/tikaydaes/churr/internal/testkit"
)

// populate gives a server a little of most things: people, a public and a
// private channel, a thread, a reaction, an edit, a file, a poll with an
// answer, and a word only one message has.
func populate(t *testing.T, ts *testkit.TestServer) {
	t.Helper()
	ann, bob := ts.Member("ann"), ts.Member("bob")
	town := ann.Channel("town", false, bob)
	den := ann.Channel("den", true, bob)
	first := ann.Post(town, "hello town, the walrus is here")["id"].(string)
	bob.Must(201, "POST", "/api/v1/conversations/"+town+"/messages", map[string]any{"text": "a reply", "parent_id": first})
	bob.Must(200, "PUT", "/api/v1/messages/"+first+"/reactions/"+url.PathEscape("🎉"), nil)
	second := ann.Post(den, "private words")["id"].(string)
	ann.Must(200, "PATCH", "/api/v1/messages/"+second, map[string]any{"text": "private words, edited"})
	f := ann.Upload(town, "notes.txt", []byte("some notes\n"))
	ann.Must(201, "POST", "/api/v1/conversations/"+town+"/messages", map[string]any{"text": "with a file", "file_ids": []any{f["id"]}})
	poll := ann.Must(201, "POST", "/api/v1/conversations/"+town+"/messages", map[string]any{"form": map[string]any{
		"preset": "poll", "title": "Lunch?", "action": map[string]any{"type": "tally"},
		"fields": []any{map[string]any{"id": "choice", "type": "choice", "label": "Where", "required": true,
			"options": []any{map[string]any{"id": "o1", "label": "Noodles"}, map[string]any{"id": "o2", "label": "Tacos"}}}}}})
	id := poll["message"].(map[string]any)["id"].(string)
	bob.Must(200, "PUT", "/api/v1/messages/"+id+"/form/answer", map[string]any{"answers": map[string]any{"choice": []any{"o1"}}})
}

// dump reads every row of every table of a SQLite file, as text, sorted, so
// two databases can be compared whatever order their rows are in.
func dump(t *testing.T, path string) map[string][]string {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows, err := db.Query(`SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'messages_fts%' AND name NOT LIKE 'sqlite_%' AND name <> 'schema_migrations'`)
	if err != nil {
		t.Fatal(err)
	}
	var tables []string
	for rows.Next() {
		var name string
		rows.Scan(&name)
		tables = append(tables, name)
	}
	rows.Close()
	out := map[string][]string{}
	for _, table := range tables {
		rows, err := db.Query(`SELECT * FROM "` + table + `"`)
		if err != nil {
			t.Fatal(err)
		}
		cols, _ := rows.Columns()
		for rows.Next() {
			vals := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			if err := rows.Scan(ptrs...); err != nil {
				t.Fatal(err)
			}
			var b strings.Builder
			for i, v := range vals {
				s := fmt.Sprintf("%T:%v", v, v)
				// PostgreSQL keeps JSON in its own spelling: spaces
				// after colons and commas, keys in its own order. A
				// value that went through it is compared without
				// regard to either.
				if text, ok := v.(string); ok && (strings.HasPrefix(text, "{") || strings.HasPrefix(text, "[")) {
					letters := strings.Split(strings.ReplaceAll(text, " ", ""), "")
					sort.Strings(letters)
					s = "json:" + strings.Join(letters, "")
				}
				fmt.Fprintf(&b, "%s=%s|", cols[i], s)
			}
			out[table] = append(out[table], b.String())
		}
		rows.Close()
		sort.Strings(out[table])
	}
	return out
}

func sameDump(t *testing.T, what string, a, b map[string][]string) {
	t.Helper()
	if len(a) != len(b) {
		t.Fatalf("%s: %d tables against %d", what, len(a), len(b))
	}
	rows := 0
	for table, want := range a {
		got := b[table]
		if len(got) != len(want) {
			t.Fatalf("%s: table %s has %d rows against %d", what, table, len(got), len(want))
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("%s: table %s differs:\n  %s\n  %s", what, table, want[i], got[i])
			}
			rows++
		}
	}
	if rows < 30 {
		t.Fatalf("%s: only %d rows were compared; the source was not populated", what, rows)
	}
}

func one(t *testing.T, driver, dsn, query string) int64 {
	t.Helper()
	db, err := sql.Open(driver, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var n int64
	if err := db.QueryRow(query).Scan(&n); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return n
}

// checkSQLite holds what must be true of a SQLite database that was copied
// into: its search index has exactly one entry per message with search
// text, and finds the word.
func checkSQLite(t *testing.T, path string) {
	t.Helper()
	dsn := "file:" + filepath.ToSlash(path) + "?mode=ro"
	indexed := one(t, "sqlite", dsn, `SELECT COUNT(*) FROM messages_fts`)
	want := one(t, "sqlite", dsn, `SELECT COUNT(*) FROM messages WHERE search_text <> ''`)
	if indexed != want || indexed == 0 {
		t.Fatalf("the search index has %d entries for %d messages with text", indexed, want)
	}
	if n := one(t, "sqlite", dsn, `SELECT COUNT(*) FROM messages_fts WHERE messages_fts MATCH 'walrus'`); n != 1 {
		t.Fatalf("searching the copy for a word one message has found %d", n)
	}
}

// TestMigrateDB copies a populated community between databases and holds
// that nothing is lost or changed: SQLite to SQLite always, and under
// tools/pgtest.sh PostgreSQL to SQLite to PostgreSQL to SQLite, comparing
// every row of the two SQLite copies.
func TestMigrateDB(t *testing.T) {
	ctx := context.Background()
	ts := testkit.NewServer(t)
	populate(t, ts)
	source := filepath.Join(ts.Server.Config.Data, "churr.db")
	if storetest.UsePostgres() {
		source = ts.Server.Config.DB
	}
	dir := t.TempDir()
	first := filepath.Join(dir, "first.db")
	counts, err := server.MigrateDB(ctx, source, first)
	if err != nil {
		t.Fatal(err)
	}
	total := func(counts []store.TableCount) (n int64) {
		for _, c := range counts {
			n += c.Rows
		}
		return n
	}
	if total(counts) < 30 {
		t.Fatalf("only %d rows were copied: %v", total(counts), counts)
	}
	checkSQLite(t, first)

	if !storetest.UsePostgres() {
		sameDump(t, "SQLite to SQLite", dump(t, source), dump(t, first))
	} else {
		pg := storetest.PostgresURL(t)
		there, err := server.MigrateDB(ctx, first, pg)
		if err != nil {
			t.Fatal(err)
		}
		if total(there) != total(counts) {
			t.Fatalf("%d rows went to PostgreSQL of %d", total(there), total(counts))
		}
		if n := one(t, "pgx", pg, `SELECT COUNT(*) FROM messages WHERE tsv @@ to_tsquery('simple', 'walrus')`); n != 1 {
			t.Fatalf("PostgreSQL's search column found %d messages for a word one has", n)
		}
		// The foreign keys are as strict afterwards as before.
		if n := one(t, "pgx", pg, `SELECT COUNT(*) FROM pg_constraint WHERE contype = 'f' AND condeferrable`); n != 0 {
			t.Fatalf("%d foreign keys were left deferrable", n)
		}
		second := filepath.Join(dir, "second.db")
		back, err := server.MigrateDB(ctx, pg, second)
		if err != nil {
			t.Fatal(err)
		}
		if total(back) != total(counts) {
			t.Fatalf("%d rows came back from PostgreSQL of %d", total(back), total(counts))
		}
		checkSQLite(t, second)
		sameDump(t, "there and back", dump(t, first), dump(t, second))
	}

	// Refusals. A destination that already holds something is left alone.
	if _, err := server.MigrateDB(ctx, source, first); err == nil || !strings.Contains(err.Error(), "not empty") {
		t.Fatalf("copying into a database that is not empty: %v", err)
	}
	if _, err := server.MigrateDB(ctx, filepath.Join(dir, "nothing.db"), filepath.Join(dir, "x.db")); err == nil {
		t.Fatal("a source that does not exist was accepted")
	}
	if _, err := server.MigrateDB(ctx, first, first); err == nil {
		t.Fatal("copying a database into itself was accepted")
	}
}
