package sqlstore

import (
	"context"
	"time"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

type janitor struct{ *queries }

// Janitor returns what the janitor needs across tables.
func (q *queries) Janitor() store.Janitor { return janitor{q} }

// ownPeriod is true for a conversation that has a retention period of its
// own: a named channel with retention_seconds set. Everything else follows
// the server's (docs/FEATURES.md section 6).
const ownPeriod = `(c.kind = 'channel' AND c.name IS NOT NULL AND c.retention_seconds IS NOT NULL)`

func (j janitor) ExpiredMessages(ctx context.Context, serverSeconds int64, now model.Time, limit int) ([]model.MessageID, error) {
	// A channel's own period of 0 means "keep for ever".
	cond := `(` + ownPeriod + ` AND c.retention_seconds > 0 AND m.created_at < ? - c.retention_seconds * 1000)`
	args := []any{int64(now)}
	if serverSeconds > 0 {
		cond = `(` + cond + ` OR (NOT ` + ownPeriod + ` AND m.created_at < ?))`
		args = append(args, int64(now)-serverSeconds*1000)
	}
	args = append(args, store.Page{Limit: limit}.Clamp())
	// The library holds no messages, and is left out for good measure.
	return collectIDs[model.MessageID](j.rd.QueryContext(ctx, `
		SELECT m.id FROM messages m JOIN conversations c ON c.id = m.conversation_id
		WHERE `+cond+` AND (c.member_key IS NULL OR c.member_key <> '`+model.LibraryMemberKey+`')
			AND NOT (m.deleted_at IS NOT NULL AND m.purged_at IS NOT NULL
				AND EXISTS (SELECT 1 FROM messages r WHERE r.parent_id = m.id))
		ORDER BY CASE WHEN m.parent_id IS NULL THEN 1 ELSE 0 END, m.created_at, m.id LIMIT ?`, args...))
}

func (j janitor) ExpireMessage(ctx context.Context, msg model.MessageID, now model.Time) ([]string, bool, error) {
	var keys []string
	deleted := false
	err := j.atomic(ctx, func(q *queries) error {
		m, err := q.message(ctx, q.wr, msg, true)
		if err != nil {
			return err
		}
		a := string(msg)
		exec := func(stmt string, args ...any) error {
			_, err := q.wr.ExecContext(ctx, stmt, args...)
			return q.mapErr(err)
		}
		// The foreign key only sets files.message_id to null, so the rows
		// go here, before the message.
		if keys, err = q.deleteFiles(ctx, "message_id = ?", a); err != nil {
			return err
		}
		loose, err := q.deleteLooseCardFiles(ctx, m)
		if err != nil {
			return err
		}
		keys = append(keys, loose...)
		if err := exec(`DELETE FROM events WHERE message_id = ?`, a); err != nil {
			return err
		}
		if err := q.ftsDelete(ctx, msg); err != nil {
			return err
		}
		if err := exec(`DELETE FROM calls WHERE message_id = ?`, a); err != nil {
			return err
		}
		var replies int
		if err := q.wr.QueryRowContext(ctx, `SELECT count(*) FROM messages WHERE parent_id = ?`, a).Scan(&replies); err != nil {
			return err
		}
		if replies > 0 {
			// Younger replies still need a parent: a bare record stays
			// until they have gone too.
			for _, stmt := range []string{
				`DELETE FROM message_versions WHERE message_id = ?`,
				`DELETE FROM checklist_items WHERE message_id = ?`,
				`DELETE FROM forms WHERE message_id = ?`,
				`DELETE FROM reactions WHERE message_id = ?`,
				`DELETE FROM mentions WHERE message_id = ?`,
				`DELETE FROM pins WHERE message_id = ?`,
				`DELETE FROM bookmarks WHERE message_id = ?`,
			} {
				if err := exec(stmt, a); err != nil {
					return err
				}
			}
			deletedAt := m.DeletedAt
			if deletedAt.IsZero() {
				deletedAt = now
			}
			return exec(`
				UPDATE messages SET text = '', entities = '[]', cards = '[]', fallback = '', search_text = '', ciphertext = NULL,
					deleted_at = ?, deleted_by = NULL, purged_at = ?, history_hidden_at = NULL, client_id = NULL,
					quote_message_id = NULL, quote_start = NULL, quote_end = NULL WHERE id = ?`, int64(deletedAt), int64(now), a)
		}
		if err := exec(`DELETE FROM messages WHERE id = ?`, a); err != nil {
			return err
		}
		deleted = true
		return q.refreshThread(ctx, m.ParentID)
	})
	return keys, deleted, err
}

func (j janitor) Tidy(ctx context.Context, now model.Time) (map[string]int64, error) {
	ago := func(d time.Duration) int64 { return int64(now.Add(-d)) }
	const day = 24 * time.Hour
	month, month35 := ago(30*day), ago(35*day)
	out := map[string]int64{}
	steps := []struct {
		table string
		stmt  string
		args  []any
	}{
		// 30 days after expiry or revocation.
		{"sessions", `DELETE FROM sessions WHERE (revoked_at IS NOT NULL AND revoked_at < ?)
			OR (expires_at IS NOT NULL AND expires_at < ?) OR (max_expires_at IS NOT NULL AND max_expires_at < ?)`,
			[]any{month, month, month}},
		// One hour after expiry or use.
		{"oauth_codes", `DELETE FROM oauth_codes WHERE expires_at < ? OR (used_at IS NOT NULL AND used_at < ?)`,
			[]any{ago(time.Hour), ago(time.Hour)}},
		// One day after expiry or use.
		{"recovery_links", `DELETE FROM recovery_links WHERE expires_at < ? OR (used_at IS NOT NULL AND used_at < ?)`,
			[]any{ago(day), ago(day)}},
		// 30 days after expiry or revocation, or after being used up. The
		// table keeps no time of last use, so a used-up invite goes 30
		// days after it was made.
		{"invites", `DELETE FROM invites WHERE (expires_at IS NOT NULL AND expires_at < ?)
			OR (revoked_at IS NOT NULL AND revoked_at < ?) OR (max_uses > 0 AND uses >= max_uses AND created_at < ?)`,
			[]any{month, month, month}},
		// A bot's dialog, one hour after it was made.
		{"forms", `DELETE FROM forms WHERE message_id IS NULL AND created_at < ?`, []any{ago(time.Hour)}},
		// 30 days after ending.
		{"timeouts", `DELETE FROM timeouts WHERE until < ? OR (lifted_at IS NOT NULL AND lifted_at < ?)`, []any{month, month}},
		{"scheduled", `DELETE FROM scheduled WHERE (cancelled_at IS NOT NULL AND cancelled_at < ?)
			OR (next_run_at IS NULL AND cancelled_at IS NULL AND COALESCE(last_run_at, failed_at, created_at) < ?)`,
			[]any{month, month}},
		{"unlock_requests", `DELETE FROM unlock_requests WHERE expires_at < ?`, []any{ago(day)}},
		{"stream_log", `DELETE FROM stream_log WHERE at < ?`, []any{month}},
		{"stream_seen", `DELETE FROM stream_seen WHERE at < ?`, []any{month35}},
		// Not a deletion: a client id only matters for a day.
		{"messages.client_id", `UPDATE messages SET client_id = NULL WHERE client_id IS NOT NULL AND created_at < ?`, []any{ago(day)}},
	}
	for _, s := range steps {
		// Each by itself: one table's rows are never held up by another's.
		n, err := j.affected(j.wr.ExecContext(ctx, s.stmt, s.args...))
		if err != nil {
			return out, err
		}
		if n > 0 {
			out[s.table] = n
		}
	}
	return out, nil
}
