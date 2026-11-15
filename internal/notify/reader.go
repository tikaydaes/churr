package notify

import (
	"context"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/perm"
)

// A client that comes back is sent every message it missed, each with what
// it means to that one reader. Decide answers for one message: it reads the
// account, the settings, the conversation, the member row, the mention rows
// and the thread state every time, ten or so queries a message, and after a
// restart with thousands of clients catching up that was most of the work.
// A Reader answers for one account and many messages: what is the same for
// all of them is read once, and what differs per message is read for a
// whole page at a time. It is the other half of Judge (fanout.go), which
// answers for one message and many accounts. The rule is the same one.

// Reader decides for one account. It is made for one catch-up and thrown
// away: what it has read is a picture of that moment. It is not safe for
// use by several goroutines.
type Reader struct {
	s  *Service
	to model.AccountID

	loaded bool
	failed bool
	acc    model.Account
	// set is the server's settings; haveSet says they were given (Know)
	// or have been read.
	set     perm.Settings
	haveSet bool
	// blocked is who the account has blocked, read when first needed.
	blocked map[model.AccountID]bool
	places  map[model.ConversationID]*place
}

// place is what the rule reads about the account in one conversation.
type place struct {
	ok       bool
	conv     model.Conversation
	mem      model.Member
	isMember bool
}

// Reader returns a Reader for one account.
func (s *Service) Reader(to model.AccountID) *Reader {
	return &Reader{s: s, to: to, places: map[model.ConversationID]*place{}}
}

// Know tells the Reader what its caller has just read: the settings, and
// the account's place in some conversations (st.Member nil for one it is
// not a member of). The Reader then does not read them again. A catch-up
// has all of this in hand from deciding what the client may be sent.
func (r *Reader) Know(states ...perm.State) {
	for _, st := range states {
		if st.Actor.ID != r.to {
			continue
		}
		if !r.haveSet {
			r.set, r.haveSet = st.Settings, true
		}
		p := &place{ok: true, conv: st.Conversation}
		if st.Member != nil {
			p.mem, p.isMember = *st.Member, true
		}
		r.places[st.Conversation.ID] = p
	}
}

// load reads what is the same for every message.
func (r *Reader) load(ctx context.Context) bool {
	if r.loaded {
		return !r.failed
	}
	r.loaded = true
	var err error
	if r.acc, err = r.s.store.Accounts().Get(ctx, r.to); err != nil {
		r.failed = true
		return false
	}
	if !r.haveSet {
		if r.set, err = r.s.perm.Settings(ctx); err != nil {
			r.failed = true
			return false
		}
		r.haveSet = true
	}
	return true
}

// hasBlocked reports whether the account has blocked an author.
func (r *Reader) hasBlocked(ctx context.Context, author model.AccountID) (bool, error) {
	if r.blocked == nil {
		list, err := r.s.store.Blocks().List(ctx, r.to)
		if err != nil {
			return false, err
		}
		r.blocked = make(map[model.AccountID]bool, len(list))
		for _, id := range list {
			r.blocked[id] = true
		}
	}
	return r.blocked[author], nil
}

// place reads the conversation and the account's member row, once.
func (r *Reader) place(ctx context.Context, id model.ConversationID) *place {
	if p := r.places[id]; p != nil {
		return p
	}
	p := &place{}
	r.places[id] = p
	var err error
	if p.conv, err = r.s.store.Conversations().Get(ctx, id); err != nil {
		return p
	}
	if p.mem, p.isMember, err = r.s.store.Members().Get(ctx, id, r.to); err != nil {
		return p
	}
	p.ok = true
	return p
}

// Decide is Service.Decide for each of the messages, in the same order.
// Like it, any failure reads as None.
func (r *Reader) Decide(ctx context.Context, msgs []model.Message) []Level {
	out := make([]Level, len(msgs))
	for i := range out {
		out[i] = None
	}
	if len(msgs) == 0 || !r.load(ctx) {
		return out
	}
	// The steps that read nothing or little, in the order judge takes
	// them; what is left needs its mention row and perhaps its thread.
	type pending struct {
		i int
		p *place
	}
	var rest []pending
	var ids, parents []model.MessageID
	seenParent := map[model.MessageID]bool{}
	for i, m := range msgs {
		if early(m, r.acc) {
			continue
		}
		if r.set.BlockingEnabled && m.AuthorID != r.acc.ID {
			if blocked, err := r.hasBlocked(ctx, m.AuthorID); err != nil || blocked {
				continue
			}
		}
		p := r.place(ctx, m.ConversationID)
		if !p.ok || (!p.isMember && !(p.conv.Public() && r.acc.Role.IsFullMember())) {
			continue
		}
		rest = append(rest, pending{i, p})
		ids = append(ids, m.ID)
		if p.isMember && m.ParentID != "" && !seenParent[m.ParentID] {
			seenParent[m.ParentID] = true
			parents = append(parents, m.ParentID)
		}
	}
	if len(rest) == 0 {
		return out
	}
	kinds := map[model.MessageID]string{}
	for from := 0; from < len(ids); from += readChunk {
		got, err := r.s.store.Messages().MentionKinds(ctx, r.to, ids[from:min(from+readChunk, len(ids))])
		if err != nil {
			return out
		}
		for id, kind := range got {
			kinds[id] = kind
		}
	}
	states := map[model.MessageID]string{}
	for from := 0; from < len(parents); from += readChunk {
		got, err := r.s.store.Threads().StatesOf(ctx, r.to, parents[from:min(from+readChunk, len(parents))])
		if err != nil {
			return out
		}
		for id, state := range got {
			states[id] = state
		}
	}
	for _, pd := range rest {
		m := msgs[pd.i]
		f := facts{conv: pd.p.conv, mem: pd.p.mem, isMember: pd.p.isMember, kind: kinds[m.ID]}
		if f.isMember && m.ParentID != "" {
			state := states[m.ParentID]
			f.following = state == model.ThreadFollowing
			f.ignored = state == model.ThreadIgnored
		}
		level, err := r.s.decide(ctx, m, rule(m, r.acc, r.s.now(), f))
		if err != nil {
			continue
		}
		out[pd.i] = level
	}
	return out
}
