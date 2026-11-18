package sched

import (
	"context"
	"errors"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/tikaydaes/churr/internal/chat"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/modhost"
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
	// modules are the built-in modules, whose commands run here once no
	// saved reply or bot command of the name has taken it (ruling R72).
	modules *modhost.Registry
}

// NewCommands makes the service.
func NewCommands(s store.Store, p *perm.Checker, c *chat.Service) *Commands {
	return &Commands{store: s, perm: p, chat: c}
}

// SetBots hands over the bots package, which is built after this one. It is
// called once while the server is being put together.
func (c *Commands) SetBots(b BotCommands) { c.bots = b }

// SetModules hands over the built-in modules the server registers. It is
// called once while the server is being put together.
func (c *Commands) SetModules(m *modhost.Registry) { c.modules = m }

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
// anyone who can read it: the built-ins, the actor's saved replies and the
// custom commands, the commands of bots that are in the conversation, and
// the commands of the built-in modules that are on there, under the names
// the admin gave them. Where two have one name, the one whose source comes
// first in the admin's order is listed, because it is what runs (ruling
// R72; docs/DESIGN-MODULES.md section 6.5); a person's own saved reply
// comes before the custom command of its name. A protected built-in always
// wins: a command of its name made before it was protected is not listed.
// The admin-only built-ins are listed for admins alone; a module that
// answers people only is not listed for a bot.
func (c *Commands) List(ctx context.Context, actor model.Account, conv model.ConversationID) ([]model.Command, error) {
	own, err := c.list(ctx, actor, conv)
	if err != nil {
		return nil, err
	}
	set, err := c.settings(ctx)
	if err != nil {
		return nil, err
	}
	off, err := c.modules.Off(ctx, conv)
	if err != nil {
		return nil, err
	}
	by := map[string][]model.Command{}
	for _, cmd := range own.cmds {
		src := model.SourceSavedReply
		if cmd.Kind == model.CommandBot {
			src = model.SourceBot
		}
		by[src] = append(by[src], cmd)
	}
	if !own.sealed {
		for _, mc := range c.moduleCommands(set) {
			if off[mc.module.Name] || (mc.module.PeopleOnly && actor.Role == model.RoleBot) || model.ProtectedCommand(mc.name) {
				continue
			}
			by[model.SourceModule] = append(by[model.SourceModule], mc.listed(set))
		}
		slices.SortFunc(by[model.SourceModule], func(a, b model.Command) int { return strings.Compare(a.Name, b.Name) })
	}
	adminOnly := model.HiddenBuiltins(nil, actor.Role.IsAdmin())
	for _, b := range model.BuiltinCommands {
		// A sealed chat carries no forms and nothing the server writes,
		// so only the built-ins marked for it are offered there (/remind's
		// reminder arrives somewhere else).
		if (own.sealed && !b.Sealed) || adminOnly[b.Name] {
			continue
		}
		by[model.SourceBuiltin] = append(by[model.SourceBuiltin], b)
	}
	won := map[string]string{}
	for _, b := range by[model.SourceBuiltin] {
		if !b.Replaceable {
			won[b.Name] = model.SourceBuiltin
		}
	}
	for _, src := range set.Order {
		for _, cmd := range by[src] {
			if _, taken := won[cmd.Name]; !taken {
				won[cmd.Name] = src
			}
		}
	}
	// Listed as the menu shows them: the built-ins in the table's order,
	// then the rest by source.
	var out []model.Command
	for _, src := range []string{model.SourceBuiltin, model.SourceSavedReply, model.SourceBot, model.SourceModule} {
		for _, cmd := range by[src] {
			if won[cmd.Name] == src {
				out = append(out, cmd)
				won[cmd.Name] = ""
			}
		}
	}
	return out, nil
}

// listed is what List finds besides the built-ins and the modules.
type listed struct {
	cmds   []model.Command
	sealed bool
}

// list finds the actor's saved replies, the custom commands (a person's own
// saved reply hides the custom command of its name) and the commands of
// bots in the conversation. None has a protected built-in's name.
func (c *Commands) list(ctx context.Context, actor model.Account, conv model.ConversationID) (listed, error) {
	st, err := c.perm.Can(ctx, actor, perm.Read, conv)
	if err != nil {
		return listed{}, err
	}
	var out []model.Command
	replies, err := c.store.Scheduled().Replies(ctx, actor.ID)
	if err != nil {
		return listed{}, err
	}
	own := map[string]bool{}
	for _, r := range replies {
		if !r.Community {
			own[r.Name] = true
		}
	}
	for _, r := range replies {
		if (r.Community && own[r.Name]) || model.ProtectedCommand(r.Name) {
			continue
		}
		out = append(out, model.Command{Name: r.Name, Kind: model.CommandSavedReply, Description: describe(r.Text), SavedReplyID: r.ID})
	}
	if st.Conversation.Sealed() {
		return listed{out, true}, nil
	}
	bots, err := c.store.Bots().CommandsIn(ctx, conv)
	if err != nil {
		return listed{}, err
	}
	for _, b := range bots {
		if model.ProtectedCommand(b.Name) {
			continue
		}
		b.Kind = model.CommandBot
		out = append(out, b)
	}
	return listed{out, false}, nil
}

// Ran is what running a command gives back: the message a saved reply or a
// module posted, or the interaction a bot command started.
type Ran struct {
	Message       *model.Message
	InteractionID string
}

// Run carries out what was typed as /name in a conversation. The name is
// looked up source by source in the admin's order (the table, table.go),
// and the first match wins: the actor's own saved replies then the custom
// commands; the commands of bots in the conversation; the commands of the
// built-in modules on there, by the names they have here; the built-ins. A
// saved reply posts its text as the actor, under every rule of posting;
// anything typed after its name is refused, so nothing is dropped silently
// (decision F15). A module's command runs through the host, and its answer
// is posted as the actor's message; one switched off here is refused with
// feature_off when nothing else has its name. The built-ins are the
// client's to carry out, and a protected one is never looked up: a command
// of its name made before it was protected is ignored. clientID and drawT
// are the client's for the message a module's command posts: its draw's
// input (docs/FAIR.md).
func (c *Commands) Run(ctx context.Context, actor model.Account, conv model.ConversationID, parent model.MessageID, clientID string, drawT int64, name, text string) (Ran, error) {
	st, err := c.perm.Can(ctx, actor, perm.SendCommand, conv)
	if err != nil {
		return Ran{}, err
	}
	if !model.ValidCommandName(name) {
		return Ran{}, model.Invalid("name", "A command name is 1 to 32 characters of a-z, 0-9, _ and -.")
	}
	if model.ReservedCommandName(name) {
		return Ran{}, model.Invalid("name", "That name is held back for a command to come.")
	}
	if utf8.RuneCountInString(text) > maxCommandText {
		return Ran{}, model.Invalid("text", "At most 4,000 characters may follow a command.")
	}
	if model.ProtectedCommand(name) {
		return Ran{}, model.Invalid("name", "That command is carried out by the app, not sent to the server.")
	}
	set, err := c.settings(ctx)
	if err != nil {
		return Ran{}, err
	}
	var switchedOff error
	for _, src := range set.Order {
		switch src {
		case model.SourceSavedReply:
			reply, found, err := c.store.Scheduled().ReplyNamed(ctx, actor.ID, name)
			if err == nil && !found {
				reply, found, err = c.store.Scheduled().ReplyNamed(ctx, "", name)
			}
			if err != nil {
				return Ran{}, err
			}
			if !found {
				continue
			}
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
		case model.SourceBot:
			if c.bots == nil {
				continue
			}
			interaction, found, err := c.bots.RunCommand(ctx, actor, conv, parent, name, strings.TrimSpace(text))
			if err != nil {
				return Ran{}, err
			}
			if found {
				return Ran{InteractionID: interaction}, nil
			}
		case model.SourceModule:
			mc, ok := c.moduleCommands(set)[name]
			if !ok {
				continue
			}
			ran, err := c.runModule(ctx, actor, st, parent, clientID, drawT, mc, text)
			if errors.Is(err, model.ErrFeatureOff) {
				// Off here, it is not in the table: a later source
				// may have the name.
				switchedOff = err
				continue
			}
			return ran, err
		case model.SourceBuiltin:
			if model.BuiltinCommand(name) {
				// It is the built-in, which the app carries out.
				return Ran{}, model.Invalid("name", "That command is carried out by the app, not sent to the server.")
			}
		}
	}
	if switchedOff != nil {
		return Ran{}, switchedOff
	}
	return Ran{}, model.ErrNotFound.With("There is no command called /" + name + " here.")
}

// runModule runs a built-in module's command, by the name the module gave
// it, and posts its answer, under every rule of posting. Nothing is written
// into a sealed chat, which the server cannot write into. A refusal is the
// module's own sentence, for the person who typed the command; whether a
// bot may run it is the module's to say.
func (c *Commands) runModule(ctx context.Context, actor model.Account, st perm.State, parent model.MessageID, clientID string, drawT int64, mc moduleCommand, text string) (Ran, error) {
	if st.Conversation.Sealed() {
		return Ran{}, model.Invalid("name", "The server cannot write into a sealed chat, so /"+mc.name+" cannot run there.")
	}
	// Post runs the command, so that its chance comes from the message's
	// draw (docs/FAIR.md).
	posted, err := c.chat.Post(ctx, actor, chat.NewMessage{ConversationID: st.Conversation.ID, ParentID: parent, ClientID: clientID, DrawT: drawT,
		Command: &chat.ModuleCommand{Name: mc.cmd.Name, Text: strings.TrimSpace(text)}})
	if err != nil {
		return Ran{}, err
	}
	return Ran{Message: &posted.Message}, nil
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
// them. Nor may either have a protected built-in's name when it is given
// one (renamed is true for a new reply too); one that already has it may
// still have its text changed, and is ignored until renamed.
func (c *Commands) checkReply(ctx context.Context, community, renamed bool, name, text string) error {
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
	if renamed && model.ProtectedCommand(name) {
		return model.ProtectedCommandError(name)
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
	if err := c.checkReply(ctx, community, true, name, text); err != nil {
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
	renamed := name != nil && *name != r.Name
	if name != nil {
		r.Name = *name
	}
	if text != nil {
		r.Text = *text
	}
	if err := c.checkReply(ctx, community, renamed, r.Name, r.Text); err != nil {
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
