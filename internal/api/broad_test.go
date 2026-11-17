package api_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
	"github.com/tikaydaes/churr/internal/testkit"
)

// Broad mentions in large channels (ruling R64).

const settingsPath = "/api/v1/admin/settings"

func convPath(id string) string { return "/api/v1/conversations/" + id }

// own returns the conversation as one client sees it.
func own(c *testkit.Client, conv string) map[string]any {
	return c.Must(200, "GET", convPath(conv), nil)["conversation"].(map[string]any)
}

// mentionsOf is the client's count of unread mentions in a conversation.
func mentionsOf(c *testkit.Client, conv string) int {
	return int(own(c, conv)["membership"].(map[string]any)["mention_count"].(float64))
}

// broadcasts counts the broad-mention entities of a posted message:
// `broadcast` ones, and a `group` one for @everyone.
func broadcasts(out map[string]any) int {
	n := 0
	ents, _ := out["message"].(map[string]any)["entities"].([]any)
	for _, e := range ents {
		switch e.(map[string]any)["type"] {
		case "broadcast", "group":
			n++
		}
	}
	return n
}

func say(c *testkit.Client, conv, text string) map[string]any {
	return c.Must(201, "POST", convPath(conv)+"/messages", map[string]any{"text": text})
}

func check(c *testkit.Client, conv, text string) map[string]any {
	return c.Must(200, "POST", convPath(conv)+"/broad-mentions/check", map[string]any{"text": text})
}

func TestBroadMentionLimit(t *testing.T) {
	ts := testkit.NewServer(t)
	owner := ts.Owner
	manager, member, carol, dave := ts.Member("manager"), ts.Member("member"), ts.Member("carol"), ts.Member("dave")
	room := owner.Channel("town", false, manager, member, carol, dave)
	owner.Must(200, "PATCH", convPath(room)+"/members/"+string(manager.ID), map[string]any{"level": "manager"})
	guest := ts.Guest("guest", room)
	bot := owner.Bot("robot")
	owner.Add(room, "member", bot)
	// Seven members: the owner (an admin), a manager, three members, a
	// guest and a bot.

	// The server's setting has its default and is the admins' to change.
	if got := owner.Must(200, "GET", settingsPath, nil)["settings"].(map[string]any)["mentions.broad_mention_limit"]; got != float64(250) {
		t.Fatalf("the default limit is %v, want 250", got)
	}
	owner.Refused(400, model.CodeInvalidValue, "PATCH", settingsPath, map[string]any{"mentions.broad_mention_limit": -1})
	member.Refused(403, model.CodeForbidden, "PATCH", settingsPath, map[string]any{"mentions.broad_mention_limit": 3})

	// Below the limit anyone who can post may: a member, a guest, a bot.
	before := mentionsOf(carol, room)
	for name, c := range map[string]*testkit.Client{"member": member, "guest": guest, "bot": bot, "manager": manager, "admin": owner} {
		out := say(c, room, "@channel hello from "+name)
		if broadcasts(out) != 1 || out["broad_mentions_refused"] != nil {
			t.Fatalf("below the limit, %s: %v", name, out)
		}
	}
	if got := mentionsOf(carol, room); got != before+5 {
		t.Fatalf("carol has %d mentions after five @channel, want %d", got, before+5)
	}
	if own(member, room)["broad_mentions_allowed"] != true || own(member, room)["broad_mentions"] != "server" {
		t.Fatalf("below the limit the conversation says a member may: %v", own(member, room))
	}

	// Above it only the channel's managers and the admins may. The change
	// to the setting holds from the next request on.
	owner.Must(200, "PATCH", settingsPath, map[string]any{"mentions.broad_mention_limit": 6})
	before = mentionsOf(carol, room)
	for name, c := range map[string]*testkit.Client{"member": member, "guest": guest, "bot": bot} {
		for _, word := range []string{"@channel", "@here", "@everyone"} {
			text := word + " plain from " + name
			out := say(c, room, text)
			if broadcasts(out) != 0 || out["broad_mentions_refused"] != true || out["message"].(map[string]any)["text"] != text {
				t.Fatalf("above the limit, %s wrote %s: %v", name, word, out)
			}
		}
		if got := check(c, room, "@channel all"); got["broad"] != true || got["allowed"] != false {
			t.Fatalf("above the limit, the check for %s: %v", name, got)
		}
	}
	if got := mentionsOf(carol, room); got != before {
		t.Fatalf("carol was mentioned %d times by words left as plain text", got-before)
	}
	for name, c := range map[string]*testkit.Client{"manager": manager, "admin": owner} {
		out := say(c, room, "@channel from "+name)
		if broadcasts(out) != 1 || out["broad_mentions_refused"] != nil {
			t.Fatalf("above the limit, %s: %v", name, out)
		}
		if got := check(c, room, "@channel all"); got["allowed"] != true {
			t.Fatalf("above the limit, the check for %s: %v", name, got)
		}
	}
	if got := mentionsOf(carol, room); got != before+2 {
		t.Fatalf("carol has %d more mentions after the manager's and the admin's, want 2", got-before)
	}
	if own(member, room)["broad_mentions_allowed"] != false || own(manager, room)["broad_mentions_allowed"] != true ||
		own(owner, room)["broad_mentions_allowed"] != true {
		t.Fatal("above the limit the conversation tells a member no and a manager and an admin yes")
	}
	// A mention by name, and @admins, are not broad and are nobody's to limit.
	if out := say(member, room, "@carol and @admins see this"); out["broad_mentions_refused"] != nil || broadcasts(out) != 1 {
		t.Fatalf("a name and @admins above the limit: %v", out)
	}
	// Text in code is not a mention and nothing is refused.
	if out := say(member, room, "type `@channel` to shout"); out["broad_mentions_refused"] != nil {
		t.Fatalf("@channel in code: %v", out)
	}
	if got := check(member, room, "type `@channel` to shout"); got["broad"] != false {
		t.Fatalf("the check of @channel in code: %v", got)
	}

	// The channel's own answer. Only its managers set it.
	member.Refused(403, model.CodeForbidden, "PATCH", convPath(room), map[string]any{"broad_mentions": "anyone"})
	manager.Refused(400, model.CodeInvalidValue, "PATCH", convPath(room), map[string]any{"broad_mentions": "everybody"})
	stream, _ := member.Events(nil)
	defer stream.Close()
	got := manager.Must(200, "PATCH", convPath(room), map[string]any{"broad_mentions": "anyone"})["conversation"].(map[string]any)
	if got["broad_mentions"] != "anyone" {
		t.Fatalf("the channel's answer was not stored: %v", got)
	}
	e := stream.Expect("conversation.updated")
	if e["conversation"].(map[string]any)["broad_mentions"] != "anyone" {
		t.Fatalf("conversation.updated does not carry the answer: %v", e)
	}
	// Held at once, by the very next read and the very next post.
	if own(member, room)["broad_mentions_allowed"] != true {
		t.Fatal("with anyone, the conversation still tells a member no")
	}
	if out := say(guest, room, "@channel anyone may"); broadcasts(out) != 1 || out["broad_mentions_refused"] != nil {
		t.Fatalf("with anyone, a guest above the limit: %v", out)
	}
	owner.Must(200, "PATCH", settingsPath, map[string]any{"mentions.broad_mention_limit": 0}) // no limit
	manager.Must(200, "PATCH", convPath(room), map[string]any{"broad_mentions": "managers"})
	if out := say(member, room, "@here managers only"); broadcasts(out) != 0 || out["broad_mentions_refused"] != true {
		t.Fatalf("with managers, a member: %v", out)
	}
	if out := say(manager, room, "@here managers only"); out["broad_mentions_refused"] != nil {
		t.Fatalf("with managers, a manager: %v", out)
	}
	manager.Must(200, "PATCH", convPath(room), map[string]any{"broad_mentions": "server"})
	if out := say(member, room, "@channel no limit"); broadcasts(out) != 1 {
		t.Fatalf("with no limit and the server's rule, a member: %v", out)
	}
	// The server-wide switch is not the channel's to loosen.
	owner.Must(200, "PATCH", settingsPath, map[string]any{"mentions.broadcast_managers_only": true})
	manager.Must(200, "PATCH", convPath(room), map[string]any{"broad_mentions": "anyone"})
	if out := say(member, room, "@channel switch"); out["broad_mentions_refused"] != true {
		t.Fatalf("with the server's switch on and anyone here, a member: %v", out)
	}

	// An edit is held to the same rule.
	owner.Must(200, "PATCH", settingsPath, map[string]any{"mentions.broadcast_managers_only": false, "mentions.broad_mention_limit": 6})
	manager.Must(200, "PATCH", convPath(room), map[string]any{"broad_mentions": "server"})
	msg := say(member, room, "nothing yet")["message"].(map[string]any)
	before = mentionsOf(carol, room)
	edited := member.Must(200, "PATCH", "/api/v1/messages/"+msg["id"].(string), map[string]any{"text": "@channel edited in"})
	if broadcasts(edited) != 0 || mentionsOf(carol, room) != before {
		t.Fatalf("an edit above the limit: %v", edited)
	}
}

func TestIgnoreBroadMentions(t *testing.T) {
	ts := testkit.NewServer(t)
	owner := ts.Owner
	bob, carol := ts.Member("bob"), ts.Member("carol")
	room := owner.Channel("town", false, bob, carol)
	group := owner.Must(201, "POST", "/api/v1/admin/groups", map[string]any{"handle": "crew", "name": "Crew"})["group"].(map[string]any)["id"].(string)
	owner.Must(204, "PUT", "/api/v1/admin/groups/"+group+"/members/"+string(bob.ID), nil)

	if own(bob, room)["membership"].(map[string]any)["ignore_broad_mentions"] != false {
		t.Fatal("a membership starts with broad mentions heard")
	}
	// The roster is in memory by now; the setting must be seen all the same.
	say(owner, room, "@channel one")
	if mentionsOf(bob, room) != 1 || mentionsOf(carol, room) != 1 {
		t.Fatal("@channel mentions every member")
	}
	stream, _ := bob.Events(nil)
	defer stream.Close()
	ms := bob.Must(200, "PATCH", convPath(room)+"/membership", map[string]any{"ignore_broad_mentions": true})["membership"].(map[string]any)
	if ms["ignore_broad_mentions"] != true {
		t.Fatalf("the membership does not say so: %v", ms)
	}
	if e := stream.Expect("membership.updated"); e["membership"].(map[string]any)["ignore_broad_mentions"] != true {
		t.Fatalf("membership.updated does not say so: %v", e)
	}
	// Bob is connected and active, so @here would reach him too.
	for _, text := range []string{"@channel two", "@here two", "@everyone two"} {
		say(owner, room, text)
		f := stream.ExpectWhere("message.created", func(f map[string]any) bool { return f["message"].(map[string]any)["text"] == text })
		if f["notify"] == "notify" {
			t.Fatalf("%q notified someone who ignores broad mentions", text)
		}
	}
	if got := mentionsOf(bob, room); got != 1 {
		t.Fatalf("bob ignores broad mentions and has %d mentions, want the 1 from before", got)
	}
	if got := mentionsOf(carol, room); got != 3 {
		// @here does not reach carol, who is not connected.
		t.Fatalf("carol has %d mentions, want 3", got)
	}
	// By name and by another group he is still told.
	say(owner, room, "@bob by name")
	if f := stream.ExpectWhere("message.created", func(f map[string]any) bool { return f["message"].(map[string]any)["text"] == "@bob by name" }); f["notify"] != "notify" {
		t.Fatalf("a mention by name of someone who ignores broad mentions: notify %v", f["notify"])
	}
	say(owner, room, "@crew by group")
	say(owner, room, "@channel and @bob together")
	if got := mentionsOf(bob, room); got != 4 {
		t.Fatalf("bob has %d mentions, want 4: one from before, by name, by group, and by name beside @channel", got)
	}
	// The check counts him out too.
	if got := check(owner, room, "@channel count"); got["recipients"] != float64(1) || got["confirm"] != false {
		t.Fatalf("the check with one of two others ignoring: %v", got)
	}
	bob.Must(200, "PATCH", convPath(room)+"/membership", map[string]any{"ignore_broad_mentions": false})
	say(owner, room, "@channel three")
	if got := mentionsOf(bob, room); got != 5 {
		t.Fatalf("bob listens again and has %d mentions, want 5", got)
	}
}

func TestHereReachesOnlyThoseOnline(t *testing.T) {
	ts := testkit.NewServer(t)
	owner := ts.Owner
	on, off := ts.Member("online"), ts.Member("offline")
	room := owner.Channel("town", false, on, off)
	stream, _ := on.Events(nil)
	defer stream.Close()
	if got := check(owner, room, "@here now"); got["broad"] != true || got["allowed"] != true || got["recipients"] != float64(1) {
		t.Fatalf("the check of @here with one member online: %v", got)
	}
	out := say(owner, room, "@here now")
	ents := out["message"].(map[string]any)["entities"].([]any)
	if len(ents) != 1 || ents[0].(map[string]any)["type"] != "broadcast" || ents[0].(map[string]any)["scope"] != "here" {
		t.Fatalf("the @here entity: %v", ents)
	}
	if f := stream.Expect("message.created"); f["notify"] != "notify" {
		t.Fatalf("@here to a member who is online: notify %v", f["notify"])
	}
	if mentionsOf(on, room) != 1 || mentionsOf(off, room) != 0 {
		t.Fatalf("@here mentioned online %d and offline %d, want 1 and 0", mentionsOf(on, room), mentionsOf(off, room))
	}
	// @channel beside it reaches everyone.
	say(owner, room, "@here and @channel")
	if mentionsOf(off, room) != 1 {
		t.Fatal("@channel beside @here did not reach the member who is offline")
	}
}

// crowd puts n more members into a channel, straight into the store.
func crowd(t *testing.T, ts *testkit.TestServer, room string, tag string, n int) {
	t.Helper()
	ctx := context.Background()
	err := ts.Server.Store.Tx(ctx, func(tx store.Tx) error {
		for i := 0; i < n; i++ {
			acc := model.Account{Username: fmt.Sprintf("%s%d", tag, i), DisplayName: "Member", Role: model.RoleMember}
			if _, err := tx.Accounts().Create(ctx, &acc); err != nil {
				return err
			}
			if _, err := tx.Members().AddDirect(ctx, model.ConversationID(room), acc.ID, model.LevelMember, model.Now()); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// A broad mention in a channel of five thousand is answered fast and costs
// the database the same number of statements as one in a channel of fifty.
func TestBroadMentionInALargeChannel(t *testing.T) {
	ts := testkit.NewServer(t)
	owner := ts.Owner
	reader := ts.Member("reader")
	small := owner.Channel("small", false, reader)
	large := owner.Channel("large", false, reader)
	crowd(t, ts, small, "s", 50)
	crowd(t, ts, large, "l", 5000)
	// This test posts more broad mentions than a person may in an hour.
	owner.Must(200, "PATCH", settingsPath, map[string]any{"mentions.broad_per_hour": 0})
	counter, ok := ts.Server.Store.(interface{ Statements() int64 })
	if !ok {
		t.Fatal("the store does not count its statements")
	}
	// cost posts and returns the statements the request took and how
	// long. The work that follows a post (the wake-ups) is let finish
	// first, so that it is not counted against the next one.
	cost := func(room, text string) (int64, time.Duration) {
		t.Helper()
		settle := func() {
			for last := int64(-1); last != counter.Statements(); {
				last = counter.Statements()
				time.Sleep(150 * time.Millisecond)
			}
		}
		settle()
		from, began := counter.Statements(), time.Now()
		out := say(owner, room, text)
		took, n := time.Since(began), counter.Statements()-from
		if broadcasts(out) != 1 {
			t.Fatalf("%q: %v", text, out)
		}
		settle()
		return n, took
	}
	// The store counts every statement the server sends, not this
	// request's alone, and the server has work of its own: the link looks
	// at its outbox and its moves every second, and statuses and mutes are
	// swept every twenty. Whatever of that falls inside a request is
	// counted with it, and a request that takes longer, as on a busy
	// machine, catches more: one run in a parallel `go test ./...` counted
	// a sweep against the large channel and none against the small. Such
	// work only ever adds, so the least of three is the request's own.
	const samples = 3
	least := func(room, text string) (int64, time.Duration) {
		t.Helper()
		n, took := cost(room, text)
		for i := 1; i < samples; i++ {
			n2, took2 := cost(room, text)
			n, took = min(n, n2), min(took, took2)
		}
		return n, took
	}
	// Once each to fill what is kept in memory, then the ones measured.
	for _, word := range []string{"@channel", "@everyone", "@here"} {
		cost(small, word+" warm")
		cost(large, word+" warm")
		few, _ := least(small, word+" measured")
		many, took := least(large, word+" measured")
		t.Logf("%s: %d statements for 51 members, %d for 5,001, in %v", word, few, many, took)
		if many > few+5 {
			t.Errorf("%s: %d statements in a channel of 5,001 and %d in one of 51: the cost grows with the members", word, many, few)
		}
		if took > 2*time.Second {
			t.Errorf("%s in a channel of 5,001 took %v", word, took)
		}
	}
	if got, want := mentionsOf(reader, large), 2*(1+samples); got != want {
		// @here reaches nobody: the reader is not connected.
		t.Errorf("the reader has %d mentions in the large channel, want %d", got, want)
	}
	if got := check(owner, large, "@channel count"); got["recipients"] != float64(5001) || got["confirm"] != true {
		t.Errorf("the check in the large channel: %v", got)
	}
	// Past the default limit a member may not, and is told before sending
	// and after.
	if own(reader, large)["broad_mentions_allowed"] != false {
		t.Error("a channel of 5,001 tells a member broad mentions are allowed")
	}
	if out := say(reader, large, "@channel hello"); out["broad_mentions_refused"] != true {
		t.Errorf("a member's @channel in a channel of 5,001: %v", out)
	}
}

// TestBroadMentionReachesOnlyTheChannel holds review finding H2: a member
// who makes a public channel of their own and is its only member, and so
// its manager, does not reach the instance with @everyone. A broad mention
// reaches the people in the conversation, whatever word is used.
func TestBroadMentionReachesOnlyTheChannel(t *testing.T) {
	ts := testkit.NewServer(t)
	troll, carol, dave := ts.Member("troll"), ts.Member("carol"), ts.Member("dave")
	own := troll.Channel("megaphone", false)
	for _, word := range []string{"@everyone", "@channel", "@here"} {
		if got := check(troll, own, word+" wake up"); got["recipients"] != float64(0) || got["confirm"] != false {
			t.Fatalf("%s from a channel of one: %v", word, got)
		}
		msg := say(troll, own, word+" wake up")["message"].(map[string]any)
		rows, err := ts.Server.Store.Messages().Mentions(context.Background(), model.MessageID(msg["id"].(string)))
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 0 {
			t.Fatalf("%s in a channel of one wrote %d mentions: %v", word, len(rows), rows)
		}
	}
	// With carol in it, @everyone reaches carol and not dave.
	troll.Must(200, "POST", convPath(own)+"/members", map[string]any{"account_ids": []string{string(carol.ID)}})
	carol.AcceptInvitation(convPath(own))
	msg := say(troll, own, "@everyone hi")["message"].(map[string]any)
	rows, err := ts.Server.Store.Messages().Mentions(context.Background(), model.MessageID(msg["id"].(string)))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].AccountID != carol.ID {
		t.Fatalf("@everyone in a channel of two reached %v, want carol only (not dave %s)", rows, dave.ID)
	}
}

// TestBroadMentionsAndAddsPerHour holds the per-person limits of review
// finding H2: past mentions.broad_per_hour a person's broad mentions are
// plain text, as when they may not use one, and past members.adds_per_hour
// adding people answers rate_limited. Bots are not held to either.
func TestBroadMentionsAndAddsPerHour(t *testing.T) {
	ts := testkit.NewServer(t)
	owner := ts.Owner
	ann, bob, cat, dan := ts.Member("ann"), ts.Member("bob"), ts.Member("cat"), ts.Member("dan")
	for k, v := range map[string]any{"mentions.broad_per_hour": 2, "members.adds_per_hour": 3} {
		if got := owner.Must(200, "GET", settingsPath, nil)["settings"].(map[string]any)[k]; got == nil {
			t.Fatalf("%s has no default", k)
		}
		owner.Must(200, "PATCH", settingsPath, map[string]any{k: v})
	}
	owner.Refused(400, model.CodeInvalidValue, "PATCH", settingsPath, map[string]any{"members.adds_per_hour": -1})
	room := ann.Channel("room", false, bob)
	other := ann.Channel("other", false, bob)
	for i, conv := range []string{room, other} {
		if out := say(ann, conv, "@here one"); broadcasts(out) != 1 || out["broad_mentions_refused"] != nil {
			t.Fatalf("broad mention %d: %v", i+1, out)
		}
	}
	// The check does not count, and says no once the hour is used up.
	if got := check(ann, room, "@channel three"); got["allowed"] != false {
		t.Fatalf("the check past the limit: %v", got)
	}
	if out := say(ann, room, "@channel three"); broadcasts(out) != 0 || out["broad_mentions_refused"] != true {
		t.Fatalf("the third broad mention in an hour: %v", out)
	}
	// Someone else is not held to ann's count.
	if out := say(bob, room, "@channel mine"); broadcasts(out) != 1 {
		t.Fatalf("bob's first: %v", out)
	}

	// Adding people: the channel ann made with bob counted 1, other 1.
	ann.Must(200, "POST", convPath(room)+"/members", map[string]any{"account_ids": []string{string(cat.ID)}})
	cat.AcceptInvitation(convPath(room))
	ann.Refused(429, model.CodeRateLimited, "POST", convPath(room)+"/members", map[string]any{"account_ids": []string{string(dan.ID)}})
	ann.Refused(429, model.CodeRateLimited, "POST", "/api/v1/channels", map[string]any{"name": "more", "private": false, "account_ids": []string{string(dan.ID)}})
	// A bot is not held to it.
	owner.Must(200, "PATCH", settingsPath, map[string]any{"members.adds_per_hour": 0})
	bot := owner.Bot("helper")
	ann.Add(room, "member", bot)
	ann.Must(200, "PATCH", convPath(room)+"/members/"+string(bot.ID), map[string]any{"level": "manager"})
	for range 3 {
		if out := say(bot, room, "@here from the bot"); broadcasts(out) != 1 {
			t.Fatalf("a bot's broad mention: %v", out)
		}
	}
}
