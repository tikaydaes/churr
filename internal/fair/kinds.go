package fair

import (
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/tikaydaes/churr/internal/model"
)

// The built-in chance kinds under derive 1 (docs/FAIR.md section 5a): each a
// fixed composition of the primitives, read from what the value's data says
// was asked, so that a verifier given the data and the draw asks the same
// primitives of the same stream and compares what they give with what the
// data says came up. The modules draw through the same functions
// (modhost.Host.Draw), so the two cannot drift; JavaScript
// (shared/fair/kinds.js) and Dart (lib/core/fair/kinds.dart) repeat them,
// all three held to testdata/fair/kinds.json.

// CoinAsks is a coin flip: yesno(1, 2), yes being heads.
func CoinAsks() []model.FairPrimitive {
	return []model.FairPrimitive{{Op: OpYesNo, Num: 1, Den: 2}}
}

// PickAsks is one of n options: pick(n, 1).
func PickAsks(n int) []model.FairPrimitive {
	return []model.FairPrimitive{{Op: OpPick, N: n, K: 1}}
}

// ShuffleAsks is n options in a new order: shuffle(n).
func ShuffleAsks(n int) []model.FairPrimitive {
	return []model.FairPrimitive{{Op: OpShuffle, N: n}}
}

// CardsAsks is count cards from a deck of size cards, none put back:
// pick(size, count); and, from a deck with orientation, then ints(0, 1,
// count), 1 being reversed, one for each card in the order drawn.
func CardsAsks(size, count int, oriented bool) []model.FairPrimitive {
	out := []model.FairPrimitive{{Op: OpPick, N: size, K: count}}
	if oriented {
		out = append(out, model.FairPrimitive{Op: OpInts, Min: 0, Max: 1, Count: count})
	}
	return out
}

// Cards are the cards CardsAsks's results give: each the deck's card at the
// position picked, and, oriented, whether it came up reversed.
func Cards(deck []string, oriented bool, rs []Result) (cards []string, reversed []bool, err error) {
	if len(rs) < 1 || (oriented && len(rs) < 2) {
		return nil, nil, fmt.Errorf("fair: cards take a pick and, oriented, ints")
	}
	for _, p := range rs[0].Picked {
		if p < 0 || p >= len(deck) {
			return nil, nil, fmt.Errorf("fair: a position outside the deck")
		}
		cards = append(cards, deck[p])
	}
	if oriented {
		if len(rs[1].Values) != len(cards) {
			return nil, nil, fmt.Errorf("fair: one orientation per card")
		}
		reversed = make([]bool, len(cards))
		for i, v := range rs[1].Values {
			reversed[i] = v == 1
		}
	}
	return cards, reversed, nil
}

// KindAsks is the primitives a value of a built-in chance kind asks of its
// stream, in order, read from its data. A kind with no composition, or data
// that does not hold together, is an error.
func KindAsks(kind string, data []byte) ([]model.FairPrimitive, error) {
	switch kind {
	case model.KindCoinFlip:
		var v model.CoinFlip
		if json.Unmarshal(data, &v) != nil || !v.Valid() {
			return nil, fmt.Errorf("fair: a coin flip that does not hold together")
		}
		return CoinAsks(), nil
	case model.KindPick:
		var v model.Pick
		if json.Unmarshal(data, &v) != nil || !v.Valid() {
			return nil, fmt.Errorf("fair: a pick that does not hold together")
		}
		return PickAsks(len(v.Options)), nil
	case model.KindShuffle:
		var v model.Shuffle
		if json.Unmarshal(data, &v) != nil || !v.Valid() {
			return nil, fmt.Errorf("fair: a shuffle that does not hold together")
		}
		return ShuffleAsks(len(v.Options)), nil
	case model.KindCardDraw:
		var v model.CardDraw
		if json.Unmarshal(data, &v) != nil || !v.Valid() {
			return nil, fmt.Errorf("fair: a draw of cards that does not hold together")
		}
		d, _ := model.FindDeck(v.Deck)
		return CardsAsks(len(d.Cards), len(v.Cards), d.Orientation), nil
	case model.KindTarotReading:
		var v model.TarotReading
		if json.Unmarshal(data, &v) != nil || !v.Valid() {
			return nil, fmt.Errorf("fair: a tarot reading that does not hold together")
		}
		d, _ := model.FindDeck("tarot")
		return CardsAsks(len(d.Cards), len(v.Cards), true), nil
	case model.KindDiceRoll:
		var v model.DiceRoll
		if json.Unmarshal(data, &v) != nil || !v.Valid() {
			return nil, fmt.Errorf("fair: a roll that does not hold together")
		}
		rows := make([]model.DiceRow, len(v.Rows))
		for i, r := range v.Rows {
			rows[i] = model.DiceRow{Dice: r.Dice}
			for _, t := range r.Terms {
				nt := model.DiceTerm{Sign: t.Sign, Count: t.Count, Sides: t.Sides, Keep: t.Keep, KeepN: t.KeepN}
				if t.Count == 0 {
					nt.Value = t.Value
				}
				rows[i].Terms = append(rows[i].Terms, nt)
			}
		}
		return []model.FairPrimitive{{Op: OpDice, Rows: rows}}, nil
	}
	return nil, fmt.Errorf("fair: no composition for the kind %q", kind)
}

// PrimitiveOf is a recorded primitive in the shape Draw reads.
func PrimitiveOf(mp model.FairPrimitive) (Primitive, error) {
	var p Primitive
	raw, err := json.Marshal(mp)
	if err != nil {
		return p, err
	}
	return p, json.Unmarshal(raw, &p)
}

// Rederive says whether a value of a built-in chance kind is what derive 1
// gives from object i's stream of a draw whose output is beta: its
// primitives asked in order, and every outcome its data shows compared with
// what they give. An error is a kind with no composition or data that does
// not hold together.
func Rederive(kind string, data []byte, beta []byte, i uint32) (bool, error) {
	asks, err := KindAsks(kind, data)
	if err != nil {
		return false, err
	}
	s := NewStream(beta, i)
	rs := make([]Result, 0, len(asks))
	for _, mp := range asks {
		p, err := PrimitiveOf(mp)
		if err != nil {
			return false, err
		}
		r, err := Draw(s, p)
		if err != nil {
			return false, err
		}
		rs = append(rs, r)
	}
	switch kind {
	case model.KindCoinFlip:
		var v model.CoinFlip
		_ = json.Unmarshal(data, &v)
		return *rs[0].Yes == (v.Outcome == "heads"), nil
	case model.KindPick:
		var v model.Pick
		_ = json.Unmarshal(data, &v)
		return rs[0].Picked[0] == v.Picked, nil
	case model.KindShuffle:
		var v model.Shuffle
		_ = json.Unmarshal(data, &v)
		return reflect.DeepEqual(rs[0].Order, v.Order), nil
	case model.KindCardDraw, model.KindTarotReading:
		var v struct {
			Deck     string   `json:"deck"`
			Cards    []string `json:"cards"`
			Reversed []bool   `json:"reversed"`
		}
		_ = json.Unmarshal(data, &v)
		if kind == model.KindTarotReading {
			v.Deck = "tarot"
		}
		d, _ := model.FindDeck(v.Deck)
		cards, rev, err := Cards(d.Cards, d.Orientation, rs)
		if err != nil {
			return false, err
		}
		return reflect.DeepEqual(cards, v.Cards) && (len(rev) == 0 && len(v.Reversed) == 0 || reflect.DeepEqual(rev, v.Reversed)), nil
	case model.KindDiceRoll:
		var v model.DiceRoll
		_ = json.Unmarshal(data, &v)
		got := rs[0].Rows
		if len(got) != len(v.Rows) {
			return false, nil
		}
		for i, row := range got {
			if row.Total != v.Rows[i].Total || len(row.Terms) != len(v.Rows[i].Terms) {
				return false, nil
			}
			for j, t := range row.Terms {
				w := v.Rows[i].Terms[j]
				if t.Value != w.Value || !sameInts(t.Rolls, w.Rolls) || !sameInts(t.Dropped, w.Dropped) {
					return false, nil
				}
			}
		}
		return true, nil
	}
	return false, fmt.Errorf("fair: no composition for the kind %q", kind)
}

// sameInts is two lists of whole numbers equal, nil and empty alike.
func sameInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
