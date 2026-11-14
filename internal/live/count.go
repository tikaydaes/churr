package live

import "github.com/tikaydaes/churr/internal/model"

// Connections reports how many live connections an account holds right now.
// A bot may hold only so many at once (docs/BOTS.md section 8); whoever
// opens a connection asks before it does.
func (h *Hub) Connections(account model.AccountID) int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.conns[account])
}
