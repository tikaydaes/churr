package api

import (
	"context"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/themes"
)

// Colour themes (ruling R61): docs/openapi.yaml, area Themes.

func init() {
	register(func(a *API) {
		a.handle("listMyThemes", a.listMyThemes)
		a.handle("createTheme", a.createTheme)
		a.handle("getTheme", a.getTheme)
		a.handle("updateTheme", a.updateTheme)
		a.handle("deleteTheme", a.deleteTheme)
		a.handle("publishTheme", func(r *Request) error { return a.setThemePublished(r, true) })
		a.handle("unpublishTheme", func(r *Request) error { return a.setThemePublished(r, false) })
		a.handle("listThemeLibrary", a.listThemeLibrary)
		a.handle("addTheme", a.addTheme)
		a.handle("removeLibraryTheme", a.removeLibraryTheme)
		a.handle("setDefaultTheme", a.setDefaultTheme)
	})
}

// themeBody is the contract's ThemeInput.
type themeBody struct {
	Name    *string           `json:"name"`
	Kind    *string           `json:"kind"`
	Colours map[string]string `json:"colours"`
}

func (b themeBody) input() themes.Input {
	return themes.Input{Name: b.Name, Kind: b.Kind, Colours: b.Colours}
}

func (a *API) listMyThemes(r *Request) error {
	list, err := a.Themes.Mine(r.Ctx(), r.Actor())
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"themes": orEmpty(list)})
}

func (a *API) createTheme(r *Request) error {
	var body themeBody
	if err := r.Decode(&body); err != nil {
		return err
	}
	t, err := a.Themes.Create(r.Ctx(), r.Actor(), body.input())
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"theme": t})
}

func (a *API) getTheme(r *Request) error {
	id, err := PathID[model.ThemeID](r, "id")
	if err != nil {
		return err
	}
	t, err := a.Themes.Get(r.Ctx(), r.Actor(), id)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"theme": t})
}

func (a *API) updateTheme(r *Request) error {
	id, err := PathID[model.ThemeID](r, "id")
	if err != nil {
		return err
	}
	var body themeBody
	if err := r.Decode(&body); err != nil {
		return err
	}
	t, err := a.Themes.Update(r.Ctx(), r.Actor(), id, body.input())
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"theme": t})
}

func (a *API) deleteTheme(r *Request) error {
	id, err := PathID[model.ThemeID](r, "id")
	if err != nil {
		return err
	}
	if err := a.Themes.Delete(r.Ctx(), r.Actor(), id); err != nil {
		return err
	}
	return r.NoContent()
}

func (a *API) setThemePublished(r *Request, publish bool) error {
	id, err := PathID[model.ThemeID](r, "id")
	if err != nil {
		return err
	}
	t, err := a.Themes.SetPublished(r.Ctx(), r.Actor(), id, publish)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"theme": t})
}

func (a *API) listThemeLibrary(r *Request) error {
	page, err := r.Page()
	if err != nil {
		return err
	}
	items, next, err := a.Themes.Library(r.Ctx(), r.Actor(), r.R.URL.Query().Get("q"), page)
	if err != nil {
		return err
	}
	return r.JSON(list("themes", orEmpty(items), next))
}

func (a *API) addTheme(r *Request) error {
	id, err := PathID[model.ThemeID](r, "id")
	if err != nil {
		return err
	}
	t, err := a.Themes.Add(r.Ctx(), r.Actor(), id)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"theme": t})
}

func (a *API) removeLibraryTheme(r *Request) error {
	id, err := PathID[model.ThemeID](r, "id")
	if err != nil {
		return err
	}
	if err := a.Themes.Remove(r.Ctx(), r.Actor(), id); err != nil {
		return err
	}
	return r.NoContent()
}

func (a *API) setDefaultTheme(r *Request) error {
	var body struct {
		Kind    string `json:"kind"`
		ThemeID string `json:"theme_id"`
	}
	if err := r.Decode(&body); err != nil {
		return err
	}
	var theme model.ThemeID
	if body.ThemeID != "" {
		var err error
		if theme, err = BodyID[model.ThemeID]("theme_id", body.ThemeID); err != nil {
			return err
		}
	}
	dark, light, err := a.Themes.SetDefault(r.Ctx(), r.Actor(), body.Kind, theme)
	if err != nil {
		return err
	}
	// The defaults are in the server's description.
	a.serverChanged(r)
	return r.JSON(map[string]any{"default_themes": defaultThemes(dark, light)})
}

// defaultThemes is the contract's DefaultThemes.
func defaultThemes(dark, light *model.DefaultTheme) map[string]any {
	out := map[string]any{}
	if dark != nil {
		out["dark"] = dark
	}
	if light != nil {
		out["light"] = light
	}
	return out
}

// addDefaultThemes puts the server's default themes in its description
// (serverObject, server_ops.go).
func (a *API) addDefaultThemes(ctx context.Context, server map[string]any) {
	server["default_themes"] = map[string]any{}
	if a.Themes == nil {
		return
	}
	// The description is built from many settings, each read on its own;
	// a failure to read these is not worth failing it for.
	if dark, light, err := a.Themes.Defaults(ctx); err == nil {
		server["default_themes"] = defaultThemes(dark, light)
	}
}
