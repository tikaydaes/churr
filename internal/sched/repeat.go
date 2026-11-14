package sched

import (
	"fmt"
	"regexp"
	"time"
	_ "time/tzdata" // the zone names must resolve on a machine with no zone database

	"github.com/tikaydaes/churr/internal/model"
)

var clock = regexp.MustCompile(`^([01][0-9]|2[0-3]):[0-5][0-9]$`)

// The values of Repeat.Every (docs/FEATURES.md section 9.3).
const (
	everyDay   = "day"
	everyWeek  = "week"
	everyMonth = "month"
)

// checkRepeat checks a repeat rule as a client sends it and fills in what
// may be left out: `every: day` with no days means every day.
func checkRepeat(r *model.Repeat) error {
	if !clock.MatchString(r.At) {
		return model.Invalid("repeat.at", "The time of day is written HH:MM.")
	}
	if r.TimeZone == "" {
		return model.Invalid("repeat.time_zone", "A repeat needs a time zone, such as Europe/Dublin.")
	}
	if _, err := time.LoadLocation(r.TimeZone); err != nil || r.TimeZone == "Local" {
		return model.Invalid("repeat.time_zone", "That is not a time zone this server knows.")
	}
	switch r.Every {
	case everyDay:
		if r.Days == "" {
			r.Days = "1111111"
		}
		on := false
		for _, c := range r.Days {
			if c != '0' && c != '1' {
				return model.Invalid("repeat.days", "days is seven of 0 and 1, Monday first.")
			}
			on = on || c == '1'
		}
		if len(r.Days) != 7 || !on {
			return model.Invalid("repeat.days", "days is seven of 0 and 1, Monday first, with at least one day on.")
		}
	case everyWeek, everyMonth:
		if r.Days != "" {
			return model.Invalid("repeat.days", "days goes only with every: day.")
		}
	default:
		return model.Invalid("repeat.every", "A repeat is every day, week or month.")
	}
	return nil
}

// mondayFirst numbers a weekday 1 (Monday) to 7 (Sunday).
func mondayFirst(d time.Weekday) int {
	if d == time.Sunday {
		return 7
	}
	return int(d)
}

// anchor fixes what the first run decides for a weekly or monthly rule: the
// weekday it runs on, or the day of the month.
func anchor(r *model.Repeat, first time.Time) {
	loc, err := time.LoadLocation(r.TimeZone)
	if err != nil {
		return
	}
	first = first.In(loc)
	switch r.Every {
	case everyWeek:
		r.Anchor = mondayFirst(first.Weekday())
	case everyMonth:
		r.Anchor = first.Day()
	default:
		r.Anchor = 0
	}
}

// next returns the first moment after `after` that the rule gives. It is
// worked out on the wall clock of the rule's time zone, so 09:30 stays 09:30
// across a clock change. A weekly or monthly rule with no anchor yet runs
// the next time the clock shows its time, and the caller then anchors it
// there.
func next(r model.Repeat, after time.Time) (time.Time, error) {
	loc, err := time.LoadLocation(r.TimeZone)
	if err != nil {
		return time.Time{}, fmt.Errorf("the time zone %q is not known: %w", r.TimeZone, err)
	}
	var hh, mm int
	if _, err := fmt.Sscanf(r.At, "%d:%d", &hh, &mm); err != nil {
		return time.Time{}, fmt.Errorf("the time of day %q cannot be read", r.At)
	}
	local := after.In(loc)
	y, m, d := local.Date()
	at := func(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, hh, mm, 0, 0, loc) }

	if r.Every == everyMonth && r.Anchor > 0 {
		// The anchor day, or the month's last day if it has no such day.
		for i := 0; i < 14; i++ {
			first := time.Date(y, m+time.Month(i), 1, 0, 0, 0, 0, loc)
			last := first.AddDate(0, 1, -1).Day()
			day := r.Anchor
			if day > last {
				day = last
			}
			if c := at(first.Year(), first.Month(), day); c.After(after) {
				return c, nil
			}
		}
		return time.Time{}, fmt.Errorf("no next run could be found for the rule")
	}
	for i := 0; i < 15; i++ {
		// time.Date carries a day past the end of the month into the
		// next, so counting days this way is safe.
		c := at(y, m, d+i)
		if !c.After(after) {
			continue
		}
		switch {
		case r.Every == everyDay && len(r.Days) == 7 && r.Days[mondayFirst(c.Weekday())-1] != '1':
			continue
		case r.Every == everyWeek && r.Anchor > 0 && mondayFirst(c.Weekday()) != r.Anchor:
			continue
		}
		return c, nil
	}
	return time.Time{}, fmt.Errorf("no next run could be found for the rule")
}
