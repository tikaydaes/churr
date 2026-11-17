package chat

import (
	"context"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/perm"
	"github.com/tikaydaes/churr/internal/store"
)

// MoveThread moves a thread out into a new named channel (docs/FEATURES.md
// section 1.6, ruling R20). The parent stays where it is; a copy of it
// starts the new channel; the replies move, keeping their authors, times,
// quotes and reactions; a system reply of kind `thread_moved` under the
// parent points to the new channel, and the old thread takes no more
// replies. It is not undone.
//
// Who may: the parent's author, at level member or above and with a role
// that may create channels, or an account acting as manager. Refused in
// shared channels, sealed chats and archived channels.
func (s *Service) MoveThread(ctx context.Context, actor model.Account, id model.MessageID, name string) (model.Conversation, error) {
	st, p, err := s.load(ctx, actor, id, perm.MoveThread)
	if err != nil {
		return model.Conversation{}, err
	}
	old := st.Conversation
	if p.ParentID != "" || p.IsSystem() {
		return old, model.BadRequest("Only a thread's first message can be moved out, and not a system notice.")
	}
	name, ok := model.CleanChannelName(name)
	if !ok {
		return old, model.Invalid("name", "A channel name is 1 to 64 characters, with no control characters.")
	}
	if moved, err := s.store.Messages().ThreadMovedOut(ctx, p.ID); err != nil {
		return old, err
	} else if moved {
		return old, model.ErrConflict.With("This thread has already been moved to a channel.")
	}
	// Deleted replies are still replies and would move; a thread with none
	// at all has nothing to move.
	if list, _, err := s.store.Messages().Replies(ctx, p.ID, store.SeqPage{Limit: 1}); err != nil {
		return old, err
	} else if len(list) == 0 {
		return old, model.BadRequest("That message has no replies to move.")
	}
	// The copy of the parent gets files of its own, content included. They
	// are written before the transaction, which must not span the file
	// store, and removed again if it fails.
	cards := append([]model.Card(nil), p.Cards...)
	var copies []model.File
	var written []string
	if !p.Deleted() {
		attached, err := s.store.Files().ForMessage(ctx, p.ID, false)
		if err != nil {
			return old, err
		}
		images, err := s.store.Files().CardImages(ctx, p.ID)
		if err != nil {
			return old, err
		}
		renamed := map[model.FileID]model.FileID{}
		for _, f := range append(attached, images...) {
			if !f.DetachedAt.IsZero() || f.BlobKey == "" {
				continue
			}
			c, err := s.copyFile(ctx, f)
			if err != nil {
				s.dropBlobs(ctx, written)
				return old, err
			}
			renamed[f.ID] = c.ID
			copies = append(copies, c)
			written = append(written, store.BlobKeys(c)...)
		}
		for i := range cards {
			if to, ok := renamed[cards[i].ImageFileID]; ok {
				cards[i].ImageFileID = to
			}
		}
	}

	// Private the same as the old channel, so a thread from an ad-hoc
	// channel gives a private channel; section and retention copied. It is
	// a sub-channel of the channel the thread was in, or its sibling if
	// that is itself a sub-channel; out of an ad-hoc channel it has no
	// parent (ruling R49). The name may be one another channel has (R50).
	channel := model.Conversation{Kind: model.KindChannel, Name: name, Private: old.Private, SectionID: old.SectionID,
		ParentID:         perm.SubChannelParent(old),
		RetentionSeconds: old.RetentionSeconds, DefaultLevel: model.LevelMember, CreatedByID: actor.ID, CreatedAt: s.now()}
	var res store.ThreadMoved
	err = s.Tx(ctx, func(tx store.Tx, rec *Recorder) error {
		seq, err := rec.Seq(ctx, old.ID)
		if err != nil {
			return err
		}
		res, err = tx.Messages().MoveThread(ctx, store.ThreadMove{ParentID: p.ID, Channel: channel, By: actor.ID, PointerSeq: seq,
			CopyID: model.NewID[model.MessageID](), CopyCards: cards, CopyFiles: copies, At: channel.CreatedAt})
		if err != nil {
			return err
		}
		parent, err := s.neutral(ctx, tx, old, res.Parent)
		if err != nil {
			return err
		}
		pointer, err := s.neutral(ctx, tx, old, res.Pointer)
		if err != nil {
			return err
		}
		// One event in the old channel, under the same seq as the pointer:
		// clients drop the replies they hold and show the pointer. It
		// carries the parent, so it is the parent's event: a member who
		// joined a private channel after the parent was posted is not
		// sent it (ruling R87, Hidden), and erasing the parent erases it.
		if err := rec.EmitAt(ctx, model.Event{Type: model.EventThreadMoved, At: channel.CreatedAt, ConversationID: old.ID, Seq: seq,
			MessageID: res.Parent.ID, Payload: map[string]any{"parent": parent, "to_conversation_id": res.Channel.ID,
				"pointer": pointer, "by_id": actor.ID}}); err != nil {
			return err
		}
		// member.joined in the new channel for each first member, each
		// with the conversation beside it. No join notices are posted:
		// they are the people it starts with.
		rec.Founding()
		if err := rec.Membership(ctx, res.Members, actor.ID, ""); err != nil {
			return err
		}
		if actor.ID != p.AuthorID {
			return s.audit(ctx, tx, old, actor.ID, "thread.moved", "message", string(p.ID),
				map[string]any{"from_conversation_id": old.ID, "to_conversation_id": res.Channel.ID, "replies": len(res.Moved)})
		}
		return nil
	})
	if err != nil {
		s.dropBlobs(ctx, written)
		return old, err
	}
	return s.afterManage(ctx, actor, res.Channel)
}
