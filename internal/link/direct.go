package link

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/tikaydaes/churr/internal/link/wire"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/secret"
	"github.com/tikaydaes/churr/internal/store"
)

// The kinds of direct message (section 7).
const (
	kOffer             = "offer"
	kAccept            = "accept"
	kDecline           = "decline"
	kKey               = "key"
	kKeyRequest        = "key_request"
	kHistory           = "history"
	kHistoryRequest    = "history_request"
	kBlobRequest       = "blob_request"
	kBlobChunk         = "blob_chunk"
	kCallPassRequest   = "call_pass_request"
	kCallPass          = "call_pass"
	kCallSignal        = "call_signal"
	kDeviceList        = "device_list"
	kDeviceListRequest = "device_list_request"
	kError             = "error"
)

// directMessage is an opened direct message: the fields every kind has, the
// whole plaintext for the kind's own, and where it came from.
type directMessage struct {
	Kind  string     `json:"kind"`
	MsgID string     `json:"msg_id"`
	At    model.Time `json:"at"`
	Re    string     `json:"re"`

	// For kind error.
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`

	raw  json.RawMessage
	from store.Server
	peer model.PeerID
}

// reaches reports whether a server can be reached over a peer: it is the
// other end of a direct link, or on a hub's roster.
func (s *Service) reaches(ctx context.Context, p store.Peer, server model.ServerID) bool {
	if p.Kind == wire.KindServer {
		return p.ServerID == server
	}
	list, err := s.store.Peers().Reachable(ctx, p.ID)
	if err != nil {
		return false
	}
	for _, sv := range list {
		if sv.ID == server {
			return true
		}
	}
	return false
}

// sendDirect seals a direct message to one server and sends it over a peer.
// fields are the kind's own; re is the msg_id it answers, or "". It returns
// the new message's msg_id, and model.ErrUnavailable when the peer is not
// connected.
func (s *Service) sendDirect(ctx context.Context, peer model.PeerID, to store.Server, kind, re string, fields map[string]any) (string, error) {
	pc := s.connOf(peer)
	if pc == nil {
		return "", model.ErrUnavailable.With("The link to " + to.Name + " is not connected.")
	}
	body := map[string]any{}
	for k, v := range fields {
		body[k] = v
	}
	id := newID()
	body["kind"], body["msg_id"], body["at"] = kind, id, s.now()
	if re != "" {
		body["re"] = re
	} else {
		body["re"] = nil
	}
	f, err := wire.SealDirect(s.keys, s.id.Name, to.Name, to.BoxKey, mustJSON(body))
	if err != nil {
		return "", err
	}
	f.ID = pc.conn.NextID()
	if err := pc.conn.Send(f); err != nil {
		return "", model.ErrUnavailable.With("The link to " + to.Name + " is not connected.")
	}
	return id, nil
}

// ask sends a direct message and waits for the one that answers it. An
// answer of kind error comes back as a *wire.Error.
func (s *Service) ask(ctx context.Context, peer model.PeerID, to store.Server, kind string, fields map[string]any, wait time.Duration) (directMessage, error) {
	ch := make(chan directMessage, 1)
	// The reply may arrive before sendDirect returns, so the wait is
	// registered under a placeholder first and moved once the id is known.
	id, err := s.sendDirectAwait(ctx, peer, to, kind, fields, ch)
	if err != nil {
		return directMessage{}, err
	}
	defer func() {
		s.mu.Lock()
		delete(s.replies, id)
		s.mu.Unlock()
	}()
	t := time.NewTimer(wait)
	defer t.Stop()
	select {
	case m := <-ch:
		if m.Kind == kError {
			return m, &wire.Error{Code: m.Code, Message: m.Message}
		}
		return m, nil
	case <-t.C:
		return directMessage{}, model.ErrUnavailable.With(to.Name + " did not answer.")
	case <-ctx.Done():
		return directMessage{}, ctx.Err()
	}
}

// sendDirectAwait is sendDirect that registers ch for the answer before the
// frame leaves.
func (s *Service) sendDirectAwait(ctx context.Context, peer model.PeerID, to store.Server, kind string, fields map[string]any, ch chan directMessage) (string, error) {
	pc := s.connOf(peer)
	if pc == nil {
		return "", model.ErrUnavailable.With("The link to " + to.Name + " is not connected.")
	}
	body := map[string]any{}
	for k, v := range fields {
		body[k] = v
	}
	id := newID()
	body["kind"], body["msg_id"], body["at"], body["re"] = kind, id, s.now(), nil
	f, err := wire.SealDirect(s.keys, s.id.Name, to.Name, to.BoxKey, mustJSON(body))
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	s.replies[id] = ch
	s.mu.Unlock()
	f.ID = pc.conn.NextID()
	if err := pc.conn.Send(f); err != nil {
		s.mu.Lock()
		delete(s.replies, id)
		s.mu.Unlock()
		return "", model.ErrUnavailable.With("The link to " + to.Name + " is not connected.")
	}
	return id, nil
}

// replyError answers a request that cannot be met.
func (s *Service) replyError(ctx context.Context, m directMessage, code, message string) {
	s.sendDirect(ctx, m.peer, m.from, kError, m.MsgID, map[string]any{"code": code, "message": message})
}

// forgetDirect drops msg_ids older than the replay window.
func (s *Service) forgetDirect() {
	now := s.now()
	s.mu.Lock()
	for id, at := range s.seen {
		if now.Sub(at) > directMemory {
			delete(s.seen, id)
		}
	}
	s.mu.Unlock()
}

// onDirect opens a direct frame and hands it to its kind. Any failure to
// verify or open drops the message.
func (s *Service) onDirect(ctx context.Context, pc *peerConn, f wire.Frame) error {
	drop := func(why string) error {
		s.count(func(st *Stats) { st.Dropped["direct:"+why]++ })
		return nil
	}
	if f.To != s.id.Name {
		return wire.Errf(wire.EUnknownServer, "%s is not on this link.", f.To)
	}
	from, err := s.store.Peers().ServerByName(ctx, f.From)
	if err != nil {
		return drop("unknown_sender")
	}
	if from.Blocked() {
		return drop("blocked")
	}
	if !s.reaches(ctx, pc.peer, from.ID) {
		// A direct link carries its two servers' messages and nobody
		// else's; a hub only those of servers on its roster.
		return drop("not_on_this_link")
	}
	plain, err := wire.OpenDirect(s.keys, s.id.Name, f, from.SignKey)
	if err != nil {
		return drop("unopened")
	}
	var m directMessage
	if err := json.Unmarshal(plain, &m); err != nil || m.Kind == "" || !wire.ValidID(m.MsgID) {
		return drop("malformed")
	}
	m.raw, m.from, m.peer = plain, from, pc.peer.ID
	s.hear(from.Name)
	// File chunks have their own limit, by bytes.
	if m.Kind != kBlobChunk {
		if r := s.limits.direct.Take(string(pc.peer.ID) + "|" + from.Name); !r.OK {
			e := wire.Errf(wire.ERateLimited, "Too many direct messages.")
			e.RetryAfter = r.RetryAfterSeconds()
			return e
		}
	}
	now := s.now()
	if d := now.Sub(m.At); d > directSkew || d < -directSkew {
		return drop("clock")
	}
	s.mu.Lock()
	_, replayed := s.seen[m.MsgID]
	if !replayed {
		s.seen[m.MsgID] = now
	}
	waiting := s.replies[m.Re]
	s.mu.Unlock()
	if replayed {
		return drop("replayed")
	}
	if waiting != nil && m.Re != "" && m.Kind != kBlobChunk {
		select {
		case waiting <- m:
		default:
		}
		// An answer that is only an answer ends here. One that carries
		// something to keep (a key, history) is also handled below.
		if m.Kind == kError || m.Kind == kCallPass {
			return nil
		}
	}
	switch m.Kind {
	case kOffer:
		s.onOffer(ctx, m)
	case kAccept:
		s.onAccept(ctx, m)
	case kDecline:
		s.onDecline(ctx, m)
	case kKey:
		s.onKey(ctx, m)
	case kKeyRequest:
		s.onKeyRequest(ctx, m)
	case kHistory:
		s.onHistory(ctx, m)
	case kHistoryRequest:
		s.onHistoryRequest(ctx, m)
	case kOpenRequest:
		s.onOpenRequest(ctx, m)
	case kOpened:
		// The answer to this server's own request; whoever asked has it.
	case kBlobRequest:
		s.onBlobRequest(ctx, m)
	case kBlobChunk:
		s.onBlobChunk(ctx, m)
	case kCallPassRequest:
		s.onCallPassRequest(ctx, m)
	case kCallSignal:
		s.onCallSignal(ctx, m)
	case kDeviceList:
		s.onDeviceList(ctx, m)
	case kDeviceListRequest:
		s.onDeviceListRequest(ctx, m)
	case kMovePropose:
		s.onMovePropose(ctx, m)
	case kMoveAccept:
		s.onMoveAccept(ctx, m)
	case kMoveDecline:
		s.onMoveDecline(ctx, m)
	case kMoveAbandon:
		s.onMoveAbandon(ctx, m)
	case kError, kCallPass:
		// Answers nobody waits for any more.
	default:
		// A kind this version does not know is ignored.
	}
	return nil
}

// ------------------------------------------------------------ stream keys

// keyPlace names a stream key's row for the sealing at rest.
func keyPlace(stream model.StreamID, epoch int64) string {
	return string(stream) + "/" + strconv.FormatInt(epoch, 10)
}

// putKey stores a stream key sealed at rest. The sealed value holds the 32
// bytes of the key and then the time of the reference clock at which its
// epoch began (0 while that is not known), since nothing else in the schema
// records when an epoch starts.
func (s *Service) putKey(ctx context.Context, a store.Areas, stream model.StreamID, epoch int64, key []byte, start int64) error {
	plain := make([]byte, 40)
	copy(plain, key)
	binary.BigEndian.PutUint64(plain[32:], uint64(start))
	sealed, err := s.keys.Seal(plain, "stream_keys", "key", keyPlace(stream, epoch))
	if err != nil {
		return err
	}
	return a.Streams().PutKey(ctx, stream, store.StreamKey{Epoch: epoch, Sealed: sealed, CreatedAt: s.now()})
}

// streamKey is an opened stream key.
type streamKey struct {
	epoch   int64
	key     []byte
	start   int64
	created model.Time
}

func (s *Service) openKey(stream model.StreamID, k store.StreamKey) (streamKey, error) {
	plain, err := s.keys.Unseal(k.Sealed, "stream_keys", "key", keyPlace(stream, k.Epoch))
	if err != nil || len(plain) != 40 {
		return streamKey{}, errors.New("link: a stream key could not be opened")
	}
	return streamKey{epoch: k.Epoch, key: plain[:32], start: int64(binary.BigEndian.Uint64(plain[32:])), created: k.CreatedAt}, nil
}

// keyOf returns the key of one epoch, or model.ErrNotFound.
func (s *Service) keyOf(ctx context.Context, a store.Areas, stream model.StreamID, epoch int64) (streamKey, error) {
	k, err := a.Streams().Key(ctx, stream, epoch)
	if err != nil {
		return streamKey{}, err
	}
	return s.openKey(stream, k)
}

// keys returns every key held for a stream, lowest epoch first.
func (s *Service) keysOf(ctx context.Context, a store.Areas, stream model.StreamID) ([]streamKey, error) {
	list, err := a.Streams().Keys(ctx, stream)
	if err != nil {
		return nil, err
	}
	out := make([]streamKey, 0, len(list))
	for _, k := range list {
		opened, err := s.openKey(stream, k)
		if err != nil {
			return nil, err
		}
		out = append(out, opened)
	}
	return out, nil
}

// sealingKey is the key this server encrypts under: that of the stream's
// current epoch as this server knows it, which is the newest one whose
// stream.servers_changed it has applied (section 10). A key it has been
// given for an epoch that has not started yet is not used until it has: the
// time reference would refuse the envelope as ahead of its epoch.
func (s *Service) sealingKey(ctx context.Context, a store.Areas, st store.Stream) (streamKey, error) {
	notYet := model.ErrUnavailable.With("This server has not been given the channel's key yet.")
	if st.KeyEpoch <= 0 {
		return streamKey{}, notYet
	}
	k, err := s.keyOf(ctx, a, st.ID, st.KeyEpoch)
	if errors.Is(err, model.ErrNotFound) {
		return streamKey{}, notYet
	}
	return k, err
}

// newEpoch makes the next stream key, as the owner does on every change to
// the server list, and sends it to every server in the list. A server that
// is not connected asks for it with key_request when it meets the epoch.
func (s *Service) newEpoch(ctx context.Context, st store.Stream) (int64, error) {
	keys, err := s.keysOf(ctx, s.store, st.ID)
	if err != nil {
		return 0, err
	}
	epoch := int64(1)
	if len(keys) > 0 {
		epoch = keys[len(keys)-1].epoch + 1
	}
	key := secret.RandomBytes(32)
	if err := s.putKey(ctx, s.store, st.ID, epoch, key, 0); err != nil {
		return 0, err
	}
	servers, err := s.store.Streams().Servers(ctx, st.ID)
	if err != nil {
		return 0, err
	}
	for _, ss := range servers {
		if ss.State != store.StreamServerActive || s.isSelf(ss.ServerID) {
			continue
		}
		s.sendKey(ctx, st, ss.ServerID, epoch, key, "")
	}
	return epoch, nil
}

// sendKey sends one epoch's key to one server, sealed to it.
func (s *Service) sendKey(ctx context.Context, st store.Stream, to model.ServerID, epoch int64, key []byte, re string) {
	sv, err := s.store.Peers().Server(ctx, to)
	if err != nil {
		return
	}
	s.sendDirect(ctx, st.PeerID, sv, kKey, re, map[string]any{"stream_id": st.ID, "epoch": epoch, "key": wire.Bytes(key),
		"version": st.ServersVersion, "history_days": st.HistoryDays})
}

// ownerOf returns the stream's owner server; for a stream this server owns,
// its own row.
func (s *Service) ownerOf(ctx context.Context, st store.Stream) (store.Server, error) {
	if st.Owned() {
		return s.self, nil
	}
	return s.store.Peers().Server(ctx, st.OwnerServerID)
}

// onKey stores a stream key. A key is accepted only from the stream's owner
// (section 10), and in a channel a hub owns only from the server the hub
// named to make that epoch's key (section 23a); from anyone else it changes
// nothing.
func (s *Service) onKey(ctx context.Context, m directMessage) {
	var body struct {
		StreamID    model.StreamID `json:"stream_id"`
		Epoch       int64          `json:"epoch"`
		Key         wire.Bytes     `json:"key"`
		HistoryDays int            `json:"history_days"`
	}
	if json.Unmarshal(m.raw, &body) != nil || len(body.Key) != 32 || body.Epoch < 1 {
		return
	}
	st, err := s.store.Streams().Get(ctx, body.StreamID)
	from := err == nil && !st.Owned() && st.OwnerServerID == m.from.ID
	if err == nil && hubOwned(st) {
		from = s.keyFrom(ctx, st, m.from, body.Epoch, m.Re != "")
	}
	if err != nil || !from || st.PeerID != m.peer {
		s.count(func(stats *Stats) { stats.Dropped["key_not_from_owner"]++ })
		if err == nil {
			s.auditRejected(ctx, st, m.from.Name, "key_not_from_owner", nil)
		}
		return
	}
	if st.State != store.StreamOffered && st.State != store.StreamActive {
		return
	}
	if _, err := s.keyOf(ctx, s.store, st.ID, body.Epoch); err == nil {
		return // already held; its start seq stays
	}
	if err := s.putKey(ctx, s.store, st.ID, body.Epoch, body.Key, 0); err != nil {
		s.log.Warn("link: a stream key could not be stored", "err", err)
		return
	}
	if hubOwned(st) && st.HistoryDays == 0 && body.HistoryDays > 0 && body.HistoryDays <= 90 {
		// How much history the channel gives a server that joins: this
		// server may be named key-maker one day and owe it to another.
		s.store.Streams().Update(ctx, st.ID, func(x *store.Stream) error {
			x.HistoryDays = body.HistoryDays
			return nil
		})
	}
	rt := s.rt(st.ID)
	rt.mu.Lock()
	close(rt.keyArrived)
	rt.keyArrived = make(chan struct{})
	rt.mu.Unlock()
	// What waited in the outbox for want of a key can go now.
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.kickStream(s.ctx, st.ID, true)
	}()
}

// onKeyRequest answers a server that met an epoch it has no key for. It
// gets the key if it was in the server list for that epoch, and an error
// otherwise.
func (s *Service) onKeyRequest(ctx context.Context, m directMessage) {
	var body struct {
		StreamID model.StreamID `json:"stream_id"`
		Epoch    int64          `json:"epoch"`
	}
	if json.Unmarshal(m.raw, &body) != nil {
		return
	}
	st, err := s.store.Streams().Get(ctx, body.StreamID)
	// The owner answers; in a channel a hub owns, any server that holds
	// the key does, under the same rule.
	if err != nil || !(st.Owned() || hubOwned(st)) || st.PeerID != m.peer || st.State != store.StreamActive {
		s.replyError(ctx, m, wire.EUnknownStream, "No such stream here.")
		return
	}
	servers, err := s.store.Streams().Servers(ctx, st.ID)
	if err != nil {
		return
	}
	var asker *store.StreamServer
	for i, ss := range servers {
		if ss.ServerID == m.from.ID && ss.State == store.StreamServerActive {
			asker = &servers[i]
		}
	}
	k, err := s.store.Streams().Key(ctx, st.ID, body.Epoch)
	// In the list for that epoch: in it now, and already in it when the
	// epoch's key was made. A new epoch starts on every change to the
	// list, so a key made before the server joined is one it never held.
	if asker == nil || err != nil || k.CreatedAt.Before(asker.JoinedAt) {
		s.replyError(ctx, m, wire.EForbidden, "That server was not in the stream for that epoch.")
		return
	}
	opened, err := s.openKey(st.ID, k)
	if err != nil {
		return
	}
	s.sendKey(ctx, st, m.from.ID, body.Epoch, opened.key, m.MsgID)
}
