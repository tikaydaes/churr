package chat

import (
	"context"
	"slices"

	"github.com/tikaydaes/churr/internal/model"
)

// A person's own sidebar (ruling R83, docs/FEATURES.md section 21.4). Only
// its owner reads or changes it; nobody orders channels for anyone else.

// SidebarOp is one small change to a sidebar (schema SidebarOp).
type SidebarOp struct {
	Op             string                 `json:"op"`
	Key            string                 `json:"key,omitempty"`
	Name           string                 `json:"name,omitempty"`
	Index          *int                   `json:"index,omitempty"`
	Sort           string                 `json:"sort,omitempty"`
	ConversationID model.ConversationID   `json:"conversation_id,omitempty"`
	IDs            []model.ConversationID `json:"ids,omitempty"`
}

// Sidebar returns the actor's own layout.
func (s *Service) Sidebar(ctx context.Context, actor model.Account) (model.Sidebar, error) {
	if !actor.Active() {
		return model.Sidebar{}, model.ErrUnauthenticated
	}
	return s.store.Sidebars().Get(ctx, actor.ID)
}

// ChangeSidebar applies ops, in order and all or none, to the actor's
// layout as it stood at version. A layout that has moved on since is
// model.ErrConflict: the client reads it again and redoes its change.
func (s *Service) ChangeSidebar(ctx context.Context, actor model.Account, version int64, ops []SidebarOp) (model.Sidebar, error) {
	if err := s.personal(ctx, actor); err != nil {
		return model.Sidebar{}, err
	}
	if len(ops) == 0 || len(ops) > model.MaxSidebarOps {
		return model.Sidebar{}, model.Invalid("ops", "Give 1 to 50 changes.")
	}
	env, err := s.sidebarEnv(ctx, actor)
	if err != nil {
		return model.Sidebar{}, err
	}
	l, err := s.store.Sidebars().Update(ctx, actor.ID, func(l *model.Sidebar) error {
		if l.Version != version {
			return sidebarMoved
		}
		for _, op := range ops {
			if err := env.apply(l, op); err != nil {
				return err
			}
		}
		env.prune(l)
		return nil
	})
	if err != nil {
		return model.Sidebar{}, err
	}
	s.sidebarChanged(actor.ID, l)
	return l, nil
}

var sidebarMoved = &model.Error{Code: model.CodeConflict,
	Message: "Your sidebar was changed on another device. Read it again and redo the change."}

// starInSidebar keeps the order of stars when a membership is starred or
// unstarred: a new star goes last.
func (s *Service) starInSidebar(ctx context.Context, actor model.Account, id model.ConversationID, starred bool) {
	l, err := s.store.Sidebars().Update(ctx, actor.ID, func(l *model.Sidebar) error {
		l.Stars = slices.DeleteFunc(l.Stars, func(x model.ConversationID) bool { return x == id })
		if starred {
			l.Stars = append(l.Stars, id)
		}
		return nil
	})
	if err != nil {
		s.log.Warn("sidebar: keeping the order of stars", "err", err)
		return
	}
	s.sidebarChanged(actor.ID, l)
}

func (s *Service) sidebarChanged(to model.AccountID, l model.Sidebar) {
	s.hub.Send(to, model.Event{Type: model.EventSidebarChanged, At: s.now(), Payload: map[string]any{"sidebar": l}})
}

// sidebarEnv is what checking a change needs: the conversations the
// person is in and the admin's sections.
type sidebarEnv struct {
	member   map[model.ConversationID]bool
	sections []model.Section
}

func (s *Service) sidebarEnv(ctx context.Context, actor model.Account) (sidebarEnv, error) {
	ids, err := s.store.Members().ConversationIDs(ctx, actor.ID)
	if err != nil {
		return sidebarEnv{}, err
	}
	secs, err := s.store.Sections().List(ctx)
	if err != nil {
		return sidebarEnv{}, err
	}
	env := sidebarEnv{member: map[model.ConversationID]bool{}, sections: secs}
	for _, id := range ids {
		env.member[id] = true
	}
	return env, nil
}

// known says whether a heading key names something that exists.
func (e sidebarEnv) known(l *model.Sidebar, key string) bool {
	if n, ok := model.OwnSection(key); ok {
		return slices.ContainsFunc(l.Sections, func(x model.SidebarSection) bool { return x.ID == n })
	}
	switch key {
	case model.HeadingChannels, model.HeadingChats, model.HeadingSealed:
		return true
	}
	return slices.ContainsFunc(e.sections, func(x model.Section) bool { return "s:"+string(x.ID) == key })
}

// headings is the person's order of channel headings as it stands, with
// every one that exists in it.
func (e sidebarEnv) headings(l *model.Sidebar) []string {
	var def []string
	for _, x := range l.Sections {
		def = append(def, model.SectionKey(x.ID))
	}
	for _, x := range e.sections {
		def = append(def, "s:"+string(x.ID))
	}
	def = append(def, model.HeadingChannels)
	var out []string
	for _, k := range l.Headings {
		if slices.Contains(def, k) && !slices.Contains(out, k) {
			out = append(out, k)
		}
	}
	for _, k := range def {
		if !slices.Contains(out, k) {
			out = append(out, k)
		}
	}
	return out
}

func (e sidebarEnv) group(l *model.Sidebar, key string) *model.SidebarGroup {
	for i := range l.Groups {
		if l.Groups[i].Key == key {
			return &l.Groups[i]
		}
	}
	l.Groups = append(l.Groups, model.SidebarGroup{Key: key, Sort: model.DefaultSort(key), Order: []model.ConversationID{}})
	return &l.Groups[len(l.Groups)-1]
}

// ids checks a list of conversations: each one the person is in, none
// twice, not too many.
func (e sidebarEnv) ids(in []model.ConversationID) ([]model.ConversationID, error) {
	if len(in) > model.MaxSidebarOrder {
		return nil, model.Invalid("ids", "A custom order holds at most 2000 conversations.")
	}
	out := make([]model.ConversationID, 0, len(in))
	for _, id := range in {
		if !e.member[id] {
			return nil, model.Invalid("ids", "Only conversations you are in can be ordered.")
		}
		if !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	return out, nil
}

func validSort(v string) bool {
	return v == model.SortAlphabetical || v == model.SortActivity || v == model.SortCustom
}

func (e sidebarEnv) apply(l *model.Sidebar, op SidebarOp) error {
	needKey := func() error {
		if !e.known(l, op.Key) {
			return model.Invalid("key", "There is no such heading in your sidebar.")
		}
		return nil
	}
	switch op.Op {
	case "create_section":
		if len(l.Sections) >= model.MaxSidebarSections {
			return model.Invalid("name", "You can have at most 50 sections of your own.")
		}
		name, err := sectionName(op.Name)
		if err != nil {
			return err
		}
		next := 1
		for _, x := range l.Sections {
			next = max(next, x.ID+1)
		}
		l.Sections = append(l.Sections, model.SidebarSection{ID: next, Name: name})
		if op.Index != nil {
			hs := slices.DeleteFunc(e.headings(l), func(k string) bool { return k == model.SectionKey(next) })
			l.Headings = slices.Insert(hs, min(max(*op.Index, 0), len(hs)), model.SectionKey(next))
		}
	case "rename_section":
		n, ok := model.OwnSection(op.Key)
		i := slices.IndexFunc(l.Sections, func(x model.SidebarSection) bool { return x.ID == n })
		if !ok || i < 0 {
			return model.Invalid("key", "Only your own sections can be renamed.")
		}
		name, err := sectionName(op.Name)
		if err != nil {
			return err
		}
		l.Sections[i].Name = name
	case "delete_section":
		n, ok := model.OwnSection(op.Key)
		if !ok || !e.known(l, op.Key) {
			return model.Invalid("key", "Only your own sections can be deleted.")
		}
		// Its conversations go back to where the server puts them.
		l.Sections = slices.DeleteFunc(l.Sections, func(x model.SidebarSection) bool { return x.ID == n })
		l.Placements = slices.DeleteFunc(l.Placements, func(p model.SidebarPlacement) bool { return p.SectionID == n })
		l.Groups = slices.DeleteFunc(l.Groups, func(g model.SidebarGroup) bool { return g.Key == op.Key })
		l.Headings = slices.DeleteFunc(l.Headings, func(k string) bool { return k == op.Key })
	case "move_section":
		if err := needKey(); err != nil {
			return err
		}
		if op.Key == model.HeadingChats || op.Key == model.HeadingSealed {
			return model.Invalid("key", "Chats and Sealed stay at the bottom.")
		}
		if op.Index == nil {
			return model.Invalid("index", "Say where the heading goes.")
		}
		hs := slices.DeleteFunc(e.headings(l), func(k string) bool { return k == op.Key })
		l.Headings = slices.Insert(hs, min(max(*op.Index, 0), len(hs)), op.Key)
	case "set_sort":
		if err := needKey(); err != nil {
			return err
		}
		if !validSort(op.Sort) {
			return model.Invalid("sort", "sort is alphabetical, activity or custom.")
		}
		g := e.group(l, op.Key)
		g.Sort = op.Sort
		if op.IDs != nil {
			ids, err := e.ids(op.IDs)
			if err != nil {
				return err
			}
			g.Order = ids
		}
	case "reorder":
		if err := needKey(); err != nil {
			return err
		}
		ids, err := e.ids(op.IDs)
		if err != nil {
			return err
		}
		g := e.group(l, op.Key)
		g.Sort, g.Order = model.SortCustom, ids
	case "reset_section":
		if err := needKey(); err != nil {
			return err
		}
		l.Groups = slices.DeleteFunc(l.Groups, func(g model.SidebarGroup) bool { return g.Key == op.Key })
	case "reset_all":
		*l = model.Sidebar{Version: l.Version, Sections: []model.SidebarSection{}, Headings: []string{},
			Groups: []model.SidebarGroup{}, Placements: []model.SidebarPlacement{}, Stars: l.Stars}
	case "move":
		if !e.member[op.ConversationID] {
			return model.Invalid("conversation_id", "Only conversations you are in can be moved.")
		}
		if err := needKey(); err != nil {
			return err
		}
		l.Placements = slices.DeleteFunc(l.Placements, func(p model.SidebarPlacement) bool { return p.ConversationID == op.ConversationID })
		if n, ok := model.OwnSection(op.Key); ok {
			l.Placements = append(l.Placements, model.SidebarPlacement{ConversationID: op.ConversationID, SectionID: n})
		}
		// Any other key puts it back where the server lists it; the key
		// then names that heading, for the order below.
		if op.IDs != nil {
			ids, err := e.ids(op.IDs)
			if err != nil {
				return err
			}
			g := e.group(l, op.Key)
			g.Sort, g.Order = model.SortCustom, ids
		}
	case "order_stars":
		ids, err := e.ids(op.IDs)
		if err != nil {
			return err
		}
		var out []model.ConversationID
		for _, id := range ids {
			if slices.Contains(l.Stars, id) {
				out = append(out, id)
			}
		}
		for _, id := range l.Stars {
			if !slices.Contains(out, id) {
				out = append(out, id)
			}
		}
		l.Stars = out
	default:
		return model.Invalid("op", "op is create_section, rename_section, delete_section, move_section, set_sort, reorder, reset_section, reset_all, move or order_stars.")
	}
	return nil
}

// prune drops what no longer exists: conversations the person has left,
// and headings of sections an admin deleted.
func (e sidebarEnv) prune(l *model.Sidebar) {
	gone := func(id model.ConversationID) bool { return !e.member[id] }
	l.Placements = slices.DeleteFunc(l.Placements, func(p model.SidebarPlacement) bool { return gone(p.ConversationID) })
	l.Stars = slices.DeleteFunc(l.Stars, gone)
	l.Groups = slices.DeleteFunc(l.Groups, func(g model.SidebarGroup) bool { return !e.known(l, g.Key) })
	for i := range l.Groups {
		l.Groups[i].Order = slices.DeleteFunc(l.Groups[i].Order, gone)
	}
	l.Headings = slices.DeleteFunc(l.Headings, func(k string) bool { return !e.known(l, k) })
}
