package wirepb

import (
	"bytes"
	"compress/flate"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"

	"github.com/tikaydaes/churr/internal/model"
)

// TestFramesRoundTrip: for every frame vector, JSON to protobuf and back
// gives the same JSON; and for every event, the same through model.Event.
func TestFramesRoundTrip(t *testing.T) {
	seen := map[string]bool{}
	for _, v := range readVectors(t, "frames.jsonl") {
		want := canonical(t, v.JSON)
		b, typed, why := EncodeFrameJSON(v.JSON)
		if !typed {
			t.Errorf("%s: carried as JSON: %v", v.Name, why)
			continue
		}
		if !bytes.Equal(b, v.pb(t)) {
			t.Errorf("%s: the bytes differ from the vector", v.Name)
		}
		back, err := DecodeFrameJSON(b)
		if err != nil {
			t.Errorf("%s: %v", v.Name, err)
			continue
		}
		if got := canonical(t, back); got != want {
			t.Errorf("%s:\n got %s\nwant %s", v.Name, got, want)
		}

		// Through the server's own event type, for frames it can hold: an
		// event always has `at`, and keeps numbers inside a payload as
		// float64, as its JSON path does.
		var head struct {
			Type string
			At   *string
		}
		json.Unmarshal(v.JSON, &head)
		seen[head.Type] = true
		if head.At == nil || strings.Contains(string(v.JSON), "9007199254740993") {
			continue
		}
		var ev model.Event
		if err := json.Unmarshal(v.JSON, &ev); err != nil {
			t.Errorf("%s: %v", v.Name, err)
			continue
		}
		direct := canonical(t, mustJSON(t, ev))
		eb, err := EncodeEvent(ev)
		if err != nil {
			t.Errorf("%s: %v", v.Name, err)
			continue
		}
		ev2, err := DecodeEvent(eb)
		if err != nil {
			t.Errorf("%s: %v", v.Name, err)
			continue
		}
		if got := canonical(t, mustJSON(t, ev2)); got != direct {
			t.Errorf("%s through model.Event:\n got %s\nwant %s", v.Name, got, direct)
		}
		if ev2.Type != ev.Type || ev2.At != ev.At || ev2.ConversationID != ev.ConversationID || ev2.Seq != ev.Seq {
			t.Errorf("%s: the event's own fields changed: %+v", v.Name, ev2)
		}
	}
	for _, typ := range FrameTypes() {
		if !seen[typ] {
			t.Errorf("no vector for %s", typ)
		}
	}
}

// TestBodiesRoundTrip: the same for every message of the schema.
func TestBodiesRoundTrip(t *testing.T) {
	index := map[string]int{}
	for i := range messages {
		index[messages[i].name] = i
	}
	seen := map[string]bool{}
	for _, v := range readVectors(t, "bodies.jsonl") {
		mi := index[v.Message]
		seen[v.Message] = true
		b, err := encodeBody(mi, v.JSON)
		if err != nil {
			t.Errorf("%s: %v", v.Name, err)
			continue
		}
		if !bytes.Equal(b, v.pb(t)) {
			t.Errorf("%s: the bytes differ from the vector", v.Name)
		}
		back, err := decodeBody(mi, b, true)
		if err != nil {
			t.Errorf("%s: %v", v.Name, err)
			continue
		}
		if got, want := canonical(t, back), canonical(t, v.JSON); got != want {
			t.Errorf("%s:\n got %s\nwant %s", v.Name, got, want)
		}
	}
	for i := range messages {
		if messages[i].shape == shapeObject && !seen[messages[i].name] {
			t.Errorf("no vector for %s", messages[i].name)
		}
	}
}

// TestUnknown: a reader gets through what it has never heard of.
func TestUnknown(t *testing.T) {
	for _, v := range readVectors(t, "unknown.jsonl") {
		back, err := DecodeFrameJSON(v.pb(t))
		if v.Skip {
			if !errors.Is(err, ErrUnknownFrame) {
				t.Errorf("%s: want ErrUnknownFrame, got %v %s", v.Name, err, back)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: %v", v.Name, err)
			continue
		}
		if got, want := canonical(t, back), canonical(t, v.JSON); got != want {
			t.Errorf("%s:\n got %s\nwant %s", v.Name, got, want)
		}
	}
}

// TestMisfitsTravelAsJSON: what the schema cannot hold exactly is not
// forced into it. Each of these comes back as it went.
func TestMisfitsTravelAsJSON(t *testing.T) {
	for name, text := range map[string]string{
		"a field the schema lacks":    `{"type":"typing.started","at":"2026-11-13T09:30:00.000Z","account_id":"0193a001-0000-7000-8000-000000000001","speed":3}`,
		"an enum value it lacks":      `{"type":"presence.changed","at":"2026-11-13T09:30:00.000Z","account_id":"0193a001-0000-7000-8000-000000000001","presence":"napping"}`,
		"a time without milliseconds": `{"type":"pong","server_time":"2026-11-13T09:30:00Z"}`,
		"an identifier in capitals":   `{"type":"typing.started","account_id":"0193A001-0000-7000-8000-000000000001"}`,
		"a number with a fraction":    `{"type":"read_position.moved","read_seq":1.5}`,
		"a null inside a list":        `{"type":"ready","new_conversation_ids":[null]}`,
		"padded base64":               `{"type":"sealed.epoch_started","epoch":{"record":"AAA="}}`,
		"no type at all":              `{"hello":"there"}`,
	} {
		b, typed, why := EncodeFrameJSON([]byte(text))
		if typed || why == nil {
			t.Errorf("%s: written as if it fitted", name)
		}
		back, err := DecodeFrameJSON(b)
		if err != nil || canonical(t, back) != canonical(t, []byte(text)) {
			t.Errorf("%s: came back as %s (%v)", name, back, err)
		}
	}
	if _, _, err := EncodeFrameJSON([]byte(`[1]`)); err == nil {
		t.Error("a frame that is not an object was accepted")
	}
}

// TestMalformed: bytes that are not protobuf are an error, not a panic.
func TestMalformed(t *testing.T) {
	vs := readVectors(t, "frames.jsonl")
	for _, v := range vs[:40] {
		b := v.pb(t)
		for cut := 0; cut < len(b); cut += 1 + len(b)/50 {
			DecodeFrame(b[:cut]) // must not panic
			flipped := append([]byte(nil), b...)
			flipped[cut] ^= 0xff
			DecodeFrame(flipped)
		}
	}
	if _, err := DecodeFrame([]byte{0xff}); !errors.Is(err, ErrMalformed) {
		t.Errorf("want ErrMalformed, got %v", err)
	}
}

func loadDescriptor(t *testing.T) protoreflect.FileDescriptor {
	raw, err := os.ReadFile("testdata/churr.binpb")
	if err != nil {
		t.Fatal(err)
	}
	var set descriptorpb.FileDescriptorSet
	if err := proto.Unmarshal(raw, &set); err != nil {
		t.Fatal(err)
	}
	fd, err := protodesc.NewFile(set.File[0], nil)
	if err != nil {
		t.Fatal(err)
	}
	return fd
}

// hasUnknown reports whether a message, or any message inside it, holds a
// field its descriptor does not have.
func hasUnknown(m protoreflect.Message) bool {
	found := len(m.GetUnknown()) > 0
	m.Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		switch {
		case fd.IsMap():
			if fd.MapValue().Message() != nil {
				v.Map().Range(func(_ protoreflect.MapKey, e protoreflect.Value) bool {
					found = found || hasUnknown(e.Message())
					return !found
				})
			}
		case fd.Message() != nil && fd.IsList():
			for i := 0; i < v.List().Len(); i++ {
				found = found || hasUnknown(v.List().Get(i).Message())
			}
		case fd.Message() != nil:
			found = found || hasUnknown(v.Message())
		}
		return !found
	})
	return found
}

// TestAgainstProtobuf holds the hand-written codec to the real thing. The
// descriptor set is what protoc made of churr.proto. Every vector must
// parse under it with no field it does not know, and the standard library
// must write the same message as the same bytes.
func TestAgainstProtobuf(t *testing.T) {
	fd := loadDescriptor(t)
	check := func(name, msg string, b []byte) {
		md := fd.Messages().ByName(protoreflect.Name(msg))
		if md == nil {
			t.Fatalf("churr.proto has no message %s", msg)
		}
		m := dynamicpb.NewMessage(md)
		if err := proto.Unmarshal(b, m); err != nil {
			t.Errorf("%s: %v", name, err)
			return
		}
		if hasUnknown(m) {
			t.Errorf("%s: holds a field churr.proto does not have", name)
		}
		again, err := proto.MarshalOptions{Deterministic: true}.Marshal(m)
		if err != nil || !bytes.Equal(again, b) {
			t.Errorf("%s: protobuf writes this message differently (%v)", name, err)
		}
	}
	for _, v := range readVectors(t, "frames.jsonl") {
		check(v.Name, "Envelope", v.pb(t))
	}
	for _, v := range readVectors(t, "bodies.jsonl") {
		check(v.Name, v.Message, v.pb(t))
	}
}

// TestTableMatchesProto: schema_gen.go and churr.proto say the same thing,
// field for field. They are written by one run of the generator; this
// catches one being regenerated without the other.
func TestTableMatchesProto(t *testing.T) {
	fd := loadDescriptor(t)
	kinds := map[kind]protoreflect.Kind{kindString: protoreflect.StringKind, kindInt: protoreflect.Int64Kind,
		kindDouble: protoreflect.DoubleKind, kindBool: protoreflect.BoolKind, kindUuid: protoreflect.BytesKind,
		kindTime: protoreflect.Int64Kind, kindBytes: protoreflect.BytesKind, kindEnum: protoreflect.EnumKind,
		kindMsg: protoreflect.MessageKind, kindValue: protoreflect.MessageKind, kindEnvelope: protoreflect.MessageKind}
	count := 0
	for i := range messages {
		m := &messages[i]
		md := fd.Messages().ByName(protoreflect.Name(m.name))
		if md == nil {
			t.Errorf("churr.proto has no message %s", m.name)
			continue
		}
		for _, f := range m.ordered {
			pf := md.Fields().ByNumber(f.num)
			if pf == nil {
				t.Errorf("%s: churr.proto has no field %d (%s)", m.name, f.num, f.json)
				continue
			}
			count++
			want := kinds[f.kind]
			got := pf.Kind()
			if pf.IsMap() {
				got = pf.MapValue().Kind()
			} else if f.card == cardUmap {
				want = protoreflect.MessageKind
			}
			if got != want || (f.card == cardList || f.card == cardUmap) != pf.IsList() || (f.card == cardMap) != pf.IsMap() {
				t.Errorf("%s.%s: the table and churr.proto disagree", m.name, f.json)
			}
			if f.kind == kindEnum {
				ed := pf.Enum()
				if pf.IsMap() {
					ed = pf.MapValue().Enum()
				}
				for n, v := range enums[f.ref] {
					if (v != "") != (n > 0 && ed.Values().ByNumber(protoreflect.EnumNumber(n)) != nil) {
						t.Errorf("%s.%s: enum value %d (%q) differs", m.name, f.json, n, v)
					}
				}
			}
		}
		if m.shape == shapeObject && md.Fields().Len() != len(m.ordered)+1+btoi(md.Fields().ByNumber(numEmpty) != nil) {
			t.Errorf("%s: churr.proto has %d fields, the table %d", m.name, md.Fields().Len(), len(m.ordered))
		}
	}
	env := fd.Messages().ByName("Envelope")
	for _, fr := range frames {
		pf := env.Fields().ByNumber(fr.num)
		if pf == nil || string(pf.Message().Name()) != messages[fr.msg].name || pf.ContainingOneof() == nil {
			t.Errorf("Envelope: body %d (%s) differs", fr.num, fr.typ)
		}
	}
	if count < 1000 {
		t.Fatalf("only %d fields compared", count)
	}
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}

func deflated(b []byte) int {
	var buf bytes.Buffer
	w, _ := flate.NewWriter(&buf, flate.DefaultCompression)
	w.Write(b)
	w.Close()
	return buf.Len()
}

// TestSizes prints what the common frames cost in each form (go test -v).
// "deflated" is each frame compressed by itself, as the live connection
// does it (docs/EVENTS.md section 1).
func TestSizes(t *testing.T) {
	want := map[string]string{
		"fixture message.created a":     "message.created, 200-byte text",
		"fixture reaction.added e":      "reaction.added",
		"fixture presence.changed g":    "presence.changed",
		"fixture presence.batch h":      "presence.batch, 3 accounts",
		"fixture typing.started i":      "typing.started",
		"fixture read_position.moved k": "read_position.moved",
	}
	found := 0
	var out strings.Builder
	fmt.Fprintf(&out, "\n%-34s %6s %6s %9s %9s\n", "frame", "JSON", "proto", "JSON defl", "proto defl")
	for _, v := range readVectors(t, "frames.jsonl") {
		label, ok := want[v.Name]
		if !ok {
			continue
		}
		found++
		b := v.pb(t)
		fmt.Fprintf(&out, "%-34s %6d %6d %9d %9d\n", label, len(v.JSON), len(b), deflated(v.JSON), deflated(b))
		if len(b) >= len(v.JSON) {
			t.Errorf("%s: protobuf (%d) is not smaller than JSON (%d)", label, len(b), len(v.JSON))
		}
	}
	// A batch as large as one gets: 2,000 accounts.
	changes := make([]any, 2000)
	for i := range changes {
		changes[i] = map[string]any{"type": "presence.changed", "at": "2026-11-13T09:30:00.100Z",
			"account_id": fmt.Sprintf("0193a001-0000-7000-8000-%012x", i*7919), "presence": "online", "in_call": false}
	}
	text := mustJSON(t, map[string]any{"type": "presence.batch", "at": "2026-11-13T09:30:01.500Z", "changes": changes})
	b, typed, _ := EncodeFrameJSON(text)
	if !typed {
		t.Fatal("the large batch did not fit")
	}
	fmt.Fprintf(&out, "%-34s %6d %6d %9d %9d\n", "presence.batch, 2,000 accounts", len(text), len(b), deflated(text), deflated(b))
	if found != len(want) {
		t.Errorf("found %d of %d frames to measure", found, len(want))
	}
	t.Log(out.String())
}

func BenchmarkEncodeMessageCreated(b *testing.B) {
	raw, _ := os.ReadFile(vectorDir + "/fixtures.json")
	var fixtures []json.RawMessage
	json.Unmarshal(raw, &fixtures)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		EncodeFrameJSON(fixtures[0])
	}
}

// TestKinds: every kind of the catalogue has a message in churr.proto, and
// its data goes to bytes protobuf itself reads as that message, and back to
// the same JSON.
func TestKinds(t *testing.T) {
	fd := loadDescriptor(t)
	for _, k := range kinds {
		if fd.Messages().ByName(protoreflect.Name(messages[k.msg].name)) == nil || k.level < 1 {
			t.Errorf("kind %s: message %s, level %d", k.name, messages[k.msg].name, k.level)
		}
	}
	roll := `{"dice":"2d6+3, 4d6kh3","rows":[` +
		`{"dice":"2d6+3","terms":[{"sign":1,"count":2,"sides":6,"rolls":[4,1],"value":5},{"sign":1,"value":3}],"total":8},` +
		`{"dice":"4d6kh3","terms":[{"sign":1,"count":4,"sides":6,"keep":"kh","keep_n":3,"rolls":[6,1,5,2],"dropped":[1],"value":13}],"total":13}]}`
	var v any
	dec := json.NewDecoder(strings.NewReader(roll))
	dec.UseNumber()
	if err := dec.Decode(&v); err != nil {
		t.Fatal(err)
	}
	b, err := EncodeKind("dice_roll", v)
	if err != nil {
		t.Fatal(err)
	}
	m := dynamicpb.NewMessage(fd.Messages().ByName("DiceRoll"))
	if err := proto.Unmarshal(b, m); err != nil || hasUnknown(m) {
		t.Fatalf("protobuf does not read the data as DiceRoll: %v", err)
	}
	back, err := DecodeKind("dice_roll", b)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := json.Marshal(back); canonical(t, got) != canonical(t, []byte(roll)) {
		t.Errorf("the data came back as %s", got)
	}
	if lv, ok := KindLevel("dice_roll"); !ok || lv != 1 {
		t.Errorf("dice_roll is level %d (%v)", lv, ok)
	}
	if _, err := EncodeKind("no_such_kind", v); err == nil {
		t.Error("a kind the catalogue does not have was encoded")
	}
}
