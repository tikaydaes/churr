package bots

import (
	"context"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/tikaydaes/churr/internal/chat"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/people"
	"github.com/tikaydaes/churr/internal/perm"
	"github.com/tikaydaes/churr/internal/secret"
	"github.com/tikaydaes/churr/internal/store"
)

// What an admin does with bots (docs/BOTS.md section 2): creating and
// removing them, and making and revoking their tokens.

const (
	maxDescription = 250
	// maxTokens is how many live tokens one bot may have, so a token can
	// be replaced without downtime and old ones do not pile up.
	maxTokens = 10
	// freshFor is how recent a passkey or authenticator check must be for
	// the operations that need a fresh one (docs/AUTH.md section 9).
	freshFor     = 10 * time.Minute
	defaultLabel = "Bot token"
)

// manage asks perm whether the actor may manage bots.
func (s *Service) manage(ctx context.Context, actor model.Account) error {
	return s.perm.CanAdminister(ctx, actor, perm.ManageBots)
}

// fresh answers whether a session was checked recently enough to make a
// credential. A token cannot have been, so a token cannot make tokens.
func (s *Service) fresh(ses model.Session) error {
	if ses.Kind != model.SessionWeb && ses.Kind != model.SessionApp {
		return model.ErrFreshCheckRequired.With("Sign in with a browser or an app to do this.")
	}
	if ses.FreshAt.IsZero() || s.now().Sub(ses.FreshAt) > freshFor {
		return model.ErrFreshCheckRequired
	}
	return nil
}

func cleanDescription(d string) (string, error) {
	d = strings.TrimSpace(d)
	if utf8.RuneCountInString(d) > maxDescription {
		return "", model.Invalid("description", "A description is at most 250 characters.")
	}
	return d, nil
}

// bot puts a bots row in the shape admins see: the account, its commands
// and the named channels it is in.
func (s *Service) bot(ctx context.Context, viewer model.Account, info store.BotInfo) (model.Bot, error) {
	acc, err := s.store.Accounts().Get(ctx, info.AccountID)
	if err != nil {
		return model.Bot{}, err
	}
	shown, err := s.people.Show(ctx, viewer, acc)
	if err != nil {
		return model.Bot{}, err
	}
	out := model.Bot{Account: shown, Description: info.Description, CreatedByID: info.CreatedBy, WebhookOnly: info.WebhookOnly}
	if out.Commands, err = s.store.Bots().Commands(ctx, info.AccountID); err != nil {
		return out, err
	}
	out.ChannelIDs, err = s.store.Bots().ChannelIDs(ctx, info.AccountID)
	return out, err
}

// find returns the bots row of a bot that has not been removed. Anything
// else, an ordinary account included, is not a bot and so is not found.
func (s *Service) find(ctx context.Context, id model.AccountID) (store.BotInfo, model.Account, error) {
	acc, err := s.store.Accounts().Get(ctx, id)
	if err != nil || acc.Role != model.RoleBot || !acc.Active() {
		return store.BotInfo{}, acc, model.ErrNotFound
	}
	info, found, err := s.store.Bots().Get(ctx, id)
	if err != nil {
		return info, acc, err
	}
	if !found {
		return info, acc, model.ErrNotFound
	}
	return info, acc, nil
}

// NewBot is what Create takes.
type NewBot struct {
	Username    string
	DisplayName string
	Description string
}

// Create makes a bot: an account with role bot, its bots row and its first
// token, which is returned here and never again. The bot is in no channel
// until someone adds it.
func (s *Service) Create(ctx context.Context, actor model.Account, ses model.Session, in NewBot) (model.Bot, model.Session, string, error) {
	if err := s.manage(ctx, actor); err != nil {
		return model.Bot{}, model.Session{}, "", err
	}
	if err := s.fresh(ses); err != nil {
		return model.Bot{}, model.Session{}, "", err
	}
	description, err := cleanDescription(in.Description)
	if err != nil {
		return model.Bot{}, model.Session{}, "", err
	}
	var (
		info  store.BotInfo
		token model.Session
	)
	secretToken, hash := secret.NewToken(secret.PrefixBot)
	err = s.store.Tx(ctx, func(tx store.Tx) error {
		// A bot is in no built-in group, so making its account brings
		// it into no channel.
		acc, _, err := s.people.CreateTx(ctx, tx, people.NewAccount{Username: in.Username, DisplayName: in.DisplayName, Role: model.RoleBot})
		if err != nil {
			return err
		}
		info = store.BotInfo{AccountID: acc.ID, Description: description, CreatedBy: actor.ID}
		if err := tx.Bots().Create(ctx, info); err != nil {
			return err
		}
		token = model.Session{AccountID: acc.ID, Kind: model.SessionBot, Label: defaultLabel, CreatedAt: s.now()}
		if err := tx.Sessions().Create(ctx, &token, hash, nil); err != nil {
			return err
		}
		if err := audit(ctx, tx, actor, "bot.created", "account", string(acc.ID), map[string]any{"username": acc.Username}); err != nil {
			return err
		}
		return audit(ctx, tx, actor, "token.created", "session", string(token.ID), map[string]any{"kind": "bot", "bot_id": acc.ID, "label": token.Label})
	})
	if err != nil {
		return model.Bot{}, model.Session{}, "", err
	}
	out, err := s.bot(ctx, actor, info)
	return out, token, secretToken, err
}

// Get returns one bot as admins see it.
func (s *Service) Get(ctx context.Context, actor model.Account, id model.AccountID) (model.Bot, error) {
	if err := s.manage(ctx, actor); err != nil {
		return model.Bot{}, err
	}
	info, _, err := s.find(ctx, id)
	if err != nil {
		return model.Bot{}, err
	}
	return s.bot(ctx, actor, info)
}

// List returns the bots, oldest first. The accounts behind webhooks are
// left out unless webhooks is true.
func (s *Service) List(ctx context.Context, actor model.Account, webhooks bool, p store.Page) ([]model.Bot, string, error) {
	if err := s.manage(ctx, actor); err != nil {
		return nil, "", err
	}
	infos, next, err := s.store.Bots().List(ctx, webhooks, p)
	if err != nil {
		return nil, "", err
	}
	out := make([]model.Bot, 0, len(infos))
	for _, info := range infos {
		b, err := s.bot(ctx, actor, info)
		if err != nil {
			return nil, "", err
		}
		out = append(out, b)
	}
	return out, next, nil
}

// BotChange is what Update takes. Nil fields stay as they are.
type BotChange struct {
	Username    *string
	DisplayName *string
	Description *string
}

// Update changes a bot's name or description.
func (s *Service) Update(ctx context.Context, actor model.Account, id model.AccountID, in BotChange) (model.Bot, error) {
	if err := s.manage(ctx, actor); err != nil {
		return model.Bot{}, err
	}
	info, acc, err := s.find(ctx, id)
	if err != nil {
		return model.Bot{}, err
	}
	if info.WebhookOnly && in.Username != nil {
		return model.Bot{}, model.Invalid("username", "The account behind a webhook keeps its generated username.")
	}
	if in.Description != nil {
		d, err := cleanDescription(*in.Description)
		if err != nil {
			return model.Bot{}, err
		}
		if err := s.store.Bots().SetDescription(ctx, id, d); err != nil {
			return model.Bot{}, err
		}
		info.Description = d
	}
	if in.Username != nil || in.DisplayName != nil {
		// The names are the account's, changed the way anyone's are, so
		// everyone who can see the bot is told.
		if _, err := s.people.UpdateMe(ctx, acc, people.ProfileChange{Username: in.Username, DisplayName: in.DisplayName}); err != nil {
			return model.Bot{}, err
		}
	}
	if err := audit(ctx, s.store, actor, "bot.updated", "account", string(id), nil); err != nil {
		return model.Bot{}, err
	}
	return s.bot(ctx, actor, info)
}

// SetAvatar stores an image as a bot's avatar.
func (s *Service) SetAvatar(ctx context.Context, actor model.Account, id model.AccountID, content io.Reader) (model.Bot, error) {
	if err := s.manage(ctx, actor); err != nil {
		return model.Bot{}, err
	}
	info, acc, err := s.find(ctx, id)
	if err != nil {
		return model.Bot{}, err
	}
	if _, err := s.people.SetAvatar(ctx, acc, content); err != nil {
		return model.Bot{}, err
	}
	return s.bot(ctx, actor, info)
}

// Remove removes a bot the way any account is removed: its tokens end, it
// leaves every channel, its commands go and its messages stay. Removing the
// account behind a webhook revokes the webhook.
func (s *Service) Remove(ctx context.Context, actor model.Account, id model.AccountID) error {
	if err := s.manage(ctx, actor); err != nil {
		return err
	}
	if _, _, err := s.find(ctx, id); err != nil {
		return err
	}
	return s.people.Remove(ctx, actor, id)
}

// botTokens returns a bot's live tokens, newest first.
func (s *Service) botTokens(ctx context.Context, a store.Areas, bot model.AccountID) ([]model.Session, error) {
	all, err := a.Sessions().ForAccount(ctx, bot, s.now())
	if err != nil {
		return nil, err
	}
	out := all[:0]
	for _, ses := range all {
		if ses.Kind == model.SessionBot {
			out = append(out, ses)
		}
	}
	return out, nil
}

// Tokens lists a bot's live tokens. The secrets are never shown again.
func (s *Service) Tokens(ctx context.Context, actor model.Account, id model.AccountID) ([]model.Session, error) {
	if err := s.manage(ctx, actor); err != nil {
		return nil, err
	}
	if _, _, err := s.find(ctx, id); err != nil {
		return nil, err
	}
	return s.botTokens(ctx, s.store, id)
}

// CreateToken makes another token for a bot, so the old one can be
// replaced without downtime. A token does not expire.
func (s *Service) CreateToken(ctx context.Context, actor model.Account, ses model.Session, id model.AccountID, label string) (model.Session, string, error) {
	if err := s.manage(ctx, actor); err != nil {
		return model.Session{}, "", err
	}
	if err := s.fresh(ses); err != nil {
		return model.Session{}, "", err
	}
	info, _, err := s.find(ctx, id)
	if err != nil {
		return model.Session{}, "", err
	}
	if info.WebhookOnly {
		return model.Session{}, "", model.ErrConflict.With("The account behind a webhook has no token. It posts through its address only.")
	}
	label = strings.TrimSpace(label)
	if label == "" {
		label = defaultLabel
	}
	if utf8.RuneCountInString(label) > 64 || strings.IndexFunc(label, func(r rune) bool { return r < ' ' }) >= 0 {
		return model.Session{}, "", model.Invalid("label", "A label is at most 64 characters.")
	}
	secretToken, hash := secret.NewToken(secret.PrefixBot)
	token := model.Session{AccountID: id, Kind: model.SessionBot, Label: label, CreatedAt: s.now()}
	// Whoever holds the new token reads as the bot, so every conversation
	// the bot is in that is not public is told, in the same transaction,
	// who made it (review finding H3).
	err = s.chat.WithAlert(ctx, id, actor.ID, chat.AlertBotToken, "", func(tx store.Tx) error {
		have, err := s.botTokens(ctx, tx, id)
		if err != nil {
			return err
		}
		if len(have) >= maxTokens {
			return model.ErrConflict.With("A bot has at most ten tokens. Revoke one first.")
		}
		if err := tx.Sessions().Create(ctx, &token, hash, nil); err != nil {
			return err
		}
		return audit(ctx, tx, actor, "token.created", "session", string(token.ID), map[string]any{"kind": "bot", "bot_id": id, "label": label})
	})
	if err != nil {
		return model.Session{}, "", err
	}
	return token, secretToken, nil
}

// RevokeToken ends one of a bot's tokens and closes, at once, any live
// connection opened with it.
func (s *Service) RevokeToken(ctx context.Context, actor model.Account, id model.AccountID, token model.SessionID) error {
	if err := s.manage(ctx, actor); err != nil {
		return err
	}
	if _, _, err := s.find(ctx, id); err != nil {
		return err
	}
	ses, err := s.store.Sessions().Get(ctx, token)
	if err != nil || ses.AccountID != id || ses.Kind != model.SessionBot || !ses.RevokedAt.IsZero() {
		return model.ErrNotFound
	}
	err = s.store.Tx(ctx, func(tx store.Tx) error {
		if err := tx.Sessions().Revoke(ctx, token, s.now()); err != nil {
			return err
		}
		return audit(ctx, tx, actor, "token.revoked", "session", string(token), map[string]any{"kind": "bot", "bot_id": id})
	})
	if err != nil {
		return err
	}
	s.hub.CloseSession(id, token, closeCredential, "this token has been revoked")
	return nil
}
