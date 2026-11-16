package perm

import (
	"testing"

	"github.com/tikaydaes/churr/internal/model"
)

// TestKnockAndBroadcast is docs/PERMISSIONS.md sections 6.2 and 9.1 (ruling
// R74): who may knock, who answers, and who sends a server broadcast.
func TestKnockAndBroadcast(t *testing.T) {
	owner, admin, mgr := acct("o", model.RoleOwner), acct("a", model.RoleAdmin), acct("m", model.RoleMember)
	bob, guest, bot := acct("b", model.RoleMember), acct("g", model.RoleGuest), acct("bot", model.RoleBot)
	var set Settings

	// Owner, admin and member accounts knock; guests and bots do not.
	for _, a := range []model.Account{owner, admin, bob} {
		wantCode(t, string(a.Role)+" knocks", CheckServer(a, Knock, set), "")
	}
	for _, a := range []model.Account{guest, bot} {
		wantCode(t, string(a.Role)+" knocks", CheckServer(a, Knock, set), model.CodeForbidden)
	}
	// Those acting as manager of a named channel answer; members do not;
	// an admin outside a private channel cannot see it.
	wantCode(t, "a manager answers", Check(stateFor(mgr, privConv, model.LevelManager), AnswerKnock), "")
	wantCode(t, "a member answers", Check(stateFor(bob, privConv, model.LevelMember), AnswerKnock), model.CodeForbidden)
	wantCode(t, "an admin outside a private channel", Check(stateFor(admin, privConv, ""), AnswerKnock), model.CodeNotFound)
	adhoc := model.Conversation{ID: "adhoc", Kind: model.KindChannel, Private: true}
	wantCode(t, "not in an ad-hoc channel", Check(stateFor(mgr, adhoc, model.LevelManager), AnswerKnock), model.CodeForbidden)
	locked := privConv
	locked.LockedAt = model.Now()
	wantCode(t, "not in a locked channel", Check(stateFor(mgr, locked, model.LevelManager), AnswerKnock), model.CodeLocked)

	// The broadcast is the owner's and the admins'.
	wantCode(t, "the owner broadcasts", CheckServer(owner, SendBroadcast, set), "")
	wantCode(t, "an admin broadcasts", CheckServer(admin, SendBroadcast, set), "")
	for _, a := range []model.Account{bob, guest, bot} {
		wantCode(t, string(a.Role)+" broadcasts", CheckServer(a, SendBroadcast, set), model.CodeForbidden)
	}
}
