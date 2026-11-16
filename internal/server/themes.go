package server

import "github.com/tikaydaes/churr/internal/themes"

// wireThemes builds colour themes (ruling R61) and connects them: the API
// serves them, and chat asks them for the card of a link to one of this
// server's themes.
func (s *Server) wireThemes() {
	t := themes.New(themes.Deps{Store: s.Store, Hub: s.Live, Origin: s.Config.Origin(), Log: s.Log})
	s.Chat.SetThemeLinks(t)
	// Links to this server's messages (ruling R73).
	s.Chat.SetOrigin(s.Config.Origin())
	s.API.Themes = t
}
