package people

import (
	"context"

	"github.com/tikaydaes/churr/internal/model"
)

// This file belongs to the sign-in area: it is what a server integration
// (docs/AUTH.md section 12.4) needs of the people service.

// SetAvailabilityFor sets or clears "in a meeting" or "out of office" on an
// account for a server integration, and tells the people who can see the
// account. An empty value clears, and clears only what this same
// integration set: a value the person or another integration set is left
// alone and the answer is model.ErrConflict. The account is returned as
// others see it.
func (s *Service) SetAvailabilityFor(ctx context.Context, target model.Account, integrationID, value string, until model.Time) (model.Account, error) {
	by := "integration:" + integrationID
	now := s.now()
	a, err := s.change(ctx, target, false, func(a *model.Account) error {
		if value != "" {
			setAvailability(a, value, until, by)
			return nil
		}
		live := a.AvailabilityIs != "" && (a.AvailabilityEnd.IsZero() || a.AvailabilityEnd.After(now))
		switch {
		case a.AvailabilityBy == by:
			setAvailability(a, "", 0, "")
		case live:
			return model.ErrConflict.With("That availability was not set by this integration, so it is left alone.")
		}
		return nil
	})
	if err != nil {
		return model.Account{}, err
	}
	return s.show(a, true), nil
}
