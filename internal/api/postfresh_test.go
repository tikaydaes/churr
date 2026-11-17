package api_test

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/testkit"
)

// The post path answers its checks from memory where it can: the settings,
// the word filters, the caller's session and account (sqlstore/kept.go),
// and reads the conversation, the membership and the time-out together.
// What is kept must never be older than the last change, so each test here
// posts first (which fills whatever is kept), makes one change, and holds
// that the very next post sees it; then undoes it where it can be undone
// and holds that the next post sees that too.

func refusedPost(c *testkit.Client, status int, code model.Code, room, text string) {
	c.Refused(status, code, "POST", convPath(room)+"/messages", map[string]any{"text": text})
}

func TestNextPostSeesAWordFilter(t *testing.T) {
	ts := testkit.NewServer(t)
	admin, bob := ts.Admin("adm"), ts.Member("bob")
	room := admin.Channel("room", false, bob)
	const wf = "/api/v1/admin/word-filters"
	say(bob, room, "a gadget")
	id := obj(admin.Must(201, "POST", wf, map[string]any{"pattern": "gadget", "action": "block"}), "word_filter")["id"].(string)
	refusedPost(bob, 422, model.CodeBlockedByFilter, room, "another gadget")
	say(bob, room, "a widget")
	// Changed: the old word passes and the new one does not.
	admin.Must(200, "PATCH", wf+"/"+id, map[string]any{"pattern": "widget", "action": "block"})
	say(bob, room, "a gadget again")
	refusedPost(bob, 422, model.CodeBlockedByFilter, room, "a widget again")
	// Edits read the same list.
	mine := say(bob, room, "harmless")["message"].(map[string]any)["id"].(string)
	bob.Refused(422, model.CodeBlockedByFilter, "PATCH", "/api/v1/messages/"+mine, map[string]any{"text": "widget"})
	// Removed.
	admin.Must(204, "DELETE", wf+"/"+id, nil)
	say(bob, room, "a widget at last")
}

func TestNextPostSeesASetting(t *testing.T) {
	ts := testkit.NewServer(t)
	owner, bob := ts.Owner, ts.Member("bob")
	room := owner.Channel("room", false, bob)
	if broadcasts(say(bob, room, "@channel one")) != 1 {
		t.Fatal("a member's @channel is a broad mention by default")
	}
	owner.Must(200, "PATCH", settingsPath, map[string]any{"mentions.broadcast_managers_only": true})
	if out := say(bob, room, "@channel two"); broadcasts(out) != 0 || out["broad_mentions_refused"] != true {
		t.Fatalf("after the setting changed, the next post still mentioned broadly: %v", out)
	}
	owner.Must(200, "PATCH", settingsPath, map[string]any{"mentions.broadcast_managers_only": false})
	if broadcasts(say(bob, room, "@channel three")) != 1 {
		t.Fatal("after the setting changed back, the next post did not mention broadly")
	}
	// A setting read one key at a time: the quick-correction window,
	// which decides whether an edit keeps the earlier version.
	edit := func(text string) map[string]any {
		id := say(bob, room, text)["message"].(map[string]any)["id"].(string)
		return bob.Must(200, "PATCH", "/api/v1/messages/"+id, map[string]any{"text": text + ", edited"})["message"].(map[string]any)
	}
	if m := edit("quick"); m["has_history"] == true {
		t.Fatalf("inside the window an edit keeps no history: %v", m)
	}
	owner.Must(200, "PATCH", settingsPath, map[string]any{"messages.quick_correction_seconds": 0})
	if m := edit("slow"); m["has_history"] != true {
		t.Fatalf("with the window set to 0 the next edit keeps history: %v", m)
	}
}

func TestNextPostSeesATimeOut(t *testing.T) {
	ts := testkit.NewServer(t)
	ann, bob := ts.Member("ann"), ts.Member("bob")
	admin := ts.Admin("adm")
	room := ann.Channel("room", false, bob)
	other := ann.Channel("other", false, bob)
	say(bob, room, "before")
	id := obj(ann.Must(201, "POST", convPath(room)+"/timeouts", map[string]any{"account_id": bob.ID, "seconds": 600}), "timeout")["id"].(string)
	refusedPost(bob, 403, model.CodeTimedOut, room, "during")
	say(bob, other, "elsewhere")
	ann.Must(204, "DELETE", convPath(room)+"/timeouts/"+id, nil)
	say(bob, room, "after")
	// On the whole server.
	all := obj(admin.Must(201, "POST", "/api/v1/admin/timeouts", map[string]any{"account_id": bob.ID, "seconds": 600}), "timeout")["id"].(string)
	refusedPost(bob, 403, model.CodeTimedOut, room, "during")
	refusedPost(bob, 403, model.CodeTimedOut, other, "during")
	admin.Must(204, "DELETE", "/api/v1/admin/timeouts/"+all, nil)
	say(bob, other, "after")
}

func TestNextPostSeesSlowMode(t *testing.T) {
	ts := testkit.NewServer(t)
	ann, bob := ts.Member("ann"), ts.Member("bob")
	room := ann.Channel("room", false, bob)
	say(bob, room, "one")
	say(bob, room, "two")
	ann.Must(200, "PATCH", convPath(room), map[string]any{"slow_mode_seconds": 30})
	// The clock starts with the first post made under it.
	say(bob, room, "three")
	refusedPost(bob, 429, model.CodeSlowMode, room, "four")
	ann.Must(200, "PATCH", convPath(room), map[string]any{"slow_mode_seconds": 0})
	say(bob, room, "five")
}

func TestNextPostSeesArchivedAndLocked(t *testing.T) {
	ts := testkit.NewServer(t)
	ann, bob, cat := ts.Member("ann"), ts.Member("bob"), ts.Member("cat")
	room := ann.Channel("room", false, bob)
	say(bob, room, "before")
	ann.Must(200, "POST", convPath(room)+"/archive", nil)
	refusedPost(bob, 403, model.CodeArchived, room, "frozen")
	ann.Must(200, "DELETE", convPath(room)+"/archive", nil)
	say(bob, room, "thawed")

	// A locked private channel takes nobody new; those in it post on.
	priv := ann.Channel("priv", true, bob)
	say(bob, priv, "before")
	ann.Must(200, "POST", convPath(priv)+"/lock", nil)
	if status, _ := ann.Do("POST", convPath(priv)+"/members", map[string]any{"account_ids": []string{string(cat.ID)}}); status == 200 {
		t.Fatal("someone was added to a channel that had just been locked")
	}
	refusedPost(cat, 404, model.CodeNotFound, priv, "let me in")
	say(bob, priv, "still here")
}

func TestNextPostSeesMembership(t *testing.T) {
	ts := testkit.NewServer(t)
	ann, bob := ts.Member("ann"), ts.Member("bob")
	priv := ann.Channel("priv", true, bob)
	say(bob, priv, "before")
	ann.Must(204, "DELETE", convPath(priv)+"/members/"+string(bob.ID), nil)
	refusedPost(bob, 404, model.CodeNotFound, priv, "after")
	ann.Add(priv, "", bob)
	say(bob, priv, "back")
	// A level that only reads.
	ann.Must(200, "PATCH", convPath(priv)+"/members/"+string(bob.ID), map[string]any{"level": "reader"})
	refusedPost(bob, 403, model.CodeForbidden, priv, "as a reader")
	ann.Must(200, "PATCH", convPath(priv)+"/members/"+string(bob.ID), map[string]any{"level": "member"})
	say(bob, priv, "as a member")
}

func TestNextPostSeesARoleChange(t *testing.T) {
	ts := testkit.NewServer(t)
	owner := ts.Owner
	adm := ts.Admin("adm")
	// Only managers post in an announcement channel. A guest account is a
	// guest in every channel and never one of its managers (ruling R99).
	news := owner.Must(201, "POST", "/api/v1/channels", map[string]any{"name": "news", "private": false, "announcement": true,
		"account_ids": []string{string(adm.ID)}})["conversation"].(map[string]any)["id"].(string)
	adm.AcceptInvitation(news)
	owner.Must(200, "PATCH", convPath(news)+"/members/"+string(adm.ID), map[string]any{"level": "manager"})
	role := "/api/v1/admin/accounts/" + string(adm.ID) + "/role"
	say(adm, news, "as a manager")
	adm.Must(200, "GET", "/api/v1/admin/word-filters", nil)
	owner.Must(200, "PUT", role, map[string]any{"role": "guest"})
	refusedPost(adm, 403, model.CodeForbidden, news, "as a guest")
	adm.Refused(403, model.CodeForbidden, "GET", "/api/v1/admin/word-filters", nil)
	if got := at(adm.Must(200, "GET", "/api/v1/me", nil), "account")["role"]; got != "guest" {
		t.Fatalf("the account still reads its old role: %v", got)
	}
	owner.Must(200, "PUT", role, map[string]any{"role": "admin"})
	say(adm, news, "a manager again")
}

func TestNextPostSeesABanAndASignOut(t *testing.T) {
	ts := testkit.NewServer(t)
	owner := ts.Owner
	bob, cat := ts.Member("bob"), ts.Member("cat")
	room := owner.Channel("room", false, bob, cat)
	token := bob.Token()
	say(bob, room, "from the browser")
	say(token, room, "from a token")
	owner.Must(201, "POST", "/api/v1/admin/bans", map[string]any{"account_id": bob.ID})
	refusedPost(bob, 401, model.CodeUnauthenticated, room, "banned")
	refusedPost(token, 401, model.CodeUnauthenticated, room, "banned")

	// A token that is revoked stops at once; the browser goes on.
	tok := cat.Must(201, "POST", "/api/v1/me/tokens", map[string]any{"label": "script"})
	script := *cat
	script.Bearer = tok["secret"].(string)
	say(&script, room, "from the script")
	list := arr(cat.Must(200, "GET", "/api/v1/me/tokens", nil), "tokens")
	if len(list) != 1 {
		t.Fatalf("tokens: %v", list)
	}
	cat.Must(204, "DELETE", "/api/v1/me/tokens/"+list[0].(map[string]any)["id"].(string), nil)
	refusedPost(&script, 401, model.CodeUnauthenticated, room, "revoked")
	say(cat, room, "from the browser")

	// A profile change is read back at once.
	cat.Must(200, "PATCH", "/api/v1/me", map[string]any{"display_name": "Catherine"})
	if got := at(cat.Must(200, "GET", "/api/v1/me", nil), "account")["display_name"]; got != "Catherine" {
		t.Fatalf("the account still reads its old name: %v", got)
	}
}

// Posts made at once into one conversation still reach a reader in seq
// order with no seq missing, and a client_id sent several times at once
// stores one message: the seq is taken late and the rows travel together,
// and neither may change this.
func TestPostsAtOnceKeepOrderAndClientID(t *testing.T) {
	ts := testkit.NewServer(t)
	owner := ts.Owner
	const writers, each = 8, 15
	var people []*testkit.Client
	for i := 0; i < writers; i++ {
		people = append(people, ts.Member(fmt.Sprintf("w%d", i)))
	}
	room := owner.Channel("room", false, people...)
	first := say(owner, room, "first")["message"].(map[string]any)["seq"].(float64)
	stream, _ := owner.Events(nil)

	var wg sync.WaitGroup
	failed := make(chan string, writers*each)
	for _, c := range people {
		wg.Add(1)
		go func(c *testkit.Client) {
			defer wg.Done()
			for i := 0; i < each; i++ {
				status, body, err := c.Try("POST", convPath(room)+"/messages", map[string]any{
					"text": fmt.Sprintf("%s %d", c.Username, i), "client_id": string(model.NewID[model.MessageID]())})
				if err != nil || status != 201 {
					failed <- fmt.Sprintf("%d %v %v", status, body, err)
				}
			}
		}(c)
	}
	wg.Wait()
	close(failed)
	for f := range failed {
		t.Fatalf("a post failed: %s", f)
	}
	want := first
	for n := 0; n < writers*each; n++ {
		e := stream.Expect(model.EventMessageCreated)
		want++
		if got := e["seq"].(float64); got != want {
			t.Fatalf("event %d has seq %v, want %v: out of order or one missing", n, got, want)
		}
	}
	if got := own(owner, room)["last_seq"].(float64); got != want {
		t.Fatalf("last_seq is %v after %v posts from seq %v", got, writers*each, first)
	}

	// One client_id, five times at once.
	clientID := string(model.NewID[model.MessageID]())
	ids := make(chan string, 5)
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			status, body, err := people[0].Try("POST", convPath(room)+"/messages", map[string]any{"text": "once", "client_id": clientID})
			if err != nil || (status != 201 && status != 200) {
				ids <- fmt.Sprintf("failed: %d %v %v", status, body, err)
				return
			}
			ids <- body["message"].(map[string]any)["id"].(string)
		}()
	}
	wg.Wait()
	close(ids)
	one := ""
	for id := range ids {
		if one == "" {
			one = id
		}
		if id != one || strings.HasPrefix(id, "failed") {
			t.Fatalf("the same client_id gave %q and %q", one, id)
		}
	}
	if got := own(owner, room)["last_seq"].(float64); got != want+1 {
		t.Fatalf("one client_id sent five times took %v seqs, want 1", got-want)
	}
}

// Who has blocked an author is kept in memory for the fan-out: a block, and
// its removal, count from the very next message.
func TestNextPostSeesABlock(t *testing.T) {
	ts := testkit.NewServer(t)
	ann, bob := ts.Member("ann"), ts.Member("bob")
	room := ann.Channel("room", false, bob)
	stream, _ := bob.Events(nil)
	level := func(text string) any {
		t.Helper()
		say(ann, room, text+" @bob")
		return stream.Expect(model.EventMessageCreated)["notify"]
	}
	if got := level("one"); got != "notify" {
		t.Fatalf("a mention notifies: %v", got)
	}
	bob.Must(204, "PUT", "/api/v1/me/blocks/"+string(ann.ID), nil)
	if got := level("two"); got != "none" {
		t.Fatalf("the first message after the block still gave %v", got)
	}
	bob.Must(204, "DELETE", "/api/v1/me/blocks/"+string(ann.ID), nil)
	if got := level("three"); got != "notify" {
		t.Fatalf("the first message after the block was removed gave %v", got)
	}
}
