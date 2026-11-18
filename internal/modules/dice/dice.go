// Package dice is the dice module (docs/DESIGN-MODULES.md section 9.1): /roll
// and /dice, and inline dice, `[[2d6+3]]` in a message, which it claims and
// rolls when the message is sent. A roll is a message whose text is the roll
// in words ("Rolled 2d6+3: [4, 1] + 3 = 8") and whose view is a value of the
// catalogue's kind dice_roll; inline dice are an object of that kind over
// their characters. The dice are rolled from the host's stream, the message's
// share of its draw (docs/FAIR.md), by the frozen rule fair.Roll: nobody can
// foresee or steer a result, and anyone re-derives every die from the proof
// the message carries (ruling R67).
package dice

import (
	"context"
	"strings"

	"github.com/tikaydaes/churr/internal/fair"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/modhost"
)

// Name is the module's name, the `by` of every roll.
const Name = "dice"

// Inside is what may stand between `[[` and `]]` to be offered as dice: the
// characters of the grammar, 1 to 100 of them. What they say is the
// grammar's to decide; an inside it refuses stays text.
const Inside = `(?i)[0-9dkhlavis%+, -]{1,100}`

// TypeDice is the kind of the dice a command is given.
const TypeDice = "dice"

// forms are the forms of /roll and /dice (docs/DESIGN-MODULES.md section
// 9.1): dice, or nothing, which is 1d20 for /roll and 1d6 for /dice.
func forms(hint string) []model.CommandForm {
	return []model.CommandForm{{Slots: []model.CommandSlot{{Name: "dice", Hint: hint, Kind: TypeDice}}}, {}}
}

// Module is the dice module, to register with the host.
func Module() modhost.Module {
	return modhost.Module{
		Name:  Name,
		Title: "Dice",
		Commands: []modhost.Command{
			{Name: "roll", Hint: "[dice]", Summary: "Rolls dice, rolled by the server: d20, 2d6+3, 4d6kh3, adv; rows separated by commas",
				Forms: forms("Dice, such as 2d6+3"), Help: rollHelp},
			{Name: "dice", Hint: "[dice]", Summary: "Rolls six-sided dice, rolled by the server: 4 is 4d6, 4+3 is 4d6+3",
				Forms: forms("A number of d6, such as 4 or 4+3"), Help: diceHelp},
			// A coin is a two-sided die (docs/DESIGN-MODULES.md section 9.1).
			{Name: "flip", Summary: "Flips a coin, flipped by the server", Forms: []model.CommandForm{{}}, Help: flipHelp},
		},
		// The characters dice are written in; what they say is the
		// grammar's to decide when the command runs.
		Types: []modhost.DerivedType{{Name: TypeDice, Pattern: Inside}},
		// A bot rolls its own dice.
		PeopleOnly: true,
		Claims:     []modhost.Claim{{Kind: model.KindDiceRoll, Open: "[[", Close: "]]", Pattern: Inside}},
		Handler:    handler{},
	}
}

type handler struct{}

// Command rolls /roll's dice, or /dice's six-sided ones: nothing at all is
// 1d20 for /roll and 1d6 for /dice.
func (handler) Command(_ context.Context, h *modhost.Host, in modhost.Invocation) (modhost.Reply, error) {
	if in.Command == "flip" {
		return flip(h, in.Text)
	}
	expr := in.Text
	if in.Command == "dice" {
		expr = D6(expr)
	}
	rows, written, err := ParseRows(strings.TrimSpace(expr))
	if err != nil {
		return modhost.Reply{}, err
	}
	r, err := roll(h, model.DiceRoll{Dice: written, Rows: rows})
	if err != nil {
		return modhost.Reply{}, err
	}
	return modhost.Reply{Text: Text(r), View: &modhost.Value{Kind: model.KindDiceRoll, Data: r}}, nil
}

// Resolve rolls inline dice: one row, in the grammar of /roll. Unlike
// /roll, nothing at all is no dice: `[[ ]]` stays text.
func (handler) Resolve(_ context.Context, h *modhost.Host, in modhost.Marked) (modhost.Value, error) {
	if strings.TrimSpace(in.Text) == "" {
		return modhost.Value{}, diceErr(diceSyntax)
	}
	terms, written, err := Parse(in.Text)
	if err != nil {
		return modhost.Value{}, err
	}
	r, err := roll(h, model.DiceRoll{Dice: written, Rows: []model.DiceRow{{Dice: written, Terms: terms}}})
	if err != nil {
		return modhost.Value{}, err
	}
	return modhost.Value{Kind: model.KindDiceRoll, Data: r}, nil
}

// roll rolls every row of a roll read with nothing rolled, from the call's
// stream by derive 1's dice (docs/FAIR.md section 5): rows, terms and dice in
// order, so a verifier given the roll's rows and the draw re-derives each.
func roll(h *modhost.Host, r model.DiceRoll) (model.DiceRoll, error) {
	s, err := h.Random()
	if err != nil {
		return r, err
	}
	rows := make([]fair.DiceRow, len(r.Rows))
	for i, row := range r.Rows {
		rows[i].Dice = row.Dice
		for _, t := range row.Terms {
			rows[i].Terms = append(rows[i].Terms, fair.DiceTerm{Sign: t.Sign, Count: t.Count, Sides: t.Sides, Keep: t.Keep, KeepN: t.KeepN, Value: t.Value})
		}
	}
	rolled, err := fair.Roll(s, rows)
	if err != nil {
		return r, err
	}
	for i, row := range rolled {
		for j, t := range row.Terms {
			mt := &r.Rows[i].Terms[j]
			mt.Rolls, mt.Dropped, mt.Value = t.Rolls, t.Dropped, t.Value
		}
		r.Rows[i].Total = row.Total
	}
	return r, nil
}

// flip flips a coin from the call's stream by derive 1's composition
// (docs/FAIR.md section 5a): yesno(1, 2), yes being heads.
func flip(h *modhost.Host, text string) (modhost.Reply, error) {
	if strings.TrimSpace(text) != "" {
		return modhost.Reply{}, model.Invalid("text", "/flip takes nothing after it.")
	}
	rs, err := h.Draw(fair.CoinAsks()...)
	if err != nil {
		return modhost.Reply{}, err
	}
	c := model.CoinFlip{Outcome: "tails"}
	if *rs[0].Yes {
		c.Outcome = "heads"
	}
	return modhost.Reply{Text: c.Text(), View: &modhost.Value{Kind: model.KindCoinFlip, Data: c}}, nil
}
