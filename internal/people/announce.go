package people

import (
	"context"
	"encoding/json"
	"sort"
	"time"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/notify"
	"github.com/tikaydaes/churr/internal/store"
)

// view is what other people are shown of an account's state at one moment:
// presence, status, availability and the do not disturb mark, plus the
// detail of do not disturb that only the account itself sees.
type view struct {
	presence     model.Presence
	status       *model.Status
	availability *model.Availability
	dnd          bool
	dndState     model.Dnd
}

// viewOf works the view out from the stored columns and the connections.
func (s *Service) viewOf(a model.Account, now model.Time) view {
	v := view{presence: s.presence.Presence(a.ID)}
	if a.Removed() {
		v.presence = model.PresenceOffline
		v.dndState = model.Dnd{Sources: []string{}}
		return v
	}
	if notify.StatusExists(a, now) {
		v.status = &model.Status{Emoji: a.StatusEmoji, Text: a.StatusText, Until: a.StatusUntil,
			PauseNotifications: a.StatusDND, AppearAway: a.StatusAway}
	}
	if av := notify.AvailabilityNow(a, now); av != "" {
		by := "self"
		if len(a.AvailabilityBy) >= len("integration") && a.AvailabilityBy[:len("integration")] == "integration" {
			by = "integration"
		}
		v.availability = &model.Availability{Value: av, Until: a.AvailabilityEnd, SetBy: by}
	}
	// Appear away, out of office and a status that appears away turn an
	// online person into an away one. Someone offline stays offline.
	if v.presence == model.PresenceOnline && notify.AppearsAway(a, now) {
		v.presence = model.PresenceAway
	}
	v.dndState = notify.DND(a, now)
	v.dnd = v.dndState.On
	return v
}

func same(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}

// connected is the part of the live hub that lists the connected accounts
// someone may be told about, and sends one event to many of them at once.
// live.Hub has it.
type connected interface {
	ConnectedAccounts(keep func(model.AccountID) bool) []model.AccountID
	SendAll(to []model.AccountID, v any)
}

// audience is who is told about an account: the accounts that share a
// conversation with it and have a connection open. Nobody else can be told
// anything, and on a large server nearly everyone shares a conversation
// with nearly everyone, so listing all of those from the database for each
// change, to find most of them away, cost as much as the server has
// members. The members of the account's conversations come from the rosters
// the store keeps in memory, and only connected accounts are looked at.
//
// A hub that cannot list its connected accounts is given everyone who
// shares a conversation, as before.
func (s *Service) audience(ctx context.Context, account model.AccountID) []model.AccountID {
	hub, ok := s.hub.(connected)
	if !ok {
		return s.everySharer(ctx, account)
	}
	convs, err := s.store.Members().ConversationIDs(ctx, account)
	if err != nil {
		s.log.Error("could not read the conversations of an account; nobody was told of a change", "err", err)
		return nil
	}
	rosters := make([]*store.Roster, 0, len(convs))
	for _, c := range convs {
		r, err := s.store.Members().Roster(ctx, c)
		if err != nil {
			s.log.Error("could not read who shares a conversation with an account; they were not told of a change", "err", err)
			return nil
		}
		rosters = append(rosters, r)
	}
	// The largest first: most people are found in it at the first look.
	sort.Slice(rosters, func(i, j int) bool { return rosters[i].Len() > rosters[j].Len() })
	return hub.ConnectedAccounts(func(id model.AccountID) bool {
		if id == account {
			return false
		}
		for _, r := range rosters {
			if r.Has(id) {
				return true
			}
		}
		return false
	})
}

// everySharer is everyone who shares a conversation with an account,
// connected or not.
func (s *Service) everySharer(ctx context.Context, account model.AccountID) []model.AccountID {
	ids, err := s.store.Accounts().SharedWith(ctx, account)
	if err != nil {
		s.log.Error("could not read who shares a conversation with an account; they were not told of a change", "err", err)
		return nil
	}
	return ids
}

// tell sends an account event to the accounts that can see the account and
// to its own connections. own, if not nil, holds extra fields for the
// account's own connections only.
func (s *Service) tell(ctx context.Context, account model.AccountID, eventType string, fields, own map[string]any) {
	e := s.event(eventType, fields)
	if all, ok := s.hub.(connected); ok {
		// Encoded once for everyone, not once for each.
		all.SendAll(s.audience(ctx, account), e)
	} else {
		for _, to := range s.audience(ctx, account) {
			s.hub.Send(to, e)
		}
	}
	if own != nil {
		mine := make(map[string]any, len(fields)+len(own))
		for k, v := range fields {
			mine[k] = v
		}
		for k, v := range own {
			mine[k] = v
		}
		e.Payload = mine
	}
	s.hub.Send(account, e)
}

// presenceSender is the part of the live hub that takes one presence change
// for many accounts at once, so it can merge and delay presence when a great
// many people's changes at the same time (ruling R62). live.Hub has it; a
// hub without it is sent the event account by account, as before.
type presenceSender interface {
	SendPresence(to []model.AccountID, about model.AccountID, v any)
}

// tellPresence sends presence.changed about an account to the accounts that
// can see it and to its own connections: the same people tell reaches.
func (s *Service) tellPresence(ctx context.Context, account model.AccountID, presence model.Presence) {
	fields := map[string]any{"account_id": account, "presence": presence, "in_call": s.inCall(account)}
	ps, ok := s.hub.(presenceSender)
	if !ok {
		s.tell(ctx, account, model.EventPresenceChanged, fields, nil)
		return
	}
	ps.SendPresence(append(s.audience(ctx, account), account), account, s.event(model.EventPresenceChanged, fields))
}

// announce compares what is now true of an account with what was last
// announced and sends an event for each thing that changed:
// presence.changed, status.changed, availability.changed and dnd.changed
// (docs/EVENTS.md section 9). before is the view from just before a change
// the caller made; it is used when nothing has been announced about the
// account yet. With neither, the view is only recorded.
func (s *Service) announce(ctx context.Context, a model.Account, before *view) {
	cur := s.viewOf(a, s.now())
	s.viewsMu.Lock()
	prev, known := s.views[a.ID]
	s.views[a.ID] = cur
	s.viewsMu.Unlock()
	if !known {
		if before == nil {
			return
		}
		prev = *before
	}
	id := a.ID
	if prev.presence != cur.presence {
		s.tellPresence(ctx, id, cur.presence)
	}
	if !same(prev.status, cur.status) {
		f := map[string]any{"account_id": id}
		if cur.status != nil {
			f["status"] = cur.status
		}
		s.tell(ctx, id, model.EventStatusChanged, f, nil)
	}
	if !same(prev.availability, cur.availability) {
		f := map[string]any{"account_id": id}
		if cur.availability != nil {
			f["availability"] = cur.availability
		}
		s.tell(ctx, id, model.EventAvailabilityChanged, f, nil)
	}
	switch {
	case prev.dnd != cur.dnd:
		// Other people are told the mark and never why.
		s.tell(ctx, id, model.EventDndChanged, map[string]any{"account_id": id, "dnd": cur.dnd}, map[string]any{"dnd_state": cur.dndState})
	case !same(prev.dndState, cur.dndState):
		// The detail changed while the mark stayed: own connections only.
		s.hub.Send(id, s.event(model.EventDndChanged, map[string]any{"account_id": id, "dnd": cur.dnd, "dnd_state": cur.dndState}))
	}
}

// PresenceChanged is called by the live hub when what the connections say
// about an account changes: it connected, disconnected, became active or
// went idle. Others are told what they are now shown.
func (s *Service) PresenceChanged(account model.AccountID) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	a, err := s.store.Accounts().Get(ctx, account)
	if err != nil {
		return
	}
	// With nothing announced yet, the account was last seen the other
	// way: someone connecting was offline, and the reverse.
	before := s.viewOf(a, s.now())
	if before.presence == model.PresenceOffline {
		before.presence = model.PresenceOnline
	} else {
		before.presence = model.PresenceOffline
	}
	s.announce(ctx, a, &before)
}

// tellMe sends me.updated to the account's own connections: something only
// it sees changed.
func (s *Service) tellMe(ctx context.Context, a model.Account) {
	me, err := s.Me(ctx, a)
	if err != nil {
		s.log.Error("me.updated could not be built", "err", err)
		return
	}
	s.hub.Send(a.ID, s.event(model.EventMeUpdated, map[string]any{"account": me}))
}

// tellProfile sends account.updated to the accounts that can see the
// account and to its own connections.
func (s *Service) tellProfile(ctx context.Context, a model.Account) {
	shown := s.show(a, true)
	groups, err := s.store.Groups().OfAccount(ctx, a.ID)
	if err == nil {
		shown.GroupIDs = groups
	}
	s.tell(ctx, a.ID, model.EventAccountUpdated, map[string]any{"account": shown}, nil)
	s.profileChanged(ctx, a.ID)
}

// Sweep looks at every account whose state can change with the clock alone
// (a status, an availability, appear away or a manual do not disturb with
// an end, quiet hours) and announces what changed since the last look: a
// status that ran out, quiet hours starting or ending. Columns whose time
// has passed are cleared, which nothing depends on (docs/SCHEMA.md,
// accounts). Run calls it; so can a test.
func (s *Service) Sweep(ctx context.Context) error {
	list, err := s.store.Accounts().Timed(ctx)
	if err != nil {
		return err
	}
	now := s.now()
	for _, a := range list {
		expired := (!a.StatusUntil.IsZero() && !a.StatusUntil.After(now)) ||
			(!a.AvailabilityEnd.IsZero() && !a.AvailabilityEnd.After(now)) ||
			(a.AwayUntil != nil && !a.AwayUntil.IsZero() && !a.AwayUntil.After(now)) ||
			(a.DNDUntil != nil && !a.DNDUntil.IsZero() && !a.DNDUntil.After(now)) ||
			(!a.QuietSkipUntil.IsZero() && !a.QuietSkipUntil.After(now))
		if expired {
			a, err = s.store.Accounts().Update(ctx, a.ID, func(a *model.Account) error {
				clearExpired(a, now)
				return nil
			})
			if err != nil {
				return err
			}
		}
		// What was last announced is compared with what is true now. An
		// account seen for the first time is only recorded.
		s.announce(ctx, a, nil)
	}
	return nil
}

// clearExpired clears the columns whose time has passed.
func clearExpired(a *model.Account, now model.Time) {
	if !a.StatusUntil.IsZero() && !a.StatusUntil.After(now) {
		a.StatusEmoji, a.StatusText, a.StatusUntil, a.StatusDND, a.StatusAway = "", "", 0, false, false
	}
	if !a.AvailabilityEnd.IsZero() && !a.AvailabilityEnd.After(now) {
		a.AvailabilityIs, a.AvailabilityEnd, a.AvailabilityBy = "", 0, ""
		a.PrefsJSON = setPref(a.PrefsJSON, "meeting_pause_skip_until", nil)
	}
	if a.AwayUntil != nil && !a.AwayUntil.IsZero() && !a.AwayUntil.After(now) {
		a.AwayUntil = nil
	}
	if a.DNDUntil != nil && !a.DNDUntil.IsZero() && !a.DNDUntil.After(now) {
		a.DNDUntil = nil
	}
	if !a.QuietSkipUntil.IsZero() && !a.QuietSkipUntil.After(now) {
		a.QuietSkipUntil = 0
	}
}

// Run sweeps until ctx ends. The server starts it.
func (s *Service) Run(ctx context.Context, every time.Duration) {
	if every <= 0 {
		every = 20 * time.Second
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := s.Sweep(ctx); err != nil && ctx.Err() == nil {
				s.log.Error("the sweep of timed account state failed", "err", err)
			}
		}
	}
}

// setPref returns prefs with one key set, or removed when value is nil.
// Keys the server does not know are kept as they are: clients may keep
// their own there.
func setPref(prefs json.RawMessage, key string, value any) json.RawMessage {
	m := map[string]json.RawMessage{}
	json.Unmarshal(prefs, &m)
	if value == nil {
		delete(m, key)
	} else {
		b, err := json.Marshal(value)
		if err != nil {
			return prefs
		}
		m[key] = b
	}
	out, err := json.Marshal(m)
	if err != nil {
		return prefs
	}
	return out
}
