package auth

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/model"
)

// The test vectors of RFC 6238 appendix B for SHA-1, cut to six digits.
func TestTotpVectors(t *testing.T) {
	key := []byte("12345678901234567890")
	for unix, want := range map[int64]string{
		59: "287082", 1111111109: "081804", 1111111111: "050471", 1234567890: "005924", 2000000000: "279037", 20000000000: "353130",
	} {
		if got := totpCode(key, unix/totpPeriod); got != want {
			t.Errorf("at %d: got %s, want %s", unix, got, want)
		}
	}
}

func TestTotpMatch(t *testing.T) {
	key := []byte("12345678901234567890")
	now := time.Unix(1111111111, 0)
	cur := totpStep(now)
	for offset, ok := range map[int64]bool{-2: false, -1: true, 0: true, 1: true, 2: false} {
		step, matched := totpMatch(key, totpCode(key, cur+offset), now)
		if matched != ok || (ok && step != cur+offset) {
			t.Errorf("a code %d steps away: matched=%v step=%d", offset, matched, step)
		}
	}
	// Spaces and hyphens a person types are ignored; a wrong length is not.
	code := totpCode(key, cur)
	if _, ok := totpMatch(key, code[:3]+" "+code[3:], now); !ok {
		t.Error("a code typed with a space was refused")
	}
	if _, ok := totpMatch(key, code[:5], now); ok {
		t.Error("a short code was accepted")
	}
	if _, ok := totpMatch(key, "", now); ok {
		t.Error("an empty code was accepted")
	}
}

// The redirect forms of docs/AUTH.md section 11.1: loopback by number with
// a port, or the apps' own scheme. Nothing else, and no fragment.
func TestRedirectAllowed(t *testing.T) {
	for addr, want := range map[string]bool{
		"http://127.0.0.1:53124/callback":     true,
		"http://[::1]:8080/cb":                true,
		"churr://oauth/callback":              true,
		"churr:/callback":                     true,
		"http://localhost:8080/cb":            false, // the name is not accepted
		"http://127.0.0.1/cb":                 false, // no port
		"https://127.0.0.1:8080/cb":           false,
		"https://evil.example/cb":             false,
		"http://127.0.0.1:8080/cb#frag":       false,
		"http://127.0.0.1:8080@evil.example/": false,
		"http://user@127.0.0.1:8080/cb":       false,
		"http://127.0.0.1.evil.example:80/":   false,
		"http://127.0.0.2:8080/cb":            false,
		"churr://x/cb#f":                      false,
		"javascript:alert(1)":                 false,
		"":                                    false,
		"//127.0.0.1:1/":                      false,
	} {
		if got := redirectAllowed(addr); got != want {
			t.Errorf("%q: got %v, want %v", addr, got, want)
		}
	}
}

func TestLocalPath(t *testing.T) {
	for in, want := range map[string]string{
		"/oauth/authorize?x=1": "/oauth/authorize?x=1", "": "/", "//evil.example": "/", "https://evil.example": "/",
		"/\\evil.example": "/", "relative": "/", "/ok": "/ok",
	} {
		if got := localPath(in); got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
}

func TestSuggestUsername(t *testing.T) {
	for in, want := range map[string]string{
		"Ann.Lee": "ann.lee", "ann@example.org": "ann", "  ": "", "Ünïcode": "ncode", "-lead": "lead",
		"a": "", "everyone": "", "ThisNameIsFarTooLongToBeAUsernameOnAnyServer": "thisnameisfartoolongtobeausernam",
	} {
		if got := suggestUsername(in); got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
}

// Admins and the owner can always use a passkey, whatever the policy;
// members and guests cannot under `sso`; bots and remote accounts never.
func TestPasskeyAllowed(t *testing.T) {
	for _, mode := range []string{"passkey", "either", "sso"} {
		p := policy{MemberSignIn: mode}
		for role, want := range map[model.Role]bool{
			model.RoleOwner: true, model.RoleAdmin: true, model.RoleMember: mode != "sso", model.RoleGuest: mode != "sso",
			model.RoleBot: false, model.RoleRemote: false,
		} {
			if got := p.passkeyAllowed(role); got != want {
				t.Errorf("policy %s, role %s: got %v, want %v", mode, role, got, want)
			}
		}
		if got := p.ssoOffered(); got != (mode != "passkey") {
			t.Errorf("policy %s: company sign-in offered = %v", mode, got)
		}
	}
}

func TestVault(t *testing.T) {
	v := newVault[int](time.Hour)
	v.put("a", 1)
	if got, ok := v.get("a"); !ok || got != 1 {
		t.Fatal("a stored value was not found")
	}
	if _, ok := v.take("a"); !ok {
		t.Fatal("take did not find the value")
	}
	if _, ok := v.take("a"); ok {
		t.Fatal("a value was taken twice")
	}
	v.putUntil("old", 2, time.Now().Add(-time.Second))
	if _, ok := v.get("old"); ok {
		t.Fatal("an expired value was returned")
	}
	// Past the cap the oldest entries go first.
	for i := 0; i < maxInMemory+5; i++ {
		v.putUntil(string(rune('a'+i%26))+time.Duration(i).String(), i, time.Now().Add(time.Duration(i+1)*time.Minute))
	}
	if len(v.m) > maxInMemory {
		t.Fatalf("the vault holds %d entries, over its cap of %d", len(v.m), maxInMemory)
	}
}

func TestSettingChecks(t *testing.T) {
	s := &Service{cfg: Config{Hostname: "chat.example.org"}}
	byKey := map[string]setting{}
	for _, st := range s.settings() {
		byKey[st.key] = st
	}
	cases := []struct {
		key, value string
		ok         bool
	}{
		{"server.name", `"Acme"`, true}, {"server.name", `""`, false}, {"server.name", `5`, false}, {"server.name", `null`, false},
		{"server.time_zone", `"Europe/London"`, true}, {"server.time_zone", `"Mars/Phobos"`, false},
		{"server.icon_file_id", `null`, true}, {"server.icon_file_id", `"nope"`, false},
		{"registration.mode", `"open"`, true}, {"registration.mode", `"anyone"`, false},
		{"invites.members_may_invite", `true`, true}, {"invites.members_may_invite", `"true"`, false}, {"invites.members_may_invite", `null`, false},
		{"auth.member_sign_in", `"sso"`, true}, {"auth.member_sign_in", `"password"`, false},
		{"auth.second_factor", `"required"`, true}, {"auth.second_factor", `"on"`, false},
		{"auth.second_factor_accepted", `["totp","passkey"]`, true}, {"auth.second_factor_accepted", `[]`, true},
		{"auth.second_factor_accepted", `["sms"]`, false}, {"auth.second_factor_accepted", `"totp"`, false}, {"auth.second_factor_accepted", `null`, false},
		{"sessions.passkey_seconds", `86400`, true}, {"sessions.passkey_seconds", `10`, false}, {"sessions.passkey_seconds", `"86400"`, false},
		{"sessions.sso_seconds", `3600`, true}, {"sessions.sso_seconds", `1`, false},
		{"messages.edit_window_seconds", `null`, true}, {"messages.edit_window_seconds", `600`, true}, {"messages.edit_window_seconds", `-1`, false},
		{"retention.seconds", `null`, true}, {"retention.seconds", `60`, false}, {"retention.seconds", `86400`, true},
		{"files.max_bytes", `1048576`, true}, {"files.max_bytes", `0`, false}, {"files.max_bytes", `1.5`, false},
		{"files.store", `"s3"`, true}, {"files.store", `"ftp"`, false},
		{"files.s3", `{"endpoint":"https://s3.example","region":"eu","bucket":"churr","prefix":"","path_style":true,"access_key_id":"AK"}`, true},
		{"files.s3", `{"bucket":"churr"}`, false}, {"files.s3", `{"endpoint":"x","bucket":"b","secret":"leak"}`, false}, {"files.s3", `null`, true},
		{"push.relay_url", `"https://relay.example"`, true}, {"push.relay_url", `"http://relay.example"`, false},
		{"calls.max_participants", `50`, true}, {"calls.max_participants", `1`, false},
		{"links.default_max_file_bytes", `0`, true}, {"welcome.text", `"Hello **all**"`, true},
	}
	for _, c := range cases {
		st, known := byKey[c.key]
		if !known {
			t.Errorf("no setting called %s", c.key)
			continue
		}
		if _, ok := st.check(json.RawMessage(c.value)); ok != c.ok {
			t.Errorf("%s = %s: accepted=%v, want %v", c.key, c.value, ok, c.ok)
		}
	}
	// Every key of docs/OPERATIONS.md section 5 an admin may change.
	for _, key := range []string{"server.name", "server.icon_file_id", "server.time_zone", "welcome.text", "registration.mode",
		"invites.members_may_invite", "auth.member_sign_in", "auth.second_factor", "auth.second_factor_accepted", "sessions.passkey_seconds",
		"sessions.sso_seconds", "messages.quick_correction_seconds", "messages.edit_window_seconds", "retention.seconds",
		"mentions.broadcast_managers_only", "mentions.broad_mention_limit", "emoji.members_may_add", "blocking.enabled", "webhooks.managers_may_approve", "files.max_bytes",
		"files.store", "files.s3", "push.relay_enabled", "push.relay_url", "push.unifiedpush_enabled", "push.send_badge", "previews.enabled",
		"sealed.enabled", "calls.enabled", "calls.max_participants", "calls.max_cameras", "links.send_presence", "links.default_max_file_bytes"} {
		if _, known := byKey[key]; !known {
			t.Errorf("the setting %s is missing", key)
		}
	}
}

func TestStringList(t *testing.T) {
	if l, ok := stringList("admins"); !ok || len(l) != 1 || l[0] != "admins" {
		t.Error("a single string is a list of one")
	}
	if l, ok := stringList([]any{"a", 1, "b"}); !ok || len(l) != 2 {
		t.Error("a list keeps its strings")
	}
	if l, ok := stringList([]any{}); !ok || l == nil {
		t.Error("an empty list is present and empty, not absent")
	}
	if _, ok := stringList(nil); ok {
		t.Error("an absent claim is not a list")
	}
}
