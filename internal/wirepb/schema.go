// Package wirepb turns the contract's JSON into Protocol Buffers and back
// (ruling R65, docs/WIRE-PROTOBUF.md): every frame of the live connection
// and every request, response and error body of the HTTP API.
//
// The server's model stays what it is. An event is a model.Event and a body
// is the JSON a handler wrote; this package walks that JSON beside a table
// of the schema (schema_gen.go, written by tools/openapi-gen from the same
// sources as docs/openapi.yaml) and writes protobuf directly, with no
// generated message types and no reflection. Decoding walks the same table
// the other way and gives the JSON back.
//
// Encoding is exact or it does not happen. Whatever the table cannot hold
// exactly (a field or an enum value newer than the schema, a time not in
// the contract's form) makes the encoder refuse; a frame then travels
// whole as JSON inside its envelope, and an HTTP body is sent as JSON. So a
// reader always gets what the JSON said, and a schema that has fallen
// behind the server costs bytes, never correctness.
package wirepb

import (
	"sort"

	"google.golang.org/protobuf/encoding/protowire"
)

//go:generate ../../tools/protogen.sh

// MediaType is what a client names in Accept and Content-Type to speak
// protobuf to the HTTP API.
const MediaType = "application/x-protobuf"

// Subprotocol is what a client asks for, beside "churr.v1", to get the
// live connection's frames as binary Envelope messages.
const Subprotocol = "churr.v1+proto"

type kind uint8

const (
	kindString kind = iota
	kindInt
	kindDouble
	kindBool
	kindUuid // 16 bytes on the wire, hyphenated hex in JSON
	kindTime // int64 microseconds on the wire, RFC 3339 in JSON
	kindBytes
	kindEnum
	kindMsg
	kindValue    // any JSON value
	kindEnvelope // an event inside a body: the live connection's Envelope
	kindConst    // always the same text; not on the wire
)

type card uint8

const (
	cardOne  card = iota
	cardList      // a JSON array
	cardMap       // a JSON object used as a map, text keys
	cardUmap      // the same with identifiers as keys: repeated entry messages
)

type shape uint8

const (
	shapeObject shape = iota
	shapeWrap         // stands for a bare list or map: one field, number 1
	shapeEntry        // one entry of a cardUmap: key 1, value 2
)

// The numbers every object message keeps for itself, and the envelope's own.
const (
	numEmpty = 1 // list and map fields that are present and empty
	numNull  = 2 // fields that are null

	envType = 3
	envAt   = 4
	envConv = 5
	envSeq  = 6
	envJSON = 7
)

type field struct {
	json string
	num  protowire.Number
	kind kind
	card card
	ref  int    // the message or enum, by index
	text string // kindConst: the text
}

type message struct {
	name   string
	shape  shape
	extra  protowire.Number // where members the schema does not name go; 0 if they are refused
	fields []field

	ordered []*field // by number, with the extra map among them
	byNum   map[protowire.Number]*field
	consts  int
}

type frame struct {
	typ string
	num protowire.Number
	msg int
}

type operation struct {
	req int
	res map[int]int
}

var (
	enumIndex   []map[string]int
	frameByType = map[string]*frame{}
	frameByNum  = map[protowire.Number]*frame{}
)

func init() {
	for i := range messages {
		m := &messages[i]
		m.byNum = make(map[protowire.Number]*field, len(m.fields)+1)
		for j := range m.fields {
			f := &m.fields[j]
			if f.kind == kindConst {
				m.consts++
				continue
			}
			m.ordered = append(m.ordered, f)
			m.byNum[f.num] = f
		}
		if m.extra != 0 {
			f := &field{json: "", num: m.extra, kind: kindValue, card: cardMap}
			m.ordered = append(m.ordered, f)
			m.byNum[f.num] = f
		}
		sort.Slice(m.ordered, func(a, b int) bool { return m.ordered[a].num < m.ordered[b].num })
	}
	enumIndex = make([]map[string]int, len(enums))
	for i, values := range enums {
		enumIndex[i] = make(map[string]int, len(values))
		for n, v := range values {
			if v != "" {
				enumIndex[i][v] = n
			}
		}
	}
	for i := range frames {
		frameByType[frames[i].typ] = &frames[i]
		frameByNum[frames[i].num] = &frames[i]
	}
}

// FrameTypes lists every frame type the schema has a body for.
func FrameTypes() []string {
	out := make([]string, len(frames))
	for i, f := range frames {
		out[i] = f.typ
	}
	return out
}
