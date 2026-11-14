package api_test

import (
	"context"
	"crypto/rand"
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/sealed/ref"
	"github.com/tikaydaes/churr/internal/store"
	"github.com/tikaydaes/churr/internal/testkit"
)

// Unlock requests, the passkey's unlock key, and the events of sealed chats
// that go to an account's own devices (docs/EVENTS.md section 12).
func TestUnlockRequestsAndPasskeys(t *testing.T) {
	ts := testkit.NewServer(t)
	ann, bob := ts.Member("ann"), ts.Member("bob")
	phone := ann.Sealed("android")
	phone.CreateBackup()
	requests := "/api/v1/me/unlock-requests"

	// A new, locked device asks. The account's other connections are
	// told; the asking one is not.
	newSession := ann.NewSession()
	desk := newSession.Sealed("linux")
	request, code, err := desk.D.NewUnlockRequest()
	if err != nil {
		t.Fatal(err)
	}
	phoneStream, _ := ann.Events(nil)
	deskStream, _ := newSession.Events(nil)
	// What the server refuses: a request that is not one, one for
	// another device, one from a session with no device.
	other := ann.NewSession().Sealed("windows")
	newSession.Refused(400, model.CodeInvalidValue, "POST", requests, map[string]any{"request": model.EncodeBytes([]byte("nonsense"))})
	other.C.Refused(400, model.CodeInvalidValue, "POST", requests, map[string]any{"request": model.EncodeBytes(request)})
	swapped := append([]byte{}, request...)
	swapped[40] ^= 1 // a byte of the signing key: the proof no longer verifies
	newSession.Refused(400, model.CodeInvalidValue, "POST", requests, map[string]any{"request": model.EncodeBytes(swapped)})
	ann.NewSession().Refused(403, model.CodeForbidden, "POST", requests, map[string]any{"request": model.EncodeBytes(request)})
	newSession.Refused(400, model.CodeInvalidRequest, "POST", requests, map[string]any{})
	made := newSession.Must(201, "POST", requests, map[string]any{"request": model.EncodeBytes(request)})["unlock_request"].(map[string]any)
	id := made["id"].(string)
	expires, _ := time.Parse(time.RFC3339Nano, made["expires_at"].(string))
	if d := time.Until(expires); d > 10*time.Minute || d < 9*time.Minute || made["device_id"] != desk.ID || made["request"] != model.EncodeBytes(request) {
		t.Fatalf("the unlock request: %v", made)
	}
	created := phoneStream.Expect("unlock_request.created")
	if got := created["unlock_request"].(map[string]any); got["id"] != id || got["device"].(map[string]any)["platform"] != "linux" || got["request"] != model.EncodeBytes(request) {
		t.Fatalf("unlock_request.created: %v", created)
	}
	// Someone else's account sees nothing of it.
	bob.Refused(404, model.CodeNotFound, "GET", requests+"/"+id, nil)
	if list := bob.Must(200, "GET", requests, nil)["unlock_requests"].([]any); len(list) != 0 {
		t.Fatalf("another account's unlock requests: %v", list)
	}
	bob.Token().Refused(403, model.CodeForbidden, "GET", requests, nil)

	// The answer: a locked copy of kind device for the asking device, and
	// nothing else.
	answer, err := phone.D.AnswerUnlockRequest(request, code)
	if err != nil {
		t.Fatal(err)
	}
	answerPath := requests + "/" + id + "/answer"
	body := func(kind, device string, key []byte) map[string]any {
		lc := map[string]any{"kind": kind, "key_version": 1, "locked_key": model.EncodeBytes(key)}
		if device != "" {
			lc["device_id"] = device
		}
		return map[string]any{"locked_copy": lc}
	}
	ann.Refused(400, model.CodeInvalidValue, "PUT", answerPath, body("phrase", "", answer))
	ann.Refused(400, model.CodeInvalidValue, "PUT", answerPath, body("device", phone.ID, answer))
	ann.Refused(400, model.CodeInvalidValue, "PUT", answerPath, body("device", desk.ID, answer[:100]))
	ann.Refused(404, model.CodeNotFound, "PUT", requests+"/"+string(model.NewID[model.MessageID]())+"/answer", body("device", desk.ID, answer))
	bob.Sealed("ios")
	bob.Refused(404, model.CodeNotFound, "PUT", answerPath, body("device", desk.ID, answer))
	// A session with no device cannot answer.
	ann.NewSession().Refused(403, model.CodeForbidden, "PUT", answerPath, body("device", desk.ID, answer))
	answered := ann.Must(200, "PUT", answerPath, body("device", desk.ID, answer))["unlock_request"].(map[string]any)
	if answered["answered_at"] == nil {
		t.Fatalf("the answered request: %v", answered)
	}
	// The asking device's connection is told, with the answer.
	got := deskStream.Expect("unlock_request.answered")["unlock_request"].(map[string]any)
	if got["id"] != id || got["answer"] != model.EncodeBytes(answer) {
		t.Fatalf("unlock_request.answered: %v", got)
	}
	// It no longer waits, and the copy is in the backup too.
	if list := ann.Must(200, "GET", requests, nil)["unlock_requests"].([]any); len(list) != 0 {
		t.Fatalf("an answered request still waits: %v", list)
	}
	if err := desk.UnlockWithDeviceCopy(); err != nil || !desk.D.Unlocked() {
		t.Fatalf("the asking device could not unlock: %v", err)
	}

	// A request expires after ten minutes: it stops waiting and can no
	// longer be answered.
	late, lateCode, _ := other.D.NewUnlockRequest()
	lateID := other.C.Must(201, "POST", requests, map[string]any{"request": model.EncodeBytes(late)})["unlock_request"].(map[string]any)["id"].(string)
	ts.Exec(`UPDATE unlock_requests SET expires_at = ? WHERE id = ?`, int64(model.Now().Add(-time.Second)), lateID)
	if list := ann.Must(200, "GET", requests, nil)["unlock_requests"].([]any); len(list) != 0 {
		t.Fatalf("an expired request still waits: %v", list)
	}
	lateAnswer, err := phone.D.AnswerUnlockRequest(late, lateCode)
	if err != nil {
		t.Fatal(err)
	}
	ann.Refused(409, model.CodeConflict, "PUT", requests+"/"+lateID+"/answer", body("device", other.ID, lateAnswer))

	// A passkey's unlock key: given by the page that holds the passkey.
	passkeys := ann.Must(200, "GET", "/api/v1/me/passkeys", nil)["passkeys"].([]any)
	first := passkeys[0].(map[string]any)["id"].(string)
	unlockPrivate := make([]byte, 32)
	rand.Read(unlockPrivate)
	unlockPublic := ref.X25519Public(unlockPrivate)
	keyPath := "/api/v1/me/passkeys/" + first + "/unlock-key"
	ann.Refused(400, model.CodeInvalidValue, "PUT", keyPath, map[string]any{"unlock_public": model.EncodeBytes(unlockPublic[:20])})
	ann.Refused(400, model.CodeInvalidRequest, "PUT", keyPath, map[string]any{})
	bob.Refused(404, model.CodeNotFound, "PUT", keyPath, map[string]any{"unlock_public": model.EncodeBytes(unlockPublic)})
	ann.Token().Refused(403, model.CodeForbidden, "PUT", keyPath, map[string]any{"unlock_public": model.EncodeBytes(unlockPublic)})
	ann.Refused(404, model.CodeNotFound, "PUT", "/api/v1/me/passkeys/"+string(model.NewID[model.PasskeyID]())+"/unlock-key", map[string]any{"unlock_public": model.EncodeBytes(unlockPublic)})
	// A copy for a passkey that has given no key is refused.
	blank := model.EncodeBytes(make([]byte, 144))
	ann.Refused(400, model.CodeInvalidValue, "POST", "/api/v1/me/backup/copies", map[string]any{"locked_copy": map[string]any{"kind": "passkey", "passkey_id": first, "key_version": 1, "locked_key": blank}})
	ann.Must(200, "PUT", keyPath, map[string]any{"unlock_public": model.EncodeBytes(unlockPublic)})
	// Unlocked devices are told, so they can ask their owner.
	phoneStream.Expect("backup.changed")
	update, err := phone.D.ApprovePasskey(ref.MustUUID(first), unlockPublic)
	if err != nil {
		t.Fatal(err)
	}
	// The account record must be the next revision.
	wrongRevision := map[string]any{
		"locked_copy":    map[string]any{"kind": "passkey", "passkey_id": first, "key_version": 1, "locked_key": model.EncodeBytes(update.Copy.Locked)},
		"account_record": map[string]any{"key_version": 1, "revision": update.Revision + 3, "record": model.EncodeBytes(update.AccountRecord)}}
	ann.Refused(409, model.CodeConflict, "POST", "/api/v1/me/backup/copies", wrongRevision)
	wrongRevision["account_record"].(map[string]any)["revision"] = update.Revision
	backup := ann.Must(201, "POST", "/api/v1/me/backup/copies", wrongRevision)["backup"].(map[string]any)
	if backup["account_record"].(map[string]any)["revision"] != float64(update.Revision) || len(backup["locked_copies"].([]any)) != 3 {
		t.Fatalf("the backup after approving a passkey: %v", backup)
	}
	phone.D.AdoptRecord(update)

	// Removing a passkey that had a locked copy: the copy goes with it,
	// and the backup key must be replaced by the next unlocked device.
	ann.Check()
	begin := ann.Must(200, "POST", "/api/v1/me/passkeys/begin", nil)
	ann.Must(201, "POST", "/api/v1/me/passkeys/finish", ann.CreatePasskey(begin))
	ann.Must(204, "DELETE", "/api/v1/me/passkeys/"+first, nil)
	phoneStream.ExpectWhere("backup.changed", func(f map[string]any) bool { return f["replacement_due"] == true })
	after := ann.Must(200, "GET", "/api/v1/me/backup", nil)["backup"].(map[string]any)
	if after["replacement_due"] != true || len(after["locked_copies"].([]any)) != 2 {
		t.Fatalf("the backup after its passkey was removed: %v", after)
	}
	// The unlocked device replaces the key; the way of the removed
	// passkey is left out, taken from the account record, not from the
	// server's rows.
	if err := phone.Lists(ann.ID); err != nil {
		t.Fatal(err)
	}
	bundle, err := phone.D.ReplaceBackup(ref.Replacement{Passkeys: []ref.UUID{ref.MustUUID(first)}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	replace := testkit.BundleBody(t, bundle)
	replace["replaces_key_version"], replace["records"] = bundle.Replaces, []any{}
	done := ann.Must(200, "POST", "/api/v1/me/backup/replace", replace)["backup"].(map[string]any)
	if done["replacement_due"] != false || done["key_version"] != float64(2) {
		t.Fatalf("the backup after its replacement: %v", done)
	}
}

// What the link asks of the sealed package before it stores a sealed event
// that arrived from another server (docs/SEALED.md section 17): the first
// record for a key epoch stands, and a message outside the current epoch
// is dropped.
func TestSealedRemoteChecks(t *testing.T) {
	w := newSealedWorld(t)
	a1 := w.annPhone.Open(w.bob.ID)
	b1 := w.bobPhone.Chat(a1.ID, w.ann.ID)
	b1.Sync()
	ctx := context.Background()
	svc, st := w.ts.Server.Sealed, w.ts.Server.Store
	conv := model.ConversationID(a1.ID)

	// The record that is already stored is not the next one: dropped.
	if _, drop, err := svc.CheckRemoteEpoch(ctx, st, conv, a1.C.Current().Stored); err != nil || !drop {
		t.Fatalf("a record for a key epoch that has one: drop=%v err=%v", drop, err)
	}
	two, _, err := b1.C.MakeEpoch()
	if err != nil {
		t.Fatal(err)
	}
	row, drop, err := svc.CheckRemoteEpoch(ctx, st, conv, two)
	if err != nil || drop || row.KeyEpoch != 2 || string(row.DeviceID) != w.bobPhone.ID || row.AccountID != w.bob.ID {
		t.Fatalf("the next record: %+v drop=%v err=%v", row, drop, err)
	}
	// A changed record, and bytes that are no record, are dropped quietly.
	bad := append([]byte{}, two...)
	bad[len(bad)-70] ^= 1
	if _, drop, err := svc.CheckRemoteEpoch(ctx, st, conv, bad); err != nil || !drop {
		t.Fatalf("a changed record: drop=%v err=%v", drop, err)
	}
	if _, drop, err := svc.CheckRemoteEpoch(ctx, st, conv, []byte("nonsense")); err != nil || !drop {
		t.Fatalf("nonsense: drop=%v err=%v", drop, err)
	}
	// Stored with the sequencer's number, it becomes the current epoch.
	if err := st.Tx(ctx, func(tx store.Tx) error {
		seq, err := tx.Conversations().NextSeq(ctx, conv)
		if err != nil {
			return err
		}
		row.Seq = seq
		return tx.Sealed().PutEpoch(ctx, row)
	}); err != nil {
		t.Fatal(err)
	}

	// A message in the old epoch is dropped; one in the current epoch is
	// kept, if its header says what the envelope says.
	old, err := a1.C.Seal(ref.MessageContent("in the old epoch", nil, nil, nil, nil))
	if err != nil {
		t.Fatal(err)
	}
	if drop, err := svc.CheckRemoteMessage(ctx, st, conv, w.ann.ID, int64(old.KeyEpoch), false, old.Ciphertext); err != nil || !drop {
		t.Fatalf("a message in an epoch that is not current: drop=%v err=%v", drop, err)
	}
	a1.C.Unsend(old)
	a1.Sync()
	cur, err := a1.C.Seal(ref.MessageContent("in the current epoch", nil, nil, nil, nil))
	if err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]struct {
		author model.AccountID
		epoch  int64
		quiet  bool
		drop   bool
	}{
		"as it is":                {w.ann.ID, 2, false, false},
		"under another author":    {w.bob.ID, 2, false, true},
		"with another quiet flag": {w.ann.ID, 2, true, true},
		"under another key epoch": {w.ann.ID, 1, false, true},
	} {
		if drop, err := svc.CheckRemoteMessage(ctx, st, conv, c.author, c.epoch, c.quiet, cur.Ciphertext); err != nil || drop != c.drop {
			t.Errorf("%s: drop=%v err=%v", name, drop, err)
		}
	}
	// A remote account's device list is stored only if it checks out, and
	// may be sent again.
	lists, err := svc.RemoteLists(ctx, w.ann.ID, 0)
	if err != nil || len(lists) != 2 {
		t.Fatalf("Ann's lists for the link: %d %v", len(lists), err)
	}
	if err := svc.StoreRemoteList(ctx, w.ann.ID, lists[1].Signed); err != nil {
		t.Fatalf("a list already held, sent again: %v", err)
	}
	changed := append([]byte{}, lists[1].Signed...)
	changed[len(changed)-70] ^= 1
	if err := svc.StoreRemoteList(ctx, w.ann.ID, changed); err == nil {
		t.Fatal("a changed list was stored")
	}
	if err := svc.StoreRemoteList(ctx, w.bob.ID, lists[1].Signed); err == nil {
		t.Fatal("Ann's list was stored as Bob's")
	}
}
