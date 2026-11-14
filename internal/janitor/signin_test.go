package janitor

import (
	"context"
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/blob"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
	"github.com/tikaydaes/churr/internal/store/storetest"
)

// The janitor removes an authenticator set-up that was begun and never
// confirmed an hour after it began, and sign-in codes an hour after they
// ran out. A confirmed authenticator and a code still in use are left.
func TestSignInLeftovers(t *testing.T) {
	ctx := context.Background()
	st := storetest.OpenSQLite(t)
	blobs, err := blob.Disk(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	j := New(Deps{Store: st, Blobs: blobs})
	account := func(name string) model.Account {
		a := model.Account{Username: name, DisplayName: name, Role: model.RoleMember}
		if _, err := st.Accounts().Create(ctx, &a); err != nil {
			t.Fatal(err)
		}
		return a
	}
	ann, bob := account("ann"), account("bob")
	ids := st.Identities()
	if err := ids.PutTotp(ctx, ann.ID, []byte("never confirmed")); err != nil {
		t.Fatal(err)
	}
	if err := ids.PutTotp(ctx, bob.ID, []byte("confirmed")); err != nil {
		t.Fatal(err)
	}
	if ok, err := ids.ConfirmTotp(ctx, bob.ID, model.Now(), 1); err != nil || !ok {
		t.Fatalf("confirming: %v %v", ok, err)
	}
	now := model.Now()
	code := func(hash string, expires model.Time) {
		if err := ids.CreateCode(ctx, []byte(hash), store.OAuthCode{AccountID: ann.ID, Kind: "authorization", ClientName: "app",
			RedirectURI: "churr://done", PKCEChallenge: "x", ExpiresAt: expires}); err != nil {
			t.Fatal(err)
		}
	}
	code("live", now.Add(3*time.Hour))
	code("ended", now.Add(time.Minute))

	has := func(account model.AccountID) bool {
		_, found, err := ids.GetTotp(ctx, account)
		if err != nil {
			t.Fatal(err)
		}
		return found
	}
	// A run at once removes nothing: the set-up is minutes old.
	if _, err := j.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if !has(ann.ID) || !has(bob.ID) {
		t.Fatal("a fresh authenticator set-up was removed")
	}
	// Two hours on.
	j.now = func() model.Time { return now.Add(2 * time.Hour) }
	rep, err := j.RunOnce(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if has(ann.ID) {
		t.Fatal("an unconfirmed authenticator set-up is still there after two hours")
	}
	if !has(bob.ID) {
		t.Fatal("a confirmed authenticator was removed")
	}
	if rep.Rows["totp"] != 1 {
		t.Fatalf("the report counts the row: %v", rep.Rows)
	}
	if _, err := ids.GetCode(ctx, []byte("ended")); model.CodeOf(err) != model.CodeNotFound {
		t.Fatalf("a code that ran out over an hour ago is still there: %v", err)
	}
	if _, err := ids.GetCode(ctx, []byte("live")); err != nil {
		t.Fatalf("a code that has not run out was removed: %v", err)
	}
}
