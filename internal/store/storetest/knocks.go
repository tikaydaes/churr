package storetest

import (
	"testing"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

func init() { register("knocks", testKnocks) }

// testKnocks is the knocks table (ruling R74): channels found by name without
// regard to case, one knock waiting per account and channel, the list
// leaving out those who became members, and an answer recorded once.
func testKnocks(t *testing.T, s store.Store) {
	ann := newAccount(t, s, "ann", model.RoleOwner)
	bob := newAccount(t, s, "bob", model.RoleMember)
	cat := newAccount(t, s, "cat", model.RoleMember)
	secret := newChannel(t, s, "Secret Garden", true, ann.ID)
	twin := newChannel(t, s, "secret garden", false, ann.ID)
	newChannel(t, s, "elsewhere", true, ann.ID)

	found, err := s.Members().KnockTargets(ctx, "SECRET garden")
	must(t, err)
	eq(t, "both of the name, oldest first", len(found) == 2 && found[0].ID == secret.ID && found[1].ID == twin.ID, true)
	none, err := s.Members().KnockTargets(ctx, "nowhere")
	must(t, err)
	eq(t, "no such name", len(none), 0)

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
