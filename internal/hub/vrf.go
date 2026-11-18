package hub

import (
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/tikaydaes/churr/internal/link/wire"
)

// The VRF key histories the hub keeps (docs/DESIGN-FAIR.md section 1): of
// every server it has met, its own from their hellos and the cluster's
// from the other hubs' rosters, each entry signed by that server. They are
// kept for good, including for a server that is removed, and every channel
// record this hub signs carries the history of each server the record has
// named and of the hub itself, so a draw made in the channel can be
// checked after its maker is gone.

const vrfSchema = `
CREATE TABLE IF NOT EXISTS vrf_keys (                        -- the VRF key history of every server this hub has met, kept for good
  name TEXT PRIMARY KEY, keys TEXT NOT NULL                  -- keys: a JSON list of {key, began, sig}, oldest first
);
`

type execQuerier interface {
	querier
	Exec(query string, args ...any) (sql.Result, error)
}

// vrfKeysOf is the history kept for a server, or nil.
func vrfKeysOf(q querier, name string) []wire.VRFKey {
	var raw string
	if q.QueryRow(`SELECT keys FROM vrf_keys WHERE name = ?`, name).Scan(&raw) != nil {
		return nil
	}
	var keys []wire.VRFKey
	if json.Unmarshal([]byte(raw), &keys) != nil {
		return nil
	}
	return keys
}

// errVRFSwapped is a history that is not the kept one with keys added.
var errVRFSwapped = errors.New("hub: a VRF key history other than the kept one")

// pinVRF keeps a history a server has shown, already checked against its
// signing key: the first one seen, and from then on only the kept one with
// keys added after it. It reports whether the kept history grew.
func pinVRF(q execQuerier, name string, shown []wire.VRFKey) (grew bool, err error) {
	kept := vrfKeysOf(q, name)
	added, ok := wire.VRFKeysExtend(kept, shown)
	if !ok {
		return false, errVRFSwapped
	}
	if added == 0 {
		return false, nil
	}
	b, err := json.Marshal(shown)
	if err != nil {
		return false, err
	}
	_, err = q.Exec(`INSERT INTO vrf_keys (name, keys) VALUES (?, ?) ON CONFLICT (name) DO UPDATE SET keys = excluded.keys`, name, string(b))
	return err == nil, err
}

// pinServerVRF is pinVRF for a server of this hub on its handshake. A
// history that grew changes the roster, which carries it.
func (h *Hub) pinServerVRF(name string, shown []wire.VRFKey) error {
	err := h.tx(func(tx *sql.Tx) error {
		grew, err := pinVRF(tx, name, shown)
		if err != nil || !grew {
			return err
		}
		return bumpRoster(tx)
	})
	if errors.Is(err, errVRFSwapped) {
		h.log.Warn("a server shows a VRF key history other than the one this hub keeps for it; it is refused", "server", name)
		return wire.Errf(wire.EKeyMismatch, "%s shows a VRF key history other than the one this hub keeps for it.", name)
	}
	return err
}

// recordVRF puts into a record the history of each server it names and of
// this hub, keeping what the record already held of any other: a server
// that has left stays. A kept history replaces the record's only where it
// is the record's with keys added.
func (h *Hub) recordVRF(q querier, c *channel) {
	if c.rec.VRFKeys == nil {
		c.rec.VRFKeys = map[string][]wire.VRFKey{}
	}
	put := func(name string, keys []wire.VRFKey) {
		if len(keys) == 0 {
			return
		}
		if _, ok := wire.VRFKeysExtend(c.rec.VRFKeys[name], keys); ok {
			c.rec.VRFKeys[name] = keys
		}
	}
	for _, name := range c.rec.Servers {
		put(name, vrfKeysOf(q, name))
	}
	put(h.id.Name, h.id.VRFKeys())
}
