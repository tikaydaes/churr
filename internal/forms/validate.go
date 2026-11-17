package forms

import (
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/tikaydaes/churr/internal/model"
)

// The field types of docs/MESSAGE-FORMAT.md section 7.
const (
	typeChoice       = "choice"
	typeText         = "text"
	typeLongText     = "longtext"
	typeNumber       = "number"
	typeDate         = "date"
	typeTime         = "time"
	typeAccount      = "account"
	typeConversation = "conversation"
	typeCheckbox     = "checkbox"
)

var fieldTypes = map[string]bool{
	typeChoice: true, typeText: true, typeLongText: true, typeNumber: true, typeDate: true,
	typeTime: true, typeAccount: true, typeConversation: true, typeCheckbox: true,
}

var presets = map[string]bool{"": true, "poll": true, "rsvp": true, "signup": true, "approve": true, "checkin": true, "ticket": true, "roles": true}

// Limits the documents leave open, chosen so that no part of a form can be
// used to store something large (reported as a decision).
const (
	maxTitle       = 200
	maxLabel       = 200
	maxHelp        = 500
	maxOptionLabel = 100
	// The longest answers a text and a longtext field take when the form
	// does not say (docs/FEATURES.md section 7.2).
	defaultTextLength = 500
	defaultLongLength = 4000
	// MaxPrefix is the longest name prefix of a `channel` form: the
	// channel's name is the prefix, a hyphen and a number.
	MaxPrefix = 32
)

var timeOfDay = regexp.MustCompile(`^([01][0-9]|2[0-3]):[0-5][0-9]$`)

func tooLong(s string, max int) bool { return utf8.RuneCountInString(s) > max }

// validID is the rule for field, option and button ids: 1 to 32 characters
// of [a-z0-9_-], and not the name button presses are tallied under.
func validID(s string) bool { return model.ValidShortID(s) && s != model.ButtonField }

// asNumber reads a JSON number, however it was decoded.
func asNumber(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, !math.IsNaN(n) && !math.IsInf(n, 0)
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	}
	return 0, false
}

func asDate(v any) (string, bool) {
	s, ok := v.(string)
	if !ok {
		return "", false
	}
	_, err := time.Parse("2006-01-02", s)
	return s, err == nil
}

// Validate checks the shape of a form as docs/FEATURES.md section 7.1 lists
// it: counts, identifiers, field types, and which settings go with which
// action. It asks nothing of the database and nothing about who is posting;
// Service.Prepare adds those. dialog is true for a form a bot shows one
// person, which takes no settings at all. now decides whether closes_at is
// still to come.
func Validate(in model.FormInput, dialog bool, now model.Time) error {
	action := in.Action.Type
	switch action {
	case model.FormTally, model.FormCollect, model.FormThread, model.FormChannel, model.FormGroup, model.FormBot:
	default:
		return model.Invalid("form.action.type", "A form's action is tally, collect, thread, channel, group or bot.")
	}
	if !presets[in.Preset] {
		return model.Invalid("form.preset", "That is not a preset this server knows.")
	}
	if tooLong(in.Title, maxTitle) {
		return model.Invalid("form.title", "A form's title is at most 200 characters.")
	}
	if len(in.Fields) > model.MaxFieldsPerForm {
		return model.Invalid("form.fields", "A form has at most 20 fields.")
	}
	if len(in.Buttons) > model.MaxButtonsPerForm {
		return model.Invalid("form.buttons", "A form has at most five buttons.")
	}
	if len(in.Fields) == 0 && len(in.Buttons) == 0 {
		return model.BadRequest("A form needs at least one field or one button.")
	}

	hasChoice := false
	seen := map[string]bool{}
	for i, f := range in.Fields {
		at := fmt.Sprintf("form.fields[%d]", i)
		if !validID(f.ID) {
			return model.Invalid(at+".id", "A field id is 1 to 32 characters of a-z, 0-9, _ and -.")
		}
		if seen[f.ID] {
			return model.Invalid(at+".id", "Two fields have the same id.")
		}
		seen[f.ID] = true
		if !fieldTypes[f.Type] {
			return model.Invalid(at+".type", "That is not a field type.")
		}
		if strings.TrimSpace(f.Label) == "" || tooLong(f.Label, maxLabel) {
			return model.Invalid(at+".label", "Every field needs a label of at most 200 characters.")
		}
		if tooLong(f.Help, maxHelp) || tooLong(f.Placeholder, maxLabel) {
			return model.Invalid(at+".help", "A field's help is at most 500 characters and its placeholder 200.")
		}
		if err := validateField(at, f, in.AllowAddedOptions); err != nil {
			return err
		}
		hasChoice = hasChoice || f.Type == typeChoice
	}
	seen = map[string]bool{}
	for i, b := range in.Buttons {
		at := fmt.Sprintf("form.buttons[%d]", i)
		if !validID(b.ID) {
			return model.Invalid(at+".id", "A button id is 1 to 32 characters of a-z, 0-9, _ and -.")
		}
		if seen[b.ID] {
			return model.Invalid(at+".id", "Two buttons have the same id.")
		}
		seen[b.ID] = true
		if strings.TrimSpace(b.Label) == "" || tooLong(b.Label, maxOptionLabel) {
			return model.Invalid(at+".label", "Every button needs a label of at most 100 characters.")
		}
		switch b.Style {
		case "", "default", "primary", "danger":
		default:
			return model.Invalid(at+".style", "A button's style is default, primary or danger.")
		}
		if action == model.FormGroup {
			if b.GroupAction != "join" && b.GroupAction != "leave" {
				return model.Invalid(at+".group_action", "In a group form every button joins or leaves a group.")
			}
			if !model.ValidID(string(b.GroupID)) {
				return model.Invalid(at+".group_id", "In a group form every button names its group.")
			}
		} else if b.GroupID != "" || b.GroupAction != "" {
			return model.Invalid(at+".group_id", "Only a group form's buttons name a group.")
		}
	}

	// Which settings go with which action.
	if action == model.FormTally && !hasChoice && len(in.Buttons) == 0 {
		return model.BadRequest("A poll needs a choice field or a button to count.")
	}
	if action == model.FormGroup && (len(in.Fields) > 0 || len(in.Buttons) == 0) {
		return model.BadRequest("A group form is a row of buttons and has no fields.")
	}
	switch in.Results {
	case "":
	case model.ResultsLive, model.ResultsAfterAnswer, model.ResultsAfterClose:
		if action != model.FormTally {
			return model.BadRequest("Only a poll has a results setting.")
		}
	default:
		return model.Invalid("form.results", "results is live, after_answer or after_close.")
	}
	if in.Anonymous && action != model.FormTally && action != model.FormCollect {
		return model.BadRequest("Only tally and collect forms can be anonymous.")
	}
	if in.OneAnswer != nil && !*in.OneAnswer && action != model.FormThread && action != model.FormChannel {
		return model.BadRequest("Only thread and channel forms take more than one answer per person.")
	}
	if in.AllowAddedOptions && !hasChoice {
		return model.BadRequest("Options can be added only to a form with a choice field.")
	}
	if !in.ClosesAt.IsZero() && !in.ClosesAt.After(now) {
		return model.Invalid("form.closes_at", "A form closes in the future.")
	}
	if in.AnswererGroupID != "" && !model.ValidID(string(in.AnswererGroupID)) {
		return model.Invalid("form.answerer_group_id", "That is not a group.")
	}
	if action == model.FormChannel {
		if !validPrefix(in.Action.NamePrefix) {
			return model.Invalid("form.action.name_prefix", "The prefix is 1 to 32 characters, with no control characters and no space at either end.")
		}
		if !model.ValidID(string(in.Action.GroupID)) {
			return model.Invalid("form.action.group_id", "A channel form names the group that joins each new channel.")
		}
		if in.Action.Level != "" && !in.Action.Level.Valid() {
			return model.Invalid("form.action.level", "The level is reader, member, moderator or manager.")
		}
	} else if in.Action.NamePrefix != "" || in.Action.GroupID != "" || in.Action.Level != "" {
		return model.Invalid("form.action", "Only a channel form's action has a prefix, a group and a level.")
	}
	if dialog && (in.Anonymous || in.Results != "" || in.AllowAddedOptions || !in.ClosesAt.IsZero() || in.AnswererGroupID != "" || in.OneAnswer != nil) {
		return model.BadRequest("A dialog takes no settings: only a title, fields and buttons.")
	}
	return nil
}

// validPrefix is the rule for a `channel` form's name prefix. Channel names
// are free-form (ruling R51), so any script, capitals and spaces are fine;
// the prefix must still leave `<prefix>-<number>` a name a channel may
// have: no control characters, no space at either end, no run of spaces.
func validPrefix(p string) bool {
	n := utf8.RuneCountInString(p)
	if n < 1 || n > MaxPrefix || !utf8.ValidString(p) || strings.TrimSpace(p) != p || strings.Contains(p, "  ") {
		return false
	}
	for _, r := range p {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

// validateField checks what one field may carry for its type.
func validateField(at string, f model.FormField, addable bool) error {
	if f.Type != typeChoice && len(f.Options) > 0 {
		return model.Invalid(at+".options", "Only a choice field has options.")
	}
	if f.Multiple && f.Type != typeChoice && f.Type != typeAccount && f.Type != typeConversation {
		return model.Invalid(at+".multiple", "Only choice, account and conversation fields take several values.")
	}
	if f.MaxLength != 0 && f.Type != typeText && f.Type != typeLongText {
		return model.Invalid(at+".max_length", "Only text fields have a longest length.")
	}
	if f.Step != 0 && f.Type != typeNumber {
		return model.Invalid(at+".step", "Only a number field has a step.")
	}
	switch f.Type {
	case typeChoice:
		if len(f.Options) > model.MaxOptionsPerField {
			return model.Invalid(at+".options", "A choice field has at most 50 options.")
		}
		if len(f.Options) == 0 && !addable {
			return model.Invalid(at+".options", "A choice field needs at least one option.")
		}
		seen := map[string]bool{}
		for i, o := range f.Options {
			oat := fmt.Sprintf("%s.options[%d]", at, i)
			if !validID(o.ID) || seen[o.ID] {
				return model.Invalid(oat+".id", "An option id is 1 to 32 characters of a-z, 0-9, _ and -, and unique in its field.")
			}
			seen[o.ID] = true
			if strings.TrimSpace(o.Label) == "" || tooLong(o.Label, maxOptionLabel) {
				return model.Invalid(oat+".label", "Every option needs a label of at most 100 characters.")
			}
			if o.Max < 0 {
				return model.Invalid(oat+".max", "The number of places is at least 1.")
			}
			if o.AddedByID != "" {
				return model.Invalid(oat+".added_by_id", "The server records who added an option.")
			}
		}
		lo, hi, err := choiceBounds(f)
		if err != nil || lo < 0 || hi < 0 || (hi > 0 && lo > hi) {
			return model.Invalid(at+".min", "min and max are whole numbers of options, min no more than max.")
		}
	case typeText, typeLongText:
		limit := defaultTextLength
		if f.Type == typeLongText {
			limit = defaultLongLength
		}
		if f.MaxLength < 0 || f.MaxLength > limit {
			return model.Invalid(at+".max_length", fmt.Sprintf("The longest length is between 1 and %d.", limit))
		}
		if f.Min != nil || f.Max != nil {
			return model.Invalid(at+".min", "A text field has no min or max.")
		}
	case typeNumber:
		lo, hasLo := asNumber(f.Min)
		hi, hasHi := asNumber(f.Max)
		if (f.Min != nil && !hasLo) || (f.Max != nil && !hasHi) || (hasLo && hasHi && lo > hi) {
			return model.Invalid(at+".min", "min and max are numbers, min no more than max.")
		}
		if f.Step < 0 || math.IsNaN(f.Step) || math.IsInf(f.Step, 0) {
			return model.Invalid(at+".step", "The step is more than 0.")
		}
	case typeDate:
		lo, hasLo := asDate(f.Min)
		hi, hasHi := asDate(f.Max)
		if (f.Min != nil && !hasLo) || (f.Max != nil && !hasHi) || (hasLo && hasHi && lo > hi) {
			return model.Invalid(at+".min", "min and max are dates written YYYY-MM-DD, min no later than max.")
		}
	default:
		if f.Min != nil || f.Max != nil {
			return model.Invalid(at+".min", "This kind of field has no min or max.")
		}
	}
	return nil
}

// choiceBounds reads a choice field's min and max: how many options an
// answer picks. 0 means not set.
func choiceBounds(f model.FormField) (lo, hi int, err error) {
	read := func(v any) (int, error) {
		if v == nil {
			return 0, nil
		}
		n, ok := asNumber(v)
		if !ok || n != math.Trunc(n) || n < 0 || n > model.MaxOptionsPerField {
			return 0, fmt.Errorf("not a count")
		}
		return int(n), nil
	}
	if lo, err = read(f.Min); err != nil {
		return 0, 0, err
	}
	hi, err = read(f.Max)
	return lo, hi, err
}

// Fallback is the plain text a message with a form carries for search and
// for clients that cannot draw it: the title, each field's label, the
// options of choice fields and the buttons, one per line.
func Fallback(f model.FormInput) string {
	var lines []string
	add := func(s string) {
		if s = strings.TrimSpace(s); s != "" {
			lines = append(lines, s)
		}
	}
	add(f.Title)
	for _, field := range f.Fields {
		add(field.Label)
		for _, o := range field.Options {
			add(o.Label)
		}
	}
	for _, b := range f.Buttons {
		add(b.Label)
	}
	return strings.Join(lines, "\n")
}

// Lookup is what checking an answer needs to know about the answerer: which
// accounts and channels they can see. Service supplies it from perm.
type Lookup interface {
	SeesAccount(id model.AccountID) bool
	SeesChannel(id model.ConversationID) bool
}

// stringList reads a list of strings out of a decoded JSON value.
func stringList(v any) ([]string, bool) {
	switch l := v.(type) {
	case []string:
		return l, true
	case []any:
		out := make([]string, len(l))
		for i, x := range l {
			s, ok := x.(string)
			if !ok {
				return nil, false
			}
			out[i] = s
		}
		return out, true
	}
	return nil, false
}

// CheckAnswers checks an answer against a form's fields (docs/FEATURES.md
// section 7.2 step 4) and returns it with nothing in it but the values of
// fields that were filled in. An error is invalid_value naming the field's
// id. Whether an option with places is already full depends on the other
// answers and is checked where the answer is stored.
func CheckAnswers(fields []model.FormField, answers map[string]any, look Lookup) (map[string]any, error) {
	known := map[string]bool{}
	for _, f := range fields {
		known[f.ID] = true
	}
	for id := range answers {
		if !known[id] {
			return nil, model.Invalid(id, "The form has no such field.")
		}
	}
	out := map[string]any{}
	for _, f := range fields {
		bad := func(sentence string) error { return model.Invalid(f.ID, sentence) }
		v := answers[f.ID]
		// No value: null, an empty string or an empty list.
		if s, ok := v.(string); ok && s == "" {
			v = nil
		}
		if l, ok := stringList(v); ok && len(l) == 0 {
			v = nil
		}
		if v == nil {
			if f.Required {
				return nil, bad("This field needs an answer.")
			}
			continue
		}
		switch f.Type {
		case typeChoice:
			ids, ok := stringList(v)
			if !ok {
				return nil, bad("A choice is a list of option ids.")
			}
			options := map[string]bool{}
			for _, o := range f.Options {
				options[o.ID] = true
			}
			picked := map[string]bool{}
			for _, id := range ids {
				if !options[id] {
					return nil, bad("That is not one of the options.")
				}
				if picked[id] {
					return nil, bad("An option can be picked once.")
				}
				picked[id] = true
			}
			if !f.Multiple && len(ids) > 1 {
				return nil, bad("Pick one option.")
			}
			lo, hi, _ := choiceBounds(f)
			if lo > 0 && len(ids) < lo {
				return nil, bad(fmt.Sprintf("Pick at least %d.", lo))
			}
			if hi > 0 && len(ids) > hi {
				return nil, bad(fmt.Sprintf("Pick at most %d.", hi))
			}
			out[f.ID] = ids
		case typeText, typeLongText:
			s, ok := v.(string)
			if !ok {
				return nil, bad("This field takes text.")
			}
			limit := f.MaxLength
			if limit == 0 {
				limit = defaultTextLength
				if f.Type == typeLongText {
					limit = defaultLongLength
				}
			}
			if f.Type == typeText && strings.ContainsAny(s, "\r\n") {
				return nil, bad("This field takes one line.")
			}
			if tooLong(s, limit) {
				return nil, bad(fmt.Sprintf("At most %d characters.", limit))
			}
			out[f.ID] = s
		case typeNumber:
			n, ok := asNumber(v)
			if !ok {
				return nil, bad("This field takes a number.")
			}
			lo, hasLo := asNumber(f.Min)
			hi, hasHi := asNumber(f.Max)
			if (hasLo && n < lo) || (hasHi && n > hi) {
				return nil, bad("That number is outside the range.")
			}
			if f.Step > 0 {
				// Steps count from min, or from 0 without one.
				steps := (n - lo) / f.Step
				if math.Abs(steps-math.Round(steps)) > 1e-9 {
					return nil, bad("That number is not on a step.")
				}
			}
			out[f.ID] = n
		case typeDate:
			d, ok := asDate(v)
			if !ok {
				return nil, bad("A date is written YYYY-MM-DD.")
			}
			lo, hasLo := asDate(f.Min)
			hi, hasHi := asDate(f.Max)
			if (hasLo && d < lo) || (hasHi && d > hi) {
				return nil, bad("That date is outside the range.")
			}
			out[f.ID] = d
		case typeTime:
			s, ok := v.(string)
			if !ok || !timeOfDay.MatchString(s) {
				return nil, bad("A time is written HH:MM.")
			}
			out[f.ID] = s
		case typeAccount, typeConversation:
			ids, ok := stringList(v)
			if !ok {
				return nil, bad("This field takes a list of ids.")
			}
			if !f.Multiple && len(ids) > 1 {
				return nil, bad("Pick one.")
			}
			if len(ids) > model.MaxOptionsPerField {
				return nil, bad("At most 50.")
			}
			picked := map[string]bool{}
			for _, id := range ids {
				if picked[id] {
					return nil, bad("Each can be picked once.")
				}
				picked[id] = true
				// Something the answerer cannot see and something that
				// does not exist get the same answer.
				seen := model.ValidID(id) && look != nil
				if seen && f.Type == typeAccount {
					seen = look.SeesAccount(model.AccountID(id))
				} else if seen {
					seen = look.SeesChannel(model.ConversationID(id))
				}
				if !seen {
					if f.Type == typeAccount {
						return nil, bad("That is not an account you can pick.")
					}
					return nil, bad("That is not a channel you can pick.")
				}
			}
			out[f.ID] = ids
		case typeCheckbox:
			b, ok := v.(bool)
			if !ok {
				return nil, bad("A tick-box is true or false.")
			}
			if f.Required && !b {
				return nil, bad("This box must be ticked.")
			}
			out[f.ID] = b
		}
	}
	return out, nil
}
