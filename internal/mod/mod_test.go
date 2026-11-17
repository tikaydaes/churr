package mod

import (
	"regexp"
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/perm"
)

// The matching rules of docs/FEATURES.md section 17.
func TestPattern(t *testing.T) {
	match := func(f model.WordFilter, text string) bool {
		return regexp.MustCompile(pattern(f)).MatchString(text)
	}
	plain := model.WordFilter{Pattern: "ass"}
	for text, want := range map[string]bool{
		"ass": true, "an ASS.": true, "(ass)": true, "ass!": true, "what an ass, really": true, "Ass\nline": true,
		"class": false, "assume": false, "passes": false, "ass_x": false, "ass9": false, "bäss": false,
	} {
		if match(plain, text) != want {
			t.Errorf("plain %q in %q: want %v", plain.Pattern, text, want)
		}
	}
	phrase := model.WordFilter{Pattern: "buy now"}
	if !match(phrase, "BUY NOW!") || match(phrase, "buy  now") || match(phrase, "rebuy now") {
		t.Error("a phrase matches as typed, at word boundaries")
	}
	// A plain pattern is never read as an expression.
	dots := model.WordFilter{Pattern: "a.c"}
	if match(dots, "abc") || !match(dots, "see a.c here") {
		t.Error("a plain pattern's punctuation is literal")
	}
	re := model.WordFilter{Pattern: `h+e+ck`, IsRegex: true}
	if !match(re, "HHEECK") || !match(re, "aheckb") || match(re, "hack") {
		t.Error("a regular expression matches without regard to case, anywhere")
	}
	if checkFilter(model.WordFilter{Pattern: "a(b", IsRegex: true, Action: model.FilterBlock}) == nil {
		t.Error("an expression that does not compile was accepted")
	}
	if checkFilter(model.WordFilter{Pattern: "a(b", Action: model.FilterBlock}) != nil {
		t.Error("a plain pattern with a bracket was refused")
	}
	// RE2 has no backreferences: what cannot be bounded is refused.
	if checkFilter(model.WordFilter{Pattern: `(a+)\1`, IsRegex: true, Action: model.FilterFlag}) == nil {
		t.Error("a backreference was accepted")
	}
}

// Slow mode exempts managers, bots and webhooks, and nobody else.
func TestSlowMode(t *testing.T) {
	now := model.Now()
	s := &Service{now: func() model.Time { return now }}
	conv := model.Conversation{ID: "c", Kind: model.KindChannel, Name: "c", SlowModeSeconds: 30}
	state := func(role model.Role, level model.Level, lastPost time.Duration) perm.State {
		a := model.Account{ID: "a", Role: role, ClaimedAt: 1}
		st := perm.State{Actor: a, Conversation: conv, Member: &model.Member{Level: level}}
		if lastPost >= 0 {
			st.Member.LastPostAt = now.Add(-lastPost)
		}
		return st
	}
	if got := s.slowModeWait(state(model.RoleMember, model.LevelMember, 10*time.Second)); got != 20 {
		t.Errorf("ten seconds into thirty: wait %d, want 20", got)
	}
	if got := s.slowModeWait(state(model.RoleMember, model.LevelMember, 29500*time.Millisecond)); got != 1 {
		t.Errorf("half a second left rounds up: %d", got)
	}
	for what, st := range map[string]perm.State{
		"after the interval":       state(model.RoleMember, model.LevelMember, 30*time.Second),
		"a first post":             state(model.RoleMember, model.LevelMember, -1),
		"a manager":                state(model.RoleMember, model.LevelManager, time.Second),
		"a moderator":              state(model.RoleMember, model.LevelModerator, time.Second),
		"a bot (and so a webhook)": state(model.RoleBot, model.LevelMember, time.Second),
	} {
		if got := s.slowModeWait(st); got != 0 {
			t.Errorf("%s must not wait, got %d", what, got)
		}
	}
	if got := s.slowModeWait(state(model.RoleGuest, model.LevelReader, time.Second)); got == 0 {
		t.Error("a guest is not exempt")
	}
	if got := s.slowModeWait(state(model.RoleAdmin, model.LevelMember, time.Second)); got == 0 {
		t.Error("an admin who is a member is not exempt (ruling R99)")
	}
	off := state(model.RoleMember, model.LevelMember, time.Second)
	off.Conversation.SlowModeSeconds = 0
	if s.slowModeWait(off) != 0 {
		t.Error("slow mode that is off holds nobody")
	}
}
