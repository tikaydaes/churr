package people

import (
	"context"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/perm"
	"github.com/tikaydaes/churr/internal/store"
)

// groupColors are the colours a group may have: the card colours of
// docs/MESSAGE-FORMAT.md.
var groupColors = map[string]bool{"lavender": true, "cyan": true, "pink": true, "yellow": true, "green": true, "red": true, "grey": true}

// Groups lists the groups the actor can see: every group for an owner,
// admin or member; for a guest or bot the groups it is in and the groups
// added to its conversations.
func (s *Service) Groups(ctx context.Context, actor model.Account) ([]model.Group, error) {
	if !actor.Active() {
		return nil, model.ErrUnauthenticated
	}
	list, err := s.store.Groups().List(ctx, actor.ID)
	if err != nil || actor.Role.IsFullMember() {
		return list, err
	}
	out := list[:0]
	for _, g := range list {
		if s.perm.CanSeeGroup(ctx, actor, g.ID) == nil {
			out = append(out, g)
		}
	}
	return out, nil
}

// Group returns one group the actor can see.
func (s *Service) Group(ctx context.Context, actor model.Account, id model.GroupID) (model.Group, error) {
	g, err := s.store.Groups().Get(ctx, id, actor.ID)
	if err != nil {
		return g, err
	}
	if err := s.perm.CanSeeGroup(ctx, actor, id); err != nil {
		return model.Group{}, err
	}
	return g, nil
}

// GroupMembers lists the accounts in a group the actor can see, in order of
// their identifiers, a page at a time.
func (s *Service) GroupMembers(ctx context.Context, actor model.Account, id model.GroupID, p store.Page) ([]model.AccountID, string, error) {
	if _, err := s.Group(ctx, actor, id); err != nil {
		return nil, "", err
	}
	ids, err := s.store.Groups().MemberIDs(ctx, id)
	if err != nil {
		return nil, "", err
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	if p.Cursor != "" {
		var after string
		if err := store.DecodeCursor(p.Cursor, &after); err != nil {
			return nil, "", err
		}
		ids = ids[sort.Search(len(ids), func(i int) bool { return string(ids[i]) > after }):]
	}
	next := ""
	if limit := p.Clamp(); len(ids) > limit {
		ids = ids[:limit]
		next = store.EncodeCursor(string(ids[limit-1]))
	}
	return ids, next, nil
}

// groupAudience is who is told about a group: every owner, admin and
// member, and the guests and bots that can see it, which are among its own
// members and the members of the channels it is in.
func (s *Service) groupAudience(ctx context.Context, g model.Group) (to []model.AccountID, in map[model.AccountID]bool) {
	in = map[model.AccountID]bool{}
	seen := map[model.AccountID]bool{}
	add := func(ids []model.AccountID) {
		for _, id := range ids {
			if !seen[id] {
				seen[id] = true
				to = append(to, id)
			}
		}
	}
	fail := func(err error) {
		s.log.Error("could not work out who to tell about a group", "err", err)
	}
	if everyone, err := s.store.Groups().Builtin(ctx, model.GroupEveryone); err == nil {
		ids, err := s.store.Groups().MemberIDs(ctx, everyone.ID)
		if err != nil {
			fail(err)
		}
		add(ids)
	} else {
		fail(err)
	}
	members, err := s.store.Groups().MemberIDs(ctx, g.ID)
	if err != nil {
		fail(err)
	}
	for _, id := range members {
		in[id] = true
	}
	add(members)
	channels, err := s.store.Groups().Channels(ctx, g.ID)
	if err != nil {
		fail(err)
	}
	for _, c := range channels {
		ids, err := s.store.Members().IDs(ctx, c.ConversationID)
		if err != nil {
			fail(err)
		}
		add(ids)
	}
	return to, in
}

// tellGroup sends a group event to everyone who may see the group, each
// with its own `is_member`. extra is added to the event; also names
// accounts told whatever they can see (the one put in or taken out).
func (s *Service) tellGroup(eventType string, g model.Group, to []model.AccountID, in map[model.AccountID]bool, extra map[string]any, also ...model.AccountID) {
	seen := map[model.AccountID]bool{}
	for _, id := range append(to, also...) {
		if seen[id] {
			continue
		}
		seen[id] = true
		mine := g
		mine.IsMember = in[id]
		fields := map[string]any{"group": mine}
		for k, v := range extra {
			fields[k] = v
		}
		s.hub.Send(id, s.event(eventType, fields))
	}
}

// GroupInput is what CreateGroup and UpdateGroup take. Nil fields are
// absent.
type GroupInput struct {
	Handle        *string
	Name          *string
	Description   *string
	Color         *string
	Open          *bool
	ProviderID    *string
	ProviderGroup *string
}

// apply writes the given fields onto a group and checks the result.
func (in GroupInput) apply(g *model.Group) error {
	if in.Handle != nil {
		g.Handle = strings.ToLower(strings.TrimSpace(*in.Handle))
	}
	if in.Name != nil {
		g.Name = strings.TrimSpace(*in.Name)
	}
	if in.Description != nil {
		g.Description = strings.TrimSpace(*in.Description)
	}
	if in.Color != nil {
		g.Color = *in.Color
	}
	if in.Open != nil {
		g.Open = *in.Open
	}
	if in.ProviderID != nil {
		g.ProviderID = *in.ProviderID
	}
	if in.ProviderGroup != nil {
		g.ProviderGroup = strings.TrimSpace(*in.ProviderGroup)
	}
	bad := func(v string) bool { return strings.IndexFunc(v, unicode.IsControl) >= 0 }
	switch {
	case !model.ValidUsername(g.Handle):
		return model.Invalid("handle", "A handle is 2 to 32 characters: letters, digits, dot, - and _, starting with a letter or digit.")
	case g.Name == "" || utf8.RuneCountInString(g.Name) > 64 || bad(g.Name):
		return model.Invalid("name", "A group's name is 1 to 64 characters.")
	case utf8.RuneCountInString(g.Description) > 250 || bad(g.Description):
		return model.Invalid("description", "A description is at most 250 characters.")
	case g.Color != "" && !groupColors[g.Color]:
		return model.Invalid("color", "That is not one of the colours.")
	case (g.ProviderID == "") != (g.ProviderGroup == ""):
		return model.Invalid("provider_group", "Give both the provider and its group, or neither.")
	case g.ProviderID != "" && !model.ValidID(g.ProviderID):
		return model.Invalid("provider_id", "That is not an identifier.")
	case g.ProviderID != "" && g.Open:
		// Its members come from the provider at sign-in.
		return model.Invalid("open", "A group filled from a provider cannot be open.")
	}
	return nil
}

// CreateGroup makes an ordinary group. Admins only.
func (s *Service) CreateGroup(ctx context.Context, actor model.Account, in GroupInput) (model.Group, error) {
	if err := s.own(ctx, actor, perm.ManageGroups); err != nil {
		return model.Group{}, err
	}
	g := model.Group{CreatedAt: s.now()}
	if err := in.apply(&g); err != nil {
		return g, err
	}
	err := s.store.Tx(ctx, func(tx store.Tx) error {
		if err := tx.Groups().Create(ctx, &g); err != nil {
			return err
		}
		return s.audit(ctx, tx, actor, "group.created", "group", string(g.ID), map[string]any{"handle": g.Handle})
	})
	if err != nil {
		return g, err
	}
	to, in2 := s.groupAudience(ctx, g)
	s.tellGroup(model.EventGroupCreated, g, to, in2, nil)
	return s.store.Groups().Get(ctx, g.ID, actor.ID)
}

// editable loads a group an admin is about to change. The built-in groups
// cannot be changed (forbidden, case 108).
func (s *Service) editable(ctx context.Context, actor model.Account, id model.GroupID) (model.Group, error) {
	set, err := s.perm.Settings(ctx)
	if err != nil {
		return model.Group{}, err
	}
	// Asked before the group is read, so someone who is not an admin
	// learns nothing about which identifiers exist.
	if err := perm.CheckServer(actor, perm.ManageGroups, set); err != nil {
		return model.Group{}, err
	}
	g, err := s.store.Groups().Get(ctx, id, actor.ID)
	if err != nil {
		return g, err
	}
	return g, perm.CheckEditGroup(actor, g, set)
}

// UpdateGroup changes an ordinary group.
func (s *Service) UpdateGroup(ctx context.Context, actor model.Account, id model.GroupID, in GroupInput) (model.Group, error) {
	if _, err := s.editable(ctx, actor, id); err != nil {
		return model.Group{}, err
	}
	var g model.Group
	err := s.store.Tx(ctx, func(tx store.Tx) error {
		var err error
		g, err = tx.Groups().Update(ctx, id, func(g *model.Group) error { return in.apply(g) })
		if err != nil {
			return err
		}
		return s.audit(ctx, tx, actor, "group.updated", "group", string(id), map[string]any{"handle": g.Handle, "open": g.Open})
	})
	if err != nil {
		return g, err
	}
	if g, err = s.store.Groups().Get(ctx, id, actor.ID); err != nil {
		return g, err
	}
	to, in2 := s.groupAudience(ctx, g)
	s.tellGroup(model.EventGroupUpdated, g, to, in2, nil)
	return g, nil
}

// DeleteGroup deletes an ordinary group. Its members leave the channels
// they were in only through it.
func (s *Service) DeleteGroup(ctx context.Context, actor model.Account, id model.GroupID) error {
	g, err := s.editable(ctx, actor, id)
	if err != nil {
		return err
	}
	// Worked out before the group goes: afterwards nobody can see it.
	to, in := s.groupAudience(ctx, g)
	err = s.changeMembership(ctx, "", "group", func(tx store.Tx) (store.MembershipResult, error) {
		res, err := tx.Groups().Delete(ctx, id)
		if err != nil {
			return res, err
		}
		return res, s.audit(ctx, tx, actor, "group.deleted", "group", string(id), map[string]any{"handle": g.Handle})
	})
	if err != nil {
		return err
	}
	s.tellGroup(model.EventGroupDeleted, g, to, map[model.AccountID]bool{}, nil)
	_ = in
	return nil
}

// groupAccessAlert is the system notice kind of an arrival through a group
// an Owner or an Admin put the person into (chat.AlertGroupAccess).
const groupAccessAlert = "group_access"

// putIn puts an account in a group or takes it out, with the channel
// memberships that follow, and tells everyone who sees the group.
func (s *Service) putIn(ctx context.Context, actor model.Account, g model.Group, account model.AccountID, add, audit bool) (model.Group, error) {
	by := actor.ID
	if account == actor.ID {
		by = ""
	}
	// Taken before a removal, so a guest who could see the group only by
	// being in it is still told.
	before, _ := s.groupAudience(ctx, g)
	change := func(fn func(tx store.Tx) (store.MembershipResult, error)) error {
		if add && audit {
			// An Owner or an Admin putting someone, themself included,
			// into a group from the console: every private channel the
			// group brings them into is told who did it, through which
			// group (review finding H3).
			return s.changeMembershipLoud(ctx, actor.ID, "group", groupAccessAlert, g.Name, fn)
		}
		return s.changeMembership(ctx, "", "group", fn)
	}
	err := change(func(tx store.Tx) (store.MembershipResult, error) {
		var res store.MembershipResult
		var err error
		action := "group.member_added"
		if add {
			res, err = tx.Groups().AddMember(ctx, g.ID, account, by, s.now())
		} else {
			action = "group.member_removed"
			res, err = tx.Groups().RemoveMember(ctx, g.ID, account)
		}
		if err != nil || !audit {
			return res, err
		}
		return res, s.audit(ctx, tx, actor, action, "group", string(g.ID), map[string]any{"account_id": account})
	})
	if err != nil {
		return g, err
	}
	// Which conversations the account is in changed with the group.
	s.forget(account)
	fresh, err := s.store.Groups().Get(ctx, g.ID, actor.ID)
	if err != nil {
		return g, err
	}
	to, in := s.groupAudience(ctx, fresh)
	eventType := model.EventGroupMemberAdded
	if !add {
		eventType = model.EventGroupMemberRemoved
		to = append(to, before...)
	}
	s.tellGroup(eventType, fresh, to, in, map[string]any{"group_id": g.ID, "account_id": account}, account)
	return fresh, nil
}

// JoinGroup puts the caller in an open group, and so in every channel the
// group is in, at the group's level there.
func (s *Service) JoinGroup(ctx context.Context, actor model.Account, id model.GroupID) (model.Group, error) {
	return s.joinOrLeave(ctx, actor, id, true)
}

// LeaveGroup takes the caller out of an open group it is in.
func (s *Service) LeaveGroup(ctx context.Context, actor model.Account, id model.GroupID) (model.Group, error) {
	return s.joinOrLeave(ctx, actor, id, false)
}

func (s *Service) joinOrLeave(ctx context.Context, actor model.Account, id model.GroupID, join bool) (model.Group, error) {
	g, err := s.Group(ctx, actor, id)
	if err != nil {
		return g, err
	}
	set, err := s.perm.Settings(ctx)
	if err != nil {
		return g, err
	}
	if err := perm.CheckJoinGroup(actor, g, set); err != nil {
		return g, err
	}
	// Joining a group one is in, and leaving one one is not in, already
	// hold: the answer is the group as it is.
	if g.IsMember == join {
		return g, nil
	}
	return s.putIn(ctx, actor, g, actor.ID, join, false)
}

// AddGroupMember puts an account in a group. Admins only; local accounts
// only; not for a group filled from a provider, whose members come from
// sign-in (conflict).
func (s *Service) AddGroupMember(ctx context.Context, actor model.Account, id model.GroupID, account model.AccountID) error {
	return s.setGroupMember(ctx, actor, id, account, true)
}

// RemoveGroupMember takes an account out of a group.
func (s *Service) RemoveGroupMember(ctx context.Context, actor model.Account, id model.GroupID, account model.AccountID) error {
	return s.setGroupMember(ctx, actor, id, account, false)
}

func (s *Service) setGroupMember(ctx context.Context, actor model.Account, id model.GroupID, account model.AccountID, add bool) error {
	g, err := s.editable(ctx, actor, id)
	if err != nil {
		return err
	}
	if g.ProviderID != "" {
		return model.ErrConflict.With("This group is filled from a sign-in provider. Its members change when they sign in.")
	}
	target, err := s.store.Accounts().Get(ctx, account)
	if err != nil {
		return err
	}
	if add {
		if err := perm.CheckOnAccount(actor, perm.AddToGroup, target); err != nil {
			return err
		}
	}
	in, err := s.store.Groups().IsMember(ctx, id, account)
	if err != nil {
		return err
	}
	if in == add {
		if add {
			return nil // already there: the outcome asked for holds
		}
		return model.ErrNotFound.With("That account is not in the group.")
	}
	_, err = s.putIn(ctx, actor, g, account, add, true)
	return err
}
