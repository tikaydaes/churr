package model

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// Chance results: /flip, /pick, /shuffle, /draw and /tarot (ruling R67,
// docs/MESSAGE-FORMAT.md section 1.2). The client asks; the server draws
// the result and stores it on the message, where no edit can reach it, so
// every reader sees a result nobody could have made up. A roll is the dice
// module's (internal/modules/dice): a message whose text is the roll in
// words and whose view is a value of kind dice_roll, whose shape and check
// are here (DiceRoll).

// The kinds of chance request.
const (
	ChanceFlip    = "flip"
	ChancePick    = "pick"
	ChanceShuffle = "shuffle"
	ChanceDraw    = "draw"
	ChanceTarot   = "tarot"
)

// The limits of the kind dice_roll, which the dice module rolls within.
// testdata/commands/dice.json holds cases at each of them.
const (
	MaxDiceTerms = 10   // terms in one row of a roll
	MaxDiceRows  = 8    // rows of a roll, separated by commas
	MaxDice      = 100  // dice in one roll, all terms and rows together
	MaxDieSides  = 1000 // sides of one die
)

// The limits of a chance request.
const (
	MinChanceOption = 2 // options for /pick and /shuffle
	MaxChanceOption = 20
	MaxOptionText   = 200 // characters of one option
)

// ChanceInput is what a client sends to have the server draw a result
// (schema ChanceInput).
type ChanceInput struct {
	Kind    string   `json:"kind"`
	Options []string `json:"options,omitempty"`
	// Count and Deck are for a draw: how many cards (1 when left out),
	// from which deck, by name or alias (standard when left out).
	Count int    `json:"count,omitempty"`
	Deck  string `json:"deck,omitempty"`
	// Spread is for a tarot reading: the spread, by name or alias (three
	// cards when left out).
	Spread string `json:"spread,omitempty"`
}

// DiceTerm is one term of a roll (schema DiceRollTerm): some dice, or a
// plain number added or taken away.
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

// DiceRow is one row of a roll (schema DiceRollRow): an expression of its
// own, rolled with its own dice, and its total.
type DiceRow struct {
	// Dice is the row written out in full.
	Dice  string     `json:"dice"`
	Terms []DiceTerm `json:"terms"`
	Total int        `json:"total"`
}

// DiceRoll is a roll the server made: the data of kind dice_roll (schema
// DiceRoll), carried in a value's envelope.
type DiceRoll struct {
	// Dice is the roll as the server read it, written out in full (`d20`
	// is `1d20`, `adv` is `2d20kh1`), its rows separated by ", ".
	Dice string `json:"dice"`
	// Rows are its rows, in order, each with its dice and total.
	Rows []DiceRow `json:"rows"`
}

// Chance is a result the server drew other than a roll (schema Chance).
type Chance struct {
	Kind string `json:"kind"`
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

// DrawChance draws the result a request asks for. intn gives a whole number from 0 to n-1; the server passes one
// that reads crypto/rand.
func DrawChance(in ChanceInput, intn func(n int) int) (Chance, error) {
	switch in.Kind {
	case ChanceFlip:
		if len(in.Options) > 0 {
			return Chance{}, Invalid("chance", "A coin flip takes nothing more.")
		}
		out := "heads"
		if intn(2) == 1 {
			out = "tails"
		}
		return Chance{Kind: ChanceFlip, Outcome: out}, nil
	case ChancePick, ChanceShuffle:
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
		if len(in.Options) > 0 {
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
		if len(in.Options) > 0 || in.Count != 0 || in.Deck != "" {
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

// Valid reports whether a roll that came from somewhere else holds
// together: it names its dice, its dice are within the limits, the dice
// it drops are among them, and they add up to each row's total. It cannot
// tell whether it was rolled fairly; whoever rolled it vouches for that.
// testdata/types/dice_roll.json holds it and the clients' checks to the
// same answers.
func (r DiceRoll) Valid() bool {
	if r.Dice == "" || len(r.Rows) == 0 || len(r.Rows) > MaxDiceRows {
		return false
	}
	dice := 0
	for _, row := range r.Rows {
		if row.Dice == "" || len(row.Terms) == 0 || len(row.Terms) > MaxDiceTerms {
			return false
		}
		total := 0
		for _, t := range row.Terms {
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
				seen := make(map[int]bool, len(t.Dropped))
				for _, i := range t.Dropped {
					if i < 0 || i >= t.Count || seen[i] {
						return false
					}
					seen[i] = true
				}
			}
			total += t.Sign * t.Value
		}
		if total != row.Total {
			return false
		}
	}
	return dice <= MaxDice
}

// Valid reports whether a result that came from somewhere else (another
// server of a shared conversation) holds together: its kind is known, and a
// pick or shuffle names its own options. It cannot tell whether it was
// drawn fairly; the server that drew it vouches for that.
func (c Chance) Valid() bool {
	switch c.Kind {
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
// cannot draw it, and that search reads.
func (c Chance) Fallback() string {
	switch c.Kind {
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
