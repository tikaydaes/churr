package wirepb

import (
	"encoding/json"
	"fmt"

	"google.golang.org/protobuf/encoding/protowire"

	"github.com/tikaydaes/churr/internal/model"
)

// EncodeFrame writes one frame of the live connection, given as the JSON
// object it is, as an Envelope. It always succeeds: a frame the schema
// cannot hold exactly is carried whole as JSON inside the envelope.
func EncodeFrame(frame map[string]any) []byte {
	b, _ := encodeFrame(frame, nil)
	return b
}

// EncodeFrameJSON is EncodeFrame for a frame already written as JSON text,
// which is what the live connection holds. typed is false when the frame
// had to be carried as JSON; why then says what did not fit, for a log.
func EncodeFrameJSON(text []byte) (b []byte, typed bool, why error) {
	v, err := parseJSON(text)
	obj, ok := v.(map[string]any)
	if err != nil || !ok {
		return nil, false, fmt.Errorf("wirepb: a frame must be one JSON object")
	}
	b, why = encodeFrame(obj, text)
	return b, why == nil, why
}

func encodeFrame(obj map[string]any, text []byte) ([]byte, error) {
	b, why := typedFrame(obj)
	if why == nil {
		return b, nil
	}
	if text == nil {
		text, _ = json.Marshal(obj)
	}
	return jsonFrame(obj, text), why
}

// EncodeEvent writes an event as an Envelope.
func EncodeEvent(ev model.Event) ([]byte, error) {
	text, err := json.Marshal(ev)
	if err != nil {
		return nil, err
	}
	b, _, _ := EncodeFrameJSON(text)
	return b, nil
}

// DecodeFrame reads an Envelope back into the JSON object of its frame.
// Whole numbers are int64 (json.Number where the frame travelled as JSON).
// An envelope from a newer peer whose body this schema does not know gives
// ErrUnknownFrame; the caller skips it.
func DecodeFrame(b []byte) (map[string]any, error) { return reader{}.frame(b) }

func (rd reader) frame(b []byte) (map[string]any, error) {
	var (
		typ, text []byte
		body      *frame
		bodyRaw   []byte
		nulls     []protowire.Number
		head      = map[string]any{}
	)
	for len(b) > 0 {
		num, t, n := protowire.ConsumeTag(b)
		if n < 0 {
			return nil, malformed("Envelope")
		}
		b = b[n:]
		var err error
		switch {
		case num == numNull:
			n, err = readPackedNums(t, b, &nulls)
		case num == envType || num == envJSON || frameByNum[num] != nil:
			raw, m := protowire.ConsumeBytes(b)
			if t != protowire.BytesType || m < 0 {
				return nil, malformed("Envelope")
			}
			n = m
			switch num {
			case envType:
				typ = raw
			case envJSON:
				text = raw
			default:
				body, bodyRaw = frameByNum[num], raw
			}
		case num == envAt || num == envConv || num == envSeq:
			f := &envelopeFields[num-envAt]
			n, err = rd.readField(f, f.json, t, b, head)
		default:
			n, err = rd.unknown(protowire.ConsumeFieldValue(num, t, b), "Envelope")
		}
		if err != nil {
			return nil, err
		}
		b = b[n:]
	}
	if text != nil {
		v, err := parseJSON(text)
		obj, ok := v.(map[string]any)
		if err != nil || !ok {
			return nil, malformed("Envelope.json")
		}
		return obj, nil
	}
	if body == nil {
		return nil, fmt.Errorf("%w (%q)", ErrUnknownFrame, typ)
	}
	out := make(map[string]any, 8)
	if err := rd.readObject(&messages[body.msg], bodyRaw, out, nil); err != nil {
		return nil, err
	}
	out["type"] = body.typ
	for k, v := range head {
		out[k] = v
	}
	for _, num := range nulls {
		if num >= envAt && num <= envSeq {
			if name := envelopeFields[num-envAt].json; out[name] == nil {
				out[name] = nil
			}
		}
	}
	return out, nil
}

// DecodeFrameJSON reads an Envelope back into the JSON text of its frame.
func DecodeFrameJSON(b []byte) ([]byte, error) {
	obj, err := DecodeFrame(b)
	if err != nil {
		return nil, err
	}
	return json.Marshal(obj)
}

// DecodeClientFrameJSON reads an Envelope a client sent into the JSON text
// of its frame. It is strict, as the server is with a request body: a
// field the frame does not have is ErrUnknownField.
func DecodeClientFrameJSON(b []byte) ([]byte, error) {
	obj, err := reader{strict: true}.frame(b)
	if err != nil {
		return nil, err
	}
	return json.Marshal(obj)
}

// DecodeEvent reads an Envelope back into an event, exactly as the JSON
// form of the same frame would be read.
func DecodeEvent(b []byte) (model.Event, error) {
	text, err := DecodeFrameJSON(b)
	if err != nil {
		return model.Event{}, err
	}
	var ev model.Event
	err = json.Unmarshal(text, &ev)
	return ev, err
}
