package hub

import (
	"database/sql"
	"encoding/json"

	"github.com/tikaydaes/churr/internal/link/wire"
)

// This file is the streams servers own (ad-hoc channels and sealed chats,
// ruling R59) on a hub and across a cluster of hubs
// (docs/LINK-PROTOCOL.md sections 6, 16 and 23b).
//
// Such a stream is not the hub's. It has no record, no name here, no listing
// and no managers; its owner server signs every change to its server list,
// and a hub only carries it. The hub it was opened on, the owner server's
// own, is its **home**: that hub keeps the list, checks the owner's control
// frames, and is the stream's time reference. A server of another hub of the
// cluster may be in the list. The control frame that says so, as the owner
// signed it, then travels from the home over the trunks toward that server's
// hub in a `stream_ctl` frame; every hub on the way checks the owner's
// signature, keeps its own copy of the list for routing, gives the frame to
// its own servers that it concerns, and carries the stream's envelopes by
// the same rules as a channel's (ship.go). A control frame that reaches a
// hub which is not the home (a server leaving, an owner that has since
// passed to a server elsewhere) is carried to the home in `ask`, as a
// request about a channel is.
//
// What a hub on the way learns is what a hub learns of any stream: its
// identifier, which servers are in it, and sizes. Not what kind of stream it
// is, and nothing in it.

// tell sends a frame to one server wherever it is: on this hub if it is
// connected, or toward its own hub.
func (h *Hub) tell(name string, f wire.Frame) {
	if h.remote(name) {
		h.forward(name, mustFrame(f))
		return
	}
	h.sendTo([]string{name}, f)
}

// floorsOf returns, for a stream a server owns, what the home hub's log held
// of each origin when each of its servers joined: where that server starts
// from. Nil if the hub holds nothing of the kind.
func (h *Hub) floorsOf(q querier, streamID string) map[string]map[string]int64 {
	var raw string
	if q.QueryRow(`SELECT floors FROM stream_ctl WHERE stream_id = ?`, streamID).Scan(&raw) != nil {
		return nil
	}
	out := map[string]map[string]int64{}
	json.Unmarshal([]byte(raw), &out)
	return out
}

// putCtl keeps the owner's last control frame of a stream, as it is relayed,
// and where its servers start from.
func putCtl(tx execer, streamID string, relay wire.Frame, floors map[string]map[string]int64) error {
	if floors == nil {
		floors = map[string]map[string]int64{}
	}
	_, err := tx.Exec(`INSERT INTO stream_ctl (stream_id, frame, floors) VALUES (?, ?, ?)
		ON CONFLICT (stream_id) DO UPDATE SET frame = excluded.frame, floors = excluded.floors`, streamID, mustFrame(relay), string(mustJSON(floors)))
	return err
}

// controlAs applies a stream control frame from a server and relays it. The
// server is on this hub, or on another hub of the cluster and its frame has
// been carried here because this hub is the stream's home.
func (h *Hub) controlAs(actor string, key []byte, f wire.Frame, raw []byte) error {
	var c wire.Control
	// The signer is the server that sent it: the owner, or for leave the
	// server that is leaving.
	if err := wire.VerifyBlock(f, key, &c); err != nil {
		return err
	}
	if !wire.ValidID(c.StreamID) {
		return wire.Errf(wire.EBadFrame, "A control frame names a stream.")
	}
	m := h.lock(c.StreamID)
	defer m.Unlock()
	relay := wire.Frame{Frame: f.Frame, Signed: f.Signed, Sig: f.Sig}
	now := int64(h.now())
	if f.Frame == wire.FStreamOpen {
		if c.Owner != actor || h.remote(actor) {
			return wire.Errf(wire.ENotOwner, "A stream is opened by its owner, on its owner's own hub.")
		}
		lim, err := h.Limits(actor)
		if err != nil {
			return err
		}
		err = h.tx(func(tx *sql.Tx) error {
			var one int
			// The first stream_open fixes the owner. A second is refused.
			if tx.QueryRow(`SELECT 1 FROM streams WHERE id = ?`, c.StreamID).Scan(&one) == nil {
				return wire.Errf(wire.EConflict, "That stream is already open.")
			}
			var owned int64
			tx.QueryRow(`SELECT count(*) FROM streams WHERE owner = ? AND state = 'open'`, actor).Scan(&owned)
			if owned >= lim.Streams {
				return wire.Errf(wire.ERateLimited, "This server owns as many streams as the hub allows.")
			}
			// The hub a stream is opened on is its home, and its time
			// reference from then on.
			if _, err := tx.Exec(`INSERT INTO streams (id, owner, version, next_seq, state, created_at, home_hub) VALUES (?, ?, 1, 1, 'open', ?, ?)`,
				c.StreamID, actor, now, h.id.Name); err != nil {
				return err
			}
			_, err := tx.Exec(`INSERT INTO stream_servers (stream_id, name) VALUES (?, ?)`, c.StreamID, actor)
			return err
		})
		if err != nil {
			return err
		}
		relay.HomeHub = h.id.Name
		h.tell(actor, relay)
		return nil
	}
	st, err := loadStream(h.db, c.StreamID)
	if err != nil {
		return err
	}
	if st.state != "open" {
		return wire.Errf(wire.EUnknownStream, "That stream is closed.")
	}
	if st.homeHub != "" && st.homeHub != h.id.Name {
		// Its home is another hub of the cluster, which keeps its list.
		if st.owner == st.homeHub && (f.Frame != wire.FLeave || c.Server != actor) {
			// A channel that hub owns. Leaving it is asked of that hub;
			// nothing else is a server's to say.
			return wire.Errf(wire.ENotOwner, "The hub %s owns that channel; its changes are made by its managers.", st.homeHub)
		}
		return h.askHome(actor, st.homeHub, raw)
	}
	if st.owner == h.id.Name {
		// A channel the hub owns: no server signs its changes. A server
		// may leave, or give up an invitation or a request to join;
		// everything else is a manager's action.
		if f.Frame != wire.FLeave || c.Server != actor {
			return wire.Errf(wire.ENotOwner, "The hub owns that channel; its changes are made by its managers.")
		}
		err := h.channelLeave(actor, c.StreamID)
		m.Unlock()
		if err == nil {
			h.tellChannels()
		}
		m.Lock() // the deferred unlock
		return err
	}
	// Beside the frame, unsigned: what the log holds of each origin now. A
	// server that is joining starts from there.
	relay.Have, relay.HomeHub = h.haveOf(st.id), st.homeHub
	if f.Frame == wire.FLeave {
		if c.Server != actor || !st.has(actor) || st.owner == actor {
			return wire.Errf(wire.ENotInStream, "A server leaves a stream it is in and does not own.")
		}
		// The hub stops delivering to it and passes the frame to the
		// owner, which announces the new list.
		if _, err := h.db.Exec(`DELETE FROM stream_servers WHERE stream_id = ? AND name = ?`, st.id, actor); err != nil {
			return err
		}
		h.forgetRoute(st.id)
		h.tell(st.owner, relay)
		return nil
	}
	if st.owner != actor || c.Owner != actor {
		return wire.Errf(wire.ENotOwner, "Only the stream's owner may do that.")
	}
	floors := h.floorsOf(h.db, st.id)
	if f.Frame == wire.FStreamClose {
		err := h.tx(func(tx *sql.Tx) error {
			if _, err := tx.Exec(`UPDATE streams SET state = 'closed', closed_at = ? WHERE id = ?`, now, st.id); err != nil {
				return err
			}
			if floors == nil {
				return nil // it never left this hub
			}
			return putCtl(tx, st.id, relay, floors)
		})
		if err != nil {
			return err
		}
		h.sendTo(st.servers, relay)
		h.shipCtl(st.id, relay, st.servers, nil, nil, nil)
		h.forgetRoute(st.id)
		return nil
	}
	if c.Version != st.version+1 {
		return wire.Errf(wire.EConflict, "The server list is at version %d; %d is not the next.", st.version, c.Version)
	}
	var to, gone []string
	switch f.Frame {
	case wire.FServerJoined, wire.FServerLeft:
		if len(c.Servers) > maxStreamServers {
			return wire.Errf(wire.ETooLarge, "A stream has at most %d servers.", maxStreamServers)
		}
		in := map[string]bool{}
		crosses := false
		for _, n := range c.Servers {
			// On this hub, or on another hub of its cluster.
			if _, ok := h.serverKey(n); !ok {
				return wire.Errf(wire.EUnknownServer, "%s is not on this hub or its cluster.", n)
			}
			in[n] = true
			crosses = crosses || h.remote(n)
		}
		if !in[st.owner] || in[c.Server] != (f.Frame == wire.FServerJoined) {
			return wire.Errf(wire.EBadFrame, "The server list does not match the change.")
		}
		if floors != nil || crosses || h.remote(c.Server) {
			// The stream crosses a trunk, or did: where each server starts
			// from is kept, so that a hub which carries it for a server
			// that joined ships that server what came after, and no more.
			if floors == nil {
				floors = map[string]map[string]int64{}
			}
			for name := range floors {
				if !in[name] {
					delete(floors, name)
				}
			}
			if f.Frame == wire.FServerJoined {
				floors[c.Server] = relay.Have
			}
		}
		err = h.tx(func(tx *sql.Tx) error {
			if _, err := tx.Exec(`DELETE FROM stream_servers WHERE stream_id = ?`, st.id); err != nil {
				return err
			}
			for n := range in {
				if _, err := tx.Exec(`INSERT INTO stream_servers (stream_id, name) VALUES (?, ?)`, st.id, n); err != nil {
					return err
				}
			}
			if _, err := tx.Exec(`UPDATE streams SET version = ? WHERE id = ?`, c.Version, st.id); err != nil {
				return err
			}
			if floors == nil {
				return nil
			}
			return putCtl(tx, st.id, relay, floors)
		})
		if err != nil {
			return err
		}
		// To every server in the stream, to a server just added and to a
		// server just removed.
		to = append(to, c.Servers...)
		if !in[c.Server] {
			to, gone = append(to, c.Server), []string{c.Server}
		}
	case wire.FOwnerChanged:
		if !st.has(c.NewOwner) || c.NewOwner == h.id.Name {
			return wire.Errf(wire.ENotInStream, "Ownership goes to a server in the stream, never to a hub.")
		}
		err = h.tx(func(tx *sql.Tx) error {
			if _, err := tx.Exec(`UPDATE streams SET owner = ?, version = ? WHERE id = ?`, c.NewOwner, c.Version, st.id); err != nil {
				return err
			}
			if floors == nil {
				return nil
			}
			return putCtl(tx, st.id, relay, floors)
		})
		if err != nil {
			return err
		}
		to = st.servers
	}
	h.forgetRoute(st.id)
	h.sendTo(to, relay) // those on this hub; the others' own hubs give it to them
	h.shipCtl(st.id, relay, to, gone, floors, nil)
	return nil
}

// shipCtl sends a control frame of a stream a server owns over every trunk
// that leads to a server it concerns, and starts the shipping of the
// stream's envelopes over any trunk its list has just brought in. from, if
// not nil, is the trunk it arrived over, which is not sent it again.
func (h *Hub) shipCtl(streamID string, relay wire.Frame, concerned, gone []string, floors map[string]map[string]int64, from *trunkConn) {
	for _, tc := range h.liveTrunks() {
		if tc == from {
			h.arm(tc, streamID)
			continue
		}
		if behind := h.behind(tc.name, concerned); len(behind) > 0 {
			out := wire.Frame{Frame: wire.FStreamCtl, Inner: mustFrame(relay), Also: h.behind(tc.name, gone), JoinedHave: map[string]map[string]int64{}}
			for _, name := range behind {
				if at, ok := floors[name]; ok {
					out.JoinedHave[name] = at
				}
			}
			tc.send(out)
		}
		h.arm(tc, streamID)
	}
}

// sendStreamsTo sends a neighbour whose trunk has just come up the last
// control frame of every stream a server owns that its side has a server
// of: it may have been away when the list changed.
func (h *Hub) sendStreamsTo(tc *trunkConn) {
	rows, err := h.db.Query(`SELECT c.stream_id, c.frame, c.floors FROM stream_ctl c JOIN streams s ON s.id = c.stream_id`)
	if err != nil {
		return
	}
	type row struct {
		id     string
		frame  []byte
		floors string
	}
	var all []row
	for rows.Next() {
		var r row
		if rows.Scan(&r.id, &r.frame, &r.floors) == nil {
			all = append(all, r)
		}
	}
	rows.Close()
	t := h.topology()
	for _, r := range all {
		var relay wire.Frame
		var c wire.Control
		if readFrame(r.frame, &relay) != nil || wire.PeekBlock(relay, &c) != nil {
			continue
		}
		if relay.HomeHub != h.id.Name && t.next[relay.HomeHub] == tc.name {
			continue // that is where it came from
		}
		st, err := loadStream(h.db, r.id)
		if err != nil {
			continue
		}
		behind := h.behind(tc.name, append(append([]string{}, st.servers...), c.Servers...))
		if len(behind) == 0 {
			continue
		}
		floors := map[string]map[string]int64{}
		json.Unmarshal([]byte(r.floors), &floors)
		out := wire.Frame{Frame: wire.FStreamCtl, Inner: r.frame, JoinedHave: map[string]map[string]int64{}}
		for _, name := range behind {
			if at, ok := floors[name]; ok {
				out.JoinedHave[name] = at
			}
		}
		tc.send(out)
	}
}

// onStreamCtl takes, over a trunk, the owner server's control frame of a
// stream that has a server on this side. The frame must be signed by the
// server it names as owner, and come from the side the stream's home hub is
// on; that hub checked that the signer is the owner and that the version is
// the next. This hub keeps its own copy of the list, gives the frame to its
// own servers that it concerns, and passes it on.
func (h *Hub) onStreamCtl(tc *trunkConn, f wire.Frame) error {
	var relay wire.Frame
	var c wire.Control
	if readFrame(f.Inner, &relay) != nil || wire.PeekBlock(relay, &c) != nil || !wire.ValidID(c.StreamID) {
		return wire.Errf(wire.EBadFrame, "A stream_ctl carries a stream's control frame.")
	}
	switch relay.Frame {
	case wire.FServerJoined, wire.FServerLeft, wire.FOwnerChanged, wire.FStreamClose:
	default:
		return wire.Errf(wire.EBadFrame, "%s does not cross a trunk.", relay.Frame)
	}
	key, ok := h.serverKey(c.Owner)
	if !ok {
		return wire.Errf(wire.EUnknownServer, "%s is not a server of this cluster.", c.Owner)
	}
	if err := wire.VerifyBlock(relay, key, &c); err != nil {
		return err
	}
	t := h.topology()
	home := relay.HomeHub
	if _, member := t.cluster.Members[home]; !member || home == h.id.Name || t.next[home] != tc.name {
		return wire.Errf(wire.EForbidden, "%s is not a hub on that side of this one.", home)
	}
	if len(c.Servers) > maxStreamServers {
		return wire.Errf(wire.ETooLarge, "A stream has at most %d servers.", maxStreamServers)
	}
	m := h.lock(c.StreamID)
	defer m.Unlock()
	var before []string
	applied := false
	floors := map[string]map[string]int64{}
	err := h.tx(func(tx *sql.Tx) error {
		st, err := loadStream(tx, c.StreamID)
		exists := err == nil
		if exists {
			before = st.servers
			switch {
			case st.homeHub != home || st.owner == st.homeHub:
				return wire.Errf(wire.EConflict, "That identifier is another stream here.")
			case st.state != "open":
				return nil
			case relay.Frame != wire.FStreamClose && c.Version <= st.version:
				return nil // held already
			}
			if kept := h.floorsOf(tx, c.StreamID); kept != nil {
				floors = kept
			}
		} else {
			if relay.Frame != wire.FServerJoined && relay.Frame != wire.FServerLeft {
				return nil // nothing here to change or close
			}
			if _, err := tx.Exec(`INSERT INTO streams (id, owner, version, next_seq, state, created_at, home_hub) VALUES (?, ?, 0, 1, 'open', ?, ?)`,
				c.StreamID, c.Owner, int64(h.now()), home); err != nil {
				return err
			}
		}
		switch relay.Frame {
		case wire.FServerJoined, wire.FServerLeft:
			if _, err := tx.Exec(`DELETE FROM stream_servers WHERE stream_id = ?`, c.StreamID); err != nil {
				return err
			}
			for _, n := range c.Servers {
				if _, err := tx.Exec(`INSERT OR IGNORE INTO stream_servers (stream_id, name) VALUES (?, ?)`, c.StreamID, n); err != nil {
					return err
				}
			}
			if _, err := tx.Exec(`UPDATE streams SET owner = ?, version = ? WHERE id = ?`, c.Owner, c.Version, c.StreamID); err != nil {
				return err
			}
			for name := range floors {
				if !has(c.Servers, name) {
					delete(floors, name)
				}
			}
			for name, at := range f.JoinedHave {
				if has(c.Servers, name) {
					floors[name] = at
				}
			}
		case wire.FOwnerChanged:
			if _, err := tx.Exec(`UPDATE streams SET owner = ?, version = ? WHERE id = ?`, c.NewOwner, c.Version, c.StreamID); err != nil {
				return err
			}
		case wire.FStreamClose:
			if _, err := tx.Exec(`UPDATE streams SET state = 'closed', closed_at = ? WHERE id = ?`, int64(h.now()), c.StreamID); err != nil {
				return err
			}
		}
		applied = true
		return putCtl(tx, c.StreamID, relay, floors)
	})
	if err != nil || !applied {
		return err
	}
	h.forgetRoute(c.StreamID)
	// Whom it concerns: the list it announces, a server it takes out, and
	// for a change of owner or a close everyone the stream had here.
	concerned := append(append([]string{}, c.Servers...), f.Also...)
	if relay.Frame == wire.FOwnerChanged || relay.Frame == wire.FStreamClose {
		concerned = append(concerned, before...)
	}
	told := map[string]bool{}
	for _, name := range concerned {
		if !told[name] && !h.remote(name) {
			told[name] = true
			h.sendTo([]string{name}, relay)
		}
	}
	h.shipCtl(c.StreamID, relay, concerned, f.Also, floors, tc)
	return nil
}
