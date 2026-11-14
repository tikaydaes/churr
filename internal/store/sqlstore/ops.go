package sqlstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

// This file is what a database does as a whole: counting itself for the
// admin console, and filling itself from another database for
// `churr migrate-db` (docs/SCHEMA.md section 17). The queries here read each
// database's own catalogue, so they are written per dialect and go to the
// handles directly rather than through the shared, rebound form.

var (
	_ store.Totaler = (*DB)(nil)
	_ store.Copier  = (*DB)(nil)

	_ store.ConversationLister = (*DB)(nil)
)

// AllConversationIDs lists every conversation, oldest first, sealed chats
// apart from the rest.
func (d *DB) AllConversationIDs(ctx context.Context) (open, sealed []model.ConversationID, err error) {
	rows, err := d.rd.QueryContext(ctx, `SELECT id, kind FROM conversations ORDER BY created_at, id`)
	if err != nil {
		return nil, nil, d.mapErr(err)
	}
	defer rows.Close()
	for rows.Next() {
		var id, kind string
		if err := rows.Scan(&id, &kind); err != nil {
			return nil, nil, err
		}
		if kind == model.KindSealed {
			sealed = append(sealed, model.ConversationID(id))
		} else {
			open = append(open, model.ConversationID(id))
		}
	}
	return open, sealed, rows.Err()
}

// Totals counts the database for the admin console.
func (d *DB) Totals(ctx context.Context) (store.Totals, error) {
	var t store.Totals
	for _, c := range []struct {
		into  *int64
		query string
	}{
		{&t.Accounts, `SELECT COUNT(*) FROM accounts`},
		{&t.Conversations, `SELECT COUNT(*) FROM conversations`},
		{&t.Messages, `SELECT COUNT(*) FROM messages`},
		{&t.WaitingWebhooks, `SELECT COUNT(*) FROM webhooks WHERE state = 'waiting'`},
	} {
		if err := d.r.QueryRowContext(ctx, c.query).Scan(c.into); err != nil {
			return t, d.mapErr(err)
		}
	}
	if d.dialect.Postgres() {
		err := d.r.QueryRowContext(ctx, `SELECT pg_database_size(current_database())`).Scan(&t.DatabaseBytes)
		return t, d.mapErr(err)
	}
	if err := d.r.QueryRowContext(ctx, `SELECT page_count * page_size FROM pragma_page_count(), pragma_page_size()`).Scan(&t.DatabaseBytes); err != nil {
		return t, d.mapErr(err)
	}
	// What has been written and not yet folded into the file.
	if d.path != "" {
		if st, err := os.Stat(d.path + "-wal"); err == nil {
			t.DatabaseBytes += st.Size()
		}
	}
	return t, nil
}

// column is one column of a table, with the type its database declares.
type column struct {
	name, kind string
}

// notCopied are the tables a copy leaves alone: each database's own record
// of its migrations, and SQLite's search index, which is built again from
// `messages` in the destination.
func notCopied(table string) bool {
	return table == "schema_migrations" || strings.HasPrefix(table, "messages_fts") || strings.HasPrefix(table, "sqlite_")
}

// tables lists the tables to copy, in name order.
func (d *DB) tables(ctx context.Context) ([]string, error) {
	q := `SELECT name FROM sqlite_master WHERE type = 'table'`
	if d.dialect.Postgres() {
		q = `SELECT table_name FROM information_schema.tables WHERE table_schema = current_schema() AND table_type = 'BASE TABLE'`
	}
	rows, err := d.r.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		if !notCopied(name) {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out, rows.Err()
}

// columns lists a table's stored columns. A generated column (the
// PostgreSQL search vector) is left out: the database fills it.
func (d *DB) columns(ctx context.Context, table string) ([]column, error) {
	var rows *sql.Rows
	var err error
	if d.dialect.Postgres() {
		rows, err = d.r.QueryContext(ctx, `
			SELECT column_name, data_type FROM information_schema.columns
			WHERE table_schema = current_schema() AND table_name = $1 AND is_generated = 'NEVER'
			ORDER BY ordinal_position`, table)
	} else {
		rows, err = d.r.QueryContext(ctx, `SELECT name, type FROM pragma_table_info(?) ORDER BY cid`, table)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []column
	for rows.Next() {
		var c column
		if err := rows.Scan(&c.name, &c.kind); err != nil {
			return nil, err
		}
		c.kind = strings.ToLower(c.kind)
		out = append(out, c)
	}
	return out, rows.Err()
}

func quote(name string) string { return `"` + strings.ReplaceAll(name, `"`, `""`) + `"` }

// convert turns a value read from one database into what the other stores
// in a column of the given declared type (docs/SCHEMA.md section 15): a
// boolean is 0 or 1 on SQLite, identifiers and JSON are text there, and a
// driver may hand text back as bytes or bytes as text.
func convert(v any, kind string) any {
	switch kind {
	case "boolean":
		switch x := v.(type) {
		case int64:
			return x != 0
		}
	case "integer", "bigint":
		switch x := v.(type) {
		case bool:
			if x {
				return int64(1)
			}
			return int64(0)
		}
	case "text", "uuid", "jsonb":
		if b, ok := v.([]byte); ok {
			return string(b)
		}
	case "blob", "bytea":
		if s, ok := v.(string); ok {
			return []byte(s)
		}
	}
	return v
}

// CopyFrom fills this database, which must be empty, with every row of
// another. It runs in one transaction here, so a copy that fails leaves the
// destination as empty as it was; the source is only read.
//
// Foreign keys are checked when the copy is complete rather than row by
// row, because rows refer to each other in circles (a message to its
// parent, a conversation to a message and back). SQLite can defer every
// check for one transaction. PostgreSQL can defer only constraints declared
// deferrable, so they are declared so for the length of the transaction and
// put back before it commits; that needs no more than owning the tables.
func (d *DB) CopyFrom(ctx context.Context, src store.Store) ([]store.TableCount, error) {
	from, ok := src.(*DB)
	if !ok {
		return nil, fmt.Errorf("the %s backend cannot be copied from", src.Dialect())
	}
	if from == d {
		return nil, errors.New("a database cannot be copied into itself")
	}
	tables, err := d.tables(ctx)
	if err != nil {
		return nil, err
	}
	theirs, err := from.tables(ctx)
	if err != nil {
		return nil, err
	}
	if strings.Join(tables, ",") != strings.Join(theirs, ",") {
		return nil, fmt.Errorf("the two databases do not have the same tables; both must be at the schema version this program knows")
	}
	for _, t := range tables {
		var n int64
		if err := d.w.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+quote(t)).Scan(&n); err != nil {
			return nil, err
		}
		if n > 0 {
			return nil, fmt.Errorf("the destination is not empty: its table %s has %d rows. Copy into a new, empty database (one no server has been started on)", t, n)
		}
	}

	tx, err := d.w.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	pg := d.dialect.Postgres()
	type constraint struct{ table, name string }
	var constraints []constraint
	if pg {
		rows, err := tx.QueryContext(ctx, `
			SELECT c.relname, con.conname FROM pg_constraint con
			JOIN pg_class c ON c.oid = con.conrelid
			JOIN pg_namespace n ON n.oid = con.connamespace
			WHERE con.contype = 'f' AND n.nspname = current_schema() AND NOT con.condeferrable`)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var c constraint
			if err := rows.Scan(&c.table, &c.name); err != nil {
				rows.Close()
				return nil, err
			}
			constraints = append(constraints, c)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
		for _, c := range constraints {
			if _, err := tx.ExecContext(ctx, `ALTER TABLE `+quote(c.table)+` ALTER CONSTRAINT `+quote(c.name)+` DEFERRABLE INITIALLY DEFERRED`); err != nil {
				return nil, err
			}
		}
		if _, err := tx.ExecContext(ctx, `SET CONSTRAINTS ALL DEFERRED`); err != nil {
			return nil, err
		}
	} else if _, err := tx.ExecContext(ctx, `PRAGMA defer_foreign_keys = ON`); err != nil {
		return nil, err
	}

	var counts []store.TableCount
	for _, t := range tables {
		n, err := d.copyTable(ctx, tx, from, t)
		if err != nil {
			return nil, fmt.Errorf("copying %s: %w", t, err)
		}
		counts = append(counts, store.TableCount{Table: t, Rows: n})
	}

	if pg {
		// Every deferred check runs here, while the constraints can
		// still be put back in the same transaction.
		if _, err := tx.ExecContext(ctx, `SET CONSTRAINTS ALL IMMEDIATE`); err != nil {
			return nil, fmt.Errorf("the copied rows do not hold together: %w", err)
		}
		for _, c := range constraints {
			if _, err := tx.ExecContext(ctx, `ALTER TABLE `+quote(c.table)+` ALTER CONSTRAINT `+quote(c.name)+` NOT DEFERRABLE`); err != nil {
				return nil, err
			}
		}
	} else {
		// PostgreSQL builds its search column as rows arrive. SQLite's
		// index is a table of its own: one entry for each message that
		// has search text (docs/SCHEMA.md section 13).
		if _, err := tx.ExecContext(ctx, `DELETE FROM messages_fts`); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO messages_fts (rowid, body, message_id, conversation_id)
			SELECT m.rowid, m.search_text, m.id, m.conversation_id FROM messages m WHERE m.search_text <> ''`); err != nil {
			return nil, fmt.Errorf("building the search index: %w", err)
		}
	}

	// The last word: the same number of rows in every table.
	for _, c := range counts {
		var theirs, ours int64
		if err := from.r.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+quote(c.Table)).Scan(&theirs); err != nil {
			return nil, err
		}
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+quote(c.Table)).Scan(&ours); err != nil {
			return nil, err
		}
		if theirs != ours || ours != c.Rows {
			return nil, fmt.Errorf("the row counts differ for %s: %d in the source, %d copied. Nothing was kept; was the server still running?", c.Table, theirs, ours)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("the copied rows do not hold together: %w", err)
	}
	return counts, nil
}

// copyBatch is how many rows go into one INSERT at most.
const copyBatch = 200

// copyTable copies one table's rows and returns how many there were.
func (d *DB) copyTable(ctx context.Context, tx *sql.Tx, from *DB, table string) (int64, error) {
	cols, err := d.columns(ctx, table)
	if err != nil {
		return 0, err
	}
	if len(cols) == 0 {
		return 0, fmt.Errorf("the destination has no such table")
	}
	names := make([]string, len(cols))
	for i, c := range cols {
		names[i] = quote(c.name)
	}
	list := strings.Join(names, ", ")
	rows, err := from.r.QueryContext(ctx, `SELECT `+list+` FROM `+quote(table))
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	// Both databases limit how many values one statement may carry.
	perBatch := max(1, min(copyBatch, 30000/len(cols)))
	pg := d.dialect.Postgres()
	insert := func(n int) string {
		var b strings.Builder
		b.WriteString(`INSERT INTO ` + quote(table) + ` (` + list + `) VALUES `)
		arg := 0
		for r := 0; r < n; r++ {
			if r > 0 {
				b.WriteString(", ")
			}
			b.WriteByte('(')
			for c := range cols {
				if c > 0 {
					b.WriteString(", ")
				}
				arg++
				if pg {
					fmt.Fprintf(&b, "$%d", arg)
				} else {
					b.WriteByte('?')
				}
			}
			b.WriteByte(')')
		}
		return b.String()
	}
	full, err := tx.PrepareContext(ctx, insert(perBatch))
	if err != nil {
		return 0, err
	}
	defer full.Close()

	var total int64
	args := make([]any, 0, perBatch*len(cols))
	flush := func() error {
		n := len(args) / len(cols)
		if n == 0 {
			return nil
		}
		var err error
		if n == perBatch {
			_, err = full.ExecContext(ctx, args...)
		} else {
			_, err = tx.ExecContext(ctx, insert(n), args...)
		}
		total += int64(n)
		args = args[:0]
		return err
	}
	vals := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	for rows.Next() {
		if err := rows.Scan(ptrs...); err != nil {
			return total, err
		}
		for i, v := range vals {
			// A driver may reuse the bytes it hands out.
			if b, ok := v.([]byte); ok {
				v = append([]byte(nil), b...)
			}
			args = append(args, convert(v, cols[i].kind))
		}
		if len(args) == perBatch*len(cols) {
			if err := flush(); err != nil {
				return total, err
			}
		}
	}
	if err := rows.Err(); err != nil {
		return total, err
	}
	return total, flush()
}

var _ store.ImportWriter = (*queries)(nil)

// SetEditedAt marks an imported message as edited at a moment, without
// making a version: there is no earlier text to keep.
func (q *queries) SetEditedAt(ctx context.Context, id model.MessageID, at model.Time) error {
	return q.changed(q.wr.ExecContext(ctx, `UPDATE messages SET edited_at = ? WHERE id = ?`, int64(at), string(id)))
}
