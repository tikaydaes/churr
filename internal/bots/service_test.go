package bots

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"regexp"
	"sync"
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/secret"
	"github.com/tikaydaes/churr/internal/store"
)

// The parts of the service that need no database: the life of an
// interaction, what happens to images that cannot be had, signatures and
// limits. Everything else is tested through the API (internal/api).

// fakeHub records what was sent to whom.
type fakeHub struct {
	mu     sync.Mutex
	online map[model.AccountID]bool
	sent   map[model.AccountID][]model.Event
}

func (h *fakeHub) Send(account model.AccountID, v any) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.sent == nil {
		h.sent = map[model.AccountID][]model.Event{}
	}
	h.sent[account] = append(h.sent[account], v.(model.Event))
}

func (h *fakeHub) Presence(account model.AccountID) model.Presence {
	if h.online[account] {
		return model.PresenceOnline
	}
	return model.PresenceOffline
}

func (h *fakeHub) CloseSession(model.AccountID, model.SessionID, int, string) {}
func (h *fakeHub) CloseAccount(model.AccountID, int, string)                  {}

func code(err error) model.Code { return model.CodeOf(err) }

func TestInteractionLife(t *testing.T) {
	ctx := context.Background()
	bot, other, asker := model.Account{ID: "bot"}, model.Account{ID: "other"}, model.AccountID("asker")
	hub := &fakeHub{online: map[model.AccountID]bool{"bot": true}}
	s := New(Deps{Hub: hub})
	none := Response{Type: ResponseNone}

	it := s.open(interaction{bot: bot.ID, asker: asker, conv: "conv", parent: "parent", command: "deploy"})
	if !model.ValidID(it.id) {
		t.Fatalf("an interaction's identifier is a UUID: %q", it.id)
	}
	// Only the bot it was sent to can respond.
	if _, err := s.Respond(ctx, other, it.id, none); code(err) != model.CodeNotFound {
		t.Fatalf("another bot responding: %v", err)
	}
	if _, err := s.Respond(ctx, bot, "not-an-id", none); code(err) != model.CodeNotFound {
		t.Fatalf("an identifier that is not one: %v", err)
	}
	if _, err := s.Respond(ctx, bot, it.id, Response{Type: "shout"}); code(err) != model.CodeInvalidValue {
		t.Fatalf("an unknown type: %v", err)
	}
	// Five responses, each told to the asker and to nobody else.
	for i := 0; i < maxResponses; i++ {
		if _, err := s.Respond(ctx, bot, it.id, none); err != nil {
			t.Fatalf("response %d: %v", i+1, err)
		}
	}
	if _, err := s.Respond(ctx, bot, it.id, none); code(err) != model.CodeNotFound {
		t.Fatalf("a sixth response: %v", err)
	}
	if got := hub.sent[asker]; len(got) != maxResponses || len(hub.sent) != 1 {
		t.Fatalf("the asker was told %d times; sent to %d accounts", len(got), len(hub.sent))
	}
	e := hub.sent[asker][0]
	if e.Type != eventInteractionResponded || e.ConversationID != "conv" || e.Seq != 0 || e.Payload["interaction_id"] != it.id ||
		e.Payload["bot_id"] != bot.ID || e.Payload["parent_id"] != model.MessageID("parent") {
		t.Fatalf("interaction.responded: %+v", e)
	}

	// A response that is refused does not use one up.
	it = s.open(interaction{bot: bot.ID, asker: asker, conv: "conv"})
	for i := 0; i < 10; i++ {
		bad := Response{Type: ResponsePrivate, Cards: []model.Card{{Footer: "nothing else"}}}
		if _, err := s.Respond(ctx, bot, it.id, bad); code(err) != model.CodeInvalidValue {
			t.Fatalf("a private response with an empty card: %v", err)
		}
	}
	if s.interactions[it.id].responses != 0 {
		t.Fatalf("refused responses were counted: %d", s.interactions[it.id].responses)
	}
	// Without the forms package there are no dialogs, and the one dialog
	// an interaction may open is still unused afterwards.
	if _, err := s.Respond(ctx, bot, it.id, Response{Type: ResponseDialog, Form: &DialogForm{}}); code(err) != model.CodeUnavailable {
		t.Fatalf("a dialog with no forms package: %v", err)
	}
	if s.interactions[it.id].dialogShown || s.interactions[it.id].responses != 0 {
		t.Fatal("a refused dialog used up the interaction's dialog")
	}

	// An interaction lasts 15 minutes.
	if got := time.Until(it.until); got < 14*time.Minute || got > interactionLife {
		t.Fatalf("an interaction lasts %v", got)
	}
	s.interactions[it.id].until = time.Now().Add(-time.Second)
	if _, err := s.Respond(ctx, bot, it.id, none); code(err) != model.CodeNotFound {
		t.Fatalf("a response after 15 minutes: %v", err)
	}
	// Ended interactions are forgotten as new ones arrive.
	s.swept = time.Now().Add(-2 * time.Minute)
	s.dialogs["old"] = &dialog{until: time.Now().Add(-time.Second)}
	s.open(interaction{bot: bot.ID, asker: asker, conv: "conv"})
	if _, kept := s.interactions[it.id]; kept || len(s.dialogs) != 0 {
		t.Fatal("an ended interaction or dialog was kept")
	}
}

func TestFormAnswered(t *testing.T) {
	ctx := context.Background()
	hub := &fakeHub{online: map[model.AccountID]bool{}}
	s := New(Deps{Hub: hub})
	sub := model.FormSubmission{BotID: "bot", AccountID: "asker", ConversationID: "conv", MessageID: "msg", ParentID: "parent", FormID: "form", ButtonID: "go"}

	// A bot that is not connected: unavailable, and nothing is queued.
	if _, err := s.FormAnswered(ctx, sub); code(err) != model.CodeUnavailable {
		t.Fatalf("an answer for a bot that is away: %v", err)
	}
	if len(hub.sent) != 0 || len(s.interactions) != 0 {
		t.Fatal("something was kept for a bot that is not connected")
	}
	hub.online["bot"] = true
	id, err := s.FormAnswered(ctx, sub)
	if err != nil {
		t.Fatal(err)
	}
	e := hub.sent["bot"][0]
	if e.Type != eventFormAnswered || e.Seq != 0 || e.ConversationID != "conv" || e.Payload["interaction_id"] != id ||
		e.Payload["message_id"] != model.MessageID("msg") || e.Payload["button_id"] != "go" || e.Payload["answers"] == nil {
		t.Fatalf("form.answered: %+v", e)
	}
	if _, has := e.Payload["dialog"]; has {
		t.Fatal("a form on a message is not a dialog")
	}
	// The interaction it made is the bot's to respond to, in the thread
	// the form is in, and not as a command.
	it := s.interactions[id]
	if it == nil || it.bot != "bot" || it.asker != "asker" || it.parent != "parent" || it.command != "" {
		t.Fatalf("the interaction of a form answer: %+v", it)
	}
	// A dialog's answer has no message and says it is a dialog; a
	// submission with no button has no button_id.
	sub.MessageID, sub.ButtonID, sub.Dialog = "", "", true
	if _, err := s.FormAnswered(ctx, sub); err != nil {
		t.Fatal(err)
	}
	e = hub.sent["bot"][1]
	_, hasMessage := e.Payload["message_id"]
	_, hasButton := e.Payload["button_id"]
	if hasMessage || hasButton || e.Payload["dialog"] != true {
		t.Fatalf("form.answered for a dialog: %+v", e.Payload)
	}
}

func TestDroppedImages(t *testing.T) {
	cards := []model.Card{
		{Title: "Keeps its title", Footer: "ci.example"},
		{}, // only an image
		{Text: "Has text"},
		{Author: &model.CardAuthor{Name: "CI"}}, // only an image and an icon
	}
	slots := []slot{
		{card: 0, url: "https://x.example/0.png"},
		{card: 1, url: "https://x.example/1.png"},
		{card: 2, author: true, url: "https://x.example/icon.png"},
		{card: 3, part: "file1"},
		{card: 3, author: true, part: "file2"},
		{card: -1, url: "https://x.example/via.png"},
	}
	// With nothing to fetch with, the images given by address go.
	var kept []slot
	for _, sl := range slots {
		if sl.url != "" {
			dropImage(cards, sl)
			continue
		}
		kept = append(kept, sl)
	}
	if cards[0].Footer != "ci.example · https://x.example/0.png" || cards[1].Footer != "https://x.example/1.png" {
		t.Fatalf("a card that lost its image gets the address in its footer: %+v", cards[:2])
	}
	if cards[2].Footer != "" {
		t.Fatalf("a dropped icon leaves nothing behind: %+v", cards[2])
	}
	out, kept := dropEmptyCards(cards, kept, map[int]bool{3: true})
	if len(out) != 3 || out[0].Title != "Keeps its title" || out[1].Text != "Has text" || out[2].Author == nil {
		t.Fatalf("a card left with nothing is removed: %+v", out)
	}
	// The slots still point at the right cards.
	if len(kept) != 2 || kept[0].card != 2 || kept[1].card != 2 {
		t.Fatalf("slots after cards were removed: %+v", kept)
	}
	via := model.Via{}
	setImage(out, &via, kept[0], "image")
	setImage(out, &via, kept[1], "icon")
	setImage(out, &via, slot{card: -1}, "via")
	if out[2].ImageFileID != "image" || out[2].Author.IconFileID != "icon" || via.IconFileID != "via" {
		t.Fatalf("stored images in their places: %+v %+v", out[2], via)
	}
}

func TestSignature(t *testing.T) {
	keys, err := secret.Open(filepath.Join(t.TempDir(), "keys"))
	if err != nil {
		t.Fatal(err)
	}
	s := New(Deps{Hub: &fakeHub{}, Keys: keys})
	const signing = "c2VjcmV0LXNlY3JldC1zZWNyZXQtc2VjcmV0LXNlY3JldA"
	sealed, err := keys.Seal([]byte(signing), "webhooks", "signing_secret", "hook-1")
	if err != nil {
		t.Fatal(err)
	}
	h := Hook{w: store.WebhookSecret{Webhook: model.Webhook{ID: "hook-1"}, SigningSecret: sealed}}
	body := []byte(`{"zen": "Keep it logically awesome."}`)
	// The key is the bytes of the secret's base64url string, which is
	// what was typed into GitHub; not the bytes it decodes to.
	mac := hmac.New(sha256.New, []byte(signing))
	mac.Write(body)
	good := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	if err := s.VerifySignature(h, good, body); err != nil {
		t.Fatalf("a good signature: %v", err)
	}
	if err := s.VerifySignature(h, "  "+good+" ", body); err != nil {
		t.Fatalf("space around the header value: %v", err)
	}
	for name, header := range map[string]string{
		"missing":      "",
		"sha1":         "sha1=" + good[7:47],
		"no prefix":    good[7:],
		"not hex":      "sha256=" + "zz" + good[9:],
		"too short":    good[:len(good)-2],
		"one bit off":  good[:len(good)-1] + string("0123456789abcdef"[(int(good[len(good)-1])+1)%16]),
		"another body": "sha256=" + hex.EncodeToString(hmac.New(sha256.New, []byte(signing)).Sum(nil)),
	} {
		if err := s.VerifySignature(h, header, body); code(err) != model.CodeUnauthenticated {
			t.Errorf("%s: %v, want unauthenticated", name, err)
		}
	}
	if err := s.VerifySignature(h, good, append(body, '\n')); code(err) != model.CodeUnauthenticated {
		t.Errorf("a body one byte longer: %v", err)
	}
	// A webhook with no signing secret verifies nothing.
	if err := s.VerifySignature(Hook{w: store.WebhookSecret{Webhook: model.Webhook{ID: "hook-1"}}}, good, body); code(err) != model.CodeUnauthenticated {
		t.Errorf("no signing secret: %v", err)
	}
	// A sealed secret copied onto another webhook's row does not open.
	moved := Hook{w: store.WebhookSecret{Webhook: model.Webhook{ID: "hook-2"}, SigningSecret: sealed}}
	if err := s.VerifySignature(moved, good, body); err == nil {
		t.Error("a signing secret moved to another row verified")
	}
}

func TestLimits(t *testing.T) {
	l := DefaultLimits()
	if l.RequestsPerMinute != 600 || l.MessagesPerMinuteInOne != 60 || l.MessagesPerMinute != 300 || l.UploadsPerMinute != 30 ||
		l.ReactionsPerMinute != 120 || l.CommandSetsPerHour != 10 || l.Connections != 5 || l.WebhookPostsPerMinute != 60 ||
		l.ImageFetchesPerMinute != 30 || l.IntegrationRequestsPerMinute != 600 || maxResponses != 5 {
		t.Fatalf("the defaults are not those of BOTS.md section 8: %+v", l)
	}
	l.MessagesPerMinuteInOne, l.MessagesPerMinute, l.UploadsPerMinute, l.RequestsPerMinute, l.IntegrationRequestsPerMinute = 2, 3, 1, 2, 1
	s := New(Deps{Hub: &fakeHub{}, Limits: &l})

	// Per conversation, and in all.
	for i := 0; i < 2; i++ {
		if err := s.AllowMessage("bot", "one"); err != nil {
			t.Fatalf("message %d in one conversation: %v", i+1, err)
		}
	}
	err := s.AllowMessage("bot", "one")
	if e, ok := model.AsError(err); !ok || e.Code != model.CodeRateLimited || e.RetryAfterSeconds < 1 {
		t.Fatalf("a third message in one conversation: %v", err)
	}
	if err := s.AllowMessage("bot", "two"); err != nil {
		t.Fatalf("a message in another conversation: %v", err)
	}
	if err := s.AllowMessage("bot", "three"); code(err) != model.CodeRateLimited {
		t.Fatalf("a fourth message in all: %v", err)
	}
	// Another bot has its own.
	if err := s.AllowMessage("other", "one"); err != nil {
		t.Fatalf("another bot's message: %v", err)
	}

	if err := s.AllowUpload("bot"); err != nil {
		t.Fatal(err)
	}
	if err := s.AllowUpload("bot"); code(err) != model.CodeRateLimited {
		t.Fatalf("a second upload: %v", err)
	}
	if !s.TakeRequest("bot").OK || !s.TakeRequest("bot").OK {
		t.Fatal("the first two requests")
	}
	if r := s.TakeRequest("bot"); r.OK || r.Limit != 2 || r.RetryAfterSeconds() < 1 {
		t.Fatalf("a third request: %+v", r)
	}
	if !s.TakeIntegration("int-1").OK || s.TakeIntegration("int-1").OK || !s.TakeIntegration("int-2").OK {
		t.Fatal("the limit of an integration is per integration")
	}
	if err := s.AllowConnection(4); err != nil {
		t.Fatalf("a fifth connection: %v", err)
	}
	if err := s.AllowConnection(5); code(err) != model.CodeRateLimited {
		t.Fatalf("a sixth connection: %v", err)
	}
}

func TestNames(t *testing.T) {
	form := regexp.MustCompile(`^hook-[a-z0-9]{8}$`)
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		name := hookUsername()
		if !form.MatchString(name) || !model.ValidUsername(name) {
			t.Fatalf("a webhook account's username: %q", name)
		}
		seen[name] = true
	}
	if len(seen) < 200 {
		t.Fatalf("generated usernames repeat: %d of 200 distinct", len(seen))
	}
	for in, want := range map[string]string{
		"build.log":           "build.log",
		"../../etc/passwd":    "passwd",
		`C:\Users\sam\a.png`:  "a.png",
		"":                    "file",
		"..":                  "file",
		"  spaced name.txt  ": "spaced name.txt",
		"new\nline\x00.txt":   "newline.txt",
	} {
		if got := cleanFileName(in); got != want {
			t.Errorf("file name %q becomes %q, want %q", in, got, want)
		}
	}
}

func TestWebhookForm(t *testing.T) {
	for action, want := range map[string]model.Code{
		model.FormTally: "", model.FormCollect: "", model.FormThread: "",
		model.FormBot: model.CodeInvalidValue, model.FormChannel: model.CodeInvalidValue, model.FormGroup: model.CodeInvalidValue,
		"": model.CodeInvalidValue, "other": model.CodeInvalidValue,
	} {
		if got := code(checkWebhookForm(model.FormInput{Action: model.FormAction{Type: action}})); got != want {
			t.Errorf("a webhook's form with action %q: %q, want %q", action, got, want)
		}
	}
	// answerer_group_id is refused: a webhook is never a manager.
	limited := model.FormInput{Action: model.FormAction{Type: model.FormTally}, AnswererGroupID: "group"}
	if got := code(checkWebhookForm(limited)); got != model.CodeInvalidValue {
		t.Errorf("a webhook's form limited to a group: %q", got)
	}
	// A webhook's poll may be anonymous, as anyone's.
	if err := checkWebhookForm(model.FormInput{Action: model.FormAction{Type: model.FormTally}, Anonymous: true}); err != nil {
		t.Errorf("an anonymous poll from a webhook: %v", err)
	}
}
