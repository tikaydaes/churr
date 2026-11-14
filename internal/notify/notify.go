package notify

import (
	"context"
	"strconv"
	"sync"
	"time"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/perm"
	"github.com/tikaydaes/churr/internal/store"
)

// Level is what a new message means for one recipient.
type Level string

// The three levels (docs/FEATURES.md decision F6).
const (
	// None: the mode does not count this message.
	None Level = "none"
	// Badge: it counts towards the badge and does not notify.
	Badge Level = "badge"
	// Notify: it notifies.
	Notify Level = "notify"
)

// Service answers the notification questions from the store. It keeps each
// account's totals in memory once they have been asked for.
type Service struct {
	store store.Store
	perm  *perm.Checker
	now   func() model.Time

	// The mention rows of the message being fanned out, so deciding for a
	// thousand recipients reads them once.
	mentionsMu  sync.Mutex
	mentionsFor model.MessageID
	mentions    map[model.AccountID]string

	totalsMu sync.Mutex
	totals   map[model.AccountID]*totalsEntry
	// byConv is, per conversation, the accounts whose kept numbers include
	// it, so a change in one conversation touches only those.
	byConv map[model.ConversationID]map[model.AccountID]struct{}
	// version goes up whenever an entry is dropped. Numbers worked out
	// from scratch while it changed are returned and not kept.
	version uint64
	// building holds, for each set of numbers being worked out from
	// scratch right now, the conversations events touched meanwhile, so
	// the result is not kept as if it had seen them.
	building map[*map[model.ConversationID]struct{}]struct{}
}

// New makes the service.
func New(s store.Store, p *perm.Checker) *Service {
	return &Service{store: s, perm: p, now: model.Now,
		totals: map[model.AccountID]*totalsEntry{}, byConv: map[model.ConversationID]map[model.AccountID]struct{}{},
		building: map[*map[model.ConversationID]struct{}]struct{}{}}
}

// mentionKind returns the kind of the mention row one account has for a
// message, or "". The rows of the last message asked about are kept; the
// lock is not held while they are read, so one slow read holds nobody else
// up.
func (s *Service) mentionKind(ctx context.Context, msg model.MessageID, to model.AccountID) (string, error) {
	s.mentionsMu.Lock()
	if s.mentionsFor == msg && s.mentions != nil {
		kind := s.mentions[to]
		s.mentionsMu.Unlock()
		return kind, nil
	}
	s.mentionsMu.Unlock()
	rows, err := s.store.Messages().Mentions(ctx, msg)
	if err != nil {
		return "", err
	}
	kinds := make(map[model.AccountID]string, len(rows))
	for _, r := range rows {
		kinds[r.AccountID] = r.Kind
	}
	s.mentionsMu.Lock()
	s.mentionsFor, s.mentions = msg, kinds
	s.mentionsMu.Unlock()
	return kinds[to], nil
}

// forgetMentions drops the kept mention rows of a message: an edit works
// them out again.
func (s *Service) forgetMentions(msg model.MessageID) {
	s.mentionsMu.Lock()
	if s.mentionsFor == msg {
		s.mentionsFor, s.mentions = "", nil
	}
	s.mentionsMu.Unlock()
}

// verdict is steps 1 to 7 of docs/FEATURES.md section 3.7 for one message
// and one account, before do not disturb is looked at.
type verdict struct {
	// eligible is false when one of steps 1 to 6 said no: the account is
	// not someone to notify at all about this message.
	eligible bool
	// would is step 7: the mode would notify.
	would bool
	// inBadge: the message is in the account's badge set (section 2.3).
	inBadge bool
	// named: the account's mention row has kind `account`.
	named bool
	// anyMode: the account is a member and its mode in force is `any`.
	anyMode bool
	// following: the message is a reply in a thread the account, a
	// member, follows.
	following bool
	account   model.Account
}

func reminder(m model.Message) bool { return m.Via != nil && m.Via.Kind == "reminder" }

// facts are what the rule reads about one message and one account, besides
// the two themselves. judge reads them one account at a time, Judge
// (fanout.go) for everyone at once; rule is the same for both.
type facts struct {
	// blocked: blocking is on for the server and the account has blocked
	// the author.
	blocked  bool
	conv     model.Conversation
	mem      model.Member
	isMember bool
	// kind is the kind of the account's mention row for the message, or
	// "".
	kind string
	// following and ignored are the account's state for the thread the
	// message is a reply in.
	following, ignored bool
}

// early is steps 1 to 4, which read nothing: it reports whether one of them
// already said no.
func early(m model.Message, acc model.Account) bool {
	// 1 and 2: a remote account's home server decides for it; removed and
	// unclaimed accounts are told nothing; a bot needs no notification.
	if !acc.Active() || acc.Role == model.RoleBot || acc.Role == model.RoleRemote {
		return true
	}
	// 3: not one's own message, unless it is a reminder.
	if m.AuthorID == acc.ID && !reminder(m) {
		return true
	}
	// 4: system notices and quiet sealed messages never notify.
	return m.IsSystem() || m.Quiet
}

// rule is the one rule: the verdict for one message and one account, from
// what was read about them.
func rule(m model.Message, acc model.Account, now model.Time, f facts) verdict {
	v := verdict{account: acc}
	if early(m, acc) {
		return v
	}
	// 5: blocked authors, while blocking is on for the server.
	if f.blocked {
		return v
	}
	// 6: the account can read the conversation.
	if !f.isMember && !(f.conv.Public() && acc.Role.IsFullMember()) {
		return v
	}
	v.eligible = true

	// 7: would the mode notify?
	row := f.kind != ""
	v.named = f.kind == model.MentionAccount
	if !f.isMember {
		// A mention in a public channel of someone who has not joined
		// notifies once and has no badge (decision F3).
		v.would = row
		return v
	}
	reply := m.ParentID != ""
	following, ignored := reply && f.following, reply && f.ignored
	v.following = following
	mode := f.mem.ModeInForce(now)
	v.anyMode = mode == model.ModeAny
	// A mention row counts as MEN unless it sits in an ignored thread and
	// is not by name.
	men := row && (!ignored || v.named)
	switch mode {
	case model.ModeAny:
		switch {
		case !reply:
			v.would, v.inBadge = true, true
		case ignored:
			v.would, v.inBadge = v.named, v.named
		default:
			v.would = following || row
			// In the flat view replies in threads that are neither
			// followed nor ignored count too, without notifying.
			v.inBadge = v.would || FlatView(acc, f.mem)
		}
	case model.ModeMentions:
		switch {
		case ignored:
			v.would = v.named
		case reply:
			v.would = following || row
		default:
			v.would = row
		}
		v.inBadge = men || following
	case model.ModeNothing:
		// Nothing notifies; a mention by name still badges.
		v.inBadge = v.named
	}
	return v
}

// judge works out the verdict. to may already be loaded; pass the account.
func (s *Service) judge(ctx context.Context, m model.Message, acc model.Account) (verdict, error) {
	v := verdict{account: acc}
	if early(m, acc) {
		return v, nil
	}
	var f facts
	set, err := s.perm.Settings(ctx)
	if err != nil {
		return v, err
	}
	if set.BlockingEnabled && m.AuthorID != acc.ID {
		if f.blocked, err = s.store.Blocks().Has(ctx, acc.ID, m.AuthorID); err != nil || f.blocked {
			return v, err
		}
	}
	if f.conv, err = s.store.Conversations().Get(ctx, m.ConversationID); err != nil {
		return v, err
	}
	if f.mem, f.isMember, err = s.store.Members().Get(ctx, f.conv.ID, acc.ID); err != nil {
		return v, err
	}
	if !f.isMember && !(f.conv.Public() && acc.Role.IsFullMember()) {
		return v, nil
	}
	if f.kind, err = s.mentionKind(ctx, m.ID, acc.ID); err != nil {
		return v, err
	}
	if f.isMember && m.ParentID != "" {
		ts, found, err := s.store.Threads().Get(ctx, m.ParentID, acc.ID)
		if err != nil {
			return v, err
		}
		f.following = found && ts.State == model.ThreadFollowing
		f.ignored = found && ts.State == model.ThreadIgnored
	}
	return rule(m, acc, s.now(), f), nil
}

// decide turns a verdict into a level, looking at do not disturb and at a
// notify-anyway request that applied.
func (s *Service) decide(ctx context.Context, m model.Message, v verdict) (Level, error) {
	if !v.eligible {
		return None, nil
	}
	if v.would {
		if !DND(v.account, s.now()).On {
			return Notify, nil
		}
		pushed, err := s.store.NotifyAnyway().Delivered(ctx, m.ID, v.account.ID)
		if err != nil {
			return None, err
		}
		if pushed {
			return Notify, nil
		}
	}
	if v.inBadge {
		return Badge, nil
	}
	return None, nil
}

// Decide says what a new message means for one account: the `notify` value
// on that account's copy of message.created, and whether its phones may be
// woken (only for Notify). The message needs its ID, ConversationID,
// AuthorID, ParentID, System, Quiet and Via. Any failure reads as None: a
// notification that cannot be justified is not sent.
func (s *Service) Decide(ctx context.Context, m model.Message, to model.AccountID) Level {
	acc, err := s.store.Accounts().Get(ctx, to)
	if err != nil {
		return None
	}
	v, err := s.judge(ctx, m, acc)
	if err != nil {
		return None
	}
	level, err := s.decide(ctx, m, v)
	if err != nil {
		return None
	}
	return level
}

// AnywayApplies reports whether a notify-anyway request for this message
// reaches this account (docs/FEATURES.md section 3.8). It applies only if
// the account can read the conversation, is in do not disturb now, allows
// the feature, has not blocked the sender, and the mode would have notified
// it because the message mentions it by name or its mode is `any`.
//
// The answer goes into notify_anyway.delivered and to the wake-up, and
// nowhere else (ruling R4).
func (s *Service) AnywayApplies(ctx context.Context, m model.Message, to model.AccountID) bool {
	acc, err := s.store.Accounts().Get(ctx, to)
	if err != nil {
		return false
	}
	v, err := s.judge(ctx, m, acc)
	if err != nil || !v.eligible || !v.would {
		return false
	}
	return acc.AllowNotifyAnyway && DND(acc, s.now()).On && (v.named || v.anyMode)
}

// Rings returns the accounts a starting call rings (decision F7): people
// who are members of the conversation at level `member` or above with the
// conversation on Any activity, who have not blocked the starter and are
// not in do not disturb. Nothing pushes a ring through do not disturb.
func (s *Service) Rings(ctx context.Context, call model.Call) ([]model.AccountID, error) {
	set, err := s.perm.Settings(ctx)
	if err != nil {
		return nil, err
	}
	ids, err := s.store.Members().IDs(ctx, call.ConversationID)
	if err != nil {
		return nil, err
	}
	accounts, err := s.store.Accounts().GetMany(ctx, ids)
	if err != nil {
		return nil, err
	}
	now := s.now()
	var out []model.AccountID
	for _, acc := range accounts {
		if acc.ID == call.StartedByID || !acc.Active() || acc.Role == model.RoleBot || DND(acc, now).On {
			continue
		}
		mem, found, err := s.store.Members().Get(ctx, call.ConversationID, acc.ID)
		if err != nil {
			return nil, err
		}
		if !found || !mem.Level.AtLeast(model.LevelMember) || mem.ModeInForce(now) != model.ModeAny {
			continue
		}
		if set.BlockingEnabled {
			blocked, err := s.store.Blocks().Has(ctx, acc.ID, call.StartedByID)
			if err != nil {
				return nil, err
			}
			if blocked {
				continue
			}
		}
		out = append(out, acc.ID)
	}
	return out, nil
}

// Counts are the three numbers of one membership (section 2.3).
type Counts struct{ Unread, Mentions, Badge int64 }

// CountsFor works out the numbers of one membership for the mode in force
// and the view. The conversation list, the events and the totals all come
// through here.
func (s *Service) CountsFor(ctx context.Context, a store.Areas, acc model.Account, m model.Member) (Counts, error) {
	u, err := a.Members().Unread(ctx, m.ConversationID, acc.ID)
	if err != nil {
		return Counts{}, err
	}
	unread, mentions, badge := u.Counts(m.ModeInForce(s.now()), FlatView(acc, m))
	return Counts{Unread: unread, Mentions: mentions, Badge: badge}, nil
}

// Badge returns the badge of one conversation for one account: 0 if it is
// not a member.
func (s *Service) Badge(ctx context.Context, account model.AccountID, conv model.ConversationID) (int, error) {
	acc, err := s.store.Accounts().Get(ctx, account)
	if err != nil {
		return 0, err
	}
	m, found, err := s.store.Members().Get(ctx, conv, account)
	if err != nil || !found {
		return 0, err
	}
	c, err := s.CountsFor(ctx, s.store, acc, m)
	return int(c.Badge), err
}

// totalsTTL bounds how long kept numbers are trusted. Every change that
// passes through the chat service marks the numbers it touches at once;
// this is for the few that do not (retention deleting old messages).
const totalsTTL = 30 * time.Second

// convNumbers are one account's numbers for one conversation in its list.
type convNumbers struct {
	counts        Counts
	lastMessageAt model.Time
	// mutedUntil is when a mute runs out, which changes the numbers
	// without any event.
	mutedUntil model.Time
	// asOf is the conversation's last seq when the numbers were read:
	// they count every message up to it and none after. Created
	// (fanout.go) moves it along with the numbers.
	asOf int64
}

// totalsEntry is what is kept for one account: the numbers of each
// conversation in its list, and which of them an event has touched since.
type totalsEntry struct {
	convs   map[model.ConversationID]convNumbers
	dirty   map[model.ConversationID]struct{}
	threads int64
	// threadsDirty: the count of unread followed threads must be read
	// again.
	threadsDirty bool
	// marks goes up with every mark, so a refresh that raced with one
	// does not clear it.
	marks   uint64
	expires model.Time
}

// number works out one conversation's numbers for an account. listed is
// false when the conversation is not in the account's list: it is not a
// member, or the conversation is archived.
func (s *Service) number(ctx context.Context, acc model.Account, conv model.Conversation, m model.Member) (convNumbers, error) {
	u, err := s.store.Members().Unread(ctx, m.ConversationID, acc.ID)
	if err != nil {
		return convNumbers{}, err
	}
	now := s.now()
	unread, mentions, badge := u.Counts(m.ModeInForce(now), FlatView(acc, m))
	n := convNumbers{counts: Counts{Unread: unread, Mentions: mentions, Badge: badge}, lastMessageAt: conv.LastMessageAt, asOf: u.AsOf}
	if m.MutedUntil.After(now) {
		n.mutedUntil = m.MutedUntil
	}
	return n, nil
}

// build works an account's numbers out from scratch.
func (s *Service) build(ctx context.Context, acc model.Account) (*totalsEntry, error) {
	e := &totalsEntry{convs: map[model.ConversationID]convNumbers{}, dirty: map[model.ConversationID]struct{}{}}
	page := store.Page{Limit: model.MaxLimit}
	for {
		list, next, err := s.store.Conversations().ForAccount(ctx, acc.ID, store.ConversationFilter{}, page)
		if err != nil {
			return nil, err
		}
		for _, cm := range list {
			n, err := s.number(ctx, acc, cm.Conversation, cm.Member)
			if err != nil {
				return nil, err
			}
			e.convs[cm.Conversation.ID] = n
		}
		if next == "" {
			break
		}
		page.Cursor = next
	}
	var err error
	e.threads, err = s.store.Threads().CountUnread(ctx, acc.ID)
	return e, err
}

// expiry is when an entry stops being trusted: after the TTL, or when the
// first mute in it runs out.
func (e *totalsEntry) expiry(now model.Time) model.Time {
	at := now.Add(totalsTTL)
	for _, n := range e.convs {
		if !n.mutedUntil.IsZero() && n.mutedUntil.Before(at) {
			at = n.mutedUntil
		}
	}
	return at
}

// sum adds an entry up.
func (e *totalsEntry) sum() (model.Totals, string) {
	t := model.Totals{UnreadThreads: e.threads}
	var newest model.Time
	for _, n := range e.convs {
		t.Badge += n.counts.Badge
		t.Mentions += n.counts.Mentions
		if n.counts.Unread > 0 {
			t.UnreadConversations++
		}
		if n.lastMessageAt.After(newest) {
			newest = n.lastMessageAt
		}
	}
	if newest.IsZero() {
		return t, ""
	}
	return t, strconv.FormatInt(int64(newest), 36)
}

// current returns an account's entry, up to date: built if there is none or
// it is too old, and with the conversations events have touched read again.
// The entry returned is a private copy.
func (s *Service) current(ctx context.Context, account model.AccountID) (*totalsEntry, error) {
	now := s.now()
	s.totalsMu.Lock()
	e := s.totals[account]
	if e != nil && !e.expires.After(now) {
		s.dropLocked(account)
		e = nil
	}
	version := s.version
	var (
		dirty        []model.ConversationID
		threadsDirty bool
		marks        uint64
	)
	touched := map[model.ConversationID]struct{}{}
	if e != nil {
		for c := range e.dirty {
			dirty = append(dirty, c)
		}
		threadsDirty, marks = e.threadsDirty, e.marks
		if len(dirty) == 0 && !threadsDirty {
			cp := e.clone()
			s.totalsMu.Unlock()
			return cp, nil
		}
	} else {
		s.building[&touched] = struct{}{}
	}
	s.totalsMu.Unlock()
	if e == nil {
		defer func() {
			s.totalsMu.Lock()
			delete(s.building, &touched)
			s.totalsMu.Unlock()
		}()
	}

	acc, err := s.store.Accounts().Get(ctx, account)
	if err != nil {
		return nil, err
	}
	if e == nil {
		built, err := s.build(ctx, acc)
		if err != nil {
			return nil, err
		}
		built.expires = built.expiry(now)
		s.totalsMu.Lock()
		out := built.clone()
		if s.version == version && s.totals[account] == nil {
			// Anything an event touched while this was being read is
			// read again next time.
			for c := range touched {
				if _, in := built.convs[c]; in {
					built.dirty[c] = struct{}{}
				}
				built.threadsDirty = true
			}
			s.totals[account] = built
			for c := range built.convs {
				s.indexLocked(c, account)
			}
		}
		s.totalsMu.Unlock()
		return out, nil
	}

	// Read the touched conversations again, outside the lock.
	fresh := map[model.ConversationID]*convNumbers{}
	for _, c := range dirty {
		fresh[c] = nil
		m, found, err := s.store.Members().Get(ctx, c, account)
		if err != nil {
			return nil, err
		}
		if !found {
			continue
		}
		conv, err := s.store.Conversations().Get(ctx, c)
		if err != nil || conv.Archived() {
			// Deleted or archived: no longer in the list.
			continue
		}
		n, err := s.number(ctx, acc, conv, m)
		if err != nil {
			return nil, err
		}
		fresh[c] = &n
	}
	threads := int64(0)
	if threadsDirty {
		if threads, err = s.store.Threads().CountUnread(ctx, account); err != nil {
			return nil, err
		}
	}

	s.totalsMu.Lock()
	defer s.totalsMu.Unlock()
	cur := s.totals[account]
	if cur != e {
		// Dropped meanwhile. What was read is still right for now.
		cur = e.clone()
		cur.apply(fresh, threadsDirty, threads)
		return cur, nil
	}
	cur.apply(fresh, threadsDirty, threads)
	if cur.marks == marks {
		cur.dirty, cur.threadsDirty = map[model.ConversationID]struct{}{}, false
	}
	cur.expires = cur.expiry(now)
	for c, n := range fresh {
		if n == nil {
			s.unindexLocked(c, account)
		}
	}
	return cur.clone(), nil
}

func (e *totalsEntry) apply(fresh map[model.ConversationID]*convNumbers, threadsDirty bool, threads int64) {
	for c, n := range fresh {
		if n == nil {
			delete(e.convs, c)
		} else {
			e.convs[c] = *n
		}
	}
	if threadsDirty {
		e.threads = threads
	}
}

func (e *totalsEntry) clone() *totalsEntry {
	cp := &totalsEntry{convs: make(map[model.ConversationID]convNumbers, len(e.convs)), threads: e.threads, expires: e.expires}
	for c, n := range e.convs {
		cp.convs[c] = n
	}
	return cp
}

func (s *Service) indexLocked(c model.ConversationID, account model.AccountID) {
	set := s.byConv[c]
	if set == nil {
		set = map[model.AccountID]struct{}{}
		s.byConv[c] = set
	}
	set[account] = struct{}{}
}

func (s *Service) unindexLocked(c model.ConversationID, account model.AccountID) {
	if set := s.byConv[c]; set != nil {
		delete(set, account)
		if len(set) == 0 {
			delete(s.byConv, c)
		}
	}
}

// Totals returns an account's numbers across the conversations in its
// list: the sum of their badges and mention counts, how many have anything
// unread, and how many followed threads have unread replies. Archived
// conversations are not in the list and count nothing. Whether a
// conversation is shown in the sidebar plays no part: a hidden one counts
// like any other (ruling R46).
//
// The numbers are kept in memory once worked out. An event in a
// conversation makes only that conversation's numbers be read again.
func (s *Service) Totals(ctx context.Context, account model.AccountID) (model.Totals, error) {
	t, _, err := s.Summary(ctx, account)
	return t, err
}

// Summary is Totals with `newest`: an opaque value that changes whenever a
// newer message exists in any of the account's conversations.
func (s *Service) Summary(ctx context.Context, account model.AccountID) (model.Totals, string, error) {
	e, err := s.current(ctx, account)
	if err != nil {
		return model.Totals{}, "", err
	}
	t, newest := e.sum()
	return t, newest, nil
}

// Numbers returns an account's numbers for one conversation together with
// its totals, as badge.changed and read_position.moved carry them. listed
// is false if the conversation is not in the account's list.
func (s *Service) Numbers(ctx context.Context, account model.AccountID, conv model.ConversationID) (c Counts, t model.Totals, listed bool, err error) {
	e, err := s.current(ctx, account)
	if err != nil {
		return c, t, false, err
	}
	n, listed := e.convs[conv]
	t, _ = e.sum()
	return n.counts, t, listed, nil
}

// dropLocked forgets everything kept for one account.
func (s *Service) dropLocked(account model.AccountID) {
	e := s.totals[account]
	if e == nil {
		return
	}
	delete(s.totals, account)
	for c := range e.convs {
		s.unindexLocked(c, account)
	}
	s.version++
}

// Forget drops what is kept for accounts whose numbers may have changed in
// ways that are not one conversation's: they changed a view or a
// preference, joined or left something, blocked or unblocked someone.
func (s *Service) Forget(accounts ...model.AccountID) {
	s.totalsMu.Lock()
	s.version++
	for _, a := range accounts {
		s.dropLocked(a)
	}
	s.totalsMu.Unlock()
}

// Touch marks one conversation's numbers of one account to be read again:
// it read something there, changed its mode, or followed a thread.
func (s *Service) Touch(account model.AccountID, conv model.ConversationID) {
	s.totalsMu.Lock()
	s.markLocked(account, conv)
	for b := range s.building {
		(*b)[conv] = struct{}{}
	}
	s.totalsMu.Unlock()
}

func (s *Service) markLocked(account model.AccountID, conv model.ConversationID) {
	if e := s.totals[account]; e != nil {
		e.dirty[conv] = struct{}{}
		e.threadsDirty = true
		e.marks++
	}
}

// Changed tells the service a conversation event was committed. Everyone
// whose list includes that conversation has its numbers there read again;
// the account a membership event is about has its list read again.
func (s *Service) Changed(e model.Event) {
	if e.Type == model.EventMessageEdited && e.MessageID != "" {
		s.forgetMentions(e.MessageID)
	}
	s.totalsMu.Lock()
	defer s.totalsMu.Unlock()
	if e.Type == "conversation.unarchived" {
		// The conversation returns to the lists of people whose kept
		// numbers do not include it; which ones is not known here.
		s.version++
		s.totals = map[model.AccountID]*totalsEntry{}
		s.byConv = map[model.ConversationID]map[model.AccountID]struct{}{}
		return
	}
	for a := range s.byConv[e.ConversationID] {
		s.markLocked(a, e.ConversationID)
	}
	for b := range s.building {
		(*b)[e.ConversationID] = struct{}{}
	}
	switch e.Type {
	case model.EventMemberJoined, model.EventMemberLeft, model.EventMemberLevelChanged:
		s.version++
		if m, ok := e.Payload["member"].(model.Member); ok {
			s.dropLocked(m.AccountID)
		}
		if id, ok := e.Payload["account_id"].(model.AccountID); ok {
			s.dropLocked(id)
		}
		for _, a := range e.AlsoTo {
			s.dropLocked(a)
		}
	}
}
