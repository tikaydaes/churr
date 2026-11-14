package notify

import (
	"strconv"
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/model"
)

// at makes a moment in a zone.
func at(t *testing.T, zone string, y int, mo time.Month, d, h, mi int) model.Time {
	t.Helper()
	loc, err := time.LoadLocation(zone)
	if err != nil {
		t.Fatal(err)
	}
	return model.FromTime(time.Date(y, mo, d, h, mi, 0, 0, loc))
}

func ptr(t model.Time) *model.Time { return &t }

func sources(d model.Dnd) string {
	out := ""
	for _, s := range d.Sources {
		out += s + " "
	}
	return out
}

// 16 November 2026 is a Monday.
const berlin = "Europe/Berlin"

func quiet(days string) model.Account {
	return model.Account{ID: "a", TimeZone: berlin, QuietStart: "22:00", QuietEnd: "07:00", QuietDays: days, ClaimedAt: 1}
}

func TestQuietPeriods(t *testing.T) {
	weeknights := quiet("1111100") // starts Monday to Friday
	for _, c := range []struct {
		name string
		now  model.Time
		in   bool
	}{
		{"Monday evening before the start", at(t, berlin, 2026, 11, 16, 21, 59), false},
		{"Monday at the start", at(t, berlin, 2026, 11, 16, 22, 0), true},
		{"Tuesday early morning, in Monday's period", at(t, berlin, 2026, 11, 17, 6, 59), true},
		{"Tuesday at the end", at(t, berlin, 2026, 11, 17, 7, 0), false},
		{"Saturday early morning, in Friday's period", at(t, berlin, 2026, 11, 21, 3, 0), true},
		{"Saturday night: no period starts on Saturday", at(t, berlin, 2026, 11, 21, 23, 0), false},
		{"Sunday early morning: none started on Saturday", at(t, berlin, 2026, 11, 22, 3, 0), false},
		{"Monday early morning: none started on Sunday", at(t, berlin, 2026, 11, 16, 3, 0), false},
	} {
		_, in := QuietPeriodAt(weeknights, c.now)
		if in != c.in {
			t.Errorf("%s: in a quiet period = %v, want %v", c.name, in, c.in)
		}
		if got := DND(weeknights, c.now).On; got != c.in {
			t.Errorf("%s: do not disturb = %v, want %v", c.name, got, c.in)
		}
	}

	// A period that ends later the same day.
	siesta := model.Account{TimeZone: berlin, QuietStart: "13:00", QuietEnd: "15:00", QuietDays: "1111111"}
	p, in := QuietPeriodAt(siesta, at(t, berlin, 2026, 11, 16, 14, 0))
	if !in || p.Start != at(t, berlin, 2026, 11, 16, 13, 0) || p.End != at(t, berlin, 2026, 11, 16, 15, 0) {
		t.Errorf("same-day period: %v %v", p, in)
	}
	if _, in := QuietPeriodAt(siesta, at(t, berlin, 2026, 11, 16, 15, 0)); in {
		t.Error("the end is not part of the period")
	}

	// Times are wall clock in the account's zone, not the server's.
	tokyo := quiet("1111111")
	tokyo.TimeZone = "Asia/Tokyo"
	if _, in := QuietPeriodAt(tokyo, at(t, "Asia/Tokyo", 2026, 11, 16, 23, 0)); !in {
		t.Error("23:00 in Tokyo is inside a 22:00 to 07:00 period there")
	}
	if _, in := QuietPeriodAt(tokyo, at(t, berlin, 2026, 11, 16, 23, 0)); in {
		t.Error("23:00 in Berlin is 07:00 in Tokyo, which is outside")
	}

	// No schedule, and a malformed one, are never quiet.
	if _, in := QuietPeriodAt(model.Account{}, at(t, berlin, 2026, 11, 16, 23, 0)); in {
		t.Error("no schedule")
	}
	if _, in := QuietPeriodAt(model.Account{QuietStart: "25:00", QuietEnd: "07:00", QuietDays: "1111111"}, at(t, berlin, 2026, 11, 16, 23, 0)); in {
		t.Error("a malformed schedule")
	}
}

func TestNextQuietPeriodAndSkip(t *testing.T) {
	a := quiet("1111100")
	// Monday afternoon: the next period is tonight's.
	now := at(t, berlin, 2026, 11, 16, 15, 0)
	next, ok := NextQuietPeriod(a, now)
	if !ok || next.Start != at(t, berlin, 2026, 11, 16, 22, 0) || next.End != at(t, berlin, 2026, 11, 17, 7, 0) {
		t.Fatalf("next period from Monday afternoon: %v %v", next, ok)
	}
	// Inside Monday's period the next one that has not started is
	// Tuesday's.
	next, _ = NextQuietPeriod(a, at(t, berlin, 2026, 11, 16, 23, 0))
	if next.Start != at(t, berlin, 2026, 11, 17, 22, 0) {
		t.Fatalf("next period from inside one: %v", next)
	}
	// From Saturday the next is Monday's.
	next, _ = NextQuietPeriod(a, at(t, berlin, 2026, 11, 21, 12, 0))
	if next.Start != at(t, berlin, 2026, 11, 23, 22, 0) {
		t.Fatalf("next period from Saturday: %v", next)
	}
	if _, ok := NextQuietPeriod(quiet("0000000"), now); ok {
		t.Error("a schedule with no days has no next period")
	}

	// Skipping affects only the period it names.
	skipped := a
	skipped.QuietSkipUntil = at(t, berlin, 2026, 11, 17, 7, 0) // the end of Monday's period
	if DND(skipped, at(t, berlin, 2026, 11, 16, 23, 0)).On {
		t.Error("the skipped period is not quiet")
	}
	if !DND(skipped, at(t, berlin, 2026, 11, 17, 23, 0)).On {
		t.Error("the period after the skipped one is quiet again")
	}
}

func TestSourcesAreIndependent(t *testing.T) {
	now := at(t, berlin, 2026, 11, 16, 23, 0) // inside quiet hours
	a := quiet("1111111")

	d := DND(a, now)
	if !d.On || sources(d) != "quiet " || d.Until != at(t, berlin, 2026, 11, 17, 7, 0) {
		t.Fatalf("quiet only: %+v", d)
	}

	// A manual do not disturb that ends before quiet hours do changes
	// nothing about when quiet hours end, and the reverse.
	a.DNDUntil = ptr(now.Add(time.Hour))
	d = DND(a, now)
	if sources(d) != "manual quiet " || d.ManualUntil != now.Add(time.Hour) || d.Until != at(t, berlin, 2026, 11, 17, 7, 0) {
		t.Fatalf("manual shorter than quiet: %+v", d)
	}
	if d := DND(a, now.Add(2*time.Hour)); sources(d) != "quiet " {
		t.Fatalf("after the manual one ran out quiet hours keep things quiet: %+v", d)
	}
	a.DNDUntil = ptr(at(t, berlin, 2026, 11, 17, 9, 0))
	if d := DND(a, at(t, berlin, 2026, 11, 17, 8, 0)); sources(d) != "manual " {
		t.Fatalf("a manual one that outlasts quiet hours stays on: %+v", d)
	}

	// Until turned off: on, with no known end.
	a.DNDUntil = ptr(0)
	d = DND(a, now)
	if !d.On || !d.ManualUntil.IsZero() || !d.Until.IsZero() {
		t.Fatalf("until turned off has no end: %+v", d)
	}
	if on, until := InDND(a, now.Time()); !on || !until.IsZero() {
		t.Fatalf("InDND with no known end: %v %v", on, until)
	}

	// A status pauses only while it exists and only with its switch on.
	b := model.Account{StatusText: "Focus", StatusDND: true, StatusUntil: now.Add(time.Hour)}
	if d := DND(b, now); sources(d) != "status " || d.Until != now.Add(time.Hour) {
		t.Fatalf("status: %+v", d)
	}
	if DND(b, now.Add(2*time.Hour)).On {
		t.Error("a status that ran out pauses nothing, whatever the column says")
	}
	b.StatusDND = false
	if DND(b, now).On {
		t.Error("a status without the switch pauses nothing")
	}
	if DND(model.Account{StatusDND: true}, now).On {
		t.Error("the switch means nothing without a status")
	}

	// A meeting pauses only with the preference.
	m := model.Account{AvailabilityIs: model.AvailabilityMeeting, AvailabilityEnd: now.Add(time.Hour)}
	if DND(m, now).On {
		t.Error("a meeting without the preference does not pause")
	}
	m.PrefsJSON = []byte(`{"meeting_pauses": true}`)
	if d := DND(m, now); sources(d) != "meeting " || d.Until != now.Add(time.Hour) {
		t.Fatalf("meeting: %+v", d)
	}
	if DND(m, now.Add(2*time.Hour)).On {
		t.Error("a meeting that ended does not pause")
	}
	m.AvailabilityIs = model.AvailabilityOutOfOffice
	if DND(m, now).On {
		t.Error("out of office does not pause")
	}
	// The pause skipped for this meeting (ruling R37, decision F5).
	m.AvailabilityIs = model.AvailabilityMeeting
	m.PrefsJSON = []byte(`{"meeting_pauses": true, "meeting_pause_skip_until": 0}`)
	if DND(m, now).On {
		t.Error("skip until 0 skips a meeting with no end")
	}
	skip := now.Add(time.Hour)
	m.PrefsJSON = []byte(`{"meeting_pauses": true, "meeting_pause_skip_until": ` + itoa(int64(skip)) + `}`)
	if DND(m, now).On {
		t.Error("the pause is skipped until the meeting's end")
	}

	// Everything at once.
	all := quiet("1111111")
	all.DNDUntil = ptr(0)
	all.StatusText, all.StatusDND = "Busy", true
	all.AvailabilityIs, all.PrefsJSON = model.AvailabilityMeeting, []byte(`{"meeting_pauses": true}`)
	if d := DND(all, now); sources(d) != "manual status meeting quiet " {
		t.Fatalf("all four: %+v", d)
	}
	if DND(model.Account{}, now).On {
		t.Error("nothing set")
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func TestAppearsAway(t *testing.T) {
	now := model.Time(1_800_000_000_000)
	for _, c := range []struct {
		name string
		a    model.Account
		want bool
	}{
		{"nothing", model.Account{}, false},
		{"appear away until turned off", model.Account{AwayUntil: ptr(0)}, true},
		{"appear away with time left", model.Account{AwayUntil: ptr(now + 1)}, true},
		{"appear away that ran out", model.Account{AwayUntil: ptr(now - 1)}, false},
		{"a status with the switch", model.Account{StatusEmoji: "🌴", StatusAway: true}, true},
		{"a status without it", model.Account{StatusEmoji: "🌴"}, false},
		{"the switch on a status that ran out", model.Account{StatusEmoji: "🌴", StatusAway: true, StatusUntil: now - 1}, false},
		{"out of office", model.Account{AvailabilityIs: model.AvailabilityOutOfOffice}, true},
		{"out of office that ended", model.Account{AvailabilityIs: model.AvailabilityOutOfOffice, AvailabilityEnd: now - 1}, false},
		{"in a meeting", model.Account{AvailabilityIs: model.AvailabilityMeeting}, false},
	} {
		if got := AppearsAway(c.a, now); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}

func TestValidQuiet(t *testing.T) {
	for _, c := range []struct {
		start, end, days string
		want             bool
	}{
		{"22:00", "07:00", "1111100", true},
		{"00:00", "23:59", "0000000", true},
		{"22:00", "22:00", "1111100", false}, // may not be equal
		{"24:00", "07:00", "1111100", false},
		{"22:60", "07:00", "1111100", false},
		{"2200", "07:00", "1111100", false},
		{"22:00", "7:00", "1111100", false},
		{"22:00", "07:00", "111110", false},
		{"22:00", "07:00", "11111x0", false},
	} {
		if got := ValidQuiet(c.start, c.end, c.days); got != c.want {
			t.Errorf("ValidQuiet(%q, %q, %q) = %v, want %v", c.start, c.end, c.days, got, c.want)
		}
	}
}
