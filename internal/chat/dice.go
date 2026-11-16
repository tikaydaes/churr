package chat

import (
	"unicode/utf16"

	"github.com/tikaydaes/churr/internal/markdown"
	"github.com/tikaydaes/churr/internal/model"
)

// Inline dice (ruling R73, docs/MESSAGE-FORMAT.md section 3.2): `[[2d6+3]]`
// in a person's message is rolled by the server when the message is
// posted, never on an edit. The result is stored on the entity over those
// characters. An edit keeps a roll whose dice it leaves exactly as they
// were (the same dice, in the same order among the message's dice); dice
// it writes or changes are marked removed, with nothing rolled. Bots,
// webhooks, imports and sealed chats get no inline dice.

// InlineDice implements markdown.DiceSource.
func (r *resolver) InlineDice() (string, func(int) int) {
	return r.dice, func(sides int) int { return cryptoIntn(sides) + 1 }
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
		if e.Type == model.EntityDice {
			before = append(before, e)
		}
	}
	out := make([]model.Entity, len(now))
	n := 0
	for i, e := range now {
		out[i] = e
		if e.Type != model.EntityDice {
			continue
		}
		if n < len(before) && !before[n].Removed && typed(ou, before[n]) == typed(nu, e) {
			kept := before[n]
			kept.At, kept.Len = e.At, e.Len
			out[i] = kept
		}
		n++
	}
	return out
}
