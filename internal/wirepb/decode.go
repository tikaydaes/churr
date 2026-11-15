package wirepb

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"

	"google.golang.org/protobuf/encoding/protowire"
)

// ErrMalformed is returned for bytes that are not the protobuf they should be.
var ErrMalformed = errors.New("wirepb: malformed protobuf")

// ErrUnknownFrame is returned for an envelope whose body this schema does
// not know and that carries no JSON: a frame from a newer peer, to skip.
var ErrUnknownFrame = errors.New("wirepb: a frame this schema does not know")

func malformed(what string) error { return fmt.Errorf("%w: %s", ErrMalformed, what) }

// ErrUnknownField is returned by a strict read, which a request body gets:
// the conventions have a server refuse what it does not know.
var ErrUnknownField = errors.New("wirepb: a field or value the schema does not have")

// reader reads protobuf back into JSON values. A strict reader refuses
// field numbers and enum values the schema does not have; the other skips
// them, as a client must.
type reader struct{ strict bool }

// unknown is called with the length of a field the schema does not have.
func (rd reader) unknown(n int, where string) (int, error) {
	if n < 0 {
		return 0, malformed(where)
	}
	if rd.strict {
		return 0, fmt.Errorf("%w (in %s)", ErrUnknownField, where)
	}
	return n, nil
}

// skipped is what readOne gives for a value to leave out: an enum number
// this schema does not have.
type skipped struct{}

// readScalar reads one varint or fixed64 value of a packable kind.
func (rd reader) readScalar(f *field, t protowire.Type, b []byte) (any, int, error) {
	if f.kind == kindDouble {
		if t != protowire.Fixed64Type {
			return nil, 0, malformed(f.json)
		}
		v, n := protowire.ConsumeFixed64(b)
		if n < 0 {
			return nil, 0, malformed(f.json)
		}
		return math.Float64frombits(v), n, nil
	}
	if t != protowire.VarintType {
		return nil, 0, malformed(f.json)
	}
	v, n := protowire.ConsumeVarint(b)
	if n < 0 {
		return nil, 0, malformed(f.json)
	}
	switch f.kind {
	case kindInt:
		return int64(v), n, nil
	case kindBool:
		return v != 0, n, nil
	case kindTime:
		return formatTime(int64(v)), n, nil
	case kindEnum:
		values := enums[f.ref]
		if v == 0 || v >= uint64(len(values)) || values[v] == "" {
			if rd.strict {
				return nil, 0, fmt.Errorf("%w (%s)", ErrUnknownField, f.json)
			}
			return skipped{}, n, nil
		}
		return values[v], n, nil
	}
	panic("wirepb: not a scalar kind")
}

// readOne reads one value of a field (an element, for a list) from the
// start of b, which follows a tag of wire type t.
func (rd reader) readOne(f *field, t protowire.Type, b []byte) (any, int, error) {
	if packed(f.kind) {
		return rd.readScalar(f, t, b)
	}
	if t != protowire.BytesType {
		return nil, 0, malformed(f.json)
	}
	raw, n := protowire.ConsumeBytes(b)
	if n < 0 {
		return nil, 0, malformed(f.json)
	}
	var v any
	var err error
	switch f.kind {
	case kindString:
		v = string(raw)
	case kindUuid:
		if len(raw) != 16 {
			return nil, 0, malformed(f.json + " is not 16 bytes")
		}
		v = formatUUID(raw)
	case kindBytes:
		v = base64.RawURLEncoding.EncodeToString(raw)
	case kindMsg:
		v, err = rd.readMessage(f.ref, raw)
	case kindValue:
		v, err = readValue(raw)
	case kindEnvelope:
		v, err = rd.frame(raw)
		if errors.Is(err, ErrUnknownFrame) {
			v, err = skipped{}, nil
		}
	}
	return v, n, err
}

func readPackedNums(t protowire.Type, b []byte, into *[]protowire.Number) (int, error) {
	if t == protowire.VarintType {
		v, n := protowire.ConsumeVarint(b)
		if n < 0 {
			return 0, malformed("field numbers")
		}
		*into = append(*into, protowire.Number(v))
		return n, nil
	}
	raw, n := protowire.ConsumeBytes(b)
	if t != protowire.BytesType || n < 0 {
		return 0, malformed("field numbers")
	}
	for len(raw) > 0 {
		v, m := protowire.ConsumeVarint(raw)
		if m < 0 {
			return 0, malformed("field numbers")
		}
		*into = append(*into, protowire.Number(v))
		raw = raw[m:]
	}
	return n, nil
}

// readEntry reads one map entry: key 1, value 2.
func (rd reader) readEntry(f *field, raw []byte) (string, any, error) {
	vf, uuidKey := f, false
	if f.card == cardUmap {
		vf, uuidKey = &messages[f.ref].fields[1], true
	}
	var key string
	var val any
	got := false
	for len(raw) > 0 {
		num, t, n := protowire.ConsumeTag(raw)
		if n < 0 {
			return "", nil, malformed(f.json)
		}
		raw = raw[n:]
		switch num {
		case 1:
			k, m := protowire.ConsumeBytes(raw)
			if t != protowire.BytesType || m < 0 || (uuidKey && len(k) != 16) {
				return "", nil, malformed(f.json)
			}
			if uuidKey {
				key = formatUUID(k)
			} else {
				key = string(k)
			}
			raw = raw[m:]
		case 2:
			v, m, err := rd.readOne(vf, t, raw)
			if err != nil {
				return "", nil, err
			}
			val, raw, got = v, raw[m:], true
		default:
			m, err := rd.unknown(protowire.ConsumeFieldValue(num, t, raw), f.json)
			if err != nil {
				return "", nil, err
			}
			raw = raw[m:]
		}
	}
	if !got {
		val = zero(vf)
	}
	return key, val, nil
}

// zero is the value of a map entry written without one, as some encoders
// write a default.
func zero(f *field) any {
	switch f.kind {
	case kindString, kindBytes:
		return ""
	case kindInt:
		return int64(0)
	case kindDouble:
		return float64(0)
	case kindBool:
		return false
	case kindMsg:
		v, _ := reader{}.readMessage(f.ref, nil)
		return v
	case kindValue:
		return nil
	}
	return skipped{} // an identifier, a time, an enum: nothing sensible
}

// readField reads one occurrence of a field into out, which holds what
// earlier occurrences left.
func (rd reader) readField(f *field, name string, t protowire.Type, b []byte, out map[string]any) (int, error) {
	switch f.card {
	case cardOne:
		v, n, err := rd.readOne(f, t, b)
		if err != nil {
			return 0, err
		}
		if _, skip := v.(skipped); !skip {
			out[name] = v
		}
		return n, nil
	case cardList:
		list, _ := out[name].([]any)
		if packed(f.kind) && t == protowire.BytesType {
			raw, n := protowire.ConsumeBytes(b)
			if n < 0 {
				return 0, malformed(f.json)
			}
			st := protowire.VarintType
			if f.kind == kindDouble {
				st = protowire.Fixed64Type
			}
			for len(raw) > 0 {
				v, m, err := rd.readScalar(f, st, raw)
				if err != nil {
					return 0, err
				}
				if _, skip := v.(skipped); !skip {
					list = append(list, v)
				}
				raw = raw[m:]
			}
			out[name] = list
			return n, nil
		}
		v, n, err := rd.readOne(f, t, b)
		if err != nil {
			return 0, err
		}
		if _, skip := v.(skipped); !skip {
			list = append(list, v)
		}
		out[name] = list
		return n, nil
	default:
		raw, n := protowire.ConsumeBytes(b)
		if t != protowire.BytesType || n < 0 {
			return 0, malformed(f.json)
		}
		k, v, err := rd.readEntry(f, raw)
		if err != nil {
			return 0, err
		}
		m, _ := out[name].(map[string]any)
		if m == nil {
			m = map[string]any{}
		}
		if _, skip := v.(skipped); !skip {
			m[k] = v
		}
		out[name] = m
		return n, nil
	}
}

// readMessage gives the JSON value of message mi.
func (rd reader) readMessage(mi int, b []byte) (any, error) {
	m := &messages[mi]
	if m.shape == shapeWrap {
		f := &m.fields[0]
		out := map[string]any{}
		for len(b) > 0 {
			num, t, n := protowire.ConsumeTag(b)
			if n < 0 {
				return nil, malformed(m.name)
			}
			b = b[n:]
			if num == 1 {
				n, err := rd.readField(f, "v", t, b, out)
				if err != nil {
					return nil, err
				}
				b = b[n:]
				continue
			}
			var err error
			if n, err = rd.unknown(protowire.ConsumeFieldValue(num, t, b), m.name); err != nil {
				return nil, err
			}
			b = b[n:]
		}
		if v, ok := out["v"]; ok {
			return v, nil
		}
		if f.card == cardList {
			return []any{}, nil
		}
		return map[string]any{}, nil
	}
	out := make(map[string]any, len(m.fields))
	return out, rd.readObject(m, b, out, nil)
}

// readObject reads an object message's fields into out. other, when not
// nil, is called for a field number the message does not have.
func (rd reader) readObject(m *message, b []byte, out map[string]any, other func(num protowire.Number, t protowire.Type, b []byte) (int, error)) error {
	var empties, nulls []protowire.Number
	var extra map[string]any
	for len(b) > 0 {
		num, t, n := protowire.ConsumeTag(b)
		if n < 0 {
			return malformed(m.name)
		}
		b = b[n:]
		var err error
		switch f := m.byNum[num]; {
		case num == numEmpty:
			n, err = readPackedNums(t, b, &empties)
		case num == numNull:
			n, err = readPackedNums(t, b, &nulls)
		case f != nil && f.json == "":
			if extra == nil {
				extra = map[string]any{}
			}
			holder := map[string]any{"x": extra}
			n, err = rd.readField(f, "x", t, b, holder)
		case f != nil:
			n, err = rd.readField(f, f.json, t, b, out)
		case other != nil:
			n, err = other(num, t, b)
		default:
			n, err = rd.unknown(protowire.ConsumeFieldValue(num, t, b), m.name)
		}
		if err != nil {
			return err
		}
		b = b[n:]
	}
	for _, num := range empties {
		if f := m.byNum[num]; f != nil && f.json != "" && f.card != cardOne {
			if _, set := out[f.json]; !set {
				if f.card == cardList {
					out[f.json] = []any{}
				} else {
					out[f.json] = map[string]any{}
				}
			}
		}
	}
	for _, num := range nulls {
		if f := m.byNum[num]; f != nil && f.json != "" {
			if _, set := out[f.json]; !set {
				out[f.json] = nil
			}
		}
	}
	for k, v := range extra {
		if _, set := out[k]; !set && m.find(k) == nil {
			out[k] = v
		}
	}
	if m.consts > 0 {
		for i := range m.fields {
			if f := &m.fields[i]; f.kind == kindConst {
				out[f.json] = f.text
			}
		}
	}
	return nil
}

// readValue gives the JSON value a Value message holds.
func readValue(b []byte) (any, error) {
	var out any
	for len(b) > 0 {
		num, t, n := protowire.ConsumeTag(b)
		if n < 0 {
			return nil, malformed("Value")
		}
		b = b[n:]
		switch {
		case num >= 1 && num <= 3 && t == protowire.VarintType:
			v, m := protowire.ConsumeVarint(b)
			if m < 0 {
				return nil, malformed("Value")
			}
			switch num {
			case 1:
				out = nil
			case 2:
				out = v != 0
			case 3:
				out = int64(v)
			}
			n = m
		case num == 4 && t == protowire.Fixed64Type:
			v, m := protowire.ConsumeFixed64(b)
			if m < 0 {
				return nil, malformed("Value")
			}
			out, n = math.Float64frombits(v), m
		case num >= 5 && num <= 7 && t == protowire.BytesType:
			raw, m := protowire.ConsumeBytes(b)
			if m < 0 {
				return nil, malformed("Value")
			}
			n = m
			switch num {
			case 5:
				out = string(raw)
			case 6:
				list := []any{}
				for len(raw) > 0 {
					inum, it, k := protowire.ConsumeTag(raw)
					if k < 0 {
						return nil, malformed("Value")
					}
					raw = raw[k:]
					if inum == 1 && it == protowire.BytesType {
						item, k := protowire.ConsumeBytes(raw)
						if k < 0 {
							return nil, malformed("Value")
						}
						v, err := readValue(item)
						if err != nil {
							return nil, err
						}
						list, raw = append(list, v), raw[k:]
						continue
					}
					if k = protowire.ConsumeFieldValue(inum, it, raw); k < 0 {
						return nil, malformed("Value")
					}
					raw = raw[k:]
				}
				out = list
			case 7:
				holder := map[string]any{}
				f := &field{json: "x", num: 1, kind: kindValue, card: cardMap}
				for len(raw) > 0 {
					inum, it, k := protowire.ConsumeTag(raw)
					if k < 0 {
						return nil, malformed("Value")
					}
					raw = raw[k:]
					if inum == 1 {
						k, err := reader{}.readField(f, "x", it, raw, holder)
						if err != nil {
							return nil, err
						}
						raw = raw[k:]
						continue
					}
					if k = protowire.ConsumeFieldValue(inum, it, raw); k < 0 {
						return nil, malformed("Value")
					}
					raw = raw[k:]
				}
				if m, ok := holder["x"]; ok {
					out = m
				} else {
					out = map[string]any{}
				}
			}
		default:
			if n = protowire.ConsumeFieldValue(num, t, b); n < 0 {
				return nil, malformed("Value")
			}
		}
		b = b[n:]
	}
	return out, nil
}

// parseJSON reads one JSON value, keeping whole numbers whole.
func parseJSON(text []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(text))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	if dec.More() {
		return nil, errors.New("wirepb: more than one JSON value")
	}
	return v, nil
}
