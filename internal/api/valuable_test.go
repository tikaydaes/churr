package api_test

import (
	"regexp"
	"testing"

	"github.com/tikaydaes/churr/internal/api"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/testkit"
)

// TestValuableActsNeedAFreshCheck holds docs/AUTH.md section 10.4 for the
// acts worth more to a thief than a session (review finding C1): an Owner's
// or an Admin's personal token never does them, and a session that was not
// checked in the last ten minutes is asked for the check before anything
// else happens. A member is refused by the operation itself, as before.
func TestValuableActsNeedAFreshCheck(t *testing.T) {
	ts := testkit.NewServer(t)
	admin := ts.Admin("ada")
	member := ts.Member("ann")
	stale := ts.Owner.NewSession()
	stale.Stale()
	staleAdmin := admin.NewSession()
	staleAdmin.Stale()
	param := regexp.MustCompile(`\{[a-z_]+\}`)
	someID := string(model.NewID[model.AccountID]())
	n := 0
	for _, op := range api.Operations {
		if !api.Valuable(op.ID) {
			continue
		}
		n++
		path := param.ReplaceAllString(op.Path, someID)
		t.Run(op.ID, func(t *testing.T) {
			ts.Owner.Token().Refused(403, model.CodeForbidden, op.Method, path, map[string]any{})
			admin.Token().Refused(403, model.CodeForbidden, op.Method, path, map[string]any{})
			stale.Refused(403, model.CodeFreshCheckRequired, op.Method, path, map[string]any{})
			staleAdmin.Refused(403, model.CodeFreshCheckRequired, op.Method, path, map[string]any{})
			if status, out := member.Do(op.Method, path, map[string]any{}); status == 403 && out["error"].(map[string]any)["code"] == string(model.CodeFreshCheckRequired) {
				t.Fatalf("a member is asked for a fresh check instead of being refused: %v", out)
			}
		})
	}
	if n < 20 {
		t.Fatalf("only %d valuable operations", n)
	}
}
