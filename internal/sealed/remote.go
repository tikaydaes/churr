package sealed

import (
	"context"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

// Sealed chats across servers (docs/SEALED.md section 17). The stream's
// sequencer assigns the numbers and cannot see what it is numbering, so
// each server applies the rules of section 5.5 itself when it stores an
// event: the first record for a key epoch stands and later ones are
// dropped; a message whose key epoch is not current at its place in the
// sequence is dropped. Both servers see the same sequence and so reach the
// same result.
//
// The link package owns the stream and its sequence numbers. These two
// checks are what it asks before it stores a sealed event that arrived, in
// the transaction that stores it. Device lists cross the link through
// StoreRemoteList and RemoteLists (lists.go) and Outbound.

// CheckRemoteEpoch says whether an epoch record that arrived over a link
// is the chat's next: it passes every check a locally posted record does
// (section 5.5), read from the device lists this server holds for the two
// accounts. If so it returns the row to store, without its sequence
// number, which the caller gives it before calling Sealed().PutEpoch.
//
// drop is true for a record that must be ignored without telling anyone:
// one for a key epoch that already has a record, or that is not the next
// one, or that fails a check. An error is a failure of this server.
func (s *Service) CheckRemoteEpoch(ctx context.Context, a store.Areas, conv model.ConversationID, record []byte) (row model.EpochRecord, drop bool, err error) {
	return s.checkRemoteEpoch(ctx, a, conv, record, -1)
}

// CheckRemoteEpochFor is CheckRemoteEpoch for a record made for a key epoch
// that already has one: it passes if it would have been the chat's next
// record when the chat stood at the epoch before. The link package asks
// when two devices on two servers made the same key epoch at the same
// moment and the one that arrived second carries the earlier stamp, which
// is the one that stands (docs/LINK-PROTOCOL.md section 11).
func (s *Service) CheckRemoteEpochFor(ctx context.Context, a store.Areas, conv model.ConversationID, record []byte, keyEpoch int64) (row model.EpochRecord, drop bool, err error) {
	if keyEpoch < 2 {
		return row, true, nil // a chat's first record is its starter's, and never replaced
	}
	return s.checkRemoteEpoch(ctx, a, conv, record, keyEpoch-1)
}

// checkRemoteEpoch checks a record against the chat standing at a key
// epoch: its current one when standing is negative.
func (s *Service) checkRemoteEpoch(ctx context.Context, a store.Areas, conv model.ConversationID, record []byte, standing int64) (row model.EpochRecord, drop bool, err error) {
	c, err := a.Conversations().Get(ctx, conv)
	if err != nil {
		return row, true, err
	}
	members, err := a.Members().IDs(ctx, conv)
	if err != nil {
		return row, true, err
	}
	if !c.Sealed() || len(members) != 2 {
		return row, true, nil
	}
	parsed, perr := parseEpoch(record)
	if perr != nil {
		return row, true, nil
	}
	current := int64(0)
	if c.KeyEpoch != nil {
		current = *c.KeyEpoch
	}
	if standing >= 0 {
		if standing >= current {
			return row, true, nil // not an epoch this chat has passed
		}
		current = standing
	}
	e, cerr := s.checkEpoch(ctx, a, conv, [2]model.AccountID{members[0], members[1]}, current, int64(parsed.KeyEpoch), record)
	if cerr != nil {
		if _, ours := model.AsError(cerr); ours {
			return row, true, nil
		}
		return row, true, cerr
	}
	return model.EpochRecord{ConversationID: conv, KeyEpoch: int64(e.KeyEpoch), AccountID: e.MakerAccountID, DeviceID: e.MakerDeviceID,
		Record: record, CreatedAt: s.now()}, false, nil
}

// CheckRemoteMessage says whether a sealed message that arrived over a
// link is stored or dropped: its header must name this chat and its
// author, agree with the key epoch and quiet flag beside it, and its key
// epoch must be the chat's current one at this point in the sequence. The
// stale-epoch rule is not applied here: it holds back a server's own
// senders, and both servers must reach the same result from the sequence
// alone.
func (s *Service) CheckRemoteMessage(ctx context.Context, a store.Areas, conv model.ConversationID, author model.AccountID, keyEpoch int64, quiet bool, ciphertext []byte) (drop bool, err error) {
	c, err := a.Conversations().Get(ctx, conv)
	if err != nil {
		return true, err
	}
	if !c.Sealed() || c.KeyEpoch == nil || *c.KeyEpoch != keyEpoch || len(ciphertext) > maxCiphertext {
		return true, nil
	}
	h, perr := parseHeader(ciphertext)
	if perr != nil || h.ConversationID != conv || h.AccountID != author || int64(h.KeyEpoch) != keyEpoch || h.Quiet != quiet {
		return true, nil
	}
	return false, nil
}
