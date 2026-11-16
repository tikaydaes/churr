package api

import "github.com/tikaydaes/churr/internal/model"

// Knocks: asking a knock channel's managers to be added (docs/FEATURES.md
// section 12.5, rulings R74 and R79).

func init() {
	register(func(a *API) {
		a.handle("knock", a.knock)
		a.handle("listKnocks", a.listKnocks)
		a.handle("answerKnock", a.answerKnock)
	})
}

func (a *API) knock(r *Request) error {
	id, err := PathID[model.ConversationID](r, "id")
	if err != nil {
		return err
	}
	k, err := a.Chat.Knock(r.Ctx(), r.Actor(), id)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"knock": k})
}

func (a *API) listKnocks(r *Request) error {
	id, err := PathID[model.ConversationID](r, "id")
	if err != nil {
		return err
	}
	list, err := a.Chat.Knocks(r.Ctx(), r.Actor(), id)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"knocks": orEmpty(list)})
}

func (a *API) answerKnock(r *Request) error {
	id, err := PathID[model.ConversationID](r, "id")
	if err != nil {
		return err
	}
	knock, err := PathID[string](r, "knock_id")
	if err != nil {
		return err
	}
	var body struct {
		Accept bool `json:"accept"`
	}
	if err := r.Decode(&body); err != nil {
		return err
	}
	k, err := a.Chat.AnswerKnock(r.Ctx(), r.Actor(), id, knock, body.Accept)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"knock": k})
}
