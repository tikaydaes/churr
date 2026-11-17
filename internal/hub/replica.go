package hub

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/tikaydaes/churr/internal/link/wire"
	"github.com/tikaydaes/churr/internal/model"
)

// This file is a channel on the hubs that are not its home
// (docs/LINK-PROTOCOL.md section 23b). A channel has one home hub, which
// owns it and alone signs its record. The record travels from there, as the
// home hub signed it, to every hub with a server it concerns and, for a
// listed channel, to every hub of the cluster, whose servers may see it in
// the directory. Each of those hubs keeps the newest record it has checked,
// sends it to its own servers, and works out from its server list where the
// channel's envelopes go.
//
// The file also holds the one way a channel's home changes: the operator's
// command, when both hubs are up.

func (h *Hub) forgetRoute(streamID string) {
	h.mu.Lock()
	delete(h.routes, streamID)
	h.mu.Unlock()
}

// reachable reports whether a server is on this hub or on a hub this one
// can reach now. It reads nothing from the database: it is asked inside
// transactions, and the hub's database has one connection.
func (h *Hub) reachable(server string) bool {
	rs, remote := h.topology().servers[server]
	return !remote || h.toward(rs.hub) != nil
}

// remote reports whether a server is on another hub of the cluster.
func (h *Hub) remote(server string) bool {
	_, remote := h.topology().servers[server]
	return remote
}

// behind lists which of some servers lie beyond a neighbour.
func (h *Hub) behind(trunk string, servers []string) []string {
	t := h.topology()
	var out []string
	for _, n := range servers {
		if rs, remote := t.servers[n]; remote && t.next[rs.hub] == trunk && !has(out, n) {
			out = append(out, n)
		}
	}
	return out
}

// handoff is what a hub that gives up a channel passes, unsigned, to the hub
// that takes it over: what the home keeps of a channel beside its record.
// It travels in the rehome and channel frames, so the link's schema has it.
type handoff = wire.Handoff

// sendRecordToTrunks sends a record that has changed over every trunk it
// should cross, and starts the shipping of the channel's envelopes over any
// trunk its server list has just brought in.
func (h *Hub) sendRecordToTrunks(c channel, f wire.Frame, also []string) {
	for _, tc := range h.liveTrunks() {
		h.sendRecordOver(tc, c, f, also, false)
		h.arm(tc, c.rec.StreamID)
	}
}

// sendRecordOver sends one record over one trunk if that side needs it. A
// record travels away from the hub that signed it: to the side of every
// server it concerns (in the channel, invited, or just taken out), to every
// side if the channel is listed, and toward a hub it names as the new home.
// A side that was sent an earlier version and no longer needs the channel is
// sent this one too, once, so that it knows.
func (h *Hub) sendRecordOver(tc *trunkConn, c channel, f wire.Frame, also []string, force bool) {
	if c.rec.Prepared || c.rec.StreamID == "" {
		return
	}
	t := h.topology()
	if c.rec.Hub != h.id.Name && t.next[c.rec.Hub] == tc.name {
		return // that is where it came from
	}
	concerned := h.behind(tc.name, append(append([]string{}, audience(c)...), also...))
	should := len(concerned) > 0 || (c.rec.Listing != wire.ListingUnlisted && !c.rec.Closed) ||
		(c.rec.HomeHub != h.id.Name && t.next[c.rec.HomeHub] == tc.name)
	tc.mu.Lock()
	told := tc.told[c.rec.StreamID]
	if (!should && told == 0 && !force) || (told >= c.rec.Version && !force) {
		tc.mu.Unlock()
		return
	}
	if should {
		tc.told[c.rec.StreamID] = c.rec.Version
	} else {
		delete(tc.told, c.rec.StreamID)
	}
	tc.mu.Unlock()
	for _, ho := range c.handovers {
		if ho.Version > told && ho.Version < c.rec.Version {
			tc.send(wire.Frame{Frame: wire.FChannel, Signed: ho.Signed, Sig: ho.Sig})
		}
	}
	out := f
	out.JoinedHave = map[string]map[string]int64{}
	for _, name := range h.behind(tc.name, c.rec.Servers) {
		if at, ok := c.joinedHave[name]; ok {
			out.JoinedHave[name] = at
		}
	}
	out.Also = h.behind(tc.name, also)
	if c.rec.Hub == h.id.Name && c.rec.HomeHub != h.id.Name && t.next[c.rec.HomeHub] == tc.name {
		// The record hands the channel over, and this is the way to its new
		// home: what the home keeps beside the record goes with it.
		var epoch int64
		h.db.QueryRow(`SELECT epoch FROM streams WHERE id = ?`, c.rec.StreamID).Scan(&epoch)
		out.State = &handoff{Creator: c.creator, Inviters: c.inviters, JoinedHave: c.joinedHave, Declined: c.declined, MoveID: c.moveID, Epoch: epoch}
	}
	tc.send(out)
}

// sendRecordsTo sends a neighbour whose trunk has just come up every record
// its side needs.
func (h *Hub) sendRecordsTo(tc *trunkConn) {
	for _, id := range h.channelIDs() {
		m := h.lock(id)
		if c, err := loadChannel(h.db, id); err == nil {
			if f, err := h.recordFrame(c); err == nil {
				h.sendRecordOver(tc, c, f, nil, false)
			}
		}
		m.Unlock()
	}
}

func (h *Hub) channelIDs() []string {
	rows, err := h.db.Query(`SELECT stream_id FROM channels`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	return ids
}

// resendRecordIfNewer sends a neighbour that holds an older version of a
// record the one this hub holds: it may have been away when its side stopped
// needing the channel.
func (h *Hub) resendRecordIfNewer(tc *trunkConn, id string, theirs int64) {
	if theirs == 0 {
		return
	}
	m := h.lock(id)
	defer m.Unlock()
	c, err := loadChannel(h.db, id)
	if err != nil || c.rec.Version <= theirs {
		return
	}
	tc.mu.Lock()
	sent := tc.told[id] >= c.rec.Version
	tc.mu.Unlock()
	if sent {
		return
	}
	if f, err := h.recordFrame(c); err == nil {
		h.sendRecordOver(tc, c, f, nil, true)
	}
}

// onTrunkRecord takes a channel record that reached this hub over a trunk.
// It is taken only if the hub that signed it is a member of the cluster
// with the key the record names, lies on the side it came from, and is the
// channel's home as this hub holds it (for a channel new here, the record's
// own word). A record that names this hub as the new home hands the channel
// over: this hub owns it from that transaction on.
func (h *Hub) onTrunkRecord(tc *trunkConn, f wire.Frame) error {
	var rec wire.ChannelRecord
	if err := wire.PeekBlock(f, &rec); err != nil {
		return err
	}
	t := h.topology()
	if rec.Hub == h.id.Name {
		return nil // this hub's own, come round again: a handover it signed, passed on by the new home
	}
	key := t.cluster.Key(rec.Hub)
	if key == nil || !bytes.Equal(key, rec.HubKey) {
		return wire.Errf(wire.EBadSignature, "The record is not signed by a hub of this cluster.")
	}
	if err := wire.VerifyBlock(f, key, &rec); err != nil {
		return err
	}
	if !wire.ValidID(rec.StreamID) || rec.Version < 1 || !wire.ValidListing(rec.Listing) || rec.Prepared || rec.HomeHub == "" ||
		(rec.Private && rec.Listing != wire.ListingUnlisted) {
		return wire.Errf(wire.EBadFrame, "The channel record is malformed.")
	}
	// A record comes from the side of the hub that signed it; one that hands
	// a channel over may also come from the side of the hub it names, which
	// passes it on before its own.
	if t.next[rec.Hub] != tc.name && !(rec.Hub != rec.HomeHub && t.next[rec.HomeHub] == tc.name) {
		return wire.Errf(wire.EForbidden, "%s is not on that side of this hub.", rec.Hub)
	}
	if o, pending := offerOf(h.db, rec.StreamID); pending && rec.Hub == o.To && rec.HomeHub == rec.Hub && rec.Version > o.Version {
		// The hub this one offered the channel to signs as its home: it
		// took the offer, and its answer was lost on the way.
		h.stepDown(f)
		return nil
	}
	if _, member := t.cluster.Members[rec.HomeHub]; !member {
		return wire.Errf(wire.EForbidden, "%s is not a hub of this cluster.", rec.HomeHub)
	}
	m := h.lock(rec.StreamID)
	err := h.tx(func(tx *sql.Tx) error {
		c, err := loadChannel(tx, rec.StreamID)
		exists := err == nil
		if exists {
			switch {
			case h.owned(c):
				return wire.Errf(wire.ENotOwner, "This hub is that channel's home.")
			case c.rec.Version >= rec.Version:
				return nil
			case c.rec.HomeHub != rec.Hub:
				return wire.Errf(wire.EForbidden, "%s is not that channel's home; %s is.", rec.Hub, c.rec.HomeHub)
			}
		} else {
			var one int
			if tx.QueryRow(`SELECT 1 FROM streams WHERE id = ?`, rec.StreamID).Scan(&one) == nil {
				return wire.Errf(wire.EConflict, "That identifier is a stream of this hub's own.")
			}
			c = channel{inviters: map[string]string{}, joinedHave: map[string]map[string]int64{}}
		}
		c.rec, c.signed, c.sig = rec, f.Signed, f.Sig
		if c.joinedHave == nil {
			c.joinedHave = map[string]map[string]int64{}
		}
		for name, at := range f.JoinedHave {
			c.joinedHave[name] = at
		}
		for name := range c.joinedHave {
			if !rec.Has(name) {
				delete(c.joinedHave, name)
			}
		}
		if rec.Hub != rec.HomeHub {
			c.handovers = append(c.handovers, handover{Version: rec.Version, Signed: f.Signed, Sig: f.Sig})
			if len(c.handovers) > 4 {
				c.handovers = c.handovers[len(c.handovers)-4:]
			}
		}
		if err := h.putReplica(tx, &c, f.Also); err != nil {
			return err
		}
		if rec.HomeHub == h.id.Name {
			return h.takeOver(tx, &c, f.State)
		}
		return nil
	})
	m.Unlock()
	if err != nil {
		return err
	}
	tc.mu.Lock()
	if tc.told[rec.StreamID] < rec.Version {
		tc.told[rec.StreamID] = rec.Version
	}
	tc.mu.Unlock()
	h.forgetRoute(rec.StreamID)
	h.tellChannels()
	return nil
}

// putReplica writes another hub's record as this hub's copy of the channel:
// the stream, its server list (which delivery and routing read), and the
// record as signed. It is marked as not yet sent on.
func (h *Hub) putReplica(tx *sql.Tx, c *channel, also []string) error {
	rec := c.rec
	state, closedAt := "open", any(nil)
	if rec.Closed {
		state, closedAt = "closed", int64(h.now())
	}
	if _, err := tx.Exec(`INSERT INTO streams (id, owner, version, next_seq, state, closed_at, created_at, home_hub, epoch) VALUES (?, ?, ?, 1, ?, ?, ?, ?, 0)
		ON CONFLICT (id) DO UPDATE SET owner = excluded.owner, version = excluded.version, state = excluded.state, closed_at = excluded.closed_at,
			home_hub = excluded.home_hub, told = 1`, rec.StreamID, rec.HomeHub, rec.Version, state, closedAt, int64(h.now()), rec.HomeHub); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM stream_servers WHERE stream_id = ?`, rec.StreamID); err != nil {
		return err
	}
	for _, n := range rec.Servers {
		if _, err := tx.Exec(`INSERT INTO stream_servers (stream_id, name) VALUES (?, ?)`, rec.StreamID, n); err != nil {
			return err
		}
	}
	var before string
	var told int
	if tx.QueryRow(`SELECT also, told FROM channels WHERE stream_id = ?`, rec.StreamID).Scan(&before, &told) == nil && told == 0 {
		var old []string
		json.Unmarshal([]byte(before), &old)
		also = append(old, also...)
	}
	_, err := tx.Exec(`
		INSERT INTO channels (stream_id, record, creator, inviters, maker_deadline, told, also, move_id, declined, joined_have, signed, sig, handovers)
		VALUES (?, ?, ?, '{}', 0, 0, ?, '', '[]', ?, ?, ?, ?)
		ON CONFLICT (stream_id) DO UPDATE SET record = excluded.record, told = 0, also = excluded.also, joined_have = excluded.joined_have,
			signed = excluded.signed, sig = excluded.sig, handovers = excluded.handovers, maker_deadline = 0`,
		rec.StreamID, mustMessage("ChannelRecord", rec), c.creator, string(mustJSON(append([]string{}, also...))), string(mustJSON(c.joinedHave)),
		c.signed, c.sig, string(mustJSON(append([]handover{}, c.handovers...))))
	return err
}

// takeOver makes this hub the home of a channel another hub has just handed
// to it: it owns the stream, keeps what the old home kept beside the record,
// and signs the next record itself.
func (h *Hub) takeOver(tx *sql.Tx, c *channel, state *handoff) error {
	var s handoff
	if state != nil {
		s = *state
	}
	c.creator, c.moveID, c.declined = s.Creator, s.MoveID, s.Declined
	if s.Inviters != nil {
		c.inviters = s.Inviters
	}
	if s.JoinedHave != nil {
		c.joinedHave = s.JoinedHave
	}
	if _, err := tx.Exec(`UPDATE streams SET owner = ?, home_hub = ?, epoch = max(epoch, ?) WHERE id = ?`, h.id.Name, h.id.Name, s.Epoch, c.rec.StreamID); err != nil {
		return err
	}
	c.signed, c.sig, c.homeTo = nil, nil, ""
	c.started = s.Epoch >= c.rec.Epoch
	if !c.started && len(c.rec.Servers) > 0 {
		c.deadline = int64(h.now()) + makerWait.Load()/int64(time.Millisecond)
	}
	h.log.Info("this hub is now a channel's home", "stream", c.rec.StreamID, "from", c.rec.Hub)
	return h.saveChannel(tx, c, nil)
}

// Rehome asks for a channel's home to be moved to another hub of the
// cluster. It is the operator's command and the only way a home changes:
// nothing re-homes a channel by itself, whatever fails. The serving hub
// does it within a second if the other hub can be reached, and says in
// RehomeResult what became of it.
func (h *Hub) Rehome(stream, to string) error {
	to = strings.ToLower(to)
	if err := h.reloadTopology(); err != nil {
		return err
	}
	if _, member := h.topology().cluster.Members[to]; !member || to == h.id.Name {
		return fmt.Errorf("%s is not another hub of this hub's cluster", to)
	}
	return h.tx(func(tx *sql.Tx) error {
		c, err := loadChannel(tx, stream)
		if err != nil {
			return errors.New("there is no such channel")
		}
		if !h.owned(c) {
			return fmt.Errorf("that channel's home is %s; it is moved from there", c.rec.HomeHub)
		}
		if c.rec.Closed || c.rec.Prepared {
			return errors.New("that channel is closed, or not yet live")
		}
		if o, pending := offerOf(tx, stream); pending {
			return fmt.Errorf("that channel's home is already on offer to %s; that is settled first", o.To)
		}
		if _, err := tx.Exec(`UPDATE channels SET rehome = ? WHERE stream_id = ?`, to, stream); err != nil {
			return err
		}
		return setSetting(tx, "rehome:"+stream, "pending")
	})
}

// RehomeResult says what became of the last Rehome of a channel: "pending",
// "done", or "failed: " and why.
func (h *Hub) RehomeResult(stream string) string { return h.setting("rehome:" + stream) }

// rehomeMaxOffset is how far apart the two hubs' clocks may be for a home to
// move: the channel's events are timed by the new home's clock from then on,
// and an origin's times only grow.
const rehomeMaxOffset = 15 * 1000

// rehomeOffer is a channel's home on offer to another hub: the record that
// hands it over, signed and not yet in force, and what the home keeps beside
// the record.
type rehomeOffer struct {
	ID      string     `json:"id"`
	To      string     `json:"to"`
	Version int64      `json:"version"`
	Signed  wire.Bytes `json:"signed"`
	Sig     wire.Bytes `json:"sig"`
	State   *handoff   `json:"state"`
	// At is when it was made, by this hub's clock. Cancel: no answer came
	// in time, and the offer is being called off.
	At     int64 `json:"at"`
	Cancel bool  `json:"cancel,omitempty"`
}

// offerOf returns the offer of a channel's home that has not been settled.
func offerOf(q querier, stream string) (rehomeOffer, bool) {
	var raw string
	var o rehomeOffer
	if q.QueryRow(`SELECT offer FROM channels WHERE stream_id = ?`, stream).Scan(&raw) != nil || raw == "" {
		return o, false
	}
	return o, json.Unmarshal([]byte(raw), &o) == nil && o.ID != ""
}

// rehomeWait is how long the hub that offers a channel's home waits for the
// other hub's answer before it starts to call the offer off. Tests shorten
// it.
var rehomeWait atomic.Int64

func init() { rehomeWait.Store(int64(5 * time.Second)) }

// SetRehomeWait changes how long an offer of a channel's home waits for its
// answer, and returns a function that puts it back. It is for tests.
func SetRehomeWait(d time.Duration) func() {
	old := rehomeWait.Swap(int64(d))
	return func() { rehomeWait.Store(old) }
}

// doRehomes carries out the moves of a channel's home the operator has asked
// for, all or nothing (section 23b).
//
// The old home does not step down and hope. It signs the record that hands
// the channel over, keeps it as an **offer**, and sends it to the other hub;
// from then the channel's record is frozen here (nothing else is signed for
// it) but this hub is still its home. **The move happens in one transaction
// on the other hub**: when the offer reaches it, it takes the channel over
// and signs the next record, or it refuses and never will take it. Its
// answer, the next record under its own signature, is what this hub steps
// down on.
//
// If no answer comes (the trunk dropped at that moment) this hub calls the
// offer off: it asks the other hub, whenever it can next reach it, which
// answers with a signed word that the offer is dead (and remembers that, so
// an offer that arrives late is refused), or with its record if it had taken
// the channel after all. Until one of those answers is in hand this hub
// cannot know which of the two hubs is the home, and signs nothing; in every
// other moment exactly one hub is the home and can act as it. There is no
// moment in which neither is.
func (h *Hub) doRehomes() {
	h.startRehomes()
	rows, err := h.db.Query(`SELECT stream_id FROM channels WHERE offer <> ''`)
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
	now := int64(h.now())
	for _, id := range ids {
		o, pending := offerOf(h.db, id)
		if !pending {
			continue
		}
		if !o.Cancel && now-o.At > rehomeWait.Load()/int64(time.Millisecond) {
			// No answer in time. From here the offer is being called off;
			// what became of it is the other hub's to say.
			o.Cancel = true
			h.tx(func(tx *sql.Tx) error {
				if _, err := tx.Exec(`UPDATE channels SET offer = ? WHERE stream_id = ?`, string(mustJSON(o)), id); err != nil {
					return err
				}
				return setSetting(tx, "rehome:"+id, "cancelling: "+o.To+" has not answered; the move is being called off, and the channel's home stays here unless "+o.To+" had already taken it")
			})
			h.log.Warn("an offer of a channel's home was not answered and is being called off", "stream", id, "to", o.To)
		}
		tc := h.toward(o.To)
		if tc == nil {
			continue
		}
		if o.Cancel {
			tc.send(wire.Frame{Frame: wire.FRehome, From: h.id.Name, To: o.To, Step: "cancel", Offer: o.ID, StreamID: id})
		} else {
			tc.send(wire.Frame{Frame: wire.FRehome, From: h.id.Name, To: o.To, Step: "offer", Offer: o.ID, StreamID: id, Signed: o.Signed, Sig: o.Sig, State: o.State})
		}
	}
}

// startRehomes makes the offer for each move the operator has just asked for.
func (h *Hub) startRehomes() {
	rows, err := h.db.Query(`SELECT stream_id, rehome FROM channels WHERE rehome <> '' AND signed IS NULL AND offer = ''`)
	if err != nil {
		return
	}
	type move struct{ id, to string }
	var moves []move
	for rows.Next() {
		var mv move
		if rows.Scan(&mv.id, &mv.to) == nil {
			moves = append(moves, mv)
		}
	}
	rows.Close()
	for _, mv := range moves {
		fail := func(why string) {
			h.tx(func(tx *sql.Tx) error {
				if _, err := tx.Exec(`UPDATE channels SET rehome = '' WHERE stream_id = ?`, mv.id); err != nil {
					return err
				}
				return setSetting(tx, "rehome:"+mv.id, "failed: "+why)
			})
		}
		hc, known := h.hubClockOf(mv.to)
		switch {
		case h.toward(mv.to) == nil:
			fail(mv.to + " cannot be reached now; a home moves only when both hubs are up")
			continue
		case !known:
			fail("this hub has no reading of " + mv.to + "'s clock yet")
			continue
		case hc.offset > rehomeMaxOffset || hc.offset < -rehomeMaxOffset:
			fail(fmt.Sprintf("the two hubs' clocks are %d ms apart; at most %d", hc.offset, rehomeMaxOffset))
			continue
		}
		m := h.lock(mv.id)
		err := h.tx(func(tx *sql.Tx) error {
			c, err := loadChannel(tx, mv.id)
			if err != nil || !h.owned(c) {
				return errors.New("the channel is not this hub's any more")
			}
			// The record that hands the channel over: the one this hub
			// holds, at the next version, naming the other hub as the home.
			// It is signed and kept, not put in force.
			rec := c.rec
			rec.Version++
			rec.Hub, rec.HubKey, rec.HomeHub, rec.At = h.id.Name, wire.Bytes(h.keys.SignPublic()), mv.to, h.now()
			f, err := wire.SignBlock(wire.FChannel, rec, h.keys)
			if err != nil {
				return err
			}
			var epoch int64
			tx.QueryRow(`SELECT epoch FROM streams WHERE id = ?`, mv.id).Scan(&epoch)
			o := rehomeOffer{ID: model.NewID[string](), To: mv.to, Version: rec.Version, Signed: f.Signed, Sig: f.Sig, At: int64(h.now()),
				State: &handoff{Creator: c.creator, Inviters: c.inviters, JoinedHave: c.joinedHave, Declined: c.declined, MoveID: c.moveID, Epoch: epoch}}
			if _, err := tx.Exec(`UPDATE channels SET offer = ?, rehome = '' WHERE stream_id = ?`, string(mustJSON(o)), mv.id); err != nil {
				return err
			}
			return setSetting(tx, "rehome:"+mv.id, "pending")
		})
		m.Unlock()
		if err != nil {
			fail(err.Error())
			continue
		}
		h.log.Info("a channel's home is on offer", "stream", mv.id, "to", mv.to)
	}
}

// onRehome takes a frame of the exchange by which a channel's home moves:
// passed on if it is for another hub, answered if this hub is the one
// offered the channel, acted on if this hub made the offer.
func (h *Hub) onRehome(tc *trunkConn, f wire.Frame) error {
	if f.To != h.id.Name {
		if next := h.toward(f.To); next != nil && next != tc {
			next.send(f)
		}
		return nil
	}
	t := h.topology()
	key := t.cluster.Key(f.From)
	if key == nil || t.next[f.From] != tc.name {
		return wire.Errf(wire.EForbidden, "%s is not a hub on that side of this one.", f.From)
	}
	reply := func(out wire.Frame) {
		out.Frame, out.From, out.To, out.Offer, out.StreamID = wire.FRehome, h.id.Name, f.From, f.Offer, f.StreamID
		tc.send(out)
	}
	switch f.Step {
	case "offer", "cancel":
		reply(h.answerOffer(f, key))
	case "taken":
		h.stepDown(f)
	case "refused", "cancelled":
		h.offerDead(f, key)
	}
	return nil
}

// answerOffer is the hub a channel's home is offered to. One transaction
// settles the offer for good: the channel is this hub's, or the offer is
// dead here and stays dead whenever it arrives again.
func (h *Hub) answerOffer(f wire.Frame, fromKey []byte) wire.Frame {
	dead := func(why string) wire.Frame {
		signed, sig, err := wire.SignBytes(wire.BRehomeAnswer, wire.RehomeAnswer{Offer: f.Offer, StreamID: f.StreamID, Hub: h.id.Name, Dead: true, Why: why, At: h.now()}, h.keys)
		if err != nil {
			return wire.Frame{Step: "refused", Message: why}
		}
		step := "refused"
		if f.Step == "cancel" {
			step = "cancelled"
		}
		return wire.Frame{Step: step, Message: why, Signed: signed, Sig: sig}
	}
	if !wire.ValidID(f.StreamID) || !wire.ValidID(f.Offer) {
		return wire.Frame{Step: "refused", Message: "The offer names a channel and itself."}
	}
	m := h.lock(f.StreamID)
	defer m.Unlock()
	var out wire.Frame
	var taken bool
	err := h.tx(func(tx *sql.Tx) error {
		was := settingIn(tx, "rehome_in:"+f.Offer)
		c, cerr := loadChannel(tx, f.StreamID)
		if was == "taken" {
			return nil // answered below, with the record as it now stands
		}
		refuse := func(why string) error {
			out = dead(why)
			return setSetting(tx, "rehome_in:"+f.Offer, "dead")
		}
		if was == "dead" {
			return refuse("that offer was refused or called off")
		}
		if f.Step == "cancel" {
			return refuse("called off by " + f.From + " before it arrived")
		}
		var rec wire.ChannelRecord
		if err := wire.VerifyBlock(wire.Frame{Frame: wire.FChannel, Signed: f.Signed, Sig: f.Sig}, fromKey, &rec); err != nil ||
			rec.Hub != f.From || rec.HomeHub != h.id.Name || rec.StreamID != f.StreamID || rec.Version < 2 || rec.Prepared || rec.Closed {
			return refuse("the offer is not a record of " + f.From + "'s that hands this hub the channel")
		}
		hc, known := h.hubClockOf(f.From)
		switch {
		case !known:
			return refuse("this hub has no reading of " + f.From + "'s clock yet")
		case hc.offset > rehomeMaxOffset || hc.offset < -rehomeMaxOffset:
			return refuse(fmt.Sprintf("the two hubs' clocks are %d ms apart; at most %d", hc.offset, rehomeMaxOffset))
		}
		if cerr == nil {
			switch {
			case h.owned(c):
				return refuse("this hub is that channel's home already")
			case c.rec.Version >= rec.Version || c.rec.HomeHub != rec.Hub:
				return refuse("this hub holds a later record of that channel, or one from another home")
			}
		} else {
			var one int
			if tx.QueryRow(`SELECT 1 FROM streams WHERE id = ?`, f.StreamID).Scan(&one) == nil {
				return refuse("that identifier is a stream of this hub's own")
			}
			c = channel{inviters: map[string]string{}, joinedHave: map[string]map[string]int64{}}
		}
		c.rec, c.signed, c.sig = rec, f.Signed, f.Sig
		c.handovers = append(c.handovers, handover{Version: rec.Version, Signed: f.Signed, Sig: f.Sig})
		if len(c.handovers) > 4 {
			c.handovers = c.handovers[len(c.handovers)-4:]
		}
		if err := h.putReplica(tx, &c, nil); err != nil {
			return err
		}
		if err := h.takeOver(tx, &c, f.State); err != nil {
			return err
		}
		taken = true
		return setSetting(tx, "rehome_in:"+f.Offer, "taken")
	})
	if err != nil {
		h.log.Warn("an offer of a channel's home could not be settled", "stream", f.StreamID, "from", f.From, "err", err)
		return wire.Frame{Step: "busy", Message: "The offer could not be settled just now."}
	}
	if out.Step != "" {
		return out
	}
	if taken {
		h.forgetRoute(f.StreamID)
		go h.tellChannels()
	}
	// Taken, now or before: the answer is the channel's record as this hub,
	// or whichever hub has it since, signs it.
	c, err := loadChannel(h.db, f.StreamID)
	if err != nil {
		return wire.Frame{Step: "busy", Message: "The channel is not here."}
	}
	rf, err := h.recordFrame(c)
	if err != nil {
		return wire.Frame{Step: "busy"}
	}
	return wire.Frame{Step: "taken", Signed: rf.Signed, Sig: rf.Sig}
}

// settingIn is setting inside a transaction.
func settingIn(q querier, key string) string {
	var v string
	q.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&v)
	return v
}

// stepDown is the old home hearing that the other hub has taken the
// channel: the proof is a record of the channel, later than the offer,
// under that hub's signature. From this transaction this hub carries the
// channel as any other hub does.
func (h *Hub) stepDown(f wire.Frame) {
	var rec wire.ChannelRecord
	if wire.PeekBlock(wire.Frame{Frame: wire.FChannel, Signed: f.Signed, Sig: f.Sig}, &rec) != nil {
		return
	}
	key := h.topology().cluster.Key(rec.Hub)
	if key == nil || !bytes.Equal(key, rec.HubKey) || wire.VerifyBlock(wire.Frame{Frame: wire.FChannel, Signed: f.Signed, Sig: f.Sig}, key, &rec) != nil {
		return
	}
	m := h.lock(rec.StreamID)
	stepped := false
	err := h.tx(func(tx *sql.Tx) error {
		o, pending := offerOf(tx, rec.StreamID)
		if !pending || rec.Version <= o.Version || rec.HomeHub != rec.Hub || rec.Hub == h.id.Name {
			return nil
		}
		c, err := loadChannel(tx, rec.StreamID)
		if err != nil || !h.owned(c) {
			return err
		}
		c.handovers = append(c.handovers, handover{Version: o.Version, Signed: o.Signed, Sig: o.Sig})
		if len(c.handovers) > 4 {
			c.handovers = c.handovers[len(c.handovers)-4:]
		}
		c.rec, c.signed, c.sig, c.deadline = rec, f.Signed, f.Sig, 0
		if err := h.putReplica(tx, &c, nil); err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE channels SET offer = '', rehome = '' WHERE stream_id = ?`, rec.StreamID); err != nil {
			return err
		}
		stepped = true
		return setSetting(tx, "rehome:"+rec.StreamID, "done")
	})
	m.Unlock()
	if err != nil {
		h.log.Warn("this hub could not step down as a channel's home", "stream", rec.StreamID, "err", err)
		return
	}
	if stepped {
		h.forgetRoute(rec.StreamID)
		h.log.Info("a channel's home has been handed over", "stream", rec.StreamID, "to", rec.HomeHub)
		go h.tellChannels()
	}
}

// offerDead is the old home hearing, under the other hub's signature, that
// its offer will never be taken. It is the channel's home as before, and
// signs for it again.
func (h *Hub) offerDead(f wire.Frame, fromKey []byte) {
	var ans wire.RehomeAnswer
	if wire.VerifyBytes(wire.BRehomeAnswer, f.Signed, f.Sig, fromKey, &ans) != nil || !ans.Dead || ans.Hub != f.From {
		return
	}
	m := h.lock(ans.StreamID)
	defer m.Unlock()
	h.tx(func(tx *sql.Tx) error {
		o, pending := offerOf(tx, ans.StreamID)
		if !pending || o.ID != ans.Offer || o.To != ans.Hub {
			return nil
		}
		if _, err := tx.Exec(`UPDATE channels SET offer = '' WHERE stream_id = ?`, ans.StreamID); err != nil {
			return err
		}
		why := ans.Why
		if o.Cancel {
			why = o.To + " did not answer in time, and the move was called off"
		}
		h.log.Warn("an offer of a channel's home is dead; the home stays here", "stream", ans.StreamID, "to", o.To, "why", why)
		return setSetting(tx, "rehome:"+ans.StreamID, "failed: "+why+". The channel's home is still this hub")
	})
}
