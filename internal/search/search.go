// Package search turns what a person typed into a storage query over the
// messages they may read (docs/FEATURES.md section 24). It never passes
// typed text to the database as query syntax, and what may be read comes
// from perm alone.
package search

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/perm"
	"github.com/tikaydaes/churr/internal/store"
)

// Query is a search as the API receives it.
type Query struct {
	// Q is the words to find, in the grammar of section 24.1.
	Q            string
	In           model.ConversationID
	From         model.AccountID
	Since, Until model.Time
	HasFile      bool
}

// Service runs searches.
type Service struct {
	store store.Store
	perm  *perm.Checker
}

// New makes the service.
func New(s store.Store, p *perm.Checker) *Service { return &Service{store: s, perm: p} }

// Parse reads the grammar: words, word*, "a phrase", -word. Anything else,
// including characters that mean something to a database's own query
// language, is text to find.
func Parse(q string) []store.SearchTerm {
	var terms []store.SearchTerm
	rest := strings.TrimSpace(q)
	for rest != "" {
		if rest[0] == '"' {
			if end := strings.IndexByte(rest[1:], '"'); end >= 0 {
				if phrase := strings.TrimSpace(rest[1 : 1+end]); phrase != "" {
					terms = append(terms, store.SearchTerm{Text: phrase, Phrase: true})
				}
				rest = strings.TrimSpace(rest[end+2:])
				continue
			}
			// An unclosed quote is just a character.
			rest = rest[1:]
		}
		word := rest
		if i := strings.IndexAny(rest, " \t\n\""); i >= 0 {
			word, rest = rest[:i], strings.TrimSpace(rest[i:])
		} else {
			rest = ""
		}
		t := store.SearchTerm{Text: word}
		if strings.HasPrefix(word, "-") && len(word) > 1 {
			t.Exclude, t.Text = true, word[1:]
		}
		// A prefix needs at least two letters before the star.
		if strings.HasSuffix(t.Text, "*") && utf8.RuneCountInString(t.Text) > 2 {
			t.Prefix, t.Text = true, strings.TrimSuffix(t.Text, "*")
		}
		if t.Text != "" {
			terms = append(terms, t)
		}
	}
	return terms
}

// Search returns matching messages the actor may read, newest first. The
// messages come as stored; the caller fills in what sits beside them. A
// filter that names a conversation the asker cannot read gives an empty
// result, not an error, so search cannot be used to test whether a private
// channel exists.
func (s *Service) Search(ctx context.Context, actor model.Account, q Query, p store.Page) ([]model.Message, string, error) {
	if !actor.Active() {
		return nil, "", model.ErrUnauthenticated
	}
	terms := Parse(q.Q)
	if len(terms) == 0 && q.In == "" && q.From == "" && q.Since.IsZero() && q.Until.IsZero() && !q.HasFile {
		return nil, "", model.Invalid("q", "Give some words to find, or a filter.")
	}
	return s.store.Search().Messages(ctx, store.SearchQuery{
		Terms: terms, Scope: s.perm.Readable(ctx, actor),
		In: q.In, From: q.From, Since: q.Since, Until: q.Until, HasFile: q.HasFile,
	}, p)
}
