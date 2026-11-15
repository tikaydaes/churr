package wirepb

import (
	"encoding/json"
	"fmt"
)

// synth makes JSON for any message of the schema, so that every message
// and every frame has vectors without anyone writing them by hand. It is
// deterministic: the same schema gives the same vectors.
//
// Three variants of each: "full" has every field, lists and maps of two;
// "sparse" has every other field, with nulls and empty lists and maps among
// them; "bare" has nothing but what must always be there.
type synth struct {
	n       int // counts values made, so neighbouring fields differ
	variant string
}

var (
	synthStrings = []string{"plain", "", "Zoë 🚀 日本語", "line\nbreak \"quoted\" <tag> & \\ \u0000 end", "x"}
	synthInts    = []int64{0, 1, 42, -7, 9007199254740992, 4821}
	synthDoubles = []any{json.Number("0"), json.Number("1.5"), json.Number("-2.25"), json.Number("3"), json.Number("1e-7"), json.Number("6.02e+23")}
	synthTimes   = []string{"2026-11-13T09:30:00.000Z", "1970-01-01T00:00:00.000Z", "2026-11-13T09:30:00.123Z", "1969-12-31T23:59:59.999Z", "2026-02-28T23:59:59.000123Z"}
	synthBytes   = []string{"AAEC_-7dzLuqmYh3ZlVEMyIRAA", "", "_w", "aGVsbG8"}
	synthValues  = []string{`true`, `7`, `-3`, `1.25`, `"s"`, `[1,"a",null,[],{}]`, `{"k":{"n":null},"e":{},"big":9223372036854775807}`, `9007199254740993`}
)

func (s *synth) next() int { s.n++; return s.n }

func (s *synth) uuid() string { return fmt.Sprintf("0193a6f2-7c1e-7b3a-9f0e-%012x", s.next()) }

func (s *synth) one(f *field, depth int) any {
	i := s.next()
	switch f.kind {
	case kindString:
		return synthStrings[i%len(synthStrings)]
	case kindInt:
		return json.Number(fmt.Sprint(synthInts[i%len(synthInts)]))
	case kindDouble:
		return synthDoubles[i%len(synthDoubles)]
	case kindBool:
		return i%2 == 0
	case kindUuid:
		return s.uuid()
	case kindTime:
		return synthTimes[i%len(synthTimes)]
	case kindBytes:
		return synthBytes[i%len(synthBytes)]
	case kindEnum:
		var values []string
		for _, v := range enums[f.ref] {
			if v != "" {
				values = append(values, v)
			}
		}
		return values[i%len(values)]
	case kindMsg:
		return s.message(f.ref, depth+1)
	case kindValue:
		v, _ := parseJSON([]byte(synthValues[i%len(synthValues)]))
		return v
	case kindEnvelope:
		if i%2 == 0 {
			// An event the schema does not know, inside a body.
			return map[string]any{"type": "future.event", "at": synthTimes[0], "novel": []any{json.Number("1")}}
		}
		return map[string]any{"type": "typing.started", "at": synthTimes[0], "conversation_id": s.uuid(), "account_id": s.uuid()}
	}
	panic("synth: kind")
}

func (s *synth) field(f *field, depth int) any {
	count := 2
	if s.variant == "sparse" {
		count = 0
	}
	switch f.card {
	case cardList:
		list := []any{}
		for i := 0; i < count; i++ {
			list = append(list, s.one(f, depth))
		}
		return list
	case cardMap:
		m := map[string]any{}
		for _, k := range []string{"a", "é key"}[:count] {
			m[k] = s.one(f, depth)
		}
		return m
	case cardUmap:
		m := map[string]any{}
		for i := 0; i < count; i++ {
			m[s.uuid()] = s.one(&messages[f.ref].fields[1], depth)
		}
		return m
	}
	return s.one(f, depth)
}

func (s *synth) message(mi, depth int) any {
	m := &messages[mi]
	if m.shape == shapeWrap {
		return s.field(&m.fields[0], depth)
	}
	out := map[string]any{}
	for i := range m.fields {
		f := &m.fields[i]
		switch {
		case f.kind == kindConst:
			out[f.json] = f.text
		case s.variant == "bare":
		case s.variant == "sparse" && i%2 == 1:
		case s.variant == "sparse" && i%6 == 4:
			out[f.json] = nil
		case depth >= 4 && (f.kind == kindMsg || f.kind == kindEnvelope):
			// Deep enough: the schema's objects nest a long way.
		default:
			out[f.json] = s.field(f, depth)
		}
	}
	if m.extra != 0 && s.variant == "full" {
		out["zz_not_in_schema"] = map[string]any{"a": []any{json.Number("1"), nil}}
	}
	return out
}

// synthFrame makes a frame of a type: the body and the envelope's fields.
func synthFrame(fr *frame, variant string) map[string]any {
	s := &synth{variant: variant, n: int(fr.num)}
	obj := s.message(fr.msg, 0).(map[string]any)
	obj["type"] = fr.typ
	switch variant {
	case "full":
		obj["at"], obj["conversation_id"], obj["seq"] = synthTimes[2], s.uuid(), json.Number("9223372036854775807")
	case "sparse":
		obj["at"], obj["seq"] = synthTimes[0], nil
	}
	return obj
}
