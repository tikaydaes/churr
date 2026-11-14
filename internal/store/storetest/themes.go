package storetest

import (
	"sync"
	"testing"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

func init() { register("themes", testThemes) }

// themesColours is a set of base colours; the store does not check them.
func themesColours(bg string) map[string]string {
	return map[string]string{"bg": bg, "panel": "#161a3d", "raise": "#1d2250", "ink": "#efeaff", "dim": "#a09dc9",
		"lav": "#b9a0ff", "cyan": "#5df0e4", "pink": "#ff5ccf", "yel": "#ffd95e"}
}

func themesNew(t testing.TB, s store.Areas, owner model.AccountID, name string, at model.Time) model.Theme {
	t.Helper()
	th := model.Theme{OwnerID: owner, Name: name, Kind: "dark", Colours: themesColours("#0d1026"), CreatedAt: at}
	must(t, s.Themes().Create(ctx, &th, model.MaxThemesPerAccount))
	return th
}

func testThemes(t *testing.T, s store.Store) {
	ann := newAccount(t, s, "ann", model.RoleMember)
	bob := newAccount(t, s, "bob", model.RoleMember)

	t.Run("create, get, update, delete", func(t *testing.T) {
		th := themesNew(t, s, ann.ID, "Night", 1000)
		eq(t, "an id is given", th.ID != "", true)
		got, err := s.Themes().Get(ctx, th.ID)
		must(t, err)
		eq(t, "name", got.Name, "Night")
		eq(t, "owner", got.OwnerID, ann.ID)
		eq(t, "colours", got.Colours["bg"], "#0d1026")
		eq(t, "colours count", len(got.Colours), 9)
		eq(t, "not published", got.Published || !got.PublishedAt.IsZero(), false)
		eq(t, "updated starts as created", got.UpdatedAt, model.Time(1000))

		up, err := s.Themes().Update(ctx, th.ID, 2000, func(x *model.Theme) error {
			x.Name, x.Kind, x.Colours = "Dusk", "light", themesColours("#f5f2ff")
			x.Published, x.PublishedAt = true, 1500
			return nil
		})
		must(t, err)
		got, _ = s.Themes().Get(ctx, th.ID)
		eq(t, "updated", got.Name == "Dusk" && got.Kind == "light" && got.Colours["bg"] == "#f5f2ff" && got.UpdatedAt == 2000, true)
		eq(t, "published", got.Published && got.PublishedAt == 1500 && up.Published, true)
		eq(t, "created kept", got.CreatedAt, model.Time(1000))

		_, err = s.Themes().Update(ctx, th.ID, 3000, func(x *model.Theme) error { x.Published = false; return nil })
		must(t, err)
		got, _ = s.Themes().Get(ctx, th.ID)
		eq(t, "unpublished", got.Published || !got.PublishedAt.IsZero(), false)

		_, err = s.Themes().Update(ctx, th.ID, 4000, func(x *model.Theme) error { x.Name = "kept?"; return model.ErrConflict })
		wantErr(t, err, model.ErrConflict)
		got, _ = s.Themes().Get(ctx, th.ID)
		eq(t, "a refused update writes nothing", got.Name, "Dusk")

		must(t, s.Themes().Delete(ctx, th.ID))
		_, err = s.Themes().Get(ctx, th.ID)
		wantErr(t, err, model.ErrNotFound)
		wantErr(t, s.Themes().Delete(ctx, th.ID), model.ErrNotFound)
		_, err = s.Themes().Update(ctx, th.ID, 1, func(*model.Theme) error { return nil })
		wantErr(t, err, model.ErrNotFound)
	})

	t.Run("by owner and the limit", func(t *testing.T) {
		dee := newAccount(t, s, "dee", model.RoleMember)
		a := themesNew(t, s, dee.ID, "a", 10)
		b := themesNew(t, s, dee.ID, "b", 20)
		themesNew(t, s, bob.ID, "not dee's", 15)
		list, err := s.Themes().ByOwner(ctx, dee.ID)
		must(t, err)
		eq(t, "dee's, oldest first", len(list) == 2 && list[0].ID == a.ID && list[1].ID == b.ID, true)
		extra := model.Theme{OwnerID: dee.ID, Name: "c", Kind: "dark", Colours: themesColours("#000000"), CreatedAt: 30}
		wantErr(t, s.Themes().Create(ctx, &extra, 2), model.ErrConflict)
		// Ten at once against a limit of five: exactly three more get in.
		var wg sync.WaitGroup
		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				th := model.Theme{OwnerID: dee.ID, Name: "race", Kind: "dark", Colours: themesColours("#000000"), CreatedAt: 40}
				_ = s.Themes().Create(ctx, &th, 5)
			}()
		}
		wg.Wait()
		list, _ = s.Themes().ByOwner(ctx, dee.ID)
		eq(t, "the limit holds under concurrent creates", len(list), 5)
		ghost := model.Theme{OwnerID: model.NewID[model.AccountID](), Name: "x", Kind: "dark", Colours: themesColours("#000000"), CreatedAt: 1}
		wantErr(t, s.Themes().Create(ctx, &ghost, 50), model.ErrNotFound)
	})

	t.Run("library", func(t *testing.T) {
		eve := newAccount(t, s, "eve", model.RoleMember)
		var ids []model.ThemeID
		for i, name := range []string{"Ocean Night", "Forest", "ocean day", "Desert", "Plain"} {
			th := themesNew(t, s, eve.ID, name, model.Time(100+i))
			if name != "Plain" {
				_, err := s.Themes().Update(ctx, th.ID, model.Time(200+i), func(x *model.Theme) error {
					x.Published, x.PublishedAt = true, model.Time(300+i)
					return nil
				})
				must(t, err)
			}
			ids = append(ids, th.ID)
		}
		all, next, err := s.Themes().Library(ctx, "", store.Page{Limit: 2})
		must(t, err)
		eq(t, "first page: newest published first", len(all) == 2 && all[0].ID == ids[3] && all[1].ID == ids[2] && next != "", true)
		more, next2, err := s.Themes().Library(ctx, "", store.Page{Limit: 2, Cursor: next})
		must(t, err)
		eq(t, "second page", len(more) == 2 && more[0].ID == ids[1] && more[1].ID == ids[0] && next2 == "", true)
		found, _, err := s.Themes().Library(ctx, "OCEAN", store.Page{})
		must(t, err)
		eq(t, "name search ignores case", len(found) == 2 && found[0].ID == ids[2] && found[1].ID == ids[0], true)
		none, _, err := s.Themes().Library(ctx, "plain", store.Page{})
		must(t, err)
		eq(t, "an unpublished theme is not in the library", len(none), 0)
		pct, _, err := s.Themes().Library(ctx, "%", store.Page{})
		must(t, err)
		eq(t, "a wildcard is matched as itself", len(pct), 0)
		_, _, err = s.Themes().Library(ctx, "", store.Page{Cursor: "nonsense"})
		wantErr(t, err, model.Invalid("cursor", ""))
	})

	t.Run("removal takes the account's themes", func(t *testing.T) {
		owner := newAccount(t, s, "owner", model.RoleOwner)
		gone := newAccount(t, s, "gone", model.RoleMember)
		th := themesNew(t, s, gone.ID, "mine", 1)
		_, err := s.Accounts().Remove(ctx, gone.ID, owner.ID, model.Now())
		must(t, err)
		_, err = s.Themes().Get(ctx, th.ID)
		wantErr(t, err, model.ErrNotFound)
	})
}
