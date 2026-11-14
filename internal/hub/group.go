package hub

import (
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"sync"

	"github.com/tikaydaes/churr/internal/link/wire"
)

// This file is how submitted envelopes are written: in groups.
//
// The hub's database has one connection and every commit waits for the disk.
// One transaction an envelope therefore fixed how many envelopes a hub could
// take in a second, however many servers were sending. Here the frames that
// arrive while a commit is under way wait in a line, and whoever finishes
// takes the whole line into the next transaction: one commit for as many
// envelopes as came in the meantime. Nothing is weaker for it. An envelope is
// acknowledged to its origin, delivered to the stream's other servers and
// shipped over a trunk only after the transaction that holds it has
// committed, exactly as before; what changed is how many share a commit.

// groupMax bounds one transaction: enough to ride out a burst, not so much
// that the servers at the front of the line wait long for those at the back.
const groupMax = 2048

// sendQueueBytes is how much may wait to be written to one server before the
// hub gives its connection up as too slow. The server catches up from the
// log when it returns.
const sendQueueBytes = 64 << 20

// submitJob is one submit frame on its way through the line.
type submitJob struct {
	sc   *serverConn
	lim  *serverLimits
	envs []wire.Envelope
	// early is what was wrong with an envelope before its stream was looked
	// at (its shape, its size, whose connection it came on); signed whether
	// its origin's signature verifies. Both are found by the connection's
	// own goroutine, so the signatures of many servers are checked at once.
	early  []*wire.Error
	signed []bool
	// What became of each envelope: taken (nil), refused or failed (errs),
	// or not looked at because a refusal earlier in the frame stopped its
	// stream (skipped).
	errs    []error
	skipped []bool
	done    chan struct{}
}

// committer is the line.
type committer struct {
	mu    sync.Mutex
	queue []*submitJob
	busy  bool
}

// onSubmit takes a submit frame: one envelope or a batch, which may be of
// several streams. Each is taken into its stream's log or refused; one frame
// answers, naming what was taken and the first envelope that was not. After
// a refusal nothing further of that stream is taken from the frame, so an
// origin's events never overtake one another.
func (h *Hub) onSubmit(sc *serverConn, f wire.Frame) error {
	var envs []wire.Envelope
	if f.Envelope != nil {
		envs = append(envs, *f.Envelope)
	}
	envs = append(envs, f.Envelopes...)
	if len(envs) == 0 {
		return wire.Errf(wire.EBadFrame, "A submit carries at least one envelope.")
	}
	job := &submitJob{sc: sc, lim: h.limits(sc.name), envs: envs, early: make([]*wire.Error, len(envs)), signed: make([]bool, len(envs)),
		errs: make([]error, len(envs)), skipped: make([]bool, len(envs)), done: make(chan struct{})}
	for i, env := range envs {
		switch {
		case !env.WellFormed() || env.Cursor != 0:
			job.early[i] = wire.Errf(wire.EBadFrame, "The frame carries a whole envelope with no cursor.")
		case len(env.Ciphertext) > wire.MaxEnvelopeBytes:
			job.early[i] = wire.Errf(wire.ETooLarge, "An envelope holds at most %d bytes of ciphertext.", wire.MaxEnvelopeBytes)
		case env.Origin != sc.name:
			job.early[i] = wire.Errf(wire.ENotInStream, "An envelope is submitted by its origin, and %s is not this connection's server.", env.Origin)
		default:
			job.signed[i] = env.Verify(sc.key)
		}
	}
	h.enqueue(job)
	<-job.done
	reply := wire.Frame{Frame: wire.FAccepted, Re: f.ID}
	for i, env := range envs {
		if job.skipped[i] {
			continue
		}
		if job.errs[i] == nil {
			reply.IDs = append(reply.IDs, env.OriginEventID)
			continue
		}
		e := wire.AsError(job.errs[i])
		if e.Code == wire.EInternal {
			h.log.Warn("a submitted envelope could not be taken", "server", sc.name, "err", job.errs[i])
		}
		if reply.Code == "" {
			reply.Code, reply.Message, reply.RetryAfterSeconds = e.Code, e.Message, e.RetryAfter
			reply.StreamID, reply.OriginEventID, reply.Clock = env.StreamID, env.OriginEventID, e.Clock
		}
	}
	return sc.conn.Send(reply)
}

// enqueue puts a frame in the line and makes sure somebody is working
// through it.
func (h *Hub) enqueue(job *submitJob) {
	c := &h.commits
	c.mu.Lock()
	c.queue = append(c.queue, job)
	if c.busy {
		c.mu.Unlock()
		return
	}
	c.busy = true
	c.mu.Unlock()
	go h.commitLoop()
}

// commitLoop writes group after group until the line is empty.
func (h *Hub) commitLoop() {
	c := &h.commits
	for {
		c.mu.Lock()
		if len(c.queue) == 0 {
			c.busy = false
			c.mu.Unlock()
			return
		}
		n, envs := 0, 0
		for n < len(c.queue) && (n == 0 || envs+len(c.queue[n].envs) <= groupMax) {
			envs += len(c.queue[n].envs)
			n++
		}
		group := c.queue[:n:n]
		c.queue = c.queue[n:]
		c.mu.Unlock()
		h.writeGroup(group)
	}
}

// writeGroup takes a group of submit frames into the log in one transaction
// and then tells everyone. If the transaction fails, each frame is tried by
// itself, so that one envelope the database will not take costs only its own
// frame.
func (h *Hub) writeGroup(jobs []*submitJob) {
	// One stream at a time everywhere else; here all of the group's, taken in
	// one order, as a batch from a trunk takes its own (onRelay).
	seen := map[string]bool{}
	var ids []string
	for _, job := range jobs {
		for i, env := range job.envs {
			if job.early[i] == nil && !seen[env.StreamID] {
				seen[env.StreamID] = true
				ids = append(ids, env.StreamID)
			}
		}
	}
	sort.Strings(ids)
	for _, id := range ids {
		defer h.lock(id).Unlock()
	}
	err := h.writeLocked(jobs)
	if err != nil && len(jobs) > 1 {
		for _, job := range jobs {
			if err := h.writeLocked([]*submitJob{job}); err != nil {
				failJob(job, err)
			}
		}
	} else if err != nil {
		failJob(jobs[0], err)
	}
	for _, job := range jobs {
		close(job.done)
	}
}

func failJob(job *submitJob, err error) {
	for i := range job.envs {
		job.errs[i], job.skipped[i] = err, false
	}
}

// originState is what the hub holds of one origin in one stream: the last
// counter it has seen, the last time it took, the last counter it logged.
type originState struct {
	lastN, lastAt, loggedN int64
	touched                bool
}

// streamState is one stream while a group is written.
type streamState struct {
	st      stream
	err     error
	origins map[string]*originState
	// rec is the channel's record, for a channel a hub owns; read when an
	// envelope names a key epoch the stream has not reached.
	rec       *wire.ChannelRecord
	recLoaded bool
	dirty     bool
}

// writeLocked is the transaction of writeGroup. The caller holds the lock of
// every stream the group names.
func (h *Hub) writeLocked(jobs []*submitJob) error {
	type taken struct {
		raw    []byte // the envelope as delivered: with its place in this log
		origin string
		to     []string
	}
	states := map[string]*streamState{}
	counts := map[string]*[3]int64{}
	countOf := func(name string) *[3]int64 {
		c := counts[name]
		if c == nil {
			c = &[3]int64{}
			counts[name] = c
		}
		return c
	}
	var rows []logRow
	var all []taken
	now := h.now()
	for _, job := range jobs {
		for i := range job.envs {
			job.errs[i], job.skipped[i] = nil, false
		}
	}
	// One line for every write to the log: the number a trunk ships a row by
	// is given in the order rows are written.
	h.logMu.Lock()
	lid := h.lid
	err := h.tx(func(tx *sql.Tx) error {
		ins, err := tx.Prepare(`INSERT INTO log (stream_id, seq, origin_event_id, key_epoch, envelope, at, origin, n, prev_n, lid, win_from, win_to)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
		if err != nil {
			return err
		}
		defer ins.Close()
		// take checks one envelope and writes it. A refusal is the origin's
		// to hear; an error is the database's.
		take := func(job *submitJob, i int) (*wire.Error, error) {
			env := job.envs[i]
			name := job.sc.name
			if e := job.early[i]; e != nil {
				return e, nil
			}
			ss := states[env.StreamID]
			if ss == nil {
				ss = &streamState{origins: map[string]*originState{}}
				ss.st, ss.err = loadStream(tx, env.StreamID)
				states[env.StreamID] = ss
			}
			if ss.err != nil {
				var e *wire.Error
				if errors.As(ss.err, &e) {
					return wire.Errf(e.Code, "%s", e.Message), nil
				}
				return nil, ss.err
			}
			st := &ss.st
			switch {
			case st.state != "open":
				return wire.Errf(wire.EUnknownStream, "That stream is closed."), nil
			case !st.has(name):
				return wire.Errf(wire.ENotInStream, "%s is not in that stream.", name), nil
			case !job.signed[i]:
				return wire.Errf(wire.EBadSignature, "The envelope's signature does not verify."), nil
			case env.N <= 0 || env.At <= 0:
				return wire.Errf(wire.EBadFrame, "A submitted envelope is stamped."), nil
			}
			// Taken before: acknowledged again, whatever key epoch it comes
			// under this time. An origin whose event was taken, and which did
			// not hear so before the epoch changed, submits the same event
			// again under the new epoch with the same identifier and the same
			// stamp. It is in the log, and its servers have it. Were it
			// refused here (its counter is no longer above the origin's last)
			// the origin would stamp it again, and the event would stand at
			// one stamp on the servers that were delivered it and at another
			// on its own.
			var one int
			if tx.QueryRow(`SELECT 1 FROM log WHERE stream_id = ? AND origin_event_id = ? LIMIT 1`, env.StreamID, env.OriginEventID).Scan(&one) == nil {
				return nil, nil
			}
			if r := job.lim.submits.Take(name); !r.OK {
				e := wire.Errf(wire.ERateLimited, "Too many envelopes.")
				e.RetryAfter = max(r.RetryAfterSeconds(), 1)
				return e, nil
			}
			// The key epoch is its maker's to start. The hub holds no key and
			// reads no event; it sees the epoch each envelope names, and takes
			// one party's word, and only that party's, for a new one: the
			// owner server of a stream a server owns, and for a channel a hub
			// owns the member server its home hub named to make that epoch's
			// key, as the record this hub holds says.
			if env.KeyEpoch < st.epoch {
				return wire.Errf(wire.EStaleEpoch, "Key epoch %d has ended; the stream is at %d.", env.KeyEpoch, st.epoch), nil
			}
			if env.KeyEpoch > st.epoch {
				starts := name == st.owner
				if st.owner == h.id.Name || (st.homeHub != "" && st.owner == st.homeHub) {
					if !ss.recLoaded {
						ss.recLoaded = true
						if c, err := loadChannel(tx, st.id); err == nil {
							ss.rec = &c.rec
						}
					}
					starts = ss.rec != nil && ss.rec.KeyMaker == name && ss.rec.Epoch == env.KeyEpoch
				}
				if !starts {
					return wire.Errf(wire.EEarlyEpoch, "Key epoch %d has not started; the stream is at %d.", env.KeyEpoch, st.epoch), nil
				}
			}
			// The stream's time reference is its home hub: this hub, or
			// another of the cluster, whose clock this hub reads through the
			// trunks between.
			e, winFrom, winTo := h.checkStamp(tx, env, st.homeHub)
			if e != nil {
				return e, nil
			}
			// An origin's `at` and `n` only ever increase.
			o := ss.origins[name]
			if o == nil {
				o = &originState{}
				tx.QueryRow(`SELECT last_n, last_at, logged_n FROM origins WHERE stream_id = ? AND name = ?`, st.id, name).Scan(&o.lastN, &o.lastAt, &o.loggedN)
				ss.origins[name] = o
			}
			if env.N <= o.lastN || env.At <= o.lastAt {
				return wire.Errf(wire.ENotMonotonic, "After n %d at %d, n %d at %d is not later.", o.lastN, o.lastAt, env.N, env.At), nil
			}
			stored := mustJSON(env)
			pos := st.nextSeq
			// The origin's envelope before this one in the log, whether or
			// not the log still holds it.
			prev := o.lastN
			lid++
			if _, err := ins.Exec(st.id, pos, env.OriginEventID, env.KeyEpoch, stored, int64(now), name, env.N, prev, lid, winFrom, winTo); err != nil {
				return nil, err
			}
			st.nextSeq++
			st.epoch = max(st.epoch, env.KeyEpoch)
			o.lastN, o.lastAt, o.loggedN, o.touched = env.N, env.At, env.N, true
			ss.dirty = true
			if st.homeHub != "" {
				rows = append(rows, logRow{lid: lid, stream: st.id, origin: name, n: env.N, prev: prev, winFrom: winFrom, winTo: winTo, envelope: stored})
			}
			c := countOf(name)
			c[0]++
			c[1] += int64(len(stored))
			env.Cursor, env.Prev = pos, prev
			var others []string
			for _, n := range st.servers {
				if n != name {
					others = append(others, n)
				}
			}
			all = append(all, taken{raw: mustJSON(env), origin: name, to: others})
			return nil, nil
		}
		for _, job := range jobs {
			stopped := map[string]bool{}
			for i, env := range job.envs {
				if stopped[env.StreamID] {
					job.skipped[i] = true
					continue
				}
				e, err := take(job, i)
				if err != nil {
					return err
				}
				if e != nil {
					e.StreamID, e.OriginEventID = env.StreamID, env.OriginEventID
					job.errs[i] = e
					stopped[env.StreamID] = true
					countOf(job.sc.name)[2]++
				}
			}
		}
		for id, ss := range states {
			if !ss.dirty {
				continue
			}
			if _, err := tx.Exec(`UPDATE streams SET next_seq = ?, epoch = ? WHERE id = ?`, ss.st.nextSeq, ss.st.epoch, id); err != nil {
				return err
			}
			for name, o := range ss.origins {
				if !o.touched {
					continue
				}
				if _, err := tx.Exec(`INSERT INTO origins (stream_id, name, last_n, last_at, logged_n) VALUES (?, ?, ?, ?, ?)
					ON CONFLICT (stream_id, name) DO UPDATE SET last_n = excluded.last_n, last_at = excluded.last_at, logged_n = excluded.logged_n`,
					id, name, o.lastN, o.lastAt, o.loggedN); err != nil {
					return err
				}
			}
		}
		hour := int64(now) / 3600000
		for name, c := range counts {
			if _, err := tx.Exec(`INSERT INTO counters (name, hour, envelopes, bytes, refusals) VALUES (?, ?, ?, ?, ?)
				ON CONFLICT (name, hour) DO UPDATE SET envelopes = envelopes + excluded.envelopes, bytes = bytes + excluded.bytes,
					refusals = refusals + excluded.refusals`, name, hour, c[0], c[1], c[2]); err != nil {
				return err
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
	if len(rows) > 0 {
		h.wakeTrunks() // the trunks that lead to the streams' other servers ship them on
	}
	// To every other server of each stream that is connected, in the order
	// taken: everything of the group that is for one server in as few frames
	// as hold it.
	out := map[string][][]byte{}
	var order []string
	for _, t := range all {
		for _, name := range t.to {
			if _, ok := out[name]; !ok {
				order = append(order, name)
			}
			out[name] = append(out[name], t.raw)
		}
	}
	for _, name := range order {
		if sc := h.connOf(name); sc != nil {
			sendDeliveries(sc.conn, out[name])
		}
	}
	return nil
}

// deliverMaxItems and deliverMaxBytes bound one deliver frame.
const (
	deliverMaxItems = 64
	deliverMaxBytes = 512 << 10
)

// sendDeliveries sends envelopes, each already as JSON with its place in the
// log, in deliver frames: one alone as `envelope`, several as `envelopes`.
func sendDeliveries(conn *wire.Conn, raws [][]byte) {
	for len(raws) > 0 {
		n, size := 0, 0
		for n < len(raws) && n < deliverMaxItems && (n == 0 || size+len(raws[n]) <= deliverMaxBytes) {
			size += len(raws[n])
			n++
		}
		var frame struct {
			Frame     string            `json:"frame"`
			Envelope  json.RawMessage   `json:"envelope,omitempty"`
			Envelopes []json.RawMessage `json:"envelopes,omitempty"`
		}
		frame.Frame = wire.FDeliver
		if n == 1 {
			frame.Envelope = raws[0]
		} else {
			for _, raw := range raws[:n] {
				frame.Envelopes = append(frame.Envelopes, raw)
			}
		}
		conn.SendRaw(mustJSON(frame))
		raws = raws[n:]
	}
}
