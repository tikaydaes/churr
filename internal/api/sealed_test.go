package api_test

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/tikaydaes/churr/internal/export"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/sealed/ref"
	"github.com/tikaydaes/churr/internal/testkit"
)

// Sealed chats through the real API, with the reference implementation
// playing the devices (docs/SEALED.md). The server stores and delivers
// bytes it cannot read; everything a device shows it has checked itself.

// sealedSecrets are things only the devices may know. The tests that send
// them scan the database and the file store for them afterwards.
var sealedSecrets = []string{
	"the vault code is 4471", "moonlit harbour rendezvous", "second thoughts about the merger",
	"floorplan-of-the-east-wing.png", "the caption nobody else reads", "FILE-BODY-SEVENTEEN-SWANS",
}

// plaintextPlaces says where the server holds any of the secrets: in the
// rows that carry text, in the events log, in the SQLite file itself (which
// includes the search index), or anywhere in the file store.
func plaintextPlaces(t *testing.T, ts *testkit.TestServer, secrets ...string) []string {
	t.Helper()
	hook, ok := ts.Server.Store.(interface {
		ExecForTest(ctx context.Context, query string, args ...any) (int64, error)
	})
	if !ok {
		t.Fatal("the store has no test hook")
	}
	var places []string
	// An UPDATE that changes nothing counts the rows a pattern matches,
	// on either database.
	for _, q := range []string{
		`UPDATE messages SET text = text WHERE text LIKE ? OR search_text LIKE ? OR fallback LIKE ?`,
		`UPDATE messages SET text = text WHERE CAST(entities AS TEXT) LIKE ? OR CAST(cards AS TEXT) LIKE ? OR CAST(system AS TEXT) LIKE ?`,
		`UPDATE events SET type = type WHERE CAST(payload AS TEXT) LIKE ? OR type LIKE ? OR type LIKE ?`,
		`UPDATE files SET name = name WHERE name LIKE ? OR caption LIKE ? OR preview_text LIKE ?`,
		`UPDATE message_versions SET text = text WHERE text LIKE ? OR CAST(entities AS TEXT) LIKE ? OR CAST(cards AS TEXT) LIKE ?`,
	} {
		for _, s := range secrets {
			p := "%" + s + "%"
			n, err := hook.ExecForTest(context.Background(), q, p, p, p)
			if err != nil {
				t.Fatalf("%s: %v", q, err)
			}
			if n != 0 {
				places = append(places, fmt.Sprintf("%q in the table %s", s, strings.Fields(q)[1]))
			}
		}
	}
	cfg := ts.Server.Config
	var files []string
	if !cfg.Postgres() {
		// The database file and its write-ahead log, byte for byte.
		matches, _ := filepath.Glob(cfg.DBPath() + "*")
		files = append(files, matches...)
	}
	filepath.WalkDir(cfg.FilesDir(), func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			files = append(files, path)
		}
		return nil
	})
	for _, path := range files {
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		for _, s := range secrets {
			if bytes.Contains(raw, []byte(s)) {
				places = append(places, fmt.Sprintf("%q in the file %s", s, filepath.Base(path)))
			}
		}
	}
	return places
}

// noPlaintext fails if the server holds any of the secrets anywhere.
func noPlaintext(t *testing.T, ts *testkit.TestServer, secrets ...string) {
	t.Helper()
	for _, place := range plaintextPlaces(t, ts, secrets...) {
		t.Errorf("the server holds %s in plain", place)
	}
}

// twoByTwo makes two accounts with two devices each, all unlocked and
// listed: each account's first device makes the backup, the second unlocks
// with the recovery phrase.
type sealedWorld struct {
	ts                 *testkit.TestServer
	ann, bob           *testkit.Client
	annPhone, annWeb   *testkit.SealedDevice
	bobPhone, bobWeb   *testkit.SealedDevice
	annPhrase, bobPhra string
}

func newSealedWorld(t *testing.T) *sealedWorld {
	t.Helper()
	w := &sealedWorld{ts: testkit.NewServer(t)}
	w.ann, w.bob = w.ts.Member("ann"), w.ts.Member("bob")
	w.annPhone = w.ann.Sealed("android")
	w.annPhrase = w.annPhone.CreateBackup()
	w.annWeb = w.ann.NewSession().Sealed("web")
	if err := w.annWeb.UnlockWithPhrase(w.annPhrase); err != nil {
		t.Fatal(err)
	}
	w.annWeb.Join()
	w.bobPhone = w.bob.Sealed("ios")
	w.bobPhra = w.bobPhone.CreateBackup()
	w.bobWeb = w.bob.NewSession().Sealed("web")
	if err := w.bobWeb.UnlockWithPhrase(w.bobPhra); err != nil {
		t.Fatal(err)
	}
	w.bobWeb.Join()
	return w
}

func wantTexts(t *testing.T, who string, c *testkit.SealedChat, want ...string) {
	t.Helper()
	c.Sync()
	if got := c.Texts(); !reflect.DeepEqual(got, want) {
		t.Fatalf("%s shows %q, want %q", who, got, want)
	}
}

func TestSealedChatEndToEnd(t *testing.T) {
	w := newSealedWorld(t)
	ts, ann, bob := w.ts, w.ann, w.bob

	// Creation: epoch record 1, sealed to all four devices.
	a1 := w.annPhone.Open(bob.ID)
	conv := a1.ID
	a2, b1, b2 := w.annWeb.Chat(conv, bob.ID), w.bobPhone.Chat(conv, ann.ID), w.bobWeb.Chat(conv, ann.ID)
	got := bob.Must(200, "GET", "/api/v1/conversations/"+conv, nil)["conversation"].(map[string]any)
	if got["kind"] != "sealed" || got["key_epoch"] != float64(1) || got["private"] != true || got["name"] != nil || len(got["member_ids"].([]any)) != 2 {
		t.Fatalf("the sealed chat: %v", got)
	}
	// There is one per pair: opening it again from the other side, with a
	// record of its own, returns the same chat and ignores the record.
	if again := w.bobPhone.Open(ann.ID); again.ID != conv {
		t.Fatalf("a second sealed chat for the pair: %s", again.ID)
	}

	// Messages from every device.
	hello := a1.Say(sealedSecrets[0])
	b1.Say(sealedSecrets[1])
	typo := a2.Say("see you at sevn")
	oops := b2.Say(sealedSecrets[2])
	all := []*testkit.SealedChat{a1, a2, b1, b2}
	for i, c := range all {
		wantTexts(t, fmt.Sprint("device ", i), c, sealedSecrets[0], sealedSecrets[1], "see you at sevn", sealedSecrets[2])
	}

	// A reaction, an edit and a checklist tick are quiet sealed messages.
	m, _ := b1.Send(ref.ReactionContent(hello, "👍", true))
	if m["quiet"] != true || m["text"] != nil || m["sealed"] == nil {
		t.Fatalf("a reaction as the server stores it: %v", m)
	}
	a2.Send(ref.EditContent(typo, "see you at seven", nil))
	// Bob cannot edit Ann's message: the device applies an edit only if
	// the sender's account wrote the target.
	b2.Send(ref.EditContent(typo, "hijacked", nil))
	// A delete: the sealed `delete`, and the row erased on the server.
	b2.Send(ref.DeleteContent(oops))
	var oopsRow string
	for _, v := range bob.Must(200, "GET", "/api/v1/conversations/"+conv+"/messages", nil)["messages"].([]any) {
		if m := v.(map[string]any); m["client_id"] == oops.String() {
			oopsRow = m["id"].(string)
		}
	}
	// Only its author may delete it (docs/PERMISSIONS.md section 6).
	ann.Refused(403, model.CodeForbidden, "DELETE", "/api/v1/messages/"+oopsRow, nil)
	marker := bob.Must(200, "DELETE", "/api/v1/messages/"+oopsRow, nil)["message"].(map[string]any)
	if marker["sealed"] != nil || marker["deleted_at"] == nil {
		t.Fatalf("the marker of a deleted sealed message: %v", marker)
	}
	bob.Refused(409, model.CodeConflict, "DELETE", "/api/v1/messages/"+oopsRow, nil)
	for i, c := range all {
		wantTexts(t, fmt.Sprint("device ", i), c, sealedSecrets[0], sealedSecrets[1], "see you at seven", "(deleted)")
		view := c.C.View()
		if !view[0].Reactions["👍"][ref.MustUUID(string(bob.ID))] || !view[2].Edited {
			t.Fatalf("device %d: reaction or edit not applied: %+v", i, view)
		}
	}

	// A file: encrypted on the device, uploaded as an unnamed blob, its
	// key, name and type inside the message.
	fileKey := make([]byte, 32)
	rand.Read(fileKey)
	body := bytes.Repeat([]byte(sealedSecrets[5]), 4000) // more than one chunk
	blob := ref.EncryptFile(fileKey, body)
	up := ann.Upload(conv, sealedSecrets[3], blob)
	if up["name"] != "" || up["media_type"] != "application/octet-stream" || up["kind"] != "file" || up["thumb_url"] != nil {
		t.Fatalf("an encrypted blob as the server files it: %v", up)
	}
	fileID := up["id"].(string)
	a1.Send(ref.MessageContent("the plan", nil, []ref.FileEntry{{ID: fileID, Key: ref.B64(fileKey), Name: sealedSecrets[3], MediaType: "image/png",
		Kind: "image", Size: int64(len(body)), Caption: sealedSecrets[4]}}, nil, nil), fileID)
	b2.Sync()
	view := b2.C.View()
	entry := view[len(view)-1].Files[0]
	if entry.Name != sealedSecrets[3] || entry.Caption != sealedSecrets[4] {
		t.Fatalf("the file entry Bob's device sees: %+v", entry)
	}
	status, _, raw := bob.Get("/api/v1/files/"+entry.ID+"/blob", nil)
	if status != 200 {
		t.Fatalf("downloading the blob = %d", status)
	}
	plain, err := ref.DecryptFile(ref.UnB64(entry.Key), raw)
	if err != nil || !bytes.Equal(plain, body) {
		t.Fatalf("the downloaded blob does not decrypt: %v", err)
	}
	// A third account cannot fetch the chat, its messages, its epoch
	// records or the blob (section 17, case 144).
	cat := ts.Member("cat")
	for _, path := range []string{"/api/v1/conversations/" + conv, "/api/v1/conversations/" + conv + "/messages",
		"/api/v1/conversations/" + conv + "/sealed/epochs", "/api/v1/files/" + entry.ID + "/blob"} {
		cat.Refused(404, model.CodeNotFound, "GET", path, nil)
	}

	// A device is added through a passkey. The page that holds the
	// passkey gives the server its unlock public key; Ann's phone, asked,
	// approves it. Later the unlock page, a browser session with no
	// device, opens the backup key with the passkey and stores a copy for
	// the new device's box key, which the device then finds.
	prf, credential := make([]byte, 32), []byte("the passkey's credential id")
	rand.Read(prf)
	unlockPrivate := ref.UnlockKeyFromPRF(prf, ref.MustUUID(string(ann.ID)), credential)
	passkey := ann.Must(200, "GET", "/api/v1/me/passkeys", nil)["passkeys"].([]any)[0].(map[string]any)["id"].(string)
	pk := ann.Must(200, "PUT", "/api/v1/me/passkeys/"+passkey+"/unlock-key", map[string]any{"unlock_public": model.EncodeBytes(ref.X25519Public(unlockPrivate))})["passkey"].(map[string]any)
	if pk["has_prf"] != true || pk["can_unlock"] != false || pk["unlock_public"] != model.EncodeBytes(ref.X25519Public(unlockPrivate)) {
		t.Fatalf("the passkey after giving its unlock key: %v", pk)
	}
	update, err := w.annPhone.D.ApprovePasskey(ref.MustUUID(passkey), ref.X25519Public(unlockPrivate))
	if err != nil {
		t.Fatal(err)
	}
	stored, _ := w.annPhone.Backup()
	ann.Must(201, "POST", "/api/v1/me/backup/copies", map[string]any{
		"locked_copy":    map[string]any{"kind": "passkey", "passkey_id": passkey, "key_version": stored.KeyVersion, "locked_key": model.EncodeBytes(update.Copy.Locked)},
		"account_record": map[string]any{"key_version": stored.KeyVersion, "revision": update.Revision, "record": model.EncodeBytes(update.AccountRecord)}})
	w.annPhone.D.AdoptRecord(update)
	if pk := ann.Must(200, "GET", "/api/v1/me/passkeys", nil)["passkeys"].([]any)[0].(map[string]any); pk["can_unlock"] != true {
		t.Fatalf("the passkey after its copy was stored: %v", pk)
	}
	annTablet := ann.NewSession().Sealed("android")
	// The unlock page: a browser signed in to the account, nothing more.
	pageSession := ann.NewSession()
	page := pageSession.SealedPage()
	if status, _, html := pageSession.Get("/oauth/unlock?device_id="+annTablet.ID, nil); status != 200 || !bytes.Contains(html, []byte("unlock.js")) {
		t.Fatalf("the unlock page = %d", status)
	}
	if err := page.Lists(ann.ID); err != nil {
		t.Fatal(err)
	}
	pageStored, pageBackup := page.Backup()
	if err := page.D.UnlockWithCopy(unlockPrivate, ref.WayPasskey, ref.MustUUID(passkey), page.Copy(pageBackup, "passkey", passkey), pageStored); err != nil {
		t.Fatalf("the page could not open the backup key with the passkey: %v", err)
	}
	forTablet, err := page.D.DeviceCopyFor(annTablet.D.ID, annTablet.D.BoxPublic())
	if err != nil {
		t.Fatal(err)
	}
	pageSession.Must(201, "POST", "/api/v1/me/backup/copies", map[string]any{"locked_copy": map[string]any{
		"kind": "device", "device_id": annTablet.ID, "key_version": pageStored.KeyVersion, "locked_key": model.EncodeBytes(forTablet)}})
	// A page may store a device copy and nothing else.
	pageSession.Refused(403, model.CodeForbidden, "POST", "/api/v1/me/backup/copies", map[string]any{"locked_copy": map[string]any{
		"kind": "phrase", "key_version": pageStored.KeyVersion, "locked_key": model.EncodeBytes(forTablet)}})
	if err := annTablet.UnlockWithDeviceCopy(); err != nil {
		t.Fatal(err)
	}
	a3 := annTablet.Chat(conv, bob.ID)
	annTablet.Join(a3)
	// It reads everything from before it existed, out of the backup. The
	// deleted message it never saw: its ciphertext was erased, and all
	// that is left on the server is the marker.
	wantTexts(t, "the new tablet", a3, sealedSecrets[0], sealedSecrets[1], "see you at seven", "the plan")
	a3.Say("from the tablet")
	if cur := a3.C.Current(); cur.KeyEpoch != 2 || len(cur.Record.Recipients) != 5 {
		t.Fatalf("the epoch the tablet made: %d with %d recipients", cur.KeyEpoch, len(cur.Record.Recipients))
	}
	b1.Sync()
	found := false
	for _, n := range w.bobPhone.D.Notices {
		found = found || (n.Kind == ref.NoticeDeviceAdded && n.Account == ref.MustUUID(string(ann.ID)) && n.Platform == ref.PlatformAndroid)
	}
	if !found {
		t.Fatalf("Bob's phone was not shown that Ann added a device: %+v", w.bobPhone.D.Notices)
	}

	// A device is added by approval from another device: the new device
	// shows a code, the old one checks it against the request it got
	// through the server.
	bobDesk := bob.NewSession().Sealed("macos")
	request, code, err := bobDesk.D.NewUnlockRequest()
	if err != nil {
		t.Fatal(err)
	}
	ur := bobDesk.C.Must(201, "POST", "/api/v1/me/unlock-requests", map[string]any{"request": model.EncodeBytes(request)})["unlock_request"].(map[string]any)
	waiting := bob.Must(200, "GET", "/api/v1/me/unlock-requests", nil)["unlock_requests"].([]any)
	if len(waiting) != 1 || waiting[0].(map[string]any)["device"].(map[string]any)["platform"] != "macos" {
		t.Fatalf("the waiting unlock requests: %v", waiting)
	}
	got1, _ := model.DecodeBytes(waiting[0].(map[string]any)["request"].(string))
	if _, err := w.bobPhone.D.AnswerUnlockRequest(got1, "00000-00000-00000-00000"); !errors.Is(err, ref.ErrCode) {
		t.Fatalf("a wrong code must stop the approval: %v", err)
	}
	answer, err := w.bobPhone.D.AnswerUnlockRequest(got1, strings.ToLower(code))
	if err != nil {
		t.Fatal(err)
	}
	bobStored, _ := w.bobPhone.Backup()
	answerBody := map[string]any{"locked_copy": map[string]any{"kind": "device", "device_id": bobDesk.ID, "key_version": bobStored.KeyVersion, "locked_key": model.EncodeBytes(answer)}}
	bob.Must(200, "PUT", "/api/v1/me/unlock-requests/"+ur["id"].(string)+"/answer", answerBody)
	bob.Refused(409, model.CodeConflict, "PUT", "/api/v1/me/unlock-requests/"+ur["id"].(string)+"/answer", answerBody)
	// Only the asking device is given the answer.
	if seen := bob.Must(200, "GET", "/api/v1/me/unlock-requests/"+ur["id"].(string), nil)["unlock_request"].(map[string]any); seen["answer"] != nil || seen["answered_at"] == nil {
		t.Fatalf("another device was given the answer: %v", seen)
	}
	mine := bobDesk.C.Must(200, "GET", "/api/v1/me/unlock-requests/"+ur["id"].(string), nil)["unlock_request"].(map[string]any)
	answerBytes, _ := model.DecodeBytes(mine["answer"].(string))
	if err := bobDesk.Lists(bob.ID); err != nil {
		t.Fatal(err)
	}
	deskStored, _ := bobDesk.Backup()
	if err := bobDesk.D.UnlockWithDeviceCopy(answerBytes, deskStored); err != nil {
		t.Fatal(err)
	}
	b3 := bobDesk.Chat(conv, ann.ID)
	bobDesk.Join(b3)
	wantTexts(t, "Bob's desktop", b3, sealedSecrets[0], sealedSecrets[1], "see you at seven", "the plan", "from the tablet")
	cat.Refused(404, model.CodeNotFound, "GET", "/api/v1/me/unlock-requests/"+ur["id"].(string), nil)

	// A device is removed: Bob signs out on the browser. As its last act
	// it publishes a list without itself; its session then ends.
	if err := w.bobWeb.Lists(bob.ID); err != nil {
		t.Fatal(err)
	}
	without, err := w.bobWeb.D.ListWithout(w.bobWeb.D.ID)
	if err != nil {
		t.Fatal(err)
	}
	w.bobWeb.Publish(without)
	// A sender that has not heard of the change is stopped by the server:
	// the chat's epoch names a list older than the newest.
	stale, err := a1.C.Seal(ref.MessageContent("too early", nil, nil, nil, nil))
	if err != nil {
		t.Fatal(err)
	}
	ann.Refused(409, model.CodeConflict, "POST", "/api/v1/conversations/"+conv+"/messages", map[string]any{"client_id": stale.ClientID.String(),
		"sealed": map[string]any{"key_epoch": stale.KeyEpoch, "ciphertext": model.EncodeBytes(stale.Ciphertext)}})
	a1.C.Unsend(stale)
	w.bobWeb.C.Must(204, "DELETE", "/api/v1/me/devices/"+w.bobWeb.ID, nil)
	w.bobWeb.C.Refused(401, model.CodeUnauthenticated, "GET", "/api/v1/me/backup", nil)
	b1.MakeEpoch()
	afterRemoval := b1.Say("only for the devices still listed")
	wantTexts(t, "Ann's phone", a1, sealedSecrets[0], sealedSecrets[1], "see you at seven", "(deleted)", "the plan", "from the tablet", "only for the devices still listed")
	// The removed device can fetch nothing. Even handed the stored bytes,
	// it cannot read what followed: the new epoch's secret was never
	// sealed to it.
	epochs := ann.Must(200, "GET", "/api/v1/conversations/"+conv+"/sealed/epochs?from=1", nil)["epochs"].([]any)
	for _, v := range epochs {
		e := v.(map[string]any)
		rec, _ := model.DecodeBytes(e["record"].(string))
		for _, account := range []model.AccountID{ann.ID, bob.ID} {
			// It is given the device lists too, by someone who can
			// still fetch them.
			lists, _ := w.annPhone.D.FetchLists(ref.MustUUID(string(account)), 0)
			w.bobWeb.D.LearnLists(ref.MustUUID(string(account)), lists)
		}
		b2.C.AcceptEpoch(uint64(e["key_epoch"].(float64)), int64(e["seq"].(float64)), rec)
	}
	last := b2.C.Current()
	if _, in := last.Record.Recipient(w.bobWeb.D.ID); in || last.History != nil || !last.Accepted {
		t.Fatalf("the removed device and the epoch made without it: accepted=%v key=%v", last.Accepted, last.History != nil)
	}
	for _, v := range ann.Must(200, "GET", "/api/v1/conversations/"+conv+"/messages?limit=200", nil)["messages"].([]any) {
		m := v.(map[string]any)
		if m["client_id"] != afterRemoval.String() {
			continue
		}
		s := m["sealed"].(map[string]any)
		ct, _ := model.DecodeBytes(s["ciphertext"].(string))
		_, err := b2.C.Receive(ref.Row{Seq: int64(m["seq"].(float64)), Author: ref.MustUUID(string(bob.ID)), KeyEpoch: uint64(s["key_epoch"].(float64)), Ciphertext: ct})
		if !errors.Is(err, ref.ErrNoKey) {
			t.Fatalf("a removed device reading a later message: %v", err)
		}
	}

	// The comparison code: both people's devices show the same 60 digits.
	codeA, err := w.annPhone.D.CompareCodeWith(ref.MustUUID(string(bob.ID)), ts.Server.Config.Hostname)
	if err != nil {
		t.Fatal(err)
	}
	b1.Sync()
	codeB, _ := w.bobPhone.D.CompareCodeWith(ref.MustUUID(string(ann.ID)), ts.Server.Config.Hostname)
	if codeA != codeB || len(strings.ReplaceAll(codeA, " ", "")) != 60 {
		t.Fatalf("comparison codes: %q and %q", codeA, codeB)
	}

	// The server never held any of it in plain.
	noPlaintext(t, ts, append(sealedSecrets, "see you at seven", "from the tablet", "only for the devices still listed")...)
	// And has nothing to search (section 17, case 146).
	for _, q := range []string{"vault", "harbour", "seven"} {
		if res := ann.Must(200, "GET", "/api/v1/search?q="+q, nil); len(res["results"].([]any)) != 0 {
			t.Fatalf("search found something in a sealed chat: %v", res)
		}
	}
	// The owner's export leaves the sealed chat out entirely (ruling
	// R15): not its messages, not its blobs, not even its identifier.
	var archive bytes.Buffer
	if err := export.Write(context.Background(), &archive, export.Options{Store: ts.Server.Store, Blobs: ts.Server.Blobs}); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(archive.Bytes()), int64(archive.Len()))
	if err != nil {
		t.Fatal(err)
	}
	var sealedRow map[string]any
	for _, v := range ann.Must(200, "GET", "/api/v1/conversations/"+conv+"/messages?limit=200", nil)["messages"].([]any) {
		if m := v.(map[string]any); m["sealed"] != nil {
			sealedRow = m
		}
	}
	ciphertext := sealedRow["sealed"].(map[string]any)["ciphertext"].(string)
	rawCiphertext, _ := model.DecodeBytes(ciphertext)
	if len(zr.File) == 0 {
		t.Fatal("the export is empty")
	}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		content, _ := io.ReadAll(rc)
		rc.Close()
		for what, needle := range map[string][]byte{
			"the chat's identifier": []byte(conv), "a sealed message's identifier": []byte(sealedRow["id"].(string)),
			"a ciphertext": []byte(ciphertext), "a ciphertext's bytes": rawCiphertext[:64], "the encrypted blob": blob[:64],
			"the blob's identifier": []byte(fileID),
		} {
			if bytes.Contains(content, needle) || strings.Contains(f.Name, conv) || strings.Contains(f.Name, fileID) {
				t.Errorf("the export's %s holds %s of a sealed chat", f.Name, what)
			}
		}
	}

	// No device refused anything along the way.
	for i, d := range []*testkit.SealedDevice{w.annPhone, w.annWeb, w.bobPhone, annTablet, bobDesk} {
		if len(d.Problems) != 0 {
			t.Errorf("device %d refused: %v", i, d.Problems)
		}
	}
}
