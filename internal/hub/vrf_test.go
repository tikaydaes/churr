package hub_test

import (
	"testing"

	"github.com/tikaydaes/churr/internal/link/wire"
	"github.com/tikaydaes/churr/internal/testkit"
)

// A hub channel's record carries the VRF key history of every server it
// has named, on this hub or on another of the cluster, and of the hub that
// signs it; a server taken out of the channel stays in it, so a draw it
// made can still be checked (docs/DESIGN-FAIR.md section 1).
func TestRecordKeepsEveryServersVRFKeysForGood(t *testing.T) {
	c := testkit.NewCluster(t, "line", 2)
	a, b := c.Hubs[0], c.Hubs[1]
	a1, a2 := newSim(t, a, "a1.sim"), newSim(t, a, "a2.sim")
	b1 := newSim(t, b, "b1.sim")
	id := shared(t, "fair", a1, a2, b1)

	same := func(got, want []wire.VRFKey) bool {
		if len(got) != len(want) {
			return false
		}
		for i := range got {
			if string(got[i].Key) != string(want[i].Key) || got[i].Began != want[i].Began {
				return false
			}
		}
		return true
	}
	holds := func(rec wire.ChannelRecord, s *sim) bool { return same(rec.VRFKeys[s.name], s.id.VRFKeys()) }
	testkit.Eventually(t, "the record carries all three servers' VRF keys", func() bool {
		rec := a1.record(id)
		return holds(rec, a1) && holds(rec, a2) && holds(rec, b1)
	})
	rec := a1.record(id)
	if err := wire.CheckVRFKeys(testkit.HubName(0), rec.HubKey, rec.VRFKeys[testkit.HubName(0)]); err != nil {
		t.Fatalf("the record does not carry its hub's own VRF keys: %v", err)
	}
	// Each entry is signed by its server, so a reader needs only the record.
	for _, s := range []*sim{a1, a2, b1} {
		if err := wire.CheckVRFKeys(s.name, s.keys.SignPublic(), rec.VRFKeys[s.name]); err != nil {
			t.Fatalf("%s: %v", s.name, err)
		}
	}

	// Two of them are taken out of the channel; the record keeps them.
	for _, s := range []*sim{a2, b1} {
		s := s
		testkit.Eventually(t, s.name+" is taken out", func() bool {
			a1.act(id, wire.ActRemoveServer, func(m *wire.ChannelAction) { m.Server = s.name })
			return !a1.record(id).Has(s.name)
		})
	}
	rec = a1.record(id)
	if !holds(rec, a1) || !holds(rec, a2) || !holds(rec, b1) {
		t.Fatalf("a server that left lost its VRF keys from the record: %+v", rec.VRFKeys)
	}
	if rec.Has("a2.sim") || rec.Has("b1.sim") {
		t.Fatal("the servers were not taken out")
	}
}
