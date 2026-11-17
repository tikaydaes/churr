package perm

import (
	"errors"
	"testing"

	"github.com/tikaydaes/churr/internal/model"
)

func acct(id string, role model.Role) model.Account {
	return model.Account{ID: model.AccountID(id), Role: role, ClaimedAt: 1}
}

func stateFor(actor model.Account, conv model.Conversation, level model.Level) State {
	st := State{Actor: actor, Conversation: conv, Settings: DefaultSettings(), Now: 1000}
	if level != "" {
		st.Member = &model.Member{ConversationID: conv.ID, AccountID: actor.ID, Level: level, DirectLevel: level}
	}
	return st
}

var (
	pubConv  = model.Conversation{ID: "pub", Kind: model.KindChannel, Name: "pub"}
	privConv = model.Conversation{ID: "priv", Kind: model.KindChannel, Name: "priv", Private: true}
	library  = model.Conversation{ID: "lib", Kind: model.KindChannel, Private: true, MemberKey: model.LibraryMemberKey}
)

func wantCode(t *testing.T, what string, err error, want model.Code) {
	t.Helper()
	if got := model.CodeOf(err); got != want {
		t.Errorf("%s: got %q (%v), want %q", what, got, err, want)
	}
}

func TestCheckFile(t *testing.T) {
	owner, ann, bob, guest := acct("o", model.RoleOwner), acct("a", model.RoleMember), acct("b", model.RoleMember), acct("g", model.RoleGuest)
	attached := model.File{ID: "f", ConversationID: "priv", MessageID: "m", UploaderID: ann.ID}
	msg := model.Message{ID: "m", ConversationID: "priv", AuthorID: ann.ID}
	wantCode(t, "a member downloads", CheckFile(stateFor(bob, privConv, model.LevelReader), attached, &msg), "")
	// Section 4.1: an admin outside a private channel reads nothing.
	wantCode(t, "the owner outside a private channel", CheckFile(stateFor(owner, privConv, ""), attached, &msg), model.CodeNotFound)
	wantCode(t, "a member outside it", CheckFile(stateFor(bob, privConv, ""), attached, &msg), model.CodeNotFound)

	inPub := model.File{ID: "f", ConversationID: "pub", MessageID: "m", UploaderID: ann.ID}
	pubMsg := model.Message{ID: "m", ConversationID: "pub", AuthorID: ann.ID}
	wantCode(t, "a member reads a public channel without joining", CheckFile(stateFor(bob, pubConv, ""), inPub, &pubMsg), "")
	wantCode(t, "a guest does not", CheckFile(stateFor(guest, pubConv, ""), inPub, &pubMsg), model.CodeNotFound)
	wantCode(t, "a file of another conversation", CheckFile(stateFor(bob, privConv, model.LevelMember), inPub, &pubMsg), model.CodeNotFound)

	// An upload not yet attached: its uploader only.
	upload := model.File{ID: "u", ConversationID: "pub", UploaderID: ann.ID}
	wantCode(t, "the uploader", CheckFile(stateFor(ann, pubConv, model.LevelMember), upload, nil), "")
	wantCode(t, "someone else", CheckFile(stateFor(bob, pubConv, model.LevelManager), upload, nil), model.CodeNotFound)

	// A detached file follows the rule for history.
	detached := inPub
	detached.DetachedAt = 5
	wantCode(t, "history is open", CheckFile(stateFor(bob, pubConv, model.LevelMember), detached, &pubMsg), "")
	hidden := pubMsg
	hidden.HistoryHiddenAt = 6
	wantCode(t, "history hidden, the author", CheckFile(stateFor(ann, pubConv, model.LevelMember), detached, &hidden), model.CodeNotFound)
	wantCode(t, "history hidden, an admin", CheckFile(stateFor(owner, pubConv, ""), detached, &hidden), "")
	wantCode(t, "a detached file whose message has gone", CheckFile(stateFor(bob, pubConv, model.LevelMember), detached, nil), model.CodeNotFound)

	// Decision P5: avatars and emoji for any signed-in account.
	for _, purpose := range []string{model.PurposeAvatar, model.PurposeEmoji} {
		wantCode(t, purpose, CheckFile(stateFor(guest, library, ""), model.File{ConversationID: "lib", Purpose: purpose}, nil), "")
	}
	wantCode(t, "a cached preview image", CheckFile(stateFor(owner, library, ""), model.File{ConversationID: "lib", Purpose: model.PurposeCard}, nil), model.CodeNotFound)
	removed := ann
	removed.RemovedAt = 9
	wantCode(t, "a removed account", CheckFile(stateFor(removed, library, ""), model.File{ConversationID: "lib", Purpose: model.PurposeEmoji}, nil), model.CodeUnauthenticated)
}

func TestCheckTimeOut(t *testing.T) {
	owner, admin, admin2 := acct("o", model.RoleOwner), acct("a", model.RoleAdmin), acct("a2", model.RoleAdmin)
	mgr, bob := acct("m", model.RoleMember), acct("b", model.RoleMember)
	mod := acct("mo", model.RoleMember)
	// Section 11.
	wantCode(t, "a manager times out a member", CheckTimeOut(stateFor(mgr, privConv, model.LevelManager), bob, model.LevelMember), "")
	wantCode(t, "a moderator times out a member", CheckTimeOut(stateFor(mod, privConv, model.LevelModerator), bob, model.LevelMember), "")
	wantCode(t, "a member cannot", CheckTimeOut(stateFor(bob, privConv, model.LevelMember), mgr, model.LevelManager), model.CodeForbidden)
	wantCode(t, "a moderator cannot time out a manager", CheckTimeOut(stateFor(mod, privConv, model.LevelModerator), mgr, model.LevelManager), model.CodeForbidden)
	wantCode(t, "a manager times out a moderator", CheckTimeOut(stateFor(mgr, privConv, model.LevelManager), mod, model.LevelModerator), "")
	wantCode(t, "a manager times out an admin", CheckTimeOut(stateFor(mgr, privConv, model.LevelManager), admin, model.LevelMember), "")
	wantCode(t, "nor an owner", CheckTimeOut(stateFor(mgr, privConv, model.LevelManager), owner, model.LevelMember), model.CodeForbidden)
	wantCode(t, "an admin who is no manager", CheckTimeOut(stateFor(admin, pubConv, ""), bob, ""), model.CodeForbidden)
	wantCode(t, "an owner who is no manager", CheckTimeOut(stateFor(owner, pubConv, model.LevelMember), admin2, model.LevelMember), model.CodeForbidden)
	wantCode(t, "an admin outside a private channel", CheckTimeOut(stateFor(admin, privConv, ""), bob, ""), model.CodeNotFound)
	wantCode(t, "not oneself", CheckTimeOut(stateFor(mgr, privConv, model.LevelManager), mgr, model.LevelManager), model.CodeForbidden)
	timedOut := stateFor(mgr, privConv, model.LevelManager)
	timedOut.TimedOutUntil = 2000
	wantCode(t, "a timed-out manager has no management powers", CheckTimeOut(timedOut, bob, model.LevelMember), model.CodeTimedOut)

	c := New(nil)
	wantCode(t, "server-wide: admin on member", c.CanTimeOutEverywhere(admin, bob), "")
	wantCode(t, "server-wide: admin on admin", c.CanTimeOutEverywhere(admin, admin2), model.CodeForbidden)
	wantCode(t, "server-wide: owner on admin", c.CanTimeOutEverywhere(owner, admin), "")
	wantCode(t, "server-wide: nobody on the owner", c.CanTimeOutEverywhere(admin, owner), model.CodeForbidden)
	wantCode(t, "server-wide: a member cannot", c.CanTimeOutEverywhere(bob, mgr), model.CodeForbidden)
}

func TestCheckRetention(t *testing.T) {
	admin, mgr, bob := acct("a", model.RoleAdmin), acct("m", model.RoleMember), acct("b", model.RoleMember)
	// Section 7 and FEATURES section 6.
	none := stateFor(mgr, pubConv, model.LevelManager)
	wantCode(t, "no server period: a manager sets any", CheckRetention(none, 10*365*86400), "")
	week := stateFor(mgr, pubConv, model.LevelManager)
	week.Settings.RetentionSeconds = 7 * 86400
	wantCode(t, "shorter than the server's", CheckRetention(week, 86400), "")
	wantCode(t, "as long as the server's", CheckRetention(week, 7*86400), model.CodeForbidden)
	wantCode(t, "longer", CheckRetention(week, 30*86400), model.CodeForbidden)
	wantCode(t, "clearing it", CheckRetention(week, 0), "")
	adm := stateFor(admin, pubConv, "")
	adm.Settings.RetentionSeconds = 7 * 86400
	wantCode(t, "an admin who is no manager sets none", CheckRetention(adm, 30*86400), model.CodeForbidden)
	admMgr := stateFor(admin, pubConv, model.LevelManager)
	admMgr.Settings.RetentionSeconds = 7 * 86400
	wantCode(t, "an admin who is a manager sets any", CheckRetention(admMgr, 30*86400), "")
	wantCode(t, "a member sets none", CheckRetention(stateFor(bob, pubConv, model.LevelMember), 60), model.CodeForbidden)
	adhoc := model.Conversation{ID: "x", Kind: model.KindChannel, Private: true}
	wantCode(t, "never an ad-hoc channel", CheckRetention(stateFor(mgr, adhoc, model.LevelManager), 60), model.CodeForbidden)
}

func TestCheckRemoveEmoji(t *testing.T) {
	admin, ann, bob := acct("a", model.RoleAdmin), acct("ann", model.RoleMember), acct("bob", model.RoleMember)
	e := model.Emoji{ID: "e", CreatedByID: ann.ID}
	open := DefaultSettings()
	open.MembersMayAddEmoji = true
	wantCode(t, "an admin", CheckRemoveEmoji(admin, e, DefaultSettings()), "")
	wantCode(t, "whoever added it, while members may add", CheckRemoveEmoji(ann, e, open), "")
	wantCode(t, "not once members may no longer add", CheckRemoveEmoji(ann, e, DefaultSettings()), model.CodeForbidden)
	wantCode(t, "not someone else's", CheckRemoveEmoji(bob, e, open), model.CodeForbidden)
}

func TestReportScope(t *testing.T) {
	if s := ReportScope(acct("a", model.RoleAdmin)); !s.Admin || s.AccountID != "a" {
		t.Errorf("an admin handles every public channel's reports: %+v", s)
	}
	if s := ReportScope(acct("m", model.RoleMember)); s.Admin {
		t.Errorf("a member handles only where they are manager: %+v", s)
	}
	gone := acct("g", model.RoleAdmin)
	gone.RemovedAt = 1
	if s := ReportScope(gone); s.AccountID != "" {
		t.Errorf("a removed account handles nothing: %+v", s)
	}
}

// Cases 120 to 122 of docs/PERMISSIONS.md section 17, as rules: in a private
// or ad-hoc channel only the stored level counts.
func TestHandleReportsStoredLevel(t *testing.T) {
	owner := acct("o", model.RoleOwner)
	wantCode(t, "120: an admin who is a member but not a manager of PRIV", Check(stateFor(owner, privConv, model.LevelMember), HandleReports), model.CodeForbidden)
	wantCode(t, "122: an admin who has not joined PUB", Check(stateFor(owner, pubConv, ""), HandleReports), "")
	wantCode(t, "a manager of PRIV", Check(stateFor(acct("m", model.RoleMember), privConv, model.LevelManager), HandleReports), "")
	if err := Check(stateFor(owner, privConv, ""), HandleReports); !errors.Is(err, model.ErrNotFound) {
		t.Errorf("an admin outside PRIV: %v", err)
	}
}

// Section 4.1 note 2 and case 178: a channel made from a moved thread.
func TestMovedThreadJoin(t *testing.T) {
	bob := acct("b", model.RoleMember)
	moved := model.Conversation{ID: "d", Kind: model.KindChannel, Name: "d", Private: true, MovedFromMessageID: "p"}
	st := stateFor(bob, moved, "")
	wantCode(t, "a stranger", Check(st, Join), model.CodeNotFound)
	st.FromMovedThreadOf = true
	wantCode(t, "a member of the old channel joins", Check(st, Join), "")
	wantCode(t, "but cannot read before joining", Check(st, Read), model.CodeNotFound)
	st.Conversation.LockedAt = 5
	wantCode(t, "178: not once it is locked", Check(st, Join), model.CodeLocked)
	guest := stateFor(acct("g", model.RoleGuest), moved, "")
	guest.FromMovedThreadOf = true
	wantCode(t, "a guest never joins by itself", Check(guest, Join), model.CodeNotFound)
}
