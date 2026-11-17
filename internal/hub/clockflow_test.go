package hub_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/link/wire"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/testkit"
)

func abs(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

// A server's clock against a home hub two trunks away is the sum of three
// measurements, and its uncertainty the sum of theirs. Skew on every clock
// and delay on the trunks, the same both ways on one and one way only on
// the other.
func TestClockComposition(t *testing.T) {
	t.Cleanup(wire.SetPingEvery(150 * time.Millisecond))
	c := testkit.NewHubs(t, 3)
	a, b, cc := c.Hubs[0], c.Hubs[1], c.Hubs[2]
	// Hub b's clock is 3 s fast, hub c's 2 s slow; a server on c is 700 ms
	// fast. Against hub a, the home, the server is 700 ms fast.
	b.Skew(3 * time.Second)
	cc.Skew(-2 * time.Second)
	// 40 ms each way between a and b; 60 ms from b to c and nothing back.
	a.Hub.TestTrunkDelay(b.Name, 40*time.Millisecond)
	b.Hub.TestTrunkDelay(a.Name, 40*time.Millisecond)
	cc.Hub.TestTrunkDelay(b.Name, 60*time.Millisecond)
	c.Join(1, 0)
	c.Join(2, 1)

	a1 := newSim(t, a, "a1.sim")
	c1 := newSimSkewed(t, cc, "c1.sim", 700*time.Millisecond)
	// Each hub's clock against the home's, give or take what it says.
	for _, want := range []struct {
		h      *testkit.TestHub
		offset int64
		floor  int64 // the least honest uncertainty: half the round trips on the way
	}{{b, 3000, 40}, {cc, -2000, 40 + 30}} {
		want := want
		testkit.Eventually(t, want.h.Name+" knows its clock against the home hub's", func() bool {
			off, unc, ok := want.h.Hub.TestHubClock(a.Name)
			return ok && abs(off-want.offset) <= unc && unc >= want.floor && unc < 500
		})
	}
	// The one-way delay moved the measured offset of that trunk by about
	// half of it: the error the protocol accepts, and inside the bound.
	off, unc, _ := cc.Hub.TestHubClock(a.Name)
	t.Logf("hub c reads its clock against hub a's as %+d ms ± %d (true: -2000)", off, unc)

	id := shared(t, "timed", a1, c1)
	// What the server was told for this home is its true offset from the
	// home hub, within the composed uncertainty.
	testkit.Eventually(t, "the server is told its clock against the home hub", func() bool {
		c1.mu.Lock()
		defer c1.mu.Unlock()
		hc, ok := c1.homes[a.Name]
		return ok && abs(hc.Offset-700) <= hc.Uncertainty && hc.Uncertainty >= 70 && hc.Uncertainty < 600
	})
	c1.mu.Lock()
	told, edge := c1.homes[a.Name], *c1.clock
	c1.mu.Unlock()
	t.Logf("the server is told %+d ms ± %d against the home hub (true: +700), %+d ms ± %d against its own hub (true: +2700)",
		told.Offset, told.Uncertainty, edge.Offset, edge.Uncertainty)
	if abs(edge.Offset-2700) > edge.Uncertainty+5 {
		t.Fatalf("against its own hub the server was told %+d ± %d, true +2700", edge.Offset, edge.Uncertainty)
	}

	// An event stamped with the composed figure is taken at the edge and at
	// every hub on the way, and its time is the home hub's.
	// The home hub's clock is this process's own, read just before and just
	// after the stamp is made: the stamp is held against the moment it was
	// made at, and not against one after the hubs have answered, which on a
	// busy machine is as much later as the answer takes.
	before := int64(model.Now())
	env := c1.seal(id, 0, "on time")
	after := int64(model.Now())
	if r := c1.submit(env); r.Code != "" {
		t.Fatalf("stamped by the home hub's clock: %s %s", r.Code, r.Message)
	}
	if early, late := before-env.At, env.At-after; early > told.Uncertainty+50 || late > told.Uncertainty+50 {
		t.Fatalf("the stamp is %d ms from the home hub's clock", max(early, late))
	}
	n := a1.count(id)
	testkit.Eventually(t, "it reaches the home side", func() bool { return a1.count(id) == n+1 })

	// Stamped with the offset against its own hub instead, 2 s out, it is
	// refused by its own hub, which says what it holds.
	c1.mu.Lock()
	c1.n[id]++
	seq := c1.n[id]
	last := c1.lastAt[id]
	c1.mu.Unlock()
	wrong, _ := wire.SealEnvelope(streamKey, id, env.KeyEpoch, c1.name, model.NewID[string](),
		wire.Stamp{At: last + 1, N: seq, Offset: edge.Offset}, []byte(`{"type":"x"}`), c1.keys)
	r := c1.submit(wrong)
	if r.Code != wire.EClockOffset || r.Clock == nil || abs(r.Clock.Offset-700) > r.Clock.Uncertainty {
		t.Fatalf("stamped with the wrong hub's offset: %q, clock %+v", r.Code, r.Clock)
	}
	// A minute ahead of the home hub's clock, or a minute behind it while
	// connected, is refused at the edge.
	for code, shift := range map[string]int64{wire.EClockAhead: 60000, wire.EClockBehind: -60000} {
		fresh := newSimSkewed(t, cc, "x"+code[6:]+".sim", 0)
		testkit.Eventually(t, "invited", func() bool {
			return a1.act(id, wire.ActInvite, func(m *wire.ChannelAction) { m.Server = fresh.name }) == "" && has(fresh.record(id).Invited, fresh.name)
		})
		if code := fresh.join(id); code != "" {
			t.Fatal(code)
		}
		fresh.in(id)
		var out wire.Frame
		testkit.Eventually(t, "the epoch its joining called for has begun", func() bool {
			e := fresh.seal(id, int64(model.Now())+shift, "")
			out = fresh.submit(e)
			if out.Code == wire.EEarlyEpoch || out.Code == wire.EStaleEpoch {
				fresh.mu.Lock()
				fresh.n[id]--
				fresh.lastAt[id] = 0
				fresh.epoch[id] = fresh.records[id].Epoch
				fresh.mu.Unlock()
				return false
			}
			return true
		})
		if out.Code != code {
			t.Fatalf("an event %d ms from the home hub's clock was answered %q, want %s", shift, out.Code, code)
		}
	}
}

// A burst over a trunk: envelopes travel in batches of several streams, at
// most eight batches are unanswered at once, the sender waits when the
// other hub is slow, and everything arrives once and in order.
func TestBatchingAndFlowControl(t *testing.T) {
	c := testkit.NewCluster(t, "line", 2)
	a, b := c.Hubs[0], c.Hubs[1]
	if err := a.Hub.SetLimits("", 1000000, 0, 0); err != nil {
		t.Fatal(err)
	}
	a.Hub.Reconcile()
	b1 := newSim(t, b, "b1.sim")
	var senders []*sim
	var streams []string
	for i := 0; i < 4; i++ {
		s := newSim(t, a, fmt.Sprintf("a%d.sim", i))
		senders = append(senders, s)
		streams = append(streams, shared(t, fmt.Sprintf("burst-%d", i), s, b1))
	}
	// Setting a channel up ends with each sim saying something, and shared
	// returns when the sim's own hub has taken that, not when it has crossed
	// the trunk. What b1 holds is counted once the last of it is there:
	// counted a moment sooner, as on two processors, the owner's words for
	// the last channel arrived during the burst and were taken for one
	// envelope too many of it.
	early := 0
	for _, id := range streams {
		early += b1.count(id)
	}
	testkit.Eventually(t, "what was said while the channels were set up has reached b1", func() bool {
		for i, id := range streams {
			s := senders[i]
			s.mu.Lock()
			said := s.n[id]
			s.mu.Unlock()
			there := false
			b1.mu.Lock()
			for _, env := range b1.got[id] {
				there = there || (env.Origin == s.name && env.N == said)
			}
			b1.mu.Unlock()
			if !there {
				return false
			}
		}
		return true
	})
	base := 0
	for _, id := range streams {
		base += b1.count(id)
	}
	if early != base {
		t.Logf("b1 held %d envelopes when the channels were set up and %d once all of that had arrived", early, base)
	}
	// Hub b takes a second and a half over every batch until the sender has
	// had to wait once, and 20 ms after that. A fixed delay made this test
	// depend on how fast the machine signs and checks envelopes: under the
	// race detector the window filled but never overflowed.
	b.Hub.TestRelayHold(1500 * time.Millisecond)
	before := a.Hub.TrunkStats(b.Name)
	go func() {
		for i := 0; i < 600 && a.Hub.TrunkStats(b.Name).Waits == before.Waits; i++ {
			time.Sleep(50 * time.Millisecond)
		}
		b.Hub.TestRelayHold(20 * time.Millisecond)
	}()
	const each = 1200 // four senders: more than the window holds at 256 a batch, however they are grouped
	done := make(chan string, len(senders))
	for i, s := range senders {
		go func(s *sim, id string) {
			for sent := 0; sent < each; {
				var batch []wire.Envelope
				for k := 0; k < 50; k++ {
					batch = append(batch, s.seal(id, 0, ""))
				}
				if r := s.submit(batch...); r.Code != "" {
					done <- r.Code + " " + r.Message
					return
				}
				sent += len(batch)
			}
			done <- ""
		}(s, streams[i])
	}
	for range senders {
		if msg := <-done; msg != "" {
			t.Fatal(msg)
		}
	}
	total := func() int {
		n := 0
		for _, id := range streams {
			n += b1.count(id)
		}
		return n
	}
	testkit.Eventually(t, "the burst arrives", func() bool { return total() >= base+each*len(senders) })
	time.Sleep(200 * time.Millisecond)
	if got := total(); got != base+each*len(senders) {
		t.Fatalf("%d arrived, want %d", got-base, each*len(senders))
	}
	for _, id := range streams {
		last := int64(0)
		b1.mu.Lock()
		for _, env := range b1.got[id] {
			if env.Origin != b1.name && env.N <= last {
				t.Fatalf("out of order or doubled: %s/%d after %d", env.Origin, env.N, last)
			}
			last = env.N
		}
		b1.mu.Unlock()
	}
	s := a.Hub.TrunkStats(b.Name)
	batches, envelopes := s.Batches-before.Batches, s.Envelopes-before.Envelopes
	t.Logf("%d envelopes in %d batches (largest %d), at most %d unanswered at once, the sender waited %d times",
		envelopes, batches, s.MaxBatch, s.MaxInflight, s.Waits-before.Waits)
	if envelopes != int64(each*len(senders)) {
		t.Fatalf("the trunk carried %d envelopes, want %d", envelopes, each*len(senders))
	}
	if batches >= envelopes/4 || s.MaxBatch < 16 {
		t.Fatalf("no batching to speak of: %d envelopes in %d batches, the largest %d", envelopes, batches, s.MaxBatch)
	}
	if s.MaxBatch > 256 {
		t.Fatalf("a batch of %d", s.MaxBatch)
	}
	if s.MaxInflight > 8 {
		t.Fatalf("%d batches were unanswered at once; the bound is 8", s.MaxInflight)
	}
	if s.Waits == before.Waits {
		t.Fatal("the sender never had to wait for the slow hub: flow control was not exercised")
	}
}
