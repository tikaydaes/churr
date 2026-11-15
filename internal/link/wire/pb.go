package wire

import (
	"encoding/hex"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"

	"google.golang.org/protobuf/encoding/protowire"
)

// This file and the two beside it (pbstruct.go, pbtree.go) are the link's
// Protocol Buffers codec (ruling R70, docs/LINK-PROTOCOL.md section 2).
//
// The schema is proto/churr/link/v1/link.proto. It is written by
// tools/openapi-gen from link.py, and so is schema_gen.go: the same schema
// as a table. Nothing here is generated message code. The codec walks the
// table beside a value and writes protobuf directly, in two ways:
//
//   - beside a Go struct, matching fields by their json names (pbstruct.go):
//     frames and everything that is signed as a block. No JSON is made on
//     the way and bytes stay bytes.
//   - beside a parsed JSON value (pbtree.go): the plaintext of direct
//     messages and of envelopes, which the link builds as loose objects of
//     many kinds.
//
// Both write a message's fields in the order of their numbers and a map's
// entries in the order of their keys, so the same value always gives the
// same bytes. Nothing depends on that for a signature (a signed block's
// bytes are made once, by its signer, and carried as they are), but it
// makes the output testable byte for byte.

type pbKind uint8

const (
	pbStr pbKind = iota
	pbInt
	pbDouble
	pbBool
	pbUuid // 16 bytes on the wire; the canonical text in Go and in JSON
	pbTime // int64 microseconds on the wire; model.Time in Go, RFC 3339 in JSON
	pbMs   // int64 microseconds on the wire; whole milliseconds in Go and in JSON
	pbBytes
	pbMsg
	pbClient // a message of the client schema, written by internal/wirepb
	pbValue  // any JSON value: the client schema's Value
)

type pbCard uint8

const (
	pbOne pbCard = iota
	pbList
	pbMap  // text keys
	pbUmap // identifiers as keys: entries with a 16-byte key
)

type pbField struct {
	json   string
	num    protowire.Number
	kind   pbKind
	card   pbCard
	ref    int    // pbMsg: the message, by index
	client string // pbClient: the client schema's message, by name
}

type pbMessage struct {
	name string
	// wrap: the message stands for a bare list or map, which is its one
	// field, number 1.
	wrap bool
	// base: a client message whose fields this one's JSON object also
	// holds. They travel whole in field 1.
	base   string
	fields []pbField
}

// pbMember is one body of a union: a frame, a kind of direct message, a type
// of inner event.
type pbMember struct {
	kind string
	num  protowire.Number
	msg  int
}

var (
	pbByName   = map[string]int{}
	pbFrameBy  = map[string]*pbMember{}
	pbFrameNum = map[protowire.Number]*pbMember{}
)

func init() {
	for i := range pbMessages {
		pbByName[pbMessages[i].name] = i
	}
	for i := range pbFrames {
		pbFrameBy[pbFrames[i].kind] = &pbFrames[i]
		pbFrameNum[pbFrames[i].num] = &pbFrames[i]
	}
}

func (m *pbMessage) byNum(num protowire.Number) *pbField {
	for i := range m.fields {
		if m.fields[i].num == num {
			return &m.fields[i]
		}
	}
	return nil
}

func (m *pbMessage) byJSON(name string) *pbField {
	for i := range m.fields {
		if m.fields[i].json == name {
			return &m.fields[i]
		}
	}
	return nil
}

// errMalformed is what a reader returns for bytes that are not the protobuf
// they should be.
var errMalformed = errors.New("wire: malformed protobuf")

func malformed(what string) error { return fmt.Errorf("%w: %s", errMalformed, what) }

// packedKind reports whether a list of this kind is written as one packed
// field. A reader takes either form.
func packedKind(k pbKind) bool {
	return k == pbInt || k == pbDouble || k == pbBool || k == pbTime || k == pbMs
}

// parseID reads the canonical text of a UUID into its 16 bytes.
func parseID(s string) ([16]byte, bool) {
	var out [16]byte
	if len(s) != 36 || s[8] != '-' || s[13] != '-' || s[18] != '-' || s[23] != '-' {
		return out, false
	}
	var raw [32]byte
	n := 0
	for i := 0; i < 36; i++ {
		c := s[i]
		if c == '-' {
			if i != 8 && i != 13 && i != 18 && i != 23 {
				return out, false
			}
			continue
		}
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return out, false
		}
		if n == 32 {
			return out, false
		}
		raw[n] = c
		n++
	}
	if n != 32 {
		return out, false
	}
	if _, err := hex.Decode(out[:], raw[:]); err != nil {
		return out, false
	}
	return out, true
}

// formatID writes 16 bytes as the canonical text of a UUID.
func formatID(b []byte) string {
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

// toMicros turns whole milliseconds into the microseconds that travel.
func toMicros(ms int64) uint64 { return uint64(ms * 1000) }

// fromMicros turns microseconds back. The link's times are whole
// milliseconds (the envelope's stamp is signed as such), so anything finer
// is not something this side could have been sent honestly.
func fromMicros(us uint64) (int64, bool) {
	v := int64(us)
	if v%1000 != 0 {
		return 0, false
	}
	return v / 1000, true
}

// ------------------------------------------------------------- struct plans

// plan is how one Go struct type lines up with one message: for each field
// of the message, where it is in the struct; and the struct's other fields,
// which the message does not carry.
type plan struct {
	at    [][]int // by position in pbMessage.fields; nil if the struct has no such field
	other []otherField
}

type otherField struct {
	name  string
	index []int
}

type planKey struct {
	mi int
	t  reflect.Type
}

var plans sync.Map // planKey -> *plan

// unionOwn are the json names a union's own message carries for its
// members: a frame's name and numbers are not fields of its body.
var unionOwn = map[string]bool{"frame": true, "id": true, "re": true}

func planFor(mi int, t reflect.Type) *plan {
	key := planKey{mi, t}
	if p, ok := plans.Load(key); ok {
		return p.(*plan)
	}
	m := &pbMessages[mi]
	p := &plan{at: make([][]int, len(m.fields))}
	isFrame := t == reflect.TypeOf(Frame{})
	var walk func(t reflect.Type, prefix []int)
	walk = func(t reflect.Type, prefix []int) {
		for i := 0; i < t.NumField(); i++ {
			sf := t.Field(i)
			index := append(append([]int(nil), prefix...), i)
			tag := sf.Tag.Get("json")
			name, _, _ := strings.Cut(tag, ",")
			if name == "-" {
				continue
			}
			if sf.Anonymous && name == "" && sf.Type.Kind() == reflect.Struct {
				walk(sf.Type, index)
				continue
			}
			if !sf.IsExported() {
				continue
			}
			if name == "" {
				name = sf.Name
			}
			if isFrame && unionOwn[name] {
				continue
			}
			found := false
			for j := range m.fields {
				if m.fields[j].json == name {
					p.at[j], found = index, true
				}
			}
			if !found {
				p.other = append(p.other, otherField{name, index})
			}
		}
	}
	walk(t, nil)
	plans.Store(key, p)
	return p
}

func sortedKeys(v reflect.Value) []string {
	keys := make([]string, 0, v.Len())
	for _, k := range v.MapKeys() {
		keys = append(keys, k.String())
	}
	sort.Strings(keys)
	return keys
}
