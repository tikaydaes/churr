package main

import (
	"context"
	"testing"
	"time"

	"github.com/tikaydaes/churr/examples/botkit"
	"github.com/tikaydaes/churr/internal/testkit"
)

// The bot is run against a real server, the way the README says to.
func TestEchoBot(t *testing.T) {
	ts := testkit.NewServer(t)
	ann := ts.Member("ann")
	ops := ann.Channel("ops", false)
	account := ts.Owner.Bot("echo")
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

	// /echo hello: answered for everyone, marked with who asked.
	ann.Must(200, "POST", "/api/v1/conversations/"+ops+"/commands", map[string]any{"name": "echo", "text": "hello there"})
	e := stream.Expect("message.created")
	m := e["message"].(map[string]any)
	via, _ := m["via"].(map[string]any)
	if m["text"] != "hello there" || m["author_id"] != string(account.ID) || via["kind"] != "command" || via["account_id"] != string(ann.ID) {
		t.Fatalf("the echo: %v", m)
	}
	stream.Expect("interaction.responded")
	// /echo with nothing: only the asker is told.
	ann.Must(200, "POST", "/api/v1/conversations/"+ops+"/commands", map[string]any{"name": "echo"})
	r := stream.Expect("interaction.responded")
	if resp, _ := r["response"].(map[string]any); resp["type"] != "private" {
		t.Fatalf("the answer to an empty echo: %v", r)
	}
	// A mention is answered in the message's thread.
	parent := ann.Post(ops, "what do you think, @echo?")["id"]
	stream.Expect("message.created") // ann's own
	e = stream.Expect("message.created")
	if m = e["message"].(map[string]any); m["parent_id"] != parent || m["text"] != "You said: what do you think, @echo?" {
		t.Fatalf("the reply to a mention: %v", m)
	}
}
