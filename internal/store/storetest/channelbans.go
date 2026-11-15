package storetest

import (
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

func init() { register("channel_bans", testChannelBans) }

// testChannelBans is the channel_bans table (docs/FEATURES.md section 19.1):
// a ban replaces the one in force, ends at its end or when lifted, keeps the
// account from getting a member row by any route, and hides a public
// channel from it in the browser.
func testChannelBans(t *testing.T, s store.Store) {
	ann := newAccount(t, s, "ann", model.RoleOwner)
	bob := newAccount(t, s, "bob", model.RoleMember)
	pub := newChannel(t, s, "pub", false, ann.ID)
	other := newChannel(t, s, "other", false, ann.ID)
	now := model.Now()

	_, found, err := s.Moderation().ChannelBan(ctx, pub.ID, bob.ID, now)
	must(t, err)
	eq(t, "nothing yet", found, false)

	first := model.ChannelBan{ConversationID: pub.ID, AccountID: bob.ID, CreatedByID: ann.ID, Reason: "one", ExpiresAt: now.Add(time.Hour)}
	must(t, s.Moderation().BanFromChannel(ctx, &first))
	second := model.ChannelBan{ConversationID: pub.ID, AccountID: bob.ID, CreatedByID: ann.ID, Reason: "two"}
	must(t, s.Moderation().BanFromChannel(ctx, &second))
	got, found, err := s.Moderation().ChannelBan(ctx, pub.ID, bob.ID, now)
	must(t, err)
	eq(t, "the newer replaces the older", found && got.ID == second.ID && got.Reason == "two" && got.ExpiresAt.IsZero(), true)
	list, err := s.Moderation().ChannelBans(ctx, pub.ID, now)
	must(t, err)
	eq(t, "one in force", len(list), 1)
	all, err := s.Moderation().ChannelBans(ctx, "", now)
	must(t, err)
	eq(t, "the console's list", len(all), 1)
	of, err := s.Moderation().ChannelBansOf(ctx, bob.ID, now)
	must(t, err)
	eq(t, "bob's bans", len(of) == 1 && of[0] == pub.ID, true)

	// No route brings a banned account in.
	res, err := s.Members().AddDirect(ctx, pub.ID, bob.ID, model.LevelMember, now)
	must(t, err)
	eq(t, "no row is made", len(res.Changes), 0)
	_, member, err := s.Members().Get(ctx, pub.ID, bob.ID)
	must(t, err)
	eq(t, "not a member", member, false)
	roster, err := s.Members().Roster(ctx, pub.ID)
	must(t, err)
	eq(t, "not in the roster", roster.Has(bob.ID), false)

	// The browser leaves the public channel out for bob alone.
	shown := func(who model.AccountID) map[model.ConversationID]bool {
		list, _, err := s.Conversations().Browse(ctx, store.BrowseFilter{AccountID: who, Public: true}, store.Page{})
		must(t, err)
		out := map[model.ConversationID]bool{}
		for _, c := range list {
			out[c.ID] = true
		}
		return out
	}
	eq(t, "hidden from bob", shown(bob.ID)[pub.ID], false)
	eq(t, "other channels shown", shown(bob.ID)[other.ID], true)

	// Lifted: he can be added, and the roster follows.
	lifted, found, err := s.Moderation().LiftChannelBan(ctx, pub.ID, bob.ID, now.Add(time.Second))
	must(t, err)
	eq(t, "lifted", found && lifted.ID == second.ID && !lifted.LiftedAt.IsZero(), true)
	_, found, err = s.Moderation().LiftChannelBan(ctx, pub.ID, bob.ID, now.Add(time.Second))
	must(t, err)
	eq(t, "nothing left to lift", found, false)
	_, err = s.Members().AddDirect(ctx, pub.ID, bob.ID, model.LevelMember, now)
	must(t, err)
	roster, err = s.Members().Roster(ctx, pub.ID)
	must(t, err)
	eq(t, "in the roster again", roster.Has(bob.ID), true)
	eq(t, "shown again", shown(bob.ID)[pub.ID], true)

	// A ban past its end counts for nothing.
	past := model.ChannelBan{ConversationID: other.ID, AccountID: bob.ID, CreatedByID: ann.ID, CreatedAt: now.Add(-2 * time.Hour), ExpiresAt: now.Add(-time.Hour)}
	must(t, s.Moderation().BanFromChannel(ctx, &past))
	_, found, err = s.Moderation().ChannelBan(ctx, other.ID, bob.ID, now)
	must(t, err)
	eq(t, "past its end", found, false)
	_, err = s.Members().AddDirect(ctx, other.ID, bob.ID, model.LevelMember, now)
	must(t, err)
	_, member, err = s.Members().Get(ctx, other.ID, bob.ID)
	must(t, err)
	eq(t, "added after its end", member, true)
}
