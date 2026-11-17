package server_test

import (
	"context"
	"testing"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/server"
	"github.com/tikaydaes/churr/internal/store"
	"github.com/tikaydaes/churr/internal/testkit"
)

// churr owner grant and revoke (ruling R99): the operator makes an owner
// and takes one away, never the last, and each is in the audit log as the
// command line's.
func TestOwnerCommand(t *testing.T) {
	ctx := context.Background()
	ts := testkit.NewServer(t)
	ann := ts.Member("ann")
	st := ts.Server.Store
	if _, err := server.SetOwnerIn(ctx, st, "nobody", true); err == nil {
		t.Fatal("granting to an account that does not exist")
	}
	if _, err := server.SetOwnerIn(ctx, st, "ann", false); err == nil {
		t.Fatal("revoking from someone who is not an owner")
	}
	acc, err := server.SetOwnerIn(ctx, st, "ANN", true)
	if err != nil || acc.Role != model.RoleOwner {
		t.Fatalf("granted: %+v %v", acc, err)
	}
	owner, _ := st.Accounts().Get(ctx, ts.Owner.ID)
	if acc, err = server.SetOwnerIn(ctx, st, owner.Username, false); err != nil || acc.Role != model.RoleAdmin {
		t.Fatalf("revoked: %+v %v", acc, err)
	}
	if _, err := server.SetOwnerIn(ctx, st, "ann", false); model.CodeOf(err) != model.CodeConflict {
		t.Fatalf("revoking the last owner: %v", err)
	}
	entries, _, err := st.Audit().List(ctx, store.AuditFilter{ActionPrefix: "owner."}, store.Page{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, e := range entries {
		if e.Action == "owner.created" {
			continue // first-run setup
		}
		if e.ActorKind != "cli" {
			t.Fatalf("an operator's change is the command line's: %+v", e)
		}
		seen[e.Action+" "+e.TargetID] = true
	}
	if !seen["owner.granted "+string(ann.ID)] || !seen["owner.revoked "+string(owner.ID)] || len(seen) != 2 {
		t.Fatalf("the audit log: %+v", entries)
	}
}
