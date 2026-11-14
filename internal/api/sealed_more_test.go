package api_test

import (
	"crypto/rand"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/sealed/ref"
	"github.com/tikaydaes/churr/internal/testkit"
)

// The scan for plaintext must be able to find some: a message, a file name
// and a file body posted in an ordinary channel are all found.
func TestPlaintextScanFindsPlaintext(t *testing.T) {
	ts := testkit.NewServer(t)
	ann := ts.Member("ann")
	ch := ann.Channel("ops", false)
	ann.Post(ch, "canary-in-a-message")
	ann.Upload(ch, "canary-file-name.txt", []byte("canary-in-a-file-body, and some more text so it is a text file"))
	for _, secret := range []string{"canary-in-a-message", "canary-file-name", "canary-in-a-file-body"} {
		if places := plaintextPlaces(t, ts, secret); len(places) == 0 {
			t.Errorf("the scan did not find %q, which the server does hold", secret)
		}
	}
}

func hasError(list []error, target error) bool {
	for _, err := range list {
		if errors.Is(err, target) {
			return true
		}
	}
	return false
}

func hasEpochRefusal(list []error) bool {
	for _, err := range list {
		var e *ref.ErrEpoch
		if errors.As(err, &e) {
			return true
		}
	}
	return false
}

// A server that tampers with or withholds what it stores is detected where
// docs/SEALED.md section 16 says it is. The tampering is done by writing
// to the database behind the server's back.
func TestSealedServerMisbehaves(t *testing.T) {
	w := newSealedWorld(t)
	ts, ann, bob := w.ts, w.ann, w.bob
	a1 := w.annPhone.Open(bob.ID)
	conv := a1.ID
	b1 := w.bobPhone.Chat(conv, ann.ID)
	row := func(clientID ref.UUID) (id string, seq int64, ciphertext []byte) {
		t.Helper()
		for _, v := range ann.Must(200, "GET", "/api/v1/conversations/"+conv+"/messages?limit=200", nil)["messages"].([]any) {
			m := v.(map[string]any)
			if m["client_id"] == clientID.String() {
				ct, _ := model.DecodeBytes(m["sealed"].(map[string]any)["ciphertext"].(string))
				return m["id"].(string), int64(m["seq"].(float64)), ct
			}
		}
		t.Fatalf("no row for %s", clientID)
		return "", 0, nil
	}

	// A changed message: the signature no longer verifies. It is shown
	// as "A message could not be checked" and its content is never drawn.
	a1.Say("one")
	two := a1.Say("two")
	a1.Say("three")
	id, _, ct := row(two)
	ct[ref.HeaderSize+ref.SaltSize+2] ^= 1
	ts.Exec(`UPDATE messages SET ciphertext = ? WHERE id = ?`, ct, id)
	if problems := b1.Sync(); !hasError(problems, ref.ErrMessage) {
		t.Fatalf("a changed message was not refused: %v", problems)
	}
	if got := strings.Join(b1.Texts(), "|"); got != "one|three" || b1.C.Unchecked != 1 {
		t.Fatalf("after a changed message Bob shows %q with %d unchecked", got, b1.C.Unchecked)
	}

	// A replayed message: the same device and counter at a second
	// sequence number. The lower one is the message; the other is dropped.
	four := a1.Say("four")
	id, _, _ = row(four)
	ts.Exec(`INSERT INTO messages (id, conversation_id, seq, author_id, quiet, ciphertext, key_epoch, created_at)
		SELECT ?, conversation_id, (SELECT next_seq FROM conversations WHERE id = ?), author_id, quiet, ciphertext, key_epoch, created_at
		FROM messages WHERE id = ?`, string(model.NewID[model.MessageID]()), conv, id)
	ts.Exec(`UPDATE conversations SET next_seq = next_seq + 1 WHERE id = ?`, conv)
	if problems := b1.Sync(); len(problems) != 0 || w.bobPhone.Dropped != 1 {
		t.Fatalf("a replayed message: problems %v, dropped %d", problems, w.bobPhone.Dropped)
	}
	if got := strings.Join(b1.Texts(), "|"); got != "one|three|four" {
		t.Fatalf("after a replay Bob shows %q", got)
	}

	// A withheld message in the middle: a jump in the device's counters,
	// shown as "A message may be missing here."
	a1.Say("five")
	six := a1.Say("six")
	a1.Say("seven")
	id, _, _ = row(six)
	ts.Exec(`DELETE FROM messages WHERE id = ?`, id)
	gaps := w.bobPhone.Gaps
	b1.Sync()
	if w.bobPhone.Gaps != gaps+1 {
		t.Fatalf("a withheld message left no gap: %d", w.bobPhone.Gaps-gaps)
	}

	// Messages delivered out of order: the later one by counter sits at
	// the lower sequence number. It is shown, with a mark.
	eight := a1.Say("eight")
	nine := a1.Say("nine")
	id8, seq8, _ := row(eight)
	id9, seq9, _ := row(nine)
	ts.Exec(`UPDATE messages SET seq = ? WHERE id = ?`, -1, id8)
	ts.Exec(`UPDATE messages SET seq = ? WHERE id = ?`, seq8, id9)
	ts.Exec(`UPDATE messages SET seq = ? WHERE id = ?`, seq9, id8)
	b1.Sync()
	if w.bobPhone.OutOfOrder != 1 {
		t.Fatalf("reordered messages were not marked: %d", w.bobPhone.OutOfOrder)
	}

	// A changed quiet flag: the flag is under the signature.
	_, reaction := a1.Send(ref.ReactionContent(two, "🎉", true))
	id, _, _ = row(reaction)
	ts.Exec(`UPDATE messages SET quiet = ? WHERE id = ?`, false, id)
	if problems := b1.Sync(); !hasError(problems, ref.ErrMessage) {
		t.Fatalf("a changed quiet flag was not refused: %v", problems)
	}

	// A second, different record for a key epoch the device already
	// holds: the first stands.
	if err := b1.C.AcceptEpoch(1, 1, append([]byte{}, b1.C.Current().Stored[:len(b1.C.Current().Stored)-1]...)); !errors.Is(err, ref.ErrSecondRecord) {
		t.Fatalf("a second record for one key epoch: %v", err)
	}

	// A changed epoch record: its maker's signature fails. Devices refuse
	// it, show that the chat's keys are out of step, and the next device
	// to send repairs the chat by making the next epoch itself, with prev
	// the hash of the refused record's stored bytes.
	b1.MakeEpoch()
	cur := b1.C.Current()
	bad := append([]byte{}, cur.Stored...)
	bad[len(bad)-ref.SignatureSize-1] ^= 1 // the last byte of `at`
	ts.Exec(`UPDATE sealed_epochs SET record = ? WHERE conversation_id = ? AND key_epoch = ?`, bad, conv, int64(cur.KeyEpoch))
	if problems := a1.Sync(); !hasEpochRefusal(problems) {
		t.Fatalf("a changed epoch record was not refused: %v", problems)
	}
	if need, why := a1.C.NeedsEpoch(); !need || why != "refused" {
		t.Fatalf("after a refused record: %v %s", need, why)
	}
	found := false
	for _, n := range w.annPhone.D.Notices {
		found = found || n.Kind == ref.NoticeOutOfStep
	}
	if !found {
		t.Fatal("Ann's phone did not show that the chat's keys are out of step")
	}
	a1.Say("after the repair")
	// A device that never saw the bad record accepts the repair too: it
	// checks prev against the stored bytes of the refused record.
	a2 := w.annWeb.Chat(conv, bob.ID)
	problems := a2.Sync()
	if !hasEpochRefusal(problems) || !a2.C.Current().Accepted || a2.C.Current().KeyEpoch != cur.KeyEpoch+1 {
		t.Fatalf("the repair as a second device sees it: %v, current %d accepted %v", problems, a2.C.Current().KeyEpoch, a2.C.Current().Accepted)
	}
	if texts := a2.Texts(); texts[len(texts)-1] != "after the repair" {
		t.Fatalf("after the repair Ann's browser shows %q", texts)
	}

	// A changed backup record: it no longer opens.
	ts.Exec(`UPDATE key_backups SET record = ? WHERE account_id = ? AND conversation_id = ? AND key_epoch = ?`,
		make([]byte, 97), string(bob.ID), conv, 1)
	bobNew := bob.NewSession().Sealed("linux")
	if err := bobNew.UnlockWithPhrase(w.bobPhra); err != nil {
		t.Fatal(err)
	}
	b4 := bobNew.Chat(conv, ann.ID)
	b4.Sync()
	if bad := b4.ReadBackup(); !hasError(bad, ref.ErrBackupRecord) {
		t.Fatalf("a changed backup record was not refused: %v", bad)
	}
	if e := b4.C.Epoch(1); e.History != nil {
		t.Fatal("a history secret was taken from a backup record that does not open")
	}

	// A locked copy the server made itself: a key of its own choosing,
	// sealed to the phrase's public unlock key, with an account record
	// and a signature to match. A device with a pin refuses it, because
	// the account key is not the one in the account's device list.
	entropy, _ := ref.ReadPhrase(w.annPhrase)
	fakeKey, fakeAccount := ref.Fixed("the server's own backup key", 32), ref.SigningKey(ref.Fixed("the server's own account key", 32))
	annID := ref.MustUUID(string(ann.ID))
	fakeRecord, err := ref.LockAccountRecord(fakeKey, &ref.AccountRecord{Format: 1, AccountID: annID, KeyVersion: 1, Revision: 9,
		AccountKey: ref.Public(fakeAccount), AccountKeyPrivate: fakeAccount.Seed()}, ref.Fixed("salt", 16))
	if err != nil {
		t.Fatal(err)
	}
	fakeCopy, err := ref.MakeLockedCopy(ref.X25519Public(ref.UnlockKeyFromPhrase(entropy, annID)), annID, 1, ref.WayPhrase, ref.UUID{}, fakeKey, fakeAccount, ref.Fixed("one-time", 32))
	if err != nil {
		t.Fatal(err)
	}
	ts.Exec(`UPDATE account_records SET record = ?, revision = ? WHERE account_id = ?`, fakeRecord, 9, string(ann.ID))
	ts.Exec(`UPDATE backup_keys SET locked_key = ? WHERE account_id = ? AND kind = ?`, fakeCopy, string(ann.ID), "phrase")
	annNew := ann.NewSession().Sealed("linux")
	if err := annNew.UnlockWithPhrase(w.annPhrase); !errors.Is(err, ref.ErrUnlock) || annNew.D.Unlocked() {
		t.Fatalf("a locked copy the server made itself was accepted: %v", err)
	}

	// An epoch record withheld: a device that is given the records on
	// either side of it sees the gap in the chain.
	ts.Exec(`DELETE FROM sealed_epochs WHERE conversation_id = ? AND key_epoch = ?`, conv, 2)
	bobLate := bob.NewSession().Sealed("windows")
	if err := bobLate.UnlockWithPhrase(w.bobPhra); err != nil {
		t.Fatal(err)
	}
	if problems := bobLate.Chat(conv, ann.ID).Sync(); !hasError(problems, ref.ErrChainGap) {
		t.Fatalf("a withheld epoch record was not noticed: %v", problems)
	}

	// A changed device list, later: the step from the pin does not check
	// out. The device keeps its pin and shows that the server sent a
	// list that does not check out.
	if err := w.annWeb.Lists(ann.ID); err != nil {
		t.Fatal(err)
	}
	next, err := w.annWeb.D.ListWithout(w.annWeb.D.ID)
	if err != nil {
		t.Fatal(err)
	}
	w.annWeb.Publish(next)
	parsed, _ := ref.DecodeDeviceList(next.List)
	stored, _ := next.Encode()
	stored[4+len(next.List)-1] ^= 1 // the last byte of the list record: `at`
	ts.Exec(`UPDATE device_lists SET signed_list = ? WHERE account_id = ? AND version = ?`, stored, string(ann.ID), int64(parsed.Version))
	pin := w.bobPhone.D.Pins[annID].Newest
	var listErr *ref.ErrList
	if err := w.bobPhone.Lists(ann.ID); !errors.As(err, &listErr) {
		t.Fatalf("a changed device list was not refused: %v", err)
	}
	if w.bobPhone.D.Pins[annID].Newest != pin {
		t.Fatal("the pin moved to a list that does not check out")
	}
	last := w.bobPhone.D.Notices[len(w.bobPhone.D.Notices)-1]
	if last.Kind != ref.NoticeListRefused {
		t.Fatalf("no notice for the refused list: %+v", last)
	}
}

// Everything a sealed chat lacks is refused, or passes it by
// (docs/SEALED.md section 1; docs/PERMISSIONS.md section 17, cases 84 and
// 144 to 148).
func TestSealedChatLacks(t *testing.T) {
	w := newSealedWorld(t)
	ts, ann, bob := w.ts, w.ann, w.bob
	a1 := w.annPhone.Open(bob.ID)
	conv := a1.ID
	b1 := w.bobPhone.Chat(conv, ann.ID)
	stored, _ := a1.Send(ref.MessageContent("https://example.org/a-link badword", nil, nil, nil, nil))
	msg := stored["id"].(string)
	messages := "/api/v1/conversations/" + conv + "/messages"

	// Only sealed messages go in: no plain text, no card, no form (case
	// 147), no thread and no quote, which are inside the ciphertext.
	sealedBody := func() map[string]any {
		out, err := a1.C.Seal(ref.MessageContent("x", nil, nil, nil, nil))
		if err != nil {
			t.Fatal(err)
		}
		a1.C.Unsend(out)
		return map[string]any{"client_id": out.ClientID.String(), "sealed": map[string]any{"key_epoch": out.KeyEpoch, "ciphertext": model.EncodeBytes(out.Ciphertext)}}
	}
	ann.Refused(400, model.CodeInvalidRequest, "POST", messages, map[string]any{"text": "in the clear"})
	for field, value := range map[string]any{
		"text":      "in the clear",
		"cards":     []map[string]any{{"title": "a card"}},
		"form":      map[string]any{"fields": []any{}, "action": map[string]any{"type": "tally"}},
		"parent_id": msg,
	} {
		body := sealedBody()
		body[field] = value
		ann.Refused(400, model.CodeInvalidRequest, "POST", messages, body)
	}
	ann.Refused(400, model.CodeInvalidRequest, "POST", messages, map[string]any{"quiet": true})
	// A sealed payload into an ordinary channel is refused too.
	ch := ann.Channel("ops", false, bob)
	ann.Refused(400, model.CodeInvalidRequest, "POST", "/api/v1/conversations/"+ch+"/messages", sealedBody())

	// What the server checks of a sealed message (section 8.2).
	good := sealedBody()
	for name, change := range map[string]func(b map[string]any){
		"no client_id":          func(b map[string]any) { delete(b, "client_id") },
		"another client_id":     func(b map[string]any) { b["client_id"] = string(model.NewID[model.MessageID]()) },
		"a quiet flag it lacks": func(b map[string]any) { b["quiet"] = true },
		"another key epoch":     func(b map[string]any) { b["sealed"].(map[string]any)["key_epoch"] = 7 },
		"not a sealed message": func(b map[string]any) {
			b["sealed"].(map[string]any)["ciphertext"] = model.EncodeBytes([]byte("short"))
		},
		"eleven files": func(b map[string]any) {
			ids := []string{}
			for i := 0; i < 11; i++ {
				ids = append(ids, string(model.NewID[model.FileID]()))
			}
			b["file_ids"] = ids
		},
	} {
		body := sealedBody()
		change(body)
		if status, out := ann.Do("POST", messages, body); status != 400 {
			t.Errorf("%s: %d %v", name, status, out)
		}
	}
	// Bob cannot post a message whose header names Ann's device.
	bob.Refused(400, model.CodeInvalidValue, "POST", messages, good)
	big := sealedBody()
	big["sealed"].(map[string]any)["ciphertext"] = model.EncodeBytes(make([]byte, 128*1024+1))
	ann.Refused(413, model.CodeTooLarge, "POST", messages, big)
	// The same client_id again returns the message already stored.
	first := ann.Must(201, "POST", messages, good)["message"].(map[string]any)
	if again := ann.Must(200, "POST", messages, good)["message"].(map[string]any); again["id"] != first["id"] {
		t.Fatalf("a repeated client_id made a second message: %v", again)
	}
	a1.C.Counter++ // the message above used the next counter
	// A session with no registered device cannot post, and neither can a
	// personal token: a script holds no device keys.
	ann.NewSession().Refused(403, model.CodeForbidden, "POST", messages, sealedBody())
	ann.Token().Refused(403, model.CodeForbidden, "POST", messages, sealedBody())
	ann.Token().Refused(403, model.CodeForbidden, "GET", "/api/v1/me/backup", nil)
	ann.Token().Refused(403, model.CodeForbidden, "POST", "/api/v1/sealed", map[string]any{"account_id": bob.ID, "epoch_record": ""})

	// No edit in place, no history, no reactions, pins, reports, forms,
	// scheduled messages or purge requests on the server.
	ann.Refused(400, model.CodeInvalidRequest, "PATCH", "/api/v1/messages/"+msg, map[string]any{"sealed": good["sealed"]})
	ann.Refused(400, model.CodeInvalidRequest, "PATCH", "/api/v1/messages/"+msg, map[string]any{"text": "edited in the clear"})
	ann.Refused(403, model.CodeForbidden, "GET", "/api/v1/messages/"+msg+"/history", nil)
	bob.Refused(403, model.CodeForbidden, "PUT", "/api/v1/messages/"+msg+"/reactions/👍", nil)
	ann.Refused(403, model.CodeForbidden, "PUT", "/api/v1/conversations/"+conv+"/pins/"+msg, nil)
	bob.Refused(403, model.CodeForbidden, "POST", "/api/v1/messages/"+msg+"/report", map[string]any{"reason": "spam"})
	if status, out := ann.Do("POST", "/api/v1/scheduled", map[string]any{"conversation_id": conv, "text": "later", "send_at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339)}); status != 403 && status != 400 {
		t.Errorf("scheduling into a sealed chat = %d %v", status, out)
	}
	// Nobody can be added (case 145), no bot can be in one (case 84), and
	// it cannot be left.
	cat := ts.Member("cat")
	ann.Refused(403, model.CodeForbidden, "POST", "/api/v1/conversations/"+conv+"/members", map[string]any{"account_ids": []string{string(cat.ID)}})
	bot := ts.Owner.Bot("helper")
	ann.Refused(403, model.CodeForbidden, "POST", "/api/v1/conversations/"+conv+"/members", map[string]any{"account_ids": []string{string(bot.ID)}})
	ann.Refused(403, model.CodeForbidden, "POST", "/api/v1/sealed", map[string]any{"account_id": bot.ID, "epoch_record": model.EncodeBytes(a1.C.Current().Stored)})
	bot.Refused(403, model.CodeForbidden, "POST", "/api/v1/sealed", map[string]any{"account_id": ann.ID, "epoch_record": ""})
	bot.Refused(404, model.CodeNotFound, "GET", messages, nil)
	if status, _ := ann.Do("POST", "/api/v1/conversations/"+conv+"/leave", nil); status != 409 {
		t.Errorf("leaving a sealed chat = %d", status)
	}
	// Nothing was previewed, filtered or indexed: the message with a link
	// and a filtered word is stored as it was sent, and search finds
	// nothing.
	b1.Sync()
	if texts := b1.Texts(); texts[0] != "https://example.org/a-link badword" {
		t.Fatalf("Bob's device shows %q", texts)
	}
	for _, q := range []string{"example", "badword", "link"} {
		for _, who := range []*testkit.Client{ann, bob, ts.Owner} {
			if res := who.Must(200, "GET", "/api/v1/search?q="+q, nil); len(res["results"].([]any)) != 0 {
				t.Fatalf("search for %q found something in a sealed chat: %v", q, res)
			}
		}
	}
	// The owner and admins see no sealed chat anywhere (case 35).
	ts.Owner.Refused(404, model.CodeNotFound, "GET", "/api/v1/conversations/"+conv, nil)
	ts.Owner.Refused(404, model.CodeNotFound, "GET", messages, nil)
	if listed := fmt.Sprint(ts.Owner.Must(200, "GET", "/api/v1/admin/channels", nil)); strings.Contains(listed, conv) {
		t.Fatal("a sealed chat is in the admin's channel list")
	}

	// A quiet message does not notify and does not count as unread.
	before := bob.Must(200, "GET", "/api/v1/conversations/"+conv, nil)["conversation"].(map[string]any)["membership"].(map[string]any)
	stream, _ := bob.Events(nil)
	a1.Send(ref.ReactionContent(ref.MustUUID(stored["client_id"].(string)), "👍", true))
	e := stream.Expect("message.created")
	if e["notify"] != "none" || e["message"].(map[string]any)["quiet"] != true || e["message"].(map[string]any)["text"] != nil {
		t.Fatalf("a quiet sealed message as Bob's connection got it: %v", e)
	}
	after := bob.Must(200, "GET", "/api/v1/conversations/"+conv, nil)["conversation"].(map[string]any)["membership"].(map[string]any)
	if after["unread_count"] != before["unread_count"] || after["badge_count"] != before["badge_count"] {
		t.Fatalf("a quiet message changed the counts: %v -> %v", before, after)
	}
	a1.Say("an ordinary one")
	e = stream.Expect("message.created")
	if e["notify"] != "notify" {
		t.Fatalf("an ordinary sealed message: notify %v", e["notify"])
	}
	now := bob.Must(200, "GET", "/api/v1/conversations/"+conv, nil)["conversation"].(map[string]any)["membership"].(map[string]any)
	if now["unread_count"].(float64) != before["unread_count"].(float64)+1 {
		t.Fatalf("an ordinary sealed message and the unread count: %v -> %v", before, now)
	}

	// The admin switch: sealed chats off. None can be started (case 148),
	// existing ones become read-only, and reading still works.
	ts.Set("sealed.enabled", false)
	dan := ts.Member("dan")
	ann.Refused(403, model.CodeFeatureOff, "POST", "/api/v1/sealed", map[string]any{"account_id": dan.ID, "epoch_record": model.EncodeBytes(a1.C.Current().Stored)})
	ann.Refused(403, model.CodeForbidden, "POST", messages, sealedBody())
	ann.Refused(403, model.CodeFeatureOff, "POST", "/api/v1/conversations/"+conv+"/sealed/epochs", map[string]any{"key_epoch": 2, "record": model.EncodeBytes(a1.C.Current().Stored)})
	ann.Refused(403, model.CodeFeatureOff, "GET", "/api/v1/accounts/"+string(bob.ID)+"/device-lists", nil)
	if c := ann.Must(200, "GET", "/api/v1/conversations/"+conv, nil)["conversation"].(map[string]any); c["read_only"] != true {
		t.Fatalf("a sealed chat while sealed chats are off: %v", c)
	}
	ann.Must(200, "GET", messages, nil)
	ann.Must(200, "GET", "/api/v1/conversations/"+conv+"/sealed/epochs", nil)
	if s := ann.Must(200, "GET", "/api/v1/server", nil)["server"].(map[string]any); s["features"].(map[string]any)["sealed_chats"] != false {
		t.Fatalf("the server's description: %v", s["features"])
	}
	ts.Set("sealed.enabled", true)
	if c := ann.Must(200, "GET", "/api/v1/conversations/"+conv, nil)["conversation"].(map[string]any); c["read_only"] == true {
		t.Fatal("still read-only with sealed chats back on")
	}
	a1.Say("back on")

	// Blocks: someone Bob has blocked cannot start a sealed chat with
	// him, and is not told why.
	eve := ts.Member("eve")
	bob.Must(204, "PUT", "/api/v1/me/blocks/"+string(eve.ID), nil)
	eveDev := eve.Sealed("web")
	eveDev.CreateBackup()
	eve.Refused(404, model.CodeNotFound, "GET", "/api/v1/accounts/"+string(bob.ID)+"/device-lists", nil)
	eve.Refused(403, model.CodeForbidden, "POST", "/api/v1/sealed", map[string]any{"account_id": bob.ID, "epoch_record": model.EncodeBytes(a1.C.Current().Stored)})
}

// The epoch record operations and their refusals (section 5.5).
func TestSealedEpochRules(t *testing.T) {
	w := newSealedWorld(t)
	ts, ann, bob := w.ts, w.ann, w.bob
	sealedPath := "/api/v1/sealed"

	// A new chat needs epoch record 1, naming a UUIDv7 the device made,
	// the two accounts, their newest lists and every device on them.
	for _, account := range []model.AccountID{ann.ID, bob.ID} {
		if err := w.annPhone.Lists(account); err != nil {
			t.Fatal(err)
		}
	}
	make1 := func(id string) []byte {
		c := w.annPhone.Chat(id, bob.ID)
		rec, _, err := c.C.MakeEpoch()
		if err != nil {
			t.Fatal(err)
		}
		delete(w.annPhone.D.Chats, ref.MustUUID(id))
		return rec
	}
	ann.Refused(400, model.CodeInvalidValue, "POST", sealedPath, map[string]any{"account_id": bob.ID})
	ann.Refused(400, model.CodeInvalidValue, "POST", sealedPath, map[string]any{"account_id": bob.ID, "epoch_record": model.EncodeBytes([]byte("not a record"))})
	// Not a UUIDv7.
	ann.Refused(400, model.CodeInvalidValue, "POST", sealedPath, map[string]any{"account_id": bob.ID, "epoch_record": model.EncodeBytes(make1("0193a0e0-0000-4000-8000-0000000000c0"))})
	// An identifier already in use.
	ch := ann.Channel("ops", false)
	ann.Refused(400, model.CodeInvalidValue, "POST", sealedPath, map[string]any{"account_id": bob.ID, "epoch_record": model.EncodeBytes(make1(ch))})
	// A record for another pair.
	cat := ts.Member("cat")
	good := make1(string(model.NewID[model.ConversationID]()))
	ann.Refused(400, model.CodeInvalidValue, "POST", sealedPath, map[string]any{"account_id": cat.ID, "epoch_record": model.EncodeBytes(good)})
	// A changed record: the maker's signature fails.
	bad := append([]byte{}, good...)
	bad[len(bad)-70] ^= 1
	ann.Refused(400, model.CodeInvalidValue, "POST", sealedPath, map[string]any{"account_id": bob.ID, "epoch_record": model.EncodeBytes(bad)})
	// With oneself, and with nobody.
	ann.Refused(400, model.CodeInvalidValue, "POST", sealedPath, map[string]any{"account_id": ann.ID, "epoch_record": model.EncodeBytes(good)})
	ann.Refused(404, model.CodeNotFound, "POST", sealedPath, map[string]any{"account_id": string(model.NewID[model.AccountID]()), "epoch_record": model.EncodeBytes(good)})
	// None of that made a chat.
	if list := ann.Must(200, "GET", "/api/v1/conversations?kind=sealed", nil)["conversations"].([]any); len(list) != 0 {
		t.Fatalf("a refused record left a chat behind: %v", list)
	}

	stream, _ := bob.Events(nil)
	a1 := w.annPhone.Open(bob.ID)
	conv := a1.ID
	e := stream.Expect("sealed.epoch_started")
	epoch := e["epoch"].(map[string]any)
	if e["conversation_id"] != conv || e["seq"] == nil || epoch["key_epoch"] != float64(1) || epoch["device_id"] != w.annPhone.ID ||
		epoch["account_id"] != string(ann.ID) || epoch["record"] != model.EncodeBytes(a1.C.Current().Stored) {
		t.Fatalf("sealed.epoch_started: %v", e)
	}
	// It is a stored conversation event: a client that was away catches up.
	events := bob.Must(200, "GET", "/api/v1/conversations/"+conv+"/events?after=0", nil)["events"].([]any)
	seen := false
	for _, v := range events {
		seen = seen || v.(map[string]any)["type"] == "sealed.epoch_started"
	}
	if !seen {
		t.Fatalf("sealed.epoch_started is not in the catch-up log: %v", events)
	}
	b1 := w.bobPhone.Chat(conv, ann.ID)
	b1.Sync()
	epochs := "/api/v1/conversations/" + conv + "/sealed/epochs"

	// Two devices make epoch 2 at once: one record wins, and the loser is
	// handed the winner beside the error.
	annTwo, _, err := a1.C.MakeEpoch()
	if err != nil {
		t.Fatal(err)
	}
	bobTwo, _, err := b1.C.MakeEpoch()
	if err != nil {
		t.Fatal(err)
	}
	// Not the next number, and a number that disagrees with the record.
	ann.Refused(400, model.CodeInvalidValue, "POST", epochs, map[string]any{"key_epoch": 3, "record": model.EncodeBytes(annTwo)})
	ann.Must(201, "POST", epochs, map[string]any{"key_epoch": 2, "record": model.EncodeBytes(annTwo)})
	status, out := bob.Do("POST", epochs, map[string]any{"key_epoch": 2, "record": model.EncodeBytes(bobTwo)})
	winner, _ := out["epoch"].(map[string]any)
	if status != 409 || out["error"].(map[string]any)["code"] != "conflict" || winner == nil || winner["record"] != model.EncodeBytes(annTwo) || winner["key_epoch"] != float64(2) {
		t.Fatalf("the loser of the race: %d %v", status, out)
	}
	if problems := b1.Sync(); len(problems) != 0 || b1.C.Current().KeyEpoch != 2 {
		t.Fatalf("the loser taking in the winner: %v", problems)
	}
	if need, _ := b1.C.NeedsEpoch(); need {
		t.Fatal("the loser still wants an epoch, though the winner covers it")
	}
	a1.Sync()
	list := ann.Must(200, "GET", epochs+"?from=2", nil)
	if got := list["epochs"].([]any); len(got) != 1 || got[0].(map[string]any)["key_epoch"] != float64(2) || list["has_more"] != false {
		t.Fatalf("listing from epoch 2: %v", list)
	}
	if page := ann.Must(200, "GET", epochs+"?limit=1", nil); len(page["epochs"].([]any)) != 1 || page["has_more"] != true {
		t.Fatalf("a page of one: %v", page)
	}
	// A third account, and a session that is no device.
	cat.Refused(404, model.CodeNotFound, "GET", epochs, nil)
	cat.Refused(404, model.CodeNotFound, "POST", epochs, map[string]any{"key_epoch": 3, "record": model.EncodeBytes(annTwo)})
	ann.NewSession().Refused(403, model.CodeForbidden, "POST", epochs, map[string]any{"key_epoch": 3, "record": model.EncodeBytes(annTwo)})
	ann.Refused(400, model.CodeInvalidValue, "POST", epochs, map[string]any{"key_epoch": 3, "record": model.EncodeBytes(annTwo)})
	ann.Refused(400, model.CodeInvalidRequest, "POST", epochs, map[string]any{"key_epoch": 3})

	// A record that names an older list than the previous record did, or
	// leaves a device out, is refused: the device set is not chosen.
	third := bob.NewSession().Sealed("linux")
	if err := third.UnlockWithPhrase(w.bobPhra); err != nil {
		t.Fatal(err)
	}
	stale, _, err := a1.C.MakeEpoch() // made before Ann's phone hears of Bob's new device
	if err != nil {
		t.Fatal(err)
	}
	b3 := third.Chat(conv, ann.ID)
	third.Join(b3) // publishes Bob's next list and makes epoch 3
	if status, out := ann.Do("POST", epochs, map[string]any{"key_epoch": 4, "record": model.EncodeBytes(stale)}); status != 400 {
		t.Fatalf("a record with a stale list and the wrong number: %d %v", status, out)
	}

	// The device lists: only the next version, only one's own, only with
	// registered devices.
	lists := "/api/v1/me/device-lists"
	if err := w.annPhone.Lists(ann.ID); err != nil {
		t.Fatal(err)
	}
	listStream, _ := bob.Events(nil)
	next, err := w.annPhone.D.ListWithout(w.annWeb.D.ID)
	if err != nil {
		t.Fatal(err)
	}
	parsed, _ := ref.DecodeDeviceList(next.List)
	body := map[string]any{"version": parsed.Version, "list": model.EncodeBytes(next.List), "sig": model.EncodeBytes(next.Sig)}
	bob.Refused(400, model.CodeInvalidValue, "POST", lists, map[string]any{"version": parsed.Version + 1, "list": body["list"], "sig": body["sig"]})
	ann.Refused(409, model.CodeConflict, "POST", lists, map[string]any{"version": parsed.Version + 1, "list": body["list"], "sig": body["sig"]})
	ann.Refused(400, model.CodeInvalidValue, "POST", lists, map[string]any{"version": parsed.Version, "list": body["list"], "sig": model.EncodeBytes(make([]byte, 64))})
	ann.Refused(400, model.CodeInvalidValue, "POST", lists, map[string]any{"version": parsed.Version, "list": body["list"]})
	ann.NewSession().Refused(403, model.CodeForbidden, "POST", lists, body)
	v := ann.Must(201, "POST", lists, body)["version"].(map[string]any)
	if v["version"] != float64(parsed.Version) || v["list"] != body["list"] || v["sig"] != body["sig"] || v["account_id"] != string(ann.ID) {
		t.Fatalf("the published version: %v", v)
	}
	ann.Refused(409, model.CodeConflict, "POST", lists, body)
	// It is announced to the other person of each sealed chat.
	changed := listStream.ExpectWhere("device_list.changed", func(f map[string]any) bool { return f["account_id"] == string(ann.ID) })
	if changed["version"] != float64(parsed.Version) {
		t.Fatalf("device_list.changed: %v", changed)
	}
	// Fetching: the newest only with no `after`; every later version with it.
	if got := bob.Must(200, "GET", "/api/v1/accounts/"+string(ann.ID)+"/device-lists", nil)["versions"].([]any); len(got) != 1 || got[0].(map[string]any)["version"] != float64(parsed.Version) {
		t.Fatalf("the newest version: %v", got)
	}
	if got := bob.Must(200, "GET", "/api/v1/accounts/"+string(ann.ID)+"/device-lists?after=0", nil)["versions"].([]any); len(got) != int(parsed.Version) || got[0].(map[string]any)["version"] != float64(1) {
		t.Fatalf("every version: %v", got)
	}
	// Someone who has no sealed chat with Ann and has never been allowed
	// one (a guest who cannot see her) is told nothing.
	guest := ts.Guest("gil", ts.Owner.Channel("lobby", false))
	guest.Refused(404, model.CodeNotFound, "GET", "/api/v1/accounts/"+string(ann.ID)+"/device-lists", nil)
}

// The backup's own operations: records, the account record, copies, a key
// replacement and a reset (section 13).
func TestSealedBackupLifecycle(t *testing.T) {
	w := newSealedWorld(t)
	ts, ann, bob := w.ts, w.ann, w.bob
	a1 := w.annPhone.Open(bob.ID)
	conv := a1.ID
	a2, b1 := w.annWeb.Chat(conv, bob.ID), w.bobPhone.Chat(conv, ann.ID)
	a1.Say("before the replacement")
	a2.Sync()
	b1.Sync()

	// Creating a backup twice: the first is kept.
	second, err := w.annWeb.D.CreateBackup()
	if err == nil {
		ann.Refused(409, model.CodeAlreadyExists, "POST", "/api/v1/me/backup", testkit.BundleBody(t, second))
	}
	// Restore the browser's own state: it holds the real keys.
	if err := w.annWeb.UnlockWithPhrase(w.annPhrase); err != nil {
		t.Fatal(err)
	}
	// An account with none is told so.
	cat := ts.Member("cat")
	cat.Refused(404, model.CodeNotFound, "GET", "/api/v1/me/backup", nil)

	// Backup records: the first for a chat and epoch is kept.
	recordPath := fmt.Sprintf("/api/v1/me/backup/records/%s/1", conv)
	records := a1.Records()
	if len(records) != 1 || records[0].KeyEpoch != 1 {
		t.Fatalf("Ann's backup records: %+v", records)
	}
	other := make([]byte, 97)
	rand.Read(other)
	kept := ann.Must(200, "PUT", recordPath, map[string]any{"key_version": 1, "record": model.EncodeBytes(other)})["record"].(map[string]any)
	if kept["record"] != model.EncodeBytes(records[0].Locked) {
		t.Fatal("a later write replaced the first backup record")
	}
	ann.Refused(400, model.CodeInvalidValue, "PUT", recordPath, map[string]any{"key_version": 1, "record": model.EncodeBytes(other[:50])})
	ann.Refused(400, model.CodeInvalidValue, "PUT", recordPath, map[string]any{"key_version": 5, "record": model.EncodeBytes(other)})
	ann.Refused(404, model.CodeNotFound, "PUT", fmt.Sprintf("/api/v1/me/backup/records/%s/9", conv), map[string]any{"key_version": 1, "record": model.EncodeBytes(other)})
	cat.Refused(403, model.CodeForbidden, "PUT", recordPath, map[string]any{"key_version": 1, "record": model.EncodeBytes(other)})
	ann.NewSession().Refused(403, model.CodeForbidden, "GET", "/api/v1/me/backup/records", nil)

	// The account record: only the next revision of the current version.
	stored, _ := w.annPhone.Backup()
	recordBody := func(revision uint64) map[string]any {
		return map[string]any{"key_version": stored.KeyVersion, "revision": revision, "record": model.EncodeBytes(stored.AccountRecord)}
	}
	ann.Refused(409, model.CodeConflict, "PUT", "/api/v1/me/backup/account-record", recordBody(stored.Revision+2))
	ann.Refused(409, model.CodeConflict, "PUT", "/api/v1/me/backup/account-record", recordBody(stored.Revision))
	ann.Refused(400, model.CodeInvalidValue, "PUT", "/api/v1/me/backup/account-record", map[string]any{"key_version": 1, "revision": 2, "record": "AAAA"})
	// A locked copy: the wrong size, the wrong way, someone else's device.
	copies := "/api/v1/me/backup/copies"
	blank := model.EncodeBytes(make([]byte, 144))
	ann.Refused(400, model.CodeInvalidValue, "POST", copies, map[string]any{"locked_copy": map[string]any{"kind": "phrase", "key_version": 1, "locked_key": model.EncodeBytes(make([]byte, 100))}})
	ann.Refused(400, model.CodeInvalidValue, "POST", copies, map[string]any{"locked_copy": map[string]any{"kind": "device", "device_id": w.bobPhone.ID, "key_version": 1, "locked_key": blank}})
	ann.Refused(400, model.CodeInvalidValue, "POST", copies, map[string]any{"locked_copy": map[string]any{"kind": "smoke", "key_version": 1, "locked_key": blank}})
	ann.Refused(409, model.CodeConflict, "POST", copies, map[string]any{"locked_copy": map[string]any{"kind": "phrase", "key_version": 4, "locked_key": blank}})
	cat.Refused(403, model.CodeForbidden, "POST", copies, map[string]any{"locked_copy": map[string]any{"kind": "phrase", "key_version": 1, "locked_key": blank}})

	// A key replacement: Ann's browser is revoked from her phone, which
	// replaces the backup key and the account key in one step.
	stream, _ := ann.Events(nil)
	ann.Must(204, "DELETE", "/api/v1/me/devices/"+w.annWeb.ID, nil)
	retired := stream.Expect("device.retired")
	if retired["device"].(map[string]any)["id"] != w.annWeb.ID {
		t.Fatalf("device.retired: %v", retired)
	}
	// A device revoked from another one held the backup key: until the
	// key is replaced the server says a replacement is due.
	stream.ExpectWhere("backup.changed", func(f map[string]any) bool { return f["replacement_due"] == true })
	if _, b := w.annPhone.Backup(); b["replacement_due"] != true {
		t.Fatalf("after a device was revoked: %v", b)
	}
	// The retired device is still listed, until an unlocked device has
	// taken it out of the device list.
	listed := false
	for _, v := range ann.Must(200, "GET", "/api/v1/me/devices", nil)["devices"].([]any) {
		d := v.(map[string]any)
		listed = listed || (d["id"] == w.annWeb.ID && d["retired_at"] != nil)
	}
	if !listed {
		t.Fatal("the retired device is no longer in the list of devices")
	}
	if err := w.annPhone.Lists(ann.ID); err != nil {
		t.Fatal(err)
	}
	bundle, err := w.annPhone.D.ReplaceBackup(ref.Replacement{Devices: []ref.UUID{w.annWeb.D.ID}}, a1.Records())
	if err != nil {
		t.Fatal(err)
	}
	replaceBody := testkit.BundleBody(t, bundle)
	replaceBody["replaces_key_version"] = bundle.Replaces
	relocked := []map[string]any{}
	for _, r := range bundle.Records {
		relocked = append(relocked, map[string]any{"conversation_id": r.ConversationID.String(), "key_epoch": r.KeyEpoch, "key_version": r.KeyVersion, "record": model.EncodeBytes(r.Locked)})
	}
	replaceBody["records"] = relocked
	// It must replace the version that is current.
	wrong := map[string]any{}
	for k, v := range replaceBody {
		wrong[k] = v
	}
	wrong["replaces_key_version"] = 7
	if status, _ := ann.Do("POST", "/api/v1/me/backup/replace", wrong); status != 400 && status != 409 {
		t.Fatalf("replacing a version that is not current = %d", status)
	}
	replaced := ann.Must(200, "POST", "/api/v1/me/backup/replace", replaceBody)["backup"].(map[string]any)
	if replaced["key_version"] != float64(2) || replaced["replacement_due"] != false {
		t.Fatalf("the backup after its replacement: %v", replaced)
	}
	// The same version cannot be replaced twice.
	ann.Refused(409, model.CodeConflict, "POST", "/api/v1/me/backup/replace", replaceBody)
	if err := w.annPhone.D.Adopt(bundle); err != nil {
		t.Fatal(err)
	}
	stream.ExpectWhere("backup.changed", func(f map[string]any) bool { return f["key_version"] == float64(2) && f["replacement_due"] == false })
	a1.MakeEpoch()
	a1.Say("after the replacement")
	// The retired device has left the list of devices with the list.
	for _, v := range ann.Must(200, "GET", "/api/v1/me/devices", nil)["devices"].([]any) {
		if v.(map[string]any)["id"] == w.annWeb.ID {
			t.Fatal("a retired device that is in no device list is still shown")
		}
	}
	// Bob's device is shown that Ann's security key changed, and still
	// reads: the old key signed the change.
	if problems := b1.Sync(); len(problems) != 0 {
		t.Fatalf("Bob's phone after Ann's key change: %v", problems)
	}
	keyChange := false
	for _, n := range w.bobPhone.D.Notices {
		keyChange = keyChange || n.Kind == ref.NoticeKeyChange
	}
	if texts := b1.Texts(); !keyChange || texts[len(texts)-1] != "after the replacement" {
		t.Fatalf("Bob's phone after Ann's key change: notices %+v, texts %q", w.bobPhone.D.Notices, texts)
	}
	// A new device of Ann's unlocks with the same phrase under the new
	// key, and reads history from the records that were locked again.
	annNew := ann.NewSession().Sealed("linux")
	if err := annNew.UnlockWithPhrase(w.annPhrase); err != nil {
		t.Fatal(err)
	}
	a3 := annNew.Chat(conv, bob.ID)
	annNew.Join(a3)
	if texts := a3.Texts(); len(texts) != 2 || texts[0] != "before the replacement" {
		t.Fatalf("a device added after the replacement shows %q", texts)
	}

	// A reset: Bob has no way of unlocking left. It needs a fresh check.
	bobNew := bob.NewSession()
	resetDev := bobNew.Sealed("ios")
	if err := resetDev.Lists(bob.ID); err != nil {
		t.Fatal(err)
	}
	reset, err := resetDev.D.ResetBackup()
	if err != nil {
		t.Fatal(err)
	}
	bobNew.Stale()
	bobNew.Refused(403, model.CodeFreshCheckRequired, "POST", "/api/v1/me/backup/reset", testkit.BundleBody(t, reset))
	bobNew.Check()
	// A reset's list must be marked reset and hold only the device.
	after := bobNew.Must(200, "POST", "/api/v1/me/backup/reset", testkit.BundleBody(t, reset))["backup"].(map[string]any)
	if after["key_version"] != float64(2) || len(after["locked_copies"].([]any)) != 1 {
		t.Fatalf("the backup after a reset: %v", after)
	}
	if err := resetDev.D.Adopt(reset); err != nil {
		t.Fatal(err)
	}
	if recs := resetDev.Chat(conv, ann.ID).Records(); len(recs) != 0 {
		t.Fatalf("backup records survived a reset: %d", len(recs))
	}
	b5 := resetDev.Chat(conv, ann.ID)
	b5.Sync()
	b5.MakeEpoch()
	b5.Say("starting again")
	if texts := b5.Texts(); len(texts) != 1 {
		t.Fatalf("a reset device reads earlier messages: %q", texts)
	}
	// Ann's devices show the reset, loudly, and receive from then on.
	if problems := a1.Sync(); len(problems) != 0 {
		t.Fatalf("Ann's phone after Bob's reset: %v", problems)
	}
	sawReset := false
	for _, n := range w.annPhone.D.Notices {
		sawReset = sawReset || (n.Kind == ref.NoticeReset && n.Account == ref.MustUUID(string(bob.ID)))
	}
	if texts := a1.Texts(); !sawReset || texts[len(texts)-1] != "starting again" {
		t.Fatalf("Ann's phone after Bob's reset: notices %+v, texts %q", w.annPhone.D.Notices, texts)
	}
	// Bob's old phone holds the old key: the server now gives it a
	// backup it cannot open, and its account key is no longer the list's.
	if err := w.bobPhone.UnlockWithPhrase(w.bobPhra); err == nil {
		t.Fatal("the old phrase opened the backup made by a reset")
	}

	// Deleting a locked copy.
	_, annBackup := w.annPhone.Backup()
	var phraseCopy string
	for _, v := range annBackup["locked_copies"].([]any) {
		if c := v.(map[string]any); c["kind"] == "phrase" {
			phraseCopy = c["id"].(string)
		}
	}
	bob.Refused(404, model.CodeNotFound, "DELETE", "/api/v1/me/backup/copies/"+phraseCopy, nil)
	ann.Must(204, "DELETE", "/api/v1/me/backup/copies/"+phraseCopy, nil)
	ann.Refused(404, model.CodeNotFound, "DELETE", "/api/v1/me/backup/copies/"+phraseCopy, nil)
}
