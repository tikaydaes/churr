package blob

import (
	"errors"
	"io"
)

// Probe reads the size of an image and the size and length of video and
// audio: the width, height and duration_seconds of a file
// (docs/MESSAGE-FORMAT.md section 5). It reads src from its start.
// Anything it cannot tell is zero; a file it does not understand at all
// gives three zeros, which is not an error, since those fields are optional.
//
// An image's width and height are as it is seen, after a JPEG's
// orientation tag is applied, so a photo taken on its side reports the
// taller-than-wide size a client will draw. Only the header is read, so
// this is safe on an image of any claimed size.
//
// Video and audio are read from their containers (containers.go): the
// numbers the file states about itself, with nothing decoded. A file that
// does not state them reports zeros and clients show a player without a
// known length or shape.
func Probe(src io.ReadSeeker) (width, height, durationSeconds int) {
	if _, err := src.Seek(0, io.SeekStart); err != nil {
		return 0, 0, 0
	}
	head := make([]byte, SniffLen)
	n, err := io.ReadFull(src, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return 0, 0, 0
	}
	mediaType, _ := Sniff(head[:n])
	if _, err := src.Seek(0, io.SeekStart); err != nil {
		return 0, 0, 0
	}

	var ok bool
	switch mediaType {
	case TypePNG, TypeJPEG, TypeGIF, TypeWebP:
		info, _, err := readImageInfo(src)
		if err != nil {
			return 0, 0, 0
		}
		if turned(info.orientation) {
			return info.height, info.width, 0
		}
		return info.width, info.height, 0
	case TypeMP4, TypeM4A:
		width, height, durationSeconds, ok = probeMP4(src)
	case TypeWebM:
		width, height, durationSeconds, ok = probeWebM(src)
	case TypeMP3:
		width, height, durationSeconds, ok = probeMP3(src)
	case TypeOgg:
		width, height, durationSeconds, ok = probeOgg(src)
	case TypeWAV:
		width, height, durationSeconds, ok = probeWAV(src)
	}
	if !ok {
		return 0, 0, 0
	}
	return width, height, durationSeconds
}
