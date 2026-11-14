package sqlstore

import (
	"context"
	"database/sql"
	"errors"
	"sort"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

type reactions struct{ *queries }

// Reactions returns the reactions area.
func (q *queries) Reactions() store.Reactions { return reactions{q} }

// reactionIDsShown is how many account ids a message carries per emoji; the
// rest come from the reactions list.
const reactionIDsShown = 10

func (r reactions) Add(ctx context.Context, msg model.MessageID, account model.AccountID, emoji string, at model.Time) (bool, error) {
	if at.IsZero() {
		at = model.Now()
	}
	added := false
	err := r.atomic(ctx, func(q *queries) error {
		// The message row is locked so that two people adding two new
		// emoji at once cannot both find room under the limit.
		var deletedAt model.Time
		err := q.wr.QueryRowContext(ctx, `SELECT deleted_at FROM messages WHERE id = ?`+q.forUpdate(), string(msg)).Scan(tm(&deletedAt))
		if err != nil {
			return q.mapErr(err)
		}
		if !deletedAt.IsZero() {
			return model.ErrConflict.With("A deleted message cannot be reacted to.")
		}
		var different, ofThis int
		err = q.wr.QueryRowContext(ctx, `
			SELECT count(DISTINCT emoji), COALESCE(SUM(CASE WHEN emoji = ? THEN 1 ELSE 0 END), 0)
			FROM reactions WHERE message_id = ?`, emoji, string(msg)).Scan(&different, &ofThis)
		if err != nil {
			return err
		}
		if ofThis == 0 && different >= model.MaxEmojiPerMessage {
			return model.ErrConflict.With("A message can carry at most twenty different emoji.")
		}
		n, err := q.affected(q.wr.ExecContext(ctx, `
			INSERT INTO reactions (message_id, account_id, emoji, at) VALUES (?, ?, ?, ?)
			ON CONFLICT (message_id, emoji, account_id) DO NOTHING`, string(msg), string(account), emoji, int64(at)))
		added = n > 0
		return err
	})
	if err != nil {
		return false, err
	}
	return added, nil
}

func (r reactions) Remove(ctx context.Context, msg model.MessageID, account model.AccountID, emoji string) (bool, error) {
	n, err := r.affected(r.wr.ExecContext(ctx,
		`DELETE FROM reactions WHERE message_id = ? AND emoji = ? AND account_id = ?`, string(msg), emoji, string(account)))
	return n > 0, err
}

func (r reactions) List(ctx context.Context, msg model.MessageID, reader model.AccountID) ([]model.Reaction, error) {
	rows, err := r.rd.QueryContext(ctx,
		`SELECT emoji, account_id, at FROM reactions WHERE message_id = ? ORDER BY at, account_id`, string(msg))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var entries []reactionEntry
	at := map[string]int{}
	for rows.Next() {
		var (
			emoji   string
			account model.AccountID
			when    model.Time
		)
		if err := rows.Scan(&emoji, str(&account), tm(&when)); err != nil {
			return nil, err
		}
		i, ok := at[emoji]
		if !ok {
			i = len(entries)
			at[emoji] = i
			entries = append(entries, reactionEntry{Reaction: model.Reaction{Emoji: emoji}, first: when})
		}
		e := &entries[i]
		e.Count++
		e.AccountIDs = append(e.AccountIDs, account)
		if account == reader && reader != "" {
			e.Me = true
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return sortReactions(entries), nil
}

func (r reactions) One(ctx context.Context, msg model.MessageID, emoji string, reader model.AccountID) (model.Reaction, error) {
	out := model.Reaction{Emoji: emoji, AccountIDs: []model.AccountID{}}
	var mine int
	err := r.rd.QueryRowContext(ctx, `
		SELECT count(*), COALESCE(SUM(CASE WHEN account_id = ? THEN 1 ELSE 0 END), 0)
		FROM reactions WHERE message_id = ? AND emoji = ?`, id(reader), string(msg), emoji).Scan(&out.Count, &mine)
	if err != nil {
		return out, err
	}
	out.Me = mine > 0
	if out.Count == 0 {
		return out, nil
	}
	ids, err := collectIDs[model.AccountID](r.rd.QueryContext(ctx, `
		SELECT account_id FROM reactions WHERE message_id = ? AND emoji = ? ORDER BY at, account_id LIMIT ?`,
		string(msg), emoji, reactionIDsShown))
	if err != nil {
		return out, err
	}
	out.AccountIDs = ids
	return out, nil
}

func (r reactions) Has(ctx context.Context, msg model.MessageID, emoji string, account model.AccountID) (bool, error) {
	var one int
	err := r.rd.QueryRowContext(ctx,
		`SELECT 1 FROM reactions WHERE message_id = ? AND emoji = ? AND account_id = ?`, string(msg), emoji, string(account)).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

// reactionEntry is one emoji on one message while it is being put together,
// with the moment the emoji first appeared, which decides its place.
type reactionEntry struct {
	model.Reaction
	first model.Time
}

// sortReactions puts a message's emoji in the order they first appeared. Two
// that appeared in the same millisecond are ordered by the emoji itself, in
// Go and not in SQL, so the two databases cannot disagree about it.
func sortReactions(entries []reactionEntry) []model.Reaction {
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].first != entries[j].first {
			return entries[i].first < entries[j].first
		}
		return entries[i].Emoji < entries[j].Emoji
	})
	out := make([]model.Reaction, len(entries))
	for i, e := range entries {
		out[i] = e.Reaction
		if out[i].AccountIDs == nil {
			out[i].AccountIDs = []model.AccountID{}
		}
	}
	return out
}

// reactionsOf returns the reactions of a batch of messages in the shape a
// message carries them: every emoji with its count and Me, and at most the
// first ten account ids. It takes two queries however many messages there
// are.
func (q *queries) reactionsOf(ctx context.Context, msgs []model.MessageID, reader model.AccountID) (map[model.MessageID][]model.Reaction, error) {
	if len(msgs) == 0 {
		return nil, nil
	}
	in := `(` + marks(len(msgs)) + `)`
	type key struct {
		msg   model.MessageID
		emoji string
	}
	entries := map[model.MessageID][]reactionEntry{}
	where := map[key]int{}

	rows, err := q.rd.QueryContext(ctx, `
		SELECT r.message_id, r.emoji, count(*), MIN(r.at), COALESCE(SUM(CASE WHEN r.account_id = ? THEN 1 ELSE 0 END), 0)
		FROM reactions r WHERE r.message_id IN `+in+`
		GROUP BY r.message_id, r.emoji`, append([]any{id(reader)}, anys(msgs)...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			k    key
			e    reactionEntry
			mine int
		)
		if err := rows.Scan(str(&k.msg), &k.emoji, &e.Count, tm(&e.first), &mine); err != nil {
			return nil, err
		}
		e.Emoji, e.Me = k.emoji, mine > 0
		where[k] = len(entries[k.msg])
		entries[k.msg] = append(entries[k.msg], e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	if len(entries) == 0 {
		return nil, nil
	}

	// The first ten accounts of each emoji, numbered by the database so a
	// message with thousands of reactions costs ten rows per emoji.
	ids, err := q.rd.QueryContext(ctx, `
		SELECT x.message_id, x.emoji, x.account_id FROM (
			SELECT r.message_id, r.emoji, r.account_id,
				ROW_NUMBER() OVER (PARTITION BY r.message_id, r.emoji ORDER BY r.at, r.account_id) AS n
			FROM reactions r WHERE r.message_id IN `+in+`) x
		WHERE x.n <= ? ORDER BY x.message_id, x.emoji, x.n`, append(anys(msgs), reactionIDsShown)...)
	if err != nil {
		return nil, err
	}
	defer ids.Close()
	for ids.Next() {
		var (
			k       key
			account model.AccountID
		)
		if err := ids.Scan(str(&k.msg), &k.emoji, str(&account)); err != nil {
			return nil, err
		}
		// A reaction added between the two queries has no entry; the
		// next read shows it.
		if i, ok := where[k]; ok {
			e := &entries[k.msg][i]
			e.AccountIDs = append(e.AccountIDs, account)
		}
	}
	if err := ids.Err(); err != nil {
		return nil, err
	}
	out := make(map[model.MessageID][]model.Reaction, len(entries))
	for msg, list := range entries {
		out[msg] = sortReactions(list)
	}
	return out, nil
}
