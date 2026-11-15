package perm_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/perm"
	"github.com/tikaydaes/churr/internal/store"
	"github.com/tikaydaes/churr/internal/store/storetest"
)

func TestLoadManySQLite(t *testing.T) { loadMany(t, storetest.OpenSQLite(t)) }

func TestLoadManyPostgres(t *testing.T) { loadMany(t, storetest.OpenPostgres(t)) }

// loadMany checks that LoadMany gives, for every actor and conversation of
// a varied store, the state Load gives.
func loadMany(t *testing.T, s store.Store) {
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
	join := func(c model.Conversation, a model.Account, l model.Level) {
		_, err := s.Members().AddDirect(ctx, c.ID, a.ID, l, model.Now())
		must(err)
	}
	channel := func(name string, private bool, creator model.Account) model.Conversation {
		c := model.Conversation{Kind: model.KindChannel, Name: name, Private: private, CreatedByID: creator.ID}
		must(s.Conversations().Create(ctx, &c))
		join(c, creator, model.LevelManager)
		return c
	}
	must(s.Groups().EnsureBuiltin(ctx))
	owner, admin := acct("owner", model.RoleOwner), acct("admin", model.RoleAdmin)
	ann, bob, cat := acct("ann", model.RoleMember), acct("bob", model.RoleMember), acct("cat", model.RoleMember)
	guest, bot := acct("guest", model.RoleGuest), acct("bot", model.RoleBot)
	must(s.Bots().Create(ctx, store.BotInfo{AccountID: bot.ID, CreatedBy: admin.ID}))

	pub, priv := channel("pub", false, ann), channel("priv", true, ann)
	join(pub, bob, model.LevelMember)
	join(pub, guest, model.LevelMember)
	join(pub, bot, model.LevelMember)
	join(priv, bob, model.LevelMember)
	arch := channel("old", false, owner)
	_, err := s.Conversations().Update(ctx, arch.ID, func(c *model.Conversation) error { c.ArchivedAt = model.Now(); return nil })
	must(err)
	// A channel made from a thread of priv: its members can see it.
	root := model.Message{ID: model.NewID[model.MessageID](), ConversationID: priv.ID, AuthorID: ann.ID, Text: "root", CreatedAt: model.Now()}
	must(s.Tx(ctx, func(tx store.Tx) error {
		seq, err := tx.Conversations().NextSeq(ctx, priv.ID)
		if err != nil {
			return err
		}
		root.Seq = seq
		return tx.Messages().Insert(ctx, &root, nil)
	}))
	moved := model.Conversation{Kind: model.KindChannel, Name: "moved", Private: true, CreatedByID: ann.ID, MovedFromMessageID: root.ID}
	must(s.Conversations().Create(ctx, &moved))
	join(moved, ann, model.LevelManager)

	hour := model.Time(3_600_000)
	must(s.Moderation().CreateTimeout(ctx, &model.Timeout{AccountID: bob.ID, ConversationID: pub.ID, Until: model.Now() + hour, CreatedByID: ann.ID}))
	must(s.Moderation().CreateTimeout(ctx, &model.Timeout{AccountID: cat.ID, Until: model.Now() + 2*hour, CreatedByID: owner.ID}))
	must(s.Moderation().CreateTimeout(ctx, &model.Timeout{AccountID: cat.ID, ConversationID: pub.ID, Until: model.Now() + 3*hour, CreatedByID: owner.ID}))
	must(s.Moderation().CreateTimeout(ctx, &model.Timeout{AccountID: guest.ID, ConversationID: pub.ID, Until: model.Now() - hour, CreatedByID: owner.ID}))

	missing := model.NewID[model.ConversationID]()
	convs := []model.ConversationID{pub.ID, priv.ID, arch.ID, moved.ID, missing}
	p := perm.New(s)
	compare := func(when string) {
		t.Helper()
		for _, actor := range []model.Account{owner, admin, ann, bob, cat, guest, bot} {
			many, err := p.LoadMany(ctx, actor, convs)
			must(err)
			for _, id := range convs {
				want, err := p.Load(ctx, actor, id)
				got, found := many[id]
				if err != nil {
					if found {
						t.Errorf("%s: %s: LoadMany has a state for a conversation Load refuses (%v)", when, actor.Username, err)
					}
					continue
				}
				if !found {
					t.Errorf("%s: %s: LoadMany has no state for %s", when, actor.Username, want.Conversation.Name)
					continue
				}
				// The two were read a moment apart.
				got.Now, want.Now = 0, 0
				if !reflect.DeepEqual(got, want) {
					t.Errorf("%s: %s in %s:\n got %+v\nwant %+v", when, actor.Username, want.Conversation.Name, got, want)
				}
				if got.CanSee() != want.CanSee() || got.IsMember() != want.IsMember() {
					t.Errorf("%s: %s in %s: the rules answer differently", when, actor.Username, want.Conversation.Name)
				}
			}
		}
	}
	compare("defaults")
	must(s.Settings().Set(ctx, store.SettingBlockingEnabled, true, owner.ID))
	must(s.Settings().Set(ctx, store.SettingEditWindow, 600, owner.ID))
	must(s.Settings().Set(ctx, store.SettingRetention, nil, owner.ID))
	must(s.Settings().Set(ctx, store.SettingMembersMayInvite, "not a bool", owner.ID))
	compare("settings changed")
	set, err := p.Settings(ctx)
	must(err)
	if !set.BlockingEnabled || set.EditWindowSeconds != 600 || set.RetentionSeconds != 0 || set.MembersMayInvite != perm.DefaultSettings().MembersMayInvite {
		t.Errorf("settings read wrongly: %+v", set)
	}
	// What the one read gives is what the reads key by key give.
	for key, got := range map[string]bool{
		store.SettingBlockingEnabled: set.BlockingEnabled, store.SettingMembersMayInvite: set.MembersMayInvite,
		store.SettingSealedEnabled: set.SealedEnabled, store.SettingCallsEnabled: set.CallsEnabled,
	} {
		def := perm.DefaultSettings()
		defs := map[string]bool{
			store.SettingBlockingEnabled: def.BlockingEnabled, store.SettingMembersMayInvite: def.MembersMayInvite,
			store.SettingSealedEnabled: def.SealedEnabled, store.SettingCallsEnabled: def.CallsEnabled,
		}
		want, err := store.SettingBool(ctx, s, key, defs[key])
		must(err)
		if got != want {
			t.Errorf("%s: got %v, want %v", key, got, want)
		}
	}

	none, err := p.LoadMany(ctx, ann, nil)
	must(err)
	if len(none) != 0 {
		t.Errorf("no conversations asked for, %d returned", len(none))
	}
}
