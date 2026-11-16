package people

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/tikaydaes/churr/internal/live"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

// Presence by interest (docs/EVENTS.md section 4, ruling R62: presence goes
// "only to people who can currently see them"). A connection says which
// accounts it is showing; a change in someone's presence goes to the
// connections showing them and to nobody else. The hub keeps who is showing
// whom (live/interest.go); this file decides who may: a subscription is
// honoured only for accounts that share a conversation with the subscriber,
// the same people presence was announced to before.

// interestHub is the part of the live hub presence by interest needs.
// live.Hub has it; with a hub that has not, presence is announced to
// everyone who may see the account, as before.
type interestHub interface {
	SetPresenceInterest(account model.AccountID, connectionID string, ids []model.AccountID) ([]model.AccountID, bool)
	SendPresenceToInterested(about model.AccountID, v any, near []model.AccountID, maySee func(model.AccountID) bool)
	SendTo(account model.AccountID, connectionID string, v any) bool
}

// nearSize is the largest conversation whose members are near each other:
// a client that has never said what it shows (one from before
// subscriptions) is told the presence of the people it shares a direct
// message or another conversation this small with, and of nobody else.
const nearSize = 10

// rostersOf is the members of every conversation an account is in, from
// the rosters the store keeps in memory.
func (s *Service) rostersOf(ctx context.Context, account model.AccountID) ([]*store.Roster, error) {
	convs, err := s.store.Members().ConversationIDs(ctx, account)
	if err != nil {
		return nil, err
	}
	rosters := make([]*store.Roster, 0, len(convs))
	for _, c := range convs {
		r, err := s.store.Members().Roster(ctx, c)
		if err != nil {
			return nil, err
		}
		rosters = append(rosters, r)
	}
	return rosters, nil
}

func inAny(rosters []*store.Roster, id model.AccountID) bool {
	for _, r := range rosters {
		if r.Has(id) {
			return true
		}
	}
	return false
}

// tellInterested sends a presence change about an account to the
// connections showing it.
func (s *Service) tellInterested(ctx context.Context, hub interestHub, account model.AccountID, e model.Event, blocked map[model.AccountID]bool) {
	rosters, err := s.rostersOf(ctx, account)
	if err != nil {
		s.log.Error("could not read who shares a conversation with an account; they were not told of a change", "err", err)
		// The account's own connections are told whatever happens.
		hub.SendPresenceToInterested(account, e, nil, func(model.AccountID) bool { return false })
		return
	}
	var near []model.AccountID
	for _, r := range rosters {
		if r.Len() <= nearSize {
			for _, id := range r.IDs {
				if keeps(blocked, id) || id == account {
					near = append(near, id)
				}
			}
		}
	}
	hub.SendPresenceToInterested(account, e, near, func(id model.AccountID) bool { return keeps(blocked, id) && inAny(rosters, id) })
}

// PresenceFrame takes the `presence.subscribe` frame of the live
// connection: the accounts this connection is showing now, in place of any
// it named before. The connection is sent the presence of the ones that are
// new to it, and from then on changes for the ones it named only.
func (s *Service) PresenceFrame(ctx context.Context, from live.Client, frameType string, frame json.RawMessage) error {
	var f struct {
		Type       string   `json:"type"`
		ID         string   `json:"id"`
		AccountIDs []string `json:"account_ids"`
	}
	dec := json.NewDecoder(bytes.NewReader(frame))
	dec.DisallowUnknownFields()
	if frameType != "presence.subscribe" || dec.Decode(&f) != nil || f.AccountIDs == nil || len(f.AccountIDs) > live.MaxPresenceInterest {
		return model.BadRequest("`presence.subscribe` takes `account_ids`, at most 500 of them.")
	}
	hub, ok := s.hub.(interestHub)
	if !ok {
		return nil
	}
	me := from.Account.ID
	rosters, err := s.rostersOf(ctx, me)
	if err != nil {
		return err
	}
	// Accounts the subscriber may not see, and ones that do not exist,
	// are left out without a word: the answer must not say which is which.
	// People the subscriber is shown in a chat they are not in (ruling
	// R77) are subscribed to as if they shared it, and are online.
	ghosts, err := s.ghostPeers(ctx, me)
	if err != nil {
		return err
	}
	ids := make([]model.AccountID, 0, len(f.AccountIDs))
	for _, raw := range f.AccountIDs {
		if !model.ValidID(raw) {
			return model.BadRequest("`presence.subscribe` takes `account_ids`, at most 500 of them.")
		}
		if id := model.AccountID(raw); id == me || inAny(rosters, id) || ghosts[id] {
			ids = append(ids, id)
		}
	}
	added, open := hub.SetPresenceInterest(me, from.ConnectionID, ids)
	if !open || len(added) == 0 {
		return nil
	}
	changes, err := s.presenceNow(ctx, added)
	if err != nil {
		return err
	}
	// Whoever has blocked the subscriber is online to them (ruling R77).
	blockers, err := s.blockersOf(ctx, me)
	if err != nil {
		return err
	}
	for i, c := range changes {
		if id, _ := c.Payload["account_id"].(model.AccountID); blockers[id] || s.perm.RemoteDeaf(id)[me] || (ghosts[id] && !inAny(rosters, id)) {
			changes[i] = s.event(model.EventPresenceChanged, map[string]any{"account_id": id, "presence": model.PresenceOnline, "in_call": false})
		}
	}
	if len(changes) == 1 {
		hub.SendTo(me, from.ConnectionID, changes[0])
	} else {
		hub.SendTo(me, from.ConnectionID, s.event(live.EventPresenceBatch, map[string]any{"changes": changes}))
	}
	return nil
}

// presenceNow is what others are shown of each account's presence now, as
// the presence.changed events that would say so.
func (s *Service) presenceNow(ctx context.Context, ids []model.AccountID) ([]model.Event, error) {
	shown := make(map[model.AccountID]model.Presence, len(ids))
	var ask []model.AccountID
	s.viewsMu.Lock()
	for _, id := range ids {
		if v, known := s.views[id]; known {
			// What was last announced is what everyone else holds.
			shown[id] = v.presence
		}
	}
	s.viewsMu.Unlock()
	inCall := map[model.AccountID]bool{}
	for _, id := range ids {
		if _, known := shown[id]; known {
			continue
		}
		// An account of another server has no connection here: what its
		// own server last said is what is shown.
		if s.remote != nil {
			if p, calling, ok := s.remote(id); ok {
				shown[id] = p
				inCall[id] = calling
				continue
			}
		}
		// Someone with no connection is offline whatever else is true
		// of them; only the others need reading, for what may turn
		// online into away.
		if s.presence.Presence(id) == model.PresenceOffline {
			shown[id] = model.PresenceOffline
		} else {
			ask = append(ask, id)
		}
	}
	if len(ask) > 0 {
		accounts, err := s.store.Accounts().GetMany(ctx, ask)
		if err != nil {
			return nil, err
		}
		now := s.now()
		for _, a := range accounts {
			shown[a.ID] = s.viewOf(a, now).presence
		}
	}
	out := make([]model.Event, 0, len(ids))
	for _, id := range ids {
		p, known := shown[id]
		if !known {
			continue
		}
		out = append(out, s.event(model.EventPresenceChanged, map[string]any{"account_id": id, "presence": p, "in_call": s.inCall(id) || inCall[id]}))
	}
	return out, nil
}
