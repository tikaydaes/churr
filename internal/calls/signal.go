package calls

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/pion/sdp/v3"
	"github.com/pion/webrtc/v4"

	"github.com/tikaydaes/churr/internal/live"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/perm"
)

// Signalling: the `call.` frames of docs/CALLS.md section 10. Requests come
// from a client with an `id`; each is answered with `call.ok` or
// `call.error` carrying it as `reply_to`, except that `call.offer` is
// answered with `call.answer`.

// Error codes of `call.error`.
const (
	errInvalidRequest   = "invalid_request"
	errTicketInvalid    = "ticket_invalid"
	errForbidden        = "forbidden"
	errNotInCall        = "not_in_call"
	errCallFull         = "call_full"
	errCamerasFull      = "cameras_full"
	errScreenInUse      = "screen_in_use"
	errTooManySubs      = "too_many_subscriptions"
	errFrameEncryption  = "frame_encryption_required"
	errHostUnreachable  = "host_unreachable"
	errFeatureOff       = "feature_off"
	errInternal         = "internal"
	frameDisconnected   = "call.disconnected"
	frameJoin           = "call.join"
	frameResume         = "call.resume"
	frameDecline        = "call.decline"
	frameLeave          = "call.leave"
	frameOffer          = "call.offer"
	frameAnswer         = "call.answer"
	frameCandidate      = "call.candidate"
	frameSubscribe      = "call.subscribe"
	frameMute           = "call.mute"
	frameRestart        = "call.restart"
	pcSend, pcRecv      = "send", "recv"
	maxSDP              = 256 << 10
	maxTracksPerPublish = 3
)

// caller is who sent a signalling frame: a client on one of this server's
// live connections, or a member of another server whose own server relays
// for it.
type caller struct {
	account model.AccountID
	// session and conn are set for a local client.
	session model.SessionID
	conn    string
	// server is the relaying server's name, for a remote one.
	server string
	out    sink
}

// request is one frame being answered.
type request struct {
	c  caller
	id string
}

func (q request) ok(fields map[string]any) {
	if fields == nil {
		fields = map[string]any{}
	}
	fields["type"] = "call.ok"
	q.answer(fields)
}

func (q request) answer(fields map[string]any) {
	if q.id != "" {
		fields["reply_to"] = q.id
	}
	q.c.out.send(fields)
}

func (q request) fail(code, message string, extra ...map[string]any) {
	f := map[string]any{"type": "call.error", "code": code, "message": message}
	for _, e := range extra {
		for k, v := range e {
			f[k] = v
		}
	}
	q.answer(f)
}

// strict decodes a frame, refusing fields the frame does not have.
func strict(data []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

// HandleClient receives the `call.` frames of a live connection. It is
// registered with the hub by server; it always answers the frame itself.
func (s *Server) HandleClient(ctx context.Context, from live.Client, frameType string, frame json.RawMessage) error {
	c := caller{account: from.Account.ID, session: from.Session.ID, conn: from.ConnectionID,
		out: localSink{hub: s.hub, account: from.Account.ID, conn: from.ConnectionID}}
	var head struct {
		ID     string `json:"id"`
		CallID string `json:"call_id"`
	}
	json.Unmarshal(frame, &head)
	q := request{c: c, id: head.ID}
	if frameType == frameDisconnected {
		q.fail(errInvalidRequest, "That frame type is not known.")
		return live.ErrAnswered
	}
	// Which call: the one the frame names, or the one this connection is
	// the signalling connection of.
	var lc *liveCall
	switch frameType {
	case frameJoin, frameResume, frameDecline, frameLeave:
		if !model.ValidID(head.CallID) {
			q.fail(errInvalidRequest, "`call_id` is missing or is not an identifier.")
			return live.ErrAnswered
		}
		lc = s.callByID(model.CallID(head.CallID))
	default:
		s.mu.Lock()
		lc = s.conns[c.conn]
		s.mu.Unlock()
	}
	switch {
	case frameType == frameDecline:
		// Ringing is this server's own, whoever carries the call.
		var f struct {
			Type, ID string
			CallID   string `json:"call_id"`
		}
		if strict(frame, &f) != nil {
			q.fail(errInvalidRequest, "`call.decline` takes `call_id`.")
		} else {
			if lc != nil {
				s.decline(lc, c.account, c.conn)
			}
			q.ok(nil)
		}
	case lc == nil && (frameType == frameJoin || frameType == frameResume):
		q.fail(errTicketInvalid, "That call is not in progress. Ask to join again.")
	case lc == nil:
		q.fail(errNotInCall, "You are not in this call.")
	case lc.away != nil:
		s.relay(ctx, lc, q, frameType, frame)
	default:
		s.process(ctx, lc, q, frameType, frame)
	}
	return live.ErrAnswered
}

// ConnectionClosed is told by the hub that a live connection has gone. If
// it was a participant's signalling connection, the participant is held:
// media may still be flowing, and the client has thirty seconds to come
// back with `call.resume`.
func (s *Server) ConnectionClosed(from live.Client) {
	s.mu.Lock()
	lc := s.conns[from.ConnectionID]
	delete(s.conns, from.ConnectionID)
	s.mu.Unlock()
	if lc == nil {
		return
	}
	if lc.away != nil {
		s.relayLost(lc, from.Account.ID, from.ConnectionID)
		return
	}
	r := lc.room
	r.mu.Lock()
	if p := r.parts[from.Account.ID]; p != nil && p.conn == from.ConnectionID && p.remote == "" {
		p.sigLostAt = time.Now()
		p.setOut(nil)
	}
	r.mu.Unlock()
}

// bind makes a connection the signalling connection of a call.
func (s *Server) bind(conn string, lc *liveCall) {
	if conn == "" {
		return
	}
	s.mu.Lock()
	s.conns[conn] = lc
	s.mu.Unlock()
}

func (s *Server) unbind(conn string, lc *liveCall) {
	if conn == "" {
		return
	}
	s.mu.Lock()
	if s.conns[conn] == lc {
		delete(s.conns, conn)
	}
	s.mu.Unlock()
}

// process handles one frame for a call this server carries.
func (s *Server) process(ctx context.Context, lc *liveCall, q request, frameType string, frame json.RawMessage) {
	switch frameType {
	case frameJoin:
		s.sigJoin(ctx, lc, q, frame)
		return
	case frameResume:
		s.sigResume(ctx, lc, q, frame)
		return
	}
	p := lc.room.get(q.c.account)
	if p != nil {
		lc.room.mu.Lock()
		mine := (q.c.server == "" && p.remote == "" && p.conn == q.c.conn) || (q.c.server != "" && p.remote == q.c.server)
		lc.room.mu.Unlock()
		if !mine {
			p = nil
		}
	}
	if p == nil {
		q.fail(errNotInCall, "You are not in this call.")
		return
	}
	switch frameType {
	case frameLeave:
		s.unbind(q.c.conn, lc)
		// Answer first: once the participant is out, it has no connection
		// to answer on.
		q.ok(nil)
		s.removeParticipant(ctx, lc, p.id, LeftLeft)
	case frameOffer:
		s.sigOffer(ctx, lc, p, q, frame)
	case frameAnswer:
		s.sigAnswer(p, q, frame)
	case frameCandidate:
		s.sigCandidate(q, frame)
	case frameSubscribe:
		s.sigSubscribe(lc, p, q, frame)
	case frameMute:
		s.sigMute(ctx, lc, p, q, frame)
	case frameRestart:
		s.sigRestart(p, q, frame)
	case frameDisconnected:
		// Only a relaying server sends this: its client's connection went.
		lc.room.mu.Lock()
		p.sigLostAt = time.Now()
		lc.room.mu.Unlock()
	default:
		q.fail(errInvalidRequest, "That frame type is not known.")
	}
}

// joined is the answer to `call.join` and `call.resume`.
func (s *Server) joined(ctx context.Context, lc *liveCall, token string) map[string]any {
	people, cameras := s.limits(ctx)
	layers := map[string]int{}
	for _, l := range cameraLayers {
		layers[l.name] = l.kbps
	}
	return map[string]any{
		"call": lc.object(true), "resume_token": token,
		"limits": map[string]any{"max_participants": people, "max_cameras": cameras, "max_subscriptions": maxSubscriptions,
			"audio_kbps": audioKbps, "screen_kbps": screenKbps, "layers": layers},
	}
}

// allowed checks again that the caller may be in the call: a ticket may be
// a minute old.
func (s *Server) allowed(ctx context.Context, lc *liveCall, c caller) (string, string) {
	acc, err := s.store.Accounts().Get(ctx, c.account)
	if err != nil {
		return errForbidden, "You may no longer join this call."
	}
	if c.server != "" {
		if acc.ServerName != c.server {
			return errForbidden, "You may no longer join this call."
		}
		err = s.perm.CanJoinCallRemote(ctx, acc, lc.conv)
	} else {
		_, err = s.perm.Can(ctx, acc, perm.StartCall, lc.conv)
	}
	switch {
	case err == nil:
		return "", ""
	case errors.Is(err, model.ErrFeatureOff):
		return errFeatureOff, "Calls are switched off on this server."
	default:
		if _, known := model.AsError(err); !known {
			s.log.Error("permission to join a call could not be checked", "err", err)
			return errInternal, "The server could not check that."
		}
		return errForbidden, "You may no longer join this call."
	}
}

func (s *Server) sigJoin(ctx context.Context, lc *liveCall, q request, frame json.RawMessage) {
	var f struct {
		Type, ID string
		CallID   string `json:"call_id"`
		Ticket   string `json:"ticket"`
		Client   *struct {
			Simulcast       bool     `json:"simulcast"`
			VideoCodecs     []string `json:"video_codecs"`
			FrameEncryption bool     `json:"frame_encryption"`
		} `json:"client"`
	}
	if strict(frame, &f) != nil || f.Ticket == "" {
		q.fail(errInvalidRequest, "`call.join` takes `call_id`, `ticket` and `client`.")
		return
	}
	c := q.c
	tk, ok := s.tickets.use(f.Ticket, time.Now())
	if !ok || tk.call != lc.id || tk.account != c.account || tk.server != c.server || (c.server == "" && tk.session != c.session) {
		q.fail(errTicketInvalid, "That ticket is unknown, used or too old. Ask to join again.")
		return
	}
	if code, msg := s.allowed(ctx, lc, c); code != "" {
		q.fail(code, msg)
		return
	}
	caps := clientCaps{codecs: map[string]bool{}}
	if f.Client != nil {
		caps.simulcast, caps.frameEncryption = f.Client.Simulcast, f.Client.FrameEncryption
		for _, name := range f.Client.VideoCodecs {
			if name == codecVP8 || name == codecH264 {
				caps.codecs[name] = true
			}
		}
	}
	if len(caps.codecs) == 0 {
		// VP8 is the one every client can decode.
		caps.codecs[codecVP8] = true
	}
	if lc.sealed && !caps.frameEncryption {
		q.fail(errFrameEncryption, "This is a sealed chat's call, and this client did not say it can encrypt media frames.")
		return
	}
	r := lc.room
	token := newSecret()
	now := model.Now()
	p := &participant{room: r, id: c.account, device: s.deviceOf(ctx, c), joinedAt: now, joined: time.Now(), caps: caps, remote: c.server, session: c.session, conn: c.conn,
		resume: hashSecret(token), tracks: map[string]*pubTrack{}, picker: newPicker(), shown: map[string]choice{}, downs: map[string]*downTrack{},
		kick: make(chan struct{}, 1), stop: make(chan struct{})}
	p.setOut(c.out)

	if old := r.get(c.account); old != nil {
		// One device per account: the call moves here. The first device
		// is told it left, and drops its media.
		s.move(lc, old, p)
	} else {
		people, _ := s.limits(ctx)
		if r.full(c.account, people) {
			q.fail(errCallFull, "The call is full.")
			return
		}
		if _, err := s.store.Calls().Join(ctx, lc.id, c.account, now); err != nil {
			s.log.Error("a participant could not be stored", "call", lc.id, "err", err)
			q.fail(errInternal, "The server could not add you to the call.")
			return
		}
		if !r.add(p) {
			s.store.Calls().Leave(ctx, lc.id, c.account, now)
			q.fail(errTicketInvalid, "That call has ended.")
			return
		}
		s.mark(c.account, 1)
		who := model.CallParticipant{AccountID: c.account, DeviceID: p.device, JoinedAt: now}
		s.hub.Publish(ctx, model.Event{Type: EventParticipantJoined, At: now, ConversationID: lc.conv,
			Payload: map[string]any{"call_id": lc.id, "participant": who}})
		// The other servers of a shared conversation are told who joined,
		// not on which device: the link's call events are as they were.
		s.submit(ctx, lc, StreamEvent{Type: EventParticipantJoined, At: now, CallID: lc.id, AccountID: c.account,
			Participant: &model.CallParticipant{AccountID: c.account, JoinedAt: now}})
		// The person's other devices stop ringing.
		s.stopRings(lc.id, c.account, RingJoinedElsewhere, c.conn)
	}
	s.bind(c.conn, lc)
	q.ok(s.joined(ctx, lc, token))
	r.kickAll()
}

// deviceOf finds the device a local caller's session belongs to. In a
// sealed chat's call the others need it to pick the key that decrypts this
// participant's frames (docs/CALL-FRAMES.md section 1). "" when the session
// has no device, and for a caller another server relays.
func (s *Server) deviceOf(ctx context.Context, c caller) model.DeviceID {
	if c.server != "" || c.session == "" {
		return ""
	}
	d, found, err := s.store.Devices().BySession(ctx, c.session)
	if err != nil {
		s.log.Warn("the device of a call participant could not be read", "err", err)
		return ""
	}
	if !found {
		return ""
	}
	return d.ID
}

// withDevice adds `device_id` to a frame's fields when the device is known.
func withDevice(fields map[string]any, device model.DeviceID) map[string]any {
	if device != "" {
		fields["device_id"] = device
	}
	return fields
}

// move puts a participant's call on another device: `call.join` came from a
// second device of an account already in the call.
func (s *Server) move(lc *liveCall, old, fresh *participant) {
	r := lc.room
	if old.remote == "" {
		// A relaying server tells its own client's first device itself:
		// by now the host's frames for the account go to the second.
		old.send(event(EventParticipantLeft, lc, map[string]any{"account_id": old.id, "reason": LeftJoinedElsewhere}))
	}
	r.mu.Lock()
	oldConn := old.conn
	old.gone = true
	fresh.joinedAt = old.joinedAt
	gone := r.dropTracks(old)
	r.parts[fresh.id] = fresh
	go fresh.run()
	r.mu.Unlock()
	s.unbind(oldConn, lc)
	old.shut()
	r.speakers.forget(old.id)
	for _, t := range gone {
		t.close()
		r.broadcast(event(EventTrackUnpublished, lc, map[string]any{"account_id": old.id, "track_id": t.id}), old.id)
	}
}

func (s *Server) sigResume(ctx context.Context, lc *liveCall, q request, frame json.RawMessage) {
	var f struct {
		Type, ID    string
		CallID      string `json:"call_id"`
		ResumeToken string `json:"resume_token"`
	}
	if strict(frame, &f) != nil || f.ResumeToken == "" {
		q.fail(errInvalidRequest, "`call.resume` takes `call_id` and `resume_token`.")
		return
	}
	c := q.c
	r := lc.room
	token := newSecret()
	r.mu.Lock()
	p := r.parts[c.account]
	if p == nil || p.resume != hashSecret(f.ResumeToken) || p.remote != c.server {
		r.mu.Unlock()
		q.fail(errTicketInvalid, "That resume token is unknown or used. Ask to join again.")
		return
	}
	oldConn := p.conn
	p.conn, p.session, p.resume, p.sigLostAt = c.conn, c.session, hashSecret(token), time.Time{}
	p.setOut(c.out)
	r.mu.Unlock()
	if oldConn != c.conn {
		s.unbind(oldConn, lc)
	}
	s.bind(c.conn, lc)
	q.ok(s.joined(ctx, lc, token))
	// An offer that was waiting for its answer is sent again: the old
	// connection may have dropped it.
	p.pcMu.Lock()
	if p.awaiting && p.lastOffer != nil {
		p.send(p.lastOffer)
	}
	p.pcMu.Unlock()
	p.wake()
}

// offered is one media line of a client's offer on `send`.
type offered struct {
	mid    string
	kind   string
	rids   []string
	source string
	width  int
	height int
}

// mediaLines reads the media lines of an SDP: each one's mid, kind, the
// rids it sends, and whether it was rejected.
func mediaLines(text string) (map[string]offered, map[string]string, error) {
	var d sdp.SessionDescription
	if err := d.UnmarshalString(text); err != nil {
		return nil, nil, err
	}
	lines := map[string]offered{}
	codecs := map[string]string{}
	for _, m := range d.MediaDescriptions {
		mid, ok := m.Attribute("mid")
		if !ok {
			continue
		}
		o := offered{mid: mid, kind: m.MediaName.Media}
		for _, a := range m.Attributes {
			if a.Key == "rid" {
				if parts := strings.Fields(a.Value); len(parts) >= 2 && parts[1] == "send" {
					o.rids = append(o.rids, parts[0])
				}
			}
		}
		lines[mid] = o
		// The codec a line settled on is its first format, when the
		// line was not rejected.
		if m.MediaName.Port.Value != 0 && len(m.MediaName.Formats) > 0 {
			for _, a := range m.Attributes {
				if a.Key == "rtpmap" && strings.HasPrefix(a.Value, m.MediaName.Formats[0]+" ") {
					name := strings.TrimPrefix(a.Value, m.MediaName.Formats[0]+" ")
					if i := strings.IndexByte(name, '/'); i >= 0 {
						name = name[:i]
					}
					codecs[mid] = strings.ToLower(name)
				}
			}
		}
	}
	return lines, codecs, nil
}

func (s *Server) sigOffer(ctx context.Context, lc *liveCall, p *participant, q request, frame json.RawMessage) {
	var f struct {
		Type, ID   string
		PC         string `json:"pc"`
		SDP        string `json:"sdp"`
		ICERestart bool   `json:"ice_restart"`
		Tracks     []struct {
			Mid    string `json:"mid"`
			Source string `json:"source"`
			Width  int    `json:"width"`
			Height int    `json:"height"`
		} `json:"tracks"`
	}
	if strict(frame, &f) != nil || f.PC != pcSend || f.SDP == "" || len(f.SDP) > maxSDP {
		q.fail(errInvalidRequest, "`call.offer` takes `pc`: `send`, `sdp`, `ice_restart` and `tracks`.")
		return
	}
	if len(f.Tracks) > maxTracksPerPublish {
		q.fail(errInvalidRequest, "A participant publishes a microphone, a camera and a screen at most.")
		return
	}
	text := withoutCandidates(f.SDP)
	lines, _, err := mediaLines(text)
	if err != nil {
		q.fail(errInvalidRequest, "That SDP could not be read.")
		return
	}
	// What the client says each sending line is.
	var list []offered
	sources := map[string]bool{}
	for _, t := range f.Tracks {
		o, known := lines[t.Mid]
		kind := kindVideo
		if t.Source == sourceMicrophone {
			kind = kindAudio
		}
		switch {
		case t.Source != sourceMicrophone && t.Source != sourceCamera && t.Source != sourceScreen:
			q.fail(errInvalidRequest, "`source` is `microphone`, `camera` or `screen`.")
			return
		case sources[t.Source]:
			q.fail(errInvalidRequest, "Each source can be published once.")
			return
		case !known || o.kind != kind:
			q.fail(errInvalidRequest, "`tracks` names a media line the SDP does not have, or one of the wrong kind.")
			return
		case t.Width < 0 || t.Height < 0 || t.Width > 16384 || t.Height > 16384:
			q.fail(errInvalidRequest, "`width` and `height` are sizes in pixels.")
			return
		case t.Source == sourceScreen && len(o.rids) > 0:
			q.fail(errInvalidRequest, "A screen is published as a single layer.")
			return
		}
		for _, rid := range o.rids {
			if layerIndex(rid) < 0 {
				q.fail(errInvalidRequest, "Simulcast layers are named `q`, `h` and `f`.")
				return
			}
		}
		sources[t.Source] = true
		o.source, o.width, o.height = t.Source, t.Width, t.Height
		list = append(list, o)
	}
	// The limits on what is on at once (docs/CALLS.md section 12).
	_, cameras := s.limits(ctx)
	r := lc.room
	r.mu.Lock()
	on := 0
	var sharing model.AccountID
	for _, t := range r.tracks {
		if t.source == sourceCamera && !t.muted.Load() && t.owner != p {
			on++
		}
		if t.source == sourceScreen && t.owner != p {
			sharing = t.owner.id
		}
	}
	var newCamera, newScreen bool
	for _, o := range list {
		if _, had := p.tracks[o.mid]; !had {
			newCamera = newCamera || o.source == sourceCamera
			newScreen = newScreen || o.source == sourceScreen
		}
	}
	r.mu.Unlock()
	if newCamera && on >= cameras {
		q.fail(errCamerasFull, "The most cameras allowed are already on. Your microphone still works.")
		return
	}
	if newScreen && sharing != "" {
		q.fail(errScreenInUse, "Someone else is sharing a screen.", map[string]any{"account_id": sharing})
		return
	}

	p.pcMu.Lock()
	if p.ended {
		// The participant left while the offer was being looked at.
		p.pcMu.Unlock()
		q.fail(errNotInCall, "You are not in this call.")
		return
	}
	if p.sendPC == nil {
		pc, err := s.media.newSend()
		if err != nil {
			p.pcMu.Unlock()
			s.log.Error("a media connection could not be made", "err", err)
			q.fail(errInternal, "The media server failed.")
			return
		}
		p.sendPC = pc
		p.sendRef.Store(pc)
		p.watchConnection(pc)
		pc.OnTrack(func(remote *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) { p.incoming(pc, remote, receiver) })
	}
	pc := p.sendPC
	var answer string
	err = pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: text})
	if err == nil {
		var desc webrtc.SessionDescription
		if desc, err = pc.CreateAnswer(nil); err == nil {
			answer, err = s.media.describe(ctx, pc, desc)
		}
	}
	p.pcMu.Unlock()
	if err != nil {
		q.fail(errInvalidRequest, "The server cannot use that SDP.")
		return
	}
	_, codecs, _ := mediaLines(answer)

	// Bring the participant's published tracks in line with the offer: a
	// line that is new is published; one that was there before and is
	// missing now has been unpublished.
	var added, removed []*pubTrack
	ids := []map[string]any{}
	r.mu.Lock()
	keep := map[string]bool{}
	for _, o := range list {
		codec := codecs[o.mid]
		if codec == "" {
			continue // the line was refused: no codec in common
		}
		keep[o.mid] = true
		t := p.tracks[o.mid]
		if t == nil {
			r.nextTrack++
			t = &pubTrack{id: "t" + itoa(r.nextTrack), seq: r.nextTrack, owner: p, mid: o.mid, source: o.source, kind: o.kind, codec: codec}
			switch {
			case len(o.rids) > 0:
				t.named = true
				for i, spec := range cameraLayers {
					for _, rid := range o.rids {
						if rid == spec.name {
							t.layers = append(t.layers, &pubLayer{spec: option{name: spec.name, height: spec.height, kbps: spec.kbps}, kept: newKeptPackets(spec.kbps)})
							_ = i
						}
					}
				}
			case o.source == sourceScreen:
				t.layers = []*pubLayer{{spec: option{name: layerF, height: 1080, kbps: screenKbps}, kept: newKeptPackets(screenKbps)}}
			case o.source == sourceCamera:
				t.layers = []*pubLayer{{spec: option{name: layerF, height: 720, kbps: cameraLayers[2].kbps}, kept: newKeptPackets(cameraLayers[2].kbps)}}
			default:
				t.layers = []*pubLayer{{spec: option{kbps: audioKbps}}}
			}
			p.tracks[o.mid], r.tracks[t.id] = t, t
			added = append(added, t)
		}
		t.width, t.height = o.width, o.height
		ids = append(ids, map[string]any{"mid": o.mid, "track_id": t.id})
	}
	for mid, t := range p.tracks {
		if !keep[mid] {
			delete(p.tracks, mid)
			delete(r.tracks, t.id)
			removed = append(removed, t)
		}
	}
	r.mu.Unlock()

	q.answer(map[string]any{"type": frameAnswer, "pc": pcSend, "sdp": answer, "tracks": ids})
	for _, t := range removed {
		t.close()
		if t.source == sourceMicrophone {
			r.speakers.forget(p.id)
		}
		r.broadcast(event(EventTrackUnpublished, lc, map[string]any{"account_id": p.id, "track_id": t.id}), "")
	}
	for _, t := range added {
		r.broadcast(event(EventTrackPublished, lc, withDevice(map[string]any{"account_id": p.id, "track": t.info()}, p.device)), "")
	}
	if len(added)+len(removed) > 0 {
		r.kickAll()
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// incoming is called by Pion when the first packet of a published stream
// arrives: one for a microphone or a screen, one per layer for a camera
// sent in several sizes.
func (p *participant) incoming(pc *webrtc.PeerConnection, remote *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
	mid := ""
	for _, tr := range pc.GetTransceivers() {
		if tr.Receiver() == receiver {
			mid = tr.Mid()
		}
	}
	// The offer that announced this line is being answered on another
	// goroutine; the track is known a moment later at most.
	var t *pubTrack
	for i := 0; i < 100 && t == nil; i++ {
		p.room.mu.Lock()
		t = p.tracks[mid]
		gone := p.gone
		p.room.mu.Unlock()
		if gone {
			return
		}
		if t == nil {
			time.Sleep(20 * time.Millisecond)
		}
	}
	// Reading the receiver's reports is what lets the server ask a
	// publisher again for packets that were lost on the way in.
	rid := remote.RID()
	go func() {
		for {
			var err error
			if rid == "" {
				_, _, err = receiver.ReadRTCP()
			} else {
				// A camera sent in several sizes has reports per size.
				_, _, err = receiver.ReadSimulcastRTCP(rid)
			}
			if err != nil {
				return
			}
		}
	}()
	level := -1
	if t != nil {
		level = t.level(rid)
	}
	if level < 0 {
		// A stream nobody announced. Read it so the connection does not
		// back up, and forward nothing.
		buf := make([]byte, 1500)
		for {
			if _, _, err := remote.Read(buf); err != nil {
				return
			}
		}
	}
	var audioLevel uint8
	for _, ext := range receiver.GetParameters().HeaderExtensions {
		if ext.URI == audioLevelURI {
			audioLevel = uint8(ext.ID)
		}
	}
	// A subscriber waiting for this layer can have its key frame now.
	t.askKey(level)
	t.receive(level, remote, audioLevel)
}

func (s *Server) sigAnswer(p *participant, q request, frame json.RawMessage) {
	var f struct {
		Type, ID string
		PC       string `json:"pc"`
		OfferID  int    `json:"offer_id"`
		SDP      string `json:"sdp"`
	}
	if strict(frame, &f) != nil || f.PC != pcRecv || f.SDP == "" || len(f.SDP) > maxSDP {
		q.fail(errInvalidRequest, "`call.answer` takes `pc`: `recv`, `offer_id` and `sdp`.")
		return
	}
	p.pcMu.Lock()
	if !p.awaiting || f.OfferID != p.offerID || p.recvPC == nil {
		p.pcMu.Unlock()
		q.fail(errInvalidRequest, "That answers no offer the server is waiting on.")
		return
	}
	err := p.recvPC.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: withoutCandidates(f.SDP)})
	if err == nil {
		p.awaiting, p.settled = false, p.offered
	}
	p.pcMu.Unlock()
	if err != nil {
		q.fail(errInvalidRequest, "The server cannot use that SDP.")
		return
	}
	q.ok(nil)
	// Changes that happened while the offer was out go into the next one.
	p.wake()
}

func (s *Server) sigCandidate(q request, frame json.RawMessage) {
	var f struct {
		Type, ID      string
		PC            string  `json:"pc"`
		Candidate     *string `json:"candidate"`
		SDPMid        *string `json:"sdp_mid"`
		SDPMLineIndex *int    `json:"sdp_mline_index"`
	}
	if strict(frame, &f) != nil || (f.PC != pcSend && f.PC != pcRecv) || (f.Candidate != nil && len(*f.Candidate) > 1024) {
		q.fail(errInvalidRequest, "`call.candidate` takes `pc`, `candidate`, `sdp_mid` and `sdp_mline_index`.")
		return
	}
	// Taken and not used. As an ICE-lite agent the server learns the
	// working path from the client's own checks, and never sends a
	// packet to an address a client named.
	q.ok(nil)
}

func (s *Server) sigSubscribe(lc *liveCall, p *participant, q request, frame json.RawMessage) {
	var f struct {
		Type, ID string
		Video    []struct {
			TrackID string `json:"track_id"`
			Width   int    `json:"width"`
			Height  int    `json:"height"`
		} `json:"video"`
	}
	if strict(frame, &f) != nil {
		q.fail(errInvalidRequest, "`call.subscribe` takes `video`: a list of `track_id`, `width` and `height`.")
		return
	}
	if len(f.Video) > maxSubscriptions {
		q.fail(errTooManySubs, "A client can show sixteen video tracks at most.")
		return
	}
	wants := make([]subscription, 0, len(f.Video))
	seen := map[string]bool{}
	for _, v := range f.Video {
		if v.TrackID == "" || len(v.TrackID) > 32 || seen[v.TrackID] || v.Width < 0 || v.Height < 0 || v.Width > 16384 || v.Height > 16384 {
			q.fail(errInvalidRequest, "Each entry of `video` names a track once, with a size in pixels.")
			return
		}
		seen[v.TrackID] = true
		wants = append(wants, subscription{track: v.TrackID, width: v.Width, height: v.Height})
	}
	estimate := p.estimate()
	r := lc.room
	r.mu.Lock()
	audio := 0
	for id, o := range r.parts {
		if id != p.id {
			for _, t := range o.tracks {
				if t.kind == kindAudio {
					audio++
				}
			}
		}
	}
	p.wants = wants
	choices, _ := p.choose(time.Now(), estimate, audio)
	// What the answer says is what the client has been told: only later
	// changes are sent as call.layer_changed.
	p.shown = choices
	r.mu.Unlock()
	video := make([]map[string]any, 0, len(wants))
	for _, w := range wants {
		c := choices[w.track]
		v := map[string]any{"track_id": w.track, "state": c.state}
		if c.layer != "" {
			v["layer"] = c.layer
		}
		video = append(video, v)
	}
	q.ok(map[string]any{"video": video})
	p.wake()
}

func (s *Server) sigMute(ctx context.Context, lc *liveCall, p *participant, q request, frame json.RawMessage) {
	var f struct {
		Type, ID string
		Source   string `json:"source"`
		Muted    *bool  `json:"muted"`
	}
	if strict(frame, &f) != nil || f.Muted == nil {
		q.fail(errInvalidRequest, "`call.mute` takes `source` and `muted`.")
		return
	}
	_, cameras := s.limits(ctx)
	r := lc.room
	r.mu.Lock()
	var t *pubTrack
	on := 0
	for _, x := range r.tracks {
		if x.owner == p && x.source == f.Source {
			t = x
		} else if x.source == sourceCamera && !x.muted.Load() {
			on++
		}
	}
	r.mu.Unlock()
	if t == nil {
		q.fail(errInvalidRequest, "You are not publishing that.")
		return
	}
	if !*f.Muted && t.source == sourceCamera && t.muted.Load() && on >= cameras {
		q.fail(errCamerasFull, "The most cameras allowed are already on.")
		return
	}
	changed := t.muted.Swap(*f.Muted) != *f.Muted
	q.ok(nil)
	if !changed {
		return
	}
	if *f.Muted && t.source == sourceMicrophone {
		r.speakers.forget(p.id)
	}
	r.broadcast(event(EventMuteChanged, lc, map[string]any{"account_id": p.id, "track_id": t.id, "source": t.source, "muted": *f.Muted}), "")
	r.kickAll()
}

func (s *Server) sigRestart(p *participant, q request, frame json.RawMessage) {
	var f struct {
		Type, ID string
		PC       string `json:"pc"`
	}
	if strict(frame, &f) != nil || f.PC != pcRecv {
		q.fail(errInvalidRequest, "`call.restart` takes `pc`: `recv`.")
		return
	}
	p.pcMu.Lock()
	p.restart, p.needOffer = true, true
	if p.awaiting && p.lastOffer != nil {
		// One offer at a time: the one that is out is sent again, and
		// the restart goes into the next.
		p.send(p.lastOffer)
	}
	p.pcMu.Unlock()
	q.ok(nil)
	p.wake()
}
