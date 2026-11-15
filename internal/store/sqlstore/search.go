package sqlstore

import (
	"context"
	"strings"
	"unicode"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

type search struct{ *queries }

// Search returns the search area.
func (q *queries) Search() store.Search { return search{q} }

// words splits a term into the runs of letters and digits both tokenizers
// would index. Everything else is punctuation to them, and leaving it out
// here is what keeps typed text from ever being read as query syntax.
func words(text string) []string {
	return strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}

// ftsQuote writes words as one FTS5 string: a phrase when there are several.
func ftsQuote(ws []string) string { return `"` + strings.Join(ws, " ") + `"` }

// termSQL returns the condition one term puts on a message (alias m) and
// its argument, or ok false if the term holds nothing a tokenizer would
// index.
func (q *queries) termSQL(t store.SearchTerm) (cond string, arg any, ok bool) {
	ws := words(t.Text)
	if len(ws) == 0 {
		return "", nil, false
	}
	if q.pg() {
		switch {
		case t.Prefix:
			// A lexeme of letters and digits needs no escaping.
			cond, arg = `m.tsv @@ to_tsquery('simple', ?)`, strings.Join(ws, " & ")+":*"
		case t.Phrase || len(ws) > 1:
			cond, arg = `m.tsv @@ phraseto_tsquery('simple', ?)`, strings.Join(ws, " ")
		default:
			cond, arg = `m.tsv @@ plainto_tsquery('simple', ?)`, ws[0]
		}
	} else {
		match := ftsQuote(ws)
		if t.Prefix {
			match += "*"
		}
		cond, arg = `m.id IN (SELECT message_id FROM messages_fts WHERE messages_fts MATCH ?)`, match
	}
	if t.Exclude {
		cond = "NOT (" + cond + ")"
	}
	return cond, arg, true
}

type searchCursor struct {
	At int64  `json:"a"`
	ID string `json:"i"`
}

func (s search) Messages(ctx context.Context, sq store.SearchQuery, p store.Page) ([]model.Message, string, error) {
	// Only channel messages that are not deleted, in conversations the
	// scope may read. The library holds no messages, but is left out by
	// name so that can never matter.
	where := []string{
		"c.kind = 'channel'", "(c.member_key IS NULL OR c.member_key <> 'library')", "m.deleted_at IS NULL", "m.seq IS NOT NULL",
	}
	var args []any
	const member = `EXISTS (SELECT 1 FROM members mb WHERE mb.conversation_id = c.id AND mb.account_id = ?)`
	if sq.Scope.PublicChannels {
		nb, nbArgs := notBanned("c.id", sq.Scope.AccountID, model.Now())
		where = append(where, "((c.private = ? AND "+nb+") OR "+member+")")
		args = append(append(args, false), nbArgs...)
		args = append(args, id(sq.Scope.AccountID))
	} else {
		where = append(where, member)
		args = append(args, id(sq.Scope.AccountID))
	}
	positive := 0
	for _, t := range sq.Terms {
		cond, arg, ok := s.termSQL(t)
		if !ok {
			continue
		}
		where = append(where, cond)
		args = append(args, arg)
		if !t.Exclude {
			positive++
		}
	}
	filters := 0
	if sq.In != "" {
		where, args, filters = append(where, "m.conversation_id = ?"), append(args, string(sq.In)), filters+1
	}
	if sq.From != "" {
		where, args, filters = append(where, "m.author_id = ?"), append(args, string(sq.From)), filters+1
	}
	if !sq.Since.IsZero() {
		where, args, filters = append(where, "m.created_at >= ?"), append(args, int64(sq.Since)), filters+1
	}
	if !sq.Until.IsZero() {
		where, args, filters = append(where, "m.created_at < ?"), append(args, int64(sq.Until)), filters+1
	}
	if sq.HasFile {
		where = append(where, `EXISTS (SELECT 1 FROM files f WHERE f.message_id = m.id AND f.detached_at IS NULL)`)
		filters++
	}
	if positive == 0 && filters == 0 {
		return nil, "", nil
	}
	if positive == 0 {
		// With nothing to find, only messages that are in the index
		// count, so both databases answer alike.
		where = append(where, "m.search_text <> ''")
	}
	if p.Cursor != "" {
		var c searchCursor
		if err := store.DecodeCursor(p.Cursor, &c); err != nil {
			return nil, "", err
		}
		where = append(where, "(m.created_at < ? OR (m.created_at = ? AND m.id < ?))")
		args = append(args, c.At, c.At, c.ID)
	}
	limit := p.Clamp()
	args = append(args, limit+1)
	rows, err := s.rd.QueryContext(ctx, `SELECT `+messageCols+`
		FROM messages m JOIN conversations c ON c.id = m.conversation_id
		WHERE `+strings.Join(where, " AND ")+` ORDER BY m.created_at DESC, m.id DESC LIMIT ?`, args...)
	out, err := collect(rows, err, scanMessage)
	if err != nil {
		return nil, "", err
	}
	next := ""
	if len(out) > limit {
		out = out[:limit]
		last := out[limit-1]
		next = store.EncodeCursor(searchCursor{At: int64(last.CreatedAt), ID: string(last.ID)})
	}
	return out, next, nil
}

func (s search) Indexed(ctx context.Context) (int64, error) {
	var n int64
	q := `SELECT count(*) FROM messages_fts`
	if s.pg() {
		q = `SELECT count(*) FROM messages m JOIN conversations c ON c.id = m.conversation_id
			WHERE c.kind = 'channel' AND m.deleted_at IS NULL AND m.search_text <> ''`
	}
	ex := s.rd
	if s.inTx {
		ex = s.wr
	}
	err := ex.QueryRowContext(ctx, q).Scan(&n)
	return n, err
}
