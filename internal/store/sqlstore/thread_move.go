package sqlstore

import (
	"context"
	"sort"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

func (ms messages) SetHistoryHidden(ctx context.Context, msg model.MessageID, at model.Time) error {
	return ms.changed(ms.wr.ExecContext(ctx, `UPDATE messages SET history_hidden_at = ? WHERE id = ?`, ms2(at), string(msg)))
}

func (ms messages) SetCards(ctx context.Context, msg model.MessageID, cards []model.Card) (model.Message, error) {
	var out model.Message
	err := ms.atomic(ctx, func(q *queries) error {
		if err := q.changed(q.wr.ExecContext(ctx, `UPDATE messages SET cards = ? WHERE id = ?`, jsonList(cards), string(msg))); err != nil {
			return err
		}
		var err error
		out, err = q.message(ctx, q.wr, msg, false)
		return err
	})
	return out, err
}

func (q *queries) threadMovedOut(ctx context.Context, ex execer, parent model.MessageID) (bool, error) {
	// The pointer is the only system notice that is ever a reply.
	var n int
	err := ex.QueryRowContext(ctx, `SELECT count(*) FROM messages WHERE parent_id = ? AND system IS NOT NULL`, string(parent)).Scan(&n)
	return n > 0, err
}

func (ms messages) ThreadMovedOut(ctx context.Context, parent model.MessageID) (bool, error) {
	return ms.threadMovedOut(ctx, ms.rd, parent)
}

// movedReply is one reply on its way to the new channel.
type movedReply struct {
	id        model.MessageID
	oldSeq    int64
	createdAt model.Time
}

// follower is an account that follows the thread and is still in the old
// channel.
type follower struct {
	account model.AccountID
	level   model.Level
	readSeq int64
	joined  model.Time
}

func (ms messages) MoveThread(ctx context.Context, mv store.ThreadMove) (store.ThreadMoved, error) {
	var out store.ThreadMoved
	if mv.At.IsZero() {
		mv.At = model.Now()
	}
	err := ms.atomic(ctx, func(q *queries) error {
		p, err := q.message(ctx, q.wr, mv.ParentID, true)
		if err != nil {
			return err
		}
		if p.ParentID != "" || p.IsSystem() {
			return model.BadRequest("Only a thread's first message can be moved out, and not a system notice.")
		}
		if moved, err := q.threadMovedOut(ctx, q.wr, p.ID); err != nil {
			return err
		} else if moved {
			return model.ErrConflict.With("This thread has already been moved to a channel.")
		}
		old := p.ConversationID
		// Lock the old channel so no reply arrives while its thread moves.
		if _, err := q.conversationState(ctx, old); err != nil {
			return err
		}
		rows, err := q.wr.QueryContext(ctx,
			`SELECT id, seq, created_at FROM messages WHERE parent_id = ? AND seq IS NOT NULL ORDER BY seq`, string(p.ID))
		replies, err := collect(rows, err, func(s scanner) (movedReply, error) {
			var r movedReply
			err := s.Scan(str(&r.id), &r.oldSeq, tm(&r.createdAt))
			return r, err
		})
		if err != nil {
			return err
		}
		if len(replies) == 0 {
			return model.BadRequest("That message has no replies to move.")
		}

		// Who follows the thread and is still here; read before the
		// thread states are cleared.
		rows, err = q.wr.QueryContext(ctx, `
			SELECT ts.account_id, m.level, ts.read_seq, m.joined_at FROM thread_states ts
			JOIN members m ON m.conversation_id = ? AND m.account_id = ts.account_id
			WHERE ts.parent_id = ? AND ts.state = 'following' ORDER BY m.joined_at, m.account_id`, string(old), string(p.ID))
		followers, err := collect(rows, err, func(s scanner) (follower, error) {
			var f follower
			err := s.Scan(str(&f.account), str(&f.level), &f.readSeq, tm(&f.joined))
			return f, err
		})
		if err != nil {
			return err
		}

		// 1. The new channel.
		d := mv.Channel
		d.Kind, d.MovedFromMessageID = model.KindChannel, p.ID
		if d.CreatedAt.IsZero() {
			d.CreatedAt = mv.At
		}
		if err := (conversations{q}).Create(ctx, &d); err != nil {
			return err
		}
		D, C, P, copyID := string(d.ID), string(old), string(p.ID), string(mv.CopyID)
		exec := func(stmt string, args ...any) error {
			_, err := q.wr.ExecContext(ctx, stmt, args...)
			return q.mapErr(err)
		}

		// 2. A copy of the parent starts it. Its history, form, mentions,
		// pins, bookmarks and reports stay with the original.
		via := model.Via{}
		if p.Via != nil {
			via = *p.Via
		}
		if err := exec(`
			INSERT INTO messages (id, conversation_id, seq, author_id, text, entities, cards, fallback, via_kind, via_name,
				via_account_id, via_icon_file_id, quiet, notified_anyway, version, created_at, edited_at, deleted_at, deleted_by, action, chance)
			VALUES (?, ?, 1, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?, ?, ?, ?, ?)`,
			copyID, D, string(p.AuthorID), p.Text, jsonList(p.Entities), jsonList(mv.CopyCards), p.Fallback, id(via.Kind),
			id(via.Name), id(via.AccountID), id(via.IconFileID), p.Quiet, false, int64(p.CreatedAt), ms2(p.EditedAt),
			ms2(p.DeletedAt), id(p.DeletedByID), p.Action, chanceArg(p.Chance)); err != nil {
			return err
		}
		for i := range mv.CopyFiles {
			f := mv.CopyFiles[i]
			f.ConversationID, f.MessageID = d.ID, mv.CopyID
			if err := (files{q}).Insert(ctx, &f); err != nil {
				return err
			}
		}
		if err := exec(`
			INSERT INTO checklist_items (message_id, item_id, position, text, checked, checked_by, checked_at)
			SELECT ?, item_id, position, text, checked, checked_by, checked_at FROM checklist_items
			WHERE message_id = ? AND removed_at IS NULL`, copyID, P); err != nil {
			return err
		}
		if err := exec(`INSERT INTO reactions (message_id, account_id, emoji, at)
			SELECT ?, account_id, emoji, at FROM reactions WHERE message_id = ?`, copyID, P); err != nil {
			return err
		}
		if err := q.ftsPut(ctx, mv.CopyID); err != nil {
			return err
		}

		// 3. The replies move, keeping everything that hangs from them by
		// their identifier.
		for i, r := range replies {
			if err := exec(`UPDATE messages SET conversation_id = ?, seq = ?, parent_id = NULL WHERE id = ?`,
				D, int64(i+2), string(r.id)); err != nil {
				return err
			}
			out.Moved = append(out.Moved, r.id)
		}
		// From here every message in the new channel but the copy is a
		// moved reply.
		const moved = `(SELECT id FROM messages WHERE conversation_id = ? AND id <> ?)`
		for _, step := range []struct {
			stmt string
			args []any
		}{
			// A reply that quoted the parent now quotes the copy, so the
			// quote can be drawn by people who cannot read the old channel.
			{`UPDATE messages SET quote_message_id = ? WHERE conversation_id = ? AND quote_message_id = ?`, []any{copyID, D, P}},
			// 4. The copies of the conversation.
			{`UPDATE files SET conversation_id = ? WHERE message_id IN ` + moved, []any{D, D, copyID}},
			{`UPDATE reports SET conversation_id = ? WHERE message_id IN ` + moved, []any{D, D, copyID}},
			{`UPDATE mentions SET conversation_id = ?, parent_id = NULL,
				seq = (SELECT m.seq FROM messages m WHERE m.id = mentions.message_id)
				WHERE message_id IN ` + moved, []any{D, D, copyID}},
			// 7. What no longer applies in the old channel.
			{`DELETE FROM pins WHERE conversation_id = ? AND message_id IN ` + moved, []any{C, D, copyID}},
			{`DELETE FROM events WHERE conversation_id = ? AND message_id IN ` + moved, []any{C, D, copyID}},
			{`DELETE FROM thread_states WHERE parent_id = ?`, []any{P}},
		} {
			if err := exec(step.stmt, step.args...); err != nil {
				return err
			}
		}
		for _, r := range replies {
			if err := q.ftsPut(ctx, r.id); err != nil {
				return err
			}
		}

		// 5. The pointer, a system reply under the parent. The thread is
		// closed from here on.
		pointer := model.Message{ID: model.NewID[model.MessageID](), ConversationID: old, Seq: mv.PointerSeq, AuthorID: mv.By,
			ParentID: p.ID, Via: &model.Via{Kind: "system"}, CreatedAt: mv.At,
			System: map[string]any{"kind": "thread_moved", "conversation_id": string(d.ID)}}
		if err := (messages{q}).Insert(ctx, &pointer, nil); err != nil {
			return err
		}

		// 8. The new channel's counters.
		last := replies[len(replies)-1]
		if err := exec(`UPDATE conversations SET next_seq = ?, last_message_at = ? WHERE id = ?`,
			int64(len(replies)+2), int64(last.createdAt), D); err != nil {
			return err
		}

		// 6. Its first members: the followers, at their level but no higher
		// than member, and the mover as manager if they were in the old
		// channel. Whoever is to be manager goes in first, so the
		// last-manager rule has nothing to do.
		type add struct {
			account model.AccountID
			level   model.Level
			readSeq int64
		}
		newSeq := func(oldRead int64) int64 {
			n := sort.Search(len(replies), func(i int) bool { return replies[i].oldSeq > oldRead })
			return int64(n + 1) // 1 is the copy: nothing of the thread read yet
		}
		var adds []add
		seen := map[model.AccountID]bool{}
		put := func(a add, first bool) {
			if seen[a.account] {
				return
			}
			seen[a.account] = true
			if first {
				adds = append([]add{a}, adds...)
			} else {
				adds = append(adds, a)
			}
		}
		for _, f := range followers {
			lvl := f.level
			if lvl == model.LevelManager {
				lvl = model.LevelMember
			}
			put(add{f.account, lvl, newSeq(f.readSeq)}, false)
		}
		promote := func(account model.AccountID) (bool, error) {
			m, found, err := q.memberRow(ctx, old, account)
			if err != nil || !found {
				return false, err
			}
			for i := range adds {
				if adds[i].account == account {
					a := adds[i]
					a.level = model.LevelManager
					adds = append(adds[:i], adds[i+1:]...)
					delete(seen, account)
					put(a, true)
					return true, nil
				}
			}
			put(add{account, model.LevelManager, newSeq(m.ReadSeq)}, true)
			return true, nil
		}
		hasManager, err := promote(mv.By)
		if err != nil {
			return err
		}
		if !hasManager {
			// An admin moved it from outside: the thread's starter, or
			// failing that the longest-standing of those added.
			if hasManager, err = promote(p.AuthorID); err != nil {
				return err
			}
		}
		if !hasManager && len(adds) > 0 {
			adds[0].level = model.LevelManager
		}
		for _, a := range adds {
			res, err := (members{q}).AddDirect(ctx, d.ID, a.account, a.level, mv.At)
			if err != nil {
				return err
			}
			// Those who come along could read the thread where it was, and
			// read all of it here (ruling R87).
			if err := exec(`UPDATE members SET mode = 'mentions', read_seq = ?, visible_from = 0 WHERE conversation_id = ? AND account_id = ?`,
				a.readSeq, D, string(a.account)); err != nil {
				return err
			}
			if err := exec(`UPDATE first_joins SET at = 0 WHERE conversation_id = ? AND account_id = ?`, D, string(a.account)); err != nil {
				return err
			}
			q.touchMember(d.ID, a.account)
			out.Members.Merge(res)
		}
		for i, ch := range out.Members.Changes {
			if m, found, err := q.memberRow(ctx, d.ID, ch.Member.AccountID); err != nil {
				return err
			} else if found {
				out.Members.Changes[i].Member = m
			}
		}
		// Mentions on the moved replies count only for people who came
		// along.
		if err := exec(`DELETE FROM mentions WHERE conversation_id = ?
			AND account_id NOT IN (SELECT account_id FROM members WHERE conversation_id = ?)`, D, D); err != nil {
			return err
		}

		if out.Channel, err = scanConversation(q.wr.QueryRowContext(ctx,
			`SELECT `+conversationCols+` FROM conversations c WHERE c.id = ?`, D)); err != nil {
			return q.mapErr(err)
		}
		if out.Copy, err = q.message(ctx, q.wr, mv.CopyID, false); err != nil {
			return err
		}
		if out.Parent, err = q.message(ctx, q.wr, p.ID, false); err != nil {
			return err
		}
		out.Pointer, err = q.message(ctx, q.wr, pointer.ID, false)
		return err
	})
	return out, err
}
