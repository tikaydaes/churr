package api_test

import (
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/testkit"
)

// Instants (ruling R69): dates and times typed in a message become
// `instant` entities, resolved by the server from the zone written or the
// author's zone the client sends, and phrases the author accepted arrive as
// hints the server checks.

// instantsOf returns a message's instant entities.
func instantsOf(m map[string]any) []map[string]any {
	var out []map[string]any
	ents, _ := m["entities"].([]any)
	for _, e := range ents {
		if em := e.(map[string]any); em["type"] == "instant" {
			out = append(out, em)
		}
	}
	return out
}

func wantInstant(t *testing.T, what string, m map[string]any, at, n int, when, precision, zone string) {
	t.Helper()
	got := instantsOf(m)
	if len(got) != 1 {
		t.Fatalf("%s: want one instant, got %v (entities %v)", what, got, m["entities"])
	}
	e := got[0]
	if int(e["at"].(float64)) != at || int(e["len"].(float64)) != n || e["time"] != when || e["precision"] != precision || e["zone"] != zone {
		t.Fatalf("%s: got %v, want at %d len %d time %s precision %s zone %s", what, e, at, n, when, precision, zone)
	}
}

func TestInstantsResolved(t *testing.T) {
	ts := testkit.NewServer(t)
	owner, sam := ts.Owner, ts.Member("sam")
	room := owner.Channel("plans", false, sam)
	msgs := convPath(room) + "/messages"
	post := func(body map[string]any) map[string]any {
		return owner.Must(201, "POST", msgs, body)["message"].(map[string]any)
	}

	// A zone written in the text needs nothing from the client: bots and
	// old clients get it too.
	m := post(map[string]any{"text": "Party 2026-12-24 18:00 Europe/Berlin!"})
	wantInstant(t, "IANA zone", m, 6, 30, "2026-12-24T17:00:00.000Z", "minute", "Europe/Berlin")
	if m["text"] != "Party 2026-12-24 18:00 Europe/Berlin!" {
		t.Fatalf("the text must stay as typed: %v", m["text"])
	}
	wantInstant(t, "offset", post(map[string]any{"text": "2026-12-24T18:00:30+10:00"}), 0, 25, "2026-12-24T08:00:30.000Z", "second", "+10:00")

	// Without a zone it is read in the author's zone, which the client
	// sends; without that it stays plain text.
	wantInstant(t, "author zone", post(map[string]any{"text": "at 2026-12-24 18:00", "time_zone": "Australia/Brisbane"}),
		3, 16, "2026-12-24T08:00:00.000Z", "minute", "Australia/Brisbane")
	if got := instantsOf(post(map[string]any{"text": "at 2026-12-24 18:00"})); len(got) != 0 {
		t.Fatalf("no zone sent: %v", got)
	}
	if got := instantsOf(post(map[string]any{"text": "at 2026-12-24 18:00", "time_zone": "Not/AZone"})); len(got) != 0 {
		t.Fatalf("an unknown author zone: %v", got)
	}

	// Clock changes: a time the clocks skip stays plain; a time that
	// happens twice is the first.
	if got := instantsOf(post(map[string]any{"text": "2026-03-29 02:30", "time_zone": "Europe/Berlin"})); len(got) != 0 {
		t.Fatalf("a time in the gap: %v", got)
	}
	wantInstant(t, "overlap", post(map[string]any{"text": "2026-10-25 02:30", "time_zone": "Europe/Berlin"}),
		0, 16, "2026-10-25T00:30:00.000Z", "minute", "Europe/Berlin")
	wantInstant(t, "New York overlap", post(map[string]any{"text": "2026-11-01 01:30 America/New_York"}),
		0, 33, "2026-11-01T05:30:00.000Z", "minute", "America/New_York")

	// Not in code, and not escaped.
	for _, text := range []string{"`2026-12-24 18:00Z`", "```\n2026-12-24 18:00Z log\n```", "2026\\-12-24 18:00Z", "2026-12-24", "18:00 Z"} {
		if got := instantsOf(post(map[string]any{"text": text, "time_zone": "UTC"})); len(got) != 0 {
			t.Fatalf("%q: %v", text, got)
		}
	}

	// Others read it as it was stored.
	read := sam.Must(200, "GET", "/api/v1/messages/"+m["id"].(string), nil)["message"].(map[string]any)
	wantInstant(t, "read back", read, 6, 30, "2026-12-24T17:00:00.000Z", "minute", "Europe/Berlin")

	// An edit resolves the new text with the zone it brings, and an edit
	// that leaves the text alone keeps the instants it had.
	z := post(map[string]any{"text": "call 2026-12-24 09:00", "time_zone": "Asia/Tokyo"})
	wantInstant(t, "before the edit", z, 5, 16, "2026-12-24T00:00:00.000Z", "minute", "Asia/Tokyo")
	path := "/api/v1/messages/" + z["id"].(string)
	same := owner.Must(200, "PATCH", path, map[string]any{"text": "call 2026-12-24 09:00"})["message"].(map[string]any)
	wantInstant(t, "same text, no zone", same, 5, 16, "2026-12-24T00:00:00.000Z", "minute", "Asia/Tokyo")
	moved := owner.Must(200, "PATCH", path, map[string]any{"text": "call 2026-12-24 10:00", "time_zone": "Asia/Tokyo"})["message"].(map[string]any)
	wantInstant(t, "edited", moved, 5, 16, "2026-12-24T01:00:00.000Z", "minute", "Asia/Tokyo")
}

func TestInstantHints(t *testing.T) {
	ts := testkit.NewServer(t)
	owner, sam := ts.Owner, ts.Member("sam")
	room := owner.Channel("hints", false, sam)
	msgs := convPath(room) + "/messages"
	hint := func(start, end int, when, precision, zone string) map[string]any {
		return map[string]any{"start": start, "end": end, "time": when, "precision": precision, "zone": zone}
	}
	post := func(text string, hints ...map[string]any) map[string]any {
		return owner.Must(201, "POST", msgs, map[string]any{"text": text, "instant_hints": hints})["message"].(map[string]any)
	}

	// A good hint becomes an ordinary instant entity over the words.
	m := post("see you this Friday at 5pm", hint(8, 26, "2026-11-20T07:00:00Z", "minute", "Australia/Brisbane"))
	wantInstant(t, "hint", m, 8, 18, "2026-11-20T07:00:00.000Z", "minute", "Australia/Brisbane")

	// Bad hints are dropped without an error, and the message is posted.
	bad := []struct {
		why  string
		text string
		h    map[string]any
	}{
		{"past the end", "tomorrow 9:30", hint(0, 14, "2026-11-16T09:30:00Z", "minute", "UTC")},
		{"empty range", "tomorrow 9:30", hint(3, 3, "2026-11-16T09:30:00Z", "minute", "UTC")},
		{"negative start", "tomorrow 9:30", hint(-1, 5, "2026-11-16T09:30:00Z", "minute", "UTC")},
		{"inside inline code", "`tomorrow 9:30`", hint(1, 14, "2026-11-16T09:30:00Z", "minute", "UTC")},
		{"inside a code block", "```\ntomorrow 9:30\n```", hint(4, 17, "2026-11-16T09:30:00Z", "minute", "UTC")},
		{"inside link text", "[tomorrow 9:30](https://example.com)", hint(1, 14, "2026-11-16T09:30:00Z", "minute", "UTC")},
		{"over a mention", "@sam tomorrow", hint(0, 13, "2026-11-16T09:30:00Z", "minute", "UTC")},
		{"over a typed instant", "2026-12-24 18:00Z soon", hint(0, 22, "2026-11-16T09:30:00Z", "minute", "UTC")},
		{"splitting a surrogate pair", "\U0001F600 tomorrow", hint(1, 11, "2026-11-16T09:30:00Z", "minute", "UTC")},
		{"across formatting", "**tomorrow** 9:30", hint(0, 17, "2026-11-16T09:30:00Z", "minute", "UTC")},
		{"unknown zone", "tomorrow 9:30", hint(0, 13, "2026-11-16T09:30:00Z", "minute", "Nowhere/Land")},
		{"not a time", "tomorrow 9:30", hint(0, 13, "tomorrow", "minute", "UTC")},
		{"year out of range", "tomorrow 9:30", hint(0, 13, "0999-11-16T09:30:00Z", "minute", "UTC")},
		{"unknown precision", "tomorrow 9:30", hint(0, 13, "2026-11-16T09:30:00Z", "hour", "UTC")},
	}
	for _, b := range bad {
		got := post(b.text, b.h)
		for _, e := range instantsOf(got) {
			if e["time"] == "2026-11-16T09:30:00.000Z" || int(e["at"].(float64)) != 0 {
				t.Fatalf("%s: the hint was kept: %v", b.why, got["entities"])
			}
		}
	}

	// Of two overlapping hints the first stands; good ones beside bad
	// ones are kept.
	two := post("Fri 5pm and Sat 6pm",
		hint(0, 7, "2026-11-20T07:00:00Z", "minute", "+10:00"),
		hint(4, 9, "2026-11-20T08:00:00Z", "minute", "+10:00"),
		hint(12, 19, "2026-11-21T08:00:00Z", "minute", "Nope/Nope"))
	if got := instantsOf(two); len(got) != 1 || got[0]["zone"] != "+10:00" || int(got[0]["len"].(float64)) != 7 {
		t.Fatalf("overlapping hints: %v", two["entities"])
	}

	// Hints arrive with an edit too.
	path := "/api/v1/messages/" + m["id"].(string)
	e := owner.Must(200, "PATCH", path, map[string]any{"text": "see you next Tuesday at noon",
		"instant_hints": []any{hint(8, 28, "2026-11-24T02:00:00Z", "minute", "Australia/Brisbane")}})["message"].(map[string]any)
	wantInstant(t, "edit hint", e, 8, 20, "2026-11-24T02:00:00.000Z", "minute", "Australia/Brisbane")
}

func TestInstantsScheduled(t *testing.T) {
	ts := testkit.NewServer(t)
	owner := ts.Owner
	room := owner.Channel("later", false)
	runAt := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	item := owner.Must(201, "POST", "/api/v1/scheduled", map[string]any{"conversation_id": room, "kind": "message", "run_at": runAt,
		"content": map[string]any{"text": "kick-off 2026-12-24 09:00", "time_zone": "Europe/London"}})["scheduled"].(map[string]any)
	sent := owner.Must(200, "POST", "/api/v1/scheduled/"+item["id"].(string)+"/send", nil)
	m, _ := sent["message"].(map[string]any)
	if m == nil {
		t.Fatalf("send now: %v", sent)
	}
	wantInstant(t, "scheduled", m, 9, 16, "2026-12-24T09:00:00.000Z", "minute", "Europe/London")
}
