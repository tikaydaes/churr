package storetest

import (
	"testing"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

func init() { register("members", testMembers) }

func testMembers(t *testing.T, s store.Store) {
	must(t, s.Groups().EnsureBuiltin(ctx))
	must(t, s.Groups().EnsureBuiltin(ctx)) // every start calls it

	owner := newAccount(t, s, "owner", model.RoleOwner)
	ann := newAccount(t, s, "ann", model.RoleMember)
	bob := newAccount(t, s, "bob", model.RoleMember)
	guest := newAccount(t, s, "guest", model.RoleGuest)

	t.Run("level is the highest of direct and group levels", func(t *testing.T) {
		priv := newChannel(t, s, "priv", true, owner.ID)
		readers := newGroup(t, s, "readers", ann.ID)
		_, added, err := s.Members().AddGroup(ctx, model.ChannelGroup{ConversationID: priv.ID, GroupID: readers.ID, Level: model.LevelReader, AddedByID: owner.ID})
		must(t, err)
		eq(t, "added", added, true)
		eq(t, "offered, not placed", level(t, s, priv.ID, ann.ID), model.Level(""))
		accept(t, s, priv.ID, ann.ID)
		eq(t, "through the group", level(t, s, priv.ID, ann.ID), model.LevelReader)

		join(t, s, priv.ID, ann.ID, model.LevelMember)
		eq(t, "direct member beats group reader", level(t, s, priv.ID, ann.ID), model.LevelMember)
		m, _, err := s.Members().Get(ctx, priv.ID, ann.ID)
		must(t, err)
		eq(t, "via_group", m.ViaGroup, true)
		eq(t, "direct_level", m.DirectLevel, model.LevelMember)

		// PERMISSIONS case 98: the direct level goes, the group's stays.
		res, err := s.Members().RemoveDirect(ctx, priv.ID, ann.ID)
		must(t, err)
		eq(t, "changes", len(res.Changes), 1)
		eq(t, "kind", res.Changes[0].Kind, store.MemberLevelChanged)
		eq(t, "back to reader", level(t, s, priv.ID, ann.ID), model.LevelReader)

		// Case 100: out of the group, out of the channel.
		res, err = s.Groups().RemoveMember(ctx, readers.ID, ann.ID)
		must(t, err)
		eq(t, "left", res.Changes[0].Kind, store.MemberLeft)
		eq(t, "no longer a member", level(t, s, priv.ID, ann.ID), model.Level(""))
	})

	t.Run("built-in groups follow roles", func(t *testing.T) {
		general := newChannel(t, s, "general", false, owner.ID)
		everyone, err := s.Groups().Builtin(ctx, model.GroupEveryone)
		must(t, err)
		res, _, err := s.Members().AddGroup(ctx, model.ChannelGroup{ConversationID: general.ID, GroupID: everyone.ID, Level: model.LevelMember})
		must(t, err)
		offered := 0
		for _, ch := range res.Changes {
			if ch.Kind == store.MemberOffered {
				offered++
			}
		}
		eq(t, "offered to ann and bob", offered, 2)
		eq(t, "nobody is placed", level(t, s, general.ID, bob.ID), model.Level(""))
		accept(t, s, general.ID, bob.ID)
		eq(t, "member is in", level(t, s, general.ID, bob.ID), model.LevelMember)
		eq(t, "guest is not", level(t, s, general.ID, guest.ID), model.Level(""))
		_, err = s.Members().AnswerOffer(ctx, general.ID, guest.ID, true, model.Now())
		wantErr(t, err, model.ErrNotFound)
		eq(t, "owner keeps manager", level(t, s, general.ID, owner.ID), model.LevelManager)

		// Case 104: a new member account is offered @everyone's channels.
		cat := newAccount(t, s, "cat", model.RoleMember)
		eq(t, "new member is offered, not in", level(t, s, general.ID, cat.ID), model.Level(""))
		offers, err := s.Members().Offers(ctx, cat.ID)
		must(t, err)
		eq(t, "the offer", len(offers) == 1 && offers[0].ConversationID == general.ID && offers[0].Level == model.LevelMember, true)
		accept(t, s, general.ID, cat.ID)
		eq(t, "new member is in", level(t, s, general.ID, cat.ID), model.LevelMember)

		// Case 106: member to guest leaves what @everyone alone gave.
		_, res, err = s.Accounts().SetRole(ctx, cat.ID, model.RoleGuest)
		must(t, err)
		eq(t, "left by role change", len(res.Changes), 1)
		eq(t, "guest is out", level(t, s, general.ID, cat.ID), model.Level(""))

		g, err := s.Groups().Get(ctx, everyone.ID, bob.ID)
		must(t, err)
		eq(t, "is_member", g.IsMember, true)
		eq(t, "member_count", g.MemberCount, int64(3)) // owner, ann, bob
	})

	t.Run("last manager and last member", func(t *testing.T) {
		c := newChannel(t, s, "team", true, ann.ID)
		join(t, s, c.ID, bob.ID, model.LevelMember)
		_, err := s.Members().SetDirectLevel(ctx, c.ID, ann.ID, model.LevelMember)
		wantErr(t, err, model.ErrConflict) // case 66
		eq(t, "still manager", level(t, s, c.ID, ann.ID), model.LevelManager)

		res, err := s.Members().RemoveDirect(ctx, c.ID, ann.ID) // case 67
		must(t, err)
		eq(t, "bob promoted", level(t, s, c.ID, bob.ID), model.LevelManager)
		eq(t, "two changes", len(res.Changes), 2)

		res, err = s.Members().RemoveDirect(ctx, c.ID, bob.ID)
		must(t, err)
		eq(t, "deleted", len(res.Deleted), 1)
		_, err = s.Conversations().Get(ctx, c.ID)
		wantErr(t, err, model.ErrNotFound)
	})

	t.Run("locking", func(t *testing.T) {
		c := newChannel(t, s, "vault", true, ann.ID)
		g := newGroup(t, s, "crew", bob.ID)
		_, _, err := s.Members().AddGroup(ctx, model.ChannelGroup{ConversationID: c.ID, GroupID: g.ID, Level: model.LevelMember})
		must(t, err)
		accept(t, s, c.ID, bob.ID)
		locked, err := s.Conversations().Lock(ctx, c.ID, ann.ID, model.Now())
		must(t, err)
		eq(t, "locked", locked.Locked(), true)
		m, _, err := s.Members().Get(ctx, c.ID, bob.ID)
		must(t, err)
		eq(t, "now direct", m.DirectLevel, model.LevelMember)
		eq(t, "not via group", m.ViaGroup, false)
		groups, err := s.Members().Groups(ctx, c.ID)
		must(t, err)
		eq(t, "groups detached", len(groups), 0)
		_, err = s.Members().AddDirect(ctx, c.ID, guest.ID, model.LevelMember, model.Now())
		wantErr(t, err, model.ErrLocked)
		_, _, err = s.Members().AddGroup(ctx, model.ChannelGroup{ConversationID: c.ID, GroupID: g.ID, Level: model.LevelMember})
		wantErr(t, err, model.ErrLocked)
	})

	t.Run("read position and unread sets", func(t *testing.T) {
		c := newChannel(t, s, "talk", false, ann.ID)
		join(t, s, c.ID, bob.ID, model.LevelMember)
		p := post(t, s, c.ID, ann.ID, "", "hello")
		post(t, s, c.ID, ann.ID, p.ID, "a reply")
		post(t, s, c.ID, ann.ID, "", "@bob look", model.Mention{AccountID: bob.ID, Kind: model.MentionAccount})
		u, err := s.Members().Unread(ctx, c.ID, bob.ID)
		must(t, err)
		eq(t, "top", u.Top, int64(2))
		eq(t, "nei", u.Nei, int64(1))
		eq(t, "men", u.Men, int64(1))
		unread, mentions, badge := u.Counts(model.ModeMentions, true)
		eq(t, "unread", unread, int64(3))
		eq(t, "mentions", mentions, int64(1))
		eq(t, "badge", badge, int64(1))

		m, err := s.Members().SetRead(ctx, c.ID, bob.ID, 999, false)
		must(t, err)
		eq(t, "clamped to the latest seq", m.ReadSeq, int64(3))
		u, err = s.Members().Unread(ctx, c.ID, bob.ID)
		must(t, err)
		eq(t, "all read", u.Top+u.Nei+u.Men, int64(0))
	})

	t.Run("removal", func(t *testing.T) {
		dee := newAccount(t, s, "dee", model.RoleMember)
		c := newChannel(t, s, "dees", true, dee.ID)
		out, err := s.Accounts().Remove(ctx, dee.ID, owner.ID, model.Now())
		must(t, err)
		eq(t, "removed", out.Account.Removed(), true)
		eq(t, "its private channel went with it", len(out.Deleted) >= 1, true)
		_, err = s.Conversations().Get(ctx, c.ID)
		wantErr(t, err, model.ErrNotFound)
		_, err = s.Accounts().Remove(ctx, dee.ID, owner.ID, model.Now())
		wantErr(t, err, model.ErrConflict)
		// The username stays taken (decision P17).
		again := model.Account{Username: "dee", DisplayName: "Dee", Role: model.RoleMember}
		_, err = s.Accounts().Create(ctx, &again)
		wantErr(t, err, model.ErrNameTaken)
	})
}
