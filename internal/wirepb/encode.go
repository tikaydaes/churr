package wirepb

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"time"

	"google.golang.org/protobuf/encoding/protowire"
)

// misfit says why a JSON value has no exact protobuf form. It is what makes
// the caller fall back to JSON; the text is for logs and tests.
type misfit struct {
	path string
	why  string
}

func (e *misfit) Error() string { return "wirepb: " + e.path + ": " + e.why }

func noFit(why string) error { return &misfit{why: why} }

// at puts a field name in front of a misfit's path as the error travels up.
func at(name string, err error) error {
	if m, ok := err.(*misfit); ok {
		if m.path == "" {
			m.path = name
		} else {
			m.path = name + "." + m.path
		}
	}
	return err
}

// The two forms a time has in JSON here: the contract's, with milliseconds,
// and the one this package writes for a time that is not a whole millisecond.
const (
	timeMillis = "2006-01-02T15:04:05.000Z"
	timeMicros = "2006-01-02T15:04:05.000000Z"
)

func parseTime(s string) (int64, bool) {
	layout := timeMillis
	if len(s) == len(timeMicros) {
		layout = timeMicros
	}
	t, err := time.Parse(layout, s)
	if err != nil {
		return 0, false
	}
	us := t.UnixMicro()
	// Only a time that will be written back exactly as it came.
	if formatTime(us) != s {
		return 0, false
	}
	return us, true
}

func formatTime(us int64) string {
	t := time.UnixMicro(us).UTC()
	if us%1000 == 0 {
		return t.Format(timeMillis)
	}
	return t.Format(timeMicros)
}

func parseUUID(s string) ([]byte, bool) {
	if len(s) != 36 || s[8] != '-' || s[13] != '-' || s[18] != '-' || s[23] != '-' {
		return nil, false
	}
	var raw [32]byte
	n := 0
	for i := 0; i < 36; i++ {
		c := s[i]
		if c == '-' {
			continue
		}
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return nil, false
		}
		raw[n] = c
		n++
	}
	out := make([]byte, 16)
	if n != 32 {
		return nil, false
	}
	if _, err := hex.Decode(out, raw[:]); err != nil {
		return nil, false
	}
	return out, true
}

func formatUUID(b []byte) string {
	var out [36]byte
	hex.Encode(out[0:8], b[0:4])
	out[8] = '-'
	hex.Encode(out[9:13], b[4:6])
	out[13] = '-'
	hex.Encode(out[14:18], b[6:8])
	out[18] = '-'
	hex.Encode(out[19:23], b[8:10])
	out[23] = '-'
	hex.Encode(out[24:36], b[10:16])
	return string(out[:])
}

// toInt reads a JSON number that is a whole number an int64 holds.
func toInt(v any) (int64, bool) {
	switch n := v.(type) {
	case json.Number:
		i, err := strconv.ParseInt(string(n), 10, 64)
		return i, err == nil
	case float64:
		if n != math.Trunc(n) || n < -9223372036854775808 || n >= 9223372036854775808 || (n == 0 && math.Signbit(n)) {
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

func toFloat(v any) (float64, bool) {
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

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// packed reports whether a list of this kind is written as one packed field.
func packed(k kind) bool {
	return k == kindInt || k == kindDouble || k == kindBool || k == kindTime || k == kindEnum
}

// appendScalar writes a value with no tag: the content of a packed list.
func appendScalar(b []byte, f *field, v any) ([]byte, error) {
	switch f.kind {
	case kindInt:
		i, ok := toInt(v)
		if !ok {
			return nil, noFit("not a whole number")
		}
		return protowire.AppendVarint(b, uint64(i)), nil
	case kindDouble:
		x, ok := toFloat(v)
		if !ok {
			return nil, noFit("not a number")
		}
		return protowire.AppendFixed64(b, math.Float64bits(x)), nil
	case kindBool:
		x, ok := v.(bool)
		if !ok {
			return nil, noFit("not true or false")
		}
		return protowire.AppendVarint(b, protowire.EncodeBool(x)), nil
	case kindTime:
		s, _ := v.(string)
		us, ok := parseTime(s)
		if !ok {
			return nil, noFit("not a time in the contract's form")
		}
		return protowire.AppendVarint(b, uint64(us)), nil
	case kindEnum:
		s, ok := v.(string)
		n, known := enumIndex[f.ref][s]
		if !ok || !known {
			return nil, noFit(fmt.Sprintf("%v is not a value the schema has", v))
		}
		return protowire.AppendVarint(b, uint64(n)), nil
	}
	panic("wirepb: not a scalar kind")
}

// appendOne writes one value under a number.
func appendOne(b []byte, num protowire.Number, f *field, v any) ([]byte, error) {
	if packed(f.kind) {
		t := protowire.VarintType
		if f.kind == kindDouble {
			t = protowire.Fixed64Type
		}
		return appendScalar(protowire.AppendTag(b, num, t), f, v)
	}
	var content []byte
	switch f.kind {
	case kindString:
		s, ok := v.(string)
		if !ok {
			return nil, noFit("not text")
		}
		return protowire.AppendString(protowire.AppendTag(b, num, protowire.BytesType), s), nil
	case kindUuid:
		s, _ := v.(string)
		raw, ok := parseUUID(s)
		if !ok {
			return nil, noFit("not an identifier")
		}
		content = raw
	case kindBytes:
		s, ok := v.(string)
		raw, err := base64.RawURLEncoding.Strict().DecodeString(s)
		if !ok || err != nil {
			return nil, noFit("not base64url")
		}
		content = raw
	case kindMsg:
		sub, err := appendMessage(nil, f.ref, v)
		if err != nil {
			return nil, err
		}
		content = sub
	case kindValue:
		sub, err := appendValue(nil, v)
		if err != nil {
			return nil, err
		}
		content = sub
	case kindEnvelope:
		obj, ok := v.(map[string]any)
		if !ok {
			return nil, noFit("not an object")
		}
		content = EncodeFrame(obj)
	}
	return protowire.AppendBytes(protowire.AppendTag(b, num, protowire.BytesType), content), nil
}

// appendField writes a present, non-null field of any cardinality.
func appendField(b []byte, f *field, v any) ([]byte, error) {
	switch f.card {
	case cardOne:
		return appendOne(b, f.num, f, v)
	case cardList:
		list, ok := v.([]any)
		if !ok {
			return nil, noFit("not a list")
		}
		if len(list) == 0 {
			return b, nil
		}
		if packed(f.kind) {
			var content []byte
			var err error
			for i, e := range list {
				if content, err = appendScalar(content, f, e); err != nil {
					return nil, at(strconv.Itoa(i), err)
				}
			}
			return protowire.AppendBytes(protowire.AppendTag(b, f.num, protowire.BytesType), content), nil
		}
		var err error
		for i, e := range list {
			if e == nil && f.kind != kindValue {
				return nil, at(strconv.Itoa(i), noFit("null in a list"))
			}
			if b, err = appendOne(b, f.num, f, e); err != nil {
				return nil, at(strconv.Itoa(i), err)
			}
		}
		return b, nil
	case cardMap, cardUmap:
		obj, ok := v.(map[string]any)
		if !ok {
			return nil, noFit("not an object")
		}
		vf := f
		if f.card == cardUmap {
			vf = &messages[f.ref].fields[1]
		}
		for _, k := range sortedKeys(obj) {
			var entry []byte
			if f.card == cardUmap {
				raw, ok := parseUUID(k)
				if !ok {
					return nil, at(k, noFit("the key is not an identifier"))
				}
				entry = protowire.AppendBytes(protowire.AppendTag(entry, 1, protowire.BytesType), raw)
			} else {
				entry = protowire.AppendString(protowire.AppendTag(entry, 1, protowire.BytesType), k)
			}
			if obj[k] == nil && vf.kind != kindValue {
				return nil, at(k, noFit("null in a map"))
			}
			entry, err := appendOne(entry, 2, vf, obj[k])
			if err != nil {
				return nil, at(k, err)
			}
			b = protowire.AppendBytes(protowire.AppendTag(b, f.num, protowire.BytesType), entry)
		}
		return b, nil
	}
	panic("wirepb: unknown cardinality")
}

func isEmpty(v any) bool {
	switch x := v.(type) {
	case []any:
		return len(x) == 0
	case map[string]any:
		return len(x) == 0
	}
	return false
}

func appendPacked(b []byte, num protowire.Number, nums []protowire.Number) []byte {
	if len(nums) == 0 {
		return b
	}
	var content []byte
	for _, n := range nums {
		content = protowire.AppendVarint(content, uint64(n))
	}
	return protowire.AppendBytes(protowire.AppendTag(b, num, protowire.BytesType), content)
}

// appendMessage writes the content of message mi for the JSON value v.
func appendMessage(b []byte, mi int, v any) ([]byte, error) {
	m := &messages[mi]
	if m.shape == shapeWrap {
		f := &m.fields[0]
		out, err := appendField(b, f, v)
		return out, err
	}
	obj, ok := v.(map[string]any)
	if !ok {
		return nil, noFit("not an object")
	}
	return appendObject(b, m, obj, nil)
}

// appendObject writes an object's fields. skip names members that are not
// this message's to write: the envelope's own, for a frame's body.
func appendObject(b []byte, m *message, obj map[string]any, skip map[string]bool) ([]byte, error) {
	var empties, nulls []protowire.Number
	known := len(skip)
	for i := range m.fields {
		f := &m.fields[i]
		v, present := obj[f.json]
		if f.kind == kindConst {
			// The reader writes it whether or not it was there, so it must be.
			if s, _ := v.(string); !present || s != f.text {
				return nil, at(f.json, noFit("must be "+f.text))
			}
			known++
			continue
		}
		if !present {
			continue
		}
		known++
		if v == nil {
			nulls = append(nulls, f.num)
		} else if f.card != cardOne && isEmpty(v) {
			empties = append(empties, f.num)
		}
	}
	for k := range skip {
		if _, present := obj[k]; !present {
			known--
		}
	}
	var extra map[string]any
	if known != len(obj) {
		if m.extra == 0 {
			for k := range obj {
				if !skip[k] && m.find(k) == nil {
					return nil, at(k, noFit("the schema has no such field"))
				}
			}
		}
		extra = make(map[string]any, len(obj)-known)
		for k, v := range obj {
			if !skip[k] && m.find(k) == nil {
				extra[k] = v
			}
		}
	}
	sort.Slice(empties, func(i, j int) bool { return empties[i] < empties[j] })
	sort.Slice(nulls, func(i, j int) bool { return nulls[i] < nulls[j] })
	b = appendPacked(b, numEmpty, empties)
	b = appendPacked(b, numNull, nulls)
	var err error
	for _, f := range m.ordered {
		if f.json == "" { // the extra map
			if len(extra) > 0 {
				if b, err = appendField(b, f, extra); err != nil {
					return nil, err
				}
			}
			continue
		}
		v, present := obj[f.json]
		if !present || v == nil {
			continue
		}
		if b, err = appendField(b, f, v); err != nil {
			return nil, at(f.json, err)
		}
	}
	return b, nil
}

// find returns the field with a JSON name, or nil. Messages are small; a
// scan is as quick as a map and needs no memory.
func (m *message) find(name string) *field {
	for i := range m.fields {
		if m.fields[i].json == name {
			return &m.fields[i]
		}
	}
	return nil
}

// appendValue writes the content of a Value message.
func appendValue(b []byte, v any) ([]byte, error) {
	switch x := v.(type) {
	case nil:
		return protowire.AppendVarint(protowire.AppendTag(b, 1, protowire.VarintType), 1), nil
	case bool:
		return protowire.AppendVarint(protowire.AppendTag(b, 2, protowire.VarintType), protowire.EncodeBool(x)), nil
	case string:
		return protowire.AppendString(protowire.AppendTag(b, 5, protowire.BytesType), x), nil
	case json.Number, float64, int64, int:
		if i, ok := toInt(x); ok {
			return protowire.AppendVarint(protowire.AppendTag(b, 3, protowire.VarintType), protowire.EncodeZigZag(i)), nil
		}
		f, ok := toFloat(x)
		if !ok {
			return nil, noFit("not a number")
		}
		return protowire.AppendFixed64(protowire.AppendTag(b, 4, protowire.Fixed64Type), math.Float64bits(f)), nil
	case []any:
		var list []byte
		for i, e := range x {
			item, err := appendValue(nil, e)
			if err != nil {
				return nil, at(strconv.Itoa(i), err)
			}
			list = protowire.AppendBytes(protowire.AppendTag(list, 1, protowire.BytesType), item)
		}
		return protowire.AppendBytes(protowire.AppendTag(b, 6, protowire.BytesType), list), nil
	case map[string]any:
		var fields []byte
		for _, k := range sortedKeys(x) {
			item, err := appendValue(nil, x[k])
			if err != nil {
				return nil, at(k, err)
			}
			entry := protowire.AppendString(protowire.AppendTag(nil, 1, protowire.BytesType), k)
			entry = protowire.AppendBytes(protowire.AppendTag(entry, 2, protowire.BytesType), item)
			fields = protowire.AppendBytes(protowire.AppendTag(fields, 1, protowire.BytesType), entry)
		}
		return protowire.AppendBytes(protowire.AppendTag(b, 7, protowire.BytesType), fields), nil
	}
	return nil, noFit(fmt.Sprintf("a %T is not a JSON value", v))
}

// envelopeKeys are the members of a frame the envelope carries itself.
var envelopeKeys = map[string]bool{"type": true, "at": true, "conversation_id": true, "seq": true}

var envelopeFields = [...]field{
	{json: "at", num: envAt, kind: kindTime},
	{json: "conversation_id", num: envConv, kind: kindUuid},
	{json: "seq", num: envSeq, kind: kindInt},
}

// typedFrame writes a frame as an Envelope with a body, or says why not.
func typedFrame(obj map[string]any) ([]byte, error) {
	typ, ok := obj["type"].(string)
	if !ok {
		return nil, noFit("no type")
	}
	fr := frameByType[typ]
	if fr == nil {
		return nil, noFit("the schema has no frame " + typ)
	}
	var nulls []protowire.Number
	for i := range envelopeFields {
		f := &envelopeFields[i]
		if v, present := obj[f.json]; present && v == nil {
			nulls = append(nulls, f.num)
		}
	}
	b := appendPacked(make([]byte, 0, 128), numNull, nulls)
	var err error
	for i := range envelopeFields {
		f := &envelopeFields[i]
		if v := obj[f.json]; v != nil {
			if b, err = appendOne(b, f.num, f, v); err != nil {
				return nil, at(f.json, err)
			}
		}
	}
	body, err := appendObject(nil, &messages[fr.msg], obj, envelopeKeys)
	if err != nil {
		return nil, at(typ, err)
	}
	return protowire.AppendBytes(protowire.AppendTag(b, fr.num, protowire.BytesType), body), nil
}

// jsonFrame writes a frame the schema cannot hold: its type, for a reader
// that wants to know what it is skipping, and the frame whole as JSON.
func jsonFrame(obj map[string]any, text []byte) []byte {
	var b []byte
	if typ, ok := obj["type"].(string); ok {
		b = protowire.AppendString(protowire.AppendTag(b, envType, protowire.BytesType), typ)
	}
	return protowire.AppendBytes(protowire.AppendTag(b, envJSON, protowire.BytesType), text)
}
