package storetest

import (
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

func init() {
	register("roles", testRoles)
	register("approvals", testApprovals)
}

// testRoles is what the store keeps of the role model (rulings R97, R99):
// the moderator level, a guest held at member however a level would come,
// the keepers of a channel, the last-manager rule passing over guests, and
// an owner's override adding to a locked channel.
func testRoles(t *testing.T, s store.Store) {
	must(t, s.Groups().EnsureBuiltin(ctx))
	ann := newAccount(t, s, "ann", model.RoleOwner)
	mo := newAccount(t, s, "mo", model.RoleMember)
	gil := newAccount(t, s, "gil", model.RoleGuest)
	bob := newAccount(t, s, "bob", model.RoleMember)
	c := newChannel(t, s, "room", true, ann.ID)

	_, err := s.Members().AddDirect(ctx, c.ID, mo.ID, model.LevelModerator, model.Now())
	must(t, err)
	eq(t, "a moderator", level(t, s, c.ID, mo.ID), model.LevelModerator)
	_, err = s.Members().AddDirect(ctx, c.ID, gil.ID, model.LevelManager, model.Now())
	must(t, err)
	eq(t, "a guest added as a manager is a member", level(t, s, c.ID, gil.ID), model.LevelMember)
	_, err = s.Members().SetDirectLevel(ctx, c.ID, gil.ID, model.LevelModerator)
	must(t, err)
	eq(t, "nor a moderator", level(t, s, c.ID, gil.ID), model.LevelMember)
	_, err = s.Members().SetDirectLevel(ctx, c.ID, gil.ID, model.LevelReader)
	must(t, err)
	eq(t, "a guest may read only", level(t, s, c.ID, gil.ID), model.LevelReader)

	// Through a group too.
	leads := newGroup(t, s, "leads", gil.ID, bob.ID)
	_, _, err = s.Members().AddGroup(ctx, model.ChannelGroup{ConversationID: c.ID, GroupID: leads.ID, Level: model.LevelModerator})
	must(t, err)
	eq(t, "a group grants a role", level(t, s, c.ID, bob.ID), model.LevelModerator)
	eq(t, "and a guest in it is a member", level(t, s, c.ID, gil.ID), model.LevelMember)

	keepers, err := s.Members().Keepers(ctx, c.ID)
	must(t, err)
	eq(t, "the keepers", len(keepers), 3) // ann, mo, bob
	managers, _ := s.Members().Managers(ctx, c.ID)
	eq(t, "the managers", len(managers), 1)

	// The last manager leaves: the longest-standing member who is not a
	// guest becomes manager, never the guest.
	g := newChannel(t, s, "guests first", false, ann.ID)
	_, err = s.Members().AddDirect(ctx, g.ID, gil.ID, model.LevelMember, model.Now())
	must(t, err)
	time.Sleep(2 * time.Millisecond)
	_, err = s.Members().AddDirect(ctx, g.ID, bob.ID, model.LevelMember, model.Now())
	must(t, err)
	_, err = s.Members().RemoveDirect(ctx, g.ID, ann.ID)
	must(t, err)
	eq(t, "the guest is passed over", level(t, s, g.ID, gil.ID), model.LevelMember)
	eq(t, "bob manages it", level(t, s, g.ID, bob.ID), model.LevelManager)

	// An owner's override adds to a locked channel; an ordinary add does
	// not.
	cat := newAccount(t, s, "cat", model.RoleMember)
	dan := newAccount(t, s, "dan", model.RoleMember)
	_, err = s.Conversations().Lock(ctx, c.ID, ann.ID, model.Now())
	must(t, err)
	_, err = s.Members().AddDirect(ctx, c.ID, cat.ID, model.LevelMember, model.Now())
	wantErr(t, err, model.ErrLocked)
	_, err = s.Members().AddByOverride(ctx, c.ID, dan.ID, model.LevelMember, model.Now())
	must(t, err)
	eq(t, "added by override", level(t, s, c.ID, dan.ID), model.LevelMember)

	// Reports reach moderators.
	m := post(t, s, c.ID, dan.ID, "", "x")
	r := model.Report{MessageID: m.ID, ConversationID: c.ID, ReportedByID: ann.ID, CreatedAt: model.Now()}
	_, err = s.Moderation().CreateReport(ctx, &r)
	must(t, err)
	got, _, err := s.Moderation().Reports(ctx, store.ReportFilter{Scope: store.ReportScope{AccountID: mo.ID}}, store.Page{})
	must(t, err)
	eq(t, "a moderator handles the channel's reports", len(got), 1)
	got, _, err = s.Moderation().Reports(ctx, store.ReportFilter{Scope: store.ReportScope{AccountID: dan.ID}}, store.Page{})
	must(t, err)
	eq(t, "a member does not", len(got), 0)
}

// testApprovals is the approvals queue: waiting, counted by kind, decided
// once, lapsing at its end, and the approvers' flag.
func testApprovals(t *testing.T, s store.Store) {
	ann := newAccount(t, s, "ann", model.RoleOwner)
	bob := newAccount(t, s, "bob", model.RoleMember)
	now := model.Now()
	reg := model.Approval{Kind: model.ApprovalRegistration, RequesterID: bob.ID, Note: "hello", CreatedAt: now}
	must(t, s.Approvals().Create(ctx, &reg))
	eq(t, "it waits", reg.State, model.ApprovalWaiting)
	eq(t, "for fourteen days", reg.ExpiresAt, now.Add(model.ApprovalLife))
	ch := model.Approval{Kind: model.ApprovalChannel, RequesterID: bob.ID, Payload: []byte(`{"name":"plans"}`), CreatedAt: now.Add(time.Millisecond)}
	must(t, s.Approvals().Create(ctx, &ch))
	old := model.Approval{Kind: model.ApprovalChannel, RequesterID: ann.ID, CreatedAt: now.Add(-20 * 24 * time.Hour)}
	must(t, s.Approvals().Create(ctx, &old))

	list, err := s.Approvals().Waiting(ctx, []string{model.ApprovalRegistration, model.ApprovalChannel}, now, 0)
	must(t, err)
	eq(t, "the waiting, oldest first, without the lapsed", len(list) == 2 && list[0].ID == reg.ID && list[1].ID == ch.ID, true)
	eq(t, "the payload is kept", string(list[1].Payload), `{"name":"plans"}`)
	list, _ = s.Approvals().Waiting(ctx, []string{model.ApprovalChannel}, now, 0)
	eq(t, "by kind", len(list), 1)
	n, err := s.Approvals().CountWaiting(ctx, []string{model.ApprovalRegistration, model.ApprovalChannel}, now)
	must(t, err)
	eq(t, "counted", n[model.ApprovalRegistration] == 1 && n[model.ApprovalChannel] == 1, true)

	got, found, err := s.Approvals().Decide(ctx, reg.ID, model.ApprovalApproved, ann.ID, "", now)
	must(t, err)
	eq(t, "decided", found && got.State == model.ApprovalApproved && got.DecidedBy == ann.ID, true)
	_, found, err = s.Approvals().Decide(ctx, reg.ID, model.ApprovalDeclined, ann.ID, "", now)
	must(t, err)
	eq(t, "decided once", found, false)
	_, found, _ = s.Approvals().Decide(ctx, old.ID, model.ApprovalApproved, ann.ID, "", now)
	eq(t, "not after its end", found, false)
	got, found, err = s.Approvals().Decide(ctx, ch.ID, model.ApprovalDeclined, ann.ID, "not now", now)
	must(t, err)
	eq(t, "declined with a reason", found && got.Reason == "not now", true)
	latest, found, err := s.Approvals().Latest(ctx, bob.ID, model.ApprovalRegistration)
	must(t, err)
	eq(t, "the latest of a kind", found && latest.ID == reg.ID, true)

	// The janitor marks the lapsed.
	_, err = s.Janitor().Tidy(ctx, now)
	must(t, err)
	lapsed, err := s.Approvals().Get(ctx, old.ID)
	must(t, err)
	eq(t, "lapsed", lapsed.State, model.ApprovalLapsed)

	// The flag.
	yes, err := s.Approvals().IsApprover(ctx, bob.ID)
	must(t, err)
	eq(t, "not yet", yes, false)
	must(t, s.Approvals().SetApprover(ctx, bob.ID, true, ann.ID, now))
	must(t, s.Approvals().SetApprover(ctx, bob.ID, true, ann.ID, now))
	yes, _ = s.Approvals().IsApprover(ctx, bob.ID)
	ids, err := s.Approvals().Approvers(ctx)
	must(t, err)
	eq(t, "given", yes && len(ids) == 1 && ids[0] == bob.ID, true)
	must(t, s.Approvals().SetApprover(ctx, bob.ID, false, ann.ID, now))
	yes, _ = s.Approvals().IsApprover(ctx, bob.ID)
	eq(t, "taken away", yes, false)
}
