package model

import (
	"bytes"
	"encoding/json"
)

// The checks of the catalogue's kinds in Go (docs/DESIGN-MODULES.md
// section 4.2): the owner checks every value in its conversations, every
// receiving server checks it again, and so does each client. Data is never
// trusted, wherever it came from.

// kindCheck is a kind this server can read: the catalogue level its check
// reads in full, and the check of the kind's data, as JSON.
type kindCheck struct {
	level int
	valid func(data json.RawMessage) bool
}

// kindChecks are the kinds this server reads. Their levels are the
// schema's (wirepb holds the two to the same numbers).
var kindChecks = map[string]kindCheck{
	KindDiceRoll:     {level: 1, valid: checkOf[DiceRoll]()},
	KindCoinFlip:     {level: 1, valid: checkOf[CoinFlip]()},
	KindPick:         {level: 1, valid: checkOf[Pick]()},
	KindShuffle:      {level: 1, valid: checkOf[Shuffle]()},
	KindCardDraw:     {level: 1, valid: checkOf[CardDraw]()},
	KindTarotReading: {level: 1, valid: checkOf[TarotReading]()},
}

// checkOf is the check of a kind whose data is T: it reads as T, and holds
// together by T's own Valid.
func checkOf[T interface{ Valid() bool }]() func(json.RawMessage) bool {
	return func(data json.RawMessage) bool {
		var v T
		return json.Unmarshal(data, &v) == nil && v.Valid()
	}
}

// KindLevels gives the catalogue level of each kind this server checks.
func KindLevels() map[string]int {
	out := make(map[string]int, len(kindChecks))
	for k, c := range kindChecks {
		out[k] = c.level
	}
	return out
}

// CheckValue is a value from somewhere else as this server keeps it and
// passes it on. A value of a kind it knows, at a level it reads in full, is
// held to the kind's check; one that fails falls back one step, to its alt
// (checked the same way), or to nothing, which leaves the text. It is not
// an error: the message still arrives, with its text. A value of a kind
// this server does not know, or of a newer level than its check, is
// carried unchanged: a newer reader may draw it.
func CheckValue(v *ValueEnvelope) *ValueEnvelope {
	if v == nil {
		return nil
	}
	if v.Kind == "" {
		return CheckValue(v.Alt)
	}
	c, known := kindChecks[v.Kind]
	if !known || v.Level > c.level {
		return v
	}
	if v.Removed {
		// Nothing was resolved for it: it carries no data.
		if len(v.Data) == 0 {
			return v
		}
		return CheckValue(v.Alt)
	}
	if d := bytes.TrimSpace(v.Data); len(d) == 0 || d[0] != '{' || !c.valid(d) {
		return CheckValue(v.Alt)
	}
	return v
}

// CheckObjects holds the object entities of a message from somewhere else
// to their kinds' checks (CheckValue). An object whose value falls back to
// nothing is left out, and its characters read as text.
func CheckObjects(ents []Entity) []Entity {
	out := ents[:0:0]
	for _, e := range ents {
		if e.Type == EntityObject {
			if e.Object = CheckValue(e.Object); e.Object == nil {
				continue
			}
		}
		out = append(out, e)
	}
	return out
}
