package storetest

import (
	"testing"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

func init() { register("knocks", testKnocks) }

// testKnocks is the knocks table and knock channels (rulings R74, R79): the
// knock column kept with private, one knock waiting per account and
// channel, the list leaving out those who became members, an answer recorded
// once, and knock channels shown in the browser to those outside.
func testKnocks(t *testing.T, s store.Store) {
	ann := newAccount(t, s, "ann", model.RoleOwner)
	bob := newAccount(t, s, "bob", model.RoleMember)
	cat := newAccount(t, s, "cat", model.RoleMember)
	secret := model.Conversation{Kind: model.KindChannel, Name: "Secret Garden", Private: true, Knock: true, CreatedByID: ann.ID}
	must(t, s.Conversations().Create(ctx, &secret))
	_, err := s.Members().AddDirect(ctx, secret.ID, ann.ID, model.LevelManager, model.Now())
	must(t, err)
	twin := newChannel(t, s, "secret garden", false, ann.ID)
	hidden := newChannel(t, s, "elsewhere", true, ann.ID)

	got0, err := s.Conversations().Get(ctx, secret.ID)
	must(t, err)
	eq(t, "the column is kept", got0.IsKnock() && got0.AccessKind() == model.AccessKnock, true)
	// The browser shows bob the knock channel and the public one, not the
	// private one.
	shown, _, err := s.Conversations().Browse(ctx, store.BrowseFilter{AccountID: bob.ID, Public: true}, store.Page{})
	must(t, err)
	seen := map[model.ConversationID]bool{}
	for _, c := range shown {
		seen[c.ID] = true
	}
	eq(t, "browse", seen[secret.ID] && seen[twin.ID] && !seen[hidden.ID], true)
	// Knock to public is allowed; private to anything is not.
	pub, err := s.Conversations().Update(ctx, twin.ID, func(c *model.Conversation) error { c.Private, c.Knock = true, true; return nil })
	must(t, err)
	eq(t, "public to knock", pub.IsKnock(), true)
	pub, err = s.Conversations().Update(ctx, twin.ID, func(c *model.Conversation) error { c.Private = false; return nil })
	must(t, err)
	eq(t, "knock to public", pub.Public() && !pub.Knock, true)
	_, err = s.Conversations().Update(ctx, hidden.ID, func(c *model.Conversation) error { c.Private = false; return nil })
	wantErr(t, err, model.ErrConflict)

	k := model.Knock{ConversationID: secret.ID, AccountID: bob.ID}
	created, err := s.Members().Knock(ctx, &k)
	must(t, err)
	eq(t, "the first knock waits", created && k.ID != "" && k.Outcome == model.KnockPending, true)
	again := model.Knock{ConversationID: secret.ID, AccountID: bob.ID}
	created, err = s.Members().Knock(ctx, &again)
	must(t, err)
	eq(t, "one waits at a time", created, false)
	other := model.Knock{ConversationID: secret.ID, AccountID: cat.ID}
	_, err = s.Members().Knock(ctx, &other)
	must(t, err)

	list, err := s.Members().Knocks(ctx, secret.ID)
	must(t, err)
	eq(t, "two waiting, oldest first", len(list) == 2 && list[0].AccountID == bob.ID, true)

	// Cat is let in another way: her knock is no longer listed.
	_, err = s.Members().AddDirect(ctx, secret.ID, cat.ID, model.LevelMember, model.Now())
	must(t, err)
	list, err = s.Members().Knocks(ctx, secret.ID)
	must(t, err)
	eq(t, "a member's knock is not listed", len(list) == 1 && list[0].ID == k.ID, true)

	at := model.Now()
	got, ok, err := s.Members().AnswerKnock(ctx, secret.ID, k.ID, ann.ID, model.KnockDeclined, at)
	must(t, err)
	eq(t, "answered", ok && got.Outcome == model.KnockDeclined && got.AnsweredByID == ann.ID && got.AccountID == bob.ID, true)
	_, ok, err = s.Members().AnswerKnock(ctx, secret.ID, k.ID, ann.ID, model.KnockAccepted, at)
	must(t, err)
	eq(t, "answered once", ok, false)
	_, ok, err = s.Members().AnswerKnock(ctx, twin.ID, other.ID, ann.ID, model.KnockAccepted, at)
	must(t, err)
	eq(t, "only in its own channel", ok, false)

	// Once answered, bob may knock again.
	next := model.Knock{ConversationID: secret.ID, AccountID: bob.ID}
	created, err = s.Members().Knock(ctx, &next)
	must(t, err)
	eq(t, "a new knock after an answer", created, true)
}
