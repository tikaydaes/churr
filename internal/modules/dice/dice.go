// Package dice is the dice module (docs/DESIGN-MODULES.md section 9.1): /roll
// and /dice, and inline dice, `[[2d6+3]]` in a message, which it claims and
// rolls when the message is sent. A roll is a message whose text is the roll
// in words ("Rolled 2d6+3: [4, 1] + 3 = 8") and whose view is a value of the
// catalogue's kind dice_roll; inline dice are an object of that kind over
// their characters. The dice are rolled from the host's randomness, which is
// crypto/rand, so no result can be foreseen or steered (ruling R67).
package dice

import (
	"context"
	"strings"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/modhost"
)

// Name is the module's name, the `by` of every roll.
const Name = "dice"

// Inside is what may stand between `[[` and `]]` to be offered as dice: the
// characters of the grammar, 1 to 100 of them. What they say is the
// grammar's to decide; an inside it refuses stays text.
const Inside = `(?i)[0-9dkhlavis%+, -]{1,100}`

// Module is the dice module, to register with the host.
func Module() modhost.Module {
	return modhost.Module{
		Name:  Name,
		Title: "Dice",
		Commands: []modhost.Command{
			{Name: "roll", Hint: "[dice]", Summary: "Rolls dice, rolled by the server: d20, 2d6+3, 4d6kh3, adv; rows separated by commas"},
			{Name: "dice", Hint: "[number]", Summary: "Rolls six-sided dice, rolled by the server: 4 is 4d6, 4+3 is 4d6+3"},
		},
		Claims:  []modhost.Claim{{Kind: model.KindDiceRoll, Open: "[[", Close: "]]", Pattern: Inside}},
		Handler: handler{},
	}
}

type handler struct{}

// Command rolls /roll's dice, or /dice's six-sided ones: nothing at all is
// 1d20 for /roll and 1d6 for /dice.
func (handler) Command(_ context.Context, h *modhost.Host, in modhost.Invocation) (modhost.Reply, error) {
	expr := in.Text
	if in.Command == "dice" {
		expr = D6(expr)
	}
	rows, written, err := ParseRows(strings.TrimSpace(expr))
	if err != nil {
		return modhost.Reply{}, err
	}
	r := roll(h, model.DiceRoll{Dice: written, Rows: rows})
	return modhost.Reply{Text: Text(r), View: &modhost.Value{Kind: model.KindDiceRoll, Data: r}}, nil
}

// Resolve rolls inline dice: one row, in the grammar of /roll.
func (handler) Resolve(_ context.Context, h *modhost.Host, in modhost.Marked) (modhost.Value, error) {
	terms, written, err := Parse(in.Text)
	if err != nil {
		return modhost.Value{}, err
	}
	r := roll(h, model.DiceRoll{Dice: written, Rows: []model.DiceRow{{Dice: written, Terms: terms}}})
	return modhost.Value{Kind: model.KindDiceRoll, Data: r}, nil
}

// roll rolls every row of a roll read with nothing rolled.
func roll(h *modhost.Host, r model.DiceRoll) model.DiceRoll {
	for i := range r.Rows {
		r.Rows[i].Terms, r.Rows[i].Total = RollTerms(r.Rows[i].Terms, func(sides int) int { return h.Intn(sides) + 1 })
	}
	return r
}
