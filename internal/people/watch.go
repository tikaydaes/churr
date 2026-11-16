package people

import (
	"context"

	"github.com/tikaydaes/churr/internal/model"
)

// What the watches need of people (ruling R76): whether someone is around
// now, being told when someone others are shown becomes around, and being
// told when someone blocks someone. The scheduler keeps the watches; people
// only says what is true.

// Watchers is told about the changes a watch waits on. The scheduler has it.
type Watchers interface {
	// Around is called when others start being shown an account as
	// online. It must cost next to nothing for an account nobody watches.
	Around(account model.AccountID)
	// Blocked is called when blocker blocks blocked.
	Blocked(blocker, blocked model.AccountID)
}

// OnBlock sets who is told when the account blocks someone (on) or lifts a
// block: the chat service, whose requests follow blocks. It is set once
// while the server is being put together.
func (s *Service) OnBlock(fn func(ctx context.Context, blocker, blocked model.AccountID, on bool)) {
	s.onBlock = fn
}

// SetWatchers connects the scheduler's watches. It is set once while the
// server is being put together.
func (s *Service) SetWatchers(w Watchers) { s.watchers = w }

// Around reports whether an account is around now: others are shown it
// online, which means a client of it sent activity in the last five minutes
// and it does not appear away (docs/FEATURES.md section 3.1). For an account
// of another server it is what its home server last said, and no when
// nothing has been heard or its server has gone quiet.
func (s *Service) Around(ctx context.Context, a model.Account) bool {
	if !a.Local() {
		if s.remote == nil {
			return false
		}
		p, _, ok := s.remote(a.ID)
		return ok && p == model.PresenceOnline
	}
	return s.viewOf(a, s.now()).presence == model.PresenceOnline
}

// PresenceSeenBy is the presence of an account as one viewer may see it:
// the one place that says so for a viewer in particular. When the account
// has blocked the viewer it always appears online to them (ruling R77),
// which is also why a watch for it answers at once that it is around. A
// block made on another server counts as that server said it.
func (s *Service) PresenceSeenBy(ctx context.Context, viewer, a model.Account) (model.Presence, error) {
	if s.deafTo(a, viewer.ID) {
		return model.PresenceOnline, nil
	}
	if blocked, err := s.perm.HasBlocked(ctx, a.ID, viewer.ID); err != nil {
		return model.PresenceOffline, err
	} else if blocked {
		return model.PresenceOnline, nil
	}
	if s.Around(ctx, a) {
		return model.PresenceOnline, nil
	}
	if !a.Local() {
		if s.remote != nil {
			if p, _, ok := s.remote(a.ID); ok {
				return p, nil
			}
		}
		return "", nil
	}
	return s.viewOf(a, s.now()).presence, nil
}

// AroundFor reports whether viewer may see account a as around now
// (PresenceSeenBy).
func (s *Service) AroundFor(ctx context.Context, viewer, a model.Account) (bool, error) {
	p, err := s.PresenceSeenBy(ctx, viewer, a)
	return p == model.PresenceOnline, err
}

// PresenceShown is the presence others are shown of one of this server's
// accounts: what was last announced, or worked out now. The link sends this
// to other servers, so appearing away is honoured there too.
func (s *Service) PresenceShown(account model.AccountID) model.Presence {
	s.viewsMu.Lock()
	v, known := s.views[account]
	s.viewsMu.Unlock()
	if known {
		return v.presence
	}
	if s.presence.Presence(account) == model.PresenceOffline {
		return model.PresenceOffline
	}
	a, err := s.store.Accounts().Get(context.Background(), account)
	if err != nil {
		return model.PresenceOffline
	}
	return s.viewOf(a, s.now()).presence
}

// shownChanged is called by announce when what others are shown of an
// account's presence changed.
func (s *Service) shownChanged(account model.AccountID, now model.Presence) {
	if now == model.PresenceOnline && s.watchers != nil {
		s.watchers.Around(account)
	}
	if s.onShown != nil {
		s.onShown(account)
	}
}

// OnShownPresence sets who else is told when what others are shown of an
// account's presence changes, including by appearing away or back: the link,
// so other servers hear of it. It is set once while the server is being put
// together.
func (s *Service) OnShownPresence(fn func(account model.AccountID)) { s.onShown = fn }
