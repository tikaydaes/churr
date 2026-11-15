package ratelimit

import (
	"bytes"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) lines() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	s := strings.TrimSpace(l.b.String())
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// A flood of refusals is a line at once, then one line an interval with
// the count, and nothing once they stop.
func TestRefusalsAreCountedNotFlooded(t *testing.T) {
	var out lockedBuffer
	r := NewRefusals(slog.New(slog.NewTextHandler(&out, nil)), "rate limited", 50*time.Millisecond)
	for i := 0; i < 500; i++ {
		r.Note("submits", "a.example", 2)
	}
	r.Note("submits", "b.example", 1)
	if got := out.lines(); len(got) != 2 || !strings.Contains(got[0], "level=WARN") || !strings.Contains(got[0], "from=a.example") || !strings.Contains(got[0], "refused=1") {
		t.Fatalf("after a flood from one sender and one refusal of another: %q", got)
	}
	deadline := time.Now().Add(5 * time.Second)
	for len(out.lines()) < 3 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	got := out.lines()
	if len(got) != 3 || !strings.Contains(got[2], "refused=499") || !strings.Contains(got[2], "refused_in_all=500") {
		t.Fatalf("the interval's line: %q", got)
	}
	time.Sleep(200 * time.Millisecond)
	if got := out.lines(); len(got) != 3 {
		t.Fatalf("lines were written with nothing refused: %q", got)
	}
	// A new run starts with a line at once again.
	r.Note("submits", "a.example", 2)
	if got := out.lines(); len(got) != 4 {
		t.Fatalf("a refusal after a quiet spell: %q", got)
	}
	var none *Refusals
	none.Note("x", "y", 0)
}
