// echo-bot is the smallest useful bot: it offers one command, `/echo`, and
// answers it for everyone; and when someone mentions it in a message it
// replies in that message's thread.
//
//	CHURR_URL=http://localhost:8080 CHURR_BOT_TOKEN=churr_bot_… go run ./examples/echo-bot
package main

import (
	"context"
	"log"
	"os"
	"os/signal"

	"github.com/tikaydaes/churr/examples/botkit"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := run(ctx, botkit.FromEnv()); err != nil && ctx.Err() == nil {
		log.Fatal(err)
	}
}

// run is the whole bot.
func run(ctx context.Context, bot *botkit.Bot) error {
	// 1. Who am I? A mention of the bot names this identifier.
	var me struct {
		Account struct {
			ID       string `json:"id"`
			Username string `json:"username"`
		} `json:"account"`
	}
	if err := bot.Call("GET", "/api/v1/me", nil, &me); err != nil {
		return err
	}
	// 2. Offer the command. The list replaces the one the bot had, so
	// the same list is sent on every start.
	if err := bot.SetCommands(map[string]string{"name": "echo", "description": "Say something back", "hint": "<text>"}); err != nil {
		return err
	}
	log.Printf("%s is running", me.Account.Username)

	// 3. Handle events until stopped.
	return bot.Run(ctx, func(e botkit.Event) {
		switch e.Str("type") {
		case "command.invoked":
			// Someone typed /echo. Nothing was posted; this event is
			// all there is, and the bot decides what happens.
			response := map[string]any{"type": "message", "message": map[string]any{"text": e.Str("text")}}
			if e.Str("text") == "" {
				// Only the asker sees a private response.
				response = map[string]any{"type": "private", "text": "Give me something to say: `/echo hello`"}
			}
			if err := bot.Respond(e.Str("interaction_id"), response); err != nil {
				log.Printf("responding: %v", err)
			}

		case "message.created":
			m := e.Obj("message")
			if m.Str("author_id") == me.Account.ID {
				return // its own message
			}
			entities, _ := m["entities"].([]any)
			for _, raw := range entities {
				ent, _ := raw.(map[string]any)
				if ent["type"] == "account" && ent["account_id"] == me.Account.ID {
					// Reply in the thread of the message that
					// mentioned the bot.
					parent := m.Str("parent_id")
					if parent == "" {
						parent = m.Str("id")
					}
					if _, err := bot.Post(m.Str("conversation_id"), map[string]any{"text": "You said: " + m.Str("text"), "parent_id": parent}); err != nil {
						log.Printf("replying: %v", err)
					}
					return
				}
			}
		}
	})
}
