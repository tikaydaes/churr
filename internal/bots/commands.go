package bots

import (
	"context"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/perm"
)

// Commands a bot offers (docs/BOTS.md section 5). Listing the commands of
// every kind and running one belong to the sched package's Commands;
// RunCommand here is the bot side of running, in the shape its BotCommands
// interface asks for.

const (
	maxCommands           = 50
	maxCommandDescription = 100
	maxCommandHint        = 100
	maxCommandText        = 4000
)

// cleanCommandLine checks one line of a command: its description or hint.
func cleanCommandLine(field, v string, max int) (string, error) {
	v = strings.TrimSpace(v)
	if utf8.RuneCountInString(v) > max || strings.IndexFunc(v, unicode.IsControl) >= 0 {
		return "", model.Invalid(field, "That is too long, or has characters that cannot be shown.")
	}
	return v, nil
}

// SetCommands replaces a bot's whole list of commands with the one given,
// so a bot can send the same list every time it starts. An empty list
// removes them all. A name is unique on the server: one that is a built-in
// command, a community saved reply or another bot's command is refused with
// name_taken, naming the command.
func (s *Service) SetCommands(ctx context.Context, actor model.Account, list []model.Command) ([]model.Command, error) {
	if err := s.perm.CanAdminister(ctx, actor, perm.RegisterCommands); err != nil {
		return nil, err
	}
	info, found, err := s.store.Bots().Get(ctx, actor.ID)
	if err != nil {
		return nil, err
	}
	if !found || info.WebhookOnly {
		return nil, model.ErrForbidden.With("Only a bot can register commands. A webhook cannot.")
	}
	if len(list) > maxCommands {
		return nil, model.Invalid("commands", "A bot offers at most 50 commands.")
	}
	clean := make([]model.Command, 0, len(list))
	seen := map[string]bool{}
	for _, c := range list {
		if !model.ValidCommandName(c.Name) {
			return nil, model.Invalid("commands", "A command name is 1 to 32 characters from a-z, 0-9, _ and -.")
		}
		if seen[c.Name] {
			return nil, model.Invalid("commands", "The command /"+c.Name+" is listed twice.")
		}
		seen[c.Name] = true
		description, err := cleanCommandLine("commands", c.Description, maxCommandDescription)
		if err != nil {
			return nil, err
		}
		if description == "" {
			return nil, model.Invalid("commands", "The command /"+c.Name+" needs a description.")
		}
		hint, err := cleanCommandLine("commands", c.Hint, maxCommandHint)
		if err != nil {
			return nil, err
		}
		if model.ReservedCommandName(c.Name) {
			return nil, model.ErrNameTaken.With("The name /" + c.Name + " is held back for a command to come.")
		}
		taken, err := s.store.Bots().CommunityReplyNamed(ctx, c.Name)
		if err != nil {
			return nil, err
		}
		if taken {
			return nil, model.ErrNameTaken.With("The command /" + c.Name + " is a saved reply on this server.")
		}
		clean = append(clean, model.Command{Name: c.Name, Kind: model.CommandBot, Description: description, Hint: hint, BotID: actor.ID})
	}
	// Only a list that could be set is counted against the ten an hour.
	if err := limited(s.limits.commandSets.Take(string(actor.ID))); err != nil {
		return nil, err
	}
	if err := s.store.Bots().SetCommands(ctx, actor.ID, clean); err != nil {
		return nil, err
	}
	return s.store.Bots().Commands(ctx, actor.ID)
}

// RunCommand sends a bot's command to the bot. Nothing is posted: the
// server makes an interaction, sends the bot `command.invoked` over its
// live connection, and returns the interaction's identifier at once.
//
// The sender must be a local account that is a member of the conversation,
// at any level, and not timed out there. found is false when no bot in this
// conversation offers the name: a bot's commands cannot be sent where the
// bot is not, and it then receives nothing. A bot that is not connected is
// unavailable, and nothing is queued.
func (s *Service) RunCommand(ctx context.Context, actor model.Account, conv model.ConversationID, parent model.MessageID, name, text string) (interactionID string, found bool, err error) {
	st, err := s.perm.Can(ctx, actor, perm.SendCommand, conv)
	if err != nil {
		return "", false, notFound(err)
	}
	cmd, found, err := s.store.Bots().CommandByName(ctx, name)
	if err != nil || !found {
		return "", false, err
	}
	if _, in, err := s.store.Members().Get(ctx, st.Conversation.ID, cmd.BotID); err != nil || !in {
		return "", false, err
	}
	if !actor.Local() {
		// A bot's commands are offered only to accounts of its own
		// server (docs/BOTS.md section 1).
		return "", true, model.ErrForbidden.With("Commands can be sent only by accounts of this server.")
	}
	text = strings.TrimSpace(text)
	if utf8.RuneCountInString(text) > maxCommandText {
		return "", true, model.Invalid("text", "The text after a command is at most 4,000 characters.")
	}
	if parent != "" {
		if parent, err = s.threadOf(ctx, st.Conversation.ID, parent); err != nil {
			return "", true, err
		}
	}
	if !s.connected(cmd.BotID) {
		return "", true, errNotConnected
	}
	entities, err := s.chat.Entities(ctx, actor, conv, text)
	if err != nil {
		return "", true, err
	}
	it := s.open(interaction{bot: cmd.BotID, asker: actor.ID, conv: st.Conversation.ID, parent: parent, command: cmd.Name})
	payload := map[string]any{
		"interaction_id": it.id, "account_id": actor.ID, "command": cmd.Name, "text": text, "entities": entities,
	}
	if parent != "" {
		payload["parent_id"] = parent
	}
	s.hub.Send(cmd.BotID, model.Event{Type: eventCommandInvoked, At: s.now(), ConversationID: st.Conversation.ID, Payload: payload})
	return it.id, true, nil
}

// threadOf returns the top-level message of the thread a message of this
// conversation belongs to.
func (s *Service) threadOf(ctx context.Context, conv model.ConversationID, id model.MessageID) (model.MessageID, error) {
	m, err := s.store.Messages().Get(ctx, id)
	if err != nil || m.ConversationID != conv {
		return "", model.ErrNotFound
	}
	if m.ParentID != "" {
		return m.ParentID, nil
	}
	return m.ID, nil
}

// interactionLife is how long an interaction accepts responses, and
// maxResponses how many (docs/BOTS.md section 5).
const (
	interactionLife = 15 * time.Minute
	maxResponses    = 5
)
