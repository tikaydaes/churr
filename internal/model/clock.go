package model

// PeerClock is what a time reference has measured of one server's clock and
// of the path between the two (docs/LINK-PROTOCOL.md section 5; schema
// PeerClock). The reference is a hub, or the owner server on a direct link.
// All durations are whole milliseconds.
type PeerClock struct {
	// Offset is the measured server's clock minus the reference's, as the
	// reference applies it: the server reads the reference's time as its
	// own clock minus Offset.
	Offset int64 `json:"offset_ms"`
	// Latency is the lowest round trip among the recent samples, and Jitter
	// the mean by which their round trips exceed it.
	Latency int64 `json:"latency_ms"`
	Jitter  int64 `json:"jitter_ms"`
	// Uncertainty bounds the error of Offset. It is never less than half
	// the lowest round trip, unless Synced.
	Uncertainty int64 `json:"uncertainty_ms"`
	// Forward and Back are the lowest one-way times among the recent
	// samples, from the reference to the server and back, with Offset taken
	// out; ForwardJitter and BackJitter are the mean by which each
	// direction's samples exceed its lowest.
	Forward       int64 `json:"forward_ms"`
	ForwardJitter int64 `json:"forward_jitter_ms"`
	Back          int64 `json:"back_ms"`
	BackJitter    int64 `json:"back_jitter_ms"`
	// Asymmetry is an estimate of how much longer the forward direction
	// takes than the back one, where there is anything to base one on; 0
	// when nothing is known.
	Asymmetry int64 `json:"asymmetry_ms"`
	// Synced: the reference's operator has declared the two clocks
	// synchronised, so Offset is held within a small bound and the rest of
	// what was measured is reported as Asymmetry.
	Synced bool `json:"synced,omitempty"`
	// Samples is how many samples the figures rest on.
	Samples int `json:"samples"`
	// MeasuredAt is the reference's clock when the newest sample arrived.
	MeasuredAt Time `json:"measured_at"`
}

// HomeClock is a server's clock against a hub it is not connected to: the
// offset its own hub measured, plus the offsets of the trunks between that
// hub and this one, each with its uncertainty added (docs/LINK-PROTOCOL.md
// section 23b). A channel whose home is that hub is stamped by it.
type HomeClock struct {
	// Offset is the server's clock minus that hub's, in milliseconds.
	Offset int64 `json:"offset_ms"`
	// Uncertainty is the sum of the uncertainties along the way.
	Uncertainty int64 `json:"uncertainty_ms"`
	// MeasuredAt is the oldest of the measurements the figure rests on.
	MeasuredAt Time `json:"measured_at"`
}

// UncertaintyAt is the uncertainty at a later moment: a figure not measured
// for a while is trusted less, as a PeerClock's is.
func (c HomeClock) UncertaintyAt(now Time) int64 {
	return PeerClock{Uncertainty: c.Uncertainty, MeasuredAt: c.MeasuredAt}.UncertaintyAt(now)
}

// clockDriftPPM is how fast two clocks are allowed to drift apart, in parts
// per million: an offset not measured for a while is trusted less by this.
const clockDriftPPM = 50

// UncertaintyAt is the uncertainty at a later moment by the reference's
// clock.
func (c PeerClock) UncertaintyAt(now Time) int64 {
	age := int64(now) - int64(c.MeasuredAt)
	if age < 0 || c.Synced {
		return c.Uncertainty
	}
	return c.Uncertainty + age*clockDriftPPM/1_000_000
}
