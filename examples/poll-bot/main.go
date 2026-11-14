// poll-bot offers `/quickpoll`. Given a question and options it posts a
// poll for everyone; given nothing it asks for them in a dialog first.
//
//	/quickpoll Lunch? | Pizza | Sushi | Salad
//	/quickpoll
//
// The poll itself is an ordinary form with the `tally` action: the server
// keeps the votes and the tally, and the bot has nothing more to do. The
// dialog shows the other half of interactions: a bot asking one person for
// more, and answering what comes back.
//
//	CHURR_URL=http://localhost:8080 CHURR_BOT_TOKEN=churr_bot_… go run ./examples/poll-bot
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"

	"github.com/tikaydaes/churr/examples/botkit"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := run(ctx, botkit.FromEnv()); err != nil && ctx.Err() == nil {
		log.Fatal(err)
	}
}

// poll builds the message that carries a poll: no text, one form.
func poll(question string, options []string) map[string]any {
	choices := []map[string]any{}
	for i, o := range options {
		choices = append(choices, map[string]any{"id": fmt.Sprintf("o%d", i+1), "label": o})
	}
	return map[string]any{"form": map[string]any{
		"preset": "poll",
		"fields": []any{map[string]any{"id": "choice", "type": "choice", "label": question, "required": true, "options": choices}},
		"action": map[string]any{"type": "tally"},
	}}
}

// split reads "Question? | Option | Option" (or one option per line).
func split(text, sep string) []string {
	var out []string
	for _, part := range strings.Split(text, sep) {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

// answer responds to an interaction with a poll, or tells the asker alone
// what was missing.
func answer(bot *botkit.Bot, interaction, question string, options []string) {
	response := map[string]any{"type": "message", "message": poll(question, options)}
	switch {
	case question == "" || len(options) < 2:
		response = map[string]any{"type": "private", "text": "A poll needs a question and at least two options."}
	case len(options) > 10:
		response = map[string]any{"type": "private", "text": "Ten options at most, please."}
	}
	if err := bot.Respond(interaction, response); err != nil {
		log.Printf("responding: %v", err)
	}
}

func run(ctx context.Context, bot *botkit.Bot) error {
	if err := bot.SetCommands(map[string]string{"name": "quickpoll", "description": "Ask the channel a question", "hint": "Question? | Option | Option"}); err != nil {
		return err
	}
	log.Print("poll-bot is running")
	return bot.Run(ctx, func(e botkit.Event) {
		switch e.Str("type") {
		case "command.invoked":
			parts := split(e.Str("text"), "|")
			if len(parts) == 0 {
				// Nothing was typed after the name: ask in a dialog.
				// Only the asker sees it, and its answer comes back
				// as form.answered with a new interaction.
				err := bot.Respond(e.Str("interaction_id"), map[string]any{"type": "dialog", "form": map[string]any{
					"title": "New poll",
					"fields": []any{
						map[string]any{"id": "question", "type": "text", "label": "Question", "required": true},
						map[string]any{"id": "options", "type": "longtext", "label": "Options, one per line", "required": true},
					},
					"buttons": []any{map[string]any{"id": "post", "label": "Post poll", "style": "primary"}},
				}})
				if err != nil {
					log.Printf("opening the dialog: %v", err)
				}
				return
			}
			answer(bot, e.Str("interaction_id"), parts[0], parts[1:])

		case "form.answered":
			if e["dialog"] != true {
				return
			}
			answers := e.Obj("answers")
			answer(bot, e.Str("interaction_id"), strings.TrimSpace(answers.Str("question")), split(answers.Str("options"), "\n"))
		}
	})
}
