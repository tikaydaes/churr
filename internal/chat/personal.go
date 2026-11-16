package chat

import (
	"context"
	"encoding/json"

	"github.com/tikaydaes/churr/internal/live"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/perm"
	"github.com/tikaydaes/churr/internal/store"
)

// What one person sets for themselves in a conversation: mode, mute, show, star
// and thread view; thread states; bookmarks; typing; and notify anyway.
// None of it changes the conversation for anyone else, so apart from the
// public mark of notify anyway nothing here takes a seq.

// ChangeMembership runs fn in one transaction and turns the membership
// changes it returns into member events. The people service changes
// memberships through it when a group, a role or a removal moves accounts
// in or out of conversations.
func (s *Service) ChangeMembership(ctx context.Context, by model.AccountID, reason string, fn func(tx store.Tx) (store.MembershipResult, error)) error {
	return s.Tx(ctx, func(tx store.Tx, rec *Recorder) error {
		res, err := fn(tx)
		if err != nil {
			return err
		}
		return rec.Membership(ctx, res, by, reason)
	})
}

// MembershipChange is what UpdateMembership takes. Nil fields stay as they
// are. Unmute and FollowAccountView say that `muted_until` or `thread_view`
// arrived as null.
//
// Mute is "nothing until a time": while it lasts the stored mode is
// `nothing` and the mode from before waits in mode_before_mute.
type MembershipChange struct {
	Mode              *model.Mode
	MutedUntil        *model.Time
	Unmute            bool
	Starred           *bool
	ThreadView        *string
	FollowAccountView bool
	// Show is whether the conversation is in the sidebar; AppDefaultShow
	// says `show` arrived as null, for the app's own default.
	Show           *string
	AppDefaultShow bool
	// IgnoreBroadMentions: @channel, @here and @everyone stop notifying
	// the person here (ruling R64).
	IgnoreBroadMentions *bool
}

// endMute rewrites a member row as for an unmute.
func endMute(m *model.Member) {
	if m.MutedUntil.IsZero() {
		return
	}
	if m.ModeBeforeMute != "" {
		m.Mode = m.ModeBeforeMute
	}
	m.ModeBeforeMute, m.MutedUntil = "", 0
}

// ownMembership builds the caller's membership with its numbers.
func (s *Service) ownMembership(ctx context.Context, actor model.Account, m model.Member) (*model.Membership, error) {
	u, err := s.store.Members().Unread(ctx, m.ConversationID, actor.ID)
	if err != nil {
		return nil, err
	}
	return membership(m, u, s.now(), flatView(actor, m)), nil
}

// UpdateMembership sets the caller's mode, mute, show, star and thread view for a
// conversation (docs/FEATURES.md section 2.4) and tells its other clients.
func (s *Service) UpdateMembership(ctx context.Context, actor model.Account, id model.ConversationID, in MembershipChange) (model.Membership, error) {
	st, err := s.perm.Can(ctx, actor, perm.SetMode, id)
	if err != nil {
		return model.Membership{}, hide(err)
	}
	now := s.now()
	if in.Mode != nil {
		if !in.Mode.Valid() {
			return model.Membership{}, model.Invalid("mode", "mode is any, mentions or nothing.")
		}
		if st.Conversation.Sealed() && *in.Mode == model.ModeMentions {
			// The server cannot see mentions in a sealed chat.
			return model.Membership{}, model.Invalid("mode", "A sealed chat has no Mentions mode.")
		}
	}
	if in.MutedUntil != nil && (in.Unmute || !in.MutedUntil.After(now)) {
		return model.Membership{}, model.Invalid("muted_until", "muted_until is a time in the future, or null to unmute.")
	}
	if in.Show != nil && (in.AppDefaultShow || !model.ValidShow(*in.Show)) {
		return model.Membership{}, model.Invalid("show", "show is always, activity, notification, hidden, or null for the app's default.")
	}
	if in.ThreadView != nil && *in.ThreadView != "threaded" && *in.ThreadView != "flat" {
		return model.Membership{}, model.Invalid("thread_view", "thread_view is threaded, flat, or null to follow your preference.")
	}
	m, err := s.store.Members().UpdateOwn(ctx, id, actor.ID, func(m *model.Member) error {
		// A mute that ran out and has not been tidied yet is over.
		if !m.MutedUntil.IsZero() && !m.MutedUntil.After(now) {
			endMute(m)
		}
		if in.Unmute {
			endMute(m)
		}
		if in.Mode != nil {
			// Setting a mode ends a mute.
			m.Mode, m.ModeBeforeMute, m.MutedUntil = *in.Mode, "", 0
		}
		if in.MutedUntil != nil {
			if m.MutedUntil.IsZero() {
				m.ModeBeforeMute = m.Mode
			}
			m.Mode, m.MutedUntil = model.ModeNothing, *in.MutedUntil
		}
		// What notifies and what is shown do not touch each other
		// (ruling R46).
		if in.Show != nil {
			m.Show = *in.Show
		}
		if in.AppDefaultShow {
			m.Show = ""
		}
		if in.Starred != nil {
			m.Starred = *in.Starred
		}
		if in.ThreadView != nil {
			m.ThreadView = *in.ThreadView
		}
		if in.FollowAccountView {
			m.ThreadView = ""
		}
		if in.IgnoreBroadMentions != nil {
			m.IgnoreBroad = *in.IgnoreBroadMentions
		}
		return nil
	})
	if err != nil {
		return model.Membership{}, err
	}
	// Mode and view decide what counts.
	s.touch(actor.ID, id)
	ms, err := s.ownMembership(ctx, actor, m)
	if err != nil {
		return model.Membership{}, err
	}
	s.hub.Send(actor.ID, model.Event{Type: model.EventMembershipUpdated, At: s.now(), ConversationID: id,
		Payload: map[string]any{"membership": ms}})
	return *ms, nil
}

// SweepMutes rewrites the member rows whose mute has run out, as for an
// unmute, and tells each person's clients (membership.updated). Reading a
// row never depends on this having run.
func (s *Service) SweepMutes(ctx context.Context) error {
	now := s.now()
	rows, err := s.store.Members().ExpiredMutes(ctx, now, 500)
	if err != nil {
		return err
	}
	for _, row := range rows {
		m, err := s.store.Members().UpdateOwn(ctx, row.ConversationID, row.AccountID, func(m *model.Member) error {
			if !m.MutedUntil.IsZero() && !m.MutedUntil.After(now) {
				endMute(m)
			}
			return nil
		})
		if err != nil {
			if model.CodeOf(err) == model.CodeNotFound {
				continue // left meanwhile
			}
			return err
		}
		s.touch(m.AccountID, m.ConversationID)
		acc, err := s.store.Accounts().Get(ctx, m.AccountID)
		if err != nil {
			return err
		}
		ms, err := s.ownMembership(ctx, acc, m)
		if err != nil {
			return err
		}
		s.hub.Send(m.AccountID, model.Event{Type: model.EventMembershipUpdated, At: s.now(), ConversationID: m.ConversationID,
			Payload: map[string]any{"membership": ms}})
	}
	return nil
}

// Typing tells the other members of a conversation that the actor is
// typing. Nothing is stored. It is not sent to the typist, nor to anyone
// who has blocked them, nor to anyone they have blocked: to those the
// typist is always online and nothing more (ruling R77).
func (s *Service) Typing(ctx context.Context, actor model.Account, id model.ConversationID, parent model.MessageID) error {
	st, err := s.perm.Can(ctx, actor, perm.Typing, id)
	if err != nil {
		return hide(err)
	}
	// Ruling R66: nobody is shown typing in an announcement channel or in
	// a conversation above the server's large_feature_limit. The notice is
	// taken and goes nowhere, so a client need not know the size.
	if roster, err := s.perm.Roster(ctx, id); err != nil {
		return err
	} else if roster.LargeAbove(st.Settings.LargeFeatureLimit) {
		return nil
	}
	fields := map[string]any{"account_id": actor.ID}
	if parent != "" {
		p, err := s.store.Messages().Get(ctx, parent)
		if err != nil || p.ConversationID != id {
			return model.Invalid("parent_id", "That is not a message of this conversation.")
		}
		if p.ParentID != "" {
			parent = p.ParentID
		}
		fields["parent_id"] = parent
	}
	to, err := s.perm.Recipients(ctx, id)
	if err != nil {
		return err
	}
	skip := map[model.AccountID]bool{actor.ID: true}
	if st.Settings.BlockingEnabled {
		blockers, err := s.store.Blocks().BlockersOf(ctx, actor.ID)
		if err != nil {
			return err
		}
		for _, b := range blockers {
			skip[b] = true
		}
		blocked, err := s.store.Blocks().Blocking(ctx, actor.ID)
		if err != nil {
			return err
		}
		for _, b := range blocked {
			skip[b] = true
		}
	}
	if s.outbound != nil && st.Conversation.IsShared() && !skipsRemote(to, skip, actor.ID, st.Conversation) {
		// The other servers of a shared conversation are told too.
		s.outbound.Typing(ctx, st.Conversation, actor.ID, parent)
	}
	e := model.Event{Type: model.EventTypingStarted, At: s.now(), ConversationID: id, Payload: fields}
	for _, account := range to {
		if !skip[account] {
			s.hub.Send(account, e)
		}
	}
	return nil
}

// TypingFrame handles the `typing` frame of the live connection, which does
// the same as the operation.
func (s *Service) TypingFrame(ctx context.Context, from live.Client, frameType string, frame json.RawMessage) error {
	var f struct {
		Type           string `json:"type"`
		ID             string `json:"id"`
		ConversationID string `json:"conversation_id"`
		ParentID       string `json:"parent_id"`
	}
	if frameType != "typing" || json.Unmarshal(frame, &f) != nil || !model.ValidID(f.ConversationID) || (f.ParentID != "" && !model.ValidID(f.ParentID)) {
		return model.BadRequest("`typing` takes `conversation_id` and, in a thread, `parent_id`.")
	}
	// The account is read again: the one on the connection is from when
	// it opened.
	acc, err := s.store.Accounts().Get(ctx, from.Account.ID)
	if err != nil {
		return err
	}
	return s.Typing(ctx, acc, model.ConversationID(f.ConversationID), model.MessageID(f.ParentID))
}

// thread loads the thread a message belongs to, for a member of its
// conversation. id may be the parent or any reply.
func (s *Service) thread(ctx context.Context, actor model.Account, id model.MessageID, a perm.Action) (perm.State, model.Message, error) {
	st, m, err := s.load(ctx, actor, id, perm.ReadMessage)
	if err != nil {
		return st, m, err
	}
	if err := perm.Check(st, a); err != nil {
		return st, m, err
	}
	if m.ParentID != "" {
		if m, err = s.store.Messages().Get(ctx, m.ParentID); err != nil {
			return st, m, err
		}
	}
	if m.IsSystem() {
		return st, m, model.BadRequest("A system notice has no thread.")
	}
	return st, m, nil
}

// tellThread sends thread_state.changed to the person's own connections.
// state is nil for "neither", and the client drops its row.
func (s *Service) tellThread(account model.AccountID, conv model.ConversationID, parent model.MessageID, state *model.ThreadState) {
	fields := map[string]any{"parent_id": parent}
	if state != nil {
		fields["thread_state"] = state
	}
	s.hub.Send(account, model.Event{Type: model.EventThreadStateChanged, At: s.now(), ConversationID: conv, Payload: fields})
}

// SetThreadState makes the actor follow or ignore a thread, or with "none"
// go back to neither, which deletes the row. It returns nil for neither.
func (s *Service) SetThreadState(ctx context.Context, actor model.Account, id model.MessageID, state string) (*model.ThreadState, error) {
	switch state {
	case model.ThreadFollowing, model.ThreadIgnored, "none":
	default:
		return nil, model.Invalid("state", "state is following, ignored or none.")
	}
	st, parent, err := s.thread(ctx, actor, id, perm.FollowThread)
	if err != nil {
		return nil, err
	}
	conv := parent.ConversationID
	if state == "none" {
		if err := s.store.Threads().Delete(ctx, parent.ID, actor.ID); err != nil {
			return nil, err
		}
		s.touch(actor.ID, conv)
		s.tellThread(actor.ID, conv, parent.ID, nil)
		return nil, nil
	}
	// A new row starts at the conversation's read position, so replies
	// the person had not seen stay unseen.
	ts, err := s.store.Threads().Set(ctx, parent.ID, actor.ID, state, st.Member.ReadSeq, s.now())
	if err != nil {
		return nil, err
	}
	s.touch(actor.ID, conv)
	s.tellThread(actor.ID, conv, parent.ID, &ts)
	return &ts, nil
}

// SetThreadRead moves the actor's read position in a thread. For a thread
// it neither follows nor ignores nothing is stored: such a thread is read
// at the conversation's position, and the answer says so with state "none".
func (s *Service) SetThreadRead(ctx context.Context, actor model.Account, id model.MessageID, seq int64) (model.ThreadState, error) {
	st, parent, err := s.thread(ctx, actor, id, perm.MarkRead)
	if err != nil {
		return model.ThreadState{}, err
	}
	if seq < 0 {
		return model.ThreadState{}, model.Invalid("seq", "A read position is 0 or more.")
	}
	if seq > st.Conversation.LastSeq {
		seq = st.Conversation.LastSeq
	}
	ts, found, err := s.store.Threads().SetRead(ctx, parent.ID, actor.ID, seq, s.now())
	if err != nil {
		return ts, err
	}
	if !found {
		return model.ThreadState{ParentID: parent.ID, ConversationID: parent.ConversationID, State: "none", ReadSeq: st.Member.ReadSeq}, nil
	}
	s.touch(actor.ID, parent.ConversationID)
	s.tellThread(actor.ID, parent.ConversationID, parent.ID, &ts)
	return ts, nil
}

// Threads returns the Threads view: the threads the actor follows that have
// unread replies, latest reply first, each with its parent. With all,
// followed threads with nothing unread too.
func (s *Service) Threads(ctx context.Context, actor model.Account, all bool, p store.Page) ([]model.ThreadEntry, string, error) {
	if !actor.Active() {
		return nil, "", model.ErrUnauthenticated
	}
	states, next, err := s.store.Threads().Unread(ctx, actor.ID, all, p)
	if err != nil {
		return nil, "", err
	}
	ids := make([]model.MessageID, len(states))
	for i, ts := range states {
		ids[i] = ts.ParentID
	}
	// A thread state exists only for a member of the conversation, so
	// every parent here is one the actor can read.
	parents, err := s.store.Messages().GetMany(ctx, ids)
	if err != nil {
		return nil, "", err
	}
	if err := s.PresentFor(ctx, actor, parents); err != nil {
		return nil, "", err
	}
	byID := make(map[model.MessageID]model.Message, len(parents))
	for _, m := range parents {
		byID[m.ID] = m
	}
	out := make([]model.ThreadEntry, 0, len(states))
	for _, ts := range states {
		if parent, ok := byID[ts.ParentID]; ok {
			out = append(out, model.ThreadEntry{ThreadState: ts, Parent: parent})
		}
	}
	return out, next, nil
}

// personal asks perm whether the actor may keep personal things at all
// (bookmarks are a person's; a bot has none).
func (s *Service) personal(ctx context.Context, actor model.Account) error {
	set, err := s.perm.Settings(ctx)
	if err != nil {
		return err
	}
	return perm.CheckServer(actor, perm.OwnPersonalThings, set)
}

// AddBookmark saves a message for the actor. Anyone can bookmark any
// message they can read. Nobody else is told.
func (s *Service) AddBookmark(ctx context.Context, actor model.Account, id model.MessageID) error {
	st, _, err := s.load(ctx, actor, id, perm.ReadMessage)
	if err != nil {
		return err
	}
	if err := perm.Check(st, perm.Bookmark); err != nil {
		return err
	}
	if err := s.personal(ctx, actor); err != nil {
		return err
	}
	b, added, err := s.store.Bookmarks().Add(ctx, actor.ID, id, s.now())
	if err != nil || !added {
		return err
	}
	s.hub.Send(actor.ID, model.Event{Type: model.EventBookmarkAdded, At: s.now(),
		Payload: map[string]any{"message_id": b.MessageID, "created_at": b.CreatedAt}})
	return nil
}

// RemoveBookmark unsaves a message. It needs no access to the message: a
// bookmark of something the actor can no longer read can still be removed.
func (s *Service) RemoveBookmark(ctx context.Context, actor model.Account, id model.MessageID) error {
	if !actor.Active() {
		return model.ErrUnauthenticated
	}
	removed, err := s.store.Bookmarks().Remove(ctx, actor.ID, id)
	if err != nil {
		return err
	}
	if !removed {
		return model.ErrNotFound
	}
	s.hub.Send(actor.ID, model.Event{Type: model.EventBookmarkRemoved, At: s.now(), Payload: map[string]any{"message_id": id}})
	return nil
}

// Bookmarks lists the actor's bookmarks, newest first, each with its
// message. A bookmark of a message the actor can no longer read is left
// out.
func (s *Service) Bookmarks(ctx context.Context, actor model.Account, p store.Page) ([]model.Bookmark, string, error) {
	if !actor.Active() {
		return nil, "", model.ErrUnauthenticated
	}
	list, next, err := s.store.Bookmarks().List(ctx, actor.ID, s.perm.Readable(ctx, actor), p)
	if err != nil {
		return nil, "", err
	}
	ids := make([]model.MessageID, len(list))
	for i, b := range list {
		ids[i] = b.MessageID
	}
	msgs, err := s.store.Messages().GetMany(ctx, ids)
	if err != nil {
		return nil, "", err
	}
	if err := s.PresentFor(ctx, actor, msgs); err != nil {
		return nil, "", err
	}
	byID := make(map[model.MessageID]*model.Message, len(msgs))
	for i := range msgs {
		byID[msgs[i].ID] = &msgs[i]
	}
	out := make([]model.Bookmark, 0, len(list))
	for _, b := range list {
		if b.Message = byID[b.MessageID]; b.Message != nil {
			out = append(out, b)
		}
	}
	return out, next, nil
}

// NotifyAnyway pushes a message through one person's do not disturb, at its
// author's request (docs/FEATURES.md section 3.8).
//
// It always appears to succeed (ruling R4). The only refusals are about the
// caller: not the author, not a person, or naming an account the caller
// cannot see. Whether it applied goes into notify_anyway.delivered and
// nowhere else: the answer, the events everyone gets and anything anyone can
// fetch later are the same either way.
func (s *Service) NotifyAnyway(ctx context.Context, actor model.Account, id model.MessageID, target model.AccountID) (model.Message, error) {
	st, m, err := s.load(ctx, actor, id, perm.ReadMessage)
	if err != nil {
		return m, err
	}
	if m.AuthorID != actor.ID {
		return m, model.ErrForbidden.With("Only a message's author can push it through do not disturb.")
	}
	if err := perm.Check(st, perm.NotifyAnyway); err != nil {
		return m, err
	}
	if m.Via != nil || m.IsSystem() || m.Deleted() {
		return m, model.ErrForbidden.With("Only a message you typed and posted yourself can be pushed through do not disturb.")
	}
	t, err := s.store.Accounts().Get(ctx, target)
	if err != nil {
		return m, hide(err)
	}
	if err := s.perm.CanSeeAccount(ctx, actor, t); err != nil {
		return m, err
	}
	// From here on nothing depends on the target in any way the caller
	// could see. An account on another server is never reached: its own
	// server decides for it.
	applies := t.Local() && t.ID != actor.ID && s.notifier != nil && s.notifier.AnywayApplies(ctx, m, t.ID)
	var first bool
	err = s.Tx(ctx, func(tx store.Tx, rec *Recorder) error {
		marked, wrote, err := tx.NotifyAnyway().Record(ctx, id, t.ID, actor.ID, s.now(), applies)
		if err != nil {
			return err
		}
		first = wrote
		if !marked {
			return nil
		}
		// The public mark is new: everyone sees the message with it.
		cur, err := tx.Messages().Get(ctx, id)
		if err != nil {
			return err
		}
		out, err := s.neutral(ctx, tx, st.Conversation, cur)
		if err != nil {
			return err
		}
		_, err = rec.Emit(ctx, model.Event{Type: model.EventMessageEdited, ConversationID: m.ConversationID, MessageID: id,
			Payload: map[string]any{"message": out}})
		return err
	})
	if err != nil {
		return m, err
	}
	if first && applies {
		s.pushThrough(ctx, st.Conversation, id, t.ID)
	}
	return s.Message(ctx, actor, id)
}

// pushThrough notifies the target of a notify-anyway request that applied,
// as if it were not in do not disturb: its connected clients get the
// message again with `notify` set to `notify`. Nobody else is sent anything.
//
// TODO(push): the phone wake-up for this message belongs to the push
// package and is sent from here once it exists.
func (s *Service) pushThrough(ctx context.Context, conv model.Conversation, id model.MessageID, target model.AccountID) {
	cur, err := s.store.Messages().Get(ctx, id)
	if err != nil {
		return
	}
	out, err := s.neutral(ctx, s.store, conv, cur)
	if err != nil {
		return
	}
	s.hub.Send(target, model.Event{Type: model.EventMessageCreated, At: cur.CreatedAt, ConversationID: cur.ConversationID, Seq: cur.Seq,
		Payload: map[string]any{"message": out, "notify": "notify"}})
	// The wake-up it causes is sent now, and looks like any other.
	s.wakeAnyway(cur, target)
}

// skipsRemote reports whether a typing notice need not leave this server: a
// direct or sealed chat whose other person, on another server, has been
// blocked by the typist (ruling R77, docs/FEATURES.md section 20.4). In a
// chat of two the notice would reach that person alone, so it is not sent.
// In a larger conversation it goes, since the other server's other members
// are owed it; that server has been told of the block and leaves the
// blocked person out (docs/LINK-PROTOCOL.md section 17a).
func skipsRemote(to []model.AccountID, skip map[model.AccountID]bool, actor model.AccountID, c model.Conversation) bool {
	if !(c.Sealed() || c.Adhoc()) || len(to) != 2 {
		return false
	}
	for _, id := range to {
		if id != actor && skip[id] {
			return true
		}
	}
	return false
}
