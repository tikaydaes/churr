package calls

// What the forwarder reads of a media payload. It never decodes a frame,
// and in a sealed chat it could not: the devices encrypt each frame before
// it is split into packets (docs/CALLS.md section 14). Everything read here
// is either written by the sender's packetizer around the frame, outside
// the encryption, or is among the leading bytes the frame format leaves
// readable.
//
//	Opus   nothing. Loudness comes from the RTP header extension.
//	VP8    the payload descriptor (RFC 7741 section 4.2, one to six bytes,
//	       written by the packetizer), and bit 0 of the first byte after it
//	       in the first packet of a frame: the frame header's inverse
//	       key-frame flag. The picture number and TL0PICIDX inside the
//	       descriptor are rewritten; nothing after the descriptor is.
//	H.264  the first byte (the NAL unit header: its type); for a STAP-A
//	       packet the two-byte size and the header byte of each unit in it;
//	       for an FU-A packet the second byte (start bit and unit type).
//	       Nothing is rewritten.

// vp8Descriptor is the parsed VP8 payload descriptor of one RTP packet.
type vp8Descriptor struct {
	// size is how many bytes the descriptor takes.
	size int
	// start is true for the first packet of a frame (S set, partition 0).
	start bool
	// keyframe is true for the first packet of a key frame.
	keyframe bool
	// hasPicture and picture: the picture number, 7 or 15 bits wide.
	hasPicture bool
	picture    uint16
	wide       bool
	pictureAt  int
	// hasTL0 and tl0: the running index of temporal base layer frames.
	hasTL0 bool
	tl0    uint8
	tl0At  int
}

// parseVP8 reads the payload descriptor. ok is false for a payload too
// short to hold what its own flags promise.
func parseVP8(p []byte) (d vp8Descriptor, ok bool) {
	if len(p) < 1 {
		return d, false
	}
	first := p[0]
	i := 1
	if first&0x80 != 0 { // X: extension byte follows
		if len(p) < 2 {
			return d, false
		}
		ext := p[1]
		i = 2
		if ext&0x80 != 0 { // I: picture number
			if len(p) <= i {
				return d, false
			}
			d.hasPicture, d.pictureAt = true, i
			if p[i]&0x80 != 0 { // M: fifteen bits
				if len(p) <= i+1 {
					return d, false
				}
				d.wide = true
				d.picture = uint16(p[i]&0x7f)<<8 | uint16(p[i+1])
				i += 2
			} else {
				d.picture = uint16(p[i])
				i++
			}
		}
		if ext&0x40 != 0 { // L: TL0PICIDX
			if len(p) <= i {
				return d, false
			}
			d.hasTL0, d.tl0, d.tl0At = true, p[i], i
			i++
		}
		if ext&0x30 != 0 { // T or K: one byte for both
			if len(p) <= i {
				return d, false
			}
			i++
		}
	}
	d.size = i
	d.start = first&0x10 != 0 && first&0x07 == 0
	if d.start && len(p) > i {
		// The frame header's first byte: bit 0 is clear on a key frame.
		d.keyframe = p[i]&0x01 == 0
	}
	return d, true
}

// rewriteVP8 writes a new picture number and TL0PICIDX into a payload whose
// descriptor is d, keeping their widths.
func rewriteVP8(p []byte, d vp8Descriptor, picture uint16, tl0 uint8) {
	if d.hasPicture {
		if d.wide {
			p[d.pictureAt] = 0x80 | byte(picture>>8)&0x7f
			p[d.pictureAt+1] = byte(picture)
		} else {
			p[d.pictureAt] = byte(picture) & 0x7f
		}
	}
	if d.hasTL0 {
		p[d.tl0At] = tl0
	}
}

// H.264 NAL unit types the forwarder tells apart.
const (
	nalIDR   = 5
	nalSPS   = 7
	nalSTAPA = 24
	nalFUA   = 28
)

// h264Keyframe reports whether an H.264 RTP payload begins a key frame: it
// carries a sequence parameter set or the start of an IDR slice. A layer is
// switched to only at such a packet.
func h264Keyframe(p []byte) bool {
	if len(p) < 1 {
		return false
	}
	switch p[0] & 0x1f {
	case nalIDR, nalSPS:
		return true
	case nalSTAPA:
		// Each unit: a two-byte size, then the unit.
		for i := 1; i+2 < len(p); {
			size := int(p[i])<<8 | int(p[i+1])
			if t := p[i+2] & 0x1f; t == nalIDR || t == nalSPS {
				return true
			}
			i += 2 + size
			if size == 0 {
				break
			}
		}
	case nalFUA:
		if len(p) < 2 {
			return false
		}
		t := p[1] & 0x1f
		return p[1]&0x80 != 0 && (t == nalIDR || t == nalSPS)
	}
	return false
}
