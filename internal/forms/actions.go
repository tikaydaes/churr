package forms

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/tikaydaes/churr/internal/chat"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/perm"
	"github.com/tikaydaes/churr/internal/store"
)

// Result is what answering a form or pressing one of its buttons gives back.
type Result struct {
	// Form is the form as the answerer now sees it.
	Form model.Form
	// ConversationID is the ticket channel a `channel` form opened.
	ConversationID model.ConversationID
	// InteractionID is the interaction a `bot` form started.
	InteractionID string
}

// target is a form on a message, with the asker's state in the message's
// conversation.
type target struct {
	st   perm.State
	msg  model.Message
	form store.Form
}

// find loads a message's form for an actor who may read the message. A
// message the actor may not know exists, and one with no form, are both not
// found.
func (s *Service) find(ctx context.Context, actor model.Account, id model.MessageID) (target, error) {
	var t target
	m, err := s.store.Messages().Get(ctx, id)
	if err != nil {
		return t, err
	}
	st, err := s.perm.Load(ctx, actor, m.ConversationID)
	if err != nil {
		return t, err
	}
	if err := perm.CheckMessage(st, perm.ReadMessage, m); err != nil {
		return t, err
	}
	f, found, err := s.store.Forms().ByMessage(ctx, id)
	if err != nil {
		return t, err
	}
	if !found || m.Deleted() {
		return t, model.ErrNotFound.With("That message has no form.")
	}
	return target{st: st, msg: m, form: f}, nil
}

// lookup answers what an answerer can see, for account and conversation
// fields.
type lookup struct {
	ctx   context.Context
	s     *Service
	actor model.Account
}

func (l lookup) SeesAccount(id model.AccountID) bool {
	target, err := l.s.store.Accounts().Get(l.ctx, id)
	// A removed account cannot be chosen in a form (docs/PERMISSIONS.md
	// section 12).
	if err != nil || target.Removed() {
		return false
	}
	return l.s.perm.CanSeeAccount(l.ctx, l.actor, target) == nil
}

func (l lookup) SeesChannel(id model.ConversationID) bool {
	st, err := l.s.perm.Load(l.ctx, l.actor, id)
	return err == nil && st.Conversation.Kind == model.KindChannel && perm.Check(st, perm.See) == nil
}

// open checks that a form still takes answers. A form whose closing time
// has passed is closed on the spot, with its event, and then refused
// (docs/FEATURES.md section 7.2 step 2).
func (s *Service) open(ctx context.Context, t target) error {
	closed := model.ErrConflict.With("This form is closed.")
	if !t.form.ClosedAt.IsZero() {
		return closed
	}
	if !t.form.ClosesAt.IsZero() && !t.form.ClosesAt.After(s.now()) {
		if err := s.close(ctx, t.msg, t.form.ID, ""); err != nil && !errors.Is(err, model.ErrConflict) {
			return err
		}
		return closed
	}
	return nil
}

// mayAnswer is the checks every way of answering shares: the asker is a
// member who may answer here (perm), the form is open, and the asker is in
// the group the form is limited to, if it names one.
func (s *Service) mayAnswer(ctx context.Context, t target, a perm.Action) error {
	if err := perm.Check(t.st, a); err != nil {
		return err
	}
	if err := s.open(ctx, t); err != nil {
		return err
	}
	if g := t.form.AnswererGroupID; g != "" {
		// If the group has been deleted, nobody can answer.
		in, err := s.store.Groups().IsMember(ctx, g, t.st.Actor.ID)
		if err != nil && !errors.Is(err, model.ErrNotFound) {
			return err
		}
		if !in {
			return model.ErrForbidden.With("Only members of one group can answer this form.")
		}
	}
	return nil
}

// Answer answers the form on a message, or changes the asker's answer.
func (s *Service) Answer(ctx context.Context, actor model.Account, msg model.MessageID, answers map[string]any) (Result, error) {
	return s.submit(ctx, actor, msg, "", answers)
}

// Press presses a button of the form on a message, submitting the form's
// fields with it.
func (s *Service) Press(ctx context.Context, actor model.Account, msg model.MessageID, button string, answers map[string]any) (Result, error) {
	if button == "" {
		return Result{}, model.ErrNotFound
	}
	return s.submit(ctx, actor, msg, button, answers)
}

func (s *Service) submit(ctx context.Context, actor model.Account, msg model.MessageID, buttonID string, answers map[string]any) (Result, error) {
	t, err := s.find(ctx, actor, msg)
	if err != nil {
		return Result{}, err
	}
	if err := s.mayAnswer(ctx, t, perm.AnswerForm); err != nil {
		return Result{}, err
	}
	var button *model.FormButton
	for i := range t.form.Buttons {
		if t.form.Buttons[i].ID == buttonID {
			button = &t.form.Buttons[i]
		}
	}
	if buttonID != "" && button == nil {
		return Result{}, model.ErrNotFound.With("The form has no such button.")
	}
	action := t.form.Action.Type
	if button == nil && (len(t.form.Fields) == 0 || action == model.FormGroup) {
		return Result{}, model.BadRequest("This form is answered by pressing one of its buttons.")
	}
	clean, err := CheckAnswers(t.form.Fields, answers, lookup{ctx, s, actor})
	if err != nil {
		return Result{}, err
	}

	var res Result
	switch action {
	case model.FormGroup:
		err = s.actGroup(ctx, t, *button)
	case model.FormBot:
		res.InteractionID, err = s.actBot(ctx, t, buttonID, clean)
	case model.FormChannel:
		res.ConversationID, err = s.actChannel(ctx, t, buttonID, clean)
	case model.FormThread:
		err = s.actThread(ctx, t, buttonID, clean)
	default:
		err = s.actKeep(ctx, t, buttonID, clean)
	}
	if err != nil {
		return Result{}, err
	}
	res.Form, err = s.show(ctx, t.form.ID, actor.ID)
	return res, err
}

// show reads a form again and shapes it for one reader.
func (s *Service) show(ctx context.Context, id model.FormID, reader model.AccountID) (model.Form, error) {
	f, err := s.store.Forms().Get(ctx, id)
	if err != nil {
		return model.Form{}, err
	}
	return forReader(ctx, s.store, f, reader)
}

// emit sends a form event to the conversation: the form as anyone may see
// it, which chat tailors for each recipient. who is named only where every
// reader may know it.
func (s *Service) emit(ctx context.Context, tx store.Tx, rec *chat.Recorder, eventType string, m model.Message, formID model.FormID, extra map[string]any) error {
	f, err := tx.Forms().Get(ctx, formID)
	if err != nil {
		return err
	}
	shown, err := neutral(ctx, tx, f)
	if err != nil {
		return err
	}
	payload := map[string]any{"message_id": m.ID, "form": shown}
	for k, v := range extra {
		payload[k] = v
	}
	_, err = rec.Emit(ctx, model.Event{Type: eventType, ConversationID: m.ConversationID, MessageID: m.ID, Payload: payload})
	return err
}

// who is the `account_id` of form.answered and form.answer_withdrawn. It is
// there only when the form is named and every reader may see who answered:
// a poll with live results, or a `thread` form, whose answers are replies
// under the answerer's name. On an anonymous form it is never there.
func who(f store.Form, account model.AccountID) map[string]any {
	if f.Anonymous {
		return nil
	}
	switch f.Action.Type {
	case model.FormTally:
		if f.Results != "" && f.Results != model.ResultsLive {
			return nil
		}
	case model.FormThread:
	default:
		return nil
	}
	return map[string]any{"account_id": account}
}

// locked reads the form again inside the transaction, holding its row, and
// checks it is still open: answers to one form are taken one after another.
func locked(ctx context.Context, tx store.Tx, id model.FormID) (store.Form, error) {
	f, err := tx.Forms().Lock(ctx, id)
	if err != nil {
		return f, err
	}
	if !f.ClosedAt.IsZero() {
		return f, model.ErrConflict.With("This form is closed.")
	}
	return f, nil
}

// newAnswer makes the row for an answer. On an anonymous form its
// identifier says nothing about when it was made.
func newAnswer(f store.Form, account model.AccountID, button string, answers map[string]any) model.FormAnswer {
	a := model.FormAnswer{FormID: f.ID, AccountID: account, ButtonID: button, Answers: answers}
	if f.Anonymous {
		a.ID = anonymousID(f.CreatedAt)
	}
	return a
}

// actKeep is the `tally` and `collect` actions: the answer is stored, in
// place of the account's earlier one, with the counts kept in step in the
// same transaction.
func (s *Service) actKeep(ctx context.Context, t target, button string, answers map[string]any) error {
	account := t.st.Actor.ID
	ctx = chat.WithActor(ctx, account) // for a poll shared with other servers
	return s.chat.Tx(ctx, func(tx store.Tx, rec *chat.Recorder) error {
		f, err := locked(ctx, tx, t.form.ID)
		if err != nil {
			return err
		}
		if f.Action.Type == model.FormTally {
			if err := placesLeft(ctx, tx, f, account, answers); err != nil {
				return err
			}
		}
		a := newAnswer(f, account, button, answers)
		if _, err := tx.Forms().Answer(ctx, &a, true); err != nil {
			return err
		}
		return s.emit(ctx, tx, rec, model.EventFormAnswered, t.msg, f.ID, who(f, account))
	})
}

// placesLeft refuses an answer that picks an option whose places are all
// taken by other accounts (the sign-up preset).
func placesLeft(ctx context.Context, tx store.Tx, f store.Form, account model.AccountID, answers map[string]any) error {
	limited := false
	for _, field := range f.Fields {
		for _, o := range field.Options {
			limited = limited || o.Max > 0
		}
	}
	if !limited {
		return nil
	}
	tallies, err := tx.Forms().Tallies(ctx, []model.FormID{f.ID})
	if err != nil {
		return err
	}
	// The account's own earlier answer does not count against it.
	own := map[[2]string]int{}
	if prev, found, err := tx.Forms().AnswerOf(ctx, f.ID, account); err != nil {
		return err
	} else if found {
		for _, k := range store.TallyKeys(f.Fields, prev.Answers, "") {
			own[k]++
		}
	}
	for _, field := range f.Fields {
		if field.Type != typeChoice {
			continue
		}
		picked, _ := stringList(answers[field.ID])
		for _, id := range picked {
			for _, o := range field.Options {
				if o.ID == id && o.Max > 0 && tallies[f.ID][field.ID][id]-own[[2]string{field.ID, id}] >= o.Max {
					return model.Invalid(field.ID, "“"+o.Label+"” is full.")
				}
			}
		}
	}
	return nil
}

// once refuses a second answer to a form that takes one per account and
// whose answers cannot be changed (decision F10).
func once(ctx context.Context, tx store.Tx, f store.Form, account model.AccountID) error {
	if !f.One() {
		return nil
	}
	_, found, err := tx.Forms().AnswerOf(ctx, f.ID, account)
	if err != nil {
		return err
	}
	if found {
		return model.ErrAlreadyExists.With("You have already answered this form, and its answers cannot be changed.")
	}
	return nil
}

// actThread is the `thread` action: the answer becomes a reply in the form
// message's thread, under the answerer's name, whatever their level.
func (s *Service) actThread(ctx context.Context, t target, button string, answers map[string]any) error {
	account := t.st.Actor.ID
	cards, fallback, err := s.answerCards(ctx, t.form, button, answers)
	if err != nil {
		return err
	}
	// A form posted as a reply is answered in the thread it is in.
	parent := t.msg.ParentID
	if parent == "" {
		parent = t.msg.ID
	}
	return s.chat.Tx(ctx, func(tx store.Tx, rec *chat.Recorder) error {
		f, err := locked(ctx, tx, t.form.ID)
		if err != nil {
			return err
		}
		if err := once(ctx, tx, f, account); err != nil {
			return err
		}
		a := newAnswer(f, account, button, answers)
		if _, err := tx.Forms().Answer(ctx, &a, false); err != nil {
			return err
		}
		reply, err := s.chat.PostTx(ctx, tx, rec, chat.ServerPost{
			ConversationID: t.msg.ConversationID, AuthorID: account, ParentID: parent, Cards: cards, Fallback: fallback,
			Via: &model.Via{Kind: "command", Name: f.Title},
		})
		if err != nil {
			return err
		}
		if err := tx.Forms().SetResultMessage(ctx, a.ID, reply.ID); err != nil {
			return err
		}
		return s.emit(ctx, tx, rec, model.EventFormAnswered, t.msg, f.ID, who(f, account))
	})
}

// ticketSetting is the setting that holds the next number for a prefix.
func ticketSetting(prefix string) string { return "tickets.next." + prefix }

// actChannel is the `channel` action, a ticket: a private channel named from
// the prefix and a counter, with the answerer and the action's group in it,
// and the answer as its first message. Channel names need not be unique
// (ruling R50), so the counter is all the name needs.
func (s *Service) actChannel(ctx context.Context, t target, button string, answers map[string]any) (model.ConversationID, error) {
	actor := t.st.Actor
	cards, fallback, err := s.answerCards(ctx, t.form, button, answers)
	if err != nil {
		return "", err
	}
	var made model.ConversationID
	run := func() error {
		return s.chat.Tx(ctx, func(tx store.Tx, rec *chat.Recorder) error {
			f, err := locked(ctx, tx, t.form.ID)
			if err != nil {
				return err
			}
			if err := once(ctx, tx, f, actor.ID); err != nil {
				return err
			}
			prefix := f.Action.NamePrefix
			number, err := store.SettingIntOr(ctx, tx, ticketSetting(prefix), 1)
			if err != nil {
				return err
			}
			if err := tx.Settings().Set(ctx, ticketSetting(prefix), number+1, ""); err != nil {
				return err
			}
			now := s.now()
			c := model.Conversation{Kind: model.KindChannel, Name: prefix + "-" + strconv.FormatInt(number, 10), Private: true,
				DefaultLevel: model.LevelMember, TicketNumber: int(number), CreatedByID: actor.ID, CreatedAt: now}
			// A ticket is a sub-channel of the channel holding the form,
			// or its sibling if that is itself a sub-channel (R49). The
			// rule is the conversations area's: chat.SubChannelParent.
			holder, err := tx.Conversations().Get(ctx, t.msg.ConversationID)
			if err != nil {
				return err
			}
			c.ParentID = chat.SubChannelParent(holder)
			rec.Founding()
			if err := tx.Conversations().Create(ctx, &c); err != nil {
				return err
			}
			// The group comes in through channel_groups, so its members
			// come and go with it. It comes in before the answerer: the
			// store makes the longest-standing member a manager when a
			// channel has none, and that should be the answerer only if
			// the group brings nobody. A group with nobody in it comes in
			// after the answerer instead: the store deletes a private
			// channel that is left with no members, and the group alone
			// would leave this one so.
			inGroup, err := tx.Groups().MemberIDs(ctx, f.Action.GroupID)
			if err != nil {
				return err
			}
			var joined store.MembershipResult
			join := func() error {
				joined, err = tx.Members().AddDirect(ctx, c.ID, actor.ID, model.LevelMember, now)
				return err
			}
			if len(inGroup) == 0 {
				if err := join(); err != nil {
					return err
				}
			}
			cg := model.ChannelGroup{ConversationID: c.ID, GroupID: f.Action.GroupID, Level: f.Action.Level, AddedByID: f.OwnerID, AddedAt: now}
			brought, _, err := tx.Members().AddGroup(ctx, cg)
			if errors.Is(err, model.ErrNotFound) {
				return model.ErrConflict.With("The group this form opens channels for no longer exists.")
			}
			if err != nil {
				return err
			}
			if len(inGroup) > 0 {
				if err := join(); err != nil {
					return err
				}
			}
			// channel_group.added stands in for one member.joined per
			// person (docs/EVENTS.md): each account the group brought in
			// gets the event with the conversation beside it, as that
			// person is sent it, their own membership included. It is
			// read now, after the answerer is in, so that an answerer the
			// group also brought is told of both ways they are in it.
			// Anything else the group changed is told the usual way.
			added := model.Event{Type: model.EventChannelGroupAdded, ConversationID: c.ID, Payload: map[string]any{"group": cg},
				Only: map[model.AccountID]map[string]any{}}
			var rest store.MembershipResult
			shown, err := tx.Conversations().Get(ctx, c.ID)
			if err != nil {
				return err
			}
			for _, ch := range brought.Changes {
				if ch.Kind != store.MemberJoined {
					rest.Changes = append(rest.Changes, ch)
					continue
				}
				m, found, err := tx.Members().Get(ctx, c.ID, ch.Member.AccountID)
				if err != nil {
					return err
				}
				if !found {
					m = ch.Member
				}
				added.Only[m.AccountID] = map[string]any{"conversation": s.chat.ShownToArrival(ctx, tx, shown, m)}
			}
			if err := rec.Membership(ctx, rest, f.OwnerID, ""); err != nil {
				return err
			}
			if _, err := rec.Emit(ctx, added); err != nil {
				return err
			}
			if err := rec.Membership(ctx, joined, "", ""); err != nil {
				return err
			}
			a := newAnswer(f, actor.ID, button, answers)
			if _, err := tx.Forms().Answer(ctx, &a, false); err != nil {
				return err
			}
			first, err := s.chat.PostTx(ctx, tx, rec, chat.ServerPost{
				ConversationID: c.ID, AuthorID: actor.ID, Cards: cards, Fallback: fallback,
				Via: &model.Via{Kind: "command", Name: f.Title},
			})
			if err != nil {
				return err
			}
			if err := tx.Forms().SetResultMessage(ctx, a.ID, first.ID); err != nil {
				return err
			}
			made = c.ID
			return s.emit(ctx, tx, rec, model.EventFormAnswered, t.msg, f.ID, nil)
		})
	}
	err = run()
	return made, err
}

// actGroup is the `group` action: the button joins or leaves its group.
// Nothing is stored about the press.
func (s *Service) actGroup(ctx context.Context, t target, b model.FormButton) error {
	if s.groups == nil {
		return model.ErrUnavailable.With("Groups are not available on this server.")
	}
	g, err := s.store.Groups().Get(ctx, b.GroupID, "")
	if errors.Is(err, model.ErrNotFound) {
		return model.ErrConflict.With("That group no longer exists.")
	}
	if err != nil {
		return err
	}
	if !g.Open {
		return model.ErrConflict.With("That group is no longer open to join or leave.")
	}
	// Guests and bots are put in groups by an admin, because a group can
	// carry channel memberships. The form shows them the button, so the
	// answer is a plain refusal and not "no such group".
	set, err := s.perm.Settings(ctx)
	if err != nil {
		return err
	}
	if err := perm.CheckServer(t.st.Actor, perm.JoinOpenGroup, set); err != nil {
		return err
	}
	if b.GroupAction == "leave" {
		_, err = s.groups.LeaveGroup(ctx, t.st.Actor, b.GroupID)
	} else {
		_, err = s.groups.JoinGroup(ctx, t.st.Actor, b.GroupID)
	}
	return err
}

// actBot is the `bot` action: the answer goes to the bot that posted the
// form, as an interaction, and is stored nowhere.
func (s *Service) actBot(ctx context.Context, t target, button string, answers map[string]any) (string, error) {
	bots := s.botsImpl()
	if bots == nil {
		return "", model.ErrUnavailable.With("The bot is not connected.")
	}
	return bots.FormAnswered(ctx, model.FormSubmission{
		BotID: t.form.OwnerID, AccountID: t.st.Actor.ID, ConversationID: t.msg.ConversationID, MessageID: t.msg.ID,
		ParentID: t.msg.ParentID, FormID: t.form.ID, ButtonID: button, Answers: answers,
	})
}

// Withdraw removes the asker's answer to a `tally` or `collect` form.
func (s *Service) Withdraw(ctx context.Context, actor model.Account, msg model.MessageID) (model.Form, error) {
	t, err := s.find(ctx, actor, msg)
	if err != nil {
		return model.Form{}, err
	}
	if err := perm.Check(t.st, perm.AnswerForm); err != nil {
		return model.Form{}, err
	}
	if !changeable(t.form.Action.Type) {
		if !stored(t.form.Action.Type) {
			return model.Form{}, model.ErrNotFound.With("This form keeps no answers.")
		}
		return model.Form{}, model.ErrConflict.With("An answer to this form has already become a message and cannot be withdrawn.")
	}
	if err := s.open(ctx, t); err != nil {
		return model.Form{}, err
	}
	ctx = chat.WithActor(ctx, actor.ID) // for a poll shared with other servers
	err = s.chat.Tx(ctx, func(tx store.Tx, rec *chat.Recorder) error {
		f, err := locked(ctx, tx, t.form.ID)
		if err != nil {
			return err
		}
		removed, err := tx.Forms().Withdraw(ctx, f.ID, actor.ID)
		if err != nil {
			return err
		}
		if !removed {
			return model.ErrNotFound.With("You have not answered this form.")
		}
		return s.emit(ctx, tx, rec, model.EventFormAnswerWithdrawn, t.msg, f.ID, who(f, actor.ID))
	})
	if err != nil {
		return model.Form{}, err
	}
	return s.show(ctx, t.form.ID, actor.ID)
}

// AddOption adds an option to a choice field of a form that allows it.
func (s *Service) AddOption(ctx context.Context, actor model.Account, msg model.MessageID, fieldID, label string) (model.Form, error) {
	t, err := s.find(ctx, actor, msg)
	if err != nil {
		return model.Form{}, err
	}
	if err := perm.Check(t.st, perm.AddFormOption); err != nil {
		return model.Form{}, err
	}
	if !t.form.AllowAddedOptions {
		return model.Form{}, model.ErrForbidden.With("This form does not take added options.")
	}
	if err := s.mayAnswer(ctx, t, perm.AddFormOption); err != nil {
		return model.Form{}, err
	}
	label = strings.TrimSpace(label)
	if label == "" || utf8.RuneCountInString(label) > maxOptionLabel || strings.ContainsAny(label, "\r\n") {
		return model.Form{}, model.Invalid("label", "An option is one line of 1 to 100 characters.")
	}
	ctx = chat.WithActor(ctx, actor.ID) // for a poll shared with other servers
	err = s.chat.Tx(ctx, func(tx store.Tx, rec *chat.Recorder) error {
		f, err := locked(ctx, tx, t.form.ID)
		if err != nil {
			return err
		}
		at := -1
		for i, field := range f.Fields {
			if field.ID == fieldID && field.Type == typeChoice {
				at = i
			}
		}
		if at < 0 {
			return model.Invalid("field_id", "The form has no choice field with that id.")
		}
		field := f.Fields[at]
		if len(field.Options) >= model.MaxOptionsPerField {
			return model.ErrConflict.With("This field already has 50 options.")
		}
		used := map[string]bool{}
		for _, o := range field.Options {
			if o.Label == label {
				return model.ErrConflict.With("There is already an option with that label.")
			}
			used[o.ID] = true
		}
		// Added options are a1, a2, …: the first number nothing uses.
		id := ""
		for n := 1; id == ""; n++ {
			if c := fmt.Sprintf("a%d", n); !used[c] {
				id = c
			}
		}
		// The slices are copied: the row read above must not change
		// under anyone still holding it.
		fields := append([]model.FormField(nil), f.Fields...)
		fields[at].Options = append(append([]model.FormOption(nil), field.Options...), model.FormOption{ID: id, Label: label, AddedByID: actor.ID})
		if err := tx.Forms().SetFields(ctx, f.ID, fields); err != nil {
			return err
		}
		return s.emit(ctx, tx, rec, model.EventFormOptionAdded, t.msg, f.ID, nil)
	})
	if err != nil {
		return model.Form{}, err
	}
	return s.show(ctx, t.form.ID, actor.ID)
}

// Close closes the form on a message for good: its author, an account
// acting as manager, or an admin who can read it.
func (s *Service) Close(ctx context.Context, actor model.Account, msg model.MessageID) (model.Form, error) {
	t, err := s.find(ctx, actor, msg)
	if err != nil {
		return model.Form{}, err
	}
	if err := perm.CheckMessage(t.st, perm.CloseOwnForm, t.msg); err != nil {
		return model.Form{}, err
	}
	if err := s.close(ctx, t.msg, t.form.ID, actor.ID); err != nil {
		return model.Form{}, err
	}
	return s.show(ctx, t.form.ID, actor.ID)
}

// close sets a form closed and sends form.closed. by is empty when the form
// closes itself. model.ErrConflict if it is closed already.
func (s *Service) close(ctx context.Context, m model.Message, id model.FormID, by model.AccountID) error {
	return s.chat.Tx(ctx, func(tx store.Tx, rec *chat.Recorder) error {
		closed, err := tx.Forms().Close(ctx, id, by, s.now())
		if err != nil {
			return err
		}
		if !closed {
			return model.ErrConflict.With("This form is already closed.")
		}
		var extra map[string]any
		if by != "" {
			extra = map[string]any{"closed_by_id": by}
		}
		return s.emit(ctx, tx, rec, model.EventFormClosed, m, id, extra)
	})
}

// CloseDue closes every form whose closing time has come and returns how
// many it closed. The scheduler calls it.
func (s *Service) CloseDue(ctx context.Context) (int, error) {
	n := 0
	for {
		due, err := s.store.Forms().DueToClose(ctx, s.now(), 100)
		if err != nil || len(due) == 0 {
			return n, err
		}
		for _, f := range due {
			m, err := s.store.Messages().Get(ctx, f.MessageID)
			if err != nil {
				return n, err
			}
			err = s.close(ctx, m, f.ID, "")
			if err != nil && !errors.Is(err, model.ErrConflict) {
				return n, err
			}
			n++
		}
	}
}

// NextClose returns when the next form closes itself. found is false if
// none will.
func (s *Service) NextClose(ctx context.Context) (model.Time, bool, error) {
	return s.store.Forms().NextClose(ctx)
}
