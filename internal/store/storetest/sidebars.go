package storetest

import (
	"testing"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

func init() { register("sidebars", testSidebars) }

func testSidebars(t *testing.T, s store.Store) {
	a := newAccount(t, s, "ann", model.RoleOwner)
	b := newAccount(t, s, "bob", model.RoleMember)

	l, err := s.Sidebars().Get(ctx, a.ID)
	must(t, err)
	eq(t, "none yet: empty at version 0", l.Version == 0 && len(l.Sections) == 0 && l.Stars != nil, true)

	l, err = s.Sidebars().Update(ctx, a.ID, func(l *model.Sidebar) error {
		l.Sections = append(l.Sections, model.SidebarSection{ID: 1, Name: "Work"})
		l.Groups = append(l.Groups, model.SidebarGroup{Key: "p:1", Sort: model.SortCustom, Order: []model.ConversationID{"c1", "c2"}})
		l.Placements = append(l.Placements, model.SidebarPlacement{ConversationID: "c1", Key: "p:1"})
		l.Headings = []string{"p:1", "s:"}
		return nil
	})
	must(t, err)
	eq(t, "first write is version 1", l.Version, int64(1))

	got, err := s.Sidebars().Get(ctx, a.ID)
	must(t, err)
	eq(t, "read back", got.Version == 1 && got.Sections[0].Name == "Work" && got.Groups[0].Order[1] == "c2" &&
		got.Placements[0].Key == "p:1" && len(got.Headings) == 2, true)
	other, _ := s.Sidebars().Get(ctx, b.ID)
	eq(t, "each account its own", other.Version, int64(0))

	_, err = s.Sidebars().Update(ctx, a.ID, func(l *model.Sidebar) error { return model.ErrForbidden })
	eq(t, "fn refuses: nothing written", model.CodeOf(err), model.CodeForbidden)
	got, _ = s.Sidebars().Get(ctx, a.ID)
	eq(t, "still version 1", got.Version, int64(1))

	l, err = s.Sidebars().Update(ctx, a.ID, func(l *model.Sidebar) error { l.Stars = []model.ConversationID{"c2"}; return nil })
	must(t, err)
	eq(t, "second write", l.Version == 2 && l.Sections[0].Name == "Work" && l.Stars[0] == "c2", true)

}
