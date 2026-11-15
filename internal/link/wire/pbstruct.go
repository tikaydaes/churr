package wire

import (
	"fmt"
	"math"
	"reflect"

	"google.golang.org/protobuf/encoding/protowire"
)

// This file writes a Go struct as a message and reads one back, matching
// fields by their json names (pb.go).
//
// What is written: a pointer that is not nil, whatever it points at; any
// other field when it is not its zero. So an absent field reads back as the
// zero, and a field whose presence matters is a pointer in the struct.
//
// A struct field the message does not have must be zero, or the value is
// refused: a frame cannot be sent with a field its kind does not carry.

// appendStruct writes the content of message mi for a struct.
func appendStruct(b []byte, mi int, v reflect.Value) ([]byte, error) {
	m := &pbMessages[mi]
	for v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return b, nil
		}
		v = v.Elem()
	}
	if m.wrap {
		return appendGoField(b, &m.fields[0], v)
	}
	if v.Kind() != reflect.Struct {
		return nil, fmt.Errorf("wire: %s is written from a struct, not a %s", m.name, v.Type())
	}
	p := planFor(mi, v.Type())
	for _, o := range p.other {
		if !emptyValue(v.FieldByIndex(o.index)) {
			return nil, fmt.Errorf("wire: %s does not carry %s", m.name, o.name)
		}
	}
	var err error
	for i := range m.fields {
		if p.at[i] == nil {
			continue
		}
		if b, err = appendGoField(b, &m.fields[i], v.FieldByIndex(p.at[i])); err != nil {
			return nil, fmt.Errorf("%s.%w", m.name, err)
		}
	}
	return b, nil
}

// emptyValue reports whether a value is its zero, or a list or map with
// nothing in it.
func emptyValue(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Slice, reflect.Map:
		return v.Len() == 0
	}
	return v.IsZero()
}

// appendGoField writes one struct field of any cardinality.
func appendGoField(b []byte, f *pbField, v reflect.Value) ([]byte, error) {
	present := false
	for v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return b, nil
		}
		v, present = v.Elem(), true
	}
	switch f.card {
	case pbOne:
		if !present && emptyValue(v) {
			return b, nil
		}
		return appendGoOne(b, f.num, f, v, present)
	case pbList:
		if v.Kind() != reflect.Slice {
			return nil, fmt.Errorf("%s: a list is written from a slice", f.json)
		}
		n := v.Len()
		if n == 0 {
			return b, nil
		}
		if packedKind(f.kind) {
			var content []byte
			var err error
			for i := 0; i < n; i++ {
				if content, err = appendGoScalar(content, f, v.Index(i)); err != nil {
					return nil, err
				}
			}
			return protowire.AppendBytes(protowire.AppendTag(b, f.num, protowire.BytesType), content), nil
		}
		var err error
		for i := 0; i < n; i++ {
			if b, err = appendGoOne(b, f.num, f, v.Index(i), true); err != nil {
				return nil, err
			}
		}
		return b, nil
	case pbMap, pbUmap:
		if v.Kind() != reflect.Map || v.Type().Key().Kind() != reflect.String {
			return nil, fmt.Errorf("%s: a map is written from a map with text keys", f.json)
		}
		for _, k := range sortedKeys(v) {
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
			entry, err := appendGoOne(entry, 2, f, v.MapIndex(reflect.ValueOf(k).Convert(v.Type().Key())), true)
			if err != nil {
				return nil, err
			}
			b = protowire.AppendBytes(protowire.AppendTag(b, f.num, protowire.BytesType), entry)
		}
		return b, nil
	}
	return nil, fmt.Errorf("%s: unknown cardinality", f.json)
}

// appendGoScalar writes a value with no tag: the content of a packed list.
func appendGoScalar(b []byte, f *pbField, v reflect.Value) ([]byte, error) {
	switch f.kind {
	case pbInt:
		if v.CanInt() {
			return protowire.AppendVarint(b, uint64(v.Int())), nil
		}
	case pbTime, pbMs:
		if v.CanInt() {
			return protowire.AppendVarint(b, toMicros(v.Int())), nil
		}
	case pbBool:
		if v.Kind() == reflect.Bool {
			return protowire.AppendVarint(b, protowire.EncodeBool(v.Bool())), nil
		}
	case pbDouble:
		if v.CanFloat() {
			return protowire.AppendFixed64(b, math.Float64bits(v.Float())), nil
		}
	}
	return nil, fmt.Errorf("%s: cannot be written from a %s", f.json, v.Type())
}

// appendGoOne writes one value under a number. always is false for a plain
// struct field holding a message, which is left out when it is empty.
func appendGoOne(b []byte, num protowire.Number, f *pbField, v reflect.Value, always bool) ([]byte, error) {
	for v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return nil, fmt.Errorf("%s: nil inside a list or map", f.json)
		}
		v = v.Elem()
	}
	if packedKind(f.kind) {
		t := protowire.VarintType
		if f.kind == pbDouble {
			t = protowire.Fixed64Type
		}
		return appendGoScalar(protowire.AppendTag(b, num, t), f, v)
	}
	switch f.kind {
	case pbStr:
		if v.Kind() == reflect.String {
			return protowire.AppendString(protowire.AppendTag(b, num, protowire.BytesType), v.String()), nil
		}
	case pbUuid:
		if v.Kind() == reflect.String {
			id, ok := parseID(v.String())
			if !ok {
				return nil, fmt.Errorf("%s: %q is not an identifier", f.json, v.String())
			}
			return protowire.AppendBytes(protowire.AppendTag(b, num, protowire.BytesType), id[:]), nil
		}
	case pbBytes:
		if v.Kind() == reflect.Slice && v.Type().Elem().Kind() == reflect.Uint8 {
			return protowire.AppendBytes(protowire.AppendTag(b, num, protowire.BytesType), v.Bytes()), nil
		}
	case pbMsg:
		sub, err := appendStruct(nil, f.ref, v)
		if err != nil {
			return nil, err
		}
		if len(sub) == 0 && !always {
			return b, nil
		}
		return protowire.AppendBytes(protowire.AppendTag(b, num, protowire.BytesType), sub), nil
	}
	return nil, fmt.Errorf("%s: cannot be written from a %s", f.json, v.Type())
}

// readStruct reads the content of message mi into a struct. Fields the
// message has and the struct has not are skipped, and so are numbers the
// message does not know: a reader ignores what it does not know.
func readStruct(mi int, b []byte, v reflect.Value) error {
	m := &pbMessages[mi]
	for v.Kind() == reflect.Pointer {
		if v.IsNil() {
			v.Set(reflect.New(v.Type().Elem()))
		}
		v = v.Elem()
	}
	var p *plan
	if !m.wrap {
		if v.Kind() != reflect.Struct {
			return fmt.Errorf("wire: %s is read into a struct, not a %s", m.name, v.Type())
		}
		p = planFor(mi, v.Type())
	}
	for len(b) > 0 {
		num, t, n := protowire.ConsumeTag(b)
		if n < 0 {
			return malformed(m.name)
		}
		b = b[n:]
		var f *pbField
		var into reflect.Value
		if m.wrap {
			if num == 1 {
				f, into = &m.fields[0], v
			}
		} else {
			for i := range m.fields {
				if m.fields[i].num == num && p.at[i] != nil {
					f, into = &m.fields[i], v.FieldByIndex(p.at[i])
				}
			}
		}
		if f == nil {
			n = protowire.ConsumeFieldValue(num, t, b)
			if n < 0 {
				return malformed(m.name)
			}
			b = b[n:]
			continue
		}
		n, err := readGoField(f, t, b, into)
		if err != nil {
			return err
		}
		b = b[n:]
	}
	return nil
}

// settable returns the value a field's content goes into, making what a
// pointer points at.
func settable(v reflect.Value) reflect.Value {
	for v.Kind() == reflect.Pointer {
		if v.IsNil() {
			v.Set(reflect.New(v.Type().Elem()))
		}
		v = v.Elem()
	}
	return v
}

func readGoField(f *pbField, t protowire.Type, b []byte, v reflect.Value) (int, error) {
	bad := func() (int, error) { return 0, malformed(f.json) }
	switch f.card {
	case pbOne:
		return readGoOne(f, t, b, settable(v))
	case pbList:
		if v.Kind() != reflect.Slice {
			return 0, fmt.Errorf("%s: a list is read into a slice", f.json)
		}
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
				e := reflect.New(v.Type().Elem()).Elem()
				m, err := readGoOne(f, vt, raw, settable(e))
				if err != nil {
					return 0, err
				}
				v.Set(reflect.Append(v, e))
				raw = raw[m:]
			}
			return n, nil
		}
		e := reflect.New(v.Type().Elem()).Elem()
		n, err := readGoOne(f, t, b, settable(e))
		if err != nil {
			return 0, err
		}
		v.Set(reflect.Append(v, e))
		return n, nil
	case pbMap, pbUmap:
		if v.Kind() != reflect.Map || v.Type().Key().Kind() != reflect.String {
			return 0, fmt.Errorf("%s: a map is read into a map with text keys", f.json)
		}
		raw, n := protowire.ConsumeBytes(b)
		if t != protowire.BytesType || n < 0 {
			return bad()
		}
		if v.IsNil() {
			v.Set(reflect.MakeMap(v.Type()))
		}
		var key string
		e := reflect.New(v.Type().Elem()).Elem()
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
				m, err := readGoOne(f, et, raw, settable(e))
				if err != nil {
					return 0, err
				}
				raw = raw[m:]
			default:
				m := protowire.ConsumeFieldValue(num, et, raw)
				if m < 0 {
					return bad()
				}
				raw = raw[m:]
			}
		}
		if e.Kind() == reflect.Map && e.IsNil() {
			// A map inside a map that came with nothing in it is still there.
			e.Set(reflect.MakeMap(e.Type()))
		}
		v.SetMapIndex(reflect.ValueOf(key).Convert(v.Type().Key()), e)
		return n, nil
	}
	return bad()
}

// readGoOne reads one value into v, which is not a pointer.
func readGoOne(f *pbField, t protowire.Type, b []byte, v reflect.Value) (int, error) {
	bad := func() (int, error) { return 0, malformed(f.json) }
	wrong := func() (int, error) { return 0, fmt.Errorf("%s: cannot be read into a %s", f.json, v.Type()) }
	switch f.kind {
	case pbDouble:
		x, n := protowire.ConsumeFixed64(b)
		if t != protowire.Fixed64Type || n < 0 {
			return bad()
		}
		if !v.CanFloat() {
			return wrong()
		}
		v.SetFloat(math.Float64frombits(x))
		return n, nil
	case pbInt, pbTime, pbMs, pbBool:
		x, n := protowire.ConsumeVarint(b)
		if t != protowire.VarintType || n < 0 {
			return bad()
		}
		switch {
		case f.kind == pbBool:
			if v.Kind() != reflect.Bool {
				return wrong()
			}
			v.SetBool(x != 0)
		case !v.CanInt():
			return wrong()
		case f.kind == pbInt:
			v.SetInt(int64(x))
		default:
			ms, ok := fromMicros(x)
			if !ok {
				return bad()
			}
			v.SetInt(ms)
		}
		return n, nil
	}
	raw, n := protowire.ConsumeBytes(b)
	if t != protowire.BytesType || n < 0 {
		return bad()
	}
	switch f.kind {
	case pbStr:
		if v.Kind() != reflect.String {
			return wrong()
		}
		v.SetString(string(raw))
	case pbUuid:
		if len(raw) != 16 {
			return bad()
		}
		if v.Kind() != reflect.String {
			return wrong()
		}
		v.SetString(formatID(raw))
	case pbBytes:
		if v.Kind() != reflect.Slice || v.Type().Elem().Kind() != reflect.Uint8 {
			return wrong()
		}
		// A copy: what is read outlives the buffer it came in.
		v.SetBytes(append(make([]byte, 0, len(raw)), raw...))
	case pbMsg:
		if err := readStruct(f.ref, raw, v); err != nil {
			return 0, err
		}
	default:
		return wrong()
	}
	return n, nil
}
