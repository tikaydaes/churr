package perm

import (
	"testing"

	"github.com/tikaydaes/churr/internal/model"
)

// The sealed-chat rules that depend only on the accounts and the settings
// (docs/PERMISSIONS.md section 9.1 and section 17, cases 84 and 148). The
// cases that need a server (144 to 147, the block, the remote account)
// are in internal/api/sealed_more_test.go.
func TestStartSealed(t *testing.T) {
	on := DefaultSettings()
	off := on
	off.SealedEnabled = false
	other := account(model.RoleMember)
	other.ID = "other"
	bot := account(model.RoleBot)
	bot.ID = "bot"
	removed := account(model.RoleMember)
	removed.RemovedAt = 5
	for _, c := range []struct {
		name   string
		actor  model.Account
		target model.Account
		set    Settings
		want   model.Code
	}{
		{"the owner starts a sealed chat", account(model.RoleOwner), other, on, ""},
		{"an admin does", account(model.RoleAdmin), other, on, ""},
		{"a member does", account(model.RoleMember), other, on, ""},
		{"a guest does, with someone it can see", account(model.RoleGuest), other, on, ""},
		{"a bot never does", account(model.RoleBot), other, on, model.CodeForbidden},
		{"a remote account does not act here: its own server starts its chats", account(model.RoleRemote), other, on, model.CodeUnauthenticated},
		{"case 84: nobody starts one with a bot", account(model.RoleMember), bot, on, model.CodeForbidden},
		{"case 148: sealed chats are off", account(model.RoleMember), other, off, model.CodeFeatureOff},
		{"case 148: off wins over who is asking", account(model.RoleBot), other, off, model.CodeFeatureOff},
		{"not with oneself", account(model.RoleMember), account(model.RoleMember), on, model.CodeInvalidValue},
		{"a removed account does nothing", removed, other, on, model.CodeUnauthenticated},
	} {
		if got := code(CheckStartSealed(c.actor, c.target, c.set)); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestSealedReadOnly(t *testing.T) {
	on := DefaultSettings()
	off := on
	off.SealedEnabled = false
	other := account(model.RoleMember)
	removed := account(model.RoleMember)
	removed.RemovedAt = 5
	sealed := model.Conversation{ID: "s", Kind: model.KindSealed, Private: true}
	channel := model.Conversation{ID: "c", Kind: model.KindChannel, Name: "ops"}
	state := func(c model.Conversation, set Settings) State {
		return State{Actor: account(model.RoleMember), Conversation: c, Settings: set, Member: &model.Member{Level: model.LevelMember}}
	}
	for _, c := range []struct {
		name  string
		st    State
		other model.Account
		want  model.Code
	}{
		{"a sealed chat between two people who are here", state(sealed, on), other, ""},
		{"sealed chats were switched off: existing ones are read-only", state(sealed, off), other, model.CodeForbidden},
		{"the other person was removed: it accepts nothing new", state(sealed, on), removed, model.CodeForbidden},
		{"the other person's row is gone", state(sealed, on), model.Account{}, model.CodeForbidden},
		{"the other person is on another server (LINK-PROTOCOL.md section 16)", state(sealed, on), remotePerson(false), ""},
		{"the other person, on another server, was removed there", state(sealed, on), remotePerson(true), model.CodeForbidden},
		{"a channel takes no sealed messages", state(channel, on), other, model.CodeInvalidRequest},
	} {
		if got := code(CheckSealedWrite(c.st, c.other)); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
	if SealedReadOnly(on, other) || !SealedReadOnly(off, other) || !SealedReadOnly(on, removed) {
		t.Error("SealedReadOnly")
	}
	// The server keeps no history of a sealed chat, and has nothing to
	// purge, react to, pin or report there.
	msg := model.Message{ID: "m", ConversationID: "s", AuthorID: state(sealed, on).Actor.ID}
	for name, a := range map[string]MessageAction{"history": OpenHistory, "a purge request": AskPurge, "a purge": PurgeDirectly, "a reaction": ReactTo} {
		if got := code(CheckMessage(state(sealed, on), a, msg)); got != model.CodeForbidden {
			t.Errorf("%s in a sealed chat: got %q, want forbidden", name, got)
		}
	}
	for name, a := range map[string]Action{"a pin": Pin, "a report": Report, "adding someone": AddAccount, "adding a bot": AddBot} {
		if got := code(Check(state(sealed, on), a)); got != model.CodeForbidden {
			t.Errorf("%s in a sealed chat: got %q, want forbidden", name, got)
		}
	}
}

// remotePerson is the shadow of an account on another server.
func remotePerson(removed bool) model.Account {
	a := account(model.RoleRemote)
	a.ServerID, a.ServerName = "0193a0d1-0000-7000-8000-00000000beef", "b.example"
	if removed {
		a.RemovedAt = 5
	}
	return a
}
