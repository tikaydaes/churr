package api

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/tikaydaes/churr/internal/chat"
	"github.com/tikaydaes/churr/internal/live"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/search"
	"github.com/tikaydaes/churr/internal/store"
)

func init() {
	register(func(a *API) {
		a.handle("listMessages", a.listMessages)
		a.handle("postMessage", a.postMessage)
		a.handle("checkBroadMentions", a.checkBroadMentions)
		a.handle("getMessage", a.getMessage)
		a.handle("getThread", a.getThread)
		a.handle("editMessage", a.editMessage)
		a.handle("deleteMessage", a.deleteMessage)
		a.handle("getMessageHistory", a.getMessageHistory)
		a.handle("listReactions", a.listReactions)
		a.handle("addReaction", a.addReaction)
		a.handle("removeReaction", a.removeReaction)
		a.handle("search", a.search)
		a.handle("openEvents", a.openEvents)
		a.handle("listEvents", a.listEvents)
	})
}

func (a *API) listMessages(r *Request) error {
	id, err := PathID[model.ConversationID](r, "id")
	if err != nil {
		return err
	}
	o := store.MessageList{}
	if o.SeqPage, err = r.SeqPage(); err != nil {
		return err
	}
	if o.TopLevel, err = r.queryBool("top_level"); err != nil {
		return err
	}
	if o.Around, _, err = r.queryInt("around"); err != nil {
		return err
	}
	msgs, states, more, err := a.Chat.Messages(r.Ctx(), r.Actor(), id, o)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"messages": orEmpty(msgs), "thread_states": orEmpty(states), "has_more": more})
}

// messageInput is the contract's MessageInput.
type messageInput struct {
	ClientID string               `json:"client_id"`
	Text     string               `json:"text"`
	FileIDs  []string             `json:"file_ids"`
	ParentID string               `json:"parent_id"`
	Quote    *model.Quote         `json:"quote"`
	Cards    []model.Card         `json:"cards"`
	Form     *model.FormInput     `json:"form"`
	Sealed   *model.SealedPayload `json:"sealed"`
	Quiet    bool                 `json:"quiet"`
	Action   bool                 `json:"action"`
	Chance   *model.ChanceInput   `json:"chance"`
	// The author's zone and the instants the author accepted (ruling R69).
	TimeZone     string              `json:"time_zone"`
	InstantHints []model.InstantHint `json:"instant_hints"`
}

// instants is what a body says about the dates and times in its text.
func (b messageInput) instants() chat.Instants {
	return chat.Instants{TimeZone: b.TimeZone, Hints: b.InstantHints}
}

func fileIDs(raw []string) ([]model.FileID, error) {
	out := make([]model.FileID, 0, len(raw))
	for _, s := range raw {
		id, err := BodyID[model.FileID]("file_ids", s)
		if err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, nil
}

func (a *API) postMessage(r *Request) error {
	id, err := PathID[model.ConversationID](r, "id")
	if err != nil {
		return err
	}
	var body messageInput
	if err := r.Decode(&body); err != nil {
		return err
	}
	in := chat.NewMessage{ConversationID: id, ClientID: body.ClientID, Text: body.Text, Quote: body.Quote, Cards: body.Cards, Form: body.Form,
		Action: body.Action, Chance: body.Chance, Instants: body.instants()}
	if body.Chance != nil && (body.Sealed != nil || body.Quiet) {
		return model.Invalid("chance", "The server cannot write into a sealed chat, so it cannot draw a result there.")
	}
	if body.Action && (body.Sealed != nil || body.Quiet) {
		return model.Invalid("action", "In a sealed chat the action mark goes inside the sealed content.")
	}
	if body.Sealed != nil || body.Quiet {
		// A sealed message: the sealed package stores it (sealed.go).
		return a.postSealedMessage(r, id, body)
	}
	if body.ParentID != "" {
		if in.ParentID, err = BodyID[model.MessageID]("parent_id", body.ParentID); err != nil {
			return err
		}
	}
	if in.Quote != nil && !model.ValidID(string(in.Quote.MessageID)) {
		return model.Invalid("quote", "quote.message_id is not an identifier.")
	}
	if in.FileIDs, err = fileIDs(body.FileIDs); err != nil {
		return err
	}
	posted, err := a.Chat.Post(r.Ctx(), r.Actor(), in)
	if err != nil {
		return err
	}
	out := map[string]any{"message": posted.Message}
	if len(posted.CannotRead) > 0 {
		out["cannot_read_ids"] = posted.CannotRead
	}
	if posted.BroadRefused {
		// The author may not use broad mentions here: the words were
		// stored as plain text and nobody was notified (ruling R64).
		out["broad_mentions_refused"] = true
	}
	// 201 for a new message; 200 for the one already made with this
	// client_id.
	status := http.StatusCreated
	if !posted.Created {
		status = http.StatusOK
	}
	return r.JSONStatus(status, out)
}

func (a *API) getMessage(r *Request) error {
	id, err := PathID[model.MessageID](r, "id")
	if err != nil {
		return err
	}
	m, err := a.Chat.Message(r.Ctx(), r.Actor(), id)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"message": m})
}

func (a *API) getThread(r *Request) error {
	id, err := PathID[model.MessageID](r, "id")
	if err != nil {
		return err
	}
	page, err := r.SeqPage()
	if err != nil {
		return err
	}
	parent, replies, state, more, err := a.Chat.Thread(r.Ctx(), r.Actor(), id, page)
	if err != nil {
		return err
	}
	out := map[string]any{"parent": parent, "messages": orEmpty(replies), "has_more": more}
	if state != nil {
		out["thread_state"] = state
	}
	return r.JSON(out)
}

func (a *API) editMessage(r *Request) error {
	id, err := PathID[model.MessageID](r, "id")
	if err != nil {
		return err
	}
	var body struct {
		Text    *string              `json:"text"`
		FileIDs *[]string            `json:"file_ids"`
		Cards   *[]model.Card        `json:"cards"`
		Form    *model.FormInput     `json:"form"`
		Sealed  *model.SealedPayload `json:"sealed"`
		// The author's zone and the instants the author accepted, for
		// the new text (ruling R69).
		TimeZone     string              `json:"time_zone"`
		InstantHints []model.InstantHint `json:"instant_hints"`
	}
	if err := r.Decode(&body); err != nil {
		return err
	}
	if body.Sealed != nil {
		// The server keeps no versions of a sealed chat: an edit travels
		// as a new sealed message (docs/SEALED.md section 8.3).
		return model.BadRequest("A sealed message is not edited in place. Post an `edit` as a new sealed message.")
	}
	in := chat.EditInput{Text: body.Text, Cards: body.Cards, Form: body.Form,
		Instants: chat.Instants{TimeZone: body.TimeZone, Hints: body.InstantHints}}
	if body.FileIDs != nil {
		ids, err := fileIDs(*body.FileIDs)
		if err != nil {
			return err
		}
		in.FileIDs = &ids
	}
	m, err := a.Chat.Edit(r.Ctx(), r.Actor(), id, in)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"message": m})
}

func (a *API) deleteMessage(r *Request) error {
	id, err := PathID[model.MessageID](r, "id")
	if err != nil {
		return err
	}
	purge, err := r.queryBool("purge")
	if err != nil {
		return err
	}
	if purge {
		// An admin's delete and purge in one step.
		m, err := a.Chat.DeleteAndPurge(r.Ctx(), r.Actor(), id)
		if err != nil {
			return err
		}
		return r.JSON(map[string]any{"message": m})
	}
	m, err := a.Chat.Delete(r.Ctx(), r.Actor(), id)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"message": m})
}

func (a *API) getMessageHistory(r *Request) error {
	id, err := PathID[model.MessageID](r, "id")
	if err != nil {
		return err
	}
	versions, err := a.Chat.History(r.Ctx(), r.Actor(), id)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"versions": orEmpty(versions)})
}

func (a *API) listReactions(r *Request) error {
	id, err := PathID[model.MessageID](r, "id")
	if err != nil {
		return err
	}
	reactions, err := a.Chat.Reactions(r.Ctx(), r.Actor(), id)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"reactions": orEmpty(reactions)})
}

// emojiParam reads the emoji from the path, where it is percent-encoded.
func emojiParam(r *Request) (string, error) {
	e, err := url.PathUnescape(r.R.PathValue("emoji"))
	if err != nil {
		return "", model.Invalid("emoji", "A reaction is one emoji.")
	}
	return e, nil
}

func (a *API) addReaction(r *Request) error {
	id, err := PathID[model.MessageID](r, "id")
	if err != nil {
		return err
	}
	emoji, err := emojiParam(r)
	if err != nil {
		return err
	}
	reaction, err := a.Chat.React(r.Ctx(), r.Actor(), id, emoji)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"reaction": reaction})
}

func (a *API) removeReaction(r *Request) error {
	id, err := PathID[model.MessageID](r, "id")
	if err != nil {
		return err
	}
	emoji, err := emojiParam(r)
	if err != nil {
		return err
	}
	if err := a.Chat.Unreact(r.Ctx(), r.Actor(), id, emoji); err != nil {
		return err
	}
	return r.NoContent()
}

func (a *API) search(r *Request) error {
	page, err := r.Page()
	if err != nil {
		return err
	}
	q := r.R.URL.Query()
	in := search.Query{Q: q.Get("q")}
	if v := q.Get("in"); v != "" {
		if in.In, err = BodyID[model.ConversationID]("in", v); err != nil {
			return err
		}
	}
	if v := q.Get("from"); v != "" {
		if in.From, err = BodyID[model.AccountID]("from", v); err != nil {
			return err
		}
	}
	if in.Since, err = r.queryTime("since"); err != nil {
		return err
	}
	if in.Until, err = r.queryTime("until"); err != nil {
		return err
	}
	if in.HasFile, err = r.queryBool("has_file"); err != nil {
		return err
	}
	msgs, next, err := a.Search.Search(r.Ctx(), r.Actor(), in, page)
	if err != nil {
		return err
	}
	if err := a.Chat.PresentFor(r.Ctx(), r.Actor(), msgs); err != nil {
		return err
	}
	terms := search.Parse(in.Q)
	results := make([]map[string]any, 0, len(msgs))
	for _, m := range msgs {
		result := map[string]any{"message": m}
		// Cut from what the index holds, so a linked channel's name is
		// in it and its identifier is not.
		if s := search.Snippet(m.SearchText, terms); s != "" {
			result["snippet"] = s
		}
		results = append(results, result)
	}
	return r.JSON(list("results", results, next))
}

// openEvents upgrades to the live connection (docs/EVENTS.md section 1).
func (a *API) openEvents(r *Request) error {
	asked := false
	for _, h := range r.R.Header.Values("Sec-WebSocket-Protocol") {
		for _, p := range strings.Split(h, ",") {
			if strings.TrimSpace(p) == live.Subprotocol {
				asked = true
			}
		}
	}
	if !asked {
		return model.BadRequest("Ask for the subprotocol churr.v1.")
	}
	// The upgrade is a GET, so the origin check that guards other cookie
	// requests is applied here by hand.
	if r.Who.Cookie && r.R.Header.Get("Origin") != a.Origin {
		return model.ErrForbidden.With("This connection did not come from this server's own pages.")
	}
	a.Live.Serve(r.W, r.R, r.Who.Session, r.Who.Account)
	return nil
}

func (a *API) listEvents(r *Request) error {
	id, err := PathID[model.ConversationID](r, "id")
	if err != nil {
		return err
	}
	after, found, err := r.queryInt("after")
	if err != nil {
		return err
	}
	if !found {
		return model.Invalid("after", "after is needed: the last seq the client has.")
	}
	until, _, err := r.queryInt("until")
	if err != nil {
		return err
	}
	limit, err := r.limit()
	if err != nil {
		return err
	}
	events, more, reload, err := a.Chat.Events(r.Ctx(), r.Actor(), id, after, until, limit)
	if err != nil {
		return err
	}
	out := map[string]any{"events": events, "has_more": more}
	if reload {
		out["reload"] = true
	}
	return r.JSON(out)
}
