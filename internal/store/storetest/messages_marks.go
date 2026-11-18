package storetest

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

func init() { register("messages_marks", testMessageMarks) }

// testMessageMarks: a /me action is stored with the message (ruling R67).
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

	plain := model.Message{ConversationID: c.ID, AuthorID: ann.ID, Text: "hello"}
	post(&plain)
	got, err = s.Messages().Get(ctx, plain.ID)
	must(t, err)
	eq(t, "not an action", got.Action, false)
}

func init() { register("messages_view", testMessageView) }

// testMessageView: a message's view, a value of the catalogue such as a
// roll, is stored as it came beside the text that is the message: its data
// the kind's object, value for value, or, for a kind that came only as
// protobuf bytes, those bytes as base64url text, unchanged. It survives an
// edit and goes with a delete.
func testMessageView(t *testing.T, s store.Store) {
	ann := newAccount(t, s, "ann", model.RoleOwner)
	c := newChannel(t, s, "dice", false, ann.ID)
	data := json.RawMessage(`{"dice":"2d6+3","rows":[{"dice":"2d6+3","terms":[{"sign":1,"count":2,"sides":6,"rolls":[4,2],"value":6},{"sign":1,"value":3}],"total":9}],"later":{"n":12345678901234567890}}`)
	roll := model.Message{ConversationID: c.ID, AuthorID: ann.ID, Text: "Rolled 2d6+3: [4, 2] + 3 = 9",
		View: &model.ValueEnvelope{Kind: model.KindDiceRoll, Level: 1, Data: data, By: "dice",
			Alt: &model.ValueEnvelope{Kind: "unknown_later", Level: 7, Data: json.RawMessage(`"AQID_w"`)}}}
	sameJSON := func(a, b json.RawMessage) bool {
		var x, y any
		da, db := json.NewDecoder(bytes.NewReader(a)), json.NewDecoder(bytes.NewReader(b))
		da.UseNumber()
		db.UseNumber()
		return da.Decode(&x) == nil && db.Decode(&y) == nil && reflect.DeepEqual(x, y)
	}
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
	eq(t, "view kept", v != nil && v.Kind == model.KindDiceRoll && v.Level == 1 && v.By == "dice" && sameJSON(v.Data, data), true)
	eq(t, "its rendition kept", v != nil && v.Alt != nil && v.Alt.Kind == "unknown_later" && v.Alt.Level == 7 && string(v.Alt.Data) == `"AQID_w"`, true)
	eq(t, "the text is searched", got.SearchText != "", true)
	edited, err := s.Messages().Edit(ctx, store.MessageEdit{ID: roll.ID, Text: got.Text, Entities: []model.Entity{{Type: model.EntityObject, At: 0, Len: 6,
		Object: &model.ValueEnvelope{Kind: model.KindDiceRoll, Level: 1, By: "dice", Removed: true}}}, By: ann.ID, At: model.Now()})
	must(t, err)
	eq(t, "view after an edit", edited.View != nil && sameJSON(edited.View.Data, data), true)
	got, err = s.Messages().Get(ctx, roll.ID)
	must(t, err)
	eq(t, "an object entity kept", len(got.Entities) == 1 && got.Entities[0].Object != nil && got.Entities[0].Object.Removed, true)
	_, err = s.Messages().Delete(ctx, roll.ID, ann.ID, model.Now())
	must(t, err)
	got, err = s.Messages().Get(ctx, roll.ID)
	must(t, err)
	eq(t, "view gone with the delete", got.View == nil && got.Text == "", true)
}
