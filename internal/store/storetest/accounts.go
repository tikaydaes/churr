package storetest

import (
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

func init() { register("accounts", testAccounts) }

func testAccounts(t *testing.T, s store.Store) {
	must(t, s.Groups().EnsureBuiltin(ctx))
	_, found, err := s.Accounts().Owner(ctx)
	must(t, err)
	eq(t, "no owner before setup", found, false)

	owner := newAccount(t, s, "owner", model.RoleOwner)
	eq(t, "id filled in", model.ValidID(string(owner.ID)), true)
	eq(t, "claimed at creation", owner.ClaimedAt.IsZero(), false)
	got, found, err := s.Accounts().Owner(ctx)
	must(t, err)
	eq(t, "owner found", found && got.ID == owner.ID, true)
	second := model.Account{Username: "owner2", DisplayName: "x", Role: model.RoleOwner}
	_, err = s.Accounts().Create(ctx, &second)
	wantErr(t, err, model.ErrConflict)

	t.Run("the username namespace", func(t *testing.T) {
		newGroup(t, s, "design")
		for _, name := range []string{"owner", "design", "everyone", "admins", "here", "channel", "me", "system", "churr"} {
			a := model.Account{Username: name, DisplayName: name, Role: model.RoleMember}
			_, err := s.Accounts().Create(ctx, &a)
			wantErr(t, err, model.ErrNameTaken)
		}
		g := model.Group{Handle: "owner", Name: "Owners"}
		wantErr(t, s.Groups().Create(ctx, &g), model.ErrNameTaken)
	})

	t.Run("update writes what it promises and nothing else", func(t *testing.T) {
		a := newAccount(t, s, "ann", model.RoleMember)
		zero, later := model.Time(0), model.Now().Add(time.Hour)
		out, err := s.Accounts().Update(ctx, a.ID, func(a *model.Account) error {
			a.DisplayName, a.Title, a.Pronouns, a.TimeZone = "Ann B", "Engineer", "she/her", "Australia/Brisbane"
			a.StatusEmoji, a.StatusText, a.StatusDND, a.StatusAway = "🌴", "On holiday", true, true
			a.AvailabilityIs, a.AvailabilityBy = "meeting", "self"
			a.DNDUntil, a.AwayUntil = &zero, &later
			a.QuietStart, a.QuietEnd, a.QuietDays = "22:00", "07:00", "1111100"
			a.AllowNotifyAnyway = false
			a.PrefsJSON = []byte(`{"thread_view":"flat"}`)
			a.WebAuthnHandle = []byte{1, 2, 3}
			// Not Update's to change:
			a.Role, a.RemovedAt, a.ClaimedAt = model.RoleOwner, 5, 0
			return nil
		})
		must(t, err)
		eq(t, "returned", out.DisplayName, "Ann B")
		got, err := s.Accounts().Get(ctx, a.ID)
		must(t, err)
		eq(t, "title", got.Title, "Engineer")
		eq(t, "status", got.StatusEmoji+got.StatusText, "🌴On holiday")
		eq(t, "status switches", got.StatusDND && got.StatusAway, true)
		eq(t, "dnd until turned off is 0, not NULL", got.DNDUntil != nil && *got.DNDUntil == 0, true)
		eq(t, "away until a time", got.AwayUntil != nil && *got.AwayUntil == later, true)
		eq(t, "quiet hours", got.QuietStart+got.QuietEnd+got.QuietDays, "22:0007:001111100")
		eq(t, "allow_notify_anyway", got.AllowNotifyAnyway, false)
		eq(t, "role untouched", got.Role, model.RoleMember)
		eq(t, "removal untouched", got.Removed(), false)
		eq(t, "claim untouched", got.ClaimedAt.IsZero(), false)
		byHandle, err := s.Accounts().ByWebAuthnHandle(ctx, []byte{1, 2, 3})
		must(t, err)
		eq(t, "by handle", byHandle.ID, a.ID)
		out, err = s.Accounts().Update(ctx, a.ID, func(a *model.Account) error { a.DNDUntil = nil; return nil })
		must(t, err)
		got, _ = s.Accounts().Get(ctx, a.ID)
		eq(t, "dnd off is NULL", got.DNDUntil == nil, true)
		_, err = s.Accounts().Update(ctx, a.ID, func(a *model.Account) error { a.Username = "owner"; return nil })
		wantErr(t, err, model.ErrNameTaken)
		_, err = s.Accounts().Update(ctx, a.ID, func(a *model.Account) error { a.DisplayName = "No"; return model.ErrConflict })
		wantErr(t, err, model.ErrConflict)
		got, _ = s.Accounts().Get(ctx, a.ID)
		eq(t, "a refused update writes nothing", got.DisplayName, "Ann B")
	})

	t.Run("list, filters and paging", func(t *testing.T) {
		for _, n := range []string{"zed", "yan", "xia"} {
			newAccount(t, s, n, model.RoleGuest)
		}
		all, next, err := s.Accounts().List(ctx, store.AccountFilter{}, store.Page{Limit: 3})
		must(t, err)
		eq(t, "first page", len(all), 3)
		eq(t, "ordered by username", all[0].Username < all[1].Username && all[1].Username < all[2].Username, true)
		rest, next2, err := s.Accounts().List(ctx, store.AccountFilter{}, store.Page{Limit: 200, Cursor: next})
		must(t, err)
		eq(t, "the rest", len(rest) >= 2 && next2 == "" && rest[0].Username > all[2].Username, true)
		guests, _, _ := s.Accounts().List(ctx, store.AccountFilter{Role: model.RoleGuest}, store.Page{})
		eq(t, "by role", len(guests), 3)
		q, _, _ := s.Accounts().List(ctx, store.AccountFilter{Query: "ZE"}, store.Page{})
		eq(t, "by query, without regard to case", len(q) == 1 && q[0].Username == "zed", true)
		pct, _, _ := s.Accounts().List(ctx, store.AccountFilter{Query: "%"}, store.Page{})
		eq(t, "a percent sign is a character, not a wildcard", len(pct), 0)
		everyone, err := s.Groups().Builtin(ctx, model.GroupEveryone)
		must(t, err)
		in, _, _ := s.Accounts().List(ctx, store.AccountFilter{GroupID: everyone.ID}, store.Page{})
		for _, a := range in {
			if a.Role == model.RoleGuest {
				t.Fatalf("a guest is listed in @everyone: %s", a.Username)
			}
		}
		ids, _, _ := s.Accounts().List(ctx, store.AccountFilter{IDs: []model.AccountID{owner.ID, guests[0].ID}}, store.Page{})
		eq(t, "by ids", len(ids), 2)
		many, err := s.Accounts().GetMany(ctx, []model.AccountID{owner.ID, model.NewID[model.AccountID]()})
		must(t, err)
		eq(t, "GetMany skips what does not exist", len(many), 1)

		// What a guest sees: only accounts that share a conversation.
		c := newChannel(t, s, "shared", true, owner.ID)
		join(t, s, c.ID, guests[0].ID, model.LevelMember)
		seen, _, _ := s.Accounts().List(ctx, store.AccountFilter{SharedWith: guests[0].ID}, store.Page{})
		eq(t, "shared with", len(seen), 2)
		yes, err := s.Accounts().SharesConversation(ctx, owner.ID, guests[0].ID)
		must(t, err)
		no, _ := s.Accounts().SharesConversation(ctx, owner.ID, guests[1].ID)
		eq(t, "shares a conversation", yes && !no, true)
	})

	t.Run("roles and ownership", func(t *testing.T) {
		a := newAccount(t, s, "riser", model.RoleMember)
		out, _, err := s.Accounts().SetRole(ctx, a.ID, model.RoleAdmin)
		must(t, err)
		eq(t, "now admin", out.Role, model.RoleAdmin)
		// The last owner stays one.
		_, _, err = s.Accounts().SetRole(ctx, owner.ID, model.RoleMember)
		wantErr(t, err, model.ErrConflict)
		// Several owners (ruling R99).
		out, _, err = s.Accounts().SetRole(ctx, a.ID, model.RoleOwner)
		must(t, err)
		eq(t, "a second owner", out.Role, model.RoleOwner)
		first, _, _ := s.Accounts().Owner(ctx)
		eq(t, "the first owner is still the first", first.ID, owner.ID)
		// Either may now stop being one, and then the other is the last.
		_, _, err = s.Accounts().SetRole(ctx, owner.ID, model.RoleAdmin)
		must(t, err)
		now, _, _ := s.Accounts().Owner(ctx)
		eq(t, "the remaining owner", now.ID, a.ID)
		_, _, err = s.Accounts().SetRole(ctx, a.ID, model.RoleAdmin)
		wantErr(t, err, model.ErrConflict)
		_, _, err = s.Accounts().SetRole(ctx, owner.ID, model.RoleOwner)
		must(t, err)
		_, _, err = s.Accounts().SetRole(ctx, a.ID, model.RoleMember)
		must(t, err)
	})

	t.Run("unclaimed accounts are offered @everyone's channels only when claimed", func(t *testing.T) {
		general := newChannel(t, s, "general", false, owner.ID)
		everyone, _ := s.Groups().Builtin(ctx, model.GroupEveryone)
		_, _, err := s.Members().AddGroup(ctx, model.ChannelGroup{ConversationID: general.ID, GroupID: everyone.ID, Level: model.LevelMember})
		must(t, err)
		imp := model.Account{Username: "imported", DisplayName: "Imported", Role: model.RoleMember}
		must(t, s.Accounts().CreateUnclaimed(ctx, &imp))
		eq(t, "not in @everyone's channel yet", level(t, s, general.ID, imp.ID), model.Level(""))
		res, err := s.Accounts().Claim(ctx, imp.ID, model.Now())
		must(t, err)
		eq(t, "offered on claiming", len(res.Changes) == 1 && res.Changes[0].Kind == store.MemberOffered, true)
		_, err = s.Accounts().Claim(ctx, imp.ID, model.Now())
		wantErr(t, err, model.ErrNotFound)
	})

	t.Run("removal", func(t *testing.T) {
		a := newAccount(t, s, "leaver", model.RoleMember)
		b := newAccount(t, s, "stayer", model.RoleMember)
		pub := newChannel(t, s, "kept", false, a.ID)
		join(t, s, pub.ID, b.ID, model.LevelMember)
		g := newGroup(t, s, "crew", a.ID)
		m := post(t, s, pub.ID, a.ID, "", "my words stay", model.Mention{AccountID: b.ID, Kind: model.MentionAccount})
		ses := model.Session{AccountID: a.ID, Kind: model.SessionWeb}
		must(t, s.Sessions().Create(ctx, &ses, []byte("hash-a"), nil))
		pk := model.Passkey{AccountID: a.ID, CredentialID: []byte("cred-a"), Credential: []byte(`{}`), Name: "k"}
		must(t, s.Passkeys().Add(ctx, &pk))
		must(t, s.Recovery().ReplaceCodes(ctx, a.ID, [][]byte{[]byte("c1")}))
		must(t, s.Blocks().Add(ctx, a.ID, b.ID, model.Now()))
		must(t, s.Blocks().Add(ctx, b.ID, a.ID, model.Now()))
		inv := model.Invite{Code: "leavers-invite", Role: model.RoleMember, MaxUses: 1, CreatedByID: a.ID}
		must(t, s.Invites().Create(ctx, &inv))
		_, err := s.Accounts().Update(ctx, a.ID, func(a *model.Account) error { a.Title, a.StatusText = "Boss", "busy"; return nil })
		must(t, err)

		out, err := s.Accounts().Remove(ctx, a.ID, owner.ID, model.Now())
		must(t, err)
		eq(t, "its sessions are reported for closing", len(out.SessionIDs), 1)
		got, _ := s.Accounts().Get(ctx, a.ID)
		eq(t, "the row stays with its names and role", got.Username+got.DisplayName+string(got.Role), "leaverleavermember")
		eq(t, "removed_by", got.RemovedBy, owner.ID)
		eq(t, "the profile is cleared", got.Title+got.StatusText, "")
		_, err = s.Sessions().ByTokenHash(ctx, []byte("hash-a"))
		wantErr(t, err, model.ErrNotFound)
		n, _ := s.Passkeys().Count(ctx, a.ID)
		eq(t, "passkeys gone", n, int64(0))
		total, _, _ := s.Recovery().CountCodes(ctx, a.ID)
		eq(t, "recovery codes gone", total, 0)
		in, _ := s.Groups().IsMember(ctx, g.ID, a.ID)
		eq(t, "out of its groups", in, false)
		b1, _ := s.Blocks().Has(ctx, a.ID, b.ID)
		b2, _ := s.Blocks().Has(ctx, b.ID, a.ID)
		eq(t, "blocks gone in both directions", b1 || b2, false)
		gotInv, _ := s.Invites().Get(ctx, inv.ID)
		eq(t, "its unused invites are revoked", gotInv.RevokedAt.IsZero(), false)
		eq(t, "out of the channel", level(t, s, pub.ID, a.ID), model.Level(""))
		eq(t, "the longest-standing member became manager", level(t, s, pub.ID, b.ID), model.LevelManager)
		kept, err := s.Messages().Get(ctx, m.ID)
		must(t, err)
		eq(t, "its messages stay", kept.Text, "my words stay")
		list, _, _ := s.Accounts().List(ctx, store.AccountFilter{Query: "leaver"}, store.Page{})
		eq(t, "not listed", len(list), 0)
		list, _, _ = s.Accounts().List(ctx, store.AccountFilter{Query: "leaver", IncludeRemoved: true}, store.Page{})
		eq(t, "listed as a former member when asked", len(list), 1)
	})
}
