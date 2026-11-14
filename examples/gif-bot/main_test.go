package main

import (
	"bytes"
	"context"
	"image/gif"
	"testing"
	"time"

	"github.com/tikaydaes/churr/examples/botkit"
	"github.com/tikaydaes/churr/internal/testkit"
)

// The stub finds three GIFs for anything but "nothing".
func TestStubSearch(t *testing.T) {
	for n := 0; n < 3; n++ {
		data := search("cats", n)
		if g, err := gif.DecodeAll(bytes.NewReader(data)); err != nil || len(g.Image) != 2 {
			t.Fatalf("result %d is not a two-frame GIF: %v", n, err)
		}
	}
	if search("cats", 3) != nil || search("nothing", 0) != nil || search("", 0) != nil {
		t.Fatal("the stub found something it should not have")
	}
	if bytes.Equal(search("cats", 0), search("dogs", 0)) || bytes.Equal(search("cats", 0), search("cats", 1)) {
		t.Fatal("different searches give the same GIF")
	}
}

func TestGifBot(t *testing.T) {
	ts := testkit.NewServer(t)
	ann, bob := ts.Member("ann"), ts.Member("bob")
	ops := ann.Channel("ops", false, bob)
	account := ts.Owner.Bot("gifbot")
	ann.Add(ops, "member", account)
	// The bot uploads; without that operation there is nothing to run.
	if status, out := account.SendForm("POST", "/api/v1/conversations/"+ops+"/files", "file", "probe.gif", search("probe", 0)); status == 503 {
		t.Skipf("uploading files is not built on this server yet: %v", out)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- run(ctx, botkit.New(ts.URL, account.Bearer)) }()
	defer func() {
		cancel()
		<-done
	}()
	for deadline := time.Now().Add(5 * time.Second); ts.Server.Live.Connections(account.ID) == 0; time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the bot did not connect")
		}
	}
	annStream, _ := ann.Events(nil)
	bobStream, _ := bob.Events(nil)
	cmd := "/api/v1/conversations/" + ops + "/commands"
	private := func(s *testkit.Stream) string {
		for {
			r := s.Expect("interaction.responded")
			if resp, _ := r["response"].(map[string]any); resp["type"] == "private" {
				return resp["text"].(string)
			}
		}
	}

	// Nothing found: only the asker hears of it.
	ann.Must(200, "POST", cmd, map[string]any{"name": "gif", "text": "nothing"})
	if got := private(annStream); got != "No GIF found for that." {
		t.Fatalf("finding nothing: %q", got)
	}
	// A GIF, for everyone, with who asked and two buttons.
	ann.Must(200, "POST", cmd, map[string]any{"name": "gif", "text": "cats"})
	m := bobStream.Expect("message.created")["message"].(map[string]any)
	id := m["id"].(string)
	files, _ := m["files"].([]any)
	form, _ := m["form"].(map[string]any)
	via, _ := m["via"].(map[string]any)
	if len(files) != 1 || files[0].(map[string]any)["media_type"] != "image/gif" || via["account_id"] != string(ann.ID) || len(form["buttons"].([]any)) != 2 {
		t.Fatalf("the GIF as posted: %v", m)
	}
	first := files[0].(map[string]any)["id"]
	// Someone else presses: told privately, and nothing changes.
	bob.Must(200, "POST", "/api/v1/messages/"+id+"/form/buttons/another", map[string]any{})
	if got := private(bobStream); got != "Only the person who asked can change this." {
		t.Fatalf("another person pressing: %q", got)
	}
	// The asker presses "Another": the message is edited in place.
	ann.Must(200, "POST", "/api/v1/messages/"+id+"/form/buttons/another", map[string]any{})
	edited := bobStream.Expect("message.edited")["message"].(map[string]any)
	if now, _ := edited["files"].([]any); len(now) != 1 || now[0].(map[string]any)["id"] == first || edited["form"] == nil {
		t.Fatalf("the next GIF: %v", edited)
	}
	// And "Remove": it is deleted.
	ann.Must(200, "POST", "/api/v1/messages/"+id+"/form/buttons/remove", map[string]any{})
	bobStream.Expect("message.deleted")
}
