package link

import (
	"context"
	"encoding/json"
	"time"

	"github.com/tikaydaes/churr/internal/link/wire"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

// What tests in package link_test need of the inside of the package: mostly
// the means to play a server that misbehaves with a real server's keys.

// SetDialBase shortens the dialler's first wait and returns a function that
// puts it back.
func SetDialBase(d time.Duration) func() {
	old := dialBase.Swap(int64(d))
	return func() { dialBase.Store(old) }
}

// NextWait is the dialler's wait after a number of failures.
func NextWait(failures int, jitter float64) time.Duration {
	return nextWait(failures, time.Second, jitter)
}

// TestStamp takes this server's next stamp in a stream, as an event it made
// now would be given: a real `at` by the reference clock as this server
// reads it, the next counter, the offset it was told.
func (s *Service) TestStamp(stream model.StreamID) (wire.Stamp, error) {
	st, err := s.store.Streams().Get(s.ctx, stream)
	if err != nil {
		return wire.Stamp{}, err
	}
	return s.stamp(s.ctx, s.store, st)
}

// TestSeal builds an envelope as this server, of any type and fields, with a
// stamp of the caller's choosing and under an epoch of the caller's choosing
// (0: the newest held).
func (s *Service) TestSeal(stream model.StreamID, typ string, fields map[string]any, stamp wire.Stamp, epoch int64) (wire.Envelope, error) {
	st, err := s.store.Streams().Get(s.ctx, stream)
	if err != nil {
		return wire.Envelope{}, err
	}
	return s.seal(s.ctx, s.store, st, newID(), typ, fields, stamp, epoch)
}

// TestSealAs is TestSeal for an event whose origin event id the caller
// chose: the same event sealed twice, as an origin does when it submits one
// again under a newer key epoch.
func (s *Service) TestSealAs(stream model.StreamID, id, typ string, fields map[string]any, stamp wire.Stamp, epoch int64) (wire.Envelope, error) {
	st, err := s.store.Streams().Get(s.ctx, stream)
	if err != nil {
		return wire.Envelope{}, err
	}
	return s.seal(s.ctx, s.store, st, id, typ, fields, stamp, epoch)
}

// TestSealNext is TestSeal with this server's own next stamp: an envelope a
// well-behaved server could have made, apart from what is in it.
func (s *Service) TestSealNext(stream model.StreamID, typ string, fields map[string]any, epoch int64) (wire.Envelope, error) {
	stamp, err := s.TestStamp(stream)
	if err != nil {
		return wire.Envelope{}, err
	}
	return s.TestSeal(stream, typ, fields, stamp, epoch)
}

// TestSubmit sends an envelope to the stream's time reference exactly as
// given and returns the reference's answer: the accepted frame, which names
// the envelope if it was taken and carries the refusal if it was not. When
// this server is itself the reference nobody checks the stamp: the envelope
// goes into its log and to the other server, and is applied here as every
// server applies what is delivered to it.
func (s *Service) TestSubmit(stream model.StreamID, env wire.Envelope) (wire.Frame, error) {
	st, err := s.store.Streams().Get(s.ctx, stream)
	if err != nil {
		return wire.Frame{}, err
	}
	if st.Sequencer == store.SequencerSelf {
		var logged wire.Envelope
		rej, err := s.apply(s.ctx, stream, env, s.open(s.ctx, st, env, false), nil, func(tx store.Tx, cur store.Stream) error {
			var err error
			logged, err = s.appendLog(s.ctx, tx, stream, env)
			return err
		})
		if err != nil {
			return wire.Frame{}, err
		}
		if rej != nil {
			s.noteRejected(s.ctx, st, env, rej)
		}
		s.deliverOut(s.ctx, st, logged)
		return wire.Frame{Frame: wire.FAccepted, IDs: []string{env.OriginEventID}}, nil
	}
	pc := s.connOf(st.PeerID)
	if pc == nil {
		return wire.Frame{}, model.ErrUnavailable
	}
	id := pc.conn.NextID()
	ch := make(chan wire.Frame, 1)
	s.mu.Lock()
	if s.testAnswers == nil {
		s.testAnswers = map[int64]chan wire.Frame{}
	}
	s.testAnswers[id] = ch
	s.mu.Unlock()
	if err := pc.conn.Send(wire.Frame{Frame: wire.FSubmit, ID: id, Envelope: &env}); err != nil {
		return wire.Frame{}, err
	}
	select {
	case f := <-ch:
		return f, nil
	case <-time.After(5 * time.Second):
		return wire.Frame{}, model.ErrUnavailable.With("The reference did not answer the submit.")
	}
}

// Took reports whether an accepted frame names an envelope as taken.
func Took(f wire.Frame, env wire.Envelope) bool {
	for _, id := range f.IDs {
		if id == env.OriginEventID {
			return true
		}
	}
	return false
}

// TestEphemeral sends an ephemeral envelope exactly as given.
func (s *Service) TestEphemeral(stream model.StreamID, env wire.Envelope) error {
	st, err := s.store.Streams().Get(s.ctx, stream)
	if err != nil {
		return err
	}
	pc := s.connOf(st.PeerID)
	if pc == nil {
		return model.ErrUnavailable
	}
	return pc.conn.Send(wire.Frame{Frame: wire.FEphemeral, Envelope: &env})
}

// TestDeliver hands this server an envelope as if whoever delivers the
// stream to it had sent it, at a log position and after a counter of the
// caller's choosing, and waits until the stream's queue has dealt with it.
func (s *Service) TestDeliver(stream model.StreamID, env wire.Envelope, cursor, prev int64) {
	st, err := s.store.Streams().Get(s.ctx, stream)
	if err != nil {
		return
	}
	env.Cursor, env.Prev = cursor, prev
	rt := s.rt(stream)
	done := make(chan struct{})
	rt.jobs <- func() {
		s.delivered(s.ctx, st.PeerID, env)
		close(done)
	}
	<-done
}

// TestHave is, per origin, the counter this server holds everything up to in
// a stream.
func (s *Service) TestHave(stream model.StreamID) map[string]int64 {
	have, _ := s.haveOf(s.ctx, s.store, stream)
	return have
}

// TestOutbox is how many of this server's events wait in a stream's outbox.
func (s *Service) TestOutbox(stream model.StreamID) int {
	rows, _ := s.store.Streams().Outbox(s.ctx, stream)
	return len(rows)
}

// TestKick submits what waits for a stream now.
func (s *Service) TestKick(stream model.StreamID) { s.kickStream(s.ctx, stream, true) }

// TestControl signs a control frame as this server and sends it over a
// stream's peer.
func (s *Service) TestControl(stream model.StreamID, name string, c wire.Control) bool {
	st, err := s.store.Streams().Get(s.ctx, stream)
	if err != nil {
		return false
	}
	return s.sendControl(s.ctx, st, name, c)
}

// TestKeys returns the epochs and keys this server holds for a stream.
func (s *Service) TestKeys(stream model.StreamID) map[int64][]byte {
	out := map[int64][]byte{}
	keys, _ := s.keysOf(s.ctx, s.store, stream)
	for _, k := range keys {
		out[k.epoch] = k.key
	}
	return out
}

// TestStream returns a stream's row.
func (s *Service) TestStream(stream model.StreamID) (store.Stream, error) {
	return s.store.Streams().Get(s.ctx, stream)
}

// TestAsk sends a direct message of any kind to a named server over a peer
// and returns the error of the answer, if it was one.
func (s *Service) TestAsk(peer model.PeerID, to, kind string, fields map[string]any) error {
	sv, err := s.store.Peers().ServerByName(s.ctx, to)
	if err != nil {
		return err
	}
	_, err = s.ask(s.ctx, peer, sv, kind, fields, 3*time.Second)
	return err
}

// TestDirect sends a direct message with a clock and a msg_id of the
// caller's choosing, and returns the frame so it can be sent again.
func (s *Service) TestDirect(peer model.PeerID, to, kind string, fields map[string]any, at model.Time, again *wire.Frame) (wire.Frame, error) {
	pc := s.connOf(peer)
	if pc == nil {
		return wire.Frame{}, model.ErrUnavailable
	}
	if again != nil {
		f := *again
		f.ID = pc.conn.NextID()
		return f, pc.conn.Send(f)
	}
	sv, err := s.store.Peers().ServerByName(s.ctx, to)
	if err != nil {
		return wire.Frame{}, err
	}
	body := map[string]any{"kind": kind, "msg_id": newID(), "at": at, "re": nil}
	for k, v := range fields {
		body[k] = v
	}
	f, err := wire.SealDirect(s.keys, s.id.Name, sv.Name, sv.BoxKey, wire.EncodeDirect(mustJSON(body)))
	if err != nil {
		return f, err
	}
	f.ID = pc.conn.NextID()
	return f, pc.conn.Send(f)
}

// TestDrop closes a peer's connection without a word, as a network would.
func (s *Service) TestDrop(peer model.PeerID) {
	if pc := s.connOf(peer); pc != nil {
		pc.conn.Close()
	}
}

// TestConnected reports whether a peer's connection is up.
func (s *Service) TestConnected(peer model.PeerID) bool { return s.connOf(peer) != nil }

// TestLastError is the last error frame another side sent this server.
func (s *Service) TestLastError() wire.Frame {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastError
}

// TestFetch asks again, now, for every file that has not arrived.
func (s *Service) TestFetch() {
	s.mu.Lock()
	for _, st := range s.fetches {
		st.next = time.Time{}
	}
	s.mu.Unlock()
	s.fetchArriving(context.Background())
}

// TestSubmitting sets a function that is given every submit frame of this
// server's own events before it leaves and returns the frame to send in its
// place. A frame with no envelopes is not sent at all, as a connection that
// loses it would not deliver it. Nil puts things back.
func (s *Service) TestSubmitting(fn func(stream model.StreamID, f wire.Frame) wire.Frame) {
	if fn == nil {
		s.testSubmitting.Store(nil)
		return
	}
	s.testSubmitting.Store(&fn)
}

// TestNewEpoch makes the stream's next key as its owner and sends it to the
// other servers, without the stream.servers_changed that starts the epoch.
func (s *Service) TestNewEpoch(stream model.StreamID) (int64, error) {
	st, err := s.store.Streams().Get(s.ctx, stream)
	if err != nil {
		return 0, err
	}
	return s.newEpoch(s.ctx, st)
}

// TestStartEpoch submits the stream.servers_changed that starts an epoch
// made with TestNewEpoch. The server list stays as it is.
func (s *Service) TestStartEpoch(stream model.StreamID, epoch int64) error {
	st, err := s.store.Streams().Get(s.ctx, stream)
	if err != nil {
		return err
	}
	_, names, err := s.activeServers(s.ctx, s.store, stream)
	if err != nil {
		return err
	}
	version := st.ServersVersion + 1
	if _, err := s.store.Streams().Update(s.ctx, stream, func(x *store.Stream) error {
		x.ServersVersion = version
		return nil
	}); err != nil {
		return err
	}
	return s.originate(s.ctx, st, evServersChanged, map[string]any{"version": version, "servers": names, "joined": []string{},
		"left": []string{}, "epoch": epoch}, epoch)
}

// TestTallyFold shortens the second within which a poll's home server
// folds changes into one form.tallied.
func (s *Service) TestTallyFold(d time.Duration) { s.tallyFold = d }

// TestAnswerBox seals an answer to a poll's home server as this server.
func (s *Service) TestAnswerBox(home string, button string, answers map[string]any) (wire.Bytes, error) {
	box := s.keys.BoxPublic()
	if home != s.id.Name {
		sv, err := s.store.Peers().ServerByName(s.ctx, home)
		if err != nil {
			return nil, err
		}
		box = sv.BoxKey
	}
	answer, err := wire.EncodeJSON("FormAnswer", mustJSON(answerPlain{ButtonID: button, Answers: answers, AddedOptions: []addedOption{}}))
	if err != nil {
		return nil, err
	}
	return wire.SealTo(s.id.Name, home, box, answer)
}

// TestAskReply is TestAsk that returns the answer: its kind and fields.
func (s *Service) TestAskReply(peer model.PeerID, to, kind string, fields map[string]any) (string, map[string]any, error) {
	sv, err := s.store.Peers().ServerByName(s.ctx, to)
	if err != nil {
		return "", nil, err
	}
	m, err := s.ask(s.ctx, peer, sv, kind, fields, 3*time.Second)
	if err != nil {
		return "", nil, err
	}
	var body map[string]any
	json.Unmarshal(m.raw, &body)
	return m.Kind, body, nil
}

// TestForgetClocks empties what the service remembers of clocks, as a
// restart does: what remains is what the peers' rows hold.
func (s *Service) TestForgetClocks() {
	s.mu.Lock()
	s.told, s.measured = map[model.PeerID]model.PeerClock{}, map[model.PeerID]model.PeerClock{}
	s.mu.Unlock()
}

// TestTold is what a peer last told this server about its clock.
func (s *Service) TestTold(peer model.PeerID) (model.PeerClock, bool) { return s.toldBy(s.ctx, peer) }

// TestHideClock makes this server behave as if a peer had never told it its
// clock offset, or with false puts things back.
func (s *Service) TestHideClock(peer model.PeerID, hide bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.testNoClock == nil {
		s.testNoClock = map[model.PeerID]bool{}
	}
	s.testNoClock[peer] = hide
}

// TestAway records a stretch a direct peer was disconnected for, by this
// server's clock, as if it had happened.
func (s *Service) TestAway(peer model.PeerID, from, to model.Time) error {
	return s.store.Peers().AddAway(s.ctx, peer, int64(from), int64(to), awayKept)
}

// TestLog returns the envelopes in this server's log of a stream, in log
// order, each with its place in the log beside it.
func (s *Service) TestLog(stream model.StreamID) []wire.Envelope {
	rows, _ := s.store.Streams().Log(s.ctx, stream, 0, 10000)
	var out []wire.Envelope
	for _, row := range rows {
		if env, err := wire.UnmarshalEnvelope(row.Envelope); err == nil {
			env.Cursor, env.Prev = row.Seq, row.Prev
			out = append(out, env)
		}
	}
	return out
}

// TestOriginate makes an event of this server's own in a stream, as the
// link makes its own events: applied here at once, then sent.
func (s *Service) TestOriginate(stream model.StreamID, typ string, fields map[string]any) error {
	st, err := s.store.Streams().Get(s.ctx, stream)
	if err != nil {
		return err
	}
	return s.enqueue(s.ctx, st, typ, fields)
}

// TestHubFrame sends a hub a frame as this server and returns the error
// frame that answers it, or a zero frame if none came within wait.
func (s *Service) TestHubFrame(peer model.PeerID, f wire.Frame, wait time.Duration) (wire.Frame, error) {
	pc := s.connOf(peer)
	if pc == nil {
		return wire.Frame{}, model.ErrUnavailable
	}
	f.ID = pc.conn.NextID()
	answer, done := s.awaitFrame(peer, f.ID)
	defer done()
	if err := pc.conn.Send(f); err != nil {
		return wire.Frame{}, err
	}
	select {
	case a := <-answer:
		return a, nil
	case <-time.After(wait):
		return wire.Frame{}, nil
	}
}

// TestSignBlock signs content as a named block with this server's key.
func (s *Service) TestSignBlock(name string, content any) wire.Frame {
	f, err := wire.SignBlock(name, content, s.keys)
	if err != nil {
		panic(err)
	}
	return f
}

// TestChannel returns the record this server holds of a hub-owned channel,
// with the bytes the hub signed and its signature.
func (s *Service) TestChannel(stream model.StreamID) (rec wire.ChannelRecord, signed, sig []byte, ok bool) {
	row, err := s.store.Streams().Channel(s.ctx, stream)
	if err != nil || wire.PeekBytes(wire.FChannel, row.Record, &rec) != nil {
		return rec, nil, nil, false
	}
	return rec, row.Record, row.Sig, true
}

// TestInjectChannel hands this server a channel frame as if the hub of a
// peer had sent it, and returns what the server answered.
func (s *Service) TestInjectChannel(peer model.PeerID, f wire.Frame) error {
	pc := s.connOf(peer)
	if pc == nil {
		return model.ErrUnavailable
	}
	return s.onChannel(s.ctx, pc, f)
}

// TestDeaf makes this server lose every answer to what it submits, or with
// false hear them again: its events are taken and it never learns so.
func (s *Service) TestDeaf(on bool) { s.testDeaf.Store(on) }

// SetFirstHistoryWait shortens how long a server that has joined a hub-owned
// channel waits for its history before asking for it, and returns the
// function that puts the wait back.
func SetFirstHistoryWait(d time.Duration) func() {
	old := firstHistoryWait.Swap(int64(d))
	return func() { firstHistoryWait.Store(old) }
}
