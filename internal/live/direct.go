package live

import (
	"errors"

	"github.com/tikaydaes/churr/internal/model"
)

// This file is what call signalling needs of the hub beyond fan-out: frames
// addressed to one connection, answers a handler writes itself, and word of
// a connection closing (docs/CALLS.md sections 10 and 11).

// ErrAnswered is returned by a ClientHandler that has sent its own answer
// to the frame, as call signalling does with `call.ok`, `call.error` and
// `call.answer`. The hub then sends neither `ack` nor `error`.
var ErrAnswered = errors.New("live: the handler answered the frame itself")

// SendTo sends one frame to one connection of an account. It reports false
// when that connection is no longer open.
func (h *Hub) SendTo(account model.AccountID, connectionID string, v any) bool {
	for _, c := range h.connsOf(account) {
		if c.id == connectionID {
			c.send(v)
			return true
		}
	}
	return false
}

// SendExcept sends one frame to every connection of an account but one: the
// other devices of someone who just did something on this one.
func (h *Hub) SendExcept(account model.AccountID, exceptConnectionID string, v any) {
	for _, c := range h.connsOf(account) {
		if c.id != exceptConnectionID {
			c.send(v)
		}
	}
}

// OnClose registers a function called after a connection has closed and
// been removed from the hub, whatever closed it. Call signalling uses it to
// start the thirty seconds a participant is held for. Functions are called
// in the order registered, on the connection's own goroutine, and must not
// block for long.
func (h *Hub) OnClose(fn func(Client)) {
	h.mu.Lock()
	h.onClose = append(h.onClose, fn)
	h.mu.Unlock()
}

// connClosed tells the registered functions that c has gone.
func (h *Hub) connClosed(c *conn) {
	h.mu.RLock()
	fns := h.onClose
	h.mu.RUnlock()
	for _, fn := range fns {
		fn(Client{ConnectionID: c.id, Session: c.session, Account: c.account})
	}
}
