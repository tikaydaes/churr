package secret

import (
	"bytes"
	"crypto/sha256"
	"regexp"
	"strings"
	"testing"
)

var recoveryShape = regexp.MustCompile(`^[0-9A-HJKMNP-TV-Z]{4}-[0-9A-HJKMNP-TV-Z]{4}-[0-9A-HJKMNP-TV-Z]{4}-[0-9A-HJKMNP-TV-Z]{4}$`)

func TestRecoveryAlphabet(t *testing.T) {
	if recoveryAlphabet != "0123456789ABCDEFGHJKMNPQRSTVWXYZ" {
		t.Errorf("alphabet = %q", recoveryAlphabet)
	}
}

func TestNewRecoveryCode(t *testing.T) {
	seen := map[string]bool{}
	used := map[rune]bool{}
	for i := 0; i < 2000; i++ {
		code := NewRecoveryCode()
		if !recoveryShape.MatchString(code) {
			t.Fatalf("code %q is not four groups of four from the alphabet", code)
		}
		if seen[code] {
			t.Fatalf("code %q was made twice", code)
		}
		seen[code] = true
		for _, r := range code {
			used[r] = true
		}
		// What is shown is what is accepted back.
		norm, ok := NormalizeRecoveryCode(code)
		if !ok || norm != strings.ReplaceAll(code, "-", "") {
			t.Fatalf("NormalizeRecoveryCode(%q) = %q, %v", code, norm, ok)
		}
	}
	// 32,000 characters: every one of the 32 turns up unless bits are stuck.
	for _, r := range recoveryAlphabet {
		if !used[r] {
			t.Errorf("character %q never appeared in 2000 codes", r)
		}
	}
}

func TestNormalizeRecoveryCode(t *testing.T) {
	const want = "7K2M9QXD04TVHB3N"
	good := []string{
		"7K2M-9QXD-04TV-HB3N",
		"7K2M9QXD04TVHB3N",
		"7k2m-9qxd-04tv-hb3n",       // case
		"7K2M 9QXD 04TV HB3N",       // spaces for hyphens
		"  7K2M-9QXD-04TV-HB3N\n",   // around it, as pasted
		"7K2M - 9QXD - 04TV - HB3N", // both
		"7-K-2-M-9-Q-X-D-0-4-T-V-H-B-3-N",
		"7K2M-9QXD-O4TV-HB3N",          // O for 0
		"7K2M-9QXD-o4TV-HB3N",          // o for 0
		"7K2M\t9QXD\u00a004TV\r\nHB3N", // tabs, a no-break space, a line end
	}
	for _, typed := range good {
		got, ok := NormalizeRecoveryCode(typed)
		if !ok || got != want {
			t.Errorf("NormalizeRecoveryCode(%q) = %q, %v; want %q", typed, got, ok, want)
		}
	}

	// I and L, in either case, are read as 1.
	for _, typed := range []string{"I111-1111-1111-1111", "l111-1111-1111-1111", "iLIl-iLIl-iLIl-iLIl", "LLLL-LLLL-LLLL-LLLL"} {
		got, ok := NormalizeRecoveryCode(typed)
		if !ok || got != "1111111111111111" {
			t.Errorf("NormalizeRecoveryCode(%q) = %q, %v; want sixteen 1s", typed, got, ok)
		}
	}
	if got, ok := NormalizeRecoveryCode("OOOO-oooo-0000-OoOo"); !ok || got != "0000000000000000" {
		t.Errorf("O was not read as 0: %q, %v", got, ok)
	}

	bad := []string{
		"",
		"-",
		"7K2M-9QXD-04TV-HB3",       // one short
		"7K2M-9QXD-04TV-HB3N7",     // one long
		"7K2M-9QXD-04TV-HB3U",      // U is not in the alphabet
		"7K2M-9QXD-04TV-HB3u",      //
		"7K2M-9QXD-04TV-HB3!",      // punctuation
		"7K2M_9QXD_04TV_HB3N",      // other separators are not dropped
		"7K2M-9QXD-04TV-HB3Ñ",      // a letter outside ASCII
		"7K2M-9QXD-04TV-HB3\u0131", // dotless i must not become I and then 1
		"7K2M-9QXD-04TV-HB3\u212a", // the Kelvin sign must not become K
		"７K2M-9QXD-04TV-HB3N",      // a full-width digit
		strings.Repeat("7", 1000),
	}
	for _, typed := range bad {
		if got, ok := NormalizeRecoveryCode(typed); ok {
			t.Errorf("NormalizeRecoveryCode(%q) accepted it as %q", typed, got)
		}
	}
}

func TestHashRecoveryCode(t *testing.T) {
	want := sha256.Sum256([]byte("7K2M9QXD04TVHB3N"))
	for _, typed := range []string{"7K2M-9QXD-04TV-HB3N", "7k2m 9qxd o4tv hb3n", "7K2M9QXD04TVHB3N"} {
		got, ok := HashRecoveryCode(typed)
		if !ok {
			t.Errorf("HashRecoveryCode(%q) refused it", typed)
			continue
		}
		// The hash is over the sixteen normalised characters, no hyphens.
		if !bytes.Equal(got, want[:]) {
			t.Errorf("HashRecoveryCode(%q) is not SHA-256 of the normalised code", typed)
		}
	}
	if h, ok := HashRecoveryCode("not a code"); ok || h != nil {
		t.Errorf("HashRecoveryCode accepted nonsense: %x, %v", h, ok)
	}
	a, _ := HashRecoveryCode("7K2M-9QXD-04TV-HB3N")
	b, _ := HashRecoveryCode("7K2M-9QXD-04TV-HB3M")
	if bytes.Equal(a, b) {
		t.Error("two codes have the same hash")
	}
}
