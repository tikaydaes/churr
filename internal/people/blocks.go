package people

import (
	"context"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/perm"
)

// Blocks lists the accounts the caller has blocked.
func (s *Service) Blocks(ctx context.Context, actor model.Account) ([]model.AccountID, error) {
	if !actor.Active() {
		return nil, model.ErrUnauthenticated
	}
	return s.store.Blocks().List(ctx, actor.ID)
}

// Block blocks an account for the caller (docs/FEATURES.md section 20).
// The blocked account is told nothing. With blocking off for the server the
// answer is feature_off.
func (s *Service) Block(ctx context.Context, actor model.Account, target model.AccountID) error {
	if err := s.own(ctx, actor, perm.CreateBlock); err != nil {
		return err
	}
	if target == actor.ID {
		return model.BadRequest("You cannot block yourself.")
	}
	t, err := s.store.Accounts().Get(ctx, target)
	if err != nil {
		return err
	}
	// An account the caller may not know exists cannot be blocked either.
	if err := s.perm.CanSeeAccount(ctx, actor, t); err != nil {
		return err
	}
	if err := s.store.Blocks().Add(ctx, actor.ID, target, s.now()); err != nil {
		return err
	}
	// Its messages no longer count in the caller's numbers.
	s.forget(actor.ID)
	s.hub.Send(actor.ID, s.event(model.EventBlockAdded, map[string]any{"account_id": target}))
	return nil
}

// Unblock removes a block. From then on the account's messages count and
// notify like anyone's, including any that are still unread.
func (s *Service) Unblock(ctx context.Context, actor model.Account, target model.AccountID) error {
	if err := s.own(ctx, actor, perm.OwnPersonalThings); err != nil {
		return err
	}
	if _, err := s.store.Accounts().Get(ctx, target); err != nil {
		return err
	}
	if err := s.store.Blocks().Remove(ctx, actor.ID, target); err != nil {
		return err
	}
	s.forget(actor.ID)
	s.hub.Send(actor.ID, s.event(model.EventBlockRemoved, map[string]any{"account_id": target}))
	return nil
}
