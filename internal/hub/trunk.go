package hub

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math/rand"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tikaydaes/churr/internal/link/wire"
	"github.com/tikaydaes/churr/internal/model"
)

// This file is a trunk: the connection between two hubs of a cluster
// (docs/LINK-PROTOCOL.md section 23b). It is the link protocol's own
// connection, with the same handshake, in which both ends prove their keys.
// The hub that was invited dials, as a server does. Over it travel the
// cluster roster, each hub's servers, the clocks, channel records on their
// way from a channel's home hub to the hubs that need them, envelopes in
// batches (ship.go), requests on their way to a home hub, and frames for
// one server on their way to its hub.

// The limits of one trunk.
const (
	// relayMaxItems and relayMaxBytes bound one batch of envelopes.
	relayMaxItems = 256
	relayMaxBytes = 512 << 10
	// relayWindow is how many batches may be on their way, sent and not yet
	// answered, on one trunk.
	relayWindow = 8
	// trunkHold is how long a hub may take to pass on what it has taken
	// before the trunk counts as down: a batch not answered in this time
	// closes it.
	trunkHold = 60 * time.Second
	// heldPerServer is how many words a hub keeps for a server of its own
	// that was not connected when they came.
	heldPerServer = 100
)

// TrunkStats counts what crossed one trunk since the hub started.
type TrunkStats struct {
	// Batches and Envelopes sent, the largest batch, and the most batches
	// that were on their way at once.
	Batches, Envelopes, MaxBatch, MaxInflight int64
	// Received counts envelopes taken from the trunk, Refused those this
	// hub would not take, and RefusedThere those the neighbour would not
	// take from this hub.
	Received, Refused, RefusedThere int64
	// Suspect counts envelopes taken from the trunk whose time this hub
	// would not have accepted had it been their edge: taken all the same,
	// because the edge hub's word on an envelope's time stands (ship.go).
	Suspect int64
	// Waits counts the times the sender had a batch ready and no room in
	// the window.
	Waits int64
}

// TrunkStats returns the counts of one trunk.
func (h *Hub) TrunkStats(name string) TrunkStats {
	h.mu.Lock()
	defer h.mu.Unlock()
	if s := h.trunkStats[name]; s != nil {
		return *s
	}
	return TrunkStats{}
}

func (h *Hub) statsOf(name string, fn func(s *TrunkStats)) {
	h.mu.Lock()
	s := h.trunkStats[name]
	if s == nil {
		s = &TrunkStats{}
		h.trunkStats[name] = s
	}
	fn(s)
	h.mu.Unlock()
}

// trunkConn is one connected trunk.
type trunkConn struct {
	h    *Hub
	name string
	conn *wire.Conn
	key  []byte
	// pinger measures the neighbour's clock, on the side that listens.
	pinger *wire.Pinger

	mu sync.Mutex
	// up: the neighbour is a member of this hub's cluster and has been sent
	// what a trunk is sent when it comes up.
	up bool
	// synced: the neighbour has said everything it holds.
	synced bool
	// theirs is what the neighbour said it holds of each stream.
	theirs map[string]wire.SyncStream
	// skip is, per stream and origin, the counter up to which the
	// neighbour holds everything; routed the streams this trunk carries;
	// backfill the streams whose older rows are still to be sent.
	skip     map[string]map[string]int64
	routed   map[string]bool
	backfill []string
	// told is the version of each channel record sent on this connection.
	told map[string]int64
	// cursor is the lid of the last row of the log looked at for shipping.
	cursor int64
	// inflight is how many batches have been sent and not answered, and
	// sentAt when the oldest of them left.
	inflight int
	sentAt   []time.Time
	wake     chan struct{}
	started  atomic.Bool
	// putOff counts the batches in a row of which something was put off.
	putOff int
}

func (tc *trunkConn) send(f wire.Frame) error {
	if tap := tc.h.testTrunkTap.Load(); tap != nil {
		(*tap)(tc.name, true, f)
	}
	return tc.conn.Send(f)
}

func (tc *trunkConn) kick() {
	select {
	case tc.wake <- struct{}{}:
	default:
	}
}

func (tc *trunkConn) isUp() bool {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	return tc.up
}

// trunkOf returns the connected trunk to a neighbour, or nil.
func (h *Hub) trunkOf(name string) *trunkConn {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.trunks[name]
}

// liveTrunks are the trunks that are connected and up.
func (h *Hub) liveTrunks() []*trunkConn {
	h.mu.Lock()
	all := make([]*trunkConn, 0, len(h.trunks))
	for _, t := range h.trunks {
		all = append(all, t)
	}
	h.mu.Unlock()
	out := all[:0]
	for _, t := range all {
		if t.isUp() {
			out = append(out, t)
		}
	}
	return out
}

// toward returns the live trunk on the way to a hub, or nil if this hub is
// cut off from it.
func (h *Hub) toward(hub string) *trunkConn {
	next := h.topology().next[hub]
	if next == "" {
		return nil
	}
	tc := h.trunkOf(next)
	if tc == nil || !tc.isUp() {
		return nil
	}
	if next != hub {
		// Beyond the neighbour: only if the neighbour says it can reach it.
		// Each hub tells its neighbours which hubs it can reach on its own
		// side whenever that changes (hub_clocks), so a break two trunks
		// away is known here within a moment.
		h.mu.Lock()
		reached := h.reach[next][hub]
		h.mu.Unlock()
		if !reached {
			return nil
		}
	}
	return tc
}

// authorizeTrunk is what the hub checks of another hub that dials it and
// whose signatures have verified: a neighbour it already has shows its
// pinned key; a new one proves an invitation made for a trunk, and is not
// already in this hub's cluster, where a second trunk would close a loop.
func (h *Hub) authorizeTrunk(d wire.Frame, proof, th []byte) error {
	if d.Name == h.id.Name {
		return wire.Errf(wire.EAuthFailed, "That is this hub's own name.")
	}
	h.mu.Lock()
	cut := h.testCut[d.Name]
	h.mu.Unlock()
	if cut {
		return wire.Errf(wire.EUnavailable, "A test is holding this trunk down.")
	}
	var key []byte
	var state string
	err := h.db.QueryRow(`SELECT sign_key, state FROM trunks WHERE name = ?`, d.Name).Scan(&key, &state)
	if err == nil && state == "active" {
		if !bytes.Equal(key, d.SignKey) {
			return wire.Errf(wire.EKeyMismatch, "%s is pinned with another key.", d.Name)
		}
		return nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	// The roster is read again: an operator's command may have changed it.
	if err := h.reloadTopology(); err != nil {
		return err
	}
	if _, in := h.topology().cluster.Members[d.Name]; in {
		return wire.Errf(wire.EConflict, "%s is in this cluster already: a second trunk would close a loop.", d.Name)
	}
	now := int64(h.now())
	rows, err := h.db.Query(`SELECT id, hash, for_name, expires_at FROM invites WHERE kind = 'trunk'`)
	if err != nil {
		return err
	}
	type invite struct {
		id      string
		hash    []byte
		forName sql.NullString
		expires int64
	}
	var all []invite
	for rows.Next() {
		var in invite
		if err := rows.Scan(&in.id, &in.hash, &in.forName, &in.expires); err != nil {
			rows.Close()
			return err
		}
		all = append(all, in)
	}
	rows.Close()
	var match *invite
	expired := false
	for i, in := range all {
		if !wire.ProofMatches(in.hash, th, proof) {
			continue
		}
		if in.expires < now {
			expired = true
			continue
		}
		if in.forName.Valid && in.forName.String != d.Name {
			return wire.Errf(wire.EInviteInvalid, "That invitation is for another hub.")
		}
		match = &all[i]
	}
	if match == nil {
		if expired {
			return wire.Errf(wire.EInviteExpired, "That invitation has expired.")
		}
		return wire.Errf(wire.EInviteInvalid, "No invitation matches.")
	}
	return h.tx(func(tx *sql.Tx) error {
		res, err := tx.Exec(`DELETE FROM invites WHERE id = ?`, match.id)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return wire.Errf(wire.EInviteInvalid, "That invitation has been used.")
		}
		_, err = tx.Exec(`INSERT INTO trunks (name, sign_key, dials, state, created_at) VALUES (?, ?, 0, 'active', ?)
			ON CONFLICT (name) DO UPDATE SET sign_key = excluded.sign_key, dials = 0, state = 'active', invite = '', clock = NULL, away = '[]'`,
			d.Name, []byte(d.SignKey), now)
		return err
	})
}

// ensureDiallers starts a dialler for every trunk this hub dials and has
// none for, and closes the trunks that have been removed.
func (h *Hub) ensureDiallers() {
	rows, err := h.db.Query(`SELECT name, state, dials FROM trunks`)
	if err != nil {
		return
	}
	type row struct {
		name, state string
		dials       bool
	}
	var all []row
	for rows.Next() {
		var r row
		if rows.Scan(&r.name, &r.state, &r.dials) == nil {
			all = append(all, r)
		}
	}
	rows.Close()
	for _, r := range all {
		// (leaving: this hub has left through that trunk and has yet to say
		// so over it.)
		wanted := r.dials && (r.state == "pending" || r.state == "active" || r.state == "leaving")
		h.mu.Lock()
		stop, running := h.diallers[r.name]
		if running && !wanted {
			stop()
			delete(h.diallers, r.name)
		}
		var ctx context.Context
		if wanted && !running && h.runCtx.Err() == nil {
			var cancel context.CancelFunc
			ctx, cancel = context.WithCancel(h.runCtx)
			h.diallers[r.name] = cancel
		}
		tc := h.trunks[r.name]
		h.mu.Unlock()
		if ctx != nil {
			go h.dialTrunk(ctx, r.name)
		}
		if tc != nil && r.state == "removed" {
			tc.conn.CloseWith(wire.Frame{Frame: wire.FBye, Reason: "removed"})
		}
	}
}

// trunkRetry is the first wait before a trunk is dialled again; it doubles
// up to five minutes, as a server's does. Tests shorten it.
var trunkRetry atomic.Int64

func init() { trunkRetry.Store(int64(time.Second)) }

// SetTrunkRetry changes the first wait before a trunk is dialled again, and
// returns a function that puts it back. It is for tests.
func SetTrunkRetry(d time.Duration) func() {
	old := trunkRetry.Swap(int64(d))
	return func() { trunkRetry.Store(old) }
}

// dialTrunk keeps one trunk connected for as long as this hub should dial
// it.
func (h *Hub) dialTrunk(ctx context.Context, name string) {
	wait := time.Duration(trunkRetry.Load())
	for ctx.Err() == nil {
		var url, invite, state string
		var key []byte
		if err := h.db.QueryRow(`SELECT url, invite, state, sign_key FROM trunks WHERE name = ?`, name).Scan(&url, &invite, &state, &key); err != nil ||
			(state != "pending" && state != "active" && state != "leaving") {
			return
		}
		h.mu.Lock()
		cut := h.testCut[name]
		h.mu.Unlock()
		var conn *wire.Conn
		err := errors.New("held down by a test")
		if !cut {
			opts := wire.DialOptions{URL: url, Identity: h.id, HTTPClient: h.opts.HTTPClient}
			var inv wire.Invitation
			if invite != "" {
				if inv, err = wire.ParseInvitation(invite); err == nil && len(key) == 0 {
					opts.InviteSecret = inv.Secret
				}
			}
			opts.Check = func(l wire.Frame) error {
				switch {
				case l.Name != name || l.Kind != wire.KindHub:
					return wire.Errf(wire.EAuthFailed, "The listener is not the hub %s.", name)
				case len(key) > 0 && !bytes.Equal(key, l.SignKey):
					return wire.Errf(wire.EKeyMismatch, "%s shows another key than the one pinned.", name)
				case len(key) == 0 && !bytes.Equal(wire.Fingerprint(l.SignKey), inv.FP):
					return wire.Errf(wire.EKeyMismatch, "%s shows another key than the invitation names.", name)
				}
				return nil
			}
			conn, err = wire.Dial(ctx, opts)
		}
		if err != nil {
			var refused *wire.Error
			if errors.As(err, &refused) && refused.Code != wire.EUnavailable {
				h.log.Warn("a trunk was refused", "hub", name, "code", refused.Code, "message", refused.Message)
				h.tx(func(tx *sql.Tx) error { return setSetting(tx, "trunk_error:"+name, refused.Code+": "+refused.Message) })
			}
			jitter := time.Duration(rand.Int63n(int64(wait)/5+1)) - wait/10
			select {
			case <-ctx.Done():
				return
			case <-time.After(wait + jitter):
			}
			if wait *= 2; wait > 5*time.Minute {
				wait = 5 * time.Minute
			}
			continue
		}
		if len(key) == 0 {
			// The first connection: the key the invitation named is pinned.
			h.db.Exec(`UPDATE trunks SET sign_key = ? WHERE name = ?`, []byte(conn.Remote.SignKey), name)
		}
		h.tx(func(tx *sql.Tx) error { return setSetting(tx, "trunk_error:"+name, "") })
		if state == "leaving" {
			h.sayFarewell(name, conn)
			return
		}
		began := time.Now()
		removed := h.runTrunk(ctx, conn)
		if removed {
			return
		}
		if time.Since(began) > 60*time.Second {
			wait = time.Duration(trunkRetry.Load())
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}

// serveTrunk is /link/v1 answered to another hub.
func (h *Hub) serveTrunk(r *http.Request, conn *wire.Conn) {
	h.runTrunk(r.Context(), conn)
}

// runTrunk serves one trunk connection until it ends. It reports whether
// the neighbour said the trunk is over for good.
func (h *Hub) runTrunk(ctx context.Context, conn *wire.Conn) (removed bool) {
	tc := &trunkConn{h: h, name: conn.Remote.Name, conn: conn, key: conn.Remote.SignKey, theirs: map[string]wire.SyncStream{},
		skip: map[string]map[string]int64{}, routed: map[string]bool{}, told: map[string]int64{}, wake: make(chan struct{}, 1)}
	// The stretch the trunk was down for is noted before anything is read
	// from it: what the other side took in that time arrives now, with old
	// times.
	h.noteTrunkAway(tc.name)
	h.mu.Lock()
	old := h.trunks[tc.name]
	h.trunks[tc.name] = tc
	h.mu.Unlock()
	if old != nil {
		old.conn.CloseWith(wire.ErrorFrame(0, wire.EReplaced, "A newer connection for this trunk took over."))
	}
	defer func() {
		h.mu.Lock()
		gone := h.trunks[tc.name] == tc
		if gone {
			delete(h.trunks, tc.name)
			delete(h.reach, tc.name)
		}
		h.mu.Unlock()
		conn.Close()
		tc.kick()
		if gone {
			// The hubs on the other sides are told what can no longer be
			// reached through this one.
			h.shareHubClocks()
		}
	}()
	go conn.Watch()
	if !conn.Dialler {
		// The side that listens measures the other's clock and tells it, as
		// a hub does a server's. Each end then knows how far the two clocks
		// are apart.
		tc.pinger = wire.NewPinger(conn, func() int64 { return int64(h.now()) }, wire.NewClockTrack(h.cfg.ClockSynced, h.trunkClock(tc.name)),
			func(c wire.Clock) { h.setTrunkClock(tc.name, c, false) })
		go tc.pinger.Run()
	}
	tc.send(h.clusterFrame())
	if _, member := h.topology().cluster.Members[tc.name]; member {
		h.trunkUp(tc)
	}
	for {
		f, raw, err := conn.Read(ctx)
		if err != nil {
			var bad *wire.Error
			if errors.As(err, &bad) {
				conn.Send(bad.Frame(0))
				continue
			}
			return false
		}
		h.mu.Lock()
		delay := h.testDelay[tc.name]
		h.mu.Unlock()
		if delay > 0 {
			time.Sleep(delay) // a test's slow trunk: everything arrives this much later
		}
		if tap := h.testTrunkTap.Load(); tap != nil {
			(*tap)(tc.name, false, f)
		}
		if drop := h.testTrunkDrop.Load(); drop != nil && (*drop)(tc.name, f) {
			continue // a test is playing a trunk that loses this frame
		}
		switch f.Frame {
		case wire.FBye:
			return f.Reason == "removed"
		case wire.FPong:
			if tc.pinger != nil {
				tc.pinger.Pong(f)
			}
			continue
		case wire.FPing:
			t1 := int64(h.now())
			conn.AnswerPing(f, t1, func() int64 { return int64(h.now()) })
			if f.Clock != nil && conn.Dialler {
				h.setTrunkClock(tc.name, *f.Clock, true)
			}
			continue
		}
		if err := h.dispatchTrunk(tc, f, raw); err != nil {
			e := wire.AsError(err)
			if e.Code == wire.EInternal {
				h.log.Warn("a frame from a trunk could not be handled", "hub", tc.name, "frame", f.Frame, "err", err)
			} else {
				h.log.Debug("a frame from a trunk was refused", "hub", tc.name, "frame", f.Frame, "code", e.Code, "message", e.Message)
			}
			tc.send(e.Frame(f.ID))
		}
	}
}

// trunkUp sends a neighbour that is a member of the cluster what it is owed
// when a trunk comes up: every hub's servers from this side, the clocks, the
// channel records that concern its side, and what this hub holds of each
// stream the two share.
func (h *Hub) trunkUp(tc *trunkConn) {
	tc.mu.Lock()
	if tc.up {
		tc.mu.Unlock()
		return
	}
	tc.up = true
	tc.mu.Unlock()
	h.sendHubServers(tc)
	h.shareHubClocks()
	h.sendRecordsTo(tc)
	h.sendStreamsTo(tc)
	h.sendSync(tc)
	if tc.started.CompareAndSwap(false, true) {
		go h.ship(tc)
	}
}

func (h *Hub) dispatchTrunk(tc *trunkConn, f wire.Frame, raw []byte) error {
	switch f.Frame {
	case wire.FCluster:
		return h.onCluster(tc, f)
	case wire.FClusterJoin:
		return h.onClusterJoin(tc, f)
	case wire.FError:
		return nil
	}
	// Everything else is between members.
	if _, member := h.topology().cluster.Members[tc.name]; !member {
		return wire.Errf(wire.EForbidden, "%s is not in this hub's cluster.", tc.name)
	}
	switch f.Frame {
	case wire.FHubServers:
		return h.onHubServers(tc, f)
	case wire.FHubClocks:
		h.onHubClocks(tc, f)
		return nil
	case wire.FChannel:
		return h.onTrunkRecord(tc, f)
	case wire.FSync:
		return h.onSync(tc, f)
	case wire.FRelay:
		return h.onRelay(tc, f)
	case wire.FRelayAck:
		h.onRelayAck(tc, f)
		return nil
	case wire.FAsk:
		return h.onAsk(tc, f)
	case wire.FForward:
		h.forward(f.Server, f.Inner)
		return nil
	case wire.FRehome:
		return h.onRehome(tc, f)
	case wire.FStreamCtl:
		return h.onStreamCtl(tc, f)
	case wire.FEphemeral:
		if f.Envelope != nil {
			h.passEphemeral(*f.Envelope, tc.name)
		}
		return nil
	}
	return wire.Errf(wire.EBadFrame, "Unknown frame %q.", f.Frame)
}

// onCluster takes a neighbour's roster.
func (h *Hub) onCluster(tc *trunkConn, f wire.Frame) error {
	changed, err := h.mergeCluster(tc.name, f.Cluster)
	if err != nil {
		h.log.Warn("a cluster roster was refused", "from", tc.name, "err", err)
		return err
	}
	if changed {
		h.sendClusterToAll()
		h.ensureDiallers()
		h.Reconcile()
	}
	t := h.topology()
	_, member := t.cluster.Members[tc.name]
	if !member {
		// The neighbour is not in this hub's cluster. If this hub is the one
		// that dialled and is waiting to be let in, it asks now.
		var state string
		h.db.QueryRow(`SELECT state FROM trunks WHERE name = ?`, tc.name).Scan(&state)
		if state == "pending" && tc.conn.Dialler {
			return h.askToJoin(tc, f.Cluster)
		}
		return nil
	}
	h.trunkUp(tc)
	return nil
}

// askToJoin asks the hub at the other end of a new trunk to let this hub
// into its cluster: this hub's own entry, signed, naming that hub as the
// one it joins through.
func (h *Hub) askToJoin(tc *trunkConn, theirs []wire.ClusterEntry) error {
	c, err := wire.VerifyCluster(theirs)
	if err != nil {
		return err
	}
	via, ok := c.Members[tc.name]
	if !ok || !bytes.Equal(via.SignKey, tc.key) {
		return wire.Errf(wire.EForbidden, "%s is not a member of the cluster it shows.", tc.name)
	}
	if len(h.topology().cluster.Members) > 1 {
		return wire.Errf(wire.EConflict, "This hub is in a cluster already.")
	}
	// An entry is later than the entry of the hub it joins through.
	at := h.now()
	if at <= via.JoinedAt {
		at = via.JoinedAt + 1
	}
	e, err := wire.SignMember(wire.ClusterMember{Cluster: c.ID, Hub: h.id.Name, SignKey: wire.Bytes(h.keys.SignPublic()), URL: h.id.URL, Via: tc.name, JoinedAt: at}, h.keys)
	if err != nil {
		return err
	}
	return tc.send(wire.Frame{Frame: wire.FClusterJoin, ID: tc.conn.NextID(), Cluster: []wire.ClusterEntry{e}})
}

// onClusterJoin lets the hub at the other end of a new trunk into the
// cluster: its entry is countersigned and the roster sent to everyone.
func (h *Hub) onClusterJoin(tc *trunkConn, f wire.Frame) error {
	if len(f.Cluster) != 1 {
		return wire.Errf(wire.EBadFrame, "A hub asks to join with its own entry.")
	}
	e := f.Cluster[0]
	var m wire.ClusterMember
	if err := wire.VerifyBytes(wire.BClusterMember, e.Signed, e.Sig, tc.key, &m); err != nil {
		return err
	}
	t := h.topology()
	me := t.cluster.Members[h.id.Name]
	if m.Hub != tc.name || m.Via != h.id.Name || !bytes.Equal(m.SignKey, tc.key) || !bytes.Equal(m.Cluster, t.cluster.ID) || m.JoinedAt <= me.JoinedAt {
		return wire.Errf(wire.EBadFrame, "The entry does not name this trunk's two hubs.")
	}
	if old, in := t.cluster.Members[m.Hub]; in {
		if old.Via == h.id.Name && bytes.Equal(old.SignKey, m.SignKey) {
			return tc.send(h.clusterFrame()) // let in already; it did not hear
		}
		return wire.Errf(wire.EConflict, "%s is in this cluster already: a second trunk would close a loop.", m.Hub)
	}
	var state string
	h.db.QueryRow(`SELECT state FROM trunks WHERE name = ?`, tc.name).Scan(&state)
	if state != "active" {
		return wire.Errf(wire.EForbidden, "%s has no trunk to this hub.", tc.name)
	}
	e = wire.Countersign(e, h.keys)
	if _, err := wire.VerifyCluster(append(append([]wire.ClusterEntry{}, t.entries...), e)); err != nil {
		return err
	}
	if _, err := h.db.Exec(`INSERT INTO cluster (hub, joined_at, entry) VALUES (?, ?, ?)`, m.Hub, int64(m.JoinedAt), mustMessage("ClusterEntry", e)); err != nil {
		return err
	}
	if err := h.reloadTopology(); err != nil {
		return err
	}
	h.log.Info("a hub has joined the cluster", "hub", m.Hub)
	h.sendClusterToAll()
	h.trunkUp(tc)
	return nil
}

// ------------------------------------------------------------------ clocks

// hubClock is this hub's clock against another hub's: the offsets of the
// trunks between them added up, and their uncertainties added up.
type hubClock struct {
	// offset is this hub's clock minus the other's, in milliseconds.
	offset, uncertainty int64
	// measuredAt is the oldest of the measurements the figure rests on, by
	// this hub's clock; via the neighbour the figure came through.
	measuredAt int64
	via        string
}

// uncertaintyAt is the uncertainty at a later moment: a figure not measured
// for a while is trusted less, by the drift the protocol allows two clocks.
func (c hubClock) uncertaintyAt(now int64) int64 {
	if c.measuredAt == 0 {
		// The hub's own clock against itself: known exactly, and no older
		// for the time that passes. (Aged from a measurement at time zero it
		// came to a day, and the home hub's comparison of an envelope's time
		// with its own clock allowed anything.)
		return c.uncertainty
	}
	return model.PeerClock{Uncertainty: c.uncertainty, MeasuredAt: model.Time(c.measuredAt)}.UncertaintyAt(model.Time(now))
}

// hubClockOf returns this hub's clock against a hub's. Against itself it is
// nothing, known exactly.
func (h *Hub) hubClockOf(hub string) (hubClock, bool) {
	if hub == h.id.Name || hub == "" {
		return hubClock{}, true
	}
	h.mu.Lock()
	c, ok := h.hubClocks[hub]
	h.mu.Unlock()
	return c, ok
}

func (h *Hub) putHubClock(hub string, c hubClock) {
	h.mu.Lock()
	h.hubClocks[hub] = c
	h.mu.Unlock()
	h.db.Exec(`INSERT INTO hub_clocks (hub, offset_ms, uncertainty_ms, measured_at, via) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (hub) DO UPDATE SET offset_ms = excluded.offset_ms, uncertainty_ms = excluded.uncertainty_ms,
			measured_at = excluded.measured_at, via = excluded.via`, hub, c.offset, c.uncertainty, c.measuredAt, c.via)
}

// trunkClock returns what was last measured or told of a trunk's two clocks.
func (h *Hub) trunkClock(name string) *wire.Clock {
	var raw sql.NullString
	if h.db.QueryRow(`SELECT clock FROM trunks WHERE name = ?`, name).Scan(&raw) != nil || !raw.Valid {
		return nil
	}
	var c wire.Clock
	if json.Unmarshal([]byte(raw.String), &c) != nil {
		return nil
	}
	return &c
}

// setTrunkClock keeps a new figure for a trunk. The figure is always the
// dialling hub's clock minus the listening hub's: measured by the one that
// listens, told to the one that dialled. Either way this hub now knows its
// own clock against its neighbour's, and passes on what follows from that.
func (h *Hub) setTrunkClock(name string, c wire.Clock, told bool) {
	h.db.Exec(`UPDATE trunks SET clock = ? WHERE name = ?`, string(mustJSON(c)), name)
	offset := -c.Offset // this hub listened: the figure is the neighbour's clock minus this one's
	if told {
		offset = c.Offset
	}
	h.putHubClock(name, hubClock{offset: offset, uncertainty: c.Uncertainty, measuredAt: int64(h.now()), via: name})
	h.recompose(name)
	h.shareHubClocks()
}

// recompose works out again this hub's clock against every hub that lies
// beyond a neighbour, from what that neighbour last said of them.
func (h *Hub) recompose(neighbour string) {
	near, ok := h.hubClockOf(neighbour)
	if !ok {
		return
	}
	h.mu.Lock()
	said := h.saidClocks[neighbour]
	h.mu.Unlock()
	now := int64(h.now())
	for hub, c := range said {
		if hub == h.id.Name || hub == neighbour {
			continue
		}
		measured := now - c.Age
		if near.measuredAt < measured {
			measured = near.measuredAt
		}
		h.putHubClock(hub, hubClock{offset: near.offset + c.Offset, uncertainty: near.uncertainty + c.Uncertainty, measuredAt: measured, via: neighbour})
	}
}

// shareHubClocks tells each neighbour this hub's clock against every hub
// that is not on that neighbour's side, so that the neighbour can add its
// own trunk's figure and know them too.
func (h *Hub) shareHubClocks() {
	now := int64(h.now())
	t := h.topology()
	for _, tc := range h.liveTrunks() {
		out := map[string]wire.HubClock{}
		var reach []string
		for hub := range t.cluster.Members {
			if hub != h.id.Name && t.next[hub] != tc.name && h.toward(hub) != nil {
				reach = append(reach, hub)
			}
		}
		h.mu.Lock()
		for hub, c := range h.hubClocks {
			if _, member := t.cluster.Members[hub]; !member || t.next[hub] == tc.name || c.via == tc.name {
				continue
			}
			out[hub] = wire.HubClock{Offset: c.offset, Uncertainty: c.uncertainty, Age: now - c.measuredAt}
		}
		h.mu.Unlock()
		tc.send(wire.Frame{Frame: wire.FHubClocks, Clocks: out, Reach: reach})
	}
}

// onHubClocks takes a neighbour's figures for the hubs on its side.
func (h *Hub) onHubClocks(tc *trunkConn, f wire.Frame) {
	h.mu.Lock()
	before := h.saidClocks[tc.name]
	if h.saidClocks == nil {
		h.saidClocks = map[string]map[string]wire.HubClock{}
	}
	h.saidClocks[tc.name] = f.Clocks
	reached := map[string]bool{}
	for _, hub := range f.Reach {
		reached[hub] = true
	}
	reachChanged := len(reached) != len(h.reach[tc.name])
	for hub := range reached {
		reachChanged = reachChanged || !h.reach[tc.name][hub]
	}
	h.reach[tc.name] = reached
	h.mu.Unlock()
	h.recompose(tc.name)
	// Passed on when it says something new: a hub further along adds its
	// own trunk to it.
	changed := len(before) != len(f.Clocks)
	for hub, c := range f.Clocks {
		if old, ok := before[hub]; !ok || old.Offset != c.Offset || old.Uncertainty != c.Uncertainty {
			changed = true
		}
	}
	if changed || reachChanged {
		h.shareHubClocks()
	}
}

// homes is a server's clock against every other hub of the cluster: what
// this hub measured of the server, plus this hub's clock against that hub.
// A server stamps the events of a channel whose home is that hub with it.
func (h *Hub) homes(c wire.Clock) map[string]model.HomeClock {
	t := h.topology()
	if len(t.cluster.Members) < 2 {
		return nil
	}
	now := int64(h.now())
	out := map[string]model.HomeClock{}
	for hub := range t.cluster.Members {
		if hub == h.id.Name {
			continue
		}
		hc, ok := h.hubClockOf(hub)
		if !ok {
			continue
		}
		measured := c.MeasuredAt
		if model.Time(hc.measuredAt) < measured {
			measured = model.Time(hc.measuredAt)
		}
		out[hub] = model.HomeClock{Offset: c.Offset + hc.offset, Uncertainty: c.Uncertainty + hc.uncertaintyAt(now), MeasuredAt: measured}
	}
	return out
}

// noteTrunkAway records, when a trunk comes up, the stretch it was down
// for: from when its clocks were last compared to now.
func (h *Hub) noteTrunkAway(name string) {
	h.trunkAways(name) // read from the database once, for what follows and for the checks on time
	c := h.trunkClock(name)
	hc, ok := h.hubClockOf(name)
	if c == nil || !ok || hc.via != name {
		return // never measured: nothing can have been missed over it
	}
	now := int64(h.now())
	from := hc.measuredAt
	if from > now {
		from = now
	}
	away := append(h.trunkAways(name), [2]int64{from, now})
	if len(away) > awayKept {
		away = away[len(away)-awayKept:]
	}
	h.mu.Lock()
	h.aways[name] = away
	h.mu.Unlock()
	h.db.Exec(`UPDATE trunks SET away = ? WHERE name = ?`, string(mustJSON(away)), name)
}

// trunkAways returns the stretches a trunk was down for: from memory, where
// they are read while a batch is being written, or from the database the
// first time.
func (h *Hub) trunkAways(name string) [][2]int64 {
	h.mu.Lock()
	away, ok := h.aways[name]
	h.mu.Unlock()
	if ok {
		return away
	}
	var raw string
	h.db.QueryRow(`SELECT away FROM trunks WHERE name = ?`, name).Scan(&raw)
	json.Unmarshal([]byte(raw), &away)
	h.mu.Lock()
	h.aways[name] = away
	h.mu.Unlock()
	return away
}

// trunkAway returns the stretch a trunk was down for that holds a time by
// this hub's clock, give or take slack; a stretch stops counting a day
// after it ended.
func (h *Hub) trunkAway(name string, at, slack int64) (from, to int64, ok bool) {
	h.mu.Lock()
	away := h.aways[name] // filled when the trunk came up
	h.mu.Unlock()
	oldest := int64(h.now().Add(-awayLife))
	for _, w := range away {
		if w[1] >= oldest && w[0]-slack <= at && at <= w[1]+slack {
			return w[0], w[1], true
		}
	}
	return 0, 0, false
}

// ------------------------------------------- asking a home hub; forwarding

// askHome carries a server's request about a channel to the channel's home
// hub, which alone may change the channel. If the home hub cannot be reached
// from here the server is told so at once: during a split, changes happen
// only on the side the home hub is on.
func (h *Hub) askHome(server, home string, raw []byte) error {
	tc := h.toward(home)
	if tc == nil {
		return wire.Errf(wire.EUnavailable, "The channel's home hub, %s, cannot be reached from this hub now. Messages still pass among the servers that can reach each other; changes to the channel wait until it can.", home)
	}
	return tc.send(wire.Frame{Frame: wire.FAsk, Hub: home, Server: server, Inner: raw})
}

// onAsk takes a request on its way to a home hub: answered here if this is
// that hub, passed on if not, and refused back to the server that asked if
// the way is cut.
func (h *Hub) onAsk(tc *trunkConn, f wire.Frame) error {
	var inner wire.Frame
	if err := readFrame(f.Inner, &inner); err != nil {
		return wire.Errf(wire.EBadFrame, "An ask carries a frame.")
	}
	refuse := func(err error) {
		h.forward(f.Server, mustFrame(wire.AsError(err).Frame(inner.ID)))
	}
	if f.Hub != h.id.Name {
		if err := h.askHome(f.Server, f.Hub, f.Inner); err != nil {
			refuse(err)
		}
		return nil
	}
	key, ok := h.serverKey(f.Server)
	if !ok || h.hubOf(f.Server) == h.id.Name {
		refuse(wire.Errf(wire.EUnknownServer, "%s is not a server of another hub of this cluster.", f.Server))
		return nil
	}
	var err error
	switch inner.Frame {
	case wire.FChannelAction, wire.FJoin:
		err = h.channelActionAs(f.Server, key, inner)
	case wire.FLeave, wire.FServerJoined, wire.FServerLeft, wire.FOwnerChanged, wire.FStreamClose:
		// A server leaving a channel this hub is the home of, or a control
		// frame of a stream a server owns that was opened here: applied as
		// if the server were on this hub, and checked the same.
		err = h.controlAs(f.Server, key, inner, f.Inner)
	case wire.FChannelCommit:
		err = h.channelCommitAs(f.Server, inner)
	default:
		err = wire.Errf(wire.EBadFrame, "%s is not asked of a home hub.", inner.Frame)
	}
	if err != nil {
		refuse(err)
	}
	return nil
}

// forward carries a frame to one server: sent if the server is on this hub
// and connected, passed toward its hub if it is on another. A word for a
// server of this hub that is not connected is kept and sent when it next
// connects; a direct message is not, and its sender is told (section 7).
func (h *Hub) forward(server string, raw []byte) {
	var inner wire.Frame
	if readFrame(raw, &inner) != nil {
		return
	}
	unavailable := func() {
		if inner.Frame == wire.FDirect && inner.From != server {
			h.forward(inner.From, mustFrame(wire.ErrorFrame(inner.ID, wire.EUnavailable, server+" is not connected.")))
		}
	}
	hub := h.hubOf(server)
	switch {
	case hub == "":
		unavailable()
	case hub != h.id.Name:
		tc := h.toward(hub)
		if tc == nil || tc.send(wire.Frame{Frame: wire.FForward, Server: server, Inner: raw}) != nil {
			unavailable()
		}
	default:
		sc := h.connOf(server)
		if sc == nil {
			if inner.Frame == wire.FDirect {
				unavailable()
				return
			}
			h.tx(func(tx *sql.Tx) error {
				if _, err := tx.Exec(`INSERT INTO held (server, frame, at) VALUES (?, ?, ?)`, server, raw, int64(h.now())); err != nil {
					return err
				}
				_, err := tx.Exec(`DELETE FROM held WHERE server = ? AND id NOT IN (SELECT id FROM held WHERE server = ? ORDER BY id DESC LIMIT ?)`,
					server, server, heldPerServer)
				return err
			})
			return
		}
		if inner.Frame == wire.FDirect {
			out := inner
			out.ID = sc.conn.NextID()
			if sc.conn.Send(out) != nil {
				unavailable()
			}
			return
		}
		sc.conn.SendRaw(raw)
	}
}

// sendHeld sends a server that has just connected the words kept for it.
func (h *Hub) sendHeld(sc *serverConn) {
	rows, err := h.db.Query(`SELECT id, frame FROM held WHERE server = ? ORDER BY id`, sc.name)
	if err != nil {
		return
	}
	var frames [][]byte
	var last int64
	for rows.Next() {
		var raw []byte
		if rows.Scan(&last, &raw) == nil {
			frames = append(frames, raw)
		}
	}
	rows.Close()
	for _, raw := range frames {
		sc.conn.SendRaw(raw)
	}
	if len(frames) > 0 {
		h.db.Exec(`DELETE FROM held WHERE server = ? AND id <= ?`, sc.name, last)
	}
}

// passEphemeral passes typing and presence on: to the stream's servers here
// and to the other trunks that lead to servers in it. It is not logged.
func (h *Hub) passEphemeral(env wire.Envelope, from string) {
	r := h.routeOf(env.StreamID)
	if r == nil {
		return
	}
	key, ok := h.serverKey(env.Origin)
	if from != "" && (!ok || !env.Verify(key)) {
		return
	}
	var local []string
	for _, n := range r.local {
		if n != env.Origin {
			local = append(local, n)
		}
	}
	h.sendEphemeral(local, wire.Frame{Frame: wire.FEphemeral, Envelope: &env})
	for _, name := range r.members {
		if name == from {
			continue
		}
		if tc := h.trunkOf(name); tc != nil && tc.isUp() {
			tc.send(wire.Frame{Frame: wire.FEphemeral, Envelope: &env})
		}
	}
}
