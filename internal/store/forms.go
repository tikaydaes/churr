package store

import (
	"context"

	"github.com/tikaydaes/churr/internal/model"
)

// Forms is the `forms`, `form_answers` and `form_tallies` tables.
//
// The store keeps these true, in the transaction of whatever changed
// (docs/SCHEMA.md, forms):
//
//   - forms.answer_count is the number of distinct accounts with a row in
//     form_answers for the form;
//   - for a form whose action is `tally`, form_tallies.count for a field and
//     option is the number of answer rows whose value for that field holds
//     that option, with a button press counted under the field
//     model.ButtonField. Recount works both out again from the rows, which
//     is how the tests check it.
//
// A writer that depends on what it read (one answer per account, places
// left on an option) calls Lock first, in its transaction.
type Forms interface {
	// Create stores a form. An empty ID and zero CreatedAt are filled in.
	// Exactly one of MessageID and DialogFor must be set.
	Create(ctx context.Context, f *Form) error
	// Get returns one form.
	Get(ctx context.Context, id model.FormID) (Form, error)
	// Lock is Get that also holds the form's row until the transaction
	// ends, so answers to one form are taken one after another.
	Lock(ctx context.Context, id model.FormID) (Form, error)
	// ByMessage returns the form a message carries. found is false if it
	// carries none.
	ByMessage(ctx context.Context, msg model.MessageID) (f Form, found bool, err error)
	// ForMessages returns the forms of the messages given, by message.
	ForMessages(ctx context.Context, msgs []model.MessageID) (map[model.MessageID]Form, error)
	// Replace rewrites what a form asks: preset, title, fields, buttons,
	// action, results, allow_added_options, answerer group, one_answer
	// and closes_at. Whether it is anonymous, who owns it, its answers
	// and whether it is closed stay as they are.
	Replace(ctx context.Context, f Form) error
	// SetFields rewrites only the fields, for an option an answerer
	// added.
	SetFields(ctx context.Context, id model.FormID, fields []model.FormField) error
	// Close sets closed_at and closed_by (empty when the form closed
	// itself) once. closed is false if it was closed already.
	Close(ctx context.Context, id model.FormID, by model.AccountID, at model.Time) (closed bool, err error)
	// Delete removes a form with its answers and tallies: a dialog that
	// was answered or dismissed, or a form an edit took off its message.
	Delete(ctx context.Context, id model.FormID) error
	// DueToClose returns open forms on messages whose closes_at is at or
	// before now, earliest first.
	DueToClose(ctx context.Context, now model.Time, limit int) ([]Form, error)
	// NextClose returns the earliest closes_at among open forms on
	// messages. found is false if there is none.
	NextClose(ctx context.Context) (at model.Time, found bool, err error)
	// DeleteDialogsBefore removes dialogs made before the given moment
	// and returns how many went.
	DeleteDialogsBefore(ctx context.Context, before model.Time) (int64, error)

	// Answer stores an answer and keeps answer_count and, for a `tally`
	// form, the tallies right. With replace set, the account's earlier
	// answer (if any) is overwritten in place and returned as prev;
	// without it a new row is always added. An empty ID and zero
	// CreatedAt are filled in; on a replace the row keeps its id and
	// created_at and gets updated_at.
	Answer(ctx context.Context, a *model.FormAnswer, replace bool) (prev *model.FormAnswer, err error)
	// Withdraw deletes every answer of an account to a form and keeps the
	// count and tallies right. removed is false if there was none.
	Withdraw(ctx context.Context, form model.FormID, account model.AccountID) (removed bool, err error)
	// AnswerOf returns an account's newest answer to a form.
	AnswerOf(ctx context.Context, form model.FormID, account model.AccountID) (a model.FormAnswer, found bool, err error)
	// AnswersBy returns one account's newest answer to each of the forms
	// given that it has answered.
	AnswersBy(ctx context.Context, forms []model.FormID, account model.AccountID) (map[model.FormID]model.FormAnswer, error)
	// Answers returns a page of a form's answers: oldest first, or with
	// byID in the order of their identifiers, which for an anonymous
	// form says nothing about when or by whom each was given.
	Answers(ctx context.Context, form model.FormID, byID bool, p Page) (answers []model.FormAnswer, next string, err error)
	// AllAnswers returns every answer to a form, oldest first.
	AllAnswers(ctx context.Context, form model.FormID) ([]model.FormAnswer, error)
	// SetResultMessage records the message a `thread` or `channel` answer
	// produced.
	SetResultMessage(ctx context.Context, answerID string, msg model.MessageID) error
	// SetTicketParent makes a ticket channel a sub-channel of the channel
	// that holds the form which opened it, or its sibling when that
	// channel is itself a sub-channel (ruling R49). It does nothing when
	// the holder is not a named channel, which is never a parent.
	SetTicketParent(ctx context.Context, ticket, holder model.ConversationID) error
	// TicketParent returns the parent a conversation was given, or "".
	TicketParent(ctx context.Context, conv model.ConversationID) (model.ConversationID, error)
	// Tallies returns the counts of the forms given: by form, then field
	// id, then option id. Options nobody chose are absent.
	Tallies(ctx context.Context, forms []model.FormID) (map[model.FormID]map[string]map[string]int, error)
	// Recount works a form's answer count and tally out again from its
	// answer rows, without changing anything.
	Recount(ctx context.Context, form model.FormID) (answerCount int, tally map[string]map[string]int, err error)
}

// Form is one row of `forms`: the form as the API shows it, with the stored
// columns that are not sent. Tally, Voters and MyAnswer are never filled by
// the store.
type Form struct {
	model.Form
	// MessageID is the message carrying the form; empty for a dialog.
	MessageID model.MessageID
	// DialogFor is the one account a dialog is shown to.
	DialogFor model.AccountID
	// OwnerID is the message's author, or the bot behind a dialog.
	OwnerID model.AccountID
	// ClosedBy is who closed it; empty if it closed itself.
	ClosedBy  model.AccountID
	CreatedAt model.Time
}

// One reports whether the form holds each account to one answer.
func (f Form) One() bool { return f.OneAnswer == nil || *f.OneAnswer }

// TallyKeys returns what one answer adds to a form's tally: a (field,
// option) pair for every option chosen in a choice field, and the button
// pressed under model.ButtonField. A value that is not a list of strings
// counts for nothing.
func TallyKeys(fields []model.FormField, answers map[string]any, button string) [][2]string {
	var out [][2]string
	for _, f := range fields {
		if f.Type != "choice" {
			continue
		}
		switch v := answers[f.ID].(type) {
		case []string:
			for _, o := range v {
				out = append(out, [2]string{f.ID, o})
			}
		case []any:
			for _, o := range v {
				if s, ok := o.(string); ok {
					out = append(out, [2]string{f.ID, s})
				}
			}
		}
	}
	if button != "" {
		out = append(out, [2]string{model.ButtonField, button})
	}
	return out
}
