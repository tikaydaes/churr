package wire

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"

	"google.golang.org/protobuf/encoding/protowire"
)

// The numbers of a Frame's own fields. The bodies' numbers are in
// schema_gen.go.
const (
	frameIDNum = 1
	frameReNum = 2
)

// ErrUnknownFrame is returned for a frame whose body this schema does not
// know: one from a newer peer. It is answered with bad_frame and the
// connection stays open (section 2).
var ErrUnknownFrame = errors.New("wire: a frame this side does not know")

// EncodeFrame writes a frame as the bytes of one binary WebSocket message.
// It refuses a frame that has no body in the schema, one that sets a field
// its kind does not carry, and one whose identifiers are not UUIDs.
func EncodeFrame(f Frame) ([]byte, error) {
	mem := pbFrameBy[f.Frame]
	if mem == nil {
		return nil, fmt.Errorf("wire: there is no frame called %q", f.Frame)
	}
	body, err := appendStruct(make([]byte, 0, 256), mem.msg, reflect.ValueOf(&f).Elem())
	if err != nil {
		return nil, err
	}
	b := make([]byte, 0, len(body)+24)
	if f.ID != 0 {
		b = protowire.AppendVarint(protowire.AppendTag(b, frameIDNum, protowire.VarintType), uint64(f.ID))
	}
	if f.Re != 0 {
		b = protowire.AppendVarint(protowire.AppendTag(b, frameReNum, protowire.VarintType), uint64(f.Re))
	}
	return protowire.AppendBytes(protowire.AppendTag(b, mem.num, protowire.BytesType), body), nil
}

// DecodeFrame reads the bytes of one frame. Nothing it returns points into
// b.
func DecodeFrame(b []byte) (Frame, error) {
	var f Frame
	for len(b) > 0 {
		num, t, n := protowire.ConsumeTag(b)
		if n < 0 {
			return Frame{}, malformed("Frame")
		}
		b = b[n:]
		switch {
		case num == frameIDNum || num == frameReNum:
			x, n := protowire.ConsumeVarint(b)
			if t != protowire.VarintType || n < 0 {
				return Frame{}, malformed("Frame")
			}
			if num == frameIDNum {
				f.ID = int64(x)
			} else {
				f.Re = int64(x)
			}
			b = b[n:]
		case pbFrameNum[num] != nil:
			raw, n := protowire.ConsumeBytes(b)
			if t != protowire.BytesType || n < 0 {
				return Frame{}, malformed("Frame")
			}
			mem := pbFrameNum[num]
			if f.Frame != "" {
				return Frame{}, malformed("a Frame with two bodies")
			}
			f.Frame = mem.kind
			if err := readStruct(mem.msg, raw, reflect.ValueOf(&f).Elem()); err != nil {
				return Frame{}, err
			}
			b = b[n:]
		default:
			n := protowire.ConsumeFieldValue(num, t, b)
			if n < 0 {
				return Frame{}, malformed("Frame")
			}
			b = b[n:]
		}
	}
	if f.Frame == "" {
		return Frame{}, ErrUnknownFrame
	}
	return f, nil
}

// MarshalEnvelope writes one envelope as an Envelope message: how a log
// keeps it, and what a deliver frame carries.
func MarshalEnvelope(e Envelope) ([]byte, error) {
	return appendStruct(make([]byte, 0, len(e.Ciphertext)+160), pbByName["Envelope"], reflect.ValueOf(&e).Elem())
}

// UnmarshalEnvelope reads what MarshalEnvelope wrote.
func UnmarshalEnvelope(b []byte) (Envelope, error) {
	var e Envelope
	err := readStruct(pbByName["Envelope"], b, reflect.ValueOf(&e).Elem())
	return e, err
}

// DeliverFrame builds a deliver frame around envelopes that are already
// Envelope messages (MarshalEnvelope), without reading them: one alone as
// `envelope`, several as `envelopes`. A hub sends what its log holds this
// way.
func DeliverFrame(envelopes [][]byte) []byte {
	mem := pbFrameBy[FDeliver]
	m := &pbMessages[mem.msg]
	num := m.byJSON("envelopes").num
	if len(envelopes) == 1 {
		num = m.byJSON("envelope").num
	}
	size := 0
	for _, e := range envelopes {
		size += len(e) + 6
	}
	body := make([]byte, 0, size)
	for _, e := range envelopes {
		body = protowire.AppendBytes(protowire.AppendTag(body, num, protowire.BytesType), e)
	}
	return protowire.AppendBytes(protowire.AppendTag(make([]byte, 0, len(body)+8), mem.num, protowire.BytesType), body)
}

// Marshal writes a value as the named message of the link's schema. It is
// for what is kept or carried as bytes beside a frame: a struct whose json
// names are the message's fields.
func Marshal(message string, v any) ([]byte, error) {
	mi, ok := pbByName[message]
	if !ok {
		return nil, fmt.Errorf("wire: the schema has no message %s", message)
	}
	return appendStruct(nil, mi, reflect.ValueOf(v))
}

// Unmarshal reads the named message into the struct out points at.
func Unmarshal(message string, b []byte, out any) error {
	mi, ok := pbByName[message]
	if !ok {
		return fmt.Errorf("wire: the schema has no message %s", message)
	}
	v := reflect.ValueOf(out)
	if v.Kind() != reflect.Pointer || v.IsNil() {
		return errors.New("wire: Unmarshal needs a pointer")
	}
	return readStruct(mi, b, v.Elem())
}

// marshalBlock writes the content of a signed block: the message its name
// stands for (schema_gen.go, pbBlocks).
func marshalBlock(name string, content any) ([]byte, error) {
	mi, ok := pbBlocks[name]
	if !ok {
		return nil, fmt.Errorf("wire: nothing is signed as %q", name)
	}
	return appendStruct(nil, mi, reflect.ValueOf(content))
}

// unmarshalBlock reads a signed block's bytes into out, which may have
// fewer fields than the message: a caller that only needs to know who
// claims to have signed reads just that.
func unmarshalBlock(name string, signed []byte, out any) error {
	mi, ok := pbBlocks[name]
	if !ok {
		return fmt.Errorf("wire: nothing is signed as %q", name)
	}
	v := reflect.ValueOf(out)
	if v.Kind() != reflect.Pointer || v.IsNil() {
		return errors.New("wire: a block is read into a pointer")
	}
	// What was there before is not kept: a block is read whole.
	v.Elem().Set(reflect.Zero(v.Elem().Type()))
	return readStruct(mi, signed, v.Elem())
}

// PeekBytes reads a signed block's bytes without checking any signature: for
// a block this side has verified and kept, or to learn who claims to have
// signed one before looking up their key.
func PeekBytes(name string, signed []byte, out any) error {
	return PeekBlock(Frame{Frame: name, Signed: signed}, out)
}

// EncodeJSON writes a JSON object as the named message of the link's
// schema: for what is sealed inside something else and built as a loose
// object. Like every writer here it is exact or it refuses.
func EncodeJSON(message string, text []byte) ([]byte, error) {
	mi, ok := pbByName[message]
	if !ok {
		return nil, fmt.Errorf("wire: the schema has no message %s", message)
	}
	v, err := parseTree(text)
	if err != nil {
		return nil, err
	}
	return appendTree(nil, mi, v)
}

// DecodeJSON reads the named message back into the JSON object it was
// written from.
func DecodeJSON(message string, b []byte) ([]byte, error) {
	mi, ok := pbByName[message]
	if !ok {
		return nil, fmt.Errorf("wire: the schema has no message %s", message)
	}
	v, err := readTree(mi, b)
	if err != nil {
		return nil, err
	}
	return json.Marshal(v)
}

// WithPlace returns an Envelope message (MarshalEnvelope) with a place in a
// log beside it: the same bytes, followed by cursor and prev. Whoever
// delivers an envelope from its log adds the two this way and leaves the
// rest as it was written.
func WithPlace(envelope []byte, cursor, prev int64) []byte {
	m := &pbMessages[pbByName["Envelope"]]
	out := make([]byte, 0, len(envelope)+16)
	out = append(out, envelope...)
	if cursor != 0 {
		out = protowire.AppendVarint(protowire.AppendTag(out, m.byJSON("cursor").num, protowire.VarintType), uint64(cursor))
	}
	if prev != 0 {
		out = protowire.AppendVarint(protowire.AppendTag(out, m.byJSON("prev").num, protowire.VarintType), uint64(prev))
	}
	return out
}
