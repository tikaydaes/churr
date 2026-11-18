package sqlstore

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/tikaydaes/churr/internal/model"
)

// The helpers in this file convert between Go values and what both drivers
// accept and return. Use them for every argument and every scanned column
// that is an identifier, a time or JSON, so a query behaves the same on
// SQLite (TEXT, INTEGER) and PostgreSQL (uuid, bigint, jsonb).

// id passes an identifier, or any string column that may be NULL, as an
// argument: the empty string becomes NULL. PostgreSQL refuses "" for a uuid.
func id[T ~string](v T) any {
	if v == "" {
		return nil
	}
	return string(v)
}

// ms passes a time as an argument: the zero time becomes NULL.
func ms(t model.Time) any {
	if t == 0 {
		return nil
	}
	return int64(t)
}

// msPtr passes one of the two columns where NULL means "off" and 0 means
// "until turned off" (dnd_until, away_until).
func msPtr(t *model.Time) any {
	if t == nil {
		return nil
	}
	return int64(*t)
}

// bin passes a bytea or BLOB value that may be absent: an empty slice becomes
// NULL. Without this SQLite would store an empty blob, and two of those
// collide in a UNIQUE column where two NULLs do not.
func bin(b []byte) any {
	if len(b) == 0 {
		return nil
	}
	return b
}

// nullInt passes an optional whole number.
func nullInt(v *int64) any {
	if v == nil {
		return nil
	}
	return *v
}

// zeroNull passes a whole number where 0 means "none" as NULL.
func zeroNull(v int64) any {
	if v == 0 {
		return nil
	}
	return v
}

// jsonArg passes a value for a JSON column. Both databases take the text.
func jsonArg(v any) any {
	b, err := json.Marshal(v)
	if err != nil {
		// Only values of our own types are stored, and they all marshal.
		panic("sqlstore: value for a JSON column does not marshal: " + err.Error())
	}
	return string(b)
}

// jsonList is jsonArg for a column that holds a list and is NOT NULL: a nil
// slice is stored as [] and not as null.
func jsonList(v any) any {
	s := jsonArg(v).(string)
	if s == "null" {
		return "[]"
	}
	return s
}

// jsonObject is jsonArg for a column that holds an object and is NOT NULL.
func jsonObject(v any) any {
	s := jsonArg(v).(string)
	if s == "null" || s == "" {
		return "{}"
	}
	return s
}

// viewArg is a message's view for its column: NULL if none.
func viewArg(v *model.ValueEnvelope) any {
	if v == nil {
		return nil
	}
	return jsonArg(v)
}

// rawJSON passes JSON that is already encoded, or NULL if there is none.
func rawJSON(b []byte) any {
	if len(b) == 0 {
		return nil
	}
	return string(b)
}

// strScan scans a text or uuid column, NULL included, into any string type.
type strScan[T ~string] struct{ p *T }

func (s strScan[T]) Scan(src any) error {
	switch v := src.(type) {
	case nil:
		*s.p = ""
	case string:
		*s.p = T(v)
	case []byte:
		*s.p = T(v)
	default:
		return fmt.Errorf("sqlstore: cannot read %T as text", src)
	}
	return nil
}

// str returns a scan target for a text or uuid column.
func str[T ~string](p *T) sql.Scanner { return strScan[T]{p} }

// timeScan scans a bigint milliseconds column, NULL included.
type timeScan struct{ p *model.Time }

func (t timeScan) Scan(src any) error {
	switch v := src.(type) {
	case nil:
		*t.p = 0
	case int64:
		*t.p = model.Time(v)
	default:
		return fmt.Errorf("sqlstore: cannot read %T as a time", src)
	}
	return nil
}

// tm returns a scan target for a time column.
func tm(p *model.Time) sql.Scanner { return timeScan{p} }

// timePtrScan scans dnd_until and away_until: NULL is nil, 0 is a pointer
// to zero.
type timePtrScan struct{ p **model.Time }

func (t timePtrScan) Scan(src any) error {
	switch v := src.(type) {
	case nil:
		*t.p = nil
	case int64:
		x := model.Time(v)
		*t.p = &x
	default:
		return fmt.Errorf("sqlstore: cannot read %T as a time", src)
	}
	return nil
}

func tmPtr(p **model.Time) sql.Scanner { return timePtrScan{p} }

// intScan scans an integer column where NULL reads as 0.
type intScan[T ~int | ~int64] struct{ p *T }

func (i intScan[T]) Scan(src any) error {
	switch v := src.(type) {
	case nil:
		*i.p = 0
	case int64:
		*i.p = T(v)
	default:
		return fmt.Errorf("sqlstore: cannot read %T as a number", src)
	}
	return nil
}

// num returns a scan target for an integer column that may be NULL.
func num[T ~int | ~int64](p *T) sql.Scanner { return intScan[T]{p} }

// intPtrScan scans an integer column where NULL must stay distinct from 0.
type intPtrScan struct{ p **int64 }

func (i intPtrScan) Scan(src any) error {
	switch v := src.(type) {
	case nil:
		*i.p = nil
	case int64:
		x := v
		*i.p = &x
	default:
		return fmt.Errorf("sqlstore: cannot read %T as a number", src)
	}
	return nil
}

func numPtr(p **int64) sql.Scanner { return intPtrScan{p} }

// boolScan scans a boolean column. PostgreSQL returns bool and SQLite
// returns 0 or 1.
type boolScan struct{ p *bool }

func (b boolScan) Scan(src any) error {
	switch v := src.(type) {
	case nil:
		*b.p = false
	case bool:
		*b.p = v
	case int64:
		*b.p = v != 0
	default:
		return fmt.Errorf("sqlstore: cannot read %T as true or false", src)
	}
	return nil
}

// flag returns a scan target for a boolean column.
func flag(p *bool) sql.Scanner { return boolScan{p} }

// bytesScan scans a bytea or BLOB column into a fresh slice, so the value
// stays valid after the row is closed.
type bytesScan struct{ p *[]byte }

func (b bytesScan) Scan(src any) error {
	switch v := src.(type) {
	case nil:
		*b.p = nil
	case []byte:
		*b.p = append([]byte(nil), v...)
	case string:
		*b.p = []byte(v)
	default:
		return fmt.Errorf("sqlstore: cannot read %T as bytes", src)
	}
	return nil
}

// blob returns a scan target for a bytea or BLOB column.
func blob(p *[]byte) sql.Scanner { return bytesScan{p} }

// jsonScan scans a JSON column into a Go value. NULL leaves the value as it
// was.
type jsonScan struct{ v any }

func (j jsonScan) Scan(src any) error {
	var b []byte
	switch v := src.(type) {
	case nil:
		return nil
	case string:
		b = []byte(v)
	case []byte:
		b = v
	default:
		return fmt.Errorf("sqlstore: cannot read %T as JSON", src)
	}
	if raw, ok := j.v.(*json.RawMessage); ok {
		*raw = append(json.RawMessage(nil), b...)
		return nil
	}
	return json.Unmarshal(b, j.v)
}

// js returns a scan target for a JSON column; v is a pointer to the value.
func js(v any) sql.Scanner { return jsonScan{v} }

// marks returns "?, ?, ?" for an IN list of n values.
func marks(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.Repeat("?, ", n-1) + "?"
}

// anys turns a slice of identifiers into arguments.
func anys[T ~string](ids []T) []any {
	out := make([]any, len(ids))
	for i, v := range ids {
		out[i] = string(v)
	}
	return out
}

// like makes a pattern that matches text containing s, without regard to
// case, for use as: LOWER(column) LIKE ? ESCAPE '\'. SQLite's LIKE ignores
// case for ASCII only and PostgreSQL's not at all, so both sides are
// lower-cased the same way instead.
func like(s string) string {
	s = strings.ToLower(s)
	s = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
	return "%" + s + "%"
}

// likeEscape is the clause that goes after a LIKE pattern made by like.
const likeEscape = ` ESCAPE '\'`

// scanner is what *sql.Row and *sql.Rows have in common.
type scanner interface{ Scan(dest ...any) error }

// collect runs scan over every row and closes the rows.
func collect[T any](rows *sql.Rows, err error, scan func(scanner) (T, error)) ([]T, error) {
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []T
	for rows.Next() {
		v, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// collectIDs reads a one-column result of identifiers.
func collectIDs[T ~string](rows *sql.Rows, err error) ([]T, error) {
	return collect(rows, err, func(s scanner) (T, error) {
		var v T
		err := s.Scan(str(&v))
		return v, err
	})
}
