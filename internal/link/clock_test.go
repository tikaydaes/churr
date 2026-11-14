package link_test

import (
	"context"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/link/wire"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/testkit"
)

// peerClock reads one of a peer's clocks from the owner's peer view and
// reports whether it is there.
func peerClock(c *testkit.Client, peer, which string) (map[string]any, bool) {
	p := c.Must(200, "GET", "/api/v1/admin/peers/"+peer, nil)["peer"].(map[string]any)
	clock, ok := p[which].(map[string]any)
	return clock, ok
}

func ms(clock map[string]any, field string) float64 {
	v, _ := clock[field].(float64)
	return v
}

// waitClock waits until a peer's clock shows an offset within tolerance of
// want and returns it.
func waitClock(t *testing.T, c *testkit.Client, peer, which string, want time.Duration) map[string]any {
	t.Helper()
	var got map[string]any
	testkit.Eventually(t, which+" shows an offset near "+want.String(), func() bool {
		clock, ok := peerClock(c, peer, which)
		got = clock
		return ok && ms(clock, "samples") >= 3 && math.Abs(ms(clock, "offset_ms")-float64(want.Milliseconds())) <= 25
	})
	return got
}

// On a direct link each server is the time reference of the streams it owns,
// so each measures the other and tells it. B's clock is seven minutes fast.
func TestClocksOverADirectLink(t *testing.T) {
	const fast = 7 * time.Minute
	l := testkit.NewLinkedSkewed(t, 2, false, 0, fast)
	a, b := l.Servers[0], l.Servers[1]

	// What A measured of B, and what B was told by A, are the same figure:
	// B's clock minus A's.
	measured := waitClock(t, a.Owner, l.Peers[0], "measured_clock", fast)
	told := waitClock(t, b.Owner, l.Peers[1], "told_clock", fast)
	// And the other way round with the other sign.
	waitClock(t, b.Owner, l.Peers[1], "measured_clock", -fast)
	waitClock(t, a.Owner, l.Peers[0], "told_clock", -fast)
	for _, c := range []map[string]any{measured, told} {
		// Loopback: a round trip of a few milliseconds at most, and an
		// uncertainty that is at least half of it and not much more.
		if ms(c, "latency_ms") > 50 || ms(c, "uncertainty_ms") < 1 || ms(c, "uncertainty_ms") > 60 || ms(c, "uncertainty_ms") < ms(c, "latency_ms")/2 {
			t.Errorf("the figures on loopback: %v", c)
		}
		for _, f := range []string{"jitter_ms", "forward_ms", "forward_jitter_ms", "back_ms", "back_jitter_ms", "asymmetry_ms", "measured_at"} {
			if _, has := c[f]; !has {
				t.Errorf("the peer view's clock has no %s: %v", f, c)
			}
		}
		if c["synced"] == true {
			t.Errorf("synced mode is on though nobody asked for it: %v", c)
		}
	}

	// The clock is set while connected. Whichever side is doing the
	// measuring, it sees both one-way times move in opposite senses and
	// follows at once; with the ordinary half-minute between pings that is
	// within half a minute and a few pings more.
	defer wire.SetPingEvery(300 * time.Millisecond)()
	l.Skew(1, -3*time.Minute)
	waitClock(t, a.Owner, l.Peers[0], "measured_clock", -3*time.Minute)
	waitClock(t, b.Owner, l.Peers[1], "told_clock", -3*time.Minute)
	waitClock(t, b.Owner, l.Peers[1], "measured_clock", 3*time.Minute)
	waitClock(t, a.Owner, l.Peers[0], "told_clock", 3*time.Minute)

	// Both figures are kept while disconnected, and are with the peer's row
	// when the server starts again with nothing in memory.
	b.Owner.Must(200, "PATCH", "/api/v1/admin/peers/"+l.Peers[1], map[string]any{"state": "paused"})
	testkit.Eventually(t, "the link is down", func() bool {
		p := a.Owner.Must(200, "GET", "/api/v1/admin/peers/"+l.Peers[0], nil)["peer"].(map[string]any)
		return p["connected_at"] == nil
	})
	for _, which := range []string{"measured_clock", "told_clock"} {
		c, ok := peerClock(a.Owner, l.Peers[0], which)
		if !ok || math.Abs(math.Abs(ms(c, "offset_ms"))-180000) > 25 || ms(c, "uncertainty_ms") < 1 {
			t.Errorf("%s while disconnected: %v", which, c)
		}
	}
	row, err := b.Server.Store.Peers().Get(context.Background(), model.PeerID(l.Peers[1]))
	if err != nil || row.ClockTold == nil || row.ClockMeasured == nil ||
		math.Abs(float64(row.ClockTold.Offset)+180000) > 25 || math.Abs(float64(row.ClockMeasured.Offset)-180000) > 25 {
		t.Errorf("the stored row: %+v %+v (%v)", row.ClockTold, row.ClockMeasured, err)
	}
	b.Server.Link.TestForgetClocks()
	if c, ok := b.Server.Link.TestTold(model.PeerID(l.Peers[1])); !ok || math.Abs(float64(c.Offset)+180000) > 25 {
		t.Errorf("after forgetting what was in memory: %+v %v", c, ok)
	}
}

// Through a hub the hub alone measures. Two servers with different wrong
// clocks are each told their own offset; the operator sees every server's
// figures, the status page only totals.
func TestClocksThroughAHub(t *testing.T) {
	l := testkit.NewLinkedSkewed(t, 2, true, -11*time.Minute, 90*time.Second)
	a, b := l.Servers[0], l.Servers[1]
	waitClock(t, a.Owner, l.Peers[0], "told_clock", -11*time.Minute)
	waitClock(t, b.Owner, l.Peers[1], "told_clock", 90*time.Second)
	if _, has := peerClock(a.Owner, l.Peers[0], "measured_clock"); has {
		t.Error("a server claims to have measured a hub's clock")
	}
	servers, err := l.Hub.Hub.Servers()
	if err != nil || len(servers) != 2 {
		t.Fatal(servers, err)
	}
	for i, want := range []int64{-11 * 60_000, 90_000} {
		c := servers[i].Clock
		if c == nil || c.Offset < want-25 || c.Offset > want+25 || c.Uncertainty < (c.Latency+1)/2 || c.Samples < 3 {
			t.Errorf("the hub's figures for %s: %+v", servers[i].Name, c)
		}
	}
	status := l.Hub.Hub.Status()
	if status.Clocks.Measured != 2 || status.Clocks.OffsetMax < 11*60_000-25 || status.Clocks.Synced {
		t.Errorf("the status totals: %+v", status.Clocks)
	}
	text := status.Text()
	servers2 := strings.ReplaceAll(text, "hub.localhost", "")
	if !strings.Contains(text, "Servers with a measured clock: 2") || strings.Contains(servers2, "a.localhost") || strings.Contains(servers2, "b.localhost") {
		t.Errorf("the status page gives totals and names no server:\n%s", text)
	}
}
