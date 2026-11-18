package hub

import (
	"database/sql"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tikaydaes/churr/internal/link/wire"
)

// This file carries envelopes between the hubs of a cluster
// (docs/LINK-PROTOCOL.md section 23b).
//
// Every hub that has a server of a channel behind it, and the channel's home
// hub, keeps its own log of the channel. A hub ships its log over each trunk
// that leads to such a hub: every row that did not come over that trunk, in
// the order it was written, in batches of several streams' envelopes, with a
// bound on how many batches may be unanswered. The log is the queue: nothing
// waits in memory for a trunk, and a trunk that was down is served, when it
// returns, from what the log holds above what the other side says it has of
// each origin. A cluster is a tree, so there is one way from any origin to
// any hub, and an origin's envelopes reach every hub in the order it made
// them.

// route is where one stream's envelopes go from this hub.
type route struct {
	home string
	// local are the stream's servers on this hub.
	local []string
	// members are the neighbours that lead to a server of the stream, and
	// trunks those and the neighbour that leads to the home hub, which is
	// sent every envelope whether or not it has a server of the stream.
	members []string
	trunks  []string
	// behind is, per neighbour, the stream's servers on that side.
	behind map[string][]string
}

func (r *route) over(trunk string) bool { return has(r.trunks, trunk) }

// routeOf works out, or remembers, where a stream's envelopes go. Nil for a
// stream this hub does not hold.
func (h *Hub) routeOf(streamID string) *route {
	h.mu.Lock()
	r, ok := h.routes[streamID]
	h.mu.Unlock()
	if ok {
		return r
	}
	st, err := loadStream(h.db, streamID)
	if err != nil || st.state != "open" {
		return nil
	}
	r = h.routeFor(st)
	h.mu.Lock()
	h.routes[streamID] = r
	h.mu.Unlock()
	return r
}

func (h *Hub) routeFor(st stream) *route {
	t := h.topology()
	r := &route{home: st.homeHub, behind: map[string][]string{}}
	for _, n := range st.servers {
		rs, remote := t.servers[n]
		if !remote {
			r.local = append(r.local, n)
			continue
		}
		next := t.next[rs.hub]
		if next == "" {
			continue
		}
		if !has(r.members, next) {
			r.members = append(r.members, next)
		}
		r.behind[next] = append(r.behind[next], n)
	}
	r.trunks = append(r.trunks, r.members...)
	if st.homeHub != "" && st.homeHub != h.id.Name {
		if next := t.next[st.homeHub]; next != "" && !has(r.trunks, next) {
			r.trunks = append(r.trunks, next)
		}
	}
	sort.Strings(r.local)
	if h.holderOf(st) {
		r.local = append(r.local, h.id.Name) // the hub's own holder (holder.go)
	}
	return r
}

// logRow is one row of the log as a trunk ships it.
type logRow struct {
	lid      int64
	stream   string
	origin   string
	n, prev  int64
	via      string
	winFrom  int64
	winTo    int64
	envelope []byte
}

// ringSize is how many of the newest rows are kept in memory, so that a
// trunk that is keeping up never reads the database.
const ringSize = 8192

// appended adds rows just written to the memory of recent rows and wakes
// the trunks. The caller holds logMu.
func (h *Hub) appended(rows []logRow) {
	h.ring = append(h.ring, rows...)
	if len(h.ring) > 2*ringSize {
		h.ring = append([]logRow(nil), h.ring[len(h.ring)-ringSize:]...)
	}
}

func (h *Hub) wakeTrunks() {
	h.mu.Lock()
	for _, t := range h.trunks {
		t.kick()
	}
	h.mu.Unlock()
}

// rowsAfter returns up to limit rows of the log after a lid, in order: from
// memory when memory reaches back that far, otherwise from the database.
func (h *Hub) rowsAfter(lid int64, limit int) []logRow {
	h.logMu.Lock()
	if len(h.ring) > 0 && h.ring[0].lid <= lid+1 {
		i := sort.Search(len(h.ring), func(i int) bool { return h.ring[i].lid > lid })
		out := append([]logRow(nil), h.ring[i:min(len(h.ring), i+limit)]...)
		h.logMu.Unlock()
		return out
	}
	top := h.lid
	h.logMu.Unlock()
	if top <= lid {
		return nil
	}
	return h.queryRows(`SELECT lid, stream_id, origin, n, prev_n, via, win_from, win_to, envelope FROM log WHERE lid > ? ORDER BY lid LIMIT ?`, lid, limit)
}

func (h *Hub) queryRows(query string, args ...any) []logRow {
	rows, err := h.db.Query(query, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []logRow
	for rows.Next() {
		var r logRow
		if rows.Scan(&r.lid, &r.stream, &r.origin, &r.n, &r.prev, &r.via, &r.winFrom, &r.winTo, &r.envelope) == nil {
			out = append(out, r)
		}
	}
	return out
}

// ---------------------------------------------------------------- the sync

// sendSync says what this hub holds of every stream it has a record or a
// log of: the record's version and, per origin, the last counter in its log.
// The neighbour ships what is above that.
func (h *Hub) sendSync(tc *trunkConn) {
	rows, err := h.db.Query(`SELECT s.id, s.version FROM streams s WHERE s.state = 'open' AND s.home_hub <> '' ORDER BY s.id`)
	if err != nil {
		return
	}
	type entry struct {
		id      string
		version int64
	}
	var all []entry
	for rows.Next() {
		var e entry
		if rows.Scan(&e.id, &e.version) == nil {
			all = append(all, e)
		}
	}
	rows.Close()
	out := map[string]wire.SyncStream{}
	flush := func(done bool) {
		tc.send(wire.Frame{Frame: wire.FSync, Streams: out, Done: done})
		out = map[string]wire.SyncStream{}
	}
	for _, e := range all {
		// Channels, and the streams servers own that cross a trunk. A
		// stream that never left this hub is nobody else's business.
		var one int
		if h.db.QueryRow(`SELECT 1 FROM channels WHERE stream_id = ? UNION ALL SELECT 1 FROM stream_ctl WHERE stream_id = ? LIMIT 1`, e.id, e.id).Scan(&one) != nil {
			continue
		}
		out[e.id] = wire.SyncStream{Version: e.version, Have: h.haveOf(e.id)}
		if len(out) >= 500 {
			flush(false)
		}
	}
	flush(true)
}

// onSync takes what the neighbour holds. For each stream this trunk carries
// the shipping starts above it; a record the neighbour holds an older
// version of is sent again.
func (h *Hub) onSync(tc *trunkConn, f wire.Frame) error {
	tc.mu.Lock()
	for id, s := range f.Streams {
		tc.theirs[id] = s
		if tc.synced {
			// Said again while the trunk is up: the neighbour missed
			// something of this stream and wants it from here.
			delete(tc.routed, id)
		}
	}
	tc.mu.Unlock()
	for id, s := range f.Streams {
		h.resendRecordIfNewer(tc, id, s.Version)
	}
	if !f.Done {
		return nil
	}
	tc.mu.Lock()
	tc.synced = true
	tc.mu.Unlock()
	h.armAll(tc)
	tc.kick()
	return nil
}

// armAll starts the shipping of every stream a trunk carries, once the
// neighbour has said what it holds.
func (h *Hub) armAll(tc *trunkConn) {
	rows, err := h.db.Query(`SELECT stream_id FROM channels UNION SELECT stream_id FROM stream_ctl`)
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
		h.arm(tc, id)
	}
}

// arm starts shipping one stream over a trunk if the stream's route has come
// to include it: everything in the log above what the neighbour holds of
// each origin and, for a stream the neighbour holds nothing of, above where
// the servers on its side start from.
func (h *Hub) arm(tc *trunkConn, streamID string) {
	r := h.routeOf(streamID)
	tc.mu.Lock()
	defer tc.mu.Unlock()
	if !tc.synced {
		return // armed when the neighbour has said what it holds
	}
	if r == nil || !r.over(tc.name) {
		delete(tc.routed, streamID)
		return
	}
	if tc.routed[streamID] {
		return
	}
	tc.routed[streamID] = true
	skip := map[string]int64{}
	theirs, known := tc.theirs[streamID]
	for origin, n := range theirs.Have {
		skip[origin] = n
	}
	if !known || len(theirs.Have) == 0 {
		// The neighbour's side starts from where its servers were let in.
		for origin, n := range h.floorBehind(streamID, r.behind[tc.name]) {
			skip[origin] = n
		}
	}
	tc.skip[streamID] = skip
	tc.backfill = append(tc.backfill, streamID)
	tc.kick()
}

// floorBehind is, per origin, the lowest counter any of the named servers
// starts from: what the home hub's log held when it let each in. A server
// that was in the channel from the start starts from nothing.
func (h *Hub) floorBehind(streamID string, servers []string) map[string]int64 {
	if len(servers) == 0 {
		return nil
	}
	var joined map[string]map[string]int64
	if c, err := loadChannel(h.db, streamID); err == nil {
		joined = c.joinedHave
	} else if joined = h.floorsOf(h.db, streamID); joined == nil {
		return nil
	}
	var floor map[string]int64
	for _, sv := range servers {
		at, ok := joined[sv]
		if !ok {
			return nil
		}
		if floor == nil {
			floor = map[string]int64{}
			for origin, n := range at {
				floor[origin] = n
			}
			continue
		}
		for origin, n := range floor {
			if at[origin] < n {
				floor[origin] = at[origin]
			}
		}
	}
	return floor
}

// ------------------------------------------------------------- the shipper

// relayWait is how long a shipper waits for room in the window before it
// gives the trunk up as down. Tests shorten it.
var relayWait atomic.Int64

func init() { relayWait.Store(int64(trunkHold)) }

// ship is one trunk's sender. It runs for the life of the connection.
func (h *Hub) ship(tc *trunkConn) {
	h.logMu.Lock()
	tc.cursor = h.lid // rows written from now on; older ones are backfilled per stream
	h.logMu.Unlock()
	var batch []wire.RelayItem
	size := 0
	flush := func() bool {
		if len(batch) == 0 {
			return true
		}
		// Flow control: at most relayWindow batches unanswered.
		deadline := time.Now().Add(time.Duration(relayWait.Load()))
		waited := false
		for {
			tc.mu.Lock()
			room := tc.inflight < relayWindow || h.testNoWindow.Load()
			if room {
				tc.inflight++
				tc.sentAt = append(tc.sentAt, time.Now())
				inflight := int64(tc.inflight)
				tc.mu.Unlock()
				n := int64(len(batch))
				h.statsOf(tc.name, func(s *TrunkStats) {
					s.Batches++
					s.Envelopes += n
					s.MaxBatch = max(s.MaxBatch, n)
					s.MaxInflight = max(s.MaxInflight, inflight)
				})
				break
			}
			tc.mu.Unlock()
			if !waited {
				waited = true
				h.statsOf(tc.name, func(s *TrunkStats) { s.Waits++ })
			}
			select {
			case <-tc.conn.Done():
				return false
			case <-tc.wake:
			case <-time.After(200 * time.Millisecond):
			}
			if time.Now().After(deadline) {
				// The neighbour has taken nothing for too long. The trunk
				// counts as down; when it returns, the stretch is one it
				// was disconnected for, and the log serves what was missed.
				h.log.Warn("a trunk took nothing for too long and is closed", "hub", tc.name)
				tc.conn.CloseWith(wire.ErrorFrame(0, wire.EUnavailable, "Nothing was answered for too long."))
				return false
			}
		}
		err := tc.send(wire.Frame{Frame: wire.FRelay, ID: tc.conn.NextID(), Items: batch})
		batch, size = nil, 0
		return err == nil
	}
	add := func(r logRow) bool {
		var env wire.Envelope
		if readEnvelope(r.envelope, &env) != nil {
			return true
		}
		item := wire.RelayItem{Envelope: env, Prev: r.prev}
		if r.winTo != 0 {
			item.Window = []int64{r.winFrom, r.winTo}
		}
		if len(batch) >= relayMaxItems || size+len(r.envelope) > relayMaxBytes {
			if !flush() {
				return false
			}
		}
		batch = append(batch, item)
		size += len(r.envelope)
		return true
	}
	wanted := func(r logRow) bool {
		if r.via == tc.name {
			return false // it came from there
		}
		tc.mu.Lock()
		defer tc.mu.Unlock()
		return tc.routed[r.stream] && r.n > tc.skip[r.stream][r.origin]
	}
	for {
		// Streams that have just come to be carried here: what the log holds
		// of them already goes first.
		tc.mu.Lock()
		pending := tc.backfill
		tc.backfill = nil
		tc.mu.Unlock()
		for _, id := range pending {
			rows := h.queryRows(`SELECT lid, stream_id, origin, n, prev_n, via, win_from, win_to, envelope FROM log
				WHERE stream_id = ? AND lid > 0 AND lid <= ? ORDER BY lid`, id, tc.cursor)
			for _, r := range rows {
				if wanted(r) && !add(r) {
					return
				}
			}
		}
		for {
			rows := h.rowsAfter(tc.cursor, 4*relayMaxItems)
			if len(rows) == 0 {
				break
			}
			again := false
			for _, r := range rows {
				// A stream that has just come to be carried sends its older
				// rows before any newer one: an origin's envelopes never
				// pass one another.
				tc.mu.Lock()
				again = len(tc.backfill) > 0
				tc.mu.Unlock()
				if again {
					break
				}
				if wanted(r) && !add(r) {
					return
				}
				tc.cursor = r.lid
			}
			if again {
				break
			}
		}
		if !flush() {
			return
		}
		tc.mu.Lock()
		again := len(tc.backfill) > 0
		tc.mu.Unlock()
		if again {
			continue
		}
		h.logMu.Lock()
		more := h.lid > tc.cursor
		h.logMu.Unlock()
		if more {
			continue
		}
		select {
		case <-tc.conn.Done():
			return
		case <-tc.wake:
		case <-time.After(time.Second):
		}
	}
}

// onRelayAck takes the answer to a batch: room in the window, and what the
// neighbour would not take.
func (h *Hub) onRelayAck(tc *trunkConn, f wire.Frame) {
	if f.Re != 0 {
		tc.mu.Lock()
		if tc.inflight > 0 {
			tc.inflight--
			tc.sentAt = tc.sentAt[1:]
		}
		tc.mu.Unlock()
		tc.kick()
	}
	for _, r := range f.Refused {
		h.refusedThere(tc.name, r)
	}
}

// refusedThere is told that a hub further on would not take an envelope this
// hub had taken. The envelope stays where it has been delivered; nothing
// takes it back. The word goes on toward the origin's own hub, which tells
// the origin; every hub on the way counts it and writes it down.
func (h *Hub) refusedThere(from string, r wire.RelayRefusal) {
	h.statsOf(from, func(s *TrunkStats) { s.RefusedThere++ })
	h.log.Warn("an envelope this hub took was refused further on", "by way of", from, "stream", r.StreamID, "event", r.OriginEventID,
		"origin", r.Origin, "code", r.Code, "message", r.Message)
	var via string
	if h.db.QueryRow(`SELECT via FROM log WHERE stream_id = ? AND origin_event_id = ? LIMIT 1`, r.StreamID, r.OriginEventID).Scan(&via) != nil {
		return
	}
	if via != "" {
		if tc := h.trunkOf(via); tc != nil {
			tc.send(wire.Frame{Frame: wire.FRelayAck, Refused: []wire.RelayRefusal{r}})
		}
		return
	}
	h.count(r.Origin, 0, 0, 1)
	h.sendTo([]string{r.Origin}, wire.Frame{Frame: wire.FError, Code: r.Code, StreamID: r.StreamID, OriginEventID: r.OriginEventID,
		Message: "This hub took the event and a hub further on refused it: " + r.Message})
}

// ------------------------------------------------------------ the receiver

// relayAt is what a hub looks at of the time of an envelope that reaches it
// over a trunk (section 23b). The origin's own hub has judged the stamp
// against the origin's measured clock, and **its judgement stands across the
// cluster**: a hub further on never refuses an envelope for its time. It
// could not do so and keep every server's copy of the channel the same: the
// envelope has been acknowledged to its origin and delivered on the side it
// came from, and no bound this hub could apply is one a correctly behaving
// edge is certain to have kept (its clocks are estimates, and a trunk may be
// down for longer than any stretch is remembered).
//
// What this hub does instead is compare, for its operator. It reads `at`
// against its own reading of the home hub's clock:
//
//   - not more than 30 seconds, plus this hub's uncertainty, ahead;
//   - not more than 30 seconds, plus that uncertainty, plus the time the
//     hubs on the way may each take to pass it on, behind; unless `at` falls
//     inside a stretch of disconnection, give or take the same: one of the
//     trunk it came over, which this hub knows for itself, or one the
//     sending hub vouches for, which must have ended within the last day
//     and not in the future.
//
// An envelope outside that is taken all the same, counted as suspect, and
// written to the hub's log of events: the hub it came through, or a clock on
// the way, is not what it should be. It returns the stretch that explains an
// old time, by the home hub's clock, so that it travels on with the envelope.
func (h *Hub) relayAt(tc *trunkConn, home string, env wire.Envelope, vouched []int64) (winFrom, winTo int64, suspect *wire.Error) {
	if h.testNoStamp.Load() {
		return 0, 0, nil
	}
	hc, ok := h.hubClockOf(home)
	if !ok {
		return 0, 0, wire.Errf(wire.EClockUnknown, "This hub has no reading of %s's clock.", home)
	}
	now := int64(h.now())
	t := h.topology()
	unc := hc.uncertaintyAt(now)
	if oc, ok := h.hubClockOf(t.servers[env.Origin].hub); ok {
		unc += oc.uncertaintyAt(now)
	}
	u := offsetScale * unc
	if u < offsetFloor {
		u = offsetFloor
	}
	hops := int64(t.hops[t.servers[env.Origin].hub])
	if hops < 1 {
		hops = 1
	}
	slack := clockSlack + u + hops*int64(trunkHold/time.Millisecond)
	nowHome := now - hc.offset
	if env.At > nowHome+clockSlack+u {
		return 0, 0, wire.Errf(wire.EClockAhead, "The envelope's time is %d ms ahead of %s's clock as %s reads it.", env.At-nowHome, home, h.id.Name)
	}
	if env.At >= nowHome-slack {
		return 0, 0, nil
	}
	// Old. A stretch this trunk was down for, known here at first hand.
	if from, to, ok := h.trunkAway(tc.name, env.At+hc.offset, slack); ok {
		return from - hc.offset, to - hc.offset, nil
	}
	// A stretch the sending hub vouches for: a server of its own that was
	// away, or a trunk further off.
	if len(vouched) == 2 && vouched[1] <= nowHome+slack && vouched[1] >= nowHome-int64(awayLife/time.Millisecond) &&
		vouched[0]-slack <= env.At && env.At <= vouched[1]+slack {
		return vouched[0], vouched[1], nil
	}
	return 0, 0, wire.Errf(wire.EClockBehind, "The envelope's time is %d ms behind %s's clock as %s reads it, and outside any time the way to its origin was cut.",
		nowHome-env.At, home, h.id.Name)
}

// onRelay takes a batch of envelopes from a neighbour's log. Each is checked
// and written to this hub's own log, all in one transaction; then it goes to
// the stream's servers here, and the trunks on the other side of this hub
// ship it on.
//
// **Nothing a correctly behaving edge hub accepted is refused here.** What
// becomes of an envelope is one of four things, and only the last is a
// refusal:
//
//   - taken: logged, delivered, shipped on;
//   - held already: counted as taken, and nothing more happens;
//   - put off: something this hub needs has not reached it yet (the channel's
//     record, the origin's key, the envelope of the same origin before this
//     one). Nothing is written, the origin's counter does not move, and the
//     neighbour is asked for the stream again; it comes in its place;
//   - refused: the envelope is one no hub that keeps the protocol could have
//     sent this way. It is malformed or over size, its signature is not its
//     origin's, its origin is not on that side of this hub, or its `at` is
//     not above the origin's last. Every one of those is a check the edge hub
//     made first, on the same bytes, the same key and counters that only ever
//     grow; so a refusal here says the neighbour is at fault, not the origin.
//
// An envelope's time is not among them (relayAt).
func (h *Hub) onRelay(tc *trunkConn, f wire.Frame) error {
	if len(f.Items) > relayMaxItems {
		return wire.Errf(wire.ETooLarge, "A batch holds at most %d envelopes.", relayMaxItems)
	}
	if d := h.testHold.Load(); d > 0 {
		time.Sleep(time.Duration(d)) // a test's slow hub
	}
	ack := wire.Frame{Frame: wire.FRelayAck, Re: f.ID}
	refuse := func(env wire.Envelope, e *wire.Error) {
		ack.Refused = append(ack.Refused, wire.RelayRefusal{StreamID: env.StreamID, OriginEventID: env.OriginEventID, Origin: env.Origin, Code: e.Code, Message: e.Message})
		h.statsOf(tc.name, func(s *TrunkStats) { s.Refused++ })
		h.log.Warn("an envelope from a trunk was refused", "from", tc.name, "stream", env.StreamID, "origin", env.Origin, "code", e.Code, "message", e.Message)
	}
	// Signatures first, outside every lock, and several at once: checking
	// one takes longer than everything else done with an envelope here, and
	// a trunk has one reader. (Done one after another, this was what a hub
	// that receives over a trunk spent most of its time on.)
	unknown := map[string]bool{}
	sound := make([]bool, len(f.Items))
	ids := map[string]bool{}
	type verdict struct {
		refusal *wire.Error
		unknown bool
	}
	verdicts := make([]verdict, len(f.Items))
	check := func(i int) {
		env := f.Items[i].Envelope
		key, ok := h.serverKey(env.Origin)
		switch {
		case !env.WellFormed() || env.N <= 0 || env.At <= 0 || len(env.Ciphertext) > wire.MaxEnvelopeBytes:
			verdicts[i].refusal = wire.Errf(wire.EBadFrame, "The envelope is malformed or not stamped.")
		case h.hubOf(env.Origin) == h.id.Name:
			verdicts[i].refusal = wire.Errf(wire.EUnknownServer, "%s is a server of this hub, and its envelopes do not come over a trunk.", env.Origin)
		case !ok || h.hubOf(env.Origin) == "":
			// Its hub's list of servers has not reached this hub yet. It is
			// on its way by the same trunk; the envelope is asked for again.
			verdicts[i].unknown = true
		case h.topology().next[h.hubOf(env.Origin)] != tc.name:
			verdicts[i].refusal = wire.Errf(wire.ENotInStream, "%s is not on that side of this hub.", env.Origin)
		case !env.Verify(key):
			verdicts[i].refusal = wire.Errf(wire.EBadSignature, "The envelope's signature does not verify.")
		}
	}
	if workers := min(runtime.GOMAXPROCS(0), 8, (len(f.Items)+15)/16); workers > 1 {
		var wg sync.WaitGroup
		var next atomic.Int64
		for w := 0; w < workers; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for {
					i := int(next.Add(1)) - 1
					if i >= len(f.Items) {
						return
					}
					check(i)
				}
			}()
		}
		wg.Wait()
	} else {
		for i := range f.Items {
			check(i)
		}
	}
	for i, it := range f.Items {
		env := it.Envelope
		switch v := verdicts[i]; {
		case v.refusal != nil:
			refuse(env, v.refusal)
		case v.unknown:
			unknown[env.StreamID] = true
		default:
			sound[i] = true
			ids[env.StreamID] = true
		}
	}
	// One stream at a time everywhere else; here all of the batch's, taken
	// in one order so that two batches cannot wait on each other.
	sorted := make([]string, 0, len(ids))
	for id := range ids {
		sorted = append(sorted, id)
	}
	sort.Strings(sorted)
	for _, id := range sorted {
		defer h.lock(id).Unlock()
	}
	type state struct {
		st      stream
		ok      bool
		closed  bool
		last    map[string][3]int64 // origin -> last n seen, last at taken, last n logged
		touched map[string]bool
	}
	streams := map[string]*state{}
	var rows []logRow
	var taken []wire.Envelope
	now := int64(h.now())
	gaps := map[string]bool{}
	later := map[string]bool{}
	for id := range unknown {
		later[id] = true
	}
	suspects := int64(0)
	h.logMu.Lock()
	lid := h.lid
	err := h.tx(func(tx *sql.Tx) error {
		// Prepared once for the batch: the two statements every envelope
		// needs.
		held, err := tx.Prepare(`SELECT 1 FROM log WHERE stream_id = ? AND origin_event_id = ? LIMIT 1`)
		if err != nil {
			return err
		}
		defer held.Close()
		ins, err := tx.Prepare(`INSERT INTO log (stream_id, seq, origin_event_id, key_epoch, envelope, at, origin, n, prev_n, lid, via, win_from, win_to)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
		if err != nil {
			return err
		}
		defer ins.Close()
		for i, it := range f.Items {
			if !sound[i] {
				continue
			}
			env := it.Envelope
			s := streams[env.StreamID]
			if s == nil {
				s = &state{last: map[string][3]int64{}, touched: map[string]bool{}}
				streams[env.StreamID] = s
				st, err := loadStream(tx, env.StreamID)
				switch {
				case err == nil && st.state == "open" && st.homeHub != "":
					s.st, s.ok = st, true
				case err == nil && st.state == "closed":
					// Over, for every server: there is nobody to give it to.
					s.closed = true
				default:
					// The record that brings the stream here has not come
					// yet, or the channel is still only prepared.
					later[env.StreamID] = true
				}
			}
			if later[env.StreamID] || s.closed {
				continue
			}
			last, known := s.last[env.Origin]
			if !known {
				tx.QueryRow(`SELECT last_n, last_at, logged_n FROM origins WHERE stream_id = ? AND name = ?`, env.StreamID, env.Origin).Scan(&last[0], &last[1], &last[2])
				if last[2] == 0 {
					last[2] = last[0] // written before the hub told the two apart
				}
				s.last[env.Origin] = last
			}
			if env.N <= last[0] {
				continue // held already: taken, and nothing more happens
			}
			var one int
			if held.QueryRow(env.StreamID, env.OriginEventID).Scan(&one) == nil {
				continue
			}
			if it.Prev > last[0] && last[0] > 0 {
				// Something of this origin was missed on the way. The
				// envelope is dropped and the neighbour told what is held;
				// it comes again in its place.
				gaps[env.StreamID] = true
				continue
			}
			// An envelope refused for its time is seen all the same: the
			// origin's counter moves past it here, so that the ones after
			// it are not taken for a gap, and it is not sent again.
			seen := func() {
				s.last[env.Origin] = [3]int64{env.N, last[1], last[2]}
				s.touched[env.Origin] = true
			}
			if env.At <= last[1] {
				refuse(env, wire.Errf(wire.ENotMonotonic, "After n %d at %d, n %d at %d is not later.", last[0], last[1], env.N, env.At))
				seen()
				continue
			}
			// Its time was the edge hub's to judge, and was judged. This hub
			// only compares, and says so if it would have judged otherwise.
			winFrom, winTo, e := h.relayAt(tc, s.st.homeHub, env, it.Window)
			if e != nil {
				if len(it.Window) == 2 {
					winFrom, winTo = it.Window[0], it.Window[1] // as vouched, for the hubs further on
				}
				if e.Code != wire.EClockUnknown {
					suspects++
					h.log.Warn("an envelope from a trunk is outside this hub's bounds on time; it was taken on its edge hub's word",
						"from", tc.name, "stream", env.StreamID, "origin", env.Origin, "event", env.OriginEventID, "code", e.Code, "message", e.Message)
				}
			}
			// The counter of the origin's envelope before this one in this
			// hub's log; for a hub that begins to carry a stream midway,
			// whose own log has none, as the sender's log has it.
			prev := last[2]
			if prev == 0 {
				prev = it.Prev
			}
			pos := s.st.nextSeq
			s.st.nextSeq++
			if env.KeyEpoch > s.st.epoch {
				s.st.epoch = env.KeyEpoch
			}
			env.Cursor, env.Prev = 0, 0
			stored := mustEnvelope(env)
			lid++
			if _, err := ins.Exec(env.StreamID, pos, env.OriginEventID, env.KeyEpoch, stored, now, env.Origin, env.N, prev, lid, tc.name, winFrom, winTo); err != nil {
				return err
			}
			s.last[env.Origin] = [3]int64{env.N, env.At, env.N}
			s.touched[env.Origin] = true
			rows = append(rows, logRow{lid: lid, stream: env.StreamID, origin: env.Origin, n: env.N, prev: prev, via: tc.name,
				winFrom: winFrom, winTo: winTo, envelope: stored})
			env.Cursor, env.Prev = pos, prev
			taken = append(taken, env)
		}
		for id, s := range streams {
			if len(s.touched) == 0 || s.st.id == "" {
				continue
			}
			if _, err := tx.Exec(`UPDATE streams SET next_seq = ?, epoch = ? WHERE id = ?`, s.st.nextSeq, s.st.epoch, id); err != nil {
				return err
			}
			for origin := range s.touched {
				if _, err := tx.Exec(`INSERT INTO origins (stream_id, name, last_n, last_at, logged_n) VALUES (?, ?, ?, ?, ?)
					ON CONFLICT (stream_id, name) DO UPDATE SET last_n = excluded.last_n, last_at = excluded.last_at, logged_n = excluded.logged_n`,
					id, origin, s.last[origin][0], s.last[origin][1], s.last[origin][2]); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err == nil {
		h.lid = lid
		h.appended(rows)
	}
	h.logMu.Unlock()
	if err != nil {
		return err
	}
	h.statsOf(tc.name, func(s *TrunkStats) {
		s.Received += int64(len(taken))
		s.Suspect += suspects
	})
	// To the servers of each stream here, in the order taken, in as few
	// frames a server as hold them; each envelope is made ready once,
	// whoever it goes to.
	out := map[string][][]byte{}
	local := map[string][]string{}
	for _, env := range taken {
		to, ok := local[env.StreamID]
		if !ok {
			to = h.routeFor(streams[env.StreamID].st).local
			local[env.StreamID] = to
		}
		var raw []byte
		for _, name := range to {
			if name != env.Origin {
				if raw == nil {
					raw = mustEnvelope(env)
				}
				out[name] = append(out[name], raw)
			}
		}
	}
	for name, raws := range out {
		if sc := h.connOf(name); sc != nil {
			sendDeliveries(sc.conn, raws)
		}
	}
	if len(taken) > 0 {
		h.wakeTrunks()
	}
	for id := range gaps {
		tc.send(wire.Frame{Frame: wire.FSync, Streams: map[string]wire.SyncStream{id: {Have: h.haveOf(id)}}, Done: true})
	}
	if len(later) > 0 {
		// Asked for again when what was missing has had time to come: soon
		// at first, then less and less often, so that a neighbour that sends
		// what can never be taken costs little.
		tc.mu.Lock()
		wait := 300 * time.Millisecond << min(tc.putOff, 7)
		tc.putOff++
		tc.mu.Unlock()
		go func() {
			select {
			case <-tc.conn.Done():
				return
			case <-time.After(wait):
			}
			for id := range later {
				tc.send(wire.Frame{Frame: wire.FSync, Streams: map[string]wire.SyncStream{id: {Have: h.haveOf(id)}}, Done: true})
			}
		}()
	} else if len(taken) > 0 {
		tc.mu.Lock()
		tc.putOff = 0
		tc.mu.Unlock()
	}
	return tc.send(ack)
}
