package notify

import (
	"context"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

// A new message in a channel of five thousand people is judged five
// thousand times: once for each connected reader's copy of the event, and
// again for each phone that might be woken. Asked one account at a time,
// the rule reads a dozen rows for each of them, and a single message costs
// tens of thousands of queries. Judge reads the same rows for everyone at
// once, in a handful of queries whatever the size of the channel, and the
// answers are then taken from memory.

// readChunk bounds how many ids go into one IN list: both databases limit
// the number of arguments of a statement.
const readChunk = 2000

// Fanout is the one rule worked out once for one new message and everyone
// it might concern: the members of its conversation and the accounts it
// mentions. It is a picture of one moment and is used for one fan-out.
type Fanout struct {
	m    model.Message
	conv model.Conversation
	by   map[model.AccountID]*judged
	ids  []model.AccountID
}

// judged is the rule's answer for one account.
type judged struct {
	v     verdict
	level Level
	// adds is what the message adds to the account's unread sets in the
	// conversation, and counts says whether it is known: only for a
	// member, since nobody else has numbers there.
	adds   store.UnreadSets
	counts bool
	mode   model.Mode
	flat   bool
}

// Judge works the rule out for a new message and every member of its
// conversation, the accounts it mentions, and the accounts in also. The
// message needs what Decide needs.
func (s *Service) Judge(ctx context.Context, m model.Message, also []model.AccountID) (*Fanout, error) {
	now := s.now()
	set, err := s.perm.Settings(ctx)
	if err != nil {
		return nil, err
	}
	conv, err := s.store.Conversations().Get(ctx, m.ConversationID)
	if err != nil {
		return nil, err
	}
	rows, err := s.store.Members().All(ctx, conv.ID)
	if err != nil {
		return nil, err
	}
	mentions, err := s.store.Messages().Mentions(ctx, m.ID)
	if err != nil {
		return nil, err
	}
	kinds := make(map[model.AccountID]string, len(mentions))
	for _, mn := range mentions {
		kinds[mn.AccountID] = mn.Kind
	}
	var states map[model.AccountID]string
	if m.ParentID != "" {
		if states, err = s.store.Threads().States(ctx, m.ParentID); err != nil {
			return nil, err
		}
	}

	members := make(map[model.AccountID]model.Member, len(rows))
	seen := make(map[model.AccountID]bool, len(rows)+len(mentions)+len(also))
	ids := make([]model.AccountID, 0, len(rows)+len(mentions)+len(also))
	add := func(id model.AccountID) {
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	for _, r := range rows {
		members[r.AccountID] = r
		add(r.AccountID)
	}
	for _, mn := range mentions {
		add(mn.AccountID)
	}
	for _, id := range also {
		add(id)
	}

	accounts := make([]model.Account, 0, len(ids))
	// Who has blocked the author is read whatever the server's setting
	// says: the setting decides whether a block stops a notification, but
	// the unread numbers leave a blocked author's messages out always.
	blockers := map[model.AccountID]bool{}
	for from := 0; from < len(ids); from += readChunk {
		chunk := ids[from:min(from+readChunk, len(ids))]
		got, err := s.store.Accounts().GetMany(ctx, chunk)
		if err != nil {
			return nil, err
		}
		accounts = append(accounts, got...)
		blocking, err := s.store.Blocks().Blockers(ctx, m.AuthorID, chunk)
		if err != nil {
			return nil, err
		}
		for _, id := range blocking {
			blockers[id] = true
		}
	}

	f := &Fanout{m: m, conv: conv, by: make(map[model.AccountID]*judged, len(accounts)), ids: make([]model.AccountID, 0, len(accounts))}
	// Requests to notify anyway are read once, and only if someone is in
	// do not disturb: a message that has just been posted has none.
	var reached map[model.AccountID]bool
	for _, acc := range accounts {
		mem, isMember := members[acc.ID]
		state := states[acc.ID]
		fc := facts{
			blocked: set.BlockingEnabled && m.AuthorID != acc.ID && blockers[acc.ID],
			conv:    conv, mem: mem, isMember: isMember, kind: kinds[acc.ID],
			following: isMember && state == model.ThreadFollowing,
			ignored:   isMember && state == model.ThreadIgnored,
		}
		j := &judged{v: rule(m, acc, now, fc), level: None}
		switch {
		case !j.v.eligible:
		case j.v.would && !DND(acc, now).On:
			j.level = Notify
		case j.v.would:
			if reached == nil {
				list, err := s.store.NotifyAnyway().Reached(ctx, m.ID)
				if err != nil {
					return nil, err
				}
				reached = make(map[model.AccountID]bool, len(list))
				for _, id := range list {
					reached[id] = true
				}
			}
			if reached[acc.ID] {
				j.level = Notify
			} else if j.v.inBadge {
				j.level = Badge
			}
		case j.v.inBadge:
			j.level = Badge
		}
		if isMember {
			j.counts, j.mode, j.flat = true, mem.ModeInForce(now), FlatView(acc, mem)
			j.adds = added(m, acc.ID, blockers[acc.ID], fc)
		}
		f.by[acc.ID] = j
		f.ids = append(f.ids, acc.ID)
	}
	return f, nil
}

// added is what a new message adds to one member's unread sets: the same
// conditions as store.Members.Unread, for a message that is certainly above
// every read position because it is the newest.
func added(m model.Message, account model.AccountID, blocked bool, f facts) store.UnreadSets {
	var u store.UnreadSets
	if m.Deleted() || m.IsSystem() || m.Quiet || blocked || (m.AuthorID == account && !reminder(m)) {
		return u
	}
	one := func(yes bool) int64 {
		if yes {
			return 1
		}
		return 0
	}
	reply := m.ParentID != ""
	top := !reply
	fol := reply && f.following
	ign := reply && f.ignored
	nei := reply && !fol && !ign
	men := f.kind != "" && (!ign || f.kind == model.MentionAccount)
	u.Top, u.Fol, u.Nei = one(top), one(fol), one(nei)
	u.Men, u.Nam = one(men), one(f.kind == model.MentionAccount)
	u.MenTop, u.MenFol, u.MenNei, u.MenIgn = one(men && top), one(men && fol), one(men && nei), one(men && ign)
	return u
}

// Accounts returns everyone the fan-out was worked out for.
func (f *Fanout) Accounts() []model.AccountID { return f.ids }

// Level is Decide's answer for one account. known is false for an account
// the fan-out did not cover; ask Decide about that one.
func (f *Fanout) Level(to model.AccountID) (level Level, known bool) {
	j := f.by[to]
	if j == nil {
		return None, false
	}
	return j.level, true
}

// Wake is Service.Wake's answer for one account: whether the message
// notifies it, and whether the wake-up is of high priority.
func (f *Fanout) Wake(to model.AccountID) (wake, high bool) {
	j := f.by[to]
	if j == nil || j.level != Notify {
		return false, false
	}
	return true, j.v.named || f.conv.Sealed() || f.conv.Adhoc() || j.v.following
}

// Created is Changed for a message.created event whose fan-out has been
// worked out. Changed marks the conversation's numbers of everyone who has
// any kept, and each of them is then read again from the database: seven
// queries for every reader of every message. Here the kept numbers of the
// accounts the fan-out covers simply move by what the message adds.
//
// That is only right for numbers that count everything up to the event
// before this one and nothing later, which the numbers say themselves (see
// convNumbers.asOf). Anything else is marked to be read again, as before.
func (s *Service) Created(e model.Event, f *Fanout) {
	if f == nil || e.Type != model.EventMessageCreated || e.Seq == 0 {
		s.Changed(e)
		return
	}
	reply := f.m.ParentID != ""
	s.totalsMu.Lock()
	defer s.totalsMu.Unlock()
	for a := range s.byConv[e.ConversationID] {
		entry := s.totals[a]
		if entry == nil {
			continue
		}
		j := f.by[a]
		n, listed := entry.convs[e.ConversationID]
		_, dirty := entry.dirty[e.ConversationID]
		switch {
		case j == nil || !j.counts || !listed || dirty || n.asOf == 0 || e.Seq > n.asOf+1:
			s.markLocked(a, e.ConversationID)
		case e.Seq <= n.asOf:
			// The numbers were read after the message was stored and
			// count it already.
		default:
			unread, mentions, badge := j.adds.Counts(j.mode, j.flat)
			n.counts.Unread += unread
			n.counts.Mentions += mentions
			n.counts.Badge += badge
			n.asOf = e.Seq
			if e.At.After(n.lastMessageAt) {
				n.lastMessageAt = e.At
			}
			entry.convs[e.ConversationID] = n
			if reply && j.adds.Fol > 0 {
				// A followed thread may have become unread: that
				// count is read again, the rest stands.
				entry.threadsDirty = true
				entry.marks++
			}
		}
	}
	for b := range s.building {
		(*b)[e.ConversationID] = struct{}{}
	}
}

// Kept is Numbers answered from memory alone. ok is false when the
// account's numbers are not kept, are too old, or wait to be read again;
// Numbers then reads them.
func (s *Service) Kept(account model.AccountID, conv model.ConversationID) (c Counts, t model.Totals, listed, ok bool) {
	now := s.now()
	s.totalsMu.Lock()
	defer s.totalsMu.Unlock()
	e := s.totals[account]
	if e == nil || !e.expires.After(now) || len(e.dirty) > 0 || e.threadsDirty {
		return c, t, false, false
	}
	n, listed := e.convs[conv]
	t, _ = e.sum()
	return n.counts, t, listed, true
}
