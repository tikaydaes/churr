# The storage layer

`store` is the interface the rest of the server uses. `sqlstore` is the
one implementation, shared by both databases through a thin dialect
layer. `sqlite` and `postgres` open a database and hand it to `sqlstore`;
they are the only packages that import a driver. `migrations` holds the
schema. `storetest` is the one suite both databases must pass.

    store/                 the interfaces: one file per area
      sqlstore/            the implementation: one file per area, same names
      sqlite/  postgres/   the two openers
      migrations/          numbered SQL files per dialect, embedded
      storetest/           the tests: one file per area, same names

`docs/schema.sql` is the source of truth for tables and columns and
`docs/SCHEMA.md` for what must hold. No SQL is written outside these
packages.

## Areas

`store.Areas` (in `store.go`) lists every group of tables: `Accounts()`,
`Conversations()`, `Messages()` and so on. Each area is three files with
the same name:

| File | Holds |
|------|-------|
| `store/<area>.go` | The interface. Its doc comments are the specification of each method |
| `store/sqlstore/<area>.go` | `type <area> struct{ *queries }`, the accessor `func (q *queries) <Area>() store.<Area>`, and the methods |
| `store/storetest/<area>.go` | `func init() { register("<area>", test<Area>) }` and the tests |

So two people working on two areas never edit the same file.

Areas that are not built yet are already declared, in `store/declared.go`
and `store/sqlstore/declared.go`, with no methods. Three more
(`Moderation`, `Blocks`, `Bots`) are in `store/started.go` with only the
methods `perm` needs.

### Adding to an area, or building a declared one

1. Move the area's interface out of `declared.go` (or `started.go`) into
   `store/<area>.go` and write its methods, each with a comment that says
   exactly what it does, which error it returns for what, and what it
   keeps true. Put the types it needs beside it; types the API returns
   live in `model`.
2. Move the area's type and accessor out of `sqlstore/declared.go` into
   `sqlstore/<area>.go` and implement the methods.
3. Write `storetest/<area>.go`, registering one test function from
   `init`. `Run` gives it a fresh, empty database on each backend.
4. Run both backends (below).

`store.go` changes only when a whole new area appears, which should be
rare: every table of the schema already has one.

## Transactions

    err := s.Tx(ctx, func(tx store.Tx) error {
        seq, err := tx.Conversations().NextSeq(ctx, conv)
        ...
        return tx.Events().Append(ctx, event)
    })

- `Store` and `Tx` offer the same areas, so code written against
  `store.Areas` works in or out of a transaction.
- If the function returns an error or panics, nothing it did is kept.
- Anything that must change together is done inside one: a message, its
  event, its mentions and its search entry; an account, its passkey and
  its session.
- A transaction is short and never spans a network call.
- Do not call the `Store` itself from inside the function. On SQLite
  there is one writer, and that call would wait for the transaction it
  is in.
- A method that needs several statements wraps them in
  `q.atomic(ctx, func(q *queries) error { … })`. It then joins the
  caller's transaction if there is one and makes its own if not, so it
  is correct either way.
- Services do not call `store.Tx` for changes that produce conversation
  events; they call `chat.Service.Tx`, which wraps it
  (`../server/README.md`).

### Reads and writes inside `sqlstore`

`queries` has two handles. `rd` is for reads; `wr` is for writes and for
the read half of a read-then-write. Inside a transaction both are the
transaction. Outside one, on SQLite, `rd` is a pool of read connections
and `wr` is the single write connection.

**A method that only reads uses `rd`.** A read through `wr` waits for
the SQLite writer, and the live fan-out reads while a writer may be
waiting on it; that deadlocks.

## Sequence numbers

Each conversation numbers its events from 1. `conversations.next_seq` is
the number the next event takes.

    seq, err := tx.Conversations().NextSeq(ctx, conv)

is one statement, `UPDATE … SET next_seq = next_seq + 1 … RETURNING
next_seq - 1`. On PostgreSQL the update locks the row until the
transaction ends, so a second writer waits and then sees the new value.
On SQLite a transaction takes the write lock as it begins. Either way
numbers are handed out in commit order, never twice, and a transaction
that fails does not use one up. `storetest/conversations.go` runs twenty
writers at once on both databases to hold this.

Take the number in the transaction that writes the event with it, and
write the event with `Events().Append`. A message's `seq` is the `seq` of
the event that created it. Readers must accept gaps: erasing a message
erases the events that carried it.

## The `members` invariant

`members.level` is the highest of the level held directly and the level
of every group added to the conversation that contains the account, and
a row exists if and only if there is a direct level or such a group.
`sqlstore/membership.go` keeps this true. Any method that changes an
input (a direct level, who is in a group, which groups are in a channel,
a role, a claim, a removal) ends by calling `recompute` for the rows
concerned and `settle` for the conversations concerned, in its
transaction. `settle` also applies the last-manager rule, deletes a
private channel whose last member went, and keeps an ad-hoc channel's
member key in step.

These methods return a `store.MembershipResult`: who joined, left or
changed level, and which conversations were deleted. The service turns
it into events (`chat.Recorder.Membership`) and removes the blobs of
deleted conversations.

Recomputing is one small query per account. Adding `@everyone` to a
channel on a large server is therefore one query per member; a
set-based version is a later optimisation and would change nothing
outside `membership.go`.

## Writing a query that works on both databases

The schema is PostgreSQL's with these substitutions for SQLite: `uuid` →
`TEXT`, `jsonb` → `TEXT`, `boolean` → `INTEGER`, `bytea` → `BLOB`,
`bigint` → `INTEGER`. One query serves both if it keeps to this:

- Write placeholders as `?`. They are renumbered for PostgreSQL. Never
  put a `?` inside a quoted SQL string.
- Pass and scan every identifier, time, boolean, JSON value and byte
  string through the helpers in `sqlstore/helpers.go`:

  | Value | As an argument | As a scan target |
  |-------|----------------|------------------|
  | Identifier or optional text | `id(v)`: `""` becomes NULL | `str(&v)` |
  | Time | `ms(t)`: zero becomes NULL; `int64(t)` where NOT NULL | `tm(&t)` |
  | The two "0 means until turned off" columns | `msPtr(p)` | `tmPtr(&p)` |
  | Boolean | a Go `bool` | `flag(&b)` |
  | JSON | `jsonArg(v)`, `jsonList(v)`, `jsonObject(v)` | `js(&v)` |
  | Bytes that may be absent | `bin(b)`: empty becomes NULL | `blob(&b)` |
  | Optional number | `nullInt(p)`, `zeroNull(n)` | `numPtr(&p)`, `num(&n)` |

  PostgreSQL refuses `""` for a `uuid`, and SQLite stores an empty blob
  where PostgreSQL stores NULL, which collide in a UNIQUE column. The
  helpers exist so nobody has to remember either.
- Compare a boolean with `col = ?` and a Go `bool`. Not `col = 1`, not
  `col = TRUE`.
- Do not query inside a JSON column (`docs/SCHEMA.md` decision S1).
- `RETURNING`, `ON CONFLICT`, partial indexes, `COALESCE`, `CASE` and
  `LIMIT ?` work on both. No `ILIKE`, no `::` casts, no dialect
  functions.
- Match text without regard to case with
  `LOWER(col) LIKE ?` + `likeEscape` and the pattern from `like(s)`.
- Build `IN (…)` with `marks(n)` and `anys(ids)`, and return early for an
  empty list.
- Lock a row you are about to change with `q.forUpdate()` at the end of
  the SELECT (`q.forUpdateOf("a")` when the query joins). It is `FOR
  UPDATE` on PostgreSQL and nothing on SQLite.
- **Never catch a constraint error and carry on inside a transaction.**
  PostgreSQL aborts the whole transaction at the first failed
  statement. Use `ON CONFLICT … DO NOTHING` and look at the rows
  affected, or check first. An error may only be mapped (`q.mapErr`,
  `q.taken`, `q.changed`) and returned.
- Page with a cursor made by `store.EncodeCursor` from the sort key of
  the last row, fetching one row more than the limit to know whether
  there is a next page. `sqlstore/audit.go` is a short example.
- Where the two databases truly differ, branch on `q.pg()`. Today that
  is search and nothing else.

### Search

PostgreSQL has a generated `tsvector` column and needs nothing kept in
step. SQLite has the FTS5 table `messages_fts`, which the store writes
in the same transaction as `messages` through `ftsPut` and `ftsDelete`
(`sqlstore/messages.go`). Any new method that changes a message's text,
deletes a message or moves it must call them. Typed text never reaches
either database as query syntax: `search.Parse` makes terms and
`sqlstore/search.go` builds each database's query from them.

## Errors

| The store returns | For |
|-------------------|-----|
| `model.ErrNotFound` | No such row |
| `model.ErrNameTaken` | A username, handle or channel name that is taken or reserved |
| `model.ErrAlreadyExists` | A row that is already there |
| `model.ErrConflict` | A change that cannot be made as things stand |
| `model.ErrLocked` | Adding to a locked channel |
| anything else | A database failure; the API answers `internal` |

The store knows nothing about who may do what. That is `perm`.

## Migrations

Migration `0001` is `docs/schema.sql`. `migrations/postgres/0001_schema.sql`
is that file as it stands; `migrations/sqlite/0001_schema.sql` is made
from it by the substitutions at the top of `schema.sql` and by swapping
the search block. Both are checked in. After changing `docs/schema.sql`:

    go test ./internal/store/migrations -update

A test regenerates both and fails if they differ from what is checked
in, and another fails if the two dialects stop declaring the same
tables, columns and indexes. Later migrations are written by hand as a
pair of files with the same number, one per dialect.

## Running the tests

    go test ./internal/store/...                       SQLite
    tools/pgtest.sh go test ./internal/store/...       SQLite and PostgreSQL

`tools/pgtest.sh` starts a throwaway PostgreSQL in a temporary
directory, sets `CHURR_TEST_POSTGRES` and removes everything afterwards.
Each area's tests get a database of their own on that server. Without
the variable the PostgreSQL tests are skipped.

`storetest/fixtures.go` has what most tests start from: `newAccount`,
`newChannel`, `newAdhoc`, `join`, `newGroup`, `post` (a message stored
the way the chat service stores one), and `must`, `wantErr`, `eq`.
Put a helper that only one area needs in that area's file, with a name
that starts with the area.
