package chat

import (
	"context"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/perm"
	"github.com/tikaydaes/churr/internal/store"
)

// Sub-channels (ruling R49, docs/PERMISSIONS.md section 7.2). Nothing is
// inherited, so almost nothing here: who may set a parent is perm's, and
// what is left is keeping `parent_id` from a reader who cannot see the
// parent, wherever a conversation is returned or sent.

// SubChannelParent says which channel a channel the server makes out of c
// sits under: a thread moved out of c, or a ticket channel opened by a form
// in c. It is c, or c's own parent when c is a sub-channel (a sibling), or
// none when c is not a named channel. Put the result in the new
// conversation's ParentID before it is created; no right over the parent is
// needed.
func SubChannelParent(c model.Conversation) model.ConversationID { return perm.SubChannelParent(c) }

// seesParent reports whether an account may be told a channel is a parent.
func (s *Service) seesParent(ctx context.Context, a store.Areas, actor model.Account, parent model.ConversationID) bool {
	if actor.ID == "" {
		// Nobody in particular: the form an event is stored in. Asking the
		// database about no account would also fail on PostgreSQL.
		return false
	}
	st, err := s.perm.With(a).Load(ctx, actor, parent)
	return err == nil && perm.SeesParent(st)
}

// parentFor returns c as one reader is given it: without its parent_id if
// the reader cannot see the parent, so that a private parent's existence
// is not revealed.
func (s *Service) parentFor(ctx context.Context, a store.Areas, actor model.Account, c model.Conversation) model.Conversation {
	if c.ParentID != "" && !s.seesParent(ctx, a, actor, c.ParentID) {
		c.ParentID = ""
	}
	return c
}

// parentForID is parentFor for a reader known only by identifier.
func (s *Service) parentForID(ctx context.Context, a store.Areas, account model.AccountID, c model.Conversation) model.Conversation {
	if c.ParentID == "" {
		return c
	}
	actor, err := a.Accounts().Get(ctx, account)
	if err != nil {
		c.ParentID = ""
		return c
	}
	return s.parentFor(ctx, a, actor, c)
}

// tailorConversationEvent fills in parent_id per reader on the events that
// carry a conversation. The event is stored with it; a recipient who cannot
// see the parent gets a copy of the conversation without it, live and on
// catch-up alike.
func (s *Service) tailorConversationEvent(ctx context.Context, e model.Event, to model.AccountID) (map[string]any, bool) {
	switch e.Type {
	case model.EventConversationUpdated, model.EventConversationArchived, model.EventConversationUnarchived:
	default:
		return nil, false
	}
	conv, _ := e.Payload["conversation"].(map[string]any)
	parent, _ := conv["parent_id"].(string)
	if parent == "" {
		return nil, true
	}
	actor, err := s.store.Accounts().Get(ctx, to)
	if err == nil && s.seesParent(ctx, s.store, actor, model.ConversationID(parent)) {
		return nil, true
	}
	out := make(map[string]any, len(conv))
	for k, v := range conv {
		if k != "parent_id" {
			out[k] = v
		}
	}
	return map[string]any{"conversation": out}, true
}

// setParent validates a change of parent for UpdateConversation and the
// admin console. parent "" clears it.
func (s *Service) setParent(ctx context.Context, child perm.State, parent model.ConversationID, viaAdmin bool) error {
	var ps *perm.State
	if parent != "" {
		st, err := s.perm.Load(ctx, child.Actor, parent)
		if err != nil {
			return hide(err)
		}
		ps = &st
	}
	subs, err := s.store.Conversations().SubChannelCount(ctx, child.Conversation.ID)
	if err != nil {
		return err
	}
	return perm.CheckSetParent(child, ps, subs > 0, viaAdmin)
}
