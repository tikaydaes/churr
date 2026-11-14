package wire

import (
	"path/filepath"
	"testing"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/secret"
)

// The cluster roster's rules (docs/LINK-PROTOCOL.md section 23b).

type rosterHub struct {
	name string
	keys *secret.Keys
}

func newRosterHub(t *testing.T, name string) rosterHub {
	t.Helper()
	keys, err := secret.Open(filepath.Join(t.TempDir(), "keys"))
	if err != nil {
		t.Fatal(err)
	}
	return rosterHub{name, keys}
}

func (h rosterHub) entry(t *testing.T, cluster Bytes, via *rosterHub, at model.Time) ClusterEntry {
	t.Helper()
	m := ClusterMember{Cluster: cluster, Hub: h.name, SignKey: Bytes(h.keys.SignPublic()), JoinedAt: at}
	if via != nil {
		m.Via = via.name
	}
	e, err := SignMember(m, h.keys)
	if err != nil {
		t.Fatal(err)
	}
	if via != nil {
		e = Countersign(e, via.keys)
	}
	return e
}

func TestVerifyCluster(t *testing.T) {
	a, b, c, d := newRosterHub(t, "a"), newRosterHub(t, "b"), newRosterHub(t, "c"), newRosterHub(t, "d")
	id := ClusterID(a.keys.SignPublic())
	ea := a.entry(t, id, nil, 100)
	eb := b.entry(t, id, &a, 200)
	ec := c.entry(t, id, &b, 300) // a - b - c
	members := func(entries ...ClusterEntry) string {
		t.Helper()
		got, err := VerifyCluster(entries)
		if err != nil {
			return "refused"
		}
		out := ""
		for _, n := range got.Names() {
			out += n
		}
		return out
	}
	gone := func(e ClusterEntry, by rosterHub) ClusterEntry {
		t.Helper()
		out, err := SignGone(e, by.name, 400, by.keys)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	for name, tc := range map[string]struct {
		entries []ClusterEntry
		want    string
	}{
		"a founder alone":                        {[]ClusterEntry{ea}, "a"},
		"a line of three, in any order":          {[]ClusterEntry{ec, ea, eb}, "abc"},
		"nothing":                                {nil, "refused"},
		"no founder":                             {[]ClusterEntry{eb, ec}, "refused"},
		"a founder whose key is not the cluster": {[]ClusterEntry{b.entry(t, id, nil, 100)}, "refused"},
		"two founders":                           {[]ClusterEntry{ea, b.entry(t, ClusterID(b.keys.SignPublic()), nil, 150)}, "refused"},
		"an entry its via did not countersign": {[]ClusterEntry{ea, eb, func() ClusterEntry {
			e := d.entry(t, id, nil, 250)
			m, _ := e.Member()
			m.Via = "a"
			e, _ = SignMember(m, d.keys)
			return Countersign(e, b.keys)
		}()}, "refused"},
		"an entry signed by another key":          {[]ClusterEntry{ea, func() ClusterEntry { e := eb; e.Sig = d.keys.Sign(e.Signed); return e }()}, "refused"},
		"a via that joined later":                 {[]ClusterEntry{ea, eb, d.entry(t, id, &b, 150)}, "refused"},
		"a via that is not in the roster":         {[]ClusterEntry{ea, d.entry(t, id, &c, 500)}, "refused"},
		"another cluster's entry":                 {[]ClusterEntry{ea, d.entry(t, ClusterID(d.keys.SignPublic()), &a, 250)}, "refused"},
		"a hub that left":                         {[]ClusterEntry{ea, eb, gone(ec, c)}, "ab"},
		"a hub its via removed":                   {[]ClusterEntry{ea, eb, gone(ec, b)}, "ab"},
		"a hub removed by a hub that may not":     {[]ClusterEntry{ea, eb, gone(ec, a)}, "refused"},
		"the hubs joined through a hub that left": {[]ClusterEntry{ea, gone(eb, b), ec}, "a"},
		"a hub admitted twice":                    {[]ClusterEntry{ea, eb, b.entry(t, id, &a, 250)}, "refused"},
		"a hub that left and joined again":        {[]ClusterEntry{ea, gone(eb, a), b.entry(t, id, &a, 500)}, "ab"},
	} {
		if got := members(tc.entries...); got != tc.want {
			t.Errorf("%s: members %q, want %q", name, got, tc.want)
		}
	}
}
