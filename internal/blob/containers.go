package blob

import (
	"bytes"
	"encoding/binary"
	"io"
	"math"
)

// The readers in this file take the length and picture size of audio and
// video from what the container says about itself. Nothing is decoded, and
// nothing a file claims is trusted for more than showing a player of the
// right shape: every size read from the file is checked against the file's
// real length before it is used to seek or allocate.
//
// Each is given src and reports ok = false when it cannot read the file.
// Width and height stay zero for audio. These small readers settle the open
// question of docs/STRUCTURE.md section 8: no container parser is brought in.

// maxContainerRead is the most any reader here holds in memory at once.
const maxContainerRead = 4 << 20

// seconds turns a length into whole seconds: rounded, and never 0 for
// something that does play.
func seconds(d float64) int {
	if math.IsNaN(d) || d <= 0 || d > 1e9 {
		return 0
	}
	if d < 1 {
		return 1
	}
	return int(math.Round(d))
}

// fileLen is the length of src.
func fileLen(src io.Seeker) int64 {
	n, err := src.Seek(0, io.SeekEnd)
	if err != nil {
		return 0
	}
	return n
}

// readAt reads exactly n bytes at an offset.
func readAt(src io.ReadSeeker, off int64, n int) ([]byte, bool) {
	if n < 0 || n > maxContainerRead {
		return nil, false
	}
	if _, err := src.Seek(off, io.SeekStart); err != nil {
		return nil, false
	}
	b := make([]byte, n)
	if _, err := io.ReadFull(src, b); err != nil {
		return nil, false
	}
	return b, true
}

// mp4Boxes calls fn for each box between start and end: its type and where
// its body lies. It seeks over bodies and never reads them, so a "moov"
// that comes after gigabytes of "mdat" is still found. fn returns true to
// stop.
func mp4Boxes(src io.ReadSeeker, start, end int64, fn func(kind string, bodyStart, bodyEnd int64) bool) {
	// A file cannot hold more boxes than this; a damaged one must not make
	// the walk go on for ever.
	for n := 0; start+8 <= end && n < 10000; n++ {
		h, ok := readAt(src, start, 8)
		if !ok {
			return
		}
		size := int64(binary.BigEndian.Uint32(h))
		body := start + 8
		switch size {
		case 0: // to the end of the file
			size = end - start
		case 1: // the real size follows, in 64 bits
			big, ok := readAt(src, start+8, 8)
			if !ok {
				return
			}
			size = int64(binary.BigEndian.Uint64(big))
			body = start + 16
		}
		if size < body-start || start+size > end {
			return
		}
		if fn(string(h[4:8]), body, start+size) {
			return
		}
		start += size
	}
}

// probeMP4 reads an MP4 or M4A file: the length is duration over timescale
// from "mvhd", and the size is the width and height of the first track
// that has any, from its "tkhd", turned if its matrix turns the picture.
func probeMP4(src io.ReadSeeker) (width, height, durationSeconds int, ok bool) {
	total := fileLen(src)
	mp4Boxes(src, 0, total, func(kind string, from, to int64) bool {
		if kind != "moov" {
			return false
		}
		mp4Boxes(src, from, to, func(kind string, from, to int64) bool {
			switch kind {
			case "mvhd":
				b, good := readAt(src, from, int(min(to-from, 32)))
				if !good || len(b) < 20 {
					return false
				}
				var scale, dur uint64
				if b[0] == 1 && len(b) >= 32 {
					scale, dur = uint64(binary.BigEndian.Uint32(b[20:])), binary.BigEndian.Uint64(b[24:])
				} else {
					scale, dur = uint64(binary.BigEndian.Uint32(b[12:])), uint64(binary.BigEndian.Uint32(b[16:]))
				}
				if scale > 0 && dur != math.MaxUint32 && dur != math.MaxUint64 {
					durationSeconds = seconds(float64(dur) / float64(scale))
				}
				ok = true
			case "trak":
				if width > 0 {
					return false
				}
				mp4Boxes(src, from, to, func(kind string, from, to int64) bool {
					if kind != "tkhd" {
						return false
					}
					b, good := readAt(src, from, int(min(to-from, 104)))
					if !good || len(b) == 0 {
						return true
					}
					// The matrix and the size are the last 44 bytes of
					// either version of the box.
					at := 40
					if b[0] == 1 {
						at = 52
					}
					if len(b) < at+44 {
						return true
					}
					matrix := b[at : at+36]
					w := int(binary.BigEndian.Uint32(b[at+36:]) >> 16)
					h := int(binary.BigEndian.Uint32(b[at+40:]) >> 16)
					// A quarter turn has zeros where an upright picture
					// has its scale: a and d of the matrix.
					if binary.BigEndian.Uint32(matrix[0:]) == 0 && binary.BigEndian.Uint32(matrix[16:]) == 0 {
						w, h = h, w
					}
					if w > 0 && h > 0 {
						width, height = w, h
					}
					return true
				})
			}
			return false
		})
		return true
	})
	return width, height, durationSeconds, ok
}

// The EBML elements probeWebM looks at.
const (
	ebmlHeader      = 0x1A45DFA3
	ebmlSegment     = 0x18538067
	ebmlInfo        = 0x1549A966
	ebmlTimeScale   = 0x2AD7B1
	ebmlDuration    = 0x4489
	ebmlTracks      = 0x1654AE6B
	ebmlTrackEntry  = 0xAE
	ebmlVideo       = 0xE0
	ebmlPixelWidth  = 0xB0
	ebmlPixelHeight = 0xBA
	ebmlCluster     = 0x1F43B675
)

// ebmlEach calls fn for each element in b with its identifier and body. An
// element whose size is unknown, or runs past b, is given what there is. fn
// returns true to stop.
func ebmlEach(b []byte, fn func(id uint64, body []byte) bool) {
	for len(b) > 0 {
		id, n := ebmlNumber(b, false)
		if n == 0 {
			return
		}
		size, m := ebmlNumber(b[n:], true)
		if m == 0 {
			return
		}
		body := b[n+m:]
		unknown := size == uint64(1)<<(7*uint(m))-1
		if !unknown && size < uint64(len(body)) {
			body = body[:size]
		}
		if fn(id, body) {
			return
		}
		if unknown || size >= uint64(len(b[n+m:])) {
			return
		}
		b = b[n+m+int(size):]
	}
}

func ebmlUint(b []byte) uint64 {
	var v uint64
	if len(b) > 8 {
		return 0
	}
	for _, c := range b {
		v = v<<8 | uint64(c)
	}
	return v
}

// probeWebM reads a WebM file: in the Segment, the length is Info's
// Duration times TimestampScale, and the size is PixelWidth and PixelHeight
// of the first video track. Both sit before the first Cluster, so only the
// start of the file is read. A file written live has no Duration and
// reports zero.
func probeWebM(src io.ReadSeeker) (width, height, durationSeconds int, ok bool) {
	if _, err := src.Seek(0, io.SeekStart); err != nil {
		return 0, 0, 0, false
	}
	head, err := io.ReadAll(io.LimitReader(src, 1<<20))
	if err != nil {
		return 0, 0, 0, false
	}
	ebmlEach(head, func(id uint64, body []byte) bool {
		if id != ebmlSegment {
			return false
		}
		ok = true
		ebmlEach(body, func(id uint64, body []byte) bool {
			switch id {
			case ebmlInfo:
				scale, dur := float64(1000000), float64(0)
				ebmlEach(body, func(id uint64, body []byte) bool {
					switch {
					case id == ebmlTimeScale:
						scale = float64(ebmlUint(body))
					case id == ebmlDuration && len(body) == 4:
						dur = float64(math.Float32frombits(binary.BigEndian.Uint32(body)))
					case id == ebmlDuration && len(body) == 8:
						dur = math.Float64frombits(binary.BigEndian.Uint64(body))
					}
					return false
				})
				durationSeconds = seconds(dur * scale / 1e9)
			case ebmlTracks:
				ebmlEach(body, func(id uint64, body []byte) bool {
					if id != ebmlTrackEntry {
						return false
					}
					ebmlEach(body, func(id uint64, body []byte) bool {
						if id != ebmlVideo {
							return false
						}
						var w, h uint64
						ebmlEach(body, func(id uint64, body []byte) bool {
							switch id {
							case ebmlPixelWidth:
								w = ebmlUint(body)
							case ebmlPixelHeight:
								h = ebmlUint(body)
							}
							return false
						})
						if w > 0 && h > 0 && w < 1<<16 && h < 1<<16 {
							width, height = int(w), int(h)
						}
						return true
					})
					return width > 0
				})
			case ebmlCluster:
				return true // the sound and picture start here
			}
			return false
		})
		return true
	})
	return width, height, durationSeconds, ok
}

// probeMP3 reads an MP3 file. After any ID3v2 tag, the first frame may
// carry a Xing, Info or VBRI header that counts the frames; otherwise the
// file is taken to be at one bit rate and the length worked out from its
// size.
func probeMP3(src io.ReadSeeker) (width, height, durationSeconds int, ok bool) {
	total := fileLen(src)
	start := int64(0)
	if h, good := readAt(src, 0, 10); good && string(h[:3]) == "ID3" {
		start = 10 + int64(h[6]&0x7F)<<21 + int64(h[7]&0x7F)<<14 + int64(h[8]&0x7F)<<7 + int64(h[9]&0x7F)
		if h[5]&0x10 != 0 {
			start += 10 // the tag has a footer
		}
	}
	if start >= total {
		return 0, 0, 0, false
	}
	b, good := readAt(src, start, int(min(total-start, 8192)))
	if !good {
		return 0, 0, 0, false
	}
	// Padding may sit between the tag and the first frame.
	at := -1
	for i := 0; i+4 <= len(b); i++ {
		if _, isFrame := mp3FrameLen(b[i:]); isFrame {
			at = i
			break
		}
	}
	if at < 0 {
		return 0, 0, 0, false
	}
	f := b[at:]
	version := f[1] >> 3 & 3 // 3: MPEG 1, 2: MPEG 2, 0: MPEG 2.5
	mono := f[3]>>6 == 3
	var kbps, hz, perFrame, side int
	if version == 3 {
		kbps = [...]int{0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320}[f[2]>>4]
		hz = [...]int{44100, 48000, 32000}[f[2]>>2&3]
		perFrame, side = 1152, 32
		if mono {
			side = 17
		}
	} else {
		kbps = [...]int{0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160}[f[2]>>4]
		hz = [...]int{22050, 24000, 16000}[f[2]>>2&3]
		if version == 0 {
			hz /= 2
		}
		perFrame, side = 576, 17
		if mono {
			side = 9
		}
	}
	frames := uint32(0)
	if x := 4 + side; len(f) >= x+12 && (string(f[x:x+4]) == "Xing" || string(f[x:x+4]) == "Info") {
		if binary.BigEndian.Uint32(f[x+4:])&1 != 0 {
			frames = binary.BigEndian.Uint32(f[x+8:])
		}
	} else if len(f) >= 36+18 && string(f[36:40]) == "VBRI" {
		frames = binary.BigEndian.Uint32(f[36+14:])
	}
	if frames > 0 {
		return 0, 0, seconds(float64(frames) * float64(perFrame) / float64(hz)), true
	}
	return 0, 0, seconds(float64(total-start-int64(at)) * 8 / float64(kbps*1000)), true
}

// probeOgg reads an Ogg audio file. The sample rate is in the first packet
// (48,000 always for Opus, with a pre-skip to subtract); the length is the
// granule position of the last page over that rate.
func probeOgg(src io.ReadSeeker) (width, height, durationSeconds int, ok bool) {
	total := fileLen(src)
	head, good := readAt(src, 0, int(min(total, 512)))
	if !good || len(head) < 28 || string(head[:4]) != "OggS" {
		return 0, 0, 0, false
	}
	first := 27 + int(head[26])
	if first > len(head) {
		return 0, 0, 0, false
	}
	packet := head[first:]
	var rate, skip float64
	switch {
	case bytes.HasPrefix(packet, []byte("OpusHead")) && len(packet) >= 12:
		rate, skip = 48000, float64(binary.LittleEndian.Uint16(packet[10:]))
	case bytes.HasPrefix(packet, []byte("\x01vorbis")) && len(packet) >= 16:
		rate = float64(binary.LittleEndian.Uint32(packet[12:]))
	default:
		return 0, 0, 0, false
	}
	if rate <= 0 {
		return 0, 0, 0, false
	}
	// The last page starts within the last 64 KB: a page is at most that
	// long.
	n := int(min(total, 65536+27+255))
	tail, good := readAt(src, total-int64(n), n)
	if !good {
		return 0, 0, 0, false
	}
	at := bytes.LastIndex(tail, []byte("OggS"))
	if at < 0 || at+14 > len(tail) {
		return 0, 0, 0, true
	}
	granule := int64(binary.LittleEndian.Uint64(tail[at+6:]))
	if granule <= 0 {
		return 0, 0, 0, true
	}
	return 0, 0, seconds((float64(granule) - skip) / rate), true
}

// probeWAV reads a WAV file: the length is the size of the "data" chunk
// over the byte rate in the "fmt " chunk.
func probeWAV(src io.ReadSeeker) (width, height, durationSeconds int, ok bool) {
	total := fileLen(src)
	h, good := readAt(src, 0, 12)
	if !good || string(h[:4]) != "RIFF" || string(h[8:12]) != "WAVE" {
		return 0, 0, 0, false
	}
	var byteRate uint32
	for at, n := int64(12), 0; at+8 <= total && n < 1000; n++ {
		c, good := readAt(src, at, 8)
		if !good {
			break
		}
		size := int64(binary.LittleEndian.Uint32(c[4:]))
		switch string(c[:4]) {
		case "fmt ":
			f, good := readAt(src, at+8, 16)
			if !good || size < 16 {
				return 0, 0, 0, false
			}
			byteRate = binary.LittleEndian.Uint32(f[8:])
		case "data":
			if byteRate == 0 {
				return 0, 0, 0, false
			}
			// A file written as a stream says 0 or all ones here; what
			// is really there is the rest of the file.
			if left := total - (at + 8); size == 0 || size > left {
				size = left
			}
			return 0, 0, seconds(float64(size) / float64(byteRate)), true
		}
		at += 8 + size + size&1 // chunks are padded to an even length
	}
	return 0, 0, 0, false
}
