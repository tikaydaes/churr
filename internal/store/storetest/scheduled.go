package storetest

import (
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

func init() { register("scheduled", testScheduled) }

func testScheduled(t *testing.T, s store.Store) {
	ann := newAccount(t, s, "ann", model.RoleOwner)
	bob := newAccount(t, s, "bob", model.RoleMember)
	c := newChannel(t, s, "later", false, ann.ID)
	join(t, s, c.ID, bob.ID, model.LevelMember)
	now := model.Now()

	one := model.ScheduledItem{AccountID: ann.ID, ConversationID: c.ID, Kind: model.ScheduledMessage,
		Content: model.MessageInput{Text: "later"}, NextRunAt: now.Add(time.Hour), CreatedAt: now.Add(-3 * time.Second)}
	must(t, s.Scheduled().Create(ctx, &one))
	rep := model.ScheduledItem{AccountID: ann.ID, ConversationID: c.ID, Kind: model.ScheduledReminder,
		Content:   model.MessageInput{Text: "stand-up"},
		Repeat:    &model.Repeat{Every: "week", At: "09:30", TimeZone: "Europe/Dublin", Anchor: 4},
		NextRunAt: now.Add(-time.Minute), CreatedAt: now.Add(-2 * time.Second)}
	must(t, s.Scheduled().Create(ctx, &rep))
	bobs := model.ScheduledItem{AccountID: bob.ID, ConversationID: c.ID, Kind: model.ScheduledForm,
		Content: model.MessageInput{Form: &model.FormInput{Title: "Check-in", Action: model.FormAction{Type: model.FormThread},
			Fields: []model.FormField{{ID: "q1", Type: "text", Label: "Yesterday"}}}},
		Repeat: &model.Repeat{Every: "day", Days: "1111100", At: "09:00", TimeZone: "UTC"}, NextRunAt: now.Add(2 * time.Hour),
		CreatedAt: now.Add(-time.Second)}
	must(t, s.Scheduled().Create(ctx, &bobs))

	got, err := s.Scheduled().Get(ctx, rep.ID)
	must(t, err)
	eq(t, "content", got.Content.Text, "stand-up")
	eq(t, "repeat", *got.Repeat, *rep.Repeat)
	eq(t, "the anchor is kept", got.Repeat.Anchor, 4)
	got, err = s.Scheduled().Get(ctx, bobs.ID)
	must(t, err)
	eq(t, "a form in the content", got.Content.Form.Fields[0].Label, "Yesterday")
	_, err = s.Scheduled().Get(ctx, model.NewID[string]())
	wantErr(t, err, model.ErrNotFound)

	n, err := s.Scheduled().CountWaiting(ctx, ann.ID)
	must(t, err)
	eq(t, "waiting", n, 2)
	due, err := s.Scheduled().Due(ctx, now, 10)
	must(t, err)
	eq(t, "due", len(due) == 1 && due[0].ID == rep.ID, true)
	at, found, err := s.Scheduled().NextRun(ctx)
	must(t, err)
	eq(t, "next run", found && at == rep.NextRunAt, true)

	list, next, err := s.Scheduled().List(ctx, store.ScheduledFilter{AccountID: ann.ID}, store.Page{Limit: 1})
	must(t, err)
	eq(t, "newest first", len(list) == 1 && list[0].ID == rep.ID && next != "", true)
	list, next, err = s.Scheduled().List(ctx, store.ScheduledFilter{AccountID: ann.ID}, store.Page{Limit: 1, Cursor: next})
	must(t, err)
	eq(t, "second page", len(list) == 1 && list[0].ID == one.ID && next == "", true)
	list, _, err = s.Scheduled().List(ctx, store.ScheduledFilter{ConversationID: c.ID, Kind: model.ScheduledForm}, store.Page{})
	must(t, err)
	eq(t, "by conversation and kind", len(list) == 1 && list[0].ID == bobs.ID, true)
	list, _, err = s.Scheduled().List(ctx, store.ScheduledFilter{AccountID: ann.ID, ConversationID: c.ID, AlsoForms: true}, store.Page{})
	must(t, err)
	eq(t, "own items and everyone's recurring forms", len(list), 3)

	// A failed run: no next run, and the reason is kept.
	failed, err := s.Scheduled().Update(ctx, rep.ID, func(it *model.ScheduledItem) error {
		it.NextRunAt, it.FailedAt, it.Failure, it.FailureMessage = 0, now, "archived", "This channel is archived."
		return nil
	})
	must(t, err)
	eq(t, "failed", failed.Failure, "archived")
	got, _ = s.Scheduled().Get(ctx, rep.ID)
	eq(t, "failure read back", got.Failure == "archived" && got.FailureMessage == "This channel is archived." && got.FailedAt == now, true)
	eq(t, "not waiting", got.Waiting(), false)
	_, found, _ = s.Scheduled().NextRun(ctx)
	eq(t, "others still wait", found, true)
	// Changing it clears the failure and gives it a new owner.
	_, err = s.Scheduled().Update(ctx, rep.ID, func(it *model.ScheduledItem) error {
		it.AccountID, it.NextRunAt, it.FailedAt, it.Failure, it.FailureMessage = bob.ID, now.Add(time.Minute), 0, "", ""
		return nil
	})
	must(t, err)
	got, _ = s.Scheduled().Get(ctx, rep.ID)
	eq(t, "cleared", got.Failure == "" && got.FailedAt.IsZero() && got.AccountID == bob.ID && got.Waiting(), true)
	_, err = s.Scheduled().Update(ctx, model.NewID[string](), func(*model.ScheduledItem) error { return nil })
	wantErr(t, err, model.ErrNotFound)

	// Cancelled items are not listed and do not run.
	_, err = s.Scheduled().Update(ctx, one.ID, func(it *model.ScheduledItem) error {
		it.CancelledAt = now.Add(-40 * 24 * time.Hour)
		return nil
	})
	must(t, err)
	list, _, _ = s.Scheduled().List(ctx, store.ScheduledFilter{AccountID: ann.ID}, store.Page{})
	eq(t, "cancelled is not listed", len(list), 0)
	n, _ = s.Scheduled().CountWaiting(ctx, ann.ID)
	eq(t, "cancelled does not wait", n, 0)
	gone, err := s.Scheduled().DeleteFinishedBefore(ctx, now.Add(-30*24*time.Hour))
	must(t, err)
	eq(t, "old cancelled rows go", gone, int64(1))

	// Leaving the conversation deletes the rows (docs/FEATURES.md 10).
	_, err = s.Members().RemoveDirect(ctx, c.ID, bob.ID)
	must(t, err)
	list, _, _ = s.Scheduled().List(ctx, store.ScheduledFilter{AccountID: bob.ID}, store.Page{})
	eq(t, "rows go with the membership", len(list), 0)
	wantErr(t, s.Scheduled().Delete(ctx, one.ID), model.ErrNotFound)

	// A watch keeps the person it waits for (ruling R76), and Waiting
	// finds the watches, everyone's or one account's.
	w := model.ScheduledItem{AccountID: ann.ID, ConversationID: c.ID, Kind: model.ScheduledWatch, WatchAccountID: bob.ID,
		NextRunAt: now.Add(48 * time.Hour), CreatedAt: now}
	must(t, s.Scheduled().Create(ctx, &w))
	got, err = s.Scheduled().Get(ctx, w.ID)
	must(t, err)
	eq(t, "the watched person", got.WatchAccountID, bob.ID)
	eq(t, "a watch has no content", got.Content.Text, "")
	ws, err := s.Scheduled().Waiting(ctx, model.ScheduledWatch, "")
	must(t, err)
	eq(t, "every watch", len(ws) == 1 && ws[0].WatchAccountID == bob.ID, true)
	ws, err = s.Scheduled().Waiting(ctx, model.ScheduledWatch, bob.ID)
	must(t, err)
	eq(t, "bob has none", len(ws), 0)
	_, err = s.Scheduled().Update(ctx, w.ID, func(it *model.ScheduledItem) error {
		it.NextRunAt, it.LastRunAt = 0, now
		return nil
	})
	must(t, err)
	got, _ = s.Scheduled().Get(ctx, w.ID)
	eq(t, "the watched person survives an update", got.WatchAccountID, bob.ID)
	ws, _ = s.Scheduled().Waiting(ctx, model.ScheduledWatch, ann.ID)
	eq(t, "a fired watch no longer waits", len(ws), 0)

	// Saved replies: names are unique among the community's and among one
	// person's own, and a person's may equal a community one.
	shrug := model.SavedReply{Name: "shrug", Text: "¯\\_(ツ)_/¯"}
	must(t, s.Scheduled().CreateReply(ctx, &shrug))
	eq(t, "community", shrug.Community, true)
	wantErr(t, s.Scheduled().CreateReply(ctx, &model.SavedReply{Name: "shrug", Text: "again"}), model.ErrNameTaken)
	own := model.SavedReply{OwnerID: ann.ID, Name: "shrug", Text: "mine"}
	must(t, s.Scheduled().CreateReply(ctx, &own))
	wantErr(t, s.Scheduled().CreateReply(ctx, &model.SavedReply{OwnerID: ann.ID, Name: "shrug", Text: "again"}), model.ErrNameTaken)
	must(t, s.Scheduled().CreateReply(ctx, &model.SavedReply{OwnerID: bob.ID, Name: "shrug", Text: "bob's"}))
	hello := model.SavedReply{OwnerID: ann.ID, Name: "hello", Text: "Hello!"}
	must(t, s.Scheduled().CreateReply(ctx, &hello))

	replies, err := s.Scheduled().Replies(ctx, ann.ID)
	must(t, err)
	eq(t, "community and own", len(replies), 3)
	eq(t, "by name", replies[0].Name, "hello")
	eq(t, "the community's first", replies[1].Community && !replies[2].Community, true)
	replies, _ = s.Scheduled().Replies(ctx, "")
	eq(t, "community only", len(replies), 1)
	r, found, err := s.Scheduled().ReplyNamed(ctx, ann.ID, "shrug")
	must(t, err)
	eq(t, "own by name", found && r.Text == "mine", true)
	r, found, _ = s.Scheduled().ReplyNamed(ctx, "", "shrug")
	eq(t, "community by name", found && r.ID == shrug.ID, true)
	_, found, _ = s.Scheduled().ReplyNamed(ctx, ann.ID, "nope")
	eq(t, "no such name", found, false)

	_, err = s.Scheduled().UpdateReply(ctx, hello.ID, "shrug", "x")
	wantErr(t, err, model.ErrNameTaken)
	up, err := s.Scheduled().UpdateReply(ctx, hello.ID, "hi", "Hi!")
	must(t, err)
	eq(t, "updated", up.Name == "hi" && up.Text == "Hi!" && up.OwnerID == ann.ID, true)
	_, err = s.Scheduled().UpdateReply(ctx, model.NewID[string](), "zz", "x")
	wantErr(t, err, model.ErrNotFound)
	r, err = s.Scheduled().Reply(ctx, hello.ID)
	must(t, err)
	eq(t, "read back", r.Name, "hi")
	must(t, s.Scheduled().DeleteReply(ctx, hello.ID))
	wantErr(t, s.Scheduled().DeleteReply(ctx, hello.ID), model.ErrNotFound)
}
