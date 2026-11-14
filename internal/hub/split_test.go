package hub_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/link/wire"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/testkit"
)

// noDuplicates fails if a hub's log of a stream holds an event twice, or an
// origin's envelopes out of the order it made them.
func noDuplicates(t *testing.T, th *testkit.TestHub, id string) map[string]int64 {
	t.Helper()
	seen := map[string]bool{}
	last := map[string]int64{}
	for _, env := range th.Hub.TestLog(id) {
		if seen[env.OriginEventID] {
			t.Fatalf("%s holds %s/%d twice", th.Name, env.Origin, env.N)
		}
		seen[env.OriginEventID] = true
		if env.N <= last[env.Origin] {
			t.Fatalf("%s holds %s/%d after %d", th.Name, env.Origin, env.N, last[env.Origin])
		}
		last[env.Origin] = env.N
	}
	return last
}

// A split, as IRC has them: both sides go on among their own servers, the
// channel's record changes only on the home hub's side, and on rejoin each
// side is sent what it missed, by per-origin counters, once.
func TestSplitAndRejoin(t *testing.T) {
	c := testkit.NewCluster(t, "line", 3)
	a1, b1 := newSim(t, c.Hubs[0], "a1.sim"), newSim(t, c.Hubs[1], "b1.sim")
	c1, c2 := newSim(t, c.Hubs[2], "c1.sim"), newSim(t, c.Hubs[2], "c2.sim")
	id := shared(t, "split", a1, b1, c1, c2)
	other := a1.create("other", wire.ListingOpen)
	testkit.Eventually(t, "the listed channel is known on hub c", func() bool {
		for _, ch := range mustChannels(t, c.Hubs[2]) {
			if ch.ID == other {
				return true
			}
		}
		return false
	})
	settle := func(s *sim, n int) {
		t.Helper()
		testkit.Eventually(t, fmt.Sprintf("%s has %d", s.name, n), func() bool { return s.count(id) >= n })
	}
	base := map[*sim]int{}
	for _, s := range []*sim{a1, b1, c1, c2} {
		settle(s, 3) // the three posts of the others that followed the joins
		time.Sleep(50 * time.Millisecond)
		base[s] = s.count(id)
	}

	c.Cut(1, 2) // a - b | c: the home hub a is on b's side

	for i := 0; i < 5; i++ {
		for _, s := range []*sim{a1, b1, c1, c2} {
			if code := s.post(id); code != "" {
				t.Fatalf("%s could not post during the split: %s", s.name, code)
			}
		}
	}
	// Each side delivers among its own: a1 and b1 have each other's five,
	// c1 and c2 each other's, and nobody anything from across.
	settle(a1, base[a1]+5)
	settle(b1, base[b1]+5)
	settle(c1, base[c1]+5)
	settle(c2, base[c2]+5)
	time.Sleep(200 * time.Millisecond)
	for s, n := range map[*sim]int{a1: base[a1] + 5, b1: base[b1] + 5, c1: base[c1] + 5, c2: base[c2] + 5} {
		if got := s.count(id); got != n {
			t.Fatalf("%s has %d during the split, want %d", s.name, got, n)
		}
	}
	// Changes to the record: only where the home hub can be reached.
	text := "changed during the split"
	for name, code := range map[string]string{
		"describe":      c1.act(id, wire.ModDescribe, func(m *wire.ModAction) { m.Description = &text }),
		"set_listing":   c1.act(id, wire.ModSetListing, func(m *wire.ModAction) { m.Listing = wire.ListingOpen }),
		"invite":        c1.act(id, wire.ModInvite, func(m *wire.ModAction) { m.Server = "a1.sim" }),
		"add_moderator": c1.act(id, wire.ModAddModerator, func(m *wire.ModAction) { mod := c2.me(); m.Moderator = &mod }),
		"remove_server": c1.act(id, wire.ModRemoveServer, func(m *wire.ModAction) { m.Server = "c2.sim" }),
		"join":          c2.join(other),
	} {
		if code != wire.EUnavailable {
			t.Fatalf("%s on the far side of the split was answered %q, not unavailable", name, code)
		}
	}
	if code := b1.act(id, wire.ModDescribe, func(m *wire.ModAction) { m.Description = &text }); code != wire.EForbidden {
		t.Fatalf("an account that is no moderator, on the home side, was answered %q", code) // it reached the home hub, which said no for its own reason
	}
	if code := a1.act(id, wire.ModDescribe, func(m *wire.ModAction) { m.Description = &text }); code != "" {
		t.Fatalf("the moderator on the home side could not change the record: %s", code)
	}
	testkit.Eventually(t, "the home side sees the change", func() bool { return b1.record(id).Description == text })
	if c1.record(id).Description == text {
		t.Fatal("the far side saw a change made across the split")
	}
	before := c.Hubs[2].Hub.TestStored(id)

	c.Restore(1, 2)

	// Everyone ends with everything: twenty envelopes of the split, fifteen
	// of them from the others.
	for _, s := range []*sim{a1, b1, c1, c2} {
		settle(s, base[s]+15)
	}
	time.Sleep(200 * time.Millisecond)
	for _, s := range []*sim{a1, b1, c1, c2} {
		if got := s.count(id); got != base[s]+15 {
			t.Fatalf("%s has %d after the rejoin, want %d: something came twice", s.name, got, base[s]+15)
		}
		seen := map[string]bool{}
		for _, key := range s.received(id) {
			if seen[key] {
				t.Fatalf("%s was delivered %s twice", s.name, key)
			}
			seen[key] = true
		}
	}
	// The three logs hold the same last counter of every origin, and none
	// holds anything twice.
	want := noDuplicates(t, c.Hubs[0], id)
	for _, h := range c.Hubs[1:] {
		got := noDuplicates(t, h, id)
		if fmt.Sprint(got) != fmt.Sprint(want) {
			t.Fatalf("%s ends at %v, %s at %v", h.Name, got, c.Hubs[0].Name, want)
		}
	}
	if after := c.Hubs[2].Hub.TestStored(id); after != before+10 {
		t.Fatalf("hub c took %d envelopes at the rejoin, want the 10 of the other side", after-before)
	}
	// The record's change reaches the far side now, and the far side can
	// change it again.
	testkit.Eventually(t, "the far side sees the change", func() bool { return c1.record(id).Description == text })
	if code := c2.join(other); code != "" {
		t.Fatalf("after the rejoin a far server could not join: %s", code)
	}
	// Catching up is by per-origin counters, across hubs as on one: a
	// server that says it holds nothing is sent the log of its own hub, the
	// other three origins' envelopes, each origin's in order.
	c1.mu.Lock()
	c1.got[id] = nil
	c1.mu.Unlock()
	r, ok := c1.catchUp(id, map[string]int64{"a1.sim": want["a1.sim"] - 2})
	if !ok || r.Frame != wire.FCaughtUp || r.Have["a1.sim"] != want["a1.sim"] || r.Have["b1.sim"] != want["b1.sim"] {
		t.Fatalf("caught_up: %+v", r)
	}
	fromA := 0
	for _, key := range c1.received(id) {
		if strings.HasPrefix(key, "a1.sim/") {
			fromA++
		}
		if strings.HasPrefix(key, "c1.sim/") {
			t.Fatal("a server was sent its own envelope back")
		}
	}
	if fromA != 2 {
		t.Fatalf("asked for what follows a1's counter %d, the server was sent %d of a1's", want["a1.sim"]-2, fromA)
	}
}

// What the far side may stamp during a split, and what the hubs on the home
// side make of it afterwards. An envelope older than the checks on time allow
// is taken at the rejoin without a word if its time falls inside the stretch
// the trunk was down (the hub next to the break knows that stretch itself;
// the home hub further on takes that hub's word for it).
//
// And an envelope its edge hub should never have taken, stamped before the
// split began or far ahead, is taken too, by every hub, and reaches every
// server: the edge hub's word on an envelope's time stands across the
// cluster, because its own servers already hold the event and nothing takes
// it back. Each hub that would have judged otherwise counts it for its
// operator. (Before this, hub b refused both, and they stayed on hub c's side
// only: the same channel, different on different servers.)
func TestHomeChecksTheDisconnectionWindow(t *testing.T) {
	t.Cleanup(wire.SetPingEvery(200 * time.Millisecond))
	c := testkit.NewCluster(t, "line", 3)
	a1 := newSim(t, c.Hubs[0], "a1.sim")
	c1, c2, c3 := newSim(t, c.Hubs[2], "c1.sim"), newSim(t, c.Hubs[2], "c2.sim"), newSim(t, c.Hubs[2], "c3.sim")
	id := a1.create("window", wire.ListingUnlisted)
	// c2 and c3 join and say nothing yet: their first envelopes may carry
	// any time, as far as "later than its last" goes.
	for _, s := range []*sim{c1, c2, c3} {
		s := s
		testkit.Eventually(t, "invited", func() bool {
			return a1.act(id, wire.ModInvite, func(m *wire.ModAction) { m.Server = s.name }) == "" && has(s.record(id).Invited, s.name)
		})
		if code := s.join(id); code != "" {
			t.Fatal(code)
		}
		s.in(id)
	}
	if code := c1.post(id); code != "" {
		t.Fatal(code)
	}
	testkit.Eventually(t, "the channel is live across the cluster", func() bool { return a1.count(id) >= 1 })
	start := a1.count(id)

	c.Cut(1, 2)
	// During the split c1 posts, honestly: the time is now.
	if code := c1.say(id, "honest"); code != "" {
		t.Fatal(code)
	}
	// Ten minutes pass on every hub's clock while the trunk is down.
	for _, h := range c.Hubs {
		h.Skew(10 * time.Minute)
	}
	// Hub c stops checking times: a hub that does not do its duty. Through
	// it c2 stamps an event forty minutes back, before the split began, and
	// c3 one twenty minutes ahead.
	c.Hubs[2].Hub.TestSkipStampChecks(true)
	now := int64(model.Now()) + int64(10*time.Minute/time.Millisecond)
	back := c2.seal(id, now-int64(40*time.Minute/time.Millisecond), "backdated")
	ahead := c3.seal(id, now+int64(20*time.Minute/time.Millisecond), "postdated")
	if r := c2.submit(back); r.Code != "" {
		t.Fatalf("the careless hub refused: %s", r.Code)
	}
	if r := c3.submit(ahead); r.Code != "" {
		t.Fatalf("the careless hub refused: %s", r.Code)
	}

	c.Restore(1, 2)

	// The honest envelope is ten minutes old when it reaches hubs b and a:
	// far older than thirty seconds. It is taken, because its time is
	// inside the stretch the trunk was down. So are the two a careful edge
	// would have refused: every server of the channel ends with all three.
	testkit.Eventually(t, "all three envelopes of the split reach the home side", func() bool { return a1.count(id) >= start+3 })
	time.Sleep(300 * time.Millisecond)
	for i, h := range c.Hubs {
		held := map[string]bool{}
		for _, env := range h.Hub.TestLog(id) {
			held[env.OriginEventID] = true
		}
		if !held[back.OriginEventID] || !held[ahead.OriginEventID] {
			t.Fatalf("%s does not hold what the edge hub took: backdated %v, postdated %v", testkit.HubName(i), held[back.OriginEventID], held[ahead.OriginEventID])
		}
	}
	if got := a1.count(id); got != start+3 {
		t.Fatalf("the home side's server was delivered %d envelopes of the split, want 3", got-start)
	}
	// Nothing was refused and nobody was told of a refusal; the two are
	// counted as suspect by each hub that would not have taken them itself,
	// and the honest one by none.
	for _, pair := range [][2]int{{1, 2}, {0, 1}} {
		s := c.Hubs[pair[0]].Hub.TrunkStats(testkit.HubName(pair[1]))
		if s.Refused != 0 || s.Suspect != 2 {
			t.Fatalf("%s refused %d and suspected %d envelopes from %s, want 0 and 2", testkit.HubName(pair[0]), s.Refused, s.Suspect, testkit.HubName(pair[1]))
		}
	}
	if s := c.Hubs[2].Hub.TrunkStats(testkit.HubName(1)); s.RefusedThere != 0 {
		t.Fatalf("hub c heard of %d refusals, want none", s.RefusedThere)
	}
	for _, s := range []*sim{c2, c3} {
		s.mu.Lock()
		for _, e := range s.errs {
			if e.StreamID == id && (e.Code == wire.EClockBehind || e.Code == wire.EClockAhead) {
				t.Errorf("%s was told %s for an event its hub had accepted", s.name, e.Code)
			}
		}
		s.mu.Unlock()
	}
	// The hub does its duty again, and what an origin says next is taken
	// everywhere as before.
	c.Hubs[2].Hub.TestSkipStampChecks(false)
	before := a1.count(id)
	// (Its hub measures its clock again first: every hub's clock has just
	// jumped ten minutes, as far as the server can tell.)
	testkit.Eventually(t, "c2 is measured again and posts", func() bool { return c2.post(id) == "" })
	for i := 0; i < 2; i++ {
		if code := c2.post(id); code != "" {
			t.Fatalf("after its backdated event, c2: %s", code)
		}
	}
	testkit.Eventually(t, "what c2 says next reaches the home side", func() bool { return a1.count(id) == before+3 })
	for _, h := range c.Hubs {
		noDuplicates(t, h, id)
	}
}

// The home hub goes away. Its channels are read-and-post only among the
// servers that can still reach each other until it returns; nothing takes
// its place. When it returns it is sent what it missed.
func TestHomeHubFailureAndRestart(t *testing.T) {
	c := testkit.NewCluster(t, "line", 3)
	a1 := newSim(t, c.Hubs[0], "a1.sim")
	b1, c1 := newSim(t, c.Hubs[1], "b1.sim"), newSim(t, c.Hubs[2], "c1.sim")
	id := shared(t, "orphan", a1, b1, c1)
	testkit.Eventually(t, "settled", func() bool { return b1.count(id) >= 2 && c1.count(id) >= 2 })
	time.Sleep(100 * time.Millisecond)
	b0, c0 := b1.count(id), c1.count(id)
	stored := c.Hubs[0].Hub.TestStored(id)

	c.Hubs[0].Stop() // the home hub is gone

	for i := 0; i < 4; i++ {
		if code := b1.post(id); code != "" {
			t.Fatalf("b1 could not post with the home hub gone: %s", code)
		}
		if code := c1.post(id); code != "" {
			t.Fatalf("c1 could not post with the home hub gone: %s", code)
		}
	}
	testkit.Eventually(t, "the reachable servers go on", func() bool { return b1.count(id) == b0+4 && c1.count(id) == c0+4 })
	text := "nobody can say"
	if code := b1.act(id, wire.ModDescribe, func(m *wire.ModAction) { m.Description = &text }); code != wire.EUnavailable {
		t.Fatalf("a change with the home hub gone was answered %q, not unavailable", code)
	}
	// Nobody has taken the channel over.
	for _, h := range c.Hubs[1:] {
		for _, ch := range mustChannels(t, h) {
			if ch.ID == id && ch.Home != testkit.HubName(0) {
				t.Fatalf("%s now says the channel's home is %s", h.Name, ch.Home)
			}
		}
	}

	c.Hubs[0].Start()
	c.Whole()
	testkit.Eventually(t, "the home hub is sent what it missed", func() bool { return c.Hubs[0].Hub.TestStored(id) == stored+8 })
	noDuplicates(t, c.Hubs[0], id)
	// Its own server reconnects and catches up from it.
	a1.reconnect()
	testkit.Eventually(t, "a1 holds its record again", func() bool { return a1.record(id).Has("a1.sim") })
	a1.mu.Lock()
	a1.got[id] = nil
	a1.mu.Unlock()
	if r, ok := a1.catchUp(id, map[string]int64{}); !ok || r.Frame != wire.FCaughtUp {
		t.Fatalf("catch-up from the restarted home hub: %+v", r)
	}
	testkit.Eventually(t, "a1 has the eight", func() bool { return a1.count(id) >= 8 })
	// And the channel can be changed again.
	testkit.Eventually(t, "the record changes again", func() bool {
		return a1.act(id, wire.ModDescribe, func(m *wire.ModAction) { m.Description = &text }) == "" && c1.record(id).Description == text
	})
	// A hub in the middle restarts too: it dials again, and nothing is
	// lost or doubled.
	c.Hubs[1].Restart()
	c.Whole()
	b1.reconnect()
	for i := 0; i < 3; i++ {
		if code := c1.post(id); code != "" {
			t.Fatal(code)
		}
	}
	testkit.Eventually(t, "the home hub has the three sent through the restarted hub", func() bool { return c.Hubs[0].Hub.TestStored(id) == stored+11 })
	for _, h := range c.Hubs {
		noDuplicates(t, h, id)
	}
}

// The operator moves a channel's home to another hub when both are up, and
// only then.
func TestRehome(t *testing.T) {
	c := testkit.NewCluster(t, "line", 3)
	a1, c1 := newSim(t, c.Hubs[0], "a1.sim"), newSim(t, c.Hubs[2], "c1.sim")
	id := shared(t, "moving", a1, c1)
	to := testkit.HubName(2)

	// Not while the other hub cannot be reached.
	c.Cut(1, 2)
	if err := c.Hubs[0].Hub.Rehome(id, to); err != nil {
		t.Fatal(err)
	}
	c.Hubs[0].Hub.Reconcile()
	if got := c.Hubs[0].Hub.RehomeResult(id); !strings.HasPrefix(got, "failed:") || !strings.Contains(got, "cannot be reached") {
		t.Fatalf("a move to a hub that cannot be reached: %q", got)
	}
	if rec := a1.record(id); rec.HomeHub != testkit.HubName(0) {
		t.Fatalf("the home is %s", rec.HomeHub)
	}
	c.Restore(1, 2)
	// Not from a hub that is not the home, and not to a hub outside.
	if err := c.Hubs[1].Hub.Rehome(id, to); err == nil {
		t.Fatal("a hub that is not the home moved it")
	}
	if err := c.Hubs[0].Hub.Rehome(id, "elsewhere.localhost"); err == nil {
		t.Fatal("a home was moved to a hub outside the cluster")
	}

	if err := c.Hubs[0].Hub.Rehome(id, to); err != nil {
		t.Fatal(err)
	}
	c.Hubs[0].Hub.Reconcile()
	// (The old home steps down when the new home's answer is in its hands.)
	testkit.Eventually(t, "the move is done", func() bool { return c.Hubs[0].Hub.RehomeResult(id) == "done" })
	for _, s := range []*sim{a1, c1} {
		s := s
		testkit.Eventually(t, s.name+" holds a record signed by the new home", func() bool {
			rec := s.record(id)
			return rec.HomeHub == to && rec.Hub == to
		})
	}
	for i, h := range c.Hubs {
		i, h := i, h
		testkit.Eventually(t, fmt.Sprintf("hub %d knows the new home", i), func() bool {
			for _, ch := range mustChannels(t, h) {
				if ch.ID == id {
					return ch.Home == to
				}
			}
			return i == 1 // the hub between holds the record only while it carries the channel
		})
	}
	// The new home owns it: a moderator's action is answered there, and the
	// old home's operator can no longer change it.
	text := "under new management"
	testkit.Eventually(t, "the record changes through the new home", func() bool {
		return a1.act(id, wire.ModDescribe, func(m *wire.ModAction) { m.Description = &text }) == "" && c1.record(id).Description == text
	})
	if rec := c1.record(id); rec.Hub != to {
		t.Fatalf("the change was signed by %s", rec.Hub)
	}
	if err := c.Hubs[0].Hub.Expel(id, "c1.sim"); err == nil {
		t.Fatal("the old home's operator changed a channel that is no longer its own")
	}
	// Messages go on, stamped by the new home's clock.
	n := c1.count(id)
	for i := 0; i < 3; i++ {
		if code := a1.post(id); code != "" {
			t.Fatalf("after the move: %s", code)
		}
	}
	testkit.Eventually(t, "c1 has them", func() bool { return c1.count(id) == n+3 })
}

// A hub leaves the cluster, and a hub is removed from it.
func TestClusterLeaveAndRemove(t *testing.T) {
	c := testkit.NewCluster(t, "star", 3)
	a, b, cc := c.Hubs[0], c.Hubs[1], c.Hubs[2]
	b1 := newSim(t, b, "b1.sim")
	a1 := newSim(t, a, "a1.sim")
	id := shared(t, "leaving", a1, b1)
	if err := a.Hub.ClusterLeave(); err == nil {
		t.Fatal("the hub the others joined through left")
	}
	if err := cc.Hub.ClusterLeave(); err != nil {
		t.Fatal(err)
	}
	cc.Hub.Reconcile()
	testkit.Eventually(t, "hub c stands alone and the others know", func() bool {
		return fmt.Sprint(cc.Hub.TestMembers()) == "[hub-c.localhost]" && fmt.Sprint(a.Hub.TestMembers()) == "[hub-a.localhost hub-b.localhost]" &&
			fmt.Sprint(b.Hub.TestMembers()) == "[hub-a.localhost hub-b.localhost]"
	})
	// Hub b is removed by the hub it joined through. Its server goes out of
	// the channel hub a is the home of.
	if err := b.Hub.ClusterRemove(a.Name); err == nil {
		t.Fatal("a hub removed the hub it joined through")
	}
	if err := a.Hub.ClusterRemove(b.Name); err != nil {
		t.Fatal(err)
	}
	a.Hub.Reconcile()
	testkit.Eventually(t, "hub b is out", func() bool {
		return fmt.Sprint(a.Hub.TestMembers()) == "[hub-a.localhost]" && fmt.Sprint(b.Hub.TestMembers()) == "[hub-b.localhost]"
	})
	testkit.Eventually(t, "its server is out of the channel", func() bool {
		a.Hub.Reconcile()
		return !a1.record(id).Has("b1.sim")
	})
	// It can join again, as a new admission.
	inv, err := a.Hub.TrunkInvite(b.Name, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Hub.TrunkJoin(inv); err != nil {
		t.Fatal(err)
	}
	b.Hub.Reconcile()
	testkit.Eventually(t, "hub b is in again", func() bool {
		return fmt.Sprint(a.Hub.TestMembers()) == "[hub-a.localhost hub-b.localhost]" && fmt.Sprint(b.Hub.TestMembers()) == "[hub-a.localhost hub-b.localhost]" && c.Up(0, 1)
	})
	if len(a.Hub.TestClusterEntries()) != 4 {
		t.Fatalf("the roster holds %d entries; want the founder's, two that are over and the new one", len(a.Hub.TestClusterEntries()))
	}
}
