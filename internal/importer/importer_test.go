package importer_test

import (
	"archive/zip"
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/auth"
	"github.com/tikaydaes/churr/internal/importer"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
	"github.com/tikaydaes/churr/internal/testkit"
)

func pngOf(w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := 0; x < w; x++ {
		for y := 0; y < h; y++ {
			img.Set(x, y, color.RGBA{uint8(x), uint8(y), 200, 255})
		}
	}
	var b bytes.Buffer
	png.Encode(&b, img)
	return b.Bytes()
}

// slackFiles stands in for Slack's file addresses. It counts what was asked
// for, so a test can hold that nothing was.
type slackFiles struct {
	*httptest.Server
	mu   sync.Mutex
	hits []string
}

func newSlackFiles(t *testing.T) *slackFiles {
	s := &slackFiles{}
	content := map[string][]byte{
		"/files/notes.txt":       []byte("walrus notes\nline two\n"),
		"/files/pic.png":         pngOf(40, 30),
		"/avatars/ann.png":       pngOf(64, 64),
		"/emoji/partyparrot.png": pngOf(32, 32),
		"/emoji/big.png":         pngOf(600, 600),
		// A link that has stopped working answers with a sign-in page.
		"/files/expired.png": []byte("<!doctype html><html><head><title>Sign in to Slack</title></head><body>Sign in</body></html>"),
	}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.hits = append(s.hits, r.URL.Path)
		s.mu.Unlock()
		b, ok := content[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Write(b)
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *slackFiles) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.hits)
}

// zipExport zips one of the exports under testdata/slack, with {{FILES}}
// replaced by the address of the stand-in for Slack's file servers.
func zipExport(t *testing.T, name, filesURL string) string {
	t.Helper()
	root := filepath.Join("..", "..", "testdata", "slack", name)
	out := filepath.Join(t.TempDir(), name+".zip")
	f, err := os.Create(out)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		w, err := zw.Create(filepath.ToSlash(rel))
		if err != nil {
			return err
		}
		_, err = w.Write(bytes.ReplaceAll(b, []byte("{{FILES}}"), []byte(filesURL)))
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()
	return out
}

func options(ts *testkit.TestServer, files *slackFiles) importer.Options {
	return importer.Options{
		Store: ts.Server.Store, Blobs: ts.Server.Blobs, DataDir: ts.Server.Config.Data, Origin: ts.Origin,
		EmailHash: func(email string) []byte { return auth.ImportEmailHashWith(ts.Server.Keys, email) },
		// The stand-in for Slack is on this machine, which the guarded
		// client refuses, as it should. This one does not.
		HTTP: files.Client(),
	}
}

func account(t *testing.T, ts *testkit.TestServer, username string) model.Account {
	t.Helper()
	a, err := ts.Server.Store.Accounts().ByUsername(context.Background(), username)
	if err != nil {
		t.Fatalf("no account %s: %v", username, err)
	}
	return a
}

func messagesOf(t *testing.T, ts *testkit.TestServer, conv model.ConversationID) []model.Message {
	t.Helper()
	ctx := context.Background()
	msgs, _, err := ts.Server.Store.Messages().List(ctx, conv, store.MessageList{SeqPage: store.SeqPage{Limit: 200}})
	if err != nil {
		t.Fatal(err)
	}
	if err := ts.Server.Store.Messages().Hydrate(ctx, msgs, ""); err != nil {
		t.Fatal(err)
	}
	return msgs
}

// TestSlackImport imports the small export into a fresh server and holds
// each row of the table in docs/FEATURES.md section 25.2, then that a second
// run makes nothing twice, and that an imported person can claim their
// account and finds their history.
func TestSlackImport(t *testing.T) {
	ctx := context.Background()
	ts := testkit.NewServer(t)
	st := ts.Server.Store
	files := newSlackFiles(t)
	zipPath := zipExport(t, "small", files.URL)
	general, err := st.Conversations().ByName(ctx, "general")
	if err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	opts := options(ts, files)
	opts.Out = &out
	rep, err := importer.Slack(ctx, zipPath, opts)
	if err != nil {
		t.Fatalf("the import: %v\n%s", err, out.String())
	}
	if f := rep.Found; f.Channels != 6 || f.People != 3 || f.Messages != 16 || f.Files != 5 {
		t.Fatalf("what the export holds: %+v", f)
	}
	if !strings.Contains(out.String(), "6 conversations, 3 people, 16 messages and 5 files") {
		t.Fatalf("what was printed: %s", out.String())
	}
	// Five conversations are made; Slack's general goes into the empty
	// #general that was there. The bots are the bot user and the one that
	// only appears as an author.
	if rep.People != 3 || rep.Bots != 2 || rep.Channels != 5 || rep.Messages != 16 || rep.Notices != 2 || rep.Files != 2 || rep.FilesMissing != 3 ||
		rep.Pins != 1 || rep.Emoji != 1 || rep.Reactions != 3 {
		t.Fatalf("what the import made: %+v", rep)
	}

	// --- People.
	ann := account(t, ts, "ann")
	if !ann.ClaimedAt.IsZero() || ann.Role != model.RoleMember || ann.DisplayName != "Ann" || ann.Title != "Head of Walruses" || ann.TimeZone != "Europe/Berlin" {
		t.Fatalf("a user becomes an unclaimed member, never an admin, with its profile: %+v", ann)
	}
	if ann.AvatarFileID == "" {
		t.Fatal("the profile picture was not stored as the avatar")
	}
	bob := account(t, ts, "bob-smith")
	if bob.Role != model.RoleGuest || bob.DisplayName != "Bob Smith" || !bob.ClaimedAt.IsZero() {
		t.Fatalf("a restricted user becomes an unclaimed guest, with its handle cleaned: %+v", bob)
	}
	// The handle `owner` is taken by this server's owner.
	other := account(t, ts, "owner2")
	if other.DisplayName != "Other Owner" || other.ID == ts.Owner.ID {
		t.Fatalf("a handle that clashes gets a number: %+v", other)
	}
	bot := account(t, ts, "deploybot")
	if bot.Role != model.RoleBot {
		t.Fatalf("a bot user: %+v", bot)
	}
	jenkins := account(t, ts, "jenkins")
	if jenkins.Role != model.RoleBot {
		t.Fatalf("a bot that only appears as an author: %+v", jenkins)
	}
	// A deleted user is a removed account. Removal clears the name it
	// had, so it is found by what it wrote.
	// The email address is kept only as a keyed hash that company sign-in
	// can match; it is the hash the sign-in code makes.
	hash := ts.Server.Auth.ImportEmailHash("ann@example.com")
	if id, found, err := st.Identities().TakeImportClaim(ctx, hash); err != nil || !found || id != ann.ID {
		t.Fatalf("the claim by email address: %v %v %v", id, found, err)
	}
	if err := st.Identities().AddImportClaim(ctx, ann.ID, hash); err != nil {
		t.Fatal(err)
	}

	// --- Conversations.
	got, err := st.Conversations().Get(ctx, general.ID)
	if err != nil {
		t.Fatal(err)
	}
	if all, _ := st.Conversations().ByNameAll(ctx, "general"); len(all) != 1 {
		t.Fatalf("Slack's general went beside the empty #general, not into it: %d channels", len(all))
	}
	for _, who := range []model.Account{ann, bob, other} {
		m, found, err := st.Members().Get(ctx, general.ID, who.ID)
		if err != nil || !found {
			t.Fatalf("%s is not in #general: %v", who.Username, err)
		}
		// Nobody starts with years of unread messages.
		if m.ReadSeq != got.LastSeq {
			t.Fatalf("%s's read position is %d of %d", who.Username, m.ReadSeq, got.LastSeq)
		}
	}
	random, err := st.Conversations().ByName(ctx, "random")
	if err != nil || random.Private || !random.Archived() || random.Topic != "Anything at all" || random.CreatedByID != ann.ID ||
		random.CreatedAt != model.Time(1680000100000) {
		t.Fatalf("an archived public channel, its purpose as its topic: %+v %v", random, err)
	}
	if m, found, _ := st.Members().Get(ctx, random.ID, ann.ID); !found || m.Level != model.LevelManager {
		t.Fatalf("the creator manages the channel: %+v", m)
	}
	plans, err := st.Conversations().ByName(ctx, "secret-plans")
	if err != nil || !plans.Private || plans.Topic != "Shh" {
		t.Fatalf("a private channel: %+v %v", plans, err)
	}
	// Its creator, bob, is a guest here, and a guest is never a manager
	// (ruling R99): ann, the first member who may be, manages it.
	if m, found, _ := st.Members().Get(ctx, plans.ID, ann.ID); !found || m.Level != model.LevelManager {
		t.Fatalf("a member of a private channel made by a guest: %+v", m)
	}
	adhoc := func(what string, ids ...model.AccountID) model.Conversation {
		t.Helper()
		c, err := st.Conversations().ByMemberKey(ctx, store.AdhocMemberKey(ids))
		if err != nil || c.Name != "" || !c.Private {
			t.Fatalf("%s is not an ad-hoc channel of those people: %+v %v", what, c, err)
		}
		return c
	}
	adhoc("a group direct message", ann.ID, bob.ID, other.ID)
	dm := adhoc("a direct message", ann.ID, bob.ID)
	self := adhoc("a direct message to oneself", ann.ID)
	if n := len(messagesOf(t, ts, dm.ID)); n != 2 {
		t.Fatalf("the direct message has %d messages", n)
	}
	if n := len(messagesOf(t, ts, self.ID)); n != 1 {
		t.Fatalf("the note to oneself has %d messages", n)
	}

	// --- Messages, in the order they were sent, with their own times.
	msgs := messagesOf(t, ts, general.ID)
	if len(msgs) != 9 {
		t.Fatalf("#general has %d messages, want 9 (the two notices left out)", len(msgs))
	}
	for i, m := range msgs {
		if m.Via == nil || m.Via.Kind != "import" || m.Via.Name != "Slack" {
			t.Fatalf("message %d does not say it came from Slack: %+v", i, m.Via)
		}
		if i > 0 && (m.Seq <= msgs[i-1].Seq || m.CreatedAt < msgs[i-1].CreatedAt) {
			t.Fatalf("messages out of order at %d", i)
		}
	}
	first := msgs[0]
	wantText := "Hello **everyone**, @channel: the walrus has landed. See <#" + string(random.ID) + ">, not #gone, & read [the site](https://example.com/a?b=1&c=2)"
	if first.Text != wantText || first.AuthorID != ann.ID || first.CreatedAt != model.Time(1700000000000) {
		t.Fatalf("the first message:\n  %q\n  %q\n  %+v", first.Text, wantText, first)
	}
	// Edited in Slack: an edit time and no earlier version.
	if first.EditedAt != model.Time(1700000050000) || first.HasHistory {
		t.Fatalf("an edited message: edited_at %v, has history %v", first.EditedAt, first.HasHistory)
	}
	kinds := map[string]bool{}
	for _, e := range first.Entities {
		kinds[e.Type] = true
		if e.Type == "conversation" && e.ConversationID != random.ID {
			t.Fatalf("the channel link's entity: %+v", e)
		}
	}
	if !kinds["conversation"] || !kinds["broadcast"] {
		t.Fatalf("entities are worked out as for any message: %+v", first.Entities)
	}
	// Reactions: a standard one by its emoji, a custom one by name; the
	// one by an unknown user is dropped.
	reacts := map[string]int{}
	for _, r := range first.Reactions {
		reacts[r.Emoji] = r.Count
	}
	if len(reacts) != 2 || reacts["👍"] != 2 || reacts[":partyparrot:"] != 1 {
		t.Fatalf("reactions: %v", reacts)
	}
	if !first.Pinned {
		t.Fatal("the pinned message is not pinned")
	}
	// The thread: two replies, the one "also sent to channel" once.
	if first.Thread == nil || first.Thread.ReplyCount != 2 {
		t.Fatalf("the thread: %+v", first.Thread)
	}
	reply, broadcast := msgs[1], msgs[2]
	if reply.ParentID != first.ID || reply.Text != "*agreed*, @ann" || reply.AuthorID != bob.ID {
		t.Fatalf("a reply: %+v", reply)
	}
	mentioned := false
	for _, e := range reply.Entities {
		mentioned = mentioned || (e.Type == "account" && e.AccountID == ann.ID)
	}
	if !mentioned {
		t.Fatalf("a mention draws as one: %+v", reply.Entities)
	}
	if broadcast.ParentID != first.ID || broadcast.AuthorID != other.ID {
		t.Fatalf("a reply also sent to the channel is the reply only: %+v", broadcast)
	}
	// Attachments become cards; an image elsewhere is named, not fetched.
	card := msgs[3]
	if card.AuthorID != jenkins.ID || len(card.Cards) != 1 {
		t.Fatalf("a bot's message with an attachment: %+v", card)
	}
	if c := card.Cards[0]; c.Color != "green" || c.Title != "Build #482 passed" || c.URL != "https://ci.example/builds/482" || c.Text != "All **green** for @ann" ||
		c.Author == nil || c.Author.Name != "CI" || len(c.Fields) != 1 || c.Fields[0].Label != "Branch" || c.ImageFileID != "" ||
		c.Footer != "ci.example · https://ci.example/graph.png" || c.At != model.Time(1700000000000) {
		t.Fatalf("the card: %+v", c)
	}
	// A deleted user's message has an author, who is a removed account.
	gone, err := st.Accounts().Get(ctx, msgs[4].AuthorID)
	if err != nil || !gone.Removed() || msgs[4].Text != "words from someone since deleted" {
		t.Fatalf("a deleted user's message: %+v %v", gone, err)
	}
	// Files: fetched while the link works; the others leave a line.
	withFiles := msgs[5]
	if len(withFiles.Files) != 1 || withFiles.Files[0].Name != "notes.txt" || withFiles.Files[0].Kind != "text" || withFiles.Files[0].Size != 22 {
		t.Fatalf("the downloaded file: %+v", withFiles.Files)
	}
	if withFiles.Text != "the notes\nFile not available: gone.pdf\nFile not available: hidden.zip" {
		t.Fatalf("files that could not be had: %q", withFiles.Text)
	}
	stored, err := st.Files().Get(ctx, withFiles.Files[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	rc, err := ts.Server.Blobs.Get(ctx, stored.BlobKey)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(rc)
	rc.Close()
	if string(body) != "walrus notes\nline two\n" || stored.MessageID != withFiles.ID || stored.UploaderID != ann.ID {
		t.Fatalf("the stored file: %q %+v", body, stored)
	}
	// A user the export does not list still has its message.
	if unknown, err := st.Accounts().Get(ctx, msgs[6].AuthorID); err != nil || !unknown.Removed() || msgs[6].Text != "who am I" {
		t.Fatalf("a message by an unlisted user: %+v %v", unknown, err)
	}
	next := msgs[7]
	if next.Text != "`code *stays*` and ~~gone~~ :tada:\nFile not available: expired.png" || next.CreatedAt != model.Time(1700086400000) {
		t.Fatalf("code is left alone, and a link that answers with a sign-in page is not a file: %q", next.Text)
	}
	if len(next.Files) != 1 || next.Files[0].Kind != "image" || next.Files[0].Width != 40 || next.Files[0].ThumbURL == "" {
		t.Fatalf("an image: %+v", next.Files)
	}
	if msgs[8].Text != "*waves*" {
		t.Fatalf("a /me message: %q", msgs[8].Text)
	}

	// --- Custom emoji: the image; not the alias, not the one too large.
	if _, err := st.Emoji().ByName(ctx, "partyparrot"); err != nil {
		t.Fatalf("the custom emoji: %v", err)
	}
	for _, name := range []string{"pp", "toobig"} {
		if _, err := st.Emoji().ByName(ctx, name); err == nil {
			t.Fatalf("the emoji %s was imported", name)
		}
	}

	// --- Search finds imported messages; nothing was sent to anyone.
	res := ts.Owner.Must(200, "GET", "/api/v1/search?q=walrus", nil)
	if n := len(res["results"].([]any)); n != 1 {
		t.Fatalf("searching for an imported word found %d", n)
	}
	events := ts.Owner.Must(200, "GET", "/api/v1/conversations/"+string(general.ID)+"/events?after=0&limit=200", nil)
	for _, e := range events["events"].([]any) {
		if typ := e.(map[string]any)["type"]; typ == "message.created" || typ == "reaction.added" {
			t.Fatalf("the import wrote an event: %v", e)
		}
	}
	entries, _, err := st.Audit().List(ctx, store.AuditFilter{Action: "import.slack"}, store.Page{})
	if err != nil || len(entries) != 1 || entries[0].ActorKind != "cli" {
		t.Fatalf("the audit log: %+v %v", entries, err)
	}
	if _, err := os.Stat(filepath.Join(ts.Server.Config.Data, "import-slack.json")); err != nil {
		t.Fatalf("the record of what was made: %v", err)
	}

	// --- Claim links: one per person; none for the deleted or the bots.
	if len(rep.Claims) != 3 {
		t.Fatalf("claim links: %+v", rep.Claims)
	}
	links := map[string]importer.Claim{}
	for _, c := range rep.Claims {
		links[c.Username] = c
		if !strings.HasPrefix(c.URL, ts.Origin+"/join/") || c.ExpiresAt.Sub(model.Now()) < 29*24*time.Hour {
			t.Fatalf("a claim link: %+v", c)
		}
	}
	if links["ann"].AccountID != ann.ID || links["bob-smith"].AccountID != bob.ID || links["owner2"].AccountID != other.ID {
		t.Fatalf("whose claim links: %+v", links)
	}

	// --- A second run makes nothing, and hands out the same links.
	hits := files.count()
	again, err := importer.Slack(ctx, zipPath, options(ts, files))
	if err != nil {
		t.Fatal(err)
	}
	if again.People+again.Bots+again.Channels+again.Messages+again.Files+again.Reactions+again.Pins+again.Emoji != 0 {
		t.Fatalf("a second run made something: %+v", again)
	}
	if n := len(messagesOf(t, ts, general.ID)); n != 9 {
		t.Fatalf("#general has %d messages after a second run", n)
	}
	if len(again.Claims) != 3 || again.Claims[0].URL != rep.Claims[0].URL {
		t.Fatalf("a second run changed the claim links: %+v", again.Claims)
	}
	// It fetched again only what could not be had before.
	if files.count()-hits > 3 {
		t.Fatalf("a second run fetched %d addresses", files.count()-hits)
	}
	// The same with the record lost: what is there is found by identifier.
	os.Remove(filepath.Join(ts.Server.Config.Data, "import-slack.json"))
	third, err := importer.Slack(ctx, zipPath, options(ts, files))
	if err != nil || third.People+third.Bots+third.Messages+third.Reactions != 0 || third.Channels > 0 {
		t.Fatalf("a run without the record made something: %+v %v", third, err)
	}

	// --- Claiming: the person opens the link, makes a passkey, and is the
	// imported account, in the private channel it was in.
	code := links["ann"].URL[strings.LastIndex(links["ann"].URL, "/")+1:]
	browser := ts.Browser()
	if got := browser.Must(200, "GET", "/api/v1/invite-codes/"+code, nil); got["claims_username"] != "ann" {
		t.Fatalf("opening the claim link: %v", got)
	}
	browser.Register("ann", code)
	history := browser.Must(200, "GET", "/api/v1/conversations/"+string(plans.ID)+"/messages", nil)
	// Its two imported messages, then the notice that the account was
	// claimed, which a private channel is told of.
	seen := history["messages"].([]any)
	if len(seen) != 3 {
		t.Fatalf("the claimed account sees %d messages of its private channel", len(seen))
	}
	if sys, _ := seen[2].(map[string]any)["system"].(map[string]any); sys["kind"] != "account_claimed" {
		t.Fatalf("the last message is not the claim notice: %v", seen[2])
	}
	unread := browser.Must(200, "GET", "/api/v1/conversations/"+string(general.ID), nil)
	if c := unread["conversation"].(map[string]any); c["unread_count"] != nil && c["unread_count"] != 0.0 {
		t.Fatalf("a claimed account starts with unread messages: %v", c["unread_count"])
	}
	// Claimed, it gets no more claim links.
	after, err := importer.Slack(ctx, zipPath, options(ts, files))
	if err != nil || len(after.Claims) != 2 {
		t.Fatalf("claim links after one was used: %+v %v", after.Claims, err)
	}
}

// With -no-files the importer contacts nothing, and the messages say which
// files they had. The client it uses otherwise refuses addresses that are
// not on the public internet. A dry run changes nothing. And an operator
// whose retention period would delete the import is asked first.
func TestSlackImportOptions(t *testing.T) {
	ctx := context.Background()
	files := newSlackFiles(t)

	t.Run("dry run", func(t *testing.T) {
		ts := testkit.NewServer(t)
		opts := options(ts, files)
		opts.DryRun = true
		rep, err := importer.Slack(ctx, zipExport(t, "small", files.URL), opts)
		if err != nil || !rep.DryRun || rep.Found.Messages != 16 || rep.Messages != 0 {
			t.Fatalf("a dry run: %+v %v", rep, err)
		}
		if _, err := ts.Server.Store.Accounts().ByUsername(ctx, "ann"); err == nil {
			t.Fatal("a dry run made an account")
		}
		if _, err := os.Stat(filepath.Join(ts.Server.Config.Data, "import-slack.json")); err == nil {
			t.Fatal("a dry run wrote the record")
		}
		if files.count() != 0 {
			t.Fatal("a dry run fetched something")
		}
	})

	t.Run("no files", func(t *testing.T) {
		ts := testkit.NewServer(t)
		before := files.count()
		opts := options(ts, files)
		opts.NoFiles = true
		rep, err := importer.Slack(ctx, zipExport(t, "small", files.URL), opts)
		if err != nil || rep.Files != 0 || rep.FilesMissing != 5 || rep.Emoji != 0 || rep.Messages != 16 {
			t.Fatalf("without files: %+v %v", rep, err)
		}
		if files.count() != before {
			t.Fatalf("-no-files contacted the file addresses %d times", files.count()-before)
		}
		if a := account(t, ts, "ann"); a.AvatarFileID != "" {
			t.Fatal("-no-files fetched a profile picture")
		}
		general, _ := ts.Server.Store.Conversations().ByName(ctx, "general")
		if text := messagesOf(t, ts, general.ID)[5].Text; !strings.Contains(text, "File not available: notes.txt") {
			t.Fatalf("a message whose file was not fetched: %q", text)
		}
	})

	t.Run("the guarded client", func(t *testing.T) {
		ts := testkit.NewServer(t)
		before := files.count()
		opts := options(ts, files)
		opts.HTTP = nil // the default: importer.Guarded()
		rep, err := importer.Slack(ctx, zipExport(t, "small", files.URL), opts)
		if err != nil || rep.Files != 0 || rep.FilesMissing != 5 {
			t.Fatalf("with addresses on this machine: %+v %v", rep, err)
		}
		if files.count() != before {
			t.Fatal("the guarded client connected to an address on this machine")
		}
		// Not a web address at all.
		for _, bad := range []string{"file:///etc/passwd", "http://user:pass@example.com/x", "ftp://example.com/x", "http://169.254.169.254/latest/meta-data/"} {
			u, _ := url.Parse(bad)
			req, _ := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
			if resp, err := importer.Guarded().Do(req); err == nil {
				resp.Body.Close()
				if strings.HasPrefix(bad, "http://169") {
					t.Fatalf("the guarded client fetched %s", bad)
				}
			}
		}
	})

	t.Run("retention", func(t *testing.T) {
		ts := testkit.NewServer(t)
		ts.Set("retention.seconds", 86400)
		opts := options(ts, files)
		asked := ""
		opts.Confirm = func(q string) bool { asked = q; return false }
		rep, err := importer.Slack(ctx, zipExport(t, "small", files.URL), opts)
		if err == nil || rep.PastRetention != 16 || !strings.Contains(asked, "16 of the export's messages") {
			t.Fatalf("an import the retention period would delete: %+v %v %q", rep, err, asked)
		}
		if _, err := ts.Server.Store.Accounts().ByUsername(ctx, "ann"); err == nil {
			t.Fatal("something was imported after the operator said no")
		}
		opts.Confirm = func(string) bool { return true }
		if rep, err := importer.Slack(ctx, zipExport(t, "small", files.URL), opts); err != nil || rep.Messages != 16 {
			t.Fatalf("after the operator said yes: %+v %v", rep, err)
		}
	})

	t.Run("not an export", func(t *testing.T) {
		ts := testkit.NewServer(t)
		notZip := filepath.Join(t.TempDir(), "x.zip")
		os.WriteFile(notZip, []byte("not a zip"), 0o600)
		if _, err := importer.Slack(ctx, notZip, options(ts, files)); err == nil {
			t.Fatal("a file that is not a zip archive was accepted")
		}
		empty := filepath.Join(t.TempDir(), "empty.zip")
		f, _ := os.Create(empty)
		zw := zip.NewWriter(f)
		w, _ := zw.Create("readme.txt")
		w.Write([]byte("hello"))
		zw.Close()
		f.Close()
		if _, err := importer.Slack(ctx, empty, options(ts, files)); err == nil || !strings.Contains(err.Error(), "users.json") {
			t.Fatalf("a zip that is not a Slack export: %v", err)
		}
	})
}
