package chat

import (
	"crypto/rand"
	"math/big"

	"github.com/tikaydaes/churr/internal/model"
)

// cryptoIntn gives a whole number from 0 to n-1 from crypto/rand, so that a
// chance result cannot be foreseen or steered (ruling R67).
func cryptoIntn(n int) int {
	v, err := rand.Int(rand.Reader, big.NewInt(int64(n)))
	if err != nil {
		// The system's random source failing is not something to carry
		// on from.
		panic("chat: crypto/rand failed: " + err.Error())
	}
	return int(v.Int64())
}

// checkMarks checks a new message's action mark, chance request and module
// view, and draws a chance result (ruling R67). A chance result or a
// module's answer stands alone, and only a person may ask for a chance
// result. (A roll is the dice module's answer to /roll, which comes as a
// view.)
func checkMarks(actor model.Account, in NewMessage) (*model.Chance, error) {
	if in.Action && in.Text == "" {
		return nil, model.Invalid("action", "An action needs text: what you are doing.")
	}
	if in.View != nil {
		// Whether a bot may have a module's answer is the module's to
		// say (modhost.Module.PeopleOnly); the host has already asked.
		if in.Text == "" || in.Action || in.Chance != nil || len(in.FileIDs) > 0 || len(in.Cards) > 0 || in.Form != nil {
			return nil, model.Invalid("view", "A module's answer is a message of its own: its words and its view.")
		}
	}
	if in.Chance == nil {
		return nil, nil
	}
	if actor.Role == model.RoleBot {
		return nil, model.Invalid("chance", "Chance results are for people; a bot draws its own.")
	}
	if in.Text != "" || in.Action || len(in.FileIDs) > 0 || len(in.Cards) > 0 || in.Form != nil {
		return nil, model.Invalid("chance", "A chance result is a message of its own, with no text, files, cards or form.")
	}
	c, err := model.DrawChance(*in.Chance, cryptoIntn)
	if err != nil {
		return nil, err
	}
	return &c, nil
}
