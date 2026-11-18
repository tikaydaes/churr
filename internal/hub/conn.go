package hub

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/tikaydaes/churr/internal/link/wire"
	"github.com/tikaydaes/churr/internal/ratelimit"
)

// mustJSON writes one of the hub's own notes (who invited whom, a clock's
// figures) as it keeps them in its database. Nothing that travels is JSON.
func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic("hub: " + err.Error())
	}
	return b
}

// mustFrame writes a frame the hub built itself as the bytes that travel.
// Its identifiers were read off the wire or made here, so it cannot fail.
func mustFrame(f wire.Frame) []byte {
	b, err := wire.EncodeFrame(f)
	if err != nil {
		panic("hub: " + err.Error())
	}
	return b
}

// readFrame reads the bytes of a frame, as kept or as carried inside
// another.
func readFrame(raw []byte, f *wire.Frame) error {
	got, err := wire.DecodeFrame(raw)
	*f = got
	return err
}

// mustEnvelope writes an envelope as the log keeps it and as a deliver
// frame carries it: an Envelope message.
func mustEnvelope(env wire.Envelope) []byte {
	b, err := wire.MarshalEnvelope(env)
	if err != nil {
		panic("hub: " + err.Error())
	}
	return b
}

// readEnvelope reads an envelope out of the log.
func readEnvelope(raw []byte, env *wire.Envelope) error {
	got, err := wire.UnmarshalEnvelope(raw)
	*env = got
	return err
}

// mustMessage writes one of the link's messages the hub keeps as bytes: a
// channel's record, an entry of the cluster roster.
func mustMessage(name string, v any) []byte {
	b, err := wire.Marshal(name, v)
	if err != nil {
		panic("hub: " + err.Error())
	}
	return b
}

// serveLink is /link/v1: a server dialling the hub.
func (h *Hub) serveLink(w http.ResponseWriter, r *http.Request) {
	conn, err := wire.AcceptKinds(w, r, h.id, func(d wire.Frame, proof, th []byte) error {
		if d.Kind == wire.KindHub {
			return h.authorizeTrunk(d, proof, th)
		}
		return h.authorize(d, proof, th)
	}, wire.KindServer, wire.KindHub)
	if err != nil {
		return
	}
	if conn.Remote.Kind == wire.KindHub {
		// Another hub: a trunk (trunk.go).
		h.serveTrunk(r, conn)
		return
	}
	if conn.Remote.Name == h.id.Name {
		// The hub's own holder (holder.go).
		h.serveHolder(r, conn)
		return
	}
	sc := &serverConn{name: conn.Remote.Name, conn: conn, key: conn.Remote.SignKey}
	// What the hub sends a server is written by the connection's own
	// goroutine, in the order it was sent: a server whose socket is slow
	// holds up nobody else's envelopes (group.go).
	conn.Queue(sendQueueBytes)
	// The stretch the server was away for is noted before anything of its
	// is read: what it made in that time arrives now, with old times.
	h.noteAway(sc.name)
	h.mu.Lock()
	old := h.conns[sc.name]
	h.conns[sc.name] = sc
	h.mu.Unlock()
	if old != nil {
		old.conn.CloseWith(wire.ErrorFrame(0, wire.EReplaced, "A newer connection for this server took over."))
	}
	defer func() {
		h.mu.Lock()
		if h.conns[sc.name] == sc {
			delete(h.conns, sc.name)
		}
		h.mu.Unlock()
		conn.Close()
	}()
	go conn.Watch()
	// The hub is the time reference of every stream it is the home of: it
	// measures each server's clock in the connection's ping and pong, and
	// tells the server its offset on the next ping.
	sc.pinger = wire.NewPinger(conn, func() int64 { return int64(h.now()) }, wire.NewClockTrack(h.cfg.ClockSynced, h.clockOf(sc.name)),
		func(c wire.Clock) { h.setClock(sc.name, c) })
	// With each ping the server is told its clock against the hub's and,
	// in a cluster, against every other hub's: a channel is stamped by its
	// home hub's clock, wherever that hub is.
	sc.pinger.Extra = func(f *wire.Frame) { f.Homes = h.homes(*f.Clock) }
	go sc.pinger.Run()
	// After ready a hub sends its roster, and the cluster's: a record of a
	// channel whose home is another hub is checked against it.
	h.sendRoster(sc)
	conn.Send(h.clusterFrame())
	h.sendHeld(sc)
	h.resendChannels(sc)
	h.resendStopped(sc)
	h.Reconcile()
	ctx := r.Context()
	for {
		f, raw, err := conn.Read(ctx)
		if err != nil {
			var bad *wire.Error
			if errors.As(err, &bad) {
				conn.Send(bad.Frame(0))
				continue
			}
			return
		}
		if tap := h.testTap.Load(); tap != nil {
			(*tap)(sc.name, raw)
		}
		switch f.Frame {
		case wire.FBye:
			return
		case wire.FPong:
			sc.pinger.Pong(f)
			continue
		case wire.FPing:
			// A server has no use for the hub's clock, but an answer costs
			// nothing and keeps a careful one from thinking the hub dead.
			t1 := int64(h.now())
			conn.AnswerPing(f, t1, func() int64 { return int64(h.now()) })
			continue
		}
		if err := h.dispatch(sc, f, raw); err != nil {
			e := wire.AsError(err)
			if e.Code == wire.EInternal {
				h.log.Warn("a frame could not be handled", "frame", f.Frame, "err", err)
			}
			h.count(sc.name, 0, 0, 1)
			conn.Send(e.Frame(f.ID))
		}
	}
}

// authorize is what the hub checks on a dialler whose signatures have
// verified: a server already joined shows its pinned keys; a new one proves
// an invitation that is unused, not expired, and for its name if it names
// one.
func (h *Hub) authorize(d wire.Frame, proof, th []byte) error {
	if d.Name == h.id.Name {
		if h.isHolder(d) {
			return nil // the hub's own holder, which holds the hub's keys
		}
		return wire.Errf(wire.EAuthFailed, "That is the hub's own name.")
	}
	var signKey, boxKey []byte
	var state string
	err := h.db.QueryRow(`SELECT sign_key, box_key, state FROM servers WHERE name = ?`, d.Name).Scan(&signKey, &boxKey, &state)
	switch {
	case err == nil && state != "active":
		return wire.Errf(wire.EBlocked, "This server has been removed from the hub.")
	case err == nil:
		if !bytes.Equal(signKey, d.SignKey) || !bytes.Equal(boxKey, d.BoxKey) {
			return wire.Errf(wire.EKeyMismatch, "%s is pinned with other keys.", d.Name)
		}
		return nil
	case !errors.Is(err, sql.ErrNoRows):
		return err
	}
	// A name is on one hub of a cluster.
	if rs, taken := h.topology().servers[d.Name]; taken {
		return wire.Errf(wire.EKeyMismatch, "%s is on %s, another hub of this cluster.", d.Name, rs.hub)
	}
	now := int64(h.now())
	rows, err := h.db.Query(`SELECT id, hash, for_name, expires_at FROM invites WHERE kind = 'server'`)
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
			return wire.Errf(wire.EInviteInvalid, "That invitation is for another server.")
		}
		match = &all[i]
	}
	if match == nil {
		if expired {
			return wire.Errf(wire.EInviteExpired, "That invitation has expired.")
		}
		return wire.Errf(wire.EInviteInvalid, "No invitation matches.")
	}
	// The invitation is used up and the keys are pinned.
	return h.tx(func(tx *sql.Tx) error {
		res, err := tx.Exec(`DELETE FROM invites WHERE id = ?`, match.id)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return wire.Errf(wire.EInviteInvalid, "That invitation has been used.")
		}
		if _, err := tx.Exec(`INSERT INTO servers (name, sign_key, box_key, box_key_sig, joined_at, state) VALUES (?, ?, ?, ?, ?, 'active')`,
			d.Name, []byte(d.SignKey), []byte(d.BoxKey), []byte(d.BoxKeySig), now); err != nil {
			return err
		}
		return bumpRoster(tx)
	})
}

// limits returns the limiters of one server, made from its limits.
func (h *Hub) limits(name string) *serverLimits {
	h.mu.Lock()
	l := h.limiters[name]
	h.mu.Unlock()
	if l != nil {
		return l
	}
	// The database is read with the hub's lock let go: a transaction takes
	// that lock for a moment (a route forgotten, a count), and the database
	// has one connection, so reading it under the lock could stop the hub.
	lim, _ := h.Limits(name)
	l = &serverLimits{
		submits:   ratelimit.New(ratelimit.Per(lim.Submits, time.Minute), lim.Submits),
		ephemeral: ratelimit.New(ratelimit.Per(defaultEphemeral, time.Minute), defaultEphemeral),
		direct:    ratelimit.New(ratelimit.Per(defaultDirect, time.Minute), defaultDirect),
		blobBytes: lim.BlobBytes, perMinute: lim.Submits,
	}
	h.mu.Lock()
	if made := h.limiters[name]; made != nil {
		l = made // another connection's frame got there first
	} else {
		h.limiters[name] = l
	}
	h.mu.Unlock()
	return l
}

// stream is a row of streams with its servers.
type stream struct {
	id, owner, state string
	version, nextSeq int64
	servers          []string
	// homeHub is the hub that is the stream's home and its time reference:
	// the hub it was opened on. epoch is the highest key epoch the owner
	// has used.
	homeHub string
	epoch   int64
}

func (s stream) has(name string) bool {
	for _, n := range s.servers {
		if n == name {
			return true
		}
	}
	return false
}

type querier interface {
	QueryRow(query string, args ...any) *sql.Row
	Query(query string, args ...any) (*sql.Rows, error)
}

func loadStream(q querier, id string) (stream, error) {
	s := stream{id: id}
	err := q.QueryRow(`SELECT owner, version, next_seq, state, home_hub, epoch FROM streams WHERE id = ?`, id).
		Scan(&s.owner, &s.version, &s.nextSeq, &s.state, &s.homeHub, &s.epoch)
	if errors.Is(err, sql.ErrNoRows) {
		return s, wire.Errf(wire.EUnknownStream, "No such stream here.")
	}
	if err != nil {
		return s, err
	}
	rows, err := q.Query(`SELECT name FROM stream_servers WHERE stream_id = ? ORDER BY name`, id)
	if err != nil {
		return s, err
	}
	defer rows.Close()
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return s, err
		}
		s.servers = append(s.servers, n)
	}
	return s, rows.Err()
}

// lock takes the lock of one stream: one envelope is taken into its log at a
// time, and its deliver frames leave in that order.
func (h *Hub) lock(id string) *sync.Mutex {
	h.mu.Lock()
	m := h.streams[id]
	if m == nil {
		m = &sync.Mutex{}
		h.streams[id] = m
	}
	h.mu.Unlock()
	m.Lock()
	return m
}

func (h *Hub) connOf(name string) *serverConn {
	h.mu.Lock()
	defer h.mu.Unlock()
	if name == h.id.Name {
		return h.held // the hub's own holder (holder.go)
	}
	return h.conns[name]
}

// sendTo sends a frame to each named server that is connected.
func (h *Hub) sendTo(names []string, f wire.Frame) {
	for _, n := range names {
		if c := h.connOf(n); c != nil {
			c.conn.Send(f)
		}
	}
}

// keyOf returns the pinned signing key of an active server.
func (h *Hub) keyOf(name string) ([]byte, bool) { return keyIn(h.db, name) }

// keyIn is keyOf inside a transaction. The hub's database has one
// connection, so a transaction must read through itself.
func keyIn(q querier, name string) ([]byte, bool) {
	var key []byte
	err := q.QueryRow(`SELECT sign_key FROM servers WHERE name = ? AND state = 'active'`, name).Scan(&key)
	return key, err == nil
}

func (h *Hub) dispatch(sc *serverConn, f wire.Frame, raw []byte) error {
	if drop := h.testDrop.Load(); drop != nil && (*drop)(sc.name, f) {
		return nil // a test is playing a hub that loses this frame
	}
	maxFrame := wire.MaxFrameBytes
	if h.cfg.MaxEnvelope > 0 && h.cfg.MaxEnvelope < maxFrame {
		maxFrame = h.cfg.MaxEnvelope
	}
	if len(raw) > maxFrame {
		return wire.Errf(wire.ETooLarge, "A frame is at most %d bytes.", maxFrame)
	}
	switch f.Frame {
	case wire.FStreamOpen, wire.FServerJoined, wire.FServerLeft, wire.FLeave, wire.FOwnerChanged, wire.FStreamClose:
		return h.control(sc, f, raw)
	case wire.FChannelCreate:
		return h.channelCreate(sc, f)
	case wire.FChannelCommit:
		// The commit of a move onto another hub of the cluster, handed over
		// by the server of the two that is on this one: that hub's to take.
		var mc wire.MoveCommit
		if wire.PeekBlock(f, &mc) == nil && mc.Hub != h.id.Name && mc.Hub != "" {
			if _, member := h.topology().cluster.Members[mc.Hub]; member {
				return h.askHome(sc.name, mc.Hub, raw)
			}
		}
		return h.channelCommitAs(sc.name, f)
	case wire.FChannelAction, wire.FJoin:
		return h.channelAction(sc, f, raw)
	case wire.FDirectory:
		return h.directory(sc, f)
	case wire.FSubmit:
		return h.onSubmit(sc, f)
	case wire.FCatchUp:
		return h.catchUp(sc, f)
	case wire.FAck:
		return nil // the log is what a server that fell behind is served from
	case wire.FEphemeral:
		return h.ephemeral(sc, f)
	case wire.FDirect:
		return h.direct(sc, f, raw)
	case wire.FError:
		return nil
	}
	return wire.Errf(wire.EBadFrame, "Unknown frame %q.", f.Frame)
}

// control applies a stream control frame and relays it (section 6). The hub
// learns from these which servers are in which stream, and nothing else.
func (h *Hub) control(sc *serverConn, f wire.Frame, raw []byte) error {
	return h.controlAs(sc.name, sc.key, f, raw)
}

// ephemeral passes typing and presence to the stream's other servers that
// are connected. It is not stamped, not logged and not kept for anyone.
func (h *Hub) ephemeral(sc *serverConn, f wire.Frame) error {
	if f.Envelope == nil || f.Envelope.N != 0 || f.Envelope.At != 0 {
		return wire.Errf(wire.EBadFrame, "An ephemeral frame carries one envelope without a stamp.")
	}
	// Under a burst from every server at once the frame is dropped here,
	// before any work is spent on it (ephemeral.go).
	if !h.takeEphemeral() {
		return nil
	}
	env, st, err := h.checkEnvelope(sc, *f.Envelope)
	if err != nil {
		return err
	}
	if r := h.limits(sc.name).ephemeral.Take(sc.name); !r.OK {
		e := wire.Errf(wire.ERateLimited, "Too many ephemeral frames.")
		e.RetryAfter = max(r.RetryAfterSeconds(), 1)
		h.refused.Note("ephemeral frames", sc.name, e.RetryAfter)
		return e
	}
	if st.homeHub == "" || len(h.topology().cluster.Members) < 2 {
		var others []string
		for _, n := range st.servers {
			if n != sc.name {
				others = append(others, n)
			}
		}
		sort.Strings(others)
		h.sendEphemeral(others, wire.Frame{Frame: wire.FEphemeral, Envelope: &env})
		return nil
	}
	// In a cluster: to the stream's servers here, and over the trunks that
	// lead to its servers elsewhere.
	h.passEphemeral(env, "")
	return nil
}

// takeDirect counts a direct message against its sender's limits: a file
// chunk (judged by its size, since the hub cannot see what a direct message
// is) against the file bytes of the hour, anything else against the direct
// messages of the minute.
func (h *Hub) takeDirect(sc *serverConn, f wire.Frame) error {
	lim := h.limits(sc.name)
	if len(f.Body) >= chunkThreshold {
		hour := int64(h.now()) / 3600000
		h.mu.Lock()
		if lim.blobHour != hour {
			lim.blobHour, lim.blobUsed = hour, 0
		}
		over := lim.blobUsed+int64(len(f.Body)) > lim.blobBytes
		if !over {
			lim.blobUsed += int64(len(f.Body))
		}
		h.mu.Unlock()
		if over {
			e := wire.Errf(wire.ERateLimited, "Too many file bytes this hour.")
			e.RetryAfter = 60
			h.refused.Note("file bytes", sc.name, e.RetryAfter)
			return e
		}
	} else if r := lim.direct.Take(sc.name); !r.OK {
		e := wire.Errf(wire.ERateLimited, "Too many direct messages.")
		e.RetryAfter = max(r.RetryAfterSeconds(), 1)
		h.refused.Note("direct messages", sc.name, e.RetryAfter)
		return e
	}
	return nil
}

// direct passes a sealed direct message on unchanged. The hub checks who it
// is from and that the signature is theirs; it sees the two names and the
// size, and cannot open the body. It never holds one for later. One
// addressed to the hub itself it opens (keys.go); one addressed to another
// hub of the cluster is carried there in `ask`.
func (h *Hub) direct(sc *serverConn, f wire.Frame, raw []byte) error {
	if f.From != sc.name {
		return wire.Errf(wire.EBadFrame, "A direct message is sent by the server it is from.")
	}
	if !wire.VerifyDirect(f, sc.key) {
		return wire.Errf(wire.EBadSignature, "The direct message's signature does not verify.")
	}
	if f.To == h.id.Name {
		// The hub's own: a key asked for, or history or a file for its
		// holder (keys.go), counted as any direct message is.
		if err := h.takeDirect(sc, f); err != nil {
			return err
		}
		return h.ownDirect(sc.name, sc.key, f)
	}
	if _, member := h.topology().cluster.Members[f.To]; member {
		return h.askHome(sc.name, f.To, raw)
	}
	there := h.hubOf(f.To)
	if there == "" {
		return wire.Errf(wire.EUnknownServer, "%s is not on this hub or its cluster.", f.To)
	}
	if err := h.takeDirect(sc, f); err != nil {
		return err
	}
	if there != h.id.Name {
		// A server of another hub of the cluster: the message is carried
		// to its hub, unopened as ever. If it cannot be delivered there the
		// sender is told, as it is here.
		tc := h.toward(there)
		if tc == nil || tc.send(wire.Frame{Frame: wire.FForward, Server: f.To, Inner: raw}) != nil {
			return wire.Errf(wire.EUnavailable, "%s is on %s, which cannot be reached now.", f.To, there)
		}
		h.count(sc.name, 0, int64(len(raw)), 0)
		return nil
	}
	to := h.connOf(f.To)
	if to == nil {
		return wire.Errf(wire.EUnavailable, "%s is not connected.", f.To)
	}
	h.count(sc.name, 0, int64(len(raw)), 0)
	fwd := wire.Frame{Frame: wire.FDirect, ID: to.conn.NextID(), From: f.From, To: f.To, Body: f.Body, Sig: f.Sig}
	if err := to.conn.Send(fwd); err != nil {
		return wire.Errf(wire.EUnavailable, "%s is not connected.", f.To)
	}
	return nil
}
