// gif-bot offers `/gif <what to search for>`: it finds a GIF, uploads it to
// the conversation and posts it for everyone, with buttons "Another" and
// "Remove" that only the person who asked can use (docs/BOTS.md section
// 16).
//
// THE SEARCH IS A STUB. A real gif-bot would call an outside GIF service
// here; the server never does, and neither does this example. search()
// below makes small coloured GIFs out of the words searched for, so the bot
// runs with nothing but a Churr server. Replace search() to make it real.
//
//	CHURR_URL=http://localhost:8080 CHURR_BOT_TOKEN=churr_bot_… go run ./examples/gif-bot
package main

import (
	"bytes"
	"context"
	"errors"
	"hash/fnv"
	"image"
	"image/color"
	"image/gif"
	"log"
	"os"
	"os/signal"
	"strings"
	"sync"

	"github.com/tikaydaes/churr/examples/botkit"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := run(ctx, botkit.FromEnv()); err != nil && ctx.Err() == nil {
		log.Fatal(err)
	}
}

// search STANDS IN FOR AN OUTSIDE GIF SERVICE. It returns the n-th result
// for a query, or nil when there is none. This stub "finds" three results
// for anything except the word "nothing", each a two-frame GIF whose
// colours come from the query.
func search(query string, n int) []byte {
	if query == "" || strings.EqualFold(query, "nothing") || n >= 3 {
		return nil
	}
	h := fnv.New32a()
	h.Write([]byte(strings.ToLower(query)))
	seed := h.Sum32() + uint32(n)*0x9e3779b9
	pal := color.Palette{
		color.RGBA{uint8(seed), uint8(seed >> 8), uint8(seed >> 16), 255},
		color.RGBA{uint8(seed >> 16), uint8(seed), uint8(seed >> 8), 255},
	}
	out := &gif.GIF{}
	for frame := 0; frame < 2; frame++ {
		img := image.NewPaletted(image.Rect(0, 0, 64, 64), pal)
		for y := 0; y < 64; y++ {
			for x := 0; x < 64; x++ {
				if (x/8+y/8+frame)%2 == 0 {
					img.SetColorIndex(x, y, 1)
				}
			}
		}
		out.Image = append(out.Image, img)
		out.Delay = append(out.Delay, 40)
	}
	var buf bytes.Buffer
	gif.EncodeAll(&buf, out)
	return buf.Bytes()
}

// shown is what the bot remembers about a GIF it posted: who asked, what
// for, and which result is showing. It is kept in memory only, so the
// buttons of GIFs posted before a restart answer "ask again".
type shown struct {
	asker, query, conversation string
	n                          int
}

// buttons is a row of buttons: a form with no fields and the `bot` action,
// whose presses come to this bot and are stored nowhere.
var buttons = map[string]any{"fields": []any{}, "action": map[string]any{"type": "bot"}, "buttons": []any{
	map[string]any{"id": "another", "label": "Another"}, map[string]any{"id": "remove", "label": "Remove", "style": "danger"}}}

func run(ctx context.Context, bot *botkit.Bot) error {
	if err := bot.SetCommands(map[string]string{"name": "gif", "description": "Post a GIF", "hint": "<what to search for>"}); err != nil {
		return err
	}
	var mu sync.Mutex
	posted := map[string]*shown{} // message id -> what it shows
	private := func(interaction, text string) {
		if err := bot.Respond(interaction, map[string]any{"type": "private", "text": text}); err != nil {
			log.Printf("responding: %v", err)
		}
	}
	log.Print("gif-bot is running (with a stubbed search)")
	return bot.Run(ctx, func(e botkit.Event) {
		interaction := e.Str("interaction_id")
		switch e.Str("type") {
		case "command.invoked":
			query := e.Str("text")
			data := search(query, 0)
			if data == nil {
				// Found nothing: tell the asker alone, so the channel
				// is not bothered.
				private(interaction, "No GIF found for that.")
				return
			}
			file, err := bot.Upload(e.Str("conversation_id"), "gif.gif", data)
			if err != nil {
				private(interaction, "The GIF could not be uploaded.")
				return
			}
			// The answer for everyone. The server marks it with who
			// asked, so everyone can see who made the bot speak.
			var out struct {
				Message struct {
					ID string `json:"id"`
				} `json:"message"`
			}
			message := map[string]any{"file_ids": []string{file}, "form": buttons}
			err = bot.Call("POST", "/api/v1/interactions/"+interaction+"/responses", map[string]any{"type": "message", "message": message}, &out)
			var refused *botkit.Error
			if errors.As(err, &refused) && refused.Status < 500 && refused.Code != "not_found" {
				// In a channel shared with other servers `bot` forms
				// are refused: post the file without the buttons.
				delete(message, "form")
				err = bot.Call("POST", "/api/v1/interactions/"+interaction+"/responses", map[string]any{"type": "message", "message": message}, &out)
			}
			if err != nil {
				log.Printf("posting: %v", err)
				return
			}
			mu.Lock()
			posted[out.Message.ID] = &shown{asker: e.Str("account_id"), query: query, conversation: e.Str("conversation_id")}
			mu.Unlock()

		case "form.answered":
			id := e.Str("message_id")
			mu.Lock()
			s := posted[id]
			mu.Unlock()
			switch {
			case s == nil:
				private(interaction, "That GIF is from before I restarted. Ask again with /gif.")
				return
			case s.asker != e.Str("account_id"):
				private(interaction, "Only the person who asked can change this.")
				return
			}
			switch e.Str("button_id") {
			case "another":
				data := search(s.query, s.n+1)
				if data == nil {
					private(interaction, "That was the last one.")
					return
				}
				file, err := bot.Upload(s.conversation, "gif.gif", data)
				if err == nil {
					// Edit the message to carry the next result. The
					// form is sent again so the buttons stay.
					err = bot.Call("PATCH", "/api/v1/messages/"+id, map[string]any{"file_ids": []string{file}, "form": buttons}, nil)
				}
				if err != nil {
					private(interaction, "The next GIF could not be posted.")
					return
				}
				mu.Lock()
				s.n++
				mu.Unlock()
			case "remove":
				if err := bot.Call("DELETE", "/api/v1/messages/"+id, nil, nil); err != nil {
					log.Printf("removing: %v", err)
				}
				mu.Lock()
				delete(posted, id)
				mu.Unlock()
			}
			// Nothing to show: tell the asker's client to stop waiting.
			if err := bot.Respond(interaction, map[string]any{"type": "none"}); err != nil {
				log.Printf("responding: %v", err)
			}
		}
	})
}
