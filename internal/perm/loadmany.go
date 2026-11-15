package perm

import (
	"context"
	"errors"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

// loadChunk bounds how many ids go into one IN list: both databases limit
// the number of arguments of a statement.
const loadChunk = 500

// LoadMany is Load for one actor and many conversations, in a handful of
// queries however many there are: a client that comes back names every
// conversation it holds, and reading the same four rows for each of them
// was most of the cost of a catch-up with nothing to send.
//
// The state of each conversation is what Load would return for it. A
// conversation that does not exist has no entry.
func (c *Checker) LoadMany(ctx context.Context, actor model.Account, convs []model.ConversationID) (map[model.ConversationID]State, error) {
	out := make(map[model.ConversationID]State, len(convs))
	if len(convs) == 0 {
		return out, nil
	}
	now := c.now()
	set, err := c.Settings(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := c.s.Members().OfAccount(ctx, actor.ID)
	if err != nil {
		return nil, err
	}
	members := make(map[model.ConversationID]*model.Member, len(rows))
	for i := range rows {
		members[rows[i].ConversationID] = &rows[i]
	}
	server, timeouts, err := c.s.Moderation().TimedOutEverywhere(ctx, actor.ID, now)
	if err != nil {
		return nil, err
	}
	// The channels the actor is banned from, read once (section 6.1).
	bannedList, err := c.s.Moderation().ChannelBansOf(ctx, actor.ID, now)
	if err != nil {
		return nil, err
	}
	banned := make(map[model.ConversationID]bool, len(bannedList))
	for _, id := range bannedList {
		banned[id] = true
	}
	var bot *store.BotInfo
	if actor.Role == model.RoleBot {
		b, found, err := c.s.Bots().Get(ctx, actor.ID)
		if err != nil {
			return nil, err
		}
		if found {
			bot = &b
		}
	}
	for from := 0; from < len(convs); from += loadChunk {
		got, err := c.s.Conversations().GetMany(ctx, convs[from:min(from+loadChunk, len(convs))])
		if err != nil {
			return nil, err
		}
		for _, conv := range got {
			st := State{Actor: actor, Now: now, Conversation: conv, Settings: set, Member: members[conv.ID], Bot: bot}
			st.TimedOutUntil = max(server, timeouts[conv.ID])
			st.ChannelBanned = st.Member == nil && banned[conv.ID]
			// The two rare cases are read as Load reads them.
			if err := c.loadRare(ctx, &st); err != nil {
				return nil, err
			}
			out[conv.ID] = st
		}
	}
	return out, nil
}

// loadRare fills in the parts of a state that only a shared channel or a
// channel made from a thread has.
func (c *Checker) loadRare(ctx context.Context, st *State) error {
	actor := st.Actor
	// A channel a hub owns has moderators, named in the record the hub
	// signed. The link package keeps, with the record, which of them are
	// accounts of this server (section 14.1).
	if st.Conversation.IsShared() && actor.Local() {
		rec, err := c.s.Streams().Channel(ctx, st.Conversation.StreamID)
		if err != nil && !errors.Is(err, model.ErrNotFound) {
			return err
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
		var err error
		st.FromMovedThreadOf, err = c.memberWhere(ctx, actor.ID, from)
		if err != nil {
			return err
		}
	}
	return nil
}
