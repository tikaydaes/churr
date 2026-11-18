package hub

import (
	"database/sql"
	"errors"
	"sort"
	"time"

	"github.com/tikaydaes/churr/internal/link/wire"
)

// This file is the hub as the time reference of the streams opened on it
// (ruling R57; docs/LINK-PROTOCOL.md sections 6 and 13). The hub orders
// nothing. Every envelope carries a stamp its origin made and signed: `at`,
// `n`, and the clock offset the origin used. The hub checks the stamp
// against what it has itself measured of the origin's clock (clock.go) and
// either takes the envelope into its log as it is, or refuses it and tells
// the origin why. It cannot change a stamp: the signature and the seal both
// cover it. A position in the hub's log is a delivery cursor.

// The bounds of the clock checks (section 13). They are the same on a
// server that is the reference of a direct link.
const (
	clockSlack  = 30 * 1000 // milliseconds an `at` may be from the reference clock, either way
	offsetFloor = 100       // the least tolerance between the offset used and the offset measured, in milliseconds
	offsetScale = 3         // or this many times the hub's uncertainty about the origin, when that is more
	awayKept    = 8
	awayLife    = 24 * time.Hour
)

// noteAway records, when a server connects, the stretch it was disconnected
// for: from when the hub last measured its clock to now.
func (h *Hub) noteAway(name string) {
	c := h.clockOf(name)
	if c == nil {
		return // never measured: it has made nothing the hub could have missed
	}
	now := int64(h.now())
	from := int64(c.MeasuredAt)
	if from > now {
		from = now
	}
	h.tx(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`INSERT INTO away (name, from_at, to_at) VALUES (?, ?, ?)`, name, from, now); err != nil {
			return err
		}
		_, err := tx.Exec(`DELETE FROM away WHERE name = ? AND to_at NOT IN (SELECT to_at FROM away WHERE name = ? ORDER BY to_at DESC LIMIT ?)`,
			name, name, awayKept)
		return err
	})
}

// wasAway reports whether a time by the hub's clock falls in a stretch a
// server was disconnected for, give or take the 30 seconds any `at` is
// allowed. A stretch stops counting a day after it ended.
func (h *Hub) wasAway(name string, at int64) bool {
	_, _, ok := h.awayAt(h.db, name, at)
	return ok
}

// awayAt is wasAway that also returns the stretch. It reads through q: the
// database, or the transaction the caller is in.
func (h *Hub) awayAt(q querier, name string, at int64) (from, to int64, ok bool) {
	err := q.QueryRow(`SELECT from_at, to_at FROM away WHERE name = ? AND to_at >= ? AND from_at - ? <= ? AND to_at + ? >= ? LIMIT 1`,
		name, int64(h.now().Add(-awayLife)), clockSlack, at, clockSlack, at).Scan(&from, &to)
	return from, to, err == nil
}

// checkStamp is what the hub checks of a submitted envelope's stamp:
//
//   - the offset the origin used is the one the hub measured, within a
//     tolerance that grows with the hub's own uncertainty about that origin
//     and is never less than a tenth of a second;
//   - `at` is not more than 30 seconds ahead of the hub's clock;
//   - `at` is not more than 30 seconds behind it, unless it falls in a
//     stretch the origin was disconnected for.
//
// A refusal carries the hub's figures, so the origin can stamp again at
// once.
//
// For a channel whose home is another hub of the cluster the reference clock
// is that hub's, and this hub is still the one that checks: it measured the
// origin, and it knows its own clock against the home hub's from the trunks
// between them (trunk.go). The offset it expects is the two added, the
// uncertainty the two added, and "the hub's clock" below is its reading of
// the home hub's. During a split it goes on checking against the figure it
// last had, which it trusts a little less with time. The second and third
// results are the stretch that excused an old time, by the home hub's clock.
// It reads through q: the transaction the envelope is being taken in.
func (h *Hub) checkStamp(q querier, env wire.Envelope, home string) (*wire.Error, int64, int64) {
	if h.testNoStamp.Load() {
		return nil, 0, 0
	}
	c := h.clockIn(q, env.Origin)
	if c == nil {
		return wire.Errf(wire.EClockUnknown, "The hub has not measured %s's clock yet.", env.Origin), 0, 0
	}
	hc, ok := h.hubClockOf(home)
	if !ok {
		return wire.Errf(wire.EClockUnknown, "This hub has no reading of the clock of %s, the channel's home hub, yet.", home), 0, 0
	}
	if hc.offset != 0 || hc.uncertainty != 0 {
		// What the origin was told for this home, and what a refusal tells
		// it again: its clock against the home hub's.
		composed := *c
		composed.Offset += hc.offset
		composed.Uncertainty += hc.uncertaintyAt(int64(h.now()))
		c = &composed
	}
	now := int64(h.now()) - hc.offset
	tolerance := offsetScale * c.UncertaintyAt(h.now())
	if tolerance < offsetFloor {
		tolerance = offsetFloor
	}
	if d := env.Offset - c.Offset; d > tolerance || d < -tolerance {
		e := wire.Errf(wire.EClockOffset, "The envelope was stamped with a clock offset of %d ms; the hub measures %d ms for %s, give or take %d.",
			env.Offset, c.Offset, env.Origin, tolerance)
		e.Clock = c
		return e, 0, 0
	}
	if env.At > now+clockSlack {
		e := wire.Errf(wire.EClockAhead, "The envelope's time is %d ms ahead of the reference clock.", env.At-now)
		e.Clock = c
		return e, 0, 0
	}
	if env.At < now-clockSlack {
		// The stretches a server was away for are kept by this hub's clock.
		from, to, away := h.awayAt(q, env.Origin, env.At+hc.offset)
		if !away {
			e := wire.Errf(wire.EClockBehind, "The envelope's time is %d ms behind the reference clock, and %s was connected then.", now-env.At, env.Origin)
			e.Clock = c
			return e, 0, 0
		}
		return nil, from - hc.offset, to - hc.offset
	}
	return nil, 0, 0
}

// checkEnvelope is what the hub checks of any envelope before it looks at
// its stamp: its shape and size, that its origin is the server on this
// connection and is in the stream, and the origin's signature.
func (h *Hub) checkEnvelope(sc *serverConn, env wire.Envelope) (wire.Envelope, stream, error) {
	fail := func(e *wire.Error) (wire.Envelope, stream, error) {
		e.StreamID, e.OriginEventID = env.StreamID, env.OriginEventID
		return env, stream{}, e
	}
	if !env.WellFormed() || env.Cursor != 0 {
		return fail(wire.Errf(wire.EBadFrame, "The frame carries a whole envelope with no cursor."))
	}
	if len(env.Ciphertext) > wire.MaxEnvelopeBytes {
		return fail(wire.Errf(wire.ETooLarge, "An envelope holds at most %d bytes of ciphertext.", wire.MaxEnvelopeBytes))
	}
	if env.Origin != sc.name {
		return fail(wire.Errf(wire.ENotInStream, "An envelope is submitted by its origin, and %s is not this connection's server.", env.Origin))
	}
	st, err := loadStream(h.db, env.StreamID)
	if err != nil {
		var e *wire.Error
		if errors.As(err, &e) {
			return fail(e)
		}
		return env, st, err
	}
	if st.state != "open" {
		return fail(wire.Errf(wire.EUnknownStream, "That stream is closed."))
	}
	if !st.has(sc.name) {
		return fail(wire.Errf(wire.ENotInStream, "%s is not in that stream.", sc.name))
	}
	if !env.Verify(sc.key) {
		return fail(wire.Errf(wire.EBadSignature, "The envelope's signature does not verify."))
	}
	return env, st, nil
}

// haveOf returns, per origin, the last counter the log of a stream has
// taken.
func (h *Hub) haveOf(streamID string) map[string]int64 { return haveIn(h.db, streamID) }

// haveIn is haveOf inside a transaction.
func haveIn(q querier, streamID string) map[string]int64 {
	have := map[string]int64{}
	rows, err := q.Query(`SELECT name, last_n FROM origins WHERE stream_id = ?`, streamID)
	if err != nil {
		return have
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		var n int64
		if rows.Scan(&name, &n) == nil {
			have[name] = n
		}
	}
	return have
}

// catchUp serves a stream's log to a server in its current list: for each
// origin but the asker, everything above the counter the asker says it
// holds, in log order; then what the log itself holds of each origin and how
// far back it reaches.
func (h *Hub) catchUp(sc *serverConn, f wire.Frame) error {
	m := h.lock(f.StreamID)
	defer m.Unlock()
	st, err := loadStream(h.db, f.StreamID)
	if err != nil {
		return err
	}
	// The log is served only to servers in the stream's current list, and
	// to the hub's holder of a channel the hub is the home of.
	if !st.has(sc.name) && !(sc.holder && h.holderOf(st)) {
		return wire.Errf(wire.ENotInStream, "%s is not in that stream.", sc.name)
	}
	have := h.haveOf(st.id)
	type entry struct {
		pos, prev int64
		raw       []byte
	}
	var entries []entry
	floor := map[string]int64{}
	for origin, last := range have {
		floor[origin] = last // unless the log still holds rows of it, below
		if origin == sc.name {
			continue // its own: it submits again whatever was not acknowledged
		}
		var oldest sql.NullInt64
		h.db.QueryRow(`SELECT prev_n FROM log WHERE stream_id = ? AND origin = ? ORDER BY seq LIMIT 1`, st.id, origin).Scan(&oldest)
		if oldest.Valid {
			floor[origin] = oldest.Int64
		}
		rows, err := h.db.Query(`SELECT seq, prev_n, envelope FROM log WHERE stream_id = ? AND origin = ? AND n > ? ORDER BY seq`,
			st.id, origin, f.Have[origin])
		if err != nil {
			return err
		}
		first := true
		for rows.Next() {
			var e entry
			if err := rows.Scan(&e.pos, &e.prev, &e.raw); err != nil {
				rows.Close()
				return err
			}
			if first {
				// "This is the next one this log has after what you hold."
				e.prev, first = f.Have[origin], false
			}
			entries = append(entries, e)
		}
		rows.Close()
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].pos < entries[j].pos })
	for _, e := range entries {
		var env wire.Envelope
		if readEnvelope(e.raw, &env) != nil {
			continue
		}
		env.Cursor, env.Prev = e.pos, e.prev
		sc.conn.Send(wire.Frame{Frame: wire.FDeliver, Envelope: &env})
	}
	return sc.conn.Send(wire.Frame{Frame: wire.FCaughtUp, Re: f.ID, StreamID: st.id, Have: have, Floor: floor})
}
