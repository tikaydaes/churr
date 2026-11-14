package link

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/tikaydaes/churr/internal/chat"
	"github.com/tikaydaes/churr/internal/link/wire"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

// This file is polls across servers (section 19, ruling R12).
//
// A poll belongs to its message, and the message's author has a home
// server. That server keeps every answer and the counts.
//
//   - A member answers on their own server, which checks the answer against
//     the form, keeps it as that member's own answer, and sends
//     form.answered with the answer sealed to the home server. The other
//     servers learn that the account answered and not what.
//   - The home server opens it, holds it to the form's rules, and sends
//     form.tallied with the new counts (and, on a named poll, who chose
//     what), or form.answer_refused.
//   - Every other server shows the counts the home server last sent. It
//     holds rows only for its own members' answers and, on a named poll, a
//     row per voter with exactly what form.tallied published about them, so
//     that readers can be shown who voted for what.

// PollRules is what the link needs of the forms package: the rules a poll's
// home server holds every answer to, a local one or another server's. The
// forms package has them (forms.Shared); the server connects the two.
type PollRules interface {
	// Neutral is a form as an event carries it: what anyone may see.
	Neutral(ctx context.Context, a store.Areas, f store.Form) (model.Form, error)
	// PlacesLeft refuses, with invalid_value, an answer for an option
	// whose places are taken.
	PlacesLeft(ctx context.Context, tx store.Tx, f store.Form, account model.AccountID, answers map[string]any) error
	// NewAnswer makes the row for an answer.
	NewAnswer(f store.Form, account model.AccountID, button string, answers map[string]any) model.FormAnswer
	// Who is the account_id local readers may be told with an answer.
	Who(f store.Form, account model.AccountID) map[string]any
	// CheckRemoteAnswers checks an answer against the form's fields.
	CheckRemoteAnswers(fields []model.FormField, answers map[string]any) (map[string]any, error)
}

// SetForms connects the forms package.
func (s *Service) SetForms(p PollRules) {
	s.mu.Lock()
	s.polls = p
	s.mu.Unlock()
}

func (s *Service) pollRules() PollRules {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.polls
}

// tallyFoldDefault is how long the home server waits after a change before
// it sends the counts, so a burst of answers makes one event.
const tallyFoldDefault = time.Second

// answerPlain is the plaintext of an answer_box.
type answerPlain struct {
	ButtonID     string         `json:"button_id"`
	Answers      map[string]any `json:"answers"`
	AddedOptions []addedOption  `json:"added_options"`
}

// addedOption is an option the answerer added on their own server, where it
// has an identifier of that server's choosing. The home server gives it the
// identifier everyone uses.
type addedOption struct {
	FieldID string `json:"field_id"`
	ID      string `json:"id"`
	Label   string `json:"label"`
}

// formBody is the fields of every form event (section 11).
type formBody struct {
	MessageID     model.MessageID                         `json:"message_id"`
	FormID        model.FormID                            `json:"form_id"`
	AccountID     model.AccountID                         `json:"account_id"`
	AnswerBox     wire.Bytes                              `json:"answer_box"`
	AnswerEventID string                                  `json:"answer_event_id"`
	Reason        string                                  `json:"reason"`
	FieldID       string                                  `json:"field_id"`
	Option        *model.FormOption                       `json:"option"`
	AddedByID     model.AccountID                         `json:"added_by_id"`
	AnswerCount   int                                     `json:"answer_count"`
	Tally         map[string]map[string]int               `json:"tally"`
	Voters        map[string]map[string][]model.AccountID `json:"voters"`
	ClosedByID    model.AccountID                         `json:"closed_by_id"`
	ClosedAt      model.Time                              `json:"closed_at"`
}

// formFromWire reads the form of a message that arrived. Only a poll is
// taken, and what each server works out for itself is dropped.
func formFromWire(raw json.RawMessage) (*store.Form, *rejection) {
	if raw == nil {
		return nil, nil
	}
	var f model.Form
	if json.Unmarshal(raw, &f) != nil || !model.ValidID(string(f.ID)) || f.Action.Type != model.FormTally || f.AnswererGroupID != "" ||
		len(f.Fields) > 50 || len(f.Buttons) > 50 {
		return nil, reject("form_not_a_poll")
	}
	f.Tally, f.Voters, f.MyAnswer, f.AnswerCount = nil, nil, nil, 0
	if f.Results == "" {
		f.Results = model.ResultsLive // the rule of a poll that names none
	}
	return &store.Form{Form: f}, nil
}

// formHome returns the server that keeps the answers of the form on a
// message: the home server of the message's author.
func (s *Service) formHome(ctx context.Context, a store.Areas, m model.Message) (home store.Server, self bool, err error) {
	acc, err := a.Accounts().Get(ctx, m.AuthorID)
	if err != nil {
		return home, false, err
	}
	if acc.Local() {
		home = s.self
		home.Name, home.BoxKey = s.id.Name, s.keys.BoxPublic()
		return home, true, nil
	}
	home, err = a.Peers().Server(ctx, acc.ServerID)
	return home, false, err
}

// counts is the answer count, the counts per option and, on a named poll,
// who chose what: what form.tallied and form.closed carry.
func (s *Service) counts(ctx context.Context, a store.Areas, f store.Form) (map[string]any, error) {
	tallies, err := a.Forms().Tallies(ctx, []model.FormID{f.ID})
	if err != nil {
		return nil, err
	}
	tally := tallies[f.ID]
	if tally == nil {
		tally = map[string]map[string]int{}
	}
	out := map[string]any{"message_id": f.MessageID, "form_id": f.ID, "answer_count": f.AnswerCount, "tally": tally}
	if f.Anonymous {
		// Never names, to anyone.
		return out, nil
	}
	answers, err := a.Forms().AllAnswers(ctx, f.ID)
	if err != nil {
		return nil, err
	}
	voters := map[string]map[string][]model.AccountID{}
	for _, ans := range answers {
		for _, k := range store.TallyKeys(f.Fields, ans.Answers, ans.ButtonID) {
			if voters[k[0]] == nil {
				voters[k[0]] = map[string][]model.AccountID{}
			}
			voters[k[0]][k[1]] = append(voters[k[0]][k[1]], ans.AccountID)
		}
	}
	out["voters"] = voters
	return out, nil
}

// tallySoon has the counts of a poll this server keeps sent to the stream,
// one event for all the changes of the next second.
func (s *Service) tallySoon(stream model.StreamID, form model.FormID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tallies == nil {
		s.tallies = map[model.FormID]*time.Timer{}
	}
	if s.tallies[form] != nil {
		return
	}
	fold := s.tallyFold
	if fold == 0 {
		fold = tallyFoldDefault
	}
	s.tallies[form] = time.AfterFunc(fold, func() {
		s.mu.Lock()
		delete(s.tallies, form)
		s.mu.Unlock()
		if s.ctx.Err() != nil {
			return
		}
		ctx := s.ctx
		f, err := s.store.Forms().Get(ctx, form)
		if err != nil || !f.ClosedAt.IsZero() {
			return // form.closed carried the last counts
		}
		st, err := s.store.Streams().Get(ctx, stream)
		if err != nil || st.State != store.StreamActive {
			return
		}
		fields, err := s.counts(ctx, s.store, f)
		if err != nil {
			s.log.Warn("link: a poll's counts could not be read", "err", err)
			return
		}
		if err := s.enqueue(ctx, st, evFormTallied, fields); err != nil {
			s.log.Warn("link: a poll's counts could not be sent", "err", err)
		}
	})
}

// later runs something of the link's own after the transaction that is
// applying an event, and off its goroutine: what it submits is numbered
// after this event.
func (s *Service) later(rec *chat.Recorder, fn func(ctx context.Context)) {
	rec.After(func() {
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			fn(s.ctx)
		}()
	})
}

// formToWire turns a local form event into the one that travels. ok is
// false for an event that is not about a form.
func (s *Service) formToWire(ctx context.Context, a store.Areas, st store.Stream, conv model.Conversation, e model.Event) (wireEvent, bool, error) {
	switch e.Type {
	case model.EventFormAnswered, model.EventFormAnswerWithdrawn, model.EventFormOptionAdded, model.EventFormClosed:
	default:
		return wireEvent{}, false, nil
	}
	m, err := a.Messages().Get(ctx, e.MessageID)
	if err != nil {
		return local(), true, nil
	}
	f, found, err := a.Forms().ByMessage(ctx, m.ID)
	if err != nil || !found {
		return local(), true, err
	}
	if f.Action.Type != model.FormTally {
		// A form of another kind that was here before the channel was
		// shared. It takes no more answers; closing it is local.
		if e.Type == model.EventFormClosed {
			return local(), true, nil
		}
		return local(), true, model.ErrConflict.With("This form stopped taking answers when the channel was shared with other servers.")
	}
	home, self, err := s.formHome(ctx, a, m)
	if err != nil {
		return local(), true, err
	}
	actor := chat.ActorOf(ctx)
	fields := map[string]any{"message_id": m.ID, "form_id": f.ID}
	tally := func() { s.tallySoon(st.ID, f.ID) }
	switch e.Type {
	case model.EventFormAnswered:
		if actor == "" || !isLocal(ctx, a, actor) {
			return local(), true, nil
		}
		ans, found, err := a.Forms().AnswerOf(ctx, f.ID, actor)
		if err != nil || !found {
			return local(), true, err
		}
		plain := answerPlain{ButtonID: ans.ButtonID, Answers: ans.Answers, AddedOptions: []addedOption{}}
		if !self {
			for _, field := range f.Fields {
				for _, o := range field.Options {
					if o.AddedByID == actor {
						plain.AddedOptions = append(plain.AddedOptions, addedOption{FieldID: field.ID, ID: o.ID, Label: o.Label})
					}
				}
			}
		}
		box, err := wire.SealTo(s.id.Name, home.Name, home.BoxKey, mustJSON(plain))
		if err != nil {
			return local(), true, err
		}
		fields["account_id"], fields["answer_box"] = actor, wire.Bytes(box)
		w := wireEvent{Type: e.Type, Fields: fields}
		if self {
			w.After = tally
		}
		return w, true, nil
	case model.EventFormAnswerWithdrawn:
		if actor == "" || !isLocal(ctx, a, actor) {
			return local(), true, nil
		}
		fields["account_id"] = actor
		w := wireEvent{Type: e.Type, Fields: fields}
		if self {
			w.After = tally
		}
		return w, true, nil
	case model.EventFormOptionAdded:
		if !self {
			// It goes to the home server with the adder's answer, and
			// comes back from there for everyone.
			return local(), true, nil
		}
		// The option just added: the newest of the adder's.
		var field string
		var option *model.FormOption
		best := -1
		for _, fl := range f.Fields {
			for i, o := range fl.Options {
				var n int
				if o.AddedByID != actor || actor == "" {
					continue
				}
				if _, err := fmt.Sscanf(o.ID, "a%d", &n); err != nil {
					continue
				}
				if n > best {
					best, field, option = n, fl.ID, &fl.Options[i]
				}
			}
		}
		if option == nil {
			return local(), true, nil
		}
		fields["field_id"], fields["option"], fields["added_by_id"] = field, model.FormOption{ID: option.ID, Label: option.Label}, actor
		return wireEvent{Type: e.Type, Fields: fields}, true, nil
	default: // form.closed
		if !self {
			return local(), true, model.ErrForbidden.With("Only the server this poll was posted on can close it.")
		}
		fields, err = s.counts(ctx, a, f)
		if err != nil {
			return local(), true, err
		}
		if f.ClosedBy != "" {
			fields["closed_by_id"] = f.ClosedBy
		}
		fields["closed_at"] = f.ClosedAt
		return wireEvent{Type: e.Type, Fields: fields}, true, nil
	}
}

// applyFormEvent applies a form event. Its caller has checked who may say
// it: an answer comes from the answerer's home server, everything else from
// the home server of the form's message.
func (s *Service) applyFormEvent(a *apply, msg model.MessageID) (*rejection, error) {
	ctx, tx := a.ctx, a.tx
	var b formBody
	if rej := a.decode(&b); rej != nil {
		return rej, nil
	}
	m, rej, err := a.message(msg)
	if rej != nil || err != nil {
		return rej, err
	}
	f, found, err := tx.Forms().ByMessage(ctx, m.ID)
	if err != nil {
		return nil, err
	}
	if !found || m.Deleted() {
		return rejectQuiet("unknown_form"), nil
	}
	if f.ID != b.FormID || f.Action.Type != model.FormTally {
		return reject("invalid"), nil
	}
	_, home, err := s.formHome(ctx, tx, m)
	if err != nil {
		return nil, err
	}
	mine := s.isSelf(a.origin.ServerID)
	forms := s.pollRules()
	if forms == nil {
		return rejectQuiet("forms_off"), nil
	}
	emit := func(typ string, extra map[string]any, only map[model.AccountID]map[string]any) error {
		cur, err := tx.Forms().Get(ctx, f.ID)
		if err != nil {
			return err
		}
		shown, err := forms.Neutral(ctx, tx, cur)
		if err != nil {
			return err
		}
		payload := map[string]any{"message_id": m.ID, "form": shown}
		for k, v := range extra {
			payload[k] = v
		}
		_, err = a.rec.Emit(ctx, model.Event{Type: typ, ConversationID: a.conv.ID, MessageID: m.ID, Payload: payload, Only: only})
		return err
	}
	stream := a.st.ID
	switch a.in.Type {
	case model.EventFormAnswered:
		if !home {
			return nil, nil // someone answered; only the home server learns what
		}
		return s.countAnswer(a, m, f, b, emit)
	case model.EventFormAnswerWithdrawn:
		if !home {
			return nil, nil
		}
		if _, err := tx.Forms().Lock(ctx, f.ID); err != nil {
			return nil, err
		}
		removed, err := tx.Forms().Withdraw(ctx, f.ID, b.AccountID)
		if err != nil || !removed {
			return nil, err
		}
		a.rec.After(func() { s.tallySoon(stream, f.ID) })
		return nil, emit(a.in.Type, forms.Who(f, b.AccountID), nil)
	case evFormRefused:
		if home || !isLocal(ctx, tx, b.AccountID) {
			return nil, nil
		}
		// One of this server's members: their answer was not counted, so
		// it is not shown to them as given.
		removed, err := tx.Forms().Withdraw(ctx, f.ID, b.AccountID)
		if err != nil || !removed {
			return nil, err
		}
		reason := b.Reason
		if reason != "closed" && reason != "full" {
			reason = "invalid"
		}
		return nil, emit(model.EventFormAnswerWithdrawn, nil, map[model.AccountID]map[string]any{b.AccountID: {"refused": reason}})
	case model.EventFormOptionAdded:
		if b.Option == nil || b.Option.ID == "" || !validOptionLabel(b.Option.Label) {
			return reject("invalid"), nil
		}
		if !home {
			if rej, err := s.adoptOption(a, f, b); rej != nil || err != nil {
				return rej, err
			}
		}
		return nil, emit(a.in.Type, nil, nil)
	case evFormTallied:
		if home {
			return nil, nil
		}
		if err := s.adoptCounts(a, f, b); err != nil {
			return nil, err
		}
		return nil, emit(model.EventFormAnswered, nil, nil)
	case model.EventFormClosed:
		if home {
			if !mine || !f.ClosedAt.IsZero() {
				return nil, nil
			}
			// This server's own closing, whose transaction never ran
			// with its number: closed here as it is everywhere else.
		} else if err := s.adoptCounts(a, f, b); err != nil {
			return nil, err
		}
		at := b.ClosedAt
		if at.IsZero() {
			at = a.in.At
		}
		closed, err := tx.Forms().Close(ctx, f.ID, b.ClosedByID, at)
		if err != nil || !closed {
			return nil, err
		}
		var extra map[string]any
		if b.ClosedByID != "" {
			extra = map[string]any{"closed_by_id": b.ClosedByID}
		}
		return nil, emit(a.in.Type, extra, nil)
	}
	return nil, nil
}

func validOptionLabel(label string) bool {
	return label != "" && label == strings.TrimSpace(label) && utf8.RuneCountInString(label) <= 100 && !strings.ContainsAny(label, "\r\n")
}

// countAnswer is the home server taking an answer: step 3 of section 19.
func (s *Service) countAnswer(a *apply, m model.Message, f store.Form, b formBody, emit func(string, map[string]any, map[model.AccountID]map[string]any) error) (*rejection, error) {
	ctx, tx := a.ctx, a.tx
	forms := s.pollRules()
	stream, answerID, account := a.st.ID, a.env.OriginEventID, b.AccountID
	mine := s.isSelf(a.origin.ServerID)
	refuse := func(reason string) (*rejection, error) {
		if mine {
			return nil, nil // refused to the member when they answered
		}
		s.later(a.rec, func(ctx context.Context) {
			if st, err := s.store.Streams().Get(ctx, stream); err == nil {
				s.enqueue(ctx, st, evFormRefused, map[string]any{"message_id": m.ID, "form_id": f.ID, "account_id": account,
					"answer_event_id": answerID, "reason": reason})
			}
		})
		return nil, nil
	}
	plainBytes, err := s.keys.OpenBoxBetween(b.AnswerBox, a.origin.Name, s.id.Name)
	var plain answerPlain
	if err != nil || json.Unmarshal(plainBytes, &plain) != nil || len(plain.AddedOptions) > model.MaxOptionsPerField {
		return refuse("invalid")
	}
	cur, err := tx.Forms().Lock(ctx, f.ID)
	if err != nil {
		return nil, err
	}
	if !cur.ClosedAt.IsZero() || (!cur.ClosesAt.IsZero() && !cur.ClosesAt.After(s.now())) {
		// Numbered after form.closed, or after the time it closes by
		// itself: not counted, by anyone.
		return refuse("closed")
	}
	if mine {
		// One of this server's own members. The transaction that took
		// the answer has stored it, unless it never ran with its number.
		if prev, found, err := tx.Forms().AnswerOf(ctx, f.ID, account); err != nil {
			return nil, err
		} else if found && prev.ButtonID == plain.ButtonID && sameJSON(prev.Answers, plain.Answers) {
			return nil, nil
		}
	}
	// Options the answerer added, under the identifiers this server gives.
	fields := append([]model.FormField(nil), cur.Fields...)
	renamed := map[string]map[string]string{}
	var added []formBody
	for _, o := range plain.AddedOptions {
		at := -1
		for i := range fields {
			if fields[i].ID == o.FieldID && fields[i].Type == "choice" {
				at = i
			}
		}
		if at < 0 || !validOptionLabel(o.Label) || o.ID == "" {
			return refuse("invalid")
		}
		id := ""
		used := map[string]bool{}
		for _, have := range fields[at].Options {
			used[have.ID] = true
			if have.Label == o.Label {
				id = have.ID
			}
		}
		if id == "" {
			if !cur.AllowAddedOptions || len(fields[at].Options) >= model.MaxOptionsPerField {
				return refuse("invalid")
			}
			for n := 1; id == ""; n++ {
				if c := fmt.Sprintf("a%d", n); !used[c] {
					id = c
				}
			}
			opt := model.FormOption{ID: id, Label: o.Label, AddedByID: account}
			fields[at].Options = append(append([]model.FormOption(nil), fields[at].Options...), opt)
			added = append(added, formBody{FieldID: o.FieldID, Option: &model.FormOption{ID: id, Label: o.Label}})
		}
		if renamed[o.FieldID] == nil {
			renamed[o.FieldID] = map[string]string{}
		}
		renamed[o.FieldID][o.ID] = id
	}
	answers := map[string]any{}
	for field, v := range plain.Answers {
		answers[field] = v
		list, isList := v.([]any)
		if names := renamed[field]; isList && names != nil {
			out := make([]any, len(list))
			for i, item := range list {
				out[i] = item
				if id, _ := item.(string); names[id] != "" {
					out[i] = names[id]
				}
			}
			answers[field] = out
		}
	}
	button := plain.ButtonID
	knownButton := button == ""
	for _, have := range cur.Buttons {
		knownButton = knownButton || have.ID == button
	}
	if !knownButton || (button == "" && len(fields) == 0) {
		return refuse("invalid")
	}
	clean, err := forms.CheckRemoteAnswers(fields, answers)
	if err != nil {
		return refuse("invalid")
	}
	cur.Fields = fields
	if err := forms.PlacesLeft(ctx, tx, cur, account, clean); err != nil {
		if model.CodeOf(err) == model.CodeInvalidValue {
			return refuse("full")
		}
		return nil, err
	}
	if len(added) > 0 {
		if err := tx.Forms().SetFields(ctx, cur.ID, fields); err != nil {
			return nil, err
		}
	}
	ans := forms.NewAnswer(cur, account, button, clean)
	if _, err := tx.Forms().Answer(ctx, &ans, true); err != nil {
		return nil, err
	}
	s.later(a.rec, func(ctx context.Context) {
		st, err := s.store.Streams().Get(ctx, stream)
		if err != nil {
			return
		}
		for _, o := range added {
			s.enqueue(ctx, st, model.EventFormOptionAdded, map[string]any{"message_id": m.ID, "form_id": f.ID, "field_id": o.FieldID,
				"option": o.Option, "added_by_id": account})
		}
		s.tallySoon(stream, f.ID)
	})
	return nil, emit(model.EventFormAnswered, forms.Who(cur, account), nil)
}

func sameJSON(x, y any) bool {
	var a, b any
	if json.Unmarshal(mustJSON(x), &a) != nil || json.Unmarshal(mustJSON(y), &b) != nil {
		return false
	}
	return reflect.DeepEqual(a, b)
}

// adoptOption takes an option the home server announced. If this server
// holds the same option under an identifier of its own (one of its members
// added it here first), that one takes the home server's identifier, in the
// form and in the answers held here.
func (s *Service) adoptOption(a *apply, f store.Form, b formBody) (*rejection, error) {
	ctx, tx := a.ctx, a.tx
	cur, err := tx.Forms().Lock(ctx, f.ID)
	if err != nil {
		return nil, err
	}
	at := -1
	for i := range cur.Fields {
		if cur.Fields[i].ID == b.FieldID && cur.Fields[i].Type == "choice" {
			at = i
		}
	}
	if at < 0 {
		return reject("invalid"), nil
	}
	want := model.FormOption{ID: b.Option.ID, Label: b.Option.Label, AddedByID: b.AddedByID}
	used := map[string]bool{}
	for _, o := range cur.Fields[at].Options {
		used[o.ID] = true
	}
	used[want.ID] = true
	fresh := func() string {
		for n := 1; ; n++ {
			if c := fmt.Sprintf("a%d", n); !used[c] {
				used[c] = true
				return c
			}
		}
	}
	// rename maps the identifiers held here to the ones that stand. Done
	// in one pass over the answers, so two options can swap.
	rename := map[string]string{}
	var options []model.FormOption
	placed := false
	for _, o := range cur.Fields[at].Options {
		switch {
		case o.ID == want.ID && o.Label == want.Label:
			o.AddedByID, placed = want.AddedByID, true
		case o.Label == want.Label:
			// The same option under this server's own identifier.
			rename[o.ID] = want.ID
			o, placed = want, true
		case o.ID == want.ID:
			// Another option of this server's making has the identifier
			// the home server gave out. It moves aside until the home
			// server names it too.
			id := fresh()
			rename[o.ID] = id
			o.ID = id
		}
		options = append(options, o)
	}
	if !placed {
		options = append(options, want)
	}
	fields := append([]model.FormField(nil), cur.Fields...)
	fields[at].Options = options
	if err := tx.Forms().SetFields(ctx, cur.ID, fields); err != nil {
		return nil, err
	}
	if len(rename) == 0 {
		return nil, nil
	}
	answers, err := tx.Forms().AllAnswers(ctx, cur.ID)
	if err != nil {
		return nil, err
	}
	tallies, err := tx.Forms().Tallies(ctx, []model.FormID{cur.ID})
	if err != nil {
		return nil, err
	}
	for _, ans := range answers {
		list, _ := ans.Answers[b.FieldID].([]any)
		changed := false
		for i, item := range list {
			if id, _ := item.(string); rename[id] != "" {
				list[i], changed = rename[id], true
			}
		}
		if changed {
			if _, err := tx.Forms().Answer(ctx, &ans, true); err != nil {
				return nil, err
			}
		}
	}
	// The counts stay what the home server last said.
	return nil, tx.Streams().SetFormTally(ctx, cur.ID, cur.AnswerCount, tallies[cur.ID])
}

// adoptCounts makes a poll's counts here what its home server says they
// are. On a named poll the voters it names get a row each, holding what was
// published about them and nothing else, and voters it no longer names lose
// theirs. This server's own members keep the rows of their own answers.
func (s *Service) adoptCounts(a *apply, f store.Form, b formBody) error {
	ctx, tx := a.ctx, a.tx
	cur, err := tx.Forms().Lock(ctx, f.ID)
	if err != nil {
		return err
	}
	if b.AnswerCount < 0 {
		b.AnswerCount = 0
	}
	if !cur.Anonymous {
		type vote struct {
			button  string
			answers map[string][]string
		}
		want := map[model.AccountID]*vote{}
		for field, byOption := range b.Voters {
			for option, accounts := range byOption {
				for _, id := range accounts {
					v := want[id]
					if v == nil {
						v = &vote{answers: map[string][]string{}}
						want[id] = v
					}
					if field == model.ButtonField {
						v.button = option
					} else {
						v.answers[field] = append(v.answers[field], option)
					}
				}
			}
		}
		rows, err := tx.Forms().AllAnswers(ctx, cur.ID)
		if err != nil {
			return err
		}
		have := map[model.AccountID]model.FormAnswer{}
		for _, row := range rows {
			if isLocal(ctx, tx, row.AccountID) {
				delete(want, row.AccountID)
				continue
			}
			have[row.AccountID] = row
			if want[row.AccountID] == nil {
				if _, err := tx.Forms().Withdraw(ctx, cur.ID, row.AccountID); err != nil {
					return err
				}
			}
		}
		ids := make([]model.AccountID, 0, len(want))
		for id := range want {
			ids = append(ids, id)
		}
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		for _, id := range ids {
			if acc, err := tx.Accounts().Get(ctx, id); err != nil || acc.Local() {
				continue // an account this server has never been told of
			}
			v := want[id]
			answers := map[string]any{}
			for field, options := range v.answers {
				sort.Strings(options)
				answers[field] = options
			}
			if prev, found := have[id]; found && prev.ButtonID == v.button && sameJSON(prev.Answers, answers) {
				continue
			}
			row := model.FormAnswer{FormID: cur.ID, AccountID: id, ButtonID: v.button, Answers: answers}
			if _, err := tx.Forms().Answer(ctx, &row, true); err != nil {
				return err
			}
		}
	}
	return tx.Streams().SetFormTally(ctx, cur.ID, b.AnswerCount, b.Tally)
}
