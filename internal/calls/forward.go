package calls

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/pion/rtcp"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
)

// downTrack forwards one published track to one subscriber. It is the
// track Pion sends on the subscriber's recv connection, and it rewrites
// sequence numbers, timestamps and VP8 picture numbers so the subscriber
// sees one continuous stream whichever layer is being forwarded.
type downTrack struct {
	id    string
	pub   *pubTrack
	sub   *participant
	codec webrtc.RTPCodecParameters
	video bool
	// sender is set once, before any packet is written.
	sender *webrtc.RTPSender

	// target is the layer the picker wants forwarded, by index; -1 is
	// none (paused, muted). The forwarding loop moves to it at the
	// layer's next key frame.
	target atomic.Int32

	mu      sync.Mutex
	bound   bool
	out     webrtc.TrackLocalWriter
	ssrc    uint32
	payload uint8
	vp8     bool
	// current is the layer being forwarded now, or -1.
	current int

	// What makes the stream continuous across switches: the offsets
	// added to what the publisher sent, and the last values sent out.
	started  bool
	seqOff   uint16
	tsOff    uint32
	picOff   uint16
	tl0Off   uint8
	lastSeq  uint16
	lastTS   uint32
	lastPic  uint16
	lastTL0  uint8
	lastAt   time.Time
	lastIn   uint16
	haveLast bool
}

// Bind is called by Pion when the subscriber has answered an offer that
// carries this track.
func (d *downTrack) Bind(c webrtc.TrackLocalContext) (webrtc.RTPCodecParameters, error) {
	for _, p := range c.CodecParameters() {
		if codecName(p.MimeType) == codecName(d.codec.MimeType) {
			d.mu.Lock()
			d.bound, d.out, d.ssrc, d.payload = true, c.WriteStream(), uint32(c.SSRC()), uint8(p.PayloadType)
			d.vp8 = codecName(p.MimeType) == codecVP8
			// Whatever was being forwarded, a subscriber that has just
			// been bound has seen none of it: begin at a key frame.
			d.current = -1
			d.mu.Unlock()
			if lvl := int(d.target.Load()); lvl >= 0 {
				d.pub.askKey(lvl)
			}
			return p, nil
		}
	}
	return webrtc.RTPCodecParameters{}, webrtc.ErrUnsupportedCodec
}

// Unbind is called by Pion when the track leaves the connection.
func (d *downTrack) Unbind(webrtc.TrackLocalContext) error {
	d.mu.Lock()
	d.bound, d.out = false, nil
	d.mu.Unlock()
	return nil
}

// ID is the track's identifier within the call.
func (d *downTrack) ID() string { return d.id }

// RID is empty: a subscriber is sent one layer at a time.
func (d *downTrack) RID() string { return "" }

// StreamID groups a participant's tracks: its account.
func (d *downTrack) StreamID() string { return string(d.pub.owner.id) }

// Kind is audio or video.
func (d *downTrack) Kind() webrtc.RTPCodecType {
	if d.video {
		return webrtc.RTPCodecTypeVideo
	}
	return webrtc.RTPCodecTypeAudio
}

// write forwards one packet of one layer, if that is the layer this
// subscriber is on. It reports true when it is waiting for a key frame of
// that layer, so the caller can ask the publisher for one. scratch is the
// caller's buffer, used when the payload must be changed.
func (d *downTrack) write(level int, pkt *rtp.Packet, key bool, vp8 vp8Descriptor, scratch []byte) (needKey bool) {
	target := int(d.target.Load())
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.bound || target < 0 {
		d.current = -1
		return false
	}
	resync := false
	if level != d.current {
		if level != target {
			return false
		}
		// A subscriber is moved to a layer only at one of its key
		// frames; until then it stays on the old one.
		if d.video && !key {
			return true
		}
		d.current, resync = level, true
	}
	payload := pkt.Payload
	if len(payload) == 0 {
		// Padding carries nothing to forward. Close the gap it would
		// leave in the numbering.
		if d.started && pkt.SequenceNumber-d.lastIn < 0x8000 && pkt.SequenceNumber != d.lastIn {
			d.seqOff--
			d.lastIn = pkt.SequenceNumber
		}
		return false
	}
	now := time.Now()
	if resync || !d.started {
		if d.started {
			// Carry on from the last packet sent: the next sequence
			// number, and a timestamp as far on as time has passed.
			gap := uint32(now.Sub(d.lastAt).Seconds() * float64(d.codec.ClockRate))
			if gap == 0 {
				gap = 1
			}
			d.seqOff = d.lastSeq + 1 - pkt.SequenceNumber
			d.tsOff = d.lastTS + gap - pkt.Timestamp
			if vp8.hasPicture {
				d.picOff = d.lastPic + 1 - vp8.picture
			}
			if vp8.hasTL0 {
				d.tl0Off = d.lastTL0 + 1 - vp8.tl0
			}
		}
		d.started = true
		d.lastIn = pkt.SequenceNumber
	}
	h := pkt.Header
	h.SSRC, h.PayloadType = d.ssrc, d.payload
	h.SequenceNumber += d.seqOff
	h.Timestamp += d.tsOff
	// Header extensions are numbered per connection; the publisher's
	// numbers mean nothing on this one. The subscriber needs none of them.
	h.Extension, h.Extensions, h.ExtensionProfile, h.CSRC = false, nil, 0, nil
	h.Padding, h.PaddingSize = false, 0
	if d.vp8 && (vp8.hasPicture || vp8.hasTL0) {
		pic, tl0 := vp8.picture+d.picOff, vp8.tl0+d.tl0Off
		if !vp8.wide {
			pic &= 0x7f
		} else {
			pic &= 0x7fff
		}
		if (d.picOff != 0 || d.tl0Off != 0) && len(payload) <= len(scratch) {
			// Only the descriptor the packetizer wrote is changed; the
			// frame after it goes on as it came.
			n := copy(scratch, payload)
			payload = scratch[:n]
			rewriteVP8(payload, vp8, pic, tl0)
		}
		if vp8.hasPicture && (!d.haveLast || (pic-d.lastPic)&0x7fff < 0x4000) {
			d.lastPic = pic
		}
		if vp8.hasTL0 && (!d.haveLast || tl0-d.lastTL0 < 0x80) {
			d.lastTL0 = tl0
		}
	}
	if !d.haveLast || h.SequenceNumber-d.lastSeq < 0x8000 {
		d.lastSeq, d.lastTS, d.lastAt = h.SequenceNumber, h.Timestamp, now
		if pkt.SequenceNumber-d.lastIn < 0x8000 {
			d.lastIn = pkt.SequenceNumber
		}
	}
	d.haveLast = true
	if _, err := d.out.WriteRTP(&h, payload); err == nil {
		d.sub.room.forwarded.Add(1)
		d.sub.sentBytes.Add(int64(len(payload)) + 12)
	}
	return false
}

// feedback reads what the subscriber reports about this track. Reading is
// what lets lost packets be sent again and the downlink be estimated; and a
// subscriber's request for a key frame becomes a request to the publisher.
func (d *downTrack) feedback(sender *webrtc.RTPSender) {
	for {
		pkts, _, err := sender.ReadRTCP()
		if err != nil {
			return
		}
		for _, p := range pkts {
			switch p.(type) {
			case *rtcp.PictureLossIndication, *rtcp.FullIntraRequest:
				d.mu.Lock()
				lvl := d.current
				d.mu.Unlock()
				if lvl < 0 {
					lvl = int(d.target.Load())
				}
				d.pub.askKey(lvl)
			}
		}
	}
}

// receive is the forwarding loop of one layer of one published track: it
// reads the publisher's packets and writes each to the subscribers that are
// on this layer.
func (t *pubTrack) receive(level int, remote *webrtc.TrackRemote, audioLevelID uint8) {
	l := t.layers[level]
	l.ssrc.Store(uint32(remote.SSRC()))
	buf := make([]byte, 1500)
	scratch := make([]byte, 1500)
	room := t.owner.room
	var pkt rtp.Packet
	for {
		n, _, err := remote.Read(buf)
		if err != nil {
			return
		}
		if t.closed.Load() {
			continue
		}
		pkt = rtp.Packet{}
		if pkt.Unmarshal(buf[:n]) != nil {
			continue
		}
		now := time.Now()
		l.lastPacket.Store(now.UnixNano())
		if t.muted.Load() {
			// A muted track sends nothing; if a client sends anyway, it
			// goes no further.
			continue
		}
		var (
			key bool
			vp8 vp8Descriptor
		)
		switch {
		case t.kind == kindAudio:
			if audioLevelID != 0 {
				if ext := pkt.GetExtension(audioLevelID); len(ext) > 0 {
					room.speakers.observe(t.owner.id, ext[0]&0x7f, now)
				}
			}
		case len(pkt.Payload) == 0:
		case t.codec == codecVP8:
			if d, ok := parseVP8(pkt.Payload); ok {
				vp8, key = d, d.keyframe
			}
		case t.codec == codecH264:
			key = h264Keyframe(pkt.Payload)
		}
		subs := t.subs.Load()
		if subs == nil {
			continue
		}
		ask := false
		for _, d := range *subs {
			if d.write(level, &pkt, key, vp8, scratch) {
				ask = true
			}
		}
		if ask {
			t.askKey(level)
		}
	}
}
