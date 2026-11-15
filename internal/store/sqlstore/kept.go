package sqlstore

import (
	"bytes"
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"sync"

	"github.com/tikaydaes/churr/internal/model"
)

// What every request reads and almost no request changes is kept in memory,
// beside the rosters and account facts of roster.go and by the same
// discipline: each is read once, and every method of this package that
// writes a row it came from marks it, at once outside a transaction and
// when the transaction has committed inside one. A reader that was reading
// while a change was marked does not keep what it read. Only this process's
// writes are seen: nothing else may write these tables while the server
// runs.
//
//   - settings: the whole table (settings.go). Writers: Settings.Set and
//     Settings.Delete.
//   - word_filters: the whole list (moderation.go). Writers:
//     CreateWordFilter, UpdateWordFilter, DeleteWordFilter.
//   - sessions, by the hash of the token (sessions.go). Writers: Touch,
//     SetFresh, Rotate, Revoke, RevokeForAccount, Complete, RevokeKinds,
//     DeleteEnded, the removal of an account (accounts.go) and the
//     janitor's Tidy (janitor.go). A session that was not found is not
//     kept, so Create has nothing to mark.
//   - who has blocked each account, by the account blocked (BlockersOf
//     in roster.go), which the fan-out asks of every message's author.
//     Writers: Blocks.Add, Blocks.Remove and the removal of an account.
//     Any of them marks them all: blocks change rarely.
//   - whole account rows of this server's own accounts, by id
//     (accounts.go): marked by touchAccount with the account facts, whose
//     writers roster.go lists. An account of another server is not kept:
//     its row is shown with its server's name, which the servers table
//     holds.
//
// A method that writes one of those tables any other way must call
// touchSettings, touchFilters, touchSession (or touchSessions), touchBlocks
// or touchAccount.
//
// Inside a transaction, settings and word filters are still answered from
// memory unless the transaction itself has written them (it then reads the
// table, to see its own write); sessions and accounts are always read from
// the table there.

// keptValue is one value read whole and kept until marked.
type keptValue[T any] struct {
	mu  sync.Mutex
	ver uint64
	ok  bool
	val T
}

// get returns the kept value, if there is one, and the version to hand to
// put with what is read instead.
func (k *keptValue[T]) get() (val T, ver uint64, ok bool) {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.val, k.ver, k.ok
}

// put keeps a value read since get returned ver, unless a change was marked
// meanwhile.
func (k *keptValue[T]) put(val T, ver uint64) {
	k.mu.Lock()
	if k.ver == ver {
		k.val, k.ok = val, true
	}
	k.mu.Unlock()
}

func (k *keptValue[T]) drop() {
	var zero T
	k.mu.Lock()
	k.ver++
	k.val, k.ok = zero, false
	k.mu.Unlock()
}

// sessionsKept bounds how many sessions are kept.
const sessionsKept = 200000

// sessionCache is the sessions kept, by token hash.
type sessionCache struct {
	mu     sync.Mutex
	ver    uint64
	byHash map[string]model.Session
	hashOf map[model.SessionID]string
}

func (c *sessionCache) get(hash []byte) (model.Session, uint64, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	s, ok := c.byHash[string(hash)]
	return s, c.ver, ok
}

func (c *sessionCache) put(hash []byte, s model.Session, ver uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ver != ver {
		// Some session changed while this one was read. Which one is not
		// tracked, so it is not kept; the next request reads it again.
		return
	}
	if c.byHash == nil {
		c.byHash, c.hashOf = map[string]model.Session{}, map[model.SessionID]string{}
	}
	if len(c.byHash) >= sessionsKept {
		// Forgetting is always safe.
		c.byHash, c.hashOf = map[string]model.Session{}, map[model.SessionID]string{}
	}
	if old, had := c.hashOf[s.ID]; had {
		delete(c.byHash, old)
	}
	c.byHash[string(hash)], c.hashOf[s.ID] = s, string(hash)
}

func (c *sessionCache) drop(ids []model.SessionID, all bool) {
	if len(ids) == 0 && !all {
		return
	}
	c.mu.Lock()
	c.ver++
	if all {
		c.byHash, c.hashOf = nil, nil
	}
	for _, id := range ids {
		if h, ok := c.hashOf[id]; ok {
			delete(c.byHash, h)
			delete(c.hashOf, id)
		}
	}
	c.mu.Unlock()
}

// keptRows is what one DB keeps besides its rosters.
type keptRows struct {
	settings keptValue[map[string]json.RawMessage]
	filters  keptValue[[]model.WordFilter]
	sessions sessionCache
	blockers keptValue[map[model.AccountID][]model.AccountID]
}

// blockersKept bounds how many accounts' blockers are kept.
const blockersKept = 100000

// touchBlocks marks the blocks table as changed.
func (q *queries) touchBlocks() {
	if q.touched != nil {
		q.touched.blocks = true
		return
	}
	q.db.kept.blockers.drop()
}

// blockersOf is who has blocked an account, from memory when it may be.
// The list is shared: read it only.
func (q *queries) blockersOf(ctx context.Context, blocked model.AccountID) ([]model.AccountID, error) {
	read := func() ([]model.AccountID, error) {
		return collectIDs[model.AccountID](q.rd.QueryContext(ctx,
			`SELECT account_id FROM blocks WHERE blocked_id = ?`, string(blocked)))
	}
	if q.inTx {
		// A transaction sees its own writes, which nobody else may.
		return read()
	}
	c := &q.db.kept.blockers
	all, ver, ok := c.get()
	if ok {
		c.mu.Lock()
		list, have := all[blocked]
		c.mu.Unlock()
		if have {
			return list, nil
		}
	}
	list, err := read()
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	if c.ver == ver {
		// Nothing was marked while this was read: it is kept.
		if !c.ok || len(c.val) >= blockersKept {
			c.val, c.ok = map[model.AccountID][]model.AccountID{}, true
		}
		c.val[blocked] = list
	}
	c.mu.Unlock()
	return list, nil
}

// touchSettings marks the settings table as changed.
func (q *queries) touchSettings() {
	if q.touched != nil {
		q.touched.settings = true
		return
	}
	q.db.kept.settings.drop()
}

// touchFilters marks the word filter list as changed.
func (q *queries) touchFilters() {
	if q.touched != nil {
		q.touched.filters = true
		return
	}
	q.db.kept.filters.drop()
}

// touchSession marks session rows as changed or deleted.
func (q *queries) touchSession(ids ...model.SessionID) {
	if q.touched != nil {
		q.touched.sessions = append(q.touched.sessions, ids...)
		return
	}
	q.db.kept.sessions.drop(ids, false)
}

// touchSessions marks every session: for a write that does not say which
// rows it changed.
func (q *queries) touchSessions() {
	if q.touched != nil {
		q.touched.allSessions = true
		return
	}
	q.db.kept.sessions.drop(nil, true)
}

func (k *keptRows) apply(t *touches) {
	if t == nil {
		return
	}
	if t.settings {
		k.settings.drop()
	}
	if t.filters {
		k.filters.drop()
	}
	k.sessions.drop(t.sessions, t.allSessions)
	if t.blocks {
		k.blockers.drop()
	}
}

// allSettings is the settings table, from memory when it may be.
func (q *queries) allSettings(ctx context.Context) (map[string]json.RawMessage, error) {
	if q.touched != nil && q.touched.settings {
		// This transaction wrote a setting: it reads what it wrote.
		return q.readSettings(ctx)
	}
	c := &q.db.kept.settings
	val, ver, ok := c.get()
	if ok {
		return val, nil
	}
	val, err := q.readSettings(ctx)
	if err != nil {
		return nil, err
	}
	if !q.inTx {
		// What a transaction reads is not kept: on PostgreSQL it may be
		// older than what was committed and marked while it ran.
		c.put(val, ver)
	}
	return val, nil
}

func (q *queries) readSettings(ctx context.Context) (map[string]json.RawMessage, error) {
	rows, err := q.rd.QueryContext(ctx, `SELECT key, value FROM settings`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]json.RawMessage{}
	for rows.Next() {
		var k string
		var v json.RawMessage
		if err := rows.Scan(&k, js(&v)); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}

// allFilters is the word filter list, from memory when it may be. The list
// is shared: callers get a copy.
func (q *queries) allFilters(ctx context.Context) ([]model.WordFilter, error) {
	read := func() ([]model.WordFilter, error) {
		rows, err := q.rd.QueryContext(ctx, `SELECT `+filterCols+` FROM word_filters ORDER BY created_at, id`)
		return collect(rows, err, scanFilter)
	}
	if q.touched != nil && q.touched.filters {
		return read()
	}
	c := &q.db.kept.filters
	val, ver, ok := c.get()
	if !ok {
		var err error
		if val, err = read(); err != nil {
			return nil, err
		}
		if !q.inTx {
			c.put(val, ver)
		}
	}
	if len(val) == 0 {
		return nil, nil
	}
	return append([]model.WordFilter(nil), val...), nil
}

// keptAccount returns one of this server's own accounts from memory.
func (c *rosterCache) keptAccount(id model.AccountID) (model.Account, uint64, bool) {
	c.accMu.RLock()
	defer c.accMu.RUnlock()
	a, ok := c.whole[id]
	if !ok {
		return model.Account{}, c.accSeq, false
	}
	return ownAccount(*a), c.accSeq, true
}

// keepAccount keeps an account read since keptAccount returned seq, unless
// it changed meanwhile.
func (c *rosterCache) keepAccount(a model.Account, seq uint64) {
	if a.ServerID != "" {
		return
	}
	c.accMu.Lock()
	if c.accTouched[a.ID] <= seq {
		if c.whole == nil {
			c.whole = map[model.AccountID]*model.Account{}
		}
		kept := ownAccount(a)
		c.whole[a.ID] = &kept
	}
	c.accMu.Unlock()
}

// ownAccount copies an account so that it shares no bytes with another.
func ownAccount(a model.Account) model.Account {
	a.WebAuthnHandle = bytes.Clone(a.WebAuthnHandle)
	a.PrefsJSON = bytes.Clone(a.PrefsJSON)
	if a.AwayUntil != nil {
		t := *a.AwayUntil
		a.AwayUntil = &t
	}
	if a.DNDUntil != nil {
		t := *a.DNDUntil
		a.DNDUntil = &t
	}
	return a
}

// Writes held back so that they travel together.
//
// On PostgreSQL every statement is a trip to another machine, and the ones
// sent after a conversation's seq is taken are sent while every other
// writer of that conversation waits. A new message is three rows (the
// message, its mentions, its event) that nothing reads in between, so
// Messages.Insert does not send its rows at once there: it holds them, and
// Events.Append holds its own row with them, and they go as one statement,
// the earlier ones as its WITH clauses: with COMMIT in a piped transaction
// (piped.go), which is every transaction on PostgreSQL.
//
// Nothing else changes: any other statement the transaction sends first
// sends what is held ahead of itself, and so does the commit. So a held
// write is always in the database before anything that could read it, and
// a caller sees no difference except in where a refusal surfaces: a held
// write the database refuses (a client_id used twice at once) fails the
// transaction as a whole, with the same error, and not the call that made
// it. SQLite has no such statement and no trip to make; nothing is held
// there.

// held is one statement waiting to be sent, written with "?" placeholders.
type held struct {
	sql  string
	args []any
}

// hold keeps a write for later if this is a transaction on PostgreSQL, and
// reports whether it did.
func (q *queries) hold(sql string, args ...any) bool {
	if !q.pg() || q.touched == nil {
		return false
	}
	q.touched.held = append(q.touched.held, held{sql, args})
	return true
}

// together builds one statement of the held ones and last: each held one a
// WITH clause, which PostgreSQL runs whether or not anything reads it. With
// no last, the newest held statement stands in its place.
func together(list []held, last *held) (string, []any) {
	if last == nil {
		last, list = &list[len(list)-1], list[:len(list)-1]
	}
	if len(list) == 0 {
		return last.sql, last.args
	}
	var b strings.Builder
	var args []any
	b.WriteString("WITH ")
	for i, h := range list {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString("held")
		b.WriteString(strconv.Itoa(i + 1))
		b.WriteString(" AS (")
		b.WriteString(h.sql)
		b.WriteString(")")
		args = append(args, h.args...)
	}
	b.WriteString(" ")
	b.WriteString(last.sql)
	return b.String(), append(args, last.args...)
}

// flush sends what is held, ahead of a statement that might read it; and
// first of all BEGIN, in a piped transaction that has not begun (piped.go).
// A refusal is remembered: the transaction is over on PostgreSQL from then
// on, and DB.tx returns this error for it whatever fails afterwards.
func (b bound) flush(ctx context.Context) {
	if b.st == nil {
		return
	}
	b.begin(ctx)
	if len(b.st.held) == 0 {
		return
	}
	list := b.st.held
	b.st.held = nil
	if b.st.failed != nil {
		return
	}
	q, args := together(list, nil)
	b.count(q)
	b.rt.Add(1)
	done := b.timed()
	if _, err := b.ex.ExecContext(ctx, rebind(b.pg, q), args...); err != nil {
		b.st.failed = err
	}
	if done != nil {
		done()
	}
}

// takenSeq is what the statement that took a conversation's seq in this
// transaction also learned of the conversation, so that storing the message
// does not ask again.
type takenSeq struct {
	kind  string
	floor int64
	// lastAt is what last_message_at was set to by that statement, or 0
	// if it was left alone.
	lastAt model.Time
}

func (q *queries) noteSeq(conv model.ConversationID, t takenSeq) {
	if q.touched == nil {
		return
	}
	if q.touched.taken == nil {
		q.touched.taken = map[model.ConversationID]takenSeq{}
	}
	q.touched.taken[conv] = t
}

func (q *queries) seqTaken(conv model.ConversationID) (takenSeq, bool) {
	if q.touched == nil {
		return takenSeq{}, false
	}
	t, ok := q.touched.taken[conv]
	return t, ok
}
