package link

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/tikaydaes/churr/internal/chat"
	"github.com/tikaydaes/churr/internal/link/wire"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

// This file is the way out: what chat calls when a local change happens in a
// shared conversation (chat.Outbound), the outbox, and the turning of a
// local event into the event that travels.
//
// A local change is made at once and shown in its place (chat/shared.go).
// Its events are stamped as they are emitted, and at the end of the
// transaction they are written here: to the stream's log when this server
// is the stream's time reference, to the outbox otherwise. An outbox row is
// one event waiting for the reference to acknowledge it, which is all that
// "unsent" means.

// wireEvent is a local event as it will travel: the type and fields of the
// inner event, and the origin event id when it must be a particular one.
type wireEvent struct {
	Type   string
	Fields map[string]any
	// ID is the message's own id for message.created, so that a message
	// and the event that made it are found by one identifier. Empty for
	// the rest.
	ID string
	// After, if set, runs once the transaction that made the event has
	// committed.
	After func()
}

// outboxRecord is what an outbox row holds: the inner event, not yet
// encrypted, and the stamp it was given when it was made. It is encrypted
// when it is submitted, under the highest epoch held at that moment, so an
// event that waits through a change of epoch leaves under the new one with
// the same origin event id and the same stamp (section 13). File contents
// are never in it.
type outboxRecord struct {
	Type   string         `json:"type"`
	Fields map[string]any `json:"fields"`
	At     int64          `json:"at"`
	N      int64          `json:"n"`
	Offset int64          `json:"offset"`
	// Epoch, when not 0, is the one key epoch the event may leave under:
	// the stream.servers_changed that starts it.
	Epoch int64 `json:"epoch,omitempty"`
	// Refusals counts the times the reference refused this event for its
	// stamp and it was stamped again for it, and StampedAgain is when, by
	// this server's clock, that last happened. They are kept apart from the
	// row's tries, which also count every time the event was sent again
	// after a wait: an event told to slow down a few times had no try left
	// to be stamped again in when it grew old (section 13).
	Refusals     int   `json:"refusals,omitempty"`
	StampedAgain int64 `json:"stamped_again,omitempty"`
}

// restampFast is how many times one event is stamped again at once. After
// that something is wrong that stamping again does not mend at once, and the
// event is stamped again no more often than its wait allows.
const restampFast = 5

func (r outboxRecord) stamp() wire.Stamp { return wire.Stamp{At: r.At, N: r.N, Offset: r.Offset} }

func (s *Service) putOutbox(ctx context.Context, a store.Areas, st store.Stream, id, typ string, fields map[string]any, stamp wire.Stamp, epoch int64) error {
	if _, has := fields["at"]; !has {
		fields["at"] = s.now()
	}
	rec := outboxRecord{Type: typ, Fields: fields, At: stamp.At, N: stamp.N, Offset: stamp.Offset, Epoch: epoch}
	return a.Streams().PutOutbox(ctx, store.OutboxRow{ID: id, StreamID: st.ID, Envelope: mustJSON(rec),
		CreatedAt: s.now(), NextTryAt: s.now().Add(outboxFirstWait)})
}

// waiting is one outbox row read back.
type waiting struct {
	row store.OutboxRow
	rec outboxRecord
}

// outboxOf returns what waits for a stream, in the order it was stamped.
func (s *Service) outboxOf(ctx context.Context, id model.StreamID) ([]waiting, error) {
	rows, err := s.store.Streams().Outbox(ctx, id)
	if err != nil {
		return nil, err
	}
	out := make([]waiting, 0, len(rows))
	for _, row := range rows {
		var rec outboxRecord
		if json.Unmarshal(row.Envelope, &rec) != nil || rec.N == 0 {
			s.store.Streams().DeleteOutbox(ctx, row.ID)
			continue
		}
		out = append(out, waiting{row, rec})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].rec.N < out[j].rec.N })
	return out, nil
}

// kickStream submits what waits for one stream, oldest first, a frame at a
// time: the next frame leaves when the last has been answered, so one
// origin's events reach the reference in the order they were made. While
// the oldest waits out a refusal nothing passes it, unless force says the
// wait is over.
func (s *Service) kickStream(ctx context.Context, id model.StreamID, force bool) {
	st, err := s.store.Streams().Get(ctx, id)
	if err != nil || st.State != store.StreamActive {
		return
	}
	rt := s.rt(id)
	rt.submit.Lock()
	defer rt.submit.Unlock()
	rows, err := s.outboxOf(ctx, id)
	if err != nil || len(rows) == 0 {
		return
	}
	if st.Sequencer == store.SequencerSelf {
		// A stream this server has come to be the reference of: what
		// waited for the old one goes straight into the log.
		s.logWaiting(ctx, st, rows)
		return
	}
	pc := s.connOf(st.PeerID)
	if pc == nil {
		return
	}
	now := time.Now()
	rt.mu.Lock()
	busy := rt.inflight != 0 && now.Sub(rt.inflightAt) < inflightWait
	announcing := rt.announcing
	rt.mu.Unlock()
	if announcing {
		// What was said before this server joined waits for its members'
		// announcements, whoever asks for it to go (announceMembers).
		return
	}
	if busy && !force {
		return
	}
	if head := rows[0].row; !force && head.Attempts > 0 && head.NextTryAt.After(s.now()) {
		s.log.Debug("link: the oldest waiting event is not due yet", "stream", id, "type", rows[0].rec.Type, "attempts", head.Attempts)
		return
	}
	var envs []wire.Envelope
	size := 0
	for _, w := range rows {
		env, err := s.seal(ctx, s.store, st, w.row.ID, w.rec.Type, w.rec.Fields, w.rec.stamp(), w.rec.Epoch)
		if err != nil {
			s.log.Debug("link: a waiting event cannot be sealed yet", "stream", id, "type", w.rec.Type, "epoch", st.KeyEpoch, "err", err)
			break // no key yet; it goes when one arrives
		}
		size += len(env.Ciphertext) + 200
		if len(envs) > 0 && (len(envs) >= submitBatch || size > wire.MaxFrameBytes/2) {
			break
		}
		envs = append(envs, env)
	}
	if len(envs) == 0 {
		return
	}
	f := wire.Frame{Frame: wire.FSubmit, ID: pc.conn.NextID(), Envelopes: envs}
	if hook := s.testSubmitting.Load(); hook != nil {
		if f = (*hook)(st.ID, f); len(f.Envelopes) == 0 {
			return
		}
	}
	rt.mu.Lock()
	rt.inflight, rt.inflightAt = f.ID, now
	rt.mu.Unlock()
	s.mu.Lock()
	s.submitted[submitKey{pc.peer.ID, f.ID}] = id
	s.mu.Unlock()
	if err := pc.conn.Send(f); err != nil {
		rt.mu.Lock()
		rt.inflight = 0
		rt.mu.Unlock()
	}
}

// logWaiting moves a stream's waiting events into the log this server now
// keeps for it.
func (s *Service) logWaiting(ctx context.Context, st store.Stream, rows []waiting) {
	for _, w := range rows {
		var env wire.Envelope
		err := s.store.Tx(ctx, func(tx store.Tx) error {
			var err error
			if env, err = s.seal(ctx, tx, st, w.row.ID, w.rec.Type, w.rec.Fields, w.rec.stamp(), w.rec.Epoch); err != nil {
				return err
			}
			if env, err = s.appendLog(ctx, tx, st.ID, env); err != nil {
				return err
			}
			_, err = tx.Streams().DeleteOutbox(ctx, w.row.ID)
			return err
		})
		if err != nil {
			return
		}
		s.acknowledged(ctx, st, w)
		s.deliverOut(ctx, st, env)
	}
}

// acknowledged is what follows a waiting event being taken by the stream's
// time reference: whoever waits for it here is told, and a message is no
// longer unsent.
func (s *Service) acknowledged(ctx context.Context, st store.Stream, w waiting) {
	rt := s.rt(st.ID)
	rt.mu.Lock()
	ch := rt.watchers[w.row.ID]
	delete(rt.watchers, w.row.ID)
	rt.mu.Unlock()
	if ch != nil {
		close(ch)
	}
	if err := s.store.Streams().Acked(ctx, st.ID, w.rec.At); err != nil {
		s.log.Warn("link: an acknowledgement could not be noted", "stream", st.ID, "err", err)
	}
	if w.rec.Type == model.EventMessageCreated && st.ConversationID != "" {
		s.chat.MessageSent(ctx, st.ConversationID, model.MessageID(w.row.ID))
	}
}

// onAccepted is the answer to a submit: the envelopes the reference took,
// and the first it refused, if any.
func (s *Service) onAccepted(ctx context.Context, pc *peerConn, f wire.Frame) {
	if s.testDeaf.Load() {
		return // a test is playing a link that loses the answer
	}
	s.mu.Lock()
	answer := s.testAnswers[f.Re]
	delete(s.testAnswers, f.Re)
	s.mu.Unlock()
	if answer != nil {
		answer <- f // a submit a test sent itself
		return
	}
	touched, took := map[model.StreamID]bool{}, map[model.StreamID]bool{}
	note := func(id model.StreamID) {
		touched[id] = true
		rt := s.rt(id)
		rt.mu.Lock()
		if rt.inflight == f.Re {
			rt.inflight = 0
		}
		rt.mu.Unlock()
	}
	// The stream the frame was for is free to send again, whether or not
	// anything it named still waits here: a frame sent twice is answered
	// twice, and the second answer names rows the first already cleared.
	s.mu.Lock()
	sent, known := s.submitted[submitKey{pc.peer.ID, f.Re}]
	delete(s.submitted, submitKey{pc.peer.ID, f.Re})
	s.mu.Unlock()
	if known {
		note(sent)
	}
	for _, id := range f.IDs {
		row, err := s.store.Streams().GetOutbox(ctx, id)
		if err != nil {
			continue
		}
		st, err := s.store.Streams().Get(ctx, row.StreamID)
		if err != nil || st.PeerID != pc.peer.ID {
			continue // not this peer's to acknowledge
		}
		var rec outboxRecord
		json.Unmarshal(row.Envelope, &rec)
		if _, err := s.store.Streams().DeleteOutbox(ctx, id); err != nil {
			continue
		}
		note(st.ID)
		took[st.ID] = true
		s.acknowledged(ctx, st, waiting{row, rec})
	}
	again := false
	if f.Code != "" {
		s.count(func(st *Stats) { st.Dropped["error:"+f.Code]++ })
		s.mu.Lock()
		s.lastError = wire.Frame{Frame: wire.FError, Re: f.Re, Code: f.Code, Message: f.Message, StreamID: f.StreamID, OriginEventID: f.OriginEventID}
		s.mu.Unlock()
		if id := model.StreamID(f.StreamID); id != "" {
			note(id)
			again = s.refused(ctx, pc, f)
		}
	}
	for id := range touched {
		switch {
		case string(id) != f.StreamID || f.Code == "":
			// The next frame, if more waits. When the reference has just
			// taken this stream's events the road is open: what waits goes
			// now, whatever wait an earlier refusal or a new stamp left on
			// the event at its head. That wait is for trying again after a
			// no, and would hold a long queue back a frame at a time.
			s.kickStream(ctx, id, took[id])
		case again:
			s.kickStream(ctx, id, true) // the refusal has been dealt with
		}
	}
}

// refused acts on the refusal of one of this server's events. It reports
// whether the stream is to be submitted again at once.
//
//   - Refused for its stamp: the event is stamped again, with the clock
//     figures the reference sent if it sent any, and goes again. A message
//     moves to where its new stamp puts it, and local members are told.
//     After five such refusals of one event it is stamped again only as
//     often as its wait allows.
//   - Refused for its key epoch: it goes again, under whatever key is held
//     by then.
//   - Anything else waits, twice as long each time, from 5 seconds up to 5
//     minutes.
func (s *Service) refused(ctx context.Context, pc *peerConn, f wire.Frame) bool {
	row, err := s.store.Streams().GetOutbox(ctx, f.OriginEventID)
	if err != nil || string(row.StreamID) != f.StreamID {
		return false
	}
	st, err := s.store.Streams().Get(ctx, row.StreamID)
	if err != nil || st.PeerID != pc.peer.ID {
		return false
	}
	var rec outboxRecord
	if json.Unmarshal(row.Envelope, &rec) != nil {
		return false
	}
	s.log.Debug("link: an event was refused", "peer", pc.name, "stream", st.ID, "type", rec.Type, "code", f.Code, "message", f.Message)
	wait := outboxFirstWait
	for i := 0; i < row.Attempts && wait < outboxMaxWait; i++ {
		wait *= 2
	}
	if wait > outboxMaxWait {
		wait = outboxMaxWait
	}
	if f.RetryAfterSeconds > 0 {
		wait = time.Duration(f.RetryAfterSeconds) * time.Second
	}
	later := func() bool {
		attempts := row.Attempts + 1
		if f.Code == wire.ERateLimited {
			// Said in the log: every event of the stream now waits
			// behind this one, and nothing else would say why.
			s.limited.Note("envelopes", pc.name, f.RetryAfterSeconds)
			// Told to slow down, not that anything is wrong with the
			// event: it is not a try that failed. Counted as one, a busy
			// minute used up the five tries an event has to be stamped
			// again in; by then its stamp was more than 30 seconds old,
			// the reference refused it for that every time it was offered,
			// and the stream sent nothing more for good.
			attempts = max(row.Attempts, 1)
		}
		s.store.Streams().OutboxTried(ctx, row.ID, attempts, s.now().Add(wait))
		return false
	}
	if f.Code == wire.EUnknownStream {
		if mv, ok := s.openMove(ctx, s.store, st.ID); ok && !outgoing(mv) && mv.FromPeerID == pc.peer.ID {
			// The owner server no longer takes this stream's events: it
			// has moved it onto the hub, and its stream.moved has not
			// reached this server. It is asked for, once: every frame of
			// this server's that is refused in the meantime says the same
			// thing, and asking again each time had the owner server send
			// the end of its log over and over, to a server that then spent
			// its time recognising what it already held.
			s.catchUp(ctx, st, false)
		}
	}
	switch {
	case wire.Restamp(f.Code):
		if rec.Refusals >= restampFast {
			// Something is wrong that stamping again does not mend at
			// once. The event waits as for any other refusal, twice as long
			// each time, and is stamped again when it is refused after a
			// wait: the stamp it has only grows older, so sending it again
			// as it is would be refused for ever, and nothing of the
			// stream would be sent again.
			slow := outboxFirstWait
			for i := restampFast; i < rec.Refusals && slow < outboxMaxWait; i++ {
				slow *= 2
			}
			if slow > outboxMaxWait {
				slow = outboxMaxWait
			}
			if since := time.Duration(int64(s.now())-rec.StampedAgain) * time.Millisecond; since >= 0 && since < slow {
				wait = slow - since
				return later()
			}
		}
		if f.Clock != nil && s.referencePeer(st) == pc.peer.ID {
			// What the reference holds of this server's clock, now.
			s.setToldFor(ctx, pc, st, *f.Clock)
			if f.Code != wire.ENotMonotonic && f.Clock.Offset == rec.Offset {
				// The offset was right and the time was not: this
				// server's clock has moved since the reference measured
				// it. Stamping again now would give the same wrong time.
				// The reference will notice within a ping or two and say
				// so (clockMoved).
				rt := s.rt(st.ID)
				rt.mu.Lock()
				rt.awaitClock = true
				rt.mu.Unlock()
				return later()
			}
		}
		if err := s.restamp(ctx, st, rec); err != nil {
			s.log.Warn("link: a refused event could not be stamped again", "stream", st.ID, "err", err)
			return later()
		}
		s.count(func(stats *Stats) { stats.Restamped++ })
		return true
	case f.Code == wire.EStaleEpoch:
		// The epoch moved on while the event waited. It goes again under
		// the current one as soon as this server has caught up with it. In
		// a hub-owned channel that is when it holds the new epoch's key,
		// which the hub sent ahead of the record that began it: if it
		// does not, it asks, and the event goes when the key arrives.
		if hubOwned(st) {
			if cur, err := s.channelOf(ctx, s.store, st.ID); err == nil && cur.Epoch > st.KeyEpoch {
				s.adoptEpoch(ctx, st.ID)
				if row.Attempts < 6 {
					wait = 200 * time.Millisecond << row.Attempts
				}
				return later()
			}
		}
		if row.Attempts >= 5 || rec.Epoch != 0 {
			return later()
		}
		s.store.Streams().OutboxTried(ctx, row.ID, row.Attempts+1, s.now())
		return true
	case f.Code == wire.EClockUnknown:
		// The reference's first measurement of this server's clock is a
		// moment away.
		if row.Attempts < 6 {
			wait = 200 * time.Millisecond << row.Attempts
		}
		return later()
	}
	return later()
}

// restamp gives a waiting event a new stamp: a new `at` by the reference
// clock as this server now reads it, and a new counter. The origin event id
// stays. Every waiting event of the stream stamped after it is stamped again
// too, in their order, so that the origin's events keep the order they were
// made in.
func (s *Service) restamp(ctx context.Context, st store.Stream, rec outboxRecord) error {
	rows, err := s.outboxOf(ctx, st.ID)
	if err != nil {
		return err
	}
	var from []waiting
	for _, w := range rows {
		if w.rec.N >= rec.N {
			from = append(from, w)
		}
	}
	return s.stampAgain(ctx, st, from, true)
}

// stampAgain gives waiting events new stamps, in the order given. With
// rewind the stamps they had are void, and so is every stamp this server
// gave after the last one the reference acknowledged: stamping starts again
// from the highest time that stands. Without it they are simply moved to
// after everything stamped so far.
func (s *Service) stampAgain(ctx context.Context, st store.Stream, rows []waiting, rewind bool) error {
	return s.again(ctx, func() error { return s.stampAgainOnce(ctx, st, rows, rewind) })
}

func (s *Service) stampAgainOnce(ctx context.Context, st store.Stream, rows []waiting, rewind bool) error {
	return s.chat.RemoteTx(ctx, func(tx store.Tx, r *chat.Recorder) error {
		cur, err := s.lockStream(ctx, tx, st.ID)
		if err != nil {
			return err
		}
		if rewind {
			// Nobody but this server has seen the stamps that are void.
			if err := tx.Streams().Rewind(ctx, st.ID, s.self.ID); err != nil {
				return err
			}
			// Every stamp this server gave from the first of these on is
			// void, those of events made since the caller looked included:
			// the conversation is held now, so nothing more is being made,
			// and what waits is read again. Stamped in the order they were
			// made, a member's messages keep the order they were said in.
			from := int64(0)
			for _, w := range rows {
				if from == 0 || w.rec.N < from {
					from = w.rec.N
				}
			}
			held, err := tx.Streams().Outbox(ctx, st.ID)
			if err != nil {
				return err
			}
			rows = rows[:0:0]
			for _, row := range held {
				var rec outboxRecord
				if json.Unmarshal(row.Envelope, &rec) == nil && rec.N >= from && rec.N != 0 {
					rows = append(rows, waiting{row, rec})
				}
			}
			sort.SliceStable(rows, func(i, j int) bool { return rows[i].rec.N < rows[j].rec.N })
			if len(rows) > 0 {
				// The first is the one the reference refused.
				rows[0].rec.Refusals, rows[0].rec.StampedAgain = rows[0].rec.Refusals+1, int64(s.now())
			}
		}
		return s.restampRows(ctx, tx, r, cur, rows, false)
	})
}

// restampRows gives waiting events new stamps, in the order given, inside a
// transaction that holds the stream. A message moves to where its new stamp
// puts it. With fresh the events count as never tried: they wait for a
// reference that has not been offered them yet (a stream that has just moved
// onto a hub).
func (s *Service) restampRows(ctx context.Context, tx store.Tx, r *chat.Recorder, cur store.Stream, rows []waiting, fresh bool) error {
	for i, w := range rows {
		stamp, err := s.stamp(ctx, tx, cur)
		if err != nil {
			return err
		}
		w.rec.At, w.rec.N, w.rec.Offset = stamp.At, stamp.N, stamp.Offset
		attempts, next := w.row.Attempts+1, s.now().Add(outboxFirstWait)
		if i > 0 {
			// Only the first was refused. Those waiting behind it were
			// not tried, and a long queue stamped again five times over
			// must not find every event in it out of tries.
			attempts = w.row.Attempts
		}
		if fresh {
			attempts, next = 0, s.now()
			w.rec.Refusals, w.rec.StampedAgain = 0, 0
		}
		if err := tx.Streams().PutOutbox(ctx, store.OutboxRow{ID: w.row.ID, StreamID: cur.ID, Envelope: mustJSON(w.rec),
			CreatedAt: w.row.CreatedAt, NextTryAt: next}); err != nil {
			return err
		}
		if err := tx.Streams().OutboxTried(ctx, w.row.ID, attempts, next); err != nil {
			return err
		}
		order := model.OrderString(stamp.At, s.id.Name, stamp.N)
		switch w.rec.Type {
		case model.EventMessageCreated:
			if err := s.chat.PlaceMessage(ctx, tx, r, model.MessageID(w.row.ID), order); err != nil {
				return err
			}
			if err := tx.Streams().RaiseTop(ctx, cur.ID, order); err != nil {
				return err
			}
		case model.EventMessageEdited:
			if id := editedMessage(w.rec); id != "" {
				if err := tx.Streams().SetEditOrder(ctx, id, order); err != nil {
					return err
				}
			}
		case model.EventSealedEpochStarted:
			// The record's stamp is what settles two made at once.
			if epoch, ok := w.rec.Fields["key_epoch"].(float64); ok && cur.ConversationID != "" {
				if err := tx.Sealed().SetEpochOrder(ctx, cur.ConversationID, int64(epoch), order); err != nil {
					return err
				}
			}
		case evCallStarted:
			s.noteCallStart(cur.ConversationID, fmt.Sprint(w.rec.Fields["call_id"]), order, stamp.At)
		}
	}
	return nil
}

// editedMessage finds the message a waiting message.edited is about.
func editedMessage(rec outboxRecord) model.MessageID {
	raw, _ := json.Marshal(rec.Fields["message"])
	var m struct {
		ID model.MessageID `json:"id"`
	}
	json.Unmarshal(raw, &m)
	return m.ID
}

// kickOutbox submits what waits for the streams of one peer: what happens at
// once on reconnection.
func (s *Service) kickOutbox(ctx context.Context, peer model.PeerID) {
	streams, err := s.store.Streams().List(ctx, store.StreamActive)
	if err != nil {
		return
	}
	for _, st := range streams {
		if st.PeerID == peer {
			rt := s.rt(st.ID)
			rt.mu.Lock()
			rt.inflight = 0
			rt.mu.Unlock()
			s.kickStream(ctx, st.ID, true)
		}
	}
}

// retryOutbox submits again the rows whose wait is over, and doubles each
// one's wait from 5 seconds up to 5 minutes.
func (s *Service) retryOutbox(ctx context.Context) {
	now := s.now()
	rows, err := s.store.Streams().OutboxDue(ctx, now, 200)
	if err != nil {
		return
	}
	due := map[model.StreamID]bool{}
	for _, row := range rows {
		wait := outboxFirstWait
		for i := 0; i < row.Attempts && wait < outboxMaxWait; i++ {
			wait *= 2
		}
		if wait > outboxMaxWait {
			wait = outboxMaxWait
		}
		s.store.Streams().OutboxTried(ctx, row.ID, row.Attempts+1, now.Add(wait))
		due[row.StreamID] = true
	}
	for id := range due {
		s.kickStream(ctx, id, true)
	}
}

// ------------------------------------------------------------ chat.Outbound

var _ chat.Outbound = (*Service)(nil)

// streamOf finds the stream a shared conversation is in, whatever its state.
func (s *Service) streamOf(ctx context.Context, a store.Areas, conv model.Conversation) (store.Stream, error) {
	return a.Streams().Get(ctx, conv.StreamID)
}

// readOnlyError is what a member gets who tries to change a cross-server
// conversation whose stream has ended.
var readOnlyError = model.ErrForbidden.With("This conversation is read-only: it is no longer shared with the other server.")

// Stamp implements chat.Outbound.
func (s *Service) Stamp(ctx context.Context, tx store.Tx, stream model.StreamID) (chat.Stamp, bool, error) {
	st, err := tx.Streams().Get(ctx, stream)
	if errors.Is(err, model.ErrNotFound) {
		return chat.Stamp{}, false, nil
	}
	if err != nil {
		return chat.Stamp{}, false, err
	}
	switch st.State {
	case store.StreamLeft, store.StreamRemoved:
		return chat.Stamp{}, false, readOnlyError
	case store.StreamActive:
	case store.StreamOffered:
		if st.ConversationID == "" {
			return chat.Stamp{}, false, nil
		}
		// Accepted here, and the owner has not added this server yet.
		// What is said meanwhile is stamped and waits; it is sent once
		// this server is in the stream, after its members have been
		// announced (announceMembers).
	default:
		return chat.Stamp{}, false, nil
	}
	w, err := s.stamp(ctx, tx, st)
	if err != nil {
		return chat.Stamp{}, false, err
	}
	return chat.Stamp{At: w.At, Origin: s.id.Name, N: w.N, Offset: w.Offset, Unsent: st.Sequencer != store.SequencerSelf}, true, nil
}

// Commit implements chat.Outbound. An event that stays on this server (a
// pin, a level, an archive) is not sent at all: its stamp is simply not
// used, and a counter that skips is nobody's concern.
func (s *Service) Commit(ctx context.Context, tx store.Tx, conv model.Conversation, events []chat.Stamped) (func(), error) {
	st, err := s.streamOf(ctx, tx, conv)
	if errors.Is(err, model.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	plain := make([]model.Event, len(events))
	for i, e := range events {
		plain[i] = e.Event
	}
	if err := s.personalWritable(ctx, tx, st, conv, plain); err != nil {
		return nil, err
	}
	if err := s.managersHeld(ctx, tx, st, plain); err != nil {
		return nil, err
	}
	var logged []wire.Envelope
	var afters []func()
	queued := false
	for _, e := range events {
		w, err := s.toWire(ctx, tx, st, conv, e.Event)
		if err != nil {
			return nil, err
		}
		if w.Type == evLocal {
			continue
		}
		if w.After != nil {
			afters = append(afters, w.After)
		}
		id := w.ID
		if id == "" {
			id = newID()
		}
		stamp := wire.Stamp{At: e.Stamp.At, N: e.Stamp.N, Offset: e.Stamp.Offset}
		switch w.Type {
		case model.EventMessageCreated:
			// What arrives from now on below this is late.
			if err := tx.Streams().RaiseTop(ctx, st.ID, e.Stamp.Order()); err != nil {
				return nil, err
			}
		case model.EventMessageEdited:
			if err := tx.Streams().SetEditOrder(ctx, e.Event.MessageID, e.Stamp.Order()); err != nil {
				return nil, err
			}
		case model.EventSealedEpochStarted:
			// Kept with the record: of two made for one key epoch on two
			// servers at once, the earlier stamp stands.
			if row, ok := e.Event.Payload["epoch"].(model.EpochRecord); ok {
				if err := tx.Sealed().SetEpochOrder(ctx, conv.ID, row.KeyEpoch, e.Stamp.Order()); err != nil {
					return nil, err
				}
			}
		}
		if _, err := tx.Streams().MarkSeen(ctx, st.ID, id, e.Event.Seq, s.now()); err != nil {
			return nil, err
		}
		if st.Sequencer != store.SequencerSelf {
			if err := s.putOutbox(ctx, tx, st, id, w.Type, w.Fields, stamp, 0); err != nil {
				return nil, err
			}
			queued = true
			continue
		}
		// This server is the stream's time reference: the event goes into
		// the log now, and to the other server once this has committed.
		env, err := s.seal(ctx, tx, st, id, w.Type, w.Fields, stamp, 0)
		if err != nil {
			return nil, err
		}
		if env, err = s.appendLog(ctx, tx, st.ID, env); err != nil {
			return nil, err
		}
		logged = append(logged, env)
	}
	if len(logged) == 0 && len(afters) == 0 && !queued {
		return nil, nil
	}
	return func() {
		for _, env := range logged {
			s.deliverOut(s.ctx, st, env)
		}
		if queued {
			s.kickStream(s.ctx, st.ID, false)
		}
		for _, fn := range afters {
			fn()
		}
	}, nil
}

// Unsent implements chat.Outbound.
func (s *Service) Unsent(ctx context.Context, a store.Areas, conv model.Conversation) map[model.MessageID]bool {
	st, err := s.streamOf(ctx, a, conv)
	if err != nil || st.Sequencer == store.SequencerSelf {
		return nil
	}
	ids, err := a.Streams().OutboxIDs(ctx, st.ID)
	if err != nil || len(ids) == 0 {
		return nil
	}
	out := make(map[model.MessageID]bool, len(ids))
	for _, id := range ids {
		out[model.MessageID(id)] = true
	}
	return out
}

// Shared implements chat.Outbound.
func (s *Service) Shared(ctx context.Context, conv model.Conversation) *model.Shared {
	st, err := s.streamOf(ctx, s.store, conv)
	if err != nil {
		return nil
	}
	owner, err := s.ownerOf(ctx, st)
	if err != nil {
		return nil
	}
	_, names, err := s.activeServers(ctx, s.store, st.ID)
	if err != nil {
		return nil
	}
	if names == nil {
		names = []string{}
	}
	return &model.Shared{StreamID: st.ID, OwnerServer: owner.Name, Servers: names, MaxBlobBytes: st.MaxBlobBytes}
}

// ------------------------------------------------------- local to wire

// local is the event that stands in for a change that stays on this server.
func local() wireEvent { return wireEvent{Type: evLocal, Fields: map[string]any{}} }

// blobOf describes a stored file for an event's blobs.
func blobOf(f model.File, purpose string) wireBlob {
	return wireBlob{ID: string(f.ID), SHA256: f.SHA256, Size: f.Size, MediaType: f.MediaType, Purpose: purpose}
}

// profileOf builds the profile of one of this server's own accounts as it
// travels. Its version is this server's clock, which only grows.
func (s *Service) profileOf(ctx context.Context, a store.Areas, acc model.Account) (wireProfile, []wireBlob) {
	p := wireProfile{ID: acc.ID, Server: s.id.Name, Username: acc.Username, DisplayName: acc.DisplayName, Title: acc.Title,
		Pronouns: acc.Pronouns, TimeZone: acc.TimeZone, Bot: acc.Role == model.RoleBot, Version: int64(s.now()), Removed: acc.Removed()}
	var blobs []wireBlob
	if acc.AvatarFileID != "" {
		if f, err := a.Files().Get(ctx, acc.AvatarFileID); err == nil && f.BlobKey != "" {
			p.AvatarFileID = f.ID
			blobs = append(blobs, blobOf(f, "avatar"))
		}
	}
	return p, blobs
}

// wireMessageOf turns a message as an event carries it into the message
// that travels, with the blobs it refers to.
func (s *Service) wireMessageOf(ctx context.Context, a store.Areas, st store.Stream, conv model.Conversation, m model.Message) (wireMessage, []wireBlob, error) {
	var quote *model.Quote
	if m.Quote != nil {
		// The text a quote shows is each server's to fill in.
		q := *m.Quote
		q.Text = ""
		quote = &q
	}
	w := wireMessage{ID: m.ID, AuthorID: m.AuthorID, ParentID: m.ParentID, Quote: quote, Text: m.Text, Cards: m.Cards, Fallback: m.Fallback,
		Via: m.Via, System: m.System, Action: m.Action, Chance: m.Chance, CreatedAt: m.CreatedAt, EditedAt: m.EditedAt}
	if conv.Sealed() {
		w.ClientID, w.Sealed, w.Quiet = m.ClientID, m.Sealed, m.Quiet
	}
	var blobs []wireBlob
	// Text is never rewritten. The entities say what each name resolved
	// to, in terms every server understands.
	for _, e := range m.Entities {
		switch e.Type {
		case model.EntityGroup:
			continue
		case model.EntityConversation:
			if e.ConversationID != conv.ID {
				continue
			}
			we := wireEntity{Entity: e, StreamID: st.ID}
			we.ConversationID = ""
			w.Entities = append(w.Entities, we)
			continue
		case model.EntityEmoji:
			if em, err := a.Emoji().Get(ctx, e.EmojiID); err == nil && em.ServerName == "" {
				if f, err := a.Files().Get(ctx, em.FileID); err == nil {
					b := blobOf(f, "emoji")
					b.ID, b.Name = string(em.ID), em.Name
					blobs = append(blobs, b)
				}
			}
		}
		w.Entities = append(w.Entities, wireEntity{Entity: e})
	}
	if len(m.Files) > 0 {
		ids := make([]model.FileID, len(m.Files))
		for i, f := range m.Files {
			ids[i] = f.ID
		}
		rows, err := a.Files().GetMany(ctx, ids)
		if err != nil {
			return w, nil, err
		}
		byID := map[model.FileID]model.File{}
		for _, f := range rows {
			byID[f.ID] = f
		}
		for _, shown := range m.Files {
			f, ok := byID[shown.ID]
			if !ok {
				continue
			}
			w.Files = append(w.Files, wireFile{ID: f.ID, Name: f.Name, Size: f.Size, MediaType: f.MediaType, Kind: f.Kind, Width: f.Width,
				Height: f.Height, DurationSeconds: f.DurationSeconds, Caption: f.Caption, SHA256: f.SHA256})
			blobs = append(blobs, blobOf(f, "attachment"))
		}
	}
	for _, c := range m.Cards {
		if c.ImageFileID == "" {
			continue
		}
		if f, err := a.Files().Get(ctx, c.ImageFileID); err == nil {
			blobs = append(blobs, blobOf(f, "card"))
		}
	}
	if m.Via != nil && m.Via.IconFileID != "" {
		if f, err := a.Files().Get(ctx, m.Via.IconFileID); err == nil {
			blobs = append(blobs, blobOf(f, "avatar"))
		}
	}
	if m.Form != nil {
		// Left out: what each server works out for itself.
		form := *m.Form
		form.Tally, form.Voters, form.MyAnswer, form.AnswerCount = nil, nil, nil, 0
		raw, err := json.Marshal(form)
		if err != nil {
			return w, nil, err
		}
		var fields map[string]any
		json.Unmarshal(raw, &fields)
		delete(fields, "answer_count")
		w.Form = mustJSON(fields)
	}
	return w, blobs, nil
}

// isLocal reports whether an account lives on this server.
func isLocal(ctx context.Context, a store.Areas, id model.AccountID) bool {
	acc, err := a.Accounts().Get(ctx, id)
	return err == nil && acc.Local()
}

// toWire turns a local event into the event that travels, or into evLocal
// when the change is this server's own business (section 12, "Levels do not
// travel", and section 20).
func (s *Service) toWire(ctx context.Context, a store.Areas, st store.Stream, conv model.Conversation, e model.Event) (wireEvent, error) {
	msg, hasMsg := e.Payload["message"].(model.Message)
	switch e.Type {
	case model.EventMessageCreated, model.EventMessageEdited:
		if !hasMsg || !isLocal(ctx, a, msg.AuthorID) {
			return local(), nil
		}
		if st.MaxBlobBytes > 0 {
			for _, f := range msg.Files {
				if f.Size > st.MaxBlobBytes {
					return local(), model.ErrTooLarge.With("That file is larger than this shared channel carries.")
				}
			}
		}
		w, blobs, err := s.wireMessageOf(ctx, a, st, conv, msg)
		if err != nil {
			return local(), err
		}
		out := wireEvent{Type: e.Type, Fields: map[string]any{"message": w, "blobs": blobs}}
		if !e.At.IsZero() {
			out.Fields["at"] = e.At
		}
		if e.Type == model.EventMessageCreated {
			out.ID = string(msg.ID)
		}
		return out, nil
	case model.EventMessageDeleted:
		if !hasMsg {
			return local(), nil
		}
		authorLocal := isLocal(ctx, a, msg.AuthorID)
		if _, err := a.Messages().Get(ctx, msg.ID); errors.Is(err, model.ErrNotFound) && authorLocal {
			// Deleted inside the quick-correction window: no trace is
			// kept here, so the others are asked to keep none either.
			return wireEvent{Type: model.EventMessagePurged, Fields: map[string]any{"message_id": msg.ID, "erase": "content"}}, nil
		}
		switch {
		case authorLocal:
			return wireEvent{Type: model.EventMessageDeleted, Fields: map[string]any{"message_id": msg.ID, "deleted_by_id": msg.DeletedByID,
				"deleted_at": msg.DeletedAt}}, nil
		case st.Owned():
			// Only the owner server can remove a message for everyone.
			return wireEvent{Type: evMessageRemoved, Fields: map[string]any{"message_id": msg.ID, "removed_by_id": msg.DeletedByID,
				"removed_at": msg.DeletedAt}}, nil
		case s.keeps(ctx, a, st, msg.DeletedByID, s.id.Name):
			// In a channel a hub owns, a manager or moderator can: every
			// instance checks the account against the lists the hub
			// signed. Their
			// profile goes with it, for a server that has not met them.
			fields := map[string]any{"message_id": msg.ID, "removed_by_id": msg.DeletedByID, "removed_at": msg.DeletedAt}
			if acc, err := a.Accounts().Get(ctx, msg.DeletedByID); err == nil {
				fields["removed_by"], fields["blobs"] = s.profileOf(ctx, a, acc)
			}
			return wireEvent{Type: evMessageRemoved, Fields: fields}, nil
		}
		return local(), nil // hidden here, and only here
	case model.EventMessagePurged:
		if !hasMsg || !(isLocal(ctx, a, msg.AuthorID) || st.Owned()) {
			return local(), nil
		}
		erase := "history"
		if erased, _ := e.Payload["erased"].(bool); erased {
			erase = "content"
		}
		return wireEvent{Type: model.EventMessagePurged, Fields: map[string]any{"message_id": msg.ID, "erase": erase}}, nil
	case model.EventPreviewAdded, model.EventPreviewRemoved:
		if !hasMsg || !isLocal(ctx, a, msg.AuthorID) {
			return local(), nil
		}
		// Either way the others are given the message's link-preview
		// cards as they now are, which replace the ones they hold.
		var cards []model.Card
		var blobs []wireBlob
		for _, c := range msg.Cards {
			if c.PreviewURL == "" {
				continue
			}
			cards = append(cards, c)
			if c.ImageFileID != "" {
				if f, err := a.Files().Get(ctx, c.ImageFileID); err == nil {
					blobs = append(blobs, blobOf(f, "card"))
				}
			}
		}
		if len(cards) > 3 {
			cards = cards[:3]
		}
		return wireEvent{Type: model.EventPreviewAdded, Fields: map[string]any{"message_id": msg.ID, "cards": cards, "blobs": blobs}}, nil
	case model.EventReactionAdded, model.EventReactionRemoved:
		account, _ := e.Payload["account_id"].(model.AccountID)
		reaction, _ := e.Payload["reaction"].(model.Reaction)
		if account == "" || reaction.Emoji == "" || !isLocal(ctx, a, account) {
			return local(), nil
		}
		fields := map[string]any{"message_id": e.MessageID, "account_id": account, "emoji": reaction.Emoji}
		if name := strings.Trim(reaction.Emoji, ":"); strings.HasPrefix(reaction.Emoji, ":") && name != "" {
			server := s.id.Name
			if at := strings.IndexByte(name, '@'); at > 0 {
				name, server = name[:at], name[at+1:]
			}
			custom := map[string]any{"name": name, "server": server}
			if server == s.id.Name {
				if em, err := a.Emoji().ByName(ctx, name); err == nil {
					custom["id"] = em.ID
					if f, err := a.Files().Get(ctx, em.FileID); err == nil {
						b := blobOf(f, "emoji")
						b.ID, b.Name = string(em.ID), em.Name
						fields["blobs"] = []wireBlob{b}
					}
				}
			} else if sv, err := a.Peers().ServerByName(ctx, server); err == nil {
				if em, err := a.Streams().RemoteEmoji(ctx, sv.ID, name); err == nil {
					custom["id"] = em.ID
				}
			}
			fields["emoji"] = custom
		}
		return wireEvent{Type: e.Type, Fields: fields}, nil
	case model.EventTaskChanged:
		task, _ := e.Payload["task"].(model.Task)
		by := task.CheckedByID
		if by == "" {
			// An untick does not say who; the item's history does.
			if hist, err := a.Checklists().History(ctx, e.MessageID, task.ID); err == nil && len(hist) > 0 {
				by = hist[len(hist)-1].AccountID
				for _, h := range hist {
					if h.At >= hist[len(hist)-1].At {
						by = h.AccountID
					}
				}
			}
		}
		if by == "" || !isLocal(ctx, a, by) {
			return local(), nil
		}
		return wireEvent{Type: e.Type, Fields: map[string]any{"message_id": e.MessageID, "item_id": task.ID, "checked": task.Checked,
			"account_id": by}}, nil
	case model.EventMemberJoined:
		member, _ := e.Payload["member"].(model.Member)
		acc, err := a.Accounts().Get(ctx, member.AccountID)
		if err != nil || !acc.Local() {
			return local(), nil
		}
		p, blobs := s.profileOf(ctx, a, acc)
		return wireEvent{Type: e.Type, Fields: map[string]any{"account": p, "blobs": blobs, "joined_at": member.JoinedAt}}, nil
	case model.EventMemberLeft:
		account, _ := e.Payload["account_id"].(model.AccountID)
		reason, _ := e.Payload["reason"].(string)
		if account == "" || !isLocal(ctx, a, account) {
			return local(), nil
		}
		if reason != "removed" && reason != "account_removed" {
			reason = "left"
		}
		return wireEvent{Type: e.Type, Fields: map[string]any{"account_id": account, "reason": reason}}, nil
	case model.EventConversationUpdated:
		c, ok := e.Payload["conversation"].(model.Conversation)
		if !ok || !st.Owned() || st.Kind != store.StreamChannel {
			return local(), nil // each server names its own copy
		}
		s.mu.Lock()
		last, sent := s.lastNames[st.ID]
		s.lastNames[st.ID] = [2]string{c.Name, c.Topic}
		s.mu.Unlock()
		if sent && last == [2]string{c.Name, c.Topic} {
			return local(), nil // something else about the channel changed
		}
		return wireEvent{Type: e.Type, Fields: map[string]any{"name": c.Name, "topic": c.Topic}}, nil
	}
	if e.Type == model.EventSealedEpochStarted {
		// A device of one of this server's accounts made the chat's next
		// epoch record. Servers carry it and cannot open it.
		if row, ok := e.Payload["epoch"].(model.EpochRecord); ok && isLocal(ctx, a, row.AccountID) {
			return wireEvent{Type: e.Type, Fields: map[string]any{"key_epoch": row.KeyEpoch, "account_id": row.AccountID,
				"device_id": row.DeviceID, "record": wire.Bytes(row.Record)}}, nil
		}
		return local(), nil
	}
	if w, ok, err := s.formToWire(ctx, a, st, conv, e); ok || err != nil {
		return w, err
	}
	return local(), nil
}

// managersHeld refuses a change to who is a manager of a channel on the
// server that has agreed to its move onto a hub, from its agreement until
// the move is over. The managers it signed for become managers at the
// commit; they must still be the managers then (section 21a).
func (s *Service) managersHeld(ctx context.Context, a store.Areas, st store.Stream, events []model.Event) error {
	touches := false
	for _, e := range events {
		touches = touches || e.Type == model.EventMemberLevelChanged || e.Type == model.EventMemberLeft
	}
	if !touches || st.Kind != store.StreamChannel {
		return nil
	}
	mv, ok := s.openMove(ctx, a, st.ID)
	if !ok || outgoing(mv) || mv.State != store.MoveAccepted {
		return nil
	}
	var acc wire.MoveAccept
	if wire.PeekBytes(wire.BMoveAccept, mv.Accept, &acc) != nil {
		return nil
	}
	for _, e := range events {
		switch e.Type {
		case model.EventMemberLevelChanged:
			return errMoving
		case model.EventMemberLeft:
			who := fmt.Sprint(e.Payload["account_id"])
			if (wire.ChannelRecord{Managers: acc.Managers}).Manages(who, s.id.Name) {
				return errMoving
			}
		}
	}
	return nil
}
