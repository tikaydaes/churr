package blob

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/model"
)

// The two worked examples Amazon publishes for Signature Version 4.
func TestSignV4(t *testing.T) {
	key := signingKey("wJalrXUtnFEMI/K7MDENG+bPxRfiCYEXAMPLEKEY", "20150830", "us-east-1", "iam")
	if got := hex.EncodeToString(key); got != "c4afb1cc5771d871763a393e44b703571b55cc28424d1a5e86da6ed3c154a4b9" {
		t.Errorf("signing key: %s", got)
	}
	auth := signV4("GET", "/", "Action=ListUsers&Version=2010-05-08", map[string]string{
		"content-type": "application/x-www-form-urlencoded; charset=utf-8",
		"host":         "iam.amazonaws.com",
		"x-amz-date":   "20150830T123600Z",
	}, emptyHash, "AKIDEXAMPLE", "wJalrXUtnFEMI/K7MDENG+bPxRfiCYEXAMPLEKEY", "us-east-1", "iam")
	want := "AWS4-HMAC-SHA256 Credential=AKIDEXAMPLE/20150830/us-east-1/iam/aws4_request, " +
		"SignedHeaders=content-type;host;x-amz-date, Signature=5d672d79c15b13162d9279b0855cfba6789a8edb4c82c400e06b5924a6f2b5d7"
	if auth != want {
		t.Errorf("IAM example:\n got %s\nwant %s", auth, want)
	}
	// The S3 example: a ranged GET of test.txt.
	auth = signV4("GET", "/test.txt", "", map[string]string{
		"host":                 "examplebucket.s3.amazonaws.com",
		"range":                "bytes=0-9",
		"x-amz-content-sha256": emptyHash,
		"x-amz-date":           "20130524T000000Z",
	}, emptyHash, "AKIAIOSFODNN7EXAMPLE", "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY", "us-east-1", "s3")
	if !strings.HasSuffix(auth, "Signature=f0e8bdb87c964420e857bd35b5d6ed310bd44f0170aba48dd91039c6036bdb41") {
		t.Errorf("S3 example: %s", auth)
	}
	if got := uriEncode("a b/ü~.txt", false); got != "a%20b/%C3%BC~.txt" {
		t.Errorf("uriEncode: %s", got)
	}
}

// fakeS3 is enough of an object store to exercise the client: path-style
// put, get, delete and list, refusing any request that is not signed.
type fakeS3 struct {
	mu      sync.Mutex
	objects map[string][]byte
	paths   []string
}

func (f *fakeS3) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "AWS4-HMAC-SHA256 Credential=key/") || !strings.Contains(auth, "SignedHeaders=host;x-amz-content-sha256;x-amz-date") ||
		r.Header.Get("X-Amz-Content-Sha256") == "" {
		http.Error(w, "<Error><Code>AccessDenied</Code><Message>not signed</Message></Error>", http.StatusForbidden)
		return
	}
	name, ok := strings.CutPrefix(r.URL.Path, "/bucket/")
	if !ok {
		http.Error(w, "<Error><Code>NoSuchBucket</Code></Error>", http.StatusNotFound)
		return
	}
	f.paths = append(f.paths, r.Method+" "+r.URL.Path)
	switch {
	case r.Method == "GET" && name == "":
		keys := make([]string, 0, len(f.objects))
		for k := range f.objects {
			if strings.HasPrefix(k, r.URL.Query().Get("prefix")) {
				keys = append(keys, k)
			}
		}
		sort.Strings(keys)
		// Two to a page, to exercise continuation.
		from := 0
		fmt.Sscanf(r.URL.Query().Get("continuation-token"), "%d", &from)
		fmt.Fprint(w, "<ListBucketResult>")
		for i := from; i < len(keys) && i < from+2; i++ {
			fmt.Fprintf(w, "<Contents><Key>%s</Key><LastModified>2026-11-01T10:00:00.000Z</LastModified></Contents>", keys[i])
		}
		if from+2 < len(keys) {
			fmt.Fprintf(w, "<IsTruncated>true</IsTruncated><NextContinuationToken>%d</NextContinuationToken>", from+2)
		}
		fmt.Fprint(w, "</ListBucketResult>")
	case r.Method == "PUT":
		if r.ContentLength < 0 {
			http.Error(w, "<Error><Code>MissingContentLength</Code></Error>", http.StatusLengthRequired)
			return
		}
		b, _ := io.ReadAll(r.Body)
		f.objects[name] = b
	case r.Method == "GET":
		b, ok := f.objects[name]
		if !ok {
			http.Error(w, "<Error><Code>NoSuchKey</Code></Error>", http.StatusNotFound)
			return
		}
		w.Write(b)
	case r.Method == "DELETE":
		delete(f.objects, name)
		w.WriteHeader(http.StatusNoContent)
	}
}

func TestS3(t *testing.T) {
	ctx := context.Background()
	fake := &fakeS3{objects: map[string][]byte{}}
	srv := httptest.NewServer(fake)
	defer srv.Close()
	cfg := S3Config{Endpoint: srv.URL, Bucket: "bucket", AccessKey: "key", SecretKey: "secret", PathStyle: true, Prefix: "churr/", TmpDir: t.TempDir()}
	b, err := S3(cfg)
	if err != nil {
		t.Fatal(err)
	}
	key, size, sum, err := b.Put(ctx, strings.NewReader("hello"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(key, "s3:churr/") || size != 5 || hex.EncodeToString(sum) != "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824" {
		t.Errorf("put: %q %d %x", key, size, sum)
	}
	r, err := b.Get(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(r)
	r.Close()
	if string(got) != "hello" {
		t.Errorf("get: %q", got)
	}
	// Content over the limit stores nothing.
	if _, _, _, err := b.Put(ctx, LimitReader(strings.NewReader("too long"), 3)); !errors.Is(err, model.ErrTooLarge) {
		t.Errorf("over the limit: %v", err)
	}
	if len(fake.objects) != 1 {
		t.Errorf("a refused upload left an object behind: %d objects", len(fake.objects))
	}
	empty, size, _, err := b.Put(ctx, strings.NewReader(""))
	if err != nil || size != 0 {
		t.Errorf("an empty blob: %v", err)
	}
	third, _, _, _ := b.Put(ctx, strings.NewReader("three"))
	var listed []string
	if err := b.(Lister).List(ctx, func(k string, at time.Time) error {
		if at.IsZero() {
			t.Errorf("no time for %s", k)
		}
		listed = append(listed, k)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(listed) != 3 {
		t.Errorf("listed across pages: %v", listed)
	}
	for _, k := range []string{key, empty, third} {
		if err := b.Delete(ctx, k); err != nil {
			t.Fatal(err)
		}
	}
	if err := b.Delete(ctx, key); err != nil {
		t.Errorf("deleting twice: %v", err)
	}
	if _, err := b.Get(ctx, key); !errors.Is(err, model.ErrNotFound) {
		t.Errorf("a missing object: %v", err)
	}
	if _, err := b.Get(ctx, "disk:aa/bb/cc"); !errors.Is(err, ErrBadKey) {
		t.Errorf("a disk key: %v", err)
	}
	if err := CheckS3(ctx, cfg); err != nil {
		t.Errorf("the console's test: %v", err)
	}
	if len(fake.objects) != 0 {
		t.Errorf("the test left an object behind")
	}
	bad := cfg
	bad.AccessKey = "wrong"
	if err := CheckS3(ctx, bad); err == nil || !strings.Contains(err.Error(), "AccessDenied") {
		t.Errorf("a wrong key should say what the store said: %v", err)
	}
	srv.Close()
	if _, err := b.Get(ctx, key); !errors.Is(err, model.ErrUnavailable) {
		t.Errorf("a store that cannot be reached: %v", err)
	}
	if _, err := S3(S3Config{Endpoint: "x"}); err == nil {
		t.Errorf("an incomplete configuration was accepted")
	}
}

func TestListAndMixed(t *testing.T) {
	ctx := context.Background()
	d, err := Disk(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a, _, _, _ := d.Put(ctx, strings.NewReader("a"))
	b, _, _, _ := d.Put(ctx, strings.NewReader("b"))
	m := Mixed(d, nil, OnDisk)
	var got []string
	if err := m.(Lister).List(ctx, func(k string, at time.Time) error {
		if time.Since(at) > time.Minute {
			t.Errorf("%s has no sensible time", k)
		}
		got = append(got, k)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	sort.Strings(got)
	want := []string{a, b}
	sort.Strings(want)
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("listed %v, want %v", got, want)
	}
	stop := errors.New("stop")
	if err := d.(Lister).List(ctx, func(string, time.Time) error { return stop }); !errors.Is(err, stop) {
		t.Errorf("List did not stop: %v", err)
	}
}

func box(kind string, body ...[]byte) []byte {
	b := bytes.Join(body, nil)
	out := make([]byte, 8, 8+len(b))
	binary.BigEndian.PutUint32(out, uint32(8+len(b)))
	copy(out[4:], kind)
	return append(out, b...)
}

func u32(v uint32) []byte { return binary.BigEndian.AppendUint32(nil, v) }

// mp4 builds a small MP4: ftyp, a large "mdat" first (as phones write
// them), then moov with a 90.5 second movie and one track.
func mp4(w, h uint32, turned bool) []byte {
	mvhd := box("mvhd", u32(0), u32(0), u32(0), u32(1000), u32(90500), make([]byte, 80))
	matrix := bytes.Join([][]byte{u32(0x10000), u32(0), u32(0), u32(0), u32(0x10000), u32(0), u32(0), u32(0), u32(0x40000000)}, nil)
	if turned {
		matrix = bytes.Join([][]byte{u32(0), u32(0x10000), u32(0), u32(0xFFFF0000), u32(0), u32(0), u32(0), u32(0), u32(0x40000000)}, nil)
	}
	tkhd := func(w, h uint32) []byte {
		return box("tkhd", u32(0), u32(0), u32(0), u32(1), u32(0), u32(90500), make([]byte, 16), matrix, u32(w<<16), u32(h<<16))
	}
	audio := box("trak", tkhd(0, 0))
	video := box("trak", tkhd(w, h), box("mdia", make([]byte, 20)))
	return bytes.Join([][]byte{box("ftyp", []byte("isom"), u32(0), []byte("isomiso2mp41")), box("mdat", make([]byte, 5000)),
		box("moov", mvhd, audio, video)}, nil)
}

func ebml(id []byte, body ...[]byte) []byte {
	b := bytes.Join(body, nil)
	// An eight-byte size is always valid.
	size := binary.BigEndian.AppendUint64(nil, uint64(len(b))|1<<56)
	return append(append(append([]byte{}, id...), size...), b...)
}

func TestProbeContainers(t *testing.T) {
	check := func(what string, data []byte, wantW, wantH, wantD int) {
		t.Helper()
		w, h, d := Probe(bytes.NewReader(data))
		if w != wantW || h != wantH || d != wantD {
			t.Errorf("%s: got %dx%d, %ds; want %dx%d, %ds", what, w, h, d, wantW, wantH, wantD)
		}
	}
	check("mp4", mp4(1280, 720, false), 1280, 720, 91)
	check("mp4 turned on its side", mp4(1280, 720, true), 720, 1280, 91)
	m4a := bytes.Join([][]byte{box("ftyp", []byte("M4A "), u32(0), []byte("M4A mp42isom")),
		box("moov", box("mvhd", u32(0), u32(0), u32(0), u32(44100), u32(44100*12), make([]byte, 80)), box("trak", box("tkhd", make([]byte, 84))))}, nil)
	check("m4a", m4a, 0, 0, 12)
	// A file cut off in the middle of a box, and boxes that lie about
	// their size, are not understood; that is not an error.
	whole := mp4(640, 480, false)
	check("mp4 cut short", whole[:len(whole)-40], 0, 0, 0)
	lying := append([]byte{}, whole...)
	binary.BigEndian.PutUint32(lying[len(lying)-250:], 0x7FFFFFFF)
	Probe(bytes.NewReader(lying)) // must not panic or run away

	dur := binary.BigEndian.AppendUint64(nil, math.Float64bits(61500)) // milliseconds at the default scale
	info := ebml([]byte{0x15, 0x49, 0xA9, 0x66}, ebml([]byte{0x2A, 0xD7, 0xB1}, []byte{0x0F, 0x42, 0x40}), ebml([]byte{0x44, 0x89}, dur))
	video := ebml([]byte{0xE0}, ebml([]byte{0xB0}, []byte{0x07, 0x80}), ebml([]byte{0xBA}, []byte{0x04, 0x38}))
	tracks := ebml([]byte{0x16, 0x54, 0xAE, 0x6B}, ebml([]byte{0xAE}, ebml([]byte{0x86}, []byte("A_OPUS"))), ebml([]byte{0xAE}, video))
	header := ebml([]byte{0x1A, 0x45, 0xDF, 0xA3}, ebml([]byte{0x42, 0x82}, []byte("webm")))
	cluster := ebml([]byte{0x1F, 0x43, 0xB6, 0x75}, make([]byte, 100))
	check("webm", bytes.Join([][]byte{header, ebml([]byte{0x18, 0x53, 0x80, 0x67}, info, tracks, cluster)}, nil), 1920, 1080, 62)
	live := bytes.Join([][]byte{header, {0x18, 0x53, 0x80, 0x67, 0x01, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}, tracks, cluster}, nil)
	check("webm written live: size known, length not", live, 1920, 1080, 0)

	// MPEG 1 Layer III, 128 kbit/s, 44.1 kHz, stereo: 417-byte frames.
	frame := append([]byte{0xFF, 0xFB, 0x90, 0x00}, make([]byte, 413)...)
	cbr := bytes.Repeat(frame, 384) // 384 frames of 1152 samples: 10.03 s
	check("mp3 at one bit rate", cbr, 0, 0, 10)
	tag := append([]byte("ID3\x04\x00\x00\x00\x00\x02\x00"), make([]byte, 256)...)
	check("mp3 behind an ID3 tag", append(tag, cbr...), 0, 0, 10)
	xing := append([]byte{}, frame...)
	copy(xing[36:], "Xing")
	binary.BigEndian.PutUint32(xing[40:], 1)
	binary.BigEndian.PutUint32(xing[44:], 11485) // 300 s of frames
	check("mp3 with a frame count", append(xing, bytes.Repeat(frame, 3)...), 0, 0, 300)

	oggPage := func(granule uint64, packet []byte) []byte {
		p := append([]byte("OggS\x00\x00"), binary.LittleEndian.AppendUint64(nil, granule)...)
		p = append(p, make([]byte, 12)...)
		p = append(p, 1, byte(len(packet)))
		return append(p, packet...)
	}
	opusHead := append([]byte("OpusHead\x01\x02"), 0x38, 0x01, 0x80, 0xBB, 0, 0, 0, 0, 0)
	opus := bytes.Join([][]byte{oggPage(0, opusHead), oggPage(0, []byte("OpusTags....")), oggPage(48000*30+312, make([]byte, 200))}, nil)
	check("ogg opus", opus, 0, 0, 30)
	vorbisHead := append([]byte("\x01vorbis\x00\x00\x00\x00\x02"), binary.LittleEndian.AppendUint32(nil, 44100)...)
	vorbisHead = append(vorbisHead, make([]byte, 13)...)
	check("ogg vorbis", bytes.Join([][]byte{oggPage(0, vorbisHead), oggPage(44100*7, make([]byte, 100))}, nil), 0, 0, 7)

	wav := func(dataSize uint32, n int) []byte {
		b := []byte("RIFF\x00\x00\x00\x00WAVEfmt \x10\x00\x00\x00\x01\x00\x02\x00")
		b = binary.LittleEndian.AppendUint32(b, 44100)
		b = binary.LittleEndian.AppendUint32(b, 176400)
		b = append(b, 4, 0, 16, 0)
		b = append(b, "LIST\x04\x00\x00\x00abcd"...)
		b = append(b, "data"...)
		b = binary.LittleEndian.AppendUint32(b, dataSize)
		return append(b, make([]byte, n)...)
	}
	check("wav", wav(176400*3, 176400*3), 0, 0, 3)
	check("wav written as a stream", wav(0xFFFFFFFF, 176400*2), 0, 0, 2)
	check("not a container at all", []byte("just some text"), 0, 0, 0)
}
