package sqlstore

import (
	"context"
	"database/sql"
	"errors"
	"sync"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

// The rosters and account facts of store/roster.go: what a fan-out reads
// for every message, kept in memory so that a message costs what it
// delivers and not what its channel holds.
//
// What is kept is only ever as right as its writers make it, so the writers
// are all here, in this package, and each marks what it changed:
//
//   - a member row made, changed or deleted: recompute and deleteMemberRow
//     (membership.go), which every way of joining, leaving and changing
//     level goes through, groups and roles and removals included;
//     Members.UpdateOwn (mode, mute, view); moving a thread (thread_move.go);
//   - a conversation deleted: deleteConversation (membership.go);
//   - a conversation's lazy_delivery or announcement changed:
//     Conversations.Update (conversations.go), which marks the whole roster,
//     because the roster carries those two for the fan-out (ruling R66);
//   - an account row made or changed: Accounts.Create, CreateUnclaimed,
//     Claim, Update (profile, status, availability, do not disturb, quiet
//     hours, prefs), SetRole, TransferOwnership, Remove (accounts.go), and
//     the profile of a remote account arriving (streams.go).
//
// A method that writes one of those rows any other way must call touchMember,
// touchRoster or touchAccount. Read positions are not kept, so SetRead does
// not. Blocks, mentions, thread states and notify-anyway requests are not
// kept either: the fan-out reads those for each message.
//
// A change made inside a transaction is marked when the transaction has
// committed. A reader that was reading while a change was marked does not
// keep what it read.
//
// Only this process's writes are seen. Nothing else may write these tables
// while the server runs.

// rosterCache is what one DB keeps.
type rosterCache struct {
	mu    sync.Mutex
	convs map[model.ConversationID]*rosterEntry

	accMu    sync.RWMutex
	accounts map[model.AccountID]*model.Account
	// whole holds the whole rows of this server's own accounts, for
	// Accounts.Get (kept.go); marked with the facts.
	whole map[model.AccountID]*model.Account
	// accSeq counts account changes, and accTouched says at which count
	// each account last changed, so that a read that began before a
	// change is not kept after it.
	accSeq     uint64
	accTouched map[model.AccountID]uint64
}

// rosterEntry is one conversation's roster. The cache's lock guards it,
// except load, which is held by the one reader filling it.
type rosterEntry struct {
	load sync.Mutex
	// ver goes up with every change marked.
	ver  uint64
	snap *store.Roster
	// dirty are the members to read again; all says read everyone.
	dirty map[model.AccountID]struct{}
	all   bool
}

// touches are the marks of one transaction, applied when it commits.
type touches struct {
	accounts []model.AccountID
	members  []memberKey
	rosters  []model.ConversationID

	// The marks of kept.go: settings, word filters, sessions.
	settings, filters bool
	sessions          []model.SessionID
	allSessions       bool

	// The rest is not marks but what the transaction itself is in the
	// middle of (kept.go): writes held back to be sent together, the
	// first refusal of one of them, and what taking a conversation's seq
	// also told it.
	held   []held
	failed error
	taken  map[model.ConversationID]takenSeq
	// tm is where the transaction's time goes, when that is being
	// watched (timed.go); nil otherwise.
	tm *txTimes
}

type memberKey struct {
	conv    model.ConversationID
	account model.AccountID
}

// touchAccount marks an account row as changed.
func (q *queries) touchAccount(ids ...model.AccountID) {
	if q.touched != nil {
		q.touched.accounts = append(q.touched.accounts, ids...)
		return
	}
	q.db.roster.applyAccounts(ids)
}

// touchMember marks one member row as made, changed or deleted.
func (q *queries) touchMember(conv model.ConversationID, account model.AccountID) {
	if q.touched != nil {
		q.touched.members = append(q.touched.members, memberKey{conv, account})
		return
	}
	q.db.roster.applyMembers([]memberKey{{conv, account}})
}

// touchRoster marks every member row of a conversation.
func (q *queries) touchRoster(conv model.ConversationID) {
	if q.touched != nil {
		q.touched.rosters = append(q.touched.rosters, conv)
		return
	}
	q.db.roster.applyRosters([]model.ConversationID{conv})
}

func (c *rosterCache) apply(t *touches) {
	if t == nil {
		return
	}
	c.applyAccounts(t.accounts)
	c.applyMembers(t.members)
	c.applyRosters(t.rosters)
}

func (c *rosterCache) applyAccounts(ids []model.AccountID) {
	if len(ids) == 0 {
		return
	}
	c.accMu.Lock()
	if c.accTouched == nil {
		c.accTouched = map[model.AccountID]uint64{}
	}
	for _, id := range ids {
		c.accSeq++
		c.accTouched[id] = c.accSeq
		delete(c.accounts, id)
		delete(c.whole, id)
	}
	c.accMu.Unlock()
}

func (c *rosterCache) applyMembers(keys []memberKey) {
	if len(keys) == 0 {
		return
	}
	c.mu.Lock()
	for _, k := range keys {
		e := c.convs[k.conv]
		if e == nil {
			continue // never read, so nothing is kept
		}
		e.ver++
		if e.all {
			continue
		}
		if e.dirty == nil {
			e.dirty = map[model.AccountID]struct{}{}
		}
		e.dirty[k.account] = struct{}{}
	}
	c.mu.Unlock()
}

func (c *rosterCache) applyRosters(convs []model.ConversationID) {
	if len(convs) == 0 {
		return
	}
	c.mu.Lock()
	for _, conv := range convs {
		if e := c.convs[conv]; e != nil {
			e.ver++
			e.all, e.dirty, e.snap = true, nil, nil
		}
	}
	c.mu.Unlock()
}

// rostersKept bounds how many conversations' rosters are kept: a server has
// few large channels and any number of chats between two or three people.
const rostersKept = 50000

const rosterCols = `account_id, level, mode, mode_before_mute, muted_until, thread_view`

func scanRosterMember(s scanner) (store.RosterMember, error) {
	var m store.RosterMember
	err := s.Scan(str(&m.AccountID), str(&m.Level), str(&m.Mode), str(&m.ModeBeforeMute), tm(&m.MutedUntil), str(&m.ThreadView))
	// The few values these columns take are not kept once per member.
	m.Level, m.Mode, m.ModeBeforeMute = internLevel(m.Level), internMode(m.Mode), internMode(m.ModeBeforeMute)
	switch m.ThreadView {
	case "flat":
		m.ThreadView = "flat"
	case "threaded":
		m.ThreadView = "threaded"
	}
	return m, err
}

func internLevel(l model.Level) model.Level {
	for _, known := range []model.Level{model.LevelMember, model.LevelManager, model.LevelReader} {
		if l == known {
			return known
		}
	}
	return l
}

func internMode(m model.Mode) model.Mode {
	switch m {
	case model.ModeAny:
		return model.ModeAny
	case model.ModeMentions:
		return model.ModeMentions
	case model.ModeNothing:
		return model.ModeNothing
	}
	return m
}

func (q *queries) readRoster(ctx context.Context, conv model.ConversationID) (*store.Roster, error) {
	rows, err := q.rd.QueryContext(ctx,
		`SELECT `+rosterCols+` FROM members WHERE conversation_id = ? ORDER BY joined_at, account_id`, string(conv))
	members, err := collect(rows, err, scanRosterMember)
	if err != nil {
		return nil, err
	}
	r := store.NewRoster(conv, members)
	// The conversation's own settings that a fan-out reads. A
	// conversation that has gone has none, and no members either.
	err = q.rd.QueryRowContext(ctx, `SELECT lazy_delivery, announcement FROM conversations WHERE id = ?`, string(conv)).
		Scan(str(&r.Lazy), flag(&r.Announcement))
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if r.Lazy == "" {
		r.Lazy = model.LazyServer
	}
	return r, nil
}

// inChunk bounds an IN list: both databases limit a statement's arguments.
const inChunk = 2000

func (m members) Roster(ctx context.Context, conv model.ConversationID) (*store.Roster, error) {
	if m.inTx {
		// A transaction sees its own writes, which nobody else may.
		return m.readRoster(ctx, conv)
	}
	c := &m.db.roster
	c.mu.Lock()
	if c.convs == nil {
		c.convs = map[model.ConversationID]*rosterEntry{}
	}
	e := c.convs[conv]
	if e == nil {
		if len(c.convs) >= rostersKept {
			// Forgetting is always safe: a roster that is not kept is
			// read when next asked for. Which ones go does not matter
			// much; the busy ones are back at their next message.
			drop := rostersKept / 8
			for id := range c.convs {
				delete(c.convs, id)
				if drop--; drop <= 0 {
					break
				}
			}
		}
		e = &rosterEntry{all: true}
		c.convs[conv] = e
	}
	if snap := e.snap; snap != nil && !e.all && len(e.dirty) == 0 {
		c.mu.Unlock()
		return snap, nil
	}
	c.mu.Unlock()

	// One reader fills it; the others wait and take what it read.
	e.load.Lock()
	defer e.load.Unlock()
	c.mu.Lock()
	snap, ver, all := e.snap, e.ver, e.all || e.snap == nil
	if snap != nil && !all && len(e.dirty) == 0 {
		c.mu.Unlock()
		return snap, nil
	}
	var changed []model.AccountID
	if !all {
		changed = make([]model.AccountID, 0, len(e.dirty))
		for id := range e.dirty {
			changed = append(changed, id)
		}
	}
	c.mu.Unlock()

	var fresh *store.Roster
	if all {
		var err error
		if fresh, err = m.readRoster(ctx, conv); err != nil {
			return nil, err
		}
	} else {
		var rows []store.RosterMember
		for from := 0; from < len(changed); from += inChunk {
			chunk := changed[from:min(from+inChunk, len(changed))]
			args := append([]any{string(conv)}, anys(chunk)...)
			r, err := m.rd.QueryContext(ctx, `SELECT `+rosterCols+` FROM members WHERE conversation_id = ? AND account_id IN (`+
				marks(len(chunk))+`) ORDER BY joined_at, account_id`, args...)
			got, err := collect(r, err, scanRosterMember)
			if err != nil {
				return nil, err
			}
			rows = append(rows, got...)
		}
		fresh = snap.Patched(changed, rows)
	}
	c.mu.Lock()
	if e.ver == ver {
		// Nothing was marked while this was read: it is kept.
		e.snap, e.all, e.dirty = fresh, false, nil
	}
	c.mu.Unlock()
	return fresh, nil
}

func (m members) ConversationIDs(ctx context.Context, account model.AccountID) ([]model.ConversationID, error) {
	return collectIDs[model.ConversationID](m.rd.QueryContext(ctx,
		`SELECT conversation_id FROM members WHERE account_id = ?`, string(account)))
}

// factCols are the columns the notification rule reads of an account.
const factCols = `id, server_id, role, time_zone, claimed_at, removed_at,
	status_emoji, status_text, status_until, status_dnd, status_away,
	availability, availability_until, dnd_until,
	quiet_start, quiet_end, quiet_days, quiet_skip_until, allow_notify_anyway, prefs`

func scanFacts(s scanner) (*model.Account, error) {
	a := &model.Account{}
	err := s.Scan(str(&a.ID), str(&a.ServerID), str(&a.Role), str(&a.TimeZone), tm(&a.ClaimedAt), tm(&a.RemovedAt),
		str(&a.StatusEmoji), str(&a.StatusText), tm(&a.StatusUntil), flag(&a.StatusDND), flag(&a.StatusAway),
		str(&a.AvailabilityIs), tm(&a.AvailabilityEnd), tmPtr(&a.DNDUntil),
		str(&a.QuietStart), str(&a.QuietEnd), str(&a.QuietDays), tm(&a.QuietSkipUntil), flag(&a.AllowNotifyAnyway), js(&a.PrefsJSON))
	return a, err
}

func (q *queries) readFacts(ctx context.Context, ids []model.AccountID) ([]*model.Account, error) {
	var out []*model.Account
	for from := 0; from < len(ids); from += inChunk {
		chunk := ids[from:min(from+inChunk, len(ids))]
		rows, err := q.rd.QueryContext(ctx, `SELECT `+factCols+` FROM accounts WHERE id IN (`+marks(len(chunk))+`)`, anys(chunk)...)
		got, err := collect(rows, err, scanFacts)
		if err != nil {
			return nil, err
		}
		out = append(out, got...)
	}
	return out, nil
}

func (a accounts) NotifyFacts(ctx context.Context, ids []model.AccountID) ([]*model.Account, error) {
	out := make([]*model.Account, len(ids))
	place := func(got []*model.Account) map[model.AccountID]*model.Account {
		by := make(map[model.AccountID]*model.Account, len(got))
		for _, acc := range got {
			by[acc.ID] = acc
		}
		for i, id := range ids {
			if out[i] == nil {
				out[i] = by[id]
			}
		}
		return by
	}
	if a.inTx {
		got, err := a.readFacts(ctx, ids)
		if err != nil {
			return nil, err
		}
		place(got)
		return out, nil
	}
	c := &a.db.roster
	var missing []model.AccountID
	c.accMu.RLock()
	for i, id := range ids {
		if out[i] = c.accounts[id]; out[i] == nil {
			missing = append(missing, id)
		}
	}
	seq := c.accSeq
	c.accMu.RUnlock()
	if len(missing) == 0 {
		return out, nil
	}
	got, err := a.readFacts(ctx, missing)
	if err != nil {
		return nil, err
	}
	place(got)
	c.accMu.Lock()
	if c.accounts == nil {
		c.accounts = map[model.AccountID]*model.Account{}
	}
	for _, acc := range got {
		// An account that changed while it was being read is not kept.
		if c.accTouched[acc.ID] <= seq {
			c.accounts[acc.ID] = acc
		}
	}
	c.accMu.Unlock()
	return out, nil
}

func (b blocks) BlockersOf(ctx context.Context, blocked model.AccountID) ([]model.AccountID, error) {
	return collectIDs[model.AccountID](b.rd.QueryContext(ctx,
		`SELECT account_id FROM blocks WHERE blocked_id = ?`, string(blocked)))
}
