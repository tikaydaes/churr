package live

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/tikaydaes/churr/internal/model"
)

// Presence by interest.
//
// Told to everyone who may see an account, one person coming online costs a
// frame on every connection, so a server's members connecting cost the
// square of their number: five thousand connecting sent 1.3 GB of presence,
// and fifty thousand would send a hundred times that, again whenever
// connections drop and come back. Nobody looks at fifty thousand people at
// once. So a connection says which accounts it is showing
// (`presence.subscribe`, docs/EVENTS.md section 4), and a change in
// someone's presence goes to the connections showing them, the ones that
// "can currently see them" of ruling R62, and to nobody else. What is sent
// then follows what is on screens, which grows with the number of
// connections and not with its square.
//
// A connection that has never subscribed is a client from before this. It
// is told about the people the caller names as close to the account (those
// it shares a small conversation with), which is what such a client shows
// most of the time, and not about the whole server.
//
// Who may see whom is not decided here. The people service honours a
// subscription only for accounts the subscriber may see, and is asked again
// when a change is sent.

// MaxPresenceInterest is how many accounts one connection may watch.
const MaxPresenceInterest = 500

// interest is who is watching whom.
type interest struct {
	mu sync.RWMutex
	// by is, for each account, the connections showing it.
	by map[model.AccountID]map[*conn]struct{}
	// of is what each connection that has subscribed is showing. A
	// connection missing from it has never subscribed.
	of map[*conn]map[model.AccountID]struct{}
}

// SetPresenceInterest replaces the accounts one connection is showing with
// ids, which the caller has already cut down to those the account may see.
// It returns the ones that were not in the set before, whose presence the
// connection does not have yet, and false if the connection has gone.
func (h *Hub) SetPresenceInterest(account model.AccountID, connectionID string, ids []model.AccountID) ([]model.AccountID, bool) {
	var c *conn
	for _, x := range h.connsOf(account) {
		if x.id == connectionID {
			c = x
		}
	}
	if c == nil {
		return nil, false
	}
	if len(ids) > MaxPresenceInterest {
		ids = ids[:MaxPresenceInterest]
	}
	in := &h.interest
	in.mu.Lock()
	defer in.mu.Unlock()
	// A connection that closed while this was on its way is not put
	// back: remove has run, or will find nothing of it here.
	c.mu.Lock()
	gone := c.closed
	c.mu.Unlock()
	if gone {
		return nil, false
	}
	if in.by == nil {
		in.by, in.of = map[model.AccountID]map[*conn]struct{}{}, map[*conn]map[model.AccountID]struct{}{}
	}
	old := in.of[c]
	now := make(map[model.AccountID]struct{}, len(ids))
	var added []model.AccountID
	for _, id := range ids {
		if _, twice := now[id]; twice {
			continue
		}
		now[id] = struct{}{}
		if _, had := old[id]; had {
			continue
		}
		added = append(added, id)
		set := in.by[id]
		if set == nil {
			set = map[*conn]struct{}{}
			in.by[id] = set
		}
		set[c] = struct{}{}
	}
	for id := range old {
		if _, kept := now[id]; !kept {
			in.dropLocked(id, c)
		}
	}
	in.of[c] = now
	return added, true
}

func (in *interest) dropLocked(id model.AccountID, c *conn) {
	if set := in.by[id]; set != nil {
		delete(set, c)
		if len(set) == 0 {
			delete(in.by, id)
		}
	}
}

// forget drops a connection that has closed.
func (in *interest) forget(c *conn) {
	in.mu.Lock()
	for id := range in.of[c] {
		in.dropLocked(id, c)
	}
	delete(in.of, c)
	in.mu.Unlock()
}

// SendPresenceToInterested sends one presence.changed event about an
// account to the connections that should hear it: the account's own, those
// showing it whose account maySee allows, and the connections of the
// accounts in near that have never said what they show. v is the event, as
// for SendPresence, and it is merged and held back under pressure in the
// same way. maySee is called with the hub's interest lock held and must
// answer from memory; nil allows everyone.
func (h *Hub) SendPresenceToInterested(about model.AccountID, v any, near []model.AccountID, maySee func(model.AccountID) bool) {
	b, err := json.Marshal(v)
	if err != nil {
		h.opts.Log.Error("a presence event could not be encoded", "err", err)
		return
	}
	in := &h.interest
	var conns []*conn
	// The account's own connections always hear what others are shown.
	h.mu.RLock()
	for c := range h.conns[about] {
		conns = append(conns, c)
	}
	h.mu.RUnlock()
	in.mu.RLock()
	for c := range in.by[about] {
		if c.account.ID != about && (maySee == nil || maySee(c.account.ID)) {
			conns = append(conns, c)
		}
	}
	// Clients from before subscriptions: found among the few accounts
	// near this one, never by walking every connection.
	if len(near) > 0 {
		h.mu.RLock()
		for i, id := range near {
			again := id == about
			for _, earlier := range near[:i] {
				again = again || earlier == id
			}
			if again {
				continue
			}
			for c := range h.conns[id] {
				if _, subscribed := in.of[c]; !subscribed {
					conns = append(conns, c)
				}
			}
		}
		h.mu.RUnlock()
	}
	in.mu.RUnlock()
	h.presStats.changes.Add(1)
	h.deliverPresence(conns, &presenceItem{about: about, frame: b})
}

// deliverPresence hands one presence change to connections, at once or to
// wait with others, as the load asks (coalesce.go).
func (h *Hub) deliverPresence(conns []*conn, item *presenceItem) {
	if len(conns) == 0 {
		return
	}
	q := &h.presQueue
	q.mu.Lock()
	defer q.mu.Unlock()
	now := time.Now()
	// The wait is read before this change is counted, so a change that
	// comes alone is never held back by its own size.
	wait := h.presenceWait(now)
	h.load.add(now, len(conns))
	h.presStats.deliveries.Add(int64(len(conns)))
	var earliest time.Time
	for _, c := range conns {
		if due, queued := c.sendPresence(item, now, wait); queued {
			q.waiting = append(q.waiting, c)
			if earliest.IsZero() || due.Before(earliest) {
				earliest = due
			}
		}
	}
	if !earliest.IsZero() {
		h.armPresenceLocked(now, earliest)
	}
}
