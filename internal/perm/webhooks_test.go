package perm

import (
	"testing"

	"github.com/tikaydaes/churr/internal/model"
)

// The webhook rules of docs/PERMISSIONS.md sections 7 and 15 and
// docs/BOTS.md section 9 that need more than Check.

func managersMayApprove(st *State) { st.Settings.WebhooksManagersMayApprove = true }

func TestDecideWebhook(t *testing.T) {
	const owner, admin, member, guest, bot = model.RoleOwner, model.RoleAdmin, model.RoleMember, model.RoleGuest, model.RoleBot
	const none, reader, lmember, manager = model.Level(""), model.LevelReader, model.LevelMember, model.LevelManager
	for _, c := range []struct {
		name      string
		st        State
		approving bool
		want      model.Code
	}{
		{"an admin approves in PUB", state(admin, "PUB", none), true, ok},
		{"the owner approves in PUB", state(owner, "PUB", lmember), true, ok},
		// BOTS.md section 9: the approving admin need not be in a private
		// channel; the request came from a member who is.
		{"an admin outside PRIV approves", state(admin, "PRIV", none), true, ok},
		{"an admin outside PRIV declines", state(admin, "PRIV", none), false, ok},
		{"a manager approves with the setting off", state(member, "PUB", manager), true, forbid},
		{"a manager approves with the setting on", state(member, "PUB", manager, managersMayApprove), true, ok},
		{"a member approves with the setting on", state(member, "PUB", lmember, managersMayApprove), true, forbid},
		{"a reader approves with the setting on", state(member, "PUB", reader, managersMayApprove), true, forbid},
		{"a member outside PRIV approves", state(member, "PRIV", none, managersMayApprove), true, notFound},
		{"a guest manager approves with the setting on", state(guest, "PUB", manager, managersMayApprove), true, forbid},
		{"a bot manager approves with the setting on", state(bot, "PUB", manager, managersMayApprove), true, forbid},
		// Ruling R8 and case 92: never in an ad-hoc channel.
		{"an admin approves in ADHOC", state(admin, "ADHOC", lmember), true, forbid},
		{"an admin approves in the library", state(admin, "LIBRARY", none), true, notFound},
		// Sections 10 and 10.1: approving would add an account.
		{"an admin approves in an archived channel", state(admin, "PUB", none, archived), true, isArch},
		{"case 179 an admin approves in a locked PRIV", state(admin, "PRIV", lmember, locked), true, isLocked},
		{"an admin declines in a locked PRIV", state(admin, "PRIV", lmember, locked), false, ok},
		{"an admin declines in an archived channel", state(admin, "PUB", none, archived), false, ok},
		{"a removed admin approves", state(admin, "PUB", none, removed), true, unauth},
	} {
		want(t, c.name, CheckDecideWebhook(c.st, c.approving), c.want)
	}
}

func TestManageWebhook(t *testing.T) {
	const admin, member, guest, bot = model.RoleAdmin, model.RoleMember, model.RoleGuest, model.RoleBot
	const none, lmember, manager = model.Level(""), model.LevelMember, model.LevelManager
	requester := account(member).ID
	other := model.AccountID("0193a000-0000-7000-8000-00000000ffff")
	for _, c := range []struct {
		name      string
		st        State
		requester model.AccountID
		want      model.Code
	}{
		{"the requester", state(member, "PUB", lmember), requester, ok},
		// The requester may have left since; the webhook is still theirs
		// to withdraw.
		{"the requester after leaving PRIV", state(member, "PRIV", none), requester, ok},
		{"a manager of the channel", state(member, "PUB", manager), other, ok},
		{"an admin outside PRIV", state(admin, "PRIV", none), other, ok},
		{"another member", state(member, "PUB", lmember), other, forbid},
		{"another member of the server, not in PUB", state(member, "PUB", none), other, forbid},
		{"someone outside PRIV", state(member, "PRIV", none), other, notFound},
		{"a guest manager", state(guest, "PUB", manager), other, ok},
		{"a bot manager", state(bot, "PUB", manager), other, forbid},
		{"a webhook's own account", state(bot, "PUB", lmember, webhook), other, notFound},
		{"a removed requester", state(member, "PUB", lmember, removed), requester, unauth},
	} {
		want(t, c.name, CheckManageWebhook(c.st, c.requester), c.want)
	}
}

func TestListWebhooks(t *testing.T) {
	const admin, member, guest, bot = model.RoleAdmin, model.RoleMember, model.RoleGuest, model.RoleBot
	const none, reader, lmember = model.Level(""), model.LevelReader, model.LevelMember
	for _, c := range []struct {
		name string
		st   State
		want model.Code
	}{
		{"a member of the channel", state(member, "PUB", lmember), ok},
		{"a reader of the channel", state(member, "PUB", reader), ok},
		{"a guest in the channel", state(guest, "PUB", lmember), ok},
		{"a bot in the channel", state(bot, "PUB", lmember), ok},
		{"an admin reading PUB without joining", state(admin, "PUB", none), ok},
		{"a member reading PUB without joining", state(member, "PUB", none), forbid},
		{"someone outside PRIV", state(member, "PRIV", none), notFound},
		{"an admin outside PRIV", state(admin, "PRIV", none), notFound},
		{"a webhook's own account", state(bot, "PUB", lmember, webhook), notFound},
	} {
		want(t, c.name, CheckListWebhooks(c.st), c.want)
	}
}
