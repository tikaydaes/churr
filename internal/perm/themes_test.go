package perm

import (
	"testing"

	"github.com/tikaydaes/churr/internal/model"
)

// Section 9.1 and cases 258 to 263 (ruling R61).
func TestCheckTheme(t *testing.T) {
	const owner, admin, member, guest, bot = model.RoleOwner, model.RoleAdmin, model.RoleMember, model.RoleGuest, model.RoleBot
	for _, c := range []struct {
		name string
		role model.Role
		a    ThemeAction
		ok   bool
	}{
		{"case 258 member makes a theme", member, OwnThemes, true},
		{"case 258 admin makes a theme", admin, OwnThemes, true},
		{"case 258 owner makes a theme", owner, OwnThemes, true},
		{"case 258 guest makes a theme", guest, OwnThemes, true},
		{"case 258 and 262 a bot owns no theme", bot, OwnThemes, false},
		{"case 259 guest publishes", guest, PublishTheme, false},
		{"case 259 member publishes", member, PublishTheme, true},
		{"bot publishes", bot, PublishTheme, false},
		{"case 261 bot reads a theme by its link", bot, ReadTheme, true},
		{"case 261 guest reads a theme by its link", guest, ReadTheme, true},
		{"case 263 member manages the library", member, ManageThemes, false},
		{"case 263 admin manages the library", admin, ManageThemes, true},
		{"case 263 owner manages the library", owner, ManageThemes, true},
		{"guest manages the library", guest, ManageThemes, false},
	} {
		err := CheckTheme(account(c.role), c.a)
		if (err == nil) != c.ok {
			t.Errorf("%s: %v", c.name, err)
		}
		if err != nil && model.CodeOf(err) != model.CodeForbidden {
			t.Errorf("%s: %v, want forbidden", c.name, err)
		}
	}
	if err := CheckTheme(model.Account{}, ReadTheme); model.CodeOf(err) != model.CodeUnauthenticated {
		t.Errorf("nobody signed in: %v", err)
	}
}
