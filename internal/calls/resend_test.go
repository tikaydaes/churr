package calls

import (
	"bytes"
	"sync"
	"testing"
	"time"
	"unsafe"

	"github.com/pion/rtcp"
	"github.com/pion/rtp"
)

func TestKeptPackets(t *testing.T) {
	for kbps, want := range map[int]int{audioKbps: 64, 150: 64, 500: 128, 1200: 256, screenKbps: 512, 50_000: 512} {
		if got := keptFor(kbps); got != want {
			t.Errorf("keptFor(%d) = %d, want %d", kbps, got, want)
		}
	}
	k := newKeptPackets(150)
	buf := make([]byte, 1500)
	for seq := uint16(65500); seq != 100; seq++ { // across the wrap
		k.put(&rtp.Packet{Header: rtp.Header{SequenceNumber: seq, Timestamp: uint32(seq) * 10, Marker: seq%2 == 0}, Payload: []byte{byte(seq), byte(seq >> 8), 7}})
	}
	for seq := uint16(99); seq != 99-64; seq-- {
		p, ts, marker, ok := k.get(seq, buf)
		if !ok || ts != uint32(seq)*10 || marker != (seq%2 == 0) || !bytes.Equal(p, []byte{byte(seq), byte(seq >> 8), 7}) {
			t.Fatalf("packet %d: %v %d %v %v", seq, p, ts, marker, ok)
		}
	}
	// The one before those has been written over, and a number never
	// kept is not found.
	if _, _, _, ok := k.get(99-64, buf); ok {
		t.Fatal("a packet older than the layer keeps was still there")
	}
	if _, _, _, ok := k.get(30000, buf); ok {
		t.Fatal("a packet never kept was found")
	}
	// A packet longer than the caller's buffer is not handed over cut.
	if _, _, _, ok := k.get(99, buf[:2]); ok {
		t.Fatal("a packet was cut to fit")
	}
	k.drop()
	if _, _, _, ok := k.get(99, buf); ok {
		t.Fatal("a packet outlived drop")
	}
}

// wrote is a subscriber's connection that records what is written to it.
type wrote struct {
	mu   sync.Mutex
	pkts []rtp.Packet
}

func (w *wrote) WriteRTP(h *rtp.Header, payload []byte) (int, error) {
	w.mu.Lock()
	w.pkts = append(w.pkts, rtp.Packet{Header: h.Clone(), Payload: append([]byte(nil), payload...)})
	w.mu.Unlock()
	return len(payload), nil
}
func (w *wrote) Write([]byte) (int, error) { return 0, nil }

func (w *wrote) take() []rtp.Packet {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := w.pkts
	w.pkts = nil
	return out
}

// vp8Packet is a VP8 payload with a fifteen-bit picture number and a
// TL0PICIDX, as browsers send.
func vp8Packet(pic uint16, tl0 uint8, key bool, body byte) []byte {
	first := byte(0x01) // not a key frame
	if key {
		first = 0
	}
	return []byte{0x90, 0xc0, 0x80 | byte(pic>>8), byte(pic), tl0, first, body, body, body}
}

// A packet sent again is the packet that was sent: the same numbers the
// subscriber saw, whichever layer it came from and however the numbering
// had been moved by then. The subscriber's side keeps no packets.
func TestResendIsWhatWasSent(t *testing.T) {
	r := &room{}
	sub := &participant{room: r}
	pub := &pubTrack{id: "t1", kind: kindVideo, codec: codecVP8, owner: &participant{room: r}, named: true}
	for _, spec := range cameraLayers {
		pub.layers = append(pub.layers, &pubLayer{spec: option{name: spec.name, kbps: spec.kbps}, kept: newKeptPackets(spec.kbps)})
	}
	out := &wrote{}
	d := &downTrack{id: "t1", pub: pub, sub: sub, codec: vp8Codec, video: true, bound: true, out: out, ssrc: 77, payload: 96, vp8: true, current: -1}
	d.target.Store(0)
	scratch := make([]byte, 1500)
	feed := func(level int, seq uint16, ts uint32, pic uint16, key bool) {
		pkt := &rtp.Packet{Header: rtp.Header{Version: 2, SequenceNumber: seq, Timestamp: ts, Marker: true, SSRC: uint32(level + 1)}, Payload: vp8Packet(pic, uint8(pic), key, byte(seq))}
		pub.layers[level].kept.put(pkt)
		desc, _ := parseVP8(pkt.Payload)
		d.write(level, pkt, key, desc, scratch)
	}
	// Ten packets of q, then a switch to f, whose numbers are elsewhere.
	for i := 0; i < 10; i++ {
		feed(0, 1000+uint16(i), 90000+uint32(i)*3000, 500+uint16(i), i == 0)
	}
	d.target.Store(2)
	for i := 0; i < 10; i++ {
		feed(2, 40000+uint16(i), 7_000_000+uint32(i)*3000, 9000+uint16(i), i == 0)
	}
	first := out.take()
	if len(first) != 20 {
		t.Fatalf("%d packets were forwarded, want 20", len(first))
	}
	if d.picOff == 0 {
		t.Fatal("the switch did not move the picture numbers, so the test shows nothing")
	}
	// Ask for one of each layer again, and one that was never sent.
	buf := make([]byte, 1500)
	ask := func(seqs ...uint16) []rtp.Packet {
		var pairs []rtcp.NackPair
		for _, s := range seqs {
			pairs = append(pairs, rtcp.NackPair{PacketID: s})
		}
		d.resend(&rtcp.TransportLayerNack{MediaSSRC: 77, Nacks: pairs}, buf)
		return out.take()
	}
	again := ask(first[3].SequenceNumber, first[14].SequenceNumber, first[19].SequenceNumber+50)
	if len(again) != 2 {
		t.Fatalf("%d packets were sent again, want 2", len(again))
	}
	for i, want := range []rtp.Packet{first[3], first[14]} {
		got := again[i]
		if got.SequenceNumber != want.SequenceNumber || got.Timestamp != want.Timestamp || got.SSRC != want.SSRC || got.PayloadType != want.PayloadType ||
			got.Marker != want.Marker || !bytes.Equal(got.Payload, want.Payload) {
			t.Fatalf("sent again: %+v %x\nfirst sent: %+v %x", got.Header, got.Payload, want.Header, want.Payload)
		}
	}
	if r.resent.Load() != 2 {
		t.Fatalf("resent = %d", r.resent.Load())
	}
	// The round trip to the subscriber, from its reception reports: it
	// names a sender report of 180 ms ago and held it for 80 ms.
	if d.heard([]rtcp.ReceptionReport{{SSRC: 78, LastSenderReport: ntpShort(time.Now().Add(-time.Second)), Delay: 1}}, time.Now()); d.rtt != 0 {
		t.Fatal("a report about another stream was taken")
	}
	now := time.Now()
	d.heard([]rtcp.ReceptionReport{{SSRC: 77, LastSenderReport: ntpShort(now.Add(-180 * time.Millisecond)), Delay: 80 * 65536 / 1000}}, now)
	if d.rtt < 99*time.Millisecond || d.rtt > 101*time.Millisecond {
		t.Fatalf("the round trip is %s, want 100ms", d.rtt)
	}
	// Asked again at once, as a client does before the copy has arrived:
	// nothing more is sent. After a round trip and a little it is.
	if got := ask(first[3].SequenceNumber); len(got) != 0 {
		t.Fatal("a packet was sent again twice within a round trip")
	}
	time.Sleep(d.rtt + resendGapOver + 40*time.Millisecond)
	if got := ask(first[3].SequenceNumber); len(got) != 1 {
		t.Fatal("a packet asked for again after a round trip was not sent")
	}
	// A packet the layer no longer keeps is not sent.
	pub.layers[0].kept.drop()
	if got := ask(first[5].SequenceNumber); len(got) != 0 {
		t.Fatal("a packet that is no longer kept was sent")
	}
	// What is sent again is bounded by the layer's bit rate. Layer f is
	// 1,200 kbit/s, 150,000 bytes a second: a burst of that much, here
	// 124 packets of 1,212 bytes, and then 150 bytes more each
	// millisecond.
	big := make([]byte, 1200)
	copy(big, vp8Packet(1, 1, false, 0))
	for i := 0; i < 200; i++ {
		pkt := &rtp.Packet{Header: rtp.Header{Version: 2, SequenceNumber: 41000 + uint16(i), Timestamp: 8_000_000}, Payload: big}
		pub.layers[2].kept.put(pkt)
		desc, _ := parseVP8(pkt.Payload)
		d.write(2, pkt, false, desc, scratch)
	}
	var all []uint16
	for _, p := range out.take() {
		all = append(all, p.SequenceNumber)
	}
	// As on a track that has sent nothing again yet: the whole burst is
	// there to use.
	d.resendAt = time.Time{}
	began := time.Now()
	burst := ask(all...)
	if len(burst) < 122 || len(burst) > 127 {
		t.Fatalf("%d packets of 1,212 bytes were sent again in a burst, want 124", len(burst))
	}
	spent := ask(all[150:]...)
	if len(spent) > 2 {
		t.Fatalf("%d packets were sent again with the burst used up", len(spent))
	}
	time.Sleep(200 * time.Millisecond)
	later := ask(all[150:]...)
	if len(later) < 20 {
		t.Fatalf("%d packets were sent again 200 ms after the burst, want about 25", len(later))
	}
	// However long the test was held up, no more went than the burst and
	// the layer's rate since.
	if sent, most := len(burst)+len(spent)+len(later), int((1+time.Since(began).Seconds())*150_000/1212)+3; sent > most {
		t.Fatalf("%d packets were sent again in %s, more than the %d the layer's bit rate allows", sent, time.Since(began), most)
	}
	// Unbound, nothing is written.
	d.Unbind(nil)
	if got := ask(all[len(all)-1]); len(got) != 0 {
		t.Fatal("a packet was sent again on a track that has left the connection")
	}
	// What a subscriber's side holds for all this is the notes and
	// nothing else: 16 bytes for each of the last 512 packets.
	if len(d.sent) != sentNotes || unsafe.Sizeof(sentNote{}) != 16 {
		t.Fatalf("the notes are %d of %d bytes, want %d of 16", len(d.sent), unsafe.Sizeof(sentNote{}), sentNotes)
	}
}
