package hub

import (
	"encoding/json"
	"fmt"

	"github.com/tikaydaes/churr/internal/link/wire"
)

// The hub is the time reference of every stream it is the home of (ruling
// R57). It measures the clock of each connected server in the connection's
// ping and pong (wire/clock.go), keeps what it measured in its database so
// the figures outlive a disconnection and a restart, and checks the events
// servers submit against them (order.go).

// setClock stores what was just measured of one server's clock.
func (h *Hub) setClock(name string, c wire.Clock) {
	h.clocks.Store(name, &c)
	if _, err := h.db.Exec(`INSERT INTO clocks (name, clock) VALUES (?, ?) ON CONFLICT (name) DO UPDATE SET clock = excluded.clock`,
		name, mustJSON(c)); err != nil {
		h.log.Warn("a measured clock could not be stored", "server", name, "err", err)
	}
}

// clockOf returns what the hub last measured of a server's clock, or nil if
// it never has.
func (h *Hub) clockOf(name string) *wire.Clock { return h.clockIn(h.db, name) }

// clockIn is clockOf through a transaction. What was measured since the hub
// started is in memory, so that taking an envelope reads the database for a
// clock only once.
func (h *Hub) clockIn(q querier, name string) *wire.Clock {
	if c, ok := h.clocks.Load(name); ok {
		return c.(*wire.Clock)
	}
	var raw []byte
	if q.QueryRow(`SELECT clock FROM clocks WHERE name = ?`, name).Scan(&raw) != nil {
		return nil
	}
	var c wire.Clock
	if json.Unmarshal(raw, &c) != nil {
		return nil
	}
	h.clocks.LoadOrStore(name, &c)
	return &c
}

// ClockText is one server's clock figures as an operator command prints
// them.
func ClockText(c *wire.Clock) string {
	if c == nil {
		return "clock: not measured yet"
	}
	out := fmt.Sprintf("clock: offset %+d ms ± %d, round trip %d ms (jitter %d), to it %d ms (jitter %d), from it %d ms (jitter %d)",
		c.Offset, c.Uncertainty, c.Latency, c.Jitter, c.Forward, c.ForwardJitter, c.Back, c.BackJitter)
	if c.Asymmetry != 0 {
		out += fmt.Sprintf(", path asymmetry about %+d ms", c.Asymmetry)
	}
	if c.Synced {
		out += ", clocks declared synchronised"
	}
	return out
}

// ClockTotals is what the status page shows of the clocks: totals over the
// servers measured, and no server's own figures, since the page names no
// server.
type ClockTotals struct {
	// Synced: the hub was started with -clock-synced.
	Synced bool
	// Measured is how many servers on the hub have a measured clock.
	Measured int
	// The lowest, middle and highest round trip among them, the largest
	// offset either way, the largest uncertainty, the highest jitter in
	// either direction and the largest asymmetry estimate either way, in
	// milliseconds.
	LatencyMin, LatencyMedian, LatencyMax int64
	OffsetMax, UncertaintyMax             int64
	JitterMax, AsymmetryMax               int64
}

func (h *Hub) clockTotals() ClockTotals {
	t := ClockTotals{Synced: h.cfg.ClockSynced}
	rows, err := h.db.Query(`SELECT c.clock FROM clocks c JOIN servers s ON s.name = c.name WHERE s.state = 'active'`)
	if err != nil {
		return t
	}
	defer rows.Close()
	var latencies []int64
	bigger := func(have *int64, v int64) {
		if v < 0 {
			v = -v
		}
		if v > *have {
			*have = v
		}
	}
	for rows.Next() {
		var raw []byte
		var c wire.Clock
		if rows.Scan(&raw) != nil || json.Unmarshal(raw, &c) != nil {
			continue
		}
		t.Measured++
		latencies = append(latencies, c.Latency)
		if t.Measured == 1 || c.Latency < t.LatencyMin {
			t.LatencyMin = c.Latency
		}
		bigger(&t.LatencyMax, c.Latency)
		bigger(&t.OffsetMax, c.Offset)
		bigger(&t.UncertaintyMax, c.Uncertainty)
		bigger(&t.JitterMax, c.ForwardJitter)
		bigger(&t.JitterMax, c.BackJitter)
		bigger(&t.AsymmetryMax, c.Asymmetry)
	}
	t.LatencyMedian = wire.Median(latencies)
	return t
}

// Text is the clock totals as the status page prints them.
func (t ClockTotals) Text() string {
	mode := "measured; a constant difference between the two directions of a path cannot be told from a clock offset"
	if t.Synced {
		mode = "declared synchronised by the operator; offset beyond 10 ms is reported as path asymmetry"
	}
	return fmt.Sprintf("Clocks: %s\nServers with a measured clock: %d\nRound trip: lowest %d ms, middle %d ms, highest %d ms\n"+
		"Largest clock offset: %d ms\nLargest uncertainty: %d ms\nHighest one-way jitter: %d ms\nLargest path asymmetry estimate: %d ms\n",
		mode, t.Measured, t.LatencyMin, t.LatencyMedian, t.LatencyMax, t.OffsetMax, t.UncertaintyMax, t.JitterMax, t.AsymmetryMax)
}
