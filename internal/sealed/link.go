package sealed

import (
	"context"
	"errors"

	"github.com/tikaydaes/churr/internal/chat"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

// This file is the receiving half of starting a sealed chat across servers
// (docs/SEALED.md section 17, docs/LINK-PROTOCOL.md section 16): what the
// link package asks when another server offers a sealed chat with one of
// this server's accounts.

// CheckRemoteStart is this server's checks on a sealed chat someone on
// another server wants to start with one of its accounts: sealed chats are
// on here, the account is a person who can act and not a bot, and the pair
// has no sealed chat already. The rule about shared channels and blocks is
// the link's to check. The error says which: feature_off, or forbidden.
func (s *Service) CheckRemoteStart(ctx context.Context, starter, local model.Account) error {
	if err := s.enabled(ctx); err != nil {
		return err
	}
	if !local.Active() || local.Role == model.RoleBot || starter.Role == model.RoleBot || starter.Local() || starter.Removed() {
		return model.ErrForbidden.With("A sealed chat is between two people.")
	}
	if _, err := s.store.Conversations().ByMemberKey(ctx, store.SealedMemberKey(starter.ID, local.ID)); err == nil {
		return model.ErrForbidden.With("Those two people already have a sealed chat.")
	} else if !errors.Is(err, model.ErrNotFound) {
		return err
	}
	return nil
}

// AcceptRemote makes this server's copy of a sealed chat that was started
// on another server: the same conversation (its identifier is inside epoch
// record 1, under the maker's signature), the two members, and that record
// as key epoch 1. The record passes every check a local one does, read from
// the device lists the offer brought, which the caller has stored. The
// chat is in its stream from the start.
//
// The record is stored under this conversation's next number for now. The
// stream carries it again as its first sealed event, and the link then
// moves it to that event's number, so both servers hold it under one seq.
func (s *Service) AcceptRemote(ctx context.Context, starter, local model.Account, stream model.StreamID, record []byte) (model.Conversation, error) {
	none := model.Conversation{}
	if err := s.CheckRemoteStart(ctx, starter, local); err != nil {
		return none, err
	}
	parsed, err := parseEpoch(record)
	if err != nil || !isUUIDv7(string(parsed.ConversationID)) {
		return none, model.Invalid("epoch_record", "The epoch record is not in its one valid form.")
	}
	if _, err := s.store.Conversations().Get(ctx, parsed.ConversationID); err == nil {
		return none, model.Invalid("epoch_record", "That conversation identifier is in use.")
	} else if !errors.Is(err, model.ErrNotFound) {
		return none, err
	}
	conv := model.Conversation{ID: parsed.ConversationID, Kind: model.KindSealed, Private: true, MemberKey: store.SealedMemberKey(starter.ID, local.ID),
		CreatedByID: starter.ID, CreatedAt: s.now(), StreamID: stream}
	err = s.chat.RemoteTx(ctx, func(tx store.Tx, rec *chat.Recorder) error {
		if err := tx.Conversations().Create(ctx, &conv); err != nil {
			return err
		}
		for _, id := range []model.AccountID{starter.ID, local.ID} {
			res, err := tx.Members().AddDirect(ctx, conv.ID, id, model.LevelMember, conv.CreatedAt)
			if err != nil {
				return err
			}
			if err := rec.Membership(ctx, res, starter.ID, ""); err != nil {
				return err
			}
		}
		e, err := s.checkEpoch(ctx, tx, conv.ID, [2]model.AccountID{starter.ID, local.ID}, 0, 1, record)
		if err != nil {
			return err
		}
		_, err = s.storeEpoch(ctx, tx, rec, conv.ID, e, record)
		return err
	})
	return conv, err
}
