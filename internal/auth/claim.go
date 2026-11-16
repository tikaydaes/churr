package auth

import (
	"context"
	"errors"

	"github.com/tikaydaes/churr/internal/chat"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/people"
	"github.com/tikaydaes/churr/internal/store"
)

// Claim links (docs/AUTH.md section 4.4). The Slack importer makes accounts
// nobody has signed in to, and one claim link per person: an invite that
// names the account. Whoever opens the link and makes a passkey takes the
// account over, with its imported history. No account is made; the one
// there gets its first passkey, and a new username if the person chose one
// that is free. Claim links work in every registration mode.

// errClaimGone is the answer when a claim link's account can no longer be
// claimed: someone has claimed it, or it was removed.
var errClaimGone = model.ErrNotFound.With("That invite can no longer be used.")

// claimable returns the account a claim link names, if it can still be
// claimed.
func claimable(ctx context.Context, a store.Areas, id model.AccountID) (model.Account, error) {
	acc, err := a.Accounts().Get(ctx, id)
	if errors.Is(err, model.ErrNotFound) {
		return acc, errClaimGone
	}
	if err != nil {
		return acc, err
	}
	if !acc.Local() || acc.Removed() || !acc.ClaimedAt.IsZero() {
		return acc, errClaimGone
	}
	return acc, nil
}

// ClaimsUsername returns the username of the imported account a claim link
// takes over, for the page that shows the link's holder what they are about
// to claim. It is "" for an ordinary invite.
func (s *Service) ClaimsUsername(ctx context.Context, inv model.Invite) (string, error) {
	if inv.ClaimsAccountID == "" {
		return "", nil
	}
	acc, err := claimable(ctx, s.store, inv.ClaimsAccountID)
	if err != nil {
		return "", err
	}
	return acc.Username, nil
}

// prepareClaim checks a claim link and the names, and returns what the
// imported account will be claimed with. An empty username or display name
// keeps the imported one.
func (s *Service) prepareClaim(ctx context.Context, inv model.Invite, username, displayName string) (ceremony, error) {
	acc, err := claimable(ctx, s.store, inv.ClaimsAccountID)
	if err != nil {
		return ceremony{}, err
	}
	if username == "" {
		username = acc.Username
	}
	if displayName == "" {
		displayName = acc.DisplayName
	}
	if username, displayName, err = people.CleanNames(username, displayName); err != nil {
		return ceremony{}, err
	}
	if username != acc.Username {
		if err := s.nameFree(ctx, username); err != nil {
			return ceremony{}, err
		}
	}
	return ceremony{kind: "register", username: username, displayName: displayName, role: acc.Role, inviteID: inv.ID, claims: acc.ID}, nil
}

// claimTx finishes a claim by passkey or access key: the invite is used,
// the account gets its names, its user handle (with a passkey) and
// `claimed_at`, joins @everyone and so the starting channels, and gets its
// first way in, recovery codes with a passkey, and a session. Everything
// happens together or not at all.
func (s *Service) claimTx(ctx context.Context, tx store.Tx, rec *chat.Recorder, c ceremony, way firstWay, label string) (SignIn, error) {
	var out SignIn
	now := s.now()
	// Counted with an update that only succeeds while the link is usable:
	// two people with the same link cannot both win.
	if err := tx.Invites().Use(ctx, c.inviteID, now); err != nil {
		return out, err
	}
	if _, err := claimable(ctx, tx, c.claims); err != nil {
		return out, err
	}
	if _, err := tx.Accounts().Update(ctx, c.claims, func(a *model.Account) error {
		a.Username, a.DisplayName = c.username, c.displayName
		if way.passkey != nil {
			a.WebAuthnHandle = c.handle
		}
		return nil
	}); err != nil {
		return out, err
	}
	joined, err := tx.Accounts().Claim(ctx, c.claims, now)
	if err != nil {
		return out, err
	}
	if out.RecoveryCodes, err = way.addTx(ctx, tx, c.claims, now); err != nil {
		return out, err
	}
	if out.Account, err = tx.Accounts().Get(ctx, c.claims); err != nil {
		return out, err
	}
	if out.Session, out.Token, err = s.newSessionByTx(ctx, tx, c.claims, label, way.how()); err != nil {
		return out, err
	}
	if err := s.audit(ctx, tx, c.claims, "account.claimed", "account", string(c.claims), map[string]any{"by": "link", "how": way.how(), "invite_id": c.inviteID}); err != nil {
		return out, err
	}
	return out, rec.Membership(ctx, joined, "", "")
}

// claimByProvider claims the imported account a claim link names for
// someone who has just been identified by a provider and holds the link:
// the same take-over as by passkey, with the provider's identity linked in
// place of a passkey.
func (s *Service) claimByProvider(ctx context.Context, p model.SsoProvider, who asserted, inv model.Invite, label string) (SignIn, error) {
	var in SignIn
	err := s.chat.Tx(ctx, func(tx store.Tx, rec *chat.Recorder) error {
		now := s.now()
		if err := tx.Invites().Use(ctx, inv.ID, now); err != nil {
			return err
		}
		if _, err := claimable(ctx, tx, inv.ClaimsAccountID); err != nil {
			return err
		}
		joined, err := tx.Accounts().Claim(ctx, inv.ClaimsAccountID, now)
		if err != nil {
			return err
		}
		if err := rec.Membership(ctx, joined, "", ""); err != nil {
			return err
		}
		acc, err := tx.Accounts().Get(ctx, inv.ClaimsAccountID)
		if err != nil {
			return err
		}
		if err := tx.Identities().Link(ctx, model.Identity{ProviderID: p.ID, Subject: who.subject, AccountID: acc.ID, LinkedAt: now}); err != nil {
			return err
		}
		if err := s.audit(ctx, tx, acc.ID, "account.claimed", "account", string(acc.ID),
			map[string]any{"by": "link", "invite_id": inv.ID, "provider": p.Name}); err != nil {
			return err
		}
		in, err = s.signInTx(ctx, tx, rec, p, acc, who, label)
		return err
	})
	if err != nil {
		if errors.Is(err, model.ErrNotFound) {
			return in, model.ErrForbidden.With("That invite can no longer be used.")
		}
		return in, err
	}
	s.signedInBySso(ctx, &in)
	return in, nil
}
