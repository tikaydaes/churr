package chat

import (
	"bytes"
	"context"
	"errors"
	"io"

	"github.com/tikaydaes/churr/internal/blob"
	"github.com/tikaydaes/churr/internal/markdown"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

// This file applies events that came from other servers. The link package
// has already decided that the event is acceptable (who may say what is its
// business, docs/LINK-PROTOCOL.md section 12); these methods make the change
// the way a local one is made, so the events log, mentions, thread
// summaries, search and live delivery behave the same. Each is called
// inside RemoteTx and emits at most one stored event, which takes this
// server's next seq: the arrival number here. Where a message is shown is
// its stamp's business, which the link passes in as an order string.

// remoteMentions works out the mention rows of a message written on another
// server: the local accounts its entities name that can read the
// conversation. Groups do not cross servers, so there are none to expand.
// A broad mention's rows are the store's to write once the message is
// stored (mentionBroadly), as for a message written here; whether its
// author might use one was its own server's to decide.
func (s *Service) remoteMentions(ctx context.Context, tx store.Tx, conv model.Conversation, m model.Message) ([]model.Mention, []model.AccountID, []store.BroadMention, error) {
	var named []model.AccountID
	scope := ""
	for _, e := range m.Entities {
		switch e.Type {
		case model.EntityAccount:
			named = append(named, e.AccountID)
		case model.EntityBroadcast:
			if scope != "channel" {
				scope = e.Scope
			}
		}
	}
	if len(named) == 0 && scope == "" {
		return nil, nil, nil, nil
	}
	members, err := tx.Members().IDs(ctx, conv.ID)
	if err != nil {
		return nil, nil, nil, err
	}
	var broad []store.BroadMention
	switch scope {
	case "":
	case "here":
		b := store.BroadMention{Scope: store.BroadHere, Blocking: true}
		for _, id := range members {
			if s.hub.Presence(id) == model.PresenceOnline {
				b.Online = append(b.Online, id)
			}
		}
		broad = append(broad, b)
	default:
		broad = append(broad, store.BroadMention{Scope: store.BroadChannel, Blocking: true})
	}
	if len(named) == 0 {
		return nil, nil, broad, nil
	}
	isMember := make(map[model.AccountID]bool, len(members))
	for _, id := range members {
		isMember[id] = true
	}
	accounts, err := tx.Accounts().GetMany(ctx, named)
	if err != nil {
		return nil, nil, nil, err
	}
	var out []model.Mention
	var byName []model.AccountID
	seen := map[model.AccountID]bool{}
	for _, acc := range accounts {
		canRead := isMember[acc.ID] || (conv.Public() && acc.Role.IsFullMember())
		if seen[acc.ID] || acc.ID == m.AuthorID || !canRead || !acc.Active() {
			continue
		}
		seen[acc.ID] = true
		if blocked, err := tx.Blocks().Has(ctx, acc.ID, m.AuthorID); err != nil {
			return nil, nil, nil, err
		} else if blocked {
			continue
		}
		out = append(out, model.Mention{AccountID: acc.ID, Kind: model.MentionAccount})
		byName = append(byName, acc.ID)
	}
	return out, byName, broad, nil
}

// RemoteMessage is a message that came from another server, with the rows
// that hang from it.
type RemoteMessage struct {
	// Message has its ID, AuthorID, ParentID, Quote, Text, Entities,
	// Cards, Fallback, Via, System, CreatedAt and, in a sealed chat,
	// ClientID, Sealed and Quiet. Order is the order string of the stamp
	// its origin gave it, and Late whether messages ordered after it have
	// already been delivered here. ConversationID and Seq are set here.
	Message model.Message
	// Files are the rows for its attachments and card images, in state
	// `arriving`; MessageID and ConversationID are set here.
	Files []model.File
	// Form is the message's form, already checked, or nil.
	Form *store.Form
}

// insertRemote stores a remote message under a seq and returns it as an
// event carries it.
func (s *Service) insertRemote(ctx context.Context, tx store.Tx, conv model.Conversation, in RemoteMessage, seq int64) (model.Message, []model.AccountID, error) {
	m := in.Message
	m.ConversationID, m.Seq = conv.ID, seq
	if m.ParentID != "" {
		parent, err := tx.Messages().Get(ctx, m.ParentID)
		if err != nil || parent.ConversationID != conv.ID || parent.ParentID != "" {
			return m, nil, model.ErrNotFound.With("The message it replies to is not held here.")
		}
	}
	var mentions []model.Mention
	var named []model.AccountID
	var broad []store.BroadMention
	if !conv.Sealed() {
		var err error
		if mentions, named, broad, err = s.remoteMentions(ctx, tx, conv, m); err != nil {
			return m, nil, err
		}
	}
	// The checklist items come from the text, as on the author's server,
	// so both give the same identifiers to the same items.
	tasks := markdown.Tasks(markdown.Parse(m.Text))
	items := checklist(m.Entities, tasks, nil)
	if err := tx.Messages().Insert(ctx, &m, mentions); err != nil {
		return m, nil, err
	}
	if err := mentionBroadly(ctx, tx, m, broad); err != nil {
		return m, nil, err
	}
	if err := attachForm(ctx, tx, m, in.Form); err != nil {
		return m, nil, err
	}
	if len(items) > 0 {
		if err := tx.Checklists().Replace(ctx, m.ID, items, m.CreatedAt); err != nil {
			return m, nil, err
		}
	}
	for i := range in.Files {
		f := in.Files[i]
		f.MessageID, f.ConversationID = m.ID, conv.ID
		if f.UploaderID == "" {
			f.UploaderID = m.AuthorID
		}
		if err := tx.Files().Insert(ctx, &f); err != nil {
			return m, nil, err
		}
	}
	return m, named, nil
}

// ApplyMessage applies message.created from another server.
func (s *Service) ApplyMessage(ctx context.Context, tx store.Tx, rec *Recorder, conv model.Conversation, in RemoteMessage) error {
	seq, err := rec.Seq(ctx, conv.ID)
	if err != nil {
		return err
	}
	m, named, err := s.insertRemote(ctx, tx, conv, in, seq)
	if err != nil {
		return err
	}
	if m.ParentID != "" {
		// The thread states the server changes by itself, for the local
		// people concerned (docs/FEATURES.md section 1.4).
		parent, err := tx.Messages().Get(ctx, m.ParentID)
		if err != nil {
			return err
		}
		follow := func(account model.AccountID) error {
			mem, found, err := tx.Members().Get(ctx, conv.ID, account)
			if err != nil || !found {
				return err
			}
			ts, changed, err := tx.Threads().Follow(ctx, m.ParentID, account, mem.ReadSeq, 0, false, m.CreatedAt)
			if err == nil && changed {
				rec.After(func() {
					s.hub.Send(account, model.Event{Type: model.EventThreadStateChanged, At: s.now(), ConversationID: conv.ID,
						Payload: map[string]any{"parent_id": ts.ParentID, "thread_state": ts}})
				})
			}
			return err
		}
		if parent.ReplyCount <= 1 && parent.AuthorID != m.AuthorID {
			if err := follow(parent.AuthorID); err != nil {
				return err
			}
		}
		for _, id := range named {
			if err := follow(id); err != nil {
				return err
			}
		}
	}
	out, err := s.neutral(ctx, tx, conv, m)
	if err != nil {
		return err
	}
	return rec.EmitAt(ctx, model.Event{Type: model.EventMessageCreated, At: m.CreatedAt, ConversationID: conv.ID, Seq: seq,
		MessageID: m.ID, Payload: map[string]any{"message": out}})
}

// ImportMessage stores a message of a history snapshot under this server's
// next seq, with the order string the snapshot gave it. It emits nothing:
// history is a snapshot, not a replay.
func (s *Service) ImportMessage(ctx context.Context, tx store.Tx, rec *Recorder, conv model.Conversation, in RemoteMessage) error {
	if _, err := tx.Messages().Get(ctx, in.Message.ID); err == nil {
		return nil // already held
	} else if !errors.Is(err, model.ErrNotFound) {
		return err
	}
	seq, err := rec.Seq(ctx, conv.ID)
	if err != nil {
		return err
	}
	_, _, err = s.insertRemote(ctx, tx, conv, in, seq)
	return err
}

// ApplyEdit applies message.edited: the whole new content. keepVersion is
// false for a quick correction, which keeps no earlier version. order is the
// order string of the edit's own stamp: of two edits to one message the
// later stands, whichever arrives first, so the link does not call this for
// an edit older than the one held.
func (s *Service) ApplyEdit(ctx context.Context, tx store.Tx, rec *Recorder, conv model.Conversation, in RemoteMessage, keepVersion bool, at model.Time, order string) error {
	m := in.Message
	if order != "" {
		if err := tx.Streams().SetEditOrder(ctx, m.ID, order); err != nil {
			return err
		}
	}
	mentions, _, broad, err := s.remoteMentions(ctx, tx, conv, m)
	if err != nil {
		return err
	}
	had, err := tx.Checklists().Items(ctx, m.ID, true)
	if err != nil {
		return err
	}
	items := checklist(m.Entities, markdown.Tasks(markdown.Parse(m.Text)), had)
	edited, err := tx.Messages().Edit(ctx, store.MessageEdit{ID: m.ID, Text: m.Text, Entities: m.Entities, Cards: m.Cards,
		Fallback: m.Fallback, Mentions: mentions, KeepVersion: keepVersion, By: m.AuthorID, At: at})
	if err != nil {
		return err
	}
	if err := mentionBroadly(ctx, tx, edited, broad); err != nil {
		return err
	}
	if err := tx.Checklists().Replace(ctx, m.ID, items, at); err != nil {
		return err
	}
	current, err := tx.Files().ForMessage(ctx, m.ID, false)
	if err != nil {
		return err
	}
	have := map[model.FileID]bool{}
	for _, f := range current {
		have[f.ID] = true
	}
	var keep []model.FileID
	for i := range in.Files {
		f := in.Files[i]
		if f.Purpose != "" && f.Purpose != "attachment" {
			continue
		}
		keep = append(keep, f.ID)
	}
	gone, err := tx.Files().Detach(ctx, m.ID, keep, at)
	if err != nil {
		return err
	}
	if !keepVersion {
		for _, f := range gone {
			if _, err := tx.Files().Delete(ctx, f.ID); err != nil {
				return err
			}
			rec.RemoveBlobs(store.BlobKeys(f)...)
		}
	}
	for i := range in.Files {
		f := in.Files[i]
		if have[f.ID] {
			continue
		}
		if _, err := tx.Files().Get(ctx, f.ID); err == nil {
			continue
		}
		f.MessageID, f.ConversationID = m.ID, conv.ID
		if f.UploaderID == "" {
			f.UploaderID = edited.AuthorID
		}
		if err := tx.Files().Insert(ctx, &f); err != nil {
			return err
		}
	}
	out, err := s.neutral(ctx, tx, conv, edited)
	if err != nil {
		return err
	}
	_, err = rec.Emit(ctx, model.Event{Type: model.EventMessageEdited, ConversationID: conv.ID, MessageID: m.ID,
		Payload: map[string]any{"message": out}})
	return err
}

// ApplyDelete applies message.deleted and message.removed: the message is
// replaced by its marker and its content kept as history, as a local delete
// does.
func (s *Service) ApplyDelete(ctx context.Context, tx store.Tx, rec *Recorder, conv model.Conversation, id model.MessageID, by model.AccountID, at model.Time) error {
	deleted, err := tx.Messages().Delete(ctx, id, by, at)
	if err != nil {
		return err
	}
	out, err := s.neutral(ctx, tx, conv, deleted)
	if err != nil {
		return err
	}
	_, err = rec.Emit(ctx, model.Event{Type: model.EventMessageDeleted, ConversationID: conv.ID, MessageID: id,
		Payload: map[string]any{"message": out}})
	return err
}

// ApplyPurge applies message.purged. With content true everything but the
// record that a message was there is erased; otherwise only its earlier
// versions. Erasing means the database, the search index and the file
// store.
func (s *Service) ApplyPurge(ctx context.Context, tx store.Tx, rec *Recorder, conv model.Conversation, id model.MessageID, content bool) error {
	if content {
		m, err := tx.Messages().Get(ctx, id)
		if err != nil {
			return err
		}
		if !m.Deleted() {
			if _, err := tx.Messages().Delete(ctx, id, m.AuthorID, s.now()); err != nil {
				return err
			}
		}
	}
	_, err := s.purge(ctx, tx, rec, conv, id, true)
	return err
}

// ApplyExpire applies messages.expired: every message stamped at or before
// through (a time of the conversation's reference clock) that erase says yes
// to is removed outright, as local retention removes one. No event is
// stored; retention sends none.
func (s *Service) ApplyExpire(ctx context.Context, tx store.Tx, rec *Recorder, conv model.Conversation, through int64, erase func(m model.Message) bool) error {
	after := int64(0)
	for {
		msgs, more, err := tx.Messages().List(ctx, conv.ID, store.MessageList{SeqPage: store.SeqPage{After: after, Limit: 200}})
		if err != nil {
			return err
		}
		for _, m := range msgs {
			after = m.Seq
			if model.OrderAt(m.Order) > through || !erase(m) {
				continue
			}
			keys, err := tx.Messages().DeleteOutright(ctx, m.ID)
			if err != nil {
				return err
			}
			rec.RemoveBlobs(keys...)
		}
		if !more || len(msgs) == 0 {
			return nil
		}
	}
}

// ApplyReaction applies reaction.added and reaction.removed.
func (s *Service) ApplyReaction(ctx context.Context, tx store.Tx, rec *Recorder, m model.Message, account model.AccountID, emoji string, add bool, at model.Time) error {
	if add {
		added, err := tx.Reactions().Add(ctx, m.ID, account, emoji, at)
		if err != nil || !added {
			return err
		}
		return s.reactionEvent(ctx, tx, rec, model.EventReactionAdded, m, account, emoji)
	}
	removed, err := tx.Reactions().Remove(ctx, m.ID, account, emoji)
	if err != nil || !removed {
		return err
	}
	return s.reactionEvent(ctx, tx, rec, model.EventReactionRemoved, m, account, emoji)
}

// ApplyTick applies task.changed.
func (s *Service) ApplyTick(ctx context.Context, tx store.Tx, rec *Recorder, m model.Message, itemID string, checked bool, account model.AccountID, at model.Time) error {
	items, err := tx.Checklists().Items(ctx, m.ID, false)
	if err != nil {
		return err
	}
	for _, it := range items {
		if it.ItemID != itemID {
			continue
		}
		if it.Checked == checked {
			return nil
		}
		task, err := tx.Checklists().Tick(ctx, m.ID, itemID, account, checked, at)
		if err != nil {
			return err
		}
		_, err = rec.Emit(ctx, model.Event{Type: model.EventTaskChanged, ConversationID: m.ConversationID, MessageID: m.ID,
			Payload: map[string]any{"message_id": m.ID, "task": task}})
		return err
	}
	return model.ErrNotFound
}

// ApplyJoin applies member.joined: the account, whose shadow exists by now,
// becomes a member at level member. Levels do not travel.
func (s *Service) ApplyJoin(ctx context.Context, tx store.Tx, rec *Recorder, conv model.Conversation, account model.AccountID, at model.Time) error {
	if _, found, err := tx.Members().Get(ctx, conv.ID, account); err != nil || found {
		return err
	}
	res, err := tx.Members().AddDirect(ctx, conv.ID, account, model.LevelMember, at)
	if err != nil {
		return err
	}
	return rec.Membership(ctx, res, "", "")
}

// ApplyLeave applies member.left.
func (s *Service) ApplyLeave(ctx context.Context, tx store.Tx, rec *Recorder, conv model.Conversation, account model.AccountID, reason string) error {
	if _, found, err := tx.Members().Get(ctx, conv.ID, account); err != nil || !found {
		return err
	}
	res, err := tx.Members().RemoveDirect(ctx, conv.ID, account)
	if err != nil {
		return err
	}
	return rec.Membership(ctx, res, "", reason)
}

// DropMembers removes accounts from a conversation without an event each:
// the accounts of a server that left the stream, who all leave at once.
// The caller emits the one event that says so (ApplyConversation).
func (s *Service) DropMembers(ctx context.Context, tx store.Tx, conv model.ConversationID, accounts []model.AccountID) error {
	for _, a := range accounts {
		if _, found, err := tx.Members().Get(ctx, conv, a); err != nil {
			return err
		} else if !found {
			continue
		}
		if _, err := tx.Members().RemoveDirect(ctx, conv, a); err != nil {
			return err
		}
	}
	return nil
}

// ApplyConversation applies conversation.updated from the stream's owner
// (name and topic; nil stays as it is), and is also how a change to the
// stream itself reaches local clients: the event carries the conversation
// with what the stream now says.
func (s *Service) ApplyConversation(ctx context.Context, tx store.Tx, rec *Recorder, conv model.ConversationID, name, topic *string) error {
	c, err := tx.Conversations().Update(ctx, conv, func(c *model.Conversation) error {
		if name != nil && c.Name != "" {
			c.Name = *name
		}
		if topic != nil {
			c.Topic = *topic
		}
		return nil
	})
	if err != nil {
		return err
	}
	return s.conversationEvent(ctx, tx, rec, model.EventConversationUpdated, c, "")
}

// ApplyCards applies preview.added and preview.removed: the message's cards
// are replaced, which is not an edit.
func (s *Service) ApplyCards(ctx context.Context, tx store.Tx, rec *Recorder, conv model.Conversation, id model.MessageID, cards []model.Card, files []model.File, eventType string) error {
	m, err := tx.Messages().SetCards(ctx, id, cards)
	if err != nil {
		return err
	}
	for i := range files {
		f := files[i]
		if _, err := tx.Files().Get(ctx, f.ID); err == nil {
			continue
		}
		f.MessageID, f.ConversationID = id, conv.ID
		if f.UploaderID == "" {
			f.UploaderID = m.AuthorID
		}
		if err := tx.Files().Insert(ctx, &f); err != nil {
			return err
		}
	}
	out, err := s.neutral(ctx, tx, conv, m)
	if err != nil {
		return err
	}
	_, err = rec.Emit(ctx, model.Event{Type: eventType, ConversationID: conv.ID, MessageID: id, Payload: map[string]any{"message": out}})
	return err
}

// Neutral returns a message as an event carries it: what sits beside it
// filled in, for nobody in particular. The link builds wire messages and
// history from it.
func (s *Service) Neutral(ctx context.Context, a store.Areas, conv model.Conversation, m model.Message) (model.Message, error) {
	return s.neutral(ctx, a, conv, m)
}

// TellLocal sends an event that is not stored to the local accounts that
// receive a conversation's events: typing from another server, a file that
// has arrived.
func (s *Service) TellLocal(ctx context.Context, conv model.ConversationID, eventType string, payload map[string]any) {
	e := model.Event{Type: eventType, At: s.now(), ConversationID: conv, Payload: payload}
	// Presence goes to the hub in one call, so it can merge and delay it
	// under pressure (ruling R62).
	if ps, ok := s.hub.(interface {
		SendPresence(to []model.AccountID, about model.AccountID, v any)
	}); ok && eventType == model.EventPresenceChanged {
		if about, _ := payload["account_id"].(model.AccountID); about != "" {
			ps.SendPresence(s.localRecipients(ctx, conv), about, e)
			return
		}
	}
	for _, to := range s.localRecipients(ctx, conv) {
		s.hub.Send(to, e)
	}
}

// AttachStream marks a conversation as shared (or, with an empty stream, as
// no longer shared) and tells its members. With archive the conversation is
// archived too, which is what leaving a stream does to the local copy, and
// the remote accounts in it are taken out so they show as former members.
func (s *Service) AttachStream(ctx context.Context, conv model.ConversationID, stream model.StreamID, archive bool) error {
	return s.RemoteTx(ctx, func(tx store.Tx, rec *Recorder) error {
		c, err := tx.Conversations().Update(ctx, conv, func(c *model.Conversation) error {
			c.StreamID = stream
			return nil
		})
		if err != nil {
			return err
		}
		if stream == "" {
			ids, err := tx.Members().IDs(ctx, conv)
			if err != nil {
				return err
			}
			accounts, err := tx.Accounts().GetMany(ctx, ids)
			if err != nil {
				return err
			}
			var remote []model.AccountID
			for _, a := range accounts {
				if !a.Local() {
					remote = append(remote, a.ID)
				}
			}
			if err := s.DropMembers(ctx, tx, conv, remote); err != nil {
				return err
			}
			if c, err = tx.Conversations().Get(ctx, conv); err != nil {
				if errors.Is(err, model.ErrNotFound) {
					return nil // its last member was remote; it went with them
				}
				return err
			}
		}
		if archive && c.Named() {
			_, err := s.setArchived(ctx, tx, rec, c, "", true)
			return err
		}
		return s.conversationEvent(ctx, tx, rec, model.EventConversationUpdated, c, "")
	})
}

// CreateShared makes the local channel for a stream this server has
// accepted: a named channel with the accepting owner as its manager, already
// marked as shared.
func (s *Service) CreateShared(ctx context.Context, by model.Account, name, topic string, private bool, section model.SectionID, stream model.StreamID) (model.Conversation, error) {
	name, ok := model.CleanChannelName(name)
	if !ok {
		return model.Conversation{}, model.Invalid("name", "A channel name is 1 to 64 characters, with no control characters.")
	}
	c := model.Conversation{Kind: model.KindChannel, Name: name, Topic: topic, Private: private, SectionID: section,
		DefaultLevel: model.LevelMember, CreatedByID: by.ID, CreatedAt: s.now(), StreamID: stream}
	err := s.RemoteTx(ctx, func(tx store.Tx, rec *Recorder) error {
		if section != "" {
			if _, err := tx.Sections().Get(ctx, section); err != nil {
				return model.Invalid("section_id", "There is no such section.")
			}
		}
		if err := tx.Conversations().Create(ctx, &c); err != nil {
			return err
		}
		res, err := tx.Members().AddDirect(ctx, c.ID, by.ID, model.LevelManager, c.CreatedAt)
		if err != nil {
			return err
		}
		return rec.Membership(ctx, res, "", "")
	})
	return c, err
}

// ReceiveFile stores the bytes of a file that came from another server and
// has been checked against the size and hash its event gave. The file is
// treated as an upload of this server's own: what it is is decided from its
// content, and an image gets this server's own thumbnail. Local members are
// told with file.changed.
func (s *Service) ReceiveFile(ctx context.Context, id model.FileID, content io.ReadSeeker) error {
	blobs := s.fileStore()
	if blobs == nil {
		return model.ErrUnavailable
	}
	f, err := s.store.Files().Get(ctx, id)
	if err != nil {
		return err
	}
	conv, err := s.store.Conversations().Get(ctx, f.ConversationID)
	if err != nil {
		return err
	}
	var thumb *blob.Thumb
	if !conv.Sealed() {
		head := make([]byte, blob.SniffLen)
		if _, err := content.Seek(0, io.SeekStart); err != nil {
			return err
		}
		n, _ := io.ReadFull(content, head)
		f.MediaType, f.Kind = blob.Sniff(head[:n])
		if f.Kind == model.FileText {
			// Text is shown inline only for uploads made here.
			f.MediaType, f.Kind = blob.TypeUnknown, model.FileOther
		}
		if f.Kind == model.FileImage {
			if _, err := content.Seek(0, io.SeekStart); err != nil {
				return err
			}
			thumbnails <- struct{}{}
			thumb, err = blob.MakeThumbnail(content)
			<-thumbnails
			if err != nil {
				thumb = nil
			}
		}
	}
	if _, err := content.Seek(0, io.SeekStart); err != nil {
		return err
	}
	var keys []string
	key, _, _, err := blobs.Put(ctx, content)
	if err != nil {
		return err
	}
	keys = append(keys, key)
	f.BlobKey = key
	if thumb != nil {
		if f.ThumbKey, _, _, err = blobs.Put(ctx, bytes.NewReader(thumb.Data)); err != nil {
			s.dropBlobs(ctx, keys)
			return err
		}
		keys = append(keys, f.ThumbKey)
	}
	if err := s.store.Streams().FileArrived(ctx, f); err != nil {
		s.dropBlobs(ctx, keys)
		return err
	}
	f.State = ""
	f.FillURLs()
	s.TellLocal(ctx, f.ConversationID, "file.changed", map[string]any{"message_id": f.MessageID, "file": f})
	return nil
}

// CreateAdhocShared makes the local copy of an ad-hoc channel that was
// started on another server (docs/LINK-PROTOCOL.md section 16): a private
// channel with no name, already in its stream, holding the accounts given.
// Remote accounts among them have their shadows by now.
func (s *Service) CreateAdhocShared(ctx context.Context, by model.AccountID, members []model.AccountID, key []model.AccountID, stream model.StreamID) (model.Conversation, error) {
	c := model.Conversation{Kind: model.KindChannel, Private: true, MemberKey: store.AdhocMemberKey(key), CreatedByID: by, CreatedAt: s.now(), StreamID: stream}
	err := s.RemoteTx(ctx, func(tx store.Tx, rec *Recorder) error {
		if err := tx.Conversations().Create(ctx, &c); err != nil {
			return err
		}
		for _, id := range members {
			res, err := tx.Members().AddDirect(ctx, c.ID, id, model.LevelMember, c.CreatedAt)
			if err != nil {
				return err
			}
			if err := rec.Membership(ctx, res, by, ""); err != nil {
				return err
			}
		}
		return nil
	})
	return c, err
}

// JoinInvited makes a local account a member of a cross-server ad-hoc
// channel it was invited to by someone on another server. It is an ordinary
// local change, so the other servers hear of it as member.joined.
func (s *Service) JoinInvited(ctx context.Context, conv model.ConversationID, account, by model.AccountID) error {
	return s.Tx(ctx, func(tx store.Tx, rec *Recorder) error {
		n, err := tx.Members().Count(ctx, conv)
		if err != nil {
			return err
		}
		if n >= model.MaxAdhocMembers {
			return model.ErrTooLarge.With("An ad-hoc channel holds at most 50 people.")
		}
		res, err := tx.Members().AddDirect(ctx, conv, account, model.LevelMember, s.now())
		if errors.Is(err, model.ErrAlreadyExists) {
			return nil
		}
		if err != nil {
			return err
		}
		return rec.Membership(ctx, res, by, "")
	})
}

// DropFromShared takes accounts out of a shared conversation as this
// server's own doing: the accounts of a server that declined an offer.
func (s *Service) DropFromShared(ctx context.Context, conv model.Conversation, accounts []model.AccountID) error {
	return s.Tx(ctx, func(tx store.Tx, rec *Recorder) error {
		for _, id := range accounts {
			if err := s.ApplyLeave(ctx, tx, rec, conv, id, "left"); err != nil {
				return err
			}
		}
		return nil
	})
}
