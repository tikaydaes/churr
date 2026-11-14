package forms

import (
	"context"
	"time"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

// DialogLife is how long a dialog nobody answered or dismissed is kept
// (docs/SCHEMA.md, forms).
const DialogLife = time.Hour

// OpenDialog stores a form a bot shows one person in a dialog and returns it
// in the shape the person's client is sent. A dialog has fields and buttons
// and nothing else: its action is always `bot` and it takes no settings.
//
// The bots package calls this when a bot responds to an interaction with a
// dialog; that the bot may show this person a dialog now (an interaction of
// theirs is open, and they are an account of this server) is its to check.
// conv is the conversation the person acted in, which the stored form does
// not keep: the caller remembers it with the interaction.
func (s *Service) OpenDialog(ctx context.Context, bot model.Account, to model.AccountID, conv model.ConversationID, in model.FormInput) (model.Dialog, error) {
	if in.Action.Type == "" {
		in.Action.Type = model.FormBot
	}
	if in.Action.Type != model.FormBot {
		return model.Dialog{}, model.BadRequest("A dialog always uses the bot action.")
	}
	if in.Preset != "" {
		return model.Dialog{}, model.BadRequest("A dialog takes no settings: only a title, fields and buttons.")
	}
	if err := Validate(in, true, s.now()); err != nil {
		return model.Dialog{}, err
	}
	f := fromInput(in)
	f.DialogFor, f.OwnerID, f.CreatedAt = to, bot.ID, s.now()
	if err := s.store.Forms().Create(ctx, &f); err != nil {
		return model.Dialog{}, err
	}
	return model.Dialog{ID: f.ID, BotID: bot.ID, ConversationID: conv, Title: f.Title, Fields: f.Fields, Buttons: f.Buttons}, nil
}

// dialog finds a dialog shown to the actor. Anyone else's, and a form on a
// message, do not exist as far as the actor is concerned.
func (s *Service) dialog(ctx context.Context, actor model.Account, id model.FormID) (store.Form, error) {
	f, err := s.store.Forms().Get(ctx, id)
	if err != nil {
		return f, err
	}
	if f.DialogFor == "" || f.DialogFor != actor.ID {
		return f, model.ErrNotFound
	}
	return f, nil
}

// SubmitDialog checks the actor's answer to a dialog, hands it to the bot
// and deletes the dialog. It returns the id of the interaction the answer
// started. If the bot is not connected the dialog stays, so the person can
// try again. conv and parent are where the person was when the dialog was
// opened, which the caller remembers.
func (s *Service) SubmitDialog(ctx context.Context, actor model.Account, id model.FormID, conv model.ConversationID, parent model.MessageID, button string, answers map[string]any) (string, error) {
	f, err := s.dialog(ctx, actor, id)
	if err != nil {
		return "", err
	}
	if button != "" {
		known := false
		for _, b := range f.Buttons {
			known = known || b.ID == button
		}
		if !known {
			return "", model.Invalid("button_id", "The dialog has no such button.")
		}
	}
	clean, err := CheckAnswers(f.Fields, answers, lookup{ctx, s, actor})
	if err != nil {
		return "", err
	}
	bots := s.botsImpl()
	if bots == nil {
		return "", model.ErrUnavailable.With("The bot is not connected.")
	}
	interaction, err := bots.FormAnswered(ctx, model.FormSubmission{
		BotID: f.OwnerID, AccountID: actor.ID, ConversationID: conv, ParentID: parent, FormID: f.ID,
		ButtonID: button, Answers: clean, Dialog: true,
	})
	if err != nil {
		return "", err
	}
	// Answered: the dialog is gone. Someone who submits twice at once
	// finds it gone the second time, which is all a missing row means.
	if err := s.store.Forms().Delete(ctx, f.ID); err != nil && model.CodeOf(err) != model.CodeNotFound {
		return "", err
	}
	return interaction, nil
}

// DismissDialog deletes a dialog the actor closed without answering. The bot
// is told nothing.
func (s *Service) DismissDialog(ctx context.Context, actor model.Account, id model.FormID) error {
	f, err := s.dialog(ctx, actor, id)
	if err != nil {
		return err
	}
	return s.store.Forms().Delete(ctx, f.ID)
}

// Dialog returns a dialog shown to the actor, in the shape a client is sent.
func (s *Service) Dialog(ctx context.Context, actor model.Account, id model.FormID, conv model.ConversationID) (model.Dialog, error) {
	f, err := s.dialog(ctx, actor, id)
	if err != nil {
		return model.Dialog{}, err
	}
	return model.Dialog{ID: f.ID, BotID: f.OwnerID, ConversationID: conv, Title: f.Title, Fields: f.Fields, Buttons: f.Buttons}, nil
}

// Sweep deletes dialogs nobody answered or dismissed within DialogLife and
// returns how many went. The scheduler calls it now and then.
func (s *Service) Sweep(ctx context.Context) (int64, error) {
	return s.store.Forms().DeleteDialogsBefore(ctx, s.now().Add(-DialogLife))
}
