// Package themes is colour themes (ruling R61): each account's own themes,
// the server's theme library of published ones, the server's default dark
// and light themes, and the theme card a link to one of this server's
// themes gets in a message.
//
// A theme stores only its base colours. The shape of those colours and the
// contrast of everything derived from them are checked here, with the
// arithmetic of package palette, before a theme is stored: a theme that
// would put text below 4.5:1 or a control edge below 3:1 is refused with
// invalid_value naming the pairing and its measured ratio.
package themes

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/url"
	"strings"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/perm"
	"github.com/tikaydaes/churr/internal/store"
	"github.com/tikaydaes/churr/internal/themes/palette"
)

// The account events of docs/EVENTS.md section "Themes". They go to every
// connection: anyone may browse the library.
const (
	// EventPublished: a theme entered the library, or a theme in it changed.
	EventPublished = "theme.published"
	// EventUnpublished: a theme left the library (unpublished, removed by
	// an admin, or deleted).
	EventUnpublished = "theme.unpublished"
)

// The settings holding the server's defaults: a copy of the chosen theme
// (model.DefaultTheme), so it stays whatever happens to the original.
const (
	SettingDefaultDark  = "themes.default_dark"
	SettingDefaultLight = "themes.default_light"
)

// PagePath is where a theme's page is on the server's own origin; a theme
// link is the origin, this, and the theme's id. The web pages serve it.
const PagePath = "/themes/"

// Hub is what themes needs of the live hub. live.Hub implements it.
type Hub interface {
	Broadcast(v any)
}

// Deps is what New needs.
type Deps struct {
	Store store.Store
	Hub   Hub
	// Origin is the server's own origin ("https://chat.example.org"), for
	// theme links.
	Origin string
	Log    *slog.Logger
}

// Service is the themes service.
type Service struct {
	store  store.Store
	hub    Hub
	origin *url.URL
	log    *slog.Logger
	now    func() model.Time
}

// New makes the service.
func New(d Deps) *Service {
	s := &Service{store: d.Store, hub: d.Hub, log: d.Log, now: model.Now}
	if s.log == nil {
		s.log = slog.Default()
	}
	if u, err := url.Parse(d.Origin); err == nil && u.Host != "" {
		s.origin = u
	}
	return s
}

// Link returns a theme's link: its page on this server.
func (s *Service) Link(id model.ThemeID) string {
	if s.origin == nil {
		return PagePath + string(id)
	}
	return s.origin.Scheme + "://" + s.origin.Host + PagePath + string(id)
}

// Input is the contract's ThemeInput. Nil fields are absent: on a new
// theme each is required, on a change absent ones stay.
type Input struct {
	Name    *string
	Kind    *string
	Colours map[string]string
}

// apply writes the given fields onto a theme and checks the whole result:
// the name, the kind, the colours' shape and their contrast.
func (in Input) apply(t *model.Theme) error {
	if in.Name != nil {
		name, ok := model.CleanDisplayName(*in.Name)
		if !ok {
			return model.Invalid("name", "A theme's name is 1 to 64 characters with no control characters.")
		}
		t.Name = name
	}
	if in.Kind != nil {
		t.Kind = *in.Kind
	}
	if in.Colours != nil {
		t.Colours = in.Colours
	}
	return check(t)
}

// check validates a theme as it would be stored, and writes its colours
// back in lower case.
func check(t *model.Theme) error {
	if t.Name == "" {
		return model.Invalid("name", "Give the theme a name.")
	}
	if t.Kind != palette.Dark && t.Kind != palette.Light {
		return model.Invalid("kind", "kind is dark or light.")
	}
	c, key, msg := palette.Parse(t.Colours)
	if msg != "" {
		field := "colours"
		if key != "" {
			field = "colours." + key
		}
		return model.Invalid(field, msg)
	}
	if !palette.KindFits(t.Kind, c) {
		if t.Kind == palette.Dark {
			return model.Invalid("kind", "A dark theme's ground (bg) must be darker than its ink.")
		}
		return model.Invalid("kind", "A light theme's ground (bg) must be lighter than its ink.")
	}
	if f := palette.Check(t.Kind, c); f != nil {
		return model.Invalid("colours", f.Message())
	}
	t.Colours = c.Hexes()
	return nil
}

// Mine lists the caller's own themes, oldest first.
func (s *Service) Mine(ctx context.Context, actor model.Account) ([]model.Theme, error) {
	if err := perm.CheckTheme(actor, perm.OwnThemes); err != nil {
		return nil, err
	}
	return s.store.Themes().ByOwner(ctx, actor.ID)
}

// Create stores a new theme for the caller, not published.
func (s *Service) Create(ctx context.Context, actor model.Account, in Input) (model.Theme, error) {
	if err := perm.CheckTheme(actor, perm.OwnThemes); err != nil {
		return model.Theme{}, err
	}
	switch {
	case in.Name == nil:
		return model.Theme{}, model.Invalid("name", "Give the theme a name.")
	case in.Kind == nil:
		return model.Theme{}, model.Invalid("kind", "Give the theme's kind: dark or light.")
	case in.Colours == nil:
		return model.Theme{}, model.Invalid("colours", "Give the theme's colours.")
	}
	t := model.Theme{OwnerID: actor.ID, CreatedAt: s.now()}
	if err := in.apply(&t); err != nil {
		return t, err
	}
	if err := s.store.Themes().Create(ctx, &t, model.MaxThemesPerAccount); err != nil {
		return model.Theme{}, err
	}
	return t, nil
}

// Get returns one theme. Its id is its link: the owner always sees it, and
// so does anyone signed in here who has the link, published or not.
func (s *Service) Get(ctx context.Context, actor model.Account, id model.ThemeID) (model.Theme, error) {
	if err := perm.CheckTheme(actor, perm.ReadTheme); err != nil {
		return model.Theme{}, err
	}
	return s.store.Themes().Get(ctx, id)
}

// own loads a theme the caller owns. Anyone else's answers not_found: the
// caller may know it exists, but it is not theirs to change.
func (s *Service) own(ctx context.Context, actor model.Account, a perm.ThemeAction, id model.ThemeID) (model.Theme, error) {
	if err := perm.CheckTheme(actor, a); err != nil {
		return model.Theme{}, err
	}
	t, err := s.store.Themes().Get(ctx, id)
	if err != nil {
		return t, err
	}
	if t.OwnerID != actor.ID {
		return model.Theme{}, model.ErrNotFound
	}
	return t, nil
}

// Update changes the caller's theme. A published theme stays published and
// the library hears of the change.
func (s *Service) Update(ctx context.Context, actor model.Account, id model.ThemeID, in Input) (model.Theme, error) {
	if _, err := s.own(ctx, actor, perm.OwnThemes, id); err != nil {
		return model.Theme{}, err
	}
	t, err := s.store.Themes().Update(ctx, id, s.now(), func(t *model.Theme) error {
		if t.OwnerID != actor.ID {
			return model.ErrNotFound
		}
		return in.apply(t)
	})
	if err != nil {
		return t, err
	}
	if t.Published {
		s.tell(EventPublished, map[string]any{"theme": t})
	}
	return t, nil
}

// Delete deletes the caller's theme, taking it out of the library if it
// was there. Copies others added, theme cards in messages and a server
// default made from it are copies and stay.
func (s *Service) Delete(ctx context.Context, actor model.Account, id model.ThemeID) error {
	t, err := s.own(ctx, actor, perm.OwnThemes, id)
	if err != nil {
		return err
	}
	if err := s.store.Themes().Delete(ctx, id); err != nil {
		return err
	}
	if t.Published {
		s.tell(EventUnpublished, map[string]any{"theme_id": id})
	}
	return nil
}

// SetPublished puts the caller's theme in the library or takes it out.
// Doing what is already so changes nothing and tells nobody.
func (s *Service) SetPublished(ctx context.Context, actor model.Account, id model.ThemeID, publish bool) (model.Theme, error) {
	if _, err := s.own(ctx, actor, perm.PublishTheme, id); err != nil {
		return model.Theme{}, err
	}
	changed := false
	t, err := s.store.Themes().Update(ctx, id, s.now(), func(t *model.Theme) error {
		if t.OwnerID != actor.ID {
			return model.ErrNotFound
		}
		if t.Published == publish {
			return errUnchanged
		}
		changed = true
		t.Published = publish
		if publish {
			t.PublishedAt = s.now()
		}
		return nil
	})
	if err == errUnchanged {
		return s.store.Themes().Get(ctx, id)
	}
	if err != nil {
		return t, err
	}
	if changed && publish {
		s.tell(EventPublished, map[string]any{"theme": t})
	} else if changed {
		s.tell(EventUnpublished, map[string]any{"theme_id": id})
	}
	return t, nil
}

// errUnchanged stops an update that would change nothing.
var errUnchanged = &model.Error{Code: model.CodeConflict, Message: "unchanged"}

// Library lists the server's published themes, most recently published
// first, optionally only those whose name contains query.
func (s *Service) Library(ctx context.Context, actor model.Account, query string, p store.Page) ([]model.Theme, string, error) {
	if err := perm.CheckTheme(actor, perm.OwnThemes); err != nil {
		return nil, "", err
	}
	query = strings.TrimSpace(query)
	if len([]rune(query)) > 64 {
		return nil, "", model.Invalid("q", "Search for at most 64 characters.")
	}
	return s.store.Themes().Library(ctx, query, p)
}

// Add gives the caller a copy of a theme: one from the library or any the
// caller has the link of. The copy is theirs, under the same name, not
// published; the original is not told.
func (s *Service) Add(ctx context.Context, actor model.Account, id model.ThemeID) (model.Theme, error) {
	if err := perm.CheckTheme(actor, perm.OwnThemes); err != nil {
		return model.Theme{}, err
	}
	src, err := s.Get(ctx, actor, id)
	if err != nil {
		return src, err
	}
	t := model.Theme{OwnerID: actor.ID, Name: src.Name, Kind: src.Kind, Colours: src.Colours, CreatedAt: s.now()}
	// Checked again: the arithmetic may have tightened since the original
	// was stored.
	if err := check(&t); err != nil {
		return model.Theme{}, err
	}
	if err := s.store.Themes().Create(ctx, &t, model.MaxThemesPerAccount); err != nil {
		return model.Theme{}, err
	}
	return t, nil
}

// Remove takes a theme out of the library, for an admin. The owner keeps
// the theme, unpublished. It is in the audit log.
func (s *Service) Remove(ctx context.Context, actor model.Account, id model.ThemeID) error {
	if err := perm.CheckTheme(actor, perm.ManageThemes); err != nil {
		return err
	}
	err := s.store.Tx(ctx, func(tx store.Tx) error {
		t, err := tx.Themes().Update(ctx, id, s.now(), func(t *model.Theme) error {
			if !t.Published {
				return model.ErrNotFound.With("That theme is not in the library.")
			}
			t.Published = false
			return nil
		})
		if err != nil {
			return err
		}
		return audit(ctx, tx, actor, "theme.removed", string(id), map[string]any{"name": t.Name, "owner_id": string(t.OwnerID)})
	})
	if err != nil {
		return err
	}
	s.tell(EventUnpublished, map[string]any{"theme_id": id})
	return nil
}

// SetDefault sets (theme non-empty) or clears the server's default theme
// of a kind, for an admin. The default is a copy of the theme, which must
// be of that kind; it is in the audit log. The caller tells everyone with
// server.updated, as the defaults are in the server's description.
func (s *Service) SetDefault(ctx context.Context, actor model.Account, kind string, theme model.ThemeID) (dark, light *model.DefaultTheme, err error) {
	if err := perm.CheckTheme(actor, perm.ManageThemes); err != nil {
		return nil, nil, err
	}
	key := SettingDefaultDark
	switch kind {
	case palette.Dark:
	case palette.Light:
		key = SettingDefaultLight
	default:
		return nil, nil, model.Invalid("kind", "kind is dark or light.")
	}
	var value *model.DefaultTheme
	if theme != "" {
		t, err := s.store.Themes().Get(ctx, theme)
		if err != nil {
			return nil, nil, err
		}
		if t.Kind != kind {
			return nil, nil, model.Invalid("theme_id", "That theme is not a "+kind+" theme.")
		}
		value = cardTheme(t)
	}
	err = s.store.Tx(ctx, func(tx store.Tx) error {
		if err := tx.Settings().Set(ctx, key, value, actor.ID); err != nil {
			return err
		}
		if value == nil {
			return audit(ctx, tx, actor, "theme.default_cleared", "", map[string]any{"kind": kind})
		}
		return audit(ctx, tx, actor, "theme.default_set", string(theme), map[string]any{"kind": kind, "name": value.Name})
	})
	if err != nil {
		return nil, nil, err
	}
	return s.Defaults(ctx)
}

// Defaults returns the server's default dark and light themes, nil where
// none is set. It needs no account: the server's description carries them
// so clients can apply them before sign-in.
func (s *Service) Defaults(ctx context.Context) (dark, light *model.DefaultTheme, err error) {
	read := func(key string) (*model.DefaultTheme, error) {
		raw, found, err := s.store.Settings().Get(ctx, key)
		if err != nil || !found {
			return nil, err
		}
		var d *model.DefaultTheme
		if json.Unmarshal(raw, &d) != nil || d == nil || d.ID == "" {
			return nil, nil
		}
		return d, nil
	}
	if dark, err = read(SettingDefaultDark); err != nil {
		return nil, nil, err
	}
	light, err = read(SettingDefaultLight)
	return dark, light, err
}

// ThemeCard returns the card for a link to one of this server's themes
// (chat.ThemeLinks): its page on this server's own origin, PagePath and the
// theme's id. Nothing is fetched; a link to anywhere else, or to a theme
// that does not exist, has no card here.
func (s *Service) ThemeCard(ctx context.Context, address string) (model.Card, bool) {
	id, ok := s.themeOf(address)
	if !ok {
		return model.Card{}, false
	}
	t, err := s.store.Themes().Get(ctx, id)
	if err != nil {
		return model.Card{}, false
	}
	return model.Card{Title: t.Name, URL: s.Link(t.ID), Theme: cardTheme(t)}, true
}

// themeOf reads the theme id out of a theme link of this server.
func (s *Service) themeOf(address string) (model.ThemeID, bool) {
	if s.origin == nil {
		return "", false
	}
	u, err := url.Parse(address)
	if err != nil || !strings.EqualFold(u.Scheme, s.origin.Scheme) || !strings.EqualFold(u.Host, s.origin.Host) {
		return "", false
	}
	id := strings.TrimSuffix(strings.TrimPrefix(u.Path, PagePath), "/")
	if !strings.HasPrefix(u.Path, PagePath) || !model.ValidID(id) {
		return "", false
	}
	return model.ThemeID(strings.ToLower(id)), true
}

// cardTheme is the copy of a theme a card or a default carries.
func cardTheme(t model.Theme) *model.CardTheme {
	return &model.CardTheme{ID: t.ID, Name: t.Name, Kind: t.Kind, Colours: t.Colours, AuthorID: t.OwnerID}
}

// tell sends an account event to every connection.
func (s *Service) tell(eventType string, fields map[string]any) {
	if s.hub == nil {
		return
	}
	s.hub.Broadcast(model.Event{Type: eventType, At: s.now(), Payload: fields})
}

// audit writes one line of the audit log inside a transaction.
func audit(ctx context.Context, tx store.Areas, actor model.Account, action, targetID string, detail map[string]any) error {
	kind := "theme"
	if targetID == "" {
		kind = ""
	}
	return tx.Audit().Append(ctx, model.AuditEntry{At: model.Now(), ActorID: string(actor.ID), ActorKind: "account",
		Action: action, TargetKind: kind, TargetID: targetID, Detail: detail})
}
