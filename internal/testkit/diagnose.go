package testkit

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// When a wait of a test times out (Eventually), the failure output says what
// every server, hub and hub's holder of the test was holding then: its link
// counters, what it holds aside and what for, the history it asked for and
// was not given, what it keeps to answer later, its record version and key
// epoch per stream, and what a hub put off (link.Service.Diagnose,
// hub.Hub.Diagnose). Each server and hub of a test registers itself here
// when it starts; nothing is read unless a wait fails, and the whole is at
// most maxDiagnosis bytes.

const (
	maxDiagnosis     = 48 << 10
	maxDiagnosisEach = 12 << 10
)

type diagnoser struct {
	name string
	fn   func(ctx context.Context, max int) string
}

var (
	diagMu    sync.Mutex
	diagnoses = map[testing.TB][]diagnoser{}
)

// diagnoseWith registers what a test's server or hub says of itself when a
// wait of the test fails; it is forgotten when the test ends.
func diagnoseWith(t testing.TB, name string, fn func(ctx context.Context, max int) string) {
	diagMu.Lock()
	first := len(diagnoses[t]) == 0
	diagnoses[t] = append(diagnoses[t], diagnoser{name, fn})
	diagMu.Unlock()
	if first {
		t.Cleanup(func() {
			diagMu.Lock()
			delete(diagnoses, t)
			diagMu.Unlock()
		})
	}
}

// diagnosis is what every server and hub of the test says of itself now.
func diagnosis(t testing.TB) string {
	diagMu.Lock()
	ds := append([]diagnoser(nil), diagnoses[t]...)
	diagMu.Unlock()
	if len(ds) == 0 {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var b strings.Builder
	b.WriteString("\n--- what each server held when the wait ran out ---\n")
	for _, d := range ds {
		if b.Len() >= maxDiagnosis {
			b.WriteString("[the rest cut]\n")
			break
		}
		out := func() (s string) {
			defer func() {
				if r := recover(); r != nil {
					s = fmt.Sprintf("(could not be read: %v)\n", r)
				}
			}()
			return d.fn(ctx, min(maxDiagnosisEach, maxDiagnosis-b.Len()))
		}()
		fmt.Fprintf(&b, "== %s\n%s", d.name, out)
	}
	return b.String()
}
