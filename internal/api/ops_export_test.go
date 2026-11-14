package api_test

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/server"
	"github.com/tikaydaes/churr/internal/store"
	"github.com/tikaydaes/churr/internal/testkit"
)

// unzip reads an archive into a map of name to content.
func unzip(t *testing.T, data []byte) map[string][]byte {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("the export is not a zip archive: %v", err)
	}
	out := map[string][]byte{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		b, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		if _, twice := out[f.Name]; twice {
			t.Fatalf("%s is in the archive twice", f.Name)
		}
		out[f.Name] = b
	}
	return out
}

// sealedThings is what the test puts in a sealed chat, to look for later.
type sealedThings struct {
	conversation model.ConversationID
	message      model.MessageID
	file         model.FileID
	ciphertext   []byte
	fileContent  []byte
}

// needles are the byte strings that would show something of the sealed chat
// had reached an archive.
func (s sealedThings) needles() map[string][]byte {
	return map[string][]byte{
		"the sealed chat's identifier":       []byte(s.conversation),
		"the sealed message's identifier":    []byte(s.message),
		"the sealed file's identifier":       []byte(s.file),
		"the ciphertext":                     s.ciphertext,
		"the ciphertext in base64":           []byte(base64.StdEncoding.EncodeToString(s.ciphertext)),
		"the sealed file's content":          s.fileContent,
		"the sealed chat's member key":       []byte("s:"),
		"the name of the sealed chat's file": []byte("sealed-attachment.bin"),
	}
}

// makeSealed stores a sealed chat between two accounts as the sealed
// package stores one: a conversation of kind `sealed`, a message that is
// ciphertext, a file that is ciphertext, and around it the things that
// could name it: a report, a time-out and a line of the audit log.
func makeSealed(t *testing.T, ts *testkit.TestServer, a, b model.AccountID) sealedThings {
	t.Helper()
	ctx := context.Background()
	st := ts.Server.Store
	out := sealedThings{ciphertext: []byte("CIPHERTEXT-zq81-nobody-can-read-this"), fileContent: []byte("SEALED-FILE-BYTES-7731-also-ciphertext")}
	c := model.Conversation{Kind: model.KindSealed, Private: true, CreatedByID: a, MemberKey: store.SealedMemberKey(a, b)}
	if err := st.Conversations().Create(ctx, &c); err != nil {
		t.Fatal(err)
	}
	out.conversation = c.ID
	for _, id := range []model.AccountID{a, b} {
		if _, err := st.Members().AddDirect(ctx, c.ID, id, model.LevelMember, model.Now()); err != nil {
			t.Fatal(err)
		}
	}
	m := model.Message{ID: model.NewID[model.MessageID](), ConversationID: c.ID, AuthorID: a, CreatedAt: model.Now(),
		Sealed: &model.SealedPayload{KeyEpoch: 1, Ciphertext: out.ciphertext}}
	out.message = m.ID
	key, size, sum, err := ts.Server.Blobs.Put(ctx, bytes.NewReader(out.fileContent))
	if err != nil {
		t.Fatal(err)
	}
	f := model.File{ConversationID: c.ID, MessageID: m.ID, UploaderID: a, Purpose: "attachment", Name: "sealed-attachment.bin",
		Size: size, SHA256: sum, MediaType: "application/octet-stream", Kind: "file", BlobKey: key}
	err = st.Tx(ctx, func(tx store.Tx) error {
		seq, err := tx.Conversations().NextSeq(ctx, c.ID)
		if err != nil {
			return err
		}
		m.Seq = seq
		if err := tx.Messages().Insert(ctx, &m, nil); err != nil {
			return err
		}
		if err := tx.Files().Insert(ctx, &f); err != nil {
			return err
		}
		if _, err := tx.Moderation().CreateReport(ctx, &model.Report{MessageID: m.ID, ConversationID: c.ID, Source: "report", ReportedByID: b, Reason: "sealed report reason"}); err != nil {
			return err
		}
		if err := tx.Moderation().CreateTimeout(ctx, &model.Timeout{AccountID: b, ConversationID: c.ID, CreatedByID: a, Until: model.Now().Add(time.Hour), Reason: "sealed timeout"}); err != nil {
			return err
		}
		if err := tx.Audit().Append(ctx, model.AuditEntry{At: model.Now(), ActorKind: "server", Action: "test.sealed_target", TargetKind: "conversation", TargetID: string(c.ID)}); err != nil {
			return err
		}
		return tx.Audit().Append(ctx, model.AuditEntry{At: model.Now(), ActorKind: "server", Action: "test.sealed_detail",
			Detail: map[string]any{"conversation_id": string(c.ID)}})
	})
	if err != nil {
		t.Fatal(err)
	}
	out.file = f.ID
	return out
}

// TestExport holds what the owner's export is: everything the server can
// read, private and ad-hoc channels included, in the layout of
// docs/SCHEMA.md section 18; nothing of a sealed chat, not even that it
// exists; no credential and no secret; the owner's alone to make; and
// written to the audit log.
func TestExport(t *testing.T) {
	ts := testkit.NewServer(t)
	ctx := context.Background()
	owner, admin := ts.Owner, ts.Admin("ada")
	ann, bob, cat := ts.Member("ann"), ts.Member("bob"), ts.Member("cat")
	const conv, msgs = "/api/v1/conversations/", "/api/v1/messages/"

	// A public channel, a private channel the owner is not in, and an
	// ad-hoc channel the owner is not in.
	town := ann.Channel("town", false, bob)
	den := ann.Channel("den", true, bob)
	_, chatOut := ann.Do("POST", "/api/v1/chats", map[string]any{"account_ids": []string{string(bob.ID), string(cat.ID)}})
	adhoc := convOf(chatOut)["id"].(string)

	first := ann.Post(town, "PUBLIC-WORDS-1 hello town")["id"].(string)
	bob.Must(201, "POST", conv+town+"/messages", map[string]any{"text": "THREAD-REPLY-2", "parent_id": first})
	bob.Must(200, "PUT", msgs+first+"/reactions/"+url.PathEscape("🎉"), nil)
	ann.Must(204, "PUT", conv+town+"/pins/"+first, nil)
	secret := ann.Post(den, "PRIVATE-WORDS-3 before the edit")["id"].(string)
	// Past the quick-correction window, so the edit leaves a version.
	ts.Exec(`UPDATE messages SET created_at = created_at - 600000 WHERE id = ?`, secret)
	ann.Must(200, "PATCH", msgs+secret, map[string]any{"text": "PRIVATE-WORDS-4 after the edit"})
	ann.Post(adhoc, "ADHOC-WORDS-5 just us three")
	fileBody := []byte("FILE-CONTENT-6 the notes\n")
	f := ann.Upload(den, "notes.txt", fileBody)
	ann.Must(201, "POST", conv+den+"/messages", map[string]any{"text": "with a file", "file_ids": []any{f["id"]}})
	// A named poll with an answer, and an anonymous one.
	named := fID(fPost(ann, town, fPoll(nil)))
	bob.Must(200, "PUT", fForm(named)+"/answer", fPick("o1"))
	anon := fID(fPost(ann, town, fPoll(map[string]any{"anonymous": true, "title": "Secret ballot"})))
	bob.Must(200, "PUT", fForm(anon)+"/answer", fPick("o2"))
	// A custom emoji.
	if s, body := admin.Form("POST", "/api/v1/emoji", testkit.Part{Field: "name", Content: []byte("party")}, testkit.Part{Field: "file", FileName: "e.png", Content: pngBytes(64, 64)}); s != 201 {
		t.Fatalf("adding an emoji: %d %v", s, body)
	}
	// Secrets that must not leave: a sealed object-store secret.
	ts.Set("files.s3_secret", []byte("SEALED-S3-SECRET"))
	ts.Set("welcome.text", "WELCOME-TEXT-7")

	sealed := makeSealed(t, ts, ann.ID, bob.ID)

	// Only the owner. An admin is refused each of the three.
	for _, who := range []*testkit.Client{admin, ann} {
		who.Refused(403, model.CodeForbidden, "POST", "/api/v1/admin/export", nil) // docs/PERMISSIONS.md case 43
		who.Refused(403, model.CodeForbidden, "GET", "/api/v1/admin/export", nil)
		who.Refused(403, model.CodeForbidden, "GET", "/api/v1/admin/export/download", nil)
	}
	ts.Browser().Refused(401, model.CodeUnauthenticated, "POST", "/api/v1/admin/export", nil)
	// Before any export: nothing to see, nothing to download.
	if got := owner.Must(200, "GET", "/api/v1/admin/export", nil); got["export"] != nil {
		t.Fatalf("before any export: %v", got)
	}
	owner.Refused(404, model.CodeNotFound, "GET", "/api/v1/admin/export/download", nil)

	started := obj(owner.Must(201, "POST", "/api/v1/admin/export", nil), "export")
	if started["state"] != "running" || started["started_by_id"] != string(owner.ID) || !model.ValidID(started["id"].(string)) || started["started_at"] == nil {
		t.Fatalf("a started export: %v", started)
	}
	ts.Server.API.Export.Wait()
	latest := obj(owner.Must(200, "GET", "/api/v1/admin/export", nil), "export")
	if latest["state"] != "ready" || latest["id"] != started["id"] || latest["finished_at"] == nil || latest["size"].(float64) < 500 {
		t.Fatalf("a finished export: %v", latest)
	}
	status, header, data := owner.Get("/api/v1/admin/export/download", nil)
	if status != 200 || header.Get("Content-Type") != "application/zip" || !strings.HasPrefix(header.Get("Content-Disposition"), "attachment") ||
		float64(len(data)) != latest["size"].(float64) {
		t.Fatalf("the download: %d %v, %d bytes", status, header, len(data))
	}
	if e := wantAudit(t, ts, "export.started"); e.ActorID != string(owner.ID) || e.ActorKind != "account" || e.Detail["export_id"] != started["id"] {
		t.Fatalf("the audit log: %+v", e)
	}

	check := func(what string, files map[string][]byte, withFiles bool) {
		t.Helper()
		// The layout of docs/SCHEMA.md section 18.
		for _, name := range []string{"export.json", "accounts.json", "groups.json", "sections.json", "emoji.json", "conversations.json",
			"moderation.json", "settings.json", "audit.jsonl",
			"messages/" + town + ".jsonl", "messages/" + den + ".jsonl", "messages/" + adhoc + ".jsonl"} {
			if _, ok := files[name]; !ok {
				t.Fatalf("%s: the archive has no %s", what, name)
			}
		}
		has := func(name, want string) {
			t.Helper()
			if !bytes.Contains(files[name], []byte(want)) {
				t.Fatalf("%s: %s does not contain %q", what, name, want)
			}
		}
		lacks := func(name, word string) {
			t.Helper()
			if bytes.Contains(files[name], []byte(word)) {
				t.Fatalf("%s: %s contains %q", what, name, word)
			}
		}
		has("messages/"+town+".jsonl", "PUBLIC-WORDS-1")
		has("messages/"+town+".jsonl", "THREAD-REPLY-2")
		has("messages/"+den+".jsonl", "PRIVATE-WORDS-4") // a private channel the owner is not in
		has("messages/"+den+".jsonl", "PRIVATE-WORDS-3") // and the version before the edit
		has("messages/"+den+".jsonl", `"version_of":"`+secret+`"`)
		has("messages/"+adhoc+".jsonl", "ADHOC-WORDS-5") // an ad-hoc channel the owner is not in
		has("settings.json", "WELCOME-TEXT-7")
		has("emoji.json", `"party"`)
		has("audit.jsonl", "export.started")

		// Messages are in seq order, one JSON object per line.
		var lastSeq float64
		var poll, ballot map[string]any
		for _, line := range bytes.Split(bytes.TrimSpace(files["messages/"+town+".jsonl"]), []byte("\n")) {
			var m map[string]any
			if err := json.Unmarshal(line, &m); err != nil {
				t.Fatalf("%s: a line of a messages file is not JSON: %s", what, line)
			}
			if m["version_of"] != nil {
				continue
			}
			seq, _ := m["seq"].(float64)
			if seq <= lastSeq {
				t.Fatalf("%s: messages out of seq order at %v", what, m)
			}
			lastSeq = seq
			switch m["id"] {
			case named:
				poll = m
			case anon:
				ballot = m
			case first:
				if len(arr(m, "all_reactions")) != 1 || m["pinned"] != true || obj(m, "thread")["reply_count"] != 1.0 {
					t.Fatalf("%s: the first message with its reaction, pin and thread: %v", what, m)
				}
			}
			if m["client_id"] != nil {
				t.Fatalf("%s: a client id was exported: %v", what, m)
			}
		}
		// A named poll's answers say who; an anonymous one's are counts.
		if poll == nil || len(arr(poll, "form_answers")) != 1 || arr(poll, "form_answers")[0].(map[string]any)["account_id"] != string(bob.ID) ||
			fTally(obj(poll, "form"), "choice", "o1") != 1.0 {
			t.Fatalf("%s: the named poll: %v", what, poll)
		}
		if ballot == nil || ballot["form_answers"] != nil || fTally(obj(ballot, "form"), "choice", "o2") != 1.0 || obj(ballot, "form")["answer_count"] != 1.0 {
			t.Fatalf("%s: the anonymous poll is counts only: %v", what, ballot)
		}
		// Who answered the anonymous one is nowhere on its line.
		if b, _ := json.Marshal(ballot); bytes.Contains(b, []byte(bob.ID)) {
			t.Fatalf("%s: the anonymous poll names its answerer: %s", what, b)
		}

		// Every channel with who is in it.
		var convs []map[string]any
		if err := json.Unmarshal(files["conversations.json"], &convs); err != nil {
			t.Fatal(err)
		}
		members := map[string]int{}
		for _, c := range convs {
			members[c["id"].(string)] = len(arr(c, "members"))
			if c["kind"] != "channel" {
				t.Fatalf("%s: a conversation that is not a channel was exported: %v", what, c)
			}
		}
		if members[town] != 2 || members[den] != 2 || members[adhoc] != 3 {
			t.Fatalf("%s: channels and their members: %v", what, members)
		}
		// Accounts: profiles and roles, nothing to sign in with.
		var accounts []map[string]any
		if err := json.Unmarshal(files["accounts.json"], &accounts); err != nil {
			t.Fatal(err)
		}
		if len(accounts) != 5 {
			t.Fatalf("%s: %d accounts", what, len(accounts))
		}
		for _, a := range accounts {
			for k := range a {
				switch k {
				case "id", "username", "display_name", "role", "server_name", "title", "pronouns", "time_zone", "avatar_file_id", "created_at", "claimed_at", "removed_at":
				default:
					t.Fatalf("%s: accounts.json carries %q", what, k)
				}
			}
		}
		// No secrets among the settings, and none of the server's own rows.
		for _, word := range []string{"files.s3_secret", "setup.", "token", base64.StdEncoding.EncodeToString([]byte("SEALED-S3-SECRET"))} {
			lacks("settings.json", word)
		}
		// Files: the content of each, under its identifier and name.
		name := "files/" + f["id"].(string) + "/notes.txt"
		if withFiles {
			if !bytes.Equal(files[name], fileBody) {
				t.Fatalf("%s: the uploaded file's content: %q", what, files[name])
			}
		} else {
			for n := range files {
				if strings.HasPrefix(n, "files/") {
					t.Fatalf("%s: made without files, yet has %s", what, n)
				}
			}
		}
		has("messages/"+den+".jsonl", `"notes.txt"`)
		var head map[string]any
		json.Unmarshal(files["export.json"], &head)
		counts := obj(head, "counts")
		if head["server_name"] == nil || head["schema_version"].(float64) < 1 || head["exported_at"] == nil || head["files_included"] != withFiles ||
			counts["accounts"] != 5.0 || counts["conversations"].(float64) < 4 || counts["messages"].(float64) < 7 || counts["message_versions"] != 1.0 {
			t.Fatalf("%s: export.json: %v", what, head)
		}

		// Nothing of the sealed chat, anywhere, in any form.
		for name, content := range files {
			for needle, b := range sealed.needles() {
				if bytes.Contains(content, b) {
					t.Fatalf("%s: %s contains %s", what, name, needle)
				}
			}
			if strings.Contains(name, string(sealed.conversation)) || strings.Contains(name, string(sealed.file)) {
				t.Fatalf("%s: the archive has %s", what, name)
			}
		}
		for _, word := range []string{"sealed report reason", "sealed timeout", "test.sealed_target", "test.sealed_detail"} {
			for name := range files {
				lacks(name, word)
			}
		}
		lacks("conversations.json", `"sealed"`)
	}
	check("from the admin console", unzip(t, data), true)

	// The same archive from the command line, here without files; the
	// audit log says it came from there.
	out := filepath.Join(t.TempDir(), "export.zip")
	counts, size, err := server.Export(ctx, server.ExportOptions{
		Offline: server.Offline{Data: ts.Server.Config.Data, DB: ts.Server.Config.DB}, Out: out, NoFiles: true})
	if err != nil {
		t.Fatal(err)
	}
	status, _, _ = owner.Get("/healthz", nil) // the server kept running
	if status != 200 || counts.Accounts != 5 || size == 0 {
		t.Fatalf("the command-line export: %+v, %d bytes, health %d", counts, size, status)
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	check("from the command line", unzip(t, raw), false)
	found := false
	for _, e := range audited(t, ts, "export.started") {
		found = found || e.ActorKind == "cli"
	}
	if !found {
		t.Fatal("the command-line export is not in the audit log")
	}

	// A second export replaces the first.
	again := obj(owner.Must(201, "POST", "/api/v1/admin/export", nil), "export")
	ts.Server.API.Export.Wait()
	if got := obj(owner.Must(200, "GET", "/api/v1/admin/export", nil), "export"); got["id"] != again["id"] || got["id"] == started["id"] || got["state"] != "ready" {
		t.Fatalf("the latest export after a second: %v", got)
	}
}
