package live

import "encoding/json"

// Broadcast sends one frame to every connection, for the events that go to
// everyone whoever they are, such as server.updated (docs/EVENTS.md section
// 9). v is anything that marshals to a JSON object with `type` and `at`.
func (h *Hub) Broadcast(v any) {
	b, err := json.Marshal(v)
	if err != nil {
		h.opts.Log.Error("an event for everyone could not be encoded", "err", err)
		return
	}
	h.mu.RLock()
	var all []*conn
	for _, set := range h.conns {
		for c := range set {
			all = append(all, c)
		}
	}
	h.mu.RUnlock()
	for _, c := range all {
		c.sendEvent("", 0, b)
	}
	h.count(kindOf(v), len(all), len(all)*len(b))
}
