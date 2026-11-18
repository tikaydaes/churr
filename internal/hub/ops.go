package hub

import (
	"database/sql"
	"errors"
	"fmt"
	"html"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/tikaydaes/churr/internal/link/wire"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/secret"
	"github.com/tikaydaes/churr/internal/version"
)

// This file is the operator's side of the hub: the commands of section 23,
// which work on the database and so can be run by another process while the
// hub serves, the tick that carries their effects to the connected servers,
// and the status page.

// roster builds the signed roster frame: every active server and its keys.
func (h *Hub) roster() (wire.Frame, int64, error) {
	ro := wire.Roster{Hub: h.id.Name, Version: h.meta("roster_version"), At: h.now(), Servers: []wire.RosterServer{}}
	rows, err := h.db.Query(`SELECT name, sign_key, box_key, box_key_sig, joined_at FROM servers WHERE state = 'active' ORDER BY name`)
	if err != nil {
		return wire.Frame{}, 0, err
	}
	defer rows.Close()
	for rows.Next() {
		var rs wire.RosterServer
		var sign, box, sig []byte
		var joined int64
		if err := rows.Scan(&rs.Name, &sign, &box, &sig, &joined); err != nil {
			return wire.Frame{}, 0, err
		}
		rs.SignKey, rs.BoxKey, rs.BoxKeySig, rs.JoinedAt = sign, box, sig, model.Time(joined)
		ro.Servers = append(ro.Servers, rs)
	}
	// And the servers of the cluster's other hubs: a server shares
	// channels with them, seals keys to them and checks their signatures,
	// exactly as with the servers of its own hub.
	t := h.topology()
	for name, rs := range t.servers {
		ro.Servers = append(ro.Servers, wire.RosterServer{Name: name, SignKey: rs.signKey, BoxKey: rs.boxKey, BoxKeySig: rs.boxKeySig, JoinedAt: model.Time(rs.joinedAt)})
	}
	sort.Slice(ro.Servers, func(i, j int) bool { return ro.Servers[i].Name < ro.Servers[j].Name })
	f, err := wire.SignBlock(wire.FRoster, ro, h.keys)
	return f, ro.Version, err
}

func (h *Hub) sendRoster(sc *serverConn) {
	if f, _, err := h.roster(); err == nil {
		sc.conn.Send(f)
	}
}

// Reconcile carries what has changed in the database to the connected
// servers: a new roster to everyone, the door for a server that was removed,
// and an unsigned stream_close for each stream whose owner was removed. The
// serving hub runs it every second, since the change may have been made by
// an operator command in another process.
func (h *Hub) Reconcile() {
	// The cluster first: a trunk to dial, a hub let go.
	if v := h.meta("cluster_version") + 1; v != h.seenCluster.Swap(v) {
		h.applyClusterChange()
	}
	version := h.meta("roster_version")
	h.mu.Lock()
	changed := version != h.sentRoster
	h.sentRoster = version
	conns := make([]*serverConn, 0, len(h.conns))
	for _, c := range h.conns {
		conns = append(conns, c)
	}
	if changed {
		h.limiters = map[string]*serverLimits{} // limits may have changed too
		h.signKeys = map[string][]byte{}
		h.routes = map[string]*route{}
	}
	h.mu.Unlock()
	if changed {
		// The other hubs of the cluster are told this hub's servers.
		if own, err := h.ownServers(); err == nil {
			for _, tc := range h.liveTrunks() {
				tc.send(own)
				h.armAll(tc)
			}
		}
		f, _, err := h.roster()
		for _, c := range conns {
			if _, active := h.keyOf(c.name); !active {
				c.conn.CloseWith(wire.ErrorFrame(0, wire.EBlocked, "This server has been removed from the hub."))
				continue
			}
			if err == nil {
				c.conn.Send(f)
			}
		}
	}
	h.doRehomes()
	h.tellChannels()
	// A stream the removed server owned has nobody to run it: the hub
	// marks it closed and tells its servers, naming the reason.
	rows, err := h.db.Query(`SELECT id FROM streams WHERE told = 0`)
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
		st, err := loadStream(h.db, id)
		if err != nil {
			continue
		}
		h.sendTo(st.servers, wire.Frame{Frame: wire.FStreamClose, StreamID: id, Reason: "owner_gone"})
		h.db.Exec(`UPDATE streams SET told = 1 WHERE id = ?`, id)
	}
}

// Invite makes an invitation to the hub and returns the string to hand to a
// server's owner. forName, if not empty, is the one server name that may use
// it. expires of 0 means seven days.
func (h *Hub) Invite(forName string, expires time.Duration) (string, error) {
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
	_, err := h.db.Exec(`INSERT INTO invites (id, hash, for_name, expires_at, created_at) VALUES (?, ?, ?, ?, ?)`,
		model.NewID[string](), wire.InviteHash(raw), name, int64(inv.ExpiresAt), int64(h.now()))
	return inv.String(), err
}

// InviteInfo is an invitation not yet used.
type InviteInfo struct {
	ID        string
	For       string
	ExpiresAt model.Time
}

// Invites lists invitations not yet used or expired.
func (h *Hub) Invites() ([]InviteInfo, error) {
	rows, err := h.db.Query(`SELECT id, COALESCE(for_name, ''), expires_at FROM invites WHERE expires_at >= ? ORDER BY created_at`, int64(h.now()))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []InviteInfo
	for rows.Next() {
		var in InviteInfo
		var exp int64
		if err := rows.Scan(&in.ID, &in.For, &exp); err != nil {
			return nil, err
		}
		in.ExpiresAt = model.Time(exp)
		out = append(out, in)
	}
	return out, rows.Err()
}

// RevokeInvite cancels an invitation.
func (h *Hub) RevokeInvite(id string) error {
	res, err := h.db.Exec(`DELETE FROM invites WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errors.New("there is no such invitation")
	}
	return nil
}

// ServerInfo is a server as `churr-hub servers` lists it.
type ServerInfo struct {
	Name        string
	Fingerprint string
	Connected   bool
	Streams     int64
	Envelopes   int64 // in the last day
	Bytes       int64
	JoinedAt    model.Time
	// Clock is what the hub last measured of the server's clock, or nil.
	Clock *wire.Clock
}

// Servers lists the servers on the hub.
func (h *Hub) Servers() ([]ServerInfo, error) {
	since := int64(h.now())/3600000 - 24
	rows, err := h.db.Query(`
		SELECT s.name, s.sign_key, s.joined_at,
			(SELECT count(*) FROM stream_servers ss JOIN streams st ON st.id = ss.stream_id WHERE ss.name = s.name AND st.state = 'open'),
			COALESCE((SELECT sum(envelopes) FROM counters c WHERE c.name = s.name AND c.hour >= ?), 0),
			COALESCE((SELECT sum(bytes) FROM counters c WHERE c.name = s.name AND c.hour >= ?), 0)
		FROM servers s WHERE s.state = 'active' ORDER BY s.name`, since, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ServerInfo
	for rows.Next() {
		var sv ServerInfo
		var key []byte
		var joined int64
		if err := rows.Scan(&sv.Name, &key, &joined, &sv.Streams, &sv.Envelopes, &sv.Bytes); err != nil {
			return nil, err
		}
		sv.Fingerprint, sv.JoinedAt, sv.Connected = wire.KeyFingerprint(key), model.Time(joined), h.connOf(sv.Name) != nil
		out = append(out, sv)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	for i := range out {
		out[i].Clock = h.clockOf(out[i].Name)
	}
	return out, nil
}

// Remove removes a server from the hub: it is refused from then on, the
// roster changes, and each stream it owned is closed. The serving hub closes
// its connection and tells the others within a second.
func (h *Hub) Remove(name string) error {
	name = strings.ToLower(name)
	return h.tx(func(tx *sql.Tx) error {
		res, err := tx.Exec(`UPDATE servers SET state = 'removed' WHERE name = ? AND state = 'active'`, name)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return fmt.Errorf("%s is not on this hub", name)
		}
		if _, err := tx.Exec(`UPDATE streams SET state = 'closed', closed_at = ?, told = 0 WHERE owner = ? AND state = 'open'`,
			int64(h.now()), name); err != nil {
			return err
		}
		// The channels the hub owns lose it at once, by the hub's own hand.
		if err := h.removeFromChannels(tx, name); err != nil {
			return err
		}
		// It is delivered nothing further, whatever the owners of the
		// streams it was in do next.
		if _, err := tx.Exec(`DELETE FROM stream_servers WHERE name = ? AND stream_id IN (SELECT id FROM streams WHERE owner <> ?)`, name, name); err != nil {
			return err
		}
		return bumpRoster(tx)
	})
}

// StreamInfo is a stream as `churr-hub streams` lists it.
type StreamInfo struct {
	ID      string
	Owner   string
	Servers []string
	// Cursor is the last position written to the stream's log. It counts
	// what the hub has carried; it orders nothing.
	Cursor   int64
	LogBytes int64
	// HomeHub is the hub that is the stream's home and time reference.
	HomeHub string
}

// Streams lists the open streams.
func (h *Hub) Streams() ([]StreamInfo, error) {
	rows, err := h.db.Query(`SELECT id FROM streams WHERE state = 'open' ORDER BY created_at, id`)
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
	var out []StreamInfo
	for _, id := range ids {
		st, err := loadStream(h.db, id)
		if err != nil {
			return nil, err
		}
		info := StreamInfo{ID: id, Owner: st.owner, Servers: st.servers, Cursor: st.nextSeq - 1, HomeHub: st.homeHub}
		h.db.QueryRow(`SELECT COALESCE(sum(length(envelope)), 0) FROM log WHERE stream_id = ?`, id).Scan(&info.LogBytes)
		out = append(out, info)
	}
	return out, nil
}

// Limits are the limits of one server, or the defaults.
type Limits struct {
	Submits   int   // envelopes a minute
	BlobBytes int64 // file chunk bytes an hour
	Streams   int64 // streams it may own
}

// Limits returns the limits of a server, or with "" the defaults.
func (h *Hub) Limits(name string) (Limits, error) {
	l := Limits{Submits: defaultSubmits, BlobBytes: defaultBlobBytes, Streams: defaultStreams}
	// -server-rate is the rate for every server nobody has set one for,
	// in envelopes a second; a default or a server's own limit set with
	// `churr-hub limits --submits` (a minute) wins over it.
	if h.cfg.ServerRate > 0 {
		l.Submits = h.cfg.ServerRate * 60
	}
	if v := h.meta("default_submits"); v > 0 {
		l.Submits = int(v)
	}
	if v := h.meta("default_blob_bytes"); v > 0 {
		l.BlobBytes = v
	}
	if v := h.meta("default_streams"); v > 0 {
		l.Streams = v
	}
	if name == "" {
		return l, nil
	}
	var submits, blob, streams sql.NullInt64
	err := h.db.QueryRow(`SELECT submits, blob_bytes, streams FROM servers WHERE name = ?`, strings.ToLower(name)).Scan(&submits, &blob, &streams)
	if errors.Is(err, sql.ErrNoRows) {
		return l, fmt.Errorf("%s is not on this hub", name)
	}
	if err != nil {
		return l, err
	}
	if submits.Valid {
		l.Submits = int(submits.Int64)
	}
	if blob.Valid {
		l.BlobBytes = blob.Int64
	}
	if streams.Valid {
		l.Streams = streams.Int64
	}
	return l, nil
}

// SetLimits changes the limits of a server, or with "" the defaults. A
// value of 0 leaves that limit as it is.
func (h *Hub) SetLimits(name string, submits int, blobBytes, streams int64) error {
	return h.tx(func(tx *sql.Tx) error {
		set := func(column, key string, v int64) error {
			if v <= 0 {
				return nil
			}
			if name == "" {
				return setMeta(tx, key, v)
			}
			res, err := tx.Exec(`UPDATE servers SET `+column+` = ? WHERE name = ?`, v, strings.ToLower(name))
			if err != nil {
				return err
			}
			if n, _ := res.RowsAffected(); n == 0 {
				return fmt.Errorf("%s is not on this hub", name)
			}
			return nil
		}
		if err := set("submits", "default_submits", int64(submits)); err != nil {
			return err
		}
		if err := set("blob_bytes", "default_blob_bytes", blobBytes); err != nil {
			return err
		}
		if err := set("streams", "default_streams", streams); err != nil {
			return err
		}
		// The serving hub rebuilds its limiters when the roster version
		// moves, so a change of limits moves it too.
		return bumpRoster(tx)
	})
}

// Status is what the status page shows: totals only.
type Status struct {
	Name, Fingerprint, Version      string
	Up                              time.Duration
	ServersJoined, ServersConnected int64
	StreamsOpen                     int64
	// Channels the hub owns, by listing mode. The page gives the counts
	// and no names: an unlisted channel is nobody's business but its
	// servers', and the directory is for member servers, not the public.
	ChannelsUnlisted, ChannelsRequest, ChannelsOpen int64
	// Hubs is how many hubs the hub's cluster has (1 for a hub that stands
	// alone), TrunksUp how many of its trunks are connected, and
	// ChannelsElsewhere how many channels of other hubs it carries or
	// lists. No hub is named.
	Hubs, TrunksUp, ChannelsElsewhere int64
	EnvelopesHour, BytesHour          int64
	EnvelopesDay, BytesDay            int64
	LogBytes                          int64
	// Ephemeral is typing and presence frames passed on and dropped since
	// the hub started (ephemeral.go).
	Ephemeral EphemeralStats
	// Clocks is the totals of what the hub has measured of its servers'
	// clocks. Each server's own figures are `churr-hub servers`' to show.
	Clocks ClockTotals
}

// Status gathers the totals. It names no server and no stream.
func (h *Hub) Status() Status {
	s := Status{Name: h.id.Name, Fingerprint: h.Fingerprint(), Version: version.String(), Up: time.Since(h.started).Round(time.Second)}
	h.db.QueryRow(`SELECT count(*) FROM servers WHERE state = 'active'`).Scan(&s.ServersJoined)
	h.db.QueryRow(`SELECT count(*) FROM streams WHERE state = 'open'`).Scan(&s.StreamsOpen)
	s.Hubs = int64(len(h.topology().cluster.Members))
	s.TrunksUp = int64(len(h.liveTrunks()))
	if chans, err := h.Channels(); err == nil {
		for _, c := range chans {
			if c.Home != h.id.Name && c.Home != "" {
				s.ChannelsElsewhere++ // carried or listed for another hub of the cluster
				continue
			}
			switch c.Listing {
			case wire.ListingOpen:
				s.ChannelsOpen++
			case wire.ListingRequest:
				s.ChannelsRequest++
			default:
				s.ChannelsUnlisted++
			}
		}
	}
	hour := int64(h.now()) / 3600000
	h.db.QueryRow(`SELECT COALESCE(sum(envelopes), 0), COALESCE(sum(bytes), 0) FROM counters WHERE hour >= ?`, hour-1).Scan(&s.EnvelopesHour, &s.BytesHour)
	h.db.QueryRow(`SELECT COALESCE(sum(envelopes), 0), COALESCE(sum(bytes), 0) FROM counters WHERE hour >= ?`, hour-24).Scan(&s.EnvelopesDay, &s.BytesDay)
	h.db.QueryRow(`SELECT COALESCE(sum(length(envelope)), 0) FROM log`).Scan(&s.LogBytes)
	h.mu.Lock()
	s.ServersConnected = int64(len(h.conns))
	h.mu.Unlock()
	s.Clocks = h.clockTotals()
	s.Ephemeral = h.EphemeralStats()
	return s
}

// Text is the status as `churr-hub status` prints it.
func (s Status) Text() string {
	return fmt.Sprintf("Hub: %s\nFingerprint: %s\nVersion: %s\nUp: %s\nServers joined: %d\nServers connected: %d\nStreams open: %d\n"+
		"Channels: %d unlisted, %d by request, %d open\n"+
		"Cluster: %d hubs, %d trunks connected, %d channels of other hubs carried or listed\n"+
		"Envelopes carried, last hour: %d (%d bytes)\nEnvelopes carried, last day: %d (%d bytes)\nSize of the log: %d bytes\n"+
		"Typing and presence frames since it started: %d passed on, %d dropped under pressure (%d on arrival, %d for servers that were behind)\n",
		s.Name, s.Fingerprint, s.Version, s.Up, s.ServersJoined, s.ServersConnected, s.StreamsOpen,
		s.ChannelsUnlisted, s.ChannelsRequest, s.ChannelsOpen, s.Hubs, s.TrunksUp, s.ChannelsElsewhere, s.EnvelopesHour, s.BytesHour,
		s.EnvelopesDay, s.BytesDay, s.LogBytes, s.Ephemeral.Passed, s.Ephemeral.Shed+s.Ephemeral.Dropped, s.Ephemeral.Shed, s.Ephemeral.Dropped) + s.Clocks.Text()
}

// StatusHandler serves the status page: plain HTML, no scripts, totals only.
func (h *Hub) StatusHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		s := h.Status()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'")
		w.Header().Set("Cache-Control", "no-store")
		var b strings.Builder
		b.WriteString("<!doctype html><html lang=\"en\"><head><meta charset=\"utf-8\"><title>Churr hub</title>" +
			"<style>body{background:#16141c;color:#e8e6f0;font:16px/1.5 system-ui,sans-serif;margin:2rem}pre{font:inherit}</style></head><body><h1>Churr hub</h1><pre>")
		b.WriteString(html.EscapeString(s.Text()))
		b.WriteString("</pre></body></html>")
		w.Write([]byte(b.String()))
	})
}

// DataDirExists reports whether a hub has been set up in a data directory:
// the operator commands refuse to make one by accident.
func DataDirExists(dir string) bool {
	_, err := os.Stat(dir)
	return err == nil
}
