package perm

import (
	"testing"

	"github.com/tikaydaes/churr/internal/model"
)

func code(err error) model.Code { return model.CodeOf(err) }

func TestGroupRules(t *testing.T) {
	set := DefaultSettings()
	open := model.Group{ID: "g", Handle: "ops", Open: true}
	closed := model.Group{ID: "g", Handle: "ops"}
	everyone := model.Group{ID: "e", Handle: "everyone", Builtin: model.GroupEveryone}
	mapped := model.Group{ID: "m", Handle: "eng", ProviderID: "p", Open: true}
	for _, c := range []struct {
		name string
		got  error
		want model.Code
	}{
		{"a member joins an open group", CheckJoinGroup(account(model.RoleMember), open, set), ""},
		{"the owner joins an open group", CheckJoinGroup(account(model.RoleOwner), open, set), ""},
		{"case 110 a member joins a group that is not open", CheckJoinGroup(account(model.RoleMember), closed, set), model.CodeForbidden},
		{"a guest cannot join by itself (P14)", CheckJoinGroup(account(model.RoleGuest), open, set), model.CodeForbidden},
		{"a bot cannot join by itself (P14)", CheckJoinGroup(account(model.RoleBot), open, set), model.CodeForbidden},
		{"nobody joins @everyone", CheckJoinGroup(account(model.RoleMember), everyone, set), model.CodeForbidden},
		{"nobody joins a provider-mapped group", CheckJoinGroup(account(model.RoleMember), mapped, set), model.CodeForbidden},
		{"an admin edits a group", CheckEditGroup(account(model.RoleAdmin), closed, set), ""},
		{"case 108 a member edits a group", CheckEditGroup(account(model.RoleMember), closed, set), model.CodeForbidden},
		{"case 108 a member edits @everyone", CheckEditGroup(account(model.RoleMember), everyone, set), model.CodeForbidden},
		{"case 108 an admin edits @everyone", CheckEditGroup(account(model.RoleAdmin), everyone, set), model.CodeForbidden},
		{"case 107 an admin adds a remote account to a group", CheckOnAccount(account(model.RoleAdmin), AddToGroup, account(model.RoleRemote)), model.CodeForbidden},
		{"an admin adds a guest to a group", CheckOnAccount(account(model.RoleAdmin), AddToGroup, account(model.RoleGuest)), ""},
		{"an admin adds a bot to a group", CheckOnAccount(account(model.RoleAdmin), AddToGroup, account(model.RoleBot)), ""},
	} {
		if code(c.got) != c.want {
			t.Errorf("%s: got %v, want %q", c.name, c.got, c.want)
		}
	}
}

func TestSetRole(t *testing.T) {
	set := DefaultSettings()
	owner := account(model.RoleOwner)
	removed := account(model.RoleMember)
	removed.RemovedAt = 5
	for _, c := range []struct {
		name   string
		actor  model.Account
		target model.Account
		role   model.Role
		want   model.Code
	}{
		{"the owner makes a member an admin", owner, account(model.RoleMember), model.RoleAdmin, ""},
		{"the owner makes an admin a guest", owner, account(model.RoleAdmin), model.RoleGuest, ""},
		{"an owner makes an admin an owner", owner, account(model.RoleAdmin), model.RoleOwner, ""},
		{"an owner makes a member an owner", owner, account(model.RoleMember), model.RoleOwner, ""},
		{"any other account of the instance (ruling R99)", owner, account(model.RoleGuest), model.RoleOwner, ""},
		{"an admin changes nobody's role", account(model.RoleAdmin), account(model.RoleMember), model.RoleAdmin, model.CodeForbidden},
		{"a member changes nobody's role", account(model.RoleMember), account(model.RoleGuest), model.RoleMember, model.CodeForbidden},
		{"a bot's role cannot be changed", owner, account(model.RoleBot), model.RoleMember, model.CodeConflict},
		{"nor a remote account's", owner, account(model.RoleRemote), model.RoleMember, model.CodeConflict},
		{"nor a removed account's", owner, removed, model.RoleMember, model.CodeConflict},
		{"an owner steps down (the store keeps the last)", owner, owner, model.RoleAdmin, ""},
		{"nobody demotes another owner", owner, func() model.Account { o := account(model.RoleOwner); o.ID += "-2"; return o }(), model.RoleAdmin, model.CodeForbidden},
		{"bot is not a role to give", owner, account(model.RoleMember), model.RoleBot, model.CodeInvalidValue},
	} {
		if got := CheckSetRole(c.actor, c.target, c.role, set); code(got) != c.want {
			t.Errorf("%s: got %v, want %q", c.name, got, c.want)
		}
	}
}

// Decision P15: admins act on members, guests and bots; only the owner acts
// on an admin; nobody removes or bans the owner.
func TestRemoveAndBan(t *testing.T) {
	for _, a := range []AccountAction{RemoveAccount, BanAccount} {
		for _, c := range []struct {
			actor, target model.Role
			want          model.Code
		}{
			{model.RoleAdmin, model.RoleMember, ""},
			{model.RoleAdmin, model.RoleGuest, ""},
			{model.RoleAdmin, model.RoleBot, ""},
			{model.RoleAdmin, model.RoleAdmin, model.CodeForbidden},
			{model.RoleOwner, model.RoleAdmin, ""},
			{model.RoleAdmin, model.RoleOwner, model.CodeForbidden},
			{model.RoleOwner, model.RoleOwner, model.CodeForbidden},
			{model.RoleMember, model.RoleGuest, model.CodeForbidden},
			{model.RoleAdmin, model.RoleRemote, model.CodeForbidden},
		} {
			if got := CheckOnAccount(account(c.actor), a, account(c.target)); code(got) != c.want {
				t.Errorf("%s by %s on %s: got %v, want %q", a, c.actor, c.target, got, c.want)
			}
		}
	}
}
