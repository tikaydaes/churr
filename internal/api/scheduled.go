package api

import (
	"bytes"
	"encoding/json"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/sched"
)

func init() {
	register(func(a *API) {
		a.handle("listScheduled", a.listScheduled)
		a.handle("createScheduled", a.createScheduled)
		a.handle("getScheduled", a.getScheduled)
		a.handle("updateScheduled", a.updateScheduled)
		a.handle("cancelScheduled", a.cancelScheduled)
		a.handle("sendScheduledNow", a.sendScheduledNow)
		a.handle("listCommands", a.listCommands)
		a.handle("runCommand", a.runCommand)
		a.handle("listSavedReplies", a.listSavedReplies)
		a.handle("createSavedReply", a.createSavedReply)
		a.handle("updateSavedReply", a.updateSavedReply)
		a.handle("deleteSavedReply", a.deleteSavedReply)
		a.handle("createCommunityReply", a.createCommunityReply)
		a.handle("updateCommunityReply", a.updateCommunityReply)
		a.handle("deleteCommunityReply", a.deleteCommunityReply)
	})
}

// scheduledContent reads the `content` of a scheduled item: a MessageInput.
// Unknown fields are refused as everywhere, which also refuses the sealed
// fields: nothing sealed can be scheduled. Identifiers are checked here so a
// bad one is the client's mistake and never reaches the database.
func scheduledContent(raw json.RawMessage) (model.MessageInput, error) {
	var c model.MessageInput
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		return c, model.BadRequest("content could not be read: " + plain(err))
	}
	for _, f := range c.FileIDs {
		if !model.ValidID(string(f)) {
			return c, model.Invalid("content.file_ids", "That is not an identifier.")
		}
	}
	if c.ParentID != "" && !model.ValidID(string(c.ParentID)) {
		return c, model.Invalid("content.parent_id", "That is not an identifier.")
	}
	if c.Quote != nil && !model.ValidID(string(c.Quote.MessageID)) {
		return c, model.Invalid("content.quote", "quote.message_id is not an identifier.")
	}
	return c, nil
}

func (a *API) listScheduled(r *Request) error {
	page, err := r.Page()
	if err != nil {
		return err
	}
	q := r.R.URL.Query()
	var conv model.ConversationID
	if v := q.Get("conversation_id"); v != "" {
		if conv, err = BodyID[model.ConversationID]("conversation_id", v); err != nil {
			return err
		}
	}
	items, next, err := a.Sched.List(r.Ctx(), r.Actor(), q.Get("kind"), conv, page)
	if err != nil {
		return err
	}
	return r.JSON(list("scheduled", orEmpty(items), next))
}

func (a *API) createScheduled(r *Request) error {
	var body struct {
		ConversationID *string         `json:"conversation_id"`
		Kind           *string         `json:"kind"`
		Content        json.RawMessage `json:"content"`
		AboutMessageID string          `json:"about_message_id"`
		RunAt          model.Time      `json:"run_at"`
		Repeat         *model.Repeat   `json:"repeat"`
	}
	if err := r.Decode(&body); err != nil {
		return err
	}
	if body.ConversationID == nil || body.Kind == nil || len(body.Content) == 0 {
		return model.BadRequest("conversation_id, kind and content are needed.")
	}
	in := sched.NewItem{Kind: *body.Kind, RunAt: body.RunAt, Repeat: body.Repeat}
	var err error
	if in.ConversationID, err = BodyID[model.ConversationID]("conversation_id", *body.ConversationID); err != nil {
		return err
	}
	if body.AboutMessageID != "" {
		if in.AboutMessageID, err = BodyID[model.MessageID]("about_message_id", body.AboutMessageID); err != nil {
			return err
		}
	}
	if in.Content, err = scheduledContent(body.Content); err != nil {
		return err
	}
	item, err := a.Sched.Create(r.Ctx(), r.Actor(), in)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"scheduled": item})
}

func (a *API) getScheduled(r *Request) error {
	id, err := PathID[string](r, "id")
	if err != nil {
		return err
	}
	item, err := a.Sched.Get(r.Ctx(), r.Actor(), id)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"scheduled": item})
}

func (a *API) updateScheduled(r *Request) error {
	id, err := PathID[string](r, "id")
	if err != nil {
		return err
	}
	var body struct {
		Content json.RawMessage `json:"content"`
		RunAt   *model.Time     `json:"run_at"`
		Repeat  *model.Repeat   `json:"repeat"`
	}
	if err := r.Decode(&body); err != nil {
		return err
	}
	ch := sched.Change{RunAt: body.RunAt, Repeat: body.Repeat}
	// Absent and null mean the same: left as it is.
	if ch.RunAt != nil && ch.RunAt.IsZero() {
		ch.RunAt = nil
	}
	if len(body.Content) > 0 && string(body.Content) != "null" {
		c, err := scheduledContent(body.Content)
		if err != nil {
			return err
		}
		ch.Content = &c
	}
	item, err := a.Sched.Update(r.Ctx(), r.Actor(), id, ch)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"scheduled": item})
}

func (a *API) cancelScheduled(r *Request) error {
	id, err := PathID[string](r, "id")
	if err != nil {
		return err
	}
	if err := a.Sched.Cancel(r.Ctx(), r.Actor(), id); err != nil {
		return err
	}
	return r.NoContent()
}

func (a *API) sendScheduledNow(r *Request) error {
	id, err := PathID[string](r, "id")
	if err != nil {
		return err
	}
	item, msg, err := a.Sched.SendNow(r.Ctx(), r.Actor(), id)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"scheduled": item, "message": msg})
}

func (a *API) listCommands(r *Request) error {
	v := r.R.URL.Query().Get("conversation_id")
	if v == "" {
		return model.Invalid("conversation_id", "conversation_id is needed: the conversation being typed in.")
	}
	conv, err := BodyID[model.ConversationID]("conversation_id", v)
	if err != nil {
		return err
	}
	commands, err := a.Commands.List(r.Ctx(), r.Actor(), conv)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"commands": orEmpty(commands)})
}

func (a *API) runCommand(r *Request) error {
	conv, err := PathID[model.ConversationID](r, "id")
	if err != nil {
		return err
	}
	var body struct {
		Name     *string `json:"name"`
		Text     string  `json:"text"`
		ParentID string  `json:"parent_id"`
	}
	if err := r.Decode(&body); err != nil {
		return err
	}
	if body.Name == nil {
		return model.BadRequest("name is needed: the command, without its slash.")
	}
	var parent model.MessageID
	if body.ParentID != "" {
		if parent, err = BodyID[model.MessageID]("parent_id", body.ParentID); err != nil {
			return err
		}
	}
	ran, err := a.Commands.Run(r.Ctx(), r.Actor(), conv, parent, *body.Name, body.Text)
	if err != nil {
		return err
	}
	out := map[string]any{}
	if ran.Message != nil {
		out["message"] = ran.Message
	}
	if ran.InteractionID != "" {
		out["interaction_id"] = ran.InteractionID
	}
	return r.JSON(out)
}

func (a *API) listSavedReplies(r *Request) error {
	replies, err := a.Commands.Replies(r.Ctx(), r.Actor())
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"saved_replies": orEmpty(replies)})
}

// savedReplyBody is the body of creating and changing a saved reply.
type savedReplyBody struct {
	Name *string `json:"name"`
	Text *string `json:"text"`
}

func (a *API) createReply(r *Request, community bool) error {
	var body savedReplyBody
	if err := r.Decode(&body); err != nil {
		return err
	}
	if body.Name == nil || body.Text == nil {
		return model.BadRequest("name and text are needed.")
	}
	reply, err := a.Commands.CreateReply(r.Ctx(), r.Actor(), community, *body.Name, *body.Text)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"saved_reply": reply})
}

func (a *API) updateReply(r *Request, community bool) error {
	id, err := PathID[string](r, "id")
	if err != nil {
		return err
	}
	var body savedReplyBody
	if err := r.Decode(&body); err != nil {
		return err
	}
	reply, err := a.Commands.UpdateReply(r.Ctx(), r.Actor(), community, id, body.Name, body.Text)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"saved_reply": reply})
}

func (a *API) deleteReply(r *Request, community bool) error {
	id, err := PathID[string](r, "id")
	if err != nil {
		return err
	}
	if err := a.Commands.DeleteReply(r.Ctx(), r.Actor(), community, id); err != nil {
		return err
	}
	return r.NoContent()
}

func (a *API) createSavedReply(r *Request) error     { return a.createReply(r, false) }
func (a *API) updateSavedReply(r *Request) error     { return a.updateReply(r, false) }
func (a *API) deleteSavedReply(r *Request) error     { return a.deleteReply(r, false) }
func (a *API) createCommunityReply(r *Request) error { return a.createReply(r, true) }
func (a *API) updateCommunityReply(r *Request) error { return a.updateReply(r, true) }
func (a *API) deleteCommunityReply(r *Request) error { return a.deleteReply(r, true) }
