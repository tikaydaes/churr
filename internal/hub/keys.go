package hub

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"sync"
	"time"

	"github.com/tikaydaes/churr/internal/link/wire"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/secret"
)

// This file is the hub as the keeper of the keys of the channels it is the
// home of (docs/LINK-PROTOCOL.md sections 10 and 23a, ruling R109). The hub
// owns those channels and is trusted as their owner: on every change to a
// channel's server list it makes the next key epoch's stream key, keeps it
// sealed at rest beside every earlier one, and sends it to each server in the
// list, sealed to that server. A server takes a hub-owned channel's key from
// the home hub and from nobody else. A hub that only carries a channel homed
// elsewhere holds none of its keys.
//
// Direct messages addressed to the hub itself are opened here. The only one
// it answers is key_request.

// keyPlace names a stream key's row for the sealing at rest.
func keyPlace(stream string, epoch int64) string {
	return stream + "/" + strconv.FormatInt(epoch, 10)
}

// beginEpoch makes the key of the epoch a record calls for, if this hub has
// not made it yet, and makes that epoch the stream's current one: from here
// the hub takes nothing under an older epoch. It runs in the transaction that
// saves the record.
func (h *Hub) beginEpoch(tx *sql.Tx, rec wire.ChannelRecord) error {
	var one int
	if tx.QueryRow(`SELECT 1 FROM stream_keys WHERE stream_id = ? AND epoch = ?`, rec.StreamID, rec.Epoch).Scan(&one) == nil {
		return nil
	}
	sealed, err := h.keys.Seal(secret.RandomBytes(32), "stream_keys", "sealed", keyPlace(rec.StreamID, rec.Epoch))
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO stream_keys (stream_id, epoch, sealed, servers, created_at) VALUES (?, ?, ?, ?, ?)`,
		rec.StreamID, rec.Epoch, sealed, string(mustJSON(orEmpty(rec.Servers))), int64(h.now())); err != nil {
		return err
	}
	_, err = tx.Exec(`UPDATE streams SET epoch = max(epoch, ?) WHERE id = ?`, rec.Epoch, rec.StreamID)
	return err
}

// streamKey returns one epoch's key of a channel this hub is the home of,
// and the servers the epoch began for.
func (h *Hub) streamKey(q querier, stream string, epoch int64) ([]byte, []string, error) {
	var sealed []byte
	var servers string
	err := q.QueryRow(`SELECT sealed, servers FROM stream_keys WHERE stream_id = ? AND epoch = ?`, stream, epoch).Scan(&sealed, &servers)
	if err != nil {
		return nil, nil, err
	}
	key, err := h.keys.Unseal(sealed, "stream_keys", "sealed", keyPlace(stream, epoch))
	if err != nil || len(key) != 32 {
		return nil, nil, errors.New("hub: a stream key could not be opened")
	}
	var list []string
	json.Unmarshal([]byte(servers), &list)
	return key, list, nil
}

// TestStreamKeys returns every key this hub holds for a stream, by epoch. It is for
// tests.
func (h *Hub) TestStreamKeys(stream string) map[int64][]byte {
	out := map[int64][]byte{}
	rows, err := h.db.Query(`SELECT epoch FROM stream_keys WHERE stream_id = ?`, stream)
	if err != nil {
		return out
	}
	var epochs []int64
	for rows.Next() {
		var e int64
		if rows.Scan(&e) == nil {
			epochs = append(epochs, e)
		}
	}
	rows.Close()
	for _, e := range epochs {
		if k, _, err := h.streamKey(h.db, stream, e); err == nil {
			out[e] = k
		}
	}
	return out
}

// boxKeyOf is the box key of a server anywhere in the cluster: what a direct
// message to it is sealed to.
func (h *Hub) boxKeyOf(name string) ([]byte, bool) {
	if rs, remote := h.topology().servers[name]; remote {
		return rs.boxKey, len(rs.boxKey) == 32
	}
	var box []byte
	err := h.db.QueryRow(`SELECT box_key FROM servers WHERE name = ? AND state = 'active'`, name).Scan(&box)
	return box, err == nil && len(box) == 32
}

// sendDirect seals a direct message from this hub to one server and sends
// it: over the server's connection if it is on this hub, toward its hub if it
// is on another. Nobody keeps a direct message for a server that is not
// connected; a key it misses it asks for.
func (h *Hub) sendDirect(to, kind, re string, fields map[string]any) {
	box, ok := h.boxKeyOf(to)
	if !ok {
		return
	}
	body := map[string]any{}
	for k, v := range fields {
		body[k] = v
	}
	body["kind"], body["msg_id"], body["at"] = kind, model.NewID[string](), h.now()
	if re != "" {
		body["re"] = re
	}
	f, err := wire.SealDirect(h.keys, h.id.Name, to, box, wire.EncodeDirect(mustJSON(body)))
	if err != nil {
		h.log.Warn("a direct message could not be sealed", "to", to, "kind", kind, "err", err)
		return
	}
	if h.hubOf(to) != h.id.Name {
		h.forward(to, mustFrame(f))
		return
	}
	if sc := h.connOf(to); sc != nil {
		f.ID = sc.conn.NextID()
		sc.conn.Send(f)
	}
}

// sendKeys sends the key of the epoch a channel's record calls for to each
// of some servers that is in the channel. It goes ahead of the record, so
// that a server holds the key when it learns that the epoch has begun.
func (h *Hub) sendKeys(c channel, to []string) {
	if !h.owned(c) || c.rec.Prepared || c.rec.Closed || c.rec.Epoch < 1 {
		return
	}
	key, servers, err := h.streamKey(h.db, c.rec.StreamID, c.rec.Epoch)
	if err != nil {
		return
	}
	for _, name := range to {
		if c.rec.Has(name) && has(servers, name) {
			h.sendDirect(name, "key", "", map[string]any{"stream_id": c.rec.StreamID, "epoch": c.rec.Epoch, "key": wire.Bytes(key),
				"version": c.rec.Version})
		}
	}
}

// directSeen remembers the msg_id of every direct message addressed to the
// hub for ten minutes, so that none is acted on twice.
type directSeen struct {
	mu   sync.Mutex
	seen map[string]time.Time
}

func (d *directSeen) first(id string, now time.Time) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.seen == nil {
		d.seen = map[string]time.Time{}
	}
	for k, at := range d.seen {
		if now.Sub(at) > 10*time.Minute {
			delete(d.seen, k)
		}
	}
	if _, ok := d.seen[id]; ok {
		return false
	}
	d.seen[id] = now
	return true
}

// ownDirect opens a direct message addressed to this hub, from a server of
// this hub or, carried in `ask`, of another hub of the cluster. Anything that
// does not verify, does not open, is out of time or was seen before is
// dropped, as a server drops it (section 7).
func (h *Hub) ownDirect(from string, fromKey []byte, f wire.Frame) error {
	if f.From != from {
		return wire.Errf(wire.EBadFrame, "A direct message is sent by the server it is from.")
	}
	plain, err := wire.OpenDirect(h.keys, h.id.Name, f, fromKey)
	if err != nil {
		return nil
	}
	if plain, err = wire.DecodeDirect(plain); err != nil {
		return nil
	}
	var m struct {
		Kind     string     `json:"kind"`
		MsgID    string     `json:"msg_id"`
		At       model.Time `json:"at"`
		StreamID string     `json:"stream_id"`
		Epoch    int64      `json:"epoch"`
	}
	if json.Unmarshal(plain, &m) != nil || !wire.ValidID(m.MsgID) {
		return nil
	}
	now := h.now()
	if d := now.Sub(m.At); d > 5*time.Minute || d < -5*time.Minute {
		return nil
	}
	if !h.seenDirect.first(m.MsgID, time.Now()) {
		return nil
	}
	if m.Kind != "key_request" {
		return nil // nothing else is asked of a hub
	}
	refuse := func(code, message string) error {
		h.sendDirect(from, "error", m.MsgID, map[string]any{"code": code, "message": message})
		return nil
	}
	if !wire.ValidID(m.StreamID) {
		return refuse(wire.EBadFrame, "A key is asked for by stream and epoch.")
	}
	c, err := loadChannel(h.db, m.StreamID)
	if err != nil || !h.owned(c) || c.rec.Prepared {
		return refuse(wire.EUnknownStream, "This hub is not the home of that channel.")
	}
	key, servers, err := h.streamKey(h.db, m.StreamID, m.Epoch)
	// In the channel now, and in the list the epoch began for: a server is
	// never given a key from before it joined or after it left.
	if err != nil || !c.rec.Has(from) || !has(servers, from) {
		return refuse(wire.EForbidden, "That server was not in the channel for that epoch.")
	}
	h.sendDirect(from, "key", m.MsgID, map[string]any{"stream_id": m.StreamID, "epoch": m.Epoch, "key": wire.Bytes(key),
		"version": c.rec.Version})
	return nil
}
