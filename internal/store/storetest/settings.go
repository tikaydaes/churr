package storetest

import (
	"encoding/json"
	"testing"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

func init() { register("settings", testSettings) }

func testSettings(t *testing.T, s store.Store) {
	_, found, err := s.Settings().Get(ctx, store.SettingRegistrationMode)
	must(t, err)
	eq(t, "unset key found", found, false)

	// Defaults come from the reader, not from a row.
	mode, err := store.SettingString(ctx, s, store.SettingRegistrationMode, "invite")
	must(t, err)
	eq(t, "default", mode, "invite")

	admin := newAccount(t, s, "admin", model.RoleOwner)
	must(t, s.Settings().Set(ctx, store.SettingRegistrationMode, "open", admin.ID))
	must(t, s.Settings().Set(ctx, store.SettingQuickCorrection, 45, ""))
	must(t, s.Settings().Set(ctx, store.SettingEditWindow, nil, ""))
	must(t, s.Settings().Set(ctx, store.SettingBlockingEnabled, false, ""))

	mode, err = store.SettingString(ctx, s, store.SettingRegistrationMode, "invite")
	must(t, err)
	eq(t, "stored string", mode, "open")
	n, err := store.SettingIntOr(ctx, s, store.SettingQuickCorrection, 30)
	must(t, err)
	eq(t, "stored int", n, int64(45))
	_, set, err := store.SettingInt(ctx, s, store.SettingEditWindow)
	must(t, err)
	eq(t, "null means not set", set, false)
	on, err := store.SettingBool(ctx, s, store.SettingBlockingEnabled, true)
	must(t, err)
	eq(t, "stored bool", on, false)

	// Setting again replaces.
	must(t, s.Settings().Set(ctx, store.SettingRegistrationMode, "closed", ""))
	all, err := s.Settings().All(ctx)
	must(t, err)
	eq(t, "number of keys", len(all), 4)
	var v string
	must(t, json.Unmarshal(all[store.SettingRegistrationMode], &v))
	eq(t, "replaced", v, "closed")

	must(t, s.Settings().Delete(ctx, store.SettingRegistrationMode))
	_, found, err = s.Settings().Get(ctx, store.SettingRegistrationMode)
	must(t, err)
	eq(t, "deleted key found", found, false)

	// A change inside a failed transaction is not kept.
	errBoom := model.ErrConflict
	err = s.Tx(ctx, func(tx store.Tx) error {
		if err := tx.Settings().Set(ctx, "server.name", "Nope", ""); err != nil {
			return err
		}
		return errBoom
	})
	wantErr(t, err, errBoom)
	_, found, err = s.Settings().Get(ctx, "server.name")
	must(t, err)
	eq(t, "rolled back", found, false)
}
