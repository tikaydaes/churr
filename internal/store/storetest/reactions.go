package storetest

import (
	"fmt"
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

func init() { register("reactions", testReactions) }

func testReactions(t *testing.T, s store.Store) {
	ann := newAccount(t, s, "ann", model.RoleOwner)
	bob := newAccount(t, s, "bob", model.RoleMember)
	c := newChannel(t, s, "react", false, ann.ID)
	m := post(t, s, c.ID, ann.ID, "", "react to me")

	added, err := s.Reactions().Add(ctx, m.ID, ann.ID, "👍", model.Now())
	must(t, err)
	eq(t, "added", added, true)
	added, err = s.Reactions().Add(ctx, m.ID, ann.ID, "👍", model.Now())
	must(t, err)
	eq(t, "the same again changes nothing", added, false)
	_, err = s.Reactions().Add(ctx, m.ID, bob.ID, "👍", model.Now().Add(time.Second))
	must(t, err)
	_, err = s.Reactions().Add(ctx, m.ID, bob.ID, "🎉", model.Now().Add(2*time.Second))
	must(t, err)

	list, err := s.Reactions().List(ctx, m.ID, bob.ID)
	must(t, err)
	eq(t, "two emoji", len(list), 2)
	eq(t, "in order of first appearance", list[0].Emoji, "👍")
	eq(t, "count", list[0].Count, 2)
	eq(t, "me", list[0].Me, true)
	eq(t, "accounts in order", list[0].AccountIDs[0], ann.ID)
	one, err := s.Reactions().One(ctx, m.ID, "🎉", ann.ID)
	must(t, err)
	eq(t, "one: count", one.Count, 1)
	eq(t, "one: not me", one.Me, false)
	has, err := s.Reactions().Has(ctx, m.ID, "🎉", bob.ID)
	must(t, err)
	eq(t, "has", has, true)

	msgs := []model.Message{m}
	must(t, s.Messages().Hydrate(ctx, msgs, ann.ID))
	eq(t, "hydrated", len(msgs[0].Reactions), 2)

	removed, err := s.Reactions().Remove(ctx, m.ID, bob.ID, "🎉")
	must(t, err)
	eq(t, "removed", removed, true)
	removed, _ = s.Reactions().Remove(ctx, m.ID, bob.ID, "🎉")
	eq(t, "nothing to remove", removed, false)
	one, _ = s.Reactions().One(ctx, m.ID, "🎉", ann.ID)
	eq(t, "entry gone", one.Count, 0)

	// Twenty different emoji at most; adding to one already there is fine.
	for i := 1; i < model.MaxEmojiPerMessage; i++ {
		_, err := s.Reactions().Add(ctx, m.ID, ann.ID, fmt.Sprintf(":e%d:", i), model.Now())
		must(t, err)
	}
	_, err = s.Reactions().Add(ctx, m.ID, ann.ID, ":one_too_many:", model.Now())
	wantErr(t, err, model.ErrConflict)
	_, err = s.Reactions().Add(ctx, m.ID, bob.ID, ":e1:", model.Now())
	must(t, err)
}
