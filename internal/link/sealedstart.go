package link

import (
	"context"
	"encoding/json"

	"github.com/tikaydaes/churr/internal/link/wire"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

// This file is starting a sealed chat across servers (section 16,
// "Starting a sealed chat"; docs/SEALED.md section 17).
//
// The starter's device has made epoch record 1, sealed to every device on
// both accounts' lists, and the sealed package has made the chat with it.
// This server opens a stream of kind sealed over the peer of the channel
// the two share and offers it to the other person's server with the
// starter's device lists and the record. That server checks, makes its copy
// of the chat under the same identifier with the record as key epoch 1, and
// accepts. Once it is in the stream, the record is submitted as
// sealed.epoch_started, and both servers move it to that event's seq.

// deviceListsBody is device lists as a device_list message and an offer
// carry them.
type deviceListsBody struct {
	AccountID model.AccountID `json:"account_id"`
	Versions  []listVersion   `json:"versions"`
}

// personalOffer builds what an ad-hoc or sealed offer adds, from what this
// server holds: who started it, which of the offered server's accounts are
// in it, every member, and for a sealed chat the starter's device lists and
// epoch record 1. by is the account that is adding, or nil for whoever made
// the conversation.
func (s *Service) personalOffer(ctx context.Context, st store.Stream, to store.Server, by *model.Account) map[string]any {
	conv, err := s.store.Conversations().Get(ctx, st.ConversationID)
	if err != nil {
		return nil
	}
	if by == nil {
		acc, err := s.store.Accounts().Get(ctx, conv.CreatedByID)
		if err != nil {
			return nil
		}
		by = &acc
	}
	profiles, remote, err := s.memberProfiles(ctx, conv.ID)
	if err != nil || len(remote[to.ID]) == 0 {
		return nil
	}
	me, _ := s.profileFor(ctx, *by)
	me.AvatarFileID = ""
	extra := map[string]any{"by_account": me, "account_ids": remote[to.ID], "members": profiles}
	if st.Kind != store.StreamSealed {
		return extra
	}
	sealed := s.sealedStore()
	if sealed == nil {
		return nil
	}
	first, found, err := s.store.Sealed().Epoch(ctx, conv.ID, 1)
	if err != nil || !found {
		return nil
	}
	lists, err := sealed.RemoteLists(ctx, by.ID, 0)
	if err != nil {
		return nil
	}
	body := deviceListsBody{AccountID: by.ID, Versions: []listVersion{}}
	for _, l := range lists {
		body.Versions = append(body.Versions, listVersion{Version: l.Version, SignedList: l.Signed})
	}
	extra["device_lists"], extra["epoch_record"] = body, wire.Bytes(first.Record)
	return extra
}

// StartSealed implements sealed.Outbound.
func (s *Service) StartSealed(ctx context.Context, by model.Account, conv model.Conversation, other model.Account, record []byte) error {
	st, servers, err := s.openPersonal(ctx, by.ID, conv, store.StreamSealed, []model.AccountID{other.ID})
	if err != nil {
		return err
	}
	for _, sv := range servers {
		if extra := s.personalOffer(ctx, st, sv, &by); extra != nil {
			s.sendOffer(ctx, st, sv, extra)
		}
	}
	return nil
}

// checkSealedOffer is the checks a sealed offer needs beyond those of every
// personal offer: sealed chats are on here, the account is not a bot, and
// the two have no sealed chat already.
func (s *Service) checkSealedOffer(ctx context.Context, st store.Stream, from store.Server, by model.Account, o offerBody) string {
	sealed := s.sealedStore()
	if sealed == nil {
		return "feature_off"
	}
	if len(o.AccountIDs) != 1 || len(o.Members) != 2 || len(o.EpochRecord) == 0 || by.ServerID != from.ID {
		return "not_allowed"
	}
	local, err := s.store.Accounts().Get(ctx, o.AccountIDs[0])
	if err != nil {
		return "not_allowed"
	}
	if err := sealed.CheckRemoteStart(ctx, by, local); err != nil {
		if model.CodeOf(err) == model.CodeFeatureOff {
			return "feature_off"
		}
		return "not_allowed"
	}
	return ""
}

// acceptSealed makes this server's copy of an offered sealed chat: the
// device lists the offer carries are stored against the starter's shadow
// account, and the sealed package makes the chat with the record as key
// epoch 1.
func (s *Service) acceptSealed(ctx context.Context, st store.Stream, o offerBody) (model.Conversation, error) {
	sealed := s.sealedStore()
	if sealed == nil {
		return model.Conversation{}, model.ErrFeatureOff
	}
	by, err := s.store.Accounts().Get(ctx, o.ByAccount.ID)
	if err != nil {
		return model.Conversation{}, err
	}
	local, err := s.store.Accounts().Get(ctx, o.AccountIDs[0])
	if err != nil {
		return model.Conversation{}, err
	}
	var lists deviceListsBody
	if len(o.DeviceLists) > 0 {
		if err := json.Unmarshal(o.DeviceLists, &lists); err != nil || lists.AccountID != by.ID {
			return model.Conversation{}, model.Invalid("device_lists", "The offer's device lists are not the starter's.")
		}
	}
	for _, v := range lists.Versions {
		// A list is signed by the account's own key; the sealed package
		// checks each version against the one before.
		if err := sealed.StoreRemoteList(ctx, by.ID, v.SignedList); err != nil {
			return model.Conversation{}, err
		}
	}
	return sealed.AcceptRemote(ctx, by, local, st.ID, o.EpochRecord)
}

// announceEpochs submits a sealed chat's epoch records to its stream once
// the other server is in it: the same record 1 the offer carried, as the
// stream's first sealed event, and any made since. Each server already
// holds them, and moves each to the seq this gives it.
func (s *Service) announceEpochs(ctx context.Context, id model.StreamID) {
	st, err := s.store.Streams().Get(ctx, id)
	if err != nil || st.ConversationID == "" || !st.Owned() {
		return
	}
	rows, _, err := s.store.Sealed().Epochs(ctx, st.ConversationID, 1, 100)
	if err != nil {
		return
	}
	for _, row := range rows {
		// The row does not say whose device made the record. A device
		// this server knows is one of its own accounts'; any other
		// record is the other server's to announce.
		dev, err := s.store.Devices().Get(ctx, row.DeviceID)
		if err != nil {
			continue
		}
		fields := map[string]any{"key_epoch": row.KeyEpoch, "account_id": dev.AccountID, "device_id": row.DeviceID, "record": wire.Bytes(row.Record)}
		if err := s.enqueue(ctx, st, model.EventSealedEpochStarted, fields); err != nil {
			s.log.Warn("link: a sealed chat's epoch record could not be submitted", "stream", id, "err", err)
			return
		}
	}
}

// beforeSealedEpoch runs before a sealed.epoch_started from another server
// is checked. The record names a version of its maker's device list, and
// that version travels by another road (a device_list message) which may be
// slower. So the maker's home server is asked for the versions this server
// lacks, and the record is checked against the same lists on every server.
// It runs outside the transaction that applies the event.
func (s *Service) beforeSealedEpoch(ctx context.Context, st store.Stream, env wire.Envelope, in inner) {
	if st.Kind != store.StreamSealed || in.Type != model.EventSealedEpochStarted || env.Origin == s.id.Name || st.ConversationID == "" {
		return
	}
	var b struct {
		AccountID model.AccountID `json:"account_id"`
	}
	if json.Unmarshal(in.raw, &b) != nil {
		return
	}
	acc, err := s.store.Accounts().Get(ctx, b.AccountID)
	if err != nil || acc.Local() {
		return
	}
	ids, err := s.store.Members().IDs(ctx, st.ConversationID)
	if err != nil {
		return
	}
	after := int64(0)
	if newest, found, err := s.store.Sealed().NewestDeviceList(ctx, b.AccountID); err == nil && found {
		after = newest.Version
	}
	for _, id := range ids {
		if id != b.AccountID && isLocal(ctx, s.store, id) {
			if err := s.RequestDeviceLists(ctx, b.AccountID, id, after); err != nil {
				s.log.Debug("link: a device list could not be fetched ahead of an epoch record", "stream", st.ID, "err", err)
			}
			return
		}
	}
}
