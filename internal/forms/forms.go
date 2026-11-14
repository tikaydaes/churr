// Package forms is forms and polls: what a form may look like, who may
// answer it, and what each action does with an answer (docs/FEATURES.md
// section 7, docs/MESSAGE-FORMAT.md section 7).
//
// A form arrives with the message that carries it, so posting goes through
// chat: Service implements chat.Forms, which chat calls to check a form and
// to fill in the parts of it that differ per reader. Everything done to a
// form afterwards (answering, pressing, adding an option, closing, reading
// the answers) is a method here.
//
// Three things are never sent to anyone but the account concerned:
// `my_answer`, and on a form whose results are not yet shown the tally and
// the voters. On an anonymous form nothing that says who answered what, or
// when, leaves this package at all.
package forms

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/tikaydaes/churr/internal/chat"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/perm"
	"github.com/tikaydaes/churr/internal/store"
)

// Bots is what forms needs of the bots package: delivering an answer or a
// button press on a bot's form or dialog to the bot, as an interaction. It
// returns the interaction's id, or model.ErrUnavailable when the bot has no
// live connection. Until one is set with SetBots, bot forms cannot be
// answered.
type Bots interface {
	FormAnswered(ctx context.Context, sub model.FormSubmission) (interactionID string, err error)
}

// Groups is what forms needs to carry out a `group` form's buttons. The
// people service implements it.
type Groups interface {
	JoinGroup(ctx context.Context, actor model.Account, id model.GroupID) (model.Group, error)
	LeaveGroup(ctx context.Context, actor model.Account, id model.GroupID) (model.Group, error)
}

// Deps is what a Service is built from.
type Deps struct {
	Store  store.Store
	Perm   *perm.Checker
	Chat   *chat.Service
	Groups Groups
	Log    *slog.Logger
}

// Service is the forms service.
type Service struct {
	store  store.Store
	perm   *perm.Checker
	chat   *chat.Service
	groups Groups
	log    *slog.Logger
	now    func() model.Time

	botsMu sync.RWMutex
	bots   Bots

	// snap caches what tailoring one event for many recipients needs, so
	// a vote in a large channel reads the answers once.
	snapMu sync.Mutex
	snap   *snapshot
}

// New makes the service. The caller hands it to chat with chat.SetForms.
func New(d Deps) *Service {
	if d.Log == nil {
		d.Log = slog.Default()
	}
	return &Service{store: d.Store, perm: d.Perm, chat: d.Chat, groups: d.Groups, log: d.Log, now: model.Now}
}

// SetBots hands the service the bots package, which is built after it.
func (s *Service) SetBots(b Bots) {
	s.botsMu.Lock()
	s.bots = b
	s.botsMu.Unlock()
}

func (s *Service) botsImpl() Bots {
	s.botsMu.RLock()
	defer s.botsMu.RUnlock()
	return s.bots
}

// stored reports whether a form keeps answers at all: `bot` and `group`
// forms store nothing.
func stored(action string) bool { return action != model.FormBot && action != model.FormGroup }

// changeable reports whether an answer can be replaced or withdrawn: only
// where it has not already become a message.
func changeable(action string) bool { return action == model.FormTally || action == model.FormCollect }

// fromInput turns a checked input into the row to store.
func fromInput(in model.FormInput) store.Form {
	one := in.OneAnswer == nil || *in.OneAnswer
	f := store.Form{Form: model.Form{
		Preset: in.Preset, Title: in.Title, Fields: in.Fields, Buttons: in.Buttons, Action: in.Action,
		Anonymous: in.Anonymous, Results: in.Results, AllowAddedOptions: in.AllowAddedOptions,
		AnswererGroupID: in.AnswererGroupID, OneAnswer: &one, ClosesAt: in.ClosesAt,
	}}
	if f.Fields == nil {
		f.Fields = []model.FormField{}
	}
	if f.Action.Type == model.FormTally && f.Results == "" {
		f.Results = model.ResultsLive
	}
	if f.Action.Type == model.FormChannel && f.Action.Level == "" {
		f.Action.Level = model.LevelMember
	}
	return f
}

// Prepare checks a form the actor of st is posting in st's conversation:
// who may use the action and the settings there (perm), its shape, and that
// the groups it names exist and, for a `group` form, are open. It implements
// chat.Forms.
func (s *Service) Prepare(ctx context.Context, a store.Areas, st perm.State, in model.FormInput) (store.Form, string, error) {
	if err := perm.CheckPostForm(st, in); err != nil {
		return store.Form{}, "", err
	}
	if err := Validate(in, false, s.now()); err != nil {
		return store.Form{}, "", err
	}
	group := func(field string, id model.GroupID, mustBeOpen bool) error {
		g, err := a.Groups().Get(ctx, id, "")
		if errors.Is(err, model.ErrNotFound) {
			return model.Invalid(field, "There is no such group.")
		}
		if err != nil {
			return err
		}
		if mustBeOpen && !g.Open {
			return model.Invalid(field, "People can join or leave only an open group.")
		}
		return nil
	}
	if in.AnswererGroupID != "" {
		if err := group("form.answerer_group_id", in.AnswererGroupID, false); err != nil {
			return store.Form{}, "", err
		}
	}
	if in.Action.Type == model.FormChannel {
		if err := group("form.action.group_id", in.Action.GroupID, false); err != nil {
			return store.Form{}, "", err
		}
	}
	if in.Action.Type == model.FormGroup {
		for _, b := range in.Buttons {
			if err := group("form.buttons", b.GroupID, true); err != nil {
				return store.Form{}, "", err
			}
		}
	}
	return fromInput(in), Fallback(in), nil
}

// Edit applies a form that arrives with an edit of a message. It implements
// chat.Forms. The form cannot change once anyone has answered; a `bot` form
// holds no answers and can always be replaced, which is how a bot shows the
// next step of a flow in place. Whether a form is anonymous never changes.
func (s *Service) Edit(ctx context.Context, tx store.Tx, st perm.State, m model.Message, in model.FormInput) (string, error) {
	next, fallback, err := s.Prepare(ctx, tx, st, in)
	if err != nil {
		return "", err
	}
	old, found, err := tx.Forms().ByMessage(ctx, m.ID)
	if err != nil {
		return "", err
	}
	if !found {
		next.MessageID, next.OwnerID, next.CreatedAt = m.ID, m.AuthorID, s.now()
		return fallback, tx.Forms().Create(ctx, &next)
	}
	if old, err = tx.Forms().Lock(ctx, old.ID); err != nil {
		return "", err
	}
	if old.AnswerCount > 0 {
		return "", model.ErrConflict.With("This form has been answered and can no longer be changed. Close it, or delete the message.")
	}
	if !old.ClosedAt.IsZero() {
		return "", model.ErrConflict.With("A closed form cannot be changed.")
	}
	if old.Anonymous != next.Anonymous {
		return "", model.Invalid("form.anonymous", "Whether a form is anonymous cannot be changed after posting.")
	}
	next.ID = old.ID
	return fallback, tx.Forms().Replace(ctx, next)
}

// view is everything about one form that deciding what a reader sees needs.
type view struct {
	form  store.Form
	tally map[string]map[string]int
	// answers returns every answer to the form. It is asked only when
	// voters are to be shown.
	answers func() ([]model.FormAnswer, error)
}

// shape builds a form as one reader may see it. mine is the reader's own
// newest answer, or nil; an empty reader is nobody in particular.
func shape(v view, mine *model.FormAnswer) (model.Form, error) {
	f := v.form.Form
	f.Tally, f.Voters, f.MyAnswer = nil, nil, nil
	one := v.form.One()
	f.OneAnswer = &one
	action := f.Action.Type
	if action != model.FormTally {
		f.Results = ""
	}
	if !stored(action) {
		f.AnswerCount = 0
		return f, nil
	}
	if mine != nil && one {
		// The reader's own answer, with the button they pressed beside
		// the fields. It goes to nobody else.
		f.MyAnswer = map[string]any{}
		for k, val := range mine.Answers {
			f.MyAnswer[k] = val
		}
		if mine.ButtonID != "" {
			f.MyAnswer[model.ButtonField] = mine.ButtonID
		}
	}
	if action != model.FormTally || !perm.SeesFormResults(f.Results, !f.ClosedAt.IsZero(), mine != nil) {
		return f, nil
	}
	// Every option is there, chosen or not, so "no votes yet" and "you
	// may not see the votes" look different.
	f.Tally = map[string]map[string]int{}
	for _, field := range f.Fields {
		if field.Type != typeChoice {
			continue
		}
		f.Tally[field.ID] = map[string]int{}
		for _, o := range field.Options {
			f.Tally[field.ID][o.ID] = v.tally[field.ID][o.ID]
		}
	}
	if len(f.Buttons) > 0 {
		f.Tally[model.ButtonField] = map[string]int{}
		for _, b := range f.Buttons {
			f.Tally[model.ButtonField][b.ID] = v.tally[model.ButtonField][b.ID]
		}
	}
	if f.Anonymous {
		// Nobody is told who chose what on an anonymous form: not the
		// author, not a manager, not the owner.
		return f, nil
	}
	answers, err := v.answers()
	if err != nil {
		return f, err
	}
	f.Voters = map[string]map[string][]model.AccountID{}
	for field := range f.Tally {
		f.Voters[field] = map[string][]model.AccountID{}
		for option := range f.Tally[field] {
			f.Voters[field][option] = []model.AccountID{}
		}
	}
	for _, a := range answers {
		for _, k := range store.TallyKeys(f.Fields, a.Answers, a.ButtonID) {
			if byOption, ok := f.Voters[k[0]]; ok {
				if _, known := byOption[k[1]]; known {
					byOption[k[1]] = append(byOption[k[1]], a.AccountID)
				}
			}
		}
	}
	return f, nil
}

// viewOf reads what shape needs for one form through a.
func viewOf(ctx context.Context, a store.Areas, f store.Form) (view, error) {
	v := view{form: f, answers: func() ([]model.FormAnswer, error) { return a.Forms().AllAnswers(ctx, f.ID) }}
	if f.Action.Type != model.FormTally {
		return v, nil
	}
	tallies, err := a.Forms().Tallies(ctx, []model.FormID{f.ID})
	v.tally = tallies[f.ID]
	return v, err
}

// neutral is the form as an event carries it before it is tailored: what
// anyone in the conversation may see.
func neutral(ctx context.Context, a store.Areas, f store.Form) (model.Form, error) {
	v, err := viewOf(ctx, a, f)
	if err != nil {
		return model.Form{}, err
	}
	return shape(v, nil)
}

// forReader is the form as one account sees it.
func forReader(ctx context.Context, a store.Areas, f store.Form, reader model.AccountID) (model.Form, error) {
	v, err := viewOf(ctx, a, f)
	if err != nil {
		return model.Form{}, err
	}
	var mine *model.FormAnswer
	if reader != "" && stored(f.Action.Type) {
		ans, found, err := a.Forms().AnswerOf(ctx, f.ID, reader)
		if err != nil {
			return model.Form{}, err
		}
		if found {
			mine = &ans
		}
	}
	return shape(v, mine)
}

// Fill sets Form on every message given that carries one, as the reader may
// see it. It implements chat.Forms. A deleted message shows no form.
func (s *Service) Fill(ctx context.Context, a store.Areas, reader model.AccountID, msgs []model.Message) error {
	ids := make([]model.MessageID, 0, len(msgs))
	for _, m := range msgs {
		if !m.Deleted() {
			ids = append(ids, m.ID)
		}
	}
	rows, err := a.Forms().ForMessages(ctx, ids)
	if err != nil || len(rows) == 0 {
		return err
	}
	formIDs := make([]model.FormID, 0, len(rows))
	for _, f := range rows {
		formIDs = append(formIDs, f.ID)
	}
	tallies, err := a.Forms().Tallies(ctx, formIDs)
	if err != nil {
		return err
	}
	mine, err := a.Forms().AnswersBy(ctx, formIDs, reader)
	if err != nil {
		return err
	}
	for i := range msgs {
		f, ok := rows[msgs[i].ID]
		if !ok || msgs[i].Deleted() {
			continue
		}
		v := view{form: f, tally: tallies[f.ID], answers: func() ([]model.FormAnswer, error) { return a.Forms().AllAnswers(ctx, f.ID) }}
		var own *model.FormAnswer
		if ans, ok := mine[f.ID]; ok {
			own = &ans
		}
		shown, err := shape(v, own)
		if err != nil {
			return err
		}
		msgs[i].Form = &shown
	}
	return nil
}

// snapshot is one form as it stood when an event was first tailored, with
// each account's newest answer.
type snapshot struct {
	conv   model.ConversationID
	seq    int64
	form   model.FormID
	loaded time.Time
	ok     bool
	view   view
	mine   map[model.AccountID]*model.FormAnswer
}

// snapshotAge is how long a snapshot serves: long enough for one fan-out,
// short enough that the same event replayed later is read afresh.
const snapshotAge = 5 * time.Second

// Tailor returns the form of an event as one recipient may see it, and false
// if that is what the event already carries. It implements chat.Forms.
//
// The event carries what anyone may see. A recipient with an answer of their
// own also gets `my_answer`, and the result of a poll that shows it only to
// those who have answered. The form is read as it is now, so an old event
// replayed on catch-up shows the current counts; the events after it bring
// the same.
func (s *Service) Tailor(ctx context.Context, e model.Event, form map[string]any, to model.AccountID) (model.Form, bool) {
	id, _ := form["id"].(string)
	// A form that has just been posted has no answers to add.
	if id == "" || to == "" || e.Type == model.EventMessageCreated {
		return model.Form{}, false
	}
	s.snapMu.Lock()
	defer s.snapMu.Unlock()
	sn := s.snap
	if sn == nil || sn.conv != e.ConversationID || sn.seq != e.Seq || sn.form != model.FormID(id) || time.Since(sn.loaded) > snapshotAge {
		sn = s.load(ctx, e, model.FormID(id))
		s.snap = sn
	}
	if !sn.ok {
		return model.Form{}, false
	}
	mine := sn.mine[to]
	if mine == nil {
		return model.Form{}, false
	}
	f, err := shape(sn.view, mine)
	if err != nil {
		return model.Form{}, false
	}
	return f, true
}

// load reads a form for Tailor. A form that is gone, or one where nobody's
// view differs, gives a snapshot that tailors nothing.
func (s *Service) load(ctx context.Context, e model.Event, id model.FormID) *snapshot {
	sn := &snapshot{conv: e.ConversationID, seq: e.Seq, form: id, loaded: time.Now()}
	f, err := s.store.Forms().Get(ctx, id)
	if err != nil || !stored(f.Action.Type) || !f.One() {
		return sn
	}
	v, err := viewOf(ctx, s.store, f)
	if err != nil {
		return sn
	}
	answers, err := s.store.Forms().AllAnswers(ctx, id)
	if err != nil {
		s.log.Warn("a form's answers could not be read to tailor an event", "err", err)
		return sn
	}
	v.answers = func() ([]model.FormAnswer, error) { return answers, nil }
	sn.view, sn.ok = v, true
	sn.mine = make(map[model.AccountID]*model.FormAnswer, len(answers))
	for i := range answers {
		sn.mine[answers[i].AccountID] = &answers[i] // oldest first, so the newest stays
	}
	return sn
}

// anonymousID makes the identifier of an answer to an anonymous form. An
// ordinary identifier begins with the moment it was made, which beside the
// events of a conversation would say who gave the answer; this one begins
// with the moment the form was made and is random after that, so it says
// nothing and sorts in no telling order.
func anonymousID(formCreated model.Time) string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("forms: no randomness: " + err.Error())
	}
	ms := uint64(formCreated)
	b[0], b[1], b[2], b[3], b[4], b[5] = byte(ms>>40), byte(ms>>32), byte(ms>>24), byte(ms>>16), byte(ms>>8), byte(ms)
	b[6] = b[6]&0x0f | 0x70 // version 7
	b[8] = b[8]&0x3f | 0x80 // the RFC variant
	h := hex.EncodeToString(b[:])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}
