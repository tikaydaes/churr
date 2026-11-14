package main

import (
	"testing"
	"time"
)

func TestWanted(t *testing.T) {
	at := func(s string) time.Time {
		v, err := time.Parse(time.RFC3339, s)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	entries := []entry{
		{"sam", "meeting", at("2026-11-13T09:30:00Z"), at("2026-11-13T10:30:00Z")},
		{"sam", "meeting", at("2026-11-13T10:00:00Z"), at("2026-11-13T11:00:00Z")},
		{"lee", "out_of_office", at("2026-11-13T00:00:00Z"), at("2026-11-20T00:00:00Z")},
		{"lee", "meeting", at("2026-11-13T09:00:00Z"), at("2026-11-13T10:00:00Z")},
		{"kim", "meeting", at("2026-11-13T09:00:00Z"), at("2026-11-16T09:00:00Z")},
	}
	for _, c := range []struct {
		who, now string
		want     want
	}{
		{"sam", "2026-11-13T09:00:00Z", want{}},
		{"sam", "2026-11-13T09:45:00Z", want{"meeting", at("2026-11-13T10:30:00Z")}},
		// Two meetings that overlap: busy until the later one ends.
		{"sam", "2026-11-13T10:15:00Z", want{"meeting", at("2026-11-13T11:00:00Z")}},
		{"sam", "2026-11-13T11:00:00Z", want{}},
		// Out of office wins over a meeting.
		{"lee", "2026-11-13T09:30:00Z", want{"out_of_office", at("2026-11-20T00:00:00Z")}},
		// A meeting is set at most 24 hours ahead.
		{"kim", "2026-11-13T10:00:00Z", want{"meeting", at("2026-11-14T10:00:00Z")}},
		{"nobody", "2026-11-13T10:00:00Z", want{}},
	} {
		if got := wanted(entries, c.who, at(c.now)); got != c.want {
			t.Errorf("%s at %s: %+v, want %+v", c.who, c.now, got, c.want)
		}
	}
}
