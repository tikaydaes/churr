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
		a.handle("updateConversation", a.updateConversation)
		a.handle("archiveConversation", a.archiveConversation)
		a.handle("unarchiveConversation", a.unarchiveConversation)
		a.handle("lockConversation", a.lockConversation)
		a.handle("setMemberLevel", a.setMemberLevel)
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
		DefaultLevel     *model.Level    `json:"default_level"`
		SectionID        json.RawMessage `json:"section_id"`
		RetentionSeconds *int64          `json:"retention_seconds"`
		SlowModeSeconds  *int            `json:"slow_mode_seconds"`
		ParentID         json.RawMessage `json:"parent_id"`
	}
	if err := r.Decode(&body); err != nil {
		return err
	}
	ch := chat.ConversationChange{Name: body.Name, Topic: body.Topic, Welcome: body.Welcome, Private: body.Private,
		DefaultLevel: body.DefaultLevel, RetentionSeconds: body.RetentionSeconds, SlowModeSeconds: body.SlowModeSeconds}
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
