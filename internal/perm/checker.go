package perm

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

// Checker loads the state the rules need from the store and asks them. It
// keeps no state of its own.
type Checker struct {
	s store.Areas
	// now is the clock; tests replace it.
	now func() model.Time
}

// New returns a checker over a store or a transaction.
func New(s store.Areas) *Checker { return &Checker{s: s, now: model.Now} }

// With returns the same checker reading through another handle, usually a
// transaction, so a check sees what the transaction has written.
func (c *Checker) With(s store.Areas) *Checker { return &Checker{s: s, now: c.now} }

// Settings reads the settings rules depend on. They are read in one query:
// every check, and every conversation of every catch-up, starts here.
func (c *Checker) Settings(ctx context.Context) (Settings, error) {
	set := DefaultSettings()
	all, err := c.s.Settings().All(ctx)
	if err != nil {
		return set, err
	}
	// A key with no row, or with a value of another kind, has its default.
	for _, b := range []struct {
		key string
		to  *bool
	}{
		{store.SettingMembersMayInvite, &set.MembersMayInvite},
		{store.SettingMembersMayAddEmoji, &set.MembersMayAddEmoji},
		{store.SettingBroadcastManagers, &set.BroadcastManagersOnly},
		{store.SettingBlockingEnabled, &set.BlockingEnabled},
		{store.SettingSealedEnabled, &set.SealedEnabled},
		{store.SettingCallsEnabled, &set.CallsEnabled},
		{store.SettingWebhooksManagersMay, &set.WebhooksManagersMayApprove},
	} {
		var v bool
		if raw, found := all[b.key]; found && json.Unmarshal(raw, &v) == nil {
			*b.to = v
		}
	}
	for _, n := range []struct {
		key string
		to  *int64
	}{
		{store.SettingEditWindow, &set.EditWindowSeconds},
		{store.SettingRetention, &set.RetentionSeconds},
	} {
		// null means "not set", as store.SettingInt reads it.
		var v *int64
		if raw, found := all[n.key]; found && json.Unmarshal(raw, &v) == nil && v != nil {
			*n.to = *v
		} else {
			*n.to = 0
		}
	}
	return set, nil
}

// Load reads the state for one actor and one conversation. It returns
// model.ErrNotFound only when the conversation row does not exist; it never
// decides anything itself.
func (c *Checker) Load(ctx context.Context, actor model.Account, conv model.ConversationID) (State, error) {
	st := State{Actor: actor, Now: c.now()}
	var err error
	if st.Conversation, err = c.s.Conversations().Get(ctx, conv); err != nil {
		return st, err
	}
	if st.Settings, err = c.Settings(ctx); err != nil {
		return st, err
	}
	m, found, err := c.s.Members().Get(ctx, conv, actor.ID)
	if err != nil {
		return st, err
	}
	if found {
		st.Member = &m
	}
	until, timedOut, err := c.s.Moderation().TimedOutUntil(ctx, actor.ID, conv, st.Now)
	if err != nil {
		return st, err
	}
	if timedOut {
		st.TimedOutUntil = until
	}
	if actor.Role == model.RoleBot {
		b, found, err := c.s.Bots().Get(ctx, actor.ID)
		if err != nil {
			return st, err
		}
		if found {
			st.Bot = &b
		}
	}
	// A channel a hub owns has moderators, named in the record the hub
	// signed. The link package keeps, with the record, which of them are
	// accounts of this server (section 14.1).
	if st.Conversation.IsShared() && actor.Local() {
		rec, err := c.s.Streams().Channel(ctx, st.Conversation.StreamID)
		if err != nil && !errors.Is(err, model.ErrNotFound) {
			return st, err
		}
		for _, id := range rec.Moderators {
			if id == actor.ID {
				st.HubModerator = true
			}
		}
	}
	// A channel made by moving a thread can be seen and joined by the
	// members of the channel the thread came out of (section 4.1 note 2).
	if from := st.Conversation.MovedFromMessageID; from != "" && st.Member == nil {
		st.FromMovedThreadOf, err = c.memberWhere(ctx, actor.ID, from)
		if err != nil {
			return st, err
		}
	}
	return st, nil
}

// Can is Load followed by Check. A conversation that does not exist and one
// the actor may not know exists give the same answer.
func (c *Checker) Can(ctx context.Context, actor model.Account, a Action, conv model.ConversationID) (State, error) {
	st, err := c.Load(ctx, actor, conv)
	if err != nil {
		return st, err
	}
	return st, Check(st, a)
}

func (c *Checker) can(ctx context.Context, actor model.Account, a Action, conv model.ConversationID) error {
	_, err := c.Can(ctx, actor, a, conv)
	return err
}

// CanSee answers whether the actor may know the conversation exists.
func (c *Checker) CanSee(ctx context.Context, actor model.Account, conv model.ConversationID) error {
	return c.can(ctx, actor, See, conv)
}

// CanRead answers whether the actor may read the conversation.
func (c *Checker) CanRead(ctx context.Context, actor model.Account, conv model.ConversationID) error {
	return c.can(ctx, actor, Read, conv)
}

// CanPost answers whether the actor may post a top-level message.
func (c *Checker) CanPost(ctx context.Context, actor model.Account, conv model.ConversationID) error {
	return c.can(ctx, actor, Post, conv)
}

// CanReact answers whether the actor may react in the conversation.
func (c *Checker) CanReact(ctx context.Context, actor model.Account, conv model.ConversationID) error {
	return c.can(ctx, actor, React, conv)
}

// CanJoin answers whether the actor may join the conversation by itself.
func (c *Checker) CanJoin(ctx context.Context, actor model.Account, conv model.ConversationID) error {
	return c.can(ctx, actor, Join, conv)
}

// CanManage answers whether the actor acts as manager of the conversation,
// the gate for the management actions of section 7.
func (c *Checker) CanManage(ctx context.Context, actor model.Account, conv model.ConversationID) error {
	return c.can(ctx, actor, SetTopic, conv)
}

// CanJoinCall answers whether the actor may start or join a call.
func (c *Checker) CanJoinCall(ctx context.Context, actor model.Account, conv model.ConversationID) error {
	return c.can(ctx, actor, StartCall, conv)
}

// CanMakePrivate answers whether the actor may make a public channel private.
func (c *Checker) CanMakePrivate(ctx context.Context, actor model.Account, conv model.ConversationID) error {
	return c.can(ctx, actor, MakePrivate, conv)
}

// CanLock answers whether the actor may lock the channel from inside it.
func (c *Checker) CanLock(ctx context.Context, actor model.Account, conv model.ConversationID) error {
	return c.can(ctx, actor, Lock, conv)
}

// CanAdminister answers a server-wide question with the settings as stored.
func (c *Checker) CanAdminister(ctx context.Context, actor model.Account, a ServerAction) error {
	set, err := c.Settings(ctx)
	if err != nil {
		return err
	}
	return CheckServer(actor, a, set)
}

// CanSeeAccount is section 4.3. An account the actor may not see does not
// exist for them.
func (c *Checker) CanSeeAccount(ctx context.Context, actor, target model.Account) error {
	if !actor.Active() {
		return model.ErrUnauthenticated
	}
	if actor.ID == target.ID {
		return nil
	}
	if actor.Role.IsFullMember() && target.Local() {
		return nil
	}
	shares, err := c.s.Accounts().SharesConversation(ctx, actor.ID, target.ID)
	if err != nil {
		return err
	}
	if !shares {
		return model.ErrNotFound
	}
	return nil
}

// blockedBy reports whether target has blocked actor, while blocking is on.
func (c *Checker) blockedBy(ctx context.Context, set Settings, actor, target model.AccountID) (bool, error) {
	if !set.BlockingEnabled {
		return false, nil
	}
	return c.s.Blocks().Has(ctx, target, actor)
}

// reachable applies what starting a chat with someone and adding someone
// have in common: the target is visible to the actor, can act, and has not
// blocked the actor.
func (c *Checker) reachable(ctx context.Context, set Settings, actor, target model.Account) error {
	if err := c.CanSeeAccount(ctx, actor, target); err != nil {
		return err
	}
	if target.Role == model.RoleRemote {
		// docs/LINK-PROTOCOL.md section 16: only while the two share a
		// shared channel. The link then checks, as does every server
		// involved, that the conversation can run over one link, and
		// puts a stream behind it; a remote account is never put into a
		// conversation that has none (CanAdd, and chat's callers).
		shared, err := c.s.Streams().SharedWith(ctx, actor.ID, target.ID)
		if err != nil {
			return err
		}
		if len(shared) == 0 {
			return forbidden("That person is on another server, and you share no shared channel with them.")
		}
		if target.Removed() {
			return forbidden("That account cannot be added.")
		}
		blocked, err := c.blockedBy(ctx, set, actor.ID, target.ID)
		if err != nil {
			return err
		}
		if blocked {
			return forbidden("That account cannot be added.")
		}
		return nil
	}
	if !target.Active() {
		return forbidden("That account cannot be added.")
	}
	blocked, err := c.blockedBy(ctx, set, actor.ID, target.ID)
	if err != nil {
		return err
	}
	if blocked {
		// The wording must not say why (section 13).
		return forbidden("That account cannot be added.")
	}
	return nil
}

// CanStartChat answers whether actor may start an ad-hoc channel with these
// accounts (section 9.1 with notes 1 and 2, section 13).
func (c *Checker) CanStartChat(ctx context.Context, actor model.Account, others []model.Account) error {
	set, err := c.Settings(ctx)
	if err != nil {
		return err
	}
	if err := CheckServer(actor, StartAdhoc, set); err != nil {
		return err
	}
	for _, o := range others {
		if err := c.reachable(ctx, set, actor, o); err != nil {
			return err
		}
	}
	// A server-wide time-out stops starting chats (section 11).
	_, timedOut, err := c.s.Moderation().TimedOutUntil(ctx, actor.ID, "", c.now())
	if err != nil {
		return err
	}
	if timedOut {
		return model.ErrTimedOut
	}
	return nil
}

// CanAdd answers whether the actor of st may add target to st's
// conversation. The cap on an ad-hoc channel's size is the chat service's.
func (c *Checker) CanAdd(ctx context.Context, st State, target model.Account) error {
	a := AddAccount
	if target.Role == model.RoleBot {
		a = AddBot
	}
	if err := Check(st, a); err != nil {
		return err
	}
	if target.Role == model.RoleRemote && !st.Conversation.Adhoc() {
		// Each server manages its own members of a shared channel
		// (docs/LINK-PROTOCOL.md section 20); only an ad-hoc channel
		// takes someone from another server.
		return forbidden("That person is on another server, and this conversation is not shared with it.")
	}
	return c.reachable(ctx, st.Settings, st.Actor, target)
}

// Recipients returns who receives a conversation's live events: its members
// and nobody else (section 4.2, "Live events"). Someone reading a public
// channel without having joined gets nothing live from it.
func (c *Checker) Recipients(ctx context.Context, conv model.ConversationID) ([]model.AccountID, error) {
	// From the roster the store keeps in memory (roster.go): a typing
	// notice in a large channel must not read every member.
	r, err := c.s.Members().Roster(ctx, conv)
	if err != nil {
		return nil, err
	}
	return r.IDs[:len(r.IDs):len(r.IDs)], nil
}

// Readable describes what the actor may read, for search and list queries
// to join against: the channels it is a member of, plus every public channel
// for an active owner, admin or member.
func (c *Checker) Readable(ctx context.Context, actor model.Account) store.Scope {
	if !actor.Active() {
		return store.Scope{}
	}
	return store.Scope{AccountID: actor.ID, PublicChannels: actor.Role.IsFullMember()}
}

// memberWhere reports whether account is a member of the conversation that
// holds a message. A message that has gone gives false.
func (c *Checker) memberWhere(ctx context.Context, account model.AccountID, msg model.MessageID) (bool, error) {
	m, err := c.s.Messages().Get(ctx, msg)
	if err != nil {
		if model.CodeOf(err) == model.CodeNotFound {
			return false, nil
		}
		return false, err
	}
	_, found, err := c.s.Members().Get(ctx, m.ConversationID, account)
	return found, err
}

// CanTimeOutEverywhere answers whether actor may time target out on the
// whole server (section 11): an admin, on members, guests, bots and remote
// accounts; only the owner on an admin; nobody on the owner.
func (c *Checker) CanTimeOutEverywhere(actor, target model.Account) error {
	if err := CheckOnAccount(actor, TimeOutAccount, target); err != nil {
		return err
	}
	return timeOutTarget(actor, target)
}
