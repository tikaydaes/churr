package cards

import (
	"testing"
	"unicode/utf8"

	"github.com/tikaydaes/churr/internal/model"
)

// The longest text the module can write fits a message: every card of the
// largest deck, each reversed where the deck has orientation, and every
// spread with every card reversed. Post checks no length after a draw
// (docs/FAIR.md section 6), so this is what keeps it within.
func TestTheLongestTextFits(t *testing.T) {
	for _, name := range model.DeckNames() {
		d, ok := model.FindDeck(name)
		if !ok {
			t.Fatalf("deck %s", name)
		}
		c := model.CardDraw{Deck: d.Name, Cards: d.Cards}
		if d.Orientation {
			c.Reversed = make([]bool, len(d.Cards))
			for i := range c.Reversed {
				c.Reversed[i] = true
			}
		}
		if n := utf8.RuneCountInString(c.Text()); n > model.MaxMessageText {
			t.Errorf("all of the %s deck: %d characters", name, n)
		}
	}
	tarot, _ := model.FindDeck("tarot")
	for _, name := range model.SpreadNames() {
		sp, _ := model.FindSpread(name)
		r := model.TarotReading{Spread: sp.Name, Cards: tarot.Cards[len(tarot.Cards)-len(sp.Positions):], Reversed: make([]bool, len(sp.Positions))}
		for i := range r.Reversed {
			r.Reversed[i] = true
		}
		if n := utf8.RuneCountInString(r.Text()); n > model.MaxMessageText {
			t.Errorf("the %s spread: %d characters", name, n)
		}
	}
}
