package api

import (
	"net/http"

	"github.com/tikaydaes/churr/internal/chat"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

func init() {
	register(func(a *API) {
		a.handle("listConversations", a.listConversations)
		a.handle("getConversation", a.getConversation)
		a.handle("browseChannels", a.browseChannels)
		a.handle("createChannel", a.createChannel)
		a.handle("openChat", a.openChat)
		a.handle("joinConversation", a.joinConversation)
		a.handle("leaveConversation", a.leaveConversation)
		a.handle("listMembers", a.listMembers)
		a.handle("addMembers", a.addMembers)
		a.handle("setReadPosition", a.setReadPosition)
	})
}

// accountIDs checks a list of account identifiers from a body.
func accountIDs(field string, raw []string) ([]model.AccountID, error) {
	out := make([]model.AccountID, 0, len(raw))
	for _, s := range raw {
		id, err := BodyID[model.AccountID](field, s)
		if err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, nil
}

func (a *API) listConversations(r *Request) error {
	page, err := r.Page()
	if err != nil {
		return err
	}
	f := store.ConversationFilter{Kind: r.R.URL.Query().Get("kind")}
	if f.Kind != "" && f.Kind != model.KindChannel && f.Kind != model.KindSealed {
		return model.Invalid("kind", "kind is channel or sealed.")
	}
	if f.IncludeArchived, err = r.queryBool("archived"); err != nil {
		return err
	}
	convs, next, err := a.Chat.Conversations(r.Ctx(), r.Actor(), f, page)
	if err != nil {
		return err
	}
	return r.JSON(list("conversations", orEmpty(convs), next))
}

func (a *API) getConversation(r *Request) error {
	id, err := PathID[model.ConversationID](r, "id")
	if err != nil {
		return err
	}
	c, err := a.Chat.Conversation(r.Ctx(), r.Actor(), id)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"conversation": c})
}

func (a *API) browseChannels(r *Request) error {
	page, err := r.Page()
	if err != nil {
		return err
	}
	q := r.R.URL.Query()
	f := store.BrowseFilter{Query: q.Get("q")}
	if s := q.Get("section_id"); s != "" {
		if f.SectionID, err = BodyID[model.SectionID]("section_id", s); err != nil {
			return err
		}
	}
	if s := q.Get("parent_id"); s != "" {
		if f.ParentID, err = BodyID[model.ConversationID]("parent_id", s); err != nil {
			return err
		}
	}
	if f.IncludeArchived, err = r.queryBool("archived"); err != nil {
		return err
	}
	convs, next, err := a.Chat.Browse(r.Ctx(), r.Actor(), f, page)
	if err != nil {
		return err
	}
	return r.JSON(list("conversations", orEmpty(convs), next))
}

func (a *API) createChannel(r *Request) error {
	in, _, err := newChannelBody(r)
	if err != nil {
		return err
	}
	c, err := a.Chat.CreateChannel(r.Ctx(), r.Actor(), in)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"conversation": c})
}

// newChannelBody reads what createChannel and requestChannel take: the
// new channel, and for a request a note to whoever decides it.
func newChannelBody(r *Request) (chat.NewChannel, string, error) {
	var body struct {
		Name         string      `json:"name"`
		Private      *bool       `json:"private"`
		Access       string      `json:"access"`
		Topic        string      `json:"topic"`
		Welcome      string      `json:"welcome"`
		DefaultLevel model.Level `json:"default_level"`
		SectionID    string      `json:"section_id"`
		AccountIDs   []string    `json:"account_ids"`
		ParentID     string      `json:"parent_id"`
		Announcement bool        `json:"announcement"`
		Replies      bool        `json:"announcement_replies"`
		Note         string      `json:"note"`
	}
	if err := r.Decode(&body); err != nil {
		return chat.NewChannel{}, "", err
	}
	switch body.Access {
	case "":
		if body.Private == nil {
			return chat.NewChannel{}, "", model.Invalid("private", "Say whether the channel is private.")
		}
	case model.AccessPublic, model.AccessKnock, model.AccessPrivate:
		p := body.Access != model.AccessPublic
		if body.Private != nil && *body.Private != p {
			return chat.NewChannel{}, "", model.Invalid("private", "private and access say different things.")
		}
		body.Private = &p
	default:
		return chat.NewChannel{}, "", model.Invalid("access", "access is public, knock or private.")
	}
	in := chat.NewChannel{Name: body.Name, Private: *body.Private, Knock: body.Access == model.AccessKnock, Topic: body.Topic, Welcome: body.Welcome, DefaultLevel: body.DefaultLevel,
		Announcement: body.Announcement, AnnouncementReplies: body.Replies}
	var err error
	if body.SectionID != "" {
		if in.SectionID, err = BodyID[model.SectionID]("section_id", body.SectionID); err != nil {
			return in, "", err
		}
	}
	if in.AccountIDs, err = accountIDs("account_ids", body.AccountIDs); err != nil {
		return in, "", err
	}
	if body.ParentID != "" {
		if in.ParentID, err = BodyID[model.ConversationID]("parent_id", body.ParentID); err != nil {
			return in, "", err
		}
	}
	return in, body.Note, nil
}

func (a *API) openChat(r *Request) error {
	var body struct {
		AccountIDs []string `json:"account_ids"`
	}
	if err := r.Decode(&body); err != nil {
		return err
	}
	ids, err := accountIDs("account_ids", body.AccountIDs)
	if err != nil {
		return err
	}
	c, created, err := a.Chat.FindOrCreateChat(r.Ctx(), r.Actor(), ids)
	if err != nil {
		return err
	}
	// The contract gives this operation two success statuses: 200 when
	// the channel already existed and 201 when it was made.
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	return r.JSONStatus(status, map[string]any{"conversation": c})
}

func (a *API) joinConversation(r *Request) error {
	id, err := PathID[model.ConversationID](r, "id")
	if err != nil {
		return err
	}
	c, err := a.Chat.Join(r.Ctx(), r.Actor(), id)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"conversation": c})
}

func (a *API) leaveConversation(r *Request) error {
	id, err := PathID[model.ConversationID](r, "id")
	if err != nil {
		return err
	}
	if err := a.Chat.Leave(r.Ctx(), r.Actor(), id); err != nil {
		return err
	}
	return r.NoContent()
}

func (a *API) listMembers(r *Request) error {
	id, err := PathID[model.ConversationID](r, "id")
	if err != nil {
		return err
	}
	page, err := r.Page()
	if err != nil {
		return err
	}
	level := model.Level(r.R.URL.Query().Get("level"))
	if level != "" && !level.Valid() {
		return model.Invalid("level", "A level is reader, member, moderator or manager.")
	}
	members, next, err := a.Chat.Members(r.Ctx(), r.Actor(), id, level, page)
	if err != nil {
		return err
	}
	return r.JSON(list("members", orEmpty(members), next))
}

func (a *API) addMembers(r *Request) error {
	id, err := PathID[model.ConversationID](r, "id")
	if err != nil {
		return err
	}
	var body struct {
		AccountIDs []string    `json:"account_ids"`
		Level      model.Level `json:"level"`
	}
	if err := r.Decode(&body); err != nil {
		return err
	}
	ids, err := accountIDs("account_ids", body.AccountIDs)
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		return model.Invalid("account_ids", "Name at least one account.")
	}
	members, err := a.Chat.AddMembers(r.Ctx(), r.Actor(), id, ids, body.Level)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"members": orEmpty(members)})
}

func (a *API) setReadPosition(r *Request) error {
	id, err := PathID[model.ConversationID](r, "id")
	if err != nil {
		return err
	}
	var body struct {
		Seq *int64 `json:"seq"`
	}
	if err := r.Decode(&body); err != nil {
		return err
	}
	if body.Seq == nil {
		return model.Invalid("seq", "seq is needed.")
	}
	m, err := a.Chat.MarkRead(r.Ctx(), r.Actor(), id, *body.Seq)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"membership": m})
}
