package fair

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/tikaydaes/churr/internal/model"
)

// The vectors of the built-in chance kinds (docs/FAIR.md section 5a), read by
// all three languages and by the modules' own tests. Written by
//
//	CHURR_WRITE_FAIR_VECTORS=1 go test -run '^TestKindVectors$' ./internal/fair
//
// and frozen with derive 1 after that.
const kindsFile = "../../testdata/fair/kinds.json"

// kindCase is one value of a kind drawn from object i of a draw: what it
// asked (Asks), what derive 1 gave (Results), and the value's data. Invalid
// cases are a value whose data shows an outcome the draw did not give: a
// verifier says it does not re-derive.
type kindCase struct {
	Label   string                `json:"label"`
	Kind    string                `json:"kind"`
	Beta    string                `json:"beta"`
	I       uint32                `json:"i"`
	Asks    []model.FairPrimitive `json:"asks"`
	Results []Result              `json:"results"`
	Data    json.RawMessage       `json:"data"`
}

type kindVectors struct {
	Description string     `json:"description"`
	Derive      int        `json:"derive"`
	Cases       []kindCase `json:"cases"`
	Invalid     []kindCase `json:"invalid"`
}

func betaOf(label string) string {
	b := sha256.Sum256([]byte("churr kinds " + label))
	return hex.EncodeToString(b[:])
}

// drawKind draws a value of a kind the way its module does: asks, results,
// and the data they make.
func drawKind(t *testing.T, label, kind string, i uint32, in map[string]any) kindCase {
	t.Helper()
	beta, _ := hex.DecodeString(betaOf(label))
	var asks []model.FairPrimitive
	var deck model.DeckInfo
	switch kind {
	case model.KindCoinFlip:
		asks = CoinAsks()
	case model.KindPick:
		asks = PickAsks(len(in["options"].([]string)))
	case model.KindShuffle:
		asks = ShuffleAsks(len(in["options"].([]string)))
	case model.KindCardDraw:
		deck, _ = model.FindDeck(in["deck"].(string))
		asks = CardsAsks(len(deck.Cards), in["count"].(int), deck.Orientation)
	case model.KindTarotReading:
		deck, _ = model.FindDeck("tarot")
		sp, _ := model.FindSpread(in["spread"].(string))
		asks = CardsAsks(len(deck.Cards), len(sp.Positions), true)
	}
	s := NewStream(beta, i)
	var rs []Result
	for _, a := range asks {
		p, err := PrimitiveOf(a)
		if err != nil {
			t.Fatal(err)
		}
		r, err := Draw(s, p)
		if err != nil {
			t.Fatal(err)
		}
		rs = append(rs, r)
	}
	var data any
	switch kind {
	case model.KindCoinFlip:
		c := model.CoinFlip{Outcome: "tails"}
		if *rs[0].Yes {
			c.Outcome = "heads"
		}
		data = c
	case model.KindPick:
		data = model.Pick{Options: in["options"].([]string), Picked: rs[0].Picked[0]}
	case model.KindShuffle:
		data = model.Shuffle{Options: in["options"].([]string), Order: rs[0].Order}
	case model.KindCardDraw:
		cards, rev, err := Cards(deck.Cards, deck.Orientation, rs)
		if err != nil {
			t.Fatal(err)
		}
		data = model.CardDraw{Deck: deck.Name, Cards: cards, Reversed: rev}
	case model.KindTarotReading:
		cards, rev, err := Cards(deck.Cards, true, rs)
		if err != nil {
			t.Fatal(err)
		}
		sp, _ := model.FindSpread(in["spread"].(string))
		data = model.TarotReading{Spread: sp.Name, Cards: cards, Reversed: rev}
	}
	raw, _ := json.Marshal(data)
	return kindCase{Label: label, Kind: kind, Beta: betaOf(label), I: i, Asks: asks, Results: rs, Data: raw}
}

func madeKinds(t *testing.T) kindVectors {
	opts := []string{"tea", "coffee", "water", "juice", "milk"}
	v := kindVectors{
		Description: "The built-in chance kinds under derive 1 (docs/FAIR.md section 5a): each case is a value of a kind drawn from object i of a draw whose output is beta (hex): the primitives it asks, in order (asks), what derive 1 gives for them (results), and the value's data. A verifier asks the primitives the data names of stream(beta, i, ·) and compares every outcome the data shows. invalid: data whose outcome the draw did not give, which every verifier says does not re-derive. Written once by internal/fair (TestKindVectors); read by the modules' tests, e2e/fair.mjs and app/test/core/fair/kinds_test.dart.",
		Derive:      Derive,
	}
	for _, c := range []struct {
		label, kind string
		i           uint32
		in          map[string]any
	}{
		{"coin a", model.KindCoinFlip, 0, nil},
		{"coin b", model.KindCoinFlip, 0, nil},
		{"coin c", model.KindCoinFlip, 3, nil},
		{"pick two", model.KindPick, 0, map[string]any{"options": opts[:2]}},
		{"pick five", model.KindPick, 1, map[string]any{"options": opts}},
		{"shuffle three", model.KindShuffle, 0, map[string]any{"options": opts[:3]}},
		{"shuffle five", model.KindShuffle, 2, map[string]any{"options": opts}},
		{"draw standard one", model.KindCardDraw, 0, map[string]any{"deck": "standard", "count": 1}},
		{"draw standard five", model.KindCardDraw, 0, map[string]any{"deck": "standard", "count": 5}},
		{"draw tarot three", model.KindCardDraw, 0, map[string]any{"deck": "tarot", "count": 3}},
		{"draw pinochle twelve", model.KindCardDraw, 1, map[string]any{"deck": "pinochle", "count": 12}},
		{"draw hanafuda all", model.KindCardDraw, 0, map[string]any{"deck": "hanafuda", "count": 48}},
		{"mahjongg thirteen", model.KindCardDraw, 0, map[string]any{"deck": "mahjong", "count": 13}},
		{"tarot three", model.KindTarotReading, 0, map[string]any{"spread": "three"}},
		{"tarot celtic", model.KindTarotReading, 0, map[string]any{"spread": "celtic"}},
		{"tarot relationship", model.KindTarotReading, 4, map[string]any{"spread": "relationship"}},
	} {
		v.Cases = append(v.Cases, drawKind(t, c.label, c.kind, c.i, c.in))
	}
	// Each invalid case is a valid one with one outcome changed.
	bad := func(label string, from int, change func(m map[string]any)) {
		c := v.Cases[from]
		var m map[string]any
		_ = json.Unmarshal(c.Data, &m)
		change(m)
		c.Data, _ = json.Marshal(m)
		c.Label = label
		c.Results = nil
		v.Invalid = append(v.Invalid, c)
	}
	flipped := func(m map[string]any) {
		if m["outcome"] == "heads" {
			m["outcome"] = "tails"
		} else {
			m["outcome"] = "heads"
		}
	}
	bad("coin turned over", 0, flipped)
	bad("another option picked", 4, func(m map[string]any) { m["picked"] = (int(m["picked"].(float64)) + 1) % 5 })
	bad("two places swapped", 6, func(m map[string]any) {
		o := m["order"].([]any)
		o[0], o[1] = o[1], o[0]
	})
	bad("another card", 8, func(m map[string]any) {
		c := m["cards"].([]any)
		for _, x := range model.DeckCards("standard") {
			if !containsAny(c, x) {
				c[0] = x
				break
			}
		}
	})
	bad("a card turned", 9, func(m map[string]any) {
		r := m["reversed"].([]any)
		r[1] = !r[1].(bool)
	})
	bad("tarot cards in another order", 13, func(m map[string]any) {
		c := m["cards"].([]any)
		c[0], c[2] = c[2], c[0]
	})
	bad("the same pick from another object", 4, func(map[string]any) {})
	v.Invalid[len(v.Invalid)-1].I = 0
	return v
}

func containsAny(list []any, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func TestKindVectors(t *testing.T) {
	if os.Getenv("CHURR_WRITE_FAIR_VECTORS") == "1" {
		out, err := json.MarshalIndent(madeKinds(t), "", " ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(kindsFile, append(out, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := os.ReadFile(kindsFile)
	if err != nil {
		t.Fatal(err)
	}
	var file kindVectors
	if err := json.Unmarshal(raw, &file); err != nil {
		t.Fatal(err)
	}
	if file.Derive != Derive || len(file.Cases) < 10 || len(file.Invalid) < 5 {
		t.Fatal("the kinds file is not derive 1's")
	}
	for _, c := range file.Cases {
		beta, _ := hex.DecodeString(c.Beta)
		asks, err := KindAsks(c.Kind, c.Data)
		if err != nil || !reflect.DeepEqual(asks, c.Asks) {
			t.Errorf("%s: asks %+v, %v", c.Label, asks, err)
		}
		ok, err := Rederive(c.Kind, c.Data, beta, c.I)
		if err != nil || !ok {
			t.Errorf("%s: does not re-derive (%v)", c.Label, err)
		}
		// The results are derive 1's for the asks.
		s := NewStream(beta, c.I)
		for j, a := range c.Asks {
			p, _ := PrimitiveOf(a)
			r, err := Draw(s, p)
			got, _ := json.Marshal(r)
			want, _ := json.Marshal(c.Results[j])
			if err != nil || !bytes.Equal(got, want) {
				t.Errorf("%s: result %d is %s, want %s", c.Label, j, got, want)
			}
		}
	}
	for _, c := range file.Invalid {
		beta, _ := hex.DecodeString(c.Beta)
		if ok, _ := Rederive(c.Kind, c.Data, beta, c.I); ok {
			t.Errorf("%s: re-derived", c.Label)
		}
	}
}
