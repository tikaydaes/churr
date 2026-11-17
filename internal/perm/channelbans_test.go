package perm

import (
	"testing"

	"github.com/tikaydaes/churr/internal/model"
)

// TestChannelBan is docs/PERMISSIONS.md section 6.1 and test cases 281 to
// 288: who may ban whom from a channel, and what a ban does to the banned.
func TestChannelBan(t *testing.T) {
	owner, admin, admin2 := acct("o", model.RoleOwner), acct("a", model.RoleAdmin), acct("a2", model.RoleAdmin)
	mgr, bob, guest, bot := acct("m", model.RoleMember), acct("b", model.RoleMember), acct("g", model.RoleGuest), acct("bot", model.RoleBot)
	adhoc := model.Conversation{ID: "adhoc", Kind: model.KindChannel, Private: true}
	sealed := model.Conversation{ID: "s", Kind: model.KindSealed}
	shared := pubConv
	shared.StreamID = "stream"

	mod := acct("mo", model.RoleMember)
	// 281: a member-level account bans nobody; a moderator or a manager
	// bans a member.
	wantCode(t, "a member bans", CheckChannelBan(stateFor(bob, privConv, model.LevelMember), guest, model.LevelMember), model.CodeForbidden)
	wantCode(t, "a manager bans a member", CheckChannelBan(stateFor(mgr, privConv, model.LevelManager), bob, model.LevelMember), "")
	wantCode(t, "a moderator bans a member", CheckChannelBan(stateFor(mod, privConv, model.LevelModerator), bob, model.LevelMember), "")
	wantCode(t, "a manager bans a guest", CheckChannelBan(stateFor(mgr, pubConv, model.LevelManager), guest, ""), "")
	wantCode(t, "a manager bans a bot", CheckChannelBan(stateFor(mgr, pubConv, model.LevelManager), bot, ""), "")
	// 282: an admin is not a manager of any channel (ruling R99).
	wantCode(t, "an admin outside a public channel", CheckChannelBan(stateFor(admin, pubConv, ""), bob, ""), model.CodeForbidden)
	wantCode(t, "an admin outside a private channel", CheckChannelBan(stateFor(admin, privConv, ""), bob, ""), model.CodeNotFound)
	wantCode(t, "an admin who is a member of it", CheckChannelBan(stateFor(admin, privConv, model.LevelMember), bob, ""), model.CodeForbidden)
	wantCode(t, "an admin who is its manager", CheckChannelBan(stateFor(admin, privConv, model.LevelManager), bob, ""), "")
	// 283: a moderator does not ban a moderator or a manager; a manager
	// bans an admin like anyone; nobody bans an owner or themselves.
	wantCode(t, "a moderator bans a manager", CheckChannelBan(stateFor(mod, pubConv, model.LevelModerator), mgr, model.LevelManager), model.CodeForbidden)
	wantCode(t, "a moderator bans a moderator", CheckChannelBan(stateFor(mod, pubConv, model.LevelModerator), bob, model.LevelModerator), model.CodeForbidden)
	wantCode(t, "a manager bans a moderator", CheckChannelBan(stateFor(mgr, pubConv, model.LevelManager), mod, model.LevelModerator), "")
	wantCode(t, "a manager bans an admin", CheckChannelBan(stateFor(mgr, pubConv, model.LevelManager), admin, model.LevelMember), "")
	wantCode(t, "a manager bans an owner", CheckChannelBan(stateFor(mgr, pubConv, model.LevelManager), owner, model.LevelMember), model.CodeForbidden)
	wantCode(t, "an admin manager bans an admin", CheckChannelBan(stateFor(admin, pubConv, model.LevelManager), admin2, ""), "")
	wantCode(t, "a manager bans themselves", CheckChannelBan(stateFor(mgr, pubConv, model.LevelManager), mgr, model.LevelManager), model.CodeForbidden)
	// 284: bots that are managers may ban, as they may remove; a guest is
	// a guest everywhere and bans nobody.
	wantCode(t, "a bot manager bans", CheckChannelBan(stateFor(bot, privConv, model.LevelManager), bob, model.LevelMember), "")
	wantCode(t, "a guest member bans", CheckChannelBan(stateFor(guest, privConv, model.LevelMember), bob, model.LevelMember), model.CodeForbidden)
	// 285: ad-hoc channels have no manager, so nobody bans there; never a
	// sealed chat; never a shared channel.
	wantCode(t, "a level of manager in an ad-hoc channel bans nobody", CheckChannelBan(stateFor(mgr, adhoc, model.LevelManager), bob, model.LevelMember), model.CodeForbidden)
	wantCode(t, "an ad-hoc member bans", CheckChannelBan(stateFor(bob, adhoc, model.LevelMember), mgr, model.LevelManager), model.CodeForbidden)
	wantCode(t, "a sealed chat", CheckChannelBan(stateFor(mgr, sealed, model.LevelMember), bob, model.LevelMember), model.CodeForbidden)
	wantCode(t, "a shared channel", CheckChannelBan(stateFor(mgr, shared, model.LevelManager), bob, model.LevelMember), model.CodeConflict)
	wantCode(t, "lifting in a shared channel", CheckChannelUnban(stateFor(mgr, shared, model.LevelManager)), "")
	// 286: a timed-out manager uses no powers there.
	timed := stateFor(mgr, pubConv, model.LevelManager)
	timed.TimedOutUntil = 5000
	wantCode(t, "a timed-out manager", CheckChannelBan(timed, bob, model.LevelMember), model.CodeTimedOut)

	// 287: the banned do not see, read or join a public channel, and the
	// admin console still sees it.
	banned := stateFor(bob, pubConv, "")
	banned.ChannelBanned = true
	for _, a := range []Action{See, Read, Join, Search, ListMembers} {
		wantCode(t, "a banned member: "+string(a), Check(banned, a), model.CodeNotFound)
	}
	bannedAdmin := stateFor(admin, pubConv, "")
	bannedAdmin.ChannelBanned = true
	wantCode(t, "a banned admin reads", Check(bannedAdmin, Read), model.CodeNotFound)
	wantCode(t, "a banned admin's console", Check(bannedAdmin, AdminView), "")
	// 288: with the ban gone, all is as before.
	wantCode(t, "unbanned, a member joins", Check(stateFor(bob, pubConv, ""), Join), "")
}
