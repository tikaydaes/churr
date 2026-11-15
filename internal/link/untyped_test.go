package link

import (
	"fmt"
	"os"
	"sort"
	"sync"
	"testing"

	"github.com/tikaydaes/churr/internal/link/wire"
)

// Everything servers seal to each other is a protobuf message of the link's
// schema (ruling R70). The codec carries an object the schema cannot hold as
// JSON text rather than lose it, which would hide a schema that has fallen
// behind the code; so every test of this package runs with that watched, and
// the run fails if it happened where a test did not say it would.

var untypedSeen = struct {
	mu      sync.Mutex
	allowed map[string]bool
	got     map[string]string
}{allowed: map[string]bool{}, got: map[string]string{}}

// AllowUntypedForTest says that a test sends a direct message or an event of
// this kind that the schema does not have, on purpose.
func AllowUntypedForTest(kind string) {
	untypedSeen.mu.Lock()
	untypedSeen.allowed[kind] = true
	untypedSeen.mu.Unlock()
}

func TestMain(m *testing.M) {
	wire.OnUntyped(func(union, kind string, why error) {
		untypedSeen.mu.Lock()
		if !untypedSeen.allowed[kind] {
			untypedSeen.got[union+" "+kind] = why.Error()
		}
		untypedSeen.mu.Unlock()
	})
	code := m.Run()
	untypedSeen.mu.Lock()
	var lines []string
	for k, why := range untypedSeen.got {
		lines = append(lines, k+": "+why)
	}
	untypedSeen.mu.Unlock()
	sort.Strings(lines)
	for _, l := range lines {
		fmt.Fprintln(os.Stderr, "FAIL: travelled as JSON, not as its protobuf message: "+l)
	}
	if len(lines) > 0 && code == 0 {
		code = 1
	}
	os.Exit(code)
}
