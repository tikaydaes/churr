package storetest

import (
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

// The small areas: audit, recovery, invites, sections, moderation, blocks
// and bots. Each is registered by itself so a failure names its area.

func init() {
	register("audit", testAudit)
	register("recovery", testRecovery)
	register("invites", testInvites)
	register("sections", testSections)
	register("reference patterns", testReferencePatterns)
	register("moderation", testModeration)
	register("blocks", testBlocks)
	register("bots", testBots)
}

func testAudit(t *testing.T, s store.Store) {
	for i, action := range []string{"invite.created", "setting.changed", "invite.created", "account.removed"} {
		must(t, s.Audit().Append(ctx, model.AuditEntry{At: model.Time(1000 + i), ActorKind: "account", ActorID: "0193a000-0000-7000-8000-000000000001",
			Action: action, TargetKind: "invite", TargetID: "x", Detail: map[string]any{"n": i}}))
	}
	must(t, s.Audit().Append(ctx, model.AuditEntry{Action: "owner.created"})) // defaults: id, time, actor kind
	all, next, err := s.Audit().List(ctx, store.AuditFilter{}, store.Page{Limit: 3})
	must(t, err)
	eq(t, "newest first", all[0].Action, "owner.created")
	eq(t, "defaults filled in", all[0].ID != "" && !all[0].At.IsZero() && all[0].ActorKind == "server" && all[0].Detail != nil, true)
	eq(t, "page", len(all) == 3 && next != "", true)
	rest, next, err := s.Audit().List(ctx, store.AuditFilter{}, store.Page{Limit: 3, Cursor: next})
	must(t, err)
	eq(t, "the rest", len(rest) == 2 && next == "", true)
	eq(t, "detail kept", rest[1].Detail["n"] == float64(0), true)
	only, _, _ := s.Audit().List(ctx, store.AuditFilter{Action: "invite.created"}, store.Page{})
	eq(t, "by action", len(only), 2)
	none, _, _ := s.Audit().List(ctx, store.AuditFilter{TargetKind: "account"}, store.Page{})
	eq(t, "by target", len(none), 0)
}

func testRecovery(t *testing.T, s store.Store) {
	a := newAccount(t, s, "ann", model.RoleOwner)
	must(t, s.Recovery().ReplaceCodes(ctx, a.ID, [][]byte{[]byte("one"), []byte("two"), []byte("three")}))
	ok, err := s.Recovery().UseCode(ctx, a.ID, []byte("two"), model.Now())
	must(t, err)
	eq(t, "a code works once", ok, true)
	ok, _ = s.Recovery().UseCode(ctx, a.ID, []byte("two"), model.Now())
	eq(t, "and not twice", ok, false)
	ok, _ = s.Recovery().UseCode(ctx, a.ID, []byte("nope"), model.Now())
	eq(t, "an unknown code", ok, false)
	total, unused, err := s.Recovery().CountCodes(ctx, a.ID)
	must(t, err)
	eq(t, "used codes are kept to count", total == 3 && unused == 2, true)
	must(t, s.Recovery().ReplaceCodes(ctx, a.ID, [][]byte{[]byte("fresh")}))
	total, unused, _ = s.Recovery().CountCodes(ctx, a.ID)
	eq(t, "a new set replaces the old", total == 1 && unused == 1, true)

	now := model.Now()
	// Making a link withdraws the account's earlier unused one.
	must(t, s.Recovery().CreateLink(ctx, []byte("first"), a.ID, a.ID, now.Add(time.Minute)))
	must(t, s.Recovery().CreateLink(ctx, []byte("stale"), a.ID, a.ID, now.Add(-time.Second)))
	_, _, ok, err = s.Recovery().UseLink(ctx, []byte("first"), now)
	must(t, err)
	eq(t, "an earlier link is withdrawn by a newer one", ok, false)
	_, _, ok, err = s.Recovery().UseLink(ctx, []byte("stale"), now)
	must(t, err)
	eq(t, "an expired link", ok, false)
	must(t, s.Recovery().CreateLink(ctx, []byte("link"), a.ID, "", now.Add(time.Minute)))
	who, by, ok, _ := s.Recovery().UseLink(ctx, []byte("link"), now)
	eq(t, "a link works once", ok && who == a.ID, true)
	eq(t, "made on the command line", by, model.AccountID(""))
	must(t, s.Recovery().CreateLink(ctx, []byte("made"), a.ID, a.ID, now.Add(time.Minute)))
	_, by, ok, _ = s.Recovery().UseLink(ctx, []byte("made"), now)
	eq(t, "who made it", ok && by == a.ID, true)
	_, _, ok, _ = s.Recovery().UseLink(ctx, []byte("link"), now)
	eq(t, "and not twice", ok, false)
	n, err := s.Recovery().DeleteLinksEnded(ctx, now.Add(time.Hour))
	must(t, err)
	eq(t, "ended links swept", n, int64(2))
}

func testInvites(t *testing.T, s store.Store) {
	a := newAccount(t, s, "ann", model.RoleOwner)
	b := newAccount(t, s, "bob", model.RoleMember)
	now := model.Now()
	g := model.NewID[model.GroupID]()
	inv := model.Invite{Code: "code-one", Role: model.RoleGuest, GroupIDs: []model.GroupID{g}, MaxUses: 2, ExpiresAt: now.Add(time.Minute), CreatedByID: a.ID}
	must(t, s.Invites().Create(ctx, &inv))
	got, err := s.Invites().ByCode(ctx, "code-one")
	must(t, err)
	eq(t, "stored", got.ID == inv.ID && got.Role == model.RoleGuest && len(got.GroupIDs) == 1 && got.GroupIDs[0] == g && got.MaxUses == 2, true)
	eq(t, "an empty list is a list", got.ConversationIDs != nil && len(got.ConversationIDs) == 0, true)
	dup := model.Invite{Code: "code-one", Role: model.RoleMember, MaxUses: 1}
	wantErr(t, s.Invites().Create(ctx, &dup), model.ErrAlreadyExists)
	_, err = s.Invites().ByCode(ctx, "no-such")
	wantErr(t, err, model.ErrNotFound)

	must(t, s.Invites().Use(ctx, inv.ID, now))
	must(t, s.Invites().Use(ctx, inv.ID, now))
	wantErr(t, s.Invites().Use(ctx, inv.ID, now), model.ErrNotFound) // used up
	got, _ = s.Invites().Get(ctx, inv.ID)
	eq(t, "uses never pass max_uses", got.Uses, 2)

	unlimited := model.Invite{Code: "code-two", Role: model.RoleMember, MaxUses: 0, CreatedByID: b.ID}
	must(t, s.Invites().Create(ctx, &unlimited))
	for i := 0; i < 5; i++ {
		must(t, s.Invites().Use(ctx, unlimited.ID, now))
	}
	must(t, s.Invites().Revoke(ctx, unlimited.ID, now))
	wantErr(t, s.Invites().Use(ctx, unlimited.ID, now), model.ErrNotFound)
	wantErr(t, s.Invites().Revoke(ctx, model.NewID[model.InviteID](), now), model.ErrNotFound)
	expired := model.Invite{Code: "code-three", Role: model.RoleMember, MaxUses: 1, ExpiresAt: now.Add(-time.Second), CreatedByID: b.ID}
	must(t, s.Invites().Create(ctx, &expired))
	wantErr(t, s.Invites().Use(ctx, expired.ID, now), model.ErrNotFound)

	all, _, err := s.Invites().List(ctx, "", store.Page{})
	must(t, err)
	eq(t, "everyone's", len(all), 3)
	bobs, next, err := s.Invites().List(ctx, b.ID, store.Page{Limit: 1})
	must(t, err)
	eq(t, "one's own, paged", len(bobs) == 1 && next != "", true)
	bobs, next, _ = s.Invites().List(ctx, b.ID, store.Page{Limit: 1, Cursor: next})
	eq(t, "second page", len(bobs) == 1 && next == "", true)
	n, err := s.Invites().DeleteEnded(ctx, now.Add(time.Hour))
	must(t, err)
	eq(t, "ended invites swept", n, int64(3))
}

func testSections(t *testing.T, s store.Store) {
	b := model.Section{Name: "Beta", Position: 2}
	a := model.Section{Name: "Alpha", Position: 1}
	must(t, s.Sections().Create(ctx, &b))
	must(t, s.Sections().Create(ctx, &a))
	list, err := s.Sections().List(ctx)
	must(t, err)
	eq(t, "ordered by position", len(list) == 2 && list[0].Name == "Alpha", true)
	b.Name, b.Position = "First", 0
	must(t, s.Sections().Update(ctx, b))
	got, err := s.Sections().Get(ctx, b.ID)
	must(t, err)
	eq(t, "updated", got.Name, "First")
	list, _ = s.Sections().List(ctx)
	eq(t, "reordered", list[0].ID, b.ID)
	must(t, s.Sections().Delete(ctx, a.ID))
	_, err = s.Sections().Get(ctx, a.ID)
	wantErr(t, err, model.ErrNotFound)
}

func testModeration(t *testing.T, s store.Store) {
	ann := newAccount(t, s, "ann", model.RoleOwner)
	bob := newAccount(t, s, "bob", model.RoleMember)
	c := newChannel(t, s, "c", false, ann.ID)
	other := newChannel(t, s, "other", false, ann.ID)
	now := model.Now()
	_, timedOut, err := s.Moderation().TimedOutUntil(ctx, bob.ID, c.ID, now)
	must(t, err)
	eq(t, "nothing yet", timedOut, false)

	in := model.Timeout{AccountID: bob.ID, ConversationID: c.ID, Until: now.Add(time.Minute), CreatedByID: ann.ID}
	must(t, s.Moderation().CreateTimeout(ctx, &in))
	until, timedOut, _ := s.Moderation().TimedOutUntil(ctx, bob.ID, c.ID, now)
	eq(t, "timed out in the channel", timedOut && until == in.Until, true)
	_, timedOut, _ = s.Moderation().TimedOutUntil(ctx, bob.ID, other.ID, now)
	eq(t, "not in another channel", timedOut, false)
	_, timedOut, _ = s.Moderation().TimedOutUntil(ctx, bob.ID, "", now)
	eq(t, "not server-wide", timedOut, false)
	_, timedOut, _ = s.Moderation().TimedOutUntil(ctx, bob.ID, c.ID, in.Until.Add(1))
	eq(t, "it ends by itself", timedOut, false)

	wide := model.Timeout{AccountID: bob.ID, Until: now.Add(time.Hour), CreatedByID: ann.ID}
	must(t, s.Moderation().CreateTimeout(ctx, &wide))
	until, timedOut, _ = s.Moderation().TimedOutUntil(ctx, bob.ID, other.ID, now)
	eq(t, "a server-wide time-out covers every channel", timedOut && until == wide.Until, true)
	until, _, _ = s.Moderation().TimedOutUntil(ctx, bob.ID, c.ID, now)
	eq(t, "the latest end wins", until, wide.Until)
	must(t, s.Moderation().LiftTimeout(ctx, wide.ID, now))
	must(t, s.Moderation().LiftTimeout(ctx, in.ID, now))
	_, timedOut, _ = s.Moderation().TimedOutUntil(ctx, bob.ID, c.ID, now)
	eq(t, "lifted", timedOut, false)
}

func testBlocks(t *testing.T, s store.Store) {
	a := newAccount(t, s, "ann", model.RoleOwner)
	b := newAccount(t, s, "bob", model.RoleMember)
	must(t, s.Blocks().Add(ctx, a.ID, b.ID, model.Now()))
	must(t, s.Blocks().Add(ctx, a.ID, b.ID, model.Now())) // again changes nothing
	has, err := s.Blocks().Has(ctx, a.ID, b.ID)
	must(t, err)
	eq(t, "ann has blocked bob", has, true)
	has, _ = s.Blocks().Has(ctx, b.ID, a.ID)
	eq(t, "bob has not blocked ann", has, false)
	must(t, s.Blocks().Remove(ctx, a.ID, b.ID))
	has, _ = s.Blocks().Has(ctx, a.ID, b.ID)
	eq(t, "unblocked", has, false)
}

func testBots(t *testing.T, s store.Store) {
	admin := newAccount(t, s, "admin", model.RoleOwner)
	bot := newAccount(t, s, "helper", model.RoleBot)
	_, found, err := s.Bots().Get(ctx, bot.ID)
	must(t, err)
	eq(t, "no row yet", found, false)
	must(t, s.Bots().Create(ctx, store.BotInfo{AccountID: bot.ID, Description: "Helps", CreatedBy: admin.ID, WebhookOnly: true}))
	got, found, err := s.Bots().Get(ctx, bot.ID)
	must(t, err)
	eq(t, "stored", found && got.Description == "Helps" && got.CreatedBy == admin.ID && got.WebhookOnly, true)
	// A bot is in no built-in group.
	must(t, s.Groups().EnsureBuiltin(ctx))
	everyone, _ := s.Groups().Builtin(ctx, model.GroupEveryone)
	in, _ := s.Groups().IsMember(ctx, everyone.ID, bot.ID)
	eq(t, "a bot is not in @everyone", in, false)
}

func testReferencePatterns(t *testing.T, s store.Store) {
	ann := newAccount(t, s, "ann", model.RoleOwner)
	conv := newChannel(t, s, "refs", false, ann.ID)
	none, err := s.ReferencePatterns().All(ctx)
	must(t, err)
	eq(t, "none to start with", len(none), 0)

	server := model.ReferencePattern{Name: "Tracker", Prefix: "PROJ-", URLTemplate: "https://t.example/{value}", CreatedByID: ann.ID, CreatedAt: 1000}
	own := model.ReferencePattern{ConversationID: conv.ID, Name: "Issues", Prefix: "#", Expression: "[0-9]{1,6}", URLTemplate: "https://g.example/{value}", CreatedByID: ann.ID, CreatedAt: 2000}
	must(t, s.ReferencePatterns().Create(ctx, &server))
	must(t, s.ReferencePatterns().Create(ctx, &own))
	all, err := s.ReferencePatterns().All(ctx)
	must(t, err)
	eq(t, "both, oldest first", len(all) == 2 && all[0].ID == server.ID && all[1].ID == own.ID, true)
	eq(t, "the server's has no conversation", all[0].ConversationID, model.ConversationID(""))
	eq(t, "the channel's has its conversation and expression", all[1].ConversationID == conv.ID && all[1].Expression == "[0-9]{1,6}", true)

	server.Name, server.Prefix, server.URLTemplate = "Jira", "JIRA-", "https://j.example/{value}"
	must(t, s.ReferencePatterns().Update(ctx, server))
	got, err := s.ReferencePatterns().Get(ctx, server.ID)
	must(t, err)
	eq(t, "updated", got.Name == "Jira" && got.Prefix == "JIRA-" && got.URLTemplate == "https://j.example/{value}" && got.CreatedAt == 1000, true)
	all, _ = s.ReferencePatterns().All(ctx)
	eq(t, "the list is read again after a change", all[0].Prefix, "JIRA-")

	// A change inside a transaction is seen inside it, and outside it
	// only once it has committed.
	must(t, s.Tx(ctx, func(tx store.Tx) error {
		if err := tx.ReferencePatterns().Delete(ctx, own.ID); err != nil {
			return err
		}
		in, err := tx.ReferencePatterns().All(ctx)
		eq(t, "gone inside the transaction", len(in), 1)
		return err
	}))
	all, _ = s.ReferencePatterns().All(ctx)
	eq(t, "gone after it", len(all), 1)
	wantErr(t, s.ReferencePatterns().Delete(ctx, own.ID), model.ErrNotFound)
	wantErr(t, s.ReferencePatterns().Update(ctx, own), model.ErrNotFound)
	_, err = s.ReferencePatterns().Get(ctx, own.ID)
	wantErr(t, err, model.ErrNotFound)
}
