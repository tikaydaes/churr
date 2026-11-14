package storetest

import (
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

func init() { register("events", testEvents) }

func testEvents(t *testing.T, s store.Store) {
	ann := newAccount(t, s, "ann", model.RoleOwner)
	c := newChannel(t, s, "log", false, ann.ID)
	oldest, err := s.Events().Oldest(ctx, c.ID)
	must(t, err)
	eq(t, "empty log", oldest, int64(0))

	var msgs []model.Message
	for i := 0; i < 6; i++ {
		msgs = append(msgs, post(t, s, c.ID, ann.ID, "", "event"))
	}
	evs, more, err := s.Events().List(ctx, c.ID, 0, 0, 4)
	must(t, err)
	eq(t, "page", len(evs), 4)
	eq(t, "more", more, true)
	eq(t, "oldest first", evs[0].Seq, int64(1))
	eq(t, "type", evs[0].Type, model.EventMessageCreated)
	eq(t, "message id column", evs[0].MessageID, msgs[0].ID)
	// The payload comes back as it was sent, in plain JSON form.
	body, ok := evs[0].Payload["message"].(map[string]any)
	eq(t, "payload carries the message", ok && body["id"] == string(msgs[0].ID), true)

	evs, more, err = s.Events().List(ctx, c.ID, 2, 4, 50)
	must(t, err)
	eq(t, "after 2 until 4", len(evs) == 2 && evs[0].Seq == 3 && evs[1].Seq == 4 && !more, true)

	// Readers must accept gaps.
	must(t, s.Events().DeleteForMessage(ctx, msgs[2].ID))
	evs, _, err = s.Events().List(ctx, c.ID, 0, 0, 50)
	must(t, err)
	eq(t, "gap", len(evs) == 5 && evs[2].Seq == 4, true)
	oldest, _ = s.Events().Oldest(ctx, c.ID)
	eq(t, "oldest", oldest, int64(1))

	n, err := s.Events().DeleteBefore(ctx, model.Now().Add(time.Hour))
	must(t, err)
	eq(t, "old events deleted", n, int64(5))
	oldest, _ = s.Events().Oldest(ctx, c.ID)
	eq(t, "log empty again", oldest, int64(0))
}
