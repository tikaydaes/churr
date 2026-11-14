package sealed

import (
	"context"
	"errors"

	"github.com/tikaydaes/churr/internal/chat"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/perm"
	"github.com/tikaydaes/churr/internal/store"
)

// Sealed chats and their key epochs (docs/SEALED.md sections 5.5 and 6).

// presentEpoch fills in the maker's account, which is inside the record.
func presentEpoch(e model.EpochRecord) model.EpochRecord {
	if parsed, err := parseEpoch(e.Record); err == nil {
		e.AccountID = parsed.MakerAccountID
	}
	return e
}

// checkEpoch makes the server's checks on a chat's next epoch record
// (section 5.5), all but "the poster is a device of one of the two
// accounts", which the caller has made. conv is the chat's identifier,
// members its two accounts, current its current key epoch (0 for none).
//
// A record for a key epoch that is not the next one is model.ErrConflict;
// anything wrong with the record itself is invalid_value for `record`.
func (s *Service) checkEpoch(ctx context.Context, a store.Areas, conv model.ConversationID, members [2]model.AccountID, current, keyEpoch int64, stored []byte) (epochRecord, error) {
	bad := func(why string) (epochRecord, error) { return epochRecord{}, model.Invalid("record", why) }
	e, err := parseEpoch(stored)
	if err != nil {
		return bad("The epoch record is not in its one valid form.")
	}
	if e.ConversationID != conv {
		return bad("The epoch record is for another chat.")
	}
	if int64(e.KeyEpoch) != keyEpoch {
		return bad("key_epoch is not the key epoch inside the record.")
	}
	if keyEpoch != current+1 {
		return e, model.ErrConflict.With("That is not this chat's next key epoch.")
	}
	if members[1] < members[0] {
		members[0], members[1] = members[1], members[0]
	}
	if e.Accounts[0].AccountID != members[0] || e.Accounts[1].AccountID != members[1] {
		return bad("The epoch record must name the chat's two accounts.")
	}
	var before *epochRecord
	if current > 0 {
		row, found, err := a.Sealed().Epoch(ctx, conv, current)
		if err != nil {
			return e, err
		}
		if found {
			if e.Prev != epochHash(row.Record) {
				return bad("prev is not the hash of the chat's current epoch record.")
			}
			if p, err := parseEpoch(row.Record); err == nil {
				before = &p
			}
		}
	} else if e.Prev != [32]byte{} {
		return bad("Epoch 1 has no previous record.")
	}
	// The device set is not chosen: it is every device of the two list
	// versions the record names.
	type listed struct {
		account model.AccountID
		signKey []byte
	}
	devices := map[model.DeviceID]listed{}
	for i, ref := range e.Accounts {
		if before != nil && ref.ListVersion < before.Accounts[i].ListVersion {
			return bad("A device list version is lower than in the previous epoch record.")
		}
		if ref.ListVersion == 0 {
			if ref.ListHash != [32]byte{} {
				return bad("An account with no device list has no list hash.")
			}
			continue
		}
		row, found, err := a.Sealed().DeviceList(ctx, ref.AccountID, int64(ref.ListVersion))
		if err != nil {
			return e, err
		}
		if !found {
			return bad("The epoch record names a device list version this server does not hold.")
		}
		signed, err := parseSignedList(row.Signed)
		if err != nil {
			return e, err
		}
		if hash(labelListHash, signed.List) != ref.ListHash {
			return bad("A list hash is not the hash of that device list version.")
		}
		l, err := parseDeviceList(signed.List)
		if err != nil {
			return e, err
		}
		for _, d := range l.Devices {
			devices[d.ID] = listed{ref.AccountID, d.SignKey}
		}
	}
	if len(e.Recipients) != len(devices) {
		return bad("The recipients must be exactly the devices of the two device lists.")
	}
	for _, r := range e.Recipients {
		if d, ok := devices[r.DeviceID]; !ok || d.account != r.AccountID {
			return bad("The recipients must be exactly the devices of the two device lists.")
		}
	}
	maker, ok := devices[e.MakerDeviceID]
	if !ok || maker.account != e.MakerAccountID {
		return bad("The maker must be one of the recipients.")
	}
	if !verify(maker.signKey, labelEpoch, e.Record, e.Sig) {
		return bad("The maker's signature does not verify.")
	}
	return e, nil
}

// storeEpoch stores a checked record as the chat's next key epoch, with the
// conversation's next sequence number, and queues sealed.epoch_started.
func (s *Service) storeEpoch(ctx context.Context, tx store.Tx, rec *chat.Recorder, conv model.ConversationID, e epochRecord, stored []byte) (model.EpochRecord, error) {
	seq, err := rec.Seq(ctx, conv)
	if err != nil {
		return model.EpochRecord{}, err
	}
	row := model.EpochRecord{ConversationID: conv, KeyEpoch: int64(e.KeyEpoch), Seq: seq, AccountID: e.MakerAccountID,
		DeviceID: e.MakerDeviceID, Record: stored, CreatedAt: s.now()}
	if err := tx.Sealed().PutEpoch(ctx, row); err != nil {
		return row, err
	}
	return row, rec.EmitAt(ctx, model.Event{Type: model.EventSealedEpochStarted, At: row.CreatedAt, ConversationID: conv, Seq: seq,
		Payload: map[string]any{"epoch": row}})
}

// isUUIDv7 reports whether an identifier is a well-formed UUID of version
// 7: the version digit is 7 and the variant bits are 10.
func isUUIDv7(id string) bool {
	return model.ValidID(id) && id[14] == '7' && (id[19] == '8' || id[19] == '9' || id[19] == 'a' || id[19] == 'b')
}

// Open finds or creates the sealed chat between the caller and one other
// account (section 6). created says which.
//
// There is one sealed chat per pair. If it exists it is returned and the
// record sent is ignored. Otherwise it is created, and record becomes its
// epoch record 1, so the chat can be written in at once.
//
// Epoch record 1 carries the conversation's identifier under the maker's
// signature, so it cannot be made before the identifier is known. The
// creating device therefore makes the identifier itself, a UUIDv7, and the
// server accepts it if it is well formed and not in use. This is the one
// stated exception to "the server makes every identifier".
func (s *Service) Open(ctx context.Context, c Caller, otherID model.AccountID, record []byte) (model.Conversation, bool, error) {
	none := model.Conversation{}
	if err := person(c); err != nil {
		return none, false, err
	}
	other, err := s.store.Accounts().Get(ctx, otherID)
	if err != nil {
		return none, false, err
	}
	// An account the caller may not see does not exist for them.
	if err := s.perm.CanSeeAccount(ctx, c.Account, other); err != nil {
		return none, false, err
	}
	key := store.SealedMemberKey(c.Account.ID, other.ID)
	existing, err := s.store.Conversations().ByMemberKey(ctx, key)
	switch {
	case err == nil:
		out, err := s.chat.Conversation(ctx, c.Account, existing.ID)
		return out, false, err
	case !errors.Is(err, model.ErrNotFound):
		return none, false, err
	}
	if err := s.perm.CanStartSealed(ctx, c.Account, other); err != nil {
		return none, false, err
	}
	if _, err := s.device(ctx, s.store, c); err != nil {
		return none, false, err
	}
	if !other.Local() && s.outbound == nil {
		return none, false, model.ErrForbidden.With("This server is not linked to that person's.")
	}
	parsed, err := parseEpoch(record)
	if err != nil {
		return none, false, model.Invalid("epoch_record", "The epoch record is not in its one valid form.")
	}
	if !isUUIDv7(string(parsed.ConversationID)) {
		return none, false, model.Invalid("epoch_record", "The epoch record must name the new chat's identifier, a UUIDv7 its device made.")
	}
	if _, err := s.store.Conversations().Get(ctx, parsed.ConversationID); err == nil {
		return none, false, model.Invalid("epoch_record", "That conversation identifier is in use. Make a new one.")
	} else if !errors.Is(err, model.ErrNotFound) {
		return none, false, err
	}
	conv := model.Conversation{ID: parsed.ConversationID, Kind: model.KindSealed, Private: true, MemberKey: key,
		CreatedByID: c.Account.ID, CreatedAt: s.now()}
	err = s.chat.Tx(ctx, func(tx store.Tx, rec *chat.Recorder) error {
		if err := tx.Conversations().Create(ctx, &conv); err != nil {
			return err
		}
		for _, id := range []model.AccountID{c.Account.ID, other.ID} {
			res, err := tx.Members().AddDirect(ctx, conv.ID, id, model.LevelMember, conv.CreatedAt)
			if err != nil {
				return err
			}
			if err := rec.Membership(ctx, res, c.Account.ID, ""); err != nil {
				return err
			}
		}
		e, err := s.checkEpoch(ctx, tx, conv.ID, [2]model.AccountID{c.Account.ID, other.ID}, 0, 1, record)
		if err != nil {
			if me, ok := model.AsError(err); ok && me.Field == "record" {
				return model.Invalid("epoch_record", me.Message)
			}
			return err
		}
		_, err = s.storeEpoch(ctx, tx, rec, conv.ID, e, record)
		return err
	})
	if errors.Is(err, model.ErrAlreadyExists) {
		// Both people created at once and the other one won: theirs is
		// the chat, and the loser's device checks the winner's record
		// and uses it (section 6, step 5).
		existing, gerr := s.store.Conversations().ByMemberKey(ctx, key)
		if gerr != nil {
			return none, false, err
		}
		out, err := s.chat.Conversation(ctx, c.Account, existing.ID)
		return out, false, err
	}
	if err != nil {
		return none, false, err
	}
	if !other.Local() {
		// Someone on another server: the chat runs in a stream between
		// the two servers, and without one it does not exist.
		if err := s.outbound.StartSealed(ctx, c.Account, conv, other, record); err != nil {
			if _, derr := s.store.Conversations().Delete(ctx, conv.ID); derr != nil {
				s.log.Warn("a sealed chat whose stream could not be opened could not be removed", "err", derr)
			}
			return none, false, err
		}
	}
	out, err := s.chat.Conversation(ctx, c.Account, conv.ID)
	return out, true, err
}

// withReadOnly marks a sealed chat that accepts nothing new.
func (s *Service) withReadOnly(ctx context.Context, actor model.Account, c model.Conversation) model.Conversation {
	if !c.Sealed() {
		return c
	}
	set, err := s.perm.Settings(ctx)
	if err != nil {
		return c
	}
	other, err := s.otherMember(ctx, s.store, c.ID, actor.ID)
	if err != nil {
		return c
	}
	c.ReadOnly = perm.SealedReadOnly(set, other)
	return c
}

// ReadOnly reports whether a sealed chat accepts nothing new for one of its
// members: sealed chats are off, or the other person was removed. The chat
// service asks when it shows a conversation.
func (s *Service) ReadOnly(ctx context.Context, actor model.AccountID, conv model.Conversation) bool {
	return s.withReadOnly(ctx, model.Account{ID: actor}, conv).ReadOnly
}

// writable loads the caller's state in a sealed chat and refuses if the
// chat is not one, is not theirs, or has become read-only.
func (s *Service) writable(ctx context.Context, c Caller, conv model.ConversationID) (perm.State, model.Account, error) {
	st, err := s.perm.Can(ctx, c.Account, perm.Post, conv)
	if err != nil {
		if errors.Is(err, model.ErrNotFound) {
			err = model.ErrNotFound
		}
		return st, model.Account{}, err
	}
	other, err := s.otherMember(ctx, s.store, conv, c.Account.ID)
	if err != nil {
		return st, other, err
	}
	return st, other, perm.CheckSealedWrite(st, other)
}

// PostEpoch stores the next epoch record of a sealed chat (section 5.5).
// When another record already holds the key epoch, the error is
// model.ErrConflict and the record returned is the winner, so the device
// that lost the race can check and use it.
func (s *Service) PostEpoch(ctx context.Context, c Caller, conv model.ConversationID, keyEpoch int64, record []byte) (model.EpochRecord, error) {
	none := model.EpochRecord{}
	if err := person(c); err != nil {
		return none, err
	}
	st, _, err := s.writable(ctx, c, conv)
	if err != nil {
		if !st.Settings.SealedEnabled && st.Conversation.Sealed() {
			return none, model.ErrFeatureOff.With("Sealed chats are switched off on this server.")
		}
		return none, err
	}
	// The poster is a device of one of the two accounts. It need not be
	// the maker: a record proves itself (section 7.4).
	if _, err := s.device(ctx, s.store, c); err != nil {
		return none, err
	}
	members, err := s.store.Members().IDs(ctx, conv)
	if err != nil {
		return none, err
	}
	if len(members) != 2 {
		return none, model.ErrConflict.With("This sealed chat no longer has two people in it.")
	}
	var out model.EpochRecord
	err = s.chat.Tx(ctx, func(tx store.Tx, rec *chat.Recorder) error {
		cur, err := tx.Conversations().Get(ctx, conv)
		if err != nil {
			return err
		}
		current := int64(0)
		if cur.KeyEpoch != nil {
			current = *cur.KeyEpoch
		}
		e, err := s.checkEpoch(ctx, tx, conv, [2]model.AccountID{members[0], members[1]}, current, keyEpoch, record)
		if err != nil {
			return err
		}
		out, err = s.storeEpoch(ctx, tx, rec, conv, e, record)
		return err
	})
	if model.CodeOf(err) == model.CodeConflict {
		if winner, found, ferr := s.store.Sealed().Epoch(ctx, conv, keyEpoch); ferr == nil && found {
			return presentEpoch(winner), err
		}
		return none, err
	}
	return out, err
}

// Epochs lists a sealed chat's epoch records from a key epoch on.
func (s *Service) Epochs(ctx context.Context, c Caller, conv model.ConversationID, from int64, limit int) ([]model.EpochRecord, bool, error) {
	if err := person(c); err != nil {
		return nil, false, err
	}
	st, err := s.perm.Can(ctx, c.Account, perm.Read, conv)
	if err != nil {
		if errors.Is(err, model.ErrNotFound) {
			err = model.ErrNotFound
		}
		return nil, false, err
	}
	if !st.Conversation.Sealed() {
		return nil, false, model.ErrNotFound
	}
	if from < 1 {
		from = 1
	}
	list, more, err := s.store.Sealed().Epochs(ctx, conv, from, limit)
	for i := range list {
		list[i] = presentEpoch(list[i])
	}
	return list, more, err
}

// clientIDWindow is how long a repeated client_id returns the first
// message (docs/CONVENTIONS.md).
const clientIDWindow = 24 * 60 * 60 * 1000

// NewMessage is a sealed message to post.
type NewMessage struct {
	ConversationID model.ConversationID
	// ClientID is the message reference the sending device made.
	ClientID   string
	KeyEpoch   int64
	Ciphertext []byte
	Quiet      bool
	// FileIDs are the encrypted blobs the message carries, named outside
	// the ciphertext so the server can tie them to its row.
	FileIDs []model.FileID
}

// Post stores a sealed message (section 8.2, step 4). The server checks
// what it can see: the sender is a member on a registered device and the
// chat is not read-only; the bytes are at most 128 KB; the header's
// conversation, account and device are the sender's own; the reference is
// the client_id; the quiet flag matches; the key epoch is the chat's
// current one, and that epoch names the newest device lists. A message in
// a key epoch that is not current, or is stale, is refused with `conflict`,
// and the device encrypts it again once a newer epoch exists.
//
// created is false when the message already existed under this client_id.
func (s *Service) Post(ctx context.Context, c Caller, in NewMessage) (model.Message, bool, error) {
	none := model.Message{}
	if err := person(c); err != nil {
		return none, false, err
	}
	st, err := s.perm.Can(ctx, c.Account, perm.Post, in.ConversationID)
	if err != nil {
		if errors.Is(err, model.ErrNotFound) {
			err = model.ErrNotFound
		}
		return none, false, err
	}
	if !st.Conversation.Sealed() {
		return none, false, model.BadRequest("Only a sealed chat takes sealed messages.")
	}
	if len(in.Ciphertext) > maxCiphertext {
		return none, false, model.ErrTooLarge.With("A sealed message is at most 128 KB.")
	}
	if !model.ValidID(in.ClientID) {
		return none, false, model.Invalid("client_id", "A sealed message needs a client_id, which is a UUID.")
	}
	if len(in.FileIDs) > model.MaxFilesPerMessage {
		return none, false, model.Invalid("file_ids", "A message carries at most ten files.")
	}
	dev, err := s.device(ctx, s.store, c)
	if err != nil {
		return none, false, err
	}
	h, err := parseHeader(in.Ciphertext)
	if err != nil {
		return none, false, model.Invalid("sealed", "The ciphertext is not a sealed message.")
	}
	switch {
	case h.ConversationID != in.ConversationID || h.AccountID != c.Account.ID || h.DeviceID != dev.ID:
		return none, false, model.Invalid("sealed", "The message's header must name this chat, this account and this device.")
	case int64(h.KeyEpoch) != in.KeyEpoch:
		return none, false, model.Invalid("sealed", "sealed.key_epoch is not the key epoch in the message's header.")
	case h.Quiet != in.Quiet:
		return none, false, model.Invalid("quiet", "quiet must match the flag in the message's header.")
	case h.Reference != in.ClientID:
		return none, false, model.Invalid("client_id", "client_id must be the message reference in the header.")
	}
	old, found, err := s.store.Messages().ByClientID(ctx, in.ConversationID, c.Account.ID, in.ClientID)
	if err != nil {
		return none, false, err
	}
	if found {
		if int64(s.now()-old.CreatedAt) > clientIDWindow {
			return none, false, model.ErrConflict.With("That client_id was used more than a day ago. Make a new one.")
		}
		return old, false, nil
	}
	other, err := s.otherMember(ctx, s.store, in.ConversationID, c.Account.ID)
	if err != nil {
		return none, false, err
	}
	if err := perm.CheckSealedWrite(st, other); err != nil {
		return none, false, err
	}
	m := model.Message{ID: model.NewID[model.MessageID](), ConversationID: in.ConversationID, AuthorID: c.Account.ID, ClientID: in.ClientID,
		Sealed: &model.SealedPayload{KeyEpoch: in.KeyEpoch, Ciphertext: in.Ciphertext}, Quiet: in.Quiet, CreatedAt: s.now()}
	err = s.chat.Tx(ctx, func(tx store.Tx, rec *chat.Recorder) error {
		if err := s.currentEpoch(ctx, tx, in.ConversationID, in.KeyEpoch); err != nil {
			return err
		}
		seq, err := rec.Seq(ctx, in.ConversationID)
		if err != nil {
			return err
		}
		m.Seq, m.Order = seq, rec.Order(in.ConversationID, seq)
		// No mentions, no search entry: the store indexes nothing that
		// has ciphertext.
		if err := tx.Messages().Insert(ctx, &m, nil); err != nil {
			return err
		}
		if len(in.FileIDs) > 0 {
			if err := tx.Files().Attach(ctx, m.ID, in.ConversationID, c.Account.ID, in.FileIDs); err != nil {
				return err
			}
		}
		return rec.EmitAt(ctx, model.Event{Type: model.EventMessageCreated, At: m.CreatedAt, ConversationID: in.ConversationID, Seq: seq,
			MessageID: m.ID, Payload: map[string]any{"message": m}})
	})
	if errors.Is(err, model.ErrAlreadyExists) {
		// The same request arrived twice at once; the other one won.
		if old, found, gerr := s.store.Messages().ByClientID(ctx, in.ConversationID, c.Account.ID, in.ClientID); gerr == nil && found {
			return old, false, nil
		}
	}
	if err != nil {
		return none, false, err
	}
	return m, true, nil
}

// currentEpoch refuses, with `conflict`, a message whose key epoch is not
// the chat's current one, or whose epoch names a device list version older
// than the newest stored: an honest server lets nothing be sent to a device
// after its removal has been published, even by a sender that has not
// heard of it (section 5.5, "A stale epoch").
func (s *Service) currentEpoch(ctx context.Context, tx store.Tx, conv model.ConversationID, keyEpoch int64) error {
	cur, err := tx.Conversations().Get(ctx, conv)
	if err != nil {
		return err
	}
	if cur.KeyEpoch == nil {
		return model.ErrConflict.With("This chat has no key epoch yet. Post epoch record 1 first.")
	}
	if *cur.KeyEpoch != keyEpoch {
		return model.ErrConflict.With("That is not this chat's current key epoch. Encrypt the message again in the current one.")
	}
	row, found, err := tx.Sealed().Epoch(ctx, conv, keyEpoch)
	if err != nil {
		return err
	}
	if !found {
		return model.ErrConflict.With("This chat's current epoch record is missing. Make a new epoch.")
	}
	e, err := parseEpoch(row.Record)
	if err != nil {
		return err
	}
	for _, ref := range e.Accounts {
		newest, err := tx.Sealed().NewestDeviceListVersion(ctx, ref.AccountID)
		if err != nil {
			return err
		}
		if newest > int64(ref.ListVersion) {
			return model.ErrConflict.With("A device list changed after this chat's current key epoch was made. Make a new epoch first.")
		}
	}
	return nil
}
