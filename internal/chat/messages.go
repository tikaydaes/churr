package chat

import (
	"context"
	"errors"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/notify"
	"github.com/tikaydaes/churr/internal/perm"
	"github.com/tikaydaes/churr/internal/store"
	"github.com/tikaydaes/churr/internal/timing"
)

// clientIDWindow is how long a repeated client_id returns the first message.
const clientIDWindow = 24 * time.Hour

// present puts messages in the shape one reader sees: what sits beside each
// filled in, and nothing that is the author's own business.
func (s *Service) present(ctx context.Context, a store.Areas, st perm.State, msgs []model.Message) error {
	if err := a.Messages().Hydrate(ctx, msgs, st.Actor.ID); err != nil {
		return err
	}
	if err := s.fillForms(ctx, a, st.Actor.ID, msgs); err != nil {
		return err
	}
	s.shape(ctx, a, st, msgs)
	return nil
}

// shape is the part of present that reads nothing from the tables beside
// messages.
func (s *Service) shape(ctx context.Context, a store.Areas, st perm.State, msgs []model.Message) {
	s.withUnsent(ctx, a, st.Conversation, msgs)
	for i := range msgs {
		if !st.Conversation.Sealed() {
			msgs[i].ClientID = ""
		} else {
			sealedShape(&msgs[i])
		}
		// While a purge has been asked for, there is nothing to open for
		// anyone but an admin.
		if !msgs[i].HistoryHiddenAt.IsZero() && !st.Actor.Role.IsAdmin() {
			msgs[i].HasHistory = false
		}
	}
}

// neutral is present for nobody in particular: the form an event carries
// before it is tailored to each recipient.
func (s *Service) neutral(ctx context.Context, a store.Areas, conv model.Conversation, m model.Message) (model.Message, error) {
	msgs := []model.Message{m}
	err := s.present(ctx, a, perm.State{Conversation: conv}, msgs)
	return msgs[0], err
}

// neutralNew is neutral for a message stored a moment ago in this
// transaction. bare says it was stored with no file, no checklist item and
// no form. Nothing else can sit beside a message that new (no reaction, pin,
// reply or earlier version), so a bare one is shaped without a read; the
// tables are asked only about a message that has something in them.
func (s *Service) neutralNew(ctx context.Context, a store.Areas, conv model.Conversation, m model.Message, bare bool) (model.Message, error) {
	if !bare || m.Deleted() || m.ReplyCount > 0 {
		return s.neutral(ctx, a, conv, m)
	}
	msgs := []model.Message{m}
	s.shape(ctx, a, perm.State{Conversation: conv}, msgs)
	return msgs[0], nil
}

// NewMessage is what Post takes: the contract's MessageInput.
type NewMessage struct {
	ConversationID model.ConversationID
	ClientID       string
	Text           string
	FileIDs        []model.FileID
	ParentID       model.MessageID
	Quote          *model.Quote
	Cards          []model.Card
	Form           *model.FormInput
	Via            *model.Via
	// Action marks text typed with /me; Chance asks the server to draw a
	// result for /roll, /flip, /pick, /shuffle or /draw (ruling R67).
	Action bool
	Chance *model.ChanceInput
	// Reminder is set by the scheduler for a reminder: it may quote the
	// message it is about at top level, from any conversation, and may
	// then have no text (docs/FEATURES.md section 9.1). The scheduler
	// has checked that its author can read the quoted message.
	Reminder bool
	// Instants is what the posting client says about dates and times in
	// the text: the author's zone and hints (ruling R69, instants.go).
	Instants Instants
	// DryRun makes Post run every check and stop before storing
	// anything: a message scheduled for later is checked as if posted
	// now. The answer is then an empty Posted.
	DryRun bool
}

// Posted is what Post returns.
type Posted struct {
	Message model.Message
	// Created is false when the message already existed under this
	// client_id.
	Created bool
	// CannotRead names accounts the text mentions that cannot read the
	// conversation.
	CannotRead []model.AccountID
	// BroadRefused says the text holds @channel, @here or @everyone that
	// the author may not use here: it was stored as plain text and
	// notified nobody (ruling R64).
	BroadRefused bool
}

func utf16Len(s string) int { return len(utf16.Encode([]rune(s))) }

// Post is the one way a message is created.
func (s *Service) Post(ctx context.Context, actor model.Account, in NewMessage) (Posted, error) {
	// post.checks is everything before the transaction; post.total the
	// whole call, the fan-out included (the timing package).
	began := timing.Now()
	defer func() { timing.Since("post.total", began) }()
	action := perm.Post
	if in.ParentID != "" {
		action = perm.Reply
	}
	st, err := s.perm.Can(ctx, actor, action, in.ConversationID)
	if err != nil {
		return Posted{}, hide(err)
	}
	conv := st.Conversation
	if conv.Sealed() && in.Chance != nil {
		return Posted{}, model.Invalid("chance", "The server cannot write into a sealed chat, so it cannot draw a result there.")
	}
	if conv.Sealed() {
		// Sealed messages are posted by the sealed package, which the
		// API reaches when the body carries `sealed`. Anything else is
		// content a sealed chat cannot hold (docs/PERMISSIONS.md
		// section 17, case 147).
		return Posted{}, model.BadRequest("A sealed chat takes only sealed messages.")
	}
	var form *store.Form
	var fallback string
	if in.Form != nil {
		if form, fallback, err = s.prepareForm(ctx, st, *in.Form); err != nil {
			return Posted{}, err
		}
	}
	if utf8.RuneCountInString(in.Text) > model.MaxMessageText {
		return Posted{}, model.ErrTooLarge.With("A message is at most 16,000 characters.")
	}
	if len(in.FileIDs) > model.MaxFilesPerMessage {
		return Posted{}, model.Invalid("file_ids", "A message carries at most ten files.")
	}
	if len(in.Cards) > 0 {
		if err := perm.Check(st, perm.PostCards); err != nil {
			return Posted{}, err
		}
		if len(in.Cards) > model.MaxCardsPerMessage {
			return Posted{}, model.Invalid("cards", "A message carries at most five cards.")
		}
		for _, c := range in.Cards {
			if c.Title == "" && c.Text == "" && c.ImageFileID == "" {
				return Posted{}, model.Invalid("cards", "A card needs at least a title, text or image.")
			}
			if err := serverCardOnly(c); err != nil {
				return Posted{}, err
			}
		}
	}
	chance, err := checkMarks(actor, in)
	if err != nil {
		return Posted{}, err
	}
	if chance != nil {
		fallback = chance.Fallback()
	}
	if in.Text == "" && len(in.FileIDs) == 0 && len(in.Cards) == 0 && form == nil && chance == nil && !(in.Reminder && in.Quote != nil) {
		return Posted{}, model.BadRequest("A message needs text, a file or a card.")
	}
	if in.ClientID != "" {
		if !model.ValidID(in.ClientID) {
			return Posted{}, model.Invalid("client_id", "client_id must be a UUID.")
		}
		old, found, err := s.store.Messages().ByClientID(ctx, conv.ID, actor.ID, in.ClientID)
		if err != nil {
			return Posted{}, err
		}
		if found {
			if s.now().Sub(old.CreatedAt) > clientIDWindow {
				return Posted{}, model.ErrConflict.With("That client_id was used more than a day ago. Make a new one.")
			}
			msgs := []model.Message{old}
			err := s.present(ctx, s.store, st, msgs)
			return Posted{Message: msgs[0]}, err
		}
	}

	// A reply names any message of the thread; its parent is the thread's
	// top-level message either way (docs/FEATURES.md section 1.1).
	var parent model.Message
	if in.ParentID != "" {
		parent, err = s.store.Messages().Get(ctx, in.ParentID)
		if err != nil || parent.ConversationID != conv.ID {
			return Posted{}, model.ErrNotFound
		}
		if parent.ParentID != "" {
			if parent, err = s.store.Messages().Get(ctx, parent.ParentID); err != nil {
				return Posted{}, err
			}
		}
		if parent.IsSystem() {
			return Posted{}, model.BadRequest("A system notice cannot be replied to.")
		}
		if err := s.threadOpen(ctx, parent); err != nil {
			return Posted{}, err
		}
	}
	if in.Quote != nil && !in.Reminder {
		if in.ParentID == "" {
			return Posted{}, model.Invalid("quote", "Only a reply can quote.")
		}
		quoted, err := s.store.Messages().Get(ctx, in.Quote.MessageID)
		if err != nil || quoted.ConversationID != conv.ID || (quoted.ID != parent.ID && quoted.ParentID != parent.ID) {
			return Posted{}, model.ErrNotFound
		}
		if (in.Quote.Start == nil) != (in.Quote.End == nil) {
			return Posted{}, model.Invalid("quote", "Give both ends of the quoted range, or neither.")
		}
		if in.Quote.Start != nil && (*in.Quote.Start < 0 || *in.Quote.End <= *in.Quote.Start || *in.Quote.End > utf16Len(quoted.Text)) {
			return Posted{}, model.Invalid("quote", "The quoted range must lie inside the quoted text.")
		}
	}
	if s.mod != nil {
		if err := s.mod.Check(ctx, st, in.Text); err != nil {
			return Posted{}, err
		}
	}

	got, err := s.readWith(ctx, s.store, st, in.Text, in.Instants)
	if err != nil {
		return Posted{}, err
	}
	if in.DryRun {
		return Posted{}, nil
	}
	items := checklist(got.entities, got.tasks, nil)
	m := model.Message{
		ID: model.NewID[model.MessageID](), ConversationID: conv.ID, AuthorID: actor.ID, ParentID: parent.ID,
		Quote: in.Quote, ClientID: in.ClientID, Text: in.Text, Entities: got.entities, Cards: in.Cards, Via: in.Via,
		Action: in.Action, Chance: chance, CreatedAt: s.now(),
	}
	m.Fallback = fallback
	var out model.Message
	type follower struct {
		account model.AccountID
		state   model.ThreadState
	}
	var followers []follower
	// What the fan-out would read back of the message is handed to it:
	// the conversation, and the mention rows when they are exactly the
	// ones worked out here (a broad mention's rows are written by the
	// store from the member rows, and are read).
	known := &notify.Known{Conversation: conv, HaveConversation: true}
	if len(got.broad) == 0 {
		known.Mentions, known.HaveMentions = got.mentions, true
	}
	s.fresh.Store(m.ID, known)
	defer s.fresh.Delete(m.ID)
	bare := form == nil && len(items) == 0 && len(in.FileIDs) == 0
	timing.Since("post.checks", began)
	err = s.Tx(ctx, func(tx store.Tx, rec *Recorder) error {
		// Every check is made and everything is ready: only now is the
		// seq taken, which holds the conversation until the commit.
		seq, err := rec.SeqAt(ctx, conv.ID, m.CreatedAt)
		if err != nil {
			return err
		}
		// In a shared conversation the message stands where its event's
		// stamp puts it (shared.go); anywhere else the order follows seq.
		m.Seq, m.Order = seq, rec.Order(conv.ID, seq)
		if err := tx.Messages().Insert(ctx, &m, got.mentions); err != nil {
			return err
		}
		if err := mentionBroadly(ctx, tx, m, got.broad); err != nil {
			return err
		}
		if err := attachForm(ctx, tx, m, form); err != nil {
			return err
		}
		if len(items) > 0 {
			if err := tx.Checklists().Replace(ctx, m.ID, items, m.CreatedAt); err != nil {
				return err
			}
		}
		if len(in.FileIDs) > 0 {
			if err := tx.Files().Attach(ctx, m.ID, conv.ID, actor.ID, in.FileIDs); err != nil {
				return err
			}
		}
		if m.ParentID != "" {
			// Thread states the server changes by itself
			// (docs/FEATURES.md section 1.4).
			follow := func(account model.AccountID, raiseTo int64, evenIfIgnored bool) error {
				var readSeq int64
				if account == actor.ID && st.Member != nil {
					// The author's own row was read with the checks.
					readSeq = st.Member.ReadSeq
				} else {
					mem, found, err := tx.Members().Get(ctx, conv.ID, account)
					if err != nil || !found {
						return err
					}
					readSeq = mem.ReadSeq
				}
				ts, changed, err := tx.Threads().Follow(ctx, m.ParentID, account, readSeq, raiseTo, evenIfIgnored, m.CreatedAt)
				if err == nil && changed {
					followers = append(followers, follower{account, ts})
				}
				return err
			}
			if err := follow(actor.ID, seq, true); err != nil {
				return err
			}
			if parent.ReplyCount == 0 && parent.AuthorID != actor.ID {
				if err := follow(parent.AuthorID, 0, false); err != nil {
					return err
				}
			}
			for _, id := range got.named {
				if err := follow(id, 0, false); err != nil {
					return err
				}
			}
		}
		if err := s.stored(ctx, tx, rec, st, m, false); err != nil {
			return err
		}
		if out, err = s.neutralNew(ctx, tx, conv, m, bare); err != nil {
			return err
		}
		out.Unsent = rec.Unsent(conv.ID, seq)
		e := model.Event{Type: model.EventMessageCreated, At: m.CreatedAt, ConversationID: conv.ID, Seq: seq, MessageID: m.ID,
			Payload: map[string]any{"message": out}}
		if in.ClientID != "" {
			// Only the author's own clients are told the client_id, to
			// match the message to what they show as pending.
			e.Only = map[model.AccountID]map[string]any{actor.ID: {"client_id": in.ClientID}}
		}
		return rec.EmitAt(ctx, e)
	})
	if errors.Is(err, model.ErrAlreadyExists) && in.ClientID != "" {
		// The same request arrived twice at once; the other one won.
		in2 := in
		return s.Post(ctx, actor, in2)
	}
	if err != nil {
		return Posted{}, err
	}
	for _, f := range followers {
		s.hub.Send(f.account, model.Event{Type: model.EventThreadStateChanged, At: s.now(), ConversationID: conv.ID,
			Payload: map[string]any{"parent_id": f.state.ParentID, "thread_state": f.state}})
	}
	s.previewLinks(conv, out, "")
	return Posted{Message: out, Created: true, CannotRead: got.cannotRead, BroadRefused: got.broadRefused}, nil
}

// Messages returns a page of a conversation's messages and the reader's
// thread states for the threads in it.
func (s *Service) Messages(ctx context.Context, actor model.Account, conv model.ConversationID, o store.MessageList) ([]model.Message, []model.ThreadState, bool, error) {
	st, err := s.perm.Can(ctx, actor, perm.Read, conv)
	if err != nil {
		return nil, nil, false, hide(err)
	}
	msgs, more, err := s.store.Messages().List(ctx, conv, o)
	if err != nil {
		return nil, nil, false, err
	}
	if err := s.present(ctx, s.store, st, msgs); err != nil {
		return nil, nil, false, err
	}
	var parents []model.MessageID
	seen := map[model.MessageID]bool{}
	for _, m := range msgs {
		p := m.ParentID
		if p == "" && m.ReplyCount > 0 {
			p = m.ID
		}
		if p != "" && !seen[p] {
			seen[p] = true
			parents = append(parents, p)
		}
	}
	states, err := s.store.Threads().ForParents(ctx, actor.ID, parents)
	return msgs, states, more, err
}

// load reads a message and the reader's state in its conversation, and asks
// perm. A message the reader may not know exists is not found.
func (s *Service) load(ctx context.Context, actor model.Account, id model.MessageID, a perm.MessageAction) (perm.State, model.Message, error) {
	m, err := s.store.Messages().Get(ctx, id)
	if err != nil {
		return perm.State{}, m, hide(err)
	}
	st, err := s.perm.Load(ctx, actor, m.ConversationID)
	if err != nil {
		return st, m, hide(err)
	}
	return st, m, perm.CheckMessage(st, a, m)
}

// Message returns one message.
func (s *Service) Message(ctx context.Context, actor model.Account, id model.MessageID) (model.Message, error) {
	st, m, err := s.load(ctx, actor, id, perm.ReadMessage)
	if err != nil {
		return m, err
	}
	msgs := []model.Message{m}
	err = s.present(ctx, s.store, st, msgs)
	return msgs[0], err
}

// Thread returns a thread: its parent, a page of its replies, and the
// reader's state for it. id may be the parent or any reply.
func (s *Service) Thread(ctx context.Context, actor model.Account, id model.MessageID, p store.SeqPage) (parent model.Message, replies []model.Message, state *model.ThreadState, more bool, err error) {
	st, m, err := s.load(ctx, actor, id, perm.ReadMessage)
	if err != nil {
		return parent, nil, nil, false, err
	}
	if m.ParentID != "" {
		if m, err = s.store.Messages().Get(ctx, m.ParentID); err != nil {
			return parent, nil, nil, false, err
		}
	}
	replies, more, err = s.store.Messages().Replies(ctx, m.ID, p)
	if err != nil {
		return parent, nil, nil, false, err
	}
	all := append([]model.Message{m}, replies...)
	if err := s.present(ctx, s.store, st, all); err != nil {
		return parent, nil, nil, false, err
	}
	ts, found, err := s.store.Threads().Get(ctx, m.ID, actor.ID)
	if err != nil {
		return parent, nil, nil, false, err
	}
	if found {
		state = &ts
	}
	return all[0], all[1:], state, more, nil
}

// EditInput is the new content of a message. Nil fields stay as they are.
type EditInput struct {
	Text    *string
	FileIDs *[]model.FileID
	Cards   *[]model.Card
	Form    *model.FormInput
	// Instants is read with the new text, as for a new message.
	Instants Instants
}

// Edit changes a message's content. Inside the quick-correction window the
// message is replaced in place; after it the earlier version is kept as
// history (docs/FEATURES.md section 4.1).
func (s *Service) Edit(ctx context.Context, actor model.Account, id model.MessageID, in EditInput) (model.Message, error) {
	st, m, err := s.load(ctx, actor, id, perm.EditMessage)
	if err != nil {
		return m, err
	}
	if m.Deleted() {
		return m, model.ErrConflict.With("A deleted message cannot be edited.")
	}
	if st.Conversation.Sealed() {
		// The server keeps no versions of a sealed chat and can store no
		// text for one: an edit travels as a new sealed message
		// (docs/SEALED.md section 8.3).
		return m, model.BadRequest("A sealed message is not edited in place. Post an `edit` as a new sealed message.")
	}
	text, cards := m.Text, m.Cards
	if in.Text != nil {
		text = *in.Text
	}
	if utf8.RuneCountInString(text) > model.MaxMessageText {
		return m, model.ErrTooLarge.With("A message is at most 16,000 characters.")
	}
	if in.Cards != nil {
		if err := perm.Check(st, perm.PostCards); err != nil {
			return m, err
		}
		var err error
		if cards, err = keepServerCards(*in.Cards, m.Cards); err != nil {
			return m, err
		}
	}
	if err := s.checkEdit(ctx, st, text); err != nil {
		return m, err
	}
	quickSeconds, err := store.SettingIntOr(ctx, s.store, store.SettingQuickCorrection, model.DefaultQuickCorrection)
	if err != nil {
		return m, err
	}
	now := s.now()
	// Strictly inside the window: a window of 0 seconds (allowed, and what
	// switches quick corrections off) holds no edit, even one made in the
	// same millisecond as the message.
	quick := now.Sub(m.CreatedAt) < time.Duration(quickSeconds)*time.Second
	got, err := s.readWith(ctx, s.store, st, text, in.Instants)
	if err != nil {
		return m, err
	}
	if in.Text == nil || *in.Text == m.Text && in.Instants.TimeZone == "" && len(in.Instants.Hints) == 0 {
		got.entities = keptInstants(m.Entities, got.entities)
	}
	var out model.Message
	err = s.Tx(ctx, func(tx store.Tx, rec *Recorder) error {
		// A form that arrives with the edit replaces the message's form,
		// unless someone has answered it; the fallback text follows.
		fallback := m.Fallback
		if in.Form != nil {
			var ferr error
			if fallback, ferr = s.editForm(ctx, tx, st, m, *in.Form); ferr != nil {
				return ferr
			}
		}
		had, err := tx.Checklists().Items(ctx, id, true)
		if err != nil {
			return err
		}
		items := checklist(got.entities, got.tasks, had)
		current, err := tx.Files().ForMessage(ctx, id, false)
		if err != nil {
			return err
		}
		files := len(current)
		if in.FileIDs != nil {
			files = len(*in.FileIDs)
			if files > model.MaxFilesPerMessage {
				return model.Invalid("file_ids", "A message carries at most ten files.")
			}
		}
		if text == "" && files == 0 && len(cards) == 0 && fallback == "" {
			return model.BadRequest("An edit may not leave a message empty. Delete it instead.")
		}
		// The version is kept before anything is taken off, so it names
		// the files the earlier content carried.
		edited, err := tx.Messages().Edit(ctx, store.MessageEdit{ID: id, Text: text, Entities: got.entities, Cards: cards,
			Fallback: fallback, Mentions: got.mentions, KeepVersion: !quick, By: actor.ID, At: now})
		if err != nil {
			return err
		}
		if err := mentionBroadly(ctx, tx, edited, got.broad); err != nil {
			return err
		}
		if err := tx.Checklists().Replace(ctx, id, items, now); err != nil {
			return err
		}
		if in.FileIDs != nil {
			keep := map[model.FileID]bool{}
			for _, f := range *in.FileIDs {
				keep[f] = true
			}
			var kept, fresh []model.FileID
			for _, f := range current {
				if keep[f.ID] {
					kept = append(kept, f.ID)
					delete(keep, f.ID)
				}
			}
			for _, f := range *in.FileIDs {
				if keep[f] {
					fresh = append(fresh, f)
				}
			}
			gone, err := tx.Files().Detach(ctx, id, kept, now)
			if err != nil {
				return err
			}
			if quick {
				// A quick correction leaves no history, so files
				// taken off are deleted outright.
				for _, f := range gone {
					if _, err := tx.Files().Delete(ctx, f.ID); err != nil {
						return err
					}
					rec.RemoveBlobs(store.BlobKeys(f)...)
				}
			}
			if len(fresh) > 0 {
				if err := tx.Files().Attach(ctx, id, m.ConversationID, actor.ID, fresh); err != nil {
					return err
				}
			}
		}
		if err := s.stored(ctx, tx, rec, st, edited, true); err != nil {
			return err
		}
		if edited, err = s.dropStalePreviews(ctx, tx, rec, edited); err != nil {
			return err
		}
		if out, err = s.neutral(ctx, tx, st.Conversation, edited); err != nil {
			return err
		}
		_, err = rec.Emit(ctx, model.Event{Type: model.EventMessageEdited, ConversationID: m.ConversationID, MessageID: id,
			Payload: map[string]any{"message": out}})
		return err
	})
	if err != nil {
		return m, err
	}
	s.previewLinks(st.Conversation, out, m.Text)
	return s.Message(ctx, actor, id)
}

// Delete deletes a message (docs/FEATURES.md section 4.2) and returns the
// marker that stays in its place.
func (s *Service) Delete(ctx context.Context, actor model.Account, id model.MessageID) (model.Message, error) {
	st, m, err := s.load(ctx, actor, id, perm.DeleteMessage)
	if err != nil {
		return m, err
	}
	if m.Deleted() {
		return m, model.ErrConflict.With("That message is already deleted.")
	}
	quickSeconds, err := store.SettingIntOr(ctx, s.store, store.SettingQuickCorrection, model.DefaultQuickCorrection)
	if err != nil {
		return m, err
	}
	now := s.now()
	own := m.AuthorID == actor.ID
	quick := own && now.Sub(m.CreatedAt) < time.Duration(quickSeconds)*time.Second // strictly inside, as for an edit
	var out model.Message
	err = s.Tx(ctx, func(tx store.Tx, rec *Recorder) error {
		// The reply count is read again inside the transaction: a reply
		// may have arrived since the message was loaded.
		cur, err := tx.Messages().Get(ctx, id)
		if err != nil {
			return err
		}
		switch {
		case st.Conversation.Sealed():
			// The ciphertext and blobs go at once; the marker stays
			// (sealed.go).
			if out, err = s.eraseSealed(ctx, tx, rec, id, actor.ID, now); err != nil {
				return err
			}
		case quick && cur.ReplyCount == 0:
			// No trace is left. The marker below exists only in the
			// event, so clients drop what they show.
			keys, err := tx.Messages().DeleteOutright(ctx, id)
			if err != nil {
				return err
			}
			rec.RemoveBlobs(keys...)
			out = model.Message{ID: m.ID, ConversationID: m.ConversationID, Seq: m.Seq, AuthorID: m.AuthorID, ParentID: m.ParentID,
				Order: m.Order, CreatedAt: m.CreatedAt, DeletedAt: now, DeletedByID: actor.ID}
		default:
			deleted, err := tx.Messages().Delete(ctx, id, actor.ID, now)
			if err != nil {
				return err
			}
			if quick {
				// Inside the window but with replies: a bare record
				// stays so the replies still have a parent.
				keys, err := tx.Messages().Purge(ctx, id, now)
				if err != nil {
					return err
				}
				rec.RemoveBlobs(keys...)
			}
			if out, err = s.neutral(ctx, tx, st.Conversation, deleted); err != nil {
				return err
			}
		}
		if !own && !st.Conversation.Adhoc() {
			// Nothing done inside an ad-hoc channel is written to the
			// audit log, which admins read (docs/SCHEMA.md section 2).
			err := tx.Audit().Append(ctx, model.AuditEntry{At: now, ActorID: string(actor.ID), ActorKind: "account",
				Action: "message.deleted", TargetKind: "message", TargetID: string(id),
				Detail: map[string]any{"conversation_id": m.ConversationID, "author_id": m.AuthorID}})
			if err != nil {
				return err
			}
		}
		_, err = rec.Emit(ctx, model.Event{Type: model.EventMessageDeleted, ConversationID: m.ConversationID, MessageID: id,
			Payload: map[string]any{"message": out}})
		return err
	})
	return out, err
}

// History returns a message's earlier versions, and the last content of a
// deleted one, with the files each carried.
func (s *Service) History(ctx context.Context, actor model.Account, id model.MessageID) ([]model.MessageVersion, error) {
	if _, _, err := s.load(ctx, actor, id, perm.OpenHistory); err != nil {
		return nil, err
	}
	versions, err := s.store.Messages().Versions(ctx, id)
	if err != nil {
		return nil, err
	}
	for i, v := range versions {
		files, err := s.store.Files().GetMany(ctx, v.FileIDs)
		if err != nil {
			return nil, err
		}
		byID := map[model.FileID]model.File{}
		for _, f := range files {
			byID[f.ID] = f
		}
		for _, fid := range v.FileIDs {
			if f, ok := byID[fid]; ok {
				versions[i].Files = append(versions[i].Files, f)
			}
		}
	}
	return versions, nil
}

// PresentFor fills in what sits beside messages for a reader who has
// already been found able to read each of them; search uses it.
func (s *Service) PresentFor(ctx context.Context, actor model.Account, msgs []model.Message) error {
	if err := s.store.Messages().Hydrate(ctx, msgs, actor.ID); err != nil {
		return err
	}
	if err := s.fillForms(ctx, s.store, actor.ID, msgs); err != nil {
		return err
	}
	for i := range msgs {
		msgs[i].ClientID = ""
		if !msgs[i].HistoryHiddenAt.IsZero() && !actor.Role.IsAdmin() {
			msgs[i].HasHistory = false
		}
	}
	return nil
}
