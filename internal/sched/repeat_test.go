package sched

import (
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/model"
)

func at(t *testing.T, zone, s string) time.Time {
	t.Helper()
	loc, err := time.LoadLocation(zone)
	if err != nil {
		t.Fatal(err)
	}
	v, err := time.ParseInLocation("2006-01-02 15:04", s, loc)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestNext(t *testing.T) {
	const dublin, ny, kolkata = "Europe/Dublin", "America/New_York", "Asia/Kolkata"
	cases := []struct {
		name  string
		rule  model.Repeat
		after string // in the rule's zone
		want  string
	}{
		{"every day, later today", model.Repeat{Every: "day", Days: "1111111", At: "09:30", TimeZone: dublin}, "2026-11-13 08:00", "2026-11-13 09:30"},
		{"every day, already past today", model.Repeat{Every: "day", Days: "1111111", At: "09:30", TimeZone: dublin}, "2026-11-13 09:30", "2026-11-14 09:30"},
		{"every weekday skips the weekend", model.Repeat{Every: "day", Days: "1111100", At: "09:30", TimeZone: dublin}, "2026-11-13 10:00", "2026-11-16 09:30"},
		{"monday and thursday", model.Repeat{Every: "day", Days: "1001000", At: "14:00", TimeZone: dublin}, "2026-11-13 10:00", "2026-11-16 14:00"},
		{"monday and thursday, from monday afternoon", model.Repeat{Every: "day", Days: "1001000", At: "14:00", TimeZone: dublin}, "2026-11-16 15:00", "2026-11-19 14:00"},
		{"every week on the anchor weekday", model.Repeat{Every: "week", At: "09:00", TimeZone: dublin, Anchor: 5}, "2026-11-13 09:00", "2026-11-20 09:00"},
		{"every week, anchor later this week", model.Repeat{Every: "week", At: "09:00", TimeZone: dublin, Anchor: 7}, "2026-11-13 12:00", "2026-11-15 09:00"},
		{"every week with no anchor yet: the next time the clock shows it", model.Repeat{Every: "week", At: "09:00", TimeZone: dublin}, "2026-11-13 12:00", "2026-11-14 09:00"},
		{"every month on the anchor day", model.Repeat{Every: "month", At: "08:00", TimeZone: dublin, Anchor: 13}, "2026-11-13 08:00", "2026-12-13 08:00"},
		{"the 31st in a month of 30 days", model.Repeat{Every: "month", At: "08:00", TimeZone: dublin, Anchor: 31}, "2026-10-31 09:00", "2026-11-30 08:00"},
		{"the 31st after a short month comes back", model.Repeat{Every: "month", At: "08:00", TimeZone: dublin, Anchor: 31}, "2026-11-30 09:00", "2026-12-31 08:00"},
		{"the 30th in February", model.Repeat{Every: "month", At: "08:00", TimeZone: dublin, Anchor: 30}, "2027-01-30 09:00", "2027-02-28 08:00"},
		{"the 29th in a leap February", model.Repeat{Every: "month", At: "08:00", TimeZone: dublin, Anchor: 31}, "2028-01-31 09:00", "2028-02-29 08:00"},
		{"across the year's end", model.Repeat{Every: "month", At: "08:00", TimeZone: dublin, Anchor: 5}, "2026-12-20 09:00", "2027-01-05 08:00"},
		// Clocks go back in Dublin on 25 October 2026 and forward on 28
		// March 2027; 09:30 on the wall stays 09:30.
		{"the wall clock holds when clocks go back", model.Repeat{Every: "day", Days: "1111111", At: "09:30", TimeZone: dublin}, "2026-10-24 10:00", "2026-10-25 09:30"},
		{"the wall clock holds when clocks go forward", model.Repeat{Every: "day", Days: "1111111", At: "09:30", TimeZone: dublin}, "2027-03-27 10:00", "2027-03-28 09:30"},
		{"new york, clocks forward", model.Repeat{Every: "day", Days: "1111111", At: "07:00", TimeZone: ny}, "2027-03-13 08:00", "2027-03-14 07:00"},
		// The kinds a check-in offers (FEATURES.md 9.3, section 11).
		{"weekly on chosen days", model.Repeat{Every: "week", Days: "1010000", At: "09:00", TimeZone: dublin}, "2026-11-16 10:00", "2026-11-18 09:00"},
		{"every two weeks, an off week is skipped", model.Repeat{Every: "week", Days: "1010000", Interval: 2, At: "09:00", TimeZone: dublin, Anchor: mondayOf(2026, 11, 16)}, "2026-11-18 10:00", "2026-11-30 09:00"},
		{"every two weeks, the rest of an on week", model.Repeat{Every: "week", Days: "1010000", Interval: 2, At: "09:00", TimeZone: dublin, Anchor: mondayOf(2026, 11, 16)}, "2026-11-30 10:00", "2026-12-02 09:00"},
		{"every three weeks, counted from an old anchor", model.Repeat{Every: "week", Days: "0000100", Interval: 3, At: "09:00", TimeZone: dublin, Anchor: mondayOf(2026, 1, 5)}, "2026-11-20 10:00", "2026-12-11 09:00"},
		{"every two weeks with no anchor yet runs on the next chosen day", model.Repeat{Every: "week", Days: "1000000", Interval: 2, At: "09:00", TimeZone: dublin}, "2026-11-13 10:00", "2026-11-16 09:00"},
		{"monthly on a day", model.Repeat{Every: "month", Day: 15, At: "09:00", TimeZone: dublin}, "2026-11-16 10:00", "2026-12-15 09:00"},
		{"monthly on the 31st in a short month", model.Repeat{Every: "month", Day: 31, At: "09:00", TimeZone: dublin}, "2027-02-01 10:00", "2027-02-28 09:00"},
		{"the first monday", model.Repeat{Every: "month", Week: 1, Weekday: 1, At: "09:00", TimeZone: dublin}, "2026-11-13 10:00", "2026-12-07 09:00"},
		{"the third wednesday", model.Repeat{Every: "month", Week: 3, Weekday: 3, At: "09:00", TimeZone: dublin}, "2026-11-13 10:00", "2026-11-18 09:00"},
		{"the last friday", model.Repeat{Every: "month", Week: -1, Weekday: 5, At: "17:00", TimeZone: dublin}, "2026-11-13 10:00", "2026-11-27 17:00"},
		{"the last sunday, into the next month", model.Repeat{Every: "month", Week: -1, Weekday: 7, At: "09:00", TimeZone: dublin}, "2026-11-29 10:00", "2026-12-27 09:00"},
		{"a zone with a half-hour offset", model.Repeat{Every: "day", Days: "1111111", At: "06:15", TimeZone: kolkata}, "2026-11-13 06:15", "2026-11-14 06:15"},
	}
	for _, c := range cases {
		got, err := next(c.rule, at(t, c.rule.TimeZone, c.after))
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if want := at(t, c.rule.TimeZone, c.want); !got.Equal(want) {
			t.Errorf("%s: got %s, want %s", c.name, got, want)
		}
	}

	// The same rule read from another part of the world gives the same
	// moment: the rule's own zone decides, not the server's.
	rule := model.Repeat{Every: "day", Days: "1111111", At: "09:30", TimeZone: dublin}
	a, _ := next(rule, at(t, dublin, "2026-10-24 10:00"))
	b, _ := next(rule, at(t, dublin, "2026-10-24 10:00").In(time.FixedZone("far", 11*3600)))
	if !a.Equal(b) {
		t.Errorf("the caller's zone changed the answer: %s and %s", a, b)
	}
	// One day really is 25 hours long when clocks go back.
	if gap := a.Sub(at(t, dublin, "2026-10-24 09:30")); gap != 25*time.Hour {
		t.Errorf("across the clock change the runs are %s apart, want 25h", gap)
	}

	// A time the clocks skip moves on by the gap; one they show twice is
	// the first (the clients' rule, testdata/commands/remind.json).
	for _, c := range []struct{ name, zone, at, after, want string }{
		{"new york skips 02:30", ny, "02:30", "2027-03-13T12:00:00Z", "2027-03-14T07:30:00Z"},
		{"new york shows 01:30 twice", ny, "01:30", "2026-10-31T12:00:00Z", "2026-11-01T05:30:00Z"},
		{"dublin skips 01:30", dublin, "01:30", "2027-03-27T12:00:00Z", "2027-03-28T01:30:00Z"},
		{"dublin shows 01:30 twice", dublin, "01:30", "2027-10-30T12:00:00Z", "2027-10-31T00:30:00Z"},
	} {
		after, _ := time.Parse(time.RFC3339, c.after)
		want, _ := time.Parse(time.RFC3339, c.want)
		got, err := next(model.Repeat{Every: "day", Days: "1111111", At: c.at, TimeZone: c.zone}, after)
		if err != nil || !got.Equal(want) {
			t.Errorf("%s: got %s (%v), want %s", c.name, got.UTC(), err, want)
		}
	}

	if _, err := next(model.Repeat{Every: "day", Days: "1111111", At: "09:30", TimeZone: "Mars/Olympus"}, time.Now()); err == nil {
		t.Error("an unknown zone was accepted")
	}
}

func TestCheckRepeat(t *testing.T) {
	good := []model.Repeat{
		{Every: "day", At: "09:30", TimeZone: "Europe/Dublin"},
		{Every: "day", Days: "1000000", At: "00:00", TimeZone: "UTC"},
		{Every: "week", At: "23:59", TimeZone: "Asia/Tokyo"},
		{Every: "month", At: "12:00", TimeZone: "America/Sao_Paulo"},
		{Every: "week", Days: "1010000", At: "09:00", TimeZone: "UTC"},
		{Every: "week", Days: "1010000", Interval: 3, At: "09:00", TimeZone: "UTC"},
		{Every: "month", Day: 31, At: "09:00", TimeZone: "UTC"},
		{Every: "month", Week: 1, Weekday: 1, At: "09:00", TimeZone: "UTC"},
		{Every: "month", Week: -1, Weekday: 5, At: "09:00", TimeZone: "UTC"},
	}
	for _, r := range good {
		if err := checkRepeat(&r); err != nil {
			t.Errorf("%+v: %v", r, err)
		}
		if r.Every == "day" && len(r.Days) != 7 {
			t.Errorf("%+v: days were not filled in", r)
		}
	}
	bad := map[string]model.Repeat{
		"repeat.every":     {Every: "year", At: "09:30", TimeZone: "UTC"},
		"repeat.at":        {Every: "day", At: "9:30", TimeZone: "UTC"},
		"repeat.time_zone": {Every: "day", At: "09:30", TimeZone: "Nowhere/Much"},
		"repeat.days":      {Every: "day", Days: "0000000", At: "09:30", TimeZone: "UTC"},
	}
	for field, r := range bad {
		err := checkRepeat(&r)
		e, ok := model.AsError(err)
		if !ok || e.Code != model.CodeInvalidValue || e.Field != field {
			t.Errorf("%+v: got %v, want invalid_value on %s", r, err, field)
		}
	}
	for _, r := range []model.Repeat{
		{Every: "day", Days: "11111", At: "09:30", TimeZone: "UTC"},
		{Every: "day", Days: "11111x1", At: "09:30", TimeZone: "UTC"},
		{Every: "day", At: "24:00", TimeZone: "UTC"},
		{Every: "day", Days: "1111111", Interval: 2, At: "09:30", TimeZone: "UTC"},
		{Every: "week", Interval: 2, At: "09:30", TimeZone: "UTC"},
		{Every: "week", Days: "1000000", Interval: 53, At: "09:30", TimeZone: "UTC"},
		{Every: "week", Days: "0000000", Interval: 2, At: "09:30", TimeZone: "UTC"},
		{Every: "week", Days: "1000000", Day: 3, At: "09:30", TimeZone: "UTC"},
		{Every: "month", Days: "1000000", At: "09:30", TimeZone: "UTC"},
		{Every: "month", Day: 32, At: "09:30", TimeZone: "UTC"},
		{Every: "month", Day: 3, Week: 1, Weekday: 1, At: "09:30", TimeZone: "UTC"},
		{Every: "month", Week: 1, At: "09:30", TimeZone: "UTC"},
		{Every: "month", Weekday: 1, At: "09:30", TimeZone: "UTC"},
		{Every: "month", Week: 5, Weekday: 1, At: "09:30", TimeZone: "UTC"},
		{Every: "month", Week: -2, Weekday: 1, At: "09:30", TimeZone: "UTC"},
		{Every: "month", Week: 1, Weekday: 8, At: "09:30", TimeZone: "UTC"},
		{Every: "day", At: "09:30"},
		{Every: "day", At: "09:30", TimeZone: "Local"},
	} {
		if checkRepeat(&r) == nil {
			t.Errorf("%+v was accepted", r)
		}
	}

	fortnightly := model.Repeat{Every: "week", Days: "1010000", Interval: 2, At: "09:00", TimeZone: "Europe/Dublin"}
	anchor(&fortnightly, at(t, "Europe/Dublin", "2026-11-18 09:00")) // a Wednesday
	if fortnightly.Anchor != mondayOf(2026, 11, 16) {
		t.Errorf("every two weeks anchored to %d, want the week of Monday 16 November", fortnightly.Anchor)
	}
	nth := model.Repeat{Every: "month", Week: 1, Weekday: 1, At: "09:00", TimeZone: "Europe/Dublin"}
	if anchor(&nth, at(t, "Europe/Dublin", "2026-12-07 09:00")); nth.Anchor != 0 {
		t.Errorf("an nth-weekday rule was anchored: %d", nth.Anchor)
	}

	weekly := model.Repeat{Every: "week", At: "09:00", TimeZone: "Europe/Dublin"}
	anchor(&weekly, at(t, "Europe/Dublin", "2026-11-13 09:00")) // a Friday
	if weekly.Anchor != 5 {
		t.Errorf("weekly anchor: got %d, want 5", weekly.Anchor)
	}
	// Late on the 31st in Tokyo is still the 31st in the rule's own zone
	// only if the rule is Tokyo's: the anchor is read on the rule's clock.
	monthly := model.Repeat{Every: "month", At: "23:30", TimeZone: "Asia/Tokyo"}
	anchor(&monthly, at(t, "Asia/Tokyo", "2026-10-31 23:30").UTC())
	if monthly.Anchor != 31 {
		t.Errorf("monthly anchor: got %d, want 31", monthly.Anchor)
	}
}
