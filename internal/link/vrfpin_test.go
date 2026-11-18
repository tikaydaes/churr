package link_test

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/ecvrf"
	"github.com/tikaydaes/churr/internal/link/wire"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/testkit"
)

// A direct link pins the other server's VRF key history when it is made
// (docs/DESIGN-FAIR.md section 1). A server that comes back with another
// VRF key, one that is not an entry added after the pinned ones, is refused
// on the link and the refusal is audited; the old history stays pinned.
func TestDirectLinkPinsTheVRFKeyAndRefusesASwappedOne(t *testing.T) {
	l := testkit.NewLinked(t, 2, false)
	a, b := l.Servers[0], l.Servers[1]
	onA, onB := l.Peers[0], l.Peers[1]
	connected := func(c *testkit.Client, id string) bool {
		p := c.Must(200, "GET", "/api/v1/admin/peers/"+id, nil)["peer"].(map[string]any)
		return p["state"] == "active" && p["connected_at"] != nil
	}
	testkit.Eventually(t, "the direct link is up", func() bool { return connected(a.Owner, onA) && connected(b.Owner, onB) })

	pinned := func(on *testkit.TestServer, name string) []wire.VRFKey {
		sv, err := on.Server.Store.Peers().ServerByName(ctx, name)
		if err != nil {
			t.Fatal(err)
		}
		var keys []wire.VRFKey
		if len(sv.VRFKeys) > 0 {
			if err := json.Unmarshal(sv.VRFKeys, &keys); err != nil {
				t.Fatal(err)
			}
		}
		return keys
	}
	// Each side pinned the other's: the one key it has.
	for _, side := range []struct{ on, of *testkit.TestServer }{{a, b}, {b, a}} {
		keys := pinned(side.on, side.of.Server.Link.Identity().Name)
		want := side.of.Server.Keys.VRFPublic()
		if len(keys) != 1 || string(keys[0].Key) != string(want) {
			t.Fatalf("%s pinned %+v for %s, want its one key", side.on.Server.Link.Identity().Name, keys, side.of.Server.Link.Identity().Name)
		}
	}
	old := pinned(a, testkit.ServerName(1))

	// b loses its VRF key and its history and starts with a new one: not an
	// entry added after the old, but another history.
	b.Stop()
	keys := b.Server.Config.KeysDir()
	for _, name := range []string{"vrf.key", "vrf.history"} {
		if err := os.Remove(filepath.Join(keys, name)); err != nil {
			t.Fatal(err)
		}
	}
	b.Restart()
	if string(b.Server.Keys.VRFPublic()) == string(old[0].Key) {
		t.Fatal("b came back with its old VRF key")
	}
	testkit.Eventually(t, "a refuses b and says so", func() bool {
		audit := fmt.Sprint(a.Owner.Must(200, "GET", "/api/v1/admin/audit?action=link.vrf_key_mismatch", nil))
		return strings.Contains(audit, testkit.ServerName(1))
	})
	if now := pinned(a, testkit.ServerName(1)); len(now) != 1 || string(now[0].Key) != string(old[0].Key) {
		t.Fatalf("the pin changed to %+v", now)
	}
}

// A key added after the pinned ones, as an operator's new key leaves the
// history, is taken: the pin grows and the change is audited.
func TestDirectLinkTakesAnAddedVRFKey(t *testing.T) {
	l := testkit.NewLinked(t, 2, false)
	a, b := l.Servers[0], l.Servers[1]
	b.Stop()
	dir := b.Server.Config.KeysDir()
	var hist []map[string]any
	raw, err := os.ReadFile(filepath.Join(dir, "vrf.history"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &hist); err != nil {
		t.Fatal(err)
	}
	sk := make([]byte, 32)
	for i := range sk {
		sk[i] = byte(i + 1)
	}
	pk, err := ecvrf.DerivePublicKey(sk)
	if err != nil {
		t.Fatal(err)
	}
	hist = append(hist, map[string]any{"key": hex.EncodeToString(pk), "began": model.Now().Add(time.Second)})
	out, _ := json.Marshal(hist)
	if err := os.WriteFile(filepath.Join(dir, "vrf.history"), out, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "vrf.key"), sk, 0o600); err != nil {
		t.Fatal(err)
	}
	b.Restart()
	testkit.Eventually(t, "a takes the added key", func() bool {
		sv, err := a.Server.Store.Peers().ServerByName(ctx, testkit.ServerName(1))
		var keys []wire.VRFKey
		return err == nil && json.Unmarshal(sv.VRFKeys, &keys) == nil && len(keys) == 2 && string(keys[1].Key) == string(pk)
	})
	audit := fmt.Sprint(a.Owner.Must(200, "GET", "/api/v1/admin/audit?action=link.vrf_key_added", nil))
	if !strings.Contains(audit, testkit.ServerName(1)) {
		t.Fatalf("the added key was not audited: %v", audit)
	}
}
