package wire

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"sync/atomic"
	"time"

	"google.golang.org/protobuf/encoding/protowire"

	"github.com/tikaydaes/churr/internal/wirepb"
)

// This file writes a parsed JSON value as a message and reads one back
// (pb.go). It is how the plaintext of a direct message and of an envelope
// travels: the link builds those as loose objects, one shape per kind, and
// reads them back the same way, so here an object goes in and an object
// comes out, and what is sealed in between is protobuf.
//
// Writing is exact or it refuses: an object with a member the message has
// no field for is an error, never a member quietly dropped. The two unions
// below then carry the object whole as JSON text, so a schema that has
// fallen behind the code costs bytes and loses nothing (EncodeDirect).
//
// JSON's `null` and an empty list or map are written as nothing, and read
// back as absent: CONVENTIONS.md has all three mean the same. Everything
// else that is present is written, a zero or an empty text included.

// The two forms a time has in JSON: with milliseconds, and the one written
// for a time that is not a whole millisecond.
const (
	treeMillis = "2006-01-02T15:04:05.000Z"
	treeMicros = "2006-01-02T15:04:05.000000Z"
)

func parseTree(text []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(text))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	if dec.More() {
		return nil, errors.New("wire: more than one JSON value")
	}
	return v, nil
}

func treeInt(v any) (int64, bool) {
	switch n := v.(type) {
	case json.Number:
		i, err := strconv.ParseInt(string(n), 10, 64)
		return i, err == nil
	case float64:
		if n != math.Trunc(n) || math.Abs(n) >= 1<<63 {
			return 0, false
		}
		return int64(n), true
	case int64:
		return n, true
	case int:
		return int64(n), true
	}
	return 0, false
}

func treeFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case json.Number:
		f, err := strconv.ParseFloat(string(n), 64)
		return f, err == nil
	case float64:
		return n, true
	case int64:
		return float64(n), true
	case int:
		return float64(n), true
	}
	return 0, false
}

func treeErr(name string, err error) error { return fmt.Errorf("%s.%w", name, err) }

// appendTree writes the content of message mi for a JSON value.
func appendTree(b []byte, mi int, v any) ([]byte, error) {
	m := &pbMessages[mi]
	if m.wrap {
		return appendTreeField(b, &m.fields[0], v)
	}
	obj, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s: not an object", m.name)
	}
	return appendTreeObject(b, m, obj, nil)
}

// appendTreeObject writes an object's members. skip names members that are
// not this message's to write: the ones a union carries itself.
func appendTreeObject(b []byte, m *pbMessage, obj map[string]any, skip map[string]bool) ([]byte, error) {
	var rest map[string]any
	for k, v := range obj {
		if skip[k] || m.byJSON(k) != nil {
			continue
		}
		if m.base == "" {
			return nil, fmt.Errorf("%s: the schema has no field %q", m.name, k)
		}
		if rest == nil {
			rest = map[string]any{}
		}
		rest[k] = v
	}
	if m.base != "" {
		sub, err := wirepb.AppendNamed(nil, m.base, map[string]any(orEmpty(rest)))
		if err != nil {
			return nil, err
		}
		b = protowire.AppendBytes(protowire.AppendTag(b, 1, protowire.BytesType), sub)
	}
	var err error
	for i := range m.fields {
		f := &m.fields[i]
		v, present := obj[f.json]
		if !present || v == nil || skip[f.json] {
			continue
		}
		if b, err = appendTreeField(b, f, v); err != nil {
			return nil, treeErr(m.name, err)
		}
	}
	return b, nil
}

func orEmpty(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}

func appendTreeField(b []byte, f *pbField, v any) ([]byte, error) {
	switch f.card {
	case pbOne:
		return appendTreeOne(b, f.num, f, v)
	case pbList:
		list, ok := v.([]any)
		if !ok {
			return nil, fmt.Errorf("%s: not a list", f.json)
		}
		if len(list) == 0 {
			return b, nil
		}
		if packedKind(f.kind) {
			var content []byte
			var err error
			for _, e := range list {
				if content, err = appendTreeScalar(content, f, e); err != nil {
					return nil, err
				}
			}
			return protowire.AppendBytes(protowire.AppendTag(b, f.num, protowire.BytesType), content), nil
		}
		var err error
		for _, e := range list {
			if b, err = appendTreeOne(b, f.num, f, e); err != nil {
				return nil, err
			}
		}
		return b, nil
	case pbMap, pbUmap:
		obj, ok := v.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%s: not an object", f.json)
		}
		keys := make([]string, 0, len(obj))
		for k := range obj {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			var entry []byte
			if f.card == pbUmap {
				id, ok := parseID(k)
				if !ok {
					return nil, fmt.Errorf("%s: the key %q is not an identifier", f.json, k)
				}
				entry = protowire.AppendBytes(protowire.AppendTag(entry, 1, protowire.BytesType), id[:])
			} else {
				entry = protowire.AppendString(protowire.AppendTag(entry, 1, protowire.BytesType), k)
			}
			entry, err := appendTreeOne(entry, 2, f, obj[k])
			if err != nil {
				return nil, err
			}
			b = protowire.AppendBytes(protowire.AppendTag(b, f.num, protowire.BytesType), entry)
		}
		return b, nil
	}
	return nil, fmt.Errorf("%s: unknown cardinality", f.json)
}

func appendTreeScalar(b []byte, f *pbField, v any) ([]byte, error) {
	switch f.kind {
	case pbInt:
		if i, ok := treeInt(v); ok {
			return protowire.AppendVarint(b, uint64(i)), nil
		}
	case pbMs:
		if i, ok := treeInt(v); ok {
			return protowire.AppendVarint(b, toMicros(i)), nil
		}
	case pbTime:
		if s, ok := v.(string); ok {
			if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
				return protowire.AppendVarint(b, uint64(t.UnixMicro())), nil
			}
		}
	case pbBool:
		if x, ok := v.(bool); ok {
			return protowire.AppendVarint(b, protowire.EncodeBool(x)), nil
		}
	case pbDouble:
		if x, ok := treeFloat(v); ok {
			return protowire.AppendFixed64(b, math.Float64bits(x)), nil
		}
	}
	return nil, fmt.Errorf("%s: %v does not fit the field", f.json, v)
}

func appendTreeOne(b []byte, num protowire.Number, f *pbField, v any) ([]byte, error) {
	if packedKind(f.kind) {
		t := protowire.VarintType
		if f.kind == pbDouble {
			t = protowire.Fixed64Type
		}
		return appendTreeScalar(protowire.AppendTag(b, num, t), f, v)
	}
	var content []byte
	var err error
	switch f.kind {
	case pbStr:
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("%s: not text", f.json)
		}
		return protowire.AppendString(protowire.AppendTag(b, num, protowire.BytesType), s), nil
	case pbUuid:
		s, _ := v.(string)
		id, ok := parseID(s)
		if !ok {
			return nil, fmt.Errorf("%s: not an identifier", f.json)
		}
		content = id[:]
	case pbBytes:
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("%s: not bytes", f.json)
		}
		if content, err = Decode(s); err != nil {
			return nil, fmt.Errorf("%s: not bytes", f.json)
		}
	case pbMsg:
		content, err = appendTree(nil, f.ref, v)
	case pbClient:
		content, err = wirepb.AppendNamed(nil, f.client, v)
	case pbValue:
		content, err = wirepb.AppendValue(nil, v)
	}
	if err != nil {
		return nil, treeErr(f.json, err)
	}
	return protowire.AppendBytes(protowire.AppendTag(b, num, protowire.BytesType), content), nil
}

// readTree reads the content of message mi back into a JSON value.
func readTree(mi int, b []byte) (any, error) {
	m := &pbMessages[mi]
	out := map[string]any{}
	if err := readTreeInto(m, b, out); err != nil {
		return nil, err
	}
	if m.wrap {
		if v, ok := out[m.fields[0].json]; ok {
			return v, nil
		}
		if m.fields[0].card == pbList {
			return []any{}, nil
		}
		return map[string]any{}, nil
	}
	return out, nil
}

func readTreeInto(m *pbMessage, b []byte, out map[string]any) error {
	for len(b) > 0 {
		num, t, n := protowire.ConsumeTag(b)
		if n < 0 {
			return malformed(m.name)
		}
		b = b[n:]
		f := m.byNum(num)
		if m.base != "" && num == 1 {
			raw, n := protowire.ConsumeBytes(b)
			if t != protowire.BytesType || n < 0 {
				return malformed(m.name)
			}
			v, err := wirepb.ReadNamed(m.base, raw)
			if err != nil {
				return err
			}
			if obj, ok := v.(map[string]any); ok {
				for k, x := range obj {
					out[k] = x
				}
			}
			b = b[n:]
			continue
		}
		if f == nil {
			n = protowire.ConsumeFieldValue(num, t, b)
			if n < 0 {
				return malformed(m.name)
			}
			b = b[n:]
			continue
		}
		n, err := readTreeField(f, t, b, out)
		if err != nil {
			return err
		}
		b = b[n:]
	}
	return nil
}

func readTreeField(f *pbField, t protowire.Type, b []byte, out map[string]any) (int, error) {
	bad := func() (int, error) { return 0, malformed(f.json) }
	switch f.card {
	case pbOne:
		v, n, err := readTreeOne(f, t, b)
		if err != nil {
			return 0, err
		}
		out[f.json] = v
		return n, nil
	case pbList:
		list, _ := out[f.json].([]any)
		if packedKind(f.kind) && t == protowire.BytesType {
			raw, n := protowire.ConsumeBytes(b)
			if n < 0 {
				return bad()
			}
			vt := protowire.VarintType
			if f.kind == pbDouble {
				vt = protowire.Fixed64Type
			}
			for len(raw) > 0 {
				v, m, err := readTreeOne(f, vt, raw)
				if err != nil {
					return 0, err
				}
				list = append(list, v)
				raw = raw[m:]
			}
			out[f.json] = list
			return n, nil
		}
		v, n, err := readTreeOne(f, t, b)
		if err != nil {
			return 0, err
		}
		out[f.json] = append(list, v)
		return n, nil
	case pbMap, pbUmap:
		raw, n := protowire.ConsumeBytes(b)
		if t != protowire.BytesType || n < 0 {
			return bad()
		}
		obj, _ := out[f.json].(map[string]any)
		if obj == nil {
			obj = map[string]any{}
			out[f.json] = obj
		}
		var key string
		var val any
		for len(raw) > 0 {
			num, et, m := protowire.ConsumeTag(raw)
			if m < 0 {
				return bad()
			}
			raw = raw[m:]
			switch num {
			case 1:
				k, m := protowire.ConsumeBytes(raw)
				if et != protowire.BytesType || m < 0 {
					return bad()
				}
				if f.card == pbUmap {
					if len(k) != 16 {
						return bad()
					}
					key = formatID(k)
				} else {
					key = string(k)
				}
				raw = raw[m:]
			case 2:
				v, m, err := readTreeOne(f, et, raw)
				if err != nil {
					return 0, err
				}
				val = v
				raw = raw[m:]
			default:
				m := protowire.ConsumeFieldValue(num, et, raw)
				if m < 0 {
					return bad()
				}
				raw = raw[m:]
			}
		}
		obj[key] = val
		return n, nil
	}
	return bad()
}

func readTreeOne(f *pbField, t protowire.Type, b []byte) (any, int, error) {
	bad := func() (any, int, error) { return nil, 0, malformed(f.json) }
	switch f.kind {
	case pbDouble:
		x, n := protowire.ConsumeFixed64(b)
		if t != protowire.Fixed64Type || n < 0 {
			return bad()
		}
		return math.Float64frombits(x), n, nil
	case pbInt, pbTime, pbMs, pbBool:
		x, n := protowire.ConsumeVarint(b)
		if t != protowire.VarintType || n < 0 {
			return bad()
		}
		switch f.kind {
		case pbBool:
			return x != 0, n, nil
		case pbInt:
			return int64(x), n, nil
		case pbMs:
			ms, ok := fromMicros(x)
			if !ok {
				return bad()
			}
			return ms, n, nil
		}
		us := int64(x)
		at := time.UnixMicro(us).UTC()
		if us%1000 == 0 {
			return at.Format(treeMillis), n, nil
		}
		return at.Format(treeMicros), n, nil
	}
	raw, n := protowire.ConsumeBytes(b)
	if t != protowire.BytesType || n < 0 {
		return bad()
	}
	var v any
	var err error
	switch f.kind {
	case pbStr:
		v = string(raw)
	case pbUuid:
		if len(raw) != 16 {
			return bad()
		}
		v = formatID(raw)
	case pbBytes:
		v = Encode(raw)
	case pbMsg:
		v, err = readTree(f.ref, raw)
	case pbClient:
		v, err = wirepb.ReadNamed(f.client, raw)
	case pbValue:
		v, err = wirepb.ReadValue(raw)
	}
	return v, n, err
}

// ------------------------------------------------------------------ unions

// pbUnion is a message that carries one body out of several: the plaintext
// of a direct message, or of an envelope. Its JSON form is one object that
// holds the name of the body (kindKey), the union's own members, and the
// body's.
type pbUnion struct {
	name    string
	kindKey string
	own     []pbField
	jsonNum protowire.Number
	byKind  map[string]*pbMember
	byNum   map[protowire.Number]*pbMember
	skip    map[string]bool
}

func newUnion(name, kindKey string, members []pbMember, jsonNum protowire.Number, own ...pbField) *pbUnion {
	u := &pbUnion{name: name, kindKey: kindKey, own: own, jsonNum: jsonNum, byKind: map[string]*pbMember{},
		byNum: map[protowire.Number]*pbMember{}, skip: map[string]bool{kindKey: true}}
	for i := range members {
		u.byKind[members[i].kind] = &members[i]
		u.byNum[members[i].num] = &members[i]
	}
	for _, f := range own {
		u.skip[f.json] = true
	}
	return u
}

// The numbers of the two unions' own fields are fixed here and in link.py
// together; a body's number comes from the lock.
var (
	directUnion = newUnion("Direct", "kind", pbDirects, 4,
		pbField{json: "msg_id", num: 1, kind: pbUuid}, pbField{json: "at", num: 2, kind: pbTime}, pbField{json: "re", num: 3, kind: pbUuid})
	eventUnion = newUnion("Event", "type", pbEvents, 2, pbField{json: "at", num: 1, kind: pbTime})
)

// untyped is told each time a direct message or an event had to travel as
// JSON text because the schema could not hold it.
var untyped atomic.Pointer[func(union, kind string, why error)]

// OnUntyped sets a function that is told whenever the plaintext of a direct
// message or of an event could not be written as its protobuf message and
// travelled as JSON text inside it. A server logs it; tests fail on it.
func OnUntyped(fn func(union, kind string, why error)) {
	if fn == nil {
		untyped.Store(nil)
		return
	}
	untyped.Store(&fn)
}

func (u *pbUnion) typed(text []byte) ([]byte, string, error) {
	v, err := parseTree(text)
	obj, ok := v.(map[string]any)
	if err != nil || !ok {
		return nil, "", errors.New("not one JSON object")
	}
	kind, _ := obj[u.kindKey].(string)
	mem := u.byKind[kind]
	if mem == nil {
		return nil, kind, fmt.Errorf("the schema has no %q", kind)
	}
	var b []byte
	for i := range u.own {
		f := &u.own[i]
		if x := obj[f.json]; x != nil {
			if b, err = appendTreeOne(b, f.num, f, x); err != nil {
				return nil, kind, err
			}
		}
	}
	body, err := appendTreeObject(nil, &pbMessages[mem.msg], obj, u.skip)
	if err != nil {
		return nil, kind, err
	}
	return protowire.AppendBytes(protowire.AppendTag(b, mem.num, protowire.BytesType), body), kind, nil
}

// encode writes the JSON object text as the union. It always succeeds: an
// object the schema cannot hold exactly is carried whole as JSON text.
func (u *pbUnion) encode(text []byte) []byte {
	b, kind, why := u.typed(text)
	if why == nil {
		return b
	}
	if fn := untyped.Load(); fn != nil {
		(*fn)(u.name, kind, why)
	}
	return protowire.AppendBytes(protowire.AppendTag(nil, u.jsonNum, protowire.BytesType), text)
}

// decode reads the union back into the JSON object text it was written
// from.
func (u *pbUnion) decode(b []byte) ([]byte, error) {
	out := map[string]any{}
	var kind string
	for len(b) > 0 {
		num, t, n := protowire.ConsumeTag(b)
		if n < 0 {
			return nil, malformed(u.name)
		}
		b = b[n:]
		if mem := u.byNum[num]; mem != nil || num == u.jsonNum {
			raw, n := protowire.ConsumeBytes(b)
			if t != protowire.BytesType || n < 0 {
				return nil, malformed(u.name)
			}
			if mem == nil {
				// Carried as JSON: it is the whole object.
				return append([]byte(nil), raw...), nil
			}
			if err := readTreeInto(&pbMessages[mem.msg], raw, out); err != nil {
				return nil, err
			}
			kind = mem.kind
			b = b[n:]
			continue
		}
		var own *pbField
		for i := range u.own {
			if u.own[i].num == num {
				own = &u.own[i]
			}
		}
		if own == nil {
			n = protowire.ConsumeFieldValue(num, t, b)
			if n < 0 {
				return nil, malformed(u.name)
			}
			b = b[n:]
			continue
		}
		v, n, err := readTreeOne(own, t, b)
		if err != nil {
			return nil, err
		}
		out[own.json] = v
		b = b[n:]
	}
	if kind == "" {
		return nil, fmt.Errorf("wire: a %s of a kind this schema does not know", u.name)
	}
	out[u.kindKey] = kind
	return json.Marshal(out)
}

// EncodeDirect writes the plaintext of a direct message (section 7), given
// as the JSON object it is: kind, msg_id, at, re and the kind's own members.
func EncodeDirect(text []byte) []byte { return directUnion.encode(text) }

// DecodeDirect reads the plaintext of a direct message back into its JSON
// object.
func DecodeDirect(b []byte) ([]byte, error) { return directUnion.decode(b) }

// EncodeEvent writes an inner event, the plaintext of an envelope (section
// 11), given as the JSON object it is: type, at and the type's own members.
func EncodeEvent(text []byte) []byte { return eventUnion.encode(text) }

// DecodeEvent reads an inner event back into its JSON object.
func DecodeEvent(b []byte) ([]byte, error) { return eventUnion.decode(b) }
