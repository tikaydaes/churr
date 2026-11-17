package chat

import (
	"context"
	"time"
	"unicode/utf8"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/perm"
	"github.com/tikaydaes/churr/internal/store"
)

// Channel bans (docs/FEATURES.md section 19.1, ruling R72). A ban is a
// removal that keeps the account out: the store gives a banned account no
// member row however it would arrive, perm hides the channel from it, and
// adding it is refused here. The removal goes through the Recorder like any
// other, so the member caches and live fan-out drop the account with the
// same transaction.

// notBanned refuses to add an account that is banned from the channel.
func (s *Service) notBanned(ctx context.Context, a store.Areas, conv model.ConversationID, account model.AccountID) error {
	_, banned, err := a.Moderation().ChannelBan(ctx, conv, account, s.now())
	if err != nil {
		return err
	}
	if banned {
		return model.ErrForbidden.With("That person is banned from this channel. Lift the ban first.")
	}
	return nil
}

// BanFromChannel bans an account from a channel: it is taken out if it is in,
// and kept out until the ban is lifted or, with seconds above 0, runs out.
// A new ban replaces one in force.
func (s *Service) BanFromChannel(ctx context.Context, actor model.Account, id model.ConversationID, account model.AccountID, reason string, seconds int64) (model.ChannelBan, error) {
	st, err := s.seen(ctx, actor, id)
	if err != nil {
		return model.ChannelBan{}, err
	}
	if err := perm.Check(st, perm.BanMember); err != nil {
		return model.ChannelBan{}, err
	}
	if utf8.RuneCountInString(reason) > model.MaxReportReason {
		return model.ChannelBan{}, model.Invalid("reason", "A reason is at most 1,000 characters.")
	}
	if seconds != 0 && (seconds < model.MinChannelBanSeconds || seconds > model.MaxChannelBanSeconds) {
		return model.ChannelBan{}, model.Invalid("seconds", "A ban with an end lasts from a minute to 366 days.")
	}
	target, err := s.store.Accounts().Get(ctx, account)
	if err != nil || !target.Active() || s.perm.CanSeeAccount(ctx, actor, target) != nil {
		return model.ChannelBan{}, model.ErrNotFound.With("There is no such account.")
	}
	row, _, err := s.store.Members().Get(ctx, id, account)
	if err != nil {
		return model.ChannelBan{}, err
	}
	if err := perm.CheckChannelBan(st, target, row.Level); err != nil {
		return model.ChannelBan{}, err
	}
	now := s.now()
	b := model.ChannelBan{ConversationID: id, AccountID: account, Reason: reason, CreatedByID: actor.ID, CreatedAt: now}
	if seconds > 0 {
		b.ExpiresAt = now.Add(time.Duration(seconds) * time.Second)
	}
	err = s.Tx(ctx, func(tx store.Tx, rec *Recorder) error {
		if err := tx.Moderation().BanFromChannel(ctx, &b); err != nil {
			return err
		}
		_, member, err := tx.Members().Get(ctx, id, account)
		if err != nil {
			return err
		}
		if member {
			// Taking away the direct level recomputes the row, and the
			// ban now in force leaves none, groups or not.
			res, err := tx.Members().RemoveDirect(ctx, id, account)
			if err != nil {
				return err
			}
			if err := rec.Membership(ctx, res, actor.ID, "banned"); err != nil {
				return err
			}
		}
		detail := map[string]any{"conversation_id": id, "ban_id": b.ID}
		if !b.ExpiresAt.IsZero() {
			detail["expires_at"] = b.ExpiresAt
		}
		return s.audit(ctx, tx, st.Conversation, actor.ID, "channel_ban.created", "account", string(account), detail)
	})
	if err != nil {
		return model.ChannelBan{}, hide(err)
	}
	s.banEvent(ctx, model.EventChannelBanCreated, st.Conversation, b)
	return b, nil
}

// LiftChannelBan lifts the ban on an account in a channel. Whoever could
// have set it may: its moderators and managers. An admin sees the bans in
// the console and does not lift them, being no manager (ruling R99). The
// account is not brought back.
func (s *Service) LiftChannelBan(ctx context.Context, actor model.Account, id model.ConversationID, account model.AccountID) error {
	st, err := s.perm.Load(ctx, actor, id)
	if err != nil {
		return hide(err)
	}
	if err := perm.CheckChannelUnban(st); err != nil {
		return err
	}
	now := s.now()
	var b model.ChannelBan
	err = s.store.Tx(ctx, func(tx store.Tx) error {
		var found bool
		var err error
		if b, found, err = tx.Moderation().LiftChannelBan(ctx, id, account, now); err != nil {
			return err
		}
		if !found {
			return model.ErrNotFound.With("That account is not banned from this channel.")
		}
		return s.audit(ctx, tx, st.Conversation, actor.ID, "channel_ban.lifted", "account", string(account),
			map[string]any{"conversation_id": id, "ban_id": b.ID})
	})
	if err != nil {
		return err
	}
	s.banEvent(ctx, model.EventChannelBanLifted, st.Conversation, b)
	return nil
}

// ChannelBans lists the bans in force in a channel, for whoever could set
// one, and for an admin from the console in a named channel.
func (s *Service) ChannelBans(ctx context.Context, actor model.Account, id model.ConversationID) ([]model.ChannelBan, error) {
	st, err := s.perm.Load(ctx, actor, id)
	if err != nil {
		return nil, hide(err)
	}
	if err := perm.Check(st, perm.ListBans); err != nil {
		if !st.Conversation.Named() || perm.Check(st, perm.AdminView) != nil {
			return nil, err
		}
	}
	return s.store.Moderation().ChannelBans(ctx, id, s.now())
}

// AllChannelBans lists the bans in force in every named channel, for the
// admin console, beside the server's own bans.
func (s *Service) AllChannelBans(ctx context.Context, actor model.Account) ([]model.ChannelBan, error) {
	if err := s.perm.CanAdminister(ctx, actor, perm.UseAdminConsole); err != nil {
		return nil, err
	}
	return s.store.Moderation().ChannelBans(ctx, "", s.now())
}

// banEvent tells the banned account, once, and the channel's moderators and managers,
// whose settings list the bans. It carries the channel's name, since the one
// banned can no longer look the channel up.
func (s *Service) banEvent(ctx context.Context, eventType string, conv model.Conversation, b model.ChannelBan) {
	to, err := s.store.Members().Keepers(ctx, conv.ID)
	if err != nil {
		s.log.Warn("could not work out who to tell about a channel ban", "err", err)
	}
	s.tell(append(to, b.AccountID, b.CreatedByID), eventType, map[string]any{"ban": b, "conversation_name": conv.Name})
}
