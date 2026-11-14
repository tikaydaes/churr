package blob

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tikaydaes/churr/internal/model"
)

var ctx = context.Background()

func TestDisk(t *testing.T) {
	dir := t.TempDir()
	d, err := Disk(dir)
	if err != nil {
		t.Fatal(err)
	}
	content := []byte("hello, churr")
	key, size, sum, err := d.Put(ctx, bytes.NewReader(content))
	if err != nil {
		t.Fatal(err)
	}
	want := sha256.Sum256(content)
	if size != int64(len(content)) || !bytes.Equal(sum, want[:]) {
		t.Fatalf("size %d, sum %x", size, sum)
	}
	// disk:3f/a1/3fa1c0…: a random name under two levels of folders.
	parts := strings.Split(strings.TrimPrefix(key, "disk:"), "/")
	if !strings.HasPrefix(key, "disk:") || len(parts) != 3 || !strings.HasPrefix(parts[2], parts[0]+parts[1]) {
		t.Fatalf("key %q is not in the documented shape", key)
	}
	rc, err := d.Get(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(rc)
	rc.Close()
	if !bytes.Equal(got, content) {
		t.Fatalf("read back %q", got)
	}
	other, _, _, _ := d.Put(ctx, bytes.NewReader(content))
	if other == key {
		t.Fatal("two blobs share a key; each file row must own its blob")
	}

	// Keys that are not this store's, or that try to leave it.
	for _, bad := range []string{"s3:abc", "disk:../../etc/passwd", "disk:/etc/passwd", "disk:aa/bb/../../../x", "nonsense", ""} {
		if _, err := d.Get(ctx, bad); err == nil {
			t.Errorf("Get(%q) should be refused", bad)
		}
		if err := d.Delete(ctx, bad); err == nil {
			t.Errorf("Delete(%q) should be refused", bad)
		}
	}
	if err := d.Delete(ctx, key); err != nil {
		t.Fatal(err)
	}
	if err := d.Delete(ctx, key); err != nil {
		t.Fatalf("deleting a blob that is already gone is not an error: %v", err)
	}
	if _, err := d.Get(ctx, key); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("a missing blob is not_found: %v", err)
	}

	// Over the limit nothing is stored, not even a partial file.
	before := countFiles(t, dir)
	if _, _, _, err := d.Put(ctx, LimitReader(bytes.NewReader(make([]byte, 100)), 99)); !errors.Is(err, model.ErrTooLarge) {
		t.Fatalf("over the limit: %v", err)
	}
	if _, _, _, err := d.Put(ctx, LimitReader(bytes.NewReader(make([]byte, 100)), 100)); err != nil {
		t.Fatalf("exactly at the limit passes: %v", err)
	}
	if after := countFiles(t, dir); after != before+1 {
		t.Fatalf("a refused upload left something behind: %d files before, %d after", before, after)
	}
}

func countFiles(t *testing.T, dir string) int {
	n := 0
	filepath.WalkDir(dir, func(_ string, e os.DirEntry, err error) error {
		if err == nil && !e.IsDir() {
			n++
		}
		return nil
	})
	return n
}

// memory is a Blobs in a map, standing in for a second store.
type memory struct {
	prefix string
	m      map[string][]byte
}

func (s *memory) Put(_ context.Context, r io.Reader) (string, int64, []byte, error) {
	b, err := io.ReadAll(r)
	if err != nil {
		return "", 0, nil, err
	}
	key := s.prefix + model.NewID[string]()
	s.m[key] = b
	return key, int64(len(b)), nil, nil
}

func (s *memory) Get(_ context.Context, key string) (io.ReadCloser, error) {
	b, ok := s.m[key]
	if !ok {
		return nil, model.ErrNotFound
	}
	return io.NopCloser(bytes.NewReader(b)), nil
}

func (s *memory) Delete(_ context.Context, key string) error { delete(s.m, key); return nil }

func TestMixed(t *testing.T) {
	disk := &memory{"disk:", map[string][]byte{}}
	s3 := &memory{"s3:", map[string][]byte{}}
	old, _, _, _ := disk.Put(ctx, strings.NewReader("an older file"))
	m := Mixed(disk, s3, "s3")
	key, _, _, err := m.Put(ctx, strings.NewReader("a new file"))
	if err != nil || !strings.HasPrefix(key, "s3:") {
		t.Fatalf("new files go to the configured store: %q %v", key, err)
	}
	// A server that switched stores still finds every older file.
	for _, k := range []string{old, key} {
		rc, err := m.Get(ctx, k)
		if err != nil {
			t.Fatalf("Get(%q): %v", k, err)
		}
		rc.Close()
	}
	if err := m.Delete(ctx, old); err != nil || len(disk.m) != 0 {
		t.Fatalf("delete goes to where the blob lives: %v", err)
	}
	if _, err := m.Get(ctx, "ftp:nope"); err == nil {
		t.Fatal("a key of neither store should be refused")
	}
	if _, _, _, err := Mixed(disk, nil, "s3").Put(ctx, strings.NewReader("x")); !errors.Is(err, model.ErrUnavailable) {
		t.Fatalf("writing to a store that is not set up: %v", err)
	}
	if _, err := S3(S3Config{}); err == nil {
		t.Fatal("S3 is not built yet and must say so")
	}
}

func pngBytes(t *testing.T, w, h int, alpha bool) []byte {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			a := uint8(255)
			if alpha && x < w/2 {
				a = 0
			}
			img.Set(x, y, color.NRGBA{uint8(x), uint8(y), 120, a})
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func jpegBytes(t *testing.T, w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{uint8(x), uint8(y), 60, 255})
		}
	}
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, nil); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func gifBytes(t *testing.T, w, h int) []byte {
	img := image.NewPaletted(image.Rect(0, 0, w, h), color.Palette{color.Black, color.White})
	var b bytes.Buffer
	if err := gif.Encode(&b, img, nil); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestSniff(t *testing.T) {
	// Magic numbers of each kind MESSAGE-FORMAT.md section 5 names. The
	// type comes from the content, never from a name.
	mp4 := append([]byte{0, 0, 0, 24, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm', 0, 0, 0, 0, 'i', 's', 'o', 'm', 'a', 'v', 'c', '1'}, make([]byte, 40)...)
	m4a := append([]byte{0, 0, 0, 24, 'f', 't', 'y', 'p', 'M', '4', 'A', ' ', 0, 0, 0, 0, 'M', '4', 'A', ' ', 'm', 'p', '4', '2'}, make([]byte, 40)...)
	webp := append([]byte("RIFF\x24\x00\x00\x00WEBPVP8 "), make([]byte, 30)...)
	wav := append([]byte("RIFF\x24\x00\x00\x00WAVEfmt "), make([]byte, 30)...)
	ogg := append([]byte("OggS\x00\x02"), make([]byte, 60)...)
	for _, c := range []struct {
		name string
		head []byte
		kind string
		typ  string
	}{
		{"png", pngBytes(t, 4, 4, false), model.FileImage, "image/png"},
		{"jpeg", jpegBytes(t, 4, 4), model.FileImage, "image/jpeg"},
		{"gif", gifBytes(t, 4, 4), model.FileImage, "image/gif"},
		{"webp", webp, model.FileImage, "image/webp"},
		{"mp4", mp4, model.FileVideo, "video/mp4"},
		{"m4a", m4a, model.FileAudio, ""},
		{"wav", wav, model.FileAudio, ""},
		{"ogg", ogg, model.FileAudio, ""},
		{"text", []byte("just some words\nand a second line\n"), model.FileText, ""},
		{"utf-8 text", []byte("héllo wörld ✓\n"), model.FileText, ""},
		{"svg is always a file", []byte(`<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`), model.FileOther, ""},
		{"bare svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg"></svg>`), model.FileOther, ""},
		{"binary", []byte{0, 1, 2, 3, 0xff, 0xfe, 0, 0, 9}, model.FileOther, ""},
		{"empty", nil, model.FileOther, ""},
		{"a png name on html content is still text or a file, never an image", []byte("<html><body>hi</body></html>"), "", ""},
	} {
		typ, kind := Sniff(c.head)
		if c.kind != "" && kind != c.kind {
			t.Errorf("%s: kind %q (%s), want %q", c.name, kind, typ, c.kind)
		}
		if c.kind == "" && kind == model.FileImage {
			t.Errorf("%s: sniffed as an image", c.name)
		}
		if c.typ != "" && typ != c.typ {
			t.Errorf("%s: type %q, want %q", c.name, typ, c.typ)
		}
		if typ == "" {
			t.Errorf("%s: no media type", c.name)
		}
	}
	if !IsText([]byte("plain")) || IsText([]byte{'a', 0, 'b'}) {
		t.Error("IsText: text has no NUL bytes")
	}
	lines := strings.Repeat("line\n", 50)
	preview, err := PreviewText(strings.NewReader(lines))
	if err != nil || strings.Count(preview, "line") != 20 {
		t.Errorf("the preview is the first 20 lines: %d lines, %v", strings.Count(preview, "line"), err)
	}
}

func TestThumbnail(t *testing.T) {
	for _, c := range []struct {
		name         string
		src          []byte
		w, h         int
		typ          string
		wantW, wantH int
	}{
		{"a wide jpeg is scaled to 720 on the long edge", jpegBytes(t, 2000, 1000), 2000, 1000, TypeJPEG, 720, 360},
		{"a tall png", pngBytes(t, 500, 1500, false), 500, 1500, TypeJPEG, 240, 720},
		{"a small image is never enlarged", pngBytes(t, 100, 50, false), 100, 50, TypeJPEG, 100, 50},
		{"transparency keeps png", pngBytes(t, 200, 100, true), 200, 100, TypePNG, 200, 100},
		{"a gif's first frame", gifBytes(t, 64, 32), 64, 32, TypeJPEG, 64, 32},
	} {
		th, err := MakeThumbnail(bytes.NewReader(c.src))
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if th.Width != c.wantW || th.Height != c.wantH || th.MediaType != c.typ {
			t.Errorf("%s: got %dx%d %s, want %dx%d %s", c.name, th.Width, th.Height, th.MediaType, c.wantW, c.wantH, c.typ)
		}
		// The result is itself a valid image of the size it says.
		cfg, _, err := image.DecodeConfig(bytes.NewReader(th.Data))
		if err != nil || cfg.Width != th.Width || cfg.Height != th.Height {
			t.Errorf("%s: the thumbnail does not decode to its size: %v", c.name, err)
		}
		w, h, d := Probe(bytes.NewReader(c.src))
		if w != c.w || h != c.h || d != 0 {
			t.Errorf("%s: Probe gave %dx%d %ds", c.name, w, h, d)
		}
	}
	r, err := Thumbnail(bytes.NewReader(jpegBytes(t, 800, 800)))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(r)
	if typ, kind := Sniff(b); kind != model.FileImage || typ != TypeJPEG {
		t.Errorf("Thumbnail's bytes sniff as %s", typ)
	}
	if _, err := MakeThumbnail(strings.NewReader("not an image at all")); !errors.Is(err, ErrNotAnImage) {
		t.Errorf("not an image: %v", err)
	}
	if w, h, _ := Probe(strings.NewReader("not an image")); w != 0 || h != 0 {
		t.Errorf("Probe of something that is no image: %dx%d", w, h)
	}
}

// TestThumbnailRefusesBombs: a few bytes of header can claim an enormous
// image. It must be refused from the header, without decoding.
func TestThumbnailRefusesBombs(t *testing.T) {
	src := pngBytes(t, 8, 8, false)
	// Rewrite the IHDR to claim 60000 x 60000 and fix nothing else: the
	// size check comes before the pixels are read.
	bomb := append([]byte(nil), src...)
	copy(bomb[16:24], []byte{0, 0, 0xEA, 0x60, 0, 0, 0xEA, 0x60})
	if _, err := MakeThumbnail(bytes.NewReader(bomb)); !errors.Is(err, ErrTooManyPixels) && !errors.Is(err, ErrNotAnImage) {
		t.Fatalf("an image claiming 3.6 gigapixels: %v", err)
	}
}

// TestMetadataIsGone: a thumbnail is re-encoded, so nothing of the
// original's metadata can survive in it.
func TestMetadataIsGone(t *testing.T) {
	src := jpegBytes(t, 900, 600)
	// Splice a comment segment holding a "location" into the JPEG, after
	// the start-of-image marker.
	secret := []byte("GPS: -27.4698, 153.0251 camera: SecretCam 3000")
	seg := append([]byte{0xFF, 0xFE, byte((len(secret) + 2) >> 8), byte(len(secret) + 2)}, secret...)
	tagged := append(append(append([]byte(nil), src[:2]...), seg...), src[2:]...)
	th, err := MakeThumbnail(bytes.NewReader(tagged))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(th.Data, []byte("SecretCam")) || bytes.Contains(th.Data, []byte("GPS")) {
		t.Fatal("the thumbnail still holds the original's metadata")
	}
}
