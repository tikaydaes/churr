package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/tikaydaes/churr/internal/bots"
	"github.com/tikaydaes/churr/internal/chat"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/ratelimit"
)

// Bots, their commands and interactions, webhooks, and the four webhook
// addresses (docs/BOTS.md).

func init() {
	register(func(a *API) {
		a.handle("listBots", a.listBots)
		a.handle("createBot", a.createBot)
		a.handle("getBot", a.getBot)
		a.handle("updateBot", a.updateBot)
		a.handle("setBotAvatar", a.setBotAvatar)
		a.handle("deleteBot", a.deleteBot)
		a.handle("listBotTokens", a.listBotTokens)
		a.handle("createBotToken", a.createBotToken)
		a.handle("revokeBotToken", a.revokeBotToken)
		a.handle("listWebhooks", a.listWebhooks)

		a.handle("setCommands", a.setCommands)
		a.handle("respondToInteraction", a.respondToInteraction)
		a.handle("submitDialog", a.submitDialog)
		a.handle("dismissDialog", a.dismissDialog)
		a.handle("requestWebhook", a.requestWebhook)
		a.handle("listChannelWebhooks", a.listChannelWebhooks)
		a.handle("approveWebhook", a.approveWebhook)
		a.handle("declineWebhook", a.declineWebhook)
		a.handle("revokeWebhook", a.revokeWebhook)
		a.handle("collectWebhookAddress", a.collectWebhookAddress)
		a.handle("resetWebhookAddress", a.resetWebhookAddress)

		a.handle("postWebhook", a.postWebhook)
		a.handle("postSlackWebhook", a.postSlackWebhook)
		a.handle("postDiscordWebhook", a.postDiscordWebhook)
		a.handle("postGithubWebhook", a.postGithubWebhook)
	})
}

// errNoBots is the answer while the bots service has not been handed over.
var errNoBots = model.ErrUnavailable.With("Bots are not available on this server.")

// takeRequest counts one request against its account. A bot has its own
// limit on requests of any kind (docs/BOTS.md section 8); everyone else has
// the server's.
func (a *API) takeRequest(acc model.Account) ratelimit.Result {
	if acc.Role == model.RoleBot && a.Bots != nil {
		return a.Bots.TakeRequest(acc.ID)
	}
	return a.limiter.Take(string(acc.ID))
}

// botLimits applies the limits a bot has beyond its requests: on messages
// posted or edited, files uploaded, reactions, and live connections held at
// once. It runs before the handler of whichever operation that is, so the
// handlers of those operations need to know nothing about bots.
func (a *API) botLimits(r *Request) error {
	acc := r.Who.Account
	if acc.Role != model.RoleBot || a.Bots == nil {
		return nil
	}
	switch r.Op.ID {
	case "postMessage":
		return a.Bots.AllowMessage(acc.ID, model.ConversationID(r.R.PathValue("id")))
	case "editMessage":
		// Which conversation an edit is in takes a look at the message;
		// one that does not exist is the handler's to refuse.
		var conv model.ConversationID
		if id := r.R.PathValue("id"); model.ValidID(id) {
			if m, err := a.Store.Messages().Get(r.Ctx(), model.MessageID(id)); err == nil {
				conv = m.ConversationID
			}
		}
		return a.Bots.AllowMessage(acc.ID, conv)
	case "uploadFile":
		return a.Bots.AllowUpload(acc.ID)
	case "addReaction", "removeReaction":
		return a.Bots.AllowReaction(acc.ID)
	case "openEvents":
		return a.Bots.AllowConnection(a.Live.Connections(acc.ID))
	}
	return nil
}

// tokenView is a token as listed (schema Token). The secret is not part of
// it.
func tokenView(s model.Session) map[string]any {
	out := map[string]any{"id": s.ID, "label": s.Label, "created_at": s.CreatedAt}
	if !s.LastSeenAt.IsZero() {
		out["last_seen_at"] = s.LastSeenAt
	}
	if !s.ExpiresAt.IsZero() {
		out["expires_at"] = s.ExpiresAt
	}
	return out
}

// ----------------------------------------------------------- admin: bots

func (a *API) listBots(r *Request) error {
	if a.Bots == nil {
		return errNoBots
	}
	webhooks, err := r.queryBool("webhooks")
	if err != nil {
		return err
	}
	page, err := r.Page()
	if err != nil {
		return err
	}
	all, next, err := a.Bots.List(r.Ctx(), r.Actor(), webhooks, page)
	if err != nil {
		return err
	}
	return r.JSON(list("bots", orEmpty(all), next))
}

func (a *API) createBot(r *Request) error {
	if a.Bots == nil {
		return errNoBots
	}
	var body struct {
		Username    *string `json:"username"`
		DisplayName *string `json:"display_name"`
		Description string  `json:"description"`
	}
	if err := r.Decode(&body); err != nil {
		return err
	}
	if body.Username == nil || body.DisplayName == nil {
		return model.BadRequest("A bot needs a username and a display_name.")
	}
	bot, token, secret, err := a.Bots.Create(r.Ctx(), r.Actor(), r.Who.Session,
		bots.NewBot{Username: *body.Username, DisplayName: *body.DisplayName, Description: body.Description})
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"bot": bot, "token": tokenView(token), "secret": secret})
}

func (a *API) getBot(r *Request) error {
	if a.Bots == nil {
		return errNoBots
	}
	id, err := PathID[model.AccountID](r, "id")
	if err != nil {
		return err
	}
	bot, err := a.Bots.Get(r.Ctx(), r.Actor(), id)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"bot": bot})
}

func (a *API) updateBot(r *Request) error {
	if a.Bots == nil {
		return errNoBots
	}
	id, err := PathID[model.AccountID](r, "id")
	if err != nil {
		return err
	}
	var body struct {
		Username    *string `json:"username"`
		DisplayName *string `json:"display_name"`
		Description *string `json:"description"`
	}
	if err := r.Decode(&body); err != nil {
		return err
	}
	bot, err := a.Bots.Update(r.Ctx(), r.Actor(), id, bots.BotChange{Username: body.Username, DisplayName: body.DisplayName, Description: body.Description})
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"bot": bot})
}

func (a *API) setBotAvatar(r *Request) error {
	if a.Bots == nil {
		return errNoBots
	}
	id, err := PathID[model.AccountID](r, "id")
	if err != nil {
		return err
	}
	mr, err := r.R.MultipartReader()
	if err != nil {
		return model.BadRequest("Send the image as multipart/form-data in a part named `file`.")
	}
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			return model.Invalid("file", "The upload has no part named `file`.")
		}
		if err != nil {
			return bodyError(err, "An avatar is at most 1 MB.")
		}
		if part.FormName() != "file" {
			continue
		}
		bot, err := a.Bots.SetAvatar(r.Ctx(), r.Actor(), id, part)
		if err != nil {
			return err
		}
		return r.JSON(map[string]any{"bot": bot})
	}
}

func (a *API) deleteBot(r *Request) error {
	if a.Bots == nil {
		return errNoBots
	}
	id, err := PathID[model.AccountID](r, "id")
	if err != nil {
		return err
	}
	if err := a.Bots.Remove(r.Ctx(), r.Actor(), id); err != nil {
		return err
	}
	return r.NoContent()
}

func (a *API) listBotTokens(r *Request) error {
	if a.Bots == nil {
		return errNoBots
	}
	id, err := PathID[model.AccountID](r, "id")
	if err != nil {
		return err
	}
	tokens, err := a.Bots.Tokens(r.Ctx(), r.Actor(), id)
	if err != nil {
		return err
	}
	out := make([]map[string]any, 0, len(tokens))
	for _, t := range tokens {
		out = append(out, tokenView(t))
	}
	return r.JSON(map[string]any{"tokens": out})
}

func (a *API) createBotToken(r *Request) error {
	if a.Bots == nil {
		return errNoBots
	}
	id, err := PathID[model.AccountID](r, "id")
	if err != nil {
		return err
	}
	var body struct {
		Label string `json:"label"`
	}
	if err := r.Decode(&body); err != nil {
		return err
	}
	token, secret, err := a.Bots.CreateToken(r.Ctx(), r.Actor(), r.Who.Session, id, body.Label)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"token": tokenView(token), "secret": secret})
}

func (a *API) revokeBotToken(r *Request) error {
	if a.Bots == nil {
		return errNoBots
	}
	id, err := PathID[model.AccountID](r, "id")
	if err != nil {
		return err
	}
	token, err := PathID[model.SessionID](r, "token_id")
	if err != nil {
		return err
	}
	if err := a.Bots.RevokeToken(r.Ctx(), r.Actor(), id, token); err != nil {
		return err
	}
	return r.NoContent()
}

func (a *API) listWebhooks(r *Request) error {
	if a.Bots == nil {
		return errNoBots
	}
	page, err := r.Page()
	if err != nil {
		return err
	}
	hooks, next, err := a.Bots.Webhooks(r.Ctx(), r.Actor(), r.R.URL.Query().Get("state"), page)
	if err != nil {
		return err
	}
	return r.JSON(list("webhooks", orEmpty(hooks), next))
}

// ----------------------------------------------- commands and interactions

func (a *API) setCommands(r *Request) error {
	if a.Bots == nil {
		return errNoBots
	}
	var body struct {
		Commands *[]struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Hint        string `json:"hint"`
		} `json:"commands"`
	}
	if err := r.Decode(&body); err != nil {
		return err
	}
	if body.Commands == nil {
		return model.BadRequest("commands is needed: the bot's whole list. An empty list removes them all.")
	}
	cmds := make([]model.Command, 0, len(*body.Commands))
	for _, c := range *body.Commands {
		cmds = append(cmds, model.Command{Name: c.Name, Description: c.Description, Hint: c.Hint})
	}
	out, err := a.Bots.SetCommands(r.Ctx(), r.Actor(), cmds)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"commands": orEmpty(out)})
}

func (a *API) respondToInteraction(r *Request) error {
	if a.Bots == nil {
		return errNoBots
	}
	// An identifier that is not a UUID names no interaction.
	id, err := PathID[string](r, "interaction_id")
	if err != nil {
		return err
	}
	var body struct {
		Type    *string          `json:"type"`
		Message *messageInput    `json:"message"`
		Text    string           `json:"text"`
		Cards   []model.Card     `json:"cards"`
		Form    *bots.DialogForm `json:"form"`
	}
	if err := r.Decode(&body); err != nil {
		return err
	}
	if body.Type == nil {
		return model.BadRequest("type is needed: message, private, dialog or none.")
	}
	resp := bots.Response{Type: *body.Type, Text: body.Text, Cards: body.Cards, Form: body.Form}
	if m := body.Message; m != nil {
		if m.Sealed != nil || m.Quiet {
			return model.Invalid("message", "A bot is never in a sealed chat.")
		}
		in := chat.NewMessage{ClientID: m.ClientID, Text: m.Text, Quote: m.Quote, Cards: m.Cards, Form: m.Form, Action: m.Action, Chance: m.Chance, DrawT: m.DrawT,
			Instants: m.instants()}
		if m.ParentID != "" {
			if in.ParentID, err = BodyID[model.MessageID]("message.parent_id", m.ParentID); err != nil {
				return err
			}
		}
		if in.Quote != nil && !model.ValidID(string(in.Quote.MessageID)) {
			return model.Invalid("message.quote", "quote.message_id is not an identifier.")
		}
		if in.FileIDs, err = fileIDs(m.FileIDs); err != nil {
			return err
		}
		resp.Message = &in
	}
	out, err := a.Bots.Respond(r.Ctx(), r.Actor(), id, resp)
	if err != nil {
		return err
	}
	answer := map[string]any{}
	if out.Message != nil {
		answer["message"] = out.Message
	}
	if out.Dialog != nil {
		answer["dialog"] = out.Dialog
	}
	return r.JSON(answer)
}

func (a *API) submitDialog(r *Request) error {
	if a.Bots == nil {
		return errNoBots
	}
	id, err := PathID[model.FormID](r, "id")
	if err != nil {
		return err
	}
	var body struct {
		ButtonID string          `json:"button_id"`
		Answers  *map[string]any `json:"answers"`
	}
	if err := r.Decode(&body); err != nil {
		return err
	}
	if body.Answers == nil {
		return model.BadRequest("answers is needed: values by field id, or {} for a dialog with no fields.")
	}
	interaction, err := a.Bots.SubmitDialog(r.Ctx(), r.Actor(), id, body.ButtonID, *body.Answers)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"interaction_id": interaction})
}

func (a *API) dismissDialog(r *Request) error {
	if a.Bots == nil {
		return errNoBots
	}
	id, err := PathID[model.FormID](r, "id")
	if err != nil {
		return err
	}
	if err := a.Bots.DismissDialog(r.Ctx(), r.Actor(), id); err != nil {
		return err
	}
	return r.NoContent()
}

// --------------------------------------------------------------- webhooks

func (a *API) requestWebhook(r *Request) error {
	if a.Bots == nil {
		return errNoBots
	}
	conv, err := PathID[model.ConversationID](r, "id")
	if err != nil {
		return err
	}
	var body struct {
		Name    *string `json:"name"`
		Purpose string  `json:"purpose"`
	}
	if err := r.Decode(&body); err != nil {
		return err
	}
	if body.Name == nil {
		return model.BadRequest("name is needed: what the webhook's messages appear under.")
	}
	w, err := a.Bots.RequestWebhook(r.Ctx(), r.Actor(), conv, *body.Name, body.Purpose)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"webhook": w})
}

func (a *API) listChannelWebhooks(r *Request) error {
	if a.Bots == nil {
		return errNoBots
	}
	conv, err := PathID[model.ConversationID](r, "id")
	if err != nil {
		return err
	}
	hooks, err := a.Bots.ChannelWebhooks(r.Ctx(), r.Actor(), conv)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"webhooks": orEmpty(hooks)})
}

func (a *API) approveWebhook(r *Request) error { return a.decideWebhook(r, true) }
func (a *API) declineWebhook(r *Request) error { return a.decideWebhook(r, false) }

func (a *API) decideWebhook(r *Request, approve bool) error {
	if a.Bots == nil {
		return errNoBots
	}
	id, err := PathID[model.WebhookID](r, "id")
	if err != nil {
		return err
	}
	decide := a.Bots.DeclineWebhook
	if approve {
		decide = a.Bots.ApproveWebhook
	}
	w, err := decide(r.Ctx(), r.Actor(), id)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"webhook": w})
}

func (a *API) revokeWebhook(r *Request) error {
	if a.Bots == nil {
		return errNoBots
	}
	id, err := PathID[model.WebhookID](r, "id")
	if err != nil {
		return err
	}
	if err := a.Bots.RevokeWebhook(r.Ctx(), r.Actor(), id); err != nil {
		return err
	}
	return r.NoContent()
}

func (a *API) collectWebhookAddress(r *Request) error { return a.webhookAddress(r, false) }
func (a *API) resetWebhookAddress(r *Request) error   { return a.webhookAddress(r, true) }

func (a *API) webhookAddress(r *Request, reset bool) error {
	if a.Bots == nil {
		return errNoBots
	}
	id, err := PathID[model.WebhookID](r, "id")
	if err != nil {
		return err
	}
	get := a.Bots.CollectWebhookAddress
	if reset {
		get = a.Bots.ResetWebhookAddress
	}
	address, signingSecret, err := get(r.Ctx(), r.Actor(), id)
	if err != nil {
		return err
	}
	// Both are shown this once and can never be shown again.
	r.W.Header().Set("Cache-Control", "no-store")
	return r.JSON(map[string]any{"url": address, "signing_secret": signingSecret})
}

// --------------------------------------------------------- webhook intake

// bodyError maps a failure to read a request body: over the cap is
// too_large, anything else is the sender's malformed request.
func bodyError(err error, tooLarge string) error {
	var tooBig *http.MaxBytesError
	if errors.As(err, &tooBig) {
		return model.ErrTooLarge.With(tooLarge)
	}
	return model.BadRequest("The request body could not be read.")
}

const hookTooLarge = "A webhook request is at most 1 MB, files included. A program that must send more needs a bot."

// hook finds the webhook a request is addressed to by the token in its
// path, which is the only credential these addresses take, and counts the
// request against the webhook's limit.
func (a *API) hook(r *Request) (bots.Hook, error) {
	if a.Bots == nil {
		return bots.Hook{}, errNoBots
	}
	h, err := a.Bots.Find(r.Ctx(), r.R.PathValue("token"))
	if err != nil {
		return h, err
	}
	res := a.Bots.TakePost(h)
	hd := r.W.Header()
	hd.Set("RateLimit-Limit", strconv.Itoa(res.Limit))
	hd.Set("RateLimit-Remaining", strconv.Itoa(res.Remaining))
	hd.Set("RateLimit-Reset", strconv.Itoa(res.ResetSeconds()))
	if !res.OK {
		return h, model.RateLimited(max(res.RetryAfterSeconds(), 1))
	}
	return h, nil
}

// hookBody reads the whole body of a webhook request. The cap of 1 MB was
// put on it before the handler ran.
func hookBody(r *Request) ([]byte, error) {
	raw, err := io.ReadAll(r.R.Body)
	if err != nil {
		return nil, bodyError(err, hookTooLarge)
	}
	return raw, nil
}

// mediaType is the request's content type without its parameters.
func mediaType(r *Request) (string, map[string]string) {
	t, params, err := mime.ParseMediaType(r.R.Header.Get("Content-Type"))
	if err != nil {
		return "", nil
	}
	return t, params
}

// hookParts reads a multipart body into memory: the text of the part that
// holds the JSON, and the file parts by their form names. It is small by
// construction, since the whole request is at most 1 MB.
func hookParts(raw []byte, boundary, jsonPart string, isFile func(name string) bool) (payload []byte, files map[string]bots.Part, fields map[string]string, err error) {
	mr := multipart.NewReader(bytes.NewReader(raw), boundary)
	files, fields = map[string]bots.Part{}, map[string]string{}
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			return payload, files, fields, nil
		}
		if err != nil {
			return nil, nil, nil, model.BadRequest("The multipart body could not be read.")
		}
		name := part.FormName()
		data, err := io.ReadAll(part)
		if err != nil {
			return nil, nil, nil, model.BadRequest("The multipart body could not be read.")
		}
		switch {
		case name == jsonPart:
			if payload != nil {
				return nil, nil, nil, model.BadRequest("The part " + name + " is given twice.")
			}
			payload = data
		case isFile(name):
			if _, dup := files[name]; dup {
				return nil, nil, nil, model.BadRequest("The part " + name + " is given twice.")
			}
			files[name] = bots.Part{Filename: part.FileName(), Data: data}
		default:
			fields[name] = string(data)
		}
	}
}

// formPayload reads the JSON out of a form-encoded body's `payload` field,
// which is how Slack and GitHub send JSON to a form address.
func formPayload(raw []byte) ([]byte, error) {
	form, err := url.ParseQuery(string(raw))
	if err != nil || form.Get("payload") == "" {
		return nil, model.BadRequest("A form-encoded body carries its JSON in a field named payload.")
	}
	return []byte(form.Get("payload")), nil
}

func (a *API) postWebhook(r *Request) error {
	h, err := a.hook(r)
	if err != nil {
		return err
	}
	raw, err := hookBody(r)
	if err != nil {
		return err
	}
	var parts map[string]bots.Part
	if t, params := mediaType(r); t == "multipart/form-data" {
		var fields map[string]string
		native := func(name string) bool {
			n, ok := strings.CutPrefix(name, "file")
			i, err := strconv.Atoi(n)
			return ok && err == nil && i >= 1 && i <= 10 && strconv.Itoa(i) == n
		}
		if raw, parts, fields, err = hookParts(raw, params["boundary"], "payload", native); err != nil {
			return err
		}
		if len(fields) > 0 {
			return model.BadRequest("A multipart request holds a part named payload and parts named file1 to file10, and nothing else.")
		}
		if raw == nil {
			return model.BadRequest("A multipart request needs a part named payload holding the JSON.")
		}
	}
	// An unknown field is refused on this address, as everywhere in the
	// API; the other three addresses ignore what they do not know.
	var p bots.Payload
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return model.BadRequest("The payload could not be read: " + plain(err))
	}
	if dec.More() {
		return model.BadRequest("The payload must be one JSON object.")
	}
	posted, err := a.Bots.Post(r.Ctx(), h, p, parts)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"message": posted})
}

func (a *API) postSlackWebhook(r *Request) error {
	h, err := a.hook(r)
	if err != nil {
		return err
	}
	raw, err := hookBody(r)
	if err != nil {
		return err
	}
	if t, _ := mediaType(r); t == "application/x-www-form-urlencoded" {
		if raw, err = formPayload(raw); err != nil {
			return err
		}
	}
	p, err := bots.ConvertSlack(raw)
	if err != nil {
		return err
	}
	if _, err := a.Bots.Post(r.Ctx(), h, p, nil); err != nil {
		return err
	}
	// What Slack answers, and what some tools check for.
	r.W.Header().Set("Content-Type", "text/plain; charset=utf-8")
	r.W.WriteHeader(http.StatusOK)
	_, err = r.W.Write([]byte("ok"))
	return err
}

func (a *API) postDiscordWebhook(r *Request) error {
	h, err := a.hook(r)
	if err != nil {
		return err
	}
	raw, err := hookBody(r)
	if err != nil {
		return err
	}
	var (
		parts  map[string]bots.Part
		dparts []bots.DiscordPart
	)
	if t, params := mediaType(r); t == "multipart/form-data" {
		index := func(name string) (int, bool) {
			n, ok := strings.CutPrefix(name, "files[")
			n, ok2 := strings.CutSuffix(n, "]")
			i, err := strconv.Atoi(n)
			return i, ok && ok2 && err == nil && i >= 0
		}
		var fields map[string]string
		payload, files, fields, err := hookParts(raw, params["boundary"], "payload_json", func(name string) bool { _, ok := index(name); return ok })
		if err != nil {
			return err
		}
		if payload == nil {
			// Without payload_json the plain fields of the form are
			// the message, as Discord reads them.
			simple := map[string]string{}
			for _, k := range []string{"content", "username", "avatar_url"} {
				if v, ok := fields[k]; ok {
					simple[k] = v
				}
			}
			payload, _ = json.Marshal(simple)
		}
		raw, parts = payload, files
		names := make([]string, 0, len(files))
		for name := range files {
			names = append(names, name)
		}
		sort.Slice(names, func(i, j int) bool {
			x, _ := index(names[i])
			y, _ := index(names[j])
			return x < y
		})
		for _, name := range names {
			dparts = append(dparts, bots.DiscordPart{Part: name, Filename: files[name].Filename})
		}
	}
	p, err := bots.ConvertDiscord(raw, dparts)
	if err != nil {
		return err
	}
	// A part the converted message does not use (one beyond the tenth
	// file) is dropped, not refused: this address ignores what it cannot
	// carry.
	used := map[string]bots.Part{}
	keep := func(name string) {
		if part, ok := parts[name]; ok && name != "" {
			used[name] = part
		}
	}
	for _, f := range p.Files {
		keep(f.Part)
	}
	for _, c := range p.Cards {
		keep(c.ImagePart)
		if c.Author != nil {
			keep(c.Author.IconPart)
		}
	}
	if p.Via != nil {
		keep(p.Via.IconPart)
	}
	posted, err := a.Bots.Post(r.Ctx(), h, p, used)
	if err != nil {
		return err
	}
	// Discord answers 204 with no body, and the message only when asked.
	if wait, _ := strconv.ParseBool(r.R.URL.Query().Get("wait")); wait {
		return r.JSONStatus(http.StatusOK, map[string]any{"id": posted.ID})
	}
	return r.NoContent()
}

func (a *API) postGithubWebhook(r *Request) error {
	h, err := a.hook(r)
	if err != nil {
		return err
	}
	raw, err := hookBody(r)
	if err != nil {
		return err
	}
	// The signature is over the raw body, exactly as received and before
	// any decoding, in both content types.
	if err := a.Bots.VerifySignature(h, r.R.Header.Get("X-Hub-Signature-256"), raw); err != nil {
		return err
	}
	event := strings.TrimSpace(r.R.Header.Get("X-GitHub-Event"))
	if event == "" {
		return model.BadRequest("The X-GitHub-Event header is needed: it names the event.")
	}
	if t, _ := mediaType(r); t == "application/x-www-form-urlencoded" {
		if raw, err = formPayload(raw); err != nil {
			return err
		}
	}
	p, post, err := bots.ConvertGitHub(event, raw, model.Now())
	if err != nil {
		return err
	}
	if !post {
		// A listed event whose action posts nothing.
		return r.NoContent()
	}
	// A delivery GitHub sends again within a day is not posted twice.
	if delivery := strings.ToLower(strings.TrimSpace(r.R.Header.Get("X-GitHub-Delivery"))); model.ValidID(delivery) {
		p.ClientID = delivery
	}
	posted, err := a.Bots.Post(r.Ctx(), h, p, nil)
	if err != nil {
		return err
	}
	return r.JSONStatus(http.StatusOK, map[string]any{"message": posted})
}
