package ref

import (
	"encoding/binary"
	"errors"
	"unicode/utf8"
)

// The record encoding of docs/SEALED.md section 2.3: a fixed binary layout
// with exactly one valid form. A record is its fields in order with nothing
// between them and nothing after the last.

// MaxRecord is the longest record: 64 KB.
const MaxRecord = 64 * 1024

// What a reader refuses.
var (
	// ErrShort: the record ends before its last field.
	ErrShort = errors.New("ref: the record is cut short")
	// ErrTrailing: bytes are left over after the last field.
	ErrTrailing = errors.New("ref: the record has bytes left over")
	// ErrChoice: a u8 choice holds a value outside its list.
	ErrChoice = errors.New("ref: a choice is out of range")
	// ErrUTF8: a text is not valid UTF-8.
	ErrUTF8 = errors.New("ref: a text is not valid UTF-8")
	// ErrOrder: a list that stands for a set is not sorted by its key, or
	// holds a key twice.
	ErrOrder = errors.New("ref: a list is not in order, or repeats a key")
	// ErrTooLong: the record is longer than 64 KB.
	ErrTooLong = errors.New("ref: the record is longer than 64 KB")
	// ErrCount: a list has a number of items the record does not allow.
	ErrCount = errors.New("ref: a list has the wrong number of items")
)

// Writer builds a record.
type Writer struct{ b []byte }

// U8 writes one byte.
func (w *Writer) U8(v byte) { w.b = append(w.b, v) }

// U32 writes 4 bytes, big-endian.
func (w *Writer) U32(v uint32) { w.b = binary.BigEndian.AppendUint32(w.b, v) }

// U64 writes 8 bytes, big-endian.
func (w *Writer) U64(v uint64) { w.b = binary.BigEndian.AppendUint64(w.b, v) }

// Raw writes exactly these bytes: the `bytes(n)` type.
func (w *Writer) Raw(b []byte) { w.b = append(w.b, b...) }

// UUID writes an identifier's 16 bytes.
func (w *Writer) UUID(u UUID) { w.b = append(w.b, u[:]...) }

// Time writes milliseconds since the Unix epoch as a u64.
func (w *Writer) Time(ms uint64) { w.U64(ms) }

// Blob writes a u32 length and then the bytes.
func (w *Writer) Blob(b []byte) {
	w.U32(uint32(len(b)))
	w.b = append(w.b, b...)
}

// Text writes a blob holding UTF-8.
func (w *Writer) Text(s string) { w.Blob([]byte(s)) }

// Bytes returns the record. A record over 64 KB is an error.
func (w *Writer) Bytes() ([]byte, error) {
	if len(w.b) > MaxRecord {
		return nil, ErrTooLong
	}
	return w.b, nil
}

// Reader takes a record apart. The first failure sticks: later reads
// return zero values, and Done reports it.
type Reader struct {
	b   []byte
	err error
}

// NewReader starts reading a record. One longer than 64 KB is refused.
func NewReader(b []byte) *Reader {
	r := &Reader{b: b}
	if len(b) > MaxRecord {
		r.err = ErrTooLong
	}
	return r
}

func (r *Reader) take(n int) []byte {
	if r.err != nil {
		return nil
	}
	if n < 0 || len(r.b) < n {
		r.err = ErrShort
		return nil
	}
	out := r.b[:n]
	r.b = r.b[n:]
	return out
}

// U8 reads one byte.
func (r *Reader) U8() byte {
	b := r.take(1)
	if b == nil {
		return 0
	}
	return b[0]
}

// Choice reads a u8 that must be one of the listed values.
func (r *Reader) Choice(allowed ...byte) byte {
	v := r.U8()
	if r.err != nil {
		return 0
	}
	for _, a := range allowed {
		if v == a {
			return v
		}
	}
	r.err = ErrChoice
	return 0
}

// U32 reads 4 bytes, big-endian.
func (r *Reader) U32() uint32 {
	b := r.take(4)
	if b == nil {
		return 0
	}
	return binary.BigEndian.Uint32(b)
}

// U64 reads 8 bytes, big-endian.
func (r *Reader) U64() uint64 {
	b := r.take(8)
	if b == nil {
		return 0
	}
	return binary.BigEndian.Uint64(b)
}

// Raw reads exactly n bytes, as a copy.
func (r *Reader) Raw(n int) []byte {
	b := r.take(n)
	if b == nil {
		return make([]byte, n)
	}
	return append([]byte(nil), b...)
}

// UUID reads an identifier.
func (r *Reader) UUID() UUID {
	var u UUID
	copy(u[:], r.take(16))
	return u
}

// Blob reads a u32 length and that many bytes.
func (r *Reader) Blob() []byte {
	n := r.U32()
	if r.err != nil {
		return nil
	}
	if uint64(n) > uint64(len(r.b)) {
		r.err = ErrShort
		return nil
	}
	return append([]byte{}, r.take(int(n))...)
}

// Text reads a blob that must hold valid UTF-8. It is never normalised.
func (r *Reader) Text() string {
	b := r.Blob()
	if r.err != nil {
		return ""
	}
	if !utf8.Valid(b) {
		r.err = ErrUTF8
		return ""
	}
	return string(b)
}

// Count reads the u32 count in front of a list. A count that could not fit
// in what is left of the record, given the smallest size of one item, is
// refused at once.
func (r *Reader) Count(minItem int) int {
	n := r.U32()
	if r.err != nil {
		return 0
	}
	if uint64(n)*uint64(minItem) > uint64(len(r.b)) {
		r.err = ErrShort
		return 0
	}
	return int(n)
}

// Fail records a refusal the caller found itself, such as a list out of
// order.
func (r *Reader) Fail(err error) {
	if r.err == nil {
		r.err = err
	}
}

// Err is the first failure so far.
func (r *Reader) Err() error { return r.err }

// Done ends the record: bytes left over are refused.
func (r *Reader) Done() error {
	if r.err != nil {
		return r.err
	}
	if len(r.b) != 0 {
		return ErrTrailing
	}
	return nil
}
