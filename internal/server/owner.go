package server

import (
	"context"
	"fmt"
	"strings"

	"github.com/tikaydaes/churr/internal/config"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

// SetOwner is `churr owner grant` and `churr owner revoke`: the operator of
// the machine makes a person's account an owner, or an owner an admin, for
// the extreme case the instance cannot settle itself (ruling R99): its only
// owner is gone, or an owner must be stopped. It opens the database named by
// the configuration, changes the role as an owner's own change would, with
// the account moved in or out of the built-in groups, and writes it to the
// audit log with actor_kind cli. The instance keeps an owner: the last one
// cannot be revoked. The server must not be running, since it keeps roles in
// memory.
func SetOwner(cfg config.Serve, username string, grant bool) (model.Account, error) {
	ctx := context.Background()
	st, err := store.Open(ctx, cfg.Data, cfg.DB)
	if err != nil {
		return model.Account{}, fmt.Errorf("opening the database: %w", err)
	}
	defer st.Close()
	return SetOwnerIn(ctx, st, username, grant)
}

// SetOwnerIn is SetOwner on a store that is open already.
func SetOwnerIn(ctx context.Context, st store.Store, username string, grant bool) (model.Account, error) {
	acc, err := st.Accounts().ByUsername(ctx, strings.ToLower(strings.TrimSpace(username)))
	if err != nil {
		return model.Account{}, fmt.Errorf("there is no account %q on this instance", username)
	}
	role, action := model.RoleOwner, "owner.granted"
	switch {
	case grant && acc.Role == model.RoleOwner:
		return acc, nil
	case grant && (!acc.Active() || acc.Role == model.RoleBot):
		return acc, fmt.Errorf("%s is not an active person's account", acc.Username)
	case !grant && acc.Role != model.RoleOwner:
		return acc, fmt.Errorf("%s is not an owner", acc.Username)
	case !grant:
		role, action = model.RoleAdmin, "owner.revoked"
	}
	var out model.Account
	err = st.Tx(ctx, func(tx store.Tx) error {
		var err error
		if out, _, err = tx.Accounts().SetRole(ctx, acc.ID, role); err != nil {
			return err
		}
		return tx.Audit().Append(ctx, model.AuditEntry{At: model.Now(), ActorKind: "cli", Action: action,
			TargetKind: "account", TargetID: string(acc.ID), Detail: map[string]any{"username": acc.Username, "from": acc.Role, "to": role}})
	})
	return out, err
}
