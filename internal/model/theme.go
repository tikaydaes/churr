package model

// ThemeID names a theme.
type ThemeID string

// Theme is a named set of base colours for one kind, dark or light (ruling
// R61, schema Theme). Everything else a client draws with is derived from
// these (docs/STYLE.md, "Themes"); the server checks the contrast of the
// result before it stores a theme (internal/themes/palette).
type Theme struct {
	ID      ThemeID   `json:"id"`
	OwnerID AccountID `json:"owner_id"`
	Name    string    `json:"name"`
	Kind    string    `json:"kind"` // dark | light
	// Colours holds exactly the base token names, each "#rrggbb".
	Colours     map[string]string `json:"colours"`
	Published   bool              `json:"published"`
	PublishedAt Time              `json:"published_at,omitempty"`
	CreatedAt   Time              `json:"created_at"`
	UpdatedAt   Time              `json:"updated_at"`
}

// MaxThemesPerAccount is how many themes one account may own.
const MaxThemesPerAccount = 50

// CardTheme is the theme a theme card shows: enough to draw its swatches
// and to offer "try" and "add" without another request. It is a copy made
// when the card was attached; the theme may change or go later.
type CardTheme struct {
	ID       ThemeID           `json:"id"`
	Name     string            `json:"name"`
	Kind     string            `json:"kind"`
	Colours  map[string]string `json:"colours"`
	AuthorID AccountID         `json:"author_id"`
}

// DefaultTheme is a server default (settings themes.default_dark and
// themes.default_light): a copy of the theme an admin chose, so it stays
// whatever later happens to the theme it was taken from.
type DefaultTheme = CardTheme
