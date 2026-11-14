package main

import (
	"context"
	"testing"
	"time"

	"github.com/tikaydaes/churr/examples/botkit"
	"github.com/tikaydaes/churr/internal/testkit"
)

func TestPollBot(t *testing.T) {
	ts := testkit.NewServer(t)
	ann := ts.Member("ann")
	ops := ann.Channel("ops", false)
	account := ts.Owner.Bot("pollbot")
	ann.Add(ops, "member", account)

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
	stream, _ := ann.Events(nil)
	cmd := "/api/v1/conversations/" + ops + "/commands"
	pollOf := func(e map[string]any) (question string, options int) {
		form, _ := e["message"].(map[string]any)["form"].(map[string]any)
		fields, _ := form["fields"].([]any)
		if len(fields) != 1 {
			t.Fatalf("a poll has one field: %v", e)
		}
		f := fields[0].(map[string]any)
		opts, _ := f["options"].([]any)
		return f["label"].(string), len(opts)
	}

	// With a question and options: the poll, at once.
	ann.Must(200, "POST", cmd, map[string]any{"name": "quickpoll", "text": "Lunch? | Pizza | Sushi | Salad"})
	if q, n := pollOf(stream.Expect("message.created")); q != "Lunch?" || n != 3 {
		t.Fatalf("the poll: %q with %d options", q, n)
	}
	// Too little: only the asker is told.
	ann.Must(200, "POST", cmd, map[string]any{"name": "quickpoll", "text": "Lunch?"})
	for {
		r := stream.Expect("interaction.responded")
		if resp, _ := r["response"].(map[string]any); resp["type"] == "private" {
			break
		}
	}
	// With nothing: a dialog, whose answer becomes the poll.
	ann.Must(200, "POST", cmd, map[string]any{"name": "quickpoll"})
	var dialog string
	for dialog == "" {
		r := stream.Expect("interaction.responded")
		if resp, _ := r["response"].(map[string]any); resp["type"] == "dialog" {
			dialog = resp["dialog"].(map[string]any)["id"].(string)
		}
	}
	ann.Must(200, "POST", "/api/v1/dialogs/"+dialog+"/submit", map[string]any{"button_id": "post",
		"answers": map[string]any{"question": "Where next?", "options": "Lisbon\nOslo\n\nKyoto"}})
	if q, n := pollOf(stream.Expect("message.created")); q != "Where next?" || n != 3 {
		t.Fatalf("the poll from the dialog: %q with %d options", q, n)
	}
}
