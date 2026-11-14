package perm

import "github.com/tikaydaes/churr/internal/model"

// ThemeAction is something done with colour themes (ruling R61, section
// 9.1 and its note [9]).
type ThemeAction string

// The theme actions.
const (
	// OwnThemes: make, change and delete one's own themes, browse the
	// library and add a copy of a theme to one's own.
	OwnThemes ThemeAction = "own_themes"
	// PublishTheme: put one's own theme in the library, or take it out.
	PublishTheme ThemeAction = "publish_theme"
	// ReadTheme: look at one theme by its id, which is its link.
	ReadTheme ThemeAction = "read_theme"
	// ManageThemes: remove a theme from the library; set the server's
	// default dark and light themes.
	ManageThemes ThemeAction = "manage_themes"
)

// CheckTheme answers a theme question, which depends only on the actor's
// role. Which theme is the caller's own is the themes service's to see:
// a theme someone else owns answers not_found for a change.
func CheckTheme(actor model.Account, a ThemeAction) error {
	if !actor.Active() {
		return model.ErrUnauthenticated
	}
	r := actor.Role
	ok := false
	switch a {
	case OwnThemes:
		// Guests use and add themes; bots own none.
		ok = r.IsFullMember() || r == model.RoleGuest
	case PublishTheme:
		ok = r.IsFullMember()
	case ReadTheme:
		// Anyone signed in here who has the link. (A remote account is
		// never active here, so it never gets this far.)
		ok = true
	case ManageThemes:
		ok = r.IsAdmin()
	}
	if !ok {
		return forbidden("Your role on this server does not allow that.")
	}
	return nil
}
