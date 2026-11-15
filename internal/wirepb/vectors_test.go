package wirepb

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"google.golang.org/protobuf/encoding/protowire"
)

var update = flag.Bool("update", false, "rewrite the vectors under testdata/wirepb")

const vectorDir = "../../testdata/wirepb"

// A vector is one JSON value and the protobuf it is. Frames name their type
// and are an Envelope; bodies name their message.
type vector struct {
	Name    string          `json:"name"`
	Message string          `json:"message,omitempty"`
	JSON    json.RawMessage `json:"json"`
	PB      string          `json:"pb"`
	// Skip is set on an envelope a reader must skip.
	Skip bool `json:"skip,omitempty"`
}

func (v vector) pb(t *testing.T) []byte {
	b, err := base64.StdEncoding.DecodeString(v.PB)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func mustJSON(t *testing.T, v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// canonical is the form two JSON texts are compared in: keys sorted, whole
// numbers as whole numbers, other numbers as the float64 they are.
func canonical(t *testing.T, text []byte) string {
	t.Helper()
	v, err := parseJSON(text)
	if err != nil {
		t.Fatalf("not JSON: %v: %s", err, text)
	}
	var norm func(v any) any
	norm = func(v any) any {
		switch x := v.(type) {
		case json.Number:
			if i, ok := toInt(x); ok {
				return i
			}
			f, _ := toFloat(x)
			if i, ok := toInt(f); ok {
				return i
			}
			return f
		case []any:
			for i := range x {
				x[i] = norm(x[i])
			}
		case map[string]any:
			for k := range x {
				x[k] = norm(x[k])
			}
		}
		return v
	}
	return string(mustJSON(t, norm(v)))
}

// buildFrames makes the frame vectors: the hand-written fixtures first, then
// three made-up frames of every type the schema has.
func buildFrames(t *testing.T) []vector {
	var out []vector
	add := func(name string, text []byte) {
		b, typed, why := EncodeFrameJSON(text)
		if !typed {
			t.Fatalf("%s does not fit the schema: %v", name, why)
		}
		out = append(out, vector{Name: name, JSON: text, PB: base64.StdEncoding.EncodeToString(b)})
	}
	raw, err := os.ReadFile(filepath.Join(vectorDir, "fixtures.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []json.RawMessage
	if err := json.Unmarshal(raw, &fixtures); err != nil {
		t.Fatal(err)
	}
	for i, f := range fixtures {
		var head struct{ Type string }
		json.Unmarshal(f, &head)
		var compact bytes.Buffer
		json.Compact(&compact, f)
		add("fixture "+head.Type+" "+string(rune('a'+i)), compact.Bytes())
	}
	for i := range frames {
		for _, variant := range []string{"full", "sparse", "bare"} {
			add(frames[i].typ+" "+variant, mustJSON(t, synthFrame(&frames[i], variant)))
		}
	}
	return out
}

// buildBodies makes three made-up values of every message.
func buildBodies(t *testing.T) []vector {
	var out []vector
	for mi := range messages {
		if messages[mi].shape != shapeObject {
			continue
		}
		for _, variant := range []string{"full", "sparse", "bare"} {
			s := &synth{variant: variant, n: mi}
			text := mustJSON(t, s.message(mi, 0))
			b, err := encodeBody(mi, text)
			if err != nil {
				t.Fatalf("%s %s: %v", messages[mi].name, variant, err)
			}
			out = append(out, vector{Name: messages[mi].name + " " + variant, Message: messages[mi].name,
				JSON: text, PB: base64.StdEncoding.EncodeToString(b)})
		}
	}
	return out
}

// unknownField is a field no message has, of each wire type, to put where a
// newer peer might have put something.
func unknownField() []byte {
	b := protowire.AppendVarint(protowire.AppendTag(nil, 9001, protowire.VarintType), 7)
	b = protowire.AppendString(protowire.AppendTag(b, 9002, protowire.BytesType), "from the future")
	b = protowire.AppendFixed64(protowire.AppendTag(b, 9003, protowire.Fixed64Type), 1)
	return protowire.AppendFixed32(protowire.AppendTag(b, 9004, protowire.Fixed32Type), 1)
}

// buildUnknown makes the vectors about what a reader has never heard of.
func buildUnknown(t *testing.T) []vector {
	enc := base64.StdEncoding.EncodeToString
	var out []vector

	// A frame type the schema does not have travels as JSON.
	future := []byte(`{"at":"2027-01-01T00:00:00.000Z","novel":{"deep":[1,2.5,null]},"type":"future.happened"}`)
	b, typed, _ := EncodeFrameJSON(future)
	if typed {
		t.Fatal("a frame type the schema does not have was written as if it did")
	}
	out = append(out, vector{Name: "unknown type, carried as JSON", JSON: future, PB: enc(b)})

	// So does a known frame with a field the schema does not have.
	extra := []byte(`{"account_id":"0193a001-0000-7000-8000-000000000001","at":"2026-11-13T09:30:00.000Z","in_call":false,"mood":"new","presence":"away","type":"presence.changed"}`)
	b, typed, _ = EncodeFrameJSON(extra)
	if typed {
		t.Fatal("a field the schema does not have was dropped")
	}
	out = append(out, vector{Name: "unknown field at the sender, carried as JSON", JSON: extra, PB: enc(b)})

	// An envelope from a newer schema: a body number this one does not
	// have, and no JSON. To skip.
	b = protowire.AppendVarint(protowire.AppendTag(nil, envAt, protowire.VarintType), 1)
	b = protowire.AppendBytes(protowire.AppendTag(b, 15000, protowire.BytesType), []byte{0x18, 0x01})
	out = append(out, vector{Name: "unknown body, skipped", JSON: json.RawMessage("null"), PB: enc(b), Skip: true})

	// A known frame with fields from a newer schema in the envelope, the
	// body and a nested message: the reader gives what it knows.
	known := []byte(`{"account_id":"0193a001-0000-7000-8000-000000000001","at":"2026-11-13T09:30:00.000Z","reaction":{"account_ids":[],"count":1,"emoji":"👍","me":true},"message_id":"0193a6f2-7c1e-7b3a-9f0e-2a4d5c6b7e8f","type":"reaction.added"}`)
	v, _ := parseJSON(known)
	obj := v.(map[string]any)
	fr := frameByType["reaction.added"]
	reaction, err := appendMessage(nil, messages[fr.msg].find("reaction").ref, obj["reaction"])
	if err != nil {
		t.Fatal(err)
	}
	reaction = append(reaction, unknownField()...)
	body := unknownField()
	for _, f := range messages[fr.msg].ordered {
		if f.json == "reaction" {
			body = protowire.AppendBytes(protowire.AppendTag(body, f.num, protowire.BytesType), reaction)
		} else if obj[f.json] != nil {
			if body, err = appendField(body, f, obj[f.json]); err != nil {
				t.Fatal(err)
			}
		}
	}
	b = unknownField()
	b, _ = appendOne(b, envAt, &envelopeFields[0], obj["at"])
	b = protowire.AppendBytes(protowire.AppendTag(b, fr.num, protowire.BytesType), body)
	out = append(out, vector{Name: "unknown fields at the reader, ignored", JSON: known, PB: enc(b)})

	// An enum value from a newer schema: the field reads as absent.
	presence := frameByType["presence.changed"]
	body, _ = appendOne(nil, messages[presence.msg].find("account_id").num, messages[presence.msg].find("account_id"), "0193a001-0000-7000-8000-000000000001")
	body = protowire.AppendVarint(protowire.AppendTag(body, messages[presence.msg].find("presence").num, protowire.VarintType), 900)
	b = protowire.AppendBytes(protowire.AppendTag(nil, presence.num, protowire.BytesType), body)
	out = append(out, vector{Name: "unknown enum value, read as absent",
		JSON: json.RawMessage(`{"account_id":"0193a001-0000-7000-8000-000000000001","type":"presence.changed"}`), PB: enc(b)})
	return out
}

func writeVectors(t *testing.T, name string, vs []vector) {
	var buf bytes.Buffer
	for _, v := range vs {
		buf.Write(mustJSON(t, v))
		buf.WriteByte('\n')
	}
	path := filepath.Join(vectorDir, name)
	if *update {
		if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	have, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(have, buf.Bytes()) {
		t.Errorf("%s is not what the schema and the fixtures give now; run tools/protogen.sh (or go test ./internal/wirepb -run TestVectors -update)", path)
	}
}

func readVectors(t *testing.T, name string) []vector {
	f, err := os.Open(filepath.Join(vectorDir, name))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var out []vector
	sc := bufio.NewScanner(f)
	sc.Buffer(nil, 16<<20)
	for sc.Scan() {
		var v vector
		if err := json.Unmarshal(sc.Bytes(), &v); err != nil {
			t.Fatal(err)
		}
		out = append(out, v)
	}
	return out
}

// TestVectors checks that the vectors checked in are the ones this schema
// and codec produce, so the app's tests read what the server writes. With
// -update it writes them.
func TestVectors(t *testing.T) {
	writeVectors(t, "frames.jsonl", buildFrames(t))
	writeVectors(t, "bodies.jsonl", buildBodies(t))
	writeVectors(t, "unknown.jsonl", buildUnknown(t))
}
