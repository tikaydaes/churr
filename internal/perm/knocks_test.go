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

// TestKnockChannel is docs/PERMISSIONS.md section 6.2 (ruling R79): a knock
// channel is seen, not read or joined, by owner, admin and member accounts
// outside it; members read it; guests and bots outside see nothing; a ban
// hides it.
func TestKnockChannel(t *testing.T) {
	knock := model.Conversation{ID: "k", Kind: model.KindChannel, Name: "council", Private: true, Knock: true}
	owner, admin, bob := acct("o", model.RoleOwner), acct("a", model.RoleAdmin), acct("b", model.RoleMember)
	guest, bot := acct("g", model.RoleGuest), acct("bot", model.RoleBot)
	for _, a := range []model.Account{owner, admin, bob} {
		st := stateFor(a, knock, "")
		if !st.CanSee() || st.CanRead() {
			t.Errorf("%s outside: sees %v, reads %v", a.Role, st.CanSee(), st.CanRead())
		}
		wantCode(t, string(a.Role)+" reads", Check(st, Read), model.CodeNotFound)
		wantCode(t, string(a.Role)+" searches", Check(st, Search), model.CodeNotFound)
		wantCode(t, string(a.Role)+" lists the members", Check(st, ListMembers), model.CodeNotFound)
		wantCode(t, string(a.Role)+" joins", Check(st, Join), model.CodeForbidden)
		wantCode(t, string(a.Role)+" receives live", Check(st, ReceiveLive), model.CodeForbidden)
		wantCode(t, string(a.Role)+" answers knocks", Check(st, AnswerKnock), model.CodeForbidden)
	}
	for _, a := range []model.Account{guest, bot} {
		st := stateFor(a, knock, "")
		if st.CanSee() {
			t.Errorf("%s outside sees a knock channel", a.Role)
		}
	}
	for _, a := range []model.Account{bob, guest, bot} {
		if st := stateFor(a, knock, model.LevelMember); !st.CanRead() {
			t.Errorf("%s member cannot read", a.Role)
		}
	}
	banned := stateFor(bob, knock, "")
	banned.ChannelBanned = true
	if banned.CanSee() {
		t.Error("a ban does not hide a knock channel")
	}
	// A knock channel's managers change its access; an admin outside it
	// does not act as its manager.
	wantCode(t, "a manager changes access", Check(stateFor(bob, knock, model.LevelManager), MakePrivate), "")
	wantCode(t, "an admin outside", Check(stateFor(admin, knock, ""), MakePrivate), model.CodeForbidden)
	wantCode(t, "a private channel", Check(stateFor(bob, privConv, model.LevelManager), MakePrivate), model.CodeForbidden)
}
