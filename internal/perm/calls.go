package perm

import (
	"context"

	"github.com/tikaydaes/churr/internal/model"
)

// CheckRemoteCall answers whether the shadow account of someone on another
// server may be in the call of a shared conversation that this server
// hosts (docs/CALLS.md sections 3 and 16). Check cannot answer it: its
// first step refuses any account that cannot act here, and a remote account
// acts only through its own server.
//
// The rule is the same one a local member is held to, read from this
// server's own copy: a member of the conversation at level `member` or
// above, the conversation not archived, the account not timed out, and
// calls switched on. Anything else is `forbidden`: the asking server
// already knows the conversation exists.
func CheckRemoteCall(st State) error {
	a := st.Actor
	if a.Role != model.RoleRemote || a.Local() || a.Removed() {
		return model.ErrForbidden.With("Only someone from a linked server can join this way.")
	}
	if !st.Settings.CallsEnabled {
		return model.ErrFeatureOff
	}
	if st.Member == nil || !st.Member.Level.AtLeast(model.LevelMember) {
		return model.ErrForbidden.With("Only members of this conversation can join its call.")
	}
	if st.Conversation.Archived() {
		return model.ErrArchived
	}
	if !st.TimedOutUntil.IsZero() {
		return model.ErrTimedOut
	}
	return nil
}

// CanJoinCallRemote is Load followed by CheckRemoteCall.
func (c *Checker) CanJoinCallRemote(ctx context.Context, actor model.Account, conv model.ConversationID) error {
	st, err := c.Load(ctx, actor, conv)
	if err != nil {
		return err
	}
	return CheckRemoteCall(st)
}
