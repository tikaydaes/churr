// Package store is the storage interface. The rest of the server depends on
// this package and never on a backend, and no SQL is written outside this
// package and the ones under it.
//
// Store is assembled from one small interface per area of the schema
// (accounts, conversations, messages, …), each in its own file here, with the
// matching implementation file in sqlstore and test file in storetest.
// README.md in this directory explains how to add to an area, how
// transactions and sequence numbers work, and how to write a query that runs
// on both databases.
//
// Errors: a missing row is model.ErrNotFound, a name that is taken is
// model.ErrNameTaken, a row that already exists is model.ErrAlreadyExists,
// and a change that cannot be made as things stand is model.ErrConflict.
// Anything else is a database failure and reaches the client as `internal`.
package store

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

// Areas is every group of tables. Store and Tx both offer all of them, so
// code that works on a Store works unchanged inside a transaction.
//
// Each line is one file in this directory. To add methods to an area, edit
// that area's three files (see README.md); this list only changes when a
// whole new area appears.
type Areas interface {
	// Implemented.
	Settings() Settings
	Audit() Audit
	Accounts() Accounts
	Passkeys() Passkeys
	Sessions() Sessions
	Recovery() Recovery
	Invites() Invites
	Groups() Groups
	Sections() Sections
	Conversations() Conversations
	Members() Members
	Messages() Messages
	Events() Events
	Reactions() Reactions
	Checklists() Checklists
	Threads() Threads
	Pins() Pins
	Files() Files
	Search() Search

	// Started: only what perm and the tests need so far.
	Moderation() Moderation
	Blocks() Blocks
	Bots() Bots

	// Declared for the feature builders; no methods yet.
	Forms() Forms
	Scheduled() Scheduled
	Bookmarks() Bookmarks
	Emoji() Emoji
	StatusPresets() StatusPresets
	NotifyAnyway() NotifyAnyway
	Devices() Devices
	Identities() Identities
	Integrations() Integrations
	Previews() Previews
	Calls() Calls
	Sealed() Sealed
	Peers() Peers
	Streams() Streams

	// Reference patterns (ruling R73).
	ReferencePatterns() ReferencePatterns

	// Colour themes (ruling R61).
	Themes() Themes

	// What the janitor needs across tables (janitor.go).
	Janitor() Janitor
}

// Store is a database. Calls made on it directly each run by themselves;
// calls made on the Tx inside Store.Tx happen together or not at all.
type Store interface {
	Areas

	// Tx runs fn in one transaction. If fn returns an error or panics,
	// nothing it did is kept. Anything that must change together (a
	// message, its event, its mentions, its search entry) is done inside
	// one. A transaction is short and never spans a network call. Do not
	// call methods of the Store itself from inside fn: on SQLite there is
	// one writer, and the call would wait for the transaction it is in.
	Tx(ctx context.Context, fn func(tx Tx) error) error

	// Dialect is "sqlite" or "postgres".
	Dialect() string
	// Ping answers once the database has run a trivial query.
	Ping(ctx context.Context) error
	// Close releases the database.
	Close() error
}

// Tx is the set of areas inside one transaction.
type Tx interface {
	Areas
}

// Opener opens one kind of database and runs its migrations.
type Opener func(ctx context.Context, dataDir, dbURL string) (Store, error)

var (
	openersMu sync.Mutex
	openers   = map[string]Opener{}
)

// Register makes a backend available to Open. The backends call it from
// init, like database/sql drivers, because this package cannot import them
// (they import it). The server imports both backends for their side effect.
func Register(dialect string, o Opener) {
	openersMu.Lock()
	defer openersMu.Unlock()
	openers[dialect] = o
}

// Open picks the backend and runs migrations. An empty dbURL means SQLite at
// churr.db in the data directory; a postgres:// address means PostgreSQL
// (docs/OPERATIONS.md).
func Open(ctx context.Context, dataDir, dbURL string) (Store, error) {
	dialect := "sqlite"
	switch {
	case dbURL == "":
	case strings.HasPrefix(dbURL, "postgres://"), strings.HasPrefix(dbURL, "postgresql://"):
		dialect = "postgres"
	default:
		return nil, fmt.Errorf("the database address must be empty (SQLite in the data directory) or start with postgres://")
	}
	openersMu.Lock()
	o := openers[dialect]
	openersMu.Unlock()
	if o == nil {
		return nil, fmt.Errorf("the %s backend is not built into this program", dialect)
	}
	return o(ctx, dataDir, dbURL)
}

// Copy copies every row of every table from one store to another, for
// `churr migrate-db` (docs/SCHEMA.md section 17).
//
// The destination must be empty apart from its schema. Rows are copied as
// they are, sealed columns included; the search index is not copied but
// built again in the destination; and the copy ends by comparing row counts
// table by table. Either everything arrives or nothing does. CopyCounted
// (ops.go) is the same and reports the counts.
func Copy(ctx context.Context, from, to Store) error {
	_, err := CopyCounted(ctx, from, to)
	return err
}

// Snapshotter is implemented by a backend that can write a consistent copy
// of itself to a file while running. SQLite can; PostgreSQL is backed up
// with its own tools.
type Snapshotter interface {
	Snapshot(ctx context.Context, path string) error
}

// Snapshot writes a consistent copy of the database to path, for
// `churr backup`.
func Snapshot(ctx context.Context, s Store, path string) error {
	sn, ok := s.(Snapshotter)
	if !ok {
		return fmt.Errorf("the %s backend is backed up with the database's own tools, not by Churr", s.Dialect())
	}
	return sn.Snapshot(ctx, path)
}
