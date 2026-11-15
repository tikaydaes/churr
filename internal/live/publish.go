package live

import (
	"context"
	"encoding/json"

	"github.com/tikaydaes/churr/internal/model"
)

// The fan-out of one conversation event.
//
// An event in a channel of fifty thousand people goes to the connections of
// those of them who are connected, and what it costs must follow that
// number, not the fifty thousand. So the members come from the roster the
// store keeps in memory; the connections are found under one hold of the
// hub's lock, walking whichever is smaller, the roster or the connected
// accounts; and the frame is encoded once for each form the event takes.
// A message.created has three forms (`notify` is none, badge or notify), so
// ten thousand readers share three byte slices and none is built for one
// reader alone.

// target is the connections of one recipient: conns[from:to] of the
// fan-out's list.
type target struct {
	account  model.AccountID
	from, to int
}

// form is one encoding of an event: the frame for every reader whose only
// difference from the plain event is one top-level field with this value.
type form struct {
	key, value string
	frame      []byte
}

func (h *Hub) publish(ctx context.Context, e model.Event) {
	norm, base, err := e.Normalize()
	if err != nil {
		h.opts.Log.Error("an event could not be encoded", "type", e.Type, "err", err)
		return
	}
	roster, err := h.perm.Roster(ctx, e.ConversationID)
	if err != nil {
		// The event is in the log; clients catch up from there.
		h.opts.Log.Error("recipients could not be read; the event was not fanned out", "type", e.Type, "err", err)
		return
	}

	var (
		targets []target
		conns   []*conn
	)
	take := func(id model.AccountID, set map[*conn]struct{}) {
		if len(set) == 0 {
			return
		}
		from := len(conns)
		for c := range set {
			conns = append(conns, c)
		}
		targets = append(targets, target{id, from, len(conns)})
	}
	h.mu.RLock()
	tailor := h.tailor
	if roster.Len() <= len(h.conns) {
		for _, id := range roster.IDs {
			take(id, h.conns[id])
		}
	} else {
		for id, set := range h.conns {
			if roster.Has(id) {
				take(id, set)
			}
		}
	}
	for i, id := range e.AlsoTo {
		again := roster.Has(id)
		for _, earlier := range e.AlsoTo[:i] {
			again = again || earlier == id
		}
		if !again {
			take(id, h.conns[id])
		}
	}
	h.mu.RUnlock()

	var (
		fields map[string]json.RawMessage // base decoded, made only if someone needs changes
		forms  []form
	)
	sent, sentBytes := 0, 0
	defer func() { h.count(e.Type, sent, sentBytes) }()
	for _, t := range targets {
		var extra map[string]any
		if tailor != nil {
			extra = tailor.Tailor(ctx, norm, t.account, true)
		}
		frame := base
		if only := e.Only[t.account]; len(extra) > 0 || len(only) > 0 {
			// The usual difference is one field with one of a few
			// values: those readers share one encoding.
			key, value, shared := "", "", false
			if len(only) == 0 && len(extra) == 1 {
				for k, v := range extra {
					key = k
					value, shared = v.(string)
				}
			}
			frame = nil
			if shared {
				for _, f := range forms {
					if f.key == key && f.value == value {
						frame = f.frame
						break
					}
				}
			}
			if frame == nil {
				if fields == nil {
					if err := json.Unmarshal(base, &fields); err != nil {
						h.opts.Log.Error("an event could not be re-read", "type", e.Type, "err", err)
						return
					}
				}
				frame = splice(fields, extra, only)
				if shared {
					forms = append(forms, form{key, value, frame})
				}
			}
		}
		for _, c := range conns[t.from:t.to] {
			c.sendEvent(e.ConversationID, e.Seq, frame)
		}
		sent += t.to - t.from
		sentBytes += (t.to - t.from) * len(frame)
	}
}

// ConnectedAccounts returns the accounts that have a connection open and
// that keep says yes to. Whoever tells people about an account asks for the
// connected ones among those who may know, in place of listing everyone who
// may know and finding most of them away. keep is called with the hub's
// lock held: it must answer from memory.
func (h *Hub) ConnectedAccounts(keep func(model.AccountID) bool) []model.AccountID {
	h.mu.RLock()
	defer h.mu.RUnlock()
	var out []model.AccountID
	for id := range h.conns {
		if keep(id) {
			out = append(out, id)
		}
	}
	return out
}

// SendAll is Send to several accounts: the frame is encoded once and every
// connection is found under one hold of the lock. An account named twice is
// sent it twice, so name each once.
func (h *Hub) SendAll(to []model.AccountID, v any) {
	if len(to) == 0 {
		return
	}
	if about, ok := presenceOf(v); ok {
		h.SendPresence(to, about, v)
		return
	}
	var conns []*conn
	h.mu.RLock()
	for _, id := range to {
		for c := range h.conns[id] {
			conns = append(conns, c)
		}
	}
	h.mu.RUnlock()
	if len(conns) == 0 {
		return
	}
	b, err := json.Marshal(v)
	if err != nil {
		h.opts.Log.Error("an account event could not be encoded", "err", err)
		return
	}
	for _, c := range conns {
		c.sendEvent("", 0, b)
	}
	h.count(kindOf(v), len(conns), len(conns)*len(b))
}
