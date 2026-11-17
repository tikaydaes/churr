package chat

import (
	"context"
	"errors"
	"unicode/utf8"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/perm"
	"github.com/tikaydaes/churr/internal/store"
)

// Managing a conversation: its name, topic and settings, privacy and the
// lock, archiving, levels and removals, groups and pins. Every rule is
// perm's (docs/PERMISSIONS.md sections 6, 7 and 10); this file carries the
// changes out and says what happened.

// seen loads the actor's state in a conversation and answers not_found for
// one it may not know exists.
func (s *Service) seen(ctx context.Context, actor model.Account, id model.ConversationID) (perm.State, error) {
	st, err := s.perm.Load(ctx, actor, id)
	if err != nil {
		return st, hide(err)
	}
	return st, perm.Check(st, perm.See)
}

// conversationEvent emits conversation.updated (or the archived pair) with
// the conversation as nobody in particular sees it.
func (s *Service) conversationEvent(ctx context.Context, tx store.Tx, rec *Recorder, eventType string, c model.Conversation, by model.AccountID) error {
	shown, err := s.show(ctx, tx, model.Account{}, c, nil)
	if err != nil {
		return err
	}
	// Stored with its parent; each recipient's copy is made in
	// tailorConversationEvent.
	shown.ParentID = c.ParentID
	payload := map[string]any{"conversation": shown}
	if by != "" {
		payload["by_id"] = by
	}
	_, err = rec.Emit(ctx, model.Event{Type: eventType, ConversationID: c.ID, Payload: payload})
	return err
}

// ConversationChange is what UpdateConversation takes. Nil fields stay as
// they are.
type ConversationChange struct {
	Name    *string
	Topic   *string
	Welcome *string
	// Private may only be true: a public channel can be made private and
	// never the reverse (ruling R1).
	Private *bool
	// Access is model.AccessPublic, AccessKnock or AccessPrivate (ruling
	// R79): public and knock change into each other freely, anything may
	// become private, and a private channel never becomes either.
	Access       *string
	DefaultLevel *model.Level
	// SectionID "" files the channel under no section.
	SectionID *model.SectionID
	// RetentionSeconds 0 clears the channel's own period.
	RetentionSeconds *int64
	SlowModeSeconds  *int
	// BroadMentions is who may use @channel, @here and @everyone here:
	// model.BroadServer, BroadAnyone or BroadManagers (ruling R64).
	BroadMentions *string
	// LazyDelivery is whether the channel is delivered in full only to
	// the connections that have it open: model.LazyServer, LazyOn or
	// LazyOff. Announcement makes it an announcement channel or an
	// ordinary one again, and AnnouncementReplies says whether its other
	// members may reply in threads (ruling R66).
	LazyDelivery        *string
	Announcement        *bool
	AnnouncementReplies *bool
	// ParentID puts the channel under another, or with "" makes it
	// top-level again (ruling R49).
	ParentID *model.ConversationID
}

// UpdateConversation changes a channel's name, topic and settings. Each
// field is its own question to perm, and nothing is changed unless all of
// them are allowed.
func (s *Service) UpdateConversation(ctx context.Context, actor model.Account, id model.ConversationID, ch ConversationChange) (model.Conversation, error) {
	st, err := s.seen(ctx, actor, id)
	if err != nil {
		return model.Conversation{}, err
	}
	c := st.Conversation
	makePrivate := false
	if ch.Private != nil && *ch.Private {
		v := model.AccessPrivate
		ch.Access = &v
		ch.Private = nil
	}
	var knock *bool
	if ch.Access != nil && *ch.Access != c.AccessKind() {
		want := *ch.Access
		if want != model.AccessPublic && want != model.AccessKnock && want != model.AccessPrivate {
			return c, model.Invalid("access", "access is public, knock or private.")
		}
		if !c.Named() {
			return c, model.ErrForbidden.With("An ad-hoc channel's privacy cannot be changed. Name it first.")
		}
		if c.AccessKind() == model.AccessPrivate {
			// Refused for everyone, the owner included (section 7.1).
			return c, model.ErrForbidden.With("A private channel can never be made public or visible.")
		}
		if err := perm.Check(st, perm.MakePrivate); err != nil {
			return c, err
		}
		if c.IsShared() {
			// TODO(link): only the owner server may; then the change
			// travels to the others. Knock channels are this server's
			// alone for now (ruling R79).
			return c, model.ErrForbidden.With("A shared channel's privacy is changed through the link, which is not built yet.")
		}
		if want == model.AccessKnock && c.ParentID != "" {
			return c, model.Invalid("access", "A sub-channel cannot take requests to join; make the top-level channel a knock channel.")
		}
		k := want == model.AccessKnock
		knock = &k
		// From public, knock and private are both private underneath:
		// the people outside lose the channel's content at once.
		makePrivate = c.Public()
		if want == model.AccessPublic {
			f := false
			ch.Private = &f
		}
	}
	if ch.Private != nil && !*ch.Private && knock == nil && !c.Public() {
		// Refused for everyone, the owner included; so is any change to an
		// ad-hoc channel's privacy (section 7.1).
		return c, model.ErrForbidden.With("A private channel can never be made public.")
	}
	for _, q := range []struct {
		asked  bool
		action perm.Action
	}{
		{ch.Name != nil, perm.Rename},
		{ch.Topic != nil, perm.SetTopic},
		{ch.Welcome != nil, perm.SetWelcome},
		{ch.DefaultLevel != nil, perm.SetDefaultLevel},
		{ch.SectionID != nil, perm.SetSection},
		{ch.SlowModeSeconds != nil, perm.SetSlowMode},
		{ch.BroadMentions != nil, perm.SetBroadMentions},
		{ch.LazyDelivery != nil, perm.SetLazyDelivery},
		{ch.Announcement != nil || ch.AnnouncementReplies != nil, perm.SetAnnouncement},
	} {
		if q.asked {
			if err := perm.Check(st, q.action); err != nil {
				return c, err
			}
		}
	}
	if ch.Name != nil {
		name, ok := model.CleanChannelName(*ch.Name)
		if !ok {
			return c, model.Invalid("name", "A channel name is 1 to 64 characters, with no control characters.")
		}
		ch.Name = &name
	}
	if ch.Topic != nil && utf8.RuneCountInString(*ch.Topic) > model.MaxTopic {
		return c, model.Invalid("topic", "A topic is at most 250 characters.")
	}
	if ch.Welcome != nil && utf8.RuneCountInString(*ch.Welcome) > model.MaxMessageText {
		return c, model.Invalid("welcome", "A welcome text is at most 16,000 characters.")
	}
	if ch.DefaultLevel != nil && *ch.DefaultLevel != model.LevelMember && *ch.DefaultLevel != model.LevelReader {
		return c, model.Invalid("default_level", "The default level is reader or member.")
	}
	if ch.SectionID != nil && *ch.SectionID != "" {
		if _, err := s.store.Sections().Get(ctx, *ch.SectionID); err != nil {
			if errors.Is(err, model.ErrNotFound) {
				return c, model.Invalid("section_id", "There is no such section.")
			}
			return c, err
		}
	}
	if ch.SlowModeSeconds != nil {
		if v := *ch.SlowModeSeconds; v != 0 && (v < model.MinSlowModeSeconds || v > model.MaxSlowModeSeconds) {
			return c, model.Invalid("slow_mode_seconds", "Slow mode is 0 for off, or from 5 seconds to 6 hours.")
		}
	}
	if ch.BroadMentions != nil && !model.ValidBroadMentions(*ch.BroadMentions) {
		return c, model.Invalid("broad_mentions", "broad_mentions is server, anyone or managers.")
	}
	if ch.LazyDelivery != nil && !model.ValidLazyDelivery(*ch.LazyDelivery) {
		return c, model.Invalid("lazy_delivery", "lazy_delivery is server, on or off.")
	}
	if ch.ParentID != nil {
		if err := s.setParent(ctx, st, *ch.ParentID, false); err != nil {
			return c, err
		}
	}
	if ch.RetentionSeconds != nil {
		if *ch.RetentionSeconds < 0 {
			return c, model.Invalid("retention_seconds", "A retention period is 0 or more seconds.")
		}
		if err := perm.CheckRetention(st, *ch.RetentionSeconds); err != nil {
			return c, err
		}
	}

	var out model.Conversation
	err = s.Tx(ctx, func(tx store.Tx, rec *Recorder) error {
		if makePrivate {
			if err := s.makePrivateTx(ctx, tx, rec, c, actor.ID, *knock); err != nil {
				return err
			}
		}
		var retentionWas *int64
		updated, err := tx.Conversations().Update(ctx, id, func(cur *model.Conversation) error {
			if ch.Name != nil {
				if cur.Adhoc() {
					// Naming an ad-hoc channel makes it a named private
					// channel; its member list stops being its identity.
					cur.MemberKey = ""
				}
				cur.Name = *ch.Name
			}
			if ch.Topic != nil {
				cur.Topic = *ch.Topic
			}
			if ch.Welcome != nil {
				cur.Welcome = *ch.Welcome
			}
			if ch.DefaultLevel != nil {
				cur.DefaultLevel = *ch.DefaultLevel
			}
			if ch.SectionID != nil {
				cur.SectionID = *ch.SectionID
			}
			if ch.SlowModeSeconds != nil {
				cur.SlowModeSeconds = *ch.SlowModeSeconds
			}
			if ch.BroadMentions != nil {
				cur.BroadMentions = *ch.BroadMentions
			}
			if ch.LazyDelivery != nil {
				cur.LazyDelivery = *ch.LazyDelivery
			}
			if ch.Announcement != nil {
				cur.Announcement = *ch.Announcement
			}
			if ch.AnnouncementReplies != nil {
				cur.AnnouncementReplies = *ch.AnnouncementReplies
			}
			if !cur.Announcement {
				// The switch means something only in an announcement
				// channel, and does not wait there for the next one.
				cur.AnnouncementReplies = false
			}
			if ch.ParentID != nil {
				cur.ParentID = *ch.ParentID
			}
			if knock != nil {
				cur.Knock = *knock
				if ch.Private != nil && !*ch.Private {
					cur.Private = false
				}
			}
			if cur.Knock && cur.ParentID != "" {
				return model.Invalid("parent_id", "A knock channel cannot be a sub-channel.")
			}
			if ch.RetentionSeconds != nil {
				retentionWas = cur.RetentionSeconds
				cur.RetentionSeconds = ch.RetentionSeconds
				if *ch.RetentionSeconds == 0 {
					cur.RetentionSeconds = nil
				}
			}
			return nil
		})
		if err != nil {
			return err
		}
		if c.Adhoc() && updated.Named() {
			// Named, the chat is a private channel, which has a manager:
			// its longest-standing person, as when a channel's last
			// manager goes.
			res, err := tx.Members().Settle(ctx, id)
			if err != nil {
				return err
			}
			if err := rec.Membership(ctx, res, actor.ID, ""); err != nil {
				return err
			}
		}
		if ch.RetentionSeconds != nil {
			// Changing a period is in the audit log; a retention run is not.
			if err := s.audit(ctx, tx, updated, actor.ID, "channel.retention_changed", "conversation", string(id),
				map[string]any{"from": retentionWas, "to": updated.RetentionSeconds}); err != nil {
				return err
			}
		}
		out = updated
		return s.conversationEvent(ctx, tx, rec, model.EventConversationUpdated, updated, actor.ID)
	})
	if err != nil {
		return c, err
	}
	return s.afterManage(ctx, actor, out)
}

// afterManage returns a conversation to whoever just changed it: as they
// see it if they still can, and otherwise (an admin who made a channel
// private from outside, or acted from the admin console) bare.
func (s *Service) afterManage(ctx context.Context, actor model.Account, c model.Conversation) (model.Conversation, error) {
	shown, err := s.Conversation(ctx, actor, c.ID)
	if errors.Is(err, model.ErrNotFound) {
		return s.show(ctx, s.store, actor, c, nil)
	}
	return shown, err
}

// makePrivateTx makes a public channel private (docs/FEATURES.md section
// 21.5): mentions stored for people who are not members go, and every
// waiting purge request in the channel is carried out as an erasure, since
// in a private channel the author's request would have been carried out at
// once.
func (s *Service) makePrivateTx(ctx context.Context, tx store.Tx, rec *Recorder, c model.Conversation, by model.AccountID, knock bool) error {
	if _, err := tx.Conversations().MakePrivate(ctx, c.ID, knock); err != nil {
		return err
	}
	waiting, err := tx.Moderation().WaitingPurgeRequestsIn(ctx, c.ID)
	if err != nil {
		return err
	}
	c.Private = true
	for _, p := range waiting {
		// Decided by nobody: the server did it.
		if _, err := tx.Moderation().DecidePurgeRequest(ctx, p.ID, model.PurgeErased, "", s.now()); err != nil {
			return err
		}
		if _, err := s.purgeTx(ctx, tx, rec, c, p.MessageID); err != nil {
			return err
		}
	}
	return s.audit(ctx, tx, c, by, "channel.made_private", "conversation", string(c.ID), map[string]any{"purge_requests_erased": len(waiting)})
}

// setArchived archives or unarchives inside a transaction and emits the
// event.
func (s *Service) setArchived(ctx context.Context, tx store.Tx, rec *Recorder, c model.Conversation, by model.AccountID, archived bool) (model.Conversation, error) {
	if c.Archived() == archived {
		return c, nil
	}
	now := s.now()
	updated, err := tx.Conversations().Update(ctx, c.ID, func(cur *model.Conversation) error {
		cur.ArchivedAt = 0
		if archived {
			cur.ArchivedAt = now
		}
		return nil
	})
	if err != nil {
		return c, err
	}
	action, event := "channel.unarchived", model.EventConversationUnarchived
	if archived {
		action, event = "channel.archived", model.EventConversationArchived
	}
	if err := s.audit(ctx, tx, updated, by, action, "conversation", string(c.ID), nil); err != nil {
		return c, err
	}
	return updated, s.conversationEvent(ctx, tx, rec, event, updated, by)
}

// Archive freezes a channel: it can be read and searched and accepts no
// posts. Unarchive brings it back.
func (s *Service) Archive(ctx context.Context, actor model.Account, id model.ConversationID, archived bool) (model.Conversation, error) {
	st, err := s.seen(ctx, actor, id)
	if err != nil {
		return model.Conversation{}, err
	}
	action := perm.Unarchive
	if archived {
		action = perm.Archive
	}
	if err := perm.Check(st, action); err != nil {
		return st.Conversation, err
	}
	var out model.Conversation
	err = s.Tx(ctx, func(tx store.Tx, rec *Recorder) error {
		out, err = s.setArchived(ctx, tx, rec, st.Conversation, actor.ID, archived)
		return err
	})
	if err != nil {
		return st.Conversation, err
	}
	return s.afterManage(ctx, actor, out)
}

// lockTx locks a private channel for good (docs/FEATURES.md section 21.6):
// the people its groups had brought in stay as direct members, the groups
// are detached, and from then on nobody can be added or join.
func (s *Service) lockTx(ctx context.Context, tx store.Tx, rec *Recorder, c model.Conversation, by model.AccountID) (model.Conversation, error) {
	if c.Locked() {
		return c, nil
	}
	locked, err := tx.Conversations().Lock(ctx, c.ID, by, s.now())
	if err != nil {
		return c, err
	}
	if err := s.audit(ctx, tx, locked, by, "channel.locked", "conversation", string(c.ID), nil); err != nil {
		return c, err
	}
	return locked, s.conversationEvent(ctx, tx, rec, model.EventConversationUpdated, locked, by)
}

// Lock locks a private channel, named or ad-hoc, from inside it. One-way.
func (s *Service) Lock(ctx context.Context, actor model.Account, id model.ConversationID) (model.Conversation, error) {
	st, err := s.seen(ctx, actor, id)
	if err != nil {
		return model.Conversation{}, err
	}
	if err := perm.Check(st, perm.Lock); err != nil {
		return st.Conversation, err
	}
	if st.Conversation.Archived() && !st.Conversation.Locked() {
		return st.Conversation, model.ErrArchived
	}
	var out model.Conversation
	err = s.Tx(ctx, func(tx store.Tx, rec *Recorder) error {
		out, err = s.lockTx(ctx, tx, rec, st.Conversation, actor.ID)
		return err
	})
	if err != nil {
		return st.Conversation, err
	}
	return s.afterManage(ctx, actor, out)
}

// SetMemberLevel sets the level a member holds directly. The level in
// force stays the highest of that and the levels of their groups.
func (s *Service) SetMemberLevel(ctx context.Context, actor model.Account, id model.ConversationID, account model.AccountID, level model.Level) (model.Member, error) {
	st, err := s.seen(ctx, actor, id)
	if err != nil {
		return model.Member{}, err
	}
	if !level.Valid() {
		return model.Member{}, model.Invalid("level", "A level is reader, member, moderator or manager.")
	}
	if err := perm.Check(st, perm.SetLevel); err != nil {
		return model.Member{}, err
	}
	target, err := s.store.Accounts().Get(ctx, account)
	if err != nil {
		return model.Member{}, hide(err)
	}
	if err := perm.CheckSetLevel(st, target, level); err != nil {
		return model.Member{}, err
	}
	if st.HubOwned {
		// Someone on the hub's manager or moderator list is one in the
		// copy for as long as the list names them (section 14.1).
		if rec, err := s.store.Streams().Channel(ctx, st.Conversation.StreamID); err == nil {
			for _, m := range append(rec.Managers, rec.Moderators...) {
				if m == account {
					return model.Member{}, model.ErrConflict.With("A manager or moderator on the hub's lists is one in this copy: take them off the channel's lists first.")
				}
			}
		}
	}
	var out model.Member
	err = s.Tx(ctx, func(tx store.Tx, rec *Recorder) error {
		res, err := tx.Members().SetDirectLevel(ctx, id, account, level)
		if err != nil {
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

// SetMayPost says whether a member may post in a channel while it is an
// announcement channel (ruling R66). The member's own clients are told.
func (s *Service) SetMayPost(ctx context.Context, actor model.Account, id model.ConversationID, account model.AccountID, may bool) (model.Member, error) {
	st, err := s.seen(ctx, actor, id)
	if err != nil {
		return model.Member{}, err
	}
	if err := perm.Check(st, perm.SetMayPost); err != nil {
		return model.Member{}, err
	}
	m, err := s.store.Members().SetMayPost(ctx, id, account, may)
	if err != nil {
		return model.Member{}, hide(err)
	}
	if acc, err := s.store.Accounts().Get(ctx, account); err == nil {
		if ms, err := s.ownMembership(ctx, acc, m); err == nil {
			s.hub.Send(account, model.Event{Type: model.EventMembershipUpdated, At: s.now(), ConversationID: id,
				Payload: map[string]any{"membership": ms}})
		}
	}
	return m, nil
}

// RemoveMember takes away an account's direct membership of a channel.
func (s *Service) RemoveMember(ctx context.Context, actor model.Account, id model.ConversationID, account model.AccountID) error {
	st, err := s.seen(ctx, actor, id)
	if err != nil {
		return err
	}
	row, found, err := s.store.Members().Get(ctx, id, account)
	if err != nil {
		return err
	}
	if !found && st.IsMember() {
		// Someone invited who has not joined: the invitation is taken
		// back (ruling R103), by whoever sent it, or by those who may
		// remove people here.
		r, invited, err := s.store.ChatRequests().Invitation(ctx, id, account)
		if err != nil {
			return err
		}
		if invited && (r.RequesterID == actor.ID || perm.Check(st, perm.RemoveMember) == nil) {
			return s.withdrawInvitation(ctx, actor, st, account)
		}
	}
	if err := perm.Check(st, perm.RemoveMember); err != nil {
		return err
	}
	target, err := s.store.Accounts().Get(ctx, account)
	if err != nil {
		return hide(err)
	}
	if !found {
		return model.ErrNotFound.With("That account is not in this channel.")
	}
	if err := perm.CheckRemoveMember(st, target, row); err != nil {
		return err
	}
	return hide(s.Tx(ctx, func(tx store.Tx, rec *Recorder) error {
		res, err := tx.Members().RemoveDirect(ctx, id, account)
		if err != nil {
			return err
		}
		if err := s.audit(ctx, tx, st.Conversation, actor.ID, "member.removed", "account", string(account),
			map[string]any{"conversation_id": id}); err != nil {
			return err
		}
		return rec.Membership(ctx, res, actor.ID, "removed")
	}))
}

// ChannelGroups lists the groups added to a channel.
func (s *Service) ChannelGroups(ctx context.Context, actor model.Account, id model.ConversationID) ([]model.ChannelGroup, error) {
	if _, err := s.perm.Can(ctx, actor, perm.ListMembers, id); err != nil {
		return nil, hide(err)
	}
	return s.store.Members().Groups(ctx, id)
}

// groupMembership turns what adding, changing or removing a group did into
// events. The channel_group event stands in for one event per person: the
// people brought in get it with the conversation beside it, as a joiner
// does, and the people taken out get their own member.left with reason
// `group`, which the rest of the channel is not sent.
func (s *Service) groupMembership(ctx context.Context, tx store.Tx, rec *Recorder, e model.Event, res store.MembershipResult) error {
	for _, d := range res.Deleted {
		// The group's people were the channel's last members.
		rec.RemoveBlobs(d.BlobKeys...)
		if d.ID == e.ConversationID {
			return nil
		}
	}
	conv, err := tx.Conversations().Get(ctx, e.ConversationID)
	if err != nil {
		return err
	}
	now := s.now()
	var arrived []joined
	for _, ch := range res.Changes {
		m := ch.Member
		if rec.offerChange(ctx, ch) {
			continue
		}
		switch ch.Kind {
		case store.MemberJoined:
			if err := rec.joinedAnswers(ctx, m); err != nil {
				return err
			}
			arrived = append(arrived, joined{conv, m.AccountID})
			if e.Only == nil {
				e.Only = map[model.AccountID]map[string]any{}
			}
			e.Only[m.AccountID] = map[string]any{"conversation": s.ShownToArrival(ctx, tx, conv, m)}
		case store.MemberLeft:
			left := model.Event{Type: model.EventMemberLeft, At: now, ConversationID: e.ConversationID,
				Payload: map[string]any{"account_id": m.AccountID, "reason": "group"}}
			account := m.AccountID
			rec.After(func() { s.hub.Send(account, left) })
		}
	}
	if _, err = rec.Emit(ctx, e); err != nil {
		return err
	}
	by, _ := e.Payload["by_id"].(model.AccountID)
	return rec.announce(ctx, arrived, by)
}

// AddChannelGroup adds a group to a named channel at a level, or changes
// the level of one already there. Everyone in the group is then in the
// channel at that level and stays in step as the group changes.
func (s *Service) AddChannelGroup(ctx context.Context, actor model.Account, id model.ConversationID, group model.GroupID, level model.Level) (model.ChannelGroup, error) {
	st, err := s.seen(ctx, actor, id)
	if err != nil {
		return model.ChannelGroup{}, err
	}
	if !level.Valid() {
		return model.ChannelGroup{}, model.Invalid("level", "A level is reader, member, moderator or manager.")
	}
	if err := perm.Check(st, perm.AddGroup); err != nil {
		return model.ChannelGroup{}, err
	}
	if _, err := s.store.Groups().Get(ctx, group, ""); err != nil {
		return model.ChannelGroup{}, hide(err)
	}
	cg := model.ChannelGroup{ConversationID: id, GroupID: group, Level: level, AddedByID: actor.ID, AddedAt: s.now()}
	err = s.Tx(ctx, func(tx store.Tx, rec *Recorder) error {
		res, added, err := tx.Members().AddGroup(ctx, cg)
		if err != nil {
			return err
		}
		event := model.EventChannelGroupChanged
		if added {
			event = model.EventChannelGroupAdded
		} else {
			// The row keeps who first added it and when.
			groups, err := tx.Members().Groups(ctx, id)
			if err != nil {
				return err
			}
			for _, g := range groups {
				if g.GroupID == group {
					cg = g
				}
			}
		}
		if err := s.audit(ctx, tx, st.Conversation, actor.ID, event, "conversation", string(id),
			map[string]any{"group_id": group, "level": level}); err != nil {
			return err
		}
		return s.groupMembership(ctx, tx, rec, model.Event{Type: event, ConversationID: id, Payload: map[string]any{"group": cg, "by_id": actor.ID}}, res)
	})
	if errors.Is(err, model.ErrConflict) {
		return cg, model.ErrForbidden.With("Groups are added to named channels only.")
	}
	return cg, err
}

// RemoveChannelGroup takes a group out of a channel. The people it alone
// had brought in leave with it.
func (s *Service) RemoveChannelGroup(ctx context.Context, actor model.Account, id model.ConversationID, group model.GroupID) error {
	st, err := s.seen(ctx, actor, id)
	if err != nil {
		return err
	}
	if err := perm.Check(st, perm.RemoveGroup); err != nil {
		return err
	}
	return hide(s.Tx(ctx, func(tx store.Tx, rec *Recorder) error {
		res, err := tx.Members().RemoveGroup(ctx, id, group)
		if err != nil {
			return err
		}
		if err := s.audit(ctx, tx, st.Conversation, actor.ID, model.EventChannelGroupRemoved, "conversation", string(id),
			map[string]any{"group_id": group}); err != nil {
			return err
		}
		return s.groupMembership(ctx, tx, rec, model.Event{Type: model.EventChannelGroupRemoved, ConversationID: id,
			Payload: map[string]any{"group_id": group, "by_id": actor.ID}}, res)
	}))
}

// Pins lists a conversation's pinned messages, newest pin first, each with
// its message as the reader sees it.
func (s *Service) Pins(ctx context.Context, actor model.Account, id model.ConversationID) ([]model.Pin, error) {
	st, err := s.perm.Can(ctx, actor, perm.Read, id)
	if err != nil {
		return nil, hide(err)
	}
	pins, err := s.store.Pins().List(ctx, id)
	if err != nil || len(pins) == 0 {
		return pins, err
	}
	ids := make([]model.MessageID, len(pins))
	for i, p := range pins {
		ids[i] = p.MessageID
	}
	msgs, err := s.store.Messages().GetMany(ctx, ids)
	if err != nil {
		return nil, err
	}
	// Pins show their messages to everyone, those from before the
	// reader's first join in a private channel too, marked as outside
	// their history (ruling R87).
	if err := s.present(ctx, s.store, st, msgs); err != nil {
		return nil, err
	}
	pinnedAlone(st, msgs)
	byID := map[model.MessageID]*model.Message{}
	for i := range msgs {
		byID[msgs[i].ID] = &msgs[i]
	}
	for i := range pins {
		if pins[i].Message = byID[pins[i].MessageID]; pins[i].Message != nil {
			pins[i].OutsideHistory = !st.Sees(*pins[i].Message)
		}
	}
	return pins, nil
}

// pinTarget loads the conversation and a message that must be in it.
func (s *Service) pinTarget(ctx context.Context, actor model.Account, id model.ConversationID, msg model.MessageID) (perm.State, model.Message, error) {
	st, err := s.seen(ctx, actor, id)
	if err != nil {
		return st, model.Message{}, err
	}
	if err := perm.Check(st, perm.Pin); err != nil {
		return st, model.Message{}, err
	}
	m, err := s.store.Messages().Get(ctx, msg)
	if err != nil || m.ConversationID != id || !st.Sees(m) {
		return st, m, model.ErrNotFound
	}
	return st, m, nil
}

// Pin marks a message for everyone in a channel, and posts the `pinned`
// notice. Pinning a message that is already pinned changes nothing.
func (s *Service) Pin(ctx context.Context, actor model.Account, id model.ConversationID, msg model.MessageID) error {
	st, m, err := s.pinTarget(ctx, actor, id, msg)
	if err != nil {
		return err
	}
	if m.Deleted() {
		return model.ErrConflict.With("A deleted message cannot be pinned.")
	}
	if m.IsSystem() {
		return model.ErrConflict.With("A system notice cannot be pinned.")
	}
	already, err := s.store.Pins().List(ctx, id)
	if err != nil {
		return err
	}
	for _, p := range already {
		if p.MessageID == msg {
			return nil
		}
	}
	// A pin shows its message to newcomers to a private channel who could
	// not otherwise read it (ruling R87). Pinning one older than someone's
	// first join is that, so it is written to the audit log.
	older := false
	if c := st.Conversation; c.Named() && c.Private && !c.IsKnock() {
		r, err := s.store.Members().Roster(ctx, id)
		if err != nil {
			return err
		}
		older = r != nil && m.CreatedAt.Before(r.LatestFloor)
	}
	err = s.Tx(ctx, func(tx store.Tx, rec *Recorder) error {
		pin, err := tx.Pins().Add(ctx, id, msg, actor.ID, s.now())
		if err != nil {
			return err
		}
		if older {
			if err := s.audit(ctx, tx, st.Conversation, actor.ID, "message.pinned_for_newcomers", "message", string(msg),
				map[string]any{"conversation_id": id, "posted_at": m.CreatedAt}); err != nil {
				return err
			}
		}
		if _, err := rec.Emit(ctx, model.Event{Type: model.EventPinAdded, ConversationID: id, MessageID: msg,
			Payload: map[string]any{"pin": pin}}); err != nil {
			return err
		}
		_, err = s.systemNotice(ctx, tx, rec, st.Conversation, actor.ID, map[string]any{"kind": "pinned", "message_id": string(msg)})
		return err
	})
	if errors.Is(err, model.ErrAlreadyExists) {
		return nil // someone pinned it a moment ago
	}
	return err
}

// Unpin takes a pin off. It posts nothing.
func (s *Service) Unpin(ctx context.Context, actor model.Account, id model.ConversationID, msg model.MessageID) error {
	if _, _, err := s.pinTarget(ctx, actor, id, msg); err != nil {
		return err
	}
	return hide(s.Tx(ctx, func(tx store.Tx, rec *Recorder) error {
		if err := tx.Pins().Remove(ctx, id, msg); err != nil {
			return err
		}
		_, err := rec.Emit(ctx, model.Event{Type: model.EventPinRemoved, ConversationID: id,
			Payload: map[string]any{"message_id": msg, "by_id": actor.ID}})
		return err
	}))
}
