package bots

import (
	"context"
	"encoding/base64"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/tikaydaes/churr/internal/chat"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/people"
	"github.com/tikaydaes/churr/internal/perm"
	"github.com/tikaydaes/churr/internal/secret"
	"github.com/tikaydaes/churr/internal/store"
)

// A webhook from the request for one to its revocation (docs/BOTS.md
// section 9). Posting through one is in intake.go.

const (
	maxWaiting = 3
	maxPurpose = 250
	// hookNameChars are what the generated username of a webhook's
	// account is drawn from.
	hookNameChars = "abcdefghijklmnopqrstuvwxyz0123456789"
)

// hookUsername makes the username of the account behind a webhook: `hook-`
// and eight random characters.
func hookUsername() string {
	raw := secret.RandomBytes(8)
	b := []byte("hook-")
	for _, r := range raw {
		// 256 is not a multiple of 36, so a few characters are a little
		// likelier than others. The name is a label, not a secret.
		b = append(b, hookNameChars[int(r)%len(hookNameChars)])
	}
	return string(b)
}

// approvers are the accounts that may decide requests for a conversation:
// the admins, and its managers if the setting hands them the decision.
func (s *Service) approvers(ctx context.Context, a store.Areas, st perm.State) ([]model.AccountID, error) {
	admins, err := a.Groups().Builtin(ctx, model.GroupAdmins)
	if err != nil {
		return nil, err
	}
	ids, err := a.Groups().MemberIDs(ctx, admins.ID)
	if err != nil {
		return nil, err
	}
	if st.Settings.WebhooksManagersMayApprove {
		managers, err := a.Members().Managers(ctx, st.Conversation.ID)
		if err != nil {
			return nil, err
		}
		ids = append(ids, managers...)
	}
	return ids, nil
}

// tell sends a webhook event to accounts, each once.
func (s *Service) tell(eventType string, w model.Webhook, to ...model.AccountID) {
	e := model.Event{Type: eventType, At: s.now(), Payload: map[string]any{"webhook": w}}
	seen := map[model.AccountID]bool{}
	for _, id := range to {
		if id != "" && !seen[id] {
			seen[id] = true
			s.hub.Send(id, e)
		}
	}
}

// RequestWebhook asks for a webhook in a named channel the actor is in. The
// request is stored waiting, together with the account that will stand
// behind the webhook, which is in no channel yet. A person may have at most
// three requests waiting. An actor who may approve requests there approves
// their own in the same step.
func (s *Service) RequestWebhook(ctx context.Context, actor model.Account, conv model.ConversationID, name, purpose string) (model.Webhook, error) {
	st, err := s.perm.Can(ctx, actor, perm.RequestWebhook, conv)
	if err != nil {
		return model.Webhook{}, notFound(err)
	}
	name, ok := model.CleanDisplayName(name)
	if !ok {
		return model.Webhook{}, model.Invalid("name", "A webhook's name is 1 to 64 characters with no control characters.")
	}
	purpose = strings.TrimSpace(purpose)
	if utf8.RuneCountInString(purpose) > maxPurpose || strings.IndexFunc(purpose, func(r rune) bool { return unicode.IsControl(r) && r != '\n' }) >= 0 {
		return model.Webhook{}, model.Invalid("purpose", "A purpose is at most 250 characters.")
	}
	w := model.Webhook{ConversationID: st.Conversation.ID, Name: name, Purpose: purpose, RequestedByID: actor.ID, RequestedAt: s.now()}
	err = s.store.Tx(ctx, func(tx store.Tx) error {
		n, err := tx.Bots().CountWaiting(ctx, actor.ID)
		if err != nil {
			return err
		}
		if n >= maxWaiting {
			return model.ErrConflict.With("You already have three webhook requests waiting.")
		}
		// The account is made with the request, under a generated
		// username, and shows the name the requester gave.
		acc, _, err := s.people.CreateTx(ctx, tx, people.NewAccount{Username: hookUsername(), DisplayName: name, Role: model.RoleBot})
		if err != nil {
			return err
		}
		if err := tx.Bots().Create(ctx, store.BotInfo{AccountID: acc.ID, Description: purpose, CreatedBy: actor.ID, WebhookOnly: true}); err != nil {
			return err
		}
		w.BotID = acc.ID
		if err := tx.Bots().CreateWebhook(ctx, &w); err != nil {
			return err
		}
		return audit(ctx, tx, actor, "webhook.requested", "webhook", string(w.ID),
			map[string]any{"conversation_id": w.ConversationID, "name": name})
	})
	if err != nil {
		return model.Webhook{}, err
	}
	if perm.CheckDecideWebhook(st, true) == nil {
		return s.ApproveWebhook(ctx, actor, w.ID)
	}
	to, err := s.approvers(ctx, s.store, st)
	if err != nil {
		return w, err
	}
	s.tell(eventWebhookRequested, w, to...)
	return w, nil
}

// loadWebhook reads a webhook and the actor's state in its conversation.
func (s *Service) loadWebhook(ctx context.Context, actor model.Account, id model.WebhookID) (model.Webhook, perm.State, error) {
	w, err := s.store.Bots().Webhook(ctx, id)
	if err != nil {
		return w, perm.State{}, notFound(err)
	}
	st, err := s.perm.Load(ctx, actor, w.ConversationID)
	return w, st, notFound(err)
}

// ApproveWebhook approves a waiting request: the webhook's account joins
// the channel at level member, with the usual notice. The requester is told
// by an event, and can then collect the address.
func (s *Service) ApproveWebhook(ctx context.Context, actor model.Account, id model.WebhookID) (model.Webhook, error) {
	w, st, err := s.loadWebhook(ctx, actor, id)
	if err != nil {
		return w, err
	}
	if err := perm.CheckDecideWebhook(st, true); err != nil {
		return w, err
	}
	err = s.chat.Tx(ctx, func(tx store.Tx, rec *chat.Recorder) error {
		if w, err = tx.Bots().DecideWebhook(ctx, id, model.WebhookApproved, actor.ID, s.now()); err != nil {
			return err
		}
		res, err := tx.Members().AddDirect(ctx, w.ConversationID, w.BotID, model.LevelMember, s.now())
		if err != nil {
			return err
		}
		if err := rec.Membership(ctx, res, actor.ID, ""); err != nil {
			return err
		}
		return audit(ctx, tx, actor, "webhook.approved", "webhook", string(id), map[string]any{"conversation_id": w.ConversationID})
	})
	if err != nil {
		return w, err
	}
	to, err := s.approvers(ctx, s.store, st)
	if err != nil {
		return w, err
	}
	s.tell(eventWebhookDecided, w, append(to, w.RequestedByID)...)
	return w, nil
}

// DeclineWebhook declines a waiting request and removes the account that
// was waiting with it. No reason is recorded beyond the audit log entry.
func (s *Service) DeclineWebhook(ctx context.Context, actor model.Account, id model.WebhookID) (model.Webhook, error) {
	w, st, err := s.loadWebhook(ctx, actor, id)
	if err != nil {
		return w, err
	}
	if err := perm.CheckDecideWebhook(st, false); err != nil {
		return w, err
	}
	err = s.chat.Tx(ctx, func(tx store.Tx, rec *chat.Recorder) error {
		if w, err = tx.Bots().DecideWebhook(ctx, id, model.WebhookDeclined, actor.ID, s.now()); err != nil {
			return err
		}
		if err := s.removeHookAccountTx(ctx, tx, rec, actor, w); err != nil {
			return err
		}
		// Removing an account revokes its webhooks; this one was
		// declined, and stays that.
		if err := tx.Bots().MarkDeclined(ctx, id); err != nil {
			return err
		}
		return audit(ctx, tx, actor, "webhook.declined", "webhook", string(id), map[string]any{"conversation_id": w.ConversationID})
	})
	if err != nil {
		return w, err
	}
	to, err := s.approvers(ctx, s.store, st)
	if err != nil {
		return w, err
	}
	s.tell(eventWebhookDecided, w, append(to, w.RequestedByID)...)
	return w, nil
}

// removeHookAccountTx removes the account behind a webhook, with a
// member.left in its channel if it had joined. Its messages stay, shown
// under its name as a former member.
func (s *Service) removeHookAccountTx(ctx context.Context, tx store.Tx, rec *chat.Recorder, by model.Account, w model.Webhook) error {
	acc, err := tx.Accounts().Get(ctx, w.BotID)
	if err != nil {
		return err
	}
	if acc.Removed() {
		return nil
	}
	removed, err := tx.Accounts().Remove(ctx, w.BotID, by.ID, s.now())
	if err != nil {
		return err
	}
	rec.RemoveBlobs(removed.BlobKeys...)
	return rec.Membership(ctx, removed.MembershipResult, by.ID, "account_removed")
}

// revokeTx revokes a webhook inside a transaction: its state becomes
// revoked, its token hash and signing secret are cleared, and its account
// is removed.
func (s *Service) revokeTx(ctx context.Context, tx store.Tx, rec *chat.Recorder, by model.Account, w model.Webhook, why string) error {
	if _, err := tx.Bots().RevokeWebhook(ctx, w.ID); err != nil {
		return err
	}
	if err := s.removeHookAccountTx(ctx, tx, rec, by, w); err != nil {
		return err
	}
	return audit(ctx, tx, by, "webhook.revoked", "webhook", string(w.ID), map[string]any{"conversation_id": w.ConversationID, "reason": why})
}

// RevokeWebhook withdraws a waiting request or revokes a webhook: the
// account that asked for it, a manager of the channel or an admin may. One
// that is already declined or revoked is left as it is.
func (s *Service) RevokeWebhook(ctx context.Context, actor model.Account, id model.WebhookID) error {
	w, st, err := s.loadWebhook(ctx, actor, id)
	if err != nil {
		return err
	}
	if err := perm.CheckManageWebhook(st, w.RequestedByID); err != nil {
		return err
	}
	if w.State != model.WebhookWaiting && w.State != model.WebhookApproved {
		return nil
	}
	return s.chat.Tx(ctx, func(tx store.Tx, rec *chat.Recorder) error {
		return s.revokeTx(ctx, tx, rec, actor, w, "revoked")
	})
}

// newAddress makes a webhook's token and signing secret and stores the hash
// of the one and the other sealed. Both are returned, to be shown once.
func (s *Service) newAddress(ctx context.Context, a store.Areas, w model.Webhook, first bool) (address, signingSecret string, err error) {
	token, hash := secret.NewToken(secret.PrefixHook)
	// 256 random bits in base64url with no prefix: it is what a sender
	// such as GitHub is given to sign with, and is never part of an
	// address.
	signingSecret = base64.RawURLEncoding.EncodeToString(secret.RandomBytes(32))
	sealed, err := s.keys.Seal([]byte(signingSecret), "webhooks", "signing_secret", string(w.ID))
	if err != nil {
		return "", "", err
	}
	if err := a.Bots().SetWebhookSecret(ctx, w.ID, hash, sealed, first); err != nil {
		return "", "", err
	}
	return s.origin + "/hooks/" + token, signingSecret, nil
}

// CollectWebhookAddress makes the address and the signing secret of an
// approved webhook and returns them, once. Only the account that asked for
// the webhook can, and until it has, the webhook cannot post. A second call
// is a conflict: a lost address is replaced, not recovered.
func (s *Service) CollectWebhookAddress(ctx context.Context, actor model.Account, id model.WebhookID) (address, signingSecret string, err error) {
	w, st, err := s.loadWebhook(ctx, actor, id)
	if err != nil {
		return "", "", err
	}
	if w.RequestedByID != actor.ID || !actor.Role.IsFullMember() {
		// Whoever could see the webhook in the channel's list is told
		// why; anyone else is told there is none.
		if perm.CheckListWebhooks(st) != nil {
			return "", "", model.ErrNotFound
		}
		return "", "", model.ErrForbidden.With("Only the person who asked for a webhook can collect its address.")
	}
	if w.State != model.WebhookApproved {
		return "", "", model.ErrConflict.With("Only an approved webhook has an address.")
	}
	if !st.IsMember() {
		// A requester who left the channel, or was removed from it,
		// before collecting loses the webhook.
		err := s.chat.Tx(ctx, func(tx store.Tx, rec *chat.Recorder) error {
			return s.revokeTx(ctx, tx, rec, actor, w, "the requester is no longer in the channel")
		})
		if err != nil {
			return "", "", err
		}
		return "", "", model.ErrConflict.With("You are no longer in that channel, so the webhook has been revoked.")
	}
	err = s.store.Tx(ctx, func(tx store.Tx) error {
		if address, signingSecret, err = s.newAddress(ctx, tx, w, true); err != nil {
			return err
		}
		return audit(ctx, tx, actor, "webhook.collected", "webhook", string(id), map[string]any{"conversation_id": w.ConversationID})
	})
	return address, signingSecret, err
}

// ResetWebhookAddress replaces a webhook's address and signing secret
// together. The old ones stop working at once and the new ones are shown,
// once, to whoever reset it: the requester, a manager of the channel or an
// admin.
func (s *Service) ResetWebhookAddress(ctx context.Context, actor model.Account, id model.WebhookID) (address, signingSecret string, err error) {
	w, st, err := s.loadWebhook(ctx, actor, id)
	if err != nil {
		return "", "", err
	}
	if err := perm.CheckManageWebhook(st, w.RequestedByID); err != nil {
		return "", "", err
	}
	if w.State != model.WebhookApproved {
		return "", "", model.ErrConflict.With("Only an approved webhook has an address.")
	}
	if !w.HasSecret {
		// The first address goes to the requester, and to nobody else.
		return "", "", model.ErrConflict.With("The address has not been collected yet.")
	}
	err = s.store.Tx(ctx, func(tx store.Tx) error {
		if address, signingSecret, err = s.newAddress(ctx, tx, w, false); err != nil {
			return err
		}
		return audit(ctx, tx, actor, "webhook.reset", "webhook", string(id), map[string]any{"conversation_id": w.ConversationID})
	})
	return address, signingSecret, err
}

// ChannelWebhooks lists the webhooks and waiting requests of a channel for
// one of its members: name, who asked, who approved and when each was last
// used. Nobody sees an address or a signing secret.
func (s *Service) ChannelWebhooks(ctx context.Context, actor model.Account, conv model.ConversationID) ([]model.Webhook, error) {
	st, err := s.perm.Load(ctx, actor, conv)
	if err != nil {
		return nil, notFound(err)
	}
	if err := perm.CheckListWebhooks(st); err != nil {
		return nil, err
	}
	return s.store.Bots().WebhooksOf(ctx, st.Conversation.ID)
}

// Webhooks lists every webhook and request on the server for an admin,
// newest first, or only those in one state.
func (s *Service) Webhooks(ctx context.Context, actor model.Account, state string, p store.Page) ([]model.Webhook, string, error) {
	if err := s.manage(ctx, actor); err != nil {
		return nil, "", err
	}
	switch state {
	case "", model.WebhookWaiting, model.WebhookApproved, model.WebhookDeclined, model.WebhookRevoked:
	default:
		return nil, "", model.Invalid("state", "A state is waiting, approved, declined or revoked.")
	}
	return s.store.Bots().ListWebhooks(ctx, state, p)
}
