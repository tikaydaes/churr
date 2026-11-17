package hub

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/tikaydaes/churr/internal/link/wire"
	"github.com/tikaydaes/churr/internal/model"
)

// This file is the hub as the owner of the channels it carries (section 23
// of docs/LINK-PROTOCOL.md, ruling R56). The hub keeps one record per
// channel (name, description, listing mode, member servers, managers) and
// is the only party that signs a change to it. Servers ask for changes: a
// manager's action signed by the manager's home server, a server's own
// wish to join or leave. The hub checks who asks, changes the record, signs
// it whole and sends it to the channel's servers. It never holds a stream
// key: on every change to the server list it names one member server to make
// the next one.

// makerWait is how long a named key-maker has to use the new epoch before
// the hub names the next server in line. Tests shorten it.
var makerWait atomic.Int64

func init() { makerWait.Store(int64(30 * time.Second)) }

// SetMakerWait changes how long a named key-maker is given, and returns a
// function that puts the old wait back. It is for tests.
func SetMakerWait(d time.Duration) func() {
	old := makerWait.Swap(int64(d))
	return func() { makerWait.Store(old) }
}

// makersKept is how many key epochs back a record says who made the key.
const makersKept = 32

// channel is a row of channels.
type channel struct {
	rec      wire.ChannelRecord
	creator  string
	inviters map[string]string
	deadline int64
	// moveID is, for a channel brought here from a direct link, the move
	// that brought it (section 21a).
	moveID string
	// declined are servers whose request to join was declined while they
	// were not connected: each is told when it next connects.
	declined []string
	// started reports whether the key epoch the record calls for has
	// begun: the hub has taken an envelope under it from its maker.
	started bool
	// joinedHave is, per member server, what the log held of each origin
	// at the moment the hub admitted it. A server starts from there, and
	// may hear that it was admitted only later, when the log holds more.
	joinedHave map[string]map[string]int64
	// For a channel whose home is another hub: the record as that hub
	// signed it, bytes and signature. Nil for a channel this hub owns.
	signed, sig []byte
	// handovers are the records by which the channel's home changed hands,
	// oldest first: each signed by the hub that gave it up.
	handovers []handover
	// rehome is the hub the operator has asked the home to be moved to.
	rehome string
	// homeTo, set while a home is being handed over, is the hub the next
	// record names as the home.
	homeTo string
}

// handover is a record in which a channel's home hub names another hub as
// the home: the last record the old home signs.
type handover struct {
	Version int64      `json:"version"`
	Signed  wire.Bytes `json:"signed"`
	Sig     wire.Bytes `json:"sig"`
}

// owned reports whether this hub is the channel's home.
func (h *Hub) owned(c channel) bool {
	return len(c.signed) == 0 && (c.rec.HomeHub == h.id.Name || c.rec.HomeHub == "")
}

func loadChannel(q querier, id string) (channel, error) {
	var c channel
	var rec []byte
	var inviters, declined, joinedHave, handovers string
	var epoch int64
	err := q.QueryRow(`SELECT c.record, c.creator, c.inviters, c.maker_deadline, c.move_id, c.declined, c.joined_have, COALESCE(s.epoch, 0),
			c.signed, c.sig, c.handovers, c.rehome
		FROM channels c LEFT JOIN streams s ON s.id = c.stream_id WHERE c.stream_id = ?`, id).
		Scan(&rec, &c.creator, &inviters, &c.deadline, &c.moveID, &declined, &joinedHave, &epoch, &c.signed, &c.sig, &handovers, &c.rehome)
	if errors.Is(err, sql.ErrNoRows) {
		return c, wire.Errf(wire.EUnknownStream, "No such channel here.")
	}
	if err != nil {
		return c, err
	}
	if err := wire.Unmarshal("ChannelRecord", rec, &c.rec); err != nil {
		return c, err
	}
	c.inviters = map[string]string{}
	json.Unmarshal([]byte(inviters), &c.inviters)
	json.Unmarshal([]byte(declined), &c.declined)
	c.joinedHave = map[string]map[string]int64{}
	json.Unmarshal([]byte(joinedHave), &c.joinedHave)
	json.Unmarshal([]byte(handovers), &c.handovers)
	c.started = epoch >= c.rec.Epoch
	return c, nil
}

// saveChannel writes a changed record as the next version, mirrors its
// server list into the tables that delivery reads, and marks it as not yet
// sent. also are servers outside the record that must see it once: a server
// just removed.
func (h *Hub) saveChannel(tx *sql.Tx, c *channel, also []string) error {
	// While the channel's home is on offer to another hub the record is not
	// changed here: the offer carries the next version, and whichever hub is
	// the home when the offer is settled signs the one after (replica.go).
	if o, pending := offerOf(tx, c.rec.StreamID); pending {
		return wire.Errf(wire.EUnavailable, "The channel's home is being moved to %s. Try again in a moment.", o.To)
	}
	c.rec.Version++
	c.rec.Hub, c.rec.HubKey, c.rec.HomeHub, c.rec.At = h.id.Name, wire.Bytes(h.keys.SignPublic()), h.id.Name, h.now()
	if c.homeTo != "" {
		c.rec.HomeHub = c.homeTo // the record that hands the channel over (replica.go)
	}
	for _, l := range []*[]string{&c.rec.Servers, &c.rec.Invited, &c.rec.Requests, &c.rec.Joined, &c.rec.Left} {
		if *l == nil {
			*l = []string{}
		}
	}
	if c.rec.Managers == nil {
		c.rec.Managers = []wire.ChannelAccount{}
	}
	if c.rec.Moderators == nil {
		c.rec.Moderators = []wire.ChannelAccount{}
	}
	// Who was named to make each of the last few keys.
	if c.rec.Makers == nil {
		c.rec.Makers = map[string]string{}
	}
	if c.rec.KeyMaker != "" {
		c.rec.Makers[strconv.FormatInt(c.rec.Epoch, 10)] = c.rec.KeyMaker
	}
	for epoch := range c.rec.Makers {
		if n, err := strconv.ParseInt(epoch, 10, 64); err != nil || n <= c.rec.Epoch-makersKept {
			delete(c.rec.Makers, epoch)
		}
	}
	state, closedAt := "open", any(nil)
	switch {
	case c.rec.Closed:
		state, closedAt = "closed", int64(h.now())
	case c.rec.Prepared:
		state = "prepared"
	}
	if _, err := tx.Exec(`UPDATE streams SET version = ?, state = ?, closed_at = ? WHERE id = ?`, c.rec.Version, state, closedAt, c.rec.StreamID); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM stream_servers WHERE stream_id = ?`, c.rec.StreamID); err != nil {
		return err
	}
	for _, n := range c.rec.Servers {
		if _, err := tx.Exec(`INSERT INTO stream_servers (stream_id, name) VALUES (?, ?)`, c.rec.StreamID, n); err != nil {
			return err
		}
	}
	// Servers told once before and not yet told stay on the list.
	var before string
	var told int
	if tx.QueryRow(`SELECT also, told FROM channels WHERE stream_id = ?`, c.rec.StreamID).Scan(&before, &told) == nil && told == 0 {
		var old []string
		json.Unmarshal([]byte(before), &old)
		also = append(old, also...)
	}
	_, err := tx.Exec(`
		INSERT INTO channels (stream_id, record, creator, inviters, maker_deadline, told, also, move_id, declined, joined_have, handovers)
		VALUES (?, ?, ?, ?, ?, 0, ?, ?, ?, ?, ?)
		ON CONFLICT (stream_id) DO UPDATE SET record = excluded.record, inviters = excluded.inviters,
			maker_deadline = excluded.maker_deadline, told = 0, also = excluded.also, declined = excluded.declined,
			joined_have = excluded.joined_have, creator = excluded.creator, handovers = excluded.handovers, signed = NULL, sig = NULL`,
		c.rec.StreamID, mustMessage("ChannelRecord", c.rec), c.creator, string(mustJSON(c.inviters)), c.deadline, string(mustJSON(append([]string{}, also...))),
		c.moveID, string(mustJSON(append([]string{}, c.declined...))), string(mustJSON(c.joinedHave)), string(mustJSON(append([]handover{}, c.handovers...))))
	if err == nil {
		h.forgetRoute(c.rec.StreamID)
	}
	return err
}

// relist is a change to the server list: a new epoch, a key-maker, and what
// the change was. prefer is the server the rule names (the home server of
// the manager who approved or invited); when it is not a member, or for an
// open join, the longest-standing member makes the key.
//
// If the epoch the record called for never began, what that change was is
// carried into this one: the maker of the new epoch owes history to every
// server that has joined since the last epoch that did begin.
func (h *Hub) relist(c *channel, joined, left []string, prefer string) {
	if !c.started {
		for _, n := range c.rec.Joined {
			if c.rec.Has(n) && !has(joined, n) {
				joined = append(joined, n)
			}
		}
		for _, n := range c.rec.Left {
			if !c.rec.Has(n) && !has(left, n) {
				left = append(left, n)
			}
		}
	}
	c.rec.Epoch++
	c.started = false
	c.rec.Joined, c.rec.Left = joined, left
	c.rec.KeyMaker = ""
	if c.rec.Has(prefer) {
		c.rec.KeyMaker = prefer
	} else if len(c.rec.Servers) > 0 {
		// The longest-standing member; of those this hub can reach now, if
		// the cluster is split (section 23b).
		c.rec.KeyMaker = c.rec.Servers[0]
		for _, n := range c.rec.Servers {
			if h.reachable(n) {
				c.rec.KeyMaker = n
				break
			}
		}
	}
	c.deadline = int64(h.now()) + makerWait.Load()/int64(time.Millisecond)
	if len(c.rec.Servers) == 0 {
		c.rec.Closed, c.deadline = true, 0
	}
}

func without(list []string, v string) []string {
	out := []string{}
	for _, x := range list {
		if x != v {
			out = append(out, x)
		}
	}
	return out
}

func has(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// dropServer takes a server out of a record: its place in the list, an
// invitation or a request, and its accounts' places among the managers and
// the moderators, who cannot act through a server that is not there.
func dropServer(c *channel, name string) {
	c.rec.Servers, c.rec.Invited, c.rec.Requests = without(c.rec.Servers, name), without(c.rec.Invited, name), without(c.rec.Requests, name)
	delete(c.inviters, name)
	delete(c.joinedHave, name)
	for _, list := range []*[]wire.ChannelAccount{&c.rec.Managers, &c.rec.Moderators} {
		kept := []wire.ChannelAccount{}
		for _, m := range *list {
			if m.Server != name {
				kept = append(kept, m)
			}
		}
		*list = kept
	}
}

// dropAccount is a list of a record's accounts without one of them.
func dropAccount(list []wire.ChannelAccount, accountID, server string) []wire.ChannelAccount {
	out := []wire.ChannelAccount{}
	for _, m := range list {
		if m.AccountID != accountID || m.Server != server {
			out = append(out, m)
		}
	}
	return out
}

// recordFrame signs a channel's record as it stands. Beside it, unsigned,
// goes what the log holds of each origin at this moment: a server that is
// joining starts from there (section 6).
func (h *Hub) recordFrame(c channel) (wire.Frame, error) {
	if len(c.signed) > 0 {
		// Another hub's channel: its record goes on as that hub signed it.
		return wire.Frame{Frame: wire.FChannel, Signed: c.signed, Sig: c.sig, Have: h.haveOf(c.rec.StreamID), HomeHub: c.rec.HomeHub}, nil
	}
	f, err := wire.SignBlock(wire.FChannel, c.rec, h.keys)
	if err != nil {
		return f, err
	}
	f.Have, f.HomeHub = h.haveOf(c.rec.StreamID), c.rec.HomeHub
	return f, nil
}

// sendRecord sends a record to servers, each with the `have` that is its
// own: for a server the hub admitted, what the log held when it did.
func (h *Hub) sendRecord(c channel, f wire.Frame, to []string) {
	for _, name := range to {
		// A record by which the home changed hands goes first: a server
		// takes the new home's signature only once it has seen the old
		// home name it.
		for _, ho := range c.handovers {
			if ho.Version < c.rec.Version {
				h.sendTo([]string{name}, wire.Frame{Frame: wire.FChannel, Signed: ho.Signed, Sig: ho.Sig, HomeHub: c.rec.HomeHub})
			}
		}
		out := f
		if at, ok := c.joinedHave[name]; ok {
			out.Have = at
		}
		h.sendTo([]string{name}, out)
	}
}

// audience is who a record is sent to: while it is only prepared, the server
// that brought it and nobody else; otherwise the servers that have just
// joined first (the key their key-maker sends follows on its heels), the
// channel's other servers, and the servers invited to it.
func audience(c channel) []string {
	if c.rec.Prepared {
		return []string{c.creator}
	}
	var to []string
	for _, l := range [][]string{c.rec.Joined, c.rec.Servers, c.rec.Invited} {
		for _, n := range l {
			if !has(to, n) && (c.rec.Has(n) || has(c.rec.Invited, n)) {
				to = append(to, n)
			}
		}
	}
	return to
}

// tellChannels signs and sends every record that has changed since it was
// last sent: to the channel's servers, to servers invited to it, and to
// those named once in also. An operator's command in another process leaves
// its changes for the serving hub to send this way.
func (h *Hub) tellChannels() {
	rows, err := h.db.Query(`SELECT stream_id FROM channels WHERE told = 0`)
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
		m := h.lock(id)
		c, err := loadChannel(h.db, id)
		var also string
		h.db.QueryRow(`SELECT also FROM channels WHERE stream_id = ?`, id).Scan(&also)
		if err == nil {
			if f, err := h.recordFrame(c); err == nil {
				to := audience(c)
				var extra []string
				json.Unmarshal([]byte(also), &extra)
				for _, n := range extra {
					if !has(to, n) {
						to = append(to, n)
					}
				}
				h.sendRecord(c, f, to)
				// And to the hubs whose servers it concerns.
				h.sendRecordToTrunks(c, f, extra)
			}
			// Marked as sent only if it is still the version that was sent:
			// an operator's command, in this process or another, does not
			// take the stream's lock, and a change it made between the read
			// above and this line would otherwise be marked sent unsent.
			h.db.Exec(`UPDATE channels SET told = 1, also = '[]' WHERE stream_id = ? AND (SELECT version FROM streams WHERE id = ?) = ?`, id, id, c.rec.Version)
		}
		m.Unlock()
	}
}

// resendChannels sends a server that has just connected the records of the
// channels it is in or invited to, since it may have missed a change, and
// the word that a request of its to join was declined while it was away.
func (h *Hub) resendChannels(sc *serverConn) {
	rows, err := h.db.Query(`SELECT stream_id FROM channels`)
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
		m := h.lock(id)
		c, err := loadChannel(h.db, id)
		if err == nil && has(c.declined, sc.name) {
			sc.conn.Send(wire.Frame{Frame: wire.FError, Code: wire.EForbidden, StreamID: id, Message: "The request to join was declined."})
			h.db.Exec(`UPDATE channels SET declined = ? WHERE stream_id = ?`, string(mustJSON(without(c.declined, sc.name))), id)
		}
		if err == nil && !c.rec.Closed && has(audience(c), sc.name) {
			if f, err := h.recordFrame(c); err == nil {
				h.sendRecord(c, f, []string{sc.name})
			}
		}
		m.Unlock()
	}
}

// checkMakers names the next key-maker for each channel whose named one let
// its time pass without starting the new epoch. The hub sees which epoch an
// envelope is under, and nothing else of it.
func (h *Hub) checkMakers() {
	now := int64(h.now())
	rows, err := h.db.Query(`SELECT stream_id FROM channels WHERE maker_deadline > 0 AND maker_deadline <= ? AND signed IS NULL`, now)
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
		m := h.lock(id)
		h.tx(func(tx *sql.Tx) error {
			c, err := loadChannel(tx, id)
			if err != nil || c.rec.Closed || c.rec.Prepared {
				return err
			}
			if _, moving := offerOf(tx, id); moving {
				return nil // its home is on offer; the next key-maker is named when that is settled
			}
			if c.started || len(c.rec.Servers) < 2 {
				// Done; or there is nobody else to ask, and the one server
				// left makes its key whenever it returns.
				_, err := tx.Exec(`UPDATE channels SET maker_deadline = 0 WHERE stream_id = ?`, id)
				return err
			}
			// The next in line: the server after the one that did not, in
			// the order they joined, going round. A server that has only
			// just joined is passed over while there is another to ask: it
			// holds nothing of the channel yet, and is itself owed its
			// history by whoever makes the key.
			at := 0
			for i, n := range c.rec.Servers {
				if n == c.rec.KeyMaker {
					at = i
				}
			}
			next := c.rec.Servers[(at+1)%len(c.rec.Servers)]
			for step := 1; step < len(c.rec.Servers); step++ {
				// So is a server this hub cannot reach while the cluster is
				// split.
				if n := c.rec.Servers[(at+step)%len(c.rec.Servers)]; !has(c.rec.Joined, n) && h.reachable(n) {
					next = n
					break
				}
			}
			// A new number, so that a key the first maker made late can
			// never be taken for this one. What the change was stays.
			c.rec.Epoch++
			c.rec.KeyMaker = next
			c.deadline = now + makerWait.Load()/int64(time.Millisecond)
			return h.saveChannel(tx, &c, nil)
		})
		m.Unlock()
	}
}

func validText(name, description string) error {
	clean, ok := model.CleanChannelName(name)
	if !ok || clean != name {
		return wire.Errf(wire.EBadFrame, "A channel's name is 1 to 64 characters with no control characters.")
	}
	if utf8.RuneCountInString(description) > 1000 {
		return wire.Errf(wire.EBadFrame, "A channel's description is at most 1,000 characters.")
	}
	return nil
}

// channelCreate records a channel a server shares to the hub. The hub owns
// it from the first moment; the person who shared it is its first
// manager.
//
// A channel moved from a direct link (section 21a) arrives with both of its
// servers, its managers, and the other server's signed agreement. The hub
// only prepares it: it is recorded, nobody but the sender is told, and
// nothing is carried in it, until the sender's signed channel_commit
// arrives. The sender may close it again until then.
func (h *Hub) channelCreate(sc *serverConn, f wire.Frame) error {
	var cc wire.ChannelCreate
	if err := wire.VerifyBlock(f, sc.key, &cc); err != nil {
		return err
	}
	if !wire.ValidID(cc.StreamID) || !wire.ValidListing(cc.Listing) || !wire.ValidID(cc.Creator.AccountID) || cc.Creator.Server != sc.name {
		return wire.Errf(wire.EBadFrame, "A channel is created with an identifier, a listing mode and its creator, an account of the server that sends it.")
	}
	if err := validText(cc.Name, cc.Description); err != nil {
		return err
	}
	if cc.Private && cc.Listing != wire.ListingUnlisted {
		// A private channel is never listed (ruling R94).
		return wire.Errf(wire.EBadFrame, "A private channel is shared unlisted, and stays so.")
	}
	servers, mods, epoch := []string{sc.name}, []wire.ChannelAccount{cc.Creator}, int64(1)
	moved := cc.MoveID != ""
	if !moved {
		// The channel comes to the hub with its managers: each becomes a
		// manager. They are accounts of the server that shares it.
		for _, m := range cc.Managers {
			if !wire.ValidID(m.AccountID) || m.Server != sc.name {
				return wire.Errf(wire.EBadFrame, "A manager of a channel shared here is an account of the server that shares it.")
			}
			if !(wire.ChannelRecord{Managers: mods}).Manages(m.AccountID, m.Server) {
				mods = append(mods, m)
			}
		}
	}
	if moved {
		// Exactly its two servers, the sender on this hub and the other on
		// it or on another hub of its cluster; the other one's signed
		// agreement to this move onto this hub; and managers who are
		// accounts of one or the other, those of the other server being
		// exactly the managers it signed for.
		if !wire.ValidID(cc.MoveID) || len(cc.Servers) != 2 || cc.Servers[0] == cc.Servers[1] || !has(cc.Servers, sc.name) || cc.Epoch < 2 {
			return wire.Errf(wire.EBadFrame, "A moved channel names its move, its two servers and its next key epoch.")
		}
		other := without(cc.Servers, sc.name)[0]
		otherKey, ok := h.serverKey(other)
		if !ok {
			return wire.Errf(wire.EUnknownServer, "%s is not on this hub or its cluster.", other)
		}
		var acc wire.MoveAccept
		if err := wire.VerifyBytes(wire.BMoveAccept, cc.Accept, cc.AcceptSig, otherKey, &acc); err != nil {
			return wire.Errf(wire.EBadSignature, "%s has not signed its agreement to the move.", other)
		}
		if acc.MoveID != cc.MoveID || acc.StreamID != cc.StreamID || acc.Hub != h.id.Name || !bytes.Equal(acc.HubKey, h.keys.SignPublic()) ||
			acc.Owner != sc.name || acc.Server != other {
			return wire.Errf(wire.EForbidden, "%s agreed to something else.", other)
		}
		if acc.ExpiresAt.Before(h.now()) {
			return wire.Errf(wire.EConflict, "The agreement to the move has expired.")
		}
		theirs := 0
		for _, m := range cc.Managers {
			switch {
			case !wire.ValidID(m.AccountID) || !has(cc.Servers, m.Server):
				return wire.Errf(wire.EBadFrame, "A manager is an account of a server in the channel.")
			case m.Server == other:
				theirs++
				if !(wire.ChannelRecord{Managers: acc.Managers}).Manages(m.AccountID, other) {
					return wire.Errf(wire.EForbidden, "%s did not name that account as a manager.", other)
				}
			}
		}
		if theirs != len(acc.Managers) {
			return wire.Errf(wire.EForbidden, "Every manager %s named stays a manager.", other)
		}
		// The sender first: it is named to make the first key here.
		servers, mods, epoch = []string{sc.name, other}, append([]wire.ChannelAccount{}, cc.Managers...), cc.Epoch
	}
	lim, err := h.Limits(sc.name)
	if err != nil {
		return err
	}
	m := h.lock(cc.StreamID)
	now := int64(h.now())
	err = h.tx(func(tx *sql.Tx) error {
		var state string
		if tx.QueryRow(`SELECT state FROM streams WHERE id = ?`, cc.StreamID).Scan(&state) == nil {
			// The identifier is taken. The server that prepared a channel
			// for a move may say so again (it did not hear the answer), and
			// may prepare it anew for another move after one it abandoned.
			old, err := loadChannel(tx, cc.StreamID)
			switch {
			case err != nil || !moved || old.creator != sc.name || !old.rec.Prepared:
				return wire.Errf(wire.EConflict, "That stream is already open.")
			case old.moveID == cc.MoveID && !old.rec.Closed:
				_, err := tx.Exec(`UPDATE channels SET told = 0 WHERE stream_id = ?`, cc.StreamID)
				return err
			case old.moveID == cc.MoveID || !old.rec.Closed:
				return wire.Errf(wire.EConflict, "That move was abandoned.")
			}
			for _, table := range []string{"stream_servers", "channels", "origins"} {
				if _, err := tx.Exec(`DELETE FROM `+table+` WHERE stream_id = ?`, cc.StreamID); err != nil {
					return err
				}
			}
			if _, err := tx.Exec(`DELETE FROM streams WHERE id = ?`, cc.StreamID); err != nil {
				return err
			}
		}
		var made int64
		tx.QueryRow(`SELECT count(*) FROM channels c JOIN streams s ON s.id = c.stream_id WHERE c.creator = ? AND s.state <> 'closed'`, sc.name).Scan(&made)
		if made >= lim.Streams {
			return wire.Errf(wire.ERateLimited, "This server has shared as many channels as the hub allows.")
		}
		// The hub is the channel's owner, its home, and its time reference.
		// A moved channel's stream stands at the epoch before the one the
		// record calls for, as a channel made here stands at 0 before 1.
		if _, err := tx.Exec(`INSERT INTO streams (id, owner, version, next_seq, state, created_at, home_hub, epoch) VALUES (?, ?, 0, 1, 'open', ?, ?, ?)`,
			cc.StreamID, h.id.Name, now, h.id.Name, epoch-1); err != nil {
			return err
		}
		c := channel{creator: sc.name, moveID: cc.MoveID, inviters: map[string]string{}, rec: wire.ChannelRecord{StreamID: cc.StreamID, Name: cc.Name,
			Description: cc.Description, Listing: cc.Listing, Servers: servers, Managers: mods, Epoch: epoch, KeyMaker: sc.name,
			Joined: []string{}, Prepared: moved, Private: cc.Private}}
		return h.saveChannel(tx, &c, nil)
	})
	m.Unlock()
	if err != nil {
		return err
	}
	h.tellChannels()
	return nil
}

// channelCommit activates a channel prepared for a move (section 21a). The
// frame is the former owner server's signed word that it has committed the
// move; either of the channel's two servers may hand it over, so that the
// channel does not wait on a server that committed and then went away. From
// here on the hub is the channel's owner, relay and time reference.
func (h *Hub) channelCommitAs(sender string, f wire.Frame) error {
	var peek wire.MoveCommit
	if err := wire.PeekBlock(f, &peek); err != nil {
		return err
	}
	if !wire.ValidID(peek.StreamID) {
		return wire.Errf(wire.EBadFrame, "The frame names a channel.")
	}
	m := h.lock(peek.StreamID)
	err := h.tx(func(tx *sql.Tx) error {
		c, err := loadChannel(tx, peek.StreamID)
		if err != nil && len(f.Inner) > 0 {
			// The hub does not hold the channel it prepared: its database
			// was lost, or put back from before the move. The server has
			// brought the record this hub signed when it prepared it, and
			// the hub prepares it again from that.
			if err := h.restorePrepared(tx, sender, f, peek); err != nil {
				return err
			}
			c, err = loadChannel(tx, peek.StreamID)
		}
		if err != nil {
			return err
		}
		if !c.rec.Has(sender) {
			return wire.Errf(wire.ENotInStream, "%s is not in that channel.", sender)
		}
		key, ok := h.serverKeyIn(tx, c.creator)
		if !ok {
			return wire.Errf(wire.EUnknownServer, "%s is not on this hub.", c.creator)
		}
		var mc wire.MoveCommit
		if err := wire.VerifyBlock(f, key, &mc); err != nil {
			return err
		}
		if c.moveID == "" || mc.MoveID != c.moveID || mc.Hub != h.id.Name || mc.Owner != c.creator {
			return wire.Errf(wire.EForbidden, "That is not the commit of this channel's move.")
		}
		if c.rec.Closed {
			return wire.Errf(wire.EConflict, "That move was abandoned.")
		}
		if !c.rec.Prepared {
			// Activated already: the record goes out again.
			_, err := tx.Exec(`UPDATE channels SET told = 0 WHERE stream_id = ?`, c.rec.StreamID)
			return err
		}
		// Each origin starts here from where it stood when the stream left
		// the direct link: its counter and its time only grow from there.
		for _, name := range c.rec.Servers {
			if _, err := tx.Exec(`INSERT INTO origins (stream_id, name, last_n, last_at) VALUES (?, ?, ?, ?)
				ON CONFLICT (stream_id, name) DO UPDATE SET last_n = excluded.last_n, last_at = excluded.last_at`,
				c.rec.StreamID, name, mc.Have[name], mc.HaveAt[name]); err != nil {
				return err
			}
		}
		if mc.Epoch < 1 {
			return wire.Errf(wire.EBadFrame, "The commit names the key epoch the stream ended its direct life under.")
		}
		if _, err := tx.Exec(`UPDATE streams SET epoch = ? WHERE id = ?`, mc.Epoch, c.rec.StreamID); err != nil {
			return err
		}
		// The managers are every manager of the channel on either server
		// at the commit: the other server's as it signed for them when it
		// agreed (checked when the channel was prepared), the committing
		// server's as it read them in the transaction that committed.
		mods := []wire.ChannelAccount{}
		for _, m := range c.rec.Managers {
			if m.Server != c.creator {
				mods = append(mods, m)
			}
		}
		for _, m := range mc.Managers {
			if m.Server == c.creator && wire.ValidID(m.AccountID) && !(wire.ChannelRecord{Managers: mods}).Manages(m.AccountID, m.Server) {
				mods = append(mods, m)
			}
		}
		if len(mods) == 0 {
			return wire.Errf(wire.EBadFrame, "A channel has at least one manager.")
		}
		c.rec.Managers = mods
		c.rec.Prepared = false
		c.rec.Epoch, c.rec.KeyMaker = mc.Epoch+1, c.creator
		c.rec.Joined, c.rec.Left = []string{}, []string{}
		c.deadline = int64(h.now()) + makerWait.Load()/int64(time.Millisecond)
		return h.saveChannel(tx, &c, nil)
	})
	m.Unlock()
	if err != nil {
		return err
	}
	h.tellChannels()
	return nil
}

// restorePrepared prepares a moved channel again on a hub that no longer
// holds it, from two things only the move itself could have produced: the
// prepared record this hub signed (its own signature is the proof that it
// prepared exactly this channel, for these two servers, with these
// managers), and the former owner server's signed commit of the move. The
// caller then activates it as if it had never been lost.
//
// It is refused for a stream this hub holds in any state, for a record that
// is not a prepared one of this hub's own under its present key, for servers
// that are not on the hub any more, and for a record older than the time the
// hub keeps a closed channel: after that, what is gone is gone.
func (h *Hub) restorePrepared(tx *sql.Tx, sender string, f wire.Frame, mc wire.MoveCommit) error {
	var inner wire.Frame
	var rec wire.ChannelRecord
	if readFrame(f.Inner, &inner) != nil {
		return wire.Errf(wire.EBadFrame, "What came with the commit is not a channel record.")
	}
	if err := wire.VerifyBlock(inner, h.keys.SignPublic(), &rec); err != nil {
		return wire.Errf(wire.EUnknownStream, "No such channel here, and the record brought with the commit is not one this hub signed.")
	}
	var one int
	if tx.QueryRow(`SELECT 1 FROM streams WHERE id = ?`, mc.StreamID).Scan(&one) == nil {
		return wire.Errf(wire.EConflict, "That stream is already here.")
	}
	creator := rec.KeyMaker
	switch {
	case rec.StreamID != mc.StreamID || rec.Hub != h.id.Name || !bytes.Equal(rec.HubKey, h.keys.SignPublic()) || !rec.Prepared || rec.Closed:
		return wire.Errf(wire.EForbidden, "The record brought with the commit is not this hub's prepared record of that channel.")
	case len(rec.Servers) != 2 || !rec.Has(sender) || !rec.Has(creator) || rec.Epoch < 2 || !wire.ValidID(mc.MoveID):
		return wire.Errf(wire.EBadFrame, "A prepared channel has its two servers, one of which prepared it.")
	case mc.Owner != creator || mc.Hub != h.id.Name:
		return wire.Errf(wire.EForbidden, "That is not the commit of this channel's move.")
	case rec.At.Before(h.now().Add(-time.Duration(h.cfg.KeepDays) * 24 * time.Hour)):
		return wire.Errf(wire.EConflict, "That channel was prepared too long ago to be brought back.")
	}
	key, ok := h.serverKeyIn(tx, creator)
	if !ok {
		return wire.Errf(wire.EUnknownServer, "%s is not on this hub.", creator)
	}
	for _, name := range rec.Servers {
		if _, ok := h.serverKeyIn(tx, name); !ok {
			return wire.Errf(wire.EUnknownServer, "%s is not on this hub.", name)
		}
	}
	var signed wire.MoveCommit
	if err := wire.VerifyBlock(wire.Frame{Frame: f.Frame, Signed: f.Signed, Sig: f.Sig}, key, &signed); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO streams (id, owner, version, next_seq, state, created_at, home_hub, epoch) VALUES (?, ?, 0, 1, 'open', ?, ?, ?)`,
		rec.StreamID, h.id.Name, int64(h.now()), h.id.Name, rec.Epoch-1); err != nil {
		return err
	}
	c := channel{creator: creator, moveID: mc.MoveID, inviters: map[string]string{}, rec: wire.ChannelRecord{StreamID: rec.StreamID, Name: rec.Name,
		Description: rec.Description, Listing: rec.Listing, Servers: rec.Servers, Managers: rec.Managers, Epoch: rec.Epoch, KeyMaker: creator,
		Joined: []string{}, Prepared: true, Private: rec.Private}}
	h.log.Warn("a moved channel this hub had prepared and no longer held was prepared again from its own record and the move's commit",
		"stream", rec.StreamID, "by", sender, "move", mc.MoveID)
	return h.saveChannel(tx, &c, nil)
}

// channelAction is a manager's action (channel_action) or a server's wish to
// join (join). Both are signed by the server on this connection; a
// manager's is accepted only when its account is on the channel's
// manager list as an account of that server.
func (h *Hub) channelAction(sc *serverConn, f wire.Frame, raw []byte) error {
	var a wire.ChannelAction
	if err := wire.VerifyBlock(f, sc.key, &a); err != nil {
		return err
	}
	// A channel whose home is another hub is that hub's to change: the
	// request is carried there, or refused if the way is cut.
	if st, err := loadStream(h.db, a.StreamID); err == nil && st.homeHub != "" && st.homeHub != h.id.Name {
		return h.askHome(sc.name, st.homeHub, raw)
	}
	return h.channelActionAs(sc.name, sc.key, f)
}

// channelActionAs is channelAction for a named server with a known key: one
// of this hub's own, or one of another hub's whose request was carried here.
func (h *Hub) channelActionAs(actor string, actorKey []byte, f wire.Frame) error {
	sc := &serverConn{name: actor, key: actorKey}
	var a wire.ChannelAction
	if err := wire.VerifyBlock(f, sc.key, &a); err != nil {
		return err
	}
	if !wire.ValidID(a.StreamID) {
		return wire.Errf(wire.EBadFrame, "The frame names a channel.")
	}
	m := h.lock(a.StreamID)
	var declined string
	err := h.tx(func(tx *sql.Tx) error {
		c, err := loadChannel(tx, a.StreamID)
		if err != nil {
			return err
		}
		if c.rec.Closed {
			return wire.Errf(wire.EUnknownStream, "That channel is closed.")
		}
		if !h.owned(c) {
			return wire.Errf(wire.EUnavailable, "This hub is not that channel's home.")
		}
		if c.rec.Prepared {
			// Prepared for a move and not yet committed: the server that
			// brought it may take it away again, and nothing else is done
			// to it.
			if f.Frame == wire.FChannelAction && a.Action == wire.ActClose && c.creator == sc.name {
				c.rec.Closed, c.deadline = true, 0
				return h.saveChannel(tx, &c, nil)
			}
			return wire.Errf(wire.EUnknownStream, "No such channel here.")
		}
		if f.Frame == wire.FJoin {
			return h.join(tx, &c, sc.name)
		}
		if a.By.Server != sc.name || !c.rec.Manages(a.By.AccountID, sc.name) {
			return wire.Errf(wire.EForbidden, "That account is not a manager of the channel.")
		}
		onHub := func(name string) error {
			if _, ok := h.serverKeyIn(tx, name); !ok || name == "" {
				return wire.Errf(wire.EUnknownServer, "%s is not on this hub or its cluster.", name)
			}
			return nil
		}
		var also []string
		switch a.Action {
		case wire.ActInvite:
			if err := onHub(a.Server); err != nil {
				return err
			}
			if c.rec.Has(a.Server) || has(c.rec.Invited, a.Server) {
				break // already so; the record goes out again all the same
			}
			if has(c.rec.Requests, a.Server) {
				// It had asked already: inviting it is approving it.
				return h.admit(tx, &c, a.Server, sc.name)
			}
			c.rec.Invited = append(c.rec.Invited, a.Server)
			c.inviters[a.Server] = sc.name
		case wire.ActApprove:
			if !has(c.rec.Requests, a.Server) {
				return wire.Errf(wire.ENotFound, "%s has not asked to join.", a.Server)
			}
			return h.admit(tx, &c, a.Server, sc.name)
		case wire.ActDecline:
			if !has(c.rec.Requests, a.Server) {
				return wire.Errf(wire.ENotFound, "%s has not asked to join.", a.Server)
			}
			c.rec.Requests = without(c.rec.Requests, a.Server)
			declined = a.Server
			if !h.remote(declined) && h.connOf(declined) == nil && !has(c.declined, declined) {
				c.declined = append(c.declined, declined) // told when it next connects
			}
		case wire.ActRemoveServer:
			switch {
			case c.rec.Has(a.Server):
				dropServer(&c, a.Server)
				h.relist(&c, []string{}, []string{a.Server}, sc.name)
				also = []string{a.Server}
			case has(c.rec.Invited, a.Server):
				dropServer(&c, a.Server) // the invitation is withdrawn
				also = []string{a.Server}
			default:
				return wire.Errf(wire.ENotFound, "%s is not in the channel.", a.Server)
			}
		case wire.ActDescribe:
			name, description := c.rec.Name, c.rec.Description
			if a.Name != nil {
				name = *a.Name
			}
			if a.Description != nil {
				description = *a.Description
			}
			if err := validText(name, description); err != nil {
				return err
			}
			c.rec.Name, c.rec.Description = name, description
		case wire.ActSetListing:
			if !wire.ValidListing(a.Listing) {
				return wire.Errf(wire.EBadFrame, "The listing mode is unlisted, request or open.")
			}
			if c.rec.Private && a.Listing != wire.ListingUnlisted {
				// Not by any manager, nor by anyone else (ruling R94).
				return wire.Errf(wire.EForbidden, "A private channel is never listed.")
			}
			c.rec.Listing = a.Listing
		case wire.ActAddManager, wire.ActAddModerator:
			if a.Account == nil || !wire.ValidID(a.Account.AccountID) || !c.rec.Has(a.Account.Server) {
				return wire.Errf(wire.EBadFrame, "A manager or moderator is an account of an instance in the channel.")
			}
			list := &c.rec.Managers
			if a.Action == wire.ActAddModerator {
				list = &c.rec.Moderators
			}
			if !(wire.ChannelRecord{Managers: *list}).Manages(a.Account.AccountID, a.Account.Server) {
				*list = append(*list, *a.Account)
			}
		case wire.ActRemoveManager, wire.ActRemoveModerator:
			list := &c.rec.Managers
			if a.Action == wire.ActRemoveModerator {
				list = &c.rec.Moderators
			}
			if a.Account == nil || !(wire.ChannelRecord{Managers: *list}).Manages(a.Account.AccountID, a.Account.Server) {
				return wire.Errf(wire.ENotFound, "That account is not on that list of the channel.")
			}
			if a.Action == wire.ActRemoveManager && len(c.rec.Managers) == 1 {
				return wire.Errf(wire.EConflict, "A channel keeps at least one manager.")
			}
			*list = dropAccount(*list, a.Account.AccountID, a.Account.Server)
		case wire.ActClose:
			c.rec.Closed, c.deadline = true, 0
		default:
			return wire.Errf(wire.EBadFrame, "Unknown action %q.", a.Action)
		}
		return h.saveChannel(tx, &c, also)
	})
	m.Unlock()
	if err != nil {
		var e *wire.Error
		if errors.As(err, &e) {
			e.StreamID = a.StreamID
		}
		return err
	}
	if declined != "" {
		// The server that asked is told no, and nothing else of the channel.
		no := wire.Frame{Frame: wire.FError, Code: wire.EForbidden, StreamID: a.StreamID, Message: "The request to join was declined."}
		if h.remote(declined) {
			h.forward(declined, mustFrame(no)) // its own hub keeps the word if it is not connected
		}
		h.sendTo([]string{declined}, no)
	}
	h.tellChannels()
	return nil
}

// admit adds a server to a channel and names who makes the next key.
func (h *Hub) admit(tx *sql.Tx, c *channel, name, maker string) error {
	if len(c.rec.Servers) >= maxStreamServers {
		return wire.Errf(wire.ETooLarge, "A stream has at most %d servers.", maxStreamServers)
	}
	c.rec.Invited, c.rec.Requests = without(c.rec.Invited, name), without(c.rec.Requests, name)
	delete(c.inviters, name)
	// From this transaction on the hub delivers the stream to it. What the
	// log holds of each origin now is where it starts: what came before
	// reaches it as history, whenever it hears that it is in.
	if c.joinedHave == nil {
		c.joinedHave = map[string]map[string]int64{}
	}
	c.joinedHave[name] = haveIn(tx, c.rec.StreamID)
	c.rec.Servers = append(c.rec.Servers, name)
	h.relist(c, []string{name}, []string{}, maker)
	return h.saveChannel(tx, c, nil)
}

// join is a server's own wish to be in a channel: taken at once when it was
// invited or the channel is open, recorded as a request when the channel is
// listed for requests, and refused otherwise.
func (h *Hub) join(tx *sql.Tx, c *channel, name string) error {
	switch {
	case c.rec.Has(name):
		// In it already; the record goes to it again.
		_, err := tx.Exec(`UPDATE channels SET told = 0 WHERE stream_id = ?`, c.rec.StreamID)
		return err
	case has(c.rec.Invited, name):
		// The home server of the manager who invited it makes the key.
		return h.admit(tx, c, name, c.inviters[name])
	case c.rec.Listing == wire.ListingOpen:
		// The longest-standing member server makes the key.
		return h.admit(tx, c, name, "")
	case c.rec.Listing == wire.ListingRequest:
		if has(c.rec.Requests, name) {
			return nil
		}
		c.rec.Requests = append(c.rec.Requests, name)
		c.declined = without(c.declined, name)
		return h.saveChannel(tx, c, nil)
	}
	return wire.Errf(wire.EForbidden, "That channel is joined by a manager's invitation.")
}

// channelLeave takes a server that asked to leave out of a channel: out of
// its server list, or off the list of those invited or asking.
func (h *Hub) channelLeave(name, stream string) error {
	err := h.tx(func(tx *sql.Tx) error {
		c, err := loadChannel(tx, stream)
		if err != nil {
			return err
		}
		member := c.rec.Has(name)
		if !member && !has(c.rec.Invited, name) && !has(c.rec.Requests, name) {
			return wire.Errf(wire.ENotInStream, "A server leaves a channel it is in.")
		}
		dropServer(&c, name)
		if member {
			h.relist(&c, []string{}, []string{name}, "")
		}
		return h.saveChannel(tx, &c, []string{name})
	})
	return err
}

// directory answers a server with the channels it may see without being
// invited: those listed as request or open. For each: name, description,
// listing mode, managers and how many servers are in it, and the asking
// server's own place in it.
func (h *Hub) directory(sc *serverConn, f wire.Frame) error {
	d := wire.Directory{Hub: h.id.Name, HubKey: wire.Bytes(h.keys.SignPublic()), At: h.now(), Channels: []wire.DirectoryEntry{}}
	infos, err := h.Channels()
	if err != nil {
		return err
	}
	for _, c := range infos {
		if c.Listing == wire.ListingUnlisted || c.Private {
			continue
		}
		e := wire.DirectoryEntry{StreamID: c.ID, Name: c.Name, Description: c.Description, Listing: c.Listing,
			Managers: c.Managers, Servers: len(c.Servers)}
		switch {
		case has(c.Servers, sc.name):
			e.Yours = "member"
		case has(c.Invited, sc.name):
			e.Yours = "invited"
		case has(c.Requests, sc.name):
			e.Yours = "requested"
		}
		d.Channels = append(d.Channels, e)
	}
	out, err := wire.SignBlock(wire.FDirectory, d, h.keys)
	if err != nil {
		return err
	}
	out.Re = f.ID
	return sc.conn.Send(out)
}

// ---------------------------------------------------------------- operator

// ChannelInfo is a channel as `churr-hub channels` lists it: everything the
// hub records of it. There is no content in it, and no key.
type ChannelInfo struct {
	ID, Name, Description, Listing string
	Servers, Invited, Requests     []string
	Managers, Moderators           []wire.ChannelAccount
	KeyMaker                       string
	Epoch, Version                 int64
	// Cursor is the last position written to the channel's log: a count
	// of what the hub has carried in it, and no more.
	Cursor int64
	// Home is the channel's home hub: this hub for a channel it owns,
	// another hub of the cluster for one it only carries or lists.
	Home string
	// Private: the channel is private for good, and never listed.
	Private bool
}

// Channels lists the open channels the hub owns and, with Home naming
// another hub, those of its cluster it carries or lists.
func (h *Hub) Channels() ([]ChannelInfo, error) {
	rows, err := h.db.Query(`SELECT c.stream_id, s.next_seq FROM channels c JOIN streams s ON s.id = c.stream_id WHERE s.state = 'open' ORDER BY s.created_at, s.id`)
	if err != nil {
		return nil, err
	}
	type row struct {
		id   string
		next int64
	}
	var ids []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.next); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, r)
	}
	rows.Close()
	out := []ChannelInfo{}
	for _, r := range ids {
		c, err := loadChannel(h.db, r.id)
		if err != nil {
			return nil, err
		}
		out = append(out, ChannelInfo{ID: r.id, Name: c.rec.Name, Description: c.rec.Description, Listing: c.rec.Listing, Servers: c.rec.Servers,
			Invited: c.rec.Invited, Requests: c.rec.Requests, Managers: c.rec.Managers, Moderators: c.rec.Moderators, KeyMaker: c.rec.KeyMaker, Epoch: c.rec.Epoch,
			Version: c.rec.Version, Cursor: r.next - 1, Home: c.rec.HomeHub, Private: c.rec.Private})
	}
	return out, nil
}

// operate changes one channel's record from an operator's command. The
// serving hub signs and sends the new record within a second.
func (h *Hub) operate(stream string, fn func(c *channel) ([]string, error)) error {
	return h.tx(func(tx *sql.Tx) error {
		c, err := loadChannel(tx, stream)
		if err != nil {
			return errors.New("there is no such channel")
		}
		if c.rec.Closed {
			return errors.New("that channel is closed")
		}
		if !h.owned(c) {
			return fmt.Errorf("that channel's home is %s; its record is changed there", c.rec.HomeHub)
		}
		also, err := fn(&c)
		if err != nil {
			return err
		}
		return h.saveChannel(tx, &c, also)
	})
}

// Appoint makes an account a manager of a channel: the operator's last
// resort when a channel has no manager who will act. who is
// username@server; the server must be in the channel.
func (h *Hub) Appoint(stream, accountID, who string) error {
	username, server, ok := strings.Cut(strings.ToLower(who), "@")
	if !ok || username == "" || !wire.ValidID(accountID) {
		return errors.New("name the account by its identifier and as username@server")
	}
	return h.operate(stream, func(c *channel) ([]string, error) {
		if !c.rec.Has(server) {
			return nil, fmt.Errorf("%s is not in that channel", server)
		}
		if !c.rec.Manages(accountID, server) {
			c.rec.Managers = append(c.rec.Managers, wire.ChannelAccount{AccountID: accountID, Server: server, Username: username})
		}
		return nil, nil
	})
}

// Dismiss takes an account off a channel's manager list.
func (h *Hub) Dismiss(stream, accountID string) error {
	return h.operate(stream, func(c *channel) ([]string, error) {
		mods, found := []wire.ChannelAccount{}, false
		for _, m := range c.rec.Managers {
			if m.AccountID == accountID {
				found = true
				continue
			}
			mods = append(mods, m)
		}
		if !found {
			return nil, errors.New("that account is not a manager of the channel")
		}
		c.rec.Managers = mods
		return nil, nil
	})
}

// Expel removes a server from one channel, leaving it on the hub.
func (h *Hub) Expel(stream, server string) error {
	server = strings.ToLower(server)
	return h.operate(stream, func(c *channel) ([]string, error) {
		if !c.rec.Has(server) && !has(c.rec.Invited, server) && !has(c.rec.Requests, server) {
			return nil, fmt.Errorf("%s is not in that channel", server)
		}
		member := c.rec.Has(server)
		dropServer(c, server)
		if member {
			h.relist(c, []string{}, []string{server}, "")
		}
		return []string{server}, nil
	})
}

// removeFromChannels takes a server the operator removed from the hub out
// of every channel, inside the transaction that removes it.
func (h *Hub) removeFromChannels(tx *sql.Tx, name string) error {
	rows, err := tx.Query(`SELECT c.stream_id FROM channels c JOIN streams s ON s.id = c.stream_id WHERE s.state <> 'closed' AND c.signed IS NULL`)
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	rows.Close()
	for _, id := range ids {
		c, err := loadChannel(tx, id)
		if err != nil {
			return err
		}
		member := c.rec.Has(name)
		if !member && !has(c.rec.Invited, name) && !has(c.rec.Requests, name) {
			continue
		}
		dropServer(&c, name)
		if member {
			h.relist(&c, []string{}, []string{name}, "")
		}
		if err := h.saveChannel(tx, &c, nil); err != nil {
			return err
		}
	}
	return nil
}
