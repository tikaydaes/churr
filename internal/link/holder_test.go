package link_test

import (
	"bytes"
	"context"
	"testing"

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
