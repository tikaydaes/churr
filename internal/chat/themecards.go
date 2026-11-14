package chat

import (
	"context"

	"github.com/tikaydaes/churr/internal/model"
)

// ThemeLinks is what chat needs of the themes package: the card for a link
// to one of this server's own themes (ruling R61). themes.Service
// implements it; server connects the two.
type ThemeLinks interface {
	// ThemeCard returns the theme card for an address, and false if the
	// address is not a theme page of this server or names no theme. It
	// reads only this server's database: nothing is fetched.
	ThemeCard(ctx context.Context, address string) (model.Card, bool)
}

// SetThemeLinks turns theme cards on in the post path. A theme card is
// attached exactly as a link preview is (previews.go): it carries the
// link as preview_url, counts towards the three previews of a message,
// arrives with preview.added, and is removed with removePreview by the
// same people.
func (s *Service) SetThemeLinks(t ThemeLinks) { s.themeLinks = t }

// themeCard asks the themes package about one link of a message.
func (s *Service) themeCard(ctx context.Context, link string) (model.Card, bool) {
	if s.themeLinks == nil {
		return model.Card{}, false
	}
	return s.themeLinks.ThemeCard(ctx, link)
}
