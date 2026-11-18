package model

import (
	"strings"
)

// The kinds of chance besides a roll (chance.go): their data, their checks
// (held, like dice_roll's, by every receiving server and every client) and
// their words, which are the message's text.

// The kinds.
const (
	KindCoinFlip     = "coin_flip"     // data: CoinFlip
	KindPick         = "pick"          // data: Pick
	KindShuffle      = "shuffle"       // data: Shuffle
	KindCardDraw     = "card_draw"     // data: CardDraw
	KindTarotReading = "tarot_reading" // data: TarotReading
)

// CoinFlip is a coin the server flipped (schema CoinFlip).
type CoinFlip struct {
	// Outcome is heads or tails.
	Outcome string `json:"outcome"`
}

// Pick is one of the options the server picked (schema Pick).
type Pick struct {
	// Options are as the person gave them, trimmed; Picked is the position
	// of the one picked.
	Options []string `json:"options"`
	Picked  int      `json:"picked"`
}

// Shuffle is the options in the order the server shuffled them into (schema
// Shuffle).
type Shuffle struct {
	// Options are as the person gave them, trimmed; Order lists their
	// positions in the order the shuffle put them.
	Options []string `json:"options"`
	Order   []int    `json:"order"`
}

// CardDraw is cards the server drew from a deck without putting any back
// (schema CardDraw): /draw, and /mahjongg from the mahjong deck.
type CardDraw struct {
	// Deck is the deck's name (testdata/commands/decks.json); Cards each
	// card drawn, in the order drawn, by its code; Reversed, for a deck with
	// orientation (tarot), whether each came up reversed.
	Deck     string   `json:"deck"`
	Cards    []string `json:"cards"`
	Reversed []bool   `json:"reversed,omitempty"`
}

// TarotReading is a tarot reading the server drew (schema TarotReading): the
// card in each position of a spread, from the tarot deck, each upright or
// reversed.
type TarotReading struct {
	Spread   string   `json:"spread"`
	Cards    []string `json:"cards"`
	Reversed []bool   `json:"reversed"`
}

// Valid reports whether a flip from somewhere else holds together.
func (c CoinFlip) Valid() bool { return c.Outcome == "heads" || c.Outcome == "tails" }

// Text is the flip in words.
func (c CoinFlip) Text() string { return "Flipped a coin: " + c.Outcome }

// optionsValid is a pick's or shuffle's options as a receiver holds them.
func optionsValid(options []string) bool {
	if len(options) < MinChanceOption || len(options) > MaxChanceOption {
		return false
	}
	for _, o := range options {
		if o == "" || len([]rune(o)) > MaxOptionText || strings.ContainsAny(o, "\r\n") {
			return false
		}
	}
	return true
}

// Valid reports whether a pick from somewhere else holds together: it names
// its options, and the one picked is among them.
func (p Pick) Valid() bool {
	return optionsValid(p.Options) && p.Picked >= 0 && p.Picked < len(p.Options)
}

// Text is the pick in words.
func (p Pick) Text() string {
	return "Picked " + p.Options[p.Picked] + " from: " + strings.Join(p.Options, " | ")
}

// Valid reports whether a shuffle from somewhere else holds together: its
// order puts each of its options in one place.
func (s Shuffle) Valid() bool {
	if !optionsValid(s.Options) || len(s.Order) != len(s.Options) {
		return false
	}
	seen := map[int]bool{}
	for _, i := range s.Order {
		if i < 0 || i >= len(s.Options) || seen[i] {
			return false
		}
		seen[i] = true
	}
	return true
}

// Text is the shuffle in words.
func (s Shuffle) Text() string {
	parts := make([]string, len(s.Order))
	for k, i := range s.Order {
		parts[k] = s.Options[i]
	}
	return "Shuffled: " + strings.Join(parts, ", ")
}

// cardsFrom reports whether cards could have been drawn from a deck without
// putting any back: each at most as often as the deck holds it.
func cardsFrom(d deck, cards []string) bool {
	all := d.Cards()
	if len(cards) == 0 || len(cards) > len(all) {
		return false
	}
	left := map[string]int{}
	for _, x := range all {
		left[x]++
	}
	for _, x := range cards {
		if left[x] == 0 {
			return false
		}
		left[x]--
	}
	return true
}

// Valid reports whether a draw from somewhere else holds together: a deck
// by its name, cards it holds, and an orientation for each card exactly
// when the deck has one.
func (c CardDraw) Valid() bool {
	d, ok := findDeck(c.Deck)
	if !ok || d.Name != c.Deck || !cardsFrom(d, c.Cards) {
		return false
	}
	if d.Orientation {
		return len(c.Reversed) == len(c.Cards)
	}
	return c.Reversed == nil
}

// Text is the draw in words.
func (c CardDraw) Text() string {
	d, _ := findDeck(c.Deck)
	names := make([]string, len(c.Cards))
	for i, x := range c.Cards {
		names[i] = cardName(d, x)
		if i < len(c.Reversed) && c.Reversed[i] {
			names[i] += " (reversed)"
		}
	}
	return "Drew from the " + c.Deck + " deck: " + strings.Join(names, ", ")
}

// Valid reports whether a reading from somewhere else holds together: a
// spread by its name, a card of the tarot deck for each of its positions,
// none twice, each upright or reversed.
func (r TarotReading) Valid() bool {
	sp, ok := findSpread(r.Spread)
	d, _ := findDeck("tarot")
	return ok && sp.Name == r.Spread && len(r.Cards) == len(sp.Positions) && len(r.Reversed) == len(r.Cards) && cardsFrom(d, r.Cards)
}

// Text is the reading in words.
func (r TarotReading) Text() string {
	d, _ := findDeck("tarot")
	sp, _ := findSpread(r.Spread)
	parts := make([]string, len(r.Cards))
	for i, x := range r.Cards {
		way := "upright"
		if r.Reversed[i] {
			way = "reversed"
		}
		parts[i] = tarotPositions[sp.Positions[i]] + ": " + cardName(d, x) + " (" + way + ")"
	}
	return "Tarot, " + sp.Title + ": " + strings.Join(parts, "; ")
}

// DeckInfo is a deck as the cards module reads it.
type DeckInfo struct {
	Name string
	// Cards are its cards in the registry's order: the positions a draw's
	// primitives index (docs/FAIR.md section 4).
	Cards []string
	// Orientation: each card drawn is also upright or reversed.
	Orientation bool
}

// FindDeck finds a deck by name or alias, without regard to case; an empty
// name is the standard deck.
func FindDeck(name string) (DeckInfo, bool) {
	d, ok := findDeck(name)
	if !ok {
		return DeckInfo{}, false
	}
	return DeckInfo{Name: d.Name, Cards: d.Cards(), Orientation: d.Orientation}, true
}

// SpreadInfo is a tarot spread as the cards module reads it.
type SpreadInfo struct {
	Name, Title string
	Positions   []string
}

// FindSpread finds a spread by name or alias, without regard to case; an
// empty name is the three-card spread.
func FindSpread(name string) (SpreadInfo, bool) {
	sp, ok := findSpread(name)
	if !ok {
		return SpreadInfo{}, false
	}
	return SpreadInfo{Name: sp.Name, Title: sp.Title, Positions: sp.Positions}, true
}
