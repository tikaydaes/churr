package auth

import (
	"context"
	"time"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/perm"
	"github.com/tikaydaes/churr/internal/secret"
	"github.com/tikaydaes/churr/internal/store"
)

// sessionStarted tells the account's own connections that a new session
// began, so a person sees at once when someone else gets in, and above all
// when a recovery session starts (docs/EVENTS.md section 9).
func (s *Service) sessionStarted(ses model.Session) {
	shown := ses
	shown.Current = false
	s.hub.Send(ses.AccountID, s.accountEvent(model.EventSessionCreated, map[string]any{"session": shown}))
}

// sessionsEnded does what follows the revoking of sessions: their live
// connections are closed at once, their devices retired, and the account's
// other connections told.
func (s *Service) sessionsEnded(ctx context.Context, reason string, refs ...store.SessionRef) {
	now := s.now()
	for _, r := range refs {
		s.hub.CloseSession(r.AccountID, r.ID, closeCredential, reason)
		s.wrongProofs.drop(string(r.ID))
		if d, found, err := s.store.Devices().RetireForSession(ctx, r.ID, now); err != nil {
			s.log.Warn("a device could not be retired", "err", err)
		} else if found {
			s.hub.Send(r.AccountID, s.accountEvent(model.EventDeviceRetired, map[string]any{"device": d}))
		}
		s.hub.Send(r.AccountID, s.accountEvent(model.EventSessionEnded, map[string]any{"session": map[string]any{"id": r.ID}}))
	}
}

// webSession makes a browser session row and its cookie token.
func (s *Service) webSession(ctx context.Context, a store.Areas, ses *model.Session) (string, error) {
	token, hash := secret.NewToken(secret.PrefixSession)
	ses.Kind = model.SessionWeb
	if ses.CreatedAt.IsZero() {
		ses.CreatedAt = s.now()
	}
	ses.LastSeenAt = ses.CreatedAt
	if err := a.Sessions().Create(ctx, ses, hash, nil); err != nil {
		return "", err
	}
	ses.Current = true
	return token, nil
}

// ListSessions returns the caller's sessions of every kind, with the one
// making the request marked. Token hashes are never returned.
func (s *Service) ListSessions(ctx context.Context, who Who) ([]model.Session, error) {
	if err := person(who); err != nil {
		return nil, err
	}
	list, err := s.store.Sessions().ForAccount(ctx, who.Account.ID, s.now())
	for i := range list {
		list[i].Current = list[i].ID == who.Session.ID
	}
	return list, err
}

// ownSession finds one of the caller's own live sessions; anybody else's
// could not be known to exist, so it is not found.
func (s *Service) ownSession(ctx context.Context, who Who, id model.SessionID) (model.Session, error) {
	ses, err := s.store.Sessions().Get(ctx, id)
	if err != nil {
		return ses, err
	}
	now := s.now()
	if ses.AccountID != who.Account.ID || !ses.RevokedAt.IsZero() || (!ses.ExpiresAt.IsZero() && !ses.ExpiresAt.After(now)) {
		return ses, model.ErrNotFound
	}
	return ses, nil
}

// EndSession ends one of the caller's own sessions.
func (s *Service) EndSession(ctx context.Context, who Who, id model.SessionID) error {
	if err := person(who); err != nil {
		return err
	}
	ses, err := s.ownSession(ctx, who, id)
	if err != nil {
		return err
	}
	err = s.store.Tx(ctx, func(tx store.Tx) error {
		if err := tx.Sessions().Revoke(ctx, ses.ID, s.now()); err != nil {
			return err
		}
		if ses.Kind == model.SessionPersonal {
			return s.audit(ctx, tx, who.Account.ID, "token.revoked", "session", string(ses.ID), map[string]any{"kind": ses.Kind, "label": ses.Label})
		}
		return nil
	})
	if err != nil {
		return err
	}
	s.sessionsEnded(ctx, "signed out", store.SessionRef{ID: ses.ID, AccountID: ses.AccountID})
	return nil
}

// EndOtherSessions ends every session of the caller except the one making
// the request, tokens included.
func (s *Service) EndOtherSessions(ctx context.Context, who Who) error {
	if err := complete(who); err != nil {
		return err
	}
	ids, err := s.store.Sessions().RevokeForAccount(ctx, who.Account.ID, who.Session.ID, s.now())
	if err != nil {
		return err
	}
	s.sessionsEnded(ctx, "signed out", refsOf(who.Account.ID, ids)...)
	return nil
}

func refsOf(account model.AccountID, ids []model.SessionID) []store.SessionRef {
	out := make([]store.SessionRef, len(ids))
	for i, id := range ids {
		out[i] = store.SessionRef{ID: id, AccountID: account}
	}
	return out
}

// ListTokens returns the caller's personal tokens.
func (s *Service) ListTokens(ctx context.Context, who Who) ([]model.Token, error) {
	if err := complete(who); err != nil {
		return nil, err
	}
	list, err := s.store.Sessions().ForAccount(ctx, who.Account.ID, s.now())
	out := []model.Token{}
	for _, ses := range list {
		if ses.Kind == model.SessionPersonal {
			out = append(out, model.Token{ID: ses.ID, Label: ses.Label, CreatedAt: ses.CreatedAt, LastSeenAt: ses.LastSeenAt, ExpiresAt: ses.ExpiresAt})
		}
	}
	return out, err
}

// RevokePersonalToken revokes one of the caller's personal tokens.
func (s *Service) RevokePersonalToken(ctx context.Context, who Who, id model.SessionID) error {
	if err := complete(who); err != nil {
		return err
	}
	ses, err := s.ownSession(ctx, who, id)
	if err != nil {
		return err
	}
	if ses.Kind != model.SessionPersonal {
		return model.ErrNotFound
	}
	return s.EndSession(ctx, who, id)
}

// SignOutAccount revokes every session and token of an account. Admins do
// this to members, guests and bots; only the owner to an admin.
func (s *Service) SignOutAccount(ctx context.Context, who Who, target model.AccountID) error {
	if err := complete(who); err != nil {
		return err
	}
	if err := s.admin(ctx, who, perm.UseAdminConsole); err != nil {
		return err
	}
	acc, err := s.store.Accounts().Get(ctx, target)
	if err != nil {
		return err
	}
	if err := perm.CheckOnAccount(who.Account, perm.SignOutAccount, acc); err != nil {
		return err
	}
	var ids []model.SessionID
	err = s.store.Tx(ctx, func(tx store.Tx) error {
		var err error
		if ids, err = tx.Sessions().RevokeForAccount(ctx, target, "", s.now()); err != nil {
			return err
		}
		return s.audit(ctx, tx, who.Account.ID, "sessions.revoked_all", "account", string(target), map[string]any{"sessions": len(ids)})
	})
	if err != nil {
		return err
	}
	s.sessionsEnded(ctx, "signed out by an admin", refsOf(target, ids)...)
	return nil
}

// SignOutEveryone revokes every browser and app session but the caller's
// own, so a changed sign-in policy takes hold at once. Personal and bot
// tokens are left alone.
func (s *Service) SignOutEveryone(ctx context.Context, who Who) error {
	if err := complete(who); err != nil {
		return err
	}
	if err := s.admin(ctx, who, perm.ChangeSettings); err != nil {
		return err
	}
	if err := s.fresh(who); err != nil {
		return err
	}
	var refs []store.SessionRef
	err := s.store.Tx(ctx, func(tx store.Tx) error {
		var err error
		if refs, err = tx.Sessions().RevokeKinds(ctx, []string{model.SessionWeb, model.SessionApp}, who.Session.ID, s.now()); err != nil {
			return err
		}
		return s.audit(ctx, tx, who.Account.ID, "sessions.revoked_all", "server", "everyone", map[string]any{"sessions": len(refs)})
	})
	if err != nil {
		return err
	}
	s.sessionsEnded(ctx, "signed out by an admin", refs...)
	return nil
}

// ssoLife is how long a session from company sign-in lasts: fixed from the
// sign-in (setting sessions.sso_seconds).
func (s *Service) ssoLife(ctx context.Context, a store.Areas) (time.Duration, error) {
	n, err := store.SettingIntOr(ctx, a, SettingSsoSeconds, ssoSeconds)
	return time.Duration(n) * time.Second, err
}
