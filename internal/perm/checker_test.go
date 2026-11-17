package perm_test

import (
	"context"
	"testing"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/perm"
	"github.com/tikaydaes/churr/internal/store"
	"github.com/tikaydaes/churr/internal/store/storetest"
)

// TestCheckerSQLite and TestCheckerPostgres run the Checker against a store
// filled with one of everything.
func TestCheckerSQLite(t *testing.T) { checker(t, storetest.OpenSQLite(t)) }

func TestCheckerPostgres(t *testing.T) { checker(t, storetest.OpenPostgres(t)) }

func checker(t *testing.T, s store.Store) {
	ctx := context.Background()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	acct := func(name string, role model.Role) model.Account {
		a := model.Account{Username: name, DisplayName: name, Role: role}
		_, err := s.Accounts().Create(ctx, &a)
		must(err)
		return a
	}
	channel := func(name string, private bool, creator model.Account) model.Conversation {
		c := model.Conversation{Kind: model.KindChannel, Name: name, Private: private, CreatedByID: creator.ID}
		must(s.Conversations().Create(ctx, &c))
		_, err := s.Members().AddDirect(ctx, c.ID, creator.ID, model.LevelManager, model.Now())
		must(err)
		return c
	}
	join := func(c model.Conversation, a model.Account, l model.Level) {
		_, err := s.Members().AddDirect(ctx, c.ID, a.ID, l, model.Now())
		must(err)
	}
	must(s.Groups().EnsureBuiltin(ctx))
	owner, admin := acct("owner", model.RoleOwner), acct("admin", model.RoleAdmin)
	ann, bob := acct("ann", model.RoleMember), acct("bob", model.RoleMember)
	guest, bot := acct("guest", model.RoleGuest), acct("bot", model.RoleBot)
	must(s.Bots().Create(ctx, store.BotInfo{AccountID: bot.ID, CreatedBy: admin.ID}))
	gone := acct("gone", model.RoleMember)
	_, err := s.Accounts().Remove(ctx, gone.ID, owner.ID, model.Now())
	must(err)
	gone, err = s.Accounts().Get(ctx, gone.ID)
	must(err)

	pub, priv := channel("pub", false, ann), channel("priv", true, ann)
	join(pub, guest, model.LevelMember)
	join(pub, bot, model.LevelMember)
	adhoc := model.Conversation{Kind: model.KindChannel, Private: true, CreatedByID: ann.ID, MemberKey: store.AdhocMemberKey([]model.AccountID{ann.ID, guest.ID})}
	must(s.Conversations().Create(ctx, &adhoc))
	join(adhoc, ann, model.LevelManager)
	join(adhoc, guest, model.LevelMember)
	lockedCh := channel("vault", true, ann)
	_, err = s.Conversations().Lock(ctx, lockedCh.ID, ann.ID, model.Now())
	must(err)
	arch := channel("old", false, ann)
	_, err = s.Conversations().Update(ctx, arch.ID, func(c *model.Conversation) error { c.ArchivedAt = model.Now(); return nil })
	must(err)
	must(s.Moderation().CreateTimeout(ctx, &model.Timeout{AccountID: bob.ID, ConversationID: pub.ID, Until: model.Now().Add(3_600_000_000_000), CreatedByID: ann.ID}))
	join(pub, bob, model.LevelMember)
	must(s.Blocks().Add(ctx, ann.ID, bob.ID, model.Now()))

	p := perm.New(s)
	code := func(err error) model.Code { return model.CodeOf(err) }
	check := func(name string, got error, want model.Code) {
		t.Helper()
		if code(got) != want {
			t.Errorf("%s: got %q (%v), want %q", name, code(got), got, want)
		}
	}
	check("case 2 member not in PRIV", p.CanSee(ctx, bob, priv.ID), model.CodeNotFound)
	check("case 23 admin not in PRIV", p.CanRead(ctx, admin, priv.ID), model.CodeNotFound)
	check("owner not in ADHOC", p.CanSee(ctx, owner, adhoc.ID), model.CodeNotFound)
	check("a conversation that does not exist", p.CanSee(ctx, owner, model.NewID[model.ConversationID]()), model.CodeNotFound)
	check("member reads PUB without joining", p.CanRead(ctx, owner, pub.ID), "")
	check("guest in PUB reads", p.CanRead(ctx, guest, pub.ID), "")
	check("guest not in PRIV", p.CanRead(ctx, guest, priv.ID), model.CodeNotFound)
	check("ann posts in PUB", p.CanPost(ctx, ann, pub.ID), "")
	check("case 129 bob is timed out in PUB", p.CanPost(ctx, bob, pub.ID), model.CodeTimedOut)
	check("case 130 bob still reads PUB", p.CanRead(ctx, bob, pub.ID), "")
	check("case 124 archived", p.CanPost(ctx, ann, arch.ID), model.CodeArchived)
	check("case 39 admin not in PUB manages", p.CanManage(ctx, admin, pub.ID), model.CodeForbidden)
	check("case 40 admin not in PUB posts", p.CanPost(ctx, admin, pub.ID), model.CodeForbidden)
	check("removed account", p.CanRead(ctx, gone, pub.ID), model.CodeUnauthenticated)
	check("case 173 lock PUB", p.CanLock(ctx, ann, pub.ID), model.CodeConflict)
	check("case 169 lock PRIV", p.CanLock(ctx, ann, priv.ID), "")
	check("case 81 bot joins by itself", p.CanJoin(ctx, bot, arch.ID), model.CodeNotFound)

	// Adding people.
	st, err := p.Load(ctx, ann, lockedCh.ID)
	must(err)
	check("case 174 add to a locked channel", p.CanAdd(ctx, st, owner), model.CodeLocked)
	st, err = p.Load(ctx, ann, priv.ID)
	must(err)
	check("manager adds a member", p.CanAdd(ctx, st, owner), "")
	check("a removed account cannot be added", p.CanAdd(ctx, st, gone), model.CodeForbidden)
	st, err = p.Load(ctx, guest, adhoc.ID)
	must(err)
	check("a guest adds someone it shares a channel with", p.CanAdd(ctx, st, bob), "")
	check("a guest cannot add someone it cannot see", p.CanAdd(ctx, st, owner), model.CodeNotFound)

	// Seeing accounts (4.3) and starting chats (9.1, 13).
	check("case 13 guest and an account it shares nothing with", p.CanSeeAccount(ctx, guest, owner), model.CodeNotFound)
	check("guest sees an account in its channel", p.CanSeeAccount(ctx, guest, ann), "")
	check("member sees every local account", p.CanSeeAccount(ctx, bob, owner), "")
	check("case 78 guest starts a chat with a stranger", p.CanStartChat(ctx, guest, []model.Account{owner}), model.CodeNotFound)
	check("guest starts a chat with someone it can see", p.CanStartChat(ctx, guest, []model.Account{bob}), "")
	// Ruling R77: a block is deaf ears, never a refusal; the chat service
	// leaves ann out without a word.
	check("case 141 bob starts a chat with ann, who blocked him", p.CanStartChat(ctx, bob, []model.Account{ann}), "")
	if blocked, err := p.HasBlocked(ctx, ann.ID, bob.ID); err != nil || !blocked {
		t.Fatalf("case 141: HasBlocked = %v, %v; want true", blocked, err)
	}
	check("ann starts a chat with bob", p.CanStartChat(ctx, ann, []model.Account{bob}), "")
	check("case 137 a chat with a removed account", p.CanStartChat(ctx, ann, []model.Account{gone}), model.CodeForbidden)
	must(s.Settings().Set(ctx, store.SettingBlockingEnabled, false, ""))
	check("case 143 blocking off: the block has no effect", p.CanStartChat(ctx, bob, []model.Account{ann}), "")
	must(s.Moderation().CreateTimeout(ctx, &model.Timeout{AccountID: bob.ID, Until: model.Now().Add(3_600_000_000_000), CreatedByID: admin.ID}))
	check("case 131 server-wide time-out: start a chat", p.CanStartChat(ctx, bob, []model.Account{owner}), model.CodeTimedOut)
	check("case 131 server-wide time-out: post anywhere", p.CanPost(ctx, bob, arch.ID), model.CodeForbidden) // not a member there; forbidden comes first
	join(priv, bob, model.LevelMember)
	check("case 131 server-wide time-out: post where a member", p.CanPost(ctx, bob, priv.ID), model.CodeTimedOut)

	// Settings reach the rules.
	check("case 79 member invite, setting off", p.CanAdminister(ctx, ann, perm.CreateMemberInvite), model.CodeForbidden)
	must(s.Settings().Set(ctx, store.SettingMembersMayInvite, true, ""))
	check("member invite, setting on", p.CanAdminister(ctx, ann, perm.CreateMemberInvite), "")

	// Recipients and Readable: the permission halves of cases 3, 6, 11, 16, 26.
	ids, err := p.Recipients(ctx, priv.ID)
	must(err)
	for _, id := range ids {
		if id == admin.ID || id == owner.ID || id == guest.ID {
			t.Errorf("case 6/26: an outsider is among the recipients of PRIV: %s", id)
		}
	}
	ids, err = p.Recipients(ctx, pub.ID)
	must(err)
	for _, id := range ids {
		if id == owner.ID || id == admin.ID {
			t.Errorf("case 16: someone who only reads PUB is among its recipients: %s", id)
		}
	}
	if sc := p.Readable(ctx, ann); sc.AccountID != ann.ID || !sc.PublicChannels {
		t.Errorf("a member reads its channels and every public one: %+v", sc)
	}
	if sc := p.Readable(ctx, bot); sc.AccountID != bot.ID || sc.PublicChannels {
		t.Errorf("case 11: a bot reads only what it is in: %+v", sc)
	}
	if sc := p.Readable(ctx, gone); sc.AccountID != "" || sc.PublicChannels {
		t.Errorf("a removed account reads nothing: %+v", sc)
	}
}
