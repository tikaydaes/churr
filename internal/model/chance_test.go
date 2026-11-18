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

// chanceTexts are the cases of testdata/commands/chance.json that are the
// model's: a value of each chance kind but a roll (the dice module's) and
// its text.
type chanceTexts struct {
	Texts []struct {
		Kind string          `json:"kind"`
		Data json.RawMessage `json:"data"`
		Text string          `json:"text"`
	} `json:"texts"`
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

// texter is a chance kind's data: whether it holds together, and its words.
type texter interface {
	Valid() bool
	Text() string
}

func kindData(t *testing.T, kind string, data json.RawMessage) texter {
	t.Helper()
	var v texter
	switch kind {
	case KindCoinFlip:
		v = &CoinFlip{}
	case KindPick:
		v = &Pick{}
	case KindShuffle:
		v = &Shuffle{}
	case KindCardDraw:
		v = &CardDraw{}
	case KindTarotReading:
		v = &TarotReading{}
	default:
		t.Fatalf("no kind %q", kind)
	}
	if err := json.Unmarshal(data, v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestChanceVectors(t *testing.T) {
	var v chanceTexts
	readJSON(t, "../../testdata/commands/chance.json", &v)
	if len(v.Texts) < 5 {
		t.Fatal("too few cases")
	}
	for _, c := range v.Texts {
		d := kindData(t, c.Kind, c.Data)
		if !d.Valid() {
			t.Errorf("%s %s is not valid", c.Kind, c.Data)
			continue
		}
		if got := d.Text(); got != c.Text {
			t.Errorf("text: got %q, want %q", got, c.Text)
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
	if (DiceRoll{Dice: "1d6", Rows: []DiceRow{{Dice: "1d6", Terms: []DiceTerm{{Sign: 1, Count: 1, Sides: 6, Rolls: []int{7}, Value: 7}}, Total: 7}}}).Valid() {
		t.Error("a die above its sides was taken")
	}
}

// TestChanceKindCases: the server's check of each chance kind that came from
// somewhere else gives the answers testdata/types/<kind>.json gives, the ones
// the clients' checks give.
func TestChanceKindCases(t *testing.T) {
	for _, kind := range []string{KindCoinFlip, KindPick, KindShuffle, KindCardDraw, KindTarotReading} {
		var cases struct {
			Kind           string `json:"kind"`
			Level          int    `json:"level"`
			Valid, Invalid []struct {
				Name string          `json:"name"`
				Data json.RawMessage `json:"data"`
			}
		}
		readJSON(t, "../../testdata/types/"+kind+".json", &cases)
		if cases.Kind != kind || cases.Level != KindLevels()[kind] || len(cases.Valid) == 0 || len(cases.Invalid) < 3 {
			t.Fatalf("%s: the file is not this kind's, or has too few cases", kind)
		}
		check := func(data json.RawMessage) bool {
			v := &ValueEnvelope{Kind: kind, Level: 1, Data: data}
			return CheckValue(v) == v
		}
		for _, c := range cases.Valid {
			if !check(c.Data) {
				t.Errorf("%s %s: refused", kind, c.Name)
			}
		}
		for _, c := range cases.Invalid {
			if check(c.Data) {
				t.Errorf("%s %s: taken", kind, c.Name)
			}
		}
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

// TestTarotLayouts holds every spread of testdata/commands/tarot.json to a
// layout: one place per position, and no two cards overlapping except a
// card laid across another (turned 90) on the same place. The steps are
// the clients': a column is 1.25 card widths, a row a card's height and a
// ninth; a card is 246/176 times as tall as it is wide.
func TestTarotLayouts(t *testing.T) {
	var tarot struct {
		Spreads []struct {
			Name      string       `json:"name"`
			Positions []string     `json:"positions"`
			Layout    [][3]float64 `json:"layout"`
		} `json:"spreads"`
	}
	readJSON(t, "../../testdata/commands/tarot.json", &tarot)
	const ratio = 246.0 / 176.0
	box := func(l [3]float64) (x0, y0, x1, y1 float64) {
		cx, cy := l[0]*1.25+0.5, l[1]*ratio*10/9+ratio/2
		w, h := 1.0, ratio
		if l[2] == 90 {
			w, h = h, w
		}
		return cx - w/2, cy - h/2, cx + w/2, cy + h/2
	}
	for _, sp := range tarot.Spreads {
		if len(sp.Layout) != len(sp.Positions) {
			t.Errorf("%s: %d places for %d positions", sp.Name, len(sp.Layout), len(sp.Positions))
			continue
		}
		for i, a := range sp.Layout {
			if a[2] != 0 && a[2] != 90 {
				t.Errorf("%s: %s turned %v", sp.Name, sp.Positions[i], a[2])
			}
			for j := i + 1; j < len(sp.Layout); j++ {
				b := sp.Layout[j]
				if a[0] == b[0] && a[1] == b[1] && a[2] != b[2] {
					continue // a crossing pair
				}
				ax0, ay0, ax1, ay1 := box(a)
				bx0, by0, bx1, by1 := box(b)
				if ax0 < bx1 && bx0 < ax1 && ay0 < by1 && by0 < ay1 {
					t.Errorf("%s: %s and %s overlap", sp.Name, sp.Positions[i], sp.Positions[j])
				}
			}
		}
	}
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

// TestLiteralSlash holds the literal slash (ruling R74) to
// testdata/commands/parse.json, literal_cases, the vectors the clients pass
// too.
func TestLiteralSlash(t *testing.T) {
	var v struct {
		LiteralCases []struct {
			Text    string  `json:"text"`
			Literal *string `json:"literal"`
		} `json:"literal_cases"`
	}
	readJSON(t, "../../testdata/commands/parse.json", &v)
	if len(v.LiteralCases) == 0 {
		t.Fatal("no cases")
	}
	for _, c := range v.LiteralCases {
		got, ok := LiteralSlash(c.Text)
		if ok != (c.Literal != nil) || (ok && got != *c.Literal) {
			t.Errorf("%q: %q %v, want %v", c.Text, got, ok, c.Literal)
		}
	}
}

// TestMuteLimit holds the longest time-out the clients' /mute takes
// (testdata/commands/parse.json, mute_max_seconds) to the server's own.
func TestMuteLimit(t *testing.T) {
	var v struct {
		MuteMaxSeconds int `json:"mute_max_seconds"`
	}
	readJSON(t, "../../testdata/commands/parse.json", &v)
	if v.MuteMaxSeconds != MaxTimeoutSeconds {
		t.Errorf("parse.json says %d, the server allows %d", v.MuteMaxSeconds, MaxTimeoutSeconds)
	}
}

// TestDiceRollCases: the server's check of a roll that came from somewhere
// else gives the answers testdata/types/dice_roll.json gives, the ones the
// clients' checks give.
func TestDiceRollCases(t *testing.T) {
	raw, err := os.ReadFile("../../testdata/types/dice_roll.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases struct {
		Valid, Invalid []struct {
			Name string          `json:"name"`
			Data json.RawMessage `json:"data"`
		}
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	read := func(data json.RawMessage) bool {
		var r DiceRoll
		return json.Unmarshal(data, &r) == nil && r.Valid()
	}
	for _, c := range cases.Valid {
		if !read(c.Data) {
			t.Errorf("%s: refused", c.Name)
		}
	}
	for _, c := range cases.Invalid {
		if read(c.Data) {
			t.Errorf("%s: taken", c.Name)
		}
	}
	if len(cases.Valid) < 3 || len(cases.Invalid) < 5 {
		t.Fatal("too few cases")
	}
}
