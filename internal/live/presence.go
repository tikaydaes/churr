package live

import (
	"sync"
	"time"

	"github.com/tikaydaes/churr/internal/model"
)

// presenceWatch remembers what the connections last said about each account
// that is not offline, so a change can be told once.
type presenceWatch struct {
	mu   sync.Mutex
	last map[model.AccountID]model.Presence
	fn   func(account model.AccountID)
	done chan struct{}
}

// OnPresence sets who is told when what the connections say about an
// account changes: it connected, its last connection went, a client
// reported activity after none had for five minutes, or five minutes passed
// without any. The listener (the people service) works out what others are
// shown and sends presence.changed. It is set once while the server is
// being put together.
//
// Connecting, disconnecting and activity are noticed as they happen. Going
// idle is the absence of something, so a watcher looks every
// Options.PresenceEvery.
func (h *Hub) OnPresence(fn func(account model.AccountID)) {
	h.pres.mu.Lock()
	first := h.pres.done == nil
	h.pres.fn = fn
	if first {
		h.pres.done = make(chan struct{})
		h.pres.last = map[model.AccountID]model.Presence{}
	}
	done := h.pres.done
	h.pres.mu.Unlock()
	if first {
		go h.watchPresence(done)
	}
}

func (h *Hub) watchPresence(done chan struct{}) {
	t := time.NewTicker(h.opts.PresenceEvery)
	defer t.Stop()
	for {
		select {
		case <-done:
			return
		case <-t.C:
		}
		// Everyone connected, and everyone who was a moment ago.
		seen := map[model.AccountID]struct{}{}
		h.mu.RLock()
		for id := range h.conns {
			seen[id] = struct{}{}
		}
		h.mu.RUnlock()
		h.pres.mu.Lock()
		for id := range h.pres.last {
			seen[id] = struct{}{}
		}
		h.pres.mu.Unlock()
		for id := range seen {
			h.presenceCheck(id)
		}
	}
}

// presenceCheck compares an account's presence with what was last told and
// tells the listener if it changed.
func (h *Hub) presenceCheck(account model.AccountID) {
	h.mu.RLock()
	closed := h.closed
	h.mu.RUnlock()
	if closed {
		// Every connection is about to go; that is the server stopping,
		// not people leaving.
		return
	}
	cur := h.Presence(account)
	h.pres.mu.Lock()
	if h.pres.last == nil {
		h.pres.mu.Unlock()
		return // nobody is listening
	}
	prev, known := h.pres.last[account]
	if !known {
		prev = model.PresenceOffline
	}
	if prev == cur {
		h.pres.mu.Unlock()
		return
	}
	if cur == model.PresenceOffline {
		delete(h.pres.last, account)
	} else {
		h.pres.last[account] = cur
	}
	fn := h.pres.fn
	h.pres.mu.Unlock()
	if fn != nil {
		fn(account)
	}
}

// stopPresence ends the watcher, when the hub shuts down.
func (h *Hub) stopPresence() {
	h.pres.mu.Lock()
	if h.pres.done != nil {
		select {
		case <-h.pres.done:
		default:
			close(h.pres.done)
		}
	}
	h.pres.mu.Unlock()
}
