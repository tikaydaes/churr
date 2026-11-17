package people

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/tikaydaes/churr/internal/live"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/perm"
	"github.com/tikaydaes/churr/internal/secret"
	"github.com/tikaydaes/churr/internal/store"
)

// claimLinkLife is how long a claim link for an imported account works.
const claimLinkLife = 30 * 24 * time.Hour

// admin asks perm whether the actor may use the admin operations.
func (s *Service) admin(ctx context.Context, actor model.Account) error {
	return s.own(ctx, actor, perm.UseAdminConsole)
}

// adminAccounts puts accounts in the shape admins see: the public shape,
// without presence or status (an admin sees those like anyone, where they
// share a conversation), plus sign-in details. Never sessions.
func (s *Service) adminAccounts(ctx context.Context, list []model.Account) ([]model.AdminAccount, error) {
	ids := make([]model.AccountID, len(list))
	for i, a := range list {
		ids[i] = a.ID
	}
	details, err := s.store.Accounts().AdminDetails(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := make([]model.AdminAccount, len(list))
	for i, a := range list {
		d := details[a.ID]
		out[i] = model.AdminAccount{Account: s.show(a, false), PasskeyCount: d.PasskeyCount, HasAccessKey: d.HasAccessKey, HasTotp: d.HasTotp,
			ProviderIDs: d.ProviderIDs, LastSeenAt: d.LastSeenAt, ClaimedAtOut: a.ClaimedAt, RemovedByID: a.RemovedBy, Banned: d.Banned}
		if out[i].ProviderIDs == nil {
			out[i].ProviderIDs = []string{}
		}
		if out[i].GroupIDs, err = s.store.Groups().OfAccount(ctx, a.ID); err != nil {
			return nil, err
		}
		if out[i].MayApprove, err = s.store.Approvals().IsApprover(ctx, a.ID); err != nil {
			return nil, err
		}
		if p, found, err := s.store.Accounts().Promotion(ctx, a.ID); err != nil {
			return nil, err
		} else if found {
			out[i].OwnerFrom, out[i].OwnerFromByID = p.EffectiveAt, p.ByID
		}
		if a.ClaimedAt.IsZero() && !a.Removed() {
			ap, found, err := s.store.Approvals().Latest(ctx, a.ID, model.ApprovalRegistration)
			if err != nil {
				return nil, err
			}
			out[i].Applying = found && ap.State == model.ApprovalWaiting
		}
	}
	return out, nil
}

// AdminList lists every account with sign-in details, for admins.
func (s *Service) AdminList(ctx context.Context, actor model.Account, f store.AccountFilter, p store.Page) ([]model.AdminAccount, string, error) {
	if err := s.admin(ctx, actor); err != nil {
		return nil, "", err
	}
	switch f.State {
	case "", "active", "removed", "unclaimed":
	default:
		return nil, "", model.Invalid("state", "state is active, removed or unclaimed.")
	}
	if f.Role != "" && !f.Role.Valid() {
		return nil, "", model.Invalid("role", "That is not a role.")
	}
	// The admin list is of everything: former members too, unless a state
	// was asked for.
	f.IncludeRemoved = true
	f.SharedWith, f.LocalOrSharedWith = "", ""
	list, next, err := s.store.Accounts().List(ctx, f, p)
	if err != nil {
		return nil, "", err
	}
	out, err := s.adminAccounts(ctx, list)
	return out, next, err
}

// AdminGet returns one account with sign-in details, for admins.
func (s *Service) AdminGet(ctx context.Context, actor model.Account, id model.AccountID) (model.AdminAccount, error) {
	if err := s.admin(ctx, actor); err != nil {
		return model.AdminAccount{}, err
	}
	a, err := s.store.Accounts().Get(ctx, id)
	if err != nil {
		return model.AdminAccount{}, err
	}
	out, err := s.adminAccounts(ctx, []model.Account{a})
	if err != nil {
		return model.AdminAccount{}, err
	}
	return out[0], nil
}

// SetRole changes an account's role between owner, admin, member and
// guest. An Owner makes another account an Owner too, or steps down while
// another Owner remains (ruling R99); an Admin moves an account between
// guest and member (ruling R104). The account moves in or out of the
// built-in groups, and so in or out of the channels they are in. Every
// change is in the audit log; an owner made or stepping down is written as
// such.
//
// While the instance holds new Owners back (store.SettingNewOwnerDelay
// above 0), making an Owner records a promotion that takes effect when the
// delay has run (ApplyPromotions), and the account keeps its role until
// then. Giving it any role in the meantime, its own included, withdraws
// the promotion; that is how any Owner stops it.
func (s *Service) SetRole(ctx context.Context, actor model.Account, id model.AccountID, role model.Role) (model.AdminAccount, error) {
	set, err := s.perm.Settings(ctx)
	if err != nil {
		return model.AdminAccount{}, err
	}
	// Asked first with nobody in particular, so only an owner or an
	// admin learns which identifiers exist.
	if err := perm.CheckServer(actor, perm.ChangeRole, set); err != nil {
		return model.AdminAccount{}, err
	}
	target, err := s.store.Accounts().Get(ctx, id)
	if err != nil {
		return model.AdminAccount{}, err
	}
	if err := perm.CheckSetRole(actor, target, role, set); err != nil {
		return model.AdminAccount{}, err
	}
	waiting, pending, err := s.store.Accounts().Promotion(ctx, id)
	if err != nil {
		return model.AdminAccount{}, err
	}
	switch {
	case role == model.RoleOwner && target.Role != model.RoleOwner:
		delay, err := store.SettingIntOr(ctx, s.store, store.SettingNewOwnerDelay, 0)
		if err != nil {
			return model.AdminAccount{}, err
		}
		if pending {
			// Asked again while it waits: it keeps its time.
			break
		}
		if delay > 0 {
			now := s.now()
			p := model.OwnerPromotion{AccountID: id, ByID: actor.ID, CreatedAt: now, EffectiveAt: now.Add(time.Duration(delay) * time.Hour)}
			err := s.store.Tx(ctx, func(tx store.Tx) error {
				if err := tx.Accounts().PromoteLater(ctx, p); err != nil {
					return err
				}
				return s.audit(ctx, tx, actor, "owner.promotion_waiting", "account", string(id), map[string]any{"from": target.Role, "effective_at": p.EffectiveAt})
			})
			if err != nil {
				return model.AdminAccount{}, err
			}
			s.TellOwners(ctx, "owner_promotion_waiting", actor.ID, string(id))
			break
		}
		if err := s.applyRole(ctx, actor.ID, target, role, nil); err != nil {
			return model.AdminAccount{}, err
		}
	default:
		if pending {
			if actor.Role != model.RoleOwner {
				return model.AdminAccount{}, model.ErrForbidden.With("Only an owner withdraws a promotion to owner.")
			}
			err := s.store.Tx(ctx, func(tx store.Tx) error {
				if _, err := tx.Accounts().WithdrawPromotion(ctx, id); err != nil {
					return err
				}
				return s.audit(ctx, tx, actor, "owner.promotion_withdrawn", "account", string(id), map[string]any{"made_by": waiting.ByID})
			})
			if err != nil {
				return model.AdminAccount{}, err
			}
		}
		if target.Role != role {
			if err := s.applyRole(ctx, actor.ID, target, role, nil); err != nil {
				return model.AdminAccount{}, err
			}
		}
	}
	return s.AdminGet(ctx, actor, id)
}

// applyRole gives target the role, with its memberships through the
// built-in groups, writes it to the audit log, and tells whoever must know.
func (s *Service) applyRole(ctx context.Context, actor model.AccountID, target model.Account, role model.Role, detail map[string]any) error {
	id := target.ID
	action := "account.role_changed"
	switch {
	case role == model.RoleOwner:
		action = "owner.made"
	case target.Role == model.RoleOwner:
		action = "owner.stepped_down"
	}
	d := map[string]any{"from": target.Role, "to": role}
	for k, v := range detail {
		d[k] = v
	}
	err := s.changeMembership(ctx, actor, "group", func(tx store.Tx) (store.MembershipResult, error) {
		_, res, err := tx.Accounts().SetRole(ctx, id, role)
		if err != nil {
			return res, err
		}
		return res, tx.Audit().Append(ctx, model.AuditEntry{At: s.now(), ActorID: string(actor), ActorKind: "account",
			Action: action, TargetKind: "account", TargetID: string(id), Detail: d})
	})
	if err != nil {
		return err
	}
	s.forget(id)
	if a, err := s.store.Accounts().Get(ctx, id); err == nil {
		s.tellProfile(ctx, a)
	}
	switch action {
	case "owner.made":
		s.TellOwners(ctx, "owner_made", actor, string(id))
	case "owner.stepped_down":
		s.TellOwners(ctx, "owner_stepped_down", actor, string(id))
	}
	return nil
}

// ApplyPromotions makes an Owner of each account whose promotion's time has
// come, as made by whoever asked for it, if that account is still an active
// Owner and the account is still active; otherwise the promotion lapses,
// written to the audit log. The sweep runs it.
func (s *Service) ApplyPromotions(ctx context.Context) error {
	due, err := s.store.Accounts().DuePromotions(ctx, s.now())
	if err != nil {
		return err
	}
	for _, p := range due {
		if _, err := s.store.Accounts().WithdrawPromotion(ctx, p.AccountID); err != nil {
			return err
		}
		by, err := s.store.Accounts().Get(ctx, p.ByID)
		if err != nil && !errors.Is(err, model.ErrNotFound) {
			return err
		}
		target, err := s.store.Accounts().Get(ctx, p.AccountID)
		if err != nil && !errors.Is(err, model.ErrNotFound) {
			return err
		}
		if by.Role != model.RoleOwner || !by.Active() || !target.Active() || target.Role == model.RoleOwner {
			if err := s.store.Audit().Append(ctx, model.AuditEntry{At: s.now(), ActorKind: "server", Action: "owner.promotion_lapsed",
				TargetKind: "account", TargetID: string(p.AccountID), Detail: map[string]any{"made_by": p.ByID}}); err != nil {
				return err
			}
			continue
		}
		if err := s.applyRole(ctx, p.ByID, target, model.RoleOwner, map[string]any{"waited_since": p.CreatedAt}); err != nil {
			return err
		}
	}
	return nil
}

// OnOwnersTold sets what is told of what every Owner must hear of.
func (s *Service) OnOwnersTold(fn func(ctx context.Context, kind string, by model.AccountID, about string)) {
	s.ownersTold = fn
}

// TellOwners tells every Owner of the instance of something an Owner must
// hear of (review finding C1): an Owner made or stepping down, a promotion
// waiting, the export started or downloaded, an override used. by is who
// did it; about the account or conversation it was done to, if any.
func (s *Service) TellOwners(ctx context.Context, kind string, by model.AccountID, about string) {
	if s.ownersTold != nil {
		s.ownersTold(ctx, kind, by, about)
	}
}

// removed does what follows an account's removal once the transaction has
// committed: its connections are closed, the people who could see it are
// told, and its avatar's content is deleted.
func (s *Service) removed(ctx context.Context, r store.RemovedAccount, audience []model.AccountID) {
	id := r.Account.ID
	s.hub.CloseAccount(id, live.CloseRemoved, "this account has been removed")
	s.removeBlobs(ctx, r.BlobKeys)
	s.forget(id)
	s.viewsMu.Lock()
	delete(s.views, id)
	s.viewsMu.Unlock()
	e := s.event(model.EventAccountRemoved, map[string]any{"account": s.show(r.Account, false)})
	for _, to := range audience {
		s.hub.Send(to, e)
	}
}

// Remove removes an account (docs/FEATURES.md section 19): it ends its
// sessions and tokens, takes it out of every group and conversation with a
// member.left in each, and keeps its messages and its username. An admin
// removes members, guests and bots; only the owner removes an admin; nobody
// removes the owner. Removing an account that is already removed changes
// nothing.
func (s *Service) Remove(ctx context.Context, actor model.Account, id model.AccountID) error {
	if err := s.admin(ctx, actor); err != nil {
		return err
	}
	target, err := s.store.Accounts().Get(ctx, id)
	if err != nil {
		return err
	}
	if err := perm.CheckOnAccount(actor, perm.RemoveAccount, target); err != nil {
		return err
	}
	if target.Removed() {
		return nil
	}
	audience := s.audience(ctx, id)
	var r store.RemovedAccount
	err = s.changeMembership(ctx, actor.ID, "account_removed", func(tx store.Tx) (store.MembershipResult, error) {
		var err error
		if r, err = tx.Accounts().Remove(ctx, id, actor.ID, s.now()); err != nil {
			return r.MembershipResult, err
		}
		return r.MembershipResult, s.audit(ctx, tx, actor, "account.removed", "account", string(id),
			map[string]any{"username": target.Username, "role": target.Role})
	})
	if err != nil {
		return err
	}
	s.removed(ctx, r, audience)
	return nil
}

// NewBan is what Ban takes: an account, or a sign-in identity that has no
// account.
type NewBan struct {
	AccountID  model.AccountID
	ProviderID string
	Subject    string
	Reason     string
}

// Ban bans an account or a sign-in identity. Banning an account is a
// removal plus rows that stop the same person coming back: one for each
// company identity it had, or one with neither. The first row is returned.
func (s *Service) Ban(ctx context.Context, actor model.Account, in NewBan) (model.Ban, error) {
	if err := s.admin(ctx, actor); err != nil {
		return model.Ban{}, err
	}
	in.Reason = strings.TrimSpace(in.Reason)
	if utf8.RuneCountInString(in.Reason) > 500 {
		return model.Ban{}, model.Invalid("reason", "A reason is at most 500 characters.")
	}
	identity := in.ProviderID != "" || in.Subject != ""
	switch {
	case in.AccountID != "" && identity:
		return model.Ban{}, model.BadRequest("Give `account_id`, or `provider_id` and `subject`, not both.")
	case in.AccountID == "" && (in.ProviderID == "" || in.Subject == ""):
		return model.Ban{}, model.BadRequest("Give `account_id`, or both `provider_id` and `subject`.")
	}
	now := s.now()
	if in.AccountID == "" {
		b := model.Ban{ProviderID: in.ProviderID, Subject: in.Subject, Reason: in.Reason, CreatedByID: actor.ID, CreatedAt: now}
		err := s.store.Tx(ctx, func(tx store.Tx) error {
			if err := tx.Accounts().BanIdentity(ctx, &b); err != nil {
				return err
			}
			return s.audit(ctx, tx, actor, "ban.created", "ban", b.ID, map[string]any{"provider_id": b.ProviderID, "reason": b.Reason})
		})
		return b, err
	}
	target, err := s.store.Accounts().Get(ctx, in.AccountID)
	if err != nil {
		return model.Ban{}, err
	}
	if err := perm.CheckOnAccount(actor, perm.BanAccount, target); err != nil {
		return model.Ban{}, err
	}
	audience := s.audience(ctx, target.ID)
	var bans []model.Ban
	var r store.RemovedAccount
	err = s.changeMembership(ctx, actor.ID, "account_removed", func(tx store.Tx) (store.MembershipResult, error) {
		var err error
		if bans, r, err = tx.Accounts().Ban(ctx, target.ID, actor.ID, in.Reason, now); err != nil {
			return r.MembershipResult, err
		}
		return r.MembershipResult, s.audit(ctx, tx, actor, "account.banned", "account", string(target.ID),
			map[string]any{"username": target.Username, "role": target.Role, "reason": in.Reason})
	})
	if err != nil {
		return model.Ban{}, err
	}
	if !target.Removed() {
		s.removed(ctx, r, audience)
	}
	return bans[0], nil
}

// Bans lists bans, newest first, for admins.
func (s *Service) Bans(ctx context.Context, actor model.Account, p store.Page) ([]model.Ban, string, error) {
	if err := s.admin(ctx, actor); err != nil {
		return nil, "", err
	}
	return s.store.Accounts().ListBans(ctx, p)
}

// LiftBan lifts a ban. The person may then register again and gets a new
// account; the removed account is not brought back.
func (s *Service) LiftBan(ctx context.Context, actor model.Account, id string) error {
	if err := s.admin(ctx, actor); err != nil {
		return err
	}
	return s.store.Tx(ctx, func(tx store.Tx) error {
		if err := tx.Accounts().LiftBan(ctx, id, s.now()); err != nil {
			return err
		}
		return s.audit(ctx, tx, actor, "ban.lifted", "ban", id, map[string]any{})
	})
}

// ClaimLink makes a fresh claim link for an unclaimed imported account and
// withdraws the earlier one. It works once and for 30 days
// (docs/FEATURES.md section 25.5).
func (s *Service) ClaimLink(ctx context.Context, actor model.Account, id model.AccountID) (model.Invite, error) {
	if err := s.admin(ctx, actor); err != nil {
		return model.Invite{}, err
	}
	target, err := s.store.Accounts().Get(ctx, id)
	if err != nil {
		return model.Invite{}, err
	}
	// An applicant's account waits for approval, not for a claim: a
	// claim link would hand it to whoever opened it.
	if _, found, err := s.store.Approvals().Latest(ctx, id, model.ApprovalRegistration); err != nil {
		return model.Invite{}, err
	} else if found {
		return model.Invite{}, model.ErrConflict.With("That account is someone's request to join. Approve or decline it.")
	}
	now := s.now()
	role := model.RoleMember
	if target.Role == model.RoleGuest {
		role = model.RoleGuest
	}
	inv := model.Invite{Code: secret.NewInviteCode(), Role: role, ClaimsAccountID: id, MaxUses: 1,
		ExpiresAt: now.Add(claimLinkLife), CreatedByID: actor.ID, CreatedAt: now}
	err = s.store.Tx(ctx, func(tx store.Tx) error {
		if err := tx.Accounts().ClaimLinkFor(ctx, &inv); err != nil {
			return err
		}
		return s.audit(ctx, tx, actor, "claim_link.created", "account", string(id), map[string]any{"expires_at": inv.ExpiresAt})
	})
	if err != nil {
		return model.Invite{}, err
	}
	return s.withURL(inv), nil
}
