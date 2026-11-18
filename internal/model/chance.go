package model

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// The catalogue's kinds of chance (docs/DESIGN-MODULES.md section 4.1,
// docs/FAIR.md): a roll of dice (DiceRoll, the dice module's), a coin flipped
// (/flip, the dice module's), a pick and a shuffle (the choose module's), and
// cards drawn and a tarot reading (the cards module's: /draw, /mahjongg,
// /tarot). Each is a message whose text is the outcome in words and whose
// view is a value of its kind, drawn from the message's one draw, so anyone
// re-derives it from the proof the message carries (ruling R67). The shapes
// and checks of the kinds are here; how they are drawn is the modules'.

// The limits of the kind dice_roll, which the dice module rolls within.
// testdata/commands/dice.json holds cases at each of them.
const (
	MaxDiceTerms = 10   // terms in one row of a roll
	MaxDiceRows  = 8    // rows of a roll, separated by commas
	MaxDice      = 100  // dice in one roll, all terms and rows together
	MaxDieSides  = 1000 // sides of one die
)

// The limits of a pick or shuffle's options.
const (
	MinChanceOption = 2 // options for /pick and /shuffle
	MaxChanceOption = 20
	MaxOptionText   = 200 // characters of one option
)

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

// CheckChanceOptions checks the options of a pick or shuffle and returns
// them trimmed.
func CheckChanceOptions(options []string) ([]string, error) {
	if len(options) < MinChanceOption || len(options) > MaxChanceOption {
		return nil, Invalid("text", fmt.Sprintf("Give %d to %d options, separated by |.", MinChanceOption, MaxChanceOption))
	}
	out := make([]string, len(options))
	for i, o := range options {
		o = strings.TrimSpace(o)
		if o == "" {
			return nil, Invalid("text", "An option cannot be empty.")
		}
		if utf8.RuneCountInString(o) > MaxOptionText || strings.ContainsAny(o, "\r\n") {
			return nil, Invalid("text", fmt.Sprintf("An option is one line of at most %d characters.", MaxOptionText))
		}
		out[i] = o
	}
	return out, nil
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
