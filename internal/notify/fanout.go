package notify

import (
	"context"
	"sync"

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

// Fanout is the one rule for one new message, answered for whoever is
// asked about: a member of its conversation, an account it mentions, or an
// account in also. It is a picture of one moment and is used for one
// fan-out, by the live delivery and by the phone wake-ups alike.
//
// Nothing in it is worked out per member in advance. What the rule reads
// that is the same for everyone (the conversation, the mention rows, who
// follows the thread, who blocked the author) is read once when it is made;
// the members and the accounts come from the store's kept rosters and
// facts (store/roster.go). An answer then costs a few lookups in memory, so
// a message costs what it is delivered to and not what its channel holds.
//
// It may be asked from several goroutines at once.
type Fanout struct {
	s      *Service
	ctx    context.Context
	m      model.Message
	now    model.Time
	conv   model.Conversation
	roster *store.Roster
	// blocking is the server's setting; blockers have blocked the author
	// whatever it says.
	blocking bool
	blockers map[model.AccountID]bool
	kinds    map[model.AccountID]string
	states   map[model.AccountID]string
	also     map[model.AccountID]bool
	// mentioned are the accounts with a mention row, in the rows' order.
	mentioned []model.AccountID

	// Requests to notify anyway are read once, and only if someone is in
	// do not disturb: a message that has just been posted has none.
	reachedOnce sync.Once
	reached     map[model.AccountID]bool
	reachedErr  error
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

// warmBelow is the size of conversation up to which the accounts of every
// member are read together the first time a message is judged there. In a
// larger one each account is read when it is first asked about, so that a
// message in a channel of fifty thousand reads the accounts of those it
// reaches and no others.
const warmBelow = readChunk

// Known is what the caller of JudgeKnown already holds of a message it has
// just stored, so that the fan-out does not read it back.
type Known struct {
	// Conversation is the message's conversation, if HaveConversation.
	// Only what never changes under a message is read from it (whether
	// it is public, ad-hoc or sealed).
	Conversation     model.Conversation
	HaveConversation bool
	// Mentions is every row of the mentions table for the message, if
	// HaveMentions: one for each account, MessageID left empty or not.
	Mentions     []model.Mention
	HaveMentions bool
}

// Judge prepares the rule for a new message. The message needs what Decide
// needs. also names accounts outside the conversation the event goes to.
func (s *Service) Judge(ctx context.Context, m model.Message, also []model.AccountID) (*Fanout, error) {
	return s.JudgeKnown(ctx, m, also, nil)
}

// JudgeKnown is Judge for a caller that holds some of what Judge reads:
// the service that stored the message a moment ago. known may be nil.
func (s *Service) JudgeKnown(ctx context.Context, m model.Message, also []model.AccountID, known *Known) (*Fanout, error) {
	if known == nil {
		known = &Known{}
	}
	set, err := s.perm.Settings(ctx)
	if err != nil {
		return nil, err
	}
	conv := known.Conversation
	if !known.HaveConversation {
		if conv, err = s.store.Conversations().Get(ctx, m.ConversationID); err != nil {
			return nil, err
		}
	}
	roster, err := s.store.Members().Roster(ctx, conv.ID)
	if err != nil {
		return nil, err
	}
	mentions := known.Mentions
	if !known.HaveMentions {
		if mentions, err = s.store.Messages().Mentions(ctx, m.ID); err != nil {
			return nil, err
		}
	}
	// The answers may be asked for after the request that posted the
	// message has ended (the wake-ups are), so what is read later does
	// not end with it.
	f := &Fanout{s: s, ctx: context.WithoutCancel(ctx), m: m, now: s.now(), conv: conv, roster: roster,
		blocking: set.BlockingEnabled}
	if len(mentions) > 0 {
		f.kinds = make(map[model.AccountID]string, len(mentions))
		for _, mn := range mentions {
			if _, dup := f.kinds[mn.AccountID]; !dup {
				f.mentioned = append(f.mentioned, mn.AccountID)
			}
			f.kinds[mn.AccountID] = mn.Kind
		}
	}
	if m.ParentID != "" {
		if f.states, err = s.store.Threads().States(ctx, m.ParentID); err != nil {
			return nil, err
		}
	}
	if len(also) > 0 {
		f.also = make(map[model.AccountID]bool, len(also))
		for _, id := range also {
			f.also[id] = true
		}
	}
	// Who has blocked the author is read whatever the server's setting
	// says: the setting decides whether a block stops a notification, but
	// the unread numbers leave a blocked author's messages out always.
	blocking, err := s.store.Blocks().BlockersOf(ctx, m.AuthorID)
	if err != nil {
		return nil, err
	}
	if len(blocking) > 0 {
		f.blockers = make(map[model.AccountID]bool, len(blocking))
		for _, id := range blocking {
			f.blockers[id] = true
		}
	}
	if roster.Len() <= warmBelow && !roster.Warm.Load() {
		if _, err := s.store.Accounts().NotifyFacts(ctx, roster.IDs); err != nil {
			return nil, err
		}
		roster.Warm.Store(true)
	}
	return f, nil
}

// covers reports whether the fan-out answers for an account: a member, an
// account the message mentions, or one named in also.
func (f *Fanout) covers(id model.AccountID) bool {
	if f.roster.Has(id) || f.also[id] {
		return true
	}
	_, mentioned := f.kinds[id]
	return mentioned
}

// one is the rule for one account whose facts are in hand.
func (f *Fanout) one(acc *model.Account) (judged, error) {
	return f.oneOf(acc, true)
}

// oneOf is one with the choice of leaving out what the message adds to the
// account's unread numbers, which a caller that only wants the level has no
// use for and which costs a look at the account's preferences.
func (f *Fanout) oneOf(acc *model.Account, numbers bool) (judged, error) {
	rm, isMember := f.roster.Find(acc.ID)
	var mem model.Member
	if isMember {
		mem = rm.Member(f.conv.ID)
	}
	state := f.states[acc.ID]
	fc := facts{
		blocked: f.blocking && f.m.AuthorID != acc.ID && f.blockers[acc.ID],
		conv:    f.conv, mem: mem, isMember: isMember, kind: f.kinds[acc.ID],
		following: isMember && state == model.ThreadFollowing,
		ignored:   isMember && state == model.ThreadIgnored,
	}
	j := judged{v: rule(f.m, *acc, f.now, fc), level: None}
	switch {
	case !j.v.eligible:
	case j.v.would && !DND(*acc, f.now).On:
		j.level = Notify
	case j.v.would:
		f.reachedOnce.Do(func() {
			list, err := f.s.store.NotifyAnyway().Reached(f.ctx, f.m.ID)
			if err != nil {
				f.reachedErr = err
				return
			}
			f.reached = make(map[model.AccountID]bool, len(list))
			for _, id := range list {
				f.reached[id] = true
			}
		})
		if f.reachedErr != nil {
			return j, f.reachedErr
		}
		if f.reached[acc.ID] {
			j.level = Notify
		} else if j.v.inBadge {
			j.level = Badge
		}
	case j.v.inBadge:
		j.level = Badge
	}
	if isMember && numbers {
		j.counts, j.mode, j.flat = true, mem.ModeInForce(f.now), FlatView(*acc, mem)
		j.adds = added(f.m, acc.ID, f.blockers[acc.ID], fc)
	}
	return j, nil
}

// judge answers for one account. known is false for an account the fan-out
// does not cover, one that does not exist, and when something could not be
// read: the caller then asks the slow way, or does without.
func (f *Fanout) judge(to model.AccountID) (j judged, known bool) {
	if !f.covers(to) {
		return j, false
	}
	got, err := f.s.store.Accounts().NotifyFacts(f.ctx, []model.AccountID{to})
	if err != nil || got[0] == nil {
		return j, false
	}
	j, err = f.one(got[0])
	return j, err == nil
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

// Accounts returns everyone the fan-out answers for: the members, the
// accounts mentioned and the accounts in also. It is as long as the
// conversation is large; a fan-out does not need it (see Notified).
func (f *Fanout) Accounts() []model.AccountID {
	ids := make([]model.AccountID, 0, f.roster.Len()+len(f.mentioned)+len(f.also))
	ids = append(ids, f.roster.IDs...)
	for _, id := range f.mentioned {
		if !f.roster.Has(id) {
			ids = append(ids, id)
		}
	}
	for id := range f.also {
		if _, mentioned := f.kinds[id]; !mentioned && !f.roster.Has(id) {
			ids = append(ids, id)
		}
	}
	return ids
}

// Level is Decide's answer for one account. known is false for an account
// the fan-out did not cover; ask Decide about that one.
func (f *Fanout) Level(to model.AccountID) (level Level, known bool) {
	j, known := f.judge(to)
	if !known {
		return None, false
	}
	return j.level, true
}

// Levels is Level for many accounts at once: their facts are taken from
// the store's memory under one hold of its lock, and read together where
// they are not there yet, so a goroutine handing a message to thousands of
// readers asks once and then works alone. known[i] is false where Level
// would say so.
func (f *Fanout) Levels(to []model.AccountID) (levels []Level, known []bool) {
	levels, known = make([]Level, len(to)), make([]bool, len(to))
	got, err := f.s.store.Accounts().NotifyFacts(f.ctx, to)
	if err != nil {
		for i := range levels {
			levels[i] = None
		}
		return levels, known
	}
	for i, id := range to {
		levels[i] = None
		if got[i] == nil || !f.covers(id) {
			continue
		}
		if j, err := f.oneOf(got[i], false); err == nil {
			levels[i], known[i] = j.level, true
		}
	}
	return levels, known
}

// high says whether a wake-up for a notified account is of high priority.
func (f *Fanout) high(j judged) bool {
	return j.v.named || f.conv.Sealed() || f.conv.Adhoc() || j.v.following
}

// Wake is Service.Wake's answer for one account: whether the message
// notifies it, and whether the wake-up is of high priority.
func (f *Fanout) Wake(to model.AccountID) (wake, high bool) {
	j, known := f.judge(to)
	if !known || j.level != Notify {
		return false, false
	}
	return true, f.high(j)
}

// Notified calls fn for every account the message notifies, with the
// priority of its wake-up. It looks only at those the message could notify:
// the accounts it mentions, and then, for a reply, the members who follow
// its thread, or else the members whose mode is `any`. Everyone else's mode
// says no without being asked, so a message in a large channel on
// "mentions" costs nothing for the members it does not name.
func (f *Fanout) Notified(fn func(to model.AccountID, high bool)) error {
	if f.m.IsSystem() || f.m.Quiet {
		return nil
	}
	ids := make([]model.AccountID, 0, len(f.mentioned)+len(f.states))
	ids = append(ids, f.mentioned...)
	if f.m.ParentID != "" {
		for id, state := range f.states {
			if _, mentioned := f.kinds[id]; !mentioned && state == model.ThreadFollowing && f.roster.Has(id) {
				ids = append(ids, id)
			}
		}
	} else {
		for _, i := range f.roster.Loud {
			id := f.roster.Members[i].AccountID
			if _, mentioned := f.kinds[id]; !mentioned {
				ids = append(ids, id)
			}
		}
	}
	accounts, err := f.s.store.Accounts().NotifyFacts(f.ctx, ids)
	if err != nil {
		return err
	}
	for _, acc := range accounts {
		if acc == nil {
			continue
		}
		j, err := f.one(acc)
		if err != nil {
			return err
		}
		if j.level == Notify {
			fn(acc.ID, f.high(j))
		}
	}
	return nil
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
	// Whose numbers are kept is looked up first and the rule worked out
	// for them with the lock let go: an account's facts may have to be
	// read.
	s.totalsMu.Lock()
	ids := make([]model.AccountID, 0, len(s.byConv[e.ConversationID]))
	for a := range s.byConv[e.ConversationID] {
		ids = append(ids, a)
	}
	s.totalsMu.Unlock()
	type move struct {
		known, fol              bool
		unread, mentions, badge int64
	}
	moves := make([]move, len(ids))
	if accounts, err := s.store.Accounts().NotifyFacts(f.ctx, ids); err == nil {
		for i, acc := range accounts {
			if acc == nil || !f.roster.Has(acc.ID) {
				continue
			}
			j, err := f.one(acc)
			if err != nil || !j.counts {
				continue
			}
			mv := move{known: true, fol: j.adds.Fol > 0}
			mv.unread, mv.mentions, mv.badge = j.adds.Counts(j.mode, j.flat)
			moves[i] = mv
		}
	}

	s.totalsMu.Lock()
	defer s.totalsMu.Unlock()
	for i, a := range ids {
		entry := s.totals[a]
		if entry == nil {
			continue
		}
		mv := moves[i]
		n, listed := entry.convs[e.ConversationID]
		_, dirty := entry.dirty[e.ConversationID]
		switch {
		case !mv.known || !listed || dirty || n.asOf == 0 || e.Seq > n.asOf+1:
			s.markLocked(a, e.ConversationID)
		case e.Seq <= n.asOf:
			// The numbers were read after the message was stored and
			// count it already.
		default:
			n.counts.Unread += mv.unread
			n.counts.Mentions += mv.mentions
			n.counts.Badge += mv.badge
			n.asOf = e.Seq
			if e.At.After(n.lastMessageAt) {
				n.lastMessageAt = e.At
			}
			entry.convs[e.ConversationID] = n
			if reply && mv.fol {
				// A followed thread may have become unread: that
				// count is read again, the rest stands.
				entry.threadsDirty = true
				entry.marks++
			}
		}
	}
	// An account whose numbers came to be kept between the two holds of
	// the lock was not looked at: its numbers are read again.
	if len(s.byConv[e.ConversationID]) != len(ids) {
		seen := make(map[model.AccountID]struct{}, len(ids))
		for _, a := range ids {
			seen[a] = struct{}{}
		}
		for a := range s.byConv[e.ConversationID] {
			if _, ok := seen[a]; !ok {
				s.markLocked(a, e.ConversationID)
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
