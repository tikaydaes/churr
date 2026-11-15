package sqlstore

import (
	"context"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

// standingMemberCols is memberCols with the alias sm, for a row that may
// not be there: the two columns read into plain numbers and not through a
// helper are given a value when it is not.
const standingMemberCols = `sm.conversation_id, sm.account_id, sm.level, sm.direct_level, sm.via_group, sm.joined_at,
	COALESCE(sm.read_seq, 0), sm.mode, sm.show, sm.mode_before_mute, sm.muted_until, sm.starred, sm.thread_view,
	sm.last_post_at, sm.ignore_broad, sm.may_post`

func (m members) Standing(ctx context.Context, conv model.ConversationID, account model.AccountID, now model.Time) (store.Standing, error) {
	var (
		out store.Standing
		mem model.Member
		bot store.BotInfo
	)
	targets := conversationTargets(&out.Conversation)
	targets = append(targets, memberTargets(&mem)...)
	targets = append(targets, tm(&out.TimedOutUntil), str(&bot.AccountID), &bot.Description, str(&bot.CreatedBy), flag(&bot.WebhookOnly))
	// The same reads as Conversations.Get, Members.Get,
	// Moderation.TimedOutUntil and Bots.Get, as one statement.
	err := m.rd.QueryRowContext(ctx, `
		SELECT `+conversationCols+`, `+standingMemberCols+`,
			(SELECT MAX(t.until) FROM timeouts t WHERE t.account_id = ? AND t.lifted_at IS NULL AND t.until > ?
				AND (t.conversation_id IS NULL OR t.conversation_id = c.id)),
			sb.account_id, COALESCE(sb.description, ''), sb.created_by, sb.webhook_only
		FROM conversations c
		LEFT JOIN members sm ON sm.conversation_id = c.id AND sm.account_id = ?
		LEFT JOIN bots sb ON sb.account_id = ?
		WHERE c.id = ?`,
		string(account), int64(now), string(account), string(account), string(conv)).Scan(targets...)
	if err != nil {
		return store.Standing{}, m.mapErr(err)
	}
	// What scanConversationInto does after its scan.
	c := &out.Conversation
	c.LastSeq = c.NextSeq - 1
	if c.BroadMentions == "" {
		c.BroadMentions = model.BroadServer
	}
	if c.LazyDelivery == "" {
		c.LazyDelivery = model.LazyServer
	}
	if mem.AccountID != "" {
		out.Member = &mem
	}
	if bot.AccountID != "" {
		out.Bot = &bot
	}
	return out, nil
}
