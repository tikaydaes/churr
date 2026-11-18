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

	flip := model.Message{ConversationID: c.ID, AuthorID: ann.ID, Fallback: "Flipped a coin: tails", Chance: &model.Chance{Kind: model.ChanceFlip, Outcome: "tails"}}
	post(&flip)
	got, err = s.Messages().Get(ctx, flip.ID)
	must(t, err)
	eq(t, "not an action", got.Action, false)
	eq(t, "chance kept", got.Chance != nil && got.Chance.Valid() && got.Chance.Outcome == "tails", true)
	edited, err := s.Messages().Edit(ctx, store.MessageEdit{ID: flip.ID, Text: "for first move", Fallback: got.Fallback, KeepVersion: true, By: ann.ID, At: model.Now()})
	must(t, err)
	eq(t, "chance after an edit", edited.Chance != nil && edited.Chance.Outcome == "tails", true)
	_, err = s.Messages().Delete(ctx, flip.ID, ann.ID, model.Now())
	must(t, err)
	got, err = s.Messages().Get(ctx, flip.ID)
	must(t, err)
	eq(t, "chance gone with the delete", got.Chance == nil, true)
}

func init() { register("messages_view", testMessageView) }

// testMessageView: a message's view, a value of the catalogue such as a
// roll, is stored as it came, its data byte for byte beside the text that is
// the message; it survives an edit and goes with a delete.
func testMessageView(t *testing.T, s store.Store) {
	ann := newAccount(t, s, "ann", model.RoleOwner)
	c := newChannel(t, s, "dice", false, ann.ID)
	data := model.Bytes{0x0a, 0x05, '2', 'd', '6', '+', '3', 0x00, 0xff, 0x80}
	roll := model.Message{ConversationID: c.ID, AuthorID: ann.ID, Text: "Rolled 2d6+3: [4, 2] + 3 = 9",
		View: &model.ValueEnvelope{Kind: model.KindDiceRoll, Level: 1, Data: data, By: "dice",
			Alt: &model.ValueEnvelope{Kind: "unknown_later", Level: 7, Data: model.Bytes{1, 2, 3}}}}
	must(t, s.Tx(ctx, func(tx store.Tx) error {
		seq, err := tx.Conversations().NextSeq(ctx, c.ID)
		if err != nil {
			return err
		}
		roll.Seq = seq
		return tx.Messages().Insert(ctx, &roll, nil)
	}))
	got, err := s.Messages().Get(ctx, roll.ID)
	must(t, err)
	v := got.View
	eq(t, "the text is the message", got.Text, "Rolled 2d6+3: [4, 2] + 3 = 9")
	eq(t, "view kept", v != nil && v.Kind == model.KindDiceRoll && v.Level == 1 && v.By == "dice" && string(v.Data) == string(data), true)
	eq(t, "its rendition kept", v != nil && v.Alt != nil && v.Alt.Kind == "unknown_later" && v.Alt.Level == 7 && string(v.Alt.Data) == "\x01\x02\x03", true)
	eq(t, "the text is searched", got.SearchText != "" && got.Chance == nil, true)
	edited, err := s.Messages().Edit(ctx, store.MessageEdit{ID: roll.ID, Text: got.Text, Entities: []model.Entity{{Type: model.EntityObject, At: 0, Len: 6,
		Object: &model.ValueEnvelope{Kind: model.KindDiceRoll, Level: 1, By: "dice", Removed: true}}}, By: ann.ID, At: model.Now()})
	must(t, err)
	eq(t, "view after an edit", edited.View != nil && string(edited.View.Data) == string(data), true)
	got, err = s.Messages().Get(ctx, roll.ID)
	must(t, err)
	eq(t, "an object entity kept", len(got.Entities) == 1 && got.Entities[0].Object != nil && got.Entities[0].Object.Removed, true)
	_, err = s.Messages().Delete(ctx, roll.ID, ann.ID, model.Now())
	must(t, err)
	got, err = s.Messages().Get(ctx, roll.ID)
	must(t, err)
	eq(t, "view gone with the delete", got.View == nil && got.Text == "", true)
}
