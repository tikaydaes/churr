package forms

import (
	"context"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

// This file is what the link to other servers uses of this package for a
// poll in a conversation shared between servers (docs/LINK-PROTOCOL.md
// section 19). The poll's home server counts every server's answers with
// the same rules a local answer is held to, so the rules are lent out from
// here rather than written a second time.

// Shared is those rules as the link takes them: it declares the methods it
// needs (link.PollRules) and the server hands it this.
type Shared struct{}

// Neutral is a form as an event carries it before it is tailored: what
// anyone in the conversation may see.
func (Shared) Neutral(ctx context.Context, a store.Areas, f store.Form) (model.Form, error) {
	return neutral(ctx, a, f)
}

// PlacesLeft refuses an answer that picks an option whose places are all
// taken by other accounts. The form's row must be locked.
func (Shared) PlacesLeft(ctx context.Context, tx store.Tx, f store.Form, account model.AccountID, answers map[string]any) error {
	return placesLeft(ctx, tx, f, account, answers)
}

// NewAnswer makes the row for an answer, with an identifier that says
// nothing on an anonymous form.
func (Shared) NewAnswer(f store.Form, account model.AccountID, button string, answers map[string]any) model.FormAnswer {
	return newAnswer(f, account, button, answers)
}

// Who is the account_id of form.answered and form.answer_withdrawn as local
// readers may be told it: only on a named poll with live results.
func (Shared) Who(f store.Form, account model.AccountID) map[string]any { return who(f, account) }

// CheckRemoteAnswers is CheckAnswers for an answer from another server.
// This server cannot say what that account sees, so a field that names an
// account or a channel cannot be answered from there.
func (Shared) CheckRemoteAnswers(fields []model.FormField, answers map[string]any) (map[string]any, error) {
	return CheckAnswers(fields, answers, noLookup{})
}

type noLookup struct{}

func (noLookup) SeesAccount(model.AccountID) bool      { return false }
func (noLookup) SeesChannel(model.ConversationID) bool { return false }
