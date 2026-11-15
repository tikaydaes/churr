// Package calltest is a call client for tests: a Pion client that joins a
// call through the real signalling of docs/CALLS.md section 10, publishes
// made-up audio and video, and counts what it is forwarded. Tests of the
// calls package, benchmarks and anything else that needs "someone in a
// call" use it. It lives here and not in testkit because only the calls
// packages import Pion.
package calltest

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pion/interceptor"
	"github.com/pion/logging"
	"github.com/pion/rtcp"
	"github.com/pion/rtp"
	"github.com/pion/sdp/v3"
	"github.com/pion/webrtc/v4"
)

// Signaller is the live connection as a call client needs it.
// testkit.Stream is one.
type Signaller interface {
	Send(v any)
	Next(wait time.Duration) (map[string]any, bool)
}

// T is the part of testing.TB the client uses.
type T interface {
	Helper()
	Fatalf(format string, args ...any)
	Logf(format string, args ...any)
}

// Options say what kind of client this is.
type Options struct {
	// TCPOnly makes the client use ICE over TCP alone, as on a network
	// that blocks UDP.
	TCPOnly bool
	// Simulcast publishes the camera in three sizes, q, h and f.
	Simulcast bool
	// Layers, with Simulcast, replaces the three sizes: a phone
	// publishes q and h.
	Layers []string
	// H264 publishes the camera as H.264 instead of VP8.
	H264 bool
	// Decodes is the `video_codecs` the client states. Empty means vp8
	// and h264.
	Decodes []string
	// FrameEncryption is what the client states in `call.join`.
	FrameEncryption bool
	// Encrypt, if set, is applied to every media frame after its
	// readable leading bytes, standing in for a device's frame
	// encryption in a sealed chat.
	Encrypt func(frame []byte)
	// Quiet stops the client from publishing media packets: it joins
	// and listens only.
	Quiet bool
	// Realistic makes the published media as heavy as the real thing:
	// audio at 32 kbit/s, and camera layers at the frame rates and bit
	// rates of docs/CALLS.md section 7, several packets to a frame. Load
	// tests use it; the default is a trickle that is easy to count.
	Realistic bool
	// NoAnswer makes the client ignore the server's offers, as a client
	// whose network lets no media through never gets as far as answering
	// usefully. It then has no media connection at all.
	NoAnswer bool
	// Wait is how long a request may take. The default is ten seconds.
	Wait time.Duration
}

const audioLevelURI = "urn:ietf:params:rtp-hdrext:ssrc-audio-level"

// Stream is what the client has received of one forwarded track.
type Stream struct {
	// Packets is how many RTP packets arrived.
	Packets atomic.Int64
	Bytes   atomic.Int64

	mu sync.Mutex
	// byLayer counts packets by the layer mark the publisher put in each.
	byLayer map[byte]int
	// last is the mark of the newest packet.
	last byte
	// seqBreaks counts sequence numbers that did not follow the one
	// before; picBreaks the same for VP8 picture numbers between frames.
	seqBreaks, picBreaks int
	// badSwitches counts layer changes that did not begin at a key frame.
	badSwitches int
	// lost is how many sequence numbers were skipped; delays are from
	// the moment the publisher made a packet to its arrival here.
	lost int64
	// late is how many packets arrived after one sent later than them.
	late          int64
	delaySum      time.Duration
	delayMax      time.Duration
	delayCount    int64
	haveSeq       bool
	seq           uint16
	havePic       bool
	pic           uint16
	lastPayload   []byte
	lastTimestamp uint32
	tsBack        int
}

// Layers returns how many packets arrived per layer mark ('q', 'h', 'f',
// 's' for a screen, 'a' for audio).
func (s *Stream) Layers() map[byte]int {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[byte]int{}
	for k, v := range s.byLayer {
		out[k] = v
	}
	return out
}

// Last is the layer mark of the newest packet.
func (s *Stream) Last() byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.last
}

// Breaks returns how often the sequence numbers, the picture numbers and
// the timestamps failed to carry on from the packet before, and how many
// layer changes did not begin at a key frame. On the loopback address all
// four should be zero.
func (s *Stream) Breaks() (seq, pic, ts, badSwitch int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.seqBreaks, s.picBreaks, s.tsBack, s.badSwitches
}

// Lost is how many packets never arrived, going by the gaps in the
// sequence numbers.
func (s *Stream) Lost() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lost
}

// Late is how many packets arrived out of order, after a later one.
func (s *Stream) Late() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.late
}

// Delay is how long packets took from the publishing client to this one:
// the mean and the worst. It is measured only when the payload is not
// encrypted, and means something only when both clients share a clock.
func (s *Stream) Delay() (mean, worst time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.delayCount == 0 {
		return 0, 0
	}
	return s.delaySum / time.Duration(s.delayCount), s.delayMax
}

// ResetStats forgets the loss and delay counted so far, so a test can
// measure a steady stretch.
func (s *Stream) ResetStats() {
	s.mu.Lock()
	s.lost, s.delaySum, s.delayMax, s.delayCount = 0, 0, 0, 0
	s.mu.Unlock()
}

// LastPayload is the payload of the newest packet, as it arrived.
func (s *Stream) LastPayload() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]byte(nil), s.lastPayload...)
}

// published is one source the client is sending.
type published struct {
	source string
	sender *webrtc.RTPSender
	// mid is the media line, a string, read by the feeding goroutine.
	mid atomic.Value
	// tracks are the encodings: one, or one per simulcast layer.
	tracks []*webrtc.TrackLocalStaticRTP
	rids   []string
	// wantKey is set per encoding when the server asks for a key frame.
	wantKey []atomic.Bool
	// PLIs counts key frame requests per encoding.
	plis   []atomic.Int64
	stop   chan struct{}
	paused atomic.Bool
}

// Client is one device in a call.
type Client struct {
	t    T
	opts Options

	mu      sync.Mutex
	sig     Signaller
	gen     int
	nextID  int
	waiting map[string]chan map[string]any
	events  []map[string]any
	arrived chan struct{}

	send *webrtc.PeerConnection
	recv *webrtc.PeerConnection
	pubs map[string]*published
	// mids maps a media line of recv to the track it carries.
	mids    map[string]string
	streams map[string]*Stream
	// Offers counts the offers the server has sent on recv.
	Offers atomic.Int64
	// SentAudio and SentVideo count the media packets handed to the send
	// connection, so a load test can set what arrived beside what left.
	SentAudio, SentVideo atomic.Int64
	// level is the loudness the microphone claims, 0 loudest, 127 silent.
	level atomic.Int32
	// dropped names simulcast layers the client has stopped sending.
	dropped sync.Map

	// CallID, ResumeToken and TrackIDs are filled in as the client joins
	// and publishes.
	CallID      string
	ResumeToken string
	TrackIDs    map[string]string
	closed      atomic.Bool
}

// New makes a client on a live connection. The client reads every frame
// that arrives on it from then on; use Expect to see them.
func New(t T, sig Signaller, o Options) *Client {
	if o.Wait == 0 {
		o.Wait = 10 * time.Second
	}
	c := &Client{t: t, opts: o, waiting: map[string]chan map[string]any{}, arrived: make(chan struct{}, 1),
		pubs: map[string]*published{}, mids: map[string]string{}, streams: map[string]*Stream{}, TrackIDs: map[string]string{}}
	c.level.Store(127)
	c.Attach(sig)
	return c
}

// Attach moves the client to another live connection, as after the first
// one dropped. Follow it with Resume.
func (c *Client) Attach(sig Signaller) {
	c.mu.Lock()
	c.sig = sig
	c.gen++
	gen := c.gen
	c.mu.Unlock()
	go c.read(sig, gen)
}

// Detach makes the client ignore what its current connection sends from
// now on, as when that connection has dropped.
func (c *Client) Detach() {
	c.mu.Lock()
	c.gen++
	c.mu.Unlock()
}

func (c *Client) api() *webrtc.API {
	se := webrtc.SettingEngine{LoggerFactory: quiet{}}
	se.SetIncludeLoopbackCandidate(true)
	if c.opts.TCPOnly {
		se.SetNetworkTypes([]webrtc.NetworkType{webrtc.NetworkTypeTCP4})
	} else {
		se.SetNetworkTypes([]webrtc.NetworkType{webrtc.NetworkTypeUDP4})
	}
	// Keep-alive checks often, so a server with shortened waits does not
	// take a listener for gone.
	se.SetICETimeouts(5*time.Second, 20*time.Second, 200*time.Millisecond)
	me := &webrtc.MediaEngine{}
	must(me.RegisterCodec(webrtc.RTPCodecParameters{PayloadType: 111, RTPCodecCapability: webrtc.RTPCodecCapability{
		MimeType: webrtc.MimeTypeOpus, ClockRate: 48000, Channels: 2, SDPFmtpLine: "minptime=10;useinbandfec=1"}}, webrtc.RTPCodecTypeAudio))
	fb := []webrtc.RTCPFeedback{{Type: "nack"}, {Type: "nack", Parameter: "pli"}, {Type: "transport-cc"}}
	vp8 := webrtc.RTPCodecParameters{PayloadType: 96, RTPCodecCapability: webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP8, ClockRate: 90000, RTCPFeedback: fb}}
	h264 := webrtc.RTPCodecParameters{PayloadType: 102, RTPCodecCapability: webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH264, ClockRate: 90000, RTCPFeedback: fb,
		SDPFmtpLine: "level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=42e01f"}}
	decodes := map[string]bool{}
	for _, d := range c.opts.Decodes {
		decodes[d] = true
	}
	if len(decodes) == 0 {
		decodes["vp8"], decodes["h264"] = true, true
	}
	if decodes["vp8"] || !c.opts.H264 {
		must(me.RegisterCodec(vp8, webrtc.RTPCodecTypeVideo))
	}
	if decodes["h264"] || c.opts.H264 {
		must(me.RegisterCodec(h264, webrtc.RTPCodecTypeVideo))
	}
	must(me.RegisterHeaderExtension(webrtc.RTPHeaderExtensionCapability{URI: audioLevelURI}, webrtc.RTPCodecTypeAudio))
	ir := &interceptor.Registry{}
	must(webrtc.RegisterDefaultInterceptorsWithOptions(me, ir, webrtc.WithInterceptorLoggerFactory(quiet{})))
	return webrtc.NewAPI(webrtc.WithSettingEngine(se), webrtc.WithMediaEngine(me), webrtc.WithInterceptorRegistry(ir))
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}

// read takes frames off the connection until another one is attached.
func (c *Client) read(sig Signaller, gen int) {
	for {
		// One long wait: a read that gives up closes the connection
		// under it. The read ends when the connection does.
		f, ok := sig.Next(24 * time.Hour)
		if !ok {
			return
		}
		c.mu.Lock()
		current := c.gen == gen
		c.mu.Unlock()
		if !current || c.closed.Load() {
			return
		}
		if reply, _ := f["reply_to"].(string); reply != "" {
			c.mu.Lock()
			ch := c.waiting[reply]
			delete(c.waiting, reply)
			c.mu.Unlock()
			if ch != nil {
				ch <- f
				continue
			}
		}
		if f["type"] == "call.offer" {
			c.Offers.Add(1)
			if !c.opts.NoAnswer {
				c.answer(sig, f)
			}
		}
		c.mu.Lock()
		c.events = append(c.events, f)
		c.mu.Unlock()
		select {
		case c.arrived <- struct{}{}:
		default:
		}
	}
}

// Request sends a frame with a fresh `id` and returns the frame that
// answers it: `call.ok`, `call.error` or `call.answer`.
func (c *Client) Request(frame map[string]any) map[string]any {
	c.t.Helper()
	c.mu.Lock()
	c.nextID++
	id := fmt.Sprintf("c%d", c.nextID)
	ch := make(chan map[string]any, 1)
	c.waiting[id] = ch
	sig := c.sig
	c.mu.Unlock()
	frame["id"] = id
	sig.Send(frame)
	select {
	case f := <-ch:
		return f
	case <-time.After(c.opts.Wait):
		c.t.Fatalf("no answer to %v", frame["type"])
		return nil
	}
}

// OK is Request that fails the test unless the answer is `call.ok`.
func (c *Client) OK(frame map[string]any) map[string]any {
	c.t.Helper()
	f := c.Request(frame)
	if f["type"] != "call.ok" {
		c.t.Fatalf("%v was answered with %v", frame["type"], f)
	}
	return f
}

// Refused is Request that fails the test unless the answer is `call.error`
// with this code.
func (c *Client) Refused(code string, frame map[string]any) map[string]any {
	c.t.Helper()
	f := c.Request(frame)
	if f["type"] != "call.error" || f["code"] != code {
		c.t.Fatalf("%v was answered with %v, want call.error %s", frame["type"], f, code)
	}
	return f
}

// Expect waits for a frame of a type that satisfies ok (nil: any), among
// those that have arrived and were not returned before, and returns it.
func (c *Client) Expect(frameType string, ok func(f map[string]any) bool) map[string]any {
	c.t.Helper()
	if f := c.Find(frameType, ok, c.opts.Wait); f != nil {
		return f
	}
	c.t.Fatalf("no %s frame arrived", frameType)
	return nil
}

// Find is Expect that returns nil instead of failing.
func (c *Client) Find(frameType string, ok func(f map[string]any) bool, wait time.Duration) map[string]any {
	deadline := time.Now().Add(wait)
	for {
		c.mu.Lock()
		for i, f := range c.events {
			if f["type"] == frameType && (ok == nil || ok(f)) {
				c.events = append(c.events[:i:i], c.events[i+1:]...)
				c.mu.Unlock()
				return f
			}
		}
		c.mu.Unlock()
		left := time.Until(deadline)
		if left <= 0 {
			return nil
		}
		select {
		case <-c.arrived:
		case <-time.After(minDur(left, 50*time.Millisecond)):
		}
	}
}

func minDur(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}

func (c *Client) join(frame map[string]any) map[string]any {
	frame["client"] = map[string]any{"simulcast": c.opts.Simulcast, "video_codecs": c.decodes(), "frame_encryption": c.opts.FrameEncryption}
	return frame
}

func (c *Client) decodes() []string {
	if len(c.opts.Decodes) > 0 {
		return c.opts.Decodes
	}
	return []string{"vp8", "h264"}
}

// Join sends `call.join` with a ticket and returns the answer.
func (c *Client) Join(callID, ticket string) map[string]any {
	c.t.Helper()
	f := c.Request(c.join(map[string]any{"type": "call.join", "call_id": callID, "ticket": ticket}))
	if f["type"] == "call.ok" {
		c.CallID = callID
		c.ResumeToken, _ = f["resume_token"].(string)
	}
	return f
}

// MustJoin is Join that fails the test unless the answer is `call.ok`.
func (c *Client) MustJoin(callID, ticket string) map[string]any {
	c.t.Helper()
	f := c.Join(callID, ticket)
	if f["type"] != "call.ok" {
		c.t.Fatalf("call.join was answered with %v", f)
	}
	return f
}

// Resume sends `call.resume` on the connection now attached.
func (c *Client) Resume() map[string]any {
	c.t.Helper()
	f := c.Request(map[string]any{"type": "call.resume", "call_id": c.CallID, "resume_token": c.ResumeToken})
	if f["type"] == "call.ok" {
		c.ResumeToken, _ = f["resume_token"].(string)
	}
	return f
}

// Leave sends `call.leave`.
func (c *Client) Leave() map[string]any {
	return c.Request(map[string]any{"type": "call.leave", "call_id": c.CallID})
}

// Publish makes the client's send connection carry exactly these sources
// ("microphone", "camera", "screen"), with a new offer, and returns the
// server's answer frame: `call.answer`, or `call.error` if it refused.
func (c *Client) Publish(sources ...string) map[string]any {
	c.t.Helper()
	return c.publish(false, sources...)
}

// RestartSend makes a new offer on send with new ICE credentials.
func (c *Client) RestartSend() map[string]any {
	c.t.Helper()
	var sources []string
	c.mu.Lock()
	for s := range c.pubs {
		sources = append(sources, s)
	}
	c.mu.Unlock()
	return c.publish(true, sources...)
}

func (c *Client) publish(restart bool, sources ...string) map[string]any {
	c.t.Helper()
	c.mu.Lock()
	if c.send == nil {
		pc, err := c.api().NewPeerConnection(webrtc.Configuration{})
		if err != nil {
			c.mu.Unlock()
			c.t.Fatalf("making the send connection: %v", err)
		}
		c.send = pc
	}
	pc := c.send
	want := map[string]bool{}
	for _, s := range sources {
		want[s] = true
	}
	var fresh []*published
	for source, p := range c.pubs {
		if !want[source] {
			close(p.stop)
			pc.RemoveTrack(p.sender)
			delete(c.pubs, source)
			delete(c.TrackIDs, source)
		}
	}
	for _, source := range sources {
		if c.pubs[source] != nil {
			continue
		}
		p, err := c.add(pc, source)
		if err != nil {
			c.mu.Unlock()
			c.t.Fatalf("adding %s: %v", source, err)
		}
		c.pubs[source] = p
		fresh = append(fresh, p)
	}
	c.mu.Unlock()

	var opts *webrtc.OfferOptions
	if restart {
		opts = &webrtc.OfferOptions{ICERestart: true}
	}
	// The offer is set only once the server has answered it: a refused
	// offer is then simply forgotten, with nothing to take back.
	offer, err := pc.CreateOffer(opts)
	if err != nil {
		c.t.Fatalf("making an offer: %v", err)
	}
	tracks := []map[string]any{}
	c.mu.Lock()
	for _, tr := range pc.GetTransceivers() {
		for source, p := range c.pubs {
			if tr.Sender() == p.sender {
				p.mid.Store(tr.Mid())
				tracks = append(tracks, map[string]any{"mid": tr.Mid(), "source": source})
			}
		}
	}
	c.mu.Unlock()
	f := c.Request(map[string]any{"type": "call.offer", "pc": "send", "sdp": offer.SDP, "ice_restart": restart, "tracks": tracks})
	if f["type"] != "call.answer" {
		// Refused: the new tracks go, and the offer was never set.
		c.mu.Lock()
		for _, p := range fresh {
			close(p.stop)
			pc.RemoveTrack(p.sender)
			delete(c.pubs, p.source)
		}
		c.mu.Unlock()
		return f
	}
	if err := pc.SetLocalDescription(offer); err != nil {
		c.t.Fatalf("setting the offer: %v", err)
	}
	if err := pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: f["sdp"].(string)}); err != nil {
		c.t.Fatalf("the server's answer could not be used: %v", err)
	}
	c.mu.Lock()
	for _, raw := range f["tracks"].([]any) {
		tr := raw.(map[string]any)
		for source, p := range c.pubs {
			if p.mid.Load() == tr["mid"] {
				c.TrackIDs[source] = tr["track_id"].(string)
			}
		}
	}
	c.mu.Unlock()
	for _, p := range fresh {
		go c.feed(p)
	}
	return f
}

// add puts one source on the send connection.
func (c *Client) add(pc *webrtc.PeerConnection, source string) (*published, error) {
	p := &published{source: source, stop: make(chan struct{})}
	p.mid.Store("")
	mime, id := webrtc.MimeTypeVP8, source
	switch {
	case source == "microphone":
		mime = webrtc.MimeTypeOpus
	case c.opts.H264:
		mime = webrtc.MimeTypeH264
	}
	capability := webrtc.RTPCodecCapability{MimeType: mime}
	rids := []string{""}
	if source == "camera" && c.opts.Simulcast {
		rids = []string{"q", "h", "f"}
		if len(c.opts.Layers) > 0 {
			rids = c.opts.Layers
		}
	}
	for i, rid := range rids {
		var opts []func(*webrtc.TrackLocalStaticRTP)
		if rid != "" {
			opts = append(opts, webrtc.WithRTPStreamID(rid))
		}
		tr, err := webrtc.NewTrackLocalStaticRTP(capability, id, "stream", opts...)
		if err != nil {
			return nil, err
		}
		if i == 0 {
			t, err := pc.AddTransceiverFromTrack(tr, webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionSendonly})
			if err != nil {
				return nil, err
			}
			p.sender = t.Sender()
		} else if err := p.sender.AddEncoding(tr); err != nil {
			return nil, err
		}
		p.tracks = append(p.tracks, tr)
	}
	p.rids = rids
	p.wantKey = make([]atomic.Bool, len(rids))
	p.plis = make([]atomic.Int64, len(rids))
	return p, nil
}

// Sizes of the made-up frames, by layer mark.
var payloadSize = map[byte]int{'q': 80, 'h': 240, 'f': 600, 's': 900, 'a': 40}

// feed sends made-up media on one published source until it is unpublished:
// fifty audio packets a second, or twenty one-packet video frames a second
// per layer. A key frame is sent first and whenever the server asks.
func (c *Client) feed(p *published) {
	var midID, ridID, levelID uint8
	for _, ext := range p.sender.GetParameters().HeaderExtensions {
		switch ext.URI {
		case sdp.SDESMidURI:
			midID = uint8(ext.ID)
		case sdp.SDESRTPStreamIDURI:
			ridID = uint8(ext.ID)
		case audioLevelURI:
			levelID = uint8(ext.ID)
		}
	}
	// Key frame requests from the server, per encoding.
	for i, rid := range p.rids {
		go func(i int, rid string) {
			for {
				var pkts []rtcp.Packet
				var err error
				if rid == "" {
					pkts, _, err = p.sender.ReadRTCP()
				} else {
					pkts, _, err = p.sender.ReadSimulcastRTCP(rid)
				}
				if err != nil {
					return
				}
				for _, pkt := range pkts {
					if _, ok := pkt.(*rtcp.PictureLossIndication); ok {
						p.plis[i].Add(1)
						p.wantKey[i].Store(true)
					}
				}
			}
		}(i, rid)
		p.wantKey[i].Store(true)
	}
	audio := p.source == "microphone"
	every := 50 * time.Millisecond
	clock := uint32(90000 / 20)
	if c.opts.Realistic {
		every, clock = time.Second/30, 90000/30
	}
	if audio {
		every, clock = 20*time.Millisecond, 960
	}
	tick := time.NewTicker(every)
	defer tick.Stop()
	frame := 0
	seqs := make([]uint16, len(p.rids))
	pics := make([]uint16, len(p.rids))
	for i := range p.rids {
		// Each layer numbers its own packets and pictures from somewhere
		// else, as separate encoders do.
		seqs[i], pics[i] = uint16(1000*(i+1)), uint16(100+7000*i)
	}
	var ts uint32 = 1
	for {
		select {
		case <-p.stop:
			return
		case <-tick.C:
		}
		ts += clock
		frame++
		if c.opts.Quiet || p.paused.Load() || c.closed.Load() {
			continue
		}
		for i, rid := range p.rids {
			if _, off := c.dropped.Load(rid); off && rid != "" {
				continue
			}
			mark := byte('a')
			switch {
			case p.source == "screen":
				mark = 's'
			case rid != "":
				mark = rid[0]
			case p.source == "camera":
				mark = 'f'
			}
			// How many packets make a frame, and how big each is.
			packets, size := 1, payloadSize[mark]
			if c.opts.Realistic {
				shape := realistic[mark]
				if shape.everyOther && frame%2 == 1 {
					continue // the small layer runs at half the frame rate
				}
				packets, size = shape.packets, shape.size
			}
			key := !audio && p.wantKey[i].Swap(false)
			pics[i] = (pics[i] + 1) & 0x7fff
			for n := 0; n < packets; n++ {
				seqs[i]++
				pkt := &rtp.Packet{Header: rtp.Header{Version: 2, SequenceNumber: seqs[i], Timestamp: ts, Marker: !audio && n == packets-1}}
				pkt.Payload = c.payload(p.source, mark, key, pics[i], n == 0, size)
				if midID != 0 {
					pkt.Header.SetExtension(midID, []byte(p.mid.Load().(string)))
				}
				if ridID != 0 && rid != "" {
					pkt.Header.SetExtension(ridID, []byte(rid))
				}
				if audio && levelID != 0 {
					pkt.Header.SetExtension(levelID, []byte{byte(c.level.Load()) & 0x7f})
				}
				if p.tracks[i].WriteRTP(pkt) == nil {
					if audio {
						c.SentAudio.Add(1)
					} else {
						c.SentVideo.Add(1)
					}
				}
			}
		}
	}
}

// realistic is the shape of each kind of media at its documented bit rate:
// q 320x180 at 15 frames a second and 150 kbit/s, h 640x360 at 30 and 500,
// f 1280x720 at 30 and 1,200, and audio at 32 kbit/s.
var realistic = map[byte]struct {
	packets    int
	size       int
	everyOther bool
}{
	'q': {2, 620, true}, 'h': {2, 1035, false}, 'f': {5, 995, false}, 's': {5, 995, false}, 'a': {1, 80, false},
}

// payload builds one packet of a made-up frame. The first bytes are what
// a real packetizer and encoder would put there, which is all the server
// reads; the layer mark and the time the packet was made follow; the rest
// is filler. first is true for the packet that begins the frame.
func (c *Client) payload(source string, mark byte, key bool, pic uint16, first bool, size int) []byte {
	var head []byte
	switch {
	case source == "microphone":
	case c.opts.H264:
		// One NAL unit: an IDR slice (type 5) or an ordinary one (type 1).
		if key {
			head = []byte{0x65}
		} else {
			head = []byte{0x41}
		}
	default:
		// VP8 payload descriptor with a 15-bit picture number, then the
		// frame header's first byte: bit 0 clear on a key frame. Later
		// packets of a frame have the S bit clear.
		head = []byte{0x90, 0x80, 0x80 | byte(pic>>8), byte(pic), 0x01}
		if key {
			head[4] = 0x00
		}
		if !first {
			head[0] = 0x80
		}
	}
	out := make([]byte, len(head)+size)
	copy(out, head)
	body := out[len(head):]
	body[0] = mark
	for i := 1; i < len(body); i++ {
		body[i] = byte(i)
	}
	if c.opts.Encrypt == nil && len(body) >= 9 {
		binary.BigEndian.PutUint64(body[1:9], uint64(time.Now().UnixNano()))
	}
	if c.opts.Encrypt != nil {
		// A device encrypts the frame after its readable leading bytes.
		// The mark is kept so tests can still tell layers apart.
		c.opts.Encrypt(body[1:])
	}
	return out
}

// SetLevel sets how loud the microphone claims to be: 0 is loudest, 127 is
// silence.
func (c *Client) SetLevel(level int) { c.level.Store(int32(level)) }

// DropLayer stops, or with false resumes, sending one simulcast layer, as
// when an uplink cannot carry it.
func (c *Client) DropLayer(rid string, drop bool) {
	if drop {
		c.dropped.Store(rid, true)
	} else {
		c.dropped.Delete(rid)
	}
}

// Pause stops, or with false resumes, sending a published source's packets
// without unpublishing it, as a muted track does.
func (c *Client) Pause(source string, paused bool) {
	c.mu.Lock()
	p := c.pubs[source]
	c.mu.Unlock()
	if p != nil {
		p.paused.Store(paused)
	}
}

// PLIs is how many key frame requests arrived for one layer of the camera
// ("" for a camera sent in one size, or for the screen via source).
func (c *Client) PLIs(source, rid string) int64 {
	c.mu.Lock()
	p := c.pubs[source]
	c.mu.Unlock()
	if p == nil {
		return 0
	}
	for i, r := range p.rids {
		if r == rid {
			return p.plis[i].Load()
		}
	}
	return 0
}

// answer answers an offer from the server on recv.
func (c *Client) answer(sig Signaller, f map[string]any) {
	c.mu.Lock()
	if c.recv == nil {
		pc, err := c.api().NewPeerConnection(webrtc.Configuration{})
		if err != nil {
			c.mu.Unlock()
			c.t.Logf("making the recv connection: %v", err)
			return
		}
		c.recv = pc
		pc.OnTrack(c.onTrack)
	}
	pc := c.recv
	if list, ok := f["tracks"].([]any); ok {
		for _, raw := range list {
			tr := raw.(map[string]any)
			c.mids[tr["mid"].(string)] = tr["track_id"].(string)
		}
	}
	c.mu.Unlock()
	sdpText, _ := f["sdp"].(string)
	if err := pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: sdpText}); err != nil {
		c.t.Logf("the server's offer could not be used: %v", err)
		return
	}
	ans, err := pc.CreateAnswer(nil)
	if err == nil {
		err = pc.SetLocalDescription(ans)
	}
	if err != nil {
		c.t.Logf("the server's offer could not be answered: %v", err)
		return
	}
	c.mu.Lock()
	c.nextID++
	id := fmt.Sprintf("c%d", c.nextID)
	c.mu.Unlock()
	sig.Send(map[string]any{"type": "call.answer", "id": id, "pc": "recv", "offer_id": f["offer_id"], "sdp": pc.LocalDescription().SDP})
}

// RestartRecv asks the server for a fresh offer with new ICE credentials.
func (c *Client) RestartRecv() map[string]any {
	return c.Request(map[string]any{"type": "call.restart", "pc": "recv"})
}

// onTrack counts what arrives of one forwarded track.
func (c *Client) onTrack(remote *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
	mid := ""
	c.mu.Lock()
	for _, tr := range c.recv.GetTransceivers() {
		if tr.Receiver() == receiver {
			mid = tr.Mid()
		}
	}
	id := c.mids[mid]
	c.mu.Unlock()
	s := c.Stream(id)
	go func() {
		for {
			if _, _, err := receiver.ReadRTCP(); err != nil {
				return
			}
		}
	}()
	video := remote.Kind() == webrtc.RTPCodecTypeVideo
	vp8 := strings.EqualFold(remote.Codec().MimeType, webrtc.MimeTypeVP8)
	for {
		pkt, _, err := remote.ReadRTP()
		if err != nil {
			return
		}
		// The track a media line carries can change with a later offer.
		c.mu.Lock()
		now := c.mids[mid]
		c.mu.Unlock()
		if now != id {
			id, s = now, c.Stream(now)
		}
		s.Packets.Add(1)
		s.Bytes.Add(int64(len(pkt.Payload)))
		s.mu.Lock()
		late := false
		if s.haveSeq && pkt.SequenceNumber != s.seq+1 {
			s.seqBreaks++
			if gap := pkt.SequenceNumber - s.seq; gap > 1 && gap < 0x8000 {
				s.lost += int64(gap - 1)
			} else if gap >= 0x8000 {
				// A packet from behind the newest: it was counted as lost
				// when it was skipped, and has only arrived out of order.
				late = true
				s.late++
				if s.lost > 0 {
					s.lost--
				}
			}
		}
		if s.haveSeq && int32(pkt.Timestamp-s.lastTimestamp) < 0 {
			s.tsBack++
		}
		if !late {
			s.seq = pkt.SequenceNumber
		}
		s.haveSeq, s.lastTimestamp = true, pkt.Timestamp
		p := pkt.Payload
		mark, key := byte('a'), false
		var body []byte
		switch {
		case !video:
			if len(p) > 0 {
				mark, body = p[0], p
			}
		case vp8 && len(p) > 5:
			pic := uint16(p[2]&0x7f)<<8 | uint16(p[3])
			// Every packet of a frame has the frame's number; the next
			// frame has the next.
			if s.havePic && pic != s.pic && pic != (s.pic+1)&0x7fff {
				s.picBreaks++
			}
			s.havePic, s.pic = true, pic
			key, mark, body = p[0]&0x10 != 0 && p[4]&0x01 == 0, p[5], p[5:]
		case len(p) > 1:
			key, mark, body = p[0]&0x1f == 5, p[1], p[1:]
		}
		if c.opts.Encrypt == nil && len(body) >= 9 {
			if d := time.Since(time.Unix(0, int64(binary.BigEndian.Uint64(body[1:9])))); d >= 0 && d < time.Minute {
				s.delaySum += d
				s.delayCount++
				if d > s.delayMax {
					s.delayMax = d
				}
			}
		}
		if video && s.last != 0 && mark != s.last && !key {
			s.badSwitches++
		}
		if s.byLayer == nil {
			s.byLayer = map[byte]int{}
		}
		s.byLayer[mark]++
		s.last = mark
		s.lastPayload = append(s.lastPayload[:0], p...)
		s.mu.Unlock()
	}
}

// Stream returns what has arrived of a track, by its identifier.
func (c *Client) Stream(trackID string) *Stream {
	c.mu.Lock()
	defer c.mu.Unlock()
	s := c.streams[trackID]
	if s == nil {
		s = &Stream{}
		c.streams[trackID] = s
	}
	return s
}

// Subscribe sends `call.subscribe` with a list of track, width and height,
// and returns the `video` list of the answer.
func (c *Client) Subscribe(video ...[3]any) []map[string]any {
	c.t.Helper()
	list := []map[string]any{}
	for _, v := range video {
		list = append(list, map[string]any{"track_id": v[0], "width": v[1], "height": v[2]})
	}
	f := c.OK(map[string]any{"type": "call.subscribe", "video": list})
	var out []map[string]any
	for _, raw := range f["video"].([]any) {
		out = append(out, raw.(map[string]any))
	}
	return out
}

// Selected says how the send connection reaches the server: "udp" or
// "tcp", and the server address in use.
func (c *Client) Selected() (protocol, address string, err error) {
	c.mu.Lock()
	pc := c.send
	c.mu.Unlock()
	if pc == nil {
		return "", "", errors.New("the client has no send connection")
	}
	for _, s := range pc.GetSenders() {
		if tr := s.Transport(); tr != nil && tr.ICETransport() != nil {
			pair, err := tr.ICETransport().GetSelectedCandidatePair()
			if err != nil || pair == nil {
				return "", "", errors.New("no candidate pair is selected")
			}
			return strings.ToLower(pair.Remote.Protocol.String()), fmt.Sprintf("%s:%d", pair.Remote.Address, pair.Remote.Port), nil
		}
	}
	return "", "", errors.New("the send connection has no transport yet")
}

// SendState and RecvState are the ICE states of the two connections.
func (c *Client) SendState() webrtc.ICEConnectionState {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.send == nil {
		return webrtc.ICEConnectionStateNew
	}
	return c.send.ICEConnectionState()
}

func (c *Client) RecvState() webrtc.ICEConnectionState {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.recv == nil {
		return webrtc.ICEConnectionStateNew
	}
	return c.recv.ICEConnectionState()
}

// DropMedia closes both media connections without a word to the server, as
// a device does when it loses its network.
func (c *Client) DropMedia() {
	c.mu.Lock()
	send, recv := c.send, c.recv
	c.send, c.recv = nil, nil
	for _, p := range c.pubs {
		close(p.stop)
	}
	c.pubs = map[string]*published{}
	c.mu.Unlock()
	if send != nil {
		send.Close()
	}
	if recv != nil {
		recv.Close()
	}
}

// Close ends the client's media and stops it reading its connection.
func (c *Client) Close() {
	if c.closed.Swap(true) {
		return
	}
	c.DropMedia()
}

// quiet drops what Pion would log about a test client.
type quiet struct{}

func (quiet) NewLogger(string) logging.LeveledLogger { return quiet{} }
func (quiet) Trace(string)                           {}
func (quiet) Tracef(string, ...any)                  {}
func (quiet) Debug(string)                           {}
func (quiet) Debugf(string, ...any)                  {}
func (quiet) Info(string)                            {}
func (quiet) Infof(string, ...any)                   {}
func (quiet) Warn(string)                            {}
func (quiet) Warnf(string, ...any)                   {}
func (quiet) Error(string)                           {}
func (quiet) Errorf(string, ...any)                  {}
