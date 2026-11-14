package calls

import (
	"context"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pion/interceptor/pkg/cc"
	"github.com/pion/rtcp"
	"github.com/pion/webrtc/v4"

	"github.com/tikaydaes/churr/internal/model"
)

// The three sources of a track (docs/CALLS.md section 1).
const (
	sourceMicrophone = "microphone"
	sourceCamera     = "camera"
	sourceScreen     = "screen"

	kindAudio = "audio"
	kindVideo = "video"
)

// trackInfo is a track as the API shows it (schema CallTrack).
type trackInfo struct {
	ID     string   `json:"id"`
	Source string   `json:"source"`
	Kind   string   `json:"kind"`
	Muted  bool     `json:"muted"`
	Codec  string   `json:"codec,omitempty"`
	Layers []string `json:"layers,omitempty"`
	Width  int      `json:"width,omitempty"`
	Height int      `json:"height,omitempty"`
}

// sink is where a participant's signalling goes: its own live connection,
// or, for someone on another server, the link to that server.
type sink interface {
	send(v any)
}

// localSink is one live connection of this server.
type localSink struct {
	hub     Hub
	account model.AccountID
	conn    string
}

func (l localSink) send(v any) { l.hub.SendTo(l.account, l.conn, v) }

// clientCaps is what a client said of itself in `call.join`.
type clientCaps struct {
	simulcast       bool
	codecs          map[string]bool
	frameEncryption bool
}

// subscription is one entry of a client's `call.subscribe` list.
type subscription struct {
	track  string
	width  int
	height int
}

// room is a live call's media state, in memory only: its participants,
// their published tracks, and who is forwarded what.
type room struct {
	s    *Server
	call *liveCall

	mu        sync.Mutex
	parts     map[model.AccountID]*participant
	tracks    map[string]*pubTrack
	nextTrack int
	// aloneSince is when the call came to have exactly one participant.
	aloneSince time.Time
	closed     bool

	speakers *speakers
	// forwarded counts the RTP packets written to subscribers.
	forwarded atomic.Uint64
}

func newRoom(s *Server, lc *liveCall) *room {
	return &room{s: s, call: lc, parts: map[model.AccountID]*participant{}, tracks: map[string]*pubTrack{}, speakers: newSpeakers()}
}

// participant is an account in a call, on one device.
type participant struct {
	room     *room
	id       model.AccountID
	joinedAt model.Time
	caps     clientCaps
	// remote is the name of the server that relays this participant's
	// signalling, or "" for one of this server's own.
	remote string
	// device is the device the account is in the call on, or "" when the
	// server does not know it (a session with no device row, or a
	// participant another server relays). It never changes: a call that
	// moves to another device gets a new participant.
	device model.DeviceID

	// out is where the participant's signalling goes; nil while it has
	// no live connection. It has its own lock so anything may send.
	outMu sync.Mutex
	out   sink

	// Guarded by room.mu.
	session     model.SessionID
	conn        string
	resume      [32]byte
	joined      time.Time
	lateAt      time.Time
	sigLostAt   time.Time
	mediaLostAt time.Time
	mediaSeen   bool
	tracks      map[string]*pubTrack // by mid
	wants       []subscription
	picker      *picker
	shown       map[string]choice
	gone        bool

	// The two media connections and what is forwarded on recv. Guarded
	// by pcMu. Lock order: pcMu before room.mu, never the reverse.
	pcMu      sync.Mutex
	sendPC    *webrtc.PeerConnection
	recvPC    *webrtc.PeerConnection
	downs     map[string]*downTrack
	offerID   int
	offeredAt time.Time
	awaiting  bool
	lastOffer map[string]any
	needOffer bool
	restart   bool
	// removals counts tracks taken off the recv connection; offered and
	// settled are its value at the last offer and at the last answer. A
	// removed track's media line can be used again only once its removal
	// has been through an offer and an answer.
	removals, offered, settled int
	// sendRef and estimator are read without pcMu, from other
	// participants' workers and from the forwarding loops.
	sendRef   atomic.Pointer[webrtc.PeerConnection]
	estimator atomic.Pointer[cc.BandwidthEstimator]

	// link follows what the downlink can take; sentBytes counts what has
	// been written to it.
	linkMu    sync.Mutex
	link      downlink
	sentBytes atomic.Int64
	// estimateOverride replaces the downlink estimate, in tests.
	estimateOverride atomic.Int64
	connected        atomic.Int32

	kick chan struct{}
	stop chan struct{}
	once sync.Once
}

func (p *participant) send(v any) {
	p.outMu.Lock()
	out := p.out
	p.outMu.Unlock()
	if out != nil {
		out.send(v)
	}
}

// setOut changes where the participant's signalling goes.
func (p *participant) setOut(out sink) {
	p.outMu.Lock()
	p.out = out
	p.outMu.Unlock()
}

// estimate is what the subscriber's downlink is taken to carry, in bits a
// second, or 0 when nothing holds it back (downlink.go).
func (p *participant) estimate() int {
	if v := p.estimateOverride.Load(); v != 0 {
		return int(v)
	}
	e := p.estimator.Load()
	if e == nil {
		return 0
	}
	// What the subscriber's feedback says of the path: how much is lost.
	loss, _ := (*e).GetStats()["averageLoss"].(float64)
	p.linkMu.Lock()
	defer p.linkMu.Unlock()
	return p.link.update(time.Now(), p.sentBytes.Load(), loss)
}

// wake asks the participant's worker to bring what it is forwarded in line
// with the room.
func (p *participant) wake() {
	select {
	case p.kick <- struct{}{}:
	default:
	}
}

// run is the participant's worker: one goroutine that makes every change to
// what the participant is forwarded, so changes never cross.
func (p *participant) run() {
	for {
		select {
		case <-p.stop:
			return
		case <-p.kick:
			p.sync()
		}
	}
}

// shut closes the participant's media and stops its worker.
func (p *participant) shut() {
	p.once.Do(func() {
		close(p.stop)
		p.pcMu.Lock()
		send, recv, downs := p.sendPC, p.recvPC, p.downs
		p.sendPC, p.recvPC, p.downs = nil, nil, map[string]*downTrack{}
		p.sendRef.Store(nil)
		p.estimator.Store(nil)
		p.pcMu.Unlock()
		for _, d := range downs {
			d.pub.detach(d)
		}
		// Closing waits for the transports; nobody needs to wait for that.
		go func() {
			if send != nil {
				send.Close()
			}
			if recv != nil {
				recv.Close()
			}
		}()
	})
}

// list returns the participants in the order they joined.
func (r *room) list(tracks bool) []model.CallParticipant {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]model.CallParticipant, 0, len(r.parts))
	for _, p := range r.parts {
		cp := model.CallParticipant{AccountID: p.id, DeviceID: p.device, JoinedAt: p.joinedAt}
		if tracks {
			cp.Tracks = []any{}
			for _, t := range p.sortedTracks() {
				cp.Tracks = append(cp.Tracks, t.info())
			}
		}
		out = append(out, cp)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].JoinedAt != out[j].JoinedAt {
			return out[i].JoinedAt < out[j].JoinedAt
		}
		return out[i].AccountID < out[j].AccountID
	})
	return out
}

func (p *participant) sortedTracks() []*pubTrack {
	out := make([]*pubTrack, 0, len(p.tracks))
	for _, t := range p.tracks {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].seq < out[j].seq })
	return out
}

// full reports whether the call has the most participants allowed and the
// account is not already one of them.
func (r *room) full(account model.AccountID, most int) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, in := r.parts[account]
	return !in && len(r.parts) >= most
}

// seat is one participant as the watch loop needs it.
type seat struct {
	account model.AccountID
	session model.SessionID
	remote  string
}

func (r *room) who() []seat {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]seat, 0, len(r.parts))
	for _, p := range r.parts {
		out = append(out, seat{p.id, p.session, p.remote})
	}
	return out
}

func (r *room) get(account model.AccountID) *participant {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.parts[account]
}

// add puts a participant in the room and starts its worker.
func (r *room) add(p *participant) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return false
	}
	r.parts[p.id] = p
	r.aloneSince = time.Time{}
	if len(r.parts) == 1 {
		r.aloneSince = time.Now()
	}
	go p.run()
	return true
}

// remove takes a participant out with its tracks. It returns the
// participant, or nil if the account was not in, and how many are left.
func (r *room) remove(account model.AccountID) (*participant, int) {
	r.mu.Lock()
	p := r.parts[account]
	if p == nil {
		n := len(r.parts)
		r.mu.Unlock()
		return nil, n
	}
	delete(r.parts, account)
	p.gone = true
	gone := r.dropTracks(p)
	n := len(r.parts)
	r.aloneSince = time.Time{}
	if n == 1 {
		r.aloneSince = time.Now()
	}
	r.mu.Unlock()
	r.speakers.forget(account)
	for _, t := range gone {
		t.close()
	}
	return p, n
}

// dropTracks forgets every track a participant published. Call with r.mu
// held; close the tracks returned after releasing it.
func (r *room) dropTracks(p *participant) []*pubTrack {
	var gone []*pubTrack
	for mid, t := range p.tracks {
		delete(r.tracks, t.id)
		delete(p.tracks, mid)
		gone = append(gone, t)
	}
	return gone
}

// close empties the room when the call ends, and returns who was in it.
func (r *room) close() []model.AccountID {
	r.mu.Lock()
	r.closed = true
	parts := make([]*participant, 0, len(r.parts))
	var ids []model.AccountID
	for id, p := range r.parts {
		parts = append(parts, p)
		ids = append(ids, id)
		p.gone = true
	}
	r.parts = map[model.AccountID]*participant{}
	r.tracks = map[string]*pubTrack{}
	r.mu.Unlock()
	for _, p := range parts {
		p.shut()
	}
	return ids
}

func (r *room) kickAll() {
	r.mu.Lock()
	for _, p := range r.parts {
		p.wake()
	}
	r.mu.Unlock()
}

// broadcast sends an event to every participant, or to all but one.
func (r *room) broadcast(v any, except model.AccountID) {
	r.mu.Lock()
	to := make([]*participant, 0, len(r.parts))
	for id, p := range r.parts {
		if id != except {
			to = append(to, p)
		}
	}
	r.mu.Unlock()
	for _, p := range to {
		p.send(v)
	}
}

// verdict is what a look at the room found.
type verdict struct {
	// dropped are participants whose signalling and media have both been
	// gone for the hold time.
	dropped []model.AccountID
	count   int
	// alone is how long exactly one participant has been in the call.
	alone time.Duration
}

// tick looks at the room: who has dropped, how long someone has been alone,
// and whether the speakers changed. It also has every subscriber's layers
// looked at again, since downlink estimates move by themselves.
func (r *room) tick(now time.Time, t Timings) verdict {
	var v verdict
	var late []*participant
	r.mu.Lock()
	for id, p := range r.parts {
		media := p.connected.Load() > 0
		switch {
		case media:
			p.mediaLostAt, p.mediaSeen = time.Time{}, true
		case p.mediaLostAt.IsZero():
			p.mediaLostAt = now
		}
		if !p.sigLostAt.IsZero() && !p.mediaLostAt.IsZero() && now.Sub(p.sigLostAt) >= t.Hold && now.Sub(p.mediaLostAt) >= t.Hold {
			v.dropped = append(v.dropped, id)
		}
		if !p.mediaSeen && p.lateAt.IsZero() && now.Sub(p.joined) >= t.MediaTimeout {
			p.lateAt = now
			late = append(late, p)
		}
		if len(p.wants) > 0 {
			p.wake()
		}
	}
	v.count = len(r.parts)
	if v.count == 1 && !r.aloneSince.IsZero() {
		v.alone = now.Sub(r.aloneSince)
	}
	r.mu.Unlock()
	for _, p := range late {
		// Signalling worked and no media connection was made: usually a
		// blocked port.
		p.send(map[string]any{"type": "call.error", "code": "media_timeout",
			"message": "No media connection could be made. This network may block calls."})
	}
	if ids, changed := r.speakers.changed(now); changed {
		r.broadcast(event(EventSpeakersChanged, r.call, map[string]any{"account_ids": ids}), "")
		// The current speaker is lowered last, so layers may move.
		r.kickAll()
	}
	return v
}

// pubLayer is one size a published track is sent in.
type pubLayer struct {
	spec option
	// ssrc is the stream carrying this layer, once its first packet came.
	ssrc atomic.Uint32
	// Times in nanoseconds since the Unix epoch; 0 is never.
	lastPacket    atomic.Int64
	demandedSince atomic.Int64
	deadUntil     atomic.Int64
	lastKeyAsk    atomic.Int64
}

// pubTrack is one track a participant publishes.
type pubTrack struct {
	id     string
	seq    int
	owner  *participant
	mid    string
	source string
	kind   string
	codec  string
	width  int
	height int
	// layers are the sizes the track is sent in, smallest first; a track
	// sent in one size has one. named is true when they have rids.
	layers []*pubLayer
	named  bool
	muted  atomic.Bool
	closed atomic.Bool

	// subs are the subscribers the track is forwarded to. The slice is
	// replaced, never changed, so the forwarding loop reads it freely.
	subs  atomic.Pointer[[]*downTrack]
	subMu sync.Mutex
	// demand is the list of layers last told to the publisher.
	demand []string
	told   bool
}

func (t *pubTrack) info() trackInfo {
	i := trackInfo{ID: t.id, Source: t.source, Kind: t.kind, Muted: t.muted.Load(), Width: t.width, Height: t.height}
	if t.kind == kindVideo {
		i.Codec = t.codec
		if t.named {
			for _, l := range t.layers {
				i.Layers = append(i.Layers, l.spec.name)
			}
		}
	}
	return i
}

// level returns the index of the layer with a rid, or 0 for a track sent in
// one size. -1 for a rid the track never declared.
func (t *pubTrack) level(rid string) int {
	if !t.named {
		return 0
	}
	for i, l := range t.layers {
		if l.spec.name == rid {
			return i
		}
	}
	return -1
}

// options are the track's layers as the picker sees them now. A layer that
// was asked for and did not come is left out for a while.
func (t *pubTrack) options(now time.Time) []option {
	out := make([]option, len(t.layers))
	n := now.UnixNano()
	for i, l := range t.layers {
		o := l.spec
		if since := l.demandedSince.Load(); since != 0 && n-since > int64(3*time.Second) && n-l.lastPacket.Load() > int64(3*time.Second) {
			// Asked for, for three seconds, and nothing came: the
			// publisher is not sending it. Do not ask again for a while.
			l.deadUntil.Store(n + int64(30*time.Second))
		}
		o.available = n >= l.deadUntil.Load()
		out[i] = o
	}
	return out
}

func (t *pubTrack) attach(d *downTrack) {
	t.subMu.Lock()
	old := t.subs.Load()
	var list []*downTrack
	if old != nil {
		list = append(list, *old...)
	}
	list = append(list, d)
	t.subs.Store(&list)
	t.subMu.Unlock()
}

func (t *pubTrack) detach(d *downTrack) {
	t.subMu.Lock()
	old := t.subs.Load()
	var list []*downTrack
	if old != nil {
		for _, x := range *old {
			if x != d {
				list = append(list, x)
			}
		}
	}
	t.subs.Store(&list)
	t.subMu.Unlock()
	t.demandChanged()
}

// close stops forwarding the track to anyone.
func (t *pubTrack) close() {
	t.closed.Store(true)
	empty := []*downTrack{}
	t.subs.Store(&empty)
}

// demandChanged works out which layers anyone is receiving and, when that
// changed, tells the publisher, so a client stops encoding what nobody
// wants (docs/CALLS.md section 7).
func (t *pubTrack) demandChanged() {
	if t.kind != kindVideo || t.closed.Load() {
		return
	}
	t.subMu.Lock()
	wanted := make([]bool, len(t.layers))
	if subs := t.subs.Load(); subs != nil {
		for _, d := range *subs {
			if lvl := int(d.target.Load()); lvl >= 0 && lvl < len(wanted) {
				wanted[lvl] = true
			}
		}
	}
	now := time.Now().UnixNano()
	names := []string{}
	for i, l := range t.layers {
		if wanted[i] {
			names = append(names, l.spec.name)
			l.demandedSince.CompareAndSwap(0, now)
		} else {
			l.demandedSince.Store(0)
		}
	}
	changed := !t.told || len(names) != len(t.demand)
	for i := 0; !changed && i < len(names); i++ {
		changed = names[i] != t.demand[i]
	}
	t.demand, t.told = names, true
	t.subMu.Unlock()
	if changed {
		t.owner.send(event(EventDemandChanged, t.owner.room.call, map[string]any{"track_id": t.id, "layers": names}))
	}
}

// askKey asks the publisher for a key frame of one layer, at most once a
// second per layer however many subscribers want one.
func (t *pubTrack) askKey(level int) {
	if level < 0 || level >= len(t.layers) {
		return
	}
	l := t.layers[level]
	ssrc := l.ssrc.Load()
	if ssrc == 0 {
		return
	}
	now := time.Now().UnixNano()
	last := l.lastKeyAsk.Load()
	if now-last < int64(time.Second) || !l.lastKeyAsk.CompareAndSwap(last, now) {
		return
	}
	if pc := t.owner.sendRef.Load(); pc != nil {
		// Off the forwarding loop: writing RTCP can wait for the transport.
		go pc.WriteRTCP([]rtcp.Packet{&rtcp.PictureLossIndication{MediaSSRC: ssrc}})
	}
}

// offerEvery is the least time between two offers to one participant. When
// many people join at once every change would otherwise be its own offer,
// and a client answering each would pass the limit of twenty signalling
// messages a second (docs/CALLS.md section 12).
const offerEvery = 150 * time.Millisecond

// sync brings what a participant is forwarded in line with the room: every
// other participant's microphone, and the video tracks it subscribed to, at
// the layer the picker chooses. It runs on the participant's worker only.
func (p *participant) sync() {
	r := p.room
	now := time.Now()
	estimate := p.estimate()

	type target struct {
		track *pubTrack
		level int
	}
	want := map[string]target{}
	r.mu.Lock()
	if p.gone {
		r.mu.Unlock()
		return
	}
	audio := 0
	for id, o := range r.parts {
		if id == p.id {
			continue
		}
		for _, t := range o.tracks {
			if t.kind == kindAudio {
				want[t.id] = target{t, 0}
				audio++
			}
		}
	}
	choices, _ := p.choose(now, estimate, audio)
	type change struct {
		id string
		c  choice
	}
	var changes []change
	for _, sub := range p.wants {
		c := choices[sub.track]
		if old, had := p.shown[sub.track]; had && (old.state != c.state || old.layer != c.layer) {
			changes = append(changes, change{sub.track, c})
		}
		if t := r.tracks[sub.track]; t != nil && c.state != stateUnavailable {
			// A paused or muted track keeps its media line, so coming
			// back needs no new offer.
			want[t.id] = target{t, c.level}
		}
	}
	p.shown = choices
	r.mu.Unlock()

	var touched []*pubTrack
	p.pcMu.Lock()
	for id, d := range p.downs {
		if _, keep := want[id]; keep && !d.pub.closed.Load() {
			continue
		}
		if p.recvPC != nil && d.sender != nil {
			p.recvPC.RemoveTrack(d.sender)
		}
		d.target.Store(-1)
		d.pub.detach(d)
		delete(p.downs, id)
		p.needOffer = true
		p.removals++
	}
	for id, tg := range want {
		d := p.downs[id]
		if d == nil {
			var err error
			if d, err = p.forward(tg.track); err != nil {
				r.s.log.Warn("a track could not be forwarded", "call", r.call.id, "err", err)
				continue
			}
			p.downs[id] = d
			p.needOffer = true
		}
		if old := d.target.Swap(int32(tg.level)); int(old) != tg.level {
			touched = append(touched, tg.track)
			if tg.level >= 0 {
				// The switch happens at the layer's next key frame.
				tg.track.askKey(tg.level)
			}
		}
	}
	if p.needOffer && !p.awaiting && p.recvPC != nil {
		if wait := offerEvery - time.Since(p.offeredAt); wait > 0 && !p.offeredAt.IsZero() {
			// Too soon after the last one: what changed meanwhile goes
			// into one offer a moment from now.
			time.AfterFunc(wait, p.wake)
		} else {
			p.offer()
		}
	}
	p.pcMu.Unlock()

	for _, t := range touched {
		t.demandChanged()
	}
	for _, ch := range changes {
		f := map[string]any{"track_id": ch.id, "state": ch.c.state, "reason": ch.c.reason}
		if ch.c.layer != "" {
			f["layer"] = ch.c.layer
		}
		p.send(event(EventLayerChanged, r.call, f))
	}
}

// choose runs the layer picker over the participant's subscriptions. Call
// with room.mu held.
func (p *participant) choose(now time.Time, estimateBps, audioTracks int) (map[string]choice, []wanted) {
	r := p.room
	speaker := r.speakers.loudest()
	list := make([]wanted, 0, len(p.wants))
	for _, sub := range p.wants {
		w := wanted{id: sub.track, height: sub.height}
		t := r.tracks[sub.track]
		switch {
		case t == nil || t.kind != kindVideo || t.owner == p || !p.caps.codecs[t.codec]:
			// Gone, not video, the client's own, or a codec this client
			// cannot decode.
			w.unavailable = true
		default:
			w.options = t.options(now)
			w.screen = t.source == sourceScreen
			w.speaker = t.source == sourceCamera && t.owner.id == speaker
			w.muted = t.muted.Load()
		}
		list = append(list, w)
	}
	return p.picker.pick(list, videoBudget(estimateBps, audioTracks), now), list
}

// forward starts forwarding a track to the participant: a new media line on
// its recv connection, or one whose last track has gone. Call with pcMu
// held.
func (p *participant) forward(t *pubTrack) (*downTrack, error) {
	if p.recvPC == nil {
		pc, est, err := p.room.s.media.newRecv()
		if err != nil {
			return nil, err
		}
		p.recvPC = pc
		if est != nil {
			p.estimator.Store(&est)
		}
		p.watchConnection(pc)
	}
	codec, _ := codecFor(t.codec)
	d := &downTrack{id: t.id, pub: t, sub: p, codec: codec, video: t.kind == kindVideo}
	d.target.Store(-1)
	d.current = -1
	kind := webrtc.RTPCodecTypeAudio
	if d.video {
		kind = webrtc.RTPCodecTypeVideo
	}
	var sender *webrtc.RTPSender
	var err error
	if p.removals == p.settled && p.spare(kind) {
		sender, err = p.recvPC.AddTrack(d)
	} else {
		var tr *webrtc.RTPTransceiver
		tr, err = p.recvPC.AddTransceiverFromTrack(d, webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionSendonly})
		if err == nil {
			sender = tr.Sender()
		}
	}
	if err != nil {
		return nil, err
	}
	d.sender = sender
	for _, tr := range p.recvPC.GetTransceivers() {
		if tr.Sender() == sender {
			// The media line offers the publisher's codec and no other:
			// a forwarding server cannot convert between codecs.
			tr.SetCodecPreferences([]webrtc.RTPCodecParameters{codec})
		}
	}
	t.attach(d)
	go d.feedback(sender)
	return d, nil
}

// spare reports whether the recv connection has a media line of this kind
// whose track has gone, which a new track can take over.
func (p *participant) spare(kind webrtc.RTPCodecType) bool {
	for _, tr := range p.recvPC.GetTransceivers() {
		if tr.Kind() == kind && tr.Sender() == nil && tr.Direction() == webrtc.RTPTransceiverDirectionInactive {
			return true
		}
	}
	return false
}

// watchConnection follows a media connection's state, for the rule that a
// participant is held while either its signalling or its media is alive.
func (p *participant) watchConnection(pc *webrtc.PeerConnection) {
	var up atomic.Bool
	pc.OnICEConnectionStateChange(func(st webrtc.ICEConnectionState) {
		is := st == webrtc.ICEConnectionStateConnected || st == webrtc.ICEConnectionStateCompleted
		if up.Swap(is) != is {
			if is {
				p.connected.Add(1)
			} else {
				p.connected.Add(-1)
			}
		}
	})
}

// offer makes a new offer on the recv connection and sends it. The server
// sends one at a time and waits for its answer; changes that happen
// meanwhile go into the next one. Call with pcMu held.
func (p *participant) offer() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	restart := p.restart
	var opts *webrtc.OfferOptions
	if restart {
		opts = &webrtc.OfferOptions{ICERestart: true}
	}
	desc, err := p.recvPC.CreateOffer(opts)
	if err != nil {
		p.room.s.log.Warn("an offer could not be made", "call", p.room.call.id, "err", err)
		return
	}
	sdp, err := p.room.s.media.describe(ctx, p.recvPC, desc)
	if err != nil {
		p.room.s.log.Warn("an offer could not be made", "call", p.room.call.id, "err", err)
		return
	}
	tracks := []map[string]any{}
	for _, tr := range p.recvPC.GetTransceivers() {
		for _, d := range p.downs {
			if d.sender != nil && tr.Sender() == d.sender && tr.Mid() != "" {
				tracks = append(tracks, withDevice(map[string]any{"mid": tr.Mid(), "track_id": d.id, "account_id": d.pub.owner.id}, d.pub.owner.device))
			}
		}
	}
	sort.Slice(tracks, func(i, j int) bool { return tracks[i]["mid"].(string) < tracks[j]["mid"].(string) })
	p.offerID++
	p.offeredAt = time.Now()
	p.awaiting, p.needOffer, p.restart = true, false, false
	p.offered = p.removals
	p.lastOffer = map[string]any{"type": "call.offer", "pc": "recv", "offer_id": p.offerID, "sdp": sdp, "ice_restart": restart, "tracks": tracks}
	p.send(p.lastOffer)
}

// tracksChanged is called by the room when a participant's own published
// tracks, or its mute state, changed: everyone's forwarding is looked at
// again.
func (r *room) tracksChanged() { r.kickAll() }
