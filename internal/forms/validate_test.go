package forms

import (
	"strings"
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

const group = "0193a0d1-0000-7000-8000-000000000001"

func choice(options ...string) model.FormField {
	f := model.FormField{ID: "choice", Type: "choice", Label: "Pick", Required: true}
	for i, o := range options {
		f.Options = append(f.Options, model.FormOption{ID: "o" + string(rune('1'+i)), Label: o})
	}
	return f
}

func poll(change ...func(*model.FormInput)) model.FormInput {
	f := model.FormInput{Preset: "poll", Title: "Lunch?", Fields: []model.FormField{choice("Noodles", "Tacos")},
		Action: model.FormAction{Type: model.FormTally}}
	for _, fn := range change {
		fn(&f)
	}
	return f
}

// The checks of docs/FEATURES.md section 7.1, each with the refusal it names.
func TestValidate(t *testing.T) {
	now := model.Now()
	no := false
	many := func(n int) []model.FormField {
		var out []model.FormField
		for i := 0; i < n; i++ {
			out = append(out, model.FormField{ID: "f" + strings.Repeat("x", i), Type: "text", Label: "Q"})
		}
		return out
	}
	buttons := func(n int) []model.FormButton {
		var out []model.FormButton
		for i := 0; i < n; i++ {
			out = append(out, model.FormButton{ID: "b" + string(rune('1'+i)), Label: "B"})
		}
		return out
	}
	cases := []struct {
		name string
		form model.FormInput
		want model.Code // "" for accepted
	}{
		{"a poll", poll(), ""},
		{"an anonymous poll with results after close", poll(func(f *model.FormInput) { f.Anonymous, f.Results = true, "after_close" }), ""},
		{"approve: buttons and no fields", model.FormInput{Preset: "approve", Title: "Ship it?", Buttons: buttons(2), Action: model.FormAction{Type: "tally"}}, ""},
		{"a collect form with every kind of field", model.FormInput{Action: model.FormAction{Type: "collect"}, Fields: []model.FormField{
			{ID: "a", Type: "text", Label: "A", MaxLength: 80}, {ID: "b", Type: "longtext", Label: "B"},
			{ID: "c", Type: "number", Label: "C", Min: 1.0, Max: 10.0, Step: 0.5}, {ID: "d", Type: "date", Label: "D", Min: "2026-01-01", Max: "2026-12-31"},
			{ID: "e", Type: "time", Label: "E"}, {ID: "f", Type: "account", Label: "F", Multiple: true},
			{ID: "g", Type: "conversation", Label: "G"}, {ID: "h", Type: "checkbox", Label: "H"},
		}}, ""},
		{"a ticket form", model.FormInput{Fields: many(2), OneAnswer: &no, Action: model.FormAction{Type: "channel", NamePrefix: "ticket", GroupID: group, Level: "manager"}}, ""},
		{"a roles form", model.FormInput{Buttons: []model.FormButton{{ID: "j", Label: "Join", GroupID: group, GroupAction: "join"}}, Action: model.FormAction{Type: "group"}}, ""},
		{"a ticket form with a free-form prefix of 32 characters", model.FormInput{Fields: many(2), Action: model.FormAction{Type: "channel", NamePrefix: "Help Desk " + strings.Repeat("é", 22), GroupID: group}}, ""},
		{"a choice field with no options that takes added ones", poll(func(f *model.FormInput) { f.Fields[0].Options, f.AllowAddedOptions = nil, true }), ""},

		{"21 fields", model.FormInput{Fields: many(21), Action: model.FormAction{Type: "collect"}}, model.CodeInvalidValue},
		{"six buttons", model.FormInput{Buttons: buttons(6), Action: model.FormAction{Type: "tally"}}, model.CodeInvalidValue},
		{"51 options", poll(func(f *model.FormInput) {
			for i := 0; i < 49; i++ {
				f.Fields[0].Options = append(f.Fields[0].Options, model.FormOption{ID: "x" + strings.Repeat("y", i%20) + string(rune('a'+i%26)) + string(rune('a'+i/26)), Label: "L"})
			}
		}), model.CodeInvalidValue},
		{"a field id with a capital", poll(func(f *model.FormInput) { f.Fields[0].ID = "Choice" }), model.CodeInvalidValue},
		{"a field called _button", poll(func(f *model.FormInput) { f.Fields[0].ID = "_button" }), model.CodeInvalidValue},
		{"two fields with one id", poll(func(f *model.FormInput) { f.Fields = append(f.Fields, f.Fields[0]) }), model.CodeInvalidValue},
		{"two options with one id", poll(func(f *model.FormInput) { f.Fields[0].Options[1].ID = "o1" }), model.CodeInvalidValue},
		{"two buttons with one id", model.FormInput{Buttons: []model.FormButton{{ID: "a", Label: "A"}, {ID: "a", Label: "B"}}, Action: model.FormAction{Type: "tally"}}, model.CodeInvalidValue},
		{"an unknown field type", poll(func(f *model.FormInput) { f.Fields[0].Type = "slider" }), model.CodeInvalidValue},
		{"a field with no label", poll(func(f *model.FormInput) { f.Fields[0].Label = "  " }), model.CodeInvalidValue},
		{"an option with no label", poll(func(f *model.FormInput) { f.Fields[0].Options[0].Label = "" }), model.CodeInvalidValue},
		{"an option that says who added it", poll(func(f *model.FormInput) { f.Fields[0].Options[0].AddedByID = "x" }), model.CodeInvalidValue},
		{"a slot with fewer than one place", poll(func(f *model.FormInput) { f.Fields[0].Options[0].Max = -1 }), model.CodeInvalidValue},
		{"a choice field with no options", poll(func(f *model.FormInput) { f.Fields[0].Options = nil }), model.CodeInvalidValue},
		{"options on a text field", model.FormInput{Fields: []model.FormField{{ID: "a", Type: "text", Label: "A", Options: []model.FormOption{{ID: "o", Label: "O"}}}}, Action: model.FormAction{Type: "collect"}}, model.CodeInvalidValue},
		{"min above max", model.FormInput{Fields: []model.FormField{{ID: "a", Type: "number", Label: "A", Min: 5.0, Max: 1.0}}, Action: model.FormAction{Type: "collect"}}, model.CodeInvalidValue},
		{"a date range that is not dates", model.FormInput{Fields: []model.FormField{{ID: "a", Type: "date", Label: "A", Min: "tomorrow"}}, Action: model.FormAction{Type: "collect"}}, model.CodeInvalidValue},
		{"a text field longer than a text field can be", model.FormInput{Fields: []model.FormField{{ID: "a", Type: "text", Label: "A", MaxLength: 501}}, Action: model.FormAction{Type: "collect"}}, model.CodeInvalidValue},
		{"an unknown action", poll(func(f *model.FormInput) { f.Action.Type = "email" }), model.CodeInvalidValue},
		{"an unknown preset", poll(func(f *model.FormInput) { f.Preset = "quiz" }), model.CodeInvalidValue},
		{"an unknown results value", poll(func(f *model.FormInput) { f.Results = "never" }), model.CodeInvalidValue},
		{"a closing time in the past", poll(func(f *model.FormInput) { f.ClosesAt = now.Add(-time.Minute) }), model.CodeInvalidValue},
		{"a channel form with a control character in its prefix", model.FormInput{Fields: many(1), Action: model.FormAction{Type: "channel", NamePrefix: "Help\tDesk", GroupID: group}}, model.CodeInvalidValue},
		{"a channel form whose prefix ends in a space", model.FormInput{Fields: many(1), Action: model.FormAction{Type: "channel", NamePrefix: "Help ", GroupID: group}}, model.CodeInvalidValue},
		{"a channel form with a prefix of 33 characters", model.FormInput{Fields: many(1), Action: model.FormAction{Type: "channel", NamePrefix: strings.Repeat("é", 33), GroupID: group}}, model.CodeInvalidValue},
		{"a channel form with an empty prefix", model.FormInput{Fields: many(1), Action: model.FormAction{Type: "channel", GroupID: group}}, model.CodeInvalidValue},
		{"a channel form with no group", model.FormInput{Fields: many(1), Action: model.FormAction{Type: "channel", NamePrefix: "ticket"}}, model.CodeInvalidValue},
		{"a channel form with a bad level", model.FormInput{Fields: many(1), Action: model.FormAction{Type: "channel", NamePrefix: "ticket", GroupID: group, Level: "boss"}}, model.CodeInvalidValue},
		{"a prefix on a poll", poll(func(f *model.FormInput) { f.Action.NamePrefix = "ticket" }), model.CodeInvalidValue},
		{"a group form whose button names no group", model.FormInput{Buttons: []model.FormButton{{ID: "j", Label: "Join", GroupAction: "join"}}, Action: model.FormAction{Type: "group"}}, model.CodeInvalidValue},
		{"a group form whose button has no action", model.FormInput{Buttons: []model.FormButton{{ID: "j", Label: "Join", GroupID: group}}, Action: model.FormAction{Type: "group"}}, model.CodeInvalidValue},
		{"a group on a poll's button", model.FormInput{Buttons: []model.FormButton{{ID: "j", Label: "Join", GroupID: group, GroupAction: "join"}}, Action: model.FormAction{Type: "tally"}}, model.CodeInvalidValue},

		{"no fields and no buttons", model.FormInput{Action: model.FormAction{Type: "collect"}}, model.CodeInvalidRequest},
		{"a poll with nothing to count", model.FormInput{Fields: many(1), Action: model.FormAction{Type: "tally"}}, model.CodeInvalidRequest},
		{"results on a collect form", model.FormInput{Fields: many(1), Results: "live", Action: model.FormAction{Type: "collect"}}, model.CodeInvalidRequest},
		{"an anonymous thread form", model.FormInput{Fields: many(1), Anonymous: true, Action: model.FormAction{Type: "thread"}}, model.CodeInvalidRequest},
		{"an anonymous bot form", model.FormInput{Fields: many(1), Anonymous: true, Action: model.FormAction{Type: "bot"}}, model.CodeInvalidRequest},
		{"many answers on a poll", poll(func(f *model.FormInput) { f.OneAnswer = &no }), model.CodeInvalidRequest},
		{"added options with no choice field", model.FormInput{Fields: many(1), AllowAddedOptions: true, Action: model.FormAction{Type: "collect"}}, model.CodeInvalidRequest},
		{"a group form with a field", model.FormInput{Fields: many(1), Buttons: []model.FormButton{{ID: "j", Label: "Join", GroupID: group, GroupAction: "join"}}, Action: model.FormAction{Type: "group"}}, model.CodeInvalidRequest},
	}
	for _, c := range cases {
		err := Validate(c.form, false, now)
		if got := model.CodeOf(err); got != c.want {
			t.Errorf("%s: got %q (%v), want %q", c.name, got, err, c.want)
		}
	}

	// A dialog takes fields and buttons and nothing else.
	dialog := model.FormInput{Title: "Why?", Fields: many(1), Buttons: buttons(1), Action: model.FormAction{Type: "bot"}}
	if err := Validate(dialog, true, now); err != nil {
		t.Errorf("a dialog: %v", err)
	}
	for name, change := range map[string]func(*model.FormInput){
		"anonymous":      func(f *model.FormInput) { f.Anonymous = true },
		"closes_at":      func(f *model.FormInput) { f.ClosesAt = now.Add(time.Hour) },
		"answerer group": func(f *model.FormInput) { f.AnswererGroupID = group },
	} {
		d := dialog
		change(&d)
		if model.CodeOf(Validate(d, true, now)) != model.CodeInvalidRequest {
			t.Errorf("a dialog with %s was accepted", name)
		}
	}
}

func TestFallback(t *testing.T) {
	f := poll(func(f *model.FormInput) { f.Buttons = []model.FormButton{{ID: "ok", Label: "Done"}} })
	if got, want := Fallback(f), "Lunch?\nPick\nNoodles\nTacos\nDone"; got != want {
		t.Errorf("fallback: got %q, want %q", got, want)
	}
}

// sees is a Lookup that knows a fixed set of ids.
type sees map[string]bool

func (s sees) SeesAccount(id model.AccountID) bool      { return s[string(id)] }
func (s sees) SeesChannel(id model.ConversationID) bool { return s[string(id)] }

// The checks of docs/FEATURES.md section 7.2 step 4.
func TestCheckAnswers(t *testing.T) {
	const seen, hidden = "0193a001-0000-7000-8000-000000000001", "0193a001-0000-7000-8000-000000000002"
	fields := []model.FormField{
		{ID: "one", Type: "choice", Label: "One", Options: []model.FormOption{{ID: "a", Label: "A"}, {ID: "b", Label: "B"}, {ID: "c", Label: "C"}}},
		{ID: "some", Type: "choice", Label: "Some", Multiple: true, Min: 2.0, Max: 2.0, Options: []model.FormOption{{ID: "a", Label: "A"}, {ID: "b", Label: "B"}, {ID: "c", Label: "C"}}},
		{ID: "line", Type: "text", Label: "Line", MaxLength: 5},
		{ID: "long", Type: "longtext", Label: "Long"},
		{ID: "n", Type: "number", Label: "N", Min: 1.0, Max: 3.0, Step: 0.5},
		{ID: "day", Type: "date", Label: "Day", Min: "2026-11-01", Max: "2026-11-30"},
		{ID: "at", Type: "time", Label: "At"},
		{ID: "who", Type: "account", Label: "Who"},
		{ID: "where", Type: "conversation", Label: "Where", Multiple: true},
		{ID: "ok", Type: "checkbox", Label: "OK"},
	}
	look := sees{seen: true}
	good := map[string]any{
		"one": []any{"b"}, "some": []any{"a", "c"}, "line": "hello", "long": "two\nlines", "n": 2.5,
		"day": "2026-11-13", "at": "09:30", "who": []any{seen}, "where": []any{seen}, "ok": false,
	}
	clean, err := CheckAnswers(fields, good, look)
	if err != nil {
		t.Fatalf("a good answer was refused: %v", err)
	}
	if len(clean) != len(good) {
		t.Fatalf("clean answer has %d values, want %d", len(clean), len(good))
	}
	// Nothing is required, so nothing is fine too, and empty values are
	// the same as none.
	clean, err = CheckAnswers(fields, map[string]any{"line": "", "one": []any{}, "long": nil}, look)
	if err != nil || len(clean) != 0 {
		t.Fatalf("an empty answer: %v %v", clean, err)
	}

	bad := map[string]struct {
		field string
		value any
	}{
		"a field the form does not have":     {"nope", "x"},
		"an option the field does not have":  {"one", []any{"z"}},
		"two options where one is allowed":   {"one", []any{"a", "b"}},
		"a choice that is not a list":        {"one", "a"},
		"fewer than min":                     {"some", []any{"a"}},
		"more than max":                      {"some", []any{"a", "b", "c"}},
		"the same option twice":              {"some", []any{"a", "a"}},
		"a line break in a text field":       {"line", "a\nb"},
		"text over max_length":               {"line", "toolong"},
		"longtext over 4,000":                {"long", strings.Repeat("x", 4001)},
		"text that is a number":              {"line", 5.0},
		"a number below min":                 {"n", 0.5},
		"a number above max":                 {"n", 3.5},
		"a number off the step":              {"n", 1.25},
		"a number that is text":              {"n", "2"},
		"a date that is not one":             {"day", "13/11/2026"},
		"a date before min":                  {"day", "2026-10-31"},
		"a date after max":                   {"day", "2026-12-01"},
		"a time that is not one":             {"at", "9:30"},
		"a time past midnight":               {"at", "24:00"},
		"an account the answerer cannot see": {"who", []any{hidden}},
		"an account id that is not one":      {"who", []any{"sam"}},
		"two accounts where one is allowed":  {"who", []any{seen, seen}},
		"a channel the answerer cannot see":  {"where", []any{hidden}},
		"a tick-box that is text":            {"ok", "yes"},
	}
	for name, c := range bad {
		_, err := CheckAnswers(fields, map[string]any{c.field: c.value}, look)
		e, ok := model.AsError(err)
		if !ok || e.Code != model.CodeInvalidValue || e.Field != c.field {
			t.Errorf("%s: got %v, want invalid_value on %s", name, err, c.field)
		}
	}

	// A required field must have a value; for a tick-box that means true.
	required := []model.FormField{{ID: "name", Type: "text", Label: "Name", Required: true}, {ID: "agree", Type: "checkbox", Label: "Agree", Required: true}}
	for _, answers := range []map[string]any{{"agree": true}, {"name": "", "agree": true}, {"name": "Sam"}, {"name": "Sam", "agree": false}} {
		if model.CodeOf(func() error { _, err := CheckAnswers(required, answers, look); return err }()) != model.CodeInvalidValue {
			t.Errorf("%v was accepted though a required field is missing", answers)
		}
	}
	if _, err := CheckAnswers(required, map[string]any{"name": "Sam", "agree": true}, look); err != nil {
		t.Errorf("required fields filled in: %v", err)
	}
}

// What each reader sees of a poll (docs/FEATURES.md section 7.4).
func TestShape(t *testing.T) {
	answers := []model.FormAnswer{
		{AccountID: "ann", Answers: map[string]any{"choice": []any{"o1"}}},
		{AccountID: "bob", Answers: map[string]any{"choice": []any{"o2"}}, ButtonID: "ok"},
	}
	base := func(results string, anonymous, closed bool) view {
		in := poll(func(f *model.FormInput) {
			f.Results, f.Anonymous = results, anonymous
			f.Buttons = []model.FormButton{{ID: "ok", Label: "OK"}}
		})
		f := fromInput(in)
		f.ID, f.AnswerCount = "form", 2
		if closed {
			f.ClosedAt = 5
		}
		return view{form: f, tally: map[string]map[string]int{"choice": {"o1": 1, "o2": 1}, model.ButtonField: {"ok": 1}},
			answers: func() ([]model.FormAnswer, error) { return answers, nil }}
	}
	cases := []struct {
		name               string
		v                  view
		mine               *model.FormAnswer
		tally, voters, own bool
	}{
		{"live, a reader with no answer", base("live", false, false), nil, true, true, false},
		{"live, a reader with an answer", base("live", false, false), &answers[1], true, true, true},
		{"after_answer, no answer", base("after_answer", false, false), nil, false, false, false},
		{"after_answer, with an answer", base("after_answer", false, false), &answers[0], true, true, true},
		{"after_close, open", base("after_close", false, false), &answers[0], false, false, true},
		{"after_close, closed", base("after_close", false, true), nil, true, true, false},
		{"anonymous and live", base("live", true, false), &answers[0], true, false, true},
		{"anonymous and closed", base("after_close", true, true), nil, true, false, false},
	}
	for _, c := range cases {
		f, err := shape(c.v, c.mine)
		if err != nil {
			t.Fatal(err)
		}
		if (f.Tally != nil) != c.tally || (f.Voters != nil) != c.voters || (f.MyAnswer != nil) != c.own {
			t.Errorf("%s: tally %v voters %v my_answer %v", c.name, f.Tally != nil, f.Voters != nil, f.MyAnswer != nil)
		}
		if f.AnswerCount != 2 {
			t.Errorf("%s: answer_count is always there, got %d", c.name, f.AnswerCount)
		}
		if c.voters && (len(f.Voters["choice"]["o1"]) != 1 || f.Voters["choice"]["o1"][0] != "ann" || f.Voters[model.ButtonField]["ok"][0] != "bob") {
			t.Errorf("%s: voters are wrong: %v", c.name, f.Voters)
		}
	}
	f, _ := shape(base("live", false, false), &answers[1])
	if f.MyAnswer[model.ButtonField] != "ok" {
		t.Errorf("my_answer should name the button pressed: %v", f.MyAnswer)
	}
	// A form that stores nothing shows no count, tally or answer.
	bot := view{form: store.Form{Form: model.Form{Action: model.FormAction{Type: model.FormBot}, AnswerCount: 3}}}
	f, _ = shape(bot, &answers[0])
	if f.AnswerCount != 0 || f.Tally != nil || f.MyAnswer != nil {
		t.Errorf("a bot form shows %d answers, tally %v, my_answer %v", f.AnswerCount, f.Tally, f.MyAnswer)
	}
}

func TestAnonymousID(t *testing.T) {
	made := model.Time(1794562200000)
	a, b := anonymousID(made), anonymousID(made)
	if !model.ValidID(a) || a == b {
		t.Fatalf("anonymous ids: %q %q", a, b)
	}
	// Both begin with the form's own time, so neither says when the
	// answer was given.
	if a[:13] != b[:13] || a[14] != '7' {
		t.Errorf("ids should share the form's time and be version 7: %q %q", a, b)
	}
}
