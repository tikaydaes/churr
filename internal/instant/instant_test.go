package instant

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"
)

var update = flag.Bool("update", false, "rewrite testdata/instants/resolve.json")

// vector is one case of testdata/instants/resolve.json, which the web chat
// and the apps run too.
type vector struct {
	Name       string `json:"name"`
	Local      string `json:"local"`
	Zone       string `json:"zone"`
	AuthorZone string `json:"author_zone"`
	// Want is the entity's fields, or null when it is not an instant.
	Want *want `json:"want"`
}

type want struct {
	Time      string `json:"time"`
	Precision string `json:"precision"`
	Zone      string `json:"zone"`
}

var inputs = []vector{
	{Name: "Z", Local: "2026-12-24T18:00", Zone: "Z"},
	{Name: "colon offset", Local: "2026-12-24T18:00", Zone: "+10:00"},
	{Name: "four-digit offset", Local: "2026-12-24T18:00", Zone: "+0530"},
	{Name: "two-digit negative offset", Local: "2026-12-24T18:00", Zone: "-05"},
	{Name: "minus zero", Local: "2026-12-24T18:00", Zone: "-00:00"},
	{Name: "offset beyond 14 hours", Local: "2026-12-24T18:00", Zone: "+15:00"},
	{Name: "seconds", Local: "2026-12-24T18:00:59", Zone: "Z"},
	{Name: "fraction to milliseconds", Local: "2026-12-24T18:00:00.98765", Zone: "Z"},
	{Name: "IANA winter", Local: "2026-12-24T18:00", Zone: "Europe/Berlin"},
	{Name: "IANA summer", Local: "2026-07-01T18:00", Zone: "Europe/Berlin"},
	{Name: "Berlin gap start", Local: "2026-03-29T02:00", Zone: "Europe/Berlin"},
	{Name: "Berlin gap middle", Local: "2026-03-29T02:59:59", Zone: "Europe/Berlin"},
	{Name: "Berlin just after the gap", Local: "2026-03-29T03:00", Zone: "Europe/Berlin"},
	{Name: "Berlin just before the gap", Local: "2026-03-29T01:59", Zone: "Europe/Berlin"},
	{Name: "Berlin overlap first", Local: "2026-10-25T02:00", Zone: "Europe/Berlin"},
	{Name: "Berlin overlap end", Local: "2026-10-25T02:59", Zone: "Europe/Berlin"},
	{Name: "Berlin after the overlap", Local: "2026-10-25T03:00", Zone: "Europe/Berlin"},
	{Name: "New York gap", Local: "2026-03-08T02:30", Zone: "America/New_York"},
	{Name: "New York overlap", Local: "2026-11-01T01:30", Zone: "America/New_York"},
	{Name: "Sydney overlap", Local: "2026-04-05T02:30", Zone: "Australia/Sydney"},
	{Name: "Sydney gap", Local: "2026-10-04T02:30", Zone: "Australia/Sydney"},
	{Name: "Lord Howe half-hour gap", Local: "2026-10-04T02:15", Zone: "Australia/Lord_Howe"},
	{Name: "Lord Howe half-hour overlap", Local: "2026-04-05T01:45", Zone: "Australia/Lord_Howe"},
	{Name: "Kolkata", Local: "2026-12-24T18:00", Zone: "Asia/Kolkata"},
	{Name: "Kathmandu", Local: "2026-12-24T18:00", Zone: "Asia/Kathmandu"},
	{Name: "Chatham", Local: "2026-12-24T18:00", Zone: "Pacific/Chatham"},
	{Name: "Kiritimati", Local: "2026-12-24T18:00", Zone: "Pacific/Kiritimati"},
	{Name: "UTC by name", Local: "2026-12-24T18:00", Zone: "UTC"},
	{Name: "Etc/GMT+5", Local: "2026-12-24T18:00", Zone: "Etc/GMT+5"},
	{Name: "unknown zone", Local: "2026-12-24T18:00", Zone: "Mars/Olympus_Mons"},
	{Name: "lower-case zone", Local: "2026-12-24T18:00", Zone: "europe/berlin"},
	{Name: "Local is not a zone", Local: "2026-12-24T18:00", Zone: "Local"},
	{Name: "author zone", Local: "2026-12-24T18:00", AuthorZone: "Australia/Brisbane"},
	{Name: "author zone in its gap", Local: "2026-03-29T02:30", AuthorZone: "Europe/Berlin"},
	{Name: "author zone in its overlap", Local: "2026-10-25T02:30", AuthorZone: "Europe/Berlin"},
	{Name: "author zone ignored when one is written", Local: "2026-12-24T18:00", Zone: "Z", AuthorZone: "Europe/Berlin"},
	{Name: "no zone and no author zone", Local: "2026-12-24T18:00"},
	{Name: "author zone unknown", Local: "2026-12-24T18:00", AuthorZone: "Nowhere/Special"},
	{Name: "author zone as an offset is not taken", Local: "2026-12-24T18:00", AuthorZone: "+10:00"},
	{Name: "leap day", Local: "2028-02-29T12:00", Zone: "Z"},
	{Name: "not a leap day", Local: "2027-02-29T12:00", Zone: "Z"},
	{Name: "year rollover by offset", Local: "2026-12-31T23:30", Zone: "-01:00"},
	{Name: "before 1970", Local: "1969-07-20T20:17", Zone: "Z"},
}

func TestVectors(t *testing.T) {
	var got []vector
	for _, v := range inputs {
		if in, ok := Resolve(v.Local, v.Zone, v.AuthorZone); ok {
			v.Want = &want{Time: Format(in.Time), Precision: in.Precision, Zone: in.Zone}
		}
		got = append(got, v)
	}
	b, err := json.MarshalIndent(map[string]any{
		"description": "How a typed instant resolves (ruling R69, docs/MESSAGE-FORMAT.md section 3.1). " +
			"local and zone are what the Markdown parser gives (the tree's instant node); author_zone is the zone the " +
			"posting client sent. want is the instant entity's time, precision and zone, or null when the text stays " +
			"plain. Written by go test ./internal/instant -update.",
		"cases": got,
	}, "", " ")
	if err != nil {
		t.Fatal(err)
	}
	b = append(b, '\n')
	path := filepath.Join("..", "..", "testdata", "instants", "resolve.json")
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, b, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	old, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(old, b) {
		t.Errorf("%s is out of date: run go test ./internal/instant -update and read the diff", path)
	}
}

func TestOffset(t *testing.T) {
	for z, want := range map[string]int{"Z": 0, "+10:00": 36000, "+1000": 36000, "-05": -18000, "+05:45": 20700, "-00:00": 0} {
		if got, ok := Offset(z); !ok || got != want {
			t.Errorf("Offset(%q) = %d, %v; want %d", z, got, ok, want)
		}
	}
	for _, z := range []string{"", "+", "+1", "+100", "+10:0", "+15", "+10:60", "10:00", "+10-00", "z"} {
		if _, ok := Offset(z); ok {
			t.Errorf("Offset(%q) accepted", z)
		}
	}
}

func TestParseTime(t *testing.T) {
	if _, ok := ParseTime("2026-12-24T17:00:00.000Z"); !ok {
		t.Error("a time as Format writes it is refused")
	}
	for _, s := range []string{"", "yesterday", "0999-01-01T00:00:00Z", "2026-12-24 17:00"} {
		if _, ok := ParseTime(s); ok {
			t.Errorf("ParseTime(%q) accepted", s)
		}
	}
}
