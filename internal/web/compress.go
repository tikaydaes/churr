package web

import (
	"bytes"
	"compress/gzip"
	"path"
	"strconv"
	"strings"
	"sync"
)

// Static files are compressed by the server itself, so that a proxy without
// compression switched on still sends the card sprites and scripts small.
// Only gzip is offered: the standard library has it, and Brotli would need
// a new dependency or a second, precompressed copy of every file in the
// binary. A proxy that compresses leaves a response alone once it carries
// Content-Encoding, so nothing is compressed twice.

// minCompress is the size under which a file is sent as it is: the gzip
// header and the work cost more than the bytes saved.
const minCompress = 1024

// textual are the extensions worth compressing. Pictures in PNG, fonts in
// WOFF2 and sound are compressed already.
var textual = map[string]bool{
	".html": true, ".css": true, ".js": true, ".json": true,
	".webmanifest": true, ".svg": true, ".txt": true, "": true,
}

func compressible(name string, body []byte) bool {
	return textual[path.Ext(name)] && len(body) >= minCompress
}

// gzipped is a file's gzip form, made once, on first use.
type gzipped struct {
	once sync.Once
	body []byte // nil when compressing did not make the file smaller
}

func (g *gzipped) get(plain []byte) []byte {
	g.once.Do(func() {
		var b bytes.Buffer
		zw, _ := gzip.NewWriterLevel(&b, gzip.BestCompression)
		zw.Write(plain)
		zw.Close()
		// Not worth it unless it saves a tenth.
		if b.Len() < len(plain)-len(plain)/10 {
			g.body = b.Bytes()
		}
	})
	return g.body
}

// gzipETag is the tag of a file's gzip form: different bytes, so a
// different tag, and one that still names the same file.
func gzipETag(tag string) string {
	return strings.TrimSuffix(tag, `"`) + `-gzip"`
}

// acceptsGzip reads an Accept-Encoding header: gzip (or *) is acceptable
// unless its quality is zero.
func acceptsGzip(header string) bool {
	star := false
	for _, part := range strings.Split(header, ",") {
		coding, params, _ := strings.Cut(part, ";")
		coding = strings.ToLower(strings.TrimSpace(coding))
		if coding != "gzip" && coding != "x-gzip" && coding != "*" {
			continue
		}
		ok := true
		for _, p := range strings.Split(params, ";") {
			k, v, found := strings.Cut(strings.TrimSpace(p), "=")
			if found && strings.EqualFold(strings.TrimSpace(k), "q") {
				if q, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil && q <= 0 {
					ok = false
				}
			}
		}
		if coding == "*" {
			star = ok
			continue
		}
		return ok
	}
	return star
}
