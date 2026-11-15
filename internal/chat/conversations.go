package chat

import (
	"context"
	"encoding/json"
	"errors"
	"unicode/utf8"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/perm"
	"github.com/tikaydaes/churr/internal/store"
)

// flatView reports whether an account reads a conversation in the flat
// view: the conversation's own choice, or else the account preference.
func flatView(actor model.Account, m model.Member) bool {
	view := m.ThreadView
	if view == "" {
		var prefs struct {
			ThreadView string `json:"thread_view"`
		}
		json.Unmarshal(actor.PrefsJSON, &prefs)
		view = prefs.ThreadView
	}
	return view == "flat"
}

// show puts a conversation in the shape one account sees: its own
// membership with counts, and the member list of an ad-hoc channel or
// sealed chat so a client can title it.
func (s *Service) show(ctx context.Context, a store.Areas, actor model.Account, c model.Conversation, m *model.Member) (model.Conversation, error) {
	c.Membership = nil
	// The settings the rules read are read once for everything below.
	if _, have := ctx.Value(settingsKey{}).(perm.Settings); !have {
		if set, err := s.perm.With(a).Settings(ctx); err == nil {
			ctx = context.WithValue(ctx, settingsKey{}, set)
		}
	}
	c.Large = s.large(ctx, c)
	// A reader who cannot see the parent is not told there is one.
	c = s.parentFor(ctx, a, actor, c)
	// The call in progress, if there is one (calls.go).
	c = s.withCall(ctx, c)
	// Whether a sealed chat still accepts anything (sealed.go).
	c = s.withSealed(ctx, actor, c)
	// What the stream says, if the conversation is shared (shared.go). It
	// comes second because it can only add to "read-only": a sealed chat
	// with someone on another server is also read-only while the two share
	// no shared channel.
	c = s.withShared(ctx, actor, c)
	if c.Kind == model.KindSealed || c.Adhoc() {
		ids, err := a.Members().IDs(ctx, c.ID)
		if err != nil {
			return c, err
		}
		c.MemberIDs = ids
	}
	if m != nil {
		u, err := a.Members().Unread(ctx, c.ID, actor.ID)
		if err != nil {
			return c, err
		}
		c.Membership = membership(*m, u, s.now(), flatView(actor, *m))
		c.BroadMentionsAllowed = s.broadAllowed(ctx, a, actor, c, *m)
	}
	return c, nil
}

// Conversation returns one conversation the actor can read.
func (s *Service) Conversation(ctx context.Context, actor model.Account, id model.ConversationID) (model.Conversation, error) {
	st, err := s.perm.Can(ctx, actor, perm.Read, id)
	if err != nil {
		return model.Conversation{}, hide(err)
	}
	return s.show(ctx, s.store, actor, st.Conversation, st.Member)
}

// hide turns "no such row" from the store into the one answer an outsider
// gets, so a missing conversation and a hidden one look the same.
func hide(err error) error {
	if errors.Is(err, model.ErrNotFound) {
		return model.ErrNotFound
	}
	return err
}

// Conversations lists the conversations the actor is in, with its
// membership of each.
func (s *Service) Conversations(ctx context.Context, actor model.Account, f store.ConversationFilter, p store.Page) ([]model.Conversation, string, error) {
	list, next, err := s.store.Conversations().ForAccount(ctx, actor.ID, f, p)
	if err != nil {
		return nil, "", err
	}
	ctx = s.withSettings(ctx)
	out := make([]model.Conversation, 0, len(list))
	for _, cm := range list {
		m := cm.Member
		// TODO(notify): the counts are worked out per conversation here;
		// notify.Totals will keep them in memory.
		c, err := s.show(ctx, s.store, actor, cm.Conversation, &m)
		if err != nil {
			return nil, "", err
		}
		out = append(out, c)
	}
	return out, next, nil
}

// Browse lists named channels: for owner, admin and member every public one
// and the private ones they are in; for guests and bots only the ones they
// are in (the contract's rule for browseChannels).
func (s *Service) Browse(ctx context.Context, actor model.Account, f store.BrowseFilter, p store.Page) ([]model.Conversation, string, error) {
	f.AccountID = actor.ID
	f.Public = actor.Role.IsFullMember()
	if f.ParentID != "" && !s.seesParent(ctx, s.store, actor, f.ParentID) {
		// The sub-channels of a channel the caller cannot see: none, the
		// same as for a channel that does not exist.
		return nil, "", nil
	}
	list, next, err := s.store.Conversations().Browse(ctx, f, p)
	if err != nil {
		return nil, "", err
	}
	ctx = s.withSettings(ctx)
	for i, c := range list {
		m, found, err := s.store.Members().Get(ctx, c.ID, actor.ID)
		if err != nil {
			return nil, "", err
		}
		var mp *model.Member
		if found {
			mp = &m
		}
		if list[i], err = s.show(ctx, s.store, actor, c, mp); err != nil {
			return nil, "", err
		}
	}
	return list, next, nil
}

// NewChannel is what CreateChannel takes.
// large says whether a conversation is without typing notices and the
// presence of single members (ruling R66): an announcement channel, or one
// with more members than the server's large_feature_limit.
func (s *Service) large(ctx context.Context, c model.Conversation) bool {
	if c.Announcement {
		return true
	}
	set, ok := ctx.Value(settingsKey{}).(perm.Settings)
	if !ok {
		set = perm.DefaultSettings()
	}
	return set.LargeFeatureLimit > 0 && c.MemberCount > set.LargeFeatureLimit
}

type NewChannel struct {
	Name         string
	Private      bool
	Topic        string
	Welcome      string
	DefaultLevel model.Level
	SectionID    model.SectionID
	AccountIDs   []model.AccountID
	// ParentID makes the new channel a sub-channel of this one (ruling
	// R49). Its creator must be a member of that channel, or an admin.
	ParentID model.ConversationID
	// Announcement makes it an announcement channel, where only its
	// managers and those they allow post; AnnouncementReplies lets the
	// other members reply in threads (ruling R66).
	Announcement        bool
	AnnouncementReplies bool
}

// CreateChannel makes a named channel with its creator as first manager.
func (s *Service) CreateChannel(ctx context.Context, actor model.Account, in NewChannel) (model.Conversation, error) {
	set, err := s.perm.Settings(ctx)
	if err != nil {
		return model.Conversation{}, err
	}
	if err := perm.CheckServer(actor, perm.CreateChannel, set); err != nil {
		return model.Conversation{}, err
	}
	var ok bool
	if in.Name, ok = model.CleanChannelName(in.Name); !ok {
		return model.Conversation{}, model.Invalid("name", "A channel name is 1 to 64 characters, with no control characters.")
	}
	if utf8.RuneCountInString(in.Topic) > model.MaxTopic {
		return model.Conversation{}, model.Invalid("topic", "A topic is at most 250 characters.")
	}
	if in.DefaultLevel == "" {
		in.DefaultLevel = model.LevelMember
	}
	if in.DefaultLevel != model.LevelMember && in.DefaultLevel != model.LevelReader {
		return model.Conversation{}, model.Invalid("default_level", "The default level is reader or member.")
	}
	if in.SectionID != "" {
		if _, err := s.store.Sections().Get(ctx, in.SectionID); err != nil {
			return model.Conversation{}, err
		}
	}
	if in.ParentID != "" {
		parent, err := s.perm.Load(ctx, actor, in.ParentID)
		if err != nil {
			return model.Conversation{}, hide(err)
		}
		if err := perm.CheckCreateUnder(parent); err != nil {
			return model.Conversation{}, err
		}
	}
	c := model.Conversation{Kind: model.KindChannel, Name: in.Name, Private: in.Private, Topic: in.Topic, Welcome: in.Welcome,
		DefaultLevel: in.DefaultLevel, SectionID: in.SectionID, ParentID: in.ParentID, CreatedByID: actor.ID, CreatedAt: s.now(),
		Announcement: in.Announcement, AnnouncementReplies: in.Announcement && in.AnnouncementReplies}
	err = s.Tx(ctx, func(tx store.Tx, rec *Recorder) error {
		if err := tx.Conversations().Create(ctx, &c); err != nil {
			return err
		}
		res, err := tx.Members().AddDirect(ctx, c.ID, actor.ID, model.LevelManager, c.CreatedAt)
		if err != nil {
			return err
		}
		if err := rec.Membership(ctx, res, "", ""); err != nil {
			return err
		}
		return s.addTx(ctx, tx, rec, actor, c.ID, in.AccountIDs, "")
	})
	if err != nil {
		return model.Conversation{}, err
	}
	return s.Conversation(ctx, actor, c.ID)
}

// addTx adds accounts to a conversation inside a transaction, asking perm
// about each. An empty level means the conversation's default.
func (s *Service) addTx(ctx context.Context, tx store.Tx, rec *Recorder, actor model.Account, conv model.ConversationID, ids []model.AccountID, level model.Level) error {
	if len(ids) == 0 {
		return nil
	}
	p := s.perm.With(tx)
	st, err := p.Load(ctx, actor, conv)
	if err != nil {
		return err
	}
	if level == "" {
		level = st.Conversation.DefaultLevel
		if st.Conversation.Adhoc() {
			level = model.LevelMember
		}
	} else if !st.ActsAsManager() {
		return model.ErrForbidden.With("Only the channel's managers choose the level someone is added at.")
	}
	if !level.Valid() {
		return model.Invalid("level", "A level is reader, member or manager.")
	}
	for _, id := range ids {
		if id == actor.ID {
			continue
		}
		target, err := tx.Accounts().Get(ctx, id)
		if err != nil {
			return err
		}
		if err := p.CanAdd(ctx, st, target); err != nil {
			return err
		}
		if !target.Local() || (st.Conversation.IsShared() && st.Conversation.Adhoc() && s.outbound != nil) {
			// In an ad-hoc channel that runs between servers everyone
			// added is invited first, so that every server has seen the
			// invitation before the join; the account's own server then
			// joins it (shared.go, Outbound.Invite). Someone on another
			// server can be added in no other kind of conversation.
			if s.outbound == nil || !st.Conversation.IsShared() {
				return model.ErrForbidden.With("That person is on another server, and this conversation is not shared with it.")
			}
			if err := s.outbound.Invite(ctx, tx, rec, st.Conversation, actor, target); err != nil {
				return err
			}
			continue
		}
		res, err := tx.Members().AddDirect(ctx, conv, id, level, s.now())
		if errors.Is(err, model.ErrAlreadyExists) {
			continue
		}
		if err != nil {
			return err
		}
		if err := rec.Membership(ctx, res, actor.ID, ""); err != nil {
			return err
		}
	}
	if st.Conversation.Adhoc() {
		n, err := tx.Members().Count(ctx, conv)
		if err != nil {
			return err
		}
		if n > model.MaxAdhocMembers {
			return model.ErrTooLarge.With("An ad-hoc channel holds at most 50 people. Give it a name first.")
		}
	}
	return nil
}

// FindOrCreateChat returns the ad-hoc channel with exactly the actor and
// these accounts, making it if there is none. created says which.
func (s *Service) FindOrCreateChat(ctx context.Context, actor model.Account, ids []model.AccountID) (model.Conversation, bool, error) {
	all := []model.AccountID{actor.ID}
	seen := map[model.AccountID]bool{actor.ID: true}
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			all = append(all, id)
		}
	}
	if len(all) > model.MaxAdhocMembers {
		return model.Conversation{}, false, model.ErrTooLarge.With("An ad-hoc channel holds at most 50 people. Make a named channel.")
	}
	others, err := s.store.Accounts().GetMany(ctx, all[1:])
	if err != nil {
		return model.Conversation{}, false, err
	}
	if len(others) != len(all)-1 {
		return model.Conversation{}, false, model.ErrNotFound
	}
	if err := s.perm.CanStartChat(ctx, actor, others); err != nil {
		return model.Conversation{}, false, err
	}
	// Accounts of other servers: the chat needs a stream behind it, which
	// the link opens once the chat exists (shared.go, Outbound.StartAdhoc).
	var remote []model.AccountID
	for _, o := range others {
		if !o.Local() {
			remote = append(remote, o.ID)
		}
	}
	if len(remote) > 0 {
		if s.outbound == nil {
			return model.Conversation{}, false, model.ErrForbidden.With("This server is not linked to another.")
		}
		if err := s.outbound.CanReach(ctx, actor.ID, remote); err != nil {
			return model.Conversation{}, false, err
		}
	}
	key := store.AdhocMemberKey(all)
	for attempt := 0; ; attempt++ {
		if c, err := s.store.Conversations().ByMemberKey(ctx, key); err == nil {
			out, err := s.Conversation(ctx, actor, c.ID)
			return out, false, err
		} else if !errors.Is(err, model.ErrNotFound) {
			return model.Conversation{}, false, err
		}
		c := model.Conversation{Kind: model.KindChannel, Private: true, MemberKey: key, CreatedByID: actor.ID, CreatedAt: s.now()}
		err := s.Tx(ctx, func(tx store.Tx, rec *Recorder) error {
			if err := tx.Conversations().Create(ctx, &c); err != nil {
				return err
			}
			for i, id := range all {
				level := model.LevelMember
				if i == 0 {
					level = model.LevelManager
				}
				res, err := tx.Members().AddDirect(ctx, c.ID, id, level, c.CreatedAt)
				if err != nil {
					return err
				}
				by := actor.ID
				if err := rec.Membership(ctx, res, by, ""); err != nil {
					return err
				}
			}
			return nil
		})
		// Someone else made the same chat a moment ago: use theirs.
		if errors.Is(err, model.ErrAlreadyExists) && attempt == 0 {
			continue
		}
		if err != nil {
			return model.Conversation{}, false, err
		}
		if len(remote) > 0 {
			if err := s.outbound.StartAdhoc(ctx, actor, c); err != nil {
				// No stream, so no chat: nothing with a remote member
				// exists without one.
				if _, derr := s.store.Conversations().Delete(ctx, c.ID); derr != nil {
					s.log.Warn("a chat whose stream could not be opened could not be removed", "err", derr)
				}
				return model.Conversation{}, false, err
			}
		}
		out, err := s.Conversation(ctx, actor, c.ID)
		return out, true, err
	}
}

// Join adds the actor to a public channel at its default level.
func (s *Service) Join(ctx context.Context, actor model.Account, id model.ConversationID) (model.Conversation, error) {
	st, err := s.perm.Can(ctx, actor, perm.Join, id)
	if err != nil {
		return model.Conversation{}, hide(err)
	}
	if st.Member == nil || st.Member.DirectLevel == "" {
		err = s.Tx(ctx, func(tx store.Tx, rec *Recorder) error {
			res, err := tx.Members().AddDirect(ctx, id, actor.ID, st.Conversation.DefaultLevel, s.now())
			if errors.Is(err, model.ErrAlreadyExists) {
				return nil
			}
			if err != nil {
				return err
			}
			return rec.Membership(ctx, res, "", "")
		})
		if err != nil {
			return model.Conversation{}, err
		}
	}
	return s.Conversation(ctx, actor, id)
}

// Leave takes the actor out of a channel. The last manager leaving promotes
// the longest-standing member, and the last member of a private or ad-hoc
// channel leaving deletes it with its content.
func (s *Service) Leave(ctx context.Context, actor model.Account, id model.ConversationID) error {
	if _, err := s.perm.Can(ctx, actor, perm.Leave, id); err != nil {
		return hide(err)
	}
	return s.Tx(ctx, func(tx store.Tx, rec *Recorder) error {
		res, err := tx.Members().RemoveDirect(ctx, id, actor.ID)
		if err != nil {
			return err
		}
		return rec.Membership(ctx, res, "", "left")
	})
}

// Members lists a conversation's members for someone who can read it.
func (s *Service) Members(ctx context.Context, actor model.Account, id model.ConversationID, level model.Level, p store.Page) ([]model.Member, string, error) {
	if _, err := s.perm.Can(ctx, actor, perm.ListMembers, id); err != nil {
		return nil, "", hide(err)
	}
	return s.store.Members().List(ctx, id, level, p)
}

// AddMembers adds accounts to a channel and returns their member rows.
func (s *Service) AddMembers(ctx context.Context, actor model.Account, id model.ConversationID, ids []model.AccountID, level model.Level) ([]model.Member, error) {
	// Asked before the transaction so an outsider gets not_found without
	// anything being opened.
	if _, err := s.perm.Can(ctx, actor, perm.See, id); err != nil {
		return nil, hide(err)
	}
	var out []model.Member
	err := s.Tx(ctx, func(tx store.Tx, rec *Recorder) error {
		if err := s.addTx(ctx, tx, rec, actor, id, ids, level); err != nil {
			return err
		}
		for _, a := range ids {
			m, found, err := tx.Members().Get(ctx, id, a)
			if err != nil {
				return err
			}
			if found {
				out = append(out, m)
			}
		}
		return nil
	})
	return out, err
}

// MarkRead moves the actor's read position in a conversation and tells its
// other clients.
func (s *Service) MarkRead(ctx context.Context, actor model.Account, id model.ConversationID, seq int64) (model.Membership, error) {
	st, err := s.perm.Can(ctx, actor, perm.MarkRead, id)
	if err != nil {
		return model.Membership{}, hide(err)
	}
	if seq < 0 {
		return model.Membership{}, model.Invalid("seq", "A read position is 0 or more.")
	}
	flat := flatView(actor, *st.Member)
	m, err := s.store.Members().SetRead(ctx, id, actor.ID, seq, flat)
	if err != nil {
		return model.Membership{}, err
	}
	u, err := s.store.Members().Unread(ctx, id, actor.ID)
	if err != nil {
		return model.Membership{}, err
	}
	ms := membership(m, u, s.now(), flat)
	payload := map[string]any{
		"read_seq":     m.ReadSeq,
		"unread_count": ms.UnreadCount, "mention_count": ms.MentionCount, "badge_count": ms.BadgeCount,
	}
	// `total` is the account's numbers across its conversations, the
	// same four the status endpoint returns.
	s.touch(actor.ID, id)
	if total, ok := s.total(ctx, actor.ID); ok {
		payload["total"] = total
	}
	s.hub.Send(actor.ID, model.Event{Type: model.EventReadPositionMoved, At: s.now(), ConversationID: id, Payload: payload})
	return *ms, nil
}
