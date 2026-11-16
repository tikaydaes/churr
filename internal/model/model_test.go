package model

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestIDs(t *testing.T) {
	a, b := NewID[AccountID](), NewID[AccountID]()
	if !ValidID(string(a)) || a == b {
		t.Fatalf("NewID: %q %q", a, b)
	}
	if a[14] != '7' {
		t.Fatalf("identifiers are UUIDv7: %s", a)
	}
	for _, bad := range []string{"", "0193A6F2-7C1E-7B3A-9F0E-2A4D5C6B7E8F", "0193a6f27c1e7b3a9f0e2a4d5c6b7e8f", "0193a6f2-7c1e-7b3a-9f0e-2a4d5c6b7e8", "zzzza6f2-7c1e-7b3a-9f0e-2a4d5c6b7e8f"} {
		if ValidID(bad) {
			t.Errorf("ValidID(%q) should be false", bad)
		}
	}
	if _, err := ParseID[MessageID]("0193a6f2-7c1e-7b3a-9f0e-2a4d5c6b7e8f"); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseID[MessageID]("nope"); err == nil {
		t.Fatal("ParseID should refuse what is not an identifier")
	}
}

func TestTime(t *testing.T) {
	at := FromTime(time.Date(2026, 11, 13, 9, 30, 0, 0, time.UTC))
	b, _ := json.Marshal(at)
	if string(b) != `"2026-11-13T09:30:00.000Z"` {
		t.Fatalf("a time in JSON: %s", b)
	}
	var back Time
	if err := json.Unmarshal([]byte(`"2026-11-13T19:30:00.123+10:00"`), &back); err != nil || back != at+123 {
		t.Fatalf("reading a time with an offset: %v %v", back, err)
	}
	if err := json.Unmarshal([]byte(`"yesterday"`), &back); err == nil {
		t.Fatal("a time that is not RFC 3339 should be refused")
	}
	if err := json.Unmarshal([]byte(`null`), &back); err != nil || !back.IsZero() {
		t.Fatal("null is no time")
	}
	// Absent and null mean the same; servers omit them.
	out, _ := json.Marshal(struct {
		A Time `json:"a,omitempty"`
		B Time `json:"b"`
	}{B: at})
	if string(out) != `{"b":"2026-11-13T09:30:00.000Z"}` {
		t.Fatalf("omitting no time: %s", out)
	}
	if at.Add(time.Second).Sub(at) != time.Second || !at.Before(at.Add(time.Millisecond)) {
		t.Fatal("time arithmetic")
	}
	if FromTime(time.Time{}) != 0 || !Time(0).Time().IsZero() {
		t.Fatal("the zero time round-trips")
	}
}

func TestErrors(t *testing.T) {
	// Every code of docs/CONVENTIONS.md has its status.
	want := map[Code]int{
		"invalid_request": 400, "invalid_value": 400, "unauthenticated": 401, "second_factor_required": 401, "session_locked": 401,
		"forbidden": 403, "timed_out": 403, "archived": 403, "locked": 403, "feature_off": 403, "fresh_check_required": 403,
		"not_found": 404, "conflict": 409, "name_taken": 409, "already_exists": 409, "too_large": 413,
		"blocked_by_filter": 422, "rate_limited": 429, "slow_mode": 429, "internal": 500, "unavailable": 503,
	}
	if len(Codes()) != len(want) {
		t.Fatalf("%d codes, want %d", len(Codes()), len(want))
	}
	for _, c := range Codes() {
		if got := (&Error{Code: c}).Status(); got != want[c] {
			t.Errorf("%s: status %d, want %d", c, got, want[c])
		}
	}
	if ErrNameTaken.Message != "That name isn't available." {
		t.Fatal("name_taken always carries the one neutral sentence (ruling R3)")
	}
	specific := ErrForbidden.With("You are not in this channel.")
	wrapped := fmt.Errorf("posting: %w", specific)
	if !errors.Is(wrapped, ErrForbidden) || errors.Is(wrapped, ErrNotFound) || CodeOf(wrapped) != CodeForbidden {
		t.Fatal("errors match by code, through wrapping")
	}
	if ErrForbidden.Message == specific.Message {
		t.Fatal("With must not change the shared value")
	}
	if CodeOf(nil) != "" || CodeOf(errors.New("disk on fire")) != CodeInternal {
		t.Fatal("CodeOf for nil and for a foreign error")
	}
	b, _ := json.Marshal(map[string]any{"error": Invalid("name", "Too long.")})
	if string(b) != `{"error":{"code":"invalid_value","message":"Too long.","field":"name"}}` {
		t.Fatalf("the error shape: %s", b)
	}
	b, _ = json.Marshal(RateLimited(12))
	if !strings.Contains(string(b), `"retry_after_seconds":12`) {
		t.Fatalf("rate_limited says when to try again: %s", b)
	}
}

func TestNames(t *testing.T) {
	for name, want := range map[string]bool{
		"sam": true, "a1": true, "a.b-c_d": true, "9lives": true, strings.Repeat("a", 32): true,
		"a": false, "": false, ".sam": false, "-sam": false, "Sam": false, "sam smith": false, "sám": false, strings.Repeat("a", 33): false,
	} {
		if ValidUsername(name) != want {
			t.Errorf("ValidUsername(%q) != %v", name, want)
		}
	}
	// Ruling R51: free-form, 1 to 64 characters of any script.
	for name, want := range map[string]bool{
		"a": true, "ops": true, "a-b_c": true, "Ops": true, "Board Games": true, "a.b": true, "_ops": true, "#1 fans!": true,
		"設計チーム": true, "Übung 2": true, "🎲 games": true, strings.Repeat("c", 64): true, strings.Repeat("語", 64): true,
		"": false, " ": false, " ops": false, "ops ": false, "a  b": false, "a\tb": false, "a\nb": false, "a\x00b": false,
		"a\u0085b": false, strings.Repeat("c", 65): false, "bad\xffutf8": false,
	} {
		if ValidChannelName(name) != want {
			t.Errorf("ValidChannelName(%q) != %v", name, want)
		}
	}
	for in, want := range map[string]string{
		"  Board   Games ": "Board Games", "a\tb\n c": "a b c", "ops": "ops", "e\u0301t\u00e9": "\u00e9t\u00e9",
	} {
		if got, ok := CleanChannelName(in); !ok || got != want {
			t.Errorf("CleanChannelName(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
	for _, in := range []string{"", "   ", "a\x07b", strings.Repeat("c", 65)} {
		if _, ok := CleanChannelName(in); ok {
			t.Errorf("CleanChannelName(%q) was accepted", in)
		}
	}
	for name, want := range map[string]bool{"ok": true, "ship_it": true, "a": false, "no-dash": false, "No": false} {
		if ValidEmojiName(name) != want {
			t.Errorf("ValidEmojiName(%q) != %v", name, want)
		}
	}
	for _, r := range []string{"everyone", "admins", "channel", "here", "churr", "system", "me"} {
		if !ReservedName(r) {
			t.Errorf("%s is reserved", r)
		}
	}
	if clean, ok := CleanDisplayName("  Kaitý  "); !ok || clean != "Kaitý" {
		t.Errorf("display names are trimmed and normalised: %q %v", clean, ok)
	}
	for _, bad := range []string{"", "   ", "tab\there", strings.Repeat("x", 65)} {
		if _, ok := CleanDisplayName(bad); ok {
			t.Errorf("CleanDisplayName(%q) should be refused", bad)
		}
	}
	if !ValidShortID("t1") || ValidShortID("T1") || ValidShortID("") {
		t.Error("short identifiers are 1 to 32 of [a-z0-9_-]")
	}
}

func TestLevelsAndModes(t *testing.T) {
	if HighestLevel(LevelReader, LevelManager, LevelMember) != LevelManager || HighestLevel() != "" || HighestLevel("", LevelReader) != LevelReader {
		t.Fatal("the highest level")
	}
	if !LevelMember.AtLeast(LevelReader) || LevelReader.AtLeast(LevelMember) || Level("").AtLeast(LevelReader) || Level("boss").Valid() {
		t.Fatal("level order")
	}
	now := Time(1000)
	muted := Member{Mode: ModeNothing, ModeBeforeMute: ModeAny, MutedUntil: 500}
	if muted.ModeInForce(now) != ModeAny {
		t.Fatal("a mute whose time has passed is read as the mode before it")
	}
	muted.MutedUntil = 2000
	if muted.ModeInForce(now) != ModeNothing {
		t.Fatal("a mute in force is nothing")
	}
	inv := Invite{MaxUses: 1, ExpiresAt: 2000}
	if !inv.Usable(now) || (Invite{MaxUses: 1, Uses: 1}).Usable(now) || (Invite{RevokedAt: 5}).Usable(now) || (Invite{ExpiresAt: 900}).Usable(now) || !(Invite{MaxUses: 0, Uses: 99}).Usable(now) {
		t.Fatal("when an invite is usable")
	}
}

func TestEventJSON(t *testing.T) {
	e := Event{Type: EventMessageCreated, At: FromTime(time.Date(2026, 11, 13, 9, 30, 0, 0, time.UTC)), ConversationID: "c1", Seq: 7,
		Payload:   map[string]any{"message": Message{ID: "m1", ConversationID: "c1", AuthorID: "a1", Text: "hi", CreatedAt: 1}},
		MessageID: "m1", Only: map[AccountID]map[string]any{"a1": {"client_id": "x"}}}
	norm, raw, err := e.Normalize()
	if err != nil {
		t.Fatal(err)
	}
	var flat map[string]any
	json.Unmarshal(raw, &flat)
	if flat["type"] != "message.created" || flat["seq"] != float64(7) || flat["conversation_id"] != "c1" || flat["at"] != "2026-11-13T09:30:00.000Z" {
		t.Fatalf("an event is one flat object: %s", raw)
	}
	if strings.Contains(string(raw), "client_id") || strings.Contains(string(raw), "MessageID") {
		t.Fatalf("what is for one account only is never in the stored form: %s", raw)
	}
	if norm.Payload["message"].(map[string]any)["text"] != "hi" || norm.MessageID != "m1" || norm.Only == nil {
		t.Fatal("Normalize gives the JSON form and carries the rest over")
	}
	// An event with no conversation has no seq.
	b, _ := json.Marshal(Event{Type: "account.updated", At: 1, Payload: map[string]any{"account": 1}})
	if strings.Contains(string(b), "seq") || strings.Contains(string(b), "conversation_id") {
		t.Fatalf("an account event: %s", b)
	}
	if _, err := json.Marshal(Event{Type: "x.y", Payload: map[string]any{"seq": 3}}); err == nil {
		t.Fatal("a payload may not use a reserved field")
	}
	// The message object omits what is absent and never sends stored-only fields.
	m, _ := json.Marshal(Message{ID: "m", ConversationID: "c", AuthorID: "a", CreatedAt: 1, Version: 3, ReplyCount: 2})
	if strings.Contains(string(m), "null") || strings.Contains(string(m), "Version") || strings.Contains(string(m), "reply_count") || strings.Contains(string(m), "text") {
		t.Fatalf("the message object: %s", m)
	}
}
