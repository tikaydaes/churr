package people

import (
	"context"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/perm"
)

// PresetInput is the contract's StatusPresetInput. Nil fields are absent:
// on a new preset they take their defaults, on a change they stay.
type PresetInput struct {
	Emoji              *string
	Text               *string
	DurationSeconds    *int64
	PauseNotifications *bool
	AppearAway         *bool
	Availability       *string
	Position           *int64
}

// apply writes the given fields onto a preset and checks the result.
func (in PresetInput) apply(p *model.StatusPreset) error {
	if in.Emoji != nil {
		p.Emoji = *in.Emoji
	}
	if in.Text != nil {
		p.Text = *in.Text
	}
	if in.DurationSeconds != nil {
		// 0 means "until cleared", which is stored as no duration.
		p.DurationSeconds = nil
		if *in.DurationSeconds != 0 {
			d := *in.DurationSeconds
			p.DurationSeconds = &d
		}
	}
	if in.PauseNotifications != nil {
		p.PauseNotifications = *in.PauseNotifications
	}
	if in.AppearAway != nil {
		p.AppearAway = *in.AppearAway
	}
	if in.Availability != nil {
		p.Availability = *in.Availability
	}
	if in.Position != nil {
		p.Position = *in.Position
	}
	var err error
	if p.Emoji, p.Text, err = cleanStatus(p.Emoji, p.Text); err != nil {
		return err
	}
	if p.DurationSeconds != nil && (*p.DurationSeconds < 0 || *p.DurationSeconds > maxSeconds) {
		return model.Invalid("duration_seconds", "A duration is a positive number of seconds, or 0 for until cleared.")
	}
	switch p.Availability {
	case "", model.AvailabilityMeeting, model.AvailabilityOutOfOffice:
	default:
		return model.Invalid("availability", "availability is meeting or out_of_office.")
	}
	if p.Position < 0 || p.Position > 1_000_000 {
		return model.Invalid("position", "position is 0 or more.")
	}
	return nil
}

// presetAction is what perm is asked for a personal or a community preset.
func presetAction(community bool) perm.ServerAction {
	if community {
		return perm.ManageCommunityReplies
	}
	return perm.OwnPersonalThings
}

// Presets lists the community's status presets and the caller's own.
// Presets have no event; clients fetch the list when the picker opens.
func (s *Service) Presets(ctx context.Context, actor model.Account) ([]model.StatusPreset, error) {
	if !actor.Active() {
		return nil, model.ErrUnauthenticated
	}
	return s.store.StatusPresets().List(ctx, actor.ID)
}

// CreatePreset saves a status preset: the caller's own, or with community
// set one for everyone, which only admins make.
func (s *Service) CreatePreset(ctx context.Context, actor model.Account, community bool, in PresetInput) (model.StatusPreset, error) {
	if err := s.own(ctx, actor, presetAction(community)); err != nil {
		return model.StatusPreset{}, err
	}
	p := model.StatusPreset{OwnerID: actor.ID}
	if community {
		p.OwnerID = ""
	}
	if err := in.apply(&p); err != nil {
		return p, err
	}
	if err := s.store.StatusPresets().Create(ctx, &p); err != nil {
		return p, err
	}
	if community {
		if err := s.audit(ctx, s.store, actor, "status_preset.created", "status_preset", p.ID, map[string]any{"text": p.Text}); err != nil {
			return p, err
		}
	}
	return p, nil
}

// preset loads a preset the caller may change: a community one for an
// admin through the admin operations, a personal one for its owner. Any
// other does not exist for the caller.
func (s *Service) preset(ctx context.Context, actor model.Account, community bool, id string) (model.StatusPreset, error) {
	if err := s.own(ctx, actor, presetAction(community)); err != nil {
		return model.StatusPreset{}, err
	}
	p, err := s.store.StatusPresets().Get(ctx, id)
	if err != nil {
		return p, err
	}
	if p.Community != community || (!community && p.OwnerID != actor.ID) {
		return p, model.ErrNotFound
	}
	return p, nil
}

// UpdatePreset changes a preset. Statuses already set from it stay as they
// are.
func (s *Service) UpdatePreset(ctx context.Context, actor model.Account, community bool, id string, in PresetInput) (model.StatusPreset, error) {
	if _, err := s.preset(ctx, actor, community, id); err != nil {
		return model.StatusPreset{}, err
	}
	return s.store.StatusPresets().Update(ctx, id, func(p *model.StatusPreset) error { return in.apply(p) })
}

// DeletePreset deletes a preset.
func (s *Service) DeletePreset(ctx context.Context, actor model.Account, community bool, id string) error {
	p, err := s.preset(ctx, actor, community, id)
	if err != nil {
		return err
	}
	if err := s.store.StatusPresets().Delete(ctx, id); err != nil {
		return err
	}
	if community {
		return s.audit(ctx, s.store, actor, "status_preset.deleted", "status_preset", id, map[string]any{"text": p.Text})
	}
	return nil
}
