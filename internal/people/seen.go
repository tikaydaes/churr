package people

import (
	"context"

	"github.com/tikaydaes/churr/internal/model"
)

// A block is deaf ears (ruling R77): when X has blocked Y, Y is always
// shown X online, with no status, availability, do not disturb mark, call
// or last seen, and is sent no change of any of them. Everything that
// shows one account's presence to another goes through here or through
// PresenceSeenBy: account objects (show's callers), presence.changed and
// the other account events (tell, tellPresence), the presence a
// subscription is sent first (presenceNow) and watches. A block made on
// another server is not known here.

// blockedView is an account as someone it has blocked is shown it: online
// and nothing more.
func blockedView(a model.Account) model.Account {
	a.Presence = model.PresenceOnline
	a.Status, a.Availability, a.DND, a.InCall = nil, nil, false, false
	if a.LastSeen != "" {
		a.LastSeen = model.LastSeenOnline
	}
	return a
}

// blocking is everyone an account has blocked, while blocking is on: the
// people who are told nothing of it.
func (s *Service) blocking(ctx context.Context, account model.AccountID) map[model.AccountID]bool {
	if set, err := s.perm.Settings(ctx); err != nil || !set.BlockingEnabled {
		return nil
	}
	ids, err := s.store.Blocks().Blocking(ctx, account)
	if err != nil {
		s.log.Error("could not read whom an account has blocked; they were told nothing of a change", "err", err)
		return map[model.AccountID]bool{"*": true}
	}
	if len(ids) == 0 {
		return nil
	}
	out := make(map[model.AccountID]bool, len(ids))
	for _, id := range ids {
		out[id] = true
	}
	return out
}

// keeps reports whether an account in a blocking set is told of changes.
// A set that could not be read tells nobody.
func keeps(blocked map[model.AccountID]bool, id model.AccountID) bool {
	return !blocked[id] && !blocked["*"]
}

// blockersOf is everyone who has blocked the viewer, while blocking is on.
func (s *Service) blockersOf(ctx context.Context, viewer model.AccountID) (map[model.AccountID]bool, error) {
	if set, err := s.perm.Settings(ctx); err != nil || !set.BlockingEnabled {
		return nil, err
	}
	ids, err := s.store.Blocks().BlockersOf(ctx, viewer)
	if err != nil || len(ids) == 0 {
		return nil, err
	}
	out := make(map[model.AccountID]bool, len(ids))
	for _, id := range ids {
		out[id] = true
	}
	return out, nil
}

// seenBy applies a block to an account already shown to viewer: if the
// account has blocked the viewer and the viewer is shown its presence at
// all, it is shown online and nothing else.
func (s *Service) seenBy(ctx context.Context, viewer model.AccountID, a model.Account, shares bool) (model.Account, error) {
	if !shares || viewer == a.ID || a.Removed() {
		return a, nil
	}
	blocked, err := s.perm.HasBlocked(ctx, a.ID, viewer)
	if err != nil || !blocked {
		return a, err
	}
	return blockedView(a), nil
}

// ghostPeers is the people the viewer is shown in an open-looking chat
// without their being in it: people who blocked the viewer (ruling R77). In
// a real chat the viewer would be shown their presence; here they are shown
// online and nothing more, whatever else is true.
func (s *Service) ghostPeers(ctx context.Context, viewer model.AccountID) (map[model.AccountID]bool, error) {
	ids, err := s.store.ChatRequests().GhostPeers(ctx, viewer)
	if err != nil || len(ids) == 0 {
		return nil, err
	}
	out := make(map[model.AccountID]bool, len(ids))
	for _, id := range ids {
		out[id] = true
	}
	return out, nil
}

// asGhostPeer shows an account the viewer shares no conversation with but
// is shown as a member of one: online, as someone who blocked them is in a
// conversation they really share. withLastSeen fills last_seen as getAccount
// does.
func asGhostPeer(a model.Account, withLastSeen bool) model.Account {
	a = blockedView(a)
	if withLastSeen && a.Local() && a.Role != model.RoleBot {
		a.LastSeen = model.LastSeenOnline
	}
	return a
}

// tellBlockChange brings what blocked is shown of blocker in line with a
// block that was just made (on) or lifted: the events that say what is now
// shown, only where it differs from what was, and only if the blocked
// account is shown the blocker's presence at all.
func (s *Service) tellBlockChange(ctx context.Context, blocker, blocked model.AccountID, on bool) {
	shares, err := s.store.Accounts().SharesConversation(ctx, blocked, blocker)
	if err != nil || !shares {
		return
	}
	a, err := s.store.Accounts().Get(ctx, blocker)
	if err != nil || a.Removed() {
		return
	}
	real := s.viewOf(a, s.now())
	inCall := s.inCall(blocker)
	ghost := view{presence: model.PresenceOnline}
	from, to, callNow := real, ghost, false
	if !on {
		from, to, callNow = ghost, real, inCall
	}
	if from.presence != to.presence || inCall {
		s.hub.Send(blocked, s.event(model.EventPresenceChanged, map[string]any{"account_id": blocker, "presence": to.presence, "in_call": callNow}))
	}
	if !same(from.status, to.status) {
		f := map[string]any{"account_id": blocker}
		if to.status != nil {
			f["status"] = to.status
		}
		s.hub.Send(blocked, s.event(model.EventStatusChanged, f))
	}
	if !same(from.availability, to.availability) {
		f := map[string]any{"account_id": blocker}
		if to.availability != nil {
			f["availability"] = to.availability
		}
		s.hub.Send(blocked, s.event(model.EventAvailabilityChanged, f))
	}
	if from.dnd != to.dnd {
		s.hub.Send(blocked, s.event(model.EventDndChanged, map[string]any{"account_id": blocker, "dnd": to.dnd}))
	}
}
