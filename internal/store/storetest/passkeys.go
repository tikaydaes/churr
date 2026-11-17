package storetest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

func init() { register("passkeys", testPasskeys) }

// passkeysExec runs one statement straight on the database through the
// backend's test hook and returns how many rows it touched. Tests use it
// only for tables no area reaches yet: to make a row another area depends
// on, or (with an update that sets a column to itself) to count rows.
func passkeysExec(t testing.TB, s store.Store, query string, args ...any) int64 {
	t.Helper()
	hook, ok := s.(interface {
		ExecForTest(ctx context.Context, query string, args ...any) (int64, error)
	})
	if !ok {
		t.Fatalf("the %s backend has no test hook for raw statements", s.Dialect())
	}
	n, err := hook.ExecForTest(ctx, query, args...)
	must(t, err)
	return n
}

// passkeysSameJSON fails the test unless two JSON values mean the same.
// PostgreSQL stores jsonb in its own spacing and key order, so bytes differ.
func passkeysSameJSON(t testing.TB, what string, got, want json.RawMessage) {
	t.Helper()
	var g, w any
	must(t, json.Unmarshal(got, &g))
	must(t, json.Unmarshal(want, &w))
	if !reflect.DeepEqual(g, w) {
		t.Fatalf("%s: got %s, want %s", what, got, want)
	}
}

func testPasskeys(t *testing.T, s store.Store) {
	ann := newAccount(t, s, "ann", model.RoleOwner)
	bob := newAccount(t, s, "bob", model.RoleMember)
	base := model.Now()

	add := func(t *testing.T, account model.AccountID, cred string, at model.Time) model.Passkey {
		t.Helper()
		p := model.Passkey{
			AccountID: account, CredentialID: []byte(cred), Credential: json.RawMessage(`{"counter":1,"key":"` + cred + `"}`),
			Name: "Key " + cred, CreatedAt: at,
		}
		must(t, s.Passkeys().Add(ctx, &p))
		return p
	}

	t.Run("add fills in defaults and get returns every column", func(t *testing.T) {
		p := model.Passkey{
			AccountID: ann.ID, CredentialID: []byte{0, 1, 2, 0xff}, Credential: json.RawMessage(`{"counter":7,"flags":["up","uv"]}`),
			Name: "Laptop", HasPRF: true, UnlockPublic: []byte("public-key"),
		}
		before := model.Now()
		must(t, s.Passkeys().Add(ctx, &p))
		eq(t, "id filled", model.ValidID(string(p.ID)), true)
		eq(t, "created_at filled", !p.CreatedAt.Before(before), true)

		got, err := s.Passkeys().Get(ctx, p.ID)
		must(t, err)
		eq(t, "id", got.ID, p.ID)
		eq(t, "account", got.AccountID, ann.ID)
		eq(t, "credential id", bytes.Equal(got.CredentialID, []byte{0, 1, 2, 0xff}), true)
		passkeysSameJSON(t, "credential", got.Credential, p.Credential)
		eq(t, "name", got.Name, "Laptop")
		eq(t, "has_prf", got.HasPRF, true)
		eq(t, "unlock_public", string(got.UnlockPublic), "public-key")
		eq(t, "created_at", got.CreatedAt, p.CreatedAt)
		eq(t, "last_used_at", got.LastUsedAt, model.Time(0))
		eq(t, "can_unlock", got.CanUnlock, false)

		// A passkey without the optional columns reads back without them.
		plain := add(t, ann.ID, "plain", base)
		got, err = s.Passkeys().Get(ctx, plain.ID)
		must(t, err)
		eq(t, "no prf", got.HasPRF, false)
		eq(t, "no unlock key", got.UnlockPublic == nil, true)

		_, err = s.Passkeys().Get(ctx, model.NewID[model.PasskeyID]())
		wantErr(t, err, model.ErrNotFound)
	})

	t.Run("a credential id is registered once", func(t *testing.T) {
		add(t, bob.ID, "once", base)
		dup := model.Passkey{AccountID: ann.ID, CredentialID: []byte("once"), Credential: json.RawMessage(`{}`), Name: "Copy"}
		wantErr(t, s.Passkeys().Add(ctx, &dup), model.ErrAlreadyExists)

		// The refusal must not spoil a transaction: PostgreSQL would
		// abort it on a failed statement.
		must(t, s.Tx(ctx, func(tx store.Tx) error {
			again := model.Passkey{AccountID: ann.ID, CredentialID: []byte("once"), Credential: json.RawMessage(`{}`), Name: "Copy"}
			if err := tx.Passkeys().Add(ctx, &again); !errors.Is(err, model.ErrAlreadyExists) {
				t.Errorf("in a transaction: got %v, want already_exists", err)
			}
			other := model.Passkey{AccountID: ann.ID, CredentialID: []byte("after-refusal"), Credential: json.RawMessage(`{}`), Name: "Next"}
			return tx.Passkeys().Add(ctx, &other)
		}))
		_, err := s.Passkeys().ByCredentialID(ctx, []byte("after-refusal"))
		must(t, err)
	})

	t.Run("by credential id", func(t *testing.T) {
		p := add(t, bob.ID, "find-me", base)
		got, err := s.Passkeys().ByCredentialID(ctx, []byte("find-me"))
		must(t, err)
		eq(t, "found", got.ID, p.ID)
		eq(t, "account", got.AccountID, bob.ID)
		_, err = s.Passkeys().ByCredentialID(ctx, []byte("nobody"))
		wantErr(t, err, model.ErrNotFound)
		_, err = s.Passkeys().ByCredentialID(ctx, nil)
		wantErr(t, err, model.ErrNotFound)
	})

	t.Run("for account is oldest first and count agrees", func(t *testing.T) {
		cat := newAccount(t, s, "cat", model.RoleMember)
		none, err := s.Passkeys().ForAccount(ctx, cat.ID)
		must(t, err)
		eq(t, "none yet", len(none), 0)
		n, err := s.Passkeys().Count(ctx, cat.ID)
		must(t, err)
		eq(t, "count of none", n, int64(0))

		third := add(t, cat.ID, "c3", base.Add(3*time.Second))
		first := add(t, cat.ID, "c1", base.Add(1*time.Second))
		second := add(t, cat.ID, "c2", base.Add(2*time.Second))
		list, err := s.Passkeys().ForAccount(ctx, cat.ID)
		must(t, err)
		eq(t, "how many", len(list), 3)
		eq(t, "oldest", list[0].ID, first.ID)
		eq(t, "middle", list[1].ID, second.ID)
		eq(t, "newest", list[2].ID, third.ID)
		n, err = s.Passkeys().Count(ctx, cat.ID)
		must(t, err)
		eq(t, "count", n, int64(3))
	})

	t.Run("used, rename and set prf", func(t *testing.T) {
		p := add(t, bob.ID, "worked", base)
		usedAt := base.Add(time.Minute)
		must(t, s.Passkeys().Used(ctx, p.ID, json.RawMessage(`{"counter":2,"key":"worked"}`), usedAt))
		got, err := s.Passkeys().Get(ctx, p.ID)
		must(t, err)
		passkeysSameJSON(t, "credential after use", got.Credential, json.RawMessage(`{"counter":2,"key":"worked"}`))
		eq(t, "last_used_at", got.LastUsedAt, usedAt)
		eq(t, "name untouched", got.Name, p.Name)

		must(t, s.Passkeys().Rename(ctx, p.ID, "Phone"))
		got, err = s.Passkeys().Get(ctx, p.ID)
		must(t, err)
		eq(t, "renamed", got.Name, "Phone")
		eq(t, "use time kept", got.LastUsedAt, usedAt)

		must(t, s.Passkeys().SetPRF(ctx, p.ID, true, []byte("unlock-1")))
		got, err = s.Passkeys().Get(ctx, p.ID)
		must(t, err)
		eq(t, "has_prf", got.HasPRF, true)
		eq(t, "key stored", string(got.UnlockPublic), "unlock-1")

		// nil leaves the key as it is, whichever way the flag goes.
		must(t, s.Passkeys().SetPRF(ctx, p.ID, true, nil))
		got, err = s.Passkeys().Get(ctx, p.ID)
		must(t, err)
		eq(t, "key kept", string(got.UnlockPublic), "unlock-1")
		must(t, s.Passkeys().SetPRF(ctx, p.ID, false, nil))
		got, err = s.Passkeys().Get(ctx, p.ID)
		must(t, err)
		eq(t, "flag cleared", got.HasPRF, false)
		eq(t, "key still kept", string(got.UnlockPublic), "unlock-1")
		must(t, s.Passkeys().SetPRF(ctx, p.ID, true, []byte("unlock-2")))
		got, err = s.Passkeys().Get(ctx, p.ID)
		must(t, err)
		eq(t, "key replaced", string(got.UnlockPublic), "unlock-2")

		missing := model.NewID[model.PasskeyID]()
		wantErr(t, s.Passkeys().Used(ctx, missing, json.RawMessage(`{}`), usedAt), model.ErrNotFound)
		wantErr(t, s.Passkeys().Rename(ctx, missing, "x"), model.ErrNotFound)
		wantErr(t, s.Passkeys().SetPRF(ctx, missing, true, nil), model.ErrNotFound)
	})

	t.Run("can unlock follows the backup key row, and delete takes it along", func(t *testing.T) {
		dee := newAccount(t, s, "dee", model.RoleMember)
		p := add(t, dee.ID, "unlocker", base)
		other := add(t, dee.ID, "bystander", base.Add(time.Second))

		// A copy locked some other way does not make a passkey able to
		// unlock.
		passkeysExec(t, s, `INSERT INTO backup_keys (id, account_id, kind, key_version, locked_key, created_at) VALUES (?, ?, 'phrase', 1, ?, ?)`,
			model.NewID[string](), string(dee.ID), []byte("locked"), int64(base))
		got, err := s.Passkeys().Get(ctx, p.ID)
		must(t, err)
		eq(t, "a phrase copy is not a passkey copy", got.CanUnlock, false)

		keyID := model.NewID[string]()
		passkeysExec(t, s, `INSERT INTO backup_keys (id, account_id, kind, passkey_id, key_version, locked_key, created_at) VALUES (?, ?, 'passkey', ?, 1, ?, ?)`,
			keyID, string(dee.ID), string(p.ID), []byte("locked"), int64(base))
		got, err = s.Passkeys().Get(ctx, p.ID)
		must(t, err)
		eq(t, "get", got.CanUnlock, true)
		got, err = s.Passkeys().ByCredentialID(ctx, []byte("unlocker"))
		must(t, err)
		eq(t, "by credential id", got.CanUnlock, true)
		list, err := s.Passkeys().ForAccount(ctx, dee.ID)
		must(t, err)
		eq(t, "how many", len(list), 2)
		eq(t, "the one with a copy", list[0].CanUnlock, true)
		eq(t, "the one without", list[1].CanUnlock, false)

		must(t, s.Passkeys().Delete(ctx, p.ID))
		_, err = s.Passkeys().Get(ctx, p.ID)
		wantErr(t, err, model.ErrNotFound)
		eq(t, "its backup key copy went by cascade",
			passkeysExec(t, s, `UPDATE backup_keys SET key_version = key_version WHERE id = ?`, keyID), int64(0))
		eq(t, "the phrase copy stayed",
			passkeysExec(t, s, `UPDATE backup_keys SET key_version = key_version WHERE account_id = ?`, string(dee.ID)), int64(1))
		_, err = s.Passkeys().Get(ctx, other.ID)
		must(t, err)
		n, err := s.Passkeys().Count(ctx, dee.ID)
		must(t, err)
		eq(t, "one left", n, int64(1))
		wantErr(t, s.Passkeys().Delete(ctx, p.ID), model.ErrNotFound)
	})

	t.Run("an access key is one per account, replaced, used and removed", func(t *testing.T) {
		eve := newAccount(t, s, "eve", model.RoleMember)
		_, found, err := s.Passkeys().AccessKey(ctx, eve.ID)
		must(t, err)
		eq(t, "none yet", found, false)
		must(t, s.Passkeys().SetAccessKey(ctx, eve.ID, []byte("first-hash"), []byte("first-unlock"), base))
		k, found, err := s.Passkeys().AccessKey(ctx, eve.ID)
		must(t, err)
		eq(t, "found", found, true)
		eq(t, "hash", string(k.AuthHash), "first-hash")
		eq(t, "unlock key", string(k.UnlockPublic), "first-unlock")
		eq(t, "made", k.CreatedAt, base)
		eq(t, "never used", k.LastUsedAt.IsZero(), true)
		must(t, s.Passkeys().AccessKeyUsed(ctx, eve.ID, base.Add(time.Minute)))
		k, _, _ = s.Passkeys().AccessKey(ctx, eve.ID)
		eq(t, "used", k.LastUsedAt, base.Add(time.Minute))
		details, err := s.Accounts().AdminDetails(ctx, []model.AccountID{eve.ID, ann.ID})
		must(t, err)
		eq(t, "admins see it", details[eve.ID].HasAccessKey && !details[ann.ID].HasAccessKey, true)

		must(t, s.Passkeys().SetAccessKey(ctx, eve.ID, []byte("second-hash"), nil, base.Add(time.Hour)))
		k, _, _ = s.Passkeys().AccessKey(ctx, eve.ID)
		eq(t, "replaced", string(k.AuthHash), "second-hash")
		eq(t, "the old unlock key goes with the old key", len(k.UnlockPublic), 0)
		eq(t, "replaced at", k.CreatedAt, base.Add(time.Hour))
		eq(t, "a new key is unused", k.LastUsedAt.IsZero(), true)

		removed, err := s.Passkeys().DeleteAccessKey(ctx, eve.ID)
		must(t, err)
		eq(t, "removed", removed, true)
		removed, err = s.Passkeys().DeleteAccessKey(ctx, eve.ID)
		must(t, err)
		eq(t, "removed twice", removed, false)
		wantErr(t, s.Passkeys().AccessKeyUsed(ctx, eve.ID, base), model.ErrNotFound)
	})
}
