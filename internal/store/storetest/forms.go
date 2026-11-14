package storetest

import (
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

func init() { register("forms", testForms) }

// formsRecount fails the test unless a form's stored answer count and tally
// equal what its answer rows give (docs/SCHEMA.md, form_tallies).
func formsRecount(t *testing.T, s store.Store, form model.FormID) {
	t.Helper()
	count, want, err := s.Forms().Recount(ctx, form)
	must(t, err)
	f, err := s.Forms().Get(ctx, form)
	must(t, err)
	eq(t, "answer_count equals the distinct accounts", f.AnswerCount, count)
	got, err := s.Forms().Tallies(ctx, []model.FormID{form})
	must(t, err)
	n := 0
	for field, options := range got[form] {
		for option, c := range options {
			n++
			eq(t, "tally of "+field+"/"+option, c, want[field][option])
		}
	}
	m := 0
	for _, options := range want {
		m += len(options)
	}
	eq(t, "options with a count", n, m)
}

func testForms(t *testing.T, s store.Store) {
	ann := newAccount(t, s, "ann", model.RoleOwner)
	bob := newAccount(t, s, "bob", model.RoleMember)
	cat := newAccount(t, s, "cat", model.RoleMember)
	c := newChannel(t, s, "polls", false, ann.ID)
	m := post(t, s, c.ID, ann.ID, "", "lunch?")

	poll := store.Form{
		Form: model.Form{
			Preset: "poll", Title: "Lunch?",
			Fields: []model.FormField{{ID: "choice", Type: "choice", Label: "Where", Multiple: true,
				Options: []model.FormOption{{ID: "o1", Label: "Noodles"}, {ID: "o2", Label: "Tacos"}}}},
			Buttons: []model.FormButton{{ID: "ok", Label: "OK"}},
			Action:  model.FormAction{Type: model.FormTally}, Results: model.ResultsAfterAnswer,
			ClosesAt: model.Now().Add(time.Hour),
		},
		MessageID: m.ID, OwnerID: ann.ID,
	}
	must(t, s.Forms().Create(ctx, &poll))
	if s.Forms().Create(ctx, &store.Form{OwnerID: ann.ID}) == nil {
		t.Fatal("a form with neither a message nor a person was stored")
	}

	got, err := s.Forms().Get(ctx, poll.ID)
	must(t, err)
	eq(t, "title", got.Title, "Lunch?")
	eq(t, "results", got.Results, model.ResultsAfterAnswer)
	eq(t, "one answer by default", got.One(), true)
	eq(t, "options", len(got.Fields[0].Options), 2)
	eq(t, "button", got.Buttons[0].ID, "ok")
	eq(t, "closes_at", got.ClosesAt, poll.ClosesAt)
	_, err = s.Forms().Get(ctx, model.NewID[model.FormID]())
	wantErr(t, err, model.ErrNotFound)
	byMsg, found, err := s.Forms().ByMessage(ctx, m.ID)
	must(t, err)
	eq(t, "by message", found && byMsg.ID == poll.ID, true)
	other := post(t, s, c.ID, ann.ID, "", "no form")
	_, found, err = s.Forms().ByMessage(ctx, other.ID)
	must(t, err)
	eq(t, "no form on a plain message", found, false)
	many, err := s.Forms().ForMessages(ctx, []model.MessageID{m.ID, other.ID})
	must(t, err)
	eq(t, "forms of messages", len(many), 1)

	// Answers keep the count and the tally in step.
	a := model.FormAnswer{FormID: poll.ID, AccountID: bob.ID, Answers: map[string]any{"choice": []string{"o1", "o2"}}}
	prev, err := s.Forms().Answer(ctx, &a, true)
	must(t, err)
	eq(t, "first answer replaces nothing", prev == nil, true)
	formsRecount(t, s, poll.ID)
	a2 := model.FormAnswer{FormID: poll.ID, AccountID: cat.ID, ButtonID: "ok", Answers: map[string]any{"choice": []string{"o2"}}}
	_, err = s.Forms().Answer(ctx, &a2, true)
	must(t, err)
	formsRecount(t, s, poll.ID)
	tallies, err := s.Forms().Tallies(ctx, []model.FormID{poll.ID})
	must(t, err)
	eq(t, "o1", tallies[poll.ID]["choice"]["o1"], 1)
	eq(t, "o2", tallies[poll.ID]["choice"]["o2"], 2)
	eq(t, "the button is tallied", tallies[poll.ID][model.ButtonField]["ok"], 1)

	// Changing an answer rewrites the row in place.
	change := model.FormAnswer{FormID: poll.ID, AccountID: bob.ID, Answers: map[string]any{"choice": []string{"o2"}}}
	prev, err = s.Forms().Answer(ctx, &change, true)
	must(t, err)
	eq(t, "the earlier answer comes back", prev != nil && prev.ID == a.ID, true)
	eq(t, "the row keeps its id", change.ID, a.ID)
	formsRecount(t, s, poll.ID)
	got, _ = s.Forms().Get(ctx, poll.ID)
	eq(t, "two accounts have answered", got.AnswerCount, 2)
	mine, found, err := s.Forms().AnswerOf(ctx, poll.ID, bob.ID)
	must(t, err)
	eq(t, "own answer", found && !mine.UpdatedAt.IsZero(), true)
	by, err := s.Forms().AnswersBy(ctx, []model.FormID{poll.ID}, cat.ID)
	must(t, err)
	eq(t, "answers by account", by[poll.ID].ButtonID, "ok")
	_, found, _ = s.Forms().AnswerOf(ctx, poll.ID, ann.ID)
	eq(t, "no answer", found, false)

	page, next, err := s.Forms().Answers(ctx, poll.ID, false, store.Page{Limit: 1})
	must(t, err)
	eq(t, "one per page", len(page) == 1 && next != "", true)
	page2, next, err := s.Forms().Answers(ctx, poll.ID, false, store.Page{Limit: 1, Cursor: next})
	must(t, err)
	eq(t, "second page", len(page2) == 1 && next == "" && page2[0].ID != page[0].ID, true)
	byID, next, err := s.Forms().Answers(ctx, poll.ID, true, store.Page{Limit: 1})
	must(t, err)
	eq(t, "by id: one per page", len(byID) == 1 && next != "", true)
	byID2, next, err := s.Forms().Answers(ctx, poll.ID, true, store.Page{Limit: 1, Cursor: next})
	must(t, err)
	eq(t, "by id: second page", len(byID2) == 1 && next == "" && byID2[0].ID > byID[0].ID, true)

	removed, err := s.Forms().Withdraw(ctx, poll.ID, bob.ID)
	must(t, err)
	eq(t, "withdrawn", removed, true)
	removed, err = s.Forms().Withdraw(ctx, poll.ID, bob.ID)
	must(t, err)
	eq(t, "nothing left to withdraw", removed, false)
	formsRecount(t, s, poll.ID)

	// An option an answerer added.
	fields := append([]model.FormField(nil), got.Fields...)
	fields[0].Options = append(fields[0].Options, model.FormOption{ID: "a1", Label: "Soup", AddedByID: bob.ID})
	must(t, s.Forms().SetFields(ctx, poll.ID, fields))
	got, _ = s.Forms().Get(ctx, poll.ID)
	eq(t, "added option", got.Fields[0].Options[2].AddedByID, bob.ID)

	// Many answers per account, as thread and channel forms allow.
	m2 := post(t, s, c.ID, ann.ID, "", "tickets")
	no := false
	ticket := store.Form{Form: model.Form{Fields: []model.FormField{{ID: "what", Type: "text", Label: "What"}},
		Action: model.FormAction{Type: model.FormThread}, OneAnswer: &no}, MessageID: m2.ID, OwnerID: ann.ID}
	must(t, s.Forms().Create(ctx, &ticket))
	for i := 0; i < 3; i++ {
		row := model.FormAnswer{FormID: ticket.ID, AccountID: bob.ID, Answers: map[string]any{"what": "x"}}
		_, err := s.Forms().Answer(ctx, &row, false)
		must(t, err)
		must(t, s.Forms().SetResultMessage(ctx, row.ID, m2.ID))
	}
	all, err := s.Forms().AllAnswers(ctx, ticket.ID)
	must(t, err)
	eq(t, "three rows", len(all), 3)
	eq(t, "result message", all[0].ResultMessageID, m2.ID)
	formsRecount(t, s, ticket.ID)
	tf, _ := s.Forms().Get(ctx, ticket.ID)
	eq(t, "one account, however many rows", tf.AnswerCount, 1)
	eq(t, "one_answer false", tf.One(), false)

	// Replace rewrites what the form asks and nothing else.
	tf.Title = "Tickets"
	tf.Anonymous = true
	must(t, s.Forms().Replace(ctx, tf))
	tf, _ = s.Forms().Get(ctx, ticket.ID)
	eq(t, "replaced title", tf.Title, "Tickets")
	eq(t, "anonymous never changes", tf.Anonymous, false)

	// Closing happens once.
	at, found, err := s.Forms().NextClose(ctx)
	must(t, err)
	eq(t, "next close", found && at == poll.ClosesAt, true)
	due, err := s.Forms().DueToClose(ctx, model.Now(), 10)
	must(t, err)
	eq(t, "nothing due yet", len(due), 0)
	due, err = s.Forms().DueToClose(ctx, poll.ClosesAt, 10)
	must(t, err)
	eq(t, "due at closes_at", len(due), 1)
	closed, err := s.Forms().Close(ctx, poll.ID, "", model.Now())
	must(t, err)
	eq(t, "closed", closed, true)
	closed, err = s.Forms().Close(ctx, poll.ID, ann.ID, model.Now())
	must(t, err)
	eq(t, "closed only once", closed, false)
	got, _ = s.Forms().Get(ctx, poll.ID)
	eq(t, "closed by nobody", got.ClosedBy == "" && !got.ClosedAt.IsZero(), true)
	_, found, _ = s.Forms().NextClose(ctx)
	eq(t, "nothing left to close", found, false)

	// Dialogs have no message and go after an hour.
	dialog := store.Form{Form: model.Form{Fields: []model.FormField{{ID: "why", Type: "longtext", Label: "Why?"}},
		Action: model.FormAction{Type: model.FormBot}}, DialogFor: bob.ID, OwnerID: ann.ID, CreatedAt: model.Now().Add(-2 * time.Hour)}
	must(t, s.Forms().Create(ctx, &dialog))
	fresh := store.Form{Form: model.Form{Action: model.FormAction{Type: model.FormBot}, Buttons: []model.FormButton{{ID: "ok", Label: "OK"}}},
		DialogFor: bob.ID, OwnerID: ann.ID}
	must(t, s.Forms().Create(ctx, &fresh))
	n, err := s.Forms().DeleteDialogsBefore(ctx, model.Now().Add(-time.Hour))
	must(t, err)
	eq(t, "old dialogs deleted", n, int64(1))
	must(t, s.Forms().Delete(ctx, fresh.ID))
	wantErr(t, s.Forms().Delete(ctx, fresh.ID), model.ErrNotFound)

	// A ticket channel is a sub-channel of the channel holding the form,
	// or its sibling when that channel is itself a sub-channel; an ad-hoc
	// channel is never a parent (ruling R49).
	t1 := newChannel(t, s, "ticket-1", true, bob.ID)
	must(t, s.Forms().SetTicketParent(ctx, t1.ID, c.ID))
	parent, err := s.Forms().TicketParent(ctx, t1.ID)
	must(t, err)
	eq(t, "a ticket's parent", parent, c.ID)
	t2 := newChannel(t, s, "ticket-2", true, bob.ID)
	must(t, s.Forms().SetTicketParent(ctx, t2.ID, t1.ID))
	parent, _ = s.Forms().TicketParent(ctx, t2.ID)
	eq(t, "a ticket opened from a sub-channel is its sibling", parent, c.ID)
	t3 := newChannel(t, s, "ticket-3", true, bob.ID)
	must(t, s.Forms().SetTicketParent(ctx, t3.ID, newAdhoc(t, s, ann.ID, bob.ID).ID))
	parent, _ = s.Forms().TicketParent(ctx, t3.ID)
	eq(t, "no parent from an ad-hoc channel", parent, model.ConversationID(""))

	// A form goes with its message, answers and tallies included.
	_, err = s.Messages().DeleteOutright(ctx, m.ID)
	must(t, err)
	_, err = s.Forms().Get(ctx, poll.ID)
	wantErr(t, err, model.ErrNotFound)
	tallies, err = s.Forms().Tallies(ctx, []model.FormID{poll.ID})
	must(t, err)
	eq(t, "tallies gone", len(tallies), 0)
}
