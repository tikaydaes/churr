package api_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/tikaydaes/churr/internal/link/wire"
	"github.com/tikaydaes/churr/internal/testkit"
)

// The VRF key history is published at /.well-known/churr, to anyone, and
// given to clients in getServer, before and after sign-in, the same
// entries in both, each signed by the signing key the document names
// (docs/DESIGN-FAIR.md section 1).
func TestVRFKeysPublished(t *testing.T) {
	ts := testkit.NewServer(t, testkit.Options{})
	anon := ts.Browser()
	doc := anon.Must(200, "GET", "/.well-known/churr", nil)
	var w wire.WellKnown
	b, _ := json.Marshal(doc)
	if err := json.Unmarshal(b, &w); err != nil {
		t.Fatal(err)
	}
	if len(w.VRFKeys) != 1 || len(w.VRFKeys[0].Key) != 33 {
		t.Fatalf("the well-known document's VRF keys: %+v", w.VRFKeys)
	}
	if err := wire.CheckVRFKeys(w.Name, w.SignKey, w.VRFKeys); err != nil {
		t.Fatal(err)
	}
	for _, c := range []*testkit.Client{anon, ts.Owner} {
		srv := c.Must(200, "GET", "/api/v1/server", nil)["server"].(map[string]any)
		if !reflect.DeepEqual(srv["vrf_keys"], doc["vrf_keys"]) {
			t.Fatalf("getServer gives %v, the well-known document %v", srv["vrf_keys"], doc["vrf_keys"])
		}
	}
}
