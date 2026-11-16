package web

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func gunzip(t *testing.T, b []byte) []byte {
	t.Helper()
	zr, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	out, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// A text file goes gzipped to a browser that takes gzip, as it is to one
// that does not, and both answers say they vary by Accept-Encoding.
func TestCompressNegotiated(t *testing.T) {
	h := testHandler()
	const p = "/shared/chance/french.svg"
	plain := files()["shared/chance/french.svg"].body

	for _, ae := range []string{"gzip", "br, gzip;q=0.8", "deflate, *", "GZIP"} {
		w := get(t, h, p, "Accept-Encoding", ae)
		if w.Header().Get("Content-Encoding") != "gzip" {
			t.Fatalf("%q: Content-Encoding %q", ae, w.Header().Get("Content-Encoding"))
		}
		if w.Header().Get("Vary") != "Accept-Encoding" || w.Header().Get("Content-Type") != "image/svg+xml" {
			t.Errorf("%q: Vary %q, Content-Type %q", ae, w.Header().Get("Vary"), w.Header().Get("Content-Type"))
		}
		if w.Body.Len() >= len(plain)/2 {
			t.Errorf("%q: %d bytes gzipped of %d", ae, w.Body.Len(), len(plain))
		}
		if !bytes.Equal(gunzip(t, w.Body.Bytes()), plain) {
			t.Errorf("%q: the gzipped body is not the file", ae)
		}
	}
	for _, ae := range []string{"", "br", "identity", "gzip;q=0", "*;q=0", "gzip;q=0, *"} {
		w := get(t, h, p, "Accept-Encoding", ae)
		if w.Header().Get("Content-Encoding") != "" || !bytes.Equal(w.Body.Bytes(), plain) {
			t.Errorf("%q: Content-Encoding %q with %d bytes", ae, w.Header().Get("Content-Encoding"), w.Body.Len())
		}
		if w.Header().Get("Vary") != "Accept-Encoding" {
			t.Errorf("%q: Vary %q", ae, w.Header().Get("Vary"))
		}
	}
}

// Each form has its own ETag, and If-None-Match is answered per form.
func TestCompressETag(t *testing.T) {
	h := testHandler()
	const p = "/shared/dom.js"
	plain := get(t, h, p).Header().Get("ETag")
	gz := get(t, h, p, "Accept-Encoding", "gzip").Header().Get("ETag")
	if plain == "" || gz == "" || plain == gz {
		t.Fatalf("ETags %q and %q", plain, gz)
	}
	if w := get(t, h, p, "Accept-Encoding", "gzip", "If-None-Match", gz); w.Code != http.StatusNotModified {
		t.Errorf("gzip tag, gzip accepted: %d", w.Code)
	}
	if w := get(t, h, p, "If-None-Match", plain); w.Code != http.StatusNotModified {
		t.Errorf("plain tag, nothing accepted: %d", w.Code)
	}
	// A tag for the other form is not the one this answer would carry.
	if w := get(t, h, p, "If-None-Match", gz); w.Code != http.StatusOK || w.Header().Get("Content-Encoding") != "" {
		t.Errorf("gzip tag, gzip not accepted: %d %q", w.Code, w.Header().Get("Content-Encoding"))
	}
	if w := get(t, h, p, "Accept-Encoding", "gzip", "If-None-Match", plain); w.Code != http.StatusOK || w.Header().Get("Content-Encoding") != "gzip" {
		t.Errorf("plain tag, gzip accepted: %d %q", w.Code, w.Header().Get("Content-Encoding"))
	}
}

// HEAD gives the headers and length of the gzipped form, and no body.
func TestCompressHead(t *testing.T) {
	h := testHandler()
	full := get(t, h, "/shared/dom.js", "Accept-Encoding", "gzip")
	r := httptest.NewRequest(http.MethodHead, "/shared/dom.js", nil)
	r.Header.Set("Accept-Encoding", "gzip")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK || w.Body.Len() != 0 || w.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("HEAD: %d, %d bytes, %q", w.Code, w.Body.Len(), w.Header().Get("Content-Encoding"))
	}
	if w.Header().Get("ETag") != full.Header().Get("ETag") {
		t.Errorf("HEAD ETag %q, GET %q", w.Header().Get("ETag"), full.Header().Get("ETag"))
	}
}

// A Range request gets a range of the file as it is, never of its gzip form.
func TestCompressRange(t *testing.T) {
	h := testHandler()
	plain := files()["shared/dom.js"].body
	w := get(t, h, "/shared/dom.js", "Accept-Encoding", "gzip", "Range", "bytes=0-9")
	if w.Code != http.StatusPartialContent || w.Header().Get("Content-Encoding") != "" {
		t.Fatalf("Range: %d %q", w.Code, w.Header().Get("Content-Encoding"))
	}
	if !bytes.Equal(w.Body.Bytes(), plain[:10]) {
		t.Errorf("Range body %q", w.Body.String())
	}
	if w.Header().Get("ETag") != files()["shared/dom.js"].etag {
		t.Errorf("Range ETag %q", w.Header().Get("ETag"))
	}
}

// Small files and formats that are compressed already go as they are.
func TestCompressLeftAlone(t *testing.T) {
	h := testHandler()
	var small string
	for name, f := range files() {
		if strings.HasSuffix(name, ".json") && len(f.body) < minCompress && !strings.HasSuffix(name, ".html") {
			small = name
			break
		}
	}
	if small == "" {
		t.Fatal("no small text file to try")
	}
	for _, p := range []string{"/" + small, "/icons/icon-512.png"} {
		w := get(t, h, p, "Accept-Encoding", "gzip")
		if w.Code != http.StatusOK || w.Header().Get("Content-Encoding") != "" || w.Header().Get("Vary") != "" {
			t.Errorf("%s: %d, Content-Encoding %q, Vary %q", p, w.Code, w.Header().Get("Content-Encoding"), w.Header().Get("Vary"))
		}
	}
}
