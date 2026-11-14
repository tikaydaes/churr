package people

import (
	"context"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/notify"
	"github.com/tikaydaes/churr/internal/perm"
)

// maxEmoji bounds the status emoji: one emoji, or the short code of a
// custom one.
const maxEmoji = 64

// maxSeconds bounds a duration: ten years is longer than any status.
const maxSeconds = 10 * 365 * 24 * 3600

// Until is how a request says when something ends: at a time, after a
// duration, or with neither until turned off.
type Until struct {
	At              model.Time
	DurationSeconds *int64
}

// end works the end out. The zero time means "until turned off".
func (u Until) end(now model.Time) (model.Time, error) {
	if !u.At.IsZero() && u.DurationSeconds != nil {
		return 0, model.BadRequest("Give `until` or `duration_seconds`, not both.")
	}
	if u.DurationSeconds != nil {
		if *u.DurationSeconds <= 0 || *u.DurationSeconds > maxSeconds {
			return 0, model.Invalid("duration_seconds", "A duration is a positive number of seconds.")
		}
		return now.Add(time.Duration(*u.DurationSeconds) * time.Second), nil
	}
	if !u.At.IsZero() && !u.At.After(now) {
		return 0, model.Invalid("until", "That time has already passed.")
	}
	return u.At, nil
}

// NewStatus is what SetStatus takes: the fields, or a preset.
type NewStatus struct {
	Emoji              string
	Text               string
	Until              Until
	PauseNotifications bool
	AppearAway         bool
	PresetID           string
}

// cleanStatus checks the text and emoji of a status or preset.
func cleanStatus(emoji, text string) (string, string, error) {
	emoji, text = strings.TrimSpace(emoji), strings.TrimSpace(text)
	if utf8.RuneCountInString(text) > model.MaxStatusText || strings.IndexFunc(text, unicode.IsControl) >= 0 {
		return "", "", model.Invalid("text", "A status is at most 100 characters.")
	}
	if utf8.RuneCountInString(emoji) > maxEmoji || strings.IndexFunc(emoji, func(r rune) bool { return unicode.IsControl(r) || unicode.IsSpace(r) }) >= 0 {
		return "", "", model.Invalid("emoji", "That is not one emoji.")
	}
	if emoji == "" && text == "" {
		return "", "", model.Invalid("text", "A status needs an emoji or some text.")
	}
	return emoji, text, nil
}

// change updates the caller's account and announces what others now see.
// fn changes the stored columns. tellSelf also sends me.updated, for things
// only the account sees.
func (s *Service) change(ctx context.Context, actor model.Account, tellSelf bool, fn func(a *model.Account) error) (model.Account, error) {
	if err := s.own(ctx, actor, perm.OwnProfile); err != nil {
		return actor, err
	}
	var before view
	a, err := s.store.Accounts().Update(ctx, actor.ID, func(a *model.Account) error {
		before = s.viewOf(*a, s.now())
		return fn(a)
	})
	if err != nil {
		return actor, err
	}
	s.announce(ctx, a, &before)
	if tellSelf {
		s.tellMe(ctx, a)
	}
	return a, nil
}

// setAvailability writes an availability on an account row. The pause
// skipped for the last meeting ends with it (decision F5).
func setAvailability(a *model.Account, value string, end model.Time, by string) {
	a.AvailabilityIs, a.AvailabilityEnd, a.AvailabilityBy = value, end, by
	a.PrefsJSON = setPref(a.PrefsJSON, "meeting_pause_skip_until", nil)
}

// SetStatus sets the caller's status, replacing the last one whole. With a
// preset it copies the preset's emoji, text and switches, ends after the
// preset's duration, and sets the preset's availability with the same end
// (docs/FEATURES.md sections 3.3 and 3.4).
func (s *Service) SetStatus(ctx context.Context, actor model.Account, in NewStatus) (model.Me, error) {
	now := s.now()
	var availability string
	if in.PresetID != "" {
		if in.Emoji != "" || in.Text != "" || !in.Until.At.IsZero() || in.Until.DurationSeconds != nil || in.PauseNotifications || in.AppearAway {
			return model.Me{}, model.BadRequest("Give the fields of a status, or `preset_id`, not both.")
		}
		p, err := s.store.StatusPresets().Get(ctx, in.PresetID)
		if err != nil {
			return model.Me{}, err
		}
		// Someone else's personal preset does not exist for the caller.
		if !p.Community && p.OwnerID != actor.ID {
			return model.Me{}, model.ErrNotFound
		}
		in = NewStatus{Emoji: p.Emoji, Text: p.Text, PauseNotifications: p.PauseNotifications, AppearAway: p.AppearAway,
			Until: Until{DurationSeconds: p.DurationSeconds}}
		availability = p.Availability
	}
	emoji, text, err := cleanStatus(in.Emoji, in.Text)
	if err != nil {
		return model.Me{}, err
	}
	end, err := in.Until.end(now)
	if err != nil {
		return model.Me{}, err
	}
	a, err := s.change(ctx, actor, false, func(a *model.Account) error {
		a.StatusEmoji, a.StatusText, a.StatusUntil, a.StatusDND, a.StatusAway = emoji, text, end, in.PauseNotifications, in.AppearAway
		if availability != "" {
			setAvailability(a, availability, end, "self")
		}
		return nil
	})
	if err != nil {
		return model.Me{}, err
	}
	return s.Me(ctx, a)
}

// ClearStatus clears the caller's status. Its two switches end with it.
func (s *Service) ClearStatus(ctx context.Context, actor model.Account) error {
	_, err := s.change(ctx, actor, false, func(a *model.Account) error {
		a.StatusEmoji, a.StatusText, a.StatusUntil, a.StatusDND, a.StatusAway = "", "", 0, false, false
		return nil
	})
	return err
}

// SetAvailability sets "in a meeting" or "out of office" for the caller,
// with an optional end. The last write wins.
func (s *Service) SetAvailability(ctx context.Context, actor model.Account, value string, until model.Time) (model.Me, error) {
	if value != model.AvailabilityMeeting && value != model.AvailabilityOutOfOffice {
		return model.Me{}, model.Invalid("value", "value is meeting or out_of_office.")
	}
	end, err := Until{At: until}.end(s.now())
	if err != nil {
		return model.Me{}, err
	}
	a, err := s.change(ctx, actor, false, func(a *model.Account) error {
		setAvailability(a, value, end, "self")
		return nil
	})
	if err != nil {
		return model.Me{}, err
	}
	return s.Me(ctx, a)
}

// ClearAvailability clears the caller's availability, whoever set it.
func (s *Service) ClearAvailability(ctx context.Context, actor model.Account) error {
	_, err := s.change(ctx, actor, false, func(a *model.Account) error {
		setAvailability(a, "", 0, "")
		return nil
	})
	return err
}

// SetDnd turns the manual do not disturb on: for a duration, until a time,
// or with neither until turned off. It is independent of quiet hours:
// neither shortens the other. The answer lists every source that is on.
func (s *Service) SetDnd(ctx context.Context, actor model.Account, u Until) (model.Dnd, error) {
	now := s.now()
	end, err := u.end(now)
	if err != nil {
		return model.Dnd{}, err
	}
	a, err := s.change(ctx, actor, false, func(a *model.Account) error {
		a.DNDUntil = &end
		return nil
	})
	return notify.DND(a, s.now()), err
}

// ClearDnd makes the account available now (ruling R37). It clears a manual
// do not disturb, the pause switch of the status that exists, the quiet
// period the moment falls in, and the pause from the current meeting, each
// as narrowly as it can: the status keeps its text and its appear-away
// switch, only this quiet period is skipped, and only this meeting's pause.
func (s *Service) ClearDnd(ctx context.Context, actor model.Account) (model.Dnd, error) {
	a, err := s.change(ctx, actor, false, func(a *model.Account) error {
		now := s.now()
		a.DNDUntil = nil
		if notify.StatusExists(*a, now) {
			a.StatusDND = false
		}
		if p, in := notify.QuietPeriodAt(*a, now); in && p.End.After(a.QuietSkipUntil) {
			a.QuietSkipUntil = p.End
		}
		if notify.AvailabilityNow(*a, now) == model.AvailabilityMeeting && notify.ReadPrefs(*a).MeetingPauses {
			// The meeting's end, or 0 if it has none.
			a.PrefsJSON = setPref(a.PrefsJSON, "meeting_pause_skip_until", int64(a.AvailabilityEnd))
		}
		return nil
	})
	return notify.DND(a, s.now()), err
}

// QuietHours returns the caller's schedule, or nil if there is none.
func (s *Service) QuietHours(actor model.Account) *model.QuietHours { return quietOf(actor) }

// SetQuietHours sets the daily schedule. Times are in the account's time
// zone. A skip that was waiting is dropped: it was for the old schedule.
func (s *Service) SetQuietHours(ctx context.Context, actor model.Account, start, end, days string) (model.QuietHours, error) {
	switch {
	case !notify.ValidClock(start):
		return model.QuietHours{}, model.Invalid("start", "start is a time of day written HH:MM.")
	case !notify.ValidClock(end):
		return model.QuietHours{}, model.Invalid("end", "end is a time of day written HH:MM.")
	case start == end:
		return model.QuietHours{}, model.Invalid("end", "Quiet hours cannot start and end at the same time.")
	case !notify.ValidQuiet(start, end, days):
		return model.QuietHours{}, model.Invalid("days", "days is seven characters of 0 and 1, Monday first.")
	}
	a, err := s.change(ctx, actor, true, func(a *model.Account) error {
		a.QuietStart, a.QuietEnd, a.QuietDays, a.QuietSkipUntil = start, end, days, 0
		return nil
	})
	if err != nil {
		return model.QuietHours{}, err
	}
	return *quietOf(a), nil
}

// RemoveQuietHours switches the schedule off, clearing it and any skip.
func (s *Service) RemoveQuietHours(ctx context.Context, actor model.Account) error {
	_, err := s.change(ctx, actor, true, func(a *model.Account) error {
		a.QuietStart, a.QuietEnd, a.QuietDays, a.QuietSkipUntil = "", "", "", 0
		return nil
	})
	return err
}

var errNoQuietHours = model.ErrNotFound.With("There is no quiet-hours schedule.")

// SkipQuietHours skips the next quiet period that has not started, and only
// that one. A period already under way is not touched; turning do not
// disturb off is what ends that.
func (s *Service) SkipQuietHours(ctx context.Context, actor model.Account) (model.QuietHours, model.Dnd, error) {
	a, err := s.change(ctx, actor, true, func(a *model.Account) error {
		next, ok := notify.NextQuietPeriod(*a, s.now())
		if !ok {
			return errNoQuietHours
		}
		a.QuietSkipUntil = next.End
		return nil
	})
	if err != nil {
		return model.QuietHours{}, model.Dnd{}, err
	}
	return *quietOf(a), notify.DND(a, s.now()), nil
}

// UnskipQuietHours stops skipping the next quiet period. If the person
// ended the period under way by turning do not disturb off, that one stays
// ended.
func (s *Service) UnskipQuietHours(ctx context.Context, actor model.Account) (model.QuietHours, error) {
	a, err := s.change(ctx, actor, true, func(a *model.Account) error {
		if a.QuietStart == "" {
			return errNoQuietHours
		}
		// A skip that reaches past the period under way is cut back to
		// it, so that period stays ended and the next one is quiet again.
		skip := a.QuietSkipUntil
		a.QuietSkipUntil = 0
		if p, in := notify.QuietPeriodAt(*a, s.now()); in && !skip.IsZero() && !p.End.After(skip) {
			a.QuietSkipUntil = p.End
		}
		return nil
	})
	if err != nil {
		return model.QuietHours{}, err
	}
	return *quietOf(a), nil
}

// SetAway makes the caller appear away while active: for a duration, until
// a time, or with neither until turned off.
func (s *Service) SetAway(ctx context.Context, actor model.Account, u Until) (model.Me, error) {
	end, err := u.end(s.now())
	if err != nil {
		return model.Me{}, err
	}
	a, err := s.change(ctx, actor, true, func(a *model.Account) error {
		a.AwayUntil = &end
		return nil
	})
	if err != nil {
		return model.Me{}, err
	}
	return s.Me(ctx, a)
}

// ClearAway stops appearing away.
func (s *Service) ClearAway(ctx context.Context, actor model.Account) error {
	_, err := s.change(ctx, actor, true, func(a *model.Account) error {
		a.AwayUntil = nil
		return nil
	})
	return err
}
