// Package botkit is the little that the example bots share: calling the
// API with a bot token, and holding the live connection open. It uses only
// the standard library, so an example can be read from top to bottom, and
// it is not a supported client library: copy what is useful.
//
// A bot is a client like any other (docs/BOTS.md section 4). It sends
// `Authorization: Bearer churr_bot_…` on every request and when opening the
// WebSocket, tells the server the last seq it has for each conversation so
// it is sent what it missed, and reconnects with a delay that doubles from
// one second to sixty.
package botkit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// Bot is one bot's connection to one server.
type Bot struct {
	// URL is the server's address, such as http://localhost:8080.
	URL string
	// Token is the bot's token, which starts churr_bot_.
	Token string
	HTTP  *http.Client

	mu sync.Mutex
	// positions is the last seq applied in each conversation, which is
	// what catching up after a disconnection needs.
	positions map[string]int64
}

// New makes a bot for a server and a token.
func New(url, token string) *Bot {
	return &Bot{URL: strings.TrimRight(url, "/"), Token: token, HTTP: &http.Client{Timeout: 30 * time.Second}, positions: map[string]int64{}}
}

// FromEnv makes a bot from CHURR_URL and CHURR_BOT_TOKEN, and stops the
// program with a sentence if either is missing.
func FromEnv() *Bot {
	url, token := os.Getenv("CHURR_URL"), os.Getenv("CHURR_BOT_TOKEN")
	if url == "" || !strings.HasPrefix(token, "churr_bot_") {
		log.Fatal("set CHURR_URL to the server's address and CHURR_BOT_TOKEN to the bot's token (churr_bot_…)")
	}
	return New(url, token)
}

// Error is a refusal from the server, in its one error shape.
type Error struct {
	Status            int    `json:"-"`
	Code              string `json:"code"`
	Message           string `json:"message"`
	RetryAfterSeconds int    `json:"retry_after_seconds"`
}

func (e *Error) Error() string { return fmt.Sprintf("%d %s: %s", e.Status, e.Code, e.Message) }

// do sends one request and decodes the answer into out, if out is not nil.
func (b *Bot) do(req *http.Request, out any) error {
	req.Header.Set("Authorization", "Bearer "+b.Token)
	resp, err := b.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		var e struct {
			Error Error `json:"error"`
		}
		json.Unmarshal(raw, &e)
		e.Error.Status = resp.StatusCode
		return &e.Error
	}
	if out == nil || len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, out)
}

// Call makes one API call with a JSON body (nil for none) and decodes the
// JSON answer into out (nil to ignore it). When the server says the bot is
// going too fast, it waits as long as it is told and tries once more.
func (b *Bot) Call(method, path string, body, out any) error {
	for attempt := 0; ; attempt++ {
		var rd io.Reader
		if body != nil {
			raw, err := json.Marshal(body)
			if err != nil {
				return err
			}
			rd = bytes.NewReader(raw)
		}
		req, err := http.NewRequest(method, b.URL+path, rd)
		if err != nil {
			return err
		}
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		err = b.do(req, out)
		var e *Error
		if errors.As(err, &e) && e.Code == "rate_limited" && attempt == 0 && e.RetryAfterSeconds <= 60 {
			time.Sleep(time.Duration(e.RetryAfterSeconds) * time.Second)
			continue
		}
		return err
	}
}

// Respond answers an interaction: a command, a button press, or an answer
// to one of the bot's forms or dialogs. response is the body of the
// respond operation, such as {"type": "private", "text": "…"}.
func (b *Bot) Respond(interactionID string, response map[string]any) error {
	return b.Call("POST", "/api/v1/interactions/"+interactionID+"/responses", response, nil)
}

// Post posts a message in a conversation and returns its identifier.
func (b *Bot) Post(conversationID string, message map[string]any) (string, error) {
	var out struct {
		Message struct {
			ID string `json:"id"`
		} `json:"message"`
	}
	err := b.Call("POST", "/api/v1/conversations/"+conversationID+"/messages", message, &out)
	return out.Message.ID, err
}

// SetCommands sets the bot's whole list of commands. A bot sends the same
// list every time it starts.
func (b *Bot) SetCommands(commands ...map[string]string) error {
	if commands == nil {
		commands = []map[string]string{}
	}
	return b.Call("PUT", "/api/v1/commands", map[string]any{"commands": commands}, nil)
}

// Upload uploads a file to a conversation and returns its identifier, to
// attach to a message with file_ids.
func (b *Bot) Upload(conversationID, name string, content []byte) (string, error) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	part, err := mw.CreateFormFile("file", name)
	if err != nil {
		return "", err
	}
	part.Write(content)
	mw.Close()
	req, err := http.NewRequest("POST", b.URL+"/api/v1/conversations/"+conversationID+"/files", &buf)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	var out struct {
		File struct {
			ID string `json:"id"`
		} `json:"file"`
	}
	err = b.do(req, &out)
	return out.File.ID, err
}

// Event is one frame from the live connection: a flat JSON object with a
// `type`.
type Event map[string]any

// Str returns a string field, or "".
func (e Event) Str(key string) string {
	s, _ := e[key].(string)
	return s
}

// Obj returns an object field, or an empty one.
func (e Event) Obj(key string) Event {
	m, _ := e[key].(map[string]any)
	return m
}

// Run holds the live connection open until ctx ends, calling handle for
// every event. After a disconnection it reconnects, waiting one second and
// then twice as long each time up to a minute, and resumes from the last
// seq it saw in each conversation, so events of a conversation are never
// missed. Events addressed to the bot alone (command.invoked, form.answered)
// are not stored: one sent while the bot was away is gone, and the person
// was told at the time that the bot was not connected.
func (b *Bot) Run(ctx context.Context, handle func(Event)) error {
	delay := time.Second
	for {
		connected, err := b.session(ctx, handle)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if connected {
			delay = time.Second
		}
		log.Printf("disconnected (%v); reconnecting in %v", err, delay)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
		if delay *= 2; delay > time.Minute {
			delay = time.Minute
		}
	}
}

// session runs one connection from hello to its end. connected says
// whether it got as far as `ready`.
func (b *Bot) session(ctx context.Context, handle func(Event)) (connected bool, err error) {
	ws, err := dial(ctx, b.URL+"/api/v1/events", b.Token)
	if err != nil {
		return false, err
	}
	defer ws.close()
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		select {
		case <-ctx.Done():
			ws.close()
		case <-stop:
		}
	}()
	for {
		raw, err := ws.read()
		if err != nil {
			return connected, err
		}
		var e Event
		if json.Unmarshal(raw, &e) != nil {
			continue
		}
		switch e.Str("type") {
		case "hello":
			b.mu.Lock()
			positions := make(map[string]int64, len(b.positions))
			for k, v := range b.positions {
				positions[k] = v
			}
			b.mu.Unlock()
			if err := ws.writeJSON(map[string]any{"type": "resume", "positions": positions}); err != nil {
				return connected, err
			}
			// The server closes a connection that says nothing for
			// three heartbeats.
			every := 30 * time.Second
			if n, ok := e["heartbeat_seconds"].(float64); ok && n >= 1 {
				every = time.Duration(n) * time.Second
			}
			go func() {
				t := time.NewTicker(every)
				defer t.Stop()
				for {
					select {
					case <-stop:
						return
					case <-t.C:
						if ws.writeJSON(map[string]any{"type": "ping"}) != nil {
							return
						}
					}
				}
			}()
			continue
		case "ready":
			connected = true
			// Conversations joined while away start from where they
			// are now.
			if ids, ok := e["new_conversation_ids"].([]any); ok {
				for _, id := range ids {
					if s, ok := id.(string); ok {
						b.follow(s, 0)
					}
				}
			}
		case "pong":
			continue
		}
		if conv, seq := e.Str("conversation_id"), e["seq"]; conv != "" && seq != nil {
			if n, ok := seq.(float64); ok {
				b.follow(conv, int64(n))
			}
		}
		handle(e)
	}
}

// follow records the last seq applied in a conversation.
func (b *Bot) follow(conversation string, seq int64) {
	b.mu.Lock()
	if seq >= b.positions[conversation] {
		b.positions[conversation] = seq
	}
	b.mu.Unlock()
}
