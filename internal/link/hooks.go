package link

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/tikaydaes/churr/internal/link/wire"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

// This file is where two other feature packages meet the link: calls, whose
// passes and signalling for a call hosted on another server travel as
// sealed direct messages (section 18, ruling R26), and sealed chats, whose
// device lists are carried between home servers (section 16). Neither
// package is imported here. Each side declares what it needs, and server
// connects them.

// ------------------------------------------------------------------ calls

// CallPassRequest is the body of call_pass_request: an account of the
// asking server wants to join a call this server hosts.
type CallPassRequest struct {
	ConversationID model.ConversationID
	CallID         model.CallID
	AccountID      model.AccountID
}

// CallMedia says where the host takes media.
type CallMedia struct {
	Host    string `json:"host"`
	UDPPort int    `json:"udp_port"`
	TCPPort int    `json:"tcp_port"`
}

// CallPass is the body of call_pass: 32 random bytes, good for sixty seconds
// and one use, for one account and one call.
type CallPass struct {
	CallID    model.CallID
	AccountID model.AccountID
	Pass      string
	ExpiresAt model.Time
	Media     CallMedia
}

// CallSignal is the body of call_signal, in either direction. Signal is the
// object a client and its own server exchange for a local call.
type CallSignal struct {
	ConversationID model.ConversationID
	CallID         model.CallID
	AccountID      model.AccountID
	Pass           string
	Signal         json.RawMessage
}

// CallHost is what link needs of the calls package. The link has
// authenticated the server named in `from` and has applied the stream's own
// rules to an event before it hands anything over.
type CallHost interface {
	// PassRequested answers a call_pass_request. An error that is a
	// *model.Error goes back as the error answer.
	PassRequested(ctx context.Context, from string, req CallPassRequest) (CallPass, error)
	// Signalled takes a call_signal.
	Signalled(ctx context.Context, from string, sig CallSignal)
	// StreamEvent takes one of the four call events from the stream of a
	// shared conversation: its type, when it happened and its fields as
	// they travelled. host is the server that submitted it.
	StreamEvent(ctx context.Context, host string, hostID model.ServerID, conv model.ConversationID, eventType string, at model.Time, fields json.RawMessage)
}

// SetCalls connects the calls package.
func (s *Service) SetCalls(c CallHost) {
	s.mu.Lock()
	s.calls = c
	s.mu.Unlock()
}

func (s *Service) callHost() CallHost {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

// modelError turns the error answer of a direct message into the error a
// service returns.
func modelError(err error) error {
	var e *wire.Error
	if !errors.As(err, &e) {
		return err
	}
	switch e.Code {
	case wire.EForbidden, wire.ENotInStream, wire.ENotOwner:
		return model.ErrForbidden.With(e.Message)
	case wire.ENotFound, wire.EUnknownStream, wire.EUnknownServer:
		return model.ErrNotFound.With(e.Message)
	case wire.EFeatureOff:
		return model.ErrFeatureOff.With(e.Message)
	case wire.EConflict:
		return model.ErrConflict.With(e.Message)
	case wire.ERateLimited:
		return model.ErrRateLimited.With(e.Message)
	}
	return model.ErrUnavailable.With(e.Message)
}

// wireCode is the code a service's error travels under.
func wireCode(err error) (string, string) {
	switch model.CodeOf(err) {
	case model.CodeForbidden:
		return wire.EForbidden, messageOf(err)
	case model.CodeNotFound:
		return wire.ENotFound, messageOf(err)
	case model.CodeFeatureOff:
		return wire.EFeatureOff, messageOf(err)
	case model.CodeConflict:
		return wire.EConflict, messageOf(err)
	case model.CodeRateLimited:
		return wire.ERateLimited, messageOf(err)
	}
	return wire.EInternal, "The receiver failed."
}

func messageOf(err error) string {
	var e *model.Error
	if errors.As(err, &e) {
		return e.Message
	}
	return "Refused."
}

// sharedWith finds the active stream of a conversation and the row of a
// server in it.
func (s *Service) sharedWith(ctx context.Context, conv model.ConversationID, server string) (store.Stream, store.Server, error) {
	st, err := s.store.Streams().ByConversation(ctx, conv)
	if err != nil {
		return st, store.Server{}, model.ErrNotFound.With("That conversation is not shared.")
	}
	sv, err := s.store.Peers().ServerByName(ctx, server)
	if err != nil {
		return st, sv, model.ErrNotFound.With("There is no such server.")
	}
	active, _, err := s.activeServers(ctx, s.store, st.ID)
	if err != nil {
		return st, sv, err
	}
	for _, ss := range active {
		if ss.ServerID == sv.ID {
			return st, sv, nil
		}
	}
	return st, sv, model.ErrNotFound.With("That server is not in this conversation.")
}

// RequestPass asks the host of a call for a pass for one of this server's
// accounts and waits for it. The host's own refusal comes back as a
// *model.Error; a host that cannot be reached as model.ErrUnavailable.
func (s *Service) RequestPass(ctx context.Context, host string, req CallPassRequest) (CallPass, error) {
	st, sv, err := s.sharedWith(ctx, req.ConversationID, host)
	if err != nil {
		return CallPass{}, err
	}
	m, err := s.ask(ctx, st.PeerID, sv, kCallPassRequest, map[string]any{"stream_id": st.ID, "call_id": req.CallID, "account_id": req.AccountID}, 10*time.Second)
	if err != nil {
		return CallPass{}, modelError(err)
	}
	var b struct {
		CallID    model.CallID    `json:"call_id"`
		AccountID model.AccountID `json:"account_id"`
		Pass      string          `json:"pass"`
		ExpiresAt model.Time      `json:"expires_at"`
		Media     CallMedia       `json:"media"`
	}
	if m.Kind != kCallPass || json.Unmarshal(m.raw, &b) != nil || b.Pass == "" || b.CallID != req.CallID || b.AccountID != req.AccountID {
		return CallPass{}, model.ErrUnavailable.With("The server hosting the call did not answer with a pass.")
	}
	return CallPass{CallID: b.CallID, AccountID: b.AccountID, Pass: b.Pass, ExpiresAt: b.ExpiresAt, Media: b.Media}, nil
}

// onCallPassRequest is the host's side: the calls package decides.
func (s *Service) onCallPassRequest(ctx context.Context, m directMessage) {
	var b struct {
		StreamID  model.StreamID  `json:"stream_id"`
		CallID    model.CallID    `json:"call_id"`
		AccountID model.AccountID `json:"account_id"`
	}
	if json.Unmarshal(m.raw, &b) != nil {
		return
	}
	host := s.callHost()
	if host == nil {
		s.replyError(ctx, m, wire.EFeatureOff, "Calls are off on this server.")
		return
	}
	st, err := s.store.Streams().Get(ctx, b.StreamID)
	if err != nil || st.State != store.StreamActive || st.PeerID != m.peer || st.ConversationID == "" {
		s.replyError(ctx, m, wire.EUnknownStream, "No such stream here.")
		return
	}
	if _, _, err := s.sharedWith(ctx, st.ConversationID, m.from.Name); err != nil {
		s.replyError(ctx, m, wire.ENotInStream, "That server is not in the stream.")
		return
	}
	pass, err := host.PassRequested(ctx, m.from.Name, CallPassRequest{ConversationID: st.ConversationID, CallID: b.CallID, AccountID: b.AccountID})
	if err != nil {
		code, message := wireCode(err)
		s.replyError(ctx, m, code, message)
		return
	}
	s.sendDirect(ctx, m.peer, m.from, kCallPass, m.MsgID, map[string]any{"call_id": pass.CallID, "account_id": pass.AccountID, "pass": pass.Pass,
		"expires_at": pass.ExpiresAt, "media": pass.Media})
}

// Signal sends one call_signal to a server as a sealed direct message: not
// numbered, never stored, and held for nobody. It fails when the server
// cannot be reached now.
func (s *Service) Signal(ctx context.Context, to string, sig CallSignal) error {
	st, sv, err := s.sharedWith(ctx, sig.ConversationID, to)
	if err != nil {
		return err
	}
	fields := map[string]any{"stream_id": st.ID, "call_id": sig.CallID, "account_id": sig.AccountID, "signal": sig.Signal}
	if sig.Pass != "" {
		fields["pass"] = sig.Pass
	}
	_, err = s.sendDirect(ctx, st.PeerID, sv, kCallSignal, "", fields)
	return err
}

func (s *Service) onCallSignal(ctx context.Context, m directMessage) {
	var b struct {
		StreamID  model.StreamID  `json:"stream_id"`
		CallID    model.CallID    `json:"call_id"`
		AccountID model.AccountID `json:"account_id"`
		Pass      string          `json:"pass"`
		Signal    json.RawMessage `json:"signal"`
	}
	host := s.callHost()
	if json.Unmarshal(m.raw, &b) != nil || host == nil {
		return
	}
	st, err := s.store.Streams().Get(ctx, b.StreamID)
	if err != nil || st.State != store.StreamActive || st.PeerID != m.peer || st.ConversationID == "" {
		return
	}
	if _, _, err := s.sharedWith(ctx, st.ConversationID, m.from.Name); err != nil {
		return
	}
	host.Signalled(ctx, m.from.Name, CallSignal{ConversationID: st.ConversationID, CallID: b.CallID, AccountID: b.AccountID, Pass: b.Pass, Signal: b.Signal})
}

// SubmitCallEvent submits a call event of a call this server hosts to the
// conversation's stream. It does not wait for the network: the stream's
// outbox carries it, in the order given. fields are those of section 11
// (and whatever else the calls package wants its other self to have;
// receivers ignore what they do not know).
func (s *Service) SubmitCallEvent(ctx context.Context, conv model.ConversationID, eventType string, at model.Time, fields map[string]any) error {
	switch eventType {
	case evCallStarted, evCallJoined, evCallLeft, evCallEnded:
	default:
		return errors.New("link: not a call event: " + eventType)
	}
	st, err := s.store.Streams().ByConversation(ctx, conv)
	if err != nil {
		return err
	}
	out := map[string]any{}
	for k, v := range fields {
		out[k] = v
	}
	if !at.IsZero() {
		out["at"] = at
	}
	if eventType == evCallEnded {
		s.CallEnded(conv, model.CallID(fmt.Sprint(fields["call_id"])))
	}
	return s.enqueue(ctx, st, eventType, out)
}

// LastHeard is when anything last arrived from a server over the link. On a
// direct link that is up, that is now: its keep-alive is the connection
// itself. The zero time means never.
func (s *Service) LastHeard(server string) time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, pc := range s.conns {
		if !pc.hub && pc.name == server {
			return time.Now()
		}
	}
	return s.heard[server]
}

// hear notes that something verified arrived from a server.
func (s *Service) hear(server string) {
	s.mu.Lock()
	s.heard[server] = time.Now()
	s.mu.Unlock()
}

// applyCallEvent applies the stream's rules to a call event (section 12)
// and hands it to the calls package: call.started only from the home server
// of whoever started the call, which must be the host it names; the other
// three from the server that hosts the call, which the calls package knows
// and checks.
func (s *Service) applyCallEvent(a *apply) (*rejection, error) {
	var b struct {
		CallID      model.CallID    `json:"call_id"`
		StartedByID model.AccountID `json:"started_by_id"`
		Host        string          `json:"host"`
	}
	if rej := a.decode(&b); rej != nil {
		return rej, nil
	}
	if !model.ValidID(string(b.CallID)) {
		return reject("invalid"), nil
	}
	if a.in.Type == evCallStarted {
		if _, rej, err := a.member(b.StartedByID); rej != nil || err != nil {
			return rej, err
		}
		if b.Host != a.origin.Name {
			return reject("not_the_host"), nil
		}
	}
	if s.isSelf(a.origin.ServerID) {
		return nil, nil // this server's own call
	}
	origin, conv, typ, at, raw := a.origin, a.conv.ID, a.in.Type, a.in.At, append([]byte(nil), a.in.raw...)
	switch typ {
	case evCallStarted:
		// Two people on two servers may start a call in one conversation
		// at the same moment, each before the other's call.started has
		// arrived. Nobody numbers events (ruling R57). The rule that
		// settles it, the same on every server: the call.started with the
		// earlier stamp is the call (section 18). A later one is not taken
		// here; its host ends it when the earlier one reaches it.
		incoming := callStart{id: string(b.CallID), order: a.order, stamped: a.env.At, origin: origin, at: at, raw: raw}
		s.mu.Lock()
		known, live := s.callStarts[conv]
		if live && incoming.stamped-known.stamped > callTieWindow {
			// Not the same moment: the call this server remembers ended
			// without its hearing so, and this is a new one.
			live = false
		}
		switch {
		case live && known.id != incoming.id && incoming.order > known.order:
			s.callRivals[conv] = incoming
			s.mu.Unlock()
			return rejectQuiet("call_lost"), nil
		case live && known.id != incoming.id:
			s.stats.Replaced["call"]++
			delete(s.callRivals, conv)
		}
		s.callStarts[conv] = incoming
		s.mu.Unlock()
	case evCallEnded:
		s.mu.Lock()
		if s.callStarts[conv].id == string(b.CallID) {
			delete(s.callStarts, conv)
		}
		if s.callRivals[conv].id == string(b.CallID) {
			delete(s.callRivals, conv)
		}
		s.mu.Unlock()
	}
	host := s.callHost()
	if host == nil {
		return nil, nil
	}
	// The calls package takes a call.started it is handed as the call: one
	// that is live here and is not it ends, and its people are offered
	// this one.
	a.rec.After(func() { host.StreamEvent(s.ctx, origin.Name, origin.ServerID, conv, typ, at, raw) })
	return nil, nil
}

// callStart is a call.started as the tie rule needs it: which call, where
// its stamp puts it, and for one that came from another server the event
// itself.
type callStart struct {
	id, order string
	stamped   int64 // the stamp's `at`
	origin    store.StreamServer
	at        model.Time
	raw       []byte
}

// callTieWindow is how far apart, by their stamps, two call.started can be
// and still be two starts of one moment. It is the 30 seconds a stamp may be
// from the reference clock, both ways.
const callTieWindow = 60 * 1000

// noteCallStart records the stamp of a call.started this server made, when
// it is made and again if the time reference has it stamped anew. If a
// call.started from another server lost to the old stamp and beats the new
// one, it is the call after all: it is handed to the calls package, which
// ends the local call.
func (s *Service) noteCallStart(conv model.ConversationID, id, order string, stamped int64) {
	s.mu.Lock()
	known, live := s.callStarts[conv]
	rival, contested := s.callRivals[conv]
	if live && known.id != id && known.order < order && stamped-known.stamped <= callTieWindow {
		// A call from elsewhere already stands, with an earlier stamp.
		s.mu.Unlock()
		return
	}
	s.callStarts[conv] = callStart{id: id, order: order, stamped: stamped}
	won := contested && rival.id != id && rival.order < order
	if won {
		s.callStarts[conv] = rival
		delete(s.callRivals, conv)
		s.stats.Replaced["call"]++
	}
	s.mu.Unlock()
	if host := s.callHost(); won && host != nil {
		go host.StreamEvent(s.ctx, rival.origin.Name, rival.origin.ServerID, conv, evCallStarted, rival.at, rival.raw)
	}
}

// CallEnded tells the link that the call it took to be live in a shared
// conversation is over, however it ended. The calls package calls it.
func (s *Service) CallEnded(conv model.ConversationID, id model.CallID) {
	s.mu.Lock()
	if s.callStarts[conv].id == string(id) {
		delete(s.callStarts, conv)
	}
	s.mu.Unlock()
}

// ---------------------------------------------------------- device lists

// SealedStore is what link needs of the sealed package: to store a version
// of a remote account's device list that arrived, and to read the versions
// of a local account's list to send. *sealed.Service has both.
type SealedStore interface {
	StoreRemoteList(ctx context.Context, account model.AccountID, signedList []byte) error
	RemoteLists(ctx context.Context, account model.AccountID, after int64) ([]store.SignedDeviceList, error)
	// CheckRemoteEpoch and CheckRemoteMessage say whether an epoch record
	// or a sealed message that arrived in a sealed stream is stored or
	// dropped, from the chat's place in the sequence (docs/SEALED.md
	// section 17). Both servers see the same sequence and so reach the
	// same result.
	CheckRemoteEpoch(ctx context.Context, a store.Areas, conv model.ConversationID, record []byte) (row model.EpochRecord, drop bool, err error)
	// CheckRemoteEpochFor is CheckRemoteEpoch for a record made for a key
	// epoch this server already holds a record of: would it have been the
	// chat's next when the chat stood at the epoch before.
	CheckRemoteEpochFor(ctx context.Context, a store.Areas, conv model.ConversationID, record []byte, keyEpoch int64) (row model.EpochRecord, drop bool, err error)
	CheckRemoteMessage(ctx context.Context, a store.Areas, conv model.ConversationID, author model.AccountID, keyEpoch int64, quiet bool, ciphertext []byte) (drop bool, err error)
	// CheckRemoteStart and AcceptRemote are the receiving server's part
	// of starting a sealed chat across servers (sealedstart.go).
	CheckRemoteStart(ctx context.Context, starter, local model.Account) error
	AcceptRemote(ctx context.Context, starter, local model.Account, stream model.StreamID, record []byte) (model.Conversation, error)
}

// applySealed applies the two events a sealed stream carries (section 11,
// "Sealed chats"). The server cannot read either: it checks who may send
// it, and asks the sealed package whether it is the chat's next record or a
// message under the chat's current epoch. One that is not is rejected
// quietly.
func (s *Service) applySealed(a *apply) (*rejection, error) {
	sealed := s.sealedStore()
	if sealed == nil {
		return rejectQuiet("sealed_off"), nil
	}
	if a.in.Type == model.EventSealedEpochStarted {
		var b struct {
			KeyEpoch  int64           `json:"key_epoch"`
			AccountID model.AccountID `json:"account_id"`
			DeviceID  model.DeviceID  `json:"device_id"`
			Record    wire.Bytes      `json:"record"`
		}
		if rej := a.decode(&b); rej != nil {
			return rej, nil
		}
		// From the home server of the account whose device made it, and
		// the account is one of the two.
		if _, rej, err := a.member(b.AccountID); rej != nil || err != nil {
			return rej, err
		}
		cur, found, err := a.tx.Sealed().Epoch(a.ctx, a.conv.ID, b.KeyEpoch)
		if err != nil {
			return nil, err
		}
		if found && string(cur.Record) == string(b.Record) {
			// A record this server holds already: its own, or epoch
			// record 1, which came with the offer.
			return nil, nil
		}
		if found {
			// Two devices, on the two servers, each made this key epoch
			// before either had the other's record. Nobody numbers events
			// (ruling R57), so each server stored its own first. The rule
			// that settles it, the same on both: the record with the
			// earlier stamp stands, and replaces a later one already held
			// (section 11). The stamp is the envelope's, signed by its
			// origin; for the record held it was kept when it was stored.
			if cur.Ord == "" || a.order >= cur.Ord {
				return rejectQuiet("sealed_epoch_lost"), nil
			}
			row, drop, err := sealed.CheckRemoteEpochFor(a.ctx, a.tx, a.conv.ID, b.Record, b.KeyEpoch)
			if err != nil {
				return nil, err
			}
			if drop || row.KeyEpoch != b.KeyEpoch || row.AccountID != b.AccountID {
				return rejectQuiet("sealed_epoch_lost"), nil
			}
			seq, err := a.rec.Seq(a.ctx, a.conv.ID)
			if err != nil {
				return nil, err
			}
			row.Seq, row.Ord = seq, a.order
			if err := a.tx.Sealed().ReplaceEpoch(a.ctx, row); err != nil {
				return nil, err
			}
			s.count(func(st *Stats) { st.Replaced["sealed_epoch"]++ })
			// The devices here are told as they are told of any record,
			// with the word that it takes the place of the one they had:
			// the device that made the loser starts again from this one
			// (docs/SEALED.md section 5.5, as when a local race is lost).
			return nil, a.rec.EmitAt(a.ctx, model.Event{Type: model.EventSealedEpochStarted, At: row.CreatedAt, ConversationID: a.conv.ID, Seq: seq,
				Payload: map[string]any{"epoch": row, "replaced": true}})
		}
		row, drop, err := sealed.CheckRemoteEpoch(a.ctx, a.tx, a.conv.ID, b.Record)
		if err != nil {
			return nil, err
		}
		if drop || row.KeyEpoch != b.KeyEpoch || row.AccountID != b.AccountID {
			return rejectQuiet("sealed_epoch_lost"), nil
		}
		seq, err := a.rec.Seq(a.ctx, a.conv.ID)
		if err != nil {
			return nil, err
		}
		row.Seq, row.Ord = seq, a.order
		if err := a.tx.Sealed().PutEpoch(a.ctx, row); err != nil {
			if errors.Is(err, model.ErrConflict) {
				return rejectQuiet("sealed_epoch_lost"), nil
			}
			return nil, err
		}
		return nil, a.rec.EmitAt(a.ctx, model.Event{Type: model.EventSealedEpochStarted, At: row.CreatedAt, ConversationID: a.conv.ID, Seq: seq,
			Payload: map[string]any{"epoch": row}})
	}
	var b struct {
		Message wireMessage `json:"message"`
		Blobs   []wireBlob  `json:"blobs"`
	}
	if rej := a.decode(&b); rej != nil {
		return rej, nil
	}
	w := b.Message
	if _, rej, err := a.member(w.AuthorID); rej != nil || err != nil {
		return rej, err
	}
	if rej := s.personalAllowed(a, w.AuthorID); rej != nil {
		return rej, nil
	}
	// The sealed shape and nothing else.
	if w.Sealed == nil || w.Text != "" || len(w.Entities) != 0 || len(w.Cards) != 0 || w.Form != nil || w.System != nil || w.Via != nil ||
		w.ParentID != "" || w.Quote != nil {
		return reject("invalid"), nil
	}
	if _, err := a.tx.Messages().Get(a.ctx, w.ID); err == nil {
		if s.isSelf(a.origin.ServerID) {
			return nil, nil // this server's own message coming back
		}
		return reject("message_exists"), nil
	} else if !errors.Is(err, model.ErrNotFound) {
		return nil, err
	}
	drop, err := sealed.CheckRemoteMessage(a.ctx, a.tx, a.conv.ID, w.AuthorID, w.Sealed.KeyEpoch, w.Quiet, w.Sealed.Ciphertext)
	if err != nil {
		return nil, err
	}
	if drop {
		// Not under the chat's current key epoch.
		return rejectQuiet("sealed_message_stale"), nil
	}
	rm, rej, err := a.toMessage(w, b.Blobs)
	if rej != nil || err != nil {
		return rej, err
	}
	rm.Message.Sealed, rm.Message.Quiet, rm.Message.ClientID = w.Sealed, w.Quiet, w.ClientID
	a.fetchAfter()
	return nil, a.store(rm)
}

// SetSealed connects the sealed package.
func (s *Service) SetSealed(st SealedStore) {
	s.mu.Lock()
	s.sealed = st
	s.mu.Unlock()
}

func (s *Service) sealedStore() SealedStore {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sealed
}

// listVersion is one version in a device_list message.
type listVersion struct {
	Version    int64      `json:"version"`
	SignedList wire.Bytes `json:"signed_list"`
}

// ruleHolds is the rule of section 16 for one local and one remote account:
// both are members of at least one active stream of kind channel. It
// returns such a stream, which is also the peer anything between the two
// travels over.
func (s *Service) ruleHolds(ctx context.Context, a, b model.AccountID) (store.Stream, bool) {
	streams, err := s.store.Streams().SharedWith(ctx, a, b)
	if err != nil || len(streams) == 0 {
		return store.Stream{}, false
	}
	return streams[0], true
}

// DeviceListPublished implements sealed.Outbound: a local account's new
// device list version goes, unasked, to the home server of each remote
// account it has a sealed chat with, while the two still share a shared
// channel.
func (s *Service) DeviceListPublished(ctx context.Context, account model.AccountID, version int64, signedList []byte) {
	partners, err := s.store.Sealed().Partners(ctx, account)
	if err != nil {
		return
	}
	sent := map[model.ServerID]bool{}
	for _, p := range partners {
		acc, err := s.store.Accounts().Get(ctx, p)
		if err != nil || acc.Local() || sent[acc.ServerID] {
			continue
		}
		st, ok := s.ruleHolds(ctx, account, p)
		if !ok {
			continue
		}
		sv, err := s.store.Peers().Server(ctx, acc.ServerID)
		if err != nil {
			continue
		}
		sent[acc.ServerID] = true
		s.sendDirect(ctx, st.PeerID, sv, kDeviceList, "", map[string]any{"account_id": account,
			"versions": []listVersion{{Version: version, SignedList: signedList}}})
	}
}

// RequestDeviceLists asks a remote account's home server for the versions
// of its device list above `after`, on behalf of a local member, and stores
// what comes back. It is refused here already if the two share no shared
// channel.
func (s *Service) RequestDeviceLists(ctx context.Context, remote, by model.AccountID, after int64) error {
	acc, err := s.store.Accounts().Get(ctx, remote)
	if err != nil || acc.Local() {
		return model.ErrNotFound
	}
	st, ok := s.ruleHolds(ctx, by, remote)
	if !ok {
		return model.ErrForbidden.With("You and that person share no shared channel.")
	}
	sv, err := s.store.Peers().Server(ctx, acc.ServerID)
	if err != nil {
		return err
	}
	m, err := s.ask(ctx, st.PeerID, sv, kDeviceListRequest, map[string]any{"account_id": remote, "by_account_id": by, "after_version": after}, 10*time.Second)
	if err != nil {
		return modelError(err)
	}
	if m.Kind != kDeviceList {
		return model.ErrUnavailable
	}
	return nil // onDeviceList has stored it
}

// onDeviceList stores device list versions an account's home server sent.
// A list is signed by the account's own key, so neither server and no hub
// can alter one unnoticed; the sealed package checks that.
func (s *Service) onDeviceList(ctx context.Context, m directMessage) {
	var b struct {
		AccountID model.AccountID `json:"account_id"`
		Versions  []listVersion   `json:"versions"`
	}
	sealed := s.sealedStore()
	if json.Unmarshal(m.raw, &b) != nil || sealed == nil {
		return
	}
	acc, err := s.store.Accounts().Get(ctx, b.AccountID)
	if err != nil || acc.ServerID != m.from.ID {
		s.count(func(st *Stats) { st.Dropped["device_list_not_from_home"]++ })
		return
	}
	for _, v := range b.Versions {
		if err := sealed.StoreRemoteList(ctx, b.AccountID, v.SignedList); err != nil {
			s.count(func(st *Stats) { st.Dropped["device_list_refused"]++ })
			return
		}
	}
}

// onDeviceListRequest answers the home server of someone who wants to start
// a sealed chat with one of this server's accounts, after the checks of
// section 16.
func (s *Service) onDeviceListRequest(ctx context.Context, m directMessage) {
	var b struct {
		AccountID    model.AccountID `json:"account_id"`
		ByAccountID  model.AccountID `json:"by_account_id"`
		AfterVersion int64           `json:"after_version"`
	}
	if json.Unmarshal(m.raw, &b) != nil {
		return
	}
	sealed := s.sealedStore()
	on, _ := store.SettingBool(ctx, s.store, store.SettingSealedEnabled, true)
	if sealed == nil || !on {
		s.replyError(ctx, m, wire.EFeatureOff, "Sealed chats are off on this server.")
		return
	}
	acc, err := s.store.Accounts().Get(ctx, b.AccountID)
	by, err2 := s.store.Accounts().Get(ctx, b.ByAccountID)
	if err != nil || err2 != nil || !acc.Active() || by.ServerID != m.from.ID {
		s.replyError(ctx, m, wire.ENotFound, "No such account.")
		return
	}
	// The rule, for the asker and this server's own account; that the
	// account has not blocked the asker; that it is not a bot.
	if _, ok := s.ruleHolds(ctx, b.AccountID, b.ByAccountID); !ok || acc.Role == model.RoleBot {
		s.replyError(ctx, m, wire.EForbidden, "Those two people share no shared channel.")
		return
	}
	if blocked, _ := s.store.Blocks().Has(ctx, b.AccountID, b.ByAccountID); blocked {
		s.replyError(ctx, m, wire.EForbidden, "That is not allowed.")
		return
	}
	lists, err := sealed.RemoteLists(ctx, b.AccountID, b.AfterVersion)
	if err != nil {
		s.replyError(ctx, m, wire.EInternal, "The receiver failed.")
		return
	}
	versions := make([]listVersion, 0, len(lists))
	for _, l := range lists {
		versions = append(versions, listVersion{Version: l.Version, SignedList: l.Signed})
	}
	s.sendDirect(ctx, m.peer, m.from, kDeviceList, m.MsgID, map[string]any{"account_id": b.AccountID, "versions": versions})
}
