package storetest

import (
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

// First contact (ruling R103): channel offers, default channels and
// invitations.

func init() { register("first contact", testFirstContact) }

func offeredTo(t *testing.T, s store.Areas, account model.AccountID) []model.ConversationID {
	t.Helper()
	list, err := s.Members().Offers(ctx, account)
	must(t, err)
	var out []model.ConversationID
	for _, o := range list {
		out = append(out, o.ConversationID)
	}
	return out
}

func kinds(res store.MembershipResult, kind string) int {
	n := 0
	for _, ch := range res.Changes {
		if ch.Kind == kind {
			n++
		}
	}
	return n
}

func testFirstContact(t *testing.T, s store.Store) {
	must(t, s.Groups().EnsureBuiltin(ctx))
	owner := newAccount(t, s, "owner", model.RoleOwner)
	ann := newAccount(t, s, "ann", model.RoleMember)
	bob := newAccount(t, s, "bob", model.RoleMember)
	guest := newAccount(t, s, "guest", model.RoleGuest)

	t.Run("a group offers a private channel; decline, then join later", func(t *testing.T) {
		priv := newChannel(t, s, "ops", true, owner.ID)
		crew := newGroup(t, s, "crew", ann.ID, guest.ID)
		res, _, err := s.Members().AddGroup(ctx, model.ChannelGroup{ConversationID: priv.ID, GroupID: crew.ID, Level: model.LevelModerator})
		must(t, err)
		eq(t, "offered to both", kinds(res, store.MemberOffered), 2)
		eq(t, "nobody joined", kinds(res, store.MemberJoined), 0)
		eq(t, "ann not a member", level(t, s, priv.ID, ann.ID), model.Level(""))
		got := offeredTo(t, s, ann.ID)
		eq(t, "in ann's offers", len(got) == 1 && got[0] == priv.ID, true)
		l, ok, err := s.Members().Eligible(ctx, priv.ID, ann.ID)
		must(t, err)
		eq(t, "eligible at the group's level", ok && l == model.LevelModerator, true)
		l, _, err = s.Members().Eligible(ctx, priv.ID, guest.ID)
		must(t, err)
		eq(t, "a guest at member at most", l, model.LevelMember)
		_, ok, err = s.Members().Eligible(ctx, priv.ID, bob.ID)
		must(t, err)
		eq(t, "bob is not", ok, false)

		// Browse lists it for ann though it is private, not for bob.
		list, _, err := s.Conversations().Browse(ctx, store.BrowseFilter{AccountID: ann.ID, Public: true}, store.Page{})
		must(t, err)
		seen := false
		for _, c := range list {
			seen = seen || c.ID == priv.ID
		}
		eq(t, "browse shows it to ann", seen, true)
		list, _, err = s.Conversations().Browse(ctx, store.BrowseFilter{AccountID: bob.ID, Public: true}, store.Page{})
		must(t, err)
		for _, c := range list {
			if c.ID == priv.ID {
				t.Fatal("browse shows a private channel to someone with no access")
			}
		}

		res, err = s.Members().AnswerOffer(ctx, priv.ID, ann.ID, false, model.Now())
		must(t, err)
		eq(t, "declining changes nothing visible", len(res.Changes), 0)
		eq(t, "declined offers leave the list", len(offeredTo(t, s, ann.ID)), 0)
		_, ok, err = s.Members().Eligible(ctx, priv.ID, ann.ID)
		must(t, err)
		eq(t, "still eligible after declining", ok, true)

		res, err = s.Members().AnswerOffer(ctx, priv.ID, ann.ID, true, model.Now())
		must(t, err)
		eq(t, "joined later", kinds(res, store.MemberJoined), 1)
		m, _, err := s.Members().Get(ctx, priv.ID, ann.ID)
		must(t, err)
		eq(t, "via the group", m.ViaGroup && m.DirectLevel == "" && m.Level == model.LevelModerator, true)

		// Leaving: the offer is declined, the membership goes, and she may
		// come back.
		res, err = s.Members().AnswerOffer(ctx, priv.ID, ann.ID, false, model.Now())
		must(t, err)
		eq(t, "left", kinds(res, store.MemberLeft), 1)
		accept(t, s, priv.ID, ann.ID)

		// The group goes: the membership and the guest's waiting offer go.
		res, err = s.Members().RemoveGroup(ctx, priv.ID, crew.ID)
		must(t, err)
		eq(t, "ann left with the group", kinds(res, store.MemberLeft), 1)
		eq(t, "the guest's offer went", kinds(res, store.MemberOfferGone), 1)
		_, found, err := s.Members().Offer(ctx, priv.ID, ann.ID)
		must(t, err)
		eq(t, "no row once no group gives access", found, false)
		_, err = s.Members().AnswerOffer(ctx, priv.ID, ann.ID, true, model.Now())
		wantErr(t, err, model.ErrNotFound)

		// A group that gives access again asks again.
		res, _, err = s.Members().AddGroup(ctx, model.ChannelGroup{ConversationID: priv.ID, GroupID: crew.ID, Level: model.LevelMember})
		must(t, err)
		eq(t, "asked again", kinds(res, store.MemberOffered), 2)
	})

	t.Run("@everyone offers, never places", func(t *testing.T) {
		pub := newChannel(t, s, "town", false, owner.ID)
		everyone, err := s.Groups().Builtin(ctx, model.GroupEveryone)
		must(t, err)
		res, _, err := s.Members().AddGroup(ctx, model.ChannelGroup{ConversationID: pub.ID, GroupID: everyone.ID, Level: model.LevelMember})
		must(t, err)
		eq(t, "ann and bob are offered it", kinds(res, store.MemberOffered), 2)
		eq(t, "nobody is placed", kinds(res, store.MemberJoined), 0)
		accept(t, s, pub.ID, bob.ID)
		m, _, err := s.Members().Get(ctx, pub.ID, bob.ID)
		must(t, err)
		eq(t, "bob in through @everyone", m.ViaGroup, true)
		res, err = s.Members().AnswerOffer(ctx, pub.ID, bob.ID, false, model.Now())
		must(t, err)
		eq(t, "and can leave it", kinds(res, store.MemberLeft), 1)
	})

	t.Run("default channels: joined at sign-up, offered when added later", func(t *testing.T) {
		lobby := newChannel(t, s, "lobby", false, owner.ID)
		news := newChannel(t, s, "news", true, owner.ID)
		must(t, s.Settings().Set(ctx, store.SettingDefaultChannels, []model.ConversationID{lobby.ID}, ""))
		cat := newAccount(t, s, "cat", model.RoleMember)
		eq(t, "a new member joins the default channel", level(t, s, lobby.ID, cat.ID), model.LevelMember)
		g := newAccount(t, s, "gus", model.RoleGuest)
		eq(t, "a new guest does not", level(t, s, lobby.ID, g.ID), model.Level(""))
		eq(t, "ann, there before, was not placed", level(t, s, lobby.ID, ann.ID), model.Level(""))

		res, err := s.Members().OfferDefault(ctx, news.ID, model.Now())
		must(t, err)
		eq(t, "offered to ann, bob and cat", kinds(res, store.MemberOffered), 3)
		eq(t, "not to the owner, who is in it", level(t, s, news.ID, owner.ID), model.LevelManager)
		_, ok, err := s.Members().Eligible(ctx, news.ID, ann.ID)
		must(t, err)
		eq(t, "private default channel: eligible", ok, true)
		accept(t, s, news.ID, ann.ID)
		m, _, err := s.Members().Get(ctx, news.ID, ann.ID)
		must(t, err)
		eq(t, "joined at the default level, directly", m.DirectLevel, model.LevelMember)
		res, err = s.Members().WithdrawDefault(ctx, news.ID)
		must(t, err)
		eq(t, "the waiting offers went", kinds(res, store.MemberOfferGone), 2)
		eq(t, "ann stays", level(t, s, news.ID, ann.ID), model.LevelMember)
		must(t, s.Settings().Set(ctx, store.SettingDefaultChannels, []model.ConversationID{}, ""))
	})

	t.Run("invitations", func(t *testing.T) {
		c := newChannel(t, s, "design", true, owner.ID)
		r := model.ChatRequest{RequesterID: owner.ID, RecipientID: ann.ID, Kind: model.RequestChannel, ConversationID: c.ID,
			State: model.RequestPending, Level: model.LevelModerator, Override: true}
		must(t, s.ChatRequests().Create(ctx, &r))
		again := model.ChatRequest{RequesterID: bob.ID, RecipientID: ann.ID, Kind: model.RequestChannel, ConversationID: c.ID, State: model.RequestPending}
		wantErr(t, s.ChatRequests().Create(ctx, &again), model.ErrAlreadyExists)
		got, found, err := s.ChatRequests().Invitation(ctx, c.ID, ann.ID)
		must(t, err)
		eq(t, "found with level and override", found && got.Level == model.LevelModerator && got.Override, true)
		all, err := s.ChatRequests().Invitations(ctx, c.ID)
		must(t, err)
		eq(t, "one invitation", len(all[c.ID]), 1)
		// Another channel: a second invitation from the same person.
		d := newChannel(t, s, "drafts", true, owner.ID)
		other := model.ChatRequest{RequesterID: owner.ID, RecipientID: ann.ID, Kind: model.RequestChannel, ConversationID: d.ID, State: model.RequestPending}
		must(t, s.ChatRequests().Create(ctx, &other))
		n, err := s.ChatRequests().Counts(ctx, owner.ID, 0, model.RequestQuickClose.Milliseconds(), model.RequestChannel)
		must(t, err)
		eq(t, "counted by kind", n.Unapproved, 2)
		n, err = s.ChatRequests().Counts(ctx, owner.ID, 0, model.RequestQuickClose.Milliseconds(), model.RequestDirect)
		must(t, err)
		eq(t, "not as direct", n.Unapproved, 0)
		ended, err := s.ChatRequests().EndInvitations(ctx, c.ID, ann.ID, model.RequestAccepted, model.Now())
		must(t, err)
		eq(t, "ended", len(ended), 1)
		_, found, err = s.ChatRequests().Invitation(ctx, c.ID, ann.ID)
		must(t, err)
		eq(t, "no longer open", found, false)
	})

	t.Run("a group chat waits for the people invited, and they read from their join", func(t *testing.T) {
		chat := newAdhoc(t, s, owner.ID, ann.ID)
		r := model.ChatRequest{RequesterID: owner.ID, RecipientID: bob.ID, Kind: model.RequestGroup, ConversationID: chat.ID, State: model.RequestPending}
		must(t, s.ChatRequests().Create(ctx, &r))
		found, err := s.Conversations().ByMemberKey(ctx, store.AdhocMemberKey([]model.AccountID{owner.ID, ann.ID, bob.ID}))
		must(t, err)
		eq(t, "found as the chat of the three", found.ID, chat.ID)
		post(t, s, chat.ID, owner.ID, "", "before bob")
		time.Sleep(2 * time.Millisecond)
		join(t, s, chat.ID, bob.ID, model.LevelMember)
		m, _, err := s.Members().Get(ctx, chat.ID, bob.ID)
		must(t, err)
		eq(t, "reads from the join", m.VisibleFrom > 0, true)
		must(t, s.ChatRequests().Answer(ctx, r.ID, []string{model.RequestPending}, model.RequestAccepted, model.Now()))
	})
}
