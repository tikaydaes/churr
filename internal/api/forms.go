package api

import (
	"net/http"

	"github.com/tikaydaes/churr/internal/forms"
	"github.com/tikaydaes/churr/internal/model"
)

func init() {
	register(func(a *API) {
		a.handle("answerForm", a.answerForm)
		a.handle("withdrawFormAnswer", a.withdrawFormAnswer)
		a.handle("pressFormButton", a.pressFormButton)
		a.handle("addFormOption", a.addFormOption)
		a.handle("closeForm", a.closeForm)
		a.handle("listFormResults", a.listFormResults)
		a.handle("downloadFormResults", a.downloadFormResults)
		a.handle("setChecklistItem", a.setChecklistItem)
		a.handle("getChecklistItemHistory", a.getChecklistItemHistory)
	})
}

// formResult is the answer of answering a form or pressing a button.
func formResult(r *Request, res forms.Result) error {
	out := map[string]any{"form": res.Form}
	if res.ConversationID != "" {
		out["conversation_id"] = res.ConversationID
	}
	if res.InteractionID != "" {
		out["interaction_id"] = res.InteractionID
	}
	return r.JSON(out)
}

func (a *API) answerForm(r *Request) error {
	id, err := PathID[model.MessageID](r, "id")
	if err != nil {
		return err
	}
	var body struct {
		Answers *map[string]any `json:"answers"`
	}
	if err := r.Decode(&body); err != nil {
		return err
	}
	if body.Answers == nil {
		return model.BadRequest("answers is needed: the values by field id.")
	}
	res, err := a.Forms.Answer(r.Ctx(), r.Actor(), id, *body.Answers)
	if err != nil {
		return err
	}
	return formResult(r, res)
}

func (a *API) withdrawFormAnswer(r *Request) error {
	id, err := PathID[model.MessageID](r, "id")
	if err != nil {
		return err
	}
	form, err := a.Forms.Withdraw(r.Ctx(), r.Actor(), id)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"form": form})
}

func (a *API) pressFormButton(r *Request) error {
	id, err := PathID[model.MessageID](r, "id")
	if err != nil {
		return err
	}
	button := r.R.PathValue("button")
	if !model.ValidShortID(button) {
		// It could name no button.
		return model.ErrNotFound
	}
	var body struct {
		Answers map[string]any `json:"answers"`
	}
	if err := r.Decode(&body); err != nil {
		return err
	}
	res, err := a.Forms.Press(r.Ctx(), r.Actor(), id, button, body.Answers)
	if err != nil {
		return err
	}
	return formResult(r, res)
}

func (a *API) addFormOption(r *Request) error {
	id, err := PathID[model.MessageID](r, "id")
	if err != nil {
		return err
	}
	var body struct {
		FieldID *string `json:"field_id"`
		Label   *string `json:"label"`
	}
	if err := r.Decode(&body); err != nil {
		return err
	}
	if body.FieldID == nil || body.Label == nil {
		return model.BadRequest("field_id and label are needed.")
	}
	form, err := a.Forms.AddOption(r.Ctx(), r.Actor(), id, *body.FieldID, *body.Label)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"form": form})
}

func (a *API) closeForm(r *Request) error {
	id, err := PathID[model.MessageID](r, "id")
	if err != nil {
		return err
	}
	form, err := a.Forms.Close(r.Ctx(), r.Actor(), id)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"form": form})
}

func (a *API) listFormResults(r *Request) error {
	id, err := PathID[model.MessageID](r, "id")
	if err != nil {
		return err
	}
	page, err := r.Page()
	if err != nil {
		return err
	}
	answers, next, err := a.Forms.Results(r.Ctx(), r.Actor(), id, page)
	if err != nil {
		return err
	}
	return r.JSON(list("answers", orEmpty(answers), next))
}

func (a *API) downloadFormResults(r *Request) error {
	id, err := PathID[model.MessageID](r, "id")
	if err != nil {
		return err
	}
	file, err := a.Forms.CSV(r.Ctx(), r.Actor(), id)
	if err != nil {
		return err
	}
	h := r.W.Header()
	h.Set("Content-Type", "text/csv; charset=utf-8")
	h.Set("Content-Disposition", `attachment; filename="answers.csv"`)
	// The answers are for the asker alone and change as people answer.
	h.Set("Cache-Control", "private, no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	r.W.WriteHeader(http.StatusOK)
	r.W.Write(file)
	return nil
}

func (a *API) setChecklistItem(r *Request) error {
	id, err := PathID[model.MessageID](r, "id")
	if err != nil {
		return err
	}
	item := r.R.PathValue("item")
	if !model.ValidShortID(item) {
		return model.ErrNotFound
	}
	var body struct {
		Checked *bool `json:"checked"`
	}
	if err := r.Decode(&body); err != nil {
		return err
	}
	if body.Checked == nil {
		return model.BadRequest("checked is needed: true or false.")
	}
	task, err := a.Chat.Tick(r.Ctx(), r.Actor(), id, item, *body.Checked)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"task": task})
}

func (a *API) getChecklistItemHistory(r *Request) error {
	id, err := PathID[model.MessageID](r, "id")
	if err != nil {
		return err
	}
	item := r.R.PathValue("item")
	if !model.ValidShortID(item) {
		return model.ErrNotFound
	}
	changes, err := a.Chat.TaskHistory(r.Ctx(), r.Actor(), id, item)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"changes": orEmpty(changes)})
}
