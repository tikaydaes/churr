// Package instant turns a date and time typed in a message into the moment
// it names (ruling R69, docs/MESSAGE-FORMAT.md section 3.1). The parser in
// internal/markdown finds the typed text and splits it into its local part
// and its zone; this package says which instant that is, if any.
//
// The same rules are implemented by the web chat and the apps, which do it
// for sealed chats and for the composer's preview. All three run the cases
// in testdata/instants/resolve.json.
package instant

import (
	"strconv"
	"strings"
	"time"
	_ "time/tzdata" // zone names must resolve on a machine with no zone database
)

// The precisions an instant is typed with.
const (
	Minute = "minute"
	Second = "second"
)

// Instant is a resolved instant.
type Instant struct {
	Time      time.Time // in UTC
	Precision string    // Minute or Second
	Zone      string    // as written, or the author's zone when none was
}

// Resolve works out the instant of a local date and time as the parser
// gives it, "YYYY-MM-DDTHH:MM" with optional ":SS" and fraction, in zone as
// written ("Z", a numeric offset, an IANA name, or "" for none). With no
// zone it is read in authorZone, the author's IANA zone; with neither it is
// not an instant. A local time that a zone's clocks skip (the gap when
// clocks go forward) is not an instant either; a local time that happens
// twice (when clocks go back) is the first of the two.
func Resolve(local, zone, authorZone string) (Instant, bool) {
	wall, precision, ok := parseLocal(local)
	if !ok {
		return Instant{}, false
	}
	out := Instant{Precision: precision, Zone: zone}
	if zone == "" {
		if authorZone == "" || !IsName(authorZone) {
			return Instant{}, false
		}
		zone, out.Zone = authorZone, authorZone
	}
	if off, ok := Offset(zone); ok {
		out.Time = wall.Add(-time.Duration(off) * time.Second)
		return out, true
	}
	loc, ok := location(zone)
	if !ok {
		return Instant{}, false
	}
	t, ok := inZone(wall, loc)
	if !ok {
		return Instant{}, false
	}
	out.Time = t
	return out, true
}

// inZone finds the instant at which loc's clocks show wall (a time whose
// fields are the wall clock, in UTC). The offsets in force a day either
// side are the only candidates: no zone changes its offset twice in a day.
func inZone(wall time.Time, loc *time.Location) (time.Time, bool) {
	var best time.Time
	found := false
	seen := map[int]bool{}
	for _, probe := range []time.Time{wall.Add(-24 * time.Hour), wall, wall.Add(24 * time.Hour)} {
		_, off := probe.In(loc).Zone()
		if seen[off] {
			continue
		}
		seen[off] = true
		u := wall.Add(-time.Duration(off) * time.Second)
		if _, got := u.In(loc).Zone(); got != off {
			continue
		}
		if !found || u.Before(best) {
			best, found = u, true
		}
	}
	return best.UTC(), found
}

// parseLocal reads "YYYY-MM-DDTHH:MM[:SS[.fraction]]" as a wall time in UTC.
func parseLocal(s string) (time.Time, string, bool) {
	if len(s) < 16 || s[4] != '-' || s[7] != '-' || s[10] != 'T' || s[13] != ':' {
		return time.Time{}, "", false
	}
	num := func(a, b int) (int, bool) {
		n, err := strconv.Atoi(s[a:b])
		return n, err == nil && s[a] != '+' && s[a] != '-'
	}
	y, ok1 := num(0, 4)
	mo, ok2 := num(5, 7)
	d, ok3 := num(8, 10)
	h, ok4 := num(11, 13)
	mi, ok5 := num(14, 16)
	if !(ok1 && ok2 && ok3 && ok4 && ok5) {
		return time.Time{}, "", false
	}
	sec, nanos, precision := 0, 0, Minute
	if len(s) > 16 {
		if len(s) < 19 || s[16] != ':' {
			return time.Time{}, "", false
		}
		var ok bool
		if sec, ok = num(17, 19); !ok {
			return time.Time{}, "", false
		}
		precision = Second
		if len(s) > 19 {
			frac := s[20:]
			if s[19] != '.' || frac == "" || len(frac) > 9 || strings.Trim(frac, "0123456789") != "" {
				return time.Time{}, "", false
			}
			ms, _ := strconv.Atoi((frac + "00")[:3])
			nanos = ms * int(time.Millisecond)
		}
	}
	if !ValidDate(y, mo, d) || h > 23 || mi > 59 || sec > 59 {
		return time.Time{}, "", false
	}
	return time.Date(y, time.Month(mo), d, h, mi, sec, nanos, time.UTC), precision, true
}

// ValidDate reports whether y-m-d is a day of the Gregorian calendar, for
// years 1 to 9999.
func ValidDate(y, m, d int) bool {
	if y < 1 || y > 9999 || m < 1 || m > 12 || d < 1 {
		return false
	}
	days := [...]int{31, 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31}[m-1]
	if m == 2 && (y%4 == 0 && y%100 != 0 || y%400 == 0) {
		days = 29
	}
	return d <= days
}

// Offset reads a zone written as an offset: "Z", "+10:00", "+1000",
// "-05". It returns the offset in seconds east of UTC. Hours go to 14,
// minutes to 59.
func Offset(z string) (int, bool) {
	if z == "Z" {
		return 0, true
	}
	if len(z) < 3 || z[0] != '+' && z[0] != '-' {
		return 0, false
	}
	digits := z[1:]
	if len(digits) == 5 && digits[2] == ':' {
		digits = digits[:2] + digits[3:]
	}
	if len(digits) != 2 && len(digits) != 4 || strings.Trim(digits, "0123456789") != "" {
		return 0, false
	}
	h, _ := strconv.Atoi(digits[:2])
	m := 0
	if len(digits) == 4 {
		m, _ = strconv.Atoi(digits[2:])
	}
	if h > 14 || m > 59 {
		return 0, false
	}
	off := h*3600 + m*60
	if z[0] == '-' {
		off = -off
	}
	return off, true
}

// IsName reports whether z is an IANA zone name the tz database knows.
func IsName(z string) bool {
	_, ok := location(z)
	return ok
}

// ValidZone reports whether z can stand as an instant's zone: an offset or
// a known IANA name.
func ValidZone(z string) bool {
	if _, ok := Offset(z); ok {
		return true
	}
	return IsName(z)
}

// location loads an IANA zone. "Local" and "" are not names. Every part
// of a name starts with a capital letter, as every name in the database
// does, so that "europe/berlin" is refused even on a machine whose file
// system ignores case.
func location(z string) (*time.Location, bool) {
	if z == "" || z == "Local" || len(z) > 64 || strings.Contains(z, "..") {
		return nil, false
	}
	for _, part := range strings.Split(z, "/") {
		if part == "" || part[0] < 'A' || part[0] > 'Z' || strings.Trim(part, "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789_+-") != "" {
			return nil, false
		}
	}
	loc, err := time.LoadLocation(z)
	if err != nil {
		return nil, false
	}
	return loc, true
}

// Format writes an instant's time as the API writes times: UTC, RFC 3339,
// with milliseconds (docs/CONVENTIONS.md).
func Format(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000Z") }

// ParseTime reads a time as Format writes it, or any RFC 3339 time, for an
// instant a client or another server sent. Years outside 1000 to 9999 are
// not sane for a message and are refused.
func ParseTime(s string) (time.Time, bool) {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil || t.Year() < 1000 || t.Year() > 9999 {
		return time.Time{}, false
	}
	return t.UTC(), true
}
