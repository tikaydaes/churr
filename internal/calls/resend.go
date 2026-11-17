package calls

import (
	"sync"
	"time"

	"github.com/pion/rtcp"
	"github.com/pion/rtp"
)

// Sending a lost packet again (docs/CALLS.md section 5, "Feedback").
//
// The packets are kept once, where they arrive: each layer of a published
// video track holds its last second or two (keptPackets). A subscriber's
// side holds no packets, only a note per packet it was sent saying which
// packet of which layer that was and how it was renumbered (sentNote).
// A request names a sequence number the subscriber saw; the note leads
// back to the kept packet, which is renumbered the same way and sent
// again. What a call holds for this therefore grows with the cameras
// published and not with cameras times people.

// keptPacket is one packet as the publisher sent it.
type keptPacket struct {
	seq     uint16
	ts      uint32
	marker  bool
	held    bool
	payload []byte
}

// keptPackets is the newest packets of one layer, by sequence number.
type keptPackets struct {
	mu    sync.Mutex
	slots []keptPacket
}

// keptFor is how many packets a layer of this bit rate keeps: a power of
// two that covers about two seconds of it, from 64 to 512.
func keptFor(kbps int) int {
	n := 64
	for n < kbps/5 && n < 512 {
		n *= 2
	}
	return n
}

func newKeptPackets(kbps int) *keptPackets {
	return &keptPackets{slots: make([]keptPacket, keptFor(kbps))}
}

// put keeps a copy of a packet, in place of the one that many before it.
// A slot's bytes are used again, so a steady stream allocates nothing.
func (k *keptPackets) put(pkt *rtp.Packet) {
	k.mu.Lock()
	s := &k.slots[int(pkt.SequenceNumber)&(len(k.slots)-1)]
	s.seq, s.ts, s.marker, s.held = pkt.SequenceNumber, pkt.Timestamp, pkt.Marker, true
	s.payload = append(s.payload[:0], pkt.Payload...)
	k.mu.Unlock()
}

// get copies the kept packet with a sequence number into buf, which must
// hold any packet. ok is false when it is no longer kept.
func (k *keptPackets) get(seq uint16, buf []byte) (payload []byte, ts uint32, marker, ok bool) {
	k.mu.Lock()
	defer k.mu.Unlock()
	s := &k.slots[int(seq)&(len(k.slots)-1)]
	if !s.held || s.seq != seq || len(s.payload) > len(buf) {
		return nil, 0, false, false
	}
	return buf[:copy(buf, s.payload)], s.ts, s.marker, true
}

// drop forgets every packet and lets their bytes go.
func (k *keptPackets) drop() {
	k.mu.Lock()
	for i := range k.slots {
		k.slots[i] = keptPacket{}
	}
	k.mu.Unlock()
}

// sentNotes is how many packets back a subscriber can ask for: as many as
// the largest layer keeps.
const sentNotes = 512

// sentNote says what one packet sent to a subscriber was made from.
type sentNote struct {
	out uint16 // the sequence number the subscriber saw
	src uint16 // the publisher's
	// The offsets that were added to the publisher's numbers.
	tsOff  uint32
	picOff uint16
	tl0Off uint8
	// level is the layer the packet came from, plus one; 0 is no note.
	level uint8
	// again is when it was last sent again, in milliseconds; 0 is never.
	again uint32
}

// resendGapOver is added to the round trip to the subscriber to make the
// least time between two resends of one packet: a subscriber asks again
// until the packet arrives, and until a round trip has passed the first
// copy is still on its way. Before any round trip has been measured this
// alone is the gap.
const resendGapOver = 5 * time.Millisecond

// What is sent again to a subscriber of a track is bounded by the bit rate
// of the layer it came from: a burst of up to resendBurst of it, and after
// that no faster than the layer itself. A subscriber losing more than that
// needs a smaller layer, which the downlink limit gives it (downlink.go),
// not more packets.
const resendBurst = time.Second

// note records a packet about to be written. Call with d.mu held.
func (d *downTrack) note(out, src uint16, level int) {
	if d.sent == nil {
		d.sent = make([]sentNote, sentNotes)
	}
	d.sent[out&(sentNotes-1)] = sentNote{out: out, src: src, tsOff: d.tsOff, picOff: d.picOff, tl0Off: d.tl0Off, level: uint8(level + 1)}
}

// resend answers one lost-packet request from the subscriber. buf is the
// caller's, and holds any packet.
func (d *downTrack) resend(nack *rtcp.TransportLayerNack, buf []byte) {
	for i := range nack.Nacks {
		more := true
		nack.Nacks[i].Range(func(seq uint16) bool {
			more = d.resendOne(seq, buf)
			return more
		})
		if !more {
			return
		}
	}
}

// resendOne sends one packet again, if it is still kept. It reports false
// when no more may be sent for now.
func (d *downTrack) resendOne(seq uint16, buf []byte) bool {
	now := time.Now()
	ms := uint32(now.UnixMilli())
	if ms == 0 {
		ms = 1
	}
	d.mu.Lock()
	if !d.bound || d.sent == nil {
		d.mu.Unlock()
		return false
	}
	n := &d.sent[seq&(sentNotes-1)]
	if n.level == 0 || n.out != seq || int(n.level) > len(d.pub.layers) {
		d.mu.Unlock()
		return true
	}
	if n.again != 0 && time.Duration(ms-n.again)*time.Millisecond < d.rtt+resendGapOver {
		d.mu.Unlock()
		return true
	}
	layer := d.pub.layers[n.level-1]
	if layer.kept == nil {
		d.mu.Unlock()
		return true
	}
	if !d.mayResend(now, layer.spec.kbps) {
		d.mu.Unlock()
		return false
	}
	note, out, vp8 := *n, d.out, d.vp8
	h := rtp.Header{Version: 2, PayloadType: d.payload, SequenceNumber: seq, SSRC: d.ssrc}
	d.mu.Unlock()
	payload, ts, marker, ok := layer.kept.get(note.src, buf)
	if !ok {
		return true
	}
	h.Timestamp, h.Marker = ts+note.tsOff, marker
	if vp8 && (note.picOff != 0 || note.tl0Off != 0) {
		if desc, ok := parseVP8(payload); ok && (desc.hasPicture || desc.hasTL0) {
			pic := desc.picture + note.picOff
			if desc.wide {
				pic &= 0x7fff
			} else {
				pic &= 0x7f
			}
			rewriteVP8(payload, desc, pic, desc.tl0+note.tl0Off)
		}
	}
	if _, err := out.WriteRTP(&h, payload); err != nil {
		return true
	}
	d.mu.Lock()
	// The note may have been written over meanwhile; then there is
	// nothing to mark.
	if n := &d.sent[seq&(sentNotes-1)]; n.out == seq && n.level == note.level {
		n.again = ms
	}
	d.resendLeft -= len(payload) + 12
	d.mu.Unlock()
	d.sub.room.resent.Add(1)
	return true
}

// mayResend reports whether anything may be sent again now, for a packet
// of a layer of this bit rate, and tops up what may be. Call with d.mu
// held.
func (d *downTrack) mayResend(now time.Time, kbps int) bool {
	rate := kbps * 125 // bytes a second
	most := int(float64(rate) * resendBurst.Seconds())
	if d.resendAt.IsZero() {
		d.resendAt, d.resendLeft = now, most
	} else if more := int(now.Sub(d.resendAt).Seconds() * float64(rate)); more > 0 {
		// The time is moved on only when it bought something, so
		// requests that come close together still add up.
		d.resendAt, d.resendLeft = now, min(most, d.resendLeft+more)
	}
	return d.resendLeft > 0
}

// heard takes the subscriber's reports of what it received, for the round
// trip to it: the time since the sender report a reception report names,
// less how long the subscriber held it.
func (d *downTrack) heard(reports []rtcp.ReceptionReport, now time.Time) {
	for _, r := range reports {
		if r.LastSenderReport == 0 {
			continue
		}
		// In 65,536ths of a second. A value that is not a round trip (a
		// report of another clock, a stale one) is past any real one.
		v := ntpShort(now) - r.LastSenderReport - r.Delay
		if v >= 4<<16 {
			continue
		}
		rtt := time.Duration(v) * time.Second >> 16
		d.mu.Lock()
		if r.SSRC == d.ssrc {
			if d.rtt == 0 {
				d.rtt = rtt
			} else {
				d.rtt += (rtt - d.rtt) / 8
			}
		}
		d.mu.Unlock()
	}
}

// ntpShort is the middle 32 bits of a time as NTP writes it, which is how
// sender and reception reports name a moment: seconds since 1900 and
// 65,536ths of a second.
func ntpShort(t time.Time) uint32 {
	return uint32((uint64(t.Unix())+2208988800)<<16 | uint64(t.Nanosecond())<<16/1e9)
}
