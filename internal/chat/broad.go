package chat

import (
	"context"
	"unicode/utf8"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/perm"
	"github.com/tikaydaes/churr/internal/store"
)

// Broad mentions in large channels (ruling R64): what a client asks before
// it sends, and what a conversation says of its reader. Whether someone may
// use a broad mention is perm's to say (perm.Broadcast); who it reaches is
// the store's (store.Messages.MentionBroadly).

type settingsKey struct{}

// withSettings reads the settings rules depend on once for a request that
// shows many conversations, so they are not read again for each.
func (s *Service) withSettings(ctx context.Context) context.Context {
	set, err := s.perm.Settings(ctx)
	if err != nil {
		return ctx
	}
	return context.WithValue(ctx, settingsKey{}, set)
}

// broadAllowed says whether a member may use broad mentions in a
// conversation now, for the conversation object. Nil when it could not be
// worked out: a client then finds out when it asks or posts.
func (s *Service) broadAllowed(ctx context.Context, a store.Areas, actor model.Account, c model.Conversation, m model.Member) *bool {
	set, ok := ctx.Value(settingsKey{}).(perm.Settings)
	if !ok {
		var err error
		if set, err = s.perm.With(a).Settings(ctx); err != nil {
			return nil
		}
	}
	allowed := perm.Check(perm.State{Actor: actor, Conversation: c, Member: &m, Settings: set, Now: s.now()}, perm.Broadcast) == nil
	return &allowed
}

// BroadCheck is what a text's broad mentions would do if it were posted.
type BroadCheck struct {
	// Broad says the text holds @channel, @here or @everyone, outside
	// code.
	Broad bool `json:"broad"`
	// Allowed says the author may use them here. If not, the message
	// would be stored with the words as plain text and notify nobody.
	Allowed bool `json:"allowed"`
	// Recipients is how many people the broad mentions would write a
	// mention for, and Confirm whether that is enough for the client to
	// ask the author first.
	Recipients int64 `json:"recipients"`
	Confirm    bool  `json:"confirm"`
}

// CheckBroadMentions reads a text as Post would and says what its broad
// mentions would do, so a client can warn or ask before sending. Nothing
// is stored and nobody is notified.
func (s *Service) CheckBroadMentions(ctx context.Context, actor model.Account, id model.ConversationID, text string) (BroadCheck, error) {
	st, err := s.perm.Can(ctx, actor, perm.Post, id)
	if err != nil {
		return BroadCheck{}, hide(err)
	}
	if utf8.RuneCountInString(text) > model.MaxMessageText {
		return BroadCheck{}, model.Invalid("text", "A message is at most 16,000 characters.")
	}
	got, err := s.read(ctx, s.store, st, text)
	if err != nil {
		return BroadCheck{}, err
	}
	out := BroadCheck{Broad: got.broadRefused || len(got.broad) > 0, Allowed: !got.broadRefused}
	if !out.Allowed {
		return out, nil
	}
	// Two broad mentions in one text reach mostly the same people; the
	// larger of them is the number shown.
	for _, b := range got.broad {
		n, err := s.store.Messages().BroadReach(ctx, st.Conversation, actor.ID, b)
		if err != nil {
			return BroadCheck{}, err
		}
		out.Recipients = max(out.Recipients, n)
	}
	out.Confirm = out.Recipients > model.BroadConfirmAbove
	return out, nil
}
