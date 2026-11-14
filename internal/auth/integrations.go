package auth

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/perm"
	"github.com/tikaydaes/churr/internal/secret"
	"github.com/tikaydaes/churr/internal/store"
)

// AllowAvailability is the one thing there is to tick on an integration.
const AllowAvailability = "availability"

// integrationAdmin is an account that may manage integrations.
func (s *Service) integrationAdmin(ctx context.Context, who Who) error {
	return s.admin(ctx, who, perm.ManageIntegrations)
}

// ListIntegrations returns every integration, for admins.
func (s *Service) ListIntegrations(ctx context.Context, who Who) ([]model.Integration, error) {
	if err := s.integrationAdmin(ctx, who); err != nil {
		return nil, err
	}
	return s.store.Integrations().List(ctx)
}

// CreateIntegration makes a server integration and returns its token, which
// is shown once.
func (s *Service) CreateIntegration(ctx context.Context, who Who, name string, allows []string) (model.Integration, string, error) {
	if err := s.integrationAdmin(ctx, who); err != nil {
		return model.Integration{}, "", err
	}
	name, ok := cleanName(name, 64)
	if !ok {
		return model.Integration{}, "", model.Invalid("name", "An integration needs a name of 1 to 64 characters.")
	}
	ticked := []string{}
	for _, a := range allows {
		if a != AllowAvailability {
			return model.Integration{}, "", model.Invalid("allows", "The one thing an integration can be allowed is availability.")
		}
		if !contains(ticked, a) {
			ticked = append(ticked, a)
		}
	}
	if err := s.fresh(who); err != nil {
		return model.Integration{}, "", err
	}
	token, hash := secret.NewToken(secret.PrefixIntegration)
	in := model.Integration{Name: name, Allows: ticked, CreatedByID: who.Account.ID, CreatedAt: s.now()}
	err := s.store.Tx(ctx, func(tx store.Tx) error {
		if err := tx.Integrations().Create(ctx, &in, hash); err != nil {
			return err
		}
		return s.audit(ctx, tx, who.Account.ID, "integration.created", "integration", in.ID, map[string]any{"name": in.Name, "allows": in.Allows})
	})
	return in, token, err
}

// RevokeIntegration ends an integration; its token stops working.
func (s *Service) RevokeIntegration(ctx context.Context, who Who, id string) error {
	if err := s.integrationAdmin(ctx, who); err != nil {
		return err
	}
	in, err := s.store.Integrations().Get(ctx, id)
	if err != nil {
		return err
	}
	return s.store.Tx(ctx, func(tx store.Tx) error {
		if err := tx.Integrations().Revoke(ctx, id, s.now()); err != nil {
			return err
		}
		return s.audit(ctx, tx, who.Account.ID, "integration.revoked", "integration", id, map[string]any{"name": in.Name})
	})
}

// Integration finds the integration behind a request. A `churr_int_` token
// is accepted only in the Authorization header of integration operations,
// and no other kind of token is accepted there.
func (s *Service) Integration(r *http.Request, addr string) (model.Integration, error) {
	h := r.Header.Get("Authorization")
	if h == "" {
		return model.Integration{}, model.ErrUnauthenticated
	}
	token, ok := strings.CutPrefix(h, "Bearer ")
	token = strings.TrimSpace(token)
	if !ok || secret.TokenPrefix(token) != secret.PrefixIntegration {
		return model.Integration{}, s.bad(addr)
	}
	in, err := s.store.Integrations().ByTokenHash(r.Context(), secret.HashToken(token))
	if errors.Is(err, model.ErrNotFound) {
		return model.Integration{}, s.bad(addr)
	}
	if err != nil {
		return model.Integration{}, err
	}
	if !in.RevokedAt.IsZero() {
		return model.Integration{}, model.ErrUnauthenticated
	}
	// Each integration has its own allowance of requests.
	if s.intLimit != nil {
		if res := s.intLimit.TakeIntegration(in.ID); !res.OK {
			return model.Integration{}, model.RateLimited(res.RetryAfterSeconds())
		}
	}
	return in, nil
}

// used records one use of an integration: every use is written to the audit
// log with the integration as the actor.
func (s *Service) used(ctx context.Context, in model.Integration, what string, target model.AccountID) {
	now := s.now()
	if err := s.store.Integrations().Used(ctx, in.ID, now); err != nil {
		s.log.Warn("an integration's use could not be recorded", "err", err)
	}
	err := s.store.Audit().Append(ctx, model.AuditEntry{At: now, ActorID: in.ID, ActorKind: "integration", Action: "integration.used",
		TargetKind: "account", TargetID: string(target), Detail: map[string]any{"name": in.Name, "did": what}})
	if err != nil {
		s.log.Warn("an integration's use could not be written to the audit log", "err", err)
	}
}

func mayAvailability(in model.Integration) error {
	if !contains(in.Allows, AllowAvailability) {
		return model.ErrForbidden.With("This integration is not allowed to set availability.")
	}
	return nil
}

// FindAccountForIntegration finds a local person by username, or by the
// identifier they have at a company sign-in provider. Bots, removed accounts
// and accounts from other servers cannot be found.
func (s *Service) FindAccountForIntegration(ctx context.Context, in model.Integration, username, provider, subject string) (model.Account, error) {
	if err := mayAvailability(in); err != nil {
		return model.Account{}, err
	}
	var acc model.Account
	var err error
	switch {
	case username != "" && provider == "" && subject == "":
		acc, err = s.store.Accounts().ByUsername(ctx, strings.ToLower(strings.TrimSpace(username)))
	case username == "" && provider != "" && subject != "":
		if !model.ValidID(provider) {
			return model.Account{}, model.Invalid("provider_id", "That is not an identifier.")
		}
		var id model.Identity
		if id, err = s.store.Identities().BySubject(ctx, provider, subject); err == nil {
			acc, err = s.store.Accounts().Get(ctx, id.AccountID)
		}
	default:
		return model.Account{}, model.BadRequest("Give username, or provider_id and subject.")
	}
	if err != nil {
		return model.Account{}, err
	}
	if !acc.Active() || !isPerson(acc.Role) {
		return model.Account{}, model.ErrNotFound
	}
	s.used(ctx, in, "found an account", acc.ID)
	return acc, nil
}

// SetAvailabilityForAccount sets or clears "in a meeting" or "out of office"
// on an account. An integration clears only availability it set itself.
func (s *Service) SetAvailabilityForAccount(ctx context.Context, in model.Integration, id model.AccountID, value string, until model.Time) (model.Account, error) {
	if err := mayAvailability(in); err != nil {
		return model.Account{}, err
	}
	acc, err := s.store.Accounts().Get(ctx, id)
	if err != nil {
		return model.Account{}, err
	}
	if !acc.Active() || !isPerson(acc.Role) {
		return model.Account{}, model.ErrNotFound
	}
	now := s.now()
	switch value {
	case "":
		until = 0
	case model.AvailabilityMeeting:
		if until.IsZero() {
			return model.Account{}, model.Invalid("until", "A meeting needs the time it ends.")
		}
		if until.Sub(now) > 24*time.Hour {
			return model.Account{}, model.Invalid("until", "A meeting ends at most 24 hours ahead.")
		}
	case model.AvailabilityOutOfOffice:
	default:
		return model.Account{}, model.Invalid("availability", "availability is meeting, out_of_office or null.")
	}
	if !until.IsZero() && !until.After(now) {
		return model.Account{}, model.Invalid("until", "That time has already passed.")
	}
	shown, err := s.people.SetAvailabilityFor(ctx, acc, in.ID, value, until)
	if err != nil {
		return model.Account{}, err
	}
	what := "set availability to " + value
	if value == "" {
		what = "cleared availability"
	}
	s.used(ctx, in, what, acc.ID)
	return shown, nil
}
