package storetest

import (
	"testing"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

func init() { register("messages_marks", testMessageMarks) }

// testMessageMarks: a /me action and a chance result are stored with the
// message, survive an edit, and a delete takes the result away (ruling R67).
func testMessageMarks(t *testing.T, s store.Store) {
	ann := newAccount(t, s, "ann", model.RoleOwner)
	c := newChannel(t, s, "games", false, ann.ID)
	post := func(m *model.Message) {
		must(t, s.Tx(ctx, func(tx store.Tx) error {
			seq, err := tx.Conversations().NextSeq(ctx, c.ID)
			if err != nil {
				return err
			}
			m.Seq = seq
			return tx.Messages().Insert(ctx, m, nil)
		}))
	}
	act := model.Message{ConversationID: c.ID, AuthorID: ann.ID, Text: "waves", Action: true}
	post(&act)
	got, err := s.Messages().Get(ctx, act.ID)
	must(t, err)
	eq(t, "action", got.Action, true)
	eq(t, "no chance", got.Chance == nil, true)

	roll := model.Message{ConversationID: c.ID, AuthorID: ann.ID, Fallback: "Rolled 2d6+3, 1d4: [4, 2] + 3 = 9; [1] = 1", Chance: &model.Chance{
		Kind: model.ChanceRoll, Dice: "2d6+3, 1d4", Rows: []model.DiceRow{
			{Dice: "2d6+3", Total: 9, Terms: []model.DiceTerm{{Sign: 1, Count: 2, Sides: 6, Rolls: []int{4, 2}, Value: 6}, {Sign: 1, Value: 3}}},
			{Dice: "1d4", Total: 1, Terms: []model.DiceTerm{{Sign: 1, Count: 1, Sides: 4, Rolls: []int{1}, Value: 1}}}}}}
	post(&roll)
	got, err = s.Messages().Get(ctx, roll.ID)
	must(t, err)
	eq(t, "not an action", got.Action, false)
	eq(t, "chance kept", got.Chance != nil && got.Chance.Valid() && len(got.Chance.Rows) == 2 && got.Chance.Rows[0].Total == 9 && got.Chance.Rows[0].Terms[0].Rolls[1] == 2 && got.Chance.Rows[1].Total == 1, true)

	edited, err := s.Messages().Edit(ctx, store.MessageEdit{ID: roll.ID, Text: "for damage", Fallback: got.Fallback, KeepVersion: true, By: ann.ID, At: model.Now()})
	must(t, err)
	eq(t, "chance after an edit", edited.Chance != nil && edited.Chance.Rows[0].Total == 9, true)

	_, err = s.Messages().Delete(ctx, roll.ID, ann.ID, model.Now())
	must(t, err)
	got, err = s.Messages().Get(ctx, roll.ID)
	must(t, err)
	eq(t, "chance gone with the delete", got.Chance == nil, true)
}
