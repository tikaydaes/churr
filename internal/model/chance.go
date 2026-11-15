package model

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Chance results: /roll, /flip, /pick and /shuffle (ruling R67,
// docs/MESSAGE-FORMAT.md section 1.2). The client asks; the server draws
// the result and stores it on the message, where no edit can reach it, so
// every reader sees a result nobody could have made up.

// The kinds of chance result.
const (
	ChanceRoll    = "roll"
	ChanceFlip    = "flip"
	ChancePick    = "pick"
	ChanceShuffle = "shuffle"
	ChanceDraw    = "draw"
	ChanceTarot   = "tarot"
)

// The limits of a chance request. testdata/commands/dice.json holds cases
// at each of them.
const (
	MaxDiceText     = 100   // characters of a dice expression
	MaxDiceTerms    = 10    // terms in one roll
	MaxDice         = 100   // dice in one roll, all terms together
	MaxDieSides     = 1000  // sides of one die
	MaxDiceNumber   = 10000 // a plain number in a roll
	MinChanceOption = 2     // options for /pick and /shuffle
	MaxChanceOption = 20
	MaxOptionText   = 200 // characters of one option
)

// ChanceInput is what a client sends to have the server draw a result
// (schema ChanceInput).
type ChanceInput struct {
	Kind    string   `json:"kind"`
	Dice    string   `json:"dice,omitempty"`
	Options []string `json:"options,omitempty"`
	// Count and Deck are for a draw: how many cards (1 when left out),
	// from which deck, by name or alias (standard when left out).
	Count int    `json:"count,omitempty"`
	Deck  string `json:"deck,omitempty"`
	// Spread is for a tarot reading: the spread, by name or alias (three
	// cards when left out).
	Spread string `json:"spread,omitempty"`
}

// DiceTerm is one term of a roll (schema DiceTerm): some dice, or a plain
// number added or taken away.
type DiceTerm struct {
	// Sign is 1 to add the term, -1 to take it away.
	Sign int `json:"sign"`
	// Count and Sides are set for dice: Count dice of Sides sides.
	Count int `json:"count,omitempty"`
	Sides int `json:"sides,omitempty"`
	// Keep is kh, kl, dh or dl (keep highest, keep lowest, drop highest,
	// drop lowest), with KeepN the number kept or dropped.
	Keep  string `json:"keep,omitempty"`
	KeepN int    `json:"keep_n,omitempty"`
	// Rolls is each die as it fell, in order. Dropped lists the positions
	// in Rolls that do not count.
	Rolls   []int `json:"rolls,omitempty"`
	Dropped []int `json:"dropped,omitempty"`
	// Value is a plain number's value, or the sum of the dice that count.
	// The sign is not applied to it.
	Value int `json:"value"`
}

// Chance is a result the server drew (schema Chance).
type Chance struct {
	Kind string `json:"kind"`
	// Dice is the roll as the server read it, written out in full
	// (`d20` is `1d20`, `adv` is `2d20kh1`).
	Dice  string     `json:"dice,omitempty"`
	Terms []DiceTerm `json:"terms,omitempty"`
	Total *int       `json:"total,omitempty"`
	// Outcome is heads or tails, for a flip.
	Outcome string `json:"outcome,omitempty"`
	// Options are as the person gave them, for pick and shuffle. Picked is
	// the position of the one picked; Order lists the positions in the
	// order the shuffle put them.
	Options []string `json:"options,omitempty"`
	Picked  *int     `json:"picked,omitempty"`
	Order   []int    `json:"order,omitempty"`
	// Deck and Cards are a draw: the deck's name and each card drawn, in
	// the order drawn, by its code (testdata/commands/decks.json).
	// Reversed is set for a deck with orientation: whether each card came
	// up reversed.
	Deck     string   `json:"deck,omitempty"`
	Cards    []string `json:"cards,omitempty"`
	Reversed []bool   `json:"reversed,omitempty"`
	// Spread is a tarot reading's spread: Cards are its positions' cards in
	// order, all from the tarot deck, each with Reversed.
	Spread string `json:"spread,omitempty"`
}

// diceSyntax is said when an expression cannot be read.
const diceSyntax = "Write dice like 2d6+3, d20, 4d6kh3, 2d20kl1, adv or dis."

func diceErr(msg string) error { return Invalid("chance.dice", msg) }

// ParseDice reads a dice expression (FEATURES.md section 12.4): terms
// joined by + and -, each NdM (N defaults to 1; d% is d100), NdM followed
// by kh, kl, dh or dl and an optional count (k alone is kh; the count
// defaults to 1), a plain number, or adv and dis for 2d20kh1 and 2d20kl1.
// Case and spaces do not matter. Nothing at all is 1d20. It returns the
// terms with nothing rolled and the expression written out in full.
func ParseDice(expr string) ([]DiceTerm, string, error) {
	if utf8.RuneCountInString(expr) > MaxDiceText {
		return nil, "", diceErr(fmt.Sprintf("A roll is at most %d characters.", MaxDiceText))
	}
	// Spaces may stand beside + and -, and nowhere else: "2d6 3" is not
	// read as 2d63.
	fields := strings.Fields(strings.ToLower(expr))
	for k := 1; k < len(fields); k++ {
		a, b := fields[k-1], fields[k]
		if !strings.ContainsAny(a[len(a)-1:], "+-") && !strings.ContainsAny(b[:1], "+-") {
			return nil, "", diceErr(diceSyntax)
		}
	}
	s := strings.Join(fields, "")
	if s == "" {
		s = "1d20"
	}
	var terms []DiceTerm
	dice := 0
	i := 0
	for i < len(s) {
		sign := 1
		switch s[i] {
		case '+':
			i++
		case '-':
			sign = -1
			i++
		default:
			if len(terms) > 0 {
				return nil, "", diceErr(diceSyntax)
			}
		}
		if i >= len(s) {
			return nil, "", diceErr(diceSyntax)
		}
		t, n, err := diceTerm(s[i:])
		if err != nil {
			return nil, "", err
		}
		i += n
		t.Sign = sign
		terms = append(terms, t)
		if len(terms) > MaxDiceTerms {
			return nil, "", diceErr(fmt.Sprintf("A roll has at most %d terms.", MaxDiceTerms))
		}
		dice += t.Count
		if dice > MaxDice {
			return nil, "", diceErr(fmt.Sprintf("A roll has at most %d dice.", MaxDice))
		}
	}
	return terms, DiceString(terms), nil
}

// diceTerm reads one term from the start of s and says how many bytes it
// took.
func diceTerm(s string) (DiceTerm, int, error) {
	for _, w := range []struct {
		word string
		keep string
	}{{"adv", "kh"}, {"dis", "kl"}} {
		if strings.HasPrefix(s, w.word) {
			return DiceTerm{Count: 2, Sides: 20, Keep: w.keep, KeepN: 1}, len(w.word), nil
		}
	}
	i := 0
	num := func() (int, bool, error) {
		start := i
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
		}
		if i == start {
			return 0, false, nil
		}
		if i-start > 6 {
			return 0, true, diceErr(fmt.Sprintf("A number in a roll is at most %d.", MaxDiceNumber))
		}
		n, _ := strconv.Atoi(s[start:i])
		return n, true, nil
	}
	count, hasCount, err := num()
	if err != nil {
		return DiceTerm{}, 0, err
	}
	if i >= len(s) || s[i] != 'd' {
		// A plain number.
		if !hasCount {
			return DiceTerm{}, 0, diceErr(diceSyntax)
		}
		if count > MaxDiceNumber {
			return DiceTerm{}, 0, diceErr(fmt.Sprintf("A number in a roll is at most %d.", MaxDiceNumber))
		}
		return DiceTerm{Value: count}, i, nil
	}
	i++ // the d
	if !hasCount {
		count = 1
	}
	if count < 1 {
		return DiceTerm{}, 0, diceErr("Roll at least one die.")
	}
	if count > MaxDice {
		return DiceTerm{}, 0, diceErr(fmt.Sprintf("A roll has at most %d dice.", MaxDice))
	}
	var sides int
	if i < len(s) && s[i] == '%' {
		sides = 100
		i++
	} else {
		var has bool
		if sides, has, err = num(); err != nil || !has {
			if err == nil {
				err = diceErr(diceSyntax)
			}
			if has {
				err = diceErr(fmt.Sprintf("A die has 2 to %d sides.", MaxDieSides))
			}
			return DiceTerm{}, 0, err
		}
	}
	if sides < 2 || sides > MaxDieSides {
		return DiceTerm{}, 0, diceErr(fmt.Sprintf("A die has 2 to %d sides.", MaxDieSides))
	}
	t := DiceTerm{Count: count, Sides: sides}
	if i < len(s) && s[i] == 'k' || i+1 < len(s) && s[i] == 'd' && (s[i+1] == 'h' || s[i+1] == 'l') {
		mode := s[i : i+1]
		i++
		switch {
		case i < len(s) && s[i] == 'h':
			mode += "h"
			i++
		case i < len(s) && s[i] == 'l':
			mode += "l"
			i++
		default:
			mode += "h" // k alone keeps the highest
		}
		n, has, err := num()
		if err != nil {
			return DiceTerm{}, 0, err
		}
		if !has {
			n = 1
		}
		t.Keep, t.KeepN = mode, n
		if mode[0] == 'k' && (n < 1 || n > count) {
			return DiceTerm{}, 0, diceErr(fmt.Sprintf("Keep between 1 and %d of %dd%d.", count, count, sides))
		}
		if mode[0] == 'd' && (n < 1 || n >= count) {
			if count == 1 {
				return DiceTerm{}, 0, diceErr("Dropping needs at least two dice.")
			}
			return DiceTerm{}, 0, diceErr(fmt.Sprintf("Drop between 1 and %d of %dd%d.", count-1, count, sides))
		}
	}
	return t, i, nil
}

// DiceString writes terms out in full: 2d6+1d4kh1-3.
func DiceString(terms []DiceTerm) string {
	var b strings.Builder
	for i, t := range terms {
		if t.Sign < 0 {
			b.WriteByte('-')
		} else if i > 0 {
			b.WriteByte('+')
		}
		if t.Count == 0 {
			b.WriteString(strconv.Itoa(t.Value))
			continue
		}
		fmt.Fprintf(&b, "%dd%d", t.Count, t.Sides)
		if t.Keep != "" {
			fmt.Fprintf(&b, "%s%d", t.Keep, t.KeepN)
		}
	}
	return b.String()
}

// RollDice rolls the dice of terms with die, which gives a whole number from
// 1 to sides, and returns the terms with their rolls and the total.
func RollDice(terms []DiceTerm, die func(sides int) int) ([]DiceTerm, int) {
	out := make([]DiceTerm, len(terms))
	total := 0
	for i, t := range terms {
		if t.Count > 0 {
			t.Rolls = make([]int, t.Count)
			for j := range t.Rolls {
				t.Rolls[j] = die(t.Sides)
			}
			t.Dropped = droppedDice(t)
			t.Value = 0
			skip := map[int]bool{}
			for _, j := range t.Dropped {
				skip[j] = true
			}
			for j, r := range t.Rolls {
				if !skip[j] {
					t.Value += r
				}
			}
		}
		total += t.Sign * t.Value
		out[i] = t
	}
	return out, total
}

// droppedDice says which rolls of a term do not count, by position. Of equal
// rolls, the later one is dropped first.
func droppedDice(t DiceTerm) []int {
	if t.Keep == "" {
		return nil
	}
	n := len(t.Rolls)
	drop := t.KeepN // dh and dl
	lowest := t.Keep == "dl" || t.Keep == "kh"
	if t.Keep[0] == 'k' {
		drop = n - t.KeepN
	}
	if drop <= 0 {
		return nil
	}
	// Positions, sorted so that the ones to drop come first.
	pos := make([]int, n)
	for i := range pos {
		pos[i] = i
	}
	before := func(a, b int) bool {
		ra, rb := t.Rolls[a], t.Rolls[b]
		if ra != rb {
			if lowest {
				return ra < rb
			}
			return ra > rb
		}
		return a > b
	}
	for i := 1; i < n; i++ { // insertion sort: at most 100 dice
		for j := i; j > 0 && before(pos[j], pos[j-1]); j-- {
			pos[j], pos[j-1] = pos[j-1], pos[j]
		}
	}
	out := append([]int(nil), pos[:drop]...)
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// CheckChanceOptions checks the options of a pick or shuffle and returns
// them trimmed.
func CheckChanceOptions(options []string) ([]string, error) {
	if len(options) < MinChanceOption || len(options) > MaxChanceOption {
		return nil, Invalid("chance.options", fmt.Sprintf("Give %d to %d options, separated by |.", MinChanceOption, MaxChanceOption))
	}
	out := make([]string, len(options))
	for i, o := range options {
		o = strings.TrimSpace(o)
		if o == "" {
			return nil, Invalid("chance.options", "An option cannot be empty.")
		}
		if utf8.RuneCountInString(o) > MaxOptionText || strings.ContainsAny(o, "\r\n") {
			return nil, Invalid("chance.options", fmt.Sprintf("An option is one line of at most %d characters.", MaxOptionText))
		}
		out[i] = o
	}
	return out, nil
}

// DrawChance draws the result a request asks for. intn gives a whole number
// from 0 to n-1; the server passes one that reads crypto/rand.
func DrawChance(in ChanceInput, intn func(n int) int) (Chance, error) {
	switch in.Kind {
	case ChanceRoll:
		terms, text, err := ParseDice(in.Dice)
		if err != nil {
			return Chance{}, err
		}
		if len(in.Options) > 0 {
			return Chance{}, Invalid("chance.options", "A roll takes no options.")
		}
		rolled, total := RollDice(terms, func(sides int) int { return intn(sides) + 1 })
		return Chance{Kind: ChanceRoll, Dice: text, Terms: rolled, Total: &total}, nil
	case ChanceFlip:
		if in.Dice != "" || len(in.Options) > 0 {
			return Chance{}, Invalid("chance", "A coin flip takes nothing more.")
		}
		out := "heads"
		if intn(2) == 1 {
			out = "tails"
		}
		return Chance{Kind: ChanceFlip, Outcome: out}, nil
	case ChancePick, ChanceShuffle:
		if in.Dice != "" {
			return Chance{}, Invalid("chance.dice", "Only a roll takes dice.")
		}
		opts, err := CheckChanceOptions(in.Options)
		if err != nil {
			return Chance{}, err
		}
		if in.Kind == ChancePick {
			p := intn(len(opts))
			return Chance{Kind: ChancePick, Options: opts, Picked: &p}, nil
		}
		order := make([]int, len(opts))
		for i := range order {
			order[i] = i
		}
		for i := len(order) - 1; i > 0; i-- { // Fisher and Yates
			j := intn(i + 1)
			order[i], order[j] = order[j], order[i]
		}
		return Chance{Kind: ChanceShuffle, Options: opts, Order: order}, nil
	case ChanceDraw:
		if in.Dice != "" || len(in.Options) > 0 {
			return Chance{}, Invalid("chance", "A draw takes a count and a deck only.")
		}
		d, ok := findDeck(in.Deck)
		if !ok {
			return Chance{}, Invalid("chance.deck", "There is no deck called "+in.Deck+". The decks are: "+strings.Join(DeckNames(), ", ")+".")
		}
		n := in.Count
		if n == 0 {
			n = 1
		}
		if size := len(d.Cards()); n < 1 || n > size {
			return Chance{}, Invalid("chance.count", fmt.Sprintf("Draw 1 to %d cards from the %s deck.", size, d.Name))
		}
		if in.Spread != "" {
			return Chance{}, Invalid("chance.spread", "Only a tarot reading takes a spread.")
		}
		cards, rev := drawCards(d, n, intn)
		return Chance{Kind: ChanceDraw, Deck: d.Name, Cards: cards, Reversed: rev}, nil
	case ChanceTarot:
		if in.Dice != "" || len(in.Options) > 0 || in.Count != 0 || in.Deck != "" {
			return Chance{}, Invalid("chance", "A tarot reading takes a spread only.")
		}
		sp, ok := findSpread(in.Spread)
		if !ok {
			return Chance{}, Invalid("chance.spread", "There is no spread called "+in.Spread+". The spreads are: "+strings.Join(SpreadNames(), ", ")+".")
		}
		d, _ := findDeck("tarot")
		cards, rev := drawCards(d, len(sp.Positions), intn)
		return Chance{Kind: ChanceTarot, Deck: d.Name, Spread: sp.Name, Cards: cards, Reversed: rev}, nil
	}
	return Chance{}, Invalid("chance.kind", "A chance result is a roll, flip, pick, shuffle, draw or tarot reading.")
}

// drawCards draws n cards from a deck without putting any back: the first n
// places of a Fisher and Yates shuffle, each place chosen evenly from the
// cards left. (The owner's library cut and riffled instead, which leaves the
// order uneven.) A deck with orientation also turns each card upright or
// reversed, evenly.
func drawCards(d deck, n int, intn func(n int) int) ([]string, []bool) {
	cards := d.Cards()
	for i := 0; i < n; i++ {
		j := i + intn(len(cards)-i)
		cards[i], cards[j] = cards[j], cards[i]
	}
	var rev []bool
	if d.Orientation {
		rev = make([]bool, n)
		for i := range rev {
			rev[i] = intn(2) == 1
		}
	}
	return cards[:n], rev
}

// Valid reports whether a result that came from somewhere else (another
// server of a shared conversation) holds together: its kind is known, a
// roll's dice are within the limits and add up to its total, and a pick or
// shuffle names its own options. It cannot tell whether it was drawn
// fairly; the server that drew it vouches for that.
func (c Chance) Valid() bool {
	switch c.Kind {
	case ChanceRoll:
		if c.Total == nil || len(c.Terms) == 0 || len(c.Terms) > MaxDiceTerms {
			return false
		}
		total, dice := 0, 0
		for _, t := range c.Terms {
			if t.Sign != 1 && t.Sign != -1 {
				return false
			}
			if t.Count > 0 {
				dice += t.Count
				if len(t.Rolls) != t.Count || t.Sides < 2 || t.Sides > MaxDieSides {
					return false
				}
				for _, r := range t.Rolls {
					if r < 1 || r > t.Sides {
						return false
					}
				}
			}
			total += t.Sign * t.Value
		}
		return dice <= MaxDice && total == *c.Total
	case ChanceFlip:
		return c.Outcome == "heads" || c.Outcome == "tails"
	case ChanceDraw, ChanceTarot:
		d, ok := findDeck(c.Deck)
		if !ok || d.Name != c.Deck {
			return false
		}
		if c.Kind == ChanceTarot {
			sp, ok := findSpread(c.Spread)
			if !ok || sp.Name != c.Spread || d.Name != "tarot" || len(c.Cards) != len(sp.Positions) {
				return false
			}
		} else if c.Spread != "" {
			return false
		}
		cards := d.Cards()
		if len(c.Cards) == 0 || len(c.Cards) > len(cards) || (c.Reversed != nil && len(c.Reversed) != len(c.Cards)) || (c.Reversed != nil) != d.Orientation {
			return false
		}
		left := map[string]int{}
		for _, x := range cards {
			left[x]++
		}
		for _, x := range c.Cards {
			if left[x] == 0 {
				return false
			}
			left[x]--
		}
		return true
	case ChancePick:
		return c.Picked != nil && *c.Picked >= 0 && *c.Picked < len(c.Options) && len(c.Options) <= MaxChanceOption
	case ChanceShuffle:
		if len(c.Order) != len(c.Options) || len(c.Options) > MaxChanceOption {
			return false
		}
		seen := map[int]bool{}
		for _, i := range c.Order {
			if i < 0 || i >= len(c.Options) || seen[i] {
				return false
			}
			seen[i] = true
		}
		return true
	}
	return false
}

// Fallback is the plain text that stands in for a result for a client that
// cannot draw it, and that search reads: "Rolled 2d6+3: [4, 1] + 3 = 8".
// Dice that do not count are in brackets.
func (c Chance) Fallback() string {
	switch c.Kind {
	case ChanceRoll:
		var b strings.Builder
		b.WriteString("Rolled " + c.Dice + ":")
		for i, t := range c.Terms {
			switch {
			case t.Sign < 0:
				b.WriteString(" - ")
			case i > 0:
				b.WriteString(" + ")
			default:
				b.WriteString(" ")
			}
			if t.Count == 0 {
				b.WriteString(strconv.Itoa(t.Value))
				continue
			}
			drop := map[int]bool{}
			for _, j := range t.Dropped {
				drop[j] = true
			}
			parts := make([]string, len(t.Rolls))
			for j, r := range t.Rolls {
				parts[j] = strconv.Itoa(r)
				if drop[j] {
					parts[j] = "(" + parts[j] + ")"
				}
			}
			b.WriteString("[" + strings.Join(parts, ", ") + "]")
		}
		if c.Total != nil {
			fmt.Fprintf(&b, " = %d", *c.Total)
		}
		return b.String()
	case ChanceFlip:
		return "Flipped a coin: " + c.Outcome
	case ChanceDraw:
		d, _ := findDeck(c.Deck)
		names := make([]string, len(c.Cards))
		for i, x := range c.Cards {
			names[i] = cardName(d, x)
			if i < len(c.Reversed) && c.Reversed[i] {
				names[i] += " (reversed)"
			}
		}
		return "Drew from the " + c.Deck + " deck: " + strings.Join(names, ", ")
	case ChanceTarot:
		d, _ := findDeck(c.Deck)
		sp, _ := findSpread(c.Spread)
		parts := make([]string, 0, len(c.Cards))
		for i, x := range c.Cards {
			way := "upright"
			if i < len(c.Reversed) && c.Reversed[i] {
				way = "reversed"
			}
			pos := ""
			if i < len(sp.Positions) {
				pos = tarotPositions[sp.Positions[i]] + ": "
			}
			parts = append(parts, pos+cardName(d, x)+" ("+way+")")
		}
		return "Tarot, " + sp.Title + ": " + strings.Join(parts, "; ")
	case ChancePick:
		if c.Picked == nil || *c.Picked < 0 || *c.Picked >= len(c.Options) {
			return ""
		}
		return "Picked " + c.Options[*c.Picked] + " from: " + strings.Join(c.Options, " | ")
	case ChanceShuffle:
		parts := make([]string, 0, len(c.Order))
		for _, i := range c.Order {
			if i >= 0 && i < len(c.Options) {
				parts = append(parts, c.Options[i])
			}
		}
		return "Shuffled: " + strings.Join(parts, ", ")
	}
	return ""
}
