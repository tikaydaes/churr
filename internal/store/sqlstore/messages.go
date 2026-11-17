package sqlstore

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/tikaydaes/churr/internal/markdown"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

type messages struct{ *queries }

// Messages returns the messages area.
func (q *queries) Messages() store.Messages { return messages{q} }

// messageCols lists the columns of a messages row with the alias m.
const messageCols = `m.id, m.conversation_id, m.seq, m.author_id, m.parent_id, m.quote_message_id, m.quote_start, m.quote_end,
	m.client_id, m.text, m.entities, m.cards, m.fallback, m.system, m.via_kind, m.via_name, m.via_account_id, m.via_icon_file_id,
	m.quiet, m.ciphertext, m.key_epoch, m.notified_anyway, m.version, m.created_at, m.edited_at, m.deleted_at, m.deleted_by,
	m.history_hidden_at, m.purged_at, m.reply_count, m.last_reply_seq, m.last_reply_at, m.search_text, m.ord, m.late, m.edit_ord, m.last_reply_ord, m.action, m.chance`

func scanMessage(s scanner) (model.Message, error) {
	var (
		m                    model.Message
		quoteID              model.MessageID
		quoteStart, quoteEnd *int64
		via                  model.Via
		ciphertext           []byte
		keyEpoch             *int64
	)
	err := s.Scan(str(&m.ID), str(&m.ConversationID), num(&m.Seq), str(&m.AuthorID), str(&m.ParentID), str(&quoteID),
		numPtr(&quoteStart), numPtr(&quoteEnd), str(&m.ClientID), &m.Text, js(&m.Entities), js(&m.Cards), &m.Fallback,
		js(&m.System), str(&via.Kind), str(&via.Name), str(&via.AccountID), str(&via.IconFileID), flag(&m.Quiet),
		blob(&ciphertext), numPtr(&keyEpoch), flag(&m.NotifiedAnyway), &m.Version, tm(&m.CreatedAt), tm(&m.EditedAt),
		tm(&m.DeletedAt), str(&m.DeletedByID), tm(&m.HistoryHiddenAt), tm(&m.PurgedAt), &m.ReplyCount,
		num(&m.LastReplySeq), tm(&m.LastReplyAt), &m.SearchText, &m.Order, flag(&m.Late), str(&m.EditOrder), str(&m.LastReplyOrder), flag(&m.Action), js(&m.Chance))
	if err != nil {
		return m, err
	}
	if m.Order == "" {
		// Never stamped and never in a conversation that was shared: by seq.
		m.Order = model.LocalOrder(0, m.Seq)
	}
	if m.LastReplyOrder == "" && m.LastReplySeq > 0 {
		// The newest reply is ordered by its seq, as its own row reads.
		m.LastReplyOrder = model.LocalOrder(0, m.LastReplySeq)
	}
	if quoteID != "" {
		m.Quote = &model.Quote{MessageID: quoteID}
		if quoteStart != nil && quoteEnd != nil {
			a, b := int(*quoteStart), int(*quoteEnd)
			m.Quote.Start, m.Quote.End = &a, &b
		}
	}
	if via.Kind != "" {
		m.Via = &via
	}
	if ciphertext != nil {
		m.Sealed = &model.SealedPayload{Ciphertext: ciphertext}
		if keyEpoch != nil {
			m.Sealed.KeyEpoch = *keyEpoch
		}
	}
	if len(m.Entities) == 0 {
		m.Entities = nil
	}
	if len(m.Cards) == 0 {
		m.Cards = nil
	}
	return m, nil
}

// storedOrder is what messages.ord holds for a message about to be stored:
// its order string, or nothing when that is simply its seq in a conversation
// that was never shared, which is what an empty column is read back as. The
// message is given the string either way.
func storedOrder(m *model.Message) string {
	local := model.LocalOrder(0, m.Seq)
	if m.Order == "" || m.Order == local {
		m.Order = local
		return ""
	}
	return m.Order
}

// searchText works out what the search index holds for a message
// (docs/SCHEMA.md section 13): its plain text, with each channel link the
// author could see written as that channel's name at this moment, followed
// by the fallback. A link is stored by identifier (ruling R52), so the name
// is what can be found and the identifier is not. Sealed and deleted
// messages have none.
func (q *queries) searchText(ctx context.Context, m model.Message) (string, error) {
	if m.Deleted() || m.Sealed != nil {
		return "", nil
	}
	var kind string
	if err := q.wr.QueryRowContext(ctx, `SELECT kind FROM conversations WHERE id = ?`, string(m.ConversationID)).Scan(&kind); err != nil {
		return "", q.mapErr(err)
	}
	return q.searchTextIn(ctx, m, kind)
}

// searchTextIn is searchText for a caller that knows the kind of the
// message's conversation.
func (q *queries) searchTextIn(ctx context.Context, m model.Message, kind string) (string, error) {
	if m.Deleted() || m.Sealed != nil || kind != model.KindChannel {
		return "", nil
	}
	// Never nil: with no entities stored, no link is one the author could
	// see, and none is named.
	entities := m.Entities
	if entities == nil {
		entities = []model.Entity{}
	}
	var failed error
	plain := markdown.PlainNames(m.Text, entities, func(c model.ConversationID) (string, bool) {
		var name sql.NullString
		err := q.wr.QueryRowContext(ctx, `SELECT name FROM conversations WHERE id = ? AND kind = 'channel'`, string(c)).Scan(&name)
		if err != nil && !errors.Is(err, sql.ErrNoRows) && failed == nil {
			failed = err
		}
		return name.String, err == nil && name.Valid
	})
	return strings.TrimSpace(plain + "\n" + m.Fallback), failed
}

// ftsPut brings a message's search entry in step with its row. On both
// databases it writes messages.search_text; PostgreSQL's search column is
// generated from that. On SQLite it then replaces the entry in
// messages_fts. A message is indexed if and only if it is in a channel, is
// not deleted and has text or fallback (docs/SCHEMA.md section 13). Call
// it after anything that changes a message's text, fallback or
// conversation.
func (q *queries) ftsPut(ctx context.Context, msg model.MessageID) error {
	m, err := q.message(ctx, q.wr, msg, false)
	if err != nil {
		return err
	}
	text, err := q.searchText(ctx, m)
	if err != nil {
		return err
	}
	if text != m.SearchText {
		if _, err := q.wr.ExecContext(ctx, `UPDATE messages SET search_text = ? WHERE id = ?`, text, string(msg)); err != nil {
			return q.mapErr(err)
		}
	}
	if q.pg() {
		return nil
	}
	if err := q.ftsDelete(ctx, msg); err != nil {
		return err
	}
	// The entry takes the message's rowid, so later replaces and deletes
	// find it without scanning the index. A rowid can be left behind by
	// another message after VACUUM renumbers the table, so a stale entry
	// at that rowid is cleared first.
	if _, err := q.wr.ExecContext(ctx, `
		DELETE FROM messages_fts WHERE rowid = (SELECT rowid FROM messages WHERE id = ?) AND message_id <> ?`,
		string(msg), string(msg)); err != nil {
		return err
	}
	_, err = q.wr.ExecContext(ctx, `
		INSERT INTO messages_fts (rowid, body, message_id, conversation_id)
		SELECT m.rowid, m.search_text, m.id, m.conversation_id
		FROM messages m WHERE m.id = ? AND m.search_text <> ''`, string(msg))
	return err
}

// ftsDelete removes a message's search entry on SQLite. It must run while
// the messages row still exists, to find the entry by rowid; if that finds
// nothing (the rowids moved) it falls back to looking for the message id.
func (q *queries) ftsDelete(ctx context.Context, msg model.MessageID) error {
	if q.pg() {
		return nil
	}
	n, err := q.affected(q.wr.ExecContext(ctx, `
		DELETE FROM messages_fts WHERE rowid = (SELECT rowid FROM messages WHERE id = ?) AND message_id = ?`,
		string(msg), string(msg)))
	if err != nil || n > 0 {
		return err
	}
	_, err = q.wr.ExecContext(ctx, `DELETE FROM messages_fts WHERE message_id = ?`, string(msg))
	return err
}

// refreshThread works a parent's summary out again from its replies, so it
// can never drift: the number of replies that are not deleted, and the seq,
// time and order string of the newest of them (the one with the highest
// seq). Anything that changes a reply's seq, order or deletion calls it.
func (q *queries) refreshThread(ctx context.Context, parent model.MessageID) error {
	if parent == "" {
		return nil
	}
	const live = `FROM messages r WHERE r.parent_id = ? AND r.deleted_at IS NULL AND r.seq IS NOT NULL`
	p := string(parent)
	_, err := q.wr.ExecContext(ctx, `
		UPDATE messages SET
			reply_count = (SELECT count(*) `+live+`),
			last_reply_seq = (SELECT max(r.seq) `+live+`),
			last_reply_at = (SELECT max(r.created_at) `+live+`),
			last_reply_ord = (SELECT r.ord `+live+` ORDER BY r.seq DESC LIMIT 1)
		WHERE id = ?`, p, p, p, p, p)
	return err
}

// mentionRows bounds the rows of one INSERT INTO mentions: both databases
// limit a statement's arguments.
const mentionRows = 1000

// mentionInserts builds the statements that store a message's mentions, one
// row for each account named, many rows to a statement.
func mentionInserts(m model.Message, mentions []model.Mention) []held {
	seen := map[model.AccountID]bool{}
	var out []held
	var b strings.Builder
	var args []any
	rows := 0
	done := func() {
		if rows > 0 {
			out = append(out, held{b.String(), args})
			b.Reset()
			args, rows = nil, 0
		}
	}
	for _, mn := range mentions {
		if mn.AccountID == "" || seen[mn.AccountID] {
			continue
		}
		seen[mn.AccountID] = true
		if rows == 0 {
			b.WriteString(`INSERT INTO mentions (message_id, account_id, conversation_id, seq, parent_id, kind) VALUES `)
		} else {
			b.WriteString(", ")
		}
		b.WriteString("(?, ?, ?, ?, ?, ?)")
		args = append(args, string(m.ID), string(mn.AccountID), string(m.ConversationID), m.Seq, id(m.ParentID), mn.Kind)
		if rows++; rows == mentionRows {
			done()
		}
	}
	done()
	return out
}

func (q *queries) insertMentions(ctx context.Context, m model.Message, mentions []model.Mention) error {
	for _, st := range mentionInserts(m, mentions) {
		if _, err := q.wr.ExecContext(ctx, st.sql, st.args...); err != nil {
			return q.mapErr(err)
		}
	}
	return nil
}

func (ms messages) Insert(ctx context.Context, m *model.Message, mentions []model.Mention) error {
	if m.Seq <= 0 {
		return errors.New("sqlstore: a message needs the seq taken with NextSeq")
	}
	if m.ID == "" {
		m.ID = model.NewID[model.MessageID]()
	}
	if m.CreatedAt.IsZero() {
		m.CreatedAt = model.Now()
	}
	m.Version = 1
	return ms.atomic(ctx, func(q *queries) error {
		// A client_id used twice by one author in one conversation is
		// refused by the unique index messages_client, and comes back as
		// model.ErrAlreadyExists; nothing is asked first.
		var quoteID model.MessageID
		var quoteStart, quoteEnd any
		if m.Quote != nil {
			quoteID = m.Quote.MessageID
			if m.Quote.Start != nil && m.Quote.End != nil {
				quoteStart, quoteEnd = *m.Quote.Start, *m.Quote.End
			}
		}
		via := model.Via{}
		if m.Via != nil {
			via = *m.Via
		}
		var system any
		if m.System != nil {
			system = jsonArg(m.System)
		}
		var chance any
		if m.Chance != nil {
			chance = jsonArg(m.Chance)
		}
		var ciphertext, keyEpoch any
		if m.Sealed != nil {
			ciphertext, keyEpoch = m.Sealed.Ciphertext, m.Sealed.KeyEpoch
		}
		// The conversation's row says, for a message no stamp places,
		// whether the conversation was shared once, in which case the
		// message is ordered after everything from that time; and its
		// kind says whether the message is searched. The statement that
		// took the seq in this transaction has usually read both and set
		// last_message_at (Streams.TakeMessageSeq). If not, or if it set
		// another time, the row is touched here.
		taken, known := q.seqTaken(m.ConversationID)
		if !known || taken.lastAt != m.CreatedAt {
			err := q.wr.QueryRowContext(ctx, `UPDATE conversations SET last_message_at = ? WHERE id = ? RETURNING order_floor, kind`,
				int64(m.CreatedAt), string(m.ConversationID)).Scan(&taken.floor, &taken.kind)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return q.mapErr(err)
			}
		}
		if m.Order == "" && taken.floor > 0 {
			m.Order = model.LocalOrder(taken.floor, m.Seq)
		}
		// What search holds for it is worked out first and stored with
		// the row (docs/SCHEMA.md section 13).
		search, err := q.searchTextIn(ctx, *m, taken.kind)
		if err != nil {
			return err
		}
		insert := held{`
			INSERT INTO messages (id, conversation_id, seq, author_id, parent_id, quote_message_id, quote_start, quote_end,
				client_id, text, entities, cards, fallback, system, via_kind, via_name, via_account_id, via_icon_file_id,
				quiet, ciphertext, key_epoch, notified_anyway, version, created_at, ord, late, action, chance, search_text)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?, ?, ?, ?, ?)`, []any{
			string(m.ID), string(m.ConversationID), m.Seq, string(m.AuthorID), id(m.ParentID), id(quoteID), quoteStart, quoteEnd,
			id(m.ClientID), m.Text, jsonList(m.Entities), jsonList(m.Cards), m.Fallback, system, id(via.Kind), id(via.Name),
			id(via.AccountID), id(via.IconFileID), m.Quiet, ciphertext, keyEpoch, m.NotifiedAnyway, int64(m.CreatedAt),
			storedOrder(m), m.Late, m.Action, chance, search}}
		// On PostgreSQL the message and its mentions wait for the event
		// that announces them, and go as one statement with it (kept.go).
		// Anything sent before that sends them first.
		for _, st := range append([]held{insert}, mentionInserts(*m, mentions)...) {
			if q.hold(st.sql, st.args...) {
				continue
			}
			if _, err := q.wr.ExecContext(ctx, st.sql, st.args...); err != nil {
				return q.mapErr(err)
			}
		}
		if !q.pg() && search != "" {
			// SQLite's index: the entry takes the message's rowid. A
			// rowid can be left behind by another message after VACUUM
			// renumbers the table, so a stale entry there is cleared
			// first (ftsPut).
			if _, err := q.wr.ExecContext(ctx, `
				DELETE FROM messages_fts WHERE rowid = (SELECT rowid FROM messages WHERE id = ?) AND message_id <> ?`,
				string(m.ID), string(m.ID)); err != nil {
				return err
			}
			if _, err := q.wr.ExecContext(ctx, `
				INSERT INTO messages_fts (rowid, body, message_id, conversation_id)
				SELECT m.rowid, m.search_text, m.id, m.conversation_id
				FROM messages m WHERE m.id = ?`, string(m.ID)); err != nil {
				return err
			}
		}
		return q.refreshThread(ctx, m.ParentID)
	})
}

func (q *queries) message(ctx context.Context, ex execer, msg model.MessageID, lock bool) (model.Message, error) {
	suffix := ""
	if lock {
		suffix = q.forUpdate()
	}
	m, err := scanMessage(ex.QueryRowContext(ctx, `SELECT `+messageCols+` FROM messages m WHERE m.id = ?`+suffix, string(msg)))
	return m, q.mapErr(err)
}

func (ms messages) Get(ctx context.Context, msg model.MessageID) (model.Message, error) {
	return ms.message(ctx, ms.rd, msg, false)
}

func (ms messages) GetMany(ctx context.Context, ids []model.MessageID) ([]model.Message, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := ms.rd.QueryContext(ctx, `SELECT `+messageCols+` FROM messages m WHERE m.id IN (`+marks(len(ids))+`)
		ORDER BY m.conversation_id, m.seq`, anys(ids)...)
	return collect(rows, err, scanMessage)
}

func (ms messages) ByClientID(ctx context.Context, conv model.ConversationID, author model.AccountID, clientID string) (model.Message, bool, error) {
	if clientID == "" {
		return model.Message{}, false, nil
	}
	m, err := scanMessage(ms.rd.QueryRowContext(ctx, `SELECT `+messageCols+` FROM messages m
		WHERE m.conversation_id = ? AND m.author_id = ? AND m.client_id = ?`, string(conv), string(author), clientID))
	if errors.Is(err, sql.ErrNoRows) {
		return model.Message{}, false, nil
	}
	return m, err == nil, err
}

// page runs one direction of a seq-ordered listing: up to limit+1 rows
// below `before` (newest first) or above `after` (oldest first), and
// returns them in ascending order with whether there were more.
func (q *queries) page(ctx context.Context, where string, args []any, before, after int64, limit int) ([]model.Message, bool, error) {
	order := "DESC"
	switch {
	case after > 0:
		where += " AND m.seq > ?"
		args = append(args, after)
		order = "ASC"
		if before > 0 {
			where += " AND m.seq < ?"
			args = append(args, before)
		}
	case before > 0:
		where += " AND m.seq < ?"
		args = append(args, before)
	}
	args = append(args, limit+1)
	rows, err := q.rd.QueryContext(ctx, `SELECT `+messageCols+` FROM messages m WHERE `+where+
		` AND m.seq IS NOT NULL ORDER BY m.seq `+order+` LIMIT ?`, args...)
	out, err := collect(rows, err, scanMessage)
	if err != nil {
		return nil, false, err
	}
	more := len(out) > limit
	if more {
		out = out[:limit]
	}
	if order == "DESC" {
		for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
			out[i], out[j] = out[j], out[i]
		}
	}
	return out, more, nil
}

func (ms messages) List(ctx context.Context, conv model.ConversationID, o store.MessageList) ([]model.Message, bool, error) {
	where := "m.conversation_id = ?"
	args := []any{string(conv)}
	if o.From > 0 {
		where += " AND m.created_at >= ?"
		args = append(args, int64(o.From))
	}
	if o.TopLevel {
		where += " AND m.parent_id IS NULL"
	}
	limit := o.Clamp()
	if o.Around > 0 {
		// Half the page at or below the seq asked for, the rest above.
		half := limit/2 + 1
		older, moreOlder, err := ms.page(ctx, where, append([]any(nil), args...), o.Around+1, 0, half)
		if err != nil {
			return nil, false, err
		}
		newer, moreNewer, err := ms.page(ctx, where, append([]any(nil), args...), 0, o.Around, limit-len(older))
		if err != nil {
			return nil, false, err
		}
		return append(older, newer...), moreOlder || moreNewer, nil
	}
	return ms.page(ctx, where, args, o.Before, o.After, limit)
}

func (ms messages) Replies(ctx context.Context, parent model.MessageID, p store.SeqPage) ([]model.Message, bool, error) {
	where, args := "m.parent_id = ?", []any{string(parent)}
	if p.From > 0 {
		where += " AND m.created_at >= ?"
		args = append(args, int64(p.From))
	}
	return ms.page(ctx, where, args, p.Before, p.After, p.Clamp())
}

// attachedFileIDs returns the ids of the files a message carries now.
func (q *queries) attachedFileIDs(ctx context.Context, msg model.MessageID) ([]model.FileID, error) {
	return collectIDs[model.FileID](q.wr.QueryContext(ctx,
		`SELECT id FROM files WHERE message_id = ? AND detached_at IS NULL AND purpose = 'attachment' ORDER BY position, id`, string(msg)))
}

// keepVersion copies a message's present content to message_versions.
func (q *queries) keepVersion(ctx context.Context, m model.Message, by model.AccountID, at model.Time, reason string) error {
	fileIDs, err := q.attachedFileIDs(ctx, m.ID)
	if err != nil {
		return err
	}
	_, err = q.wr.ExecContext(ctx, `
		INSERT INTO message_versions (message_id, version, text, entities, cards, file_ids, replaced_at, replaced_by, reason)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		string(m.ID), m.Version, m.Text, jsonList(m.Entities), jsonList(m.Cards), jsonList(fileIDs), int64(at), string(by), reason)
	return q.mapErr(err)
}

func (ms messages) Edit(ctx context.Context, e store.MessageEdit) (model.Message, error) {
	var out model.Message
	err := ms.atomic(ctx, func(q *queries) error {
		m, err := q.message(ctx, q.wr, e.ID, true)
		if err != nil {
			return err
		}
		if m.Deleted() {
			return model.ErrConflict.With("A deleted message cannot be edited.")
		}
		if e.KeepVersion {
			if err := q.keepVersion(ctx, m, e.By, e.At, "edit"); err != nil {
				return err
			}
			m.Version++
			m.EditedAt = e.At
		}
		m.Text, m.Entities, m.Cards, m.Fallback = e.Text, e.Entities, e.Cards, e.Fallback
		_, err = q.wr.ExecContext(ctx, `
			UPDATE messages SET text = ?, entities = ?, cards = ?, fallback = ?, version = ?, edited_at = ? WHERE id = ?`,
			m.Text, jsonList(m.Entities), jsonList(m.Cards), m.Fallback, m.Version, ms2(m.EditedAt), string(m.ID))
		if err != nil {
			return q.mapErr(err)
		}
		if _, err := q.wr.ExecContext(ctx, `DELETE FROM mentions WHERE message_id = ?`, string(m.ID)); err != nil {
			return err
		}
		if err := q.insertMentions(ctx, m, e.Mentions); err != nil {
			return err
		}
		// Nobody is told of an edit to a message posted before their first
		// join in a private channel (ruling R87).
		if _, err := q.wr.ExecContext(ctx, `
			DELETE FROM mentions WHERE message_id = ? AND account_id IN (
				SELECT account_id FROM members WHERE conversation_id = ? AND visible_from > ?)`,
			string(m.ID), string(m.ConversationID), int64(m.CreatedAt)); err != nil {
			return err
		}
		out = m
		return q.ftsPut(ctx, m.ID)
	})
	return out, err
}

// ms2 is ms under a name that does not collide with a receiver called ms.
func ms2(t model.Time) any { return ms(t) }

func (ms messages) Delete(ctx context.Context, msg model.MessageID, by model.AccountID, at model.Time) (model.Message, error) {
	var out model.Message
	err := ms.atomic(ctx, func(q *queries) error {
		m, err := q.message(ctx, q.wr, msg, true)
		if err != nil {
			return err
		}
		if m.Deleted() {
			return model.ErrConflict.With("That message is already deleted.")
		}
		// What it held stays in its history until purged.
		if err := q.keepVersion(ctx, m, by, at, "delete"); err != nil {
			return err
		}
		if err := q.ftsDelete(ctx, msg); err != nil {
			return err
		}
		a := string(msg)
		for _, step := range []struct {
			stmt string
			args []any
		}{
			{`UPDATE messages SET text = '', entities = '[]', cards = '[]', fallback = '', search_text = '', chance = NULL, deleted_at = ?, deleted_by = ? WHERE id = ?`,
				[]any{int64(at), id(by), a}},
			{`UPDATE files SET detached_at = ? WHERE message_id = ? AND detached_at IS NULL`, []any{int64(at), a}},
			{`DELETE FROM reactions WHERE message_id = ?`, []any{a}},
			{`DELETE FROM mentions WHERE message_id = ?`, []any{a}},
			{`DELETE FROM pins WHERE message_id = ?`, []any{a}},
			{`UPDATE forms SET closed_at = ?, closed_by = ? WHERE message_id = ? AND closed_at IS NULL`, []any{int64(at), id(by), a}},
		} {
			if _, err := q.wr.ExecContext(ctx, step.stmt, step.args...); err != nil {
				return q.mapErr(err)
			}
		}
		m.Text, m.Entities, m.Cards, m.Fallback = "", nil, nil, ""
		m.DeletedAt, m.DeletedByID = at, by
		out = m
		return q.refreshThread(ctx, m.ParentID)
	})
	return out, err
}

func (ms messages) DeleteOutright(ctx context.Context, msg model.MessageID) ([]string, error) {
	var keys []string
	err := ms.atomic(ctx, func(q *queries) error {
		m, err := q.message(ctx, q.wr, msg, true)
		if err != nil {
			return err
		}
		if err := q.ftsDelete(ctx, msg); err != nil {
			return err
		}
		// Replies go with the row by cascade; their search entries and
		// events must go too.
		replies, err := collectIDs[model.MessageID](q.wr.QueryContext(ctx, `SELECT id FROM messages WHERE parent_id = ?`, string(msg)))
		if err != nil {
			return err
		}
		for _, r := range append(replies, msg) {
			if rm, err := q.message(ctx, q.wr, r, false); err == nil {
				loose, err := q.deleteLooseCardFiles(ctx, rm)
				if err != nil {
					return err
				}
				keys = append(keys, loose...)
			}
			if err := q.ftsDelete(ctx, r); err != nil {
				return err
			}
			k, err := q.deleteFiles(ctx, "message_id = ?", string(r))
			if err != nil {
				return err
			}
			keys = append(keys, k...)
			if _, err := q.wr.ExecContext(ctx, `DELETE FROM events WHERE message_id = ?`, string(r)); err != nil {
				return err
			}
		}
		if _, err := q.wr.ExecContext(ctx, `DELETE FROM messages WHERE id = ?`, string(msg)); err != nil {
			return q.mapErr(err)
		}
		return q.refreshThread(ctx, m.ParentID)
	})
	return keys, err
}

func (ms messages) Purge(ctx context.Context, msg model.MessageID, at model.Time) ([]string, error) {
	var keys []string
	err := ms.atomic(ctx, func(q *queries) error {
		m, err := q.message(ctx, q.wr, msg, true)
		if err != nil {
			return err
		}
		a := string(msg)
		exec := func(stmt string, args ...any) error {
			_, err := q.wr.ExecContext(ctx, stmt, args...)
			return q.mapErr(err)
		}
		var loose []string
		if m.Deleted() {
			// The cards went into its history when it was deleted, so
			// their images are found there, before the history goes.
			if loose, err = q.deleteLooseCardFiles(ctx, m); err != nil {
				return err
			}
		}
		if err := exec(`DELETE FROM message_versions WHERE message_id = ?`, a); err != nil {
			return err
		}
		if err := exec(`DELETE FROM checklist_items WHERE message_id = ? AND removed_at IS NOT NULL`, a); err != nil {
			return err
		}
		if !m.Deleted() {
			// The message stays as it now reads. Its earlier versions
			// travelled in its message.edited events, so those go; the
			// message.created event is left, and a client that replays
			// it then fetches the message as it is.
			k, err := q.deleteFiles(ctx, "message_id = ? AND detached_at IS NOT NULL", a)
			if err != nil {
				return err
			}
			keys = k
			if err := exec(`DELETE FROM events WHERE message_id = ? AND type = 'message.edited'`, a); err != nil {
				return err
			}
			return exec(`UPDATE messages SET purged_at = ?, history_hidden_at = NULL WHERE id = ?`, int64(at), a)
		}
		// A deleted message becomes a bare record: who, where, when.
		k, err := q.deleteFiles(ctx, "message_id = ?", a)
		if err != nil {
			return err
		}
		keys = append(k, loose...)
		for _, stmt := range []string{
			`DELETE FROM checklist_items WHERE message_id = ?`,
			`DELETE FROM forms WHERE message_id = ?`,
			`DELETE FROM mentions WHERE message_id = ?`,
			`DELETE FROM pins WHERE message_id = ?`,
			`DELETE FROM bookmarks WHERE message_id = ?`,
			`DELETE FROM events WHERE message_id = ? AND type <> 'message.deleted'`,
		} {
			if err := exec(stmt, a); err != nil {
				return err
			}
		}
		return exec(`
			UPDATE messages SET purged_at = ?, history_hidden_at = NULL, client_id = NULL, quote_message_id = NULL,
				quote_start = NULL, quote_end = NULL WHERE id = ?`, int64(at), a)
	})
	return keys, err
}

func (ms messages) Versions(ctx context.Context, msg model.MessageID) ([]model.MessageVersion, error) {
	rows, err := ms.rd.QueryContext(ctx, `
		SELECT version, text, entities, cards, file_ids, replaced_at, replaced_by, reason
		FROM message_versions WHERE message_id = ? ORDER BY version, replaced_at`, string(msg))
	return collect(rows, err, func(s scanner) (model.MessageVersion, error) {
		v := model.MessageVersion{Entities: []model.Entity{}, Cards: []model.Card{}, Files: []model.File{}}
		err := s.Scan(&v.Version, &v.Text, js(&v.Entities), js(&v.Cards), js(&v.FileIDs), tm(&v.ReplacedAt), str(&v.ReplacedByID), &v.Reason)
		return v, err
	})
}

func (ms messages) Mentions(ctx context.Context, msg model.MessageID) ([]model.Mention, error) {
	rows, err := ms.rd.QueryContext(ctx, `SELECT message_id, account_id, kind FROM mentions WHERE message_id = ? ORDER BY account_id`, string(msg))
	return collect(rows, err, func(s scanner) (model.Mention, error) {
		var mn model.Mention
		err := s.Scan(str(&mn.MessageID), str(&mn.AccountID), &mn.Kind)
		return mn, err
	})
}

func (ms messages) Hydrate(ctx context.Context, msgs []model.Message, reader model.AccountID) error {
	if len(msgs) == 0 {
		return nil
	}
	var all, live, parents []model.MessageID
	at := map[model.MessageID][]int{}
	for i, m := range msgs {
		if _, dup := at[m.ID]; !dup {
			all = append(all, m.ID)
			if !m.Deleted() {
				live = append(live, m.ID)
			}
			if m.ReplyCount > 0 {
				parents = append(parents, m.ID)
			}
		}
		at[m.ID] = append(at[m.ID], i)
	}
	each := func(msg model.MessageID, fn func(m *model.Message)) {
		for _, i := range at[msg] {
			fn(&msgs[i])
		}
	}
	// Inside a transaction every read must go through it, to see what it
	// has written; outside one the read pool will do.
	ex := ms.rd
	if ms.inTx {
		ex = ms.wr
	}

	if len(live) > 0 {
		rows, err := ex.QueryContext(ctx, `
			SELECT message_id, item_id, checked, checked_by, checked_at FROM checklist_items
			WHERE message_id IN (`+marks(len(live))+`) AND removed_at IS NULL ORDER BY message_id, position, item_id`, anys(live)...)
		if err != nil {
			return err
		}
		for rows.Next() {
			var msg model.MessageID
			var t model.Task
			if err := rows.Scan(str(&msg), &t.ID, flag(&t.Checked), str(&t.CheckedByID), tm(&t.CheckedAt)); err != nil {
				rows.Close()
				return err
			}
			each(msg, func(m *model.Message) { m.Tasks = append(m.Tasks, t) })
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}

		frows, err := ex.QueryContext(ctx, `SELECT `+fileCols+` FROM files
			WHERE message_id IN (`+marks(len(live))+`) AND detached_at IS NULL AND purpose = 'attachment'`, anys(live)...)
		files, err := collect(frows, err, scanFile)
		if err != nil {
			return err
		}
		sortFiles(files)
		for _, f := range files {
			each(f.MessageID, func(m *model.Message) { m.Files = append(m.Files, f) })
		}

		reactions, err := (&queries{db: ms.db, rd: ex, wr: ex, inTx: ms.inTx}).reactionsOf(ctx, live, reader)
		if err != nil {
			return err
		}
		for msg, list := range reactions {
			each(msg, func(m *model.Message) { m.Reactions = list })
		}
	}

	if len(parents) > 0 {
		rows, err := ex.QueryContext(ctx, `
			SELECT parent_id, author_id, min(seq) FROM messages
			WHERE parent_id IN (`+marks(len(parents))+`) AND deleted_at IS NULL AND seq IS NOT NULL
			GROUP BY parent_id, author_id ORDER BY parent_id, min(seq)`, anys(parents)...)
		if err != nil {
			return err
		}
		who := map[model.MessageID][]model.AccountID{}
		for rows.Next() {
			var parent model.MessageID
			var author model.AccountID
			var first int64
			if err := rows.Scan(str(&parent), str(&author), &first); err != nil {
				rows.Close()
				return err
			}
			who[parent] = append(who[parent], author)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		for _, p := range parents {
			each(p, func(m *model.Message) {
				ids := who[p]
				if ids == nil {
					ids = []model.AccountID{}
				}
				m.Thread = &model.ThreadSummary{ReplyCount: m.ReplyCount, LastReplySeq: m.LastReplySeq, LastReplyOrder: m.LastReplyOrder,
					LastReplyAt: m.LastReplyAt, ParticipantIDs: ids}
			})
		}
	}

	for _, probe := range []struct {
		query string
		set   func(m *model.Message)
	}{
		{`SELECT DISTINCT message_id FROM pins WHERE message_id IN (`, func(m *model.Message) { m.Pinned = true }},
		{`SELECT DISTINCT message_id FROM message_versions WHERE message_id IN (`, func(m *model.Message) { m.HasHistory = true }},
	} {
		ids, err := collectIDs[model.MessageID](ex.QueryContext(ctx, probe.query+marks(len(all))+`)`, anys(all)...))
		if err != nil {
			return err
		}
		for _, msg := range ids {
			each(msg, probe.set)
		}
	}
	return nil
}
