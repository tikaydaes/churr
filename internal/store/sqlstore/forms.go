package sqlstore

import (
	"context"
	"database/sql"
	"errors"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

type forms struct{ *queries }

// Forms returns the forms area.
func (q *queries) Forms() store.Forms { return forms{q} }

const formCols = `id, message_id, dialog_for, owner_id, preset, title, fields, buttons, action, anonymous, results,
	allow_added_options, answerer_group_id, one_answer, closes_at, closed_at, closed_by, answer_count, created_at`

func scanForm(s scanner) (store.Form, error) {
	var f store.Form
	var one bool
	err := s.Scan(str(&f.ID), str(&f.MessageID), str(&f.DialogFor), str(&f.OwnerID), str(&f.Preset), &f.Title,
		js(&f.Fields), js(&f.Buttons), js(&f.Action), flag(&f.Anonymous), &f.Results, flag(&f.AllowAddedOptions),
		str(&f.AnswererGroupID), flag(&one), tm(&f.ClosesAt), tm(&f.ClosedAt), str(&f.ClosedBy), &f.AnswerCount, tm(&f.CreatedAt))
	f.OneAnswer = &one
	if f.Fields == nil {
		f.Fields = []model.FormField{}
	}
	if len(f.Buttons) == 0 {
		f.Buttons = nil
	}
	return f, err
}

// results is what goes in the `results` column, which is never null.
func results(f store.Form) string {
	if f.Results == "" {
		return model.ResultsLive
	}
	return f.Results
}

func (fs forms) Create(ctx context.Context, f *store.Form) error {
	if (f.MessageID == "") == (f.DialogFor == "") {
		return errors.New("sqlstore: a form is on a message or is a dialog, and not both")
	}
	if f.ID == "" {
		f.ID = model.NewID[model.FormID]()
	}
	if f.CreatedAt.IsZero() {
		f.CreatedAt = model.Now()
	}
	one := f.One()
	f.OneAnswer = &one
	_, err := fs.wr.ExecContext(ctx, `
		INSERT INTO forms (id, message_id, dialog_for, owner_id, preset, title, fields, buttons, action, anonymous, results,
			allow_added_options, answerer_group_id, one_answer, closes_at, answer_count, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ?)`,
		string(f.ID), id(f.MessageID), id(f.DialogFor), string(f.OwnerID), id(f.Preset), f.Title, jsonList(f.Fields),
		jsonList(f.Buttons), jsonObject(f.Action), f.Anonymous, results(*f), f.AllowAddedOptions, id(f.AnswererGroupID),
		one, ms(f.ClosesAt), int64(f.CreatedAt))
	return fs.mapErr(err)
}

func (fs forms) Get(ctx context.Context, formID model.FormID) (store.Form, error) {
	f, err := scanForm(fs.rd.QueryRowContext(ctx, `SELECT `+formCols+` FROM forms WHERE id = ?`, string(formID)))
	return f, fs.mapErr(err)
}

func (fs forms) Lock(ctx context.Context, formID model.FormID) (store.Form, error) {
	f, err := scanForm(fs.wr.QueryRowContext(ctx, `SELECT `+formCols+` FROM forms WHERE id = ?`+fs.forUpdate(), string(formID)))
	return f, fs.mapErr(err)
}

func (fs forms) ByMessage(ctx context.Context, msg model.MessageID) (store.Form, bool, error) {
	f, err := scanForm(fs.rd.QueryRowContext(ctx, `SELECT `+formCols+` FROM forms WHERE message_id = ?`, string(msg)))
	if errors.Is(err, sql.ErrNoRows) {
		return f, false, nil
	}
	return f, err == nil, err
}

func (fs forms) ForMessages(ctx context.Context, msgs []model.MessageID) (map[model.MessageID]store.Form, error) {
	out := map[model.MessageID]store.Form{}
	if len(msgs) == 0 {
		return out, nil
	}
	rows, err := fs.rd.QueryContext(ctx, `SELECT `+formCols+` FROM forms WHERE message_id IN (`+marks(len(msgs))+`)`, anys(msgs)...)
	list, err := collect(rows, err, scanForm)
	for _, f := range list {
		out[f.MessageID] = f
	}
	return out, err
}

func (fs forms) Replace(ctx context.Context, f store.Form) error {
	return fs.changed(fs.wr.ExecContext(ctx, `
		UPDATE forms SET preset = ?, title = ?, fields = ?, buttons = ?, action = ?, results = ?, allow_added_options = ?,
			answerer_group_id = ?, one_answer = ?, closes_at = ? WHERE id = ?`,
		id(f.Preset), f.Title, jsonList(f.Fields), jsonList(f.Buttons), jsonObject(f.Action), results(f), f.AllowAddedOptions,
		id(f.AnswererGroupID), f.One(), ms(f.ClosesAt), string(f.ID)))
}

func (fs forms) SetFields(ctx context.Context, formID model.FormID, fields []model.FormField) error {
	return fs.changed(fs.wr.ExecContext(ctx, `UPDATE forms SET fields = ? WHERE id = ?`, jsonList(fields), string(formID)))
}

func (fs forms) Close(ctx context.Context, formID model.FormID, by model.AccountID, at model.Time) (bool, error) {
	n, err := fs.affected(fs.wr.ExecContext(ctx,
		`UPDATE forms SET closed_at = ?, closed_by = ? WHERE id = ? AND closed_at IS NULL`, int64(at), id(by), string(formID)))
	return n > 0, err
}

func (fs forms) Delete(ctx context.Context, formID model.FormID) error {
	// The answers and tallies go with the row (ON DELETE CASCADE).
	return fs.changed(fs.wr.ExecContext(ctx, `DELETE FROM forms WHERE id = ?`, string(formID)))
}

func (fs forms) DueToClose(ctx context.Context, now model.Time, limit int) ([]store.Form, error) {
	rows, err := fs.rd.QueryContext(ctx, `
		SELECT `+formCols+` FROM forms
		WHERE closes_at IS NOT NULL AND closes_at <= ? AND closed_at IS NULL AND message_id IS NOT NULL
		ORDER BY closes_at, id LIMIT ?`, int64(now), limit)
	return collect(rows, err, scanForm)
}

func (fs forms) NextClose(ctx context.Context) (model.Time, bool, error) {
	var at model.Time
	err := fs.rd.QueryRowContext(ctx, `
		SELECT min(closes_at) FROM forms WHERE closes_at IS NOT NULL AND closed_at IS NULL AND message_id IS NOT NULL`).Scan(tm(&at))
	return at, !at.IsZero(), err
}

func (fs forms) DeleteDialogsBefore(ctx context.Context, before model.Time) (int64, error) {
	return fs.affected(fs.wr.ExecContext(ctx,
		`DELETE FROM forms WHERE dialog_for IS NOT NULL AND created_at < ?`, int64(before)))
}

const answerCols = `id, form_id, account_id, button_id, answers, result_message_id, created_at, updated_at`

func scanAnswer(s scanner) (model.FormAnswer, error) {
	var a model.FormAnswer
	err := s.Scan(str(&a.ID), str(&a.FormID), str(&a.AccountID), str(&a.ButtonID), js(&a.Answers), str(&a.ResultMessageID),
		tm(&a.CreatedAt), tm(&a.UpdatedAt))
	if a.Answers == nil {
		a.Answers = map[string]any{}
	}
	return a, err
}

// bump adds delta to the tally of each key an answer counts for. A count
// that reaches zero keeps its row: a missing row and a zero mean the same.
func (q *queries) bump(ctx context.Context, form model.FormID, keys [][2]string, delta int) error {
	for _, k := range keys {
		_, err := q.wr.ExecContext(ctx, `
			INSERT INTO form_tallies (form_id, field_id, option_id, count) VALUES (?, ?, ?, ?)
			ON CONFLICT (form_id, field_id, option_id) DO UPDATE SET count = form_tallies.count + excluded.count`,
			string(form), k[0], k[1], delta)
		if err != nil {
			return q.mapErr(err)
		}
	}
	return nil
}

// recountAnswers sets answer_count from the rows, so it can never drift.
func (q *queries) recountAnswers(ctx context.Context, form model.FormID) error {
	_, err := q.wr.ExecContext(ctx, `
		UPDATE forms SET answer_count = (SELECT count(DISTINCT account_id) FROM form_answers WHERE form_id = ?) WHERE id = ?`,
		string(form), string(form))
	return err
}

func (fs forms) Answer(ctx context.Context, a *model.FormAnswer, replace bool) (*model.FormAnswer, error) {
	var prev *model.FormAnswer
	err := fs.atomic(ctx, func(q *queries) error {
		f, err := forms{q}.Lock(ctx, a.FormID)
		if err != nil {
			return err
		}
		tally := f.Action.Type == model.FormTally
		now := model.Now()
		if replace {
			old, err := scanAnswer(q.wr.QueryRowContext(ctx, `
				SELECT `+answerCols+` FROM form_answers WHERE form_id = ? AND account_id = ?
				ORDER BY created_at DESC, id DESC LIMIT 1`, string(a.FormID), string(a.AccountID)))
			switch {
			case errors.Is(err, sql.ErrNoRows):
			case err != nil:
				return err
			default:
				prev = &old
			}
		}
		if prev != nil {
			if tally {
				if err := q.bump(ctx, f.ID, store.TallyKeys(f.Fields, prev.Answers, prev.ButtonID), -1); err != nil {
					return err
				}
			}
			a.ID, a.CreatedAt = prev.ID, prev.CreatedAt
			if a.UpdatedAt.IsZero() {
				a.UpdatedAt = now
			}
			_, err := q.wr.ExecContext(ctx, `UPDATE form_answers SET button_id = ?, answers = ?, updated_at = ? WHERE id = ?`,
				id(a.ButtonID), jsonObject(a.Answers), int64(a.UpdatedAt), a.ID)
			if err != nil {
				return q.mapErr(err)
			}
		} else {
			if a.ID == "" {
				a.ID = model.NewID[string]()
			}
			if a.CreatedAt.IsZero() {
				a.CreatedAt = now
			}
			_, err := q.wr.ExecContext(ctx, `
				INSERT INTO form_answers (id, form_id, account_id, button_id, answers, result_message_id, created_at)
				VALUES (?, ?, ?, ?, ?, ?, ?)`,
				a.ID, string(a.FormID), string(a.AccountID), id(a.ButtonID), jsonObject(a.Answers), id(a.ResultMessageID),
				int64(a.CreatedAt))
			if err != nil {
				return q.mapErr(err)
			}
			if err := q.recountAnswers(ctx, f.ID); err != nil {
				return err
			}
		}
		if tally {
			return q.bump(ctx, f.ID, store.TallyKeys(f.Fields, a.Answers, a.ButtonID), 1)
		}
		return nil
	})
	return prev, err
}

func (fs forms) Withdraw(ctx context.Context, form model.FormID, account model.AccountID) (bool, error) {
	removed := false
	err := fs.atomic(ctx, func(q *queries) error {
		f, err := forms{q}.Lock(ctx, form)
		if err != nil {
			return err
		}
		rows, err := q.wr.QueryContext(ctx, `SELECT `+answerCols+` FROM form_answers WHERE form_id = ? AND account_id = ?`,
			string(form), string(account))
		old, err := collect(rows, err, scanAnswer)
		if err != nil || len(old) == 0 {
			return err
		}
		removed = true
		if f.Action.Type == model.FormTally {
			for _, a := range old {
				if err := q.bump(ctx, form, store.TallyKeys(f.Fields, a.Answers, a.ButtonID), -1); err != nil {
					return err
				}
			}
		}
		if _, err := q.wr.ExecContext(ctx, `DELETE FROM form_answers WHERE form_id = ? AND account_id = ?`,
			string(form), string(account)); err != nil {
			return err
		}
		return q.recountAnswers(ctx, form)
	})
	return removed, err
}

func (fs forms) AnswerOf(ctx context.Context, form model.FormID, account model.AccountID) (model.FormAnswer, bool, error) {
	a, err := scanAnswer(fs.rd.QueryRowContext(ctx, `
		SELECT `+answerCols+` FROM form_answers WHERE form_id = ? AND account_id = ?
		ORDER BY created_at DESC, id DESC LIMIT 1`, string(form), string(account)))
	if errors.Is(err, sql.ErrNoRows) {
		return a, false, nil
	}
	return a, err == nil, err
}

func (fs forms) AnswersBy(ctx context.Context, formIDs []model.FormID, account model.AccountID) (map[model.FormID]model.FormAnswer, error) {
	out := map[model.FormID]model.FormAnswer{}
	if len(formIDs) == 0 || account == "" {
		return out, nil
	}
	args := append(anys(formIDs), string(account))
	rows, err := fs.rd.QueryContext(ctx, `
		SELECT `+answerCols+` FROM form_answers WHERE form_id IN (`+marks(len(formIDs))+`) AND account_id = ?
		ORDER BY created_at, id`, args...)
	list, err := collect(rows, err, scanAnswer)
	for _, a := range list {
		out[a.FormID] = a // the newest wins: rows come oldest first
	}
	return out, err
}

// answerCursor is the sort key of the last answer of a page.
type answerCursor struct {
	At int64  `json:"a"`
	ID string `json:"i"`
}

func (fs forms) Answers(ctx context.Context, form model.FormID, byID bool, p store.Page) ([]model.FormAnswer, string, error) {
	where, order := "form_id = ?", "created_at, id"
	args := []any{string(form)}
	if byID {
		order = "id"
	}
	if p.Cursor != "" {
		var c answerCursor
		if err := store.DecodeCursor(p.Cursor, &c); err != nil {
			return nil, "", err
		}
		if byID {
			where += " AND id > ?"
			args = append(args, c.ID)
		} else {
			where += " AND (created_at > ? OR (created_at = ? AND id > ?))"
			args = append(args, c.At, c.At, c.ID)
		}
	}
	limit := p.Clamp()
	args = append(args, limit+1)
	rows, err := fs.rd.QueryContext(ctx, `SELECT `+answerCols+` FROM form_answers WHERE `+where+` ORDER BY `+order+` LIMIT ?`, args...)
	out, err := collect(rows, err, scanAnswer)
	if err != nil {
		return nil, "", err
	}
	next := ""
	if len(out) > limit {
		out = out[:limit]
		last := out[limit-1]
		next = store.EncodeCursor(answerCursor{At: int64(last.CreatedAt), ID: last.ID})
		if byID {
			// The cursor is opaque but not secret: it must not carry
			// the time an anonymous answer was given.
			next = store.EncodeCursor(answerCursor{ID: last.ID})
		}
	}
	return out, next, nil
}

func (fs forms) AllAnswers(ctx context.Context, form model.FormID) ([]model.FormAnswer, error) {
	ex := fs.rd
	if fs.inTx {
		ex = fs.wr
	}
	rows, err := ex.QueryContext(ctx, `SELECT `+answerCols+` FROM form_answers WHERE form_id = ? ORDER BY created_at, id`, string(form))
	return collect(rows, err, scanAnswer)
}

func (fs forms) SetResultMessage(ctx context.Context, answerID string, msg model.MessageID) error {
	return fs.changed(fs.wr.ExecContext(ctx, `UPDATE form_answers SET result_message_id = ? WHERE id = ?`, id(msg), answerID))
}

// SetTicketParent writes conversations.parent_id directly. The rules of
// sub-channels belong to the conversations area; this is the one case the
// forms area needs, kept here until that area offers it.
func (fs forms) SetTicketParent(ctx context.Context, ticket, holder model.ConversationID) error {
	_, err := fs.wr.ExecContext(ctx, `
		UPDATE conversations SET parent_id = (
			SELECT COALESCE(h.parent_id, h.id) FROM conversations h
			WHERE h.id = ? AND h.kind = 'channel' AND h.name IS NOT NULL)
		WHERE id = ?`, string(holder), string(ticket))
	return fs.mapErr(err)
}

func (fs forms) TicketParent(ctx context.Context, conv model.ConversationID) (model.ConversationID, error) {
	var parent model.ConversationID
	err := fs.rd.QueryRowContext(ctx, `SELECT parent_id FROM conversations WHERE id = ?`, string(conv)).Scan(str(&parent))
	return parent, fs.mapErr(err)
}

func (fs forms) Tallies(ctx context.Context, formIDs []model.FormID) (map[model.FormID]map[string]map[string]int, error) {
	out := map[model.FormID]map[string]map[string]int{}
	if len(formIDs) == 0 {
		return out, nil
	}
	rows, err := fs.rd.QueryContext(ctx, `
		SELECT form_id, field_id, option_id, count FROM form_tallies
		WHERE form_id IN (`+marks(len(formIDs))+`) AND count > 0`, anys(formIDs)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var form model.FormID
		var field, option string
		var n int
		if err := rows.Scan(str(&form), &field, &option, &n); err != nil {
			return nil, err
		}
		if out[form] == nil {
			out[form] = map[string]map[string]int{}
		}
		if out[form][field] == nil {
			out[form][field] = map[string]int{}
		}
		out[form][field][option] = n
	}
	return out, rows.Err()
}

func (fs forms) Recount(ctx context.Context, form model.FormID) (int, map[string]map[string]int, error) {
	f, err := fs.Get(ctx, form)
	if err != nil {
		return 0, nil, err
	}
	answers, err := fs.AllAnswers(ctx, form)
	if err != nil {
		return 0, nil, err
	}
	accounts := map[model.AccountID]bool{}
	tally := map[string]map[string]int{}
	for _, a := range answers {
		accounts[a.AccountID] = true
		for _, k := range store.TallyKeys(f.Fields, a.Answers, a.ButtonID) {
			if tally[k[0]] == nil {
				tally[k[0]] = map[string]int{}
			}
			tally[k[0]][k[1]]++
		}
	}
	return len(accounts), tally, nil
}
