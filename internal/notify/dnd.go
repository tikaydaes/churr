// Package notify holds the one rule for what an event means to each person:
// whether a new message notifies, only counts for the badge, or neither;
// whether an account is in do not disturb; whether a notify-anyway request
// reaches its target; who a starting call rings; and the totals the status
// endpoint returns (docs/FEATURES.md sections 2 and 3, docs/PUSH.md sections
// 2, 11 and 12).
//
// The live hint on message.created, badges and phone wake-ups all ask here,
// so they cannot disagree.
package notify

import (
	"encoding/json"
	"time"

	"github.com/tikaydaes/churr/internal/model"
)

// The sources of do not disturb, as the API names them.
const (
	SourceManual  = "manual"
	SourceStatus  = "status"
	SourceMeeting = "meeting"
	SourceQuiet   = "quiet"
)

// Prefs are the keys of accounts.prefs the server reads (docs/SCHEMA.md,
// accounts). Clients may keep other keys there.
type Prefs struct {
	ThreadView    string `json:"thread_view"`
	MeetingPauses bool   `json:"meeting_pauses"`
	// MeetingPauseSkipUntil is set when the person turned do not disturb
	// off during a meeting that pauses notifications: that meeting's end
	// in milliseconds, or 0 if it has none (decision F5).
	MeetingPauseSkipUntil *int64 `json:"meeting_pause_skip_until"`
}

// ReadPrefs reads the server's keys out of an account's prefs. Anything
// unreadable reads as the defaults.
func ReadPrefs(a model.Account) Prefs {
	var p Prefs
	json.Unmarshal(a.PrefsJSON, &p)
	if p.ThreadView != "flat" {
		p.ThreadView = "threaded"
	}
	return p
}

// FlatView reports whether an account reads a conversation in the flat
// view: the conversation's own choice, or else the account preference.
func FlatView(a model.Account, m model.Member) bool {
	if m.ThreadView != "" {
		return m.ThreadView == "flat"
	}
	return ReadPrefs(a).ThreadView == "flat"
}

// until reports whether an "until" column is in force: unset is off, 0 is
// "until turned off", otherwise until that moment.
func until(p *model.Time, now model.Time) bool {
	return p != nil && (*p == 0 || p.After(now))
}

// StatusExists reports whether the account's status exists at now: its
// emoji or text is set and its end, if it has one, is still ahead
// (docs/FEATURES.md section 3.3). The two switches count only while it does.
func StatusExists(a model.Account, now model.Time) bool {
	return (a.StatusText != "" || a.StatusEmoji != "") && (a.StatusUntil.IsZero() || a.StatusUntil.After(now))
}

// AvailabilityNow returns the account's availability at now, or "" if it
// has none or it has run out.
func AvailabilityNow(a model.Account, now model.Time) string {
	if a.AvailabilityIs == "" || (!a.AvailabilityEnd.IsZero() && !a.AvailabilityEnd.After(now)) {
		return ""
	}
	return a.AvailabilityIs
}

// AppearsAway reports whether others are shown `away` whatever the
// connections say: appear away is on, the status that exists has its
// appear-away switch on, or the account is out of office (section 3.1).
func AppearsAway(a model.Account, now model.Time) bool {
	return until(a.AwayUntil, now) ||
		(StatusExists(a, now) && a.StatusAway) ||
		AvailabilityNow(a, now) == model.AvailabilityOutOfOffice
}

// meetingPauses reports whether the meeting source is on.
func meetingPauses(a model.Account, now model.Time) bool {
	if AvailabilityNow(a, now) != model.AvailabilityMeeting {
		return false
	}
	p := ReadPrefs(a)
	if !p.MeetingPauses {
		return false
	}
	if skip := p.MeetingPauseSkipUntil; skip != nil && (*skip == 0 || int64(now) < *skip) {
		return false
	}
	return true
}

// DND works out an account's do not disturb at one moment, source by source
// (docs/FEATURES.md section 3.6). The sources do not touch each other.
//
// Until is set only when every source that is on has an end: it is the
// latest of them.
func DND(a model.Account, now model.Time) model.Dnd {
	d := model.Dnd{Sources: []string{}}
	known := true
	var end model.Time
	add := func(source string, ends model.Time) {
		d.Sources = append(d.Sources, source)
		if ends.IsZero() {
			known = false
		} else if ends.After(end) {
			end = ends
		}
	}
	if until(a.DNDUntil, now) {
		add(SourceManual, *a.DNDUntil)
		d.ManualUntil = *a.DNDUntil
	}
	if StatusExists(a, now) && a.StatusDND {
		add(SourceStatus, a.StatusUntil)
	}
	if meetingPauses(a, now) {
		add(SourceMeeting, a.AvailabilityEnd)
	}
	if p, ok := QuietPeriodAt(a, now); ok && !(!a.QuietSkipUntil.IsZero() && !p.End.After(a.QuietSkipUntil)) {
		add(SourceQuiet, p.End)
	}
	d.On = len(d.Sources) > 0
	if d.On && known {
		d.Until = end
	}
	return d
}

// InDND reports whether an account is in do not disturb at a moment, from
// any cause, and when that ends if it is known (the zero time if not).
func InDND(a model.Account, now time.Time) (bool, time.Time) {
	d := DND(a, model.FromTime(now))
	if !d.On || d.Until.IsZero() {
		return d.On, time.Time{}
	}
	return true, d.Until.Time()
}

// Period is one quiet period.
type Period struct{ Start, End model.Time }

// ValidQuiet reports whether a quiet-hours schedule is well formed: two
// HH:MM times that differ, and seven characters of 0 and 1.
func ValidQuiet(start, end, days string) bool {
	_, _, ok1 := clock(start)
	_, _, ok2 := clock(end)
	if !ok1 || !ok2 || start == end || len(days) != 7 {
		return false
	}
	for _, c := range days {
		if c != '0' && c != '1' {
			return false
		}
	}
	return true
}

// ValidClock reports whether s is a time of day written HH:MM.
func ValidClock(s string) bool {
	_, _, ok := clock(s)
	return ok
}

// clock reads HH:MM.
func clock(s string) (h, m int, ok bool) {
	if len(s) != 5 || s[2] != ':' {
		return 0, 0, false
	}
	for _, i := range []int{0, 1, 3, 4} {
		if s[i] < '0' || s[i] > '9' {
			return 0, 0, false
		}
	}
	h = int(s[0]-'0')*10 + int(s[1]-'0')
	m = int(s[3]-'0')*10 + int(s[4]-'0')
	return h, m, h < 24 && m < 60
}

// zone is the account's time zone, or the server's if it has none or names
// one this machine does not know.
func zone(a model.Account) *time.Location {
	if a.TimeZone != "" {
		if loc, err := time.LoadLocation(a.TimeZone); err == nil {
			return loc
		}
	}
	return time.Local
}

// periodFrom returns the quiet period that starts on the given calendar day
// in the account's zone, if the schedule runs on that day. It ends at the
// next quiet_end after its start: later the same day if the end is later
// than the start, otherwise the following day.
func periodFrom(a model.Account, loc *time.Location, year int, month time.Month, day int) (Period, bool) {
	sh, sm, ok1 := clock(a.QuietStart)
	eh, em, ok2 := clock(a.QuietEnd)
	if !ok1 || !ok2 || len(a.QuietDays) != 7 {
		return Period{}, false
	}
	start := time.Date(year, month, day, sh, sm, 0, 0, loc)
	// quiet_days is Monday first; time.Weekday is Sunday first.
	if a.QuietDays[(int(start.Weekday())+6)%7] != '1' {
		return Period{}, false
	}
	end := time.Date(year, month, day, eh, em, 0, 0, loc)
	if !end.After(start) {
		end = time.Date(year, month, day+1, eh, em, 0, 0, loc)
	}
	return Period{Start: model.FromTime(start), End: model.FromTime(end)}, true
}

// QuietPeriodAt returns the quiet period the moment falls in, if any,
// without regard to skipping. A period is shorter than a day, so it started
// today or yesterday.
func QuietPeriodAt(a model.Account, now model.Time) (Period, bool) {
	if a.QuietStart == "" || a.QuietEnd == "" || a.QuietDays == "" {
		return Period{}, false
	}
	loc := zone(a)
	local := now.Time().In(loc)
	for back := 0; back <= 1; back++ {
		p, ok := periodFrom(a, loc, local.Year(), local.Month(), local.Day()-back)
		if ok && !p.Start.After(now) && now.Before(p.End) {
			return p, true
		}
	}
	return Period{}, false
}

// NextQuietPeriod returns the next quiet period that has not started at the
// given moment: the one "skip the next quiet period" skips.
func NextQuietPeriod(a model.Account, now model.Time) (Period, bool) {
	if a.QuietStart == "" || a.QuietEnd == "" || a.QuietDays == "" {
		return Period{}, false
	}
	loc := zone(a)
	local := now.Time().In(loc)
	for ahead := 0; ahead <= 7; ahead++ {
		p, ok := periodFrom(a, loc, local.Year(), local.Month(), local.Day()+ahead)
		if ok && p.Start.After(now) {
			return p, true
		}
	}
	return Period{}, false
}
