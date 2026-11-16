package api

import (
	"bytes"
	"encoding/json"

	"github.com/tikaydaes/churr/internal/chat"
	"github.com/tikaydaes/churr/internal/model"
)

// The rest of conversations: settings, privacy and the lock, archiving,
// levels and removals, groups, pins, and moving a thread out.

func init() {
	register(func(a *API) {
		a.handle("listSections", a.listSections)
		a.handle("listReferencePatterns", a.listReferencePatterns)
		a.handle("createReferencePattern", a.createReferencePattern)
		a.handle("updateReferencePattern", a.updateReferencePattern)
		a.handle("deleteReferencePattern", a.deleteReferencePattern)
		a.handle("updateConversation", a.updateConversation)
		a.handle("archiveConversation", a.archiveConversation)
		a.handle("unarchiveConversation", a.unarchiveConversation)
		a.handle("lockConversation", a.lockConversation)
		a.handle("setMemberLevel", a.setMemberLevel)
		a.handle("setMemberMayPost", a.setMemberMayPost)
		a.handle("removeMember", a.removeMember)
		a.handle("listChannelGroups", a.listChannelGroups)
		a.handle("addChannelGroup", a.addChannelGroup)
		a.handle("removeChannelGroup", a.removeChannelGroup)
		a.handle("listPins", a.listPins)
		a.handle("pinMessage", a.pinMessage)
		a.handle("unpinMessage", a.unpinMessage)
		a.handle("moveThreadToChannel", a.moveThreadToChannel)
		a.handle("removeLinkPreview", a.removeLinkPreview)
	})
}

// optionalID reads a field that may be absent (nil), null (a pointer to
// "", which clears) or an identifier.
func optionalID[T ~string](field string, raw json.RawMessage) (*T, error) {
	if raw == nil {
		return nil, nil
	}
	var empty T
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return &empty, nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, model.Invalid(field, "That is not an identifier.")
	}
	if s == "" {
		return &empty, nil
	}
	id, err := BodyID[T](field, s)
	return &id, err
}

// refPatternBody is the contract's ReferencePatternInput.
type refPatternBody struct {
	ConversationID string  `json:"conversation_id"`
	Name           *string `json:"name"`
	Prefix         *string `json:"prefix"`
	Expression     *string `json:"expression"`
	URLTemplate    *string `json:"url_template"`
}

func (b refPatternBody) input() chat.ReferencePatternInput {
	return chat.ReferencePatternInput{Name: b.Name, Prefix: b.Prefix, Expression: b.Expression, URLTemplate: b.URLTemplate}
}

func (a *API) listReferencePatterns(r *Request) error {
	var conv model.ConversationID
	if v := r.R.URL.Query().Get("conversation_id"); v != "" {
		id, err := BodyID[model.ConversationID]("conversation_id", v)
		if err != nil {
			return err
		}
		conv = id
	}
	list, err := a.Chat.ReferencePatterns(r.Ctx(), r.Actor(), conv)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"patterns": orEmpty(list)})
}

func (a *API) createReferencePattern(r *Request) error {
	var body refPatternBody
	if err := r.Decode(&body); err != nil {
		return err
	}
	var conv model.ConversationID
	if body.ConversationID != "" {
		id, err := BodyID[model.ConversationID]("conversation_id", body.ConversationID)
		if err != nil {
			return err
		}
		conv = id
	}
	p, err := a.Chat.CreateReferencePattern(r.Ctx(), r.Actor(), conv, body.input())
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"pattern": p})
}

func (a *API) updateReferencePattern(r *Request) error {
	id, err := PathID[string](r, "id")
	if err != nil {
		return err
	}
	var body refPatternBody
	if err := r.Decode(&body); err != nil {
		return err
	}
	p, err := a.Chat.UpdateReferencePattern(r.Ctx(), r.Actor(), id, body.input())
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"pattern": p})
}

func (a *API) deleteReferencePattern(r *Request) error {
	id, err := PathID[string](r, "id")
	if err != nil {
		return err
	}
	if err := a.Chat.DeleteReferencePattern(r.Ctx(), r.Actor(), id); err != nil {
		return err
	}
	return r.NoContent()
}

func (a *API) listSections(r *Request) error {
	sections, err := a.Chat.Sections(r.Ctx(), r.Actor())
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"sections": orEmpty(sections)})
}

func (a *API) updateConversation(r *Request) error {
	id, err := PathID[model.ConversationID](r, "id")
	if err != nil {
		return err
	}
	var body struct {
		Name             *string         `json:"name"`
		Topic            *string         `json:"topic"`
		Welcome          *string         `json:"welcome"`
		Private          *bool           `json:"private"`
		Access           *string         `json:"access"`
		DefaultLevel     *model.Level    `json:"default_level"`
		SectionID        json.RawMessage `json:"section_id"`
		RetentionSeconds *int64          `json:"retention_seconds"`
		SlowModeSeconds  *int            `json:"slow_mode_seconds"`
		BroadMentions    *string         `json:"broad_mentions"`
		LazyDelivery     *string         `json:"lazy_delivery"`
		Announcement     *bool           `json:"announcement"`
		Replies          *bool           `json:"announcement_replies"`
		ParentID         json.RawMessage `json:"parent_id"`
	}
	if err := r.Decode(&body); err != nil {
		return err
	}
	ch := chat.ConversationChange{Name: body.Name, Topic: body.Topic, Welcome: body.Welcome, Private: body.Private, Access: body.Access,
		DefaultLevel: body.DefaultLevel, RetentionSeconds: body.RetentionSeconds, SlowModeSeconds: body.SlowModeSeconds,
		BroadMentions: body.BroadMentions, LazyDelivery: body.LazyDelivery, Announcement: body.Announcement,
		AnnouncementReplies: body.Replies}
	if ch.SectionID, err = optionalID[model.SectionID]("section_id", body.SectionID); err != nil {
		return err
	}
	if ch.ParentID, err = optionalID[model.ConversationID]("parent_id", body.ParentID); err != nil {
		return err
	}
	c, err := a.Chat.UpdateConversation(r.Ctx(), r.Actor(), id, ch)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"conversation": c})
}

func (a *API) archive(r *Request, archived bool) error {
	id, err := PathID[model.ConversationID](r, "id")
	if err != nil {
		return err
	}
	c, err := a.Chat.Archive(r.Ctx(), r.Actor(), id, archived)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"conversation": c})
}

func (a *API) archiveConversation(r *Request) error   { return a.archive(r, true) }
func (a *API) unarchiveConversation(r *Request) error { return a.archive(r, false) }

func (a *API) lockConversation(r *Request) error {
	id, err := PathID[model.ConversationID](r, "id")
	if err != nil {
		return err
	}
	c, err := a.Chat.Lock(r.Ctx(), r.Actor(), id)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"conversation": c})
}

// memberPath reads the conversation and account of a member's path.
func memberPath(r *Request) (model.ConversationID, model.AccountID, error) {
	id, err := PathID[model.ConversationID](r, "id")
	if err != nil {
		return "", "", err
	}
	account, err := PathID[model.AccountID](r, "account_id")
	return id, account, err
}

func (a *API) setMemberLevel(r *Request) error {
	id, account, err := memberPath(r)
	if err != nil {
		return err
	}
	var body struct {
		Level model.Level `json:"level"`
	}
	if err := r.Decode(&body); err != nil {
		return err
	}
	m, err := a.Chat.SetMemberLevel(r.Ctx(), r.Actor(), id, account, body.Level)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"member": m})
}

func (a *API) setMemberMayPost(r *Request) error {
	id, account, err := memberPath(r)
	if err != nil {
		return err
	}
	var body struct {
		MayPost *bool `json:"may_post"`
	}
	if err := r.Decode(&body); err != nil {
		return err
	}
	if body.MayPost == nil {
		return model.Invalid("may_post", "Say whether the member may post.")
	}
	m, err := a.Chat.SetMayPost(r.Ctx(), r.Actor(), id, account, *body.MayPost)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"member": m})
}

func (a *API) removeMember(r *Request) error {
	id, account, err := memberPath(r)
	if err != nil {
		return err
	}
	if err := a.Chat.RemoveMember(r.Ctx(), r.Actor(), id, account); err != nil {
		return err
	}
	return r.NoContent()
}

func (a *API) listChannelGroups(r *Request) error {
	id, err := PathID[model.ConversationID](r, "id")
	if err != nil {
		return err
	}
	groups, err := a.Chat.ChannelGroups(r.Ctx(), r.Actor(), id)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"groups": orEmpty(groups)})
}

func groupPath(r *Request) (model.ConversationID, model.GroupID, error) {
	id, err := PathID[model.ConversationID](r, "id")
	if err != nil {
		return "", "", err
	}
	group, err := PathID[model.GroupID](r, "group_id")
	return id, group, err
}

func (a *API) addChannelGroup(r *Request) error {
	id, group, err := groupPath(r)
	if err != nil {
		return err
	}
	var body struct {
		Level model.Level `json:"level"`
	}
	if err := r.Decode(&body); err != nil {
		return err
	}
	cg, err := a.Chat.AddChannelGroup(r.Ctx(), r.Actor(), id, group, body.Level)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"group": cg})
}

func (a *API) removeChannelGroup(r *Request) error {
	id, group, err := groupPath(r)
	if err != nil {
		return err
	}
	if err := a.Chat.RemoveChannelGroup(r.Ctx(), r.Actor(), id, group); err != nil {
		return err
	}
	return r.NoContent()
}

func (a *API) listPins(r *Request) error {
	id, err := PathID[model.ConversationID](r, "id")
	if err != nil {
		return err
	}
	pins, err := a.Chat.Pins(r.Ctx(), r.Actor(), id)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"pins": orEmpty(pins)})
}

func pinPath(r *Request) (model.ConversationID, model.MessageID, error) {
	id, err := PathID[model.ConversationID](r, "id")
	if err != nil {
		return "", "", err
	}
	msg, err := PathID[model.MessageID](r, "message_id")
	return id, msg, err
}

func (a *API) pinMessage(r *Request) error {
	id, msg, err := pinPath(r)
	if err != nil {
		return err
	}
	if err := a.Chat.Pin(r.Ctx(), r.Actor(), id, msg); err != nil {
		return err
	}
	return r.NoContent()
}

func (a *API) unpinMessage(r *Request) error {
	id, msg, err := pinPath(r)
	if err != nil {
		return err
	}
	if err := a.Chat.Unpin(r.Ctx(), r.Actor(), id, msg); err != nil {
		return err
	}
	return r.NoContent()
}

func (a *API) moveThreadToChannel(r *Request) error {
	id, err := PathID[model.MessageID](r, "id")
	if err != nil {
		return err
	}
	var body struct {
		Name string `json:"name"`
	}
	if err := r.Decode(&body); err != nil {
		return err
	}
	c, err := a.Chat.MoveThread(r.Ctx(), r.Actor(), id, body.Name)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"conversation": c})
}

func (a *API) removeLinkPreview(r *Request) error {
	id, err := PathID[model.MessageID](r, "id")
	if err != nil {
		return err
	}
	index, perr := parseIndex(r.R.PathValue("index"))
	if perr != nil {
		return perr
	}
	m, err := a.Chat.RemovePreview(r.Ctx(), r.Actor(), id, index)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"message": m})
}

// parseIndex reads a position in a list from a path. One that is not a
// small whole number could name nothing.
func parseIndex(s string) (int, error) {
	if s == "" || len(s) > 3 {
		return 0, model.ErrNotFound
	}
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, model.ErrNotFound
		}
		n = n*10 + int(c-'0')
	}
	return n, nil
}
