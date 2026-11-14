package version

import (
	"strings"
	"testing"
)

// set swaps the two variables for one test and puts them back afterwards.
func set(t *testing.T, v, c string) {
	t.Helper()
	oldV, oldC := Version, Commit
	Version, Commit = v, c
	t.Cleanup(func() { Version, Commit = oldV, oldC })
}

func TestDefaults(t *testing.T) {
	if Version != "dev" {
		t.Errorf("Version = %q, want dev", Version)
	}
	if Commit != "" {
		t.Errorf("Commit = %q, want empty", Commit)
	}
	// Whatever the build recorded, an unstamped build still says "dev" first.
	if s := String(); !strings.HasPrefix(s, "dev") {
		t.Errorf("String() = %q, want it to start with dev", s)
	}
}

func TestStringWithCommit(t *testing.T) {
	set(t, "1.4.0", "3fa1c0de")
	if got, want := String(), "1.4.0 (3fa1c0de)"; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
	if got := Revision(); got != "3fa1c0de" {
		t.Errorf("Revision() = %q", got)
	}
}

func TestStringShortensLongCommit(t *testing.T) {
	full := "3fa1c0de9b7a4c5d8e6f00112233445566778899"
	set(t, "1.4.0", full)
	if got, want := String(), "1.4.0 (3fa1c0de9b7a)"; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
	// Revision keeps the whole hash for anything that needs it.
	if got := Revision(); got != full {
		t.Errorf("Revision() = %q, want the full hash", got)
	}
}
