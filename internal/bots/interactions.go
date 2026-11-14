package bots

import (
	"context"
	"time"
	"unicode/utf8"

	"github.com/tikaydaes/churr/internal/chat"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/perm"
)

// Interactions (docs/BOTS.md section 5): one request from one person to one
// bot, and the bot's responses to it. They are held in memory, so a restart
// ends them, and nothing about them is ever stored.

// interaction is one request from one person to one bot.
type interaction struct {
	id      string
	bot     model.AccountID
	asker   model.AccountID
	conv    model.ConversationID
	parent  model.MessageID // the thread the person acted in, if any
	command string          // the command's name, when it was a command
	until   time.Time
	// responses counts the responses given or under way.
	responses int
	// dialogShown is set once the one dialog this interaction may open
	// has been opened.
	dialogShown bool
}

// dialog is what this package remembers of a form a bot is showing one
// person. The form itself is the forms package's.
type dialog struct {
	bot   model.AccountID
	asker model.AccountID
	// conv, parent and until come from the interaction that opened it:
	// where the person acted, and when the dialog ends with it.
	conv   model.ConversationID
	parent model.MessageID
	until  time.Time
}

// open records a new interaction and returns it with its identifier.
func (s *Service) open(it interaction) interaction {
	now := time.Now()
	it.id = model.NewID[string]()
	it.until = now.Add(interactionLife)
	s.mu.Lock()
	defer s.mu.Unlock()
	// Ended interactions and dialogs are forgotten about once a minute,
	// as new ones arrive.
	if now.Sub(s.swept) > time.Minute {
		s.swept = now
		for id, old := range s.interactions {
			if now.After(old.until) {
				delete(s.interactions, id)
			}
		}
		for id, d := range s.dialogs {
			if now.After(d.until) {
				delete(s.dialogs, id)
			}
		}
	}
	stored := it
	s.interactions[it.id] = &stored
	return it
}

// claim finds a bot's live interaction and takes one of its five responses.
// An interaction that has ended, has had its five, or was sent to another
// bot is not found. release gives the response back when it could not be
// made, so a refused response does not use one up.
func (s *Service) claim(id string, bot model.AccountID, forDialog bool) (it interaction, release func(), err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.interactions[id]
	if p == nil || p.bot != bot || time.Now().After(p.until) || p.responses >= maxResponses {
		return interaction{}, nil, model.ErrNotFound
	}
	if forDialog {
		if p.dialogShown {
			return interaction{}, nil, model.ErrConflict.With("This interaction has already opened its one dialog.")
		}
		p.dialogShown = true
	}
	p.responses++
	return *p, func() {
		s.mu.Lock()
		p.responses--
		if forDialog {
			p.dialogShown = false
		}
		s.mu.Unlock()
	}, nil
}

// The types of response.
const (
	ResponseMessage = "message"
	ResponsePrivate = "private"
	ResponseDialog  = "dialog"
	ResponseNone    = "none"
)

// DialogForm is the form of a `dialog` response: the fields and buttons of
// any form, and none of its settings. Its action is always `bot`.
type DialogForm struct {
	Title   string             `json:"title"`
	Fields  []model.FormField  `json:"fields"`
	Buttons []model.FormButton `json:"buttons"`
}

// Response is a bot's response to an interaction (schema
// InteractionResponse).
type Response struct {
	Type string
	// Message is for `message`: what the post-a-message operation takes.
	Message *chat.NewMessage
	// Text and Cards are for `private`.
	Text  string
	Cards []model.Card
	// Form is for `dialog`.
	Form *DialogForm
}

// Responded is what Respond returns: the message a `message` response
// posted, or the dialog a `dialog` response opened.
type Responded struct {
	Message *model.Message
	Dialog  *model.Dialog
}

// Respond carries out a bot's response to an interaction that was sent to
// it. An interaction lasts 15 minutes and takes at most five responses;
// after that it is not found. The asker's clients learn of each response
// through `interaction.responded`, sent to that account alone.
func (s *Service) Respond(ctx context.Context, actor model.Account, id string, r Response) (Responded, error) {
	switch r.Type {
	case ResponseMessage:
		if r.Message == nil {
			return Responded{}, model.Invalid("message", "A message response needs a message.")
		}
	case ResponsePrivate:
		if r.Text == "" && len(r.Cards) == 0 {
			return Responded{}, model.Invalid("text", "A private response needs text or a card.")
		}
	case ResponseDialog:
		if r.Form == nil {
			return Responded{}, model.Invalid("form", "A dialog response needs a form.")
		}
	case ResponseNone:
	default:
		return Responded{}, model.Invalid("type", "A response is message, private, dialog or none.")
	}
	if !model.ValidID(id) {
		return Responded{}, model.ErrNotFound
	}
	it, release, err := s.claim(id, actor.ID, r.Type == ResponseDialog)
	if err != nil {
		return Responded{}, err
	}
	out, response, err := s.respond(ctx, actor, it, r)
	if err != nil {
		release()
		return Responded{}, err
	}
	payload := map[string]any{"interaction_id": it.id, "bot_id": it.bot, "response": response}
	if it.parent != "" {
		payload["parent_id"] = it.parent
	}
	s.hub.Send(it.asker, model.Event{Type: eventInteractionResponded, At: s.now(), ConversationID: it.conv, Payload: payload})
	return out, nil
}

// respond does what one type of response does and returns the `response`
// member of the event the asker receives.
func (s *Service) respond(ctx context.Context, actor model.Account, it interaction, r Response) (Responded, map[string]any, error) {
	switch r.Type {
	case ResponseMessage:
		in := *r.Message
		in.ConversationID = it.conv
		// An answer to something done in a thread goes into that thread
		// unless the bot says otherwise.
		if in.ParentID == "" {
			in.ParentID = it.parent
		}
		in.Via = nil
		if it.command != "" {
			// Everyone can see who made the bot speak.
			in.Via = &model.Via{Kind: "command", Name: it.command, AccountID: it.asker}
		}
		if err := s.AllowMessage(actor.ID, it.conv); err != nil {
			return Responded{}, nil, err
		}
		posted, err := s.chat.Post(ctx, actor, in)
		if err != nil {
			return Responded{}, nil, err
		}
		return Responded{Message: &posted.Message}, map[string]any{"type": ResponseMessage, "message_id": posted.Message.ID}, nil

	case ResponsePrivate:
		if utf8.RuneCountInString(r.Text) > model.MaxMessageText {
			return Responded{}, nil, model.ErrTooLarge.With("A private response is at most 16,000 characters.")
		}
		if err := checkCards(r.Cards); err != nil {
			return Responded{}, nil, err
		}
		// The bot must still be where the person asked.
		if _, err := s.perm.Can(ctx, actor, perm.See, it.conv); err != nil {
			return Responded{}, nil, notFound(err)
		}
		entities, err := s.chat.Entities(ctx, actor, it.conv, r.Text)
		if err != nil {
			return Responded{}, nil, err
		}
		cards := r.Cards
		if cards == nil {
			cards = []model.Card{}
		}
		return Responded{}, map[string]any{"type": ResponsePrivate, "text": r.Text, "entities": entities, "cards": cards}, nil

	case ResponseDialog:
		forms := s.dialogStore()
		if forms == nil {
			return Responded{}, nil, model.ErrUnavailable.With("Dialogs are not available on this server.")
		}
		if _, err := s.perm.Can(ctx, actor, perm.See, it.conv); err != nil {
			return Responded{}, nil, notFound(err)
		}
		// A dialog is always the direct result of something this person
		// just did: the interaction is theirs, and still open. Its form
		// is the forms package's to check and keep.
		form := model.FormInput{Title: r.Form.Title, Fields: r.Form.Fields, Buttons: r.Form.Buttons, Action: model.FormAction{Type: model.FormBot}}
		d, err := forms.OpenDialog(ctx, actor, it.asker, it.conv, form)
		if err != nil {
			return Responded{}, nil, err
		}
		if d.Fields == nil {
			d.Fields = []model.FormField{}
		}
		s.mu.Lock()
		s.dialogs[d.ID] = &dialog{bot: it.bot, asker: it.asker, conv: it.conv, parent: it.parent, until: it.until}
		s.mu.Unlock()
		return Responded{Dialog: &d}, map[string]any{"type": ResponseDialog, "dialog": d}, nil
	}
	return Responded{}, map[string]any{"type": ResponseNone}, nil
}

// checkCards checks cards that reach a person without passing through a
// message: those of a private response.
func checkCards(cards []model.Card) error {
	if len(cards) > model.MaxCardsPerMessage {
		return model.Invalid("cards", "At most five cards.")
	}
	for _, c := range cards {
		if err := checkCard(c); err != nil {
			return err
		}
	}
	return nil
}

// checkCard applies the card rules of docs/MESSAGE-FORMAT.md section 6.
func checkCard(c model.Card) error { return checkCardShape(c, c.ImageFileID != "") }

// checkCardShape is checkCard for a card whose image may still be on its
// way: hasImage says whether it will have one.
func checkCardShape(c model.Card, hasImage bool) error {
	valid := c.Color == ""
	for _, name := range model.CardColors {
		valid = valid || c.Color == name
	}
	switch {
	case !valid:
		return model.Invalid("cards", "A card's colour is lavender, cyan, pink, yellow, green, red or grey.")
	case c.Title == "" && c.Text == "" && !hasImage:
		return model.Invalid("cards", "A card needs at least a title, text or image.")
	case utf8.RuneCountInString(c.Title) > maxCardTitle:
		return model.Invalid("cards", "A card's title is at most 200 characters.")
	case utf8.RuneCountInString(c.Text) > maxCardText:
		return model.Invalid("cards", "A card's text is at most 4,000 characters.")
	case len(c.Fields) > model.MaxFieldsPerCard:
		return model.Invalid("cards", "A card has at most ten fields.")
	case c.URL != "" && webAddress(c.URL) == "":
		return model.Invalid("cards", "A card's link must be an http or https address.")
	case c.PreviewURL != "":
		return model.Invalid("cards", "Only the server sets preview_url.")
	case c.Author != nil && c.Author.Name == "":
		return model.Invalid("cards", "A card's author needs a name.")
	case c.ImageFileID != "" && !model.ValidID(string(c.ImageFileID)):
		return model.Invalid("cards", "image_file_id is not an identifier.")
	case c.Author != nil && c.Author.IconFileID != "" && !model.ValidID(string(c.Author.IconFileID)):
		return model.Invalid("cards", "icon_file_id is not an identifier.")
	}
	return nil
}

// FormAnswered delivers an answer or a button press on a bot's form to the
// bot, as an interaction, and returns the interaction's identifier. The
// forms package calls it after checking who may answer and that the answer
// fits the form; nothing is stored. A bot with no live connection is
// unavailable.
func (s *Service) FormAnswered(ctx context.Context, sub model.FormSubmission) (string, error) {
	if !s.connected(sub.BotID) {
		return "", errNotConnected
	}
	it := s.open(interaction{bot: sub.BotID, asker: sub.AccountID, conv: sub.ConversationID, parent: sub.ParentID})
	answers := sub.Answers
	if answers == nil {
		answers = map[string]any{}
	}
	payload := map[string]any{"interaction_id": it.id, "form_id": sub.FormID, "account_id": sub.AccountID, "answers": answers}
	if sub.MessageID != "" {
		payload["message_id"] = sub.MessageID
	}
	if sub.ButtonID != "" {
		payload["button_id"] = sub.ButtonID
	}
	if sub.Dialog {
		payload["dialog"] = true
	}
	s.hub.Send(sub.BotID, model.Event{Type: eventFormAnswered, At: s.now(), ConversationID: sub.ConversationID, Payload: payload})
	return it.id, nil
}

// dialogOf finds what is remembered of the dialog being shown to an
// account. One that was shown to someone else, was answered or dismissed,
// or has ended with its interaction, is not found.
func (s *Service) dialogOf(ctx context.Context, id model.FormID, actor model.Account) (*dialog, Dialogs, error) {
	forms := s.dialogStore()
	s.mu.Lock()
	d := s.dialogs[id]
	ended := d != nil && d.asker == actor.ID && time.Now().After(d.until)
	if ended {
		delete(s.dialogs, id)
	}
	s.mu.Unlock()
	if ended && forms != nil {
		// Its interaction is over; the stored form goes with it.
		forms.DismissDialog(ctx, actor, id)
	}
	if d == nil || d.asker != actor.ID || ended || forms == nil {
		return nil, nil, model.ErrNotFound
	}
	return d, forms, nil
}

// forget drops what is remembered of a dialog, if it is still this one.
func (s *Service) forget(id model.FormID, d *dialog) {
	s.mu.Lock()
	if s.dialogs[id] == d {
		delete(s.dialogs, id)
	}
	s.mu.Unlock()
}

// SubmitDialog sends the answer to a dialog to the bot that opened it, as
// `form.answered` with a new interaction, whose identifier it returns. The
// bot can respond to that in turn. The answer is checked against the form
// first. The dialog is gone once answered; if the bot is not connected it
// stays, so the person can try again.
func (s *Service) SubmitDialog(ctx context.Context, actor model.Account, id model.FormID, buttonID string, answers map[string]any) (string, error) {
	d, forms, err := s.dialogOf(ctx, id, actor)
	if err != nil {
		return "", err
	}
	if answers == nil {
		answers = map[string]any{}
	}
	interactionID, err := forms.SubmitDialog(ctx, actor, id, d.conv, d.parent, buttonID, answers)
	if err != nil {
		if model.CodeOf(err) == model.CodeNotFound {
			s.forget(id, d)
		}
		return "", err
	}
	s.forget(id, d)
	return interactionID, nil
}

// DismissDialog closes a dialog without answering it. The bot is told
// nothing.
func (s *Service) DismissDialog(ctx context.Context, actor model.Account, id model.FormID) error {
	d, forms, err := s.dialogOf(ctx, id, actor)
	if err != nil {
		return err
	}
	s.forget(id, d)
	if err := forms.DismissDialog(ctx, actor, id); err != nil && model.CodeOf(err) != model.CodeNotFound {
		return err
	}
	return nil
}
