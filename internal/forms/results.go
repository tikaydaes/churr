package forms

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"strconv"
	"strings"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/perm"
	"github.com/tikaydaes/churr/internal/store"
)

// names turns the ids in answers into something a person can read: accounts
// as @username and channels as #name.
type names struct {
	accounts map[string]string
	channels map[string]string
}

// namesFor looks up every account and channel the answers to a form name.
func (s *Service) namesFor(ctx context.Context, fields []model.FormField, answers ...map[string]any) (names, error) {
	n := names{accounts: map[string]string{}, channels: map[string]string{}}
	var accounts []model.AccountID
	var channels []model.ConversationID
	for _, f := range fields {
		for _, a := range answers {
			ids, _ := stringList(a[f.ID])
			for _, id := range ids {
				switch f.Type {
				case typeAccount:
					accounts = append(accounts, model.AccountID(id))
				case typeConversation:
					channels = append(channels, model.ConversationID(id))
				}
			}
		}
	}
	if len(accounts) > 0 {
		list, err := s.store.Accounts().GetMany(ctx, accounts)
		if err != nil {
			return n, err
		}
		for _, a := range list {
			n.accounts[string(a.ID)] = "@" + a.Username
		}
	}
	if len(channels) > 0 {
		list, err := s.store.Conversations().GetMany(ctx, channels)
		if err != nil {
			return n, err
		}
		for _, c := range list {
			if c.Name != "" {
				n.channels[string(c.ID)] = "#" + c.Name
			}
		}
	}
	return n, nil
}

// text writes one field's value as plain text: option labels for a choice,
// names for accounts and channels, Yes or No for a tick-box.
func (n names) text(f model.FormField, v any) string {
	switch f.Type {
	case typeChoice, typeAccount, typeConversation:
		ids, _ := stringList(v)
		parts := make([]string, 0, len(ids))
		for _, id := range ids {
			label := id
			switch f.Type {
			case typeChoice:
				for _, o := range f.Options {
					if o.ID == id {
						label = o.Label
					}
				}
			case typeAccount:
				if name, ok := n.accounts[id]; ok {
					label = name
				}
			case typeConversation:
				label = "a chat"
				if name, ok := n.channels[id]; ok {
					label = name
				}
			}
			parts = append(parts, label)
		}
		return strings.Join(parts, ", ")
	case typeNumber:
		if x, ok := asNumber(v); ok {
			return strconv.FormatFloat(x, 'f', -1, 64)
		}
	case typeCheckbox:
		if b, ok := v.(bool); ok {
			if b {
				return "Yes"
			}
			return "No"
		}
	}
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func buttonLabel(f store.Form, id string) string {
	for _, b := range f.Buttons {
		if b.ID == id {
			return b.Label
		}
	}
	return id
}

// answerCards lays an answer out as the cards of the message a `thread` or
// `channel` form turns it into: the form's labels and the answers, ten
// fields to a card. It also returns the same as plain text, which is the
// message's fallback so the answer can be searched.
func (s *Service) answerCards(ctx context.Context, f store.Form, button string, answers map[string]any) ([]model.Card, string, error) {
	n, err := s.namesFor(ctx, f.Fields, answers)
	if err != nil {
		return nil, "", err
	}
	var rows []model.CardField
	var lines []string
	for _, field := range f.Fields {
		v, ok := answers[field.ID]
		if !ok {
			continue
		}
		text := n.text(field, v)
		rows = append(rows, model.CardField{Label: field.Label, Value: text, Short: field.Type != typeLongText && len(text) <= 40})
		lines = append(lines, field.Label+": "+text)
	}
	cards := []model.Card{{Title: f.Title}}
	for len(rows) > 0 {
		take := len(rows)
		if take > model.MaxFieldsPerCard {
			take = model.MaxFieldsPerCard
		}
		last := &cards[len(cards)-1]
		if len(last.Fields) > 0 {
			cards = append(cards, model.Card{})
			last = &cards[len(cards)-1]
		}
		last.Fields, rows = rows[:take], rows[take:]
	}
	if button != "" {
		label := buttonLabel(f, button)
		cards[len(cards)-1].Footer = label
		lines = append(lines, label)
	}
	// A card must show something even for a form with no title whose
	// answer is one button.
	if cards[0].Title == "" && len(cards[0].Fields) == 0 {
		cards[0].Title = cards[0].Footer
	}
	return cards, strings.Join(lines, "\n"), nil
}

// readable checks that the actor may read the answers to a form and returns
// the form.
func (s *Service) readable(ctx context.Context, actor model.Account, msg model.MessageID) (target, error) {
	t, err := s.find(ctx, actor, msg)
	if err != nil {
		return t, err
	}
	sees := false
	if t.form.Action.Type == model.FormTally {
		_, answered, err := s.store.Forms().AnswerOf(ctx, t.form.ID, actor.ID)
		if err != nil {
			return t, err
		}
		sees = perm.SeesFormResults(t.form.Results, !t.form.ClosedAt.IsZero(), answered)
	}
	return t, perm.CheckReadFormAnswers(t.st, t.msg, t.form.Action.Type, sees)
}

// scrub takes out of an answer to an anonymous form everything that could
// say who gave it: the account, and the times, which beside the events of
// the conversation would say the same. The identifier was made for this
// (anonymousID). The time shown is the form's own.
func scrub(f store.Form, a model.FormAnswer) model.FormAnswer {
	a.AccountID, a.UpdatedAt, a.ResultMessageID = "", 0, ""
	a.CreatedAt = f.CreatedAt
	return a
}

// Results returns a page of the answers to the form on a message: for a
// poll to anyone who may see its result, otherwise to the form's author and
// accounts acting as manager. On an anonymous form the rows carry no
// account and come in an order that says nothing.
func (s *Service) Results(ctx context.Context, actor model.Account, msg model.MessageID, p store.Page) ([]model.FormAnswer, string, error) {
	t, err := s.readable(ctx, actor, msg)
	if err != nil {
		return nil, "", err
	}
	answers, next, err := s.store.Forms().Answers(ctx, t.form.ID, t.form.Anonymous, p)
	if err != nil {
		return nil, "", err
	}
	if t.form.Anonymous {
		for i := range answers {
			answers[i] = scrub(t.form, answers[i])
		}
	}
	return answers, next, nil
}

// cell guards a spreadsheet against text an answerer typed that begins like
// a formula.
func cell(s string) string {
	if s != "" && strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return "'" + s
	}
	return s
}

// CSV returns the answers to the form on a message as a CSV file: one row
// per answer, one column per field, then the button if the form has any,
// and for a named form the answerer and the time. An anonymous form's rows
// have neither and come in an order that says nothing.
func (s *Service) CSV(ctx context.Context, actor model.Account, msg model.MessageID) ([]byte, error) {
	t, err := s.readable(ctx, actor, msg)
	if err != nil {
		return nil, err
	}
	f := t.form
	var all []model.FormAnswer
	for cursor := ""; ; {
		page, next, err := s.store.Forms().Answers(ctx, f.ID, f.Anonymous, store.Page{Limit: model.MaxLimit, Cursor: cursor})
		if err != nil {
			return nil, err
		}
		all = append(all, page...)
		if next == "" {
			break
		}
		cursor = next
	}
	maps := make([]map[string]any, len(all))
	var accounts []model.AccountID
	for i, a := range all {
		maps[i] = a.Answers
		accounts = append(accounts, a.AccountID)
	}
	n, err := s.namesFor(ctx, f.Fields, maps...)
	if err != nil {
		return nil, err
	}
	usernames := map[model.AccountID]string{}
	if !f.Anonymous && len(accounts) > 0 {
		list, err := s.store.Accounts().GetMany(ctx, accounts)
		if err != nil {
			return nil, err
		}
		for _, a := range list {
			usernames[a.ID] = a.Username
		}
	}

	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	var header []string
	for _, field := range f.Fields {
		header = append(header, cell(field.Label))
	}
	if len(f.Buttons) > 0 {
		header = append(header, "Button")
	}
	if !f.Anonymous {
		header = append(header, "Answered by", "Answered at")
	}
	if err := w.Write(header); err != nil {
		return nil, err
	}
	for _, a := range all {
		var row []string
		for _, field := range f.Fields {
			text := ""
			if v, ok := a.Answers[field.ID]; ok {
				text = n.text(field, v)
			}
			// Names the server wrote itself (@username, #channel) are
			// left as they are; what people typed is guarded.
			if field.Type != typeAccount && field.Type != typeConversation {
				text = cell(text)
			}
			row = append(row, text)
		}
		if len(f.Buttons) > 0 {
			label := ""
			if a.ButtonID != "" {
				label = buttonLabel(f, a.ButtonID)
			}
			row = append(row, cell(label))
		}
		if !f.Anonymous {
			at := a.UpdatedAt
			if at.IsZero() {
				at = a.CreatedAt
			}
			row = append(row, usernames[a.AccountID], at.String())
		}
		if err := w.Write(row); err != nil {
			return nil, err
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return nil, fmt.Errorf("writing the answers as CSV: %w", err)
	}
	return buf.Bytes(), nil
}
