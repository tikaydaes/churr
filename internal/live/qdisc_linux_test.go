//go:build linux

package live

import (
	"encoding/binary"
	"testing"
)

func TestQdiscStats(t *testing.T) {
	msg := make([]byte, 20)
	kind := append([]byte{0, 0, 1, 0}, "fq_codel\x00"...) // TCA_KIND, padded below
	binary.NativeEndian.PutUint16(kind, uint16(len(kind)))
	for len(kind)%4 != 0 {
		kind = append(kind, 0)
	}
	stats := make([]byte, 4+36)
	binary.NativeEndian.PutUint16(stats[0:], uint16(len(stats)))
	binary.NativeEndian.PutUint16(stats[2:], 3)
	binary.NativeEndian.PutUint32(stats[4+12:], 10948274)
	binary.NativeEndian.PutUint32(stats[4+28:], 69081)
	msg = append(append(msg, kind...), stats...)
	if d, w := qdiscStats(msg); d != 10948274 || w != 69081 {
		t.Fatalf("got %d dropped, %d waiting", d, w)
	}
	if d, w := qdiscStats(msg[:30]); d != 0 || w != 0 {
		t.Fatalf("a cut message gave %d, %d", d, w)
	}
	// On a real machine the kernel answers.
	d, w, ok := sendQueues()
	t.Logf("this machine's transmit queues: %d dropped, %d waiting, read %v", d, w, ok)
}
