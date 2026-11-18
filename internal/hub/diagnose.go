package hub

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// Diagnose writes down what the hub holds that a test waiting in vain would
// want to see: the servers connected, each channel's record version, key
// epoch, log cursor and counters of each origin, the direct messages kept
// for a holder not connected, what it has put off of each neighbour's
// batches, and its holder's own state (link.Service.Diagnose). It is for the
// failure output of tests and costs nothing until called; the result is at
// most max bytes.
func (h *Hub) Diagnose(ctx context.Context, max int) string {
	var b strings.Builder
	w := func(format string, a ...any) {
		if b.Len() < max {
			fmt.Fprintf(&b, format, a...)
		}
	}
	h.mu.Lock()
	var conns []string
	for name, c := range h.conns {
		if c.holder {
			name += " (holder)"
		}
		conns = append(conns, name)
	}
	forHolder := len(h.forHolder)
	h.mu.Unlock()
	sort.Strings(conns)
	w("connected: %v; direct messages kept for the holder: %d\n", conns, forHolder)
	if chans, err := h.Channels(); err != nil {
		w("channels: %v\n", err)
	} else {
		for _, c := range chans {
			w("channel %s %q: home %s version %d epoch %d cursor %d servers %v invited %v; log holds %v\n",
				c.ID, c.Name, c.Home, c.Version, c.Epoch, c.Cursor, c.Servers, c.Invited, h.haveOf(c.ID))
		}
	}
	for _, tc := range h.liveTrunks() {
		tc.mu.Lock()
		var ids []string
		for id := range tc.putOffs {
			ids = append(ids, id)
		}
		w("trunk %s: put off %d batches in a row, for streams %v; inflight %d\n", tc.name, tc.putOff, ids, tc.inflight)
		tc.mu.Unlock()
	}
	if l := h.Holder(); l != nil && b.Len() < max {
		w("holder:\n%s", l.Diagnose(ctx, max-b.Len()))
	}
	out := b.String()
	if len(out) > max {
		out = out[:max] + "\n[cut]\n"
	}
	return out
}
