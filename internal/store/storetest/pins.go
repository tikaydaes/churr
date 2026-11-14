package storetest

import (
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

func init() { register("pins", testPins) }

func testPins(t *testing.T, s store.Store) {
	ann := newAccount(t, s, "ann", model.RoleOwner)
	c := newChannel(t, s, "pinned", false, ann.ID)
	first := post(t, s, c.ID, ann.ID, "", "pin me")
	pin, err := s.Pins().Add(ctx, c.ID, first.ID, ann.ID, model.Now())
	must(t, err)
	eq(t, "pinned by", pin.PinnedByID, ann.ID)
	_, err = s.Pins().Add(ctx, c.ID, first.ID, ann.ID, model.Now())
	wantErr(t, err, model.ErrAlreadyExists)
	msgs := []model.Message{first}
	must(t, s.Messages().Hydrate(ctx, msgs, ann.ID))
	eq(t, "pinned flag", msgs[0].Pinned, true)

	for i := 1; i < model.MaxPinsPerConversation; i++ {
		m := post(t, s, c.ID, ann.ID, "", "more")
		_, err := s.Pins().Add(ctx, c.ID, m.ID, ann.ID, model.Now().Add(time.Second))
		must(t, err)
	}
	extra := post(t, s, c.ID, ann.ID, "", "one too many")
	_, err = s.Pins().Add(ctx, c.ID, extra.ID, ann.ID, model.Now())
	wantErr(t, err, model.ErrConflict)
	list, err := s.Pins().List(ctx, c.ID)
	must(t, err)
	eq(t, "a hundred pins", len(list), model.MaxPinsPerConversation)

	must(t, s.Pins().Remove(ctx, c.ID, first.ID))
	wantErr(t, s.Pins().Remove(ctx, c.ID, first.ID), model.ErrNotFound)
	_, err = s.Pins().Add(ctx, c.ID, extra.ID, ann.ID, model.Now())
	must(t, err)

	// Deleting a message unpins it.
	_, err = s.Messages().Delete(ctx, extra.ID, ann.ID, model.Now())
	must(t, err)
	list, _ = s.Pins().List(ctx, c.ID)
	eq(t, "unpinned by the delete", len(list), model.MaxPinsPerConversation-1)
}
