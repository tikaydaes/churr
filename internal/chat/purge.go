package chat

import (
	"context"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/perm"
	"github.com/tikaydaes/churr/internal/store"
)

// Purging erases for good (docs/FEATURES.md section 5). For a message that
// is not deleted it erases the history and leaves the message as it now
// reads; for a deleted message it erases everything and leaves a bare
// record. "Erased" means gone from the database, the search index, the
// events log and the file store (ruling R5).

// The two answers of Purge.
const (
	PurgeDone      = "purged"
	PurgeRequested = "requested"
)

// purgeTx carries out a purge of one message inside a transaction. With
// announce it emits message.purged; an admin's later decision on a request
// is not announced, because to everyone but admins hidden and erased look
// the same.
func (s *Service) purgeTx(ctx context.Context, tx store.Tx, rec *Recorder, conv model.Conversation, id model.MessageID) (model.Message, error) {
	return s.purge(ctx, tx, rec, conv, id, true)
}

func (s *Service) purge(ctx context.Context, tx store.Tx, rec *Recorder, conv model.Conversation, id model.MessageID, announce bool) (model.Message, error) {
	keys, err := tx.Messages().Purge(ctx, id, s.now())
	if err != nil {
		return model.Message{}, err
	}
	rec.RemoveBlobs(keys...)
	m, err := tx.Messages().Get(ctx, id)
	if err != nil {
		return m, err
	}
	out, err := s.neutral(ctx, tx, conv, m)
	if err != nil || !announce {
		return out, err
	}
	out.HasHistory = false
	_, err = rec.Emit(ctx, model.Event{Type: model.EventMessagePurged, ConversationID: m.ConversationID, MessageID: id,
		Payload: map[string]any{"message": out, "erased": m.Deleted()}})
	return out, err
}

// Purge purges a message's history, or asks for it. The author's request is
// carried out at once in a private or ad-hoc channel; in a public channel
// it hides the history from everyone but admins and waits for one of them.
// An admin's own call deletes and purges in one step. state is PurgeDone or
// PurgeRequested.
func (s *Service) Purge(ctx context.Context, actor model.Account, id model.MessageID) (state string, out model.Message, err error) {
	st, m, err := s.load(ctx, actor, id, perm.ReadMessage)
	if err != nil {
		return "", m, err
	}
	if m.AuthorID != actor.ID && perm.CheckMessage(st, perm.PurgeDirectly, m) == nil {
		// An admin, for someone else's message. For their own message an
		// admin is its author like any other, so asking to purge its
		// history never deletes it; delete-and-purge is DeleteAndPurge.
		out, err := s.DeleteAndPurge(ctx, actor, id)
		return PurgeDone, out, err
	}
	if err := perm.CheckMessage(st, perm.AskPurge, m); err != nil {
		return "", m, err
	}
	conv := st.Conversation
	// There must be something to purge: the message is deleted and not
	// yet a bare record, or it has history.
	versions, err := s.store.Messages().Versions(ctx, id)
	if err != nil {
		return "", m, err
	}
	if len(versions) == 0 && !(m.Deleted() && m.PurgedAt.IsZero()) {
		return "", m, model.ErrConflict.With("There is nothing to purge: the message has no history.")
	}
	if !conv.Public() {
		// Carried out in the same request. No purge_requests row, no audit
		// row, no event to anyone outside the channel.
		err = s.Tx(ctx, func(tx store.Tx, rec *Recorder) error {
			_, err := s.purgeTx(ctx, tx, rec, conv, id)
			return err
		})
		if err != nil {
			return "", m, err
		}
		out, err = s.Message(ctx, actor, id)
		return PurgeDone, out, err
	}

	latest, found, err := s.store.Moderation().LatestPurgeRequest(ctx, id)
	if err != nil {
		return "", m, err
	}
	switch {
	case found && latest.Waiting():
		// Asking twice while a request waits changes nothing.
		out, err = s.Message(ctx, actor, id)
		return PurgeRequested, out, err
	case found && latest.Outcome == model.PurgeShown && !m.EditedAt.After(latest.DecidedAt) && !m.DeletedAt.After(latest.DecidedAt):
		// Otherwise an author could overrule the admin by asking again
		// (ruling R38). The sentence does not say what was decided.
		return "", m, model.ErrConflict.With("A purge of this message has already been asked for. It can be asked again once the message changes.")
	}
	req := model.PurgeRequest{MessageID: id, RequestedByID: actor.ID, RequestedAt: s.now()}
	err = s.Tx(ctx, func(tx store.Tx, rec *Recorder) error {
		// From this moment only admins can open the history.
		if err := tx.Messages().SetHistoryHidden(ctx, id, req.RequestedAt); err != nil {
			return err
		}
		if err := tx.Moderation().CreatePurgeRequest(ctx, &req); err != nil {
			return err
		}
		cur, err := tx.Messages().Get(ctx, id)
		if err != nil {
			return err
		}
		shown, err := s.neutral(ctx, tx, conv, cur)
		if err != nil {
			return err
		}
		shown.HasHistory = false
		if _, err := rec.Emit(ctx, model.Event{Type: model.EventMessagePurged, ConversationID: m.ConversationID, MessageID: id,
			Payload: map[string]any{"message": shown, "erased": false}}); err != nil {
			return err
		}
		to, err := admins(ctx, tx)
		if err != nil {
			return err
		}
		rec.After(func() { s.tell(to, model.EventPurgeRequestCreated, map[string]any{"purge_request": req}) })
		return nil
	})
	if err != nil {
		return "", m, err
	}
	out, err = s.Message(ctx, actor, id)
	return PurgeRequested, out, err
}

// DeleteAndPurge is an admin's direct purge (section 5.3): an ordinary
// delete, if the message is not already deleted, followed by the purge,
// with one audit row. No purge request is written; one that waits for the
// message is marked erased.
func (s *Service) DeleteAndPurge(ctx context.Context, actor model.Account, id model.MessageID) (model.Message, error) {
	st, m, err := s.load(ctx, actor, id, perm.PurgeDirectly)
	if err != nil {
		return m, err
	}
	conv := st.Conversation
	now := s.now()
	err = s.Tx(ctx, func(tx store.Tx, rec *Recorder) error {
		cur, err := tx.Messages().Get(ctx, id)
		if err != nil {
			return err
		}
		if !cur.Deleted() {
			deleted, err := tx.Messages().Delete(ctx, id, actor.ID, now)
			if err != nil {
				return err
			}
			marker, err := s.neutral(ctx, tx, conv, deleted)
			if err != nil {
				return err
			}
			if _, err := rec.Emit(ctx, model.Event{Type: model.EventMessageDeleted, ConversationID: m.ConversationID, MessageID: id,
				Payload: map[string]any{"message": marker}}); err != nil {
				return err
			}
		}
		if _, err := s.purgeTx(ctx, tx, rec, conv, id); err != nil {
			return err
		}
		if latest, found, err := tx.Moderation().LatestPurgeRequest(ctx, id); err != nil {
			return err
		} else if found && latest.Waiting() {
			decided, err := tx.Moderation().DecidePurgeRequest(ctx, latest.ID, model.PurgeErased, actor.ID, now)
			if err != nil {
				return err
			}
			to, err := admins(ctx, tx)
			if err != nil {
				return err
			}
			rec.After(func() { s.tell(to, model.EventPurgeRequestDecided, map[string]any{"purge_request": decided}) })
		}
		return s.audit(ctx, tx, conv, actor.ID, "message.purged", "message", string(id),
			map[string]any{"conversation_id": m.ConversationID, "author_id": m.AuthorID})
	})
	if err != nil {
		return m, err
	}
	return s.Message(ctx, actor, id)
}

// PurgeRequests lists purge requests for the admins, each with its message.
func (s *Service) PurgeRequests(ctx context.Context, actor model.Account, waiting bool, p store.Page) ([]model.PurgeRequest, string, error) {
	if err := s.perm.CanAdminister(ctx, actor, perm.DecidePurgeRequests); err != nil {
		return nil, "", err
	}
	list, next, err := s.store.Moderation().PurgeRequests(ctx, waiting, p)
	if err != nil {
		return nil, "", err
	}
	for i := range list {
		// A request whose channel has since gone private is still listed,
		// as the record, without content the admin can no longer read.
		if m, err := s.Message(ctx, actor, list[i].MessageID); err == nil {
			list[i].Message = &m
		}
	}
	return list, next, nil
}

// DecidePurgeRequest is an admin's decision on a waiting request: show the
// history again, keep it hidden, or erase it. The author is not told which.
func (s *Service) DecidePurgeRequest(ctx context.Context, actor model.Account, id, outcome string) (model.PurgeRequest, error) {
	if err := s.perm.CanAdminister(ctx, actor, perm.DecidePurgeRequests); err != nil {
		return model.PurgeRequest{}, err
	}
	switch outcome {
	case model.PurgeShown, model.PurgeHidden, model.PurgeErased:
	default:
		return model.PurgeRequest{}, model.Invalid("outcome", "An outcome is shown, hidden or erased.")
	}
	req, err := s.store.Moderation().GetPurgeRequest(ctx, id)
	if err != nil {
		return req, hide(err)
	}
	if !req.Waiting() {
		return req, model.ErrConflict.With("That request has already been decided.")
	}
	m, err := s.store.Messages().Get(ctx, req.MessageID)
	if err != nil {
		return req, hide(err)
	}
	conv, err := s.store.Conversations().Get(ctx, m.ConversationID)
	if err != nil {
		return req, hide(err)
	}
	now := s.now()
	err = s.Tx(ctx, func(tx store.Tx, rec *Recorder) error {
		var err error
		if req, err = tx.Moderation().DecidePurgeRequest(ctx, id, outcome, actor.ID, now); err != nil {
			return err
		}
		switch outcome {
		case model.PurgeShown:
			// Everyone who can read the channel can open the history
			// again.
			if err := tx.Messages().SetHistoryHidden(ctx, m.ID, 0); err != nil {
				return err
			}
			cur, err := tx.Messages().Get(ctx, m.ID)
			if err != nil {
				return err
			}
			shown, err := s.neutral(ctx, tx, conv, cur)
			if err != nil {
				return err
			}
			if _, err := rec.Emit(ctx, model.Event{Type: model.EventMessageHistoryRestored, ConversationID: m.ConversationID,
				MessageID: m.ID, Payload: map[string]any{"message": shown}}); err != nil {
				return err
			}
		case model.PurgeErased:
			// Not announced: the channel was told when the history was
			// hidden, and must not learn what the admin chose.
			if _, err := s.purge(ctx, tx, rec, conv, m.ID, false); err != nil {
				return err
			}
		}
		if err := s.audit(ctx, tx, conv, actor.ID, "purge_request.decided", "message", string(m.ID),
			map[string]any{"conversation_id": m.ConversationID, "outcome": outcome, "request_id": id}); err != nil {
			return err
		}
		to, err := admins(ctx, tx)
		if err != nil {
			return err
		}
		decided := req
		rec.After(func() { s.tell(to, model.EventPurgeRequestDecided, map[string]any{"purge_request": decided}) })
		return nil
	})
	if err != nil {
		return req, err
	}
	if msg, err := s.Message(ctx, actor, m.ID); err == nil {
		req.Message = &msg
	}
	return req, nil
}
