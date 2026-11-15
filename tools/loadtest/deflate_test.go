package main

import (
	"bufio"
	"bytes"
	"compress/flate"
	"net"
	"testing"
)

// packed is a message as a server that compresses sends it: deflated, the
// four bytes that end the last block left off, and the frame marked.
func packed(t *testing.T, plain []byte) []byte {
	t.Helper()
	var b bytes.Buffer
	w, _ := flate.NewWriter(&b, flate.BestSpeed)
	w.Write(plain)
	w.Flush()
	f := serverFrame(true, 1, b.Bytes()[:b.Len()-4])
	f[0] |= 0x40
	return f
}

// TestWSReadCompressed: compressed messages, small and large, are unpacked,
// plain ones between them are read as they are, and the bytes counted are
// those that crossed the socket.
func TestWSReadCompressed(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	small := bytes.Repeat([]byte(`{"type":"message.created","text":"hello hello"}`), 8)
	large := bytes.Repeat([]byte("a large message that repeats itself. "), 20000)
	plain := []byte(`{"type":"pong"}`)
	frames := [][]byte{packed(t, small), serverFrame(true, 1, plain), packed(t, large), packed(t, small)}
	go func() {
		for _, f := range frames {
			server.Write(f)
		}
	}()
	c := &wsConn{conn: client, r: bufio.NewReaderSize(client, readBuffer), deflate: true}
	for i, want := range [][]byte{small, plain, large, small} {
		got, err := c.read()
		if err != nil {
			t.Fatalf("message %d: %v", i, err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("message %d: read %d bytes %.20q, want %d bytes %.20q", i, len(got), got, len(want), want)
		}
		if c.wire != len(frames[i]) {
			t.Fatalf("message %d: counted %d bytes on the wire, want %d", i, c.wire, len(frames[i]))
		}
	}
}
