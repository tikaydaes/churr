package calls

import (
	"context"
	"time"

	"github.com/tikaydaes/churr/internal/model"
)

// Why a ring stopped (docs/CALLS.md section 15).
const (
	RingJoinedElsewhere   = "joined_elsewhere"
	RingDeclinedElsewhere = "declined_elsewhere"
	RingEnded             = "ended"
	RingTimeout           = "timeout"
)

// Ring is one account being rung for one call. It is what the push package
// needs to know to wake that account's phones (docs/PUSH.md section 2): the
// decision to ring, do not disturb included, has been made already.
type Ring struct {
	CallID         model.CallID
	ConversationID model.ConversationID
	// AccountID is who is being rung; FromAccountID who started the call.
	AccountID     model.AccountID
	FromAccountID model.AccountID
	// Until is when the ring gives up.
	Until model.Time
}

// RingStop is a ring ending, for whatever reason.
type RingStop struct {
	CallID         model.CallID
	ConversationID model.ConversationID
	AccountID      model.AccountID
	Reason         string
}

// ring is the state of one ring. It is in memory only.
type ring struct {
	Ring
	timer *time.Timer
}

// OnRing registers a function called once for each account a starting call
// rings, after `call.ring_started` has gone to its connected clients. The
// push package registers here to wake phones. The function is called on its
// own goroutine.
func (s *Server) OnRing(fn func(Ring)) {
	s.mu.Lock()
	s.onRing = append(s.onRing, fn)
	s.mu.Unlock()
}

// OnRingStopped registers a function called when a ring ends: the person
// joined or declined, the call ended, or forty-five seconds passed.
func (s *Server) OnRingStopped(fn func(RingStop)) {
	s.mu.Lock()
	s.onRingStop = append(s.onRingStop, fn)
	s.mu.Unlock()
}

// Ringing returns the call that is ringing for an account right now, the
// newest if there are several, or "". The status endpoint reports it.
func (s *Server) Ringing(account model.AccountID) model.CallID {
	if r, ok := s.RingingFor(account); ok {
		return r.CallID
	}
	return ""
}

// RingingFor is Ringing with the whole ring, for the wake-up fetch.
func (s *Server) RingingFor(account model.AccountID) (Ring, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	list := s.ringing[account]
	if len(list) == 0 {
		return Ring{}, false
	}
	return list[len(list)-1].Ring, true
}

// ring starts the ring of a call that has just started: every account the
// one rule of docs/FEATURES.md decision F7 names is sent
// `call.ring_started` on every connection, and the ring runs until the
// person joins or declines, the call ends, or the ring time passes. Joining
// a call that is already going never rings anyone.
func (s *Server) ring(ctx context.Context, lc *liveCall, call model.Call) {
	if s.rings == nil {
		return
	}
	ids, err := s.rings.Rings(ctx, call)
	if err != nil {
		s.log.Error("who to ring could not be worked out; nobody was rung", "call", lc.id, "err", err)
		return
	}
	if len(ids) == 0 {
		return
	}
	// People on other servers are rung by their own server, from its own
	// copy of their mode and do not disturb.
	accounts, err := s.store.Accounts().GetMany(ctx, ids)
	if err != nil {
		s.log.Error("who to ring could not be read; nobody was rung", "call", lc.id, "err", err)
		return
	}
	local := map[model.AccountID]bool{}
	for _, a := range accounts {
		local[a.ID] = a.Local()
	}
	until := model.Now().Add(s.t.Ring)
	for _, id := range ids {
		if !local[id] || id == call.StartedByID {
			continue
		}
		r := &ring{Ring: Ring{CallID: lc.id, ConversationID: lc.conv, AccountID: id, FromAccountID: call.StartedByID, Until: until}}
		s.mu.Lock()
		if s.byID[lc.id] != lc {
			s.mu.Unlock()
			return // the call ended meanwhile
		}
		r.timer = time.AfterFunc(s.t.Ring, func() { s.stopRings(r.CallID, r.AccountID, RingTimeout, "") })
		s.ringing[id] = append(s.ringing[id], r)
		hooks := s.onRing
		s.mu.Unlock()
		s.hub.Send(id, model.Event{Type: EventRingStarted, At: model.Now(), ConversationID: lc.conv,
			Payload: map[string]any{"call_id": lc.id, "from_account_id": call.StartedByID, "until": until}})
		for _, fn := range hooks {
			go fn(r.Ring)
		}
	}
}

// stopRings ends the ring of a call for one account, or for everyone it was
// ringing when account is "". The account's clients get `call.ring_stopped`
// with the reason; exceptConn is the connection that caused it by joining
// or declining, which needs no telling. It reports whether a ring ended.
func (s *Server) stopRings(call model.CallID, account model.AccountID, reason, exceptConn string) bool {
	var stopped []*ring
	s.mu.Lock()
	for id, list := range s.ringing {
		if account != "" && id != account {
			continue
		}
		keep := list[:0]
		for _, r := range list {
			if r.CallID == call {
				stopped = append(stopped, r)
			} else {
				keep = append(keep, r)
			}
		}
		if len(keep) == 0 {
			delete(s.ringing, id)
		} else {
			s.ringing[id] = keep
		}
	}
	hooks := s.onRingStop
	s.mu.Unlock()
	for _, r := range stopped {
		r.timer.Stop()
		e := model.Event{Type: EventRingStopped, At: model.Now(), ConversationID: r.ConversationID,
			Payload: map[string]any{"call_id": r.CallID, "reason": reason}}
		if exceptConn != "" {
			s.hub.SendExcept(r.AccountID, exceptConn, e)
		} else {
			s.hub.Send(r.AccountID, e)
		}
		for _, fn := range hooks {
			go fn(RingStop{CallID: r.CallID, ConversationID: r.ConversationID, AccountID: r.AccountID, Reason: reason})
		}
	}
	return len(stopped) > 0
}

// decline is `call.decline` from someone being rung: their other devices
// stop ringing, and the call's participants are told.
func (s *Server) decline(lc *liveCall, account model.AccountID, conn string) bool {
	if !s.stopRings(lc.id, account, RingDeclinedElsewhere, conn) {
		return false
	}
	if lc.room != nil {
		lc.room.broadcast(event(EventDeclined, lc, map[string]any{"account_id": account}), "")
	}
	return true
}
