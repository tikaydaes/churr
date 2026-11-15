package model

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

// The shared cases of testdata/commands (ruling R67). The clients read the
// same files.

type diceVectors struct {
	Parse []struct {
		Input string     `json:"input"`
		Dice  string     `json:"dice"`
		Terms []DiceTerm `json:"terms"`
	} `json:"parse"`
	Errors []struct {
		Input string `json:"input"`
		Error string `json:"error"`
	} `json:"errors"`
	Rolls []struct {
		Dice  string `json:"dice"`
		Rolls []int  `json:"rolls"`
		Terms []struct {
			Rolls   []int `json:"rolls"`
			Dropped []int `json:"dropped"`
			Value   int   `json:"value"`
		} `json:"terms"`
		Total int `json:"total"`
	} `json:"rolls"`
	Fallback []struct {
		Chance Chance `json:"chance"`
		Text   string `json:"text"`
	} `json:"fallback"`
	Refusals []struct {
		Input ChanceInput `json:"input"`
		Error string      `json:"error"`
	} `json:"refusals"`
}

func readJSON(t *testing.T, path string, v any) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, v); err != nil {
		t.Fatal(err)
	}
}

func TestDiceVectors(t *testing.T) {
	var v diceVectors
	readJSON(t, "../../testdata/commands/dice.json", &v)
	for _, c := range v.Parse {
		terms, text, err := ParseDice(c.Input)
		if err != nil {
			t.Errorf("%q: %v", c.Input, err)
			continue
		}
		if text != c.Dice || !reflect.DeepEqual(terms, c.Terms) {
			t.Errorf("%q: got %q %+v, want %q %+v", c.Input, text, terms, c.Dice, c.Terms)
		}
	}
	for _, c := range v.Errors {
		_, _, err := ParseDice(c.Input)
		if err == nil || !strings.Contains(err.Error(), c.Error) {
			t.Errorf("%q: got %v, want an error with %q", c.Input, err, c.Error)
		}
	}
	for _, c := range v.Rolls {
		terms, _, err := ParseDice(c.Dice)
		if err != nil {
			t.Fatal(err)
		}
		next := 0
		rolled, total := RollDice(terms, func(sides int) int { r := c.Rolls[next]; next++; return r })
		if total != c.Total || next != len(c.Rolls) {
			t.Errorf("%s %v: total %d, want %d", c.Dice, c.Rolls, total, c.Total)
		}
		for i, w := range c.Terms {
			g := rolled[i]
			if len(g.Rolls) != len(w.Rolls) || (len(w.Rolls) > 0 && !reflect.DeepEqual(g.Rolls, w.Rolls)) ||
				len(g.Dropped) != len(w.Dropped) || (len(w.Dropped) > 0 && !reflect.DeepEqual(g.Dropped, w.Dropped)) || g.Value != w.Value {
				t.Errorf("%s %v term %d: got %+v, want %+v", c.Dice, c.Rolls, i, g, w)
			}
		}
		total2 := total
		if !(Chance{Kind: ChanceRoll, Dice: c.Dice, Terms: rolled, Total: &total2}).Valid() {
			t.Errorf("%s: a rolled result is not valid", c.Dice)
		}
	}
	for _, c := range v.Fallback {
		if got := c.Chance.Fallback(); got != c.Text {
			t.Errorf("fallback: got %q, want %q", got, c.Text)
		}
		if !c.Chance.Valid() {
			t.Errorf("%+v is not valid", c.Chance)
		}
	}
	var dk struct {
		Decks []struct {
			Name        string   `json:"name"`
			Aliases     []string `json:"aliases"`
			Size        int      `json:"size"`
			Orientation bool     `json:"orientation"`
			Cards       []string `json:"cards"`
		} `json:"decks"`
		English map[string]map[string]string `json:"english"`
	}
	readJSON(t, "../../testdata/commands/decks.json", &dk)
	if len(dk.Decks) != len(decks) {
		t.Errorf("%d decks in the file, %d here", len(dk.Decks), len(decks))
	}
	for _, w := range dk.Decks {
		for _, name := range append([]string{w.Name, strings.ToUpper(w.Name)}, w.Aliases...) {
			if got := DeckCards(name); !reflect.DeepEqual(got, w.Cards) || len(got) != w.Size {
				t.Errorf("deck %s: got %v", name, got)
			}
		}
		d, _ := findDeck(w.Name)
		if d.Orientation != w.Orientation {
			t.Errorf("deck %s: orientation", w.Name)
		}
		for code, name := range dk.English[w.Name] {
			if got := cardName(d, code); got != name {
				t.Errorf("%s %s: %q, want %q", w.Name, code, got, name)
			}
		}
	}
	for _, c := range v.Refusals {
		_, err := DrawChance(c.Input, func(n int) int { return 0 })
		if err == nil || !strings.Contains(err.Error(), c.Error) {
			t.Errorf("%+v: got %v, want an error with %q", c.Input, err, c.Error)
		}
	}
}

// TestDrawChance draws with the lowest and highest number every time and
// checks each result is whole: in range, no card twice, a shuffle a
// permutation.
func TestDrawChance(t *testing.T) {
	for _, pick := range []func(n int) int{func(int) int { return 0 }, func(n int) int { return n - 1 }} {
		for _, in := range []ChanceInput{
			{Kind: ChanceRoll, Dice: "100d1000"}, {Kind: ChanceRoll}, {Kind: ChanceFlip},
			{Kind: ChancePick, Options: []string{"a", "b", "c"}}, {Kind: ChanceShuffle, Options: []string{"a", "b", "c", "d"}},
			{Kind: ChanceDraw}, {Kind: ChanceDraw, Count: 78, Deck: "tarot"}, {Kind: ChanceDraw, Count: 5, Deck: "54"},
			{Kind: ChanceDraw, Count: 144, Deck: "mahjong"}, {Kind: ChanceDraw, Count: 108, Deck: "canasta"},
			{Kind: ChanceDraw, Count: 416, Deck: "shoe8"}, {Kind: ChanceTarot}, {Kind: ChanceTarot, Spread: "Celtic-Cross"},
			{Kind: ChanceTarot, Spread: "horseshoe"}, {Kind: ChanceTarot, Spread: "love"},
		} {
			c, err := DrawChance(in, pick)
			if err != nil {
				t.Fatalf("%+v: %v", in, err)
			}
			if !c.Valid() || c.Fallback() == "" {
				t.Errorf("%+v gave %+v", in, c)
			}
		}
	}
	bad := Chance{Kind: ChanceDraw, Deck: "standard", Cards: []string{"spades-ace", "spades-ace"}}
	if bad.Valid() {
		t.Error("a card drawn twice was taken")
	}
	if !(Chance{Kind: ChanceDraw, Deck: "pinochle", Cards: []string{"spades-ace", "spades-ace"}}).Valid() {
		t.Error("pinochle has two of each card")
	}
	if (Chance{Kind: ChanceDraw, Deck: "tarot", Cards: []string{"major-0"}}).Valid() {
		t.Error("a tarot card needs its orientation")
	}
	five := 5
	if (Chance{Kind: ChanceRoll, Dice: "1d6", Terms: []DiceTerm{{Sign: 1, Count: 1, Sides: 6, Rolls: []int{7}, Value: 7}}, Total: &five}).Valid() {
		t.Error("a die above its sides was taken")
	}
}

// TestBuiltinTable holds BuiltinCommands to testdata/commands/commands.json,
// the table the clients hold.
func TestBuiltinTable(t *testing.T) {
	var table struct {
		Commands []struct {
			Name        string `json:"name"`
			Hint        string `json:"hint"`
			Description string `json:"description"`
			Sealed      bool   `json:"sealed"`
			Admin       bool   `json:"admin"`
			Replaceable bool   `json:"replaceable"`
		} `json:"commands"`
		Aliases  map[string]string `json:"aliases"`
		HeldBack []string          `json:"held_back"`
	}
	readJSON(t, "../../testdata/commands/commands.json", &table)
	if len(table.Commands) != len(BuiltinCommands) {
		t.Fatalf("%d in the file, %d here", len(table.Commands), len(BuiltinCommands))
	}
	for i, c := range table.Commands {
		b := BuiltinCommands[i]
		if b.Name != c.Name || b.Hint != c.Hint || b.Description != c.Description || b.Sealed != c.Sealed || b.Admin != c.Admin || b.Replaceable != c.Replaceable || b.Kind != CommandBuiltin {
			t.Errorf("%d: %+v here, %+v in the file", i, b, c)
		}
		// A built-in's name may be taken by the community (ruling R72).
		if !ValidCommandName(b.Name) || ReservedCommandName(b.Name) || !BuiltinCommand(b.Name) {
			t.Errorf("%s is not a valid built-in name that may be taken", b.Name)
		}
	}
	if len(table.Aliases) != len(BuiltinAliases) {
		t.Errorf("aliases: %v in the file, %v here", table.Aliases, BuiltinAliases)
	}
	for a, to := range table.Aliases {
		if BuiltinAliases[a] != to || !BuiltinCommand(a) || ProtectedCommand(a) != ProtectedCommand(to) {
			t.Errorf("alias %s: %s in the file, %s here", a, to, BuiltinAliases[a])
		}
	}
	for _, n := range table.HeldBack {
		if !ReservedCommandName(n) || BuiltinCommand(n) {
			t.Errorf("%s should be held back", n)
		}
	}
}

// TestDecksAndSpreads is ported from the owner's game library (cards_test.go,
// game_cards_test.go, tarot_reading_test.go): the sizes and make-up of the
// decks, no card drawn more often than the deck holds it, the spreads'
// sizes, and every tarot card's meanings.
func TestDecksAndSpreads(t *testing.T) {
	sizes := map[string]int{"standard": 52, "jokers": 54, "jokers3": 55, "piquet": 32, "durak": 36, "euchre": 24, "tarot": 78,
		"spanish": 40, "italian": 40, "german": 32, "austrian": 32, "swiss": 36, "pinochle": 48, "canasta": 108,
		"hanafuda": 48, "mahjong": 144, "shoe2": 104, "shoe4": 208, "shoe6": 312, "shoe8": 416}
	for name, n := range sizes {
		if got := len(DeckCards(name)); got != n {
			t.Errorf("%s has %d cards, want %d", name, got, n)
		}
	}
	if len(sizes) != len(decks) {
		t.Errorf("%d decks, %d sizes known", len(decks), len(sizes))
	}
	count := func(name string) map[string]int {
		m := map[string]int{}
		for _, c := range DeckCards(name) {
			m[c]++
		}
		return m
	}
	if c := count("pinochle"); len(c) != 24 || c["hearts-9"] != 2 {
		t.Errorf("pinochle: %v", c)
	}
	if c := count("canasta"); c["joker-red"] != 2 || c["joker-black"] != 2 || c["spades-ace"] != 2 {
		t.Errorf("canasta: %v", c)
	}
	if c := count("mahjong"); c["dots-1"] != 4 || c["wind-east"] != 4 || c["dragon-white"] != 4 || c["flower-plum"] != 1 || len(c) != 42 {
		t.Errorf("mahjong: %d kinds", len(c))
	}
	if c := count("hanafuda"); len(c) != 48 || c["paulownia-bright"] != 1 {
		t.Errorf("hanafuda: %v", c)
	}
	if c := count("german"); c["acorns-unter"] != 1 || c["acorns-jack"] != 0 {
		t.Errorf("german: %v", c)
	}
	for i := 0; i < 50; i++ {
		c, err := DrawChance(ChanceInput{Kind: ChanceDraw, Count: 30, Deck: "pinochle"}, cryptoForTest)
		if err != nil || !c.Valid() || len(c.Cards) != 30 {
			t.Fatalf("%v %+v", err, c)
		}
		left := count("pinochle")
		for _, x := range c.Cards {
			if left[x]--; left[x] < 0 {
				t.Fatalf("%s drawn more often than the deck holds it: %v", x, c.Cards)
			}
		}
	}
	for name, n := range map[string]int{"three": 3, "celtic": 10, "horseshoe": 7, "relationship": 7} {
		c, err := DrawChance(ChanceInput{Kind: ChanceTarot, Spread: name}, cryptoForTest)
		if err != nil || len(c.Cards) != n || len(c.Reversed) != n || !c.Valid() {
			t.Errorf("%s: %v %+v", name, err, c)
		}
		seen := map[string]bool{}
		for _, x := range c.Cards {
			if seen[x] {
				t.Errorf("%s: %s twice", name, x)
			}
			seen[x] = true
		}
	}
	var tarot struct {
		Meanings map[string]struct {
			Upright  string `json:"upright"`
			Reversed string `json:"reversed"`
		} `json:"meanings"`
	}
	readJSON(t, "../../testdata/commands/tarot.json", &tarot)
	for _, c := range DeckCards("tarot") {
		if m := tarot.Meanings[c]; m.Upright == "" || m.Reversed == "" {
			t.Errorf("%s has no meanings", c)
		}
	}
}

// cryptoForTest stands in for crypto/rand, which model does not import.
var testSeed uint64 = 88172645463325252

func cryptoForTest(n int) int {
	testSeed ^= testSeed << 13
	testSeed ^= testSeed >> 7
	testSeed ^= testSeed << 17
	return int(testSeed % uint64(n))
}

// TestHiddenBuiltins holds the one rule of which built-ins are hidden
// (ruling R72) to testdata/commands/parse.json, hidden_cases, the vectors
// the clients pass too.
func TestHiddenBuiltins(t *testing.T) {
	var v struct {
		HiddenCases []struct {
			Listed []Command `json:"listed"`
			Admin  bool      `json:"admin"`
			Hidden []string  `json:"hidden"`
		} `json:"hidden_cases"`
	}
	readJSON(t, "../../testdata/commands/parse.json", &v)
	if len(v.HiddenCases) == 0 {
		t.Fatal("no cases")
	}
	for i, c := range v.HiddenCases {
		got := HiddenBuiltins(c.Listed, c.Admin)
		want := map[string]bool{}
		for _, n := range c.Hidden {
			want[n] = true
		}
		if len(got) != len(want) {
			t.Errorf("%d: %v, want %v", i, got, c.Hidden)
			continue
		}
		for n := range want {
			if !got[n] {
				t.Errorf("%d: %v, want %v", i, got, c.Hidden)
			}
		}
	}
}
