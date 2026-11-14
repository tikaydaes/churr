package blob

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/tikaydaes/churr/internal/model"
)

// SniffLen is how much of a file's start Sniff needs to do its best. It
// works on less, but may then miss what it would have recognised.
const SniffLen = 512

// The media types this package gives the kinds that are drawn inline.
const (
	TypePNG  = "image/png"
	TypeJPEG = "image/jpeg"
	TypeGIF  = "image/gif"
	TypeWebP = "image/webp"
	TypeMP4  = "video/mp4"
	TypeWebM = "video/webm"
	TypeMP3  = "audio/mpeg"
	TypeM4A  = "audio/mp4"
	TypeOgg  = "audio/ogg"
	TypeWAV  = "audio/wav"
	// TypeText is what every text file is called, whatever it holds. HTML
	// and scripts are text here and are never given a type a browser would
	// run.
	TypeText = "text/plain; charset=utf-8"
	// TypeUnknown is for content nothing is known about, and for content
	// that must not be given its real type (see Sniff on SVG).
	TypeUnknown = "application/octet-stream"
)

// inlineTypes are the types that go with the kinds image, video, audio and
// text. Sniff gives them out only on the strength of its own checks, never
// on the word of net/http's detector, which is looser about some of them.
var inlineTypes = map[string]bool{
	TypePNG: true, TypeJPEG: true, TypeGIF: true, TypeWebP: true,
	TypeMP4: true, TypeWebM: true,
	TypeMP3: true, TypeM4A: true, TypeOgg: true, TypeWAV: true,
	"audio/wave": true, "application/ogg": true,
}

// Sniff works out what a file is from its first bytes: its media type and
// its kind, one of model.FileImage, FileVideo, FileAudio, FileText and
// FileOther (docs/MESSAGE-FORMAT.md section 5). Give it the first SniffLen
// bytes or more. It never looks at a name.
//
//	image   PNG, JPEG, GIF, WebP
//	video   MP4, WebM
//	audio   MP3, M4A, Ogg, WAV
//	text    valid UTF-8 with no NUL bytes
//	file    anything else
//
// Things worth knowing:
//
//   - The "under 1 MB" half of the text rule is the caller's to apply; a
//     larger text file should be stored with kind file. Sniff sees only the
//     head, so the caller may also want IsText over the whole content.
//   - SVG is always kind file, and its type is application/octet-stream,
//     not image/svg+xml. An SVG can carry script, and a type that starts
//     with "image/" invites some later code to show it as one.
//   - MP4 and M4A are the same container. They are told apart by the brand
//     the file declares, since the head does not say what tracks follow.
//     QuickTime, HEIC, AVIF and 3GPP share the container too and are kind
//     file, as is Matroska, which shares WebM's.
//   - Which codecs are inside is not checked. An MP4 that is not H.264
//     with AAC is still kind video, and a player may fail to play it.
//   - An empty file is kind file.
func Sniff(head []byte) (mediaType, kind string) {
	switch {
	case bytes.HasPrefix(head, []byte("\x89PNG\r\n\x1a\n")):
		return TypePNG, model.FileImage
	case bytes.HasPrefix(head, []byte("\xff\xd8\xff")):
		return TypeJPEG, model.FileImage
	case bytes.HasPrefix(head, []byte("GIF87a")), bytes.HasPrefix(head, []byte("GIF89a")):
		return TypeGIF, model.FileImage
	}
	if len(head) >= 12 && string(head[0:4]) == "RIFF" {
		switch string(head[8:12]) {
		case "WEBP":
			return TypeWebP, model.FileImage
		case "WAVE":
			return TypeWAV, model.FileAudio
		}
	}
	if len(head) >= 12 && string(head[4:8]) == "ftyp" {
		return sniffISO(head)
	}
	if bytes.HasPrefix(head, []byte("\x1a\x45\xdf\xa3")) {
		// EBML holds both WebM and Matroska; only its DocType tells them
		// apart. Browsers play the first and not the second.
		switch ebmlDocType(head) {
		case "webm":
			return TypeWebM, model.FileVideo
		case "matroska":
			return "video/x-matroska", model.FileOther
		}
		return TypeUnknown, model.FileOther
	}
	if bytes.HasPrefix(head, []byte("OggS")) {
		if oggHasVideo(head) {
			return "video/ogg", model.FileOther
		}
		return TypeOgg, model.FileAudio
	}
	if isMP3(head) {
		return TypeMP3, model.FileAudio
	}
	if len(head) > 0 && IsText(head) {
		if looksLikeSVG(head) {
			return TypeUnknown, model.FileOther
		}
		return TypeText, model.FileText
	}

	// Everything else is kind file. net/http knows a good list of types
	// (PDF, zip, fonts and so on) that make a download more useful.
	t := http.DetectContentType(head)
	base, _, _ := strings.Cut(t, ";")
	if inlineTypes[base] || strings.HasPrefix(base, "text/") {
		// It thinks this is something the checks above said it is not, or
		// text that is not UTF-8. Either way we do not know what it is.
		return TypeUnknown, model.FileOther
	}
	return t, model.FileOther
}

// sniffISO tells apart the formats built on the ISO base media container,
// whose files all begin with an "ftyp" box naming a major brand and a list
// of brands they are compatible with.
func sniffISO(head []byte) (mediaType, kind string) {
	if t, k := isoBrand(string(head[8:12])); t != "" {
		return t, k
	}
	// A major brand we do not know. See whether the file says it can be
	// read as plain MP4, as files from newer tools generally do.
	end := int64(binary.BigEndian.Uint32(head[0:4]))
	if end > int64(len(head)) || end < 16 {
		end = int64(len(head))
	}
	for i := int64(16); i+4 <= end; i += 4 {
		if t, k := isoBrand(string(head[i : i+4])); t == TypeMP4 {
			return t, k
		}
	}
	return TypeUnknown, model.FileOther
}

// isoBrand gives the type and kind for one brand, or "" for a brand that
// says nothing we can use.
func isoBrand(brand string) (mediaType, kind string) {
	switch brand {
	case "M4A ", "M4B ", "M4P ":
		return TypeM4A, model.FileAudio
	case "avc1", "dash", "M4V ", "MSNV", "mmp4":
		return TypeMP4, model.FileVideo
	case "qt  ":
		return "video/quicktime", model.FileOther
	case "heic", "heix", "hevc", "hevx", "heim", "heis", "hevm", "hevs", "mif1", "msf1":
		return "image/heic", model.FileOther
	case "avif", "avis":
		return "image/avif", model.FileOther
	}
	switch {
	case strings.HasPrefix(brand, "iso"), strings.HasPrefix(brand, "mp4"):
		// isom, iso2 … iso9, mp41, mp42.
		return TypeMP4, model.FileVideo
	case strings.HasPrefix(brand, "3gp"):
		return "video/3gpp", model.FileOther
	case strings.HasPrefix(brand, "3g2"):
		return "video/3gpp2", model.FileOther
	}
	return "", ""
}

// ebmlDocType reads the DocType out of an EBML header: "webm", "matroska",
// or "" if it cannot be found in head.
func ebmlDocType(head []byte) string {
	const docType = 0x4282
	pos := 4 // past the EBML header's own identifier
	size, n := ebmlNumber(head[pos:], true)
	if n == 0 {
		return ""
	}
	pos += n
	end := len(head)
	if size < uint64(end-pos) {
		end = pos + int(size)
	}
	for pos < end {
		id, n := ebmlNumber(head[pos:end], false)
		if n == 0 {
			return ""
		}
		pos += n
		size, n := ebmlNumber(head[pos:end], true)
		if n == 0 {
			return ""
		}
		pos += n
		if size > uint64(end-pos) {
			return ""
		}
		if id == docType {
			return string(bytes.TrimRight(head[pos:pos+int(size)], "\x00"))
		}
		pos += int(size)
	}
	return ""
}

// ebmlNumber reads one of EBML's variable-length numbers from the start of
// b and returns it with how many bytes it took, or 0 bytes if b does not
// hold a whole one. The count of leading zero bits in the first byte says
// how long the number is. An element's size has the marker bit removed;
// an element's identifier keeps it.
func ebmlNumber(b []byte, stripMarker bool) (value uint64, length int) {
	if len(b) == 0 || b[0] == 0 {
		return 0, 0
	}
	length = 1
	for mask := byte(0x80); b[0]&mask == 0; mask >>= 1 {
		length++
	}
	if len(b) < length {
		return 0, 0
	}
	value = uint64(b[0])
	if stripMarker {
		value &= uint64(0xFF) >> length
	}
	for _, c := range b[1:length] {
		value = value<<8 | uint64(c)
	}
	return value, length
}

// oggHasVideo reports whether the first stream in an Ogg file is video.
// Ogg is kind audio in the format's table, and a file of Theora video is
// not something an audio player can do anything with.
func oggHasVideo(head []byte) bool {
	// A page header is 27 bytes and then one length byte for each segment;
	// the first packet, which names the codec, follows.
	if len(head) < 27 {
		return false
	}
	start := 27 + int(head[26])
	if start > len(head) {
		return false
	}
	packet := head[start:]
	return bytes.HasPrefix(packet, []byte("\x80theora")) || bytes.HasPrefix(packet, []byte("\x01video"))
}

// isMP3 reports whether head starts an MP3 file: an ID3v2 tag, or an MPEG
// Layer III audio frame.
func isMP3(head []byte) bool {
	// "ID3", a version of 2, 3 or 4, a revision, flags, then a length
	// written in four bytes that each keep their top bit clear.
	if len(head) >= 10 && string(head[0:3]) == "ID3" && head[3] >= 2 && head[3] <= 4 && head[4] != 0xFF &&
		head[6]&0x80 == 0 && head[7]&0x80 == 0 && head[8]&0x80 == 0 && head[9]&0x80 == 0 {
		return true
	}
	n, ok := mp3FrameLen(head)
	if !ok {
		return false
	}
	// Two bytes of sync are thin evidence. Where the head reaches as far as
	// the next frame, that one must be there too.
	if n+4 <= len(head) {
		_, ok = mp3FrameLen(head[n:])
	}
	return ok
}

// mp3FrameLen checks that b starts with the header of an MPEG Layer III
// frame and returns the frame's length in bytes.
func mp3FrameLen(b []byte) (int, bool) {
	if len(b) < 4 || b[0] != 0xFF || b[1]&0xE0 != 0xE0 {
		return 0, false
	}
	version := b[1] >> 3 & 3 // 0: MPEG 2.5, 1: reserved, 2: MPEG 2, 3: MPEG 1
	layer := b[1] >> 1 & 3   // 1: Layer III
	bitrate := b[2] >> 4
	rate := b[2] >> 2 & 3
	padding := int(b[2] >> 1 & 1)
	emphasis := b[3] & 3
	if version == 1 || layer != 1 || bitrate == 0 || bitrate == 15 || rate == 3 || emphasis == 2 {
		return 0, false
	}
	var kbps, hz int
	if version == 3 {
		kbps = [...]int{0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320}[bitrate]
		hz = [...]int{44100, 48000, 32000}[rate]
		return 144000*kbps/hz + padding, true
	}
	kbps = [...]int{0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160}[bitrate]
	hz = [...]int{22050, 24000, 16000}[rate]
	if version == 0 {
		hz /= 2
	}
	return 72000*kbps/hz + padding, true
}

// utf8BOM is the byte-order mark some editors put at the start of a UTF-8
// file.
const utf8BOM = "\xef\xbb\xbf"

// IsText reports whether head is valid UTF-8 with no NUL bytes, which is
// what makes a file kind text. A character cut in half at the very end is
// forgiven, since head is usually the start of something longer.
func IsText(head []byte) bool {
	if bytes.IndexByte(head, 0) >= 0 {
		return false
	}
	return utf8.Valid(trimPartialRune(head))
}

// trimPartialRune drops the start of a character from the end of b when
// the rest of that character is missing.
func trimPartialRune(b []byte) []byte {
	// A character is at most four bytes, so its first byte is among the
	// last three or the character is complete.
	for i := len(b) - 1; i >= 0 && i >= len(b)-3; i-- {
		if utf8.RuneStart(b[i]) {
			if !utf8.FullRune(b[i:]) {
				return b[:i]
			}
			break
		}
	}
	return b
}

// looksLikeSVG reports whether text is an SVG document: markup, with an
// <svg tag in the part we can see.
func looksLikeSVG(head []byte) bool {
	s := bytes.TrimLeft(bytes.TrimPrefix(head, []byte(utf8BOM)), " \t\r\n")
	if len(s) == 0 || s[0] != '<' {
		return false
	}
	return bytes.Contains(bytes.ToLower(s), []byte("<svg"))
}

// The limits of a text preview.
const (
	// PreviewLines is how many lines of a text file are shown inline.
	PreviewLines = 20
	// PreviewBytes caps the preview's size. Twenty lines of a minified
	// script could otherwise be the whole megabyte.
	PreviewBytes = 8 << 10
)

// ErrNotText is returned by PreviewText for content that is not text.
var ErrNotText = errors.New("blob: not UTF-8 text")

// PreviewText returns the start of a text file for preview_text: its first
// PreviewLines lines, and no more than PreviewBytes bytes of them. Line
// ends are returned as "\n" whatever the file used, and a byte-order mark
// is dropped. It reads only as much of r as it needs.
func PreviewText(r io.Reader) (string, error) {
	// A little more than the cap, so that a character lying across it can
	// be seen to be whole or not.
	data, err := io.ReadAll(io.LimitReader(r, PreviewBytes+utf8.UTFMax))
	if err != nil {
		return "", err
	}
	if len(data) > PreviewBytes {
		// Cut between characters, not through one.
		data = trimPartialRune(data[:PreviewBytes])
	}
	if bytes.IndexByte(data, 0) >= 0 || !utf8.Valid(data) {
		return "", ErrNotText
	}
	text := strings.TrimPrefix(string(data), utf8BOM)
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	lines := strings.SplitN(text, "\n", PreviewLines+1)
	if len(lines) > PreviewLines {
		lines = lines[:PreviewLines]
	}
	return strings.Join(lines, "\n"), nil
}
