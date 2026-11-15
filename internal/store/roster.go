package store

import (
	"sync/atomic"

	"github.com/tikaydaes/churr/internal/model"
)

// A message in a channel of fifty thousand people concerns the few who are
// connected and the few it notifies. Reading every member's row, and every
// member's account, for each message costs as much as the channel is large,
// whoever is there to read it. So the store keeps, for each conversation
// that has been asked about, the short list below in memory, and for each
// account the columns the notification rule reads. Both are read once and
// then kept right by the store itself: every method that writes a row they
// came from marks them, and the next reader sees the change
// (sqlstore/roster.go lists the writers).

// RosterMember is what a fan-out needs to know of one membership: who, at
// what level, and what decides whether a message there notifies them.
type RosterMember struct {
	AccountID      model.AccountID
	Level          model.Level
	Mode           model.Mode
	ModeBeforeMute model.Mode
	MutedUntil     model.Time
	// ThreadView is the membership's own choice of view, or "".
	ThreadView string
}

// Member returns the membership as a model.Member with the columns a
// roster keeps filled in, and no others.
func (m RosterMember) Member(conv model.ConversationID) model.Member {
	return model.Member{ConversationID: conv, AccountID: m.AccountID, Level: m.Level, Mode: m.Mode,
		ModeBeforeMute: m.ModeBeforeMute, MutedUntil: m.MutedUntil, ThreadView: m.ThreadView}
}

// Roster is the members of one conversation at one moment. It never
// changes once made and is shared by everyone who asks: read it, do not
// write to it or to its slices.
type Roster struct {
	ConversationID model.ConversationID
	// IDs is every member, longest-standing first, as Members.IDs
	// returns them.
	IDs []model.AccountID
	// Members is the same people in the same order.
	Members []RosterMember
	// Loud is the positions in Members of those whose mode is, or was
	// before a mute, `any`: the only members a message that neither
	// mentions them nor continues a thread they follow can notify.
	Loud []int32

	index map[model.AccountID]int32
	// Warm is set by the notification rule once it has had the accounts
	// of every member read together, so it does that once and not for
	// every message.
	Warm atomic.Bool
}

// NewRoster makes a roster of the members given, in their order.
func NewRoster(conv model.ConversationID, members []RosterMember) *Roster {
	r := &Roster{ConversationID: conv, Members: members, IDs: make([]model.AccountID, len(members)),
		index: make(map[model.AccountID]int32, len(members))}
	for i, m := range members {
		r.IDs[i] = m.AccountID
		r.index[m.AccountID] = int32(i)
		if m.Mode == model.ModeAny || m.ModeBeforeMute == model.ModeAny {
			r.Loud = append(r.Loud, int32(i))
		}
	}
	return r
}

// Len is how many members there are.
func (r *Roster) Len() int {
	if r == nil {
		return 0
	}
	return len(r.Members)
}

// Has reports whether an account is a member.
func (r *Roster) Has(account model.AccountID) bool {
	if r == nil {
		return false
	}
	_, ok := r.index[account]
	return ok
}

// Find returns one member.
func (r *Roster) Find(account model.AccountID) (RosterMember, bool) {
	if r == nil {
		return RosterMember{}, false
	}
	i, ok := r.index[account]
	if !ok {
		return RosterMember{}, false
	}
	return r.Members[i], true
}

// Patched returns a roster with some members read again: rows holds the
// ones among changed that are still members, and the rest of changed have
// left. Members who stay keep their place; new ones go to the end.
func (r *Roster) Patched(changed []model.AccountID, rows []RosterMember) *Roster {
	fresh := make(map[model.AccountID]RosterMember, len(rows))
	for _, m := range rows {
		fresh[m.AccountID] = m
	}
	gone := make(map[model.AccountID]bool, len(changed))
	for _, id := range changed {
		if _, still := fresh[id]; !still {
			gone[id] = true
		}
	}
	out := make([]RosterMember, 0, len(r.Members)+len(rows))
	for _, m := range r.Members {
		if gone[m.AccountID] {
			continue
		}
		if now, ok := fresh[m.AccountID]; ok {
			m = now
			delete(fresh, m.AccountID)
		}
		out = append(out, m)
	}
	for _, m := range rows {
		if _, isNew := fresh[m.AccountID]; isNew {
			out = append(out, m)
		}
	}
	p := NewRoster(r.ConversationID, out)
	p.Warm.Store(r.Warm.Load())
	return p
}
