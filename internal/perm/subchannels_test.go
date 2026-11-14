package perm

import (
	"testing"

	"github.com/tikaydaes/churr/internal/model"
)

// docs/PERMISSIONS.md section 7.2; the numbers are the cases of section 17.
func TestSetParent(t *testing.T) {
	owner, admin := acct("o", model.RoleOwner), acct("a", model.RoleAdmin)
	mgr, bob, guest := acct("m", model.RoleMember), acct("b", model.RoleMember), acct("g", model.RoleGuest)
	pub2 := model.Conversation{ID: "pub2", Kind: model.KindChannel, Name: "pub2"}
	sub := model.Conversation{ID: "sub", Kind: model.KindChannel, Name: "sub", ParentID: "pub"}
	adhoc := model.Conversation{ID: "adhoc", Kind: model.KindChannel, Private: true}
	sealed := model.Conversation{ID: "sealed", Kind: model.KindSealed}
	in := func(a model.Account, c model.Conversation, l model.Level) *State {
		st := stateFor(a, c, l)
		return &st
	}
	set := func(child, parent *State) error { return CheckSetParent(*child, parent, false, false) }

	wantCode(t, "217: a manager of PUB2 who is a member of PUB", set(in(mgr, pub2, model.LevelManager), in(mgr, pubConv, model.LevelReader)), "")
	wantCode(t, "218: not a member of PUB", set(in(mgr, pub2, model.LevelManager), in(mgr, pubConv, "")), model.CodeForbidden)
	wantCode(t, "219: PRIV they are not in", set(in(mgr, pub2, model.LevelManager), in(mgr, privConv, "")), model.CodeNotFound)
	wantCode(t, "220: member level in both, set", set(in(bob, pub2, model.LevelMember), in(bob, pubConv, model.LevelMember)), model.CodeForbidden)
	wantCode(t, "220: member level in both, clear", set(in(bob, sub, model.LevelMember), nil), model.CodeForbidden)
	wantCode(t, "221: an admin in neither", set(in(admin, pub2, ""), in(admin, pubConv, "")), "")
	wantCode(t, "the owner too", set(in(owner, pub2, ""), in(owner, pubConv, "")), "")
	wantCode(t, "222: a sub-channel as parent", set(in(mgr, pub2, model.LevelManager), in(mgr, sub, model.LevelManager)), model.CodeConflict)
	wantCode(t, "223: a channel with sub-channels given a parent",
		CheckSetParent(*in(mgr, pub2, model.LevelManager), in(mgr, pubConv, model.LevelMember), true, false), model.CodeConflict)
	wantCode(t, "223: for an admin too", CheckSetParent(*in(owner, pub2, ""), in(owner, pubConv, ""), true, false), model.CodeConflict)
	wantCode(t, "224: a parent for ADHOC", set(in(mgr, adhoc, model.LevelManager), in(mgr, pubConv, model.LevelMember)), model.CodeInvalidValue)
	wantCode(t, "224: ADHOC as parent", set(in(mgr, pub2, model.LevelManager), in(mgr, adhoc, model.LevelManager)), model.CodeInvalidValue)
	wantCode(t, "a sealed chat as parent", set(in(mgr, pub2, model.LevelManager), in(mgr, sealed, model.LevelMember)), model.CodeInvalidValue)
	wantCode(t, "an ad-hoc channel the caller is not in", set(in(mgr, pub2, model.LevelManager), in(mgr, adhoc, "")), model.CodeNotFound)
	wantCode(t, "225: its own parent", set(in(mgr, pub2, model.LevelManager), in(mgr, pub2, model.LevelManager)), model.CodeInvalidValue)

	// Clearing needs no membership of the parent.
	wantCode(t, "a manager not in the parent clears", set(in(mgr, sub, model.LevelManager), nil), "")
	// A private parent the manager is in.
	wantCode(t, "a private parent they are a member of", set(in(mgr, pub2, model.LevelManager), in(mgr, privConv, model.LevelMember)), "")
	// An admin outside a private child: the ordinary operation does not
	// know the channel; the admin view does.
	privChild := model.Conversation{ID: "pc", Kind: model.KindChannel, Name: "pc", Private: true}
	wantCode(t, "an admin outside a private channel, ordinary path", set(in(admin, privChild, ""), in(admin, pubConv, "")), model.CodeNotFound)
	wantCode(t, "the same from the admin view", CheckSetParent(*in(admin, privChild, ""), in(admin, pubConv, ""), false, true), "")
	wantCode(t, "an admin puts a channel under a private channel they are not in", CheckSetParent(*in(admin, pub2, ""), in(admin, privConv, ""), false, true), "")
	wantCode(t, "the admin view is for admins", CheckSetParent(*in(mgr, pub2, model.LevelManager), in(mgr, pubConv, model.LevelMember), false, true), model.CodeForbidden)
	wantCode(t, "the admin view has no ad-hoc channels", CheckSetParent(*in(admin, adhoc, ""), nil, false, true), model.CodeNotFound)
	archived := pub2
	archived.ArchivedAt = 5
	wantCode(t, "an archived channel", set(in(mgr, archived, model.LevelManager), in(mgr, pubConv, model.LevelMember)), model.CodeArchived)
	wantCode(t, "a guest who manages a channel but cannot see the public parent", set(in(guest, pub2, model.LevelManager), in(guest, pubConv, "")), model.CodeNotFound)

	// At creation: a member of the parent, or an admin.
	wantCode(t, "create under a channel one is in", CheckCreateUnder(*in(bob, pubConv, model.LevelReader)), "")
	wantCode(t, "create under one not joined", CheckCreateUnder(*in(bob, pubConv, "")), model.CodeForbidden)
	wantCode(t, "create under a private one not joined", CheckCreateUnder(*in(bob, privConv, "")), model.CodeNotFound)
	wantCode(t, "an admin creates under any named channel", CheckCreateUnder(*in(admin, privConv, "")), "")
	wantCode(t, "create under a sub-channel", CheckCreateUnder(*in(bob, sub, model.LevelMember)), model.CodeConflict)
	wantCode(t, "create under an ad-hoc channel", CheckCreateUnder(*in(bob, adhoc, model.LevelMember)), model.CodeInvalidValue)
}

func TestSubChannelsInheritNothing(t *testing.T) {
	mgr, bob, admin := acct("m", model.RoleMember), acct("b", model.RoleMember), acct("a", model.RoleAdmin)
	privSub := model.Conversation{ID: "ps", Kind: model.KindChannel, Name: "ps", Private: true, ParentID: "pub"}
	pubSub := model.Conversation{ID: "s", Kind: model.KindChannel, Name: "s", ParentID: "priv"}
	// 226, 227: a member, even a manager, of the parent is told nothing of
	// a private sub-channel they are not in. The state holds no trace of
	// the parent, so there is nothing to inherit from.
	wantCode(t, "226: fetch a private sub-channel", Check(stateFor(bob, privSub, ""), See), model.CodeNotFound)
	wantCode(t, "227: read its messages", Check(stateFor(mgr, privSub, ""), Read), model.CodeNotFound)
	wantCode(t, "an admin outside it, ordinary path", Check(stateFor(admin, privSub, ""), Read), model.CodeNotFound)
	wantCode(t, "an admin in the admin view", Check(stateFor(admin, privSub, ""), AdminView), "")
	// 228: being in a sub-channel gives nothing in the parent.
	wantCode(t, "228: post in the private parent", Check(stateFor(bob, privConv, ""), Post), model.CodeNotFound)
	wantCode(t, "228: post in the public parent without joining", Check(stateFor(bob, pubConv, ""), Post), model.CodeForbidden)
	// 230: a public sub-channel of a private parent is an ordinary public
	// channel; the reader is not told its parent.
	wantCode(t, "230: read a public sub-channel of PRIV", Check(stateFor(bob, pubSub, ""), Read), "")
	if SeesParent(stateFor(bob, privConv, "")) {
		t.Errorf("230: someone outside PRIV must not be given the parent_id")
	}
	if !SeesParent(stateFor(bob, privConv, model.LevelReader)) || !SeesParent(stateFor(bob, pubConv, "")) {
		t.Errorf("a member of the parent, and anyone who can see a public parent, are given it")
	}
	if !SeesParent(stateFor(admin, privConv, "")) {
		t.Errorf("an admin sees named private channels in the admin view, so is given it")
	}
	if SeesParent(stateFor(acct("g", model.RoleGuest), pubConv, "")) {
		t.Errorf("a guest cannot see a public channel it is not in")
	}

	// 233: where a channel the server makes sits.
	if got := SubChannelParent(pubConv); got != "pub" {
		t.Errorf("out of PUB: %q", got)
	}
	if got := SubChannelParent(privSub); got != "pub" {
		t.Errorf("out of a sub-channel of PUB, a sibling: %q", got)
	}
	if got := SubChannelParent(model.Conversation{ID: "x", Kind: model.KindChannel, Private: true}); got != "" {
		t.Errorf("out of ADHOC: %q", got)
	}
}
