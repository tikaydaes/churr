package secret

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"regexp"
	"strings"
	"testing"
)

var allPrefixes = map[string]string{
	"PrefixSession":     PrefixSession,
	"PrefixApp":         PrefixApp,
	"PrefixRefresh":     PrefixRefresh,
	"PrefixPersonal":    PrefixPersonal,
	"PrefixBot":         PrefixBot,
	"PrefixHook":        PrefixHook,
	"PrefixIntegration": PrefixIntegration,
	"PrefixLink":        PrefixLink,
}

func TestPrefixesMatchConventions(t *testing.T) {
	want := map[string]string{
		"PrefixSession":     "churr_ses_",
		"PrefixApp":         "churr_app_",
		"PrefixRefresh":     "churr_ref_",
		"PrefixPersonal":    "churr_pat_",
		"PrefixBot":         "churr_bot_",
		"PrefixHook":        "churr_hook_",
		"PrefixIntegration": "churr_int_",
		"PrefixLink":        "churr_link_",
	}
	for name, w := range want {
		if allPrefixes[name] != w {
			t.Errorf("%s = %q, want %q", name, allPrefixes[name], w)
		}
	}
	if len(prefixes) != len(want) {
		t.Errorf("TokenPrefix knows %d prefixes, want %d", len(prefixes), len(want))
	}
	// TokenPrefix can only be right if no prefix begins another.
	for _, a := range prefixes {
		for _, b := range prefixes {
			if a != b && strings.HasPrefix(a, b) {
				t.Errorf("prefix %q begins with prefix %q", a, b)
			}
		}
	}
}

var base64url43 = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)

func TestNewToken(t *testing.T) {
	for name, prefix := range allPrefixes {
		token, hash := NewToken(prefix)
		if !strings.HasPrefix(token, prefix) {
			t.Errorf("%s: token %q does not begin with %q", name, token, prefix)
		}
		body := strings.TrimPrefix(token, prefix)
		// 256 bits are 43 base64url characters, with no padding.
		if !base64url43.MatchString(body) {
			t.Errorf("%s: token body %q is not 43 base64url characters", name, body)
		}
		raw, err := base64.RawURLEncoding.DecodeString(body)
		if err != nil || len(raw) != 32 {
			t.Errorf("%s: token body decodes to %d bytes (%v), want 32", name, len(raw), err)
		}
		// The hash is over the whole string, prefix included.
		want := sha256.Sum256([]byte(token))
		if !bytes.Equal(hash, want[:]) {
			t.Errorf("%s: the hash is not SHA-256 of the whole token", name)
		}
		if !bytes.Equal(hash, HashToken(token)) {
			t.Errorf("%s: NewToken and HashToken disagree", name)
		}
		withoutPrefix := sha256.Sum256([]byte(body))
		if bytes.Equal(hash, withoutPrefix[:]) {
			t.Errorf("%s: the hash leaves the prefix out", name)
		}
		if got := TokenPrefix(token); got != prefix {
			t.Errorf("%s: TokenPrefix = %q, want %q", name, got, prefix)
		}
	}
}

func TestTokensDoNotRepeat(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 1000; i++ {
		token, _ := NewToken(PrefixSession)
		if seen[token] {
			t.Fatal("the same token was made twice")
		}
		seen[token] = true
	}
}

func TestHashToken(t *testing.T) {
	// SHA-256 of the empty string, to pin the algorithm.
	const empty = "\xe3\xb0\xc4\x42\x98\xfc\x1c\x14\x9a\xfb\xf4\xc8\x99\x6f\xb9\x24\x27\xae\x41\xe4\x64\x9b\x93\x4c\xa4\x95\x99\x1b\x78\x52\xb8\x55"
	if string(HashToken("")) != empty {
		t.Error("HashToken is not SHA-256")
	}
	if len(HashToken("churr_pat_x")) != 32 {
		t.Error("HashToken is not 32 bytes")
	}
	if bytes.Equal(HashToken("churr_pat_x"), HashToken("churr_bot_x")) {
		t.Error("the same body under two prefixes has the same hash")
	}
}

func TestTokenPrefix(t *testing.T) {
	cases := map[string]string{
		"churr_ses_abc":   PrefixSession,
		"churr_app_abc":   PrefixApp,
		"churr_ref_abc":   PrefixRefresh,
		"churr_pat_abc":   PrefixPersonal,
		"churr_bot_abc":   PrefixBot,
		"churr_hook_abc":  PrefixHook,
		"churr_int_abc":   PrefixIntegration,
		"churr_link_eyJ2": PrefixLink,
		"churr_ses_":      PrefixSession,
		"":                "",
		"churr_":          "",
		"churr_ses":       "",
		"churr_xyz_abc":   "",
		"CHURR_SES_abc":   "", // prefixes are exact
		" churr_ses_abc":  "",
		"Bearer churr_":   "",
		"abc":             "",
	}
	for token, want := range cases {
		if got := TokenPrefix(token); got != want {
			t.Errorf("TokenPrefix(%q) = %q, want %q", token, got, want)
		}
	}
}

func TestNewLinkValue(t *testing.T) {
	value, hash := NewLinkValue()
	if !base64url43.MatchString(value) {
		t.Errorf("link value %q is not 43 base64url characters", value)
	}
	if TokenPrefix(value) != "" {
		t.Errorf("link value %q carries a token prefix", value)
	}
	want := sha256.Sum256([]byte(value))
	if !bytes.Equal(hash, want[:]) {
		t.Error("the hash is not SHA-256 of the value")
	}
	other, _ := NewLinkValue()
	if other == value {
		t.Error("two link values are the same")
	}
}

func TestNewInviteCode(t *testing.T) {
	code := NewInviteCode()
	if !regexp.MustCompile(`^[A-Za-z0-9_-]{22}$`).MatchString(code) {
		t.Errorf("invite code %q is not 22 base64url characters", code)
	}
	raw, err := base64.RawURLEncoding.DecodeString(code)
	if err != nil || len(raw) != 16 {
		t.Errorf("invite code decodes to %d bytes (%v), want 16", len(raw), err)
	}
	if NewInviteCode() == code {
		t.Error("two invite codes are the same")
	}
}
