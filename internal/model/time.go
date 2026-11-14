package model

import (
	"encoding/json"
	"fmt"
	"time"
)

// Time is a moment: milliseconds since the Unix epoch in the database, and
// RFC 3339 in UTC with milliseconds in JSON. The zero value means "no time"
// and is left out of JSON by `omitempty`.
type Time int64

// timeLayout is the one way a time is written in JSON.
const timeLayout = "2006-01-02T15:04:05.000Z"

// Now returns the current moment.
func Now() Time { return FromTime(time.Now()) }

// FromTime converts a time.Time. The zero time.Time becomes the zero Time.
func FromTime(t time.Time) Time {
	if t.IsZero() {
		return 0
	}
	return Time(t.UnixMilli())
}

// Time converts back to a time.Time in UTC. The zero Time gives the zero time.Time.
func (t Time) Time() time.Time {
	if t == 0 {
		return time.Time{}
	}
	return time.UnixMilli(int64(t)).UTC()
}

// IsZero reports whether t means "no time".
func (t Time) IsZero() bool { return t == 0 }

// Add returns t moved by d.
func (t Time) Add(d time.Duration) Time { return t + Time(d/time.Millisecond) }

// Sub returns the time from u to t.
func (t Time) Sub(u Time) time.Duration { return time.Duration(t-u) * time.Millisecond }

// Before reports whether t is earlier than u.
func (t Time) Before(u Time) bool { return t < u }

// After reports whether t is later than u.
func (t Time) After(u Time) bool { return t > u }

// String writes the JSON form, for logs.
func (t Time) String() string { return t.Time().Format(timeLayout) }

// MarshalJSON writes RFC 3339 in UTC with milliseconds, or null for no time.
func (t Time) MarshalJSON() ([]byte, error) {
	if t == 0 {
		return []byte("null"), nil
	}
	return []byte(`"` + t.Time().Format(timeLayout) + `"`), nil
}

// UnmarshalJSON reads any RFC 3339 time, or null.
func (t *Time) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		*t = 0
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return fmt.Errorf("a time must be a string")
	}
	parsed, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return fmt.Errorf("not an RFC 3339 time: %q", s)
	}
	*t = FromTime(parsed)
	return nil
}
