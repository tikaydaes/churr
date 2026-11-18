package hub_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/hub"
	"github.com/tikaydaes/churr/internal/link/wire"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/secret"
	"github.com/tikaydaes/churr/internal/testkit"
)

// These tests drive hubs joined by trunks with simulated servers (sim_test.go).
// The same things with real servers, on both databases, are in internal/link.

// shared makes a channel on the first sim's hub and brings the others into
// it by invitation, wherever in the cluster they are.
func shared(t *testing.T, name string, sims ...*sim) string {
	t.Helper()
	owner := sims[0]
	id := owner.create(name, wire.ListingUnlisted)
	for _, s := range sims[1:] {
		s := s
		testkit.Eventually(t, "the hub takes the invitation of "+s.name, func() bool {
			code := owner.act(id, wire.ActInvite, func(a *wire.ChannelAction) { a.Server = s.name })
			return code == "" && (has(owner.record(id).Invited, s.name) || owner.record(id).Has(s.name))
		})
		testkit.Eventually(t, "the invitation reaches "+s.name, func() bool { return has(s.record(id).Invited, s.name) || s.record(id).Has(s.name) })
		if code := s.join(id); code != "" {
			t.Fatalf("%s could not join: %s", s.name, code)
		}
		s.in(id)
		owner.in(id)
		testkit.Eventually(t, "the owner sees "+s.name+" in the channel", func() bool { return owner.record(id).Has(s.name) })
	}
	// Every epoch the joins called for has begun before anything is said.
	for _, s := range sims {
		if code := s.post(id); code != "" {
			t.Fatalf("%s could not post after joining: %s", s.name, code)
		}
	}
	return id
}

func has(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func TestClusterForms(t *testing.T) {
	for _, shape := range []string{"line", "star"} {
		t.Run(shape, func(t *testing.T) {
			c := testkit.NewCluster(t, shape, 3)
			for i, h := range c.Hubs {
				if got := fmt.Sprint(h.Hub.TestMembers()); got != "[hub-a.localhost hub-b.localhost hub-c.localhost]" {
					t.Fatalf("hub %d lists %s", i, got)
				}
			}
			// Each hub knows its clock against every other, through the
			// trunks between.
			for i, h := range c.Hubs {
				for j := range c.Hubs {
					j := j
					testkit.Eventually(t, fmt.Sprintf("hub %d knows hub %d's clock", i, j), func() bool {
						off, _, ok := h.Hub.TestHubClock(testkit.HubName(j))
						return ok && off > -50 && off < 50
					})
				}
			}
		})
	}
}

func TestEnvelopesCrossTheCluster(t *testing.T) {
	c := testkit.NewCluster(t, "line", 3)
	a, b, cc := newSim(t, c.Hubs[0], "a.sim"), newSim(t, c.Hubs[1], "b.sim"), newSim(t, c.Hubs[2], "c.sim")
	id := shared(t, "across", a, b, cc)
	for i := 0; i < 5; i++ {
		for _, s := range []*sim{a, b, cc} {
			if code := s.post(id); code != "" {
				t.Fatalf("%s: %s", s.name, code)
			}
		}
	}
	for _, s := range []*sim{a, b, cc} {
		s := s
		testkit.Eventually(t, s.name+" has everything the others said", func() bool { return s.count(id) >= 10 })
	}
	time.Sleep(100 * time.Millisecond)
	// Each origin's envelopes arrive in the order it made them, once.
	for _, s := range []*sim{a, b, cc} {
		last := map[string]int64{}
		s.mu.Lock()
		for _, env := range s.got[id] {
			if env.N <= last[env.Origin] {
				t.Fatalf("%s was delivered %s/%d after %d", s.name, env.Origin, env.N, last[env.Origin])
			}
			last[env.Origin] = env.N
		}
		s.mu.Unlock()
	}
	// Every hub holds everything said since the last server joined: the 15
	// above and the 3 that followed the joins. A hub that came to carry the
	// channel midway holds nothing older than where its servers start.
	for i, h := range c.Hubs {
		i, h := i, h
		testkit.Eventually(t, fmt.Sprintf("hub %d holds the 18", i), func() bool { return h.Hub.TestStored(id) >= 18 })
	}
	if a, last := c.Hubs[0].Hub.TestStored(id), c.Hubs[2].Hub.TestStored(id); last > a {
		t.Fatalf("the hub that came last holds %d, more than the home's %d", last, a)
	}
}

// Nothing is delivered to a hub with no member server behind it for a
// channel: not an envelope, and for an unlisted channel not its record.
// Asserted by what each hub stores and by what crosses each trunk.
func TestNoDeliveryToAHubWithNoMemberBehindIt(t *testing.T) {
	for _, shape := range []string{"line", "star"} {
		t.Run(shape, func(t *testing.T) {
			c := testkit.NewCluster(t, shape, 3)
			// In the line a - b - c the channel's servers are on a and b:
			// c has none behind it. In the star b - a - c they are on a
			// and b too: c has none, and a carries for b.
			a, b := newSim(t, c.Hubs[0], "a.sim"), newSim(t, c.Hubs[1], "b.sim")
			idle := newSim(t, c.Hubs[2], "c.sim")
			type seen struct{ envelopes, records int }
			var mu sync.Mutex
			toC := seen{}
			var id string
			for i, h := range c.Hubs {
				i := i
				h.Hub.TestTrunkTap(func(trunk string, out bool, f wire.Frame) {
					mu.Lock()
					defer mu.Unlock()
					if !out || trunk != testkit.HubName(2) || id == "" {
						return
					}
					_ = i
					for _, it := range f.Items {
						if it.Envelope.StreamID == id {
							toC.envelopes++
						}
					}
					if f.Frame == wire.FChannel {
						var rec wire.ChannelRecord
						if wire.PeekBlock(f, &rec) == nil && rec.StreamID == id {
							toC.records++
						}
					}
					if f.Frame == wire.FEphemeral && f.Envelope != nil && f.Envelope.StreamID == id {
						toC.envelopes++
					}
				})
			}
			made := shared(t, "quiet", a, b)
			mu.Lock()
			id = made
			mu.Unlock()
			for i := 0; i < 10; i++ {
				if code := a.post(id); code != "" {
					t.Fatal(code)
				}
				if code := b.post(id); code != "" {
					t.Fatal(code)
				}
			}
			testkit.Eventually(t, "both servers have the other's ten", func() bool { return a.count(id) >= 10 && b.count(id) >= 10 })
			// The record changes too, and still does not go to c.
			if code := a.act(id, wire.ActDescribe, func(m *wire.ChannelAction) { d := "still nobody's business"; m.Description = &d }); code != "" {
				t.Fatal(code)
			}
			testkit.Eventually(t, "b sees the description", func() bool { return b.record(id).Description == "still nobody's business" })
			time.Sleep(300 * time.Millisecond)
			mu.Lock()
			defer mu.Unlock()
			if toC.envelopes != 0 || toC.records != 0 {
				t.Fatalf("hub c, with no server of the channel behind it, was sent %d envelopes and %d records", toC.envelopes, toC.records)
			}
			if n := c.Hubs[2].Hub.TestStored(id); n != 0 {
				t.Fatalf("hub c stores %d envelopes of a channel none of its servers is in", n)
			}
			for _, ch := range mustChannels(t, c.Hubs[2]) {
				if ch.ID == id {
					t.Fatal("hub c holds the record of an unlisted channel none of its servers is in or invited to")
				}
			}
			if idle.count(id) != 0 || idle.record(id).Version != 0 {
				t.Fatal("a server outside the channel was sent something of it")
			}
			// The hubs that do have servers of it hold it.
			if c.Hubs[0].Hub.TestStored(id) < 20 || c.Hubs[1].Hub.TestStored(id) < 20 {
				t.Fatalf("hubs a and b hold %d and %d", c.Hubs[0].Hub.TestStored(id), c.Hubs[1].Hub.TestStored(id))
			}
		})
	}
}

func mustChannels(t *testing.T, h *testkit.TestHub) []hub.ChannelInfo {
	t.Helper()
	list, err := h.Hub.Channels()
	if err != nil {
		t.Fatal(err)
	}
	return list
}

// A roster with an entry nobody let in is refused whole by every hub, and
// a trunk that would close a loop is refused by both of its ends.
func TestForgedRosterEntryAndLoopRefused(t *testing.T) {
	c := testkit.NewCluster(t, "line", 3)
	a, b, cc := c.Hubs[0], c.Hubs[1], c.Hubs[2]

	// Hub b is a member and sends its neighbours a roster with one more
	// entry: a hub of its own invention, "let in" by hub a, whose
	// countersignature b cannot make. It signs in a's place itself.
	evil, err := secret.Open(filepath.Join(t.TempDir(), "keys"))
	if err != nil {
		t.Fatal(err)
	}
	entries := b.Hub.TestClusterEntries()
	first, _ := entries[0].Member()
	forged, err := wire.SignMember(wire.ClusterMember{Cluster: first.Cluster, Hub: "evil.localhost", SignKey: wire.Bytes(evil.SignPublic()), BoxKey: evil.BoxPublic(),
		Via: a.Name, JoinedAt: model.Now()}, evil)
	if err != nil {
		t.Fatal(err)
	}
	forged = wire.Countersign(forged, b.Hub.TestKeys()) // not a's key
	if _, err := wire.VerifyCluster(append(append([]wire.ClusterEntry{}, entries...), forged)); err == nil {
		t.Fatal("a roster with an entry its via did not countersign verifies")
	}
	b.Hub.TestSendCluster(append(append([]wire.ClusterEntry{}, entries...), forged))
	time.Sleep(300 * time.Millisecond)
	for _, h := range []*testkit.TestHub{a, cc} {
		if has(h.Hub.TestMembers(), "evil.localhost") || len(h.Hub.TestMembers()) != 3 {
			t.Fatalf("%s took a forged entry: %v", h.Name, h.Hub.TestMembers())
		}
	}
	// An entry that names a real member under another key is no better.
	stolen, _ := wire.SignMember(wire.ClusterMember{Cluster: first.Cluster, Hub: cc.Name, SignKey: wire.Bytes(evil.SignPublic()), BoxKey: evil.BoxPublic(), Via: b.Name, JoinedAt: model.Now()}, evil)
	stolen = wire.Countersign(stolen, b.Hub.TestKeys())
	b.Hub.TestSendCluster(append(append([]wire.ClusterEntry{}, entries...), stolen))
	time.Sleep(300 * time.Millisecond)
	for _, e := range a.Hub.TestClusterEntries() {
		if m, _ := e.Member(); m.Hub == cc.Name && string(m.SignKey) == string(evil.SignPublic()) {
			t.Fatal("hub a took an entry that gives a member another key")
		}
	}

	// The loop. c is joined through b, which is joined through a. A trunk
	// from c to a would make a ring. c's own command refuses:
	inv, err := a.Hub.TrunkInvite("", 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := cc.Hub.TrunkJoin(inv); err == nil || !strings.Contains(err.Error(), "loop") {
		t.Fatalf("a hub in the cluster was let to join it again: %v", err)
	}
	// And a refuses the dial of a hub that is in its cluster already, were
	// c to try all the same: a handshake with a good invitation, as c.
	parsed, _ := wire.ParseInvitation(inv)
	_, err = wire.Dial(context.Background(), wire.DialOptions{URL: parsed.URL, InviteSecret: parsed.Secret, HTTPClient: testkit.LoopbackClient(),
		Identity: wire.Identity{Name: cc.Name, Kind: wire.KindHub, Keys: cc.Hub.TestKeys(), URL: "ws://" + cc.Name, Software: "test"}})
	var refused *wire.Error
	if !errors.As(err, &refused) || refused.Code != wire.EConflict {
		t.Fatalf("hub a answered a second trunk from a member with %v, not conflict", err)
	}
	if got := fmt.Sprint(a.Hub.TestMembers()); got != "[hub-a.localhost hub-b.localhost hub-c.localhost]" {
		t.Fatalf("the cluster changed: %s", got)
	}
	c.Whole()
}
