package link_test

import (
	"bytes"
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/link"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
	"github.com/tikaydaes/churr/internal/testkit"
)

// heldMessages is what the hub's holder keeps of a channel: its messages by
// text, as they stand now.
func heldMessages(t *testing.T, hub *testkit.TestHub, stream string) (model.Conversation, map[string]model.Message) {
	t.Helper()
	ctx := context.Background()
	st := hub.Hub.HolderStore()
	if st == nil {
		t.Fatal("the hub has no holder")
	}
	out := map[string]model.Message{}
	row, err := st.Streams().Get(ctx, model.StreamID(stream))
	if err != nil || row.ConversationID == "" {
		return model.Conversation{}, out
	}
	conv, err := st.Conversations().Get(ctx, row.ConversationID)
	if err != nil {
		t.Fatal(err)
	}
	msgs, _, err := st.Messages().List(ctx, conv.ID, store.MessageList{SeqPage: store.SeqPage{Limit: 200}})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range msgs {
		key := m.Text
		if m.Deleted() {
			key = "deleted:" + string(m.ID)
		}
		out[key] = m
	}
	return conv, out
}

// The home hub keeps a channel it owns whole, in a database of its own: what
// was said before the channel was shared (which the instance that shared it
// gives the hub), and every message since as it now stands, edited or
// deleted, with its reactions, the channel's members and their profiles. It
// is in no record's list of instances.
func TestTheHubHoldsTheWholeChannel(t *testing.T) {
	l := testkit.NewLinked(t, 2, true)
	a, b := l.Servers[0], l.Servers[1]
	ann := a.Member("ann")
	ch := a.Owner.Channel("ledger", false, ann)
	a.Owner.Post(ch, "said before the channel was shared")
	stream, local := l.Share(0, ch, "ledger", map[string]any{"listing": "open", "history_days": 1}, 1)
	ann.Post(ch, "from ann")
	edited := b.Owner.Post(local[1], "from b, before the edit")
	gone := a.Owner.Post(ch, "deleted for everyone")
	b.Owner.Must(200, "PATCH", "/api/v1/messages/"+edited["id"].(string), map[string]any{"text": "from b, edited"})
	b.Owner.HasMessage(local[1], "deleted for everyone")
	b.Owner.Must(200, "PUT", "/api/v1/messages/"+gone["id"].(string)+"/reactions/👍", nil)
	a.Owner.Must(200, "DELETE", "/api/v1/messages/"+gone["id"].(string), nil)
	reacted := b.Owner.HasMessage(local[1], "from ann")
	b.Owner.Must(200, "PUT", "/api/v1/messages/"+reacted["id"].(string)+"/reactions/🎉", nil)
	a.Owner.HasMessage(ch, "from b, edited")

	var held map[string]model.Message
	var conv model.Conversation
	testkit.Eventually(t, "the hub holds every message as it stands", func() bool {
		conv, held = heldMessages(t, l.Hub, stream)
		_, before := held["said before the channel was shared"]
		_, fromAnn := held["from ann"]
		_, edit := held["from b, edited"]
		_, del := held["deleted:"+gone["id"].(string)]
		return before && fromAnn && edit && del
	})
	if _, stale := held["from b, before the edit"]; stale {
		t.Error("the hub holds the text from before the edit as the message")
	}
	if held["from b, edited"].ID != model.MessageID(edited["id"].(string)) {
		t.Error("the edited message has another id on the hub")
	}
	ctx := context.Background()
	st := l.Hub.Hub.HolderStore()
	testkit.Eventually(t, "the hub holds the reaction", func() bool {
		rs, err := st.Reactions().List(ctx, held["from ann"].ID, "")
		return err == nil && len(rs) == 1 && rs[0].Emoji == "🎉"
	})
	members, err := st.Members().IDs(ctx, conv.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := map[model.AccountID]bool{a.Owner.ID: true, ann.ID: true, b.Owner.ID: true}
	for _, id := range members {
		delete(want, id)
	}
	if len(want) != 0 {
		t.Fatalf("the hub's copy lacks members %v; it has %v", want, members)
	}
	acc, err := st.Accounts().Get(ctx, ann.ID)
	if err != nil || acc.Username != "ann" || acc.ServerName != "a.localhost" {
		t.Fatalf("the hub holds ann as %+v (%v)", acc, err)
	}
	if rec := hubChannel(a.Owner, stream); names(rec["servers"]) != "a.localhost,b.localhost" {
		t.Fatalf("the channel's instances are %v", rec["servers"])
	}
	if r := l.Hub.Hub.Holder().StatsNow().Rejected; len(r) != 0 {
		t.Fatalf("the hub's holder rejected %v", r)
	}
}

// A joining instance is given its history by the channel's home hub, and by
// nobody else: with every other instance of the channel switched off, it is
// given all it is owed, and who said it.
func TestAJoinerTakesHistoryFromTheHubAlone(t *testing.T) {
	l := testkit.NewLinked(t, 3, true)
	a, b, c := l.Servers[0], l.Servers[1], l.Servers[2]
	ch := a.Owner.Channel("annals", false)
	a.Owner.Post(ch, "from a, before it was shared")
	stream, local := l.Share(0, ch, "annals", map[string]any{"listing": "open", "history_days": 30}, 1)
	a.Owner.Post(ch, "from a, on the hub")
	said := b.Owner.Post(local[1], "from b, on the hub")
	a.Owner.HasMessage(ch, "from b, on the hub")
	testkit.Eventually(t, "the hub holds both", func() bool {
		_, held := heldMessages(t, l.Hub, stream)
		_, fromA := held["from a, on the hub"]
		_, fromB := held["from b, on the hub"]
		return fromA && fromB
	})
	a.Stop()
	b.Stop()
	convC := joinListed(l, 2, stream, "annals-c")
	l.Joined(2, stream)
	for _, text := range []string{"from a, before it was shared", "from a, on the hub", "from b, on the hub"} {
		c.Owner.HasMessage(convC, text)
	}
	author := c.Owner.Must(200, "GET", "/api/v1/accounts/"+said["author_id"].(string), nil)["account"].(map[string]any)
	if author["username"] != "owner" || author["server_name"] != "b.localhost" {
		t.Fatalf("c shows b's author as %v", author)
	}
	if d := c.Server.Link.StatsNow().Dropped; d["history_not_from_home"] != 0 || d["history_unasked"] != 0 {
		t.Fatalf("c dropped history: %v", d)
	}
}

// fileOn is a message's first file as an instance's owner is shown it, once
// it has arrived there: its address and the bytes it serves.
func fileOn(t *testing.T, c *testkit.Client, conv, text string) (map[string]any, []byte) {
	t.Helper()
	var file map[string]any
	testkit.Eventually(t, "the file of "+text+" arrives", func() bool {
		for _, m := range c.Messages(conv) {
			if m["text"] == text && len(arr(m["files"])) > 0 {
				file = arr(m["files"])[0].(map[string]any)
				url, _ := file["url"].(string)
				return url != "" && file["state"] == nil
			}
		}
		return false
	})
	status, _, got := c.Get(file["url"].(string), nil)
	if status != 200 {
		t.Fatalf("the file of %q is served with status %d", text, status)
	}
	return file, got
}

// ready reports whether an instance or a hub's holder holds a file's bytes.
func ready(st store.Store, id string) bool {
	f, err := st.Files().Get(context.Background(), model.FileID(id))
	return err == nil && f.BlobKey != "" && (f.State == "" || f.State == "ready")
}

// The hub fetches every file of a channel it owns from its origin when its
// message passes, a custom emoji and a picture included, checks each by its
// hash, keeps it, and serves it: an instance that joins with the author's
// instance switched off is given every file by the hub.
func TestTheHubKeepsAndServesFiles(t *testing.T) {
	l := testkit.NewLinked(t, 3, true)
	a, b, c := l.Servers[0], l.Servers[1], l.Servers[2]
	ch := a.Owner.Channel("drawings", false)
	stream, local := l.Share(0, ch, "drawings", map[string]any{"listing": "open", "history_days": 30}, 1)
	if status, out := b.Owner.SendForm("PUT", "/api/v1/me/avatar", "file", "me.png", pngBytes(t)); status != 200 {
		t.Fatalf("b's owner's picture: %d %v", status, out)
	}
	status, made := b.Owner.Form("POST", "/api/v1/emoji", testkit.Part{Field: "name", Content: []byte("party")},
		testkit.Part{Field: "file", FileName: "party.png", Content: pngBytes(t)})
	if status != 201 {
		t.Fatalf("making the emoji: %d %v", status, made)
	}
	emojiID := made["emoji"].(map[string]any)["id"].(string)
	b.Owner.Post(local[1], "well done :party:")
	content := bytes.Repeat([]byte("a file the hub keeps, in more than one chunk. "), 12000)
	up := b.Owner.Upload(local[1], "notes.bin", content)
	b.Owner.Must(201, "POST", "/api/v1/conversations/"+local[1]+"/messages", map[string]any{"text": "with a file", "file_ids": []any{up["id"]}})
	fileOn(t, a.Owner, ch, "with a file")
	me := b.Owner.Must(200, "GET", "/api/v1/me", nil)["account"].(map[string]any)
	avatar, _ := me["avatar_file_id"].(string)
	if avatar == "" {
		t.Fatalf("b's owner has no picture: %v", me)
	}
	held := l.Hub.Hub.HolderStore()
	testkit.Eventually(t, "the hub holds the file, the emoji and the picture", func() bool {
		return ready(held, up["id"].(string)) && ready(held, emojiID) && ready(held, avatar)
	})
	f, _ := held.Files().Get(context.Background(), model.FileID(up["id"].(string)))
	if f.Size != int64(len(content)) {
		t.Fatalf("the hub holds %d bytes of a file of %d", f.Size, len(content))
	}

	b.Stop()
	convC := joinListed(l, 2, stream, "drawings-c")
	l.Joined(2, stream)
	c.Owner.HasMessage(convC, "well done :party:")
	if _, got := fileOn(t, c.Owner, convC, "with a file"); !bytes.Equal(got, content) {
		t.Fatalf("c was given %d bytes, not the %d posted", len(got), len(content))
	}
	testkit.Eventually(t, "c holds the emoji and b's owner's picture, from the hub", func() bool {
		return ready(c.Server.Store, emojiID) && ready(c.Server.Store, avatar)
	})
}

// A channel handed to another hub of the cluster is taken whole by its new
// home from the old one, with every instance switched off: what was said
// before, and its files. The old home then lets its copy go, and a hub that
// only carries the channel never holds any of it.
func TestHandingAChannelOverMovesItsContents(t *testing.T) {
	l, stream, local := clustered(t, "star")
	b := l.Servers[1]
	b.Owner.Post(local[1], "said before the handover")
	content := bytes.Repeat([]byte("a file posted before the handover. "), 9000)
	up := b.Owner.Upload(local[1], "old.bin", content)
	b.Owner.Must(201, "POST", "/api/v1/conversations/"+local[1]+"/messages", map[string]any{"text": "with an old file", "file_ids": []any{up["id"]}})
	old, edge, next := l.Cluster.Hubs[0], l.Cluster.Hubs[1], l.Cluster.Hubs[2]
	holds := func(h *testkit.TestHub) bool {
		_, held := heldMessages(t, h, stream)
		_, said := held["said before the handover"]
		return said && ready(h.Hub.HolderStore(), up["id"].(string))
	}
	testkit.Eventually(t, "the home hub holds the message and the file", func() bool { return holds(old) })
	for _, ts := range l.Servers {
		ts.Stop()
	}
	if err := old.Hub.Rehome(stream, testkit.HubName(2)); err != nil {
		t.Fatal(err)
	}
	old.Hub.Reconcile()
	testkit.Eventually(t, "the move is done", func() bool { return old.Hub.RehomeResult(stream) == "done" })
	testkit.Eventually(t, "the new home holds the message and the file, from the old home", func() bool {
		return holds(next)
	})
	f, _ := next.Hub.HolderStore().Files().Get(context.Background(), model.FileID(up["id"].(string)))
	if f.Size != int64(len(content)) {
		t.Fatalf("the new home holds %d bytes of a file of %d", f.Size, len(content))
	}
	ctx := context.Background()
	if _, err := edge.Hub.HolderStore().Streams().Get(ctx, model.StreamID(stream)); err == nil {
		t.Fatal("a hub that only carries the channel holds a copy of it")
	}
	old.Hub.Holder().ForgetHandedOver(ctx, model.Now().Add(time.Minute))
	if _, err := old.Hub.HolderStore().Streams().Get(ctx, model.StreamID(stream)); err == nil {
		t.Fatal("the old home still holds the channel it handed over")
	}
	if _, held := heldMessages(t, next, stream); len(held) < 2 {
		t.Fatalf("the new home holds %d messages after the old home let go", len(held))
	}
}

// The closing case of "the hub holds the whole channel": an instance drops
// off for good, every other instance goes offline, and an instance that
// joins afterwards is given, by the hub alone, the whole history, the names
// and pictures of the people of the instance that is gone, and their files.
func TestTheHubAloneGivesAJoinerEverything(t *testing.T) {
	l := testkit.NewLinked(t, 4, true)
	a, b, c, d := l.Servers[0], l.Servers[1], l.Servers[2], l.Servers[3]
	ch := a.Owner.Channel("commons", false)
	a.Owner.Post(ch, "from a, before the channel was shared")
	stream, local := l.Share(0, ch, "commons", map[string]any{"listing": "open", "history_days": 90}, 1)
	local[2] = joinListed(l, 2, stream, "commons-c")
	l.Joined(2, stream)

	// b's people: its owner, with a picture and an emoji of b's, and bea.
	if status, out := b.Owner.SendForm("PUT", "/api/v1/me/avatar", "file", "me.png", pngBytes(t)); status != 200 {
		t.Fatalf("b's owner's picture: %d %v", status, out)
	}
	b.Owner.Must(200, "PATCH", "/api/v1/me", map[string]any{"display_name": "Bo Owner"})
	bea := b.Member("bea")
	bea.Must(200, "PATCH", "/api/v1/me", map[string]any{"display_name": "Bea Departed"})
	bea.Must(200, "POST", "/api/v1/conversations/"+local[1]+"/join", nil)
	status, made := b.Owner.Form("POST", "/api/v1/emoji", testkit.Part{Field: "name", Content: []byte("wave")},
		testkit.Part{Field: "file", FileName: "wave.png", Content: pngBytes(t)})
	if status != 201 {
		t.Fatalf("making the emoji: %d %v", status, made)
	}
	emojiID := made["emoji"].(map[string]any)["id"].(string)
	b.Owner.Post(local[1], "goodbye from b :wave:")
	fromBea := bea.Post(local[1], "bea was here")
	content := bytes.Repeat([]byte("a file whose instance is gone for good. "), 10000)
	up := b.Owner.Upload(local[1], "left.bin", content)
	b.Owner.Must(201, "POST", "/api/v1/conversations/"+local[1]+"/messages", map[string]any{"text": "a file left behind", "file_ids": []any{up["id"]}})
	a.Owner.Post(ch, "from a, on the hub")
	c.Owner.Post(local[2], "from c, on the hub")
	me := b.Owner.Must(200, "GET", "/api/v1/me", nil)["account"].(map[string]any)
	avatar := me["avatar_file_id"].(string)
	held := l.Hub.Hub.HolderStore()
	texts := []string{"from a, before the channel was shared", "goodbye from b :wave:", "bea was here", "a file left behind",
		"from a, on the hub", "from c, on the hub"}
	testkit.Eventually(t, "the hub holds everything", func() bool {
		_, msgs := heldMessages(t, l.Hub, stream)
		for _, text := range texts {
			if _, ok := msgs[text]; !ok {
				return false
			}
		}
		// b's people's names as they set them, which their profiles carry
		// in account.updated, made a moment after each change.
		for id, name := range map[model.AccountID]string{b.Owner.ID: "Bo Owner", model.AccountID(fromBea["author_id"].(string)): "Bea Departed"} {
			if acc, err := held.Accounts().Get(context.Background(), id); err != nil || acc.DisplayName != name {
				return false
			}
		}
		return ready(held, up["id"].(string)) && ready(held, emojiID) && ready(held, avatar)
	})

	// b drops off for good: switched off, and removed from the hub.
	b.Stop()
	if err := l.Hub.Hub.Remove("b.localhost"); err != nil {
		t.Fatal(err)
	}
	l.Hub.Hub.Reconcile()
	testkit.Eventually(t, "the hub has taken b out of the channel", func() bool {
		return names(hubChannel(a.Owner, stream)["servers"]) == "a.localhost,c.localhost"
	})
	// Every other instance goes offline.
	a.Stop()
	c.Stop()

	convD := joinListed(l, 3, stream, "commons-d")
	l.Joined(3, stream)
	for _, text := range texts {
		d.Owner.HasMessage(convD, text)
	}
	if _, got := fileOn(t, d.Owner, convD, "a file left behind"); !bytes.Equal(got, content) {
		t.Fatalf("d was given %d bytes of b's file, not the %d posted", len(got), len(content))
	}
	for id, want := range map[string][2]string{string(b.Owner.ID): {"owner", "Bo Owner"}, fromBea["author_id"].(string): {"bea", "Bea Departed"}} {
		acc := d.Owner.Must(200, "GET", "/api/v1/accounts/"+id, nil)["account"].(map[string]any)
		if acc["username"] != want[0] || acc["display_name"] != want[1] || acc["server_name"] != "b.localhost" {
			t.Fatalf("d shows b's person %s as %v", id, acc)
		}
	}
	testkit.Eventually(t, "d holds b's owner's picture and b's emoji, from the hub", func() bool {
		return ready(d.Server.Store, avatar) && ready(d.Server.Store, emojiID)
	})
	if r := d.Server.Link.StatsNow().Rejected; len(r) != 0 {
		t.Fatalf("d rejected %v", r)
	}
}

// The hub's holder writes down whom it still waits on for what was said
// before it held a channel, and asks until it has it: an instance that does
// not answer is asked again, round after round; after the hub restarts the
// asking resumes from what was written down; and the instance, away through
// all of it, is asked as soon as it connects again.
func TestTheHubAsksForEarlierHistoryUntilItHasIt(t *testing.T) {
	defer link.SetFirstHistoryWait(200 * time.Millisecond)()
	defer link.SetHistoryOwedWait(time.Hour)()
	deaf := link.SetDeafToHome("a.localhost")
	defer deaf()
	l := testkit.NewLinked(t, 2, true)
	a := l.Servers[0]
	ch := a.Owner.Channel("archive", false)
	a.Owner.Post(ch, "said before the hub held it")
	stream, _ := l.Share(0, ch, "archive", map[string]any{"listing": "open", "history_days": 30}, 1)
	ctx, id := context.Background(), model.StreamID(stream)
	owed := func() string {
		if h := l.Hub.Hub.Holder(); h != nil {
			return fmt.Sprint(h.HistoryOwed(ctx, id))
		}
		return "no holder"
	}
	testkit.Eventually(t, "the holder writes down that a owes it the history", func() bool { return owed() == "[a.localhost]" })
	time.Sleep(time.Second) // rounds of asking, unanswered
	if _, held := heldMessages(t, l.Hub, stream); len(held) != 0 || owed() != "[a.localhost]" {
		t.Fatalf("the hub holds %v while a has answered nothing; it waits on %s", held, owed())
	}

	// a goes away and the hub restarts. From here on only a's connecting
	// again wakes the asking: the rounds would wait an hour.
	a.Stop()
	defer link.SetFirstHistoryWait(time.Hour)()
	l.Hub.Restart()
	deaf()
	testkit.Eventually(t, "after the restart the holder still waits on a", func() bool { return owed() == "[a.localhost]" })
	a.Restart()
	testkit.Eventually(t, "the hub holds what a said before the hub held the channel", func() bool {
		_, held := heldMessages(t, l.Hub, stream)
		_, ok := held["said before the hub held it"]
		return ok
	})
	testkit.Eventually(t, "nobody owes the hub anything more", func() bool { return owed() == "[]" })
}

// The old home of a channel handed to another hub of the cluster whose
// holder was cut off at the moment of the handover learns of it when the
// holder connects again: it takes nothing more of the channel, gives the new
// home what it holds, and lets its copy go after the same seven days. Until
// then the new home goes on waiting for it, with every instance away.
func TestAHandoverTheOldHomeMissedIsLearnedWhenItReturns(t *testing.T) {
	defer link.SetFirstHistoryWait(200 * time.Millisecond)()
	defer link.SetHistoryOwedWait(500 * time.Millisecond)()
	l, stream, local := clustered(t, "star")
	l.Servers[1].Owner.Post(local[1], "said before the handover")
	old, next := l.Cluster.Hubs[0], l.Cluster.Hubs[2]
	ctx, id := context.Background(), model.StreamID(stream)
	holds := func(h *testkit.TestHub) bool {
		_, held := heldMessages(t, h, stream)
		_, said := held["said before the handover"]
		return said
	}
	testkit.Eventually(t, "the home hub holds the message", func() bool { return holds(old) })
	if err := old.Hub.TestHolder(false); err != nil {
		t.Fatal(err)
	}
	for _, ts := range l.Servers {
		ts.Stop()
	}
	if err := old.Hub.Rehome(stream, testkit.HubName(2)); err != nil {
		t.Fatal(err)
	}
	old.Hub.Reconcile()
	testkit.Eventually(t, "the move is done", func() bool { return old.Hub.RehomeResult(stream) == "done" })
	testkit.Eventually(t, "the new home waits on the old one", func() bool {
		owed := next.Hub.Holder().HistoryOwed(ctx, id)
		return len(owed) > 0 && owed[0] == testkit.HubName(0)
	})
	time.Sleep(time.Second)
	if holds(next) {
		t.Fatal("the new home holds the message though nobody that holds it could answer")
	}

	if err := old.Hub.TestHolder(true); err != nil {
		t.Fatal(err)
	}
	testkit.Eventually(t, "the old home's holder learns it is no longer the home", func() bool {
		st, err := old.Hub.HolderStore().Streams().Get(ctx, id)
		return err == nil && st.State == store.StreamRemoved && st.HomeHub == testkit.HubName(2)
	})
	testkit.Eventually(t, "the new home holds the message, from the old home", func() bool { return holds(next) })
	testkit.Eventually(t, "and waits on nobody", func() bool { return len(next.Hub.Holder().HistoryOwed(ctx, id)) == 0 })
	old.Hub.Holder().ForgetHandedOver(ctx, model.Now().Add(-time.Minute))
	if _, err := old.Hub.HolderStore().Streams().Get(ctx, id); err != nil {
		t.Fatal("the old home let its copy go before its seven days")
	}
	old.Hub.Holder().ForgetHandedOver(ctx, model.Now().Add(time.Minute))
	if _, err := old.Hub.HolderStore().Streams().Get(ctx, id); err == nil {
		t.Fatal("the old home still holds the channel it handed over")
	}
}

// What everyone in a channel sees of a poll and of a thread is held by the
// hub and given to an instance that joins with every other switched off: the
// poll with its counts, who voted for what and that it is closed, and a
// thread whose first message was deleted for everyone, with its replies.
func TestAJoinerIsGivenPollsAndThreadsAsTheyStand(t *testing.T) {
	l := testkit.NewLinked(t, 3, true)
	a, b, c := l.Servers[0], l.Servers[1], l.Servers[2]
	a.Server.Link.TestTallyFold(50 * time.Millisecond)
	ch := a.Owner.Channel("council", false)
	stream, local := l.Share(0, ch, "council", map[string]any{"listing": "open", "history_days": 30}, 1)
	msgs := "/api/v1/conversations/" + ch + "/messages"
	poll := a.Owner.Must(201, "POST", msgs, pollBody("a vote", nil))["message"].(map[string]any)["id"].(string)
	b.Owner.HasMessage(local[1], "a vote")
	answer(b.Owner, poll, "y")
	answer(a.Owner, poll, "n")
	testkit.Eventually(t, "b sees both answers counted", func() bool {
		f := formOf(b.Owner, poll)
		return count(f, "y") == 1 && count(f, "n") == 1
	})
	a.Owner.Must(200, "POST", "/api/v1/messages/"+poll+"/form/close", nil)
	testkit.Eventually(t, "b sees the poll closed", func() bool { return formOf(b.Owner, poll)["closed_at"] != nil })
	root := a.Owner.Post(ch, "a thread that goes")["id"].(string)
	b.Owner.HasMessage(local[1], "a thread that goes")
	reply := b.Owner.Must(201, "POST", "/api/v1/conversations/"+local[1]+"/messages", map[string]any{"text": "a reply that stays", "parent_id": root})["message"].(map[string]any)["id"].(string)
	testkit.Eventually(t, "a holds the reply", func() bool {
		status, _, _ := a.Owner.Get("/api/v1/messages/"+reply, nil)
		return status == 200
	})
	a.Owner.Must(200, "DELETE", "/api/v1/messages/"+root, nil)
	ctx := context.Background()
	held := l.Hub.Hub.HolderStore()
	testkit.Eventually(t, "the hub holds the poll as it stands and the thread", func() bool {
		f, found, err := held.Forms().ByMessage(ctx, model.MessageID(poll))
		if err != nil || !found || f.AnswerCount != 2 || f.ClosedAt.IsZero() {
			return false
		}
		r, err := held.Messages().Get(ctx, model.MessageID(reply))
		gone, err2 := held.Messages().Get(ctx, model.MessageID(root))
		return err == nil && err2 == nil && r.ParentID == model.MessageID(root) && gone.Deleted()
	})

	a.Stop()
	b.Stop()
	convC := joinListed(l, 2, stream, "council-c")
	l.Joined(2, stream)
	c.Owner.HasMessage(convC, "a vote")
	f := formOf(c.Owner, poll)
	if count(f, "y") != 1 || count(f, "n") != 1 || f["answer_count"] != float64(2) || f["closed_at"] == nil {
		t.Fatalf("c is shown the poll as %v", f)
	}
	if voters := fmt.Sprint(f["voters"]); voters != fmt.Sprint(map[string]any{"q": map[string]any{"n": []any{string(a.Owner.ID)}, "y": []any{string(b.Owner.ID)}}}) {
		t.Fatalf("c is shown the voters as %v", voters)
	}
	got := c.Owner.Must(200, "GET", "/api/v1/messages/"+reply, nil)["message"].(map[string]any)
	if got["text"] != "a reply that stays" || got["parent_id"] != root {
		t.Fatalf("c holds the reply as %v", got)
	}
	if gone := c.Owner.Must(200, "GET", "/api/v1/messages/"+root, nil)["message"].(map[string]any); gone["deleted_at"] == nil || (gone["text"] != nil && gone["text"] != "") {
		t.Fatalf("c holds the deleted first message as %v", gone)
	}
}

// The new home of a channel handed to it loses nothing said in the channel
// before it has the channel's history: what it cannot apply yet, for want of
// the authors that history names, waits for it, however long it is in
// coming, and is applied when it arrives.
func TestTheNewHomeLosesNothingSaidBeforeItHasTheHistory(t *testing.T) {
	defer link.SetFirstHistoryWait(200 * time.Millisecond)()
	defer link.SetHistoryOwedWait(500 * time.Millisecond)()
	defer link.SetHistoryWaitMax(200 * time.Millisecond)()
	l, stream, local := clustered(t, "star")
	l.Servers[1].Owner.Post(local[1], "said before the handover")
	old, next := l.Cluster.Hubs[0], l.Cluster.Hubs[2]
	ctx, id := context.Background(), model.StreamID(stream)
	texts := []string{"said before the handover"}
	holds := func(h *testkit.TestHub) bool {
		_, held := heldMessages(t, h, stream)
		for _, text := range texts {
			if _, ok := held[text]; !ok {
				return false
			}
		}
		return true
	}
	testkit.Eventually(t, "the home hub holds the message", func() bool { return holds(old) })
	// Nobody who holds the channel's history gives it to the new home for a
	// while: the old home's holder is off, and every instance is deaf to it.
	if err := old.Hub.TestHolder(false); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a.localhost", "b.localhost", "c.localhost"} {
		defer link.SetDeafToHome(name)()
	}
	if err := old.Hub.Rehome(stream, testkit.HubName(2)); err != nil {
		t.Fatal(err)
	}
	old.Hub.Reconcile()
	testkit.Eventually(t, "the move is done", func() bool { return old.Hub.RehomeResult(stream) == "done" })
	testkit.Eventually(t, "the new home waits for the history", func() bool { return len(next.Hub.Holder().HistoryOwed(ctx, id)) > 0 })
	for i, ts := range l.Servers {
		text := fmt.Sprintf("after the move, from %d", i)
		ts.Owner.Post(local[i], text)
		texts = append(texts, text)
	}
	sameOrder(t, l, local, len(texts))
	time.Sleep(time.Second) // well past the wait before what is delivered is applied
	if holds(next) {
		t.Fatal("the new home holds what was said after the move though it has met none of its authors")
	}

	for _, name := range []string{"a.localhost", "b.localhost", "c.localhost"} {
		link.SetDeafToHome(name)()
	}
	if err := old.Hub.TestHolder(true); err != nil {
		t.Fatal(err)
	}
	testkit.Eventually(t, "the new home holds everything, from before the move and after it", func() bool { return holds(next) })
	testkit.Eventually(t, "and waits on nobody", func() bool { return len(next.Hub.Holder().HistoryOwed(ctx, id)) == 0 })
	if r := next.Hub.Holder().StatsNow().Rejected; r["unknown_account"] != 0 || r["not_a_member"] != 0 {
		t.Fatalf("the new home rejected %v", r)
	}
}

// An instance that joins while the hub's holder still waits for some of what
// was said before the hub held the channel is given what the holder has, and
// the rest once the holder has it: across a restart of the instance and of
// the hub. What it is delivered meanwhile from people it has not met waits,
// and is not lost.
func TestAnEarlyJoinerIsGivenTheRestOfTheHistory(t *testing.T) {
	defer link.SetFirstHistoryWait(200 * time.Millisecond)()
	defer link.SetHistoryOwedWait(500 * time.Millisecond)()
	defer link.SetHistoryWaitMax(200 * time.Millisecond)()
	deaf := link.SetDeafToHome("a.localhost")
	defer deaf()
	l := testkit.NewLinked(t, 2, true)
	a, b := l.Servers[0], l.Servers[1]
	ch := a.Owner.Channel("annals", false)
	a.Owner.Post(ch, "said before the hub held it")
	stream, local := l.Share(0, ch, "annals", map[string]any{"listing": "open", "history_days": 30}, 1)
	ctx, id := context.Background(), model.StreamID(stream)
	// The holder writes down every instance of the first record it takes.
	// On a slow machine that record already names b, which then answers
	// with the nothing it holds and comes off the list; a, deaf, stays.
	testkit.Eventually(t, "the holder waits on a alone", func() bool {
		owed := l.Hub.Hub.Holder().HistoryOwed(ctx, id)
		return len(owed) == 1 && owed[0] == "a.localhost"
	})
	a.Owner.Post(ch, "said after b joined")
	time.Sleep(time.Second)
	for _, m := range b.Owner.Messages(local[1]) {
		if m["text"] == "said before the hub held it" {
			t.Fatal("b holds what the hub does not hold yet")
		}
	}
	b.Restart()
	l.Hub.Restart()
	deaf()
	b.Owner.HasMessage(local[1], "said before the hub held it")
	b.Owner.HasMessage(local[1], "said after b joined")
	testkit.Eventually(t, "the holder has given nobody a short history it has not made whole", func() bool {
		return len(l.Hub.Hub.Holder().ShortGiven(ctx, id)) == 0
	})
	if r := b.Server.Link.StatsNow().Rejected; r["unknown_account"] != 0 || r["not_a_member"] != 0 {
		t.Fatalf("b rejected %v", r)
	}
}
