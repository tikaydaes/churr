package sqlstore

import (
	"context"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

// A broad mention in a channel of five thousand people is five thousand
// rows. They are written by the database from the member rows it already
// holds, in one statement, and never by this process reading the members
// and writing a row for each (ruling R64).

// broadFrom is the FROM and WHERE that select the accounts a broad mention
// reaches, as the alias a, and the arguments that go with them.
func broadFrom(conv model.Conversation, author model.AccountID, b store.BroadMention, online []model.AccountID) (string, []any) {
	var sql string
	args := []any{string(conv.ID)}
	if b.Scope == store.BroadEveryone {
		// Everyone in the built-in group who can read the conversation:
		// its members, and for a public channel the rest of the group.
		sql = ` FROM accounts a LEFT JOIN members mb ON mb.account_id = a.id AND mb.conversation_id = ?
			WHERE ` + inEveryone + ` AND (mb.ignore_broad IS NULL OR mb.ignore_broad = ?)`
		args = append(args, false)
		if !conv.Public() {
			sql += ` AND mb.account_id IS NOT NULL`
		}
	} else {
		sql = ` FROM members mb JOIN accounts a ON a.id = mb.account_id
			WHERE mb.conversation_id = ? AND mb.ignore_broad = ?
			AND a.server_id IS NULL AND a.claimed_at IS NOT NULL AND a.removed_at IS NULL`
		args = append(args, false)
	}
	sql += ` AND a.id <> ?`
	args = append(args, string(author))
	if b.Blocking {
		sql += ` AND NOT EXISTS (SELECT 1 FROM blocks bl WHERE bl.account_id = a.id AND bl.blocked_id = ?)`
		args = append(args, string(author))
	}
	if b.Scope == store.BroadHere {
		sql += ` AND a.id IN (` + marks(len(online)) + `)`
		args = append(args, anys(online)...)
	}
	return sql, args
}

// broadChunks calls fn once, or for BroadHere once for each inChunk of the
// people online: a statement takes only so many arguments.
func broadChunks(b store.BroadMention, fn func(online []model.AccountID) error) error {
	if b.Scope != store.BroadHere {
		return fn(nil)
	}
	for from := 0; from < len(b.Online); from += inChunk {
		if err := fn(b.Online[from:min(from+inChunk, len(b.Online))]); err != nil {
			return err
		}
	}
	return nil
}

func (ms messages) MentionBroadly(ctx context.Context, m model.Message, b store.BroadMention) (int64, error) {
	conv, err := ms.Conversations().Get(ctx, m.ConversationID)
	if err != nil {
		return 0, err
	}
	var wrote int64
	err = broadChunks(b, func(online []model.AccountID) error {
		from, args := broadFrom(conv, m.AuthorID, b, online)
		// Nor a broad one, on a message from before their first join in a
		// private channel (ruling R87).
		from += ` AND (mb.visible_from IS NULL OR mb.visible_from <= ?)`
		args = append(args, int64(m.CreatedAt))
		args = append([]any{string(m.ID), string(m.ConversationID), m.Seq, id(m.ParentID), b.Kind()}, args...)
		n, err := ms.affected(ms.wr.ExecContext(ctx, `
			INSERT INTO mentions (message_id, account_id, conversation_id, seq, parent_id, kind)
			SELECT ?, a.id, ?, ?, ?, ?`+from+`
			ON CONFLICT (message_id, account_id) DO NOTHING`, args...))
		wrote += n
		return err
	})
	return wrote, err
}

func (ms messages) BroadReach(ctx context.Context, conv model.Conversation, author model.AccountID, b store.BroadMention) (int64, error) {
	var total int64
	err := broadChunks(b, func(online []model.AccountID) error {
		from, args := broadFrom(conv, author, b, online)
		var n int64
		if err := ms.rd.QueryRowContext(ctx, `SELECT count(*)`+from, args...).Scan(&n); err != nil {
			return ms.mapErr(err)
		}
		total += n
		return nil
	})
	return total, err
}
