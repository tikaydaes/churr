package ref

import (
	"crypto/sha256"
	"encoding/binary"
)

// Stream is a fixed stream of bytes made from a seed, which stands in for
// the random generator when the vectors are written. Block i is
// SHA-256("churr test stream v1" || 0x00 || seed || i as a u64); the blocks
// are joined end to end.
//
// Nothing depends on other implementations reproducing it: every value
// drawn from a Stream is written out in the vector that used it.
type Stream struct {
	seed  []byte
	block uint64
	rest  []byte
}

// NewStream makes the stream of a seed.
func NewStream(seed string) *Stream { return &Stream{seed: []byte(seed)} }

// Read fills p with the next bytes of the stream. It never fails.
func (s *Stream) Read(p []byte) (int, error) {
	for i := range p {
		if len(s.rest) == 0 {
			sum := sha256.Sum256(labelled("churr test stream v1", s.seed, binary.BigEndian.AppendUint64(nil, s.block)))
			s.rest = sum[:]
			s.block++
		}
		p[i] = s.rest[0]
		s.rest = s.rest[1:]
	}
	return len(p), nil
}

// Take returns the next n bytes.
func (s *Stream) Take(n int) []byte {
	b := make([]byte, n)
	s.Read(b)
	return b
}

// Fixed returns the first n bytes of the stream of a seed: a fixed value
// that looks random, named by what it is for.
func Fixed(seed string, n int) []byte { return NewStream(seed).Take(n) }
