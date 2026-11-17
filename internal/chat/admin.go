package chat

import (
	"context"
	"errors"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/perm"
	"github.com/tikaydaes/churr/internal/store"
)

// The admin view of channels (docs/PERMISSIONS.md section 4.1, note 1). An
// admin sees every named channel's name, members and managers, and may set
// its section, parent and retention there, without being able to read it;
// an Owner may also appoint its manager and add someone by owner override: nothing in this file returns a topic of a
// private channel, a message, a pin, a file or a count of messages. Ad-hoc
// channels and sealed chats do not exist as far as it is concerned, and
// every ordinary operation still answers an admin outside a private channel
// with not_found (decision P22).

// An admin may also set or clear a named channel's parent from here.

// adminChannel builds what the admin console shows about a channel.
func (s *Service) adminChannel(ctx context.Context, a store.Areas, c model.Conversation) (model.AdminChannel, error) {
	managers, err := a.Members().Managers(ctx, c.ID)
	if err != nil {
		return model.AdminChannel{}, err
	}
	if managers == nil {
		managers = []model.AccountID{}
	}
	out := model.AdminChannel{ID: c.ID, Name: c.Name, Private: c.Private, SectionID: c.SectionID, ParentID: c.ParentID, RetentionSeconds: c.RetentionSeconds,
		MemberCount: c.MemberCount, ManagerIDs: managers, CreatedAt: c.CreatedAt, ArchivedAt: c.ArchivedAt, Shared: c.IsShared(),
		Announcement: c.Announcement, Knock: c.IsKnock(), LockedAt: c.LockedAt}
	if c.Public() {
		out.Topic = c.Topic
	}
	return out, nil
}

// adminState answers one admin-console question about a channel. Anything
// that is not a named channel is not found.
func (s *Service) adminState(ctx context.Context, actor model.Account, id model.ConversationID, a perm.Action) (perm.State, error) {
	if err := s.perm.CanAdminister(ctx, actor, perm.UseAdminConsole); err != nil {
		return perm.State{}, err
	}
	st, err := s.perm.Load(ctx, actor, id)
	if err != nil {
		return st, hide(err)
	}
	return st, perm.Check(st, a)
}

// AdminChannels lists every named channel, public and private.
func (s *Service) AdminChannels(ctx context.Context, actor model.Account, f store.AdminChannelFilter, p store.Page) ([]model.AdminChannel, string, error) {
	if err := s.perm.CanAdminister(ctx, actor, perm.UseAdminConsole); err != nil {
		return nil, "", err
	}
	list, next, err := s.store.Conversations().AdminList(ctx, f, p)
	if err != nil {
		return nil, "", err
	}
	out := make([]model.AdminChannel, 0, len(list))
	for _, c := range list {
		ac, err := s.adminChannel(ctx, s.store, c)
		if err != nil {
			return nil, "", err
		}
		out = append(out, ac)
	}
	return out, next, nil
}

// AdminChannel returns a named channel's name, members and managers.
func (s *Service) AdminChannel(ctx context.Context, actor model.Account, id model.ConversationID) (model.AdminChannel, []model.Member, error) {
	st, err := s.adminState(ctx, actor, id, perm.AdminView)
	if err != nil {
		return model.AdminChannel{}, nil, err
	}
	ac, err := s.adminChannel(ctx, s.store, st.Conversation)
	if err != nil {
		return ac, nil, err
	}
	var members []model.Member
	p := store.Page{Limit: model.MaxLimit}
	for {
		list, next, err := s.store.Members().List(ctx, id, "", p)
		if err != nil {
			return ac, nil, err
		}
		members = append(members, list...)
		if next == "" {
			return ac, members, nil
		}
		p.Cursor = next
	}
}

// AdminUpdateChannel sets a named channel's section or retention period.
// An admin may set any period; 0 clears the channel's own, so that it
// follows the server's.
func (s *Service) AdminUpdateChannel(ctx context.Context, actor model.Account, id model.ConversationID, section *model.SectionID, retention *int64, parent *model.ConversationID) (model.AdminChannel, error) {
	st, err := s.adminState(ctx, actor, id, perm.AdminSetRetention)
	if err != nil {
		return model.AdminChannel{}, err
	}
	if section != nil && *section != "" {
		if _, err := s.store.Sections().Get(ctx, *section); err != nil {
			if errors.Is(err, model.ErrNotFound) {
				return model.AdminChannel{}, model.Invalid("section_id", "There is no such section.")
			}
			return model.AdminChannel{}, err
		}
	}
	if retention != nil && *retention < 0 {
		return model.AdminChannel{}, model.Invalid("retention_seconds", "A retention period is 0 or more seconds.")
	}
	if parent != nil {
		if err := s.setParent(ctx, st, *parent, true); err != nil {
			return model.AdminChannel{}, err
		}
	}
	var out model.AdminChannel
	err = s.Tx(ctx, func(tx store.Tx, rec *Recorder) error {
		var was *int64
		updated, err := tx.Conversations().Update(ctx, id, func(cur *model.Conversation) error {
			if section != nil {
				cur.SectionID = *section
			}
			if parent != nil {
				cur.ParentID = *parent
			}
			if retention != nil {
				was = cur.RetentionSeconds
				cur.RetentionSeconds = retention
				if *retention == 0 {
					cur.RetentionSeconds = nil
				}
			}
			return nil
		})
		if err != nil {
			return err
		}
		if retention != nil {
			if err := s.audit(ctx, tx, updated, actor.ID, "channel.retention_changed", "conversation", string(id),
				map[string]any{"from": was, "to": updated.RetentionSeconds}); err != nil {
				return err
			}
		}
		if err := s.conversationEvent(ctx, tx, rec, model.EventConversationUpdated, updated, actor.ID); err != nil {
			return err
		}
		out, err = s.adminChannel(ctx, tx, updated)
		return err
	})
	return out, err
}

// AppointManager is an Owner or an Admin making someone a manager of a named
// channel that has lost its managers or has a rogue one (rulings R99,
// R104), without being
// in it or reading it. In a public channel the account may be anyone of the
// instance who may join it, and joins as part of the appointment; in a knock
// channel anyone who may ask to join, added and announced as an add is. In a
// private channel, or a locked one, the account must already be in it, or
// override must be set: it is then added by owner override first, with the
// notice that goes with that (section 6.3). A channel a hub owns has the
// managers on the hub's list and is refused.
func (s *Service) AppointManager(ctx context.Context, actor model.Account, id model.ConversationID, account model.AccountID, override bool) (model.Member, error) {
	st, err := s.adminState(ctx, actor, id, perm.ConsoleAppointManager)
	if err != nil {
		return model.Member{}, err
	}
	target, err := s.store.Accounts().Get(ctx, account)
	if err != nil || target.Removed() {
		return model.Member{}, model.ErrNotFound.With("There is no such account.")
	}
	if err := perm.LevelFor(target, model.LevelManager); err != nil {
		return model.Member{}, err
	}
	if target.Role == model.RoleBot {
		return model.Member{}, model.ErrForbidden.With("A bot is made a manager by the channel's managers.")
	}
	row, found, err := s.store.Members().Get(ctx, id, account)
	if err != nil {
		return model.Member{}, err
	}
	if found && row.Level == model.LevelManager {
		return row, nil
	}
	c := st.Conversation
	if !found {
		if err := s.overrideTarget(ctx, c, actor, target); err != nil {
			return model.Member{}, err
		}
		if !c.Public() && (!c.IsKnock() || c.Locked()) && !override {
			return model.Member{}, model.ErrConflict.With("That person is not in this private channel. Add them by override, which every member is shown.")
		}
		if c.IsShared() {
			return model.Member{}, model.ErrForbidden.With("In a channel shared with other servers, appoint someone who is already in it.")
		}
	}
	var out model.Member
	err = s.Tx(ctx, func(tx store.Tx, rec *Recorder) error {
		var res store.MembershipResult
		var err error
		switch {
		case found:
			res, err = tx.Members().SetDirectLevel(ctx, id, account, model.LevelManager)
		case c.Public() || (c.IsKnock() && !c.Locked()):
			res, err = tx.Members().AddDirect(ctx, id, account, model.LevelManager, s.now())
		default:
			rec.override = true
			res, err = tx.Members().AddByOverride(ctx, id, account, model.LevelManager, s.now())
			if err == nil {
				err = s.audit(ctx, tx, c, actor.ID, "channel.owner_override", "account", string(account),
					map[string]any{"conversation_id": id})
			}
		}
		if err != nil {
			return err
		}
		if err := s.audit(ctx, tx, c, actor.ID, "channel.manager_appointed", "account", string(account),
			map[string]any{"conversation_id": id, "by_owner": true}); err != nil {
			return err
		}
		if err := rec.Membership(ctx, res, actor.ID, ""); err != nil {
			return err
		}
		m, found, err := tx.Members().Get(ctx, id, account)
		if err == nil && !found {
			err = model.ErrNotFound
		}
		out = m
		return err
	})
	return out, hide(err)
}

// overrideTarget is who an Owner or an Admin may put into a channel from
// the console:
// an active account of this instance, not a bot (its channel's managers add
// those), not banned from the channel, not a guest in a channel shared with
// other servers (ruling R99), and not the Owner or Admin themselves unless
// the channel is public.
func (s *Service) overrideTarget(ctx context.Context, c model.Conversation, actor, target model.Account) error {
	switch {
	case target.ID == actor.ID && !c.Public():
		// Nobody gains anything by an override: not their own way in.
		return model.ErrForbidden.With("You cannot add yourself to a private channel. One of its members can.")
	case !target.Local() || !target.Active():
		return model.ErrForbidden.With("Only an active account of this instance can be added.")
	case target.Role == model.RoleBot:
		return model.ErrForbidden.With("A bot is added by the channel's managers.")
	case target.Role == model.RoleGuest && c.IsShared():
		return model.ErrForbidden.With("A guest cannot be in a channel shared with other servers.")
	}
	return s.notBanned(ctx, s.store, c.ID, target.ID)
}

// OverrideAdd is an Owner or an Admin putting someone into a private
// channel, locked or not, without being in it or reading it (section 6.3,
// ruling R104). Every member is shown an owner_override notice naming who
// did it, which nobody can turn off, and it is written to the audit log.
// The person arrives as any newcomer does: they read from their first join
// (section 4.4). Whoever did it gains nothing.
func (s *Service) OverrideAdd(ctx context.Context, actor model.Account, id model.ConversationID, account model.AccountID) (model.Member, error) {
	st, err := s.adminState(ctx, actor, id, perm.OverrideAdd)
	if err != nil {
		return model.Member{}, err
	}
	target, err := s.store.Accounts().Get(ctx, account)
	if err != nil || target.Removed() {
		return model.Member{}, model.ErrNotFound.With("There is no such account.")
	}
	if _, found, err := s.store.Members().Get(ctx, id, account); err != nil {
		return model.Member{}, err
	} else if found {
		return model.Member{}, model.ErrAlreadyExists.With("That person is in this channel already.")
	}
	if err := s.overrideTarget(ctx, st.Conversation, actor, target); err != nil {
		return model.Member{}, err
	}
	level := st.Conversation.DefaultLevel
	if level == "" {
		level = model.LevelMember
	}
	var out model.Member
	err = s.Tx(ctx, func(tx store.Tx, rec *Recorder) error {
		rec.override = true
		res, err := tx.Members().AddByOverride(ctx, id, account, level, s.now())
		if err != nil {
			return err
		}
		if err := s.audit(ctx, tx, st.Conversation, actor.ID, "channel.owner_override", "account", string(account),
			map[string]any{"conversation_id": id}); err != nil {
			return err
		}
		if err := rec.Membership(ctx, res, actor.ID, ""); err != nil {
			return err
		}
		m, found, err := tx.Members().Get(ctx, id, account)
		if err == nil && !found {
			err = model.ErrNotFound
		}
		out = m
		return err
	})
	return out, hide(err)
}

// DismissManager is an Owner or an Admin taking a manager's level in a named
// channel away (ruling R104), without being in it or reading it: the
// person stays in the channel as a member. Every member is shown a
// manager_dismissed notice naming who did it, which nobody can turn off, and
// it is written to the audit log. An Admin does not dismiss an Owner or
// another Admin, and nobody dismisses an Owner but the Owner (section 9.1
// note 5). A manager who is one through a group keeps that level: the
// group's role there is changed instead. The channel keeps at least one
// manager: appoint another first (the store refuses the last).
func (s *Service) DismissManager(ctx context.Context, actor model.Account, id model.ConversationID, account model.AccountID) (model.Member, error) {
	st, err := s.adminState(ctx, actor, id, perm.ConsoleDismissManager)
	if err != nil {
		return model.Member{}, err
	}
	target, err := s.store.Accounts().Get(ctx, account)
	if err != nil {
		return model.Member{}, model.ErrNotFound.With("There is no such account.")
	}
	switch {
	case target.Role == model.RoleOwner && target.ID != actor.ID:
		return model.Member{}, model.ErrForbidden.With("Nobody can do that to an owner.")
	case target.Role == model.RoleAdmin && actor.Role != model.RoleOwner && target.ID != actor.ID:
		return model.Member{}, model.ErrForbidden.With("Only an owner can do that to an admin.")
	}
	row, found, err := s.store.Members().Get(ctx, id, account)
	if err != nil {
		return model.Member{}, err
	}
	if !found || row.Level != model.LevelManager {
		return model.Member{}, model.ErrNotFound.With("That person is not a manager of this channel.")
	}
	if row.DirectLevel != model.LevelManager {
		return model.Member{}, model.ErrConflict.With("That person is a manager through a group: change the group's role in this channel instead.")
	}
	c := st.Conversation
	var out model.Member
	err = s.Tx(ctx, func(tx store.Tx, rec *Recorder) error {
		res, err := tx.Members().SetDirectLevel(ctx, id, account, model.LevelMember)
		if err != nil {
			if errors.Is(err, model.ErrConflict) {
				return model.ErrConflict.With("That is the channel's last manager. Appoint another first.")
			}
			return err
		}
		if err := s.audit(ctx, tx, c, actor.ID, "channel.manager_dismissed", "account", string(account),
			map[string]any{"conversation_id": id, "by_role": actor.Role}); err != nil {
			return err
		}
		if err := rec.Membership(ctx, res, actor.ID, ""); err != nil {
			return err
		}
		if _, err := s.systemNotice(ctx, tx, rec, c, account, map[string]any{"kind": "manager_dismissed", "by_id": string(actor.ID)}); err != nil {
			return err
		}
		m, found, err := tx.Members().Get(ctx, id, account)
		if err == nil && !found {
			err = model.ErrNotFound
		}
		out = m
		return err
	})
	return out, hide(err)
}

// AuditLog reads the audit log, newest first.
func (s *Service) AuditLog(ctx context.Context, actor model.Account, f store.AuditFilter, p store.Page) ([]model.AuditEntry, string, error) {
	if err := s.perm.CanAdminister(ctx, actor, perm.ReadAuditLog); err != nil {
		return nil, "", err
	}
	return s.store.Audit().List(ctx, f, p)
}
