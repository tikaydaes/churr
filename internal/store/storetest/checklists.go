package storetest

import (
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

func init() { register("checklists", testChecklists) }

func testChecklists(t *testing.T, s store.Store) {
	ann := newAccount(t, s, "ann", model.RoleOwner)
	c := newChannel(t, s, "lists", false, ann.ID)
	m := post(t, s, c.ID, ann.ID, "", "- [ ] one\n- [x] two")
	must(t, s.Checklists().Replace(ctx, m.ID, []store.ChecklistItem{
		{ItemID: "t1", Position: 0, Text: "one"}, {ItemID: "t2", Position: 1, Text: "two", Checked: true},
	}, model.Now()))
	items, err := s.Checklists().Items(ctx, m.ID, false)
	must(t, err)
	eq(t, "items", len(items), 2)
	eq(t, "typed state", items[1].Checked, true)

	at := model.Now()
	task, err := s.Checklists().Tick(ctx, m.ID, "t1", ann.ID, true, at)
	must(t, err)
	eq(t, "ticked", task.Checked && task.CheckedByID == ann.ID && task.CheckedAt == at, true)
	_, err = s.Checklists().Tick(ctx, m.ID, "t1", ann.ID, false, at.Add(time.Second))
	must(t, err)
	_, err = s.Checklists().Tick(ctx, m.ID, "t1", ann.ID, true, at.Add(2*time.Second))
	must(t, err)
	_, err = s.Checklists().Tick(ctx, m.ID, "nope", ann.ID, true, at)
	wantErr(t, err, model.ErrNotFound)
	hist, err := s.Checklists().History(ctx, m.ID, "t1")
	must(t, err)
	eq(t, "every change kept", len(hist), 3)
	eq(t, "oldest first", hist[0].Checked && !hist[1].Checked && hist[2].Checked, true)

	// An edit removes t2 and rewords t1: t1 keeps its state.
	must(t, s.Checklists().Replace(ctx, m.ID, []store.ChecklistItem{{ItemID: "t1", Position: 0, Text: "one, reworded"}}, model.Now()))
	items, _ = s.Checklists().Items(ctx, m.ID, false)
	eq(t, "one left", len(items), 1)
	eq(t, "still ticked", items[0].Checked, true)
	eq(t, "reworded", items[0].Text, "one, reworded")
	all, _ := s.Checklists().Items(ctx, m.ID, true)
	eq(t, "removed item kept for history", len(all), 2)
	_, err = s.Checklists().Tick(ctx, m.ID, "t2", ann.ID, false, model.Now())
	wantErr(t, err, model.ErrNotFound)

	msgs := []model.Message{m}
	must(t, s.Messages().Hydrate(ctx, msgs, ann.ID))
	eq(t, "tasks on the message", len(msgs[0].Tasks) == 1 && msgs[0].Tasks[0].ID == "t1", true)

	// Named again, it comes back.
	must(t, s.Checklists().Replace(ctx, m.ID, []store.ChecklistItem{{ItemID: "t1", Position: 0, Text: "one"}, {ItemID: "t2", Position: 1, Text: "two"}}, model.Now()))
	items, _ = s.Checklists().Items(ctx, m.ID, false)
	eq(t, "both back", len(items), 2)
	eq(t, "with its state", items[1].Checked, true)
}
