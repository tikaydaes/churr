package storetest

import (
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

func init() {
	register("sealed_lists", testSealedLists)
	register("sealed_epochs", testSealedEpochs)
	register("sealed_backup", testSealedBackup)
	register("sealed_unlock", testSealedUnlock)
	register("devices_more", testDevicesMore)
}

// sealedChat makes the sealed chat of two accounts, as the sealed service
// does: the row with the pair as its member key, and two members.
func sealedChat(t testing.TB, s store.Areas, a, b model.AccountID) model.Conversation {
	t.Helper()
	c := model.Conversation{Kind: model.KindSealed, Private: true, CreatedByID: a, MemberKey: store.SealedMemberKey(a, b)}
	must(t, s.Conversations().Create(ctx, &c))
	join(t, s, c.ID, a, model.LevelMember)
	join(t, s, c.ID, b, model.LevelMember)
	return c
}

// sealedPost stores a sealed message in a key epoch.
func sealedPost(t testing.TB, s store.Store, conv model.ConversationID, author model.AccountID, keyEpoch int64, at model.Time) model.Message {
	t.Helper()
	m := model.Message{ID: model.NewID[model.MessageID](), ConversationID: conv, AuthorID: author, ClientID: model.NewID[string](),
		Sealed: &model.SealedPayload{KeyEpoch: keyEpoch, Ciphertext: []byte("ciphertext")}, CreatedAt: at}
	must(t, s.Tx(ctx, func(tx store.Tx) error {
		seq, err := tx.Conversations().NextSeq(ctx, conv)
		if err != nil {
			return err
		}
		m.Seq = seq
		return tx.Messages().Insert(ctx, &m, nil)
	}))
	return m
}

func testSealedLists(t *testing.T, s store.Store) {
	ann := newAccount(t, s, "ann", model.RoleOwner)
	bob := newAccount(t, s, "bob", model.RoleMember)
	cat := newAccount(t, s, "cat", model.RoleMember)

	v, err := s.Sealed().NewestDeviceListVersion(ctx, ann.ID)
	must(t, err)
	eq(t, "no list yet", v, 0)
	_, found, err := s.Sealed().NewestDeviceList(ctx, ann.ID)
	must(t, err)
	eq(t, "no newest", found, false)

	// Only the next version is accepted: not 2 before 1, not 1 twice, not
	// a jump.
	wantErr(t, s.Sealed().PutDeviceList(ctx, store.SignedDeviceList{AccountID: ann.ID, Version: 2, Signed: []byte("two")}), model.ErrConflict)
	must(t, s.Sealed().PutDeviceList(ctx, store.SignedDeviceList{AccountID: ann.ID, Version: 1, Signed: []byte("one")}))
	wantErr(t, s.Sealed().PutDeviceList(ctx, store.SignedDeviceList{AccountID: ann.ID, Version: 1, Signed: []byte("again")}), model.ErrConflict)
	wantErr(t, s.Sealed().PutDeviceList(ctx, store.SignedDeviceList{AccountID: ann.ID, Version: 3, Signed: []byte("three")}), model.ErrConflict)
	must(t, s.Sealed().PutDeviceList(ctx, store.SignedDeviceList{AccountID: ann.ID, Version: 2, Signed: []byte("two")}))
	must(t, s.Sealed().PutDeviceList(ctx, store.SignedDeviceList{AccountID: ann.ID, Version: 3, Signed: []byte("three")}))
	// Another account's list has its own numbering.
	must(t, s.Sealed().PutDeviceList(ctx, store.SignedDeviceList{AccountID: bob.ID, Version: 1, Signed: []byte("bob one")}))

	// Inside a transaction a refused version leaves the transaction
	// usable, on PostgreSQL too.
	must(t, s.Tx(ctx, func(tx store.Tx) error {
		wantErr(t, tx.Sealed().PutDeviceList(ctx, store.SignedDeviceList{AccountID: ann.ID, Version: 3, Signed: []byte("x")}), model.ErrConflict)
		return tx.Sealed().PutDeviceList(ctx, store.SignedDeviceList{AccountID: ann.ID, Version: 4, Signed: []byte("four")})
	}))

	v, _ = s.Sealed().NewestDeviceListVersion(ctx, ann.ID)
	eq(t, "newest version", v, 4)
	newest, found, err := s.Sealed().NewestDeviceList(ctx, ann.ID)
	must(t, err)
	eq(t, "newest", found && newest.Version == 4 && string(newest.Signed) == "four" && !newest.CreatedAt.IsZero(), true)
	one, found, err := s.Sealed().DeviceList(ctx, ann.ID, 1)
	must(t, err)
	eq(t, "every version is kept", found && string(one.Signed) == "one", true)
	_, found, _ = s.Sealed().DeviceList(ctx, ann.ID, 9)
	eq(t, "no such version", found, false)
	after, err := s.Sealed().DeviceLists(ctx, ann.ID, 1, 100)
	must(t, err)
	eq(t, "versions after 1", len(after) == 3 && after[0].Version == 2 && after[2].Version == 4, true)
	capped, _ := s.Sealed().DeviceLists(ctx, ann.ID, 0, 2)
	eq(t, "limit", len(capped) == 2 && capped[1].Version == 2, true)

	// Twenty devices publishing the same next version at once: one wins.
	results := make(chan error, 20)
	for i := 0; i < 20; i++ {
		go func() {
			results <- s.Sealed().PutDeviceList(ctx, store.SignedDeviceList{AccountID: ann.ID, Version: 5, Signed: []byte("five")})
		}()
	}
	won := 0
	for i := 0; i < 20; i++ {
		if err := <-results; err == nil {
			won++
		} else if model.CodeOf(err) != model.CodeConflict {
			t.Fatalf("a losing publish: %v", err)
		}
	}
	eq(t, "one publish of version 5 wins", won, 1)

	// A remote account's chain is taken up where it stands.
	must(t, s.Sealed().PutFirstDeviceList(ctx, store.SignedDeviceList{AccountID: cat.ID, Version: 7, Signed: []byte("seven")}))
	wantErr(t, s.Sealed().PutFirstDeviceList(ctx, store.SignedDeviceList{AccountID: cat.ID, Version: 9, Signed: []byte("nine")}), model.ErrConflict)
	must(t, s.Sealed().PutDeviceList(ctx, store.SignedDeviceList{AccountID: cat.ID, Version: 8, Signed: []byte("eight")}))
	v, _ = s.Sealed().NewestDeviceListVersion(ctx, cat.ID)
	eq(t, "carried on from 7", v, 8)

	// Who is told of a list change: the other person of each sealed chat.
	c1 := sealedChat(t, s, ann.ID, bob.ID)
	sealedChat(t, s, cat.ID, ann.ID)
	newAdhoc(t, s, ann.ID, bob.ID, cat.ID)
	partners, err := s.Sealed().Partners(ctx, ann.ID)
	must(t, err)
	eq(t, "partners", len(partners), 2)
	partners, _ = s.Sealed().Partners(ctx, bob.ID)
	eq(t, "bob's partner", len(partners) == 1 && partners[0] == ann.ID, true)
	chats, err := s.Sealed().Chats(ctx, bob.ID)
	must(t, err)
	eq(t, "bob's sealed chats", len(chats) == 1 && chats[0] == c1.ID, true)
}

func testSealedEpochs(t *testing.T, s store.Store) {
	ann := newAccount(t, s, "ann", model.RoleOwner)
	bob := newAccount(t, s, "bob", model.RoleMember)
	c := sealedChat(t, s, ann.ID, bob.ID)
	dev := model.NewID[model.DeviceID]()
	now := model.Now()
	put := func(a store.Areas, epoch, seq int64) error {
		return a.Sealed().PutEpoch(ctx, model.EpochRecord{ConversationID: c.ID, KeyEpoch: epoch, Seq: seq, DeviceID: dev,
			Record: []byte{byte(epoch)}, CreatedAt: now})
	}
	got, err := s.Conversations().Get(ctx, c.ID)
	must(t, err)
	eq(t, "a new chat has no key epoch", got.KeyEpoch == nil, true)

	// Only the next number, starting at 1.
	wantErr(t, put(s, 2, 1), model.ErrConflict)
	wantErr(t, put(s, 0, 1), model.ErrConflict)
	must(t, put(s, 1, 1))
	wantErr(t, put(s, 1, 2), model.ErrConflict)
	wantErr(t, put(s, 3, 2), model.ErrConflict)
	must(t, put(s, 2, 2))
	got, _ = s.Conversations().Get(ctx, c.ID)
	eq(t, "the chat's current epoch", got.KeyEpoch != nil && *got.KeyEpoch == 2, true)

	// A refused record inside a transaction changes nothing and leaves
	// the transaction usable.
	must(t, s.Tx(ctx, func(tx store.Tx) error {
		wantErr(t, put(tx, 2, 3), model.ErrConflict)
		return put(tx, 3, 3)
	}))
	// A channel is not a sealed chat.
	ch := newChannel(t, s, "ops", false, ann.ID)
	wantErr(t, s.Sealed().PutEpoch(ctx, model.EpochRecord{ConversationID: ch.ID, KeyEpoch: 1, Seq: 1, DeviceID: dev, Record: []byte("x"), CreatedAt: now}),
		model.ErrConflict)

	e, found, err := s.Sealed().Epoch(ctx, c.ID, 2)
	must(t, err)
	eq(t, "one record", found && e.KeyEpoch == 2 && e.Seq == 2 && e.DeviceID == dev && len(e.Record) == 1 && e.Record[0] == 2 && e.CreatedAt == now, true)
	_, found, _ = s.Sealed().Epoch(ctx, c.ID, 9)
	eq(t, "no such epoch", found, false)
	list, more, err := s.Sealed().Epochs(ctx, c.ID, 1, 2)
	must(t, err)
	eq(t, "a page", len(list) == 2 && more && list[0].KeyEpoch == 1 && list[1].KeyEpoch == 2, true)
	list, more, _ = s.Sealed().Epochs(ctx, c.ID, 3, 50)
	eq(t, "from 3", len(list) == 1 && !more && list[0].KeyEpoch == 3, true)

	// Two devices making the same epoch at once: one wins.
	results := make(chan error, 20)
	for i := 0; i < 20; i++ {
		go func() {
			results <- s.Tx(ctx, func(tx store.Tx) error {
				seq, err := tx.Conversations().NextSeq(ctx, c.ID)
				if err != nil {
					return err
				}
				return put(tx, 4, seq)
			})
		}()
	}
	won := 0
	for i := 0; i < 20; i++ {
		if err := <-results; err == nil {
			won++
		} else if model.CodeOf(err) != model.CodeConflict {
			t.Fatalf("a losing epoch: %v", err)
		}
	}
	eq(t, "one record for epoch 4", won, 1)

	// The janitor's rule. Epochs 1 to 4 exist; 4 is current. Backup
	// records exist for both accounts in 1, 2 and 3.
	for epoch := int64(1); epoch <= 3; epoch++ {
		for _, who := range []model.AccountID{ann.ID, bob.ID} {
			_, _, err := s.Sealed().PutBackupRecord(ctx, model.BackupRecord{AccountID: who, ConversationID: c.ID, KeyEpoch: epoch, KeyVersion: 1, Record: []byte("r")})
			must(t, err)
		}
	}
	m2 := sealedPost(t, s, c.ID, ann.ID, 2, now)
	// Epoch 1 has no message and is older than everything left: it goes.
	// Epoch 2 has one. Epoch 3 is empty, but 2 before it is not, so it
	// stays: what remains must be an unbroken run.
	epochs, records, err := s.Sealed().DeleteEmptyEpochs(ctx)
	must(t, err)
	eq(t, "epoch 1 deleted", epochs == 1 && records == 2, true)
	list, _, _ = s.Sealed().Epochs(ctx, c.ID, 1, 50)
	eq(t, "2, 3 and 4 remain", len(list) == 3 && list[0].KeyEpoch == 2, true)
	epochs, records, _ = s.Sealed().DeleteEmptyEpochs(ctx)
	eq(t, "nothing more to delete", epochs == 0 && records == 0, true)
	// A deleted message keeps its row and loses its ciphertext: its
	// epoch then counts as empty.
	must(t, s.Events().Append(ctx, model.Event{Type: model.EventMessageCreated, At: now, ConversationID: c.ID, Seq: m2.Seq,
		MessageID: m2.ID, Payload: map[string]any{"message": m2}}))
	f := model.File{ConversationID: c.ID, UploaderID: ann.ID, Name: "", Size: 26, MediaType: "application/octet-stream", Kind: model.FileOther,
		SHA256: []byte("hash"), BlobKey: "blob/one"}
	must(t, s.Files().Create(ctx, &f))
	must(t, s.Files().Attach(ctx, m2.ID, c.ID, ann.ID, []model.FileID{f.ID}))
	erased, keys, err := s.Sealed().EraseMessage(ctx, m2.ID, ann.ID, now.Add(time.Minute))
	must(t, err)
	eq(t, "the marker", erased.Sealed == nil && erased.DeletedAt == now.Add(time.Minute) && erased.DeletedByID == ann.ID && erased.Seq == m2.Seq, true)
	eq(t, "its blob is handed back", len(keys) == 1 && keys[0] == "blob/one", true)
	stored, err := s.Messages().Get(ctx, m2.ID)
	must(t, err)
	eq(t, "the row stays without ciphertext", stored.Sealed == nil && stored.Deleted(), true)
	evs, _, err := s.Events().List(ctx, c.ID, 0, 0, 100)
	must(t, err)
	eq(t, "the event that carried the ciphertext is gone", len(evs), 0)
	_, err = s.Files().Get(ctx, f.ID)
	wantErr(t, err, model.ErrNotFound)
	_, _, err = s.Sealed().EraseMessage(ctx, m2.ID, ann.ID, now)
	wantErr(t, err, model.ErrConflict)
	epochs, records, err = s.Sealed().DeleteEmptyEpochs(ctx)
	must(t, err)
	eq(t, "2 and 3 deleted, never the current epoch", epochs == 2 && records == 4, true)
	list, _, _ = s.Sealed().Epochs(ctx, c.ID, 1, 50)
	eq(t, "the current epoch remains", len(list) == 1 && list[0].KeyEpoch == 4, true)
	recs, _, _ := s.Sealed().BackupRecords(ctx, ann.ID, "", store.Page{})
	eq(t, "no backup records left", len(recs), 0)

	// Deleting the chat takes its records with it.
	_, err = s.Conversations().Delete(ctx, c.ID)
	must(t, err)
	list, _, _ = s.Sealed().Epochs(ctx, c.ID, 1, 50)
	eq(t, "records go with the chat", len(list), 0)
}

func testSealedBackup(t *testing.T, s store.Store) {
	ann := newAccount(t, s, "ann", model.RoleOwner)
	bob := newAccount(t, s, "bob", model.RoleMember)
	c := sealedChat(t, s, ann.ID, bob.ID)
	now := model.Now()
	ses := model.Session{AccountID: ann.ID, Kind: model.SessionApp, Label: "Pixel"}
	must(t, s.Sessions().Create(ctx, &ses, []byte("tok-b"), []byte("ref-b")))
	dev := model.Device{AccountID: ann.ID, SessionID: ses.ID, Name: "Pixel", Platform: "android"}
	must(t, s.Devices().Create(ctx, &dev))
	pk := model.Passkey{AccountID: ann.ID, CredentialID: []byte("cred-b"), Credential: []byte(`{}`), Name: "Laptop", HasPRF: true}
	must(t, s.Passkeys().Add(ctx, &pk))

	_, found, err := s.Sealed().Backup(ctx, ann.ID)
	must(t, err)
	eq(t, "no backup yet", found, false)
	wantErr(t, s.Sealed().PutAccountRecord(ctx, ann.ID, model.AccountRecord{KeyVersion: 1, Revision: 2, Record: []byte("r")}, now), model.ErrNotFound)
	changed, err := s.Sealed().SetReplacementDue(ctx, ann.ID, true)
	must(t, err)
	eq(t, "nothing to mark", changed, false)

	copies := []model.LockedCopy{
		{Kind: model.CopyPhrase, KeyVersion: 1, LockedKey: []byte("phrase-1")},
		{Kind: model.CopyDevice, DeviceID: dev.ID, KeyVersion: 1, LockedKey: []byte("device-1")},
	}
	must(t, s.Sealed().CreateBackup(ctx, ann.ID, model.AccountRecord{KeyVersion: 1, Revision: 1, Record: []byte("record-1")}, copies, now))
	// Of two devices creating the backup, the first is kept.
	wantErr(t, s.Sealed().CreateBackup(ctx, ann.ID, model.AccountRecord{KeyVersion: 1, Revision: 1, Record: []byte("other")}, nil, now), model.ErrAlreadyExists)
	b, found, err := s.Sealed().Backup(ctx, ann.ID)
	must(t, err)
	eq(t, "backup", found && b.KeyVersion == 1 && b.AccountRecord.Revision == 1 && string(b.AccountRecord.Record) == "record-1" &&
		len(b.LockedCopies) == 2 && !b.ReplacementDue && b.AccountRecord.UpdatedAt == now, true)

	// A copy for the same way replaces the earlier one; another way adds.
	pc := model.LockedCopy{AccountID: ann.ID, Kind: model.CopyPasskey, PasskeyID: pk.ID, KeyVersion: 1, LockedKey: []byte("passkey-1")}
	must(t, s.Sealed().PutLockedCopy(ctx, &pc))
	again := model.LockedCopy{AccountID: ann.ID, Kind: model.CopyPasskey, PasskeyID: pk.ID, KeyVersion: 1, LockedKey: []byte("passkey-1b")}
	must(t, s.Sealed().PutLockedCopy(ctx, &again))
	phrase := model.LockedCopy{AccountID: ann.ID, Kind: model.CopyPhrase, KeyVersion: 1, LockedKey: []byte("phrase-1b")}
	must(t, s.Sealed().PutLockedCopy(ctx, &phrase))
	b, _, _ = s.Sealed().Backup(ctx, ann.ID)
	eq(t, "three ways, one copy each", len(b.LockedCopies), 3)
	one, err := s.Sealed().LockedCopy(ctx, again.ID)
	must(t, err)
	eq(t, "the newer copy", string(one.LockedKey) == "passkey-1b" && one.PasskeyID == pk.ID && one.AccountID == ann.ID, true)
	_, err = s.Sealed().LockedCopy(ctx, pc.ID)
	wantErr(t, err, model.ErrNotFound)
	got, err := s.Passkeys().Get(ctx, pk.ID)
	must(t, err)
	eq(t, "the passkey can unlock", got.CanUnlock, true)

	// The account record: only the next revision of the current version.
	wantErr(t, s.Sealed().PutAccountRecord(ctx, ann.ID, model.AccountRecord{KeyVersion: 1, Revision: 3, Record: []byte("r3")}, now), model.ErrConflict)
	wantErr(t, s.Sealed().PutAccountRecord(ctx, ann.ID, model.AccountRecord{KeyVersion: 2, Revision: 2, Record: []byte("r2")}, now), model.ErrConflict)
	must(t, s.Sealed().PutAccountRecord(ctx, ann.ID, model.AccountRecord{KeyVersion: 1, Revision: 2, Record: []byte("record-2")}, now))
	wantErr(t, s.Sealed().PutAccountRecord(ctx, ann.ID, model.AccountRecord{KeyVersion: 1, Revision: 2, Record: []byte("again")}, now), model.ErrConflict)

	wantErr(t, s.Sealed().DeleteLockedCopy(ctx, bob.ID, phrase.ID), model.ErrNotFound)
	must(t, s.Sealed().DeleteLockedCopy(ctx, ann.ID, phrase.ID))
	wantErr(t, s.Sealed().DeleteLockedCopy(ctx, ann.ID, phrase.ID), model.ErrNotFound)

	// Backup records: the first for a chat and epoch is kept.
	r1, created, err := s.Sealed().PutBackupRecord(ctx, model.BackupRecord{AccountID: ann.ID, ConversationID: c.ID, KeyEpoch: 1, KeyVersion: 1, Record: []byte("first")})
	must(t, err)
	eq(t, "created", created && string(r1.Record) == "first", true)
	r1, created, err = s.Sealed().PutBackupRecord(ctx, model.BackupRecord{AccountID: ann.ID, ConversationID: c.ID, KeyEpoch: 1, KeyVersion: 1, Record: []byte("second")})
	must(t, err)
	eq(t, "the first is kept", !created && string(r1.Record) == "first", true)
	// Bob's record for the same chat and epoch is his own.
	_, created, _ = s.Sealed().PutBackupRecord(ctx, model.BackupRecord{AccountID: bob.ID, ConversationID: c.ID, KeyEpoch: 1, KeyVersion: 1, Record: []byte("bob")})
	eq(t, "per account", created, true)
	c2 := sealedChat(t, s, ann.ID, newAccount(t, s, "cat", model.RoleMember).ID)
	for epoch := int64(2); epoch <= 4; epoch++ {
		_, _, err := s.Sealed().PutBackupRecord(ctx, model.BackupRecord{AccountID: ann.ID, ConversationID: c.ID, KeyEpoch: epoch, KeyVersion: 1, Record: []byte("r")})
		must(t, err)
	}
	_, _, err = s.Sealed().PutBackupRecord(ctx, model.BackupRecord{AccountID: ann.ID, ConversationID: c2.ID, KeyEpoch: 1, KeyVersion: 1, Record: []byte("other chat")})
	must(t, err)
	page, next, err := s.Sealed().BackupRecords(ctx, ann.ID, "", store.Page{Limit: 3})
	must(t, err)
	eq(t, "first page", len(page) == 3 && next != "", true)
	rest, next, err := s.Sealed().BackupRecords(ctx, ann.ID, "", store.Page{Limit: 3, Cursor: next})
	must(t, err)
	eq(t, "second page", len(rest) == 2 && next == "", true)
	seen := map[string]bool{}
	for _, r := range append(page, rest...) {
		seen[string(r.ConversationID)+":"+string(rune('0'+r.KeyEpoch))] = true
	}
	eq(t, "every record once", len(seen), 5)
	only, _, _ := s.Sealed().BackupRecords(ctx, ann.ID, c.ID, store.Page{})
	eq(t, "one chat", len(only) == 4 && only[0].KeyEpoch == 1 && only[3].KeyEpoch == 4, true)

	// "Replacement due" is a mark on the account record.
	changed, _ = s.Sealed().SetReplacementDue(ctx, ann.ID, true)
	eq(t, "marked", changed, true)
	changed, _ = s.Sealed().SetReplacementDue(ctx, ann.ID, true)
	eq(t, "marked once", changed, false)
	b, _, _ = s.Sealed().Backup(ctx, ann.ID)
	eq(t, "due", b.ReplacementDue, true)

	// Replacing the backup key swaps everything together.
	gone := model.NewID[model.ConversationID]()
	newCopies := []model.LockedCopy{{Kind: model.CopyPhrase, KeyVersion: 2, LockedKey: []byte("phrase-2")}}
	newRecords := []model.BackupRecord{
		{ConversationID: c.ID, KeyEpoch: 1, KeyVersion: 2, Record: []byte("first, relocked")},
		{ConversationID: c.ID, KeyEpoch: 2, KeyVersion: 2, Record: []byte("r, relocked")},
		{ConversationID: gone, KeyEpoch: 1, KeyVersion: 2, Record: []byte("a chat that no longer exists")},
	}
	wantErr(t, s.Sealed().ReplaceBackup(ctx, ann.ID, 7, model.AccountRecord{KeyVersion: 8, Revision: 1, Record: []byte("x")}, newCopies, newRecords, now), model.ErrConflict)
	must(t, s.Sealed().ReplaceBackup(ctx, ann.ID, 1, model.AccountRecord{KeyVersion: 2, Revision: 1, Record: []byte("record v2")}, newCopies, newRecords, now))
	// The same version cannot be replaced twice.
	wantErr(t, s.Sealed().ReplaceBackup(ctx, ann.ID, 1, model.AccountRecord{KeyVersion: 2, Revision: 1, Record: []byte("y")}, newCopies, newRecords, now), model.ErrConflict)
	b, _, _ = s.Sealed().Backup(ctx, ann.ID)
	eq(t, "the new backup", b.KeyVersion == 2 && string(b.AccountRecord.Record) == "record v2" && len(b.LockedCopies) == 1 &&
		string(b.LockedCopies[0].LockedKey) == "phrase-2" && !b.ReplacementDue, true)
	only, _, _ = s.Sealed().BackupRecords(ctx, ann.ID, "", store.Page{})
	eq(t, "the old records are gone and the new ones stored", len(only) == 2 && only[0].KeyVersion == 2 && string(only[0].Record) == "first, relocked", true)
	bobs, _, _ := s.Sealed().BackupRecords(ctx, bob.ID, "", store.Page{})
	eq(t, "bob's backup is untouched", len(bobs), 1)
	got, _ = s.Passkeys().Get(ctx, pk.ID)
	eq(t, "the passkey's copy went with the old key", got.CanUnlock, false)

	// Removing a passkey or a device takes its locked copy with it.
	pc = model.LockedCopy{AccountID: ann.ID, Kind: model.CopyPasskey, PasskeyID: pk.ID, KeyVersion: 2, LockedKey: []byte("passkey-2")}
	must(t, s.Sealed().PutLockedCopy(ctx, &pc))
	dc := model.LockedCopy{AccountID: ann.ID, Kind: model.CopyDevice, DeviceID: dev.ID, KeyVersion: 2, LockedKey: []byte("device-2")}
	must(t, s.Sealed().PutLockedCopy(ctx, &dc))
	must(t, s.Passkeys().Delete(ctx, pk.ID))
	b, _, _ = s.Sealed().Backup(ctx, ann.ID)
	eq(t, "the passkey's copy went by cascade", len(b.LockedCopies), 2)
}

func testSealedUnlock(t *testing.T, s store.Store) {
	ann := newAccount(t, s, "ann", model.RoleOwner)
	bob := newAccount(t, s, "bob", model.RoleMember)
	ses := model.Session{AccountID: ann.ID, Kind: model.SessionApp, Label: "Pixel"}
	must(t, s.Sessions().Create(ctx, &ses, []byte("tok-u"), []byte("ref-u")))
	dev := model.Device{AccountID: ann.ID, SessionID: ses.ID, Name: "Pixel", Platform: "android"}
	must(t, s.Devices().Create(ctx, &dev))
	now := model.Now()

	r := model.UnlockRequest{AccountID: ann.ID, DeviceID: dev.ID, Request: []byte("request"), CreatedAt: now, ExpiresAt: now.Add(10 * time.Minute)}
	must(t, s.Sealed().CreateUnlockRequest(ctx, &r))
	old := model.UnlockRequest{AccountID: ann.ID, DeviceID: dev.ID, Request: []byte("old"), CreatedAt: now.Add(-time.Hour), ExpiresAt: now.Add(-50 * time.Minute)}
	must(t, s.Sealed().CreateUnlockRequest(ctx, &old))

	got, err := s.Sealed().UnlockRequest(ctx, r.ID)
	must(t, err)
	eq(t, "stored", got.AccountID == ann.ID && got.DeviceID == dev.ID && string(got.Request) == "request" && got.Answer == nil &&
		got.ExpiresAt == r.ExpiresAt && got.AnsweredAt.IsZero(), true)
	_, err = s.Sealed().UnlockRequest(ctx, model.NewID[string]())
	wantErr(t, err, model.ErrNotFound)

	waiting, err := s.Sealed().UnlockRequests(ctx, ann.ID, now)
	must(t, err)
	eq(t, "only the one that has not expired waits", len(waiting) == 1 && waiting[0].ID == r.ID, true)
	none, _ := s.Sealed().UnlockRequests(ctx, bob.ID, now)
	eq(t, "another account's", len(none), 0)

	// An expired request cannot be answered; a waiting one, once.
	_, err = s.Sealed().AnswerUnlockRequest(ctx, old.ID, []byte("late"), now)
	wantErr(t, err, model.ErrConflict)
	_, err = s.Sealed().AnswerUnlockRequest(ctx, model.NewID[string](), []byte("x"), now)
	wantErr(t, err, model.ErrNotFound)
	_, err = s.Sealed().AnswerUnlockRequest(ctx, r.ID, []byte("too late"), now.Add(11*time.Minute))
	wantErr(t, err, model.ErrConflict)
	ans, err := s.Sealed().AnswerUnlockRequest(ctx, r.ID, []byte("answer"), now.Add(time.Minute))
	must(t, err)
	eq(t, "answered", string(ans.Answer) == "answer" && ans.AnsweredAt == now.Add(time.Minute), true)
	_, err = s.Sealed().AnswerUnlockRequest(ctx, r.ID, []byte("again"), now.Add(2*time.Minute))
	wantErr(t, err, model.ErrConflict)
	waiting, _ = s.Sealed().UnlockRequests(ctx, ann.ID, now)
	eq(t, "an answered request no longer waits", len(waiting), 0)

	// The hand-over to an app rides on its one-time code.
	hash := []byte("code-hash")
	must(t, s.Identities().CreateCode(ctx, hash, store.OAuthCode{Kind: store.CodePairing, ExpiresAt: now.Add(2 * time.Minute)}))
	must(t, s.Sealed().SetHandover(ctx, hash, []byte("handover"), now))
	code, err := s.Identities().GetCode(ctx, hash)
	must(t, err)
	eq(t, "handover stored", string(code.Handover), "handover")
	wantErr(t, s.Sealed().SetHandover(ctx, hash, []byte("late"), now.Add(3*time.Minute)), model.ErrNotFound)
	wantErr(t, s.Sealed().SetHandover(ctx, []byte("no such code"), []byte("x"), now), model.ErrNotFound)
}

func testDevicesMore(t *testing.T, s store.Store) {
	ann := newAccount(t, s, "ann", model.RoleOwner)
	bob := newAccount(t, s, "bob", model.RoleMember)
	now := model.Now()
	session := func(who model.AccountID, token string) model.Session {
		ses := model.Session{AccountID: who, Kind: model.SessionApp, Label: token}
		must(t, s.Sessions().Create(ctx, &ses, []byte("tok-"+token), []byte("ref-"+token)))
		return ses
	}
	s1 := session(ann.ID, "one")
	phone := model.Device{AccountID: ann.ID, SessionID: s1.ID, Name: "Phone", Platform: "ios", CreatedAt: now.Add(-time.Hour)}
	must(t, s.Devices().Create(ctx, &phone))
	s2 := session(ann.ID, "two")
	laptop := model.Device{AccountID: ann.ID, SessionID: s2.ID, Name: "Laptop", Platform: "macos", CreatedAt: now}
	must(t, s.Devices().Create(ctx, &laptop))
	s3 := session(bob.ID, "three")
	bobs := model.Device{AccountID: bob.ID, SessionID: s3.ID, Name: "Bob's", Platform: "android"}
	must(t, s.Devices().Create(ctx, &bobs))

	got, err := s.Devices().Get(ctx, phone.ID)
	must(t, err)
	eq(t, "get", got.Name == "Phone" && got.AccountID == ann.ID && got.SessionID == s1.ID && got.PushKind == "" && got.PushAddress == "", true)
	_, err = s.Devices().Get(ctx, model.NewID[model.DeviceID]())
	wantErr(t, err, model.ErrNotFound)
	list, err := s.Devices().ForAccount(ctx, ann.ID)
	must(t, err)
	eq(t, "oldest first", len(list) == 2 && list[0].ID == phone.ID && list[1].ID == laptop.ID, true)

	// Registering: push address and keys.
	got, err = s.Devices().Update(ctx, phone.ID, func(d *model.Device) error {
		d.Name, d.PushKind, d.PushAddress = "Ann's iPhone", model.PushAPNs, "aabbcc"
		d.SignKey, d.BoxKey = []byte("sign"), []byte("box")
		d.PushPublicKey, d.PushAuth = []byte("p256"), []byte("auth")
		d.LastSeenAt = now
		return nil
	})
	must(t, err)
	eq(t, "updated", got.Name == "Ann's iPhone" && got.PushAddress == "aabbcc", true)
	got, _ = s.Devices().Get(ctx, phone.ID)
	eq(t, "stored", got.PushKind == model.PushAPNs && string(got.SignKey) == "sign" && string(got.BoxKey) == "box" &&
		string(got.PushPublicKey) == "p256" && string(got.PushAuth) == "auth" && got.LastSeenAt == now, true)
	_, err = s.Devices().Update(ctx, model.NewID[model.DeviceID](), func(d *model.Device) error { return nil })
	wantErr(t, err, model.ErrNotFound)
	_, err = s.Devices().Update(ctx, laptop.ID, func(d *model.Device) error {
		d.PushKind = model.PushNone
		return nil
	})
	must(t, err)

	// Only a device with an address and a kind that can be woken is.
	wake, err := s.Devices().Wakeable(ctx, ann.ID)
	must(t, err)
	eq(t, "wakeable", len(wake) == 1 && wake[0].ID == phone.ID, true)

	// The failure mark holds the first failure of a run; success clears it.
	must(t, s.Devices().PushFailed(ctx, phone.ID, now.Add(-8*24*time.Hour)))
	must(t, s.Devices().PushFailed(ctx, phone.ID, now))
	got, _ = s.Devices().Get(ctx, phone.ID)
	eq(t, "first failure kept", got.PushFailedAt, now.Add(-8*24*time.Hour))
	must(t, s.Devices().PushSucceeded(ctx, phone.ID))
	got, _ = s.Devices().Get(ctx, phone.ID)
	eq(t, "cleared", got.PushFailedAt.IsZero(), true)
	// Seven days of failures: the address is forgotten.
	must(t, s.Devices().PushFailed(ctx, phone.ID, now.Add(-8*24*time.Hour)))
	n, err := s.Devices().ForgetFailedPush(ctx, now.Add(-9*24*time.Hour))
	must(t, err)
	eq(t, "not yet", n, 0)
	n, err = s.Devices().ForgetFailedPush(ctx, now.Add(-7*24*time.Hour))
	must(t, err)
	eq(t, "forgotten", n, 1)
	wake, _ = s.Devices().Wakeable(ctx, ann.ID)
	eq(t, "no longer woken", len(wake), 0)
	// Registering again stores the address and clears the mark.
	_, err = s.Devices().Update(ctx, phone.ID, func(d *model.Device) error {
		d.PushAddress, d.PushFailedAt = "ddeeff", 0
		return nil
	})
	must(t, err)
	wake, _ = s.Devices().Wakeable(ctx, ann.ID)
	eq(t, "woken again", len(wake) == 1 && wake[0].PushAddress == "ddeeff" && wake[0].PushFailedAt.IsZero(), true)
	// "Gone" forgets at once.
	must(t, s.Devices().ForgetPush(ctx, phone.ID, now))
	got, _ = s.Devices().Get(ctx, phone.ID)
	eq(t, "gone", got.PushAddress == "" && got.PushFailedAt == now, true)

	// A session that ends by itself leaves the device in place; the same
	// app signing in again is attached to the new session.
	must(t, s.Sessions().Revoke(ctx, s2.ID, now))
	s4 := session(ann.ID, "four")
	// A session that already has a device cannot take another.
	_, err = s.Devices().Attach(ctx, laptop.ID, s1.ID, now)
	wantErr(t, err, model.ErrConflict)
	got, err = s.Devices().Attach(ctx, laptop.ID, s4.ID, now)
	must(t, err)
	eq(t, "attached", got.SessionID == s4.ID && got.LastSeenAt == now, true)
	// Attaching to the session it already has changes nothing.
	_, err = s.Devices().Attach(ctx, laptop.ID, s4.ID, now)
	must(t, err)

	// A keyless row can be deleted outright.
	spare := model.Device{AccountID: ann.ID, SessionID: session(ann.ID, "spare").ID, Name: "Spare", Platform: "linux"}
	must(t, s.Devices().Create(ctx, &spare))
	must(t, s.Devices().Delete(ctx, spare.ID))
	wantErr(t, s.Devices().Delete(ctx, spare.ID), model.ErrNotFound)

	// Retiring.
	got, found, err := s.Devices().Retire(ctx, phone.ID, now)
	must(t, err)
	eq(t, "retired", found && got.RetiredAt == now, true)
	_, found, _ = s.Devices().Retire(ctx, phone.ID, now)
	eq(t, "retired once", found, false)
	_, err = s.Devices().Attach(ctx, phone.ID, session(ann.ID, "five").ID, now)
	wantErr(t, err, model.ErrConflict)
	list, _ = s.Devices().ForAccount(ctx, ann.ID)
	eq(t, "retired devices are still listed", len(list), 2)

	// Thirty days without a session: bob's session expired long ago.
	must(t, s.Sessions().Touch(ctx, s3.ID, now.Add(-40*24*time.Hour), now.Add(-31*24*time.Hour)))
	idle, err := s.Devices().RetireIdle(ctx, now.Add(-30*24*time.Hour), now)
	must(t, err)
	eq(t, "bob's device is retired", len(idle) == 1 && idle[0].ID == bobs.ID && idle[0].RetiredAt == now, true)
	idle, _ = s.Devices().RetireIdle(ctx, now.Add(-30*24*time.Hour), now)
	eq(t, "once", len(idle), 0)
	got, _ = s.Devices().Get(ctx, laptop.ID)
	eq(t, "a device with a live session stays", got.RetiredAt.IsZero(), true)

	// A device whose session row is gone outlives it by thirty days,
	// counted from when the device was last seen.
	s5 := session(bob.ID, "six")
	recent := model.Device{AccountID: bob.ID, SessionID: s5.ID, Name: "Recent", Platform: "android", LastSeenAt: now.Add(-10 * 24 * time.Hour)}
	must(t, s.Devices().Create(ctx, &recent))
	s6 := session(bob.ID, "seven")
	stale := model.Device{AccountID: bob.ID, SessionID: s6.ID, Name: "Stale", Platform: "android", LastSeenAt: now.Add(-31 * 24 * time.Hour)}
	must(t, s.Devices().Create(ctx, &stale))
	// The janitor deletes ended session rows, which empties the column.
	must(t, s.Sessions().Revoke(ctx, s5.ID, now.Add(-10*24*time.Hour)))
	must(t, s.Sessions().Revoke(ctx, s6.ID, now.Add(-31*24*time.Hour)))
	_, err = s.Sessions().DeleteEnded(ctx, now)
	must(t, err)
	got, _ = s.Devices().Get(ctx, recent.ID)
	eq(t, "the session row is gone", got.SessionID, model.SessionID(""))
	idle, err = s.Devices().RetireIdle(ctx, now.Add(-30*24*time.Hour), now)
	must(t, err)
	eq(t, "only the one unseen for thirty days is retired", len(idle) == 1 && idle[0].ID == stale.ID, true)
	got, _ = s.Devices().Get(ctx, recent.ID)
	eq(t, "the one seen ten days ago stays", got.RetiredAt.IsZero(), true)
}
