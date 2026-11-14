package storetest

import (
	"testing"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

func init() { register("groups", testGroups) }

func testGroups(t *testing.T, s store.Store) {
	for i := 0; i < 3; i++ {
		must(t, s.Groups().EnsureBuiltin(ctx))
	}
	owner := newAccount(t, s, "owner", model.RoleOwner)
	admin := newAccount(t, s, "admin", model.RoleAdmin)
	ann := newAccount(t, s, "ann", model.RoleMember)
	guest := newAccount(t, s, "guest", model.RoleGuest)

	list, err := s.Groups().List(ctx, ann.ID)
	must(t, err)
	eq(t, "the two built-in groups exist once", len(list), 2)
	everyone, err := s.Groups().Builtin(ctx, model.GroupEveryone)
	must(t, err)
	admins, err := s.Groups().Builtin(ctx, model.GroupAdmins)
	must(t, err)

	// Built-in membership follows roles.
	ids, err := s.Groups().MemberIDs(ctx, everyone.ID)
	must(t, err)
	eq(t, "@everyone", len(ids), 3)
	ids, _ = s.Groups().MemberIDs(ctx, admins.ID)
	eq(t, "@admins", len(ids), 2)
	in, _ := s.Groups().IsMember(ctx, everyone.ID, guest.ID)
	eq(t, "a guest is not in @everyone", in, false)
	in, _ = s.Groups().IsMember(ctx, admins.ID, admin.ID)
	eq(t, "an admin is in @admins", in, true)
	g, _ := s.Groups().Get(ctx, admins.ID, owner.ID)
	eq(t, "count and is_member of a built-in", g.MemberCount == 2 && g.IsMember, true)

	// They cannot be changed.
	_, err = s.Groups().Update(ctx, everyone.ID, func(g *model.Group) error { g.Name = "All"; return nil })
	wantErr(t, err, model.ErrConflict)
	_, err = s.Groups().Delete(ctx, everyone.ID)
	wantErr(t, err, model.ErrConflict)
	_, err = s.Groups().AddMember(ctx, admins.ID, ann.ID, owner.ID, model.Now())
	wantErr(t, err, model.ErrConflict)

	// Ordinary groups.
	design := newGroup(t, s, "design", ann.ID)
	dup := model.Group{Handle: "design", Name: "Again"}
	wantErr(t, s.Groups().Create(ctx, &dup), model.ErrNameTaken)
	taken := model.Group{Handle: "ann", Name: "A username"}
	wantErr(t, s.Groups().Create(ctx, &taken), model.ErrNameTaken)
	_, err = s.Groups().AddMember(ctx, design.ID, ann.ID, owner.ID, model.Now())
	wantErr(t, err, model.ErrAlreadyExists)
	_, err = s.Groups().AddMember(ctx, design.ID, guest.ID, owner.ID, model.Now())
	must(t, err) // admins put guests into groups
	gone := newAccount(t, s, "gone", model.RoleMember)
	_, err = s.Accounts().Remove(ctx, gone.ID, owner.ID, model.Now())
	must(t, err)
	_, err = s.Groups().AddMember(ctx, design.ID, gone.ID, owner.ID, model.Now())
	wantErr(t, err, model.ErrConflict)
	got, err := s.Groups().ByHandle(ctx, "design")
	must(t, err)
	eq(t, "member_count", got.MemberCount, int64(2))
	of, err := s.Groups().OfAccount(ctx, ann.ID)
	must(t, err)
	eq(t, "the groups of an account leave built-ins out", len(of) == 1 && of[0] == design.ID, true)
	out, err := s.Groups().Update(ctx, design.ID, func(g *model.Group) error { g.Name, g.Open, g.Color = "Design team", true, "pink"; return nil })
	must(t, err)
	eq(t, "updated", out.Name+out.Color, "Design teampink")
	_, err = s.Groups().Update(ctx, design.ID, func(g *model.Group) error { g.Handle = "owner"; return nil })
	wantErr(t, err, model.ErrNameTaken)

	// A group in channels: two groups at two levels (PERMISSIONS case 99).
	c := newChannel(t, s, "studio", true, owner.ID)
	leads := newGroup(t, s, "leads", ann.ID)
	_, _, err = s.Members().AddGroup(ctx, model.ChannelGroup{ConversationID: c.ID, GroupID: design.ID, Level: model.LevelMember})
	must(t, err)
	_, _, err = s.Members().AddGroup(ctx, model.ChannelGroup{ConversationID: c.ID, GroupID: leads.ID, Level: model.LevelManager})
	must(t, err)
	eq(t, "the highest of two groups", level(t, s, c.ID, ann.ID), model.LevelManager)
	chans, err := s.Groups().Channels(ctx, design.ID)
	must(t, err)
	eq(t, "channels of a group", len(chans) == 1 && chans[0].ConversationID == c.ID, true)
	_, err = s.Groups().RemoveMember(ctx, leads.ID, ann.ID)
	must(t, err)
	eq(t, "out of the manager group", level(t, s, c.ID, ann.ID), model.LevelMember)
	_, err = s.Groups().RemoveMember(ctx, leads.ID, ann.ID)
	wantErr(t, err, model.ErrNotFound)

	// Changing a group's level in a channel is an update, not a second row.
	_, added, err := s.Members().AddGroup(ctx, model.ChannelGroup{ConversationID: c.ID, GroupID: design.ID, Level: model.LevelReader})
	must(t, err)
	eq(t, "changed, not added", added, false)
	eq(t, "the new level", level(t, s, c.ID, ann.ID), model.LevelReader)

	// Deleting a group takes away what it alone brought (case 103).
	res, err := s.Groups().Delete(ctx, design.ID)
	must(t, err)
	eq(t, "both members left the channel", len(res.Changes), 2)
	eq(t, "ann is out", level(t, s, c.ID, ann.ID), model.Level(""))
	_, err = s.Groups().Get(ctx, design.ID, "")
	wantErr(t, err, model.ErrNotFound)
	groups, _ := s.Members().Groups(ctx, c.ID)
	eq(t, "the channel keeps only the other group", len(groups), 1)
}
