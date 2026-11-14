package perm

import (
	"testing"

	"github.com/tikaydaes/churr/internal/model"
)

// Signing an account out everywhere and resetting its authenticator app
// follow the rule for acting on another account (docs/PERMISSIONS.md
// section 9.1, note 5): admins on members, guests and bots; only the owner
// on an admin; nobody on the owner.
func TestSignInAccountActions(t *testing.T) {
	acc := func(role model.Role) model.Account {
		return model.Account{ID: model.NewID[model.AccountID](), Role: role, ClaimedAt: model.Now()}
	}
	owner, admin, member, guest, bot := acc(model.RoleOwner), acc(model.RoleAdmin), acc(model.RoleMember), acc(model.RoleGuest), acc(model.RoleBot)
	for _, action := range []AccountAction{SignOutAccount, ResetTotp, MakeRecoveryLink} {
		cases := []struct {
			actor, target model.Account
			ok            bool
		}{
			{owner, admin, true}, {owner, member, true}, {owner, guest, true},
			{admin, member, true}, {admin, guest, true}, {admin, bot, true},
			{admin, acc(model.RoleAdmin), false}, {admin, owner, false},
			{member, guest, false}, {guest, member, false},
		}
		for _, c := range cases {
			err := CheckOnAccount(c.actor, action, c.target)
			if (err == nil) != c.ok {
				t.Errorf("%s by %s on %s: got %v, want ok=%v", action, c.actor.Role, c.target.Role, err, c.ok)
			}
		}
	}
	if err := CheckOnAccount(owner, SignOutAccount, owner); err == nil {
		t.Error("nobody signs the owner out through the console")
	}
}
