package model

import "strings"

// A deck /draw draws from (ruling R67). The registry itself is decks_gen.go,
// written by tools/decks-gen/gen.py with testdata/commands/decks.json.
type deck struct {
	Name    string
	Aliases []string
	Suits   []string
	Ranks   []string
	// Copies is how many times the suits and ranks are in the deck.
	Copies int
	// Extras are cards outside the suits: jokers, the major arcana, the
	// honours and bonus tiles of mahjong.
	Extras []deckExtra
	// Orientation: each card drawn is also upright or reversed (tarot).
	Orientation bool
	// Naming is how a card is read: "of" (queen of cups), "suit_rank"
	// (pine bright) or "suit_number" (dots 5).
	Naming string
}

// A spread of /tarot: its positions in order (ruling R68).
type tarotSpread struct {
	Name      string
	Title     string
	Aliases   []string
	Positions []string
}

// findSpread finds a spread by name or alias, without regard to case. An
// empty name is the three-card spread.
func findSpread(name string) (tarotSpread, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		name = "three"
	}
	for _, s := range tarotSpreads {
		if s.Name == name {
			return s, true
		}
		for _, a := range s.Aliases {
			if a == name {
				return s, true
			}
		}
	}
	return tarotSpread{}, false
}

// SpreadNames lists the spreads' names.
func SpreadNames() []string {
	out := make([]string, len(tarotSpreads))
	for i, s := range tarotSpreads {
		out[i] = s.Name
	}
	return out
}

type deckExtra struct {
	Code  string
	Count int
}

// Cards lists the deck's cards in the registry's order. A card may be in it
// more than once.
func (d deck) Cards() []string {
	var out []string
	for c := 0; c < d.Copies; c++ {
		for _, s := range d.Suits {
			for _, r := range d.Ranks {
				out = append(out, s+"-"+r)
			}
		}
	}
	for _, e := range d.Extras {
		for i := 0; i < e.Count; i++ {
			out = append(out, e.Code)
		}
	}
	return out
}

// findDeck finds a deck by name or alias, without regard to case. An empty
// name is the standard deck.
func findDeck(name string) (deck, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		name = "standard"
	}
	for _, d := range decks {
		if d.Name == name {
			return d, true
		}
		for _, a := range d.Aliases {
			if a == name {
				return d, true
			}
		}
	}
	return deck{}, false
}

// DeckNames lists the decks' names, for the error that says which exist.
func DeckNames() []string {
	out := make([]string, len(decks))
	for i, d := range decks {
		out[i] = d.Name
	}
	return out
}

// DeckCards lists the cards of the deck with this name or alias, or nil.
func DeckCards(name string) []string {
	d, ok := findDeck(name)
	if !ok {
		return nil
	}
	return d.Cards()
}

// cardName is the English name of a card, for the fallback text.
func cardName(d deck, code string) string {
	if n, ok := extraNames[code]; ok {
		return n
	}
	s, r, _ := strings.Cut(code, "-")
	switch d.Naming {
	case "suit_rank":
		return suitNames[s] + " " + rankNames[r]
	case "suit_number":
		return suitNames[s] + " " + rankShort[r]
	}
	return rankNames[r] + " of " + suitNames[s]
}
