package wire

import (
	"bytes"
	"compress/flate"
	"context"
	"net/http"
	"strings"
	"sync"

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
//
// The WebSocket library compresses by size alone, so the link compresses and
// frames its own messages (socket.go); the library inflates what arrives.

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

// deflaters are compressors at the level the WebSocket library uses.
var deflaters = sync.Pool{New: func() any {
	w, _ := flate.NewWriter(nil, flate.BestSpeed)
	return w
}}

// deflate is a frame compressed as one message of permessage-deflate: by
// itself, with the four bytes every flushed deflate stream ends in left off,
// which the reader puts back (RFC 7692 section 7.2.1).
func deflate(b []byte) []byte {
	var out bytes.Buffer
	out.Grow(len(b)/2 + 64)
	w := deflaters.Get().(*flate.Writer)
	w.Reset(&out)
	w.Write(b)
	w.Flush()
	deflaters.Put(w)
	z := out.Bytes()
	return z[:len(z)-4]
}

// DeflatedSize is how many bytes a frame takes once deflated as the link
// sends it.
func DeflatedSize(b []byte) int { return len(deflate(b)) }

// writeMessage writes one frame as one binary message: deflated if that
// makes it smaller, as it is if not. The caller holds whatever keeps two
// writes of its own on one connection apart.
func writeMessage(ctx context.Context, ws *websocket.Conn, out *socket, b []byte) error {
	if z := deflate(b); len(z) < len(b) {
		return out.send(ctx, ws, z, true)
	}
	return out.send(ctx, ws, b, false)
}
