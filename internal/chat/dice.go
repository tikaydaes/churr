package chat

import (
	"bytes"
	"encoding/json"
	"unicode/utf16"

	"github.com/tikaydaes/churr/internal/markdown"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/wirepb"
)

// Dice (ruling R73, docs/MESSAGE-FORMAT.md sections 1.2 and 3.2) are values
// of the catalogue's kind dice_roll. A roll asked for with /roll or /dice is
// a message whose text is the roll in words and whose view is the roll.
// Inline dice, `[[2d6+3]]` in a person's message, are an object entity over
// those characters, rolled by the server when the message is posted, never
// on an edit. An edit keeps a roll whose dice it leaves exactly as they were
// (the same dice, in the same order among the message's dice); dice it
// writes or changes are marked removed, with nothing rolled. Bots, webhooks,
// imports and sealed chats get no inline dice.

// diceModule is who makes the values of kind dice_roll: the dice module.
const diceModule = "dice"

// diceValue is the value of kind dice_roll for a roll, or, for nil, for
// dice nothing was rolled for.
func diceValue(roll *model.DiceRoll) (model.ValueEnvelope, error) {
	level, _ := wirepb.KindLevel(model.KindDiceRoll)
	v := model.ValueEnvelope{Kind: model.KindDiceRoll, Level: level, By: diceModule}
	if roll == nil {
		v.Removed = true
		return v, nil
	}
	raw, err := json.Marshal(roll)
	if err != nil {
		return v, err
	}
	// Held to the kind's schema here, so that every reader on either wire
	// gets it in full.
	var data any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&data); err != nil {
		return v, err
	}
	if _, err := wirepb.EncodeKind(model.KindDiceRoll, data); err != nil {
		return v, err
	}
	v.Data = raw
	return v, nil
}

// InlineDice implements markdown.DiceSource: the dice are rolled from
// crypto/rand.
func (r *resolver) InlineDice() (string, func(*model.DiceRoll) (model.ValueEnvelope, bool)) {
	return r.dice, func(roll *model.DiceRoll) (model.ValueEnvelope, bool) {
		if roll != nil {
			for i := range roll.Rows {
				roll.Rows[i].Terms, roll.Rows[i].Total = model.RollDice(roll.Rows[i].Terms, func(sides int) int { return cryptoIntn(sides) + 1 })
			}
		}
		v, err := diceValue(roll)
		return v, err == nil
	}
}

// diceMode is how a new message's inline dice are treated: rolled for a
// person writing it now or through a schedule, left as text otherwise.
func diceMode(actor model.Account, via *model.Via) string {
	if actor.Role == model.RoleBot {
		return markdown.DiceOff
	}
	if via != nil && via.Kind != "scheduled" {
		return markdown.DiceOff
	}
	return markdown.DiceRoll
}

// isDice reports whether an entity is inline dice: an object of kind
// dice_roll.
func isDice(e model.Entity) bool {
	return e.Type == model.EntityObject && e.Object != nil && e.Object.Kind == model.KindDiceRoll
}

// keepRolls gives the dice of an edited text the rolls they had, where the
// same dice stand in the same place among the message's dice; the rest
// stay marked removed.
func keepRolls(oldText string, old []model.Entity, newText string, now []model.Entity) []model.Entity {
	ou, nu := utf16.Encode([]rune(oldText)), utf16.Encode([]rune(newText))
	typed := func(u []uint16, e model.Entity) string {
		if e.At < 0 || e.At+e.Len > len(u) {
			return ""
		}
		return string(utf16.Decode(u[e.At : e.At+e.Len]))
	}
	var before []model.Entity
	for _, e := range old {
		if isDice(e) {
			before = append(before, e)
		}
	}
	out := make([]model.Entity, len(now))
	n := 0
	for i, e := range now {
		out[i] = e
		if !isDice(e) {
			continue
		}
		if n < len(before) && !before[n].Object.Removed && typed(ou, before[n]) == typed(nu, e) {
			kept := before[n]
			kept.At, kept.Len = e.At, e.Len
			out[i] = kept
		}
		n++
	}
	return out
}
