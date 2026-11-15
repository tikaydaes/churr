package link

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/tikaydaes/churr/internal/chat"
	"github.com/tikaydaes/churr/internal/link/wire"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

// This file is the path of an envelope: stamped and made here (stamp, seal,
// emitOwn), taken into the log when this server is the stream's time
// reference (onSubmit, onCatchUp), and checked and applied wherever it
// arrives (onDeliver, open, settle). events.go decides what each event may
// do.
//
// Nobody numbers events (ruling R57). Every event carries a stamp its origin
// made and signed: `at`, the time of the stream's reference clock as the
// origin read it; `n`, the origin's counter; and the clock offset the origin
// used. A conversation is shown in the order of (at, origin, n) on every
// server, whatever order the events arrived in. What a reference keeps is a
// log of the envelopes it took, in the order it took them; a position in
// that log is a delivery cursor and orders nothing.

// The bounds of the clock checks (section 13).
const (
	// clockSlack is how far an event's `at` may be from the reference
	// clock, either way, without an explanation.
	clockSlack = 30 * 1000
	// offsetFloor is the least tolerance, in milliseconds, between the
	// offset an origin says it used and the one the reference measured,
	// and offsetScale how many times the reference's uncertainty about
	// that origin the tolerance is when that is more.
	offsetFloor = 100
	offsetScale = 3
	// awayKept is how many stretches of disconnection are remembered per
	// peer, and awayLife how long after its end one still excuses an old
	// `at`.
	awayKept = 8
	awayLife = 24 * time.Hour
	// submitBatch is the most envelopes one submit frame carries, and
	// inflightWait how long an unanswered one holds its stream back.
	submitBatch  = 32
	inflightWait = 10 * time.Second
)

// inner is the plaintext of an envelope: one event (section 11). Its place
// in the conversation is not in here but in the envelope's stamp.
type inner struct {
	Type string     `json:"type"`
	At   model.Time `json:"at"`
	raw  []byte
}

// referenceNow is this server's best reading of a stream's reference clock,
// in milliseconds, and how sure it is. known is false when the reference is
// another party that has never told this server its offset.
//
// Nothing here assumes that the party this server submits to and receives
// from is the reference: the offset is looked up for the reference, which
// today is always reached over the stream's own peer.
func (s *Service) referenceNow(ctx context.Context, st store.Stream) (now, offset, uncertainty int64, known bool) {
	if st.Sequencer == store.SequencerSelf {
		return s.nowMs(), 0, 0, true
	}
	c, ok := s.toldFor(ctx, st)
	if !ok {
		return s.nowMs(), 0, 0, false
	}
	return s.nowMs() - c.Offset, c.Offset, c.UncertaintyAt(s.now()), true
}

// referencePeer is the peer whose clock figures apply to a stream: the one
// that is, or leads to, the stream's time reference.
func (s *Service) referencePeer(st store.Stream) model.PeerID { return st.PeerID }

// errNoClock is why a server that has never been told its offset by a
// stream's reference cannot make events there.
var errNoClock = model.ErrUnavailable.With("This server has not yet been told its clock offset by the conversation's time reference. Try again in a moment.")

// stamp gives the next event this server makes in a stream its stamp, in
// the transaction that makes the event: `at` above everything the server
// has applied in the stream and above its own last, `n` one more than its
// last.
func (s *Service) stamp(ctx context.Context, a store.Areas, st store.Stream) (wire.Stamp, error) {
	now, offset, _, known := s.referenceNow(ctx, st)
	if !known {
		return wire.Stamp{}, errNoClock
	}
	at, n, err := a.Streams().Stamp(ctx, st.ID, s.self.ID, now)
	return wire.Stamp{At: at, N: n, Offset: offset}, err
}

// plainOf builds the plaintext of an event.
func (s *Service) plainOf(typ string, fields map[string]any) ([]byte, error) {
	body := map[string]any{}
	for key, v := range fields {
		body[key] = v
	}
	body["type"] = typ
	if _, has := body["at"]; !has {
		body["at"] = s.now()
	}
	text, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	// What is sealed is an Event message of the link's schema.
	plain := wire.EncodeEvent(text)
	if len(plain)+40 > wire.MaxEnvelopeBytes {
		return nil, model.ErrTooLarge.With("That is too large to send to the other servers.")
	}
	return plain, nil
}

// sealPlain builds this server's envelope around a plaintext. epoch 0 means
// the highest epoch a key is held for.
func (s *Service) sealPlain(ctx context.Context, a store.Areas, st store.Stream, eventID string, plain []byte, stamp wire.Stamp, epoch int64) (wire.Envelope, error) {
	var k streamKey
	var err error
	if epoch == 0 {
		k, err = s.sealingKey(ctx, a, st)
	} else {
		k, err = s.keyOf(ctx, a, st.ID, epoch)
	}
	if err != nil {
		return wire.Envelope{}, err
	}
	return wire.SealEnvelope(k.key, string(st.ID), k.epoch, s.id.Name, eventID, stamp, plain, s.keys)
}

// seal builds this server's envelope for an event.
func (s *Service) seal(ctx context.Context, a store.Areas, st store.Stream, eventID, typ string, fields map[string]any, stamp wire.Stamp, epoch int64) (wire.Envelope, error) {
	plain, err := s.plainOf(typ, fields)
	if err != nil {
		return wire.Envelope{}, err
	}
	return s.sealPlain(ctx, a, st, eventID, plain, stamp, epoch)
}

// errRejected ends the transaction of an event this server made itself and
// then found it may not make.
type errRejected struct{ rej *rejection }

func (e errRejected) Error() string { return "link: the event was not accepted: " + e.rej.reason }

// originate makes an event of this server's own (not one a member's action
// produced): it is stamped, applied here at once, and sent on its way. On a
// stream whose time reference is someone else it waits for that party to
// acknowledge it, and fails with `unavailable` if it does not; the event
// stays applied here and is delivered when the link allows.
func (s *Service) originate(ctx context.Context, st store.Stream, typ string, fields map[string]any, epoch int64) error {
	return s.emitOwn(ctx, st, typ, fields, epoch, true)
}

// enqueue is originate for events that need not wait to be acknowledged.
func (s *Service) enqueue(ctx context.Context, st store.Stream, typ string, fields map[string]any) error {
	return s.emitOwn(ctx, st, typ, fields, 0, false)
}

func (s *Service) emitOwn(ctx context.Context, st store.Stream, typ string, fields map[string]any, epoch int64, wait bool) error {
	return s.emitOwnID(ctx, st, newID(), typ, fields, epoch, wait)
}

// emitOwnID is emitOwn for an event whose origin event id the caller chose,
// so that it can tell afterwards whether the event has been acknowledged.
func (s *Service) emitOwnID(ctx context.Context, st store.Stream, id, typ string, fields map[string]any, epoch int64, wait bool) error {
	plain, err := s.plainOf(typ, fields)
	if err != nil {
		return err
	}
	var env wire.Envelope
	var cur store.Stream
	logged := false
	err = s.again(ctx, func() error {
		logged = false
		return s.chat.RemoteTx(ctx, func(tx store.Tx, rec *chat.Recorder) error {
			return s.emitIn(ctx, tx, rec, st.ID, id, typ, fields, plain, epoch, &cur, &env, &logged)
		})
	})
	var rejected errRejected
	if errors.As(err, &rejected) {
		s.noteRejected(ctx, st, env, rejected.rej)
		return model.ErrConflict.With("The event was not accepted: " + rejected.rej.reason + ".")
	}
	if err != nil {
		return err
	}
	return s.emitted(ctx, cur, env, id, logged, wait)
}

// transient reports whether a failed transaction is worth trying again: it
// failed for a reason of the database's (two transactions in each other's
// way), not because this server or another refused something.
func transient(err error) bool {
	if err == nil {
		return false
	}
	var refused *wire.Error
	var rejected errRejected
	if errors.As(err, &refused) || errors.As(err, &rejected) {
		return false
	}
	_, ours := model.AsError(err)
	return !ours
}

// again runs a transaction, and runs it again, up to three more times, if it
// failed for a passing reason.
func (s *Service) again(ctx context.Context, fn func() error) error {
	err := fn()
	for try := 0; transient(err) && ctx.Err() == nil && try < 3; try++ {
		time.Sleep(time.Duration(try+1) * 50 * time.Millisecond)
		err = fn()
	}
	return err
}

// emitIn is the transaction of emitOwn.
func (s *Service) emitIn(ctx context.Context, tx store.Tx, rec *chat.Recorder, stream model.StreamID, id, typ string, fields map[string]any, plain []byte, epoch int64, curOut *store.Stream, envOut *wire.Envelope, logged *bool) error {
	var err error
	var cur store.Stream
	var env wire.Envelope
	defer func() { *curOut, *envOut = cur, env }()
	if cur, err = tx.Streams().Get(ctx, stream); err != nil {
		return err
	}
	if cur.State != store.StreamActive {
		return model.ErrConflict.With("That stream is not active.")
	}
	// The event is applied before it is stamped. Applying locks what
	// the event changes and then the conversation; stamping locks the
	// stream's row. That is the order a member's own change takes them
	// in, and the order an event from another server is applied in, so
	// none of the three can wait on another. What applying needs of the
	// envelope is its key epoch, which is known before the stamp is.
	var k streamKey
	if epoch == 0 {
		k, err = s.sealingKey(ctx, tx, cur)
	} else {
		k, err = s.keyOf(ctx, tx, cur.ID, epoch)
	}
	if err != nil {
		return err
	}
	env = wire.Envelope{StreamID: string(cur.ID), KeyEpoch: k.epoch, Origin: s.id.Name, OriginEventID: id, At: s.nowMs()}
	origin := store.StreamServer{StreamID: cur.ID, ServerID: s.self.ID, Name: s.id.Name, State: store.StreamServerActive}
	// The event is applied here from what will be sealed, read back as
	// every other server will read it.
	text, err := wire.DecodeEvent(plain)
	if err != nil {
		return err
	}
	rej, err := s.applyEvent(ctx, tx, rec, cur, origin, inner{Type: typ, At: s.now(), raw: text}, env)
	if err != nil {
		return err
	}
	if rej != nil {
		// What this server would reject from another it does not make
		// itself.
		return errRejected{rej}
	}
	if err := s.lockConversation(ctx, tx, cur); err != nil {
		return err
	}
	// Read again now that the conversation is held: a move onto a hub may
	// have re-pointed the stream while this event was being applied, and
	// what is stamped and where it goes must then be the stream's as it
	// stands. Only a move changes a stream's peer, so that is the sign.
	// (What this event itself changed is not taken from here: an owner
	// that hands the stream over logs that event as its last act as the
	// time reference, which it was when it began.)
	if fresh, err := tx.Streams().Get(ctx, stream); err != nil {
		return err
	} else if fresh.PeerID != cur.PeerID {
		cur.PeerID, cur.Sequencer, cur.OwnerServerID, cur.HomeHub = fresh.PeerID, fresh.Sequencer, fresh.OwnerServerID, fresh.HomeHub
	}
	stamp, err := s.stamp(ctx, tx, cur)
	if err != nil {
		return err
	}
	switch typ {
	case evCallStarted:
		// The stamp is what settles two calls started at once (hooks.go).
		s.noteCallStart(cur.ConversationID, fmt.Sprint(fields["call_id"]), model.OrderString(stamp.At, s.id.Name, stamp.N), stamp.At)
	case model.EventSealedEpochStarted:
		// And two records made for one key epoch of a sealed chat. The
		// record was stored when the event was applied, before it had its
		// stamp.
		if epoch, ok := fields["key_epoch"].(int64); ok && cur.ConversationID != "" {
			if err := tx.Sealed().SetEpochOrder(ctx, cur.ConversationID, epoch, model.OrderString(stamp.At, s.id.Name, stamp.N)); err != nil {
				return err
			}
		}
	}
	if env, err = s.sealPlain(ctx, tx, cur, id, plain, stamp, k.epoch); err != nil {
		return err
	}
	if _, err := tx.Streams().MarkSeen(ctx, cur.ID, id, 0, s.now()); err != nil {
		return err
	}
	if cur.Sequencer == store.SequencerSelf {
		*logged = true
		env, err = s.appendLog(ctx, tx, cur.ID, env)
		return err
	}
	return s.putOutbox(ctx, tx, cur, id, typ, fields, stamp, epoch)
}

// emitted is what follows the transaction of emitOwn: the event goes on its
// way, and with wait this server waits for the reference to take it.
func (s *Service) emitted(ctx context.Context, cur store.Stream, env wire.Envelope, id string, logged, wait bool) error {
	if logged {
		s.deliverOut(ctx, cur, env)
		return nil
	}
	if !wait {
		s.kickStream(ctx, cur.ID, false)
		return nil
	}
	rt := s.rt(cur.ID)
	ch := make(chan struct{})
	rt.mu.Lock()
	rt.watchers[id] = ch
	rt.mu.Unlock()
	defer func() {
		rt.mu.Lock()
		delete(rt.watchers, id)
		rt.mu.Unlock()
	}()
	s.kickStream(ctx, cur.ID, false)
	t := time.NewTimer(2 * submitWait)
	defer t.Stop()
	select {
	case <-ch:
		return nil
	case <-t.C:
		return model.ErrUnavailable.With("The other servers cannot be reached right now. The change is made here and will reach them when the link returns.")
	case <-ctx.Done():
		return ctx.Err()
	}
}

// lockConversation locks the row of a stream's conversation, if it has one,
// before the stream's own row is written: the order every writer keeps
// (store.Streams.LockConversation says why it matters).
func (s *Service) lockConversation(ctx context.Context, tx store.Tx, st store.Stream) error {
	if st.ConversationID == "" {
		return nil
	}
	return tx.Streams().LockConversation(ctx, st.ConversationID)
}

// lockStream reads a stream inside a transaction that is about to change it
// and the conversation it is in. The conversation's row is locked first and
// the stream's second, the order every writer takes them in: a local change
// takes its seq, which locks the conversation, before its event is stamped.
func (s *Service) lockStream(ctx context.Context, tx store.Tx, id model.StreamID) (store.Stream, error) {
	st, err := tx.Streams().Get(ctx, id)
	if err != nil {
		return st, err
	}
	if err := s.lockConversation(ctx, tx, st); err != nil {
		return st, err
	}
	return tx.Streams().Update(ctx, id, func(*store.Stream) error { return nil })
}

// appendLog writes an envelope to the log of a stream this server is the
// time reference of, and returns it with its place in the log beside it.
func (s *Service) appendLog(ctx context.Context, a store.Areas, id model.StreamID, env wire.Envelope) (wire.Envelope, error) {
	env.Cursor, env.Prev = 0, 0
	row, err := a.Streams().AppendLog(ctx, id, store.LogRow{Origin: env.Origin, N: env.N, OriginEventID: env.OriginEventID,
		Envelope: mustEnvelope(env), At: s.now()})
	env.Cursor, env.Prev = row.Seq, row.Prev
	return env, err
}

// rejection is why an envelope was not applied (section 9).
type rejection struct {
	reason string
	// quiet: no audit line. An event about a message this server does not
	// hold, a sealed record that lost, an envelope from before it joined.
	quiet  bool
	detail map[string]any
	// forged: the event names an account of another server.
	forged bool
	// missing: the event is about a message this server does not hold. In
	// a stream that comes through a hub the event waits for it (hold.go).
	missing model.MessageID
}

func reject(reason string) *rejection      { return &rejection{reason: reason} }
func rejectQuiet(reason string) *rejection { return &rejection{reason: reason, quiet: true} }

// opened is an envelope after the checks that need no transaction: steps 3
// to 7 of section 9.
type opened struct {
	origin store.StreamServer
	in     inner
	rej    *rejection
}

// open checks an envelope's origin and signature and decrypts it. With wait
// it holds, for up to 60 seconds, an envelope whose key this server has not
// been given, and asks the owner for the key.
func (s *Service) open(ctx context.Context, st store.Stream, env wire.Envelope, wait bool) opened {
	var o opened
	servers, err := s.store.Streams().Servers(ctx, st.ID)
	if err != nil {
		o.rej = reject("internal")
		return o
	}
	found := false
	for _, ss := range servers {
		if ss.Name == env.Origin && ss.State == store.StreamServerActive {
			o.origin, found = ss, true
		}
	}
	if !found {
		o.rej = reject("origin_not_in_stream")
		return o
	}
	sv, err := s.store.Peers().Server(ctx, o.origin.ServerID)
	if err != nil || !env.Verify(sv.SignKey) {
		o.rej = reject("bad_signature")
		return o
	}
	if sv.Blocked() {
		o.rej = rejectQuiet("blocked")
		return o
	}
	k, err := s.keyOf(ctx, s.store, st.ID, env.KeyEpoch)
	if errors.Is(err, model.ErrNotFound) {
		keys, _ := s.keysOf(ctx, s.store, st.ID)
		if len(keys) == 0 && wait {
			// Just joined, and the key of the epoch this server joined at
			// is still on its way. Until it is here there is no telling
			// whether this envelope is from before then. In a hub-owned
			// channel the key may have been sent while this server was
			// not connected, and nobody keeps it for it: it asks.
			if hubOwned(st) {
				rt := s.rt(st.ID)
				rt.mu.Lock()
				already := rt.keyAsked[env.KeyEpoch]
				rt.keyAsked[env.KeyEpoch] = true
				rt.mu.Unlock()
				if !already {
					for _, holder := range s.keyHolders(ctx, st, env.KeyEpoch) {
						s.sendDirect(ctx, st.PeerID, holder, kKeyRequest, "", map[string]any{"stream_id": st.ID, "epoch": env.KeyEpoch})
					}
				}
			}
			keys = s.awaitFirstKey(ctx, st)
		}
		if len(keys) > 0 && env.KeyEpoch < keys[0].epoch {
			// From before this server joined: encrypted under a key it
			// was never given. What it is meant to have of it arrives
			// as history.
			o.rej = rejectQuiet("before_joining")
			return o
		}
		if wait {
			k, err = s.awaitKey(ctx, st, env.KeyEpoch)
		}
	}
	if err != nil {
		o.rej = reject("no_key")
		return o
	}
	plain, err := env.Open(k.key)
	if err != nil {
		o.rej = reject("undecryptable")
		return o
	}
	// The plaintext is an Event message; from here on the event is the
	// JSON object it stands for.
	if plain, err = wire.DecodeEvent(plain); err != nil || json.Unmarshal(plain, &o.in) != nil || o.in.Type == "" {
		o.rej = reject("malformed")
		return o
	}
	s.hear(env.Origin)
	o.in.raw = plain
	if wait {
		// Not when this server is taking the envelope into its log: that
		// runs on the connection's reader, which the answer would have to
		// come in on. There the list arrived first, on the same connection.
		s.beforeSealedEpoch(ctx, st, env, o.in)
	}
	return o
}

// awaitFirstKey waits, for up to 60 seconds, until this server holds any key
// of a stream at all, and returns the keys it then holds.
func (s *Service) awaitFirstKey(ctx context.Context, st store.Stream) []streamKey {
	rt := s.rt(st.ID)
	deadline := time.NewTimer(keyWait)
	defer deadline.Stop()
	for {
		rt.mu.Lock()
		arrived := rt.keyArrived
		rt.mu.Unlock()
		if keys, _ := s.keysOf(ctx, s.store, st.ID); len(keys) > 0 {
			return keys
		}
		select {
		case <-arrived:
		case <-deadline.C:
			return nil
		case <-ctx.Done():
			return nil
		}
	}
}

// awaitKey asks the owner for an epoch's key and waits up to 60 seconds for
// it to arrive.
func (s *Service) awaitKey(ctx context.Context, st store.Stream, epoch int64) (streamKey, error) {
	rt := s.rt(st.ID)
	deadline := time.NewTimer(keyWait)
	defer deadline.Stop()
	asked := false
	for {
		rt.mu.Lock()
		arrived := rt.keyArrived
		rt.mu.Unlock()
		if k, err := s.keyOf(ctx, s.store, st.ID, epoch); err == nil {
			return k, nil
		}
		if !asked {
			// The owner; in a hub-owned channel the server the hub named to
			// make that key, and then the others, any of which may hold it.
			asked = true
			rt.mu.Lock()
			rt.keyAsked[epoch] = true
			rt.mu.Unlock()
			for _, holder := range s.keyHolders(ctx, st, epoch) {
				s.sendDirect(ctx, st.PeerID, holder, kKeyRequest, "", map[string]any{"stream_id": st.ID, "epoch": epoch})
			}
		}
		select {
		case <-arrived:
		case <-deadline.C:
			return streamKey{}, model.ErrNotFound
		case <-ctx.Done():
			return streamKey{}, ctx.Err()
		}
	}
}

// settle finishes an envelope inside the transaction that applies it:
// duplicates, the clock (an event from the future is refused by everyone who
// can read the reference clock, whoever let it through), the event itself,
// and the note that it has been applied. The caller moves the origin's
// counter.
func (s *Service) settle(ctx context.Context, tx store.Tx, rec *chat.Recorder, st store.Stream, env wire.Envelope, o opened) (*rejection, error) {
	if o.rej != nil {
		return o.rej, nil
	}
	if _, dup, err := tx.Streams().Seen(ctx, st.ID, env.OriginEventID); err != nil {
		return nil, err
	} else if dup {
		return rejectQuiet("duplicate"), nil
	}
	if now, _, uncertainty, known := s.referenceNow(ctx, st); known {
		if env.At > now+clockSlack+uncertainty {
			return &rejection{reason: "clock_ahead", detail: map[string]any{"at": model.Time(env.At), "reference": model.Time(now)}}, nil
		}
		if now-env.At > int64(eventMaxAge/time.Millisecond) {
			return reject("too_old"), nil
		}
	}
	rej, err := s.applyEvent(ctx, tx, rec, st, o.origin, o.in, env)
	if err != nil || rej != nil {
		return rej, err
	}
	_, err = tx.Streams().MarkSeen(ctx, st.ID, env.OriginEventID, 0, s.now())
	return nil, err
}

// noteRejected counts a rejection and writes its audit line.
func (s *Service) noteRejected(ctx context.Context, st store.Stream, env wire.Envelope, rej *rejection) {
	s.count(func(stats *Stats) { stats.Rejected[rej.reason]++ })
	if rej.forged {
		rt := s.rt(st.ID)
		hour := int64(s.now()) / int64(time.Hour/time.Millisecond)
		rt.mu.Lock()
		if rt.forgedHour != hour {
			rt.forgedHour, rt.forged = hour, map[string]int{}
		}
		rt.forged[env.Origin]++
		warn := rt.forged[env.Origin] == 100
		rt.mu.Unlock()
		if warn {
			s.audit(ctx, s.store, "", "link.forgery_warning", "stream", string(st.ID), map[string]any{"origin": env.Origin})
		}
	}
	if !rej.quiet {
		s.auditRejected(ctx, st, env.Origin, rej.reason, rej.detail)
	}
}

// apply runs an envelope's transaction: the event is settled, and then is
// done in the same transaction.
// A failure that passes (two transactions in each other's way) is tried
// again before the event is given up.
func (s *Service) apply(ctx context.Context, id model.StreamID, env wire.Envelope, o opened, seen **rejection, then func(tx store.Tx, cur store.Stream) error) (*rejection, error) {
	var rej *rejection
	run := func() error {
		return s.chat.RemoteTx(ctx, func(tx store.Tx, rec *chat.Recorder) error {
			// Nothing is locked here. The event locks what it changes as
			// it changes it, the conversation when it takes its seq, and
			// the stream's row last (then): the order a local change takes
			// them in, so the two cannot wait on each other.
			cur, err := tx.Streams().Get(ctx, id)
			if err != nil {
				return err
			}
			if rej, err = s.settle(ctx, tx, rec, cur, env, o); err != nil {
				return err
			}
			if seen != nil {
				*seen = rej // then may want to know
			}
			if err := s.lockConversation(ctx, tx, cur); err != nil {
				return err
			}
			return then(tx, cur)
		})
	}
	err := run()
	var refused *wire.Error
	for try := 0; err != nil && !errors.As(err, &refused) && ctx.Err() == nil && try < 3; try++ {
		time.Sleep(time.Duration(try+1) * 50 * time.Millisecond)
		err = run()
	}
	if err != nil && !errors.As(err, &refused) && ctx.Err() == nil && o.in.Type == evMoved {
		// Not this one. stream.moved is the stream changing hands: if it
		// cannot be applied now it does not count as held, and comes again.
		return rej, err
	}
	if err != nil && !errors.As(err, &refused) && ctx.Err() == nil {
		// The event could not be applied here. The stream goes on all the
		// same: the envelope counts as held, and nothing of it is stored.
		s.log.Warn("link: an event could not be applied", "stream", id, "origin", env.Origin, "n", env.N, "type", o.in.Type, "err", err)
		rej = reject("internal")
		if seen != nil {
			*seen = rej
		}
		err = s.chat.RemoteTx(ctx, func(tx store.Tx, rec *chat.Recorder) error {
			cur, err := tx.Streams().Get(ctx, id)
			if err != nil {
				return err
			}
			if err := s.lockConversation(ctx, tx, cur); err != nil {
				return err
			}
			return then(tx, cur)
		})
	}
	return rej, err
}

// ----------------------------------------------- this server as reference

// other returns the connection to the other server of a stream this server
// is the reference of, if that server is in the stream and connected.
func (s *Service) other(ctx context.Context, st store.Stream) *peerConn {
	pc := s.connOf(st.PeerID)
	if pc == nil || pc.hub {
		return nil
	}
	servers, err := s.store.Streams().Servers(ctx, st.ID)
	if err != nil {
		return nil
	}
	for _, ss := range servers {
		if ss.ServerID == pc.peer.ServerID && ss.State == store.StreamServerActive {
			return pc
		}
	}
	return nil
}

// deliverOut sends an envelope from this server's log to the other server of
// the stream. A server that is not connected, or has a thousand envelopes
// unacknowledged, gets it from the log later.
func (s *Service) deliverOut(ctx context.Context, st store.Stream, env wire.Envelope) {
	pc := s.other(ctx, st)
	if pc == nil || env.Origin == pc.name {
		return // nobody to tell, or its own
	}
	rt := s.rt(st.ID)
	rt.send.Lock()
	defer rt.send.Unlock()
	rt.mu.Lock()
	if rt.stalled || (rt.acked > 0 && env.Cursor-rt.acked > maxInFlight) {
		rt.stalled = true
		rt.mu.Unlock()
		return
	}
	if env.Cursor > rt.sent {
		rt.sent = env.Cursor
	}
	rt.mu.Unlock()
	pc.conn.Send(wire.Frame{Frame: wire.FDeliver, Envelope: &env})
}

// deliverRow sends one row of the log, with its place in the log beside it.
// prev replaces the row's own note of the origin's envelope before it when
// it is not negative.
func (s *Service) deliverRow(pc *peerConn, row store.LogRow, prev int64) {
	env, err := wire.UnmarshalEnvelope(row.Envelope)
	if err != nil {
		return
	}
	env.Cursor, env.Prev = row.Seq, row.Prev
	if prev >= 0 {
		env.Prev = prev
	}
	pc.conn.Send(wire.Frame{Frame: wire.FDeliver, Envelope: &env})
}

// envelopesOf returns the envelopes a submit or deliver frame carries: the
// one, or a batch, which may be of several streams.
func envelopesOf(f wire.Frame) []wire.Envelope {
	var out []wire.Envelope
	if f.Envelope != nil {
		out = append(out, *f.Envelope)
	}
	return append(out, f.Envelopes...)
}

// onSubmit is the reference's side of submit (section 6). Each envelope is
// taken into the log or refused; one frame answers, naming what was taken
// and the first envelope that was not. After a refusal nothing further of
// that stream is taken from the frame, so an origin's events never overtake
// one another.
func (s *Service) onSubmit(ctx context.Context, pc *peerConn, f wire.Frame) error {
	envs := envelopesOf(f)
	if len(envs) == 0 {
		return wire.Errf(wire.EBadFrame, "A submit carries at least one envelope.")
	}
	reply := wire.Frame{Frame: wire.FAccepted, Re: f.ID}
	stopped := map[string]bool{}
	for _, env := range envs {
		if stopped[env.StreamID] {
			continue
		}
		err := s.takeSubmitted(ctx, pc, env)
		if err == nil {
			reply.IDs = append(reply.IDs, env.OriginEventID)
			continue
		}
		stopped[env.StreamID] = true
		e := wire.AsError(err)
		if e.Code == wire.EInternal {
			s.log.Warn("link: a submitted envelope could not be taken", "peer", pc.name, "stream", env.StreamID, "err", err)
		}
		s.count(func(st *Stats) { st.Refused[e.Code]++ })
		if reply.Code == "" {
			reply.Code, reply.Message, reply.RetryAfterSeconds = e.Code, e.Message, e.RetryAfter
			reply.StreamID, reply.OriginEventID, reply.Clock = env.StreamID, env.OriginEventID, e.Clock
		}
	}
	return pc.conn.Send(reply)
}

// checkStamp is what a time reference checks of a submitted envelope's
// stamp, against what it has itself measured of the origin's clock:
//
//   - the offset the origin used is the one the reference measured, within a
//     tolerance that grows with the reference's own uncertainty about that
//     origin and is never less than a tenth of a second;
//   - `at` is not more than 30 seconds ahead of the reference clock;
//   - `at` is not more than 30 seconds behind it, unless it falls in a
//     stretch the origin was disconnected for.
//
// A refusal for the offset carries the reference's figures, so the origin
// can stamp again at once.
func (s *Service) checkStamp(ctx context.Context, peer model.PeerID, env wire.Envelope) *wire.Error {
	c, ok := s.measuredOf(ctx, peer)
	if !ok {
		return wire.Errf(wire.EClockUnknown, "This server has not measured %s's clock yet.", env.Origin)
	}
	now := s.nowMs()
	tolerance := offsetScale * c.UncertaintyAt(s.now())
	if tolerance < offsetFloor {
		tolerance = offsetFloor
	}
	if d := env.Offset - c.Offset; d > tolerance || d < -tolerance {
		e := wire.Errf(wire.EClockOffset, "The envelope was stamped with a clock offset of %d ms; this server measures %d ms for %s, give or take %d.",
			env.Offset, c.Offset, env.Origin, tolerance)
		e.Clock = &c
		return e
	}
	if env.At > now+clockSlack {
		e := wire.Errf(wire.EClockAhead, "The envelope's time is %d ms ahead of the reference clock.", env.At-now)
		e.Clock = &c
		return e
	}
	if env.At < now-clockSlack && !s.wasAway(ctx, peer, env.At) {
		e := wire.Errf(wire.EClockBehind, "The envelope's time is %d ms behind the reference clock, and %s was connected then.", now-env.At, env.Origin)
		e.Clock = &c
		return e
	}
	return nil
}

// takeSubmitted checks one submitted envelope and takes it into the log.
func (s *Service) takeSubmitted(ctx context.Context, pc *peerConn, env wire.Envelope) error {
	refuse := func(e *wire.Error) error {
		e.StreamID, e.OriginEventID = env.StreamID, env.OriginEventID
		return e
	}
	if !env.WellFormed() || env.Cursor != 0 || env.N <= 0 || env.At <= 0 {
		return refuse(wire.Errf(wire.EBadFrame, "A submitted envelope is whole, stamped, and carries no cursor."))
	}
	if len(env.Ciphertext) > wire.MaxEnvelopeBytes {
		return refuse(wire.Errf(wire.ETooLarge, "An envelope holds at most %d bytes of ciphertext.", wire.MaxEnvelopeBytes))
	}
	if env.Origin != pc.name {
		return refuse(wire.Errf(wire.ENotInStream, "An envelope is submitted by its origin, and %s is not this connection's server.", env.Origin))
	}
	st, err := s.store.Streams().Get(ctx, model.StreamID(env.StreamID))
	if err != nil || st.PeerID != pc.peer.ID || st.Sequencer != store.SequencerSelf || st.State != store.StreamActive {
		return refuse(wire.Errf(wire.EUnknownStream, "No such stream here."))
	}
	if s.other(ctx, st) == nil {
		return refuse(wire.Errf(wire.ENotInStream, "%s is not in that stream.", pc.name))
	}
	sv, err := s.store.Peers().Server(ctx, pc.peer.ServerID)
	if err != nil {
		return err
	}
	if !env.Verify(sv.SignKey) {
		return refuse(wire.Errf(wire.EBadSignature, "The envelope's signature does not verify."))
	}
	if has, err := s.store.Streams().LogHas(ctx, st.ID, env.OriginEventID); err != nil {
		return err
	} else if has {
		return nil // taken before: acknowledged again
	}
	if r := s.limits.submit.Take(string(pc.peer.ID)); !r.OK {
		e := wire.Errf(wire.ERateLimited, "Too many envelopes.")
		e.RetryAfter = r.RetryAfterSeconds()
		s.refusing.Note("envelopes", pc.name, e.RetryAfter)
		return refuse(e)
	}
	// The key epoch is the owner's to move, and this server is the owner.
	switch {
	case env.KeyEpoch < st.KeyEpoch:
		return refuse(wire.Errf(wire.EStaleEpoch, "Key epoch %d has ended; the stream is at %d.", env.KeyEpoch, st.KeyEpoch))
	case env.KeyEpoch > st.KeyEpoch:
		return refuse(wire.Errf(wire.EEarlyEpoch, "Key epoch %d has not started; the stream is at %d.", env.KeyEpoch, st.KeyEpoch))
	}
	if e := s.checkStamp(ctx, pc.peer.ID, env); e != nil {
		return refuse(e)
	}
	o := s.open(ctx, st, env, false)
	rej, err := s.apply(ctx, st.ID, env, o, nil, func(tx store.Tx, cur store.Stream) error {
		// Read now, with the conversation held: if a move onto a hub
		// committed while this envelope was being checked, this server is
		// no longer the stream's time reference and takes nothing more
		// into the log, which ended with stream.moved.
		if fresh, err := tx.Streams().Get(ctx, cur.ID); err != nil {
			return err
		} else {
			cur = fresh
		}
		if cur.State != store.StreamActive || cur.Sequencer != store.SequencerSelf || cur.PeerID != pc.peer.ID {
			return wire.Errf(wire.EUnknownStream, "This server is not that stream's time reference.")
		}
		// An origin's `at` and `n` only ever increase.
		servers, err := tx.Streams().Servers(ctx, cur.ID)
		if err != nil {
			return err
		}
		for _, ss := range servers {
			if ss.ServerID == pc.peer.ServerID && (env.N <= ss.LastN || env.At <= ss.LastAt) {
				return wire.Errf(wire.ENotMonotonic, "After n %d at %d, n %d at %d is not later.", ss.LastN, ss.LastAt, env.N, env.At)
			}
		}
		if _, err := s.appendLog(ctx, tx, cur.ID, env); err != nil {
			return err
		}
		return tx.Streams().Applied(ctx, cur.ID, pc.peer.ServerID, env.N, env.At)
	})
	var refused *wire.Error
	if errors.As(err, &refused) {
		return refuse(refused)
	}
	if err != nil {
		return err
	}
	if rej != nil {
		s.noteRejected(ctx, st, env, rej)
	}
	return nil
}

// haveOf returns, per server in a stream's list, the counter this server
// holds everything up to: what it says when it asks to catch up.
func (s *Service) haveOf(ctx context.Context, a store.Areas, id model.StreamID) (map[string]int64, error) {
	servers, err := a.Streams().Servers(ctx, id)
	if err != nil {
		return nil, err
	}
	have := map[string]int64{}
	for _, ss := range servers {
		if ss.State != store.StreamServerInvited {
			have[ss.Name] = ss.LastN
		}
	}
	return have, nil
}

// loggedOf returns, per server in a stream's list, the counter of its last
// envelope in the log this server keeps for the stream: what the log holds,
// which is where a server that joins starts from.
func (s *Service) loggedOf(ctx context.Context, a store.Areas, id model.StreamID) (map[string]int64, error) {
	servers, err := a.Streams().Servers(ctx, id)
	if err != nil {
		return nil, err
	}
	have := map[string]int64{}
	for _, ss := range servers {
		if ss.State != store.StreamServerInvited {
			have[ss.Name] = ss.LoggedN
		}
	}
	return have, nil
}

// onCatchUp serves the log of a stream this server is the reference of: for
// each origin, everything above the counter the asker says it holds, in log
// order; then what the log itself holds and how far back it reaches.
func (s *Service) onCatchUp(ctx context.Context, pc *peerConn, f wire.Frame) error {
	st, err := s.store.Streams().Get(ctx, model.StreamID(f.StreamID))
	if err != nil {
		return wire.Errf(wire.EUnknownStream, "No such stream here.")
	}
	// A stream this server has moved onto a hub: the log of its direct
	// life, which ends with stream.moved, is still served to the other
	// server over the direct link, for as long as it is kept.
	_, moved := s.movedFrom(ctx, st.ID, pc.peer.ID)
	if !moved && (st.PeerID != pc.peer.ID || st.Sequencer != store.SequencerSelf || st.State != store.StreamActive) {
		return wire.Errf(wire.EUnknownStream, "No such stream here.")
	}
	if !moved && s.other(ctx, st) == nil {
		// The log is served only to servers in the stream's list.
		return wire.Errf(wire.ENotInStream, "%s is not in that stream.", pc.name)
	}
	have, err := s.loggedOf(ctx, s.store, st.ID)
	if err != nil {
		return err
	}
	rt := s.rt(st.ID)
	// Nothing new leaves for this stream while the log is replayed, so the
	// asker sees one origin's envelopes in the order of the log.
	rt.send.Lock()
	defer rt.send.Unlock()
	var rows []store.LogRow
	first := map[int64]int64{} // log position to the counter the asker said it held, for each origin's first row
	for origin := range have {
		if origin == pc.name {
			continue // its own: it submits again whatever was not acknowledged
		}
		after := f.Have[origin]
		for {
			page, err := s.store.Streams().LogSince(ctx, st.ID, origin, after, 200)
			if err != nil {
				return err
			}
			for i, row := range page {
				if after == f.Have[origin] && i == 0 {
					first[row.Seq] = after
				}
				rows = append(rows, row)
			}
			if len(page) < 200 {
				break
			}
			after = page[len(page)-1].N
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Seq < rows[j].Seq })
	top := int64(0)
	for _, row := range rows {
		prev := int64(-1)
		if p, is := first[row.Seq]; is {
			// "This is the next one this log has after what you hold."
			prev = p
		}
		s.deliverRow(pc, row, prev)
		top = row.Seq
	}
	rt.mu.Lock()
	rt.stalled = false
	if top > rt.sent {
		rt.sent = top
	}
	rt.mu.Unlock()
	floor, err := s.store.Streams().LogFloor(ctx, st.ID)
	if err != nil {
		return err
	}
	for origin, n := range have {
		if _, in := floor[origin]; !in {
			floor[origin] = n // nothing of it is left in the log
		}
	}
	return pc.conn.Send(wire.Frame{Frame: wire.FCaughtUp, Re: f.ID, StreamID: f.StreamID, Have: have, Floor: floor})
}

// onAck notes how far the other server has stored what this server's log
// delivered, and resumes sending if it had stopped at the in-flight limit.
func (s *Service) onAck(ctx context.Context, pc *peerConn, f wire.Frame) {
	st, err := s.store.Streams().Get(ctx, model.StreamID(f.StreamID))
	if err != nil {
		return
	}
	if mv, moved := s.movedFrom(ctx, st.ID, pc.peer.ID); moved {
		s.movedAcked(ctx, mv, f.Cursor)
		return
	}
	if st.PeerID != pc.peer.ID || st.Sequencer != store.SequencerSelf {
		return
	}
	rt := s.rt(st.ID)
	rt.send.Lock()
	defer rt.send.Unlock()
	rt.mu.Lock()
	if f.Cursor > rt.acked {
		rt.acked = f.Cursor
	}
	resume := rt.stalled
	rt.stalled = false
	from := rt.sent
	rt.mu.Unlock()
	if !resume {
		return
	}
	for {
		rows, err := s.store.Streams().Log(ctx, st.ID, from, 200)
		if err != nil || len(rows) == 0 {
			return
		}
		for _, row := range rows {
			if row.Origin != pc.name {
				s.deliverRow(pc, row, -1)
			}
			from = row.Seq
		}
		rt.mu.Lock()
		rt.sent = from
		rt.mu.Unlock()
	}
}

// ------------------------------------------------------------- receiving

// onDeliver queues delivered envelopes, each for its own stream. A stream's
// one goroutine applies them as they come, so the connection's reader is
// free to bring the key or the answer an envelope may be waiting for.
func (s *Service) onDeliver(ctx context.Context, pc *peerConn, f wire.Frame) {
	peer := pc.peer.ID
	for _, env := range envelopesOf(f) {
		if !env.WellFormed() || env.N <= 0 {
			s.count(func(st *Stats) { st.Dropped["deliver_malformed"]++ })
			continue
		}
		id := model.StreamID(env.StreamID)
		s.count(func(st *Stats) { st.Delivered[id]++ })
		env := env
		select {
		case s.rt(id).jobs <- func() { s.delivered(s.ctx, peer, env) }:
		default:
			// The queue is full; the envelope comes again with catch_up.
			s.count(func(st *Stats) { st.Dropped["deliver_overflow"]++ })
		}
	}
}

// delivered takes one envelope from whoever delivers a stream to this
// server. Arrival order decides nothing about where the event is shown, so
// envelopes are applied as they come. The one thing watched is that nothing
// of an origin is missed: each delivery names the counter of the same
// origin's envelope before it in the deliverer's log (prev), and if this
// server holds less than that from the origin, it has a gap, drops the
// envelope and asks to catch up, which brings both.
func (s *Service) delivered(ctx context.Context, peer model.PeerID, env wire.Envelope) {
	st, err := s.store.Streams().Get(ctx, model.StreamID(env.StreamID))
	if err != nil || st.State != store.StreamActive || st.PeerID != peer || st.Sequencer == store.SequencerSelf {
		s.count(func(stats *Stats) { stats.Dropped["deliver_not_in_stream"]++ })
		if err == nil && st.State == store.StreamActive {
			// The direct log of a stream that has since moved onto a hub,
			// sent again: this server has it all, and says so, which is how
			// the server that moved the stream knows the move is done.
			if list, err := s.store.Streams().Moves(ctx, st.ID); err == nil {
				for _, mv := range list {
					if mv.FromPeerID == peer && mv.State == store.MoveDone && !outgoing(mv) {
						if pc := s.connOf(peer); pc != nil {
							pc.conn.Send(wire.Frame{Frame: wire.FAck, StreamID: string(st.ID), Cursor: env.Cursor})
						}
						break
					}
				}
			}
		}
		return
	}
	rt := s.rt(st.ID)
	rt.mu.Lock()
	wait := rt.historyWait
	rt.mu.Unlock()
	if wait != nil {
		// Just joined: the snapshot that names the members comes first.
		select {
		case <-wait:
		case <-time.After(10 * time.Second):
			rt.mu.Lock()
			rt.historyWait = nil
			rt.mu.Unlock()
		case <-ctx.Done():
			return
		}
	}
	if env.Origin == s.id.Name {
		// This server's own, sent back. It was applied when it was made.
		s.ackSoon(st.ID, peer, env.Cursor)
		return
	}
	held := int64(0)
	known := false
	servers, err := s.store.Streams().Servers(ctx, st.ID)
	if err != nil {
		return
	}
	for _, ss := range servers {
		if ss.Name == env.Origin {
			held, known = ss.LastN, true
		}
	}
	if known && env.N <= held {
		// Not above this server's counter of the origin: most likely sent
		// before. A log served again after a reconnection, or to fill a
		// gap, sends what was taken the first time, and that must cost
		// little: it is recognised by its event id here, before the key is
		// looked up and before any transaction. (The counter alone does not
		// say so: a deliverer may hand one origin's envelopes over in any
		// order, and each is judged for itself.)
		if _, dup, err := s.store.Streams().Seen(ctx, st.ID, env.OriginEventID); err == nil && dup {
			s.noteRejected(ctx, st, env, rejectQuiet("duplicate"))
			s.ackSoon(st.ID, peer, env.Cursor)
			return
		}
	}
	if h := rt.hold; h != nil {
		if h.has(env.OriginEventID) {
			return // waiting here for the message it refers to
		}
		// While something of an origin is held, the counter in the database
		// stays below it; what has been delivered since is known in memory.
		if v := h.seen[env.Origin]; v > held {
			held = v
		}
	}
	if known && env.Prev > held {
		// Something of this origin is missing between what is held and
		// this. Ask for it; this one comes again with it.
		s.count(func(stats *Stats) { stats.Gaps++ })
		s.log.Debug("link: a delivery shows a gap", "stream", st.ID, "origin", env.Origin, "n", env.N, "prev", env.Prev, "held", held)
		s.catchUp(ctx, st, false)
		return
	}
	o := s.open(ctx, st, env, true)
	if o.rej != nil && o.rej.reason == "before_joining" && st.Sequencer == store.SequencerHub {
		// Under a key from before this server was let in, and delivered to
		// it all the same: its origin had not heard of the new epoch when it
		// made it. A server that holds that key is asked to open it.
		if !s.askOpen(ctx, st, env, &o) {
			s.unreadable(st, env)
		}
	}
	if s.settleDelivered(ctx, rt, st, peer, env, o, false, time.Time{}) {
		// A message that has just arrived may be the one held events are
		// waiting for.
		s.retryHeld(ctx, rt)
	}
}

// settleDelivered applies a delivered envelope and moves this server's
// counter of its origin, or puts the envelope in the stream's hold if it is
// about a message that has not arrived (hold.go). released says the envelope
// has just come out of the hold; until is then when its time there ends. It
// reports whether the event was applied.
func (s *Service) settleDelivered(ctx context.Context, rt *streamRT, st store.Stream, peer model.PeerID, env wire.Envelope, o opened, released bool, until time.Time) bool {
	var rej *rejection
	parked := false
	rej, err := s.apply(ctx, st.ID, env, o, &rej, func(tx store.Tx, cur store.Stream) error {
		parked = false
		if s.mayHold(cur, rej) && (!released || time.Now().Before(until)) {
			// Nothing is written and the counter does not move: after a
			// restart the envelope is delivered again.
			parked = true
			return nil
		}
		if o.origin.ServerID != "" {
			// Held either way, so the counter moves. Its time counts only
			// if the event was applied: a time this server would not
			// accept must not push its own stamps forward.
			at := env.At
			if rej != nil {
				at = 0
			}
			// (Not past an event of the same origin that is waiting in the
			// hold: the counter says "everything up to here is settled".)
			n := env.N
			if released {
				if low := rt.hold.lowest(env.Origin); low != 0 {
					n = low - 1
				} else if top := rt.hold.seen[env.Origin]; top > n {
					n = top
				}
			} else {
				n, _ = rt.hold.counterFor(env.Origin, env.N)
			}
			if err := tx.Streams().Applied(ctx, cur.ID, o.origin.ServerID, n, at); err != nil {
				return err
			}
		}
		_, err := tx.Streams().Update(ctx, cur.ID, func(x *store.Stream) error {
			// (Not if this very event moved the stream to another peer:
			// the cursor is of the log it has just left.)
			if env.Cursor > x.LastSeq && x.PeerID == peer {
				x.LastSeq = env.Cursor
			}
			return nil
		})
		return err
	})
	if err != nil {
		s.log.Warn("link: an envelope could not be settled", "stream", st.ID, "origin", env.Origin, "n", env.N, "err", err)
		return false
	}
	if parked {
		s.park(ctx, rt, st, peer, env, o, rej)
		if released {
			rt.hold.events[len(rt.hold.events)-1].until = until
		}
		return false
	}
	if released && rt.hold.lowest(env.Origin) == 0 {
		delete(rt.hold.seen, env.Origin)
	}
	if rej != nil {
		s.log.Debug("link: a delivered event was not applied", "stream", st.ID, "origin", env.Origin, "n", env.N, "type", o.in.Type, "why", rej.reason)
		s.noteRejected(ctx, st, env, rej)
	}
	s.ackSoon(st.ID, peer, env.Cursor)
	return rej == nil
}

// catchUp asks whoever delivers a stream for what this server is missing,
// saying per origin the counter it holds everything up to. With again it
// asks even if an earlier request has not been answered: a new connection.
func (s *Service) catchUp(ctx context.Context, st store.Stream, again bool) {
	rt := s.rt(st.ID)
	rt.mu.Lock()
	// (An answer that has not come in catchWait is not coming: the request
	// or the answer was lost with a connection. Asking again costs a
	// replay; never asking again would stop the stream here.)
	if rt.catching && !again && time.Since(rt.catchingAt) < catchWait {
		rt.mu.Unlock()
		return
	}
	rt.catching, rt.catchingAt = true, time.Now()
	rt.mu.Unlock()
	pc := s.connOf(st.PeerID)
	servers, err := s.store.Streams().Servers(ctx, st.ID)
	if pc == nil || err != nil {
		rt.mu.Lock()
		rt.catching = false
		rt.mu.Unlock()
		return
	}
	have, at := map[string]int64{}, map[string]int64{}
	for _, ss := range servers {
		if ss.State != store.StreamServerInvited {
			have[ss.Name], at[ss.Name] = ss.LastN, ss.LastAt
		}
	}
	id := pc.conn.NextID()
	rt.mu.Lock()
	rt.asked, rt.askedAt = have, at
	rt.catchingID, rt.catchingPeer = id, st.PeerID
	rt.mu.Unlock()
	pc.conn.Send(wire.Frame{Frame: wire.FCatchUp, ID: id, StreamID: string(st.ID), Have: have})
}

// catchWait is how long a catch_up is waited for before another may be sent
// on the same connection.
const catchWait = 10 * time.Second

// catchUpRefused takes an error frame that answers a catch_up: the request
// is over, and the next gap may ask again.
func (s *Service) catchUpRefused(peer model.PeerID, re int64) {
	if re == 0 {
		return
	}
	s.mu.Lock()
	rts := make([]*streamRT, 0, len(s.rts))
	for _, rt := range s.rts {
		rts = append(rts, rt)
	}
	s.mu.Unlock()
	for _, rt := range rts {
		rt.mu.Lock()
		if rt.catching && rt.catchingID == re && rt.catchingPeer == peer {
			rt.catching = false
		}
		rt.mu.Unlock()
	}
}

// onCaughtUp ends a catch-up. The deliverer has sent everything its log
// holds above what this server said it had. If the log no longer reaches
// back that far for some origin, what was missed is asked of the owner as
// history (section 13), and the counters move up to where the log begins,
// so that what the log does hold is taken.
func (s *Service) onCaughtUp(ctx context.Context, pc *peerConn, f wire.Frame) {
	id := model.StreamID(f.StreamID)
	rt := s.rt(id)
	peer := pc.peer.ID
	rt.jobs <- func() {
		ctx := s.ctx
		st, err := s.store.Streams().Get(ctx, id)
		if err != nil || st.State != store.StreamActive || st.PeerID != peer {
			return
		}
		rt.mu.Lock()
		rt.catching = false
		asked, askedAt := rt.asked, rt.askedAt
		rt.mu.Unlock()
		servers, err := s.store.Streams().Servers(ctx, id)
		if err != nil {
			return
		}
		// What the log could not give is measured against what this server
		// held when it asked: by now it holds what the log did give.
		since, lost := int64(0), false
		for _, ss := range servers {
			held, was := asked[ss.Name]
			if s.isSelf(ss.ServerID) || ss.State == store.StreamServerInvited || !was {
				continue
			}
			floor := f.Floor[ss.Name]
			if floor <= held {
				continue
			}
			// Part of what this origin made is gone from the log.
			if !lost || askedAt[ss.Name] < since {
				since, lost = askedAt[ss.Name], true
			}
			if floor > ss.LastN {
				// Nothing of it is left there at all. The counter moves to
				// where the log stands, or nothing later would be taken.
				s.store.Streams().Applied(ctx, id, ss.ServerID, floor, 0)
			}
			s.audit(ctx, s.store, "", "link.log_gap", "stream", string(id), map[string]any{"origin": ss.Name, "held": held, "log_begins_after": floor})
		}
		if lost {
			go s.recoverSince(ctx, st, since)
		}
	}
}

// ackSoon sends ack for a stream, at most once a second, naming the last
// position of the deliverer's log this server has taken.
func (s *Service) ackSoon(id model.StreamID, peer model.PeerID, cursor int64) {
	rt := s.rt(id)
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.ackPeer != peer {
		// Another peer delivers the stream now (it moved onto a hub). What
		// was owed the old one is sent at once, and its cursor forgotten:
		// the new peer's log has positions of its own.
		if rt.ackTimer != nil {
			rt.ackTimer.Stop()
			rt.ackTimer = nil
		}
		if old := s.connOf(rt.ackPeer); old != nil && rt.cursor > 0 {
			old.conn.Send(wire.Frame{Frame: wire.FAck, StreamID: string(id), Cursor: rt.cursor})
		}
		rt.ackPeer, rt.cursor = peer, 0
	}
	if cursor > rt.cursor {
		rt.cursor = cursor
	}
	if rt.ackTimer != nil {
		return
	}
	rt.ackTimer = time.AfterFunc(ackEvery, func() {
		rt.mu.Lock()
		rt.ackTimer = nil
		cursor := rt.cursor
		rt.mu.Unlock()
		if pc := s.connOf(peer); pc != nil && cursor > 0 {
			pc.conn.Send(wire.Frame{Frame: wire.FAck, StreamID: string(id), Cursor: cursor})
		}
	})
}
