// Package fair turns a draw into outcomes (docs/FAIR.md): the frozen
// derivation `derive: 1`, which makes whole numbers, picks, shuffles, deals,
// yes or no and dice out of a draw's 32-byte output, and the chain that says
// what a conversation's next draw is proved over. The same rule is written in
// JavaScript (internal/web/static/shared/fair/derive.js) and Dart
// (app/lib/core/fair/derive.dart), and all three are held to one vectors
// file, testdata/fair/derive.json. Nothing here uses floating point or any
// source of randomness: the outcome is a function of the output alone, so
// anyone holding the output re-derives it.
package fair

import (
	"crypto/sha256"
	"encoding/binary"
)

// Derive is the version of the derivation this package implements. A new rule
// is a new number; a message keeps the number it was drawn under.
const Derive = 1

// Block is block k of object i's stream:
// SHA-256(beta ‖ u32be(i) ‖ u32be(k)).
func Block(beta []byte, i, k uint32) [32]byte {
	b := make([]byte, 0, len(beta)+8)
	b = append(b, beta...)
	b = binary.BigEndian.AppendUint32(b, i)
	b = binary.BigEndian.AppendUint32(b, k)
	return sha256.Sum256(b)
}

// Stream is one chance object's share of a draw: the blocks of stream(i, k)
// for k = 0, 1, 2, ..., read as big-endian 32-bit words in order, eight to a
// block. Everything an object derives takes its words from its stream in the
// order the derivation asks for them, and only so.
type Stream struct {
	beta  []byte
	i     uint32
	k     uint32
	block [32]byte
	at    int // the next word's byte offset in block; 32 when a new block is due
	used  int // words taken
}

// NewStream is object i's stream of a draw whose output is beta.
func NewStream(beta []byte, i uint32) *Stream {
	return &Stream{beta: append([]byte(nil), beta...), i: i, at: 32}
}

// Object is the object index the stream is for.
func (s *Stream) Object() uint32 { return s.i }

// Used is how many words have been taken.
func (s *Stream) Used() int { return s.used }

// Word is the stream's next 32-bit word.
func (s *Stream) Word() uint32 {
	if s.at == 32 {
		s.block = Block(s.beta, s.i, s.k)
		s.k++
		s.at = 0
	}
	w := binary.BigEndian.Uint32(s.block[s.at:])
	s.at += 4
	s.used++
	return w
}

// span is 2^32, the number of values a word takes.
const span = uint64(1) << 32

// Below is a whole number from 0 to n-1, every one equally likely, for n from
// 1 to 2^32, by rejection: a word w is taken while w ≥ 2^32 - (2^32 mod n),
// and the first one below that gives w mod n. For n = 1 no word is taken.
// It panics for an n out of range: callers check first (Check).
func (s *Stream) Below(n uint64) uint64 {
	if n < 1 || n > span {
		panic("fair: Below out of range")
	}
	if n == 1 {
		return 0
	}
	limit := span - span%n
	for {
		w := uint64(s.Word())
		if w < limit {
			return w % n
		}
	}
}

// Intn is a whole number from 0 to n-1, for n from 1 to 2^31-1: Below for
// callers that count in int.
func (s *Stream) Intn(n int) int {
	return int(s.Below(uint64(n)))
}

// Objects hands out the streams of one draw's chance objects: object 0, then
// 1, and so on, in the order they are asked for, which is their order of
// appearance in the message.
type Objects struct {
	beta []byte
	next uint32
}

// NewObjects is the objects of a draw whose output is beta.
func NewObjects(beta []byte) *Objects { return &Objects{beta: append([]byte(nil), beta...)} }

// Stream is the next object's stream.
func (o *Objects) Stream() (*Stream, error) {
	s := NewStream(o.beta, o.next)
	o.next++
	return s, nil
}

// Taken is how many objects have taken a stream.
func (o *Objects) Taken() int { return int(o.next) }
