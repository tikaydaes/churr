package hub

import (
	"bytes"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/tikaydaes/churr/internal/link/wire"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/secret"
)

// This file is the hub as a member of a cluster (docs/LINK-PROTOCOL.md
// section 23b, ruling R58): the roster of hubs, how a hub joins and leaves,
// which neighbour leads to which hub, and the servers on the other hubs.
//
// A hub that has never been joined to another is a cluster of one, which it
// founded. Joining means trunking to one member of another cluster and being
// let in by it; the roster records that member as the new hub's `via`, so
// the roster is a tree and a second trunk between two of its hubs is refused
// by both ends.

// topology is the cluster as this hub last checked it.
type topology struct {
	cluster wire.Cluster
	entries []wire.ClusterEntry
	// next is, for every other hub, the neighbour on the way to it, and
	// hops how many trunks away it is.
	next map[string]string
	hops map[string]int
	// servers are the servers on the other hubs, by name.
	servers map[string]remoteServer
}

type remoteServer struct {
	hub                        string
	signKey, boxKey, boxKeySig []byte
	joinedAt                   int64
}

func (h *Hub) topology() *topology { return h.topo.Load() }

// hubOf names the hub a server is on: this one for a server of its own, ""
// for a name nobody in the cluster knows.
func (h *Hub) hubOf(server string) string {
	if server == h.id.Name {
		return h.id.Name // the hub's own holder (holder.go)
	}
	if rs, ok := h.topology().servers[server]; ok {
		return rs.hub
	}
	if _, ok := h.keyOf(server); ok {
		return h.id.Name
	}
	return ""
}

// serverKey is the signing key of a server anywhere in the cluster.
func (h *Hub) serverKey(name string) ([]byte, bool) {
	h.mu.Lock()
	k, ok := h.signKeys[name]
	h.mu.Unlock()
	if ok {
		return k, true
	}
	if rs, remote := h.topology().servers[name]; remote {
		k, ok = rs.signKey, true
	} else {
		k, ok = h.keyOf(name)
	}
	if ok {
		h.mu.Lock()
		h.signKeys[name] = k
		h.mu.Unlock()
	}
	return k, ok
}

// serverKeyIn is serverKey for use inside a transaction: the hub's database
// has one connection.
func (h *Hub) serverKeyIn(q querier, name string) ([]byte, bool) {
	if rs, remote := h.topology().servers[name]; remote {
		return rs.signKey, true
	}
	return keyIn(q, name)
}

func (h *Hub) setting(key string) string {
	var v string
	h.db.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&v)
	return v
}

func setSetting(tx execer, key, value string) error {
	if value == "" {
		_, err := tx.Exec(`DELETE FROM settings WHERE key = ?`, key)
		return err
	}
	_, err := tx.Exec(`INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT (key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

type execer interface {
	Exec(query string, args ...any) (sql.Result, error)
}

// bumpCluster tells whoever is serving that the cluster's tables have
// changed under it: an operator's command in another process.
func bumpCluster(tx execer) error {
	_, err := tx.Exec(`INSERT INTO meta (key, value) VALUES ('cluster_version', 1) ON CONFLICT (key) DO UPDATE SET value = value + 1`)
	return err
}

// openCluster reads the cluster when the hub's data is opened, founding a
// cluster of one if there is none.
func (h *Hub) openCluster() error {
	h.lid = h.meta("log_lid")
	var top sql.NullInt64
	h.db.QueryRow(`SELECT max(lid) FROM log`).Scan(&top)
	if top.Int64 > h.lid {
		h.lid = top.Int64
	}
	var n int
	h.db.QueryRow(`SELECT count(*) FROM cluster`).Scan(&n)
	if n == 0 {
		if err := h.tx(func(tx *sql.Tx) error { return h.found(tx) }); err != nil {
			return err
		}
	}
	rows, err := h.db.Query(`SELECT hub, offset_ms, uncertainty_ms, measured_at, via FROM hub_clocks`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var name string
		var c hubClock
		if err := rows.Scan(&name, &c.offset, &c.uncertainty, &c.measuredAt, &c.via); err != nil {
			rows.Close()
			return err
		}
		h.hubClocks[name] = c
	}
	rows.Close()
	return h.reloadTopology()
}

// found makes this hub the founder and only member of a new cluster.
func (h *Hub) found(tx *sql.Tx) error {
	m := wire.ClusterMember{Cluster: wire.ClusterID(h.keys.SignPublic()), Hub: h.id.Name, SignKey: wire.Bytes(h.keys.SignPublic()), BoxKey: h.keys.BoxPublic(), URL: h.id.URL, JoinedAt: h.now()}
	e, err := wire.SignMember(m, h.keys)
	if err != nil {
		return err
	}
	for _, table := range []string{"cluster", "hub_rosters", "hub_servers", "hub_clocks"} {
		if _, err := tx.Exec(`DELETE FROM ` + table); err != nil {
			return err
		}
	}
	_, err = tx.Exec(`INSERT INTO cluster (hub, joined_at, entry) VALUES (?, ?, ?)`, m.Hub, int64(m.JoinedAt), mustMessage("ClusterEntry", e))
	return err
}

func readEntries(q querier) ([]wire.ClusterEntry, error) {
	rows, err := q.Query(`SELECT entry FROM cluster ORDER BY joined_at, hub`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []wire.ClusterEntry
	for rows.Next() {
		var raw []byte
		var e wire.ClusterEntry
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err := wire.Unmarshal("ClusterEntry", raw, &e); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// reloadTopology reads the roster and the other hubs' servers from the
// database and works out the way to each hub.
func (h *Hub) reloadTopology() error {
	entries, err := readEntries(h.db)
	if err != nil {
		return err
	}
	c, err := wire.VerifyCluster(entries)
	if err != nil {
		return err
	}
	t := &topology{cluster: c, entries: entries, next: map[string]string{}, hops: map[string]int{}, servers: map[string]remoteServer{}}
	// The roster is the tree: each hub is joined to its via.
	near := map[string][]string{}
	for name, m := range c.Members {
		if m.Via != "" {
			near[name] = append(near[name], m.Via)
			near[m.Via] = append(near[m.Via], name)
		}
	}
	queue := []string{h.id.Name}
	seen := map[string]bool{h.id.Name: true}
	for len(queue) > 0 {
		at := queue[0]
		queue = queue[1:]
		for _, n := range near[at] {
			if seen[n] {
				continue
			}
			seen[n] = true
			if at == h.id.Name {
				t.next[n] = n
			} else {
				t.next[n] = t.next[at]
			}
			t.hops[n] = t.hops[at] + 1
			queue = append(queue, n)
		}
	}
	rows, err := h.db.Query(`SELECT name, hub, sign_key, box_key, box_key_sig, joined_at FROM hub_servers`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		var rs remoteServer
		if err := rows.Scan(&name, &rs.hub, &rs.signKey, &rs.boxKey, &rs.boxKeySig, &rs.joinedAt); err != nil {
			return err
		}
		if _, member := c.Members[rs.hub]; member {
			t.servers[name] = rs
		}
	}
	h.topo.Store(t)
	h.forgetRoutes()
	return rows.Err()
}

// forgetRoutes drops what was worked out from server lists and rosters.
func (h *Hub) forgetRoutes() {
	h.mu.Lock()
	h.routes = map[string]*route{}
	h.signKeys = map[string][]byte{}
	h.mu.Unlock()
}

// neighbours are the hubs the roster joins this one to directly.
func (t *topology) neighbours() []string {
	var out []string
	for hub, via := range t.next {
		if hub == via {
			out = append(out, hub)
		}
	}
	sort.Strings(out)
	return out
}

// entryKey names an admission.
func entryKey(e wire.ClusterEntry) string {
	m, _ := e.Member()
	return fmt.Sprintf("%s/%d", m.Hub, int64(m.JoinedAt))
}

// mergeCluster takes a roster a neighbour sent. Entries are only ever added
// to a roster, or ended; so two rosters of one cluster are joined by taking
// every entry of both. The result is checked whole before anything is
// stored, and a roster with one entry that is not what it claims is refused
// whole. It reports whether this hub's roster changed.
//
// A hub that stands alone and is shown a roster of another cluster that
// lists it, let in by the neighbour that sent it, takes that roster in
// place of its own: that is how it joins.
func (h *Hub) mergeCluster(from string, in []wire.ClusterEntry) (bool, error) {
	theirs, err := wire.VerifyCluster(in)
	if err != nil {
		return false, err
	}
	t := h.topology()
	if !bytes.Equal(theirs.ID, t.cluster.ID) {
		me, listed := theirs.Members[h.id.Name]
		if !listed || len(t.cluster.Members) > 1 || me.Via != from || !bytes.Equal(me.SignKey, h.keys.SignPublic()) {
			return false, nil
		}
		err := h.tx(func(tx *sql.Tx) error {
			for _, table := range []string{"cluster", "hub_rosters", "hub_servers", "hub_clocks"} {
				if _, err := tx.Exec(`DELETE FROM ` + table); err != nil {
					return err
				}
			}
			for _, e := range in {
				m, _ := e.Member()
				if _, err := tx.Exec(`INSERT INTO cluster (hub, joined_at, entry) VALUES (?, ?, ?)`, m.Hub, int64(m.JoinedAt), mustMessage("ClusterEntry", e)); err != nil {
					return err
				}
			}
			_, err := tx.Exec(`UPDATE trunks SET state = 'active', invite = '' WHERE name = ?`, from)
			return err
		})
		if err != nil {
			return false, err
		}
		h.mu.Lock()
		h.hubClocks = map[string]hubClock{}
		h.mu.Unlock()
		h.log.Info("this hub has joined a cluster", "through", from, "hubs", len(theirs.Members))
		return true, h.reloadTopology()
	}
	have := map[string]wire.ClusterEntry{}
	for _, e := range t.entries {
		have[entryKey(e)] = e
	}
	var changed []wire.ClusterEntry
	for _, e := range in {
		old, known := have[entryKey(e)]
		if known && (len(old.Gone) > 0 || len(e.Gone) == 0) {
			continue
		}
		have[entryKey(e)] = e
		changed = append(changed, e)
	}
	if len(changed) == 0 {
		return false, nil
	}
	merged := make([]wire.ClusterEntry, 0, len(have))
	for _, e := range have {
		merged = append(merged, e)
	}
	after, err := wire.VerifyCluster(merged)
	if err != nil {
		return false, err
	}
	err = h.tx(func(tx *sql.Tx) error {
		for _, e := range changed {
			m, _ := e.Member()
			if _, err := tx.Exec(`INSERT INTO cluster (hub, joined_at, entry) VALUES (?, ?, ?)
				ON CONFLICT (hub, joined_at) DO UPDATE SET entry = excluded.entry`, m.Hub, int64(m.JoinedAt), mustMessage("ClusterEntry", e)); err != nil {
				return err
			}
		}
		if _, still := after.Members[h.id.Name]; !still {
			// This hub's own admission has been ended: it stands alone
			// again, as the founder of a new cluster of one.
			if _, err := tx.Exec(`UPDATE trunks SET state = 'removed', invite = ''`); err != nil {
				return err
			}
			return h.found(tx)
		}
		return h.dropGoneHubs(tx, after)
	})
	if err != nil {
		return false, err
	}
	return true, h.reloadTopology()
}

// dropGoneHubs forgets the servers of hubs that are no longer in the
// cluster, and takes those servers out of the channels this hub owns: a
// server nothing can reach is in no channel.
func (h *Hub) dropGoneHubs(tx *sql.Tx, c wire.Cluster) error {
	rows, err := tx.Query(`SELECT name, hub FROM hub_servers`)
	if err != nil {
		return err
	}
	var gone []string
	for rows.Next() {
		var name, hub string
		if err := rows.Scan(&name, &hub); err != nil {
			rows.Close()
			return err
		}
		if _, member := c.Members[hub]; !member {
			gone = append(gone, name)
		}
	}
	rows.Close()
	for _, name := range gone {
		if _, err := tx.Exec(`DELETE FROM hub_servers WHERE name = ?`, name); err != nil {
			return err
		}
		if err := h.removeFromChannels(tx, name); err != nil {
			return err
		}
	}
	for _, table := range []string{"hub_rosters", "hub_clocks"} {
		rows, err := tx.Query(`SELECT hub FROM ` + table)
		if err != nil {
			return err
		}
		var names []string
		for rows.Next() {
			var name string
			if rows.Scan(&name) == nil {
				names = append(names, name)
			}
		}
		rows.Close()
		for _, name := range names {
			if _, member := c.Members[name]; !member {
				if _, err := tx.Exec(`DELETE FROM `+table+` WHERE hub = ?`, name); err != nil {
					return err
				}
			}
		}
	}
	// A trunk to a hub that is no longer a member is over.
	trunks, err := tx.Query(`SELECT name FROM trunks WHERE state = 'active'`)
	if err != nil {
		return err
	}
	var over []string
	for trunks.Next() {
		var name string
		if trunks.Scan(&name) == nil {
			if _, member := c.Members[name]; !member {
				over = append(over, name)
			}
		}
	}
	trunks.Close()
	for _, name := range over {
		if _, err := tx.Exec(`UPDATE trunks SET state = 'removed', invite = '' WHERE name = ?`, name); err != nil {
			return err
		}
	}
	if len(gone) > 0 {
		return bumpRoster(tx)
	}
	return nil
}

// clusterFrame is the roster as it is sent: every entry, each carrying its
// own signatures.
func (h *Hub) clusterFrame() wire.Frame {
	return wire.Frame{Frame: wire.FCluster, Hub: h.id.Name, Cluster: h.topology().entries}
}

// sendClusterToAll sends the roster to every connected neighbour and server.
func (h *Hub) sendClusterToAll() {
	f := h.clusterFrame()
	h.mu.Lock()
	trunks := make([]*trunkConn, 0, len(h.trunks))
	for _, t := range h.trunks {
		trunks = append(trunks, t)
	}
	conns := make([]*serverConn, 0, len(h.conns)+1)
	for _, c := range h.conns {
		conns = append(conns, c)
	}
	if h.held != nil {
		conns = append(conns, h.held)
	}
	h.mu.Unlock()
	for _, t := range trunks {
		t.send(f)
	}
	for _, c := range conns {
		c.conn.Send(f)
	}
}

// ------------------------------------------------- the other hubs' servers

// ownServers signs this hub's own servers for the other hubs of the
// cluster: the roster a server is sent holds every hub's.
func (h *Hub) ownServers() (wire.Frame, error) {
	ro := wire.Roster{Hub: h.id.Name, Version: h.meta("roster_version"), At: h.now(), Servers: []wire.RosterServer{}}
	rows, err := h.db.Query(`SELECT name, sign_key, box_key, box_key_sig, joined_at FROM servers WHERE state = 'active' ORDER BY name`)
	if err != nil {
		return wire.Frame{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var rs wire.RosterServer
		var sign, box, sig []byte
		var joined int64
		if err := rows.Scan(&rs.Name, &sign, &box, &sig, &joined); err != nil {
			return wire.Frame{}, err
		}
		rs.SignKey, rs.BoxKey, rs.BoxKeySig, rs.JoinedAt = sign, box, sig, model.Time(joined)
		ro.Servers = append(ro.Servers, rs)
	}
	return wire.SignBlock(wire.FHubServers, ro, h.keys)
}

// onHubServers takes one hub's signed list of its own servers, from that hub
// or passed on by a hub between. A name is on one hub: a list that claims a
// name this hub or another already has loses that name, and the rest of it
// stands.
func (h *Hub) onHubServers(from *trunkConn, f wire.Frame) error {
	var peek wire.Roster
	if err := wire.PeekBlock(f, &peek); err != nil {
		return err
	}
	t := h.topology()
	key := t.cluster.Key(peek.Hub)
	if key == nil || peek.Hub == h.id.Name {
		return nil // a hub this one does not know as a member; its list comes again once it does
	}
	if t.next[peek.Hub] != from.name {
		return nil // not from the side that hub is on
	}
	var ro wire.Roster
	if err := wire.VerifyBlock(f, key, &ro); err != nil {
		return err
	}
	var have int64
	h.db.QueryRow(`SELECT version FROM hub_rosters WHERE hub = ?`, ro.Hub).Scan(&have)
	if ro.Version <= have {
		return nil
	}
	err := h.tx(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`INSERT INTO hub_rosters (hub, version, signed, sig) VALUES (?, ?, ?, ?)
			ON CONFLICT (hub) DO UPDATE SET version = excluded.version, signed = excluded.signed, sig = excluded.sig`,
			ro.Hub, ro.Version, []byte(f.Signed), []byte(f.Sig)); err != nil {
			return err
		}
		before := map[string]bool{}
		rows, err := tx.Query(`SELECT name FROM hub_servers WHERE hub = ?`, ro.Hub)
		if err != nil {
			return err
		}
		for rows.Next() {
			var name string
			if rows.Scan(&name) == nil {
				before[name] = true
			}
		}
		rows.Close()
		if _, err := tx.Exec(`DELETE FROM hub_servers WHERE hub = ?`, ro.Hub); err != nil {
			return err
		}
		for _, rs := range ro.Servers {
			if !wire.VerifyBoxKey(rs.Name, rs.SignKey, rs.BoxKey, rs.BoxKeySig) {
				continue
			}
			var one int
			if tx.QueryRow(`SELECT 1 FROM servers WHERE name = ?`, rs.Name).Scan(&one) == nil ||
				tx.QueryRow(`SELECT 1 FROM hub_servers WHERE name = ?`, rs.Name).Scan(&one) == nil {
				h.log.Warn("a hub lists a server that is on another hub already; the name stays where it was", "hub", ro.Hub, "server", rs.Name)
				continue
			}
			if _, err := tx.Exec(`INSERT INTO hub_servers (name, hub, sign_key, box_key, box_key_sig, joined_at) VALUES (?, ?, ?, ?, ?, ?)`,
				rs.Name, ro.Hub, []byte(rs.SignKey), []byte(rs.BoxKey), []byte(rs.BoxKeySig), int64(rs.JoinedAt)); err != nil {
				return err
			}
			delete(before, rs.Name)
		}
		// A server that hub no longer lists is in no channel of this one's.
		for name := range before {
			if err := h.removeFromChannels(tx, name); err != nil {
				return err
			}
		}
		return bumpRoster(tx)
	})
	if err != nil {
		return err
	}
	if err := h.reloadTopology(); err != nil {
		return err
	}
	// On to the hubs on the other side of this one, unchanged.
	for _, tc := range h.liveTrunks() {
		if tc != from {
			tc.send(wire.Frame{Frame: wire.FHubServers, Signed: f.Signed, Sig: f.Sig})
		}
		// Which hub a server is on decides where a channel's envelopes go.
		h.armAll(tc)
	}
	// A record that named a server this hub had not yet heard of crosses
	// the trunk that leads to it now that it has.
	for _, tc := range h.liveTrunks() {
		h.sendRecordsTo(tc)
	}
	return nil
}

// sendHubServers sends a neighbour this hub's own servers and every other
// hub's list it holds from the other side of itself.
func (h *Hub) sendHubServers(tc *trunkConn) {
	if f, err := h.ownServers(); err == nil {
		tc.send(f)
	}
	t := h.topology()
	rows, err := h.db.Query(`SELECT hub, signed, sig FROM hub_rosters`)
	if err != nil {
		return
	}
	type held struct {
		hub         string
		signed, sig []byte
	}
	var all []held
	for rows.Next() {
		var x held
		if rows.Scan(&x.hub, &x.signed, &x.sig) == nil {
			all = append(all, x)
		}
	}
	rows.Close()
	for _, x := range all {
		if t.next[x.hub] != tc.name && t.next[x.hub] != "" {
			tc.send(wire.Frame{Frame: wire.FHubServers, Signed: x.signed, Sig: x.sig})
		}
	}
}

// ---------------------------------------------------------------- operator

// TrunkInvite makes an invitation for another hub to trunk to this one and
// so join its cluster. forName, if not empty, is the one hub name that may
// use it.
func (h *Hub) TrunkInvite(forName string, expires time.Duration) (string, error) {
	if expires <= 0 || expires > 7*24*time.Hour {
		expires = 7 * 24 * time.Hour
	}
	raw := secret.RandomBytes(32)
	inv := wire.Invitation{V: 1, Kind: wire.KindHub, Name: h.id.Name, URL: h.id.URL, FP: wire.Fingerprint(h.keys.SignPublic()),
		Secret: raw, ExpiresAt: h.now().Add(expires)}
	var name any
	if forName != "" {
		forName = strings.ToLower(forName)
		inv.For, name = &forName, forName
	}
	_, err := h.db.Exec(`INSERT INTO invites (id, hash, for_name, expires_at, created_at, kind) VALUES (?, ?, ?, ?, ?, 'trunk')`,
		model.NewID[string](), wire.InviteHash(raw), name, int64(inv.ExpiresAt), int64(h.now()))
	return inv.String(), err
}

// TrunkJoin takes an invitation another hub's operator made with
// TrunkInvite: this hub will dial that one and ask to be let into its
// cluster. A hub that is in a cluster with other hubs already is refused: a
// second trunk into the same cluster would close a loop, and a hub is in
// one cluster at a time.
func (h *Hub) TrunkJoin(invitation string) error {
	inv, err := wire.ParseInvitation(strings.TrimSpace(invitation))
	if err != nil || inv.Kind != wire.KindHub {
		return errors.New("that is not a hub's invitation")
	}
	if inv.Name == h.id.Name {
		return errors.New("that invitation is this hub's own")
	}
	if inv.ExpiresAt.Before(h.now()) {
		return errors.New("that invitation has expired")
	}
	if inv.For != nil && *inv.For != h.id.Name {
		return fmt.Errorf("that invitation is for %s", *inv.For)
	}
	if err := h.reloadTopology(); err != nil {
		return err
	}
	if t := h.topology(); len(t.cluster.Members) > 1 {
		if _, in := t.cluster.Members[inv.Name]; in {
			return fmt.Errorf("%s is in this hub's cluster already: a second trunk would close a loop", inv.Name)
		}
		return errors.New("this hub is in a cluster already; it must leave that one before it joins another (churr-hub cluster leave)")
	}
	return h.tx(func(tx *sql.Tx) error {
		var pending int
		tx.QueryRow(`SELECT count(*) FROM trunks WHERE state = 'pending'`).Scan(&pending)
		if pending > 0 {
			return errors.New("this hub is already joining a cluster; cancel that first (churr-hub trunk cancel)")
		}
		if _, err := tx.Exec(`INSERT INTO trunks (name, url, dials, invite, state, created_at) VALUES (?, ?, 1, ?, 'pending', ?)
			ON CONFLICT (name) DO UPDATE SET url = excluded.url, dials = 1, invite = excluded.invite, state = 'pending', sign_key = NULL`,
			inv.Name, inv.URL, inv.String(), int64(h.now())); err != nil {
			return err
		}
		return bumpCluster(tx)
	})
}

// TrunkCancel gives up a join that has not happened yet.
func (h *Hub) TrunkCancel() error {
	return h.tx(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`UPDATE trunks SET state = 'removed', invite = '' WHERE state = 'pending'`); err != nil {
			return err
		}
		return bumpCluster(tx)
	})
}

// ClusterLeave takes this hub out of its cluster. It must be at the edge of
// the tree (no hub joined through it), and no channel it is the home of may
// have a server on another hub: such a channel is moved first (Rehome), or
// its far servers removed. The hub then stands alone again.
func (h *Hub) ClusterLeave() error {
	if err := h.reloadTopology(); err != nil {
		return err
	}
	t := h.topology()
	me := t.cluster.Members[h.id.Name]
	if me.Via == "" {
		if len(t.cluster.Members) > 1 {
			return errors.New("this hub founded the cluster and others are joined through it; they leave first")
		}
		return errors.New("this hub is in no cluster but its own")
	}
	for name, m := range t.cluster.Members {
		if m.Via == h.id.Name {
			return fmt.Errorf("%s is joined to the cluster through this hub; it leaves first", name)
		}
	}
	chans, err := h.Channels()
	if err != nil {
		return err
	}
	owned := map[string]bool{}
	for _, c := range chans {
		owned[c.ID] = true
		if c.Home != h.id.Name {
			continue
		}
		for _, sv := range c.Servers {
			if _, remote := t.servers[sv]; remote {
				return fmt.Errorf("channel %s (%q) has its home here and %s is on another hub; move its home (churr-hub rehome) or remove that server first", c.ID, c.Name, sv)
			}
		}
	}
	// The same for a stream one of this hub's servers owns (an ad-hoc
	// channel, a sealed chat): it was opened here, and a server of another
	// hub that is in it would be cut off from its owner.
	streams, err := h.Streams()
	if err != nil {
		return err
	}
	for _, st := range streams {
		if owned[st.ID] || st.HomeHub != h.id.Name { // (Streams lists the open ones)
			continue
		}
		for _, sv := range st.Servers {
			if _, remote := t.servers[sv]; remote {
				return fmt.Errorf("stream %s, which %s owns, was opened here and %s is on another hub; its owner removes that server or closes it first", st.ID, st.Owner, sv)
			}
		}
	}
	return h.endAdmission(h.id.Name)
}

// ClusterRemove ends the admission of a hub that joined through this one.
// It is the operator's last resort for a hub that is gone for good; the
// hub must have nothing joined through it.
func (h *Hub) ClusterRemove(name string) error {
	name = strings.ToLower(name)
	if err := h.reloadTopology(); err != nil {
		return err
	}
	t := h.topology()
	m, ok := t.cluster.Members[name]
	if !ok {
		return fmt.Errorf("%s is not in this hub's cluster", name)
	}
	if m.Via != h.id.Name {
		return fmt.Errorf("%s joined through %s; it is removed there", name, m.Via)
	}
	for other, om := range t.cluster.Members {
		if om.Via == name {
			return fmt.Errorf("%s is joined to the cluster through %s; it is removed first", other, name)
		}
	}
	return h.endAdmission(name)
}

// endAdmission signs the end of a hub's admission, as that hub or as the
// member it joined through, and stores it. The serving hub sends it on and
// closes the trunk within a second.
func (h *Hub) endAdmission(name string) error {
	t := h.topology()
	var entry *wire.ClusterEntry
	for i, e := range t.entries {
		if m, err := e.Member(); err == nil && m.Hub == name && len(e.Gone) == 0 {
			entry = &t.entries[i]
		}
	}
	if entry == nil {
		return fmt.Errorf("%s has no admission to end", name)
	}
	gone, err := wire.SignGone(*entry, h.id.Name, h.now(), h.keys)
	if err != nil {
		return err
	}
	m, _ := gone.Member()
	return h.tx(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`UPDATE cluster SET entry = ? WHERE hub = ? AND joined_at = ?`, mustMessage("ClusterEntry", gone), m.Hub, int64(m.JoinedAt)); err != nil {
			return err
		}
		// The serving hub sends the roster on before it closes the trunk
		// (applyClusterChange); the trunk is marked so that it does.
		other := name
		var farewell any
		if name == h.id.Name {
			// This hub is leaving. The roster that says so is owed to the
			// neighbour it leaves through, and is kept until it has been
			// sent: at once if the hub is serving and the trunk is up,
			// otherwise when the hub next reaches that neighbour.
			other = m.Via
			entries := append([]wire.ClusterEntry{}, t.entries...)
			for i, e := range entries {
				if em, err := e.Member(); err == nil && em.Hub == m.Hub && em.JoinedAt == m.JoinedAt {
					entries[i] = gone
				}
			}
			// Kept as the frame that will say it.
			farewell = mustFrame(wire.Frame{Frame: wire.FCluster, Hub: h.id.Name, Cluster: entries})
		}
		if _, err := tx.Exec(`UPDATE trunks SET state = 'leaving', farewell = ? WHERE name = ?`, farewell, other); err != nil {
			return err
		}
		return bumpCluster(tx)
	})
}

// HubInfo is a hub of the cluster as `churr-hub cluster` lists it.
type HubInfo struct {
	Name, Fingerprint, Via string
	// Trunk is the state of this hub's own trunk to it, if it has one:
	// pending, active or removed; Connected whether that trunk is up now.
	Trunk     string
	Connected bool
	// Known says whether this hub has a figure for its clock against that
	// hub's; Offset is this hub's clock minus that hub's, in milliseconds,
	// and Uncertainty how sure it is.
	Known               bool
	Offset, Uncertainty int64
	Servers             int
	Stats               TrunkStats
}

// Cluster lists the hubs of the cluster, this one first.
func (h *Hub) Cluster() ([]HubInfo, error) {
	if err := h.reloadTopology(); err != nil {
		return nil, err
	}
	t := h.topology()
	trunks := map[string]string{}
	rows, err := h.db.Query(`SELECT name, state FROM trunks`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var name, state string
		if rows.Scan(&name, &state) == nil {
			trunks[name] = state
		}
	}
	rows.Close()
	var out []HubInfo
	names := t.cluster.Names()
	for name, state := range trunks {
		if _, member := t.cluster.Members[name]; !member && state == "pending" {
			names = append(names, name)
		}
	}
	for _, name := range names {
		m := t.cluster.Members[name]
		info := HubInfo{Name: name, Via: m.Via, Trunk: trunks[name]}
		if len(m.SignKey) > 0 {
			info.Fingerprint = wire.KeyFingerprint(m.SignKey)
		}
		if c, ok := h.hubClockOf(name); ok {
			info.Known, info.Offset, info.Uncertainty = true, c.offset, c.uncertaintyAt(int64(h.now()))
		}
		for _, rs := range t.servers {
			if rs.hub == name {
				info.Servers++
			}
		}
		info.Connected = h.trunkOf(name) != nil
		info.Stats = h.TrunkStats(name)
		if name == h.id.Name {
			out = append([]HubInfo{info}, out...)
		} else {
			out = append(out, info)
		}
	}
	return out, nil
}

// applyClusterChange is the serving hub catching up with what an operator's
// command changed in the database: a trunk to dial, an admission ended.
func (h *Hub) applyClusterChange() {
	if err := h.reloadTopology(); err != nil {
		h.log.Warn("the cluster roster in the database does not verify", "err", err)
		return
	}
	h.sendClusterToAll()
	// A trunk whose hub has just been taken out, or through which this hub
	// has just left, is closed once the roster that says so has gone over it.
	rows, err := h.db.Query(`SELECT name FROM trunks WHERE state = 'leaving'`)
	if err != nil {
		return
	}
	var leaving []string
	for rows.Next() {
		var name string
		if rows.Scan(&name) == nil {
			leaving = append(leaving, name)
		}
	}
	rows.Close()
	t := h.topology()
	for _, name := range leaving {
		var owed []byte
		var dials bool
		h.db.QueryRow(`SELECT farewell, dials FROM trunks WHERE name = ?`, name).Scan(&owed, &dials)
		tc := h.trunkOf(name)
		if tc == nil && len(owed) > 0 && dials {
			// This hub has left and its neighbour has not heard: the trunk
			// is down, or the command was run while the hub was not serving.
			// The trunk stays as it is, its dialler goes on trying, and the
			// first thing said over it is the farewell (sayFarewell).
			continue
		}
		if tc != nil {
			tc.conn.CloseWith(wire.Frame{Frame: wire.FBye, Reason: "removed"})
		}
		h.db.Exec(`UPDATE trunks SET state = 'removed', invite = '', farewell = NULL WHERE name = ?`, name)
	}
	if _, member := t.cluster.Members[h.id.Name]; !member {
		// This hub has left: it stands alone again. The channels it carried
		// for other hubs have stopped for its servers, and they are told.
		var stopped []string
		h.tx(func(tx *sql.Tx) error {
			if _, err := tx.Exec(`UPDATE trunks SET state = 'removed', invite = '' WHERE state <> 'leaving'`); err != nil {
				return err
			}
			var err error
			if stopped, err = h.stopCarried(tx); err != nil {
				return err
			}
			if err := h.found(tx); err != nil {
				return err
			}
			return bumpRoster(tx)
		})
		h.mu.Lock()
		h.hubClocks = map[string]hubClock{}
		h.routes = map[string]*route{}
		h.mu.Unlock()
		h.reloadTopology()
		h.sendClusterToAll()
		for _, id := range stopped {
			h.tellStopped(id, nil)
		}
	} else {
		h.tx(func(tx *sql.Tx) error { return h.dropGoneHubs(tx, t.cluster) })
		h.reloadTopology()
	}
	h.ensureDiallers()
}

// stoppedReason is what a hub says beside the unsigned stream_close by which
// it tells a server that a stream homed on another hub has stopped for it:
// this hub has left that hub's cluster, and carries the stream no longer.
const stoppedReason = "hub_left"

// stopCarried ends, on a hub that has just left its cluster, every stream it
// carried for another hub: the channels of other homes, and the streams
// servers of other hubs own. Nothing of them can reach this hub's servers
// any more. The streams are kept, closed, so that a server that is not
// connected now is told when it returns.
func (h *Hub) stopCarried(tx *sql.Tx) ([]string, error) {
	rows, err := tx.Query(`SELECT id FROM streams WHERE state <> 'closed' AND home_hub <> '' AND home_hub <> ?`, h.id.Name)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	for _, id := range ids {
		// told 2: its servers here are owed the word, whenever they connect.
		if _, err := tx.Exec(`UPDATE streams SET state = 'closed', closed_at = ?, told = 2 WHERE id = ?`, int64(h.now()), id); err != nil {
			return nil, err
		}
	}
	return ids, nil
}

// tellStopped tells this hub's servers that were in a stream it has stopped
// carrying, or were invited to it, that it has stopped for them: all of them
// that are connected, or the one named.
func (h *Hub) tellStopped(id string, only *serverConn) {
	names := map[string]bool{}
	rows, err := h.db.Query(`SELECT name FROM stream_servers WHERE stream_id = ?`, id)
	if err != nil {
		return
	}
	for rows.Next() {
		var n string
		if rows.Scan(&n) == nil {
			names[n] = true
		}
	}
	rows.Close()
	if c, err := loadChannel(h.db, id); err == nil {
		for _, n := range c.rec.Invited {
			names[n] = true
		}
	}
	f := wire.Frame{Frame: wire.FStreamClose, StreamID: id, Reason: stoppedReason, Hub: h.id.Name}
	if only != nil {
		if names[only.name] {
			only.conn.Send(f)
		}
		return
	}
	for n := range names {
		if _, local := h.keyOf(n); local {
			h.sendTo([]string{n}, f)
		}
	}
}

// resendStopped tells a server that has just connected of the streams that
// stopped for it while this hub left a cluster.
func (h *Hub) resendStopped(sc *serverConn) {
	rows, err := h.db.Query(`SELECT id FROM streams WHERE told = 2`)
	if err != nil {
		return
	}
	var ids []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	for _, id := range ids {
		h.tellStopped(id, sc)
	}
}

// sayFarewell is the first and last thing said over a trunk this hub has
// left through: the roster that carries the end of its own admission, and
// goodbye. The neighbour takes this hub out of the cluster and tells the
// rest.
func (h *Hub) sayFarewell(name string, conn *wire.Conn) {
	var owed []byte
	h.db.QueryRow(`SELECT farewell FROM trunks WHERE name = ?`, name).Scan(&owed)
	var farewell wire.Frame
	if len(owed) > 0 && readFrame(owed, &farewell) == nil && len(farewell.Cluster) > 0 {
		conn.Send(farewell)
	}
	conn.CloseWith(wire.Frame{Frame: wire.FBye, Reason: "removed"})
	h.db.Exec(`UPDATE trunks SET state = 'removed', invite = '', farewell = NULL WHERE name = ?`, name)
	h.log.Info("the hub this one left its cluster through has been told", "hub", name)
}
