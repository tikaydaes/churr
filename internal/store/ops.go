package store

import (
	"context"
	"fmt"

	"github.com/tikaydaes/churr/internal/model"
)

// This file is what running a server needs of a database as a whole, rather
// than of one area of it: its size and a few totals for the admin console,
// and the table-by-table copy behind `churr migrate-db`.

// Totals are counts across the whole database, for the admin console's
// Overview and the head of an export.
type Totals struct {
	// DatabaseBytes is the size of the database on disk: the file and its
	// write-ahead log for SQLite, the database's total size for PostgreSQL.
	DatabaseBytes int64
	// Accounts, Conversations and Messages are every row of those tables,
	// removed accounts and deleted messages included.
	Accounts, Conversations, Messages int64
	// WaitingWebhooks is webhook requests no admin has decided yet.
	WaitingWebhooks int64
}

// Totaler is implemented by a backend that can count itself.
type Totaler interface {
	Totals(ctx context.Context) (Totals, error)
}

// ReadTotals returns the totals of a store, or zeroes from a backend that
// cannot count itself.
func ReadTotals(ctx context.Context, s Store) (Totals, error) {
	if t, ok := s.(Totaler); ok {
		return t.Totals(ctx)
	}
	return Totals{}, nil
}

// TableCount is how many rows of one table a copy moved.
type TableCount struct {
	Table string
	Rows  int64
}

// Copier is implemented by a backend that can fill itself from another
// store.
type Copier interface {
	// CopyFrom copies every row of every table from another store into
	// this one, which must be empty, rebuilds this one's search index and
	// compares the row counts. Nothing is kept unless all of it worked.
	CopyFrom(ctx context.Context, from Store) ([]TableCount, error)
}

// CopyCounted is Copy that also says what it copied, table by table, in the
// order copied.
func CopyCounted(ctx context.Context, from, to Store) ([]TableCount, error) {
	c, ok := to.(Copier)
	if !ok {
		return nil, fmt.Errorf("the %s backend cannot be copied into", to.Dialect())
	}
	return c.CopyFrom(ctx, from)
}

// ConversationLister is implemented by a backend that can list every
// conversation there is, whoever is in it. Nothing a request reaches uses
// it: the export does (docs/SCHEMA.md section 18), and so does the Slack
// importer when it looks for what it made before.
type ConversationLister interface {
	// AllConversationIDs returns the identifier of every conversation,
	// oldest first, in two lists: sealed chats, and everything else
	// (named channels public and private, ad-hoc channels, the library).
	AllConversationIDs(ctx context.Context) (open, sealed []model.ConversationID, err error)
}

// AllConversationIDs lists every conversation of a store.
func AllConversationIDs(ctx context.Context, s Store) (open, sealed []model.ConversationID, err error) {
	l, ok := s.(ConversationLister)
	if !ok {
		return nil, nil, fmt.Errorf("the %s backend cannot list its conversations", s.Dialect())
	}
	return l.AllConversationIDs(ctx)
}

// ImportWriter is implemented by a backend, and by its transactions, for the
// one thing the Slack importer writes that no request does: a message that
// was edited before it ever reached this server, and so has an edit time
// and no earlier version (docs/FEATURES.md section 25.2).
type ImportWriter interface {
	// SetEditedAt sets a message's edited_at and nothing else.
	SetEditedAt(ctx context.Context, id model.MessageID, at model.Time) error
}
