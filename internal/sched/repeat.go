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
	days := func() error {
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
		return nil
	}
	if r.Every != everyWeek && r.Interval != 0 {
		return model.Invalid("repeat.interval", "interval goes only with every: week.")
	}
	if r.Every != everyMonth && (r.Day != 0 || r.Week != 0 || r.Weekday != 0) {
		return model.Invalid("repeat.day", "day, week and weekday go only with every: month.")
	}
	switch r.Every {
	case everyDay:
		if r.Days == "" {
			r.Days = "1111111"
		}
		return days()
	case everyWeek:
		if r.Days == "" {
			if r.Interval != 0 {
				return model.Invalid("repeat.interval", "interval goes with days: name the days of the week it runs on.")
			}
			return nil
		}
		if r.Interval < 0 || r.Interval > 52 {
			return model.Invalid("repeat.interval", "interval is a number of weeks from 1 to 52.")
		}
		return days()
	case everyMonth:
		if r.Days != "" {
			return model.Invalid("repeat.days", "days goes only with every: day or week.")
		}
		switch {
		case r.Day != 0 && (r.Week != 0 || r.Weekday != 0):
			return model.Invalid("repeat.day", "A monthly repeat runs on a day of the month or on a weekday, not both.")
		case r.Day < 0 || r.Day > 31:
			return model.Invalid("repeat.day", "day is a day of the month from 1 to 31.")
		case (r.Week == 0) != (r.Weekday == 0):
			return model.Invalid("repeat.week", "week and weekday go together: the first Monday is week 1, weekday 1.")
		case r.Week != 0 && (r.Week < -1 || r.Week > 4):
			return model.Invalid("repeat.week", "week is 1 to 4, or -1 for the last.")
		case r.Weekday < 0 || r.Weekday > 7:
			return model.Invalid("repeat.weekday", "weekday is 1 (Monday) to 7 (Sunday).")
		}
		return nil
	}
	return model.Invalid("repeat.every", "A repeat is every day, week or month.")
}

// mondayFirst numbers a weekday 1 (Monday) to 7 (Sunday).
func mondayFirst(d time.Weekday) int {
	if d == time.Sunday {
		return 7
	}
	return int(d)
}

// mondayOf numbers the week a date falls in: the days since 1970-01-01 of
// its Monday.
func mondayOf(y int, m time.Month, d int) int {
	days := int(time.Date(y, m, d, 0, 0, 0, 0, time.UTC).Unix() / 86400)
	return days - (mondayFirst(time.Date(y, m, d, 0, 0, 0, 0, time.UTC).Weekday()) - 1)
}

// anchor fixes what the first run decides: the weekday a weekly rule
// without days runs on, the week an every-n-weeks rule counts from, or the
// day of the month for a monthly rule that names none.
func anchor(r *model.Repeat, first time.Time) {
	loc, err := time.LoadLocation(r.TimeZone)
	if err != nil {
		return
	}
	first = first.In(loc)
	switch {
	case r.Every == everyWeek && r.Days == "":
		r.Anchor = mondayFirst(first.Weekday())
	case r.Every == everyWeek && r.Interval > 1:
		r.Anchor = mondayOf(first.Date())
	case r.Every == everyMonth && r.Day == 0 && r.Week == 0:
		r.Anchor = first.Day()
	default:
		r.Anchor = 0
	}
}

// nthWeekday is the day of the month of the nth weekday (1 Monday to 7
// Sunday) of a month: n from 1 to 4, or -1 for the last.
func nthWeekday(y int, m time.Month, n, weekday int, loc *time.Location) int {
	if n == -1 {
		last := time.Date(y, m+1, 0, 12, 0, 0, 0, loc)
		return last.Day() - (mondayFirst(last.Weekday())-weekday+7)%7
	}
	first := time.Date(y, m, 1, 12, 0, 0, 0, loc)
	return 1 + (weekday-mondayFirst(first.Weekday())+7)%7 + 7*(n-1)
}

// wallAt is the moment loc's clocks show the wall time y-m-d hh:mm (days
// past the end of a month carry into the next), by the rule the clients'
// time grammar shares (testdata/commands/remind.json): a time the clocks
// skip moves on by the length of the gap, so 02:30 becomes 03:30, and a
// time they show twice is the first of the two.
func wallAt(y int, m time.Month, d, hh, mm int, loc *time.Location) time.Time {
	w := time.Date(y, m, d, hh, mm, 0, 0, time.UTC)
	var best time.Time
	for _, probe := range []time.Time{w.Add(-24 * time.Hour), w, w.Add(24 * time.Hour)} {
		_, off := probe.In(loc).Zone()
		u := w.Add(-time.Duration(off) * time.Second)
		if _, o := u.In(loc).Zone(); o != off {
			continue
		}
		if best.IsZero() || u.Before(best) {
			best = u
		}
	}
	if best.IsZero() {
		_, off := w.Add(-24 * time.Hour).In(loc).Zone()
		best = w.Add(-time.Duration(off) * time.Second)
	}
	return best.In(loc)
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
	at := func(y int, m time.Month, d int) time.Time { return wallAt(y, m, d, hh, mm, loc) }

	if r.Every == everyMonth && (r.Anchor > 0 || r.Day > 0 || r.Week != 0) {
		// The day (or the anchor day), or the month's last day if it has
		// no such day; or the nth weekday.
		for i := 0; i < 14; i++ {
			first := time.Date(y, m+time.Month(i), 1, 0, 0, 0, 0, loc)
			last := first.AddDate(0, 1, -1).Day()
			day := r.Day
			switch {
			case r.Week != 0:
				day = nthWeekday(first.Year(), first.Month(), r.Week, r.Weekday, loc)
			case day == 0:
				day = r.Anchor
			}
			if day > last {
				day = last
			}
			if c := at(first.Year(), first.Month(), day); c.After(after) {
				return c, nil
			}
		}
		return time.Time{}, fmt.Errorf("no next run could be found for the rule")
	}
	limit := 15
	if r.Every == everyWeek && r.Interval > 1 {
		limit = 7*r.Interval + 8
	}
	for i := 0; i < limit; i++ {
		// time.Date carries a day past the end of the month into the
		// next, so counting days this way is safe.
		c := at(y, m, d+i)
		if !c.After(after) {
			continue
		}
		cy, cm, cd := c.In(loc).Date()
		switch {
		case (r.Every == everyDay || r.Every == everyWeek) && len(r.Days) == 7 && r.Days[mondayFirst(c.Weekday())-1] != '1':
			continue
		case r.Every == everyWeek && r.Days == "" && r.Anchor > 0 && mondayFirst(c.Weekday()) != r.Anchor:
			continue
		case r.Every == everyWeek && r.Days != "" && r.Interval > 1 && r.Anchor != 0 &&
			((mondayOf(cy, cm, cd)-r.Anchor)/7%r.Interval+r.Interval)%r.Interval != 0:
			continue
		}
		return c, nil
	}
	return time.Time{}, fmt.Errorf("no next run could be found for the rule")
}
