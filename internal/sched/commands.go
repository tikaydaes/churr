package sched

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/tikaydaes/churr/internal/chat"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/perm"
	"github.com/tikaydaes/churr/internal/store"
)

// BotCommands is what running a command needs of the bots package: sending a
// bot's command to the bot as an interaction. found is false when no bot in
// the conversation has registered the name. The caller has already checked
// that the actor may send commands there. Until one is set with SetBots, no
// bot command can be run.
type BotCommands interface {
	RunCommand(ctx context.Context, actor model.Account, conv model.ConversationID, parent model.MessageID, name, text string) (interactionID string, found bool, err error)
}

// Commands is saved replies, custom commands and the command menu
// (docs/FEATURES.md section 12).
type Commands struct {
	store store.Store
	perm  *perm.Checker
	chat  *chat.Service
	bots  BotCommands
}

// NewCommands makes the service.
func NewCommands(s store.Store, p *perm.Checker, c *chat.Service) *Commands {
	return &Commands{store: s, perm: p, chat: c}
}

// SetBots hands over the bots package, which is built after this one. It is
// called once while the server is being put together.
func (c *Commands) SetBots(b BotCommands) { c.bots = b }

// maxCommandText is the most that may be typed after a command's name.
const maxCommandText = 4000

// describe is what the menu shows for a saved reply: the start of its text.
func describe(text string) string {
	line := strings.TrimSpace(text)
	if i := strings.IndexAny(line, "\r\n"); i >= 0 {
		line = line[:i]
	}
	if utf8.RuneCountInString(line) > 100 {
		line = string([]rune(line)[:99]) + "…"
	}
	return line
}

// List returns everything that can be typed as /name in a conversation, for
// anyone who can read it: the built-ins, the actor's saved replies, the
// custom commands, and the commands of bots that are in the conversation.
// Where a person's saved reply has the name of a custom or bot command,
// theirs is listed, because theirs is what runs.
func (c *Commands) List(ctx context.Context, actor model.Account, conv model.ConversationID) ([]model.Command, error) {
	st, err := c.perm.Can(ctx, actor, perm.Read, conv)
	if err != nil {
		return nil, err
	}
	var out []model.Command
	for _, b := range model.BuiltinCommands {
		// A sealed chat carries no forms and nothing the server writes,
		// so only the built-ins marked for it are offered there (/remind's
		// reminder arrives somewhere else).
		if st.Conversation.Sealed() && !b.Sealed {
			continue
		}
		out = append(out, b)
	}
	replies, err := c.store.Scheduled().Replies(ctx, actor.ID)
	if err != nil {
		return nil, err
	}
	own := map[string]bool{}
	for _, r := range replies {
		if !r.Community {
			own[r.Name] = true
		}
	}
	taken := map[string]bool{}
	for _, r := range replies {
		// A name that became a built-in after the reply was saved runs
		// the built-in (ruling R67), so the reply is not listed.
		if (r.Community && own[r.Name]) || model.BuiltinCommand(r.Name) {
			continue
		}
		taken[r.Name] = true
		out = append(out, model.Command{Name: r.Name, Kind: model.CommandSavedReply, Description: describe(r.Text), SavedReplyID: r.ID})
	}
	if st.Conversation.Sealed() {
		return out, nil
	}
	bots, err := c.store.Bots().CommandsIn(ctx, conv)
	if err != nil {
		return nil, err
	}
	for _, b := range bots {
		if taken[b.Name] || model.BuiltinCommand(b.Name) {
			continue
		}
		b.Kind = model.CommandBot
		out = append(out, b)
	}
	return out, nil
}

// Ran is what running a command gives back: the message a saved reply
// posted, or the interaction a bot command started.
type Ran struct {
	Message       *model.Message
	InteractionID string
}

// Run carries out what was typed as /name in a conversation. The name is
// looked up among the actor's own saved replies, then the custom commands,
// then the commands of bots in the conversation, and the first match wins.
// A saved reply posts its text as the actor, under every rule of posting;
// anything typed after its name is refused, so nothing is dropped silently
// (decision F15). Built-in commands are the client's to carry out.
func (c *Commands) Run(ctx context.Context, actor model.Account, conv model.ConversationID, parent model.MessageID, name, text string) (Ran, error) {
	st, err := c.perm.Can(ctx, actor, perm.SendCommand, conv)
	if err != nil {
		return Ran{}, err
	}
	if !model.ValidCommandName(name) {
		return Ran{}, model.Invalid("name", "A command name is 1 to 32 characters of a-z, 0-9, _ and -.")
	}
	if model.ReservedCommandName(name) {
		return Ran{}, model.Invalid("name", "That command is carried out by the app, not sent to the server.")
	}
	if utf8.RuneCountInString(text) > maxCommandText {
		return Ran{}, model.Invalid("text", "At most 4,000 characters may follow a command.")
	}
	reply, found, err := c.store.Scheduled().ReplyNamed(ctx, actor.ID, name)
	if err == nil && !found {
		reply, found, err = c.store.Scheduled().ReplyNamed(ctx, "", name)
	}
	if err != nil {
		return Ran{}, err
	}
	if found {
		if strings.TrimSpace(text) != "" {
			return Ran{}, model.Invalid("text", "/"+name+" posts a saved text and takes nothing after its name.")
		}
		if st.Conversation.Sealed() {
			return Ran{}, model.BadRequest("In a sealed chat the app fetches the saved text and sends it itself.")
		}
		posted, err := c.chat.Post(ctx, actor, chat.NewMessage{ConversationID: conv, ParentID: parent, Text: reply.Text,
			Via: &model.Via{Kind: "command", Name: name}})
		if err != nil {
			return Ran{}, err
		}
		return Ran{Message: &posted.Message}, nil
	}
	if c.bots != nil {
		interaction, found, err := c.bots.RunCommand(ctx, actor, conv, parent, name, strings.TrimSpace(text))
		if err != nil {
			return Ran{}, err
		}
		if found {
			return Ran{InteractionID: interaction}, nil
		}
	}
	return Ran{}, model.ErrNotFound.With("There is no command called /" + name + " here.")
}

// Replies returns the community's saved replies and the actor's own.
func (c *Commands) Replies(ctx context.Context, actor model.Account) ([]model.SavedReply, error) {
	return c.store.Scheduled().Replies(ctx, actor.ID)
}

// may asks perm whether the actor may keep saved replies of this kind:
// their own for any person, the community's for admins.
func (c *Commands) may(ctx context.Context, actor model.Account, community bool) error {
	set, err := c.perm.Settings(ctx)
	if err != nil {
		return err
	}
	if community {
		return perm.CheckServer(actor, perm.ManageCommunityReplies, set)
	}
	return perm.CheckServer(actor, perm.OwnPersonalThings, set)
}

// checkReply checks a saved reply's name and text. A custom command may not
// have the name of a bot's command; a person's own may, and then wins for
// them.
func (c *Commands) checkReply(ctx context.Context, community bool, name, text string) error {
	if !model.ValidCommandName(name) {
		return model.Invalid("name", "A command name is 1 to 32 characters of a-z, 0-9, _ and -.")
	}
	if strings.TrimSpace(text) == "" {
		return model.Invalid("text", "A saved reply needs its text.")
	}
	if utf8.RuneCountInString(text) > model.MaxMessageText {
		return model.Invalid("text", "A saved reply is at most 16,000 characters, like a message.")
	}
	if model.ReservedCommandName(name) {
		return model.ErrNameTaken
	}
	if community {
		if _, taken, err := c.store.Bots().CommandByName(ctx, name); err != nil {
			return err
		} else if taken {
			return model.ErrNameTaken
		}
	}
	return nil
}

func (c *Commands) audit(ctx context.Context, actor model.Account, action string, r model.SavedReply) error {
	return c.store.Audit().Append(ctx, model.AuditEntry{ActorID: string(actor.ID), ActorKind: "account", Action: action,
		TargetKind: "saved_reply", TargetID: r.ID, Detail: map[string]any{"name": r.Name}})
}

// CreateReply saves a reply: the actor's own, or with community set a
// custom command for everyone, which only admins may make.
func (c *Commands) CreateReply(ctx context.Context, actor model.Account, community bool, name, text string) (model.SavedReply, error) {
	if err := c.may(ctx, actor, community); err != nil {
		return model.SavedReply{}, err
	}
	if err := c.checkReply(ctx, community, name, text); err != nil {
		return model.SavedReply{}, err
	}
	r := model.SavedReply{Name: name, Text: text}
	if !community {
		r.OwnerID = actor.ID
	}
	if err := c.store.Scheduled().CreateReply(ctx, &r); err != nil {
		return r, err
	}
	if community {
		return r, c.audit(ctx, actor, "saved_reply.created", r)
	}
	return r, nil
}

// reply finds a saved reply the actor may change: one of their own, or with
// community set one of the community's. Any other does not exist for them.
func (c *Commands) reply(ctx context.Context, actor model.Account, community bool, id string) (model.SavedReply, error) {
	if err := c.may(ctx, actor, community); err != nil {
		return model.SavedReply{}, err
	}
	r, err := c.store.Scheduled().Reply(ctx, id)
	if err != nil {
		return r, err
	}
	if r.Community != community || (!community && r.OwnerID != actor.ID) {
		return r, model.ErrNotFound
	}
	return r, nil
}

// UpdateReply changes a saved reply's name, text or both. Nil stays as it is.
func (c *Commands) UpdateReply(ctx context.Context, actor model.Account, community bool, id string, name, text *string) (model.SavedReply, error) {
	r, err := c.reply(ctx, actor, community, id)
	if err != nil {
		return r, err
	}
	if name != nil {
		r.Name = *name
	}
	if text != nil {
		r.Text = *text
	}
	if err := c.checkReply(ctx, community, r.Name, r.Text); err != nil {
		return r, err
	}
	out, err := c.store.Scheduled().UpdateReply(ctx, id, r.Name, r.Text)
	if err != nil {
		return out, err
	}
	if community {
		return out, c.audit(ctx, actor, "saved_reply.updated", out)
	}
	return out, nil
}

// DeleteReply deletes a saved reply.
func (c *Commands) DeleteReply(ctx context.Context, actor model.Account, community bool, id string) error {
	r, err := c.reply(ctx, actor, community, id)
	if err != nil {
		return err
	}
	if err := c.store.Scheduled().DeleteReply(ctx, id); err != nil {
		if errors.Is(err, model.ErrNotFound) {
			return model.ErrNotFound
		}
		return err
	}
	if community {
		return c.audit(ctx, actor, "saved_reply.deleted", r)
	}
	return nil
}
