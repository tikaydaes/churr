package chat

import (
	"context"
	"errors"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

// AttachStreamIn is AttachStream inside a transaction the caller already
// runs. The link uses it when a stream ends for this server, so that the
// stream's state and what becomes of the conversation are one change: nobody
// can read a stream that is over beside a conversation that still says it is
// shared.
func (s *Service) AttachStreamIn(ctx context.Context, tx store.Tx, rec *Recorder, conv model.ConversationID, stream model.StreamID, archive bool) error {
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
}
