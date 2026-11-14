package chat

import (
	"context"
	"errors"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/perm"
	"github.com/tikaydaes/churr/internal/store"
)

// The admin view of channels (docs/PERMISSIONS.md section 4.1, note 1). An
// admin sees every named channel's name, members and managers, and may
// appoint a manager, archive or unarchive, lock and set retention there,
// without being able to read it: nothing in this file returns a topic of a
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
		MemberCount: c.MemberCount, ManagerIDs: managers, CreatedAt: c.CreatedAt, ArchivedAt: c.ArchivedAt, Shared: c.IsShared()}
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

// AppointManager makes a member of a named channel one of its managers. In
// a private channel the admin is not in, this is the only change to its
// members an admin can make: the account must already be in the channel.
func (s *Service) AppointManager(ctx context.Context, actor model.Account, id model.ConversationID, account model.AccountID) (model.Member, error) {
	st, err := s.adminState(ctx, actor, id, perm.AdminAppointManager)
	if err != nil {
		return model.Member{}, err
	}
	target, err := s.store.Accounts().Get(ctx, account)
	if err != nil {
		return model.Member{}, hide(err)
	}
	row, found, err := s.store.Members().Get(ctx, id, account)
	if err != nil {
		return model.Member{}, err
	}
	if !found {
		return model.Member{}, model.ErrNotFound.With("That account is not in this channel.")
	}
	if target.Role == model.RoleRemote {
		return row, model.ErrForbidden.With("Someone on another server cannot be a manager here.")
	}
	if st.Conversation.Archived() {
		return row, model.ErrArchived
	}
	if row.Level == model.LevelManager {
		return row, nil
	}
	var out model.Member
	err = s.Tx(ctx, func(tx store.Tx, rec *Recorder) error {
		res, err := tx.Members().SetDirectLevel(ctx, id, account, model.LevelManager)
		if err != nil {
			return err
		}
		if err := s.audit(ctx, tx, st.Conversation, actor.ID, "channel.manager_appointed", "account", string(account),
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

// AdminArchive archives or unarchives a named channel from the console.
func (s *Service) AdminArchive(ctx context.Context, actor model.Account, id model.ConversationID, archived bool) (model.AdminChannel, error) {
	action := perm.AdminUnarchive
	if archived {
		action = perm.AdminArchive
	}
	st, err := s.adminState(ctx, actor, id, action)
	if err != nil {
		return model.AdminChannel{}, err
	}
	var out model.AdminChannel
	err = s.Tx(ctx, func(tx store.Tx, rec *Recorder) error {
		c, err := s.setArchived(ctx, tx, rec, st.Conversation, actor.ID, archived)
		if err != nil {
			return err
		}
		out, err = s.adminChannel(ctx, tx, c)
		return err
	})
	return out, err
}

// AdminLock locks a named private channel for good, without the admin
// being a member of it (ruling R44). The admin still cannot read it or
// join it afterwards.
func (s *Service) AdminLock(ctx context.Context, actor model.Account, id model.ConversationID) (model.AdminChannel, error) {
	st, err := s.adminState(ctx, actor, id, perm.AdminLock)
	if err != nil {
		return model.AdminChannel{}, err
	}
	if st.Conversation.Archived() && !st.Conversation.Locked() {
		return model.AdminChannel{}, model.ErrArchived
	}
	var out model.AdminChannel
	err = s.Tx(ctx, func(tx store.Tx, rec *Recorder) error {
		c, err := s.lockTx(ctx, tx, rec, st.Conversation, actor.ID)
		if err != nil {
			return err
		}
		out, err = s.adminChannel(ctx, tx, c)
		return err
	})
	return out, err
}

// AuditLog reads the audit log, newest first.
func (s *Service) AuditLog(ctx context.Context, actor model.Account, f store.AuditFilter, p store.Page) ([]model.AuditEntry, string, error) {
	if err := s.perm.CanAdminister(ctx, actor, perm.ReadAuditLog); err != nil {
		return nil, "", err
	}
	return s.store.Audit().List(ctx, f, p)
}
