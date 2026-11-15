package wire

import (
	"compress/flate"
	"context"
	"io"
	"math"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"unsafe"

	"github.com/coder/websocket"
)

// This file is the link's compression (docs/LINK-PROTOCOL.md section 2,
// rulings R65 and R70): every link connection negotiates permessage-deflate
// (RFC 7692), a peer that does not offer it is refused, and every frame is
// sent deflated, except a frame that deflate would not make smaller, which
// goes as it is. Most single envelopes are like that: a signature and a
// ciphertext do not compress.
//
// Each message is compressed by itself: neither side keeps a window between
// messages, so an idle link holds no compressor.

// compressionMode is what both sides ask for at the upgrade.
const compressionMode = websocket.CompressionNoContextTakeover

// offersDeflate reports whether the headers of an upgrade request, or of its
// answer, name the permessage-deflate extension.
func offersDeflate(h http.Header) bool {
	for _, line := range h.Values("Sec-WebSocket-Extensions") {
		for _, ext := range strings.Split(line, ",") {
			name, _, _ := strings.Cut(ext, ";")
			if strings.TrimSpace(name) == "permessage-deflate" {
				return true
			}
		}
	}
	return false
}

// deflaters are compressors of the kind the WebSocket library uses, kept
// for trying a frame.
var deflaters = sync.Pool{New: func() any {
	w, _ := flate.NewWriter(io.Discard, flate.BestSpeed)
	return w
}}

type byteCount struct{ n int }

func (c *byteCount) Write(p []byte) (int, error) { c.n += len(p); return len(p), nil }

// DeflatedSize is how many bytes a frame takes once deflated as the link
// sends it: compressed by itself, with the four bytes every message ends in
// left off (RFC 7692 section 7.2.1).
func DeflatedSize(b []byte) int {
	w := deflaters.Get().(*flate.Writer)
	var c byteCount
	w.Reset(&c)
	w.Write(b)
	w.Flush()
	deflaters.Put(w)
	return c.n - 4
}

// thresholdAt is where the WebSocket library keeps, in a connection, the
// size from which it compresses a message. The library decides by size
// alone and offers no way to say "this message, as it is"; the rule here is
// by result, so before each write the link sets that size to "always" or to
// "never". If a later version of the library keeps it elsewhere, canChoose
// is false and every frame is deflated, which is still the protocol: only
// the few bytes saved on frames that do not compress are lost. A test fails
// then, so it is noticed.
var thresholdAt, canChoose = func() (uintptr, bool) {
	f, ok := reflect.TypeOf((*websocket.Conn)(nil)).Elem().FieldByName("flateThreshold")
	if !ok || f.Type.Kind() != reflect.Int {
		return 0, false
	}
	return f.Offset, true
}()

// writeMessage writes one frame as one binary message: deflated if that
// makes it smaller, as it is if not. The caller holds whatever keeps two
// writes on one connection apart.
func writeMessage(ctx context.Context, ws *websocket.Conn, b []byte) error {
	if canChoose {
		from := math.MaxInt
		if DeflatedSize(b) < len(b) {
			from = 1
		}
		*(*int)(unsafe.Add(unsafe.Pointer(ws), thresholdAt)) = from
	}
	return ws.Write(ctx, websocket.MessageBinary, b)
}
