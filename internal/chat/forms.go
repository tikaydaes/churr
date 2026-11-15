package chat

import (
	"context"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/perm"
	"github.com/tikaydaes/churr/internal/store"
)

// Forms is what chat needs of the forms package, which sits above it: the
// checks on a form that arrives with a message, and the parts of a form that
// differ per reader. forms.Service implements it and server hands it over
// with SetForms. Without one, a message that carries a form is refused and
// messages are shown without their forms.
type Forms interface {
	// Prepare checks a form the actor of st is posting in st's
	// conversation and returns the row to store (without its message)
	// and the message's fallback text.
	Prepare(ctx context.Context, a store.Areas, st perm.State, in model.FormInput) (store.Form, string, error)
	// Edit applies a form that arrives with an edit of message m, inside
	// the edit's transaction: it replaces the message's form, or gives it
	// one, and returns the new fallback text. model.ErrConflict once
	// anyone has answered.
	Edit(ctx context.Context, tx store.Tx, st perm.State, m model.Message, in model.FormInput) (fallback string, err error)
	// Fill sets Form on every message given that carries one, as the
	// reader may see it. An empty reader is nobody in particular: the
	// form as an event carries it before it is tailored.
	Fill(ctx context.Context, a store.Areas, reader model.AccountID, msgs []model.Message) error
	// Tailor returns the form of an event as one recipient may see it,
	// and false if that is what the event already carries. form is the
	// event's form in plain JSON.
	Tailor(ctx context.Context, e model.Event, form map[string]any, to model.AccountID) (model.Form, bool)
}

// SetForms hands the service the forms package. It is called once while the
// server is being put together.
func (s *Service) SetForms(f Forms) { s.forms = f }

// prepareForm checks a form that arrives with a new message.
func (s *Service) prepareForm(ctx context.Context, st perm.State, in model.FormInput) (*store.Form, string, error) {
	if s.forms == nil {
		return nil, "", model.ErrUnavailable.With("Forms are not available on this server.")
	}
	f, fallback, err := s.forms.Prepare(ctx, s.store, st, in)
	return &f, fallback, err
}

// attachForm stores the form of a message that has just been inserted.
func attachForm(ctx context.Context, tx store.Tx, m model.Message, f *store.Form) error {
	if f == nil {
		return nil
	}
	f.MessageID, f.OwnerID = m.ID, m.AuthorID
	if f.CreatedAt.IsZero() {
		f.CreatedAt = m.CreatedAt
	}
	return tx.Forms().Create(ctx, f)
}

// editForm applies a form that arrives with an edit.
func (s *Service) editForm(ctx context.Context, tx store.Tx, st perm.State, m model.Message, in model.FormInput) (string, error) {
	if s.forms == nil {
		return "", model.ErrUnavailable.With("Forms are not available on this server.")
	}
	return s.forms.Edit(ctx, tx, st, m, in)
}

// fillForms sets the forms of messages for one reader.
func (s *Service) fillForms(ctx context.Context, a store.Areas, reader model.AccountID, msgs []model.Message) error {
	if s.forms == nil || len(msgs) == 0 {
		return nil
	}
	return s.forms.Fill(ctx, a, reader, msgs)
}

// formEvents are the events that carry a form at the top level.
var formEvents = map[string]bool{
	model.EventFormAnswered: true, model.EventFormAnswerWithdrawn: true,
	model.EventFormOptionAdded: true, model.EventFormClosed: true,
}

// tailorFormEvent fills in a form event for one recipient. handled is false
// for any other kind of event.
func (s *Service) tailorFormEvent(ctx context.Context, e model.Event, to model.AccountID) (extra map[string]any, handled bool) {
	if !formEvents[e.Type] {
		return nil, false
	}
	form, _ := e.Payload["form"].(map[string]any)
	if s.forms == nil || form == nil {
		return nil, true
	}
	if f, changed := s.forms.Tailor(ctx, e, form, to); changed {
		return map[string]any{"form": f}, true
	}
	return nil, true
}

// tailorMessageForm fills in the form of a message an event carries for one
// recipient. It returns the message to send, which is a copy when the form
// differs for this recipient: the event is shared between recipients and
// must stay as it is.
func (s *Service) tailorMessageForm(ctx context.Context, e model.Event, msg map[string]any, to model.AccountID) (map[string]any, bool) {
	form, _ := msg["form"].(map[string]any)
	if s.forms == nil || form == nil {
		return msg, false
	}
	f, changed := s.forms.Tailor(ctx, e, form, to)
	if !changed {
		return msg, false
	}
	out := make(map[string]any, len(msg))
	for k, v := range msg {
		out[k] = v
	}
	out["form"] = f
	return out, true
}

// ServerPost is a message the server posts on someone's behalf as the result
// of something they were allowed to do: the answer to a `thread` or
// `channel` form, which becomes a message under the answerer's name.
type ServerPost struct {
	ConversationID model.ConversationID
	AuthorID       model.AccountID
	// ParentID is the top-level message to reply under, or "".
	ParentID model.MessageID
	Text     string
	Cards    []model.Card
	Fallback string
	Via      *model.Via
}

// PostTx stores a message inside the caller's transaction and emits its
// message.created. It asks perm nothing and reads no mentions or checklist
// out of the text: the caller has decided the message may exist (a form's
// answer is posted even for a reader, docs/FEATURES.md section 7.4). The
// usual thread rules still follow: the author follows the thread, and so
// does the parent's author on the first reply.
func (s *Service) PostTx(ctx context.Context, tx store.Tx, rec *Recorder, in ServerPost) (model.Message, error) {
	conv, err := tx.Conversations().Get(ctx, in.ConversationID)
	if err != nil {
		return model.Message{}, err
	}
	var parent model.Message
	if in.ParentID != "" {
		if parent, err = tx.Messages().Get(ctx, in.ParentID); err != nil {
			return model.Message{}, err
		}
		if parent.ConversationID != conv.ID || parent.ParentID != "" {
			return model.Message{}, model.ErrNotFound
		}
	}
	m := model.Message{
		ID: model.NewID[model.MessageID](), ConversationID: conv.ID, AuthorID: in.AuthorID, ParentID: in.ParentID,
		Text: in.Text, Cards: in.Cards, Fallback: in.Fallback, Via: in.Via, CreatedAt: s.now(),
	}
	seq, err := rec.SeqAt(ctx, conv.ID, m.CreatedAt)
	if err != nil {
		return m, err
	}
	m.Seq, m.Order = seq, rec.Order(conv.ID, seq)
	if err := tx.Messages().Insert(ctx, &m, nil); err != nil {
		return m, err
	}
	if m.ParentID != "" {
		follow := func(account model.AccountID, raiseTo int64, evenIfIgnored bool) error {
			mem, found, err := tx.Members().Get(ctx, conv.ID, account)
			if err != nil || !found {
				return err
			}
			ts, changed, err := tx.Threads().Follow(ctx, m.ParentID, account, mem.ReadSeq, raiseTo, evenIfIgnored, m.CreatedAt)
			if err == nil && changed {
				at := s.now()
				rec.After(func() {
					s.hub.Send(account, model.Event{Type: model.EventThreadStateChanged, At: at, ConversationID: conv.ID,
						Payload: map[string]any{"parent_id": ts.ParentID, "thread_state": ts}})
				})
			}
			return err
		}
		if err := follow(m.AuthorID, seq, true); err != nil {
			return m, err
		}
		if parent.ReplyCount == 0 && parent.AuthorID != m.AuthorID {
			if err := follow(parent.AuthorID, 0, false); err != nil {
				return m, err
			}
		}
	}
	out, err := s.neutral(ctx, tx, conv, m)
	if err != nil {
		return m, err
	}
	return out, rec.EmitAt(ctx, model.Event{Type: model.EventMessageCreated, At: m.CreatedAt, ConversationID: conv.ID, Seq: seq,
		MessageID: m.ID, Payload: map[string]any{"message": out}})
}

// Tick sets a checklist item of a message ticked or unticked
// (docs/FEATURES.md section 8). It is not an edit: the message's version and
// edited time stay as they are and nobody is notified. Setting an item to
// the state it already has changes nothing and writes no history.
func (s *Service) Tick(ctx context.Context, actor model.Account, id model.MessageID, itemID string, checked bool) (model.Task, error) {
	_, m, err := s.load(ctx, actor, id, perm.TickOn)
	if err != nil {
		return model.Task{}, err
	}
	var task model.Task
	err = s.Tx(ctx, func(tx store.Tx, rec *Recorder) error {
		// A deleted message keeps its items for history, but they can no
		// longer be ticked: for the asker there is no such item.
		if m.Deleted() {
			return model.ErrNotFound
		}
		items, err := tx.Checklists().Items(ctx, id, false)
		if err != nil {
			return err
		}
		for _, it := range items {
			if it.ItemID != itemID {
				continue
			}
			if it.Checked == checked {
				task = it.Task()
				return nil
			}
			if task, err = tx.Checklists().Tick(ctx, id, itemID, actor.ID, checked, s.now()); err != nil {
				return err
			}
			_, err = rec.Emit(ctx, model.Event{Type: model.EventTaskChanged, ConversationID: m.ConversationID, MessageID: id,
				Payload: map[string]any{"message_id": id, "task": task}})
			return err
		}
		return model.ErrNotFound
	})
	return task, err
}

// TaskHistory returns every tick and untick of one checklist item, oldest
// first, for anyone who can read the message. An item an edit removed still
// has its history.
func (s *Service) TaskHistory(ctx context.Context, actor model.Account, id model.MessageID, itemID string) ([]model.TaskChange, error) {
	if _, _, err := s.load(ctx, actor, id, perm.ReadMessage); err != nil {
		return nil, err
	}
	items, err := s.store.Checklists().Items(ctx, id, true)
	if err != nil {
		return nil, err
	}
	for _, it := range items {
		if it.ItemID == itemID {
			return s.store.Checklists().History(ctx, id, itemID)
		}
	}
	return nil, model.ErrNotFound
}
