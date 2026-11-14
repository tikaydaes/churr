package blob

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"

	"golang.org/x/image/draw"
	"golang.org/x/image/webp"
)

const (
	// ThumbnailEdge is the longest edge of a thumbnail, in pixels.
	ThumbnailEdge = 720

	// MaxPixels is the largest image, width times height, that Thumbnail
	// will decode. A few kilobytes of image file can claim to be a hundred
	// thousand pixels square, and decoding it would take all the memory
	// there is. 64 megapixels is above what phones and most cameras make.
	//
	// It is still a lot of memory: a decoded image takes up to 8 bytes a
	// pixel, and scaling takes more. A caller that thumbnails uploads as
	// they arrive should do only a few at a time.
	MaxPixels = 64_000_000

	// jpegQuality is the quality thumbnails are encoded at: no visible loss
	// at this size, at roughly a third of the bytes of the maximum.
	jpegQuality = 85
)

// ErrNotAnImage is returned for content that is not a PNG, JPEG, GIF or
// WebP image this package can decode.
var ErrNotAnImage = errors.New("blob: not a PNG, JPEG, GIF or WebP image")

// ErrTooManyPixels is returned for an image whose header claims more than
// MaxPixels pixels. Such an image gets no thumbnail; it is not decoded.
var ErrTooManyPixels = errors.New("blob: the image has too many pixels to make a thumbnail of")

// Thumb is a finished thumbnail.
type Thumb struct {
	// Data is the encoded image.
	Data []byte
	// MediaType is TypePNG when the thumbnail has transparency in it, and
	// TypeJPEG otherwise.
	MediaType string
	// Width and Height are its size in pixels.
	Width, Height int
}

// Thumbnail makes a thumbnail of the image in src and returns it encoded.
// It is MakeThumbnail for a caller that only wants to store the bytes; the
// result is a JPEG or a PNG, and Sniff tells which.
func Thumbnail(src io.Reader) (io.Reader, error) {
	t, err := MakeThumbnail(src)
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(t.Data), nil
}

// MakeThumbnail makes a thumbnail of the PNG, JPEG, GIF or WebP image in
// src: at most ThumbnailEdge pixels on its long edge, and never larger than
// the original.
//
// The thumbnail is drawn afresh from the decoded pixels, so nothing else
// in the file survives: no location, camera details, comments or colour
// profile. A JPEG's orientation tag is applied before it is dropped, so a
// photo taken with the phone on its side still comes out upright. Of an
// animated GIF it shows the first frame; an animated WebP cannot be decoded
// and gives ErrNotAnImage.
//
// The result is a PNG if any pixel of it is see-through, and a JPEG
// otherwise.
//
// The image's size is read from its header and checked against MaxPixels
// before anything is decoded.
func MakeThumbnail(src io.Reader) (*Thumb, error) {
	info, rest, err := readImageInfo(src)
	if err != nil {
		return nil, err
	}
	if int64(info.width)*int64(info.height) > MaxPixels {
		return nil, fmt.Errorf("%w: %d by %d", ErrTooManyPixels, info.width, info.height)
	}

	var img image.Image
	switch info.mediaType {
	case TypePNG:
		img, err = png.Decode(rest)
	case TypeJPEG:
		img, err = jpeg.Decode(rest)
	case TypeGIF:
		// Decode, unlike DecodeAll, stops after the first frame.
		img, err = gif.Decode(rest)
	case TypeWebP:
		img, err = webp.Decode(rest)
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNotAnImage, err)
	}

	b := img.Bounds()
	w, h := thumbnailSize(b.Dx(), b.Dy())
	small := image.NewRGBA(image.Rect(0, 0, w, h))
	if w == b.Dx() && h == b.Dy() {
		draw.Draw(small, small.Bounds(), img, b.Min, draw.Src)
	} else {
		// Catmull-Rom weighs every source pixel that falls under a
		// destination pixel, so fine detail averages out instead of
		// turning into speckle as it does with nearest-neighbour.
		draw.CatmullRom.Scale(small, small.Bounds(), img, b, draw.Src, nil)
	}
	// Turning the small image is far cheaper than turning the large one.
	small = oriented(small, info.orientation)

	var out bytes.Buffer
	t := &Thumb{Width: small.Bounds().Dx(), Height: small.Bounds().Dy()}
	if small.Opaque() {
		t.MediaType = TypeJPEG
		err = jpeg.Encode(&out, small, &jpeg.Options{Quality: jpegQuality})
	} else {
		// JPEG has no transparency; it would turn see-through areas black.
		t.MediaType = TypePNG
		err = png.Encode(&out, small)
	}
	if err != nil {
		return nil, fmt.Errorf("blob: encoding a thumbnail: %w", err)
	}
	t.Data = out.Bytes()
	return t, nil
}

// thumbnailSize is the size an image of w by h is scaled to: the long edge
// brought down to ThumbnailEdge, the other in proportion, and nothing
// changed if the image is already small enough.
func thumbnailSize(w, h int) (int, int) {
	long := max(w, h)
	if long <= ThumbnailEdge {
		return w, h
	}
	scale := func(n int) int {
		// Rounded to the nearest pixel, and never to none.
		return max(1, (n*ThumbnailEdge+long/2)/long)
	}
	return scale(w), scale(h)
}

// imageInfo is what an image's header says about it.
type imageInfo struct {
	mediaType     string
	width, height int // as stored, before orientation is applied
	orientation   int // the EXIF orientation, 1 to 8; 1 means upright
}

// readImageInfo reads the header of the image at the start of src. It
// returns what the header says, and a reader that yields the whole image
// from its first byte again: what was read to get this far, then the rest
// of src. So the image can be decoded afterwards without src having to be
// seekable and without holding the file in memory.
func readImageInfo(src io.Reader) (imageInfo, io.Reader, error) {
	// Everything read from src is kept in head as it goes by.
	var head bytes.Buffer
	tee := io.TeeReader(src, &head)

	magic := make([]byte, 12) // enough to tell the four formats apart
	n, err := io.ReadFull(tee, magic)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return imageInfo{}, nil, err
	}
	info := imageInfo{orientation: 1}
	info.mediaType, _ = Sniff(magic[:n])

	// Each decoder is called by name. image.DecodeConfig would also accept
	// whatever other formats some package elsewhere in the program had
	// registered.
	var cfg image.Config
	header := io.MultiReader(bytes.NewReader(magic[:n]), tee)
	switch info.mediaType {
	case TypePNG:
		cfg, err = png.DecodeConfig(header)
	case TypeJPEG:
		cfg, err = jpeg.DecodeConfig(header)
	case TypeGIF:
		cfg, err = gif.DecodeConfig(header)
	case TypeWebP:
		cfg, err = webp.DecodeConfig(header)
	default:
		return imageInfo{}, nil, ErrNotAnImage
	}
	if err != nil {
		return imageInfo{}, nil, fmt.Errorf("%w: %v", ErrNotAnImage, err)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return imageInfo{}, nil, fmt.Errorf("%w: it has no size", ErrNotAnImage)
	}
	info.width, info.height = cfg.Width, cfg.Height
	if info.mediaType == TypeJPEG {
		// The EXIF block comes before the size in a JPEG, so by now it has
		// gone by and is in head.
		info.orientation = exifOrientation(head.Bytes())
	}
	return info, io.MultiReader(bytes.NewReader(head.Bytes()), src), nil
}

// exifOrientation finds the orientation tag in the start of a JPEG file.
// It returns 1, upright, when there is none or it cannot be read: a photo
// shown sideways is a better failure than no thumbnail.
func exifOrientation(jpg []byte) int {
	// A JPEG is a run of segments, each a marker (0xFF and a code) and,
	// for most, a two-byte length that counts itself. EXIF data is in an
	// APP1 segment that begins "Exif\0\0".
	pos := 2 // past the start-of-image marker
	for pos+4 <= len(jpg) {
		if jpg[pos] != 0xFF {
			return 1
		}
		marker := jpg[pos+1]
		switch {
		case marker == 0xFF: // padding before a marker
			pos++
			continue
		case marker == 0xDA || marker == 0xD9: // image data, or the end
			return 1
		case marker >= 0xD0 && marker <= 0xD7 || marker == 0x01: // no length
			pos += 2
			continue
		}
		length := int(binary.BigEndian.Uint16(jpg[pos+2:]))
		if length < 2 {
			return 1
		}
		end := min(pos+2+length, len(jpg))
		if seg := jpg[pos+4 : end]; marker == 0xE1 && bytes.HasPrefix(seg, []byte("Exif\x00\x00")) {
			return tiffOrientation(seg[6:])
		}
		pos += 2 + length
	}
	return 1
}

// tiffOrientation reads the Orientation entry from the first directory of
// a TIFF structure, which is what an EXIF block is inside. It returns 1 if
// the entry is missing or makes no sense.
func tiffOrientation(tiff []byte) int {
	if len(tiff) < 8 {
		return 1
	}
	var order binary.ByteOrder
	switch string(tiff[0:4]) {
	case "II*\x00":
		order = binary.LittleEndian
	case "MM\x00*":
		order = binary.BigEndian
	default:
		return 1
	}
	// Offsets come from the file and may point anywhere; every one is
	// checked against the length before it is used.
	dir := uint64(order.Uint32(tiff[4:8]))
	if dir+2 > uint64(len(tiff)) {
		return 1
	}
	count := uint64(order.Uint16(tiff[dir:]))
	for i := uint64(0); i < count; i++ {
		entry := dir + 2 + 12*i
		if entry+12 > uint64(len(tiff)) {
			return 1
		}
		const orientationTag, typeShort = 0x0112, 3
		if order.Uint16(tiff[entry:]) != orientationTag {
			continue
		}
		if order.Uint16(tiff[entry+2:]) != typeShort || order.Uint32(tiff[entry+4:]) != 1 {
			return 1
		}
		if v := int(order.Uint16(tiff[entry+8:])); v >= 1 && v <= 8 {
			return v
		}
		return 1
	}
	return 1
}

// turned reports whether an orientation swaps width and height.
func turned(orientation int) bool { return orientation >= 5 && orientation <= 8 }

// oriented returns img as it is meant to be seen, given the EXIF
// orientation it was stored with. The eight values are the eight ways of
// turning and mirroring a rectangle.
func oriented(img *image.RGBA, orientation int) *image.RGBA {
	if orientation < 2 || orientation > 8 {
		return img
	}
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	dw, dh := w, h
	if turned(orientation) {
		dw, dh = h, w
	}
	out := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for y := 0; y < dh; y++ {
		for x := 0; x < dw; x++ {
			// (sx, sy) is the stored pixel that is seen at (x, y).
			var sx, sy int
			switch orientation {
			case 2: // mirrored left to right
				sx, sy = w-1-x, y
			case 3: // upside down
				sx, sy = w-1-x, h-1-y
			case 4: // mirrored top to bottom
				sx, sy = x, h-1-y
			case 5: // mirrored, then a quarter turn
				sx, sy = y, x
			case 6: // needs a quarter turn clockwise
				sx, sy = y, h-1-x
			case 7: // mirrored, then a quarter turn the other way
				sx, sy = w-1-y, h-1-x
			case 8: // needs a quarter turn anticlockwise
				sx, sy = w-1-y, x
			}
			s := img.PixOffset(sx, sy)
			d := out.PixOffset(x, y)
			copy(out.Pix[d:d+4], img.Pix[s:s+4])
		}
	}
	return out
}
