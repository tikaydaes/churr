// Package cards is the module of /draw, /tarot and /mahjongg (with /mahjong
// as another name): cards from a freshly shuffled deck, a tarot reading laid
// out in a spread, and tiles from a full mahjong set. A draw is a message
// whose text is the draw in words and whose view is a value of the
// catalogue's kind card_draw (/mahjongg's too, from the mahjong deck); a
// reading likewise, of kind tarot_reading. Every card comes from the host's
// stream, the message's share of its draw (docs/FAIR.md), by derive 1's
// primitives as section 5a composes them: the positions picked from the
// deck without putting any back, then, for a deck with orientation, which
// ones came up reversed. Anyone re-derives every card from the proof the
// message carries.
package cards

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/tikaydaes/churr/internal/fair"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/modhost"
)

// Name is the module's name, the `by` of every draw and reading.
const Name = "cards"

// The kinds of argument the module reads.
const (
	TypeCount = "count" // how many cards
	TypeName  = "name"  // a deck or a spread, by name or alias
)

var (
	countSlot = model.CommandSlot{Name: "count", Hint: "How many", Kind: TypeCount}
	isCount   = regexp.MustCompile(`^[0-9]{1,6}$`)
)

// Module is the module, to register with the host.
func Module() modhost.Module {
	deck := model.CommandSlot{Name: "deck", Hint: "A deck, such as tarot", Kind: TypeName}
	spread := model.CommandSlot{Name: "spread", Hint: "A spread, such as celtic", Kind: TypeName}
	count := []model.CommandForm{{Slots: []model.CommandSlot{countSlot}}, {}}
	return modhost.Module{
		Name:  Name,
		Title: "Cards",
		Commands: []modhost.Command{
			{Name: "draw", Hint: "[count] [deck]", Summary: "Draws cards, drawn by the server: standard, jokers, tarot, hanafuda and other decks",
				Forms: []model.CommandForm{{Slots: []model.CommandSlot{countSlot, deck}}, {Slots: []model.CommandSlot{countSlot}}, {Slots: []model.CommandSlot{deck}}, {}}, Help: drawHelp()},
			{Name: "tarot", Hint: "[spread]", Summary: "Reads tarot cards, drawn by the server: three, celtic, horseshoe, relationship",
				Forms: []model.CommandForm{{Slots: []model.CommandSlot{spread}}, {}}, Help: tarotHelp()},
			{Name: "mahjongg", Hint: "[count]", Summary: "Draws mahjong tiles, drawn by the server", Forms: count, Help: mahjonggHelp},
			{Name: "mahjong", Hint: "[count]", Summary: "Draws mahjong tiles, drawn by the server (as /mahjongg)", Forms: count, Help: mahjonggHelp},
		},
		Types:      []modhost.DerivedType{{Name: TypeCount, Pattern: `[0-9]{1,6}`}, {Name: TypeName, Pattern: `[A-Za-z0-9-]{1,32}`}},
		PeopleOnly: true,
		Handler:    handler{},
	}
}

type handler struct{}

// Command draws cards, tiles or a reading.
func (handler) Command(_ context.Context, h *modhost.Host, in modhost.Invocation) (modhost.Reply, error) {
	words := strings.Fields(in.Text)
	switch in.Command {
	case "tarot":
		if len(words) > 1 {
			return modhost.Reply{}, model.Invalid("text", "Write /"+in.Command+" [spread], for example /"+in.Command+" celtic.")
		}
		r, err := Reading(h, strings.Join(words, ""))
		if err != nil {
			return modhost.Reply{}, err
		}
		return modhost.Reply{Text: r.Text(), View: &modhost.Value{Kind: model.KindTarotReading, Data: r}}, nil
	case "mahjongg", "mahjong":
		n := 1
		if len(words) > 1 || (len(words) == 1 && !isCount.MatchString(words[0])) {
			return modhost.Reply{}, model.Invalid("text", "Write /"+in.Command+" [count], for example /"+in.Command+" 13.")
		}
		if len(words) == 1 {
			n, _ = strconv.Atoi(words[0])
		}
		d, err := Draw(h, n, "mahjong")
		if err != nil {
			return modhost.Reply{}, err
		}
		return modhost.Reply{Text: d.Text(), View: &modhost.Value{Kind: model.KindCardDraw, Data: d}}, nil
	}
	// /draw [count] [deck]: a count alone, a deck alone, or a count then a
	// deck.
	n, deck := 1, ""
	usage := model.Invalid("text", "Write /"+in.Command+" [count] [deck], for example /"+in.Command+" 3 tarot.")
	switch {
	case len(words) > 2:
		return modhost.Reply{}, usage
	case len(words) >= 1 && isCount.MatchString(words[0]):
		n, _ = strconv.Atoi(words[0])
		if len(words) == 2 {
			deck = words[1]
		}
	case len(words) == 1:
		deck = words[0]
	case len(words) == 2:
		return modhost.Reply{}, usage
	}
	d, err := Draw(h, n, deck)
	if err != nil {
		return modhost.Reply{}, err
	}
	return modhost.Reply{Text: d.Text(), View: &modhost.Value{Kind: model.KindCardDraw, Data: d}}, nil
}

// Draw draws n cards from a deck, by name or alias ("" is the standard
// deck), none put back, from the call's stream.
func Draw(h *modhost.Host, n int, deck string) (model.CardDraw, error) {
	d, ok := model.FindDeck(deck)
	if !ok {
		return model.CardDraw{}, model.Invalid("text", "There is no deck called "+deck+". The decks are: "+strings.Join(model.DeckNames(), ", ")+".")
	}
	if n < 1 || n > len(d.Cards) {
		return model.CardDraw{}, model.Invalid("text", fmt.Sprintf("Draw 1 to %d cards from the %s deck.", len(d.Cards), d.Name))
	}
	rs, err := h.Draw(fair.CardsAsks(len(d.Cards), n, d.Orientation)...)
	if err != nil {
		return model.CardDraw{}, err
	}
	cards, rev, err := fair.Cards(d.Cards, d.Orientation, rs)
	if err != nil {
		return model.CardDraw{}, err
	}
	return model.CardDraw{Deck: d.Name, Cards: cards, Reversed: rev}, nil
}

// Reading lays out a tarot spread, by name or alias ("" is three cards), from
// the call's stream: a card of the tarot deck for each position, each
// upright or reversed.
func Reading(h *modhost.Host, spread string) (model.TarotReading, error) {
	sp, ok := model.FindSpread(spread)
	if !ok {
		return model.TarotReading{}, model.Invalid("text", "There is no spread called "+spread+". The spreads are: "+strings.Join(model.SpreadNames(), ", ")+".")
	}
	d, _ := model.FindDeck("tarot")
	rs, err := h.Draw(fair.CardsAsks(len(d.Cards), len(sp.Positions), true)...)
	if err != nil {
		return model.TarotReading{}, err
	}
	cards, rev, err := fair.Cards(d.Cards, true, rs)
	if err != nil {
		return model.TarotReading{}, err
	}
	return model.TarotReading{Spread: sp.Name, Cards: cards, Reversed: rev}, nil
}

// Resolve claims nothing.
func (handler) Resolve(context.Context, *modhost.Host, modhost.Marked) (modhost.Value, error) {
	return modhost.Value{}, model.Invalid("kind", "The cards module resolves nothing.")
}

func drawHelp() model.CommandHelp {
	return model.CommandHelp{
		Description: "Draws cards from a freshly shuffled deck, drawn by the server. Each card is shown by name and picture; from the tarot deck a card may come up reversed, drawn upside down. No card comes up more often than the deck holds it. The server proves the draw, and the shield beside it says whether this app checked the proof.",
		Args: []model.CommandHelpArg{
			{Name: "count", About: "How many cards: 1 unless given, at most the size of the deck."},
			{Name: "deck", About: "Which deck, by name or by another name it answers to: " + strings.Join(model.DeckNames(), ", ") + ". The standard deck unless given. A count alone, a deck alone, or a count then a deck."},
		},
		Examples: []model.CommandExample{
			{Type: "/{cmd}", Then: "Draws one card from a standard deck."},
			{Type: "/{cmd} 5", Then: "Deals five cards from a standard deck."},
			{Type: "/{cmd} 3 tarot", Then: "Draws three tarot cards, some perhaps reversed."},
			{Type: "/{cmd} hanafuda", Then: "Draws one flower card."},
		},
		Where: "Channels and chats. Not in sealed chats.",
		Who:   "Anyone who can post. Not bots.",
	}
}

func tarotHelp() model.CommandHelp {
	return model.CommandHelp{
		Description: "Lays out a tarot reading, drawn by the server: one card for each position of the spread, laid out in the spread's shape (a row, the Celtic cross and its staff, a horseshoe), each upright or reversed, and a line on the reading as a whole. Press a card for its position, name and meaning. It is for fun.",
		Args:        []model.CommandHelpArg{{Name: "spread", About: "Which spread, by name or another name it answers to: " + strings.Join(model.SpreadNames(), ", ") + ". Three cards unless given."}},
		Examples: []model.CommandExample{
			{Type: "/{cmd}", Then: "Past, present and future: three cards."},
			{Type: "/{cmd} celtic", Then: "The Celtic cross: ten cards."},
			{Type: "/{cmd} love", Then: "The relationship spread: seven cards."},
		},
		Where: "Channels and chats. Not in sealed chats.",
		Who:   "Anyone who can post. Not bots.",
	}
}

var mahjonggHelp = model.CommandHelp{
	Description: "Draws tiles from a full mahjong set of 144, drawn by the server, each shown by name and picture.",
	Args:        []model.CommandHelpArg{{Name: "count", About: "How many tiles: 1 unless given, at most 144."}},
	Examples: []model.CommandExample{
		{Type: "/{cmd}", Then: "Draws one tile."},
		{Type: "/{cmd} 13", Then: "Draws a hand of thirteen tiles."},
	},
	Where: "Channels and chats. Not in sealed chats.",
	Who:   "Anyone who can post. Not bots.",
}
