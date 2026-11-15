package wire

import (
	"bytes"
	"sort"

	"github.com/tikaydaes/churr/internal/model"
)

// This file is the cluster roster: which hubs are joined by trunks, and
// their keys (docs/LINK-PROTOCOL.md section 23b, ruling R58). A hub checks
// it before it carries anything for another hub; a server checks it before
// it takes a channel record signed by a hub it is not connected to.
//
// The roster is a set of entries, one per admission. An entry is signed by
// the hub it names, which proves the key is that hub's, and countersigned
// by the member it trunked to, which proves a member let it in. Every hub
// but the founder names the member it trunked to as `via`, so the roster is
// also the shape of the cluster: a tree, because a hub is let in over one
// trunk and no other.

// ClusterMember is the signed content of a cluster_member block.
type ClusterMember struct {
	// Cluster names the cluster: the fingerprint of its founder's signing
	// key, so that a roster cannot be re-rooted under another founder.
	Cluster Bytes  `json:"cluster"`
	Hub     string `json:"hub"`
	SignKey Bytes  `json:"sign_key"`
	// URL is where the hub accepts link connections.
	URL string `json:"url,omitempty"`
	// Via is the member this hub trunked to when it joined; empty for the
	// founder.
	Via      string     `json:"via,omitempty"`
	JoinedAt model.Time `json:"joined_at"`
}

// ClusterGone is the signed content of a cluster_gone block: the admission
// it names is over. It is signed by the hub itself (it left) or by the
// member it had trunked to (it was removed).
type ClusterGone struct {
	Cluster  Bytes      `json:"cluster"`
	Hub      string     `json:"hub"`
	JoinedAt model.Time `json:"joined_at"`
	By       string     `json:"by"`
	At       model.Time `json:"at"`
}

// ClusterEntry is one admission as it travels: the member block with its two
// signatures and, once it is over, the block that says so.
type ClusterEntry struct {
	Signed Bytes `json:"signed"`
	Sig    Bytes `json:"sig"`
	// ViaSig is the countersignature of the member named as via. Absent on
	// the founder's entry.
	ViaSig  Bytes `json:"via_sig,omitempty"`
	Gone    Bytes `json:"gone,omitempty"`
	GoneSig Bytes `json:"gone_sig,omitempty"`
}

// ClusterID is the name of the cluster a hub with this key founds.
func ClusterID(founderKey []byte) Bytes { return Fingerprint(founderKey) }

// SignMember makes a hub's own entry, signed by itself and not yet
// countersigned.
func SignMember(m ClusterMember, keys Signer) (ClusterEntry, error) {
	signed, sig, err := SignBytes(BClusterMember, m, keys)
	return ClusterEntry{Signed: signed, Sig: sig}, err
}

// Countersign adds the signature of the member that lets the hub in.
func Countersign(e ClusterEntry, keys Signer) ClusterEntry {
	e.ViaSig = keys.Sign(blockMessage(BClusterMember+"/via", e.Signed))
	return e
}

// SignGone ends an admission.
func SignGone(e ClusterEntry, by string, at model.Time, keys Signer) (ClusterEntry, error) {
	var m ClusterMember
	if err := unmarshalBlock(BClusterMember, e.Signed, &m); err != nil {
		return e, err
	}
	var err error
	e.Gone, e.GoneSig, err = SignBytes(BClusterGone, ClusterGone{Cluster: m.Cluster, Hub: m.Hub, JoinedAt: m.JoinedAt, By: by, At: at}, keys)
	return e, err
}

// Member reads an entry's member block without checking it.
func (e ClusterEntry) Member() (ClusterMember, error) {
	var m ClusterMember
	err := unmarshalBlock(BClusterMember, e.Signed, &m)
	return m, err
}

// Cluster is a roster that has been checked: its live members.
type Cluster struct {
	ID      Bytes
	Founder string
	// Members are the hubs in the cluster now, by name.
	Members map[string]ClusterMember
}

// Key returns the signing key of a member, or nil.
func (c Cluster) Key(hub string) []byte {
	if m, ok := c.Members[hub]; ok {
		return m.SignKey
	}
	return nil
}

// Names lists the members in name order.
func (c Cluster) Names() []string {
	out := make([]string, 0, len(c.Members))
	for n := range c.Members {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// VerifyCluster checks a roster and returns its live members. It is refused
// as a whole if any entry in it is not what it claims:
//
//   - every entry names the same cluster, and exactly one has no via: the
//     founder, whose key's fingerprint is the cluster's name;
//   - every entry is signed by the key it names;
//   - every entry but the founder's is countersigned by the hub it names as
//     via, which was a member before it;
//   - a block that ends an admission is signed by that hub or by its via;
//   - a hub is admitted once at a time.
//
// A hub whose via is gone is gone with it: nothing joins it to the rest.
func VerifyCluster(entries []ClusterEntry) (Cluster, error) {
	type row struct {
		m    ClusterMember
		e    ClusterEntry
		gone bool
	}
	bad := func(format string, args ...any) (Cluster, error) {
		return Cluster{}, Errf(EBadSignature, "The cluster roster is refused: "+format, args...)
	}
	rows := make([]row, 0, len(entries))
	for _, e := range entries {
		var m ClusterMember
		if err := VerifyBytes(BClusterMember, e.Signed, e.Sig, peekKey(e.Signed), &m); err != nil {
			return bad("an entry is not signed by the key it names.")
		}
		if m.Hub == "" || len(m.SignKey) != 32 || len(m.Cluster) == 0 || m.Hub == m.Via {
			return bad("an entry is malformed.")
		}
		rows = append(rows, row{m: m, e: e})
	}
	if len(rows) == 0 {
		return bad("it is empty.")
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].m.JoinedAt != rows[j].m.JoinedAt {
			return rows[i].m.JoinedAt < rows[j].m.JoinedAt
		}
		return rows[i].m.Hub < rows[j].m.Hub
	})
	c := Cluster{Members: map[string]ClusterMember{}}
	// admitted is every admission checked so far, live or over, by hub: a
	// countersignature is checked against the key its via had.
	admitted := map[string][]row{}
	for i, r := range rows {
		m := r.m
		if m.Via == "" {
			if c.Founder != "" || !bytes.Equal(ClusterID(m.SignKey), m.Cluster) {
				return bad("%s is not the founder of the cluster it names.", m.Hub)
			}
			c.Founder, c.ID = m.Hub, m.Cluster
		}
		if c.ID != nil && !bytes.Equal(m.Cluster, c.ID) {
			return bad("%s names another cluster.", m.Hub)
		}
		if m.Via != "" {
			ok := false
			for _, v := range admitted[m.Via] {
				if v.m.JoinedAt <= m.JoinedAt && verify(v.m.SignKey, blockMessage(BClusterMember+"/via", r.e.Signed), r.e.ViaSig) {
					ok = true
				}
			}
			if !ok {
				return bad("%s was not let in by %s.", m.Hub, m.Via)
			}
		}
		if len(r.e.Gone) > 0 {
			var g ClusterGone
			if err := unmarshalBlock(BClusterGone, r.e.Gone, &g); err != nil || g.Hub != m.Hub || g.JoinedAt != m.JoinedAt || !bytes.Equal(g.Cluster, m.Cluster) {
				return bad("a block that ends %s's admission is not about it.", m.Hub)
			}
			ok := g.By == m.Hub && verify(m.SignKey, blockMessage(BClusterGone, r.e.Gone), r.e.GoneSig)
			if g.By == m.Via && m.Via != "" {
				for _, v := range admitted[m.Via] {
					ok = ok || verify(v.m.SignKey, blockMessage(BClusterGone, r.e.Gone), r.e.GoneSig)
				}
			}
			if !ok {
				return bad("%s's admission was ended by nobody who may.", m.Hub)
			}
			rows[i].gone = true
		}
		admitted[m.Hub] = append(admitted[m.Hub], rows[i])
	}
	if c.Founder == "" {
		return bad("it has no founder.")
	}
	// The live members: admitted, not gone, and joined to the founder by a
	// chain of live members.
	live := map[string]row{}
	for _, r := range rows {
		if r.gone {
			continue
		}
		if _, twice := live[r.m.Hub]; twice {
			return bad("%s is admitted twice.", r.m.Hub)
		}
		live[r.m.Hub] = r
	}
	for name, r := range live {
		at, steps := r.m, 0
		for at.Via != "" {
			up, ok := live[at.Via]
			if !ok || steps > len(live) {
				at.Hub = ""
				break
			}
			at, steps = up.m, steps+1
		}
		if at.Hub == c.Founder && at.Via == "" {
			c.Members[name] = r.m
		}
	}
	return c, nil
}

// peekKey reads the key a member block names, to check the block's own
// signature against. Nothing else is done with it until that has verified.
func peekKey(signed []byte) []byte {
	var m ClusterMember
	if unmarshalBlock(BClusterMember, signed, &m) != nil {
		return nil
	}
	return m.SignKey
}

// HubClock is one line of a hub_clocks frame: the sender's clock minus the
// named hub's, the uncertainty of that figure, and how old its oldest
// measurement was when the frame left, all in milliseconds.
type HubClock struct {
	Offset      int64 `json:"offset_ms"`
	Uncertainty int64 `json:"uncertainty_ms"`
	Age         int64 `json:"age_ms"`
}

// SyncStream is what a hub says it holds of one stream when a trunk comes
// up: the version of the channel record, and per origin the counter of the
// last envelope in its log.
type SyncStream struct {
	Version int64            `json:"v,omitempty"`
	Have    map[string]int64 `json:"have,omitempty"`
}

// RelayItem is one envelope of a relay frame, with what the sending hub adds
// beside it: the counter of the same origin's envelope before it in the
// sender's log, and, when the envelope is older than the checks on time
// allow, the stretch of disconnection that excuses it, by the home hub's
// clock as the sender reads it.
type RelayItem struct {
	Envelope Envelope `json:"envelope"`
	Prev     int64    `json:"prev,omitempty"`
	Window   []int64  `json:"window,omitempty"`
}

// RelayRefusal names an envelope a hub would not take from a trunk.
type RelayRefusal struct {
	StreamID      string `json:"stream_id"`
	OriginEventID string `json:"origin_event_id"`
	Origin        string `json:"origin,omitempty"`
	Code          string `json:"code"`
	Message       string `json:"message,omitempty"`
}

// Handoff is what a channel's home hub keeps beside the channel's record,
// handed with the record to the hub that takes the home over (section 23b):
// who shared the channel, who invited whom, where each member server's
// history starts, who is still to be told they were declined.
type Handoff struct {
	Creator    string                      `json:"creator"`
	Inviters   map[string]string           `json:"inviters"`
	JoinedHave map[string]map[string]int64 `json:"joined_have"`
	Declined   []string                    `json:"declined"`
	MoveID     string                      `json:"move_id"`
	Epoch      int64                       `json:"epoch"`
}
