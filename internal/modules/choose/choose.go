// Package choose is the module of /pick and /shuffle: one of the options a
// person gives, picked by the server, or all of them in a new order. A pick
// is a message whose text is the pick in words ("Picked coffee from: tea |
// coffee") and whose view is a value of the catalogue's kind pick; a shuffle
// likewise, of kind shuffle. Both come from the host's stream, the message's
// share of its draw (docs/FAIR.md), by derive 1's primitives as section 5a
// composes them, so anyone re-derives the outcome from the proof the message
// carries.
package choose

import (
	"context"
	"strings"

	"github.com/tikaydaes/churr/internal/fair"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/modhost"
)

// Name is the module's name, the `by` of every pick and shuffle.
const Name = "choose"

// forms are the forms of /pick and /shuffle: the options, separated by |.
var forms = []model.CommandForm{{Slots: []model.CommandSlot{{Name: "options", Hint: "Options separated by |, such as tea | coffee", Kind: "text"}}}}

// Module is the module, to register with the host.
func Module() modhost.Module {
	return modhost.Module{
		Name:  Name,
		Title: "Pick and shuffle",
		Commands: []modhost.Command{
			{Name: "pick", Hint: "a | b | c", Summary: "Picks one, picked by the server", Forms: forms, Help: pickHelp},
			{Name: "shuffle", Hint: "a | b | c", Summary: "Shuffles them, shuffled by the server", Forms: forms, Help: shuffleHelp},
		},
		// A bot picks its own.
		PeopleOnly: true,
		Handler:    handler{},
	}
}

type handler struct{}

// Options reads the options of /pick or /shuffle: two to twenty, separated
// by |, none empty, each one line of at most 200 characters, trimmed.
func Options(text string) ([]string, error) {
	if strings.TrimSpace(text) == "" {
		return nil, model.Invalid("text", "Give 2 to 20 options, separated by |.")
	}
	return model.CheckChanceOptions(strings.Split(text, "|"))
}

// Command picks one of the options or shuffles them.
func (handler) Command(_ context.Context, h *modhost.Host, in modhost.Invocation) (modhost.Reply, error) {
	opts, err := Options(in.Text)
	if err != nil {
		return modhost.Reply{}, err
	}
	if in.Command == "pick" {
		rs, err := h.Draw(fair.PickAsks(len(opts))...)
		if err != nil {
			return modhost.Reply{}, err
		}
		p := model.Pick{Options: opts, Picked: rs[0].Picked[0]}
		return modhost.Reply{Text: p.Text(), View: &modhost.Value{Kind: model.KindPick, Data: p}}, nil
	}
	rs, err := h.Draw(fair.ShuffleAsks(len(opts))...)
	if err != nil {
		return modhost.Reply{}, err
	}
	s := model.Shuffle{Options: opts, Order: rs[0].Order}
	return modhost.Reply{Text: s.Text(), View: &modhost.Value{Kind: model.KindShuffle, Data: s}}, nil
}

// Resolve claims nothing.
func (handler) Resolve(context.Context, *modhost.Host, modhost.Marked) (modhost.Value, error) {
	return modhost.Value{}, model.Invalid("kind", "The choose module resolves nothing.")
}

var pickHelp = model.CommandHelp{
	Description: "Picks one of the options you give, picked by the server, so nobody can say you chose. Everyone sees all the options and which one came up. The server proves the pick, and the shield beside it says whether this app checked the proof.",
	Args:        []model.CommandHelpArg{{Name: "a | b | c", About: "Two to twenty options, separated by |. None may be empty; each is one line of at most 200 characters."}},
	Examples: []model.CommandExample{
		{Type: "/{cmd} Pizza | Sushi | Tacos", Then: "The server picks one of the three."},
		{Type: "/{cmd} Monday | Wednesday | Friday", Then: "Settles which day the meeting is."},
	},
	Where: "Channels and chats. Not in sealed chats.",
	Who:   "Anyone who can post. Not bots.",
}

var shuffleHelp = model.CommandHelp{
	Description: "Puts the options you give in a random order, shuffled by the server. Handy for speaking orders and rotas: everyone sees the same order, and the shield beside it says whether this app checked the server's proof.",
	Args:        []model.CommandHelpArg{{Name: "a | b | c", About: "Two to twenty options, separated by |. None may be empty."}},
	Examples: []model.CommandExample{
		{Type: "/{cmd} Ada | Sam | Lee | Kit", Then: "Posts the four names in a random order."},
		{Type: "/{cmd} red | blue | green", Then: "Shuffles three teams."},
	},
	Where: "Channels and chats. Not in sealed chats.",
	Who:   "Anyone who can post. Not bots.",
}
