package wire

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/coder/websocket"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/secret"
)

// These tests are the proof that the link's own codec writes the protobuf
// that proto/churr/link/v1/link.proto describes: the table it walks is
// compared with the schema as protoc read it, and what it writes is parsed
// by the standard protobuf library.

func linkFiles(t testing.TB) *protoregistry.Files {
	t.Helper()
	raw, err := os.ReadFile("testdata/link.binpb")
	if err != nil {
		t.Fatal(err)
	}
	var set descriptorpb.FileDescriptorSet
	if err := proto.Unmarshal(raw, &set); err != nil {
		t.Fatal(err)
	}
	files, err := protodesc.NewFiles(&set)
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func linkMessage(t testing.TB, files *protoregistry.Files, name string) protoreflect.MessageDescriptor {
	t.Helper()
	d, err := files.FindDescriptorByName(protoreflect.FullName("churr.link.v1." + name))
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return d.(protoreflect.MessageDescriptor)
}

func protoName(jsonName string) string {
	return strings.Trim(regexp.MustCompile(`[^A-Za-z0-9]+`).ReplaceAllString(jsonName, "_"), "_")
}

// The table in schema_gen.go and the schema protoc read say the same.
func TestTableMatchesTheSchema(t *testing.T) {
	files := linkFiles(t)
	wantKind := map[pbKind]protoreflect.Kind{pbStr: protoreflect.StringKind, pbInt: protoreflect.Int64Kind,
		pbDouble: protoreflect.DoubleKind, pbBool: protoreflect.BoolKind, pbUuid: protoreflect.BytesKind,
		pbTime: protoreflect.Int64Kind, pbMs: protoreflect.Int64Kind, pbBytes: protoreflect.BytesKind,
		pbMsg: protoreflect.MessageKind, pbClient: protoreflect.MessageKind, pbValue: protoreflect.MessageKind}
	for i := range pbMessages {
		m := &pbMessages[i]
		md := linkMessage(t, files, m.name)
		want := len(m.fields)
		if m.base != "" {
			want++
			if fd := md.Fields().ByNumber(1); fd == nil || string(fd.Message().FullName()) != "churr.v1."+m.base {
				t.Errorf("%s: field 1 is not churr.v1.%s", m.name, m.base)
			}
		}
		if md.Fields().Len() != want {
			t.Errorf("%s: the schema has %d fields, the table %d", m.name, md.Fields().Len(), want)
		}
		last := protowire.Number(0)
		for j := range m.fields {
			f := &m.fields[j]
			if f.num <= last {
				t.Errorf("%s.%s: the table is not in the order of the numbers", m.name, f.json)
			}
			last = f.num
			fd := md.Fields().ByNumber(f.num)
			if fd == nil || string(fd.Name()) != protoName(f.json) {
				t.Errorf("%s.%s = %d: the schema has %v there", m.name, f.json, f.num, fd)
				continue
			}
			vd := fd
			switch f.card {
			case pbOne:
				if fd.IsList() || fd.IsMap() {
					t.Errorf("%s.%s: not a single field in the schema", m.name, f.json)
				}
			case pbList:
				if !fd.IsList() {
					t.Errorf("%s.%s: not repeated in the schema", m.name, f.json)
				}
			case pbMap:
				if !fd.IsMap() || fd.MapKey().Kind() != protoreflect.StringKind {
					t.Errorf("%s.%s: not a map with text keys in the schema", m.name, f.json)
					continue
				}
				vd = fd.MapValue()
			case pbUmap:
				// Entries with a 16-byte key: the wire form of a map, which
				// protobuf's own maps cannot have with bytes as keys.
				if !fd.IsList() || fd.Message() == nil || fd.Message().Fields().ByNumber(1).Kind() != protoreflect.BytesKind {
					t.Errorf("%s.%s: not a list of entries in the schema", m.name, f.json)
					continue
				}
				vd = fd.Message().Fields().ByNumber(2)
			}
			if vd.Kind() != wantKind[f.kind] {
				t.Errorf("%s.%s: the schema says %v", m.name, f.json, vd.Kind())
			}
			switch f.kind {
			case pbMsg:
				if string(vd.Message().Name()) != pbMessages[f.ref].name {
					t.Errorf("%s.%s: the schema says %s", m.name, f.json, vd.Message().FullName())
				}
			case pbClient:
				if string(vd.Message().FullName()) != "churr.v1."+f.client {
					t.Errorf("%s.%s: the schema says %s", m.name, f.json, vd.Message().FullName())
				}
			case pbValue:
				if string(vd.Message().FullName()) != "churr.v1.Value" {
					t.Errorf("%s.%s: the schema says %s", m.name, f.json, vd.Message().FullName())
				}
			}
		}
	}
	unions := map[string][]pbMember{"Frame": pbFrames, "Direct": pbDirects, "Event": pbEvents}
	for name, members := range unions {
		md := linkMessage(t, files, name)
		for _, mem := range members {
			fd := md.Fields().ByNumber(mem.num)
			if fd == nil || fd.ContainingOneof() == nil || string(fd.Name()) != protoName(mem.kind) ||
				string(fd.Message().Name()) != pbMessages[mem.msg].name {
				t.Errorf("%s: %s = %d is not that body in the schema", name, mem.kind, mem.num)
			}
		}
	}
	// The unions' own fields, whose numbers the codec has as constants.
	own := map[string]map[string]protowire.Number{"Frame": {"id": frameIDNum, "re": frameReNum}, "Direct": {}, "Event": {}}
	for _, f := range directUnion.own {
		own["Direct"][f.json] = f.num
	}
	own["Direct"]["json"] = directUnion.jsonNum
	for _, f := range eventUnion.own {
		own["Event"][f.json] = f.num
	}
	own["Event"]["json"] = eventUnion.jsonNum
	for name, fields := range own {
		md := linkMessage(t, files, name)
		for json, num := range fields {
			if fd := md.Fields().ByNumber(num); fd == nil || string(fd.Name()) != json || fd.ContainingOneof() != nil && !fd.HasOptionalKeyword() {
				t.Errorf("%s.%s is not number %d in the schema", name, json, num)
			}
		}
	}
	for block, mi := range pbBlocks {
		if mi < 0 || mi >= len(pbMessages) {
			t.Errorf("the block %s has no message", block)
		}
	}
}

// Every Go struct that is written as a message has exactly the message's
// fields: nothing the schema lacks, nothing the struct lacks.
func TestStructsMatchTheirMessages(t *testing.T) {
	pairs := map[string]any{
		"Limits": Limits{}, "Envelope": Envelope{}, "Clock": Clock{}, "HomeClock": model.HomeClock{}, "ClusterEntry": ClusterEntry{},
		"SyncStream": SyncStream{}, "RelayItem": RelayItem{}, "RelayRefusal": RelayRefusal{}, "HubClock": HubClock{}, "Handoff": Handoff{},
		"RosterServer": RosterServer{}, "Roster": Roster{}, "Control": Control{}, "Moderator": Moderator{}, "ChannelRecord": ChannelRecord{},
		"ChannelCreate": ChannelCreate{}, "MoveAccept": MoveAccept{}, "MoveCommit": MoveCommit{}, "ModAction": ModAction{},
		"DirectoryEntry": DirectoryEntry{}, "Directory": Directory{}, "ClusterMember": ClusterMember{}, "ClusterGone": ClusterGone{},
		"RehomeAnswer": RehomeAnswer{},
	}
	for name, v := range pairs {
		mi, ok := pbByName[name]
		if !ok {
			t.Errorf("the schema has no %s", name)
			continue
		}
		p := planFor(mi, reflect.TypeOf(v))
		for _, o := range p.other {
			t.Errorf("%s: the struct has %s and the message does not", name, o.name)
		}
		for i, at := range p.at {
			if at == nil {
				t.Errorf("%s: the message has %s and the struct does not", name, pbMessages[mi].fields[i].json)
			}
		}
	}
	// A frame: every field of the struct is carried by some frame, and
	// every field a frame carries is in the struct.
	carried := map[string]bool{}
	for _, mem := range pbFrames {
		p := planFor(mem.msg, reflect.TypeOf(Frame{}))
		for i, at := range p.at {
			if at == nil {
				t.Errorf("the %s frame carries %s, which Frame does not have", mem.kind, pbMessages[mem.msg].fields[i].json)
			}
			carried[pbMessages[mem.msg].fields[i].json] = true
		}
	}
	ft := reflect.TypeOf(Frame{})
	for i := 0; i < ft.NumField(); i++ {
		name, _, _ := strings.Cut(ft.Field(i).Tag.Get("json"), ",")
		if !unionOwn[name] && !carried[name] {
			t.Errorf("no frame carries Frame.%s", ft.Field(i).Name)
		}
	}
}

func id() string { return model.NewID[string]() }

// fullFrames is one frame of every kind with every field it carries set.
func fullFrames(t testing.TB) []Frame {
	k := testKeys(t)
	env := func(n int64) Envelope {
		e, err := SealEnvelope(secret.RandomBytes(32), id(), 3, "a.example", id(), Stamp{At: 1_794_557_400_120, N: n, Offset: -250}, []byte("plain"), k)
		if err != nil {
			t.Fatal(err)
		}
		return e
	}
	delivered := env(7)
	delivered.Cursor, delivered.Prev = 4822, 6
	zero, t1, t2 := int64(0), int64(1_794_557_399_727), int64(1_794_557_399_728)
	clock := Clock{Offset: -412, Latency: 38, Jitter: 3, Uncertainty: 20, Forward: 19, ForwardJitter: 1, Back: 19, BackJitter: 2,
		Asymmetry: 1, Synced: true, Samples: 16, MeasuredAt: 1_794_557_400_120}
	signed, sig := Bytes("the signed bytes"), Bytes(secret.RandomBytes(64))
	lim := DefaultLimits()
	e1 := env(1)
	inner, err := EncodeFrame(Frame{Frame: FLeave, ID: 3, Signed: signed, Sig: sig})
	if err != nil {
		t.Fatal(err)
	}
	have := map[string]int64{"a.example": 211, "c.example": 40}
	joined := map[string]map[string]int64{"b.example": {"a.example": 3}, "c.example": {}}
	frames := []Frame{
		{Frame: FHello, Versions: []int{2, 3}, Version: 3, Kind: KindServer, Name: "b.example", SignKey: secret.RandomBytes(32),
			BoxKey: secret.RandomBytes(32), BoxKeySig: secret.RandomBytes(64), Nonce: secret.RandomBytes(32), Software: "churr/test", Limits: &lim},
		{Frame: FAuth, Sig: sig, InviteProof: secret.RandomBytes(32)},
		{Frame: FReady},
		{Frame: FBye, Reason: "shutdown"},
		{Frame: FPing, ID: 7, T0: &t1, Clock: &clock, Homes: map[string]model.HomeClock{"hub2.example": {Offset: 5, Uncertainty: 9, MeasuredAt: 1_794_557_400_000}}},
		{Frame: FPing, ID: 8, T0: &zero},
		{Frame: FPong, Re: 7, T0: &t1, T1: &t1, T2: &t2},
		{Frame: FSubmit, ID: 31, Envelope: &e1},
		{Frame: FSubmit, ID: 32, Envelopes: []Envelope{env(2), env(3)}},
		{Frame: FDeliver, Envelope: &delivered},
		{Frame: FDeliver, Envelopes: []Envelope{delivered, delivered}},
		{Frame: FAccepted, Re: 31, IDs: []string{id(), id()}, Code: EClockAhead, Message: "ahead", StreamID: id(), OriginEventID: id(),
			Clock: &clock, RetryAfterSeconds: 20},
		{Frame: FAck, StreamID: id(), Cursor: 4822},
		{Frame: FEphemeral, Envelope: &e1},
		{Frame: FDirect, ID: 17, From: "a.example", To: "b.example", Body: secret.RandomBytes(100), Sig: sig},
		{Frame: FError, Re: 31, Code: ERateLimited, Message: "Too many envelopes.", RetryAfterSeconds: 20, StreamID: id(), OriginEventID: id(), Clock: &clock},
		{Frame: FCatchUp, ID: 4, StreamID: id(), Have: have},
		{Frame: FCaughtUp, Re: 4, StreamID: id(), Have: have, Floor: map[string]int64{"a.example": 180}},
		{Frame: FRoster, Signed: signed, Sig: sig},
		{Frame: FChannelCreate, ID: 5, Signed: signed, Sig: sig},
		{Frame: FChannel, Signed: signed, Sig: sig, HomeHub: "hub.example", Have: have, JoinedHave: joined, Also: []string{"d.example"},
			State: &Handoff{Creator: "a.example", Inviters: map[string]string{"b.example": "a.example"}, JoinedHave: joined,
				Declined: []string{"e.example"}, MoveID: id(), Epoch: 4}},
		{Frame: FModAction, ID: 6, Signed: signed, Sig: sig},
		{Frame: FJoin, ID: 6, Signed: signed, Sig: sig},
		{Frame: FDirectory, ID: 9},
		{Frame: FDirectory, Re: 9, Signed: signed, Sig: sig},
		{Frame: FChannelCommit, ID: 10, Signed: signed, Sig: sig, Inner: inner},
		{Frame: FCluster, Hub: "hub.example", Cluster: []ClusterEntry{{Signed: signed, Sig: sig, ViaSig: sig, Gone: signed, GoneSig: sig}, {Signed: signed, Sig: sig}}},
		{Frame: FClusterJoin, ID: 11, Cluster: []ClusterEntry{{Signed: signed, Sig: sig}}},
		{Frame: FHubServers, Signed: signed, Sig: sig},
		{Frame: FHubClocks, Clocks: map[string]HubClock{"hub2.example": {Offset: -3, Uncertainty: 4, Age: 900}}, Reach: []string{"hub3.example"}},
		{Frame: FSync, Streams: map[string]SyncStream{id(): {Version: 3, Have: have}, id(): {}}, Done: true},
		{Frame: FRelay, ID: 12, Items: []RelayItem{{Envelope: env(4), Prev: 3, Window: []int64{1_794_557_000_000, 1_794_557_400_000}}, {Envelope: env(5)}}},
		{Frame: FRelayAck, Re: 12, Refused: []RelayRefusal{{StreamID: id(), OriginEventID: id(), Origin: "a.example", Code: EStaleEpoch, Message: "old"}}},
		{Frame: FAsk, Hub: "hub2.example", Server: "a.example", Inner: inner},
		{Frame: FForward, Server: "a.example", Inner: inner},
		{Frame: FRehome, From: "hub.example", To: "hub2.example", Step: "offer", Offer: id(), StreamID: id(), Signed: signed, Sig: sig,
			State: &Handoff{Creator: "a.example"}, Message: "no"},
		{Frame: FStreamCtl, Inner: inner, JoinedHave: joined, Also: []string{"d.example"}},
	}
	for _, name := range []string{FStreamOpen, FServerJoined, FServerLeft, FLeave, FOwnerChanged, FStreamClose} {
		frames = append(frames, Frame{Frame: name, ID: 20, Signed: signed, Sig: sig, Have: have, HomeHub: "hub.example"})
	}
	// A hub's own word to one of its servers that a stream has stopped.
	frames = append(frames, Frame{Frame: FStreamClose, StreamID: id(), Reason: "hub_left", Hub: "hub.example"})
	return frames
}

func testKeys(t testing.TB) *secret.Keys {
	t.Helper()
	k, err := secret.Open(t.TempDir() + "/keys")
	if err != nil {
		t.Fatal(err)
	}
	return k
}

// A frame of every kind survives the wire exactly, and what is written is
// protobuf the standard library reads with nothing left over: every byte
// belongs to a field link.proto declares.
func TestFramesRoundTrip(t *testing.T) {
	md := linkMessage(t, linkFiles(t), "Frame")
	seen := map[string]bool{}
	for _, f := range fullFrames(t) {
		seen[f.Frame] = true
		b, err := EncodeFrame(f)
		if err != nil {
			t.Errorf("%s: %v", f.Frame, err)
			continue
		}
		got, err := DecodeFrame(b)
		if err != nil {
			t.Errorf("%s: %v", f.Frame, err)
			continue
		}
		// An empty map inside a map comes back as no map; nothing reads the
		// difference.
		if want, have := mustJSON(t, f), mustJSON(t, got); !bytes.Equal(want, have) {
			t.Errorf("%s came back changed:\n sent %s\n  got %s", f.Frame, want, have)
		}
		again, err := EncodeFrame(got)
		if err != nil || !bytes.Equal(again, b) {
			t.Errorf("%s: written again, the bytes differ (%v)", f.Frame, err)
		}
		msg := dynamicpb.NewMessage(md)
		if err := (proto.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(b, msg); err != nil {
			t.Errorf("%s: the protobuf library cannot read it: %v", f.Frame, err)
			continue
		}
		if unknown := findUnknown(msg.ProtoReflect()); unknown != "" {
			t.Errorf("%s: bytes that belong to no field of the schema, in %s", f.Frame, unknown)
		}
		// And the library, writing the same message deterministically,
		// writes the same bytes: the codec's order is the canonical one.
		lib, err := proto.MarshalOptions{Deterministic: true}.Marshal(msg)
		if err != nil || !bytes.Equal(lib, b) {
			t.Errorf("%s: the protobuf library writes other bytes for it (%v)", f.Frame, err)
		}
	}
	for _, mem := range pbFrames {
		if !seen[mem.kind] {
			t.Errorf("no frame of kind %s was tried", mem.kind)
		}
	}
}

func mustJSON(t testing.TB, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	// {"x":{}} and no x read the same.
	return regexp.MustCompile(`,?"[a-z._]+":\{\}`).ReplaceAll(b, nil)
}

func findUnknown(m protoreflect.Message) string {
	if len(m.GetUnknown()) > 0 {
		return string(m.Descriptor().FullName())
	}
	found := ""
	m.Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		switch {
		case fd.IsMap():
			if fd.MapValue().Message() != nil {
				v.Map().Range(func(_ protoreflect.MapKey, e protoreflect.Value) bool {
					found = findUnknown(e.Message())
					return found == ""
				})
			}
		case fd.IsList():
			if fd.Message() != nil {
				for i := 0; i < v.List().Len() && found == ""; i++ {
					found = findUnknown(v.List().Get(i).Message())
				}
			}
		case fd.Message() != nil:
			found = findUnknown(v.Message())
		}
		return found == ""
	})
	return found
}

// A frame cannot be sent with a field its kind does not carry, or around
// an identifier that is not one.
func TestFramesAreStrict(t *testing.T) {
	for name, f := range map[string]Frame{
		"an unknown kind":             {Frame: "nonsense"},
		"no kind":                     {},
		"an ack with a reason":        {Frame: FAck, StreamID: id(), Reason: "x"},
		"a bye with an envelope":      {Frame: FBye, Envelope: &Envelope{}},
		"a stream that is not a UUID": {Frame: FAck, StreamID: "x"},
		"an upper-case UUID":          {Frame: FAck, StreamID: strings.ToUpper(id())},
	} {
		if _, err := EncodeFrame(f); err == nil {
			t.Errorf("%s was written", name)
		}
	}
	// A reader skips what it does not know: a field of a newer version
	// inside a body, and beside it.
	b, _ := EncodeFrame(Frame{Frame: FAck, ID: 4, StreamID: id(), Cursor: 9})
	body := protowire.AppendString(protowire.AppendTag(nil, 900, protowire.BytesType), "new")
	mem := pbFrameBy[FAck]
	newer := protowire.AppendVarint(protowire.AppendTag(nil, 901, protowire.VarintType), 5)
	_, _, n := protowire.ConsumeTag(b[2:])
	old, _ := protowire.ConsumeBytes(b[2+n:])
	newer = append(newer, b[:2]...)
	newer = protowire.AppendBytes(protowire.AppendTag(newer, mem.num, protowire.BytesType), append(append([]byte(nil), old...), body...))
	f, err := DecodeFrame(newer)
	if err != nil || f.Frame != FAck || f.ID != 4 || f.Cursor != 9 {
		t.Fatalf("a frame with fields from the future: %+v, %v", f, err)
	}
	// A body this side has never heard of is a frame it does not know.
	unknown := protowire.AppendBytes(protowire.AppendTag(nil, 4000, protowire.BytesType), []byte{1, 2})
	if _, err := DecodeFrame(unknown); !errors.Is(err, ErrUnknownFrame) {
		t.Errorf("a frame with an unknown body: %v", err)
	}
	// Garbage, an identifier of the wrong length, and a time finer than
	// the link's milliseconds are malformed.
	stream := protowire.AppendBytes(protowire.AppendTag(nil, pbMessages[mem.msg].byJSON("stream_id").num, protowire.BytesType), []byte("short"))
	short := protowire.AppendBytes(protowire.AppendTag(nil, mem.num, protowire.BytesType), stream)
	t0 := protowire.AppendVarint(protowire.AppendTag(nil, pbMessages[pbFrameBy[FPing].msg].byJSON("t0").num, protowire.VarintType), 1_794_557_400_120_001)
	fine := protowire.AppendBytes(protowire.AppendTag(nil, pbFrameBy[FPing].num, protowire.BytesType), t0)
	for name, raw := range map[string][]byte{"garbage": {0xff, 0xff, 0xff}, "a short identifier": short, "a time in microseconds": fine} {
		if _, err := DecodeFrame(raw); err == nil {
			t.Errorf("%s was read as a frame", name)
		}
	}
}

// The rule of section 2, on what is signed: a signed block's bytes are made
// once and carried as they are. A carrier that reads a frame and writes it
// again passes the very same bytes on; the same content written any other
// way does not verify, and neither does a block with one bit changed.
func TestSignedBytesAreCarriedNotRewritten(t *testing.T) {
	k := testKeys(t)
	rec := ChannelRecord{StreamID: id(), Hub: "hub.example", HubKey: Bytes(k.SignPublic()), HomeHub: "hub.example", Version: 4,
		Name: "general", Description: "", Listing: ListingOpen, Servers: []string{"a.example", "b.example"},
		Moderators: []Moderator{{AccountID: id(), Server: "a.example", Username: "sam"}}, Invited: []string{}, Requests: nil,
		Epoch: 2, KeyMaker: "a.example", Makers: map[string]string{"2": "a.example", "1": "b.example"}, At: model.Now()}
	f, err := SignBlock(FChannel, rec, k)
	if err != nil {
		t.Fatal(err)
	}
	var got ChannelRecord
	if err := VerifyBlock(f, k.SignPublic(), &got); err != nil || got.Name != "general" || got.Makers["1"] != "b.example" || got.Version != 4 {
		t.Fatalf("a fresh block: %v %+v", err, got)
	}
	// Through three carriers, each reading the frame and writing it again,
	// and once inside another frame.
	relayed := f
	for hop := 0; hop < 3; hop++ {
		b, err := EncodeFrame(relayed)
		if err != nil {
			t.Fatal(err)
		}
		if hop == 1 {
			out, _ := EncodeFrame(Frame{Frame: FForward, Server: "a.example", Inner: b})
			back, err := DecodeFrame(out)
			if err != nil {
				t.Fatal(err)
			}
			b = back.Inner
		}
		if relayed, err = DecodeFrame(b); err != nil {
			t.Fatal(err)
		}
		relayed.HomeHub, relayed.Have = "hub.example", map[string]int64{"a.example": int64(hop)} // what a carrier adds beside it
	}
	if !bytes.Equal(relayed.Signed, f.Signed) || !bytes.Equal(relayed.Sig, f.Sig) {
		t.Fatal("the signed bytes changed on the way")
	}
	if err := VerifyBlock(relayed, k.SignPublic(), &got); err != nil {
		t.Fatalf("a relayed block: %v", err)
	}
	// One bit, anywhere in the bytes or the signature.
	for i := range f.Signed {
		g := f
		g.Signed = append(Bytes(nil), f.Signed...)
		g.Signed[i] ^= 1
		if VerifyBlock(g, k.SignPublic(), &got) == nil {
			t.Fatalf("the block verified with byte %d of the signed bytes changed", i)
		}
	}
	for i := range f.Sig {
		g := f
		g.Sig = append(Bytes(nil), f.Sig...)
		g.Sig[i] ^= 1
		if VerifyBlock(g, k.SignPublic(), &got) == nil {
			t.Fatalf("the block verified with byte %d of the signature changed", i)
		}
	}
	// The same record written as other bytes: its fields in another order,
	// which every protobuf reader takes as the same message. It reads, and
	// it does not verify: the signature is over bytes, not over meaning.
	var fields [][]byte
	for rest := []byte(f.Signed); len(rest) > 0; {
		num, typ, n := protowire.ConsumeTag(rest)
		m := protowire.ConsumeFieldValue(num, typ, rest[n:])
		fields = append(fields, rest[:n+m])
		rest = rest[n+m:]
	}
	var reordered Bytes
	for i := len(fields) - 1; i >= 0; i-- {
		reordered = append(reordered, fields[i]...)
	}
	var same ChannelRecord
	if err := unmarshalBlock(FChannel, reordered, &same); err != nil || same.Name != rec.Name || same.StreamID != rec.StreamID || same.Version != rec.Version {
		t.Fatalf("the reordered bytes do not read as the same record: %v", err)
	}
	g := f
	g.Signed = reordered
	if AsError(VerifyBlock(g, k.SignPublic(), &got)).Code != EBadSignature {
		t.Fatal("the same record in other bytes verified")
	}
	// And the signer's own bytes are what the schema's canonical form is:
	// the standard library, writing the message deterministically, writes
	// the same.
	msg := dynamicpb.NewMessage(linkMessage(t, linkFiles(t), "ChannelRecord"))
	if err := proto.Unmarshal(f.Signed, msg); err != nil {
		t.Fatal(err)
	}
	if lib, err := (proto.MarshalOptions{Deterministic: true}).Marshal(msg); err != nil || !bytes.Equal(lib, f.Signed) {
		t.Errorf("the protobuf library writes the record as other bytes (%v)", err)
	}
	// A block under another name does not verify, whatever it holds.
	g = f
	g.Frame = FChannelCreate
	if VerifyBlock(g, k.SignPublic(), &ChannelCreate{}) == nil {
		t.Error("a block verified under another name")
	}
}

// An envelope's signature is over a byte layout built from its fields, not
// over the message that carries it. Whatever a carrier does to the bytes of
// a frame, either the envelope it reads is the one that was signed, or it
// does not verify.
func TestEnvelopeSignatureSurvivesNothingButTheTruth(t *testing.T) {
	k := testKeys(t)
	key := secret.RandomBytes(32)
	env, err := SealEnvelope(key, id(), 3, "a.example", id(), Stamp{At: 1_794_557_400_120, N: 12, Offset: -250}, []byte("the plaintext of an event"), k)
	if err != nil {
		t.Fatal(err)
	}
	b, err := EncodeFrame(Frame{Frame: FSubmit, ID: 9, Envelope: &env})
	if err != nil {
		t.Fatal(err)
	}
	f, err := DecodeFrame(b)
	if err != nil || !f.Envelope.Verify(k.SignPublic()) {
		t.Fatalf("an envelope off the wire does not verify: %v", err)
	}
	if plain, err := f.Envelope.Open(key); err != nil || string(plain) != "the plaintext of an event" {
		t.Fatalf("an envelope off the wire does not open: %v", err)
	}
	// A hub's way: kept as an Envelope message with its place in the log,
	// and sent on without being read.
	delivered := *f.Envelope
	delivered.Cursor, delivered.Prev = 77, 11
	kept, err := MarshalEnvelope(delivered)
	if err != nil {
		t.Fatal(err)
	}
	for _, frame := range [][]byte{DeliverFrame([][]byte{kept}), DeliverFrame([][]byte{kept, kept})} {
		d, err := DecodeFrame(frame)
		if err != nil || d.Frame != FDeliver {
			t.Fatal(err)
		}
		list := d.Envelopes
		if d.Envelope != nil {
			list = append(list, *d.Envelope)
		}
		for _, e := range list {
			if !e.Verify(k.SignPublic()) || e.Cursor != 77 || e.Prev != 11 || !bytes.Equal(e.Ciphertext, env.Ciphertext) {
				t.Fatal("a delivered envelope is not the one that was submitted")
			}
		}
	}
	// Every single bit of the frame, flipped.
	same := func(e Envelope) bool {
		return e.StreamID == env.StreamID && e.KeyEpoch == env.KeyEpoch && e.Origin == env.Origin && e.OriginEventID == env.OriginEventID &&
			e.At == env.At && e.N == env.N && e.Offset == env.Offset && bytes.Equal(e.Ciphertext, env.Ciphertext) && bytes.Equal(e.Sig, env.Sig)
	}
	changed, refused := 0, 0
	for i := range b {
		for bit := 0; bit < 8; bit++ {
			m := append([]byte(nil), b...)
			m[i] ^= 1 << bit
			g, err := DecodeFrame(m)
			if err != nil || g.Envelope == nil {
				refused++
				continue
			}
			if same(*g.Envelope) {
				continue // the bit was outside the envelope's signed part: the frame's id, say
			}
			changed++
			if g.Envelope.Verify(k.SignPublic()) {
				t.Fatalf("bit %d of byte %d changed the envelope and it still verifies", bit, i)
			}
			// A change to the signature alone leaves the seal whole; any
			// other change breaks that too.
			if _, err := g.Envelope.Open(key); err == nil && bytes.Equal(g.Envelope.Sig, env.Sig) {
				t.Fatalf("bit %d of byte %d changed the envelope and it still opens", bit, i)
			}
		}
	}
	if changed < 8*len(env.Ciphertext) || refused == 0 {
		t.Fatalf("the mutation did not reach the envelope: %d changed, %d refused", changed, refused)
	}
}

// The plaintext of a direct message and of an event: an object goes in, the
// same object comes out, and in between it is protobuf with no base64 and
// no JSON in it.
func TestDirectAndEventPlaintext(t *testing.T) {
	var fell []string
	OnUntyped(func(union, kind string, why error) { fell = append(fell, union+" "+kind+": "+why.Error()) })
	defer OnUntyped(nil)
	files := linkFiles(t)
	data := make([]byte, 4096)
	rand.Read(data)
	stream, msg, acct := id(), id(), id()
	cases := []struct {
		union string
		in    map[string]any
	}{
		{"Direct", map[string]any{"kind": "blob_chunk", "msg_id": msg, "at": model.Time(1_794_557_400_120), "re": id(),
			"stream_id": stream, "blob_id": id(), "index": 3, "last": true, "data": Bytes(data)}},
		{"Direct", map[string]any{"kind": "key", "msg_id": msg, "at": model.Time(1_794_557_400_120), "re": nil,
			"stream_id": stream, "epoch": 2, "key": Bytes(data[:32]), "version": 0, "history_days": 30}},
		{"Direct", map[string]any{"kind": "history_request", "msg_id": msg, "at": model.Time(1_794_557_400_120),
			"stream_id": stream, "after_at": int64(1_794_557_400_120), "through_at": 0}},
		{"Direct", map[string]any{"kind": "call_signal", "msg_id": msg, "at": model.Time(1_794_557_400_120), "stream_id": stream,
			"call_id": id(), "account_id": acct, "signal": map[string]any{"sdp": "v=0", "n": 3, "list": []any{1, "two", nil, 2.5}}}},
		{"Direct", map[string]any{"kind": "error", "msg_id": msg, "at": model.Time(1_794_557_400_120), "re": id(), "code": "not_found", "message": ""}},
		{"Event", map[string]any{"type": "message.created", "at": model.Time(1_794_557_400_120),
			"message": map[string]any{"id": msg, "author_id": acct, "text": "hello @sam", "created_at": model.Time(1_794_557_400_000),
				"entities": []any{map[string]any{"type": "account", "at": 6, "len": 4, "account_id": acct},
					map[string]any{"type": "conversation", "at": 0, "len": 1, "stream_id": stream}},
				"files":  []any{map[string]any{"id": id(), "name": "a.png", "size": 482113, "media_type": "image/png", "kind": "image", "sha256": Bytes(data[:32])}},
				"system": map[string]any{"kind": "joined", "count": 2}},
			"blobs": []any{map[string]any{"id": id(), "sha256": Bytes(data[:32]), "size": 482113, "media_type": "image/png", "purpose": "attachment"}}}},
		{"Event", map[string]any{"type": "conversation.updated", "at": model.Time(1_794_557_400_120), "name": "general", "topic": ""}},
		{"Event", map[string]any{"type": "reaction.added", "at": model.Time(1_794_557_400_120), "message_id": msg, "account_id": acct,
			"emoji": map[string]any{"id": id(), "name": "party", "server": "a.example"}}},
		{"Event", map[string]any{"type": "form.tallied", "at": model.Time(1_794_557_400_120), "message_id": msg, "form_id": id(), "answer_count": 3,
			"tally": map[string]any{"q1": map[string]any{"yes": 2, "no": 1}}, "voters": map[string]any{"q1": map[string]any{"yes": []any{acct}}}}},
		{"Event", map[string]any{"type": "stream.closed", "at": model.Time(1_794_557_400_120)}},
	}
	for _, c := range cases {
		text, _ := json.Marshal(c.in)
		enc, dec := EncodeDirect, DecodeDirect
		if c.union == "Event" {
			enc, dec = EncodeEvent, DecodeEvent
		}
		kind := c.in["kind"]
		if kind == nil {
			kind = c.in["type"]
		}
		b := enc(text)
		if len(fell) > 0 {
			t.Errorf("%s %v was carried as JSON: %v", c.union, kind, fell)
			fell = nil
			continue
		}
		for k := range c.in {
			if bytes.Contains(b, []byte(`"`+k+`"`)) {
				t.Errorf("%s %v: there is JSON in the protobuf", c.union, kind)
			}
		}
		out, err := dec(b)
		if err != nil {
			t.Errorf("%s %v: %v", c.union, kind, err)
			continue
		}
		var want, got any
		json.Unmarshal(text, &want)
		json.Unmarshal(out, &got)
		strip(want)
		if !reflect.DeepEqual(want, got) {
			t.Errorf("%s %v came back changed:\n sent %s\n  got %s", c.union, kind, text, out)
		}
		m := dynamicpb.NewMessage(linkMessage(t, files, c.union))
		if err := proto.Unmarshal(b, m); err != nil {
			t.Errorf("%s %v: the protobuf library cannot read it: %v", c.union, kind, err)
		} else if unknown := findUnknown(m.ProtoReflect()); unknown != "" {
			t.Errorf("%s %v: bytes that belong to no field of the schema, in %s", c.union, kind, unknown)
		}
	}
	// A chunk of a file is its bytes and little else.
	chunk, _ := json.Marshal(cases[0].in)
	if n := len(EncodeDirect(chunk)); n > len(data)+100 {
		t.Errorf("a 4096-byte chunk takes %d bytes", n)
	}

	// What the schema cannot hold is carried whole, and the receiver gets
	// exactly what was sent.
	for _, text := range []string{
		`{"kind":"offer","msg_id":"` + msg + `","at":"2026-11-13T09:30:00.120Z","stream_id":"` + stream + `","a_field_from_the_future":1}`,
		`{"kind":"a_kind_from_the_future","msg_id":"` + msg + `","at":"2026-11-13T09:30:00.120Z"}`,
		`{"kind":"accept","msg_id":"not-a-uuid","at":"2026-11-13T09:30:00.120Z","stream_id":"` + stream + `"}`,
	} {
		fell = nil
		b := EncodeDirect([]byte(text))
		out, err := DecodeDirect(b)
		if err != nil || string(out) != text || len(fell) != 1 {
			t.Errorf("%s: carried as %q (%v), told %v", text, out, err, fell)
		}
	}
	// A body from a newer schema, with no JSON beside it, is an error to
	// its reader, who drops the message.
	if _, err := DecodeDirect(protowire.AppendBytes(protowire.AppendTag(nil, 4000, protowire.BytesType), []byte{1})); err == nil {
		t.Error("a direct message of an unknown kind was read")
	}
}

// strip takes out of a JSON value what does not travel: null, and empty
// lists and maps.
func strip(v any) {
	switch x := v.(type) {
	case map[string]any:
		for k, e := range x {
			strip(e)
			switch y := e.(type) {
			case nil:
				delete(x, k)
			case []any:
				if len(y) == 0 {
					delete(x, k)
				}
			case map[string]any:
				if len(y) == 0 && k != "system" && k != "signal" {
					delete(x, k)
				}
			}
		}
	case []any:
		for _, e := range x {
			if e != nil {
				strip(e)
			}
		}
	}
}

// Compression: both sides must offer permessage-deflate, and a frame goes
// deflated exactly when that makes it smaller.
func TestCompression(t *testing.T) {
	a, b := ident(t, "a.example", KindServer), ident(t, "b.example", KindServer)
	got := make(chan *Conn, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := Accept(w, r, a, func(Frame, []byte, []byte) error { return nil })
		if err != nil {
			return
		}
		got <- c
		<-c.Done()
	}))
	defer srv.Close()

	// A dialler that does not offer compression is refused before the
	// upgrade.
	_, resp, err := websocket.Dial(context.Background(), "ws"+strings.TrimPrefix(srv.URL, "http"),
		&websocket.DialOptions{Subprotocols: []string{Subprotocol}, CompressionMode: websocket.CompressionDisabled})
	if err == nil || resp == nil || resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("a dialler without compression: %v, %v", err, resp)
	}
	// A listener that does not agree to it is left at once.
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{Subprotocols: []string{Subprotocol}, CompressionMode: websocket.CompressionDisabled})
		if err == nil {
			ws.Read(r.Context())
			ws.CloseNow()
		}
	}))
	defer plain.Close()
	if _, err := Dial(context.Background(), DialOptions{URL: "ws" + strings.TrimPrefix(plain.URL, "http"), Identity: b}); AsError(err).Code != EVersionUnsupported {
		t.Fatalf("a listener without compression: %v", err)
	}

	// Through a relay that keeps what each side sends, to see each
	// message's compressed bit.
	target := strings.TrimPrefix(srv.URL, "http://")
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	var seen, seenUp bytes.Buffer
	copied := make(chan struct{})
	copiedUp := make(chan struct{})
	go func() {
		in, err := ln.Accept()
		if err != nil {
			return
		}
		out, err := net.Dial("tcp", target)
		if err != nil {
			return
		}
		go func() {
			io.Copy(io.MultiWriter(out, &seenUp), in)
			close(copiedUp)
		}()
		io.Copy(io.MultiWriter(in, &seen), out)
		close(copied)
	}()
	c, err := Dial(context.Background(), DialOptions{URL: "ws://" + ln.Addr().String(), Identity: b})
	if err != nil {
		t.Fatal(err)
	}
	server := <-got
	stream := id()
	// One that does not compress (a sealed body), one that does (the same
	// names over and over), and a small one.
	random := Frame{Frame: FDirect, From: "a.example", To: "b.example", Body: secret.RandomBytes(600), Sig: secret.RandomBytes(64)}
	names := Frame{Frame: FHubClocks, Reach: strings.Split(strings.Repeat("hub.example ", 60), " ")}
	small := Frame{Frame: FAck, StreamID: stream, Cursor: 9}
	sizes := map[int]bool{}
	for _, f := range []Frame{random, names, small} {
		raw, _ := EncodeFrame(f)
		sizes[len(raw)] = DeflatedSize(raw) < len(raw)
		if err := server.Send(f); err != nil {
			t.Fatal(err)
		}
		g, _, err := c.Read(context.Background())
		if err != nil || g.Frame != f.Frame || !bytes.Equal(g.Body, f.Body) || len(g.Reach) != len(f.Reach) || g.Cursor != f.Cursor {
			t.Fatalf("%s did not arrive whole: %v", f.Frame, err)
		}
		// And the other way, where the dialler's frames are masked.
		if err := c.Send(f); err != nil {
			t.Fatal(err)
		}
		g, _, err = server.Read(context.Background())
		if err != nil || g.Frame != f.Frame || !bytes.Equal(g.Body, f.Body) || len(g.Reach) != len(f.Reach) || g.Cursor != f.Cursor {
			t.Fatalf("%s did not arrive whole from the dialler: %v", f.Frame, err)
		}
	}
	if len(sizes) != 3 {
		t.Fatal("the three frames must differ in size for the check below")
	}
	c.Close()
	server.Close()
	<-copied
	<-copiedUp
	// Skip the HTTP request or answer, then read the frames: for each data
	// frame, whether RSV1 (the "this message is deflated" bit) is set, by
	// its payload's size or, for a deflated one, by elimination. Every
	// frame of the dialler's is masked and none of the listener's.
	for who, all := range map[string][]byte{"listener": seen.Bytes(), "dialler": seenUp.Bytes()} {
		rest := all[bytes.Index(all, []byte("\r\n\r\n"))+4:]
		deflated, raw := 0, 0
		for len(rest) >= 2 {
			op, rsv1 := rest[0]&0x0f, rest[0]&0x40 != 0
			n, head := int(rest[1]&0x7f), 2
			switch n {
			case 126:
				n, head = int(rest[2])<<8|int(rest[3]), 4
			case 127:
				t.Fatal("a frame larger than the test sends")
			}
			if masked := rest[1]&0x80 != 0; masked != (who == "dialler") {
				t.Fatalf("a frame of the %s's: masked %v", who, masked)
			} else if masked {
				head += 4
			}
			if op == 2 {
				if shrinks, known := sizes[n]; known && !rsv1 {
					if shrinks {
						t.Errorf("a frame of %d bytes that deflate makes smaller was sent as it is by the %s", n, who)
					}
					raw++
				} else if rsv1 {
					deflated++
				}
			}
			rest = rest[head+n:]
		}
		// Of the three: the random one and the small one as they are,
		// the repeated names deflated. (The handshake's frames are there
		// too.)
		if raw < 2 || deflated < 1 {
			t.Fatalf("%d frames of the %s's went as they are and %d deflated", raw, who, deflated)
		}
	}
	if !sizes[len(mustFrame(t, names))] || sizes[len(mustFrame(t, random))] {
		t.Fatal("the test's frames do not compress as it assumes")
	}
}

func mustFrame(t testing.TB, f Frame) []byte {
	t.Helper()
	b, err := EncodeFrame(f)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
