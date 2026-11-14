package live

import (
	"encoding/json"

	"github.com/tikaydaes/churr/internal/model"
)

// SendToSession sends one frame to the connections of one session of an
// account: an event meant for one device, such as the answer to its unlock
// request (docs/EVENTS.md section 12).
func (h *Hub) SendToSession(account model.AccountID, session model.SessionID, v any) {
	h.sendWhere(account, v, func(c *conn) bool { return c.session.ID == session })
}

// SendExceptSession sends one frame to every connection of an account but
// those of one session: an event for the account's other devices.
func (h *Hub) SendExceptSession(account model.AccountID, session model.SessionID, v any) {
	h.sendWhere(account, v, func(c *conn) bool { return c.session.ID != session })
}

func (h *Hub) sendWhere(account model.AccountID, v any, want func(c *conn) bool) {
	conns := h.connsOf(account)
	if len(conns) == 0 {
		return
	}
	b, err := json.Marshal(v)
	if err != nil {
		h.opts.Log.Error("an account event could not be encoded", "err", err)
		return
	}
	for _, c := range conns {
		if want(c) {
			c.sendEvent("", 0, b)
		}
	}
}
