package chat

import (
	"github.com/tikaydaes/churr/internal/model"
)

// checkMarks checks a new message's action mark and module view (ruling
// R67). A module's answer (a roll, a coin flipped, a pick, cards drawn)
// stands alone: its words and its view.
func checkMarks(in NewMessage) error {
	if in.Action && in.Text == "" {
		return model.Invalid("action", "An action needs text: what you are doing.")
	}
	if in.View != nil {
		// Whether a bot may have a module's answer is the module's to
		// say (modhost.Module.PeopleOnly); the host has already asked.
		if in.Text == "" || in.Action || len(in.FileIDs) > 0 || len(in.Cards) > 0 || in.Form != nil {
			return model.Invalid("view", "A module's answer is a message of its own: its words and its view.")
		}
	}
	return nil
}
