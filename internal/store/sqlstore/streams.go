package sqlstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

type streams struct{ *queries }

// Streams returns the Streams area.
func (q *queries) Streams() store.Streams { return streams{q} }

const streamCols = `s.id, s.peer_id, s.conversation_id, s.owner_server_id, s.sequencer, s.kind, s.state, s.key_epoch, s.last_seq,
	s.first_seq, s.servers_version, s.max_blob_bytes, s.history_days, s.offered_name, s.offered_at, s.accepted_by, s.created_at,
	s.home_hub, s.max_at, s.top_ord`

func scanStream(sc scanner) (store.Stream, error) {
	var s store.Stream
	err := sc.Scan(str(&s.ID), str(&s.PeerID), str(&s.ConversationID), str(&s.OwnerServerID), &s.Sequencer, &s.Kind, &s.State,
		&s.KeyEpoch, &s.LastSeq, &s.FirstSeq, &s.ServersVersion, num(&s.MaxBlobBytes), &s.HistoryDays, str(&s.OfferedName),
		tm(&s.OfferedAt), str(&s.AcceptedBy), tm(&s.CreatedAt), str(&s.HomeHub), &s.MaxAt, &s.TopOrd)
	return s, err
}

func (st streams) Create(ctx context.Context, s *store.Stream) error {
	if s.CreatedAt.IsZero() {
		s.CreatedAt = model.Now()
	}
	return st.atomic(ctx, func(q *queries) error {
		var one int
		err := q.wr.QueryRowContext(ctx, `SELECT 1 FROM streams WHERE id = ?`, string(s.ID)).Scan(&one)
		if err == nil {
			return model.ErrAlreadyExists
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		_, err = q.wr.ExecContext(ctx, `
			INSERT INTO streams (id, peer_id, conversation_id, owner_server_id, sequencer, kind, state, key_epoch, last_seq,
				first_seq, servers_version, max_blob_bytes, history_days, offered_name, offered_at, accepted_by, created_at, home_hub)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			string(s.ID), string(s.PeerID), id(s.ConversationID), id(s.OwnerServerID), s.Sequencer, s.Kind, s.State, s.KeyEpoch,
			s.LastSeq, s.FirstSeq, s.ServersVersion, zeroNull(s.MaxBlobBytes), s.HistoryDays, id(s.OfferedName), ms(s.OfferedAt),
			id(s.AcceptedBy), int64(s.CreatedAt), id(s.HomeHub))
		return q.mapErr(err)
	})
}

func (st streams) Get(ctx context.Context, stream model.StreamID) (store.Stream, error) {
	out, err := scanStream(st.rd.QueryRowContext(ctx, `SELECT `+streamCols+` FROM streams s WHERE s.id = ?`, string(stream)))
	return out, st.mapErr(err)
}

func (st streams) ByConversation(ctx context.Context, conv model.ConversationID) (store.Stream, error) {
	out, err := scanStream(st.rd.QueryRowContext(ctx,
		`SELECT `+streamCols+` FROM streams s WHERE s.conversation_id = ? AND s.state = 'active'`, string(conv)))
	return out, st.mapErr(err)
}

func (st streams) List(ctx context.Context, state string) ([]store.Stream, error) {
	where, args := "1 = 1", []any{}
	if state != "" {
		where, args = "s.state = ?", []any{state}
	}
	rows, err := st.rd.QueryContext(ctx, `SELECT `+streamCols+` FROM streams s WHERE `+where+` ORDER BY s.created_at, s.id`, args...)
	return collect(rows, err, scanStream)
}

func (st streams) Update(ctx context.Context, stream model.StreamID, fn func(*store.Stream) error) (store.Stream, error) {
	var out store.Stream
	err := st.atomic(ctx, func(q *queries) error {
		cur, err := scanStream(q.wr.QueryRowContext(ctx, `SELECT `+streamCols+` FROM streams s WHERE s.id = ?`+q.forUpdate(), string(stream)))
		if err != nil {
			return q.mapErr(err)
		}
		if err := fn(&cur); err != nil {
			return err
		}
		_, err = q.wr.ExecContext(ctx, `
			UPDATE streams SET peer_id = ?, conversation_id = ?, owner_server_id = ?, sequencer = ?, kind = ?, state = ?,
				key_epoch = ?, last_seq = ?, first_seq = ?, servers_version = ?, max_blob_bytes = ?, history_days = ?,
				offered_name = ?, offered_at = ?, accepted_by = ?, home_hub = ?
			WHERE id = ?`,
			string(cur.PeerID), id(cur.ConversationID), id(cur.OwnerServerID), cur.Sequencer, cur.Kind, cur.State, cur.KeyEpoch,
			cur.LastSeq, cur.FirstSeq, cur.ServersVersion, zeroNull(cur.MaxBlobBytes), cur.HistoryDays, id(cur.OfferedName),
			ms(cur.OfferedAt), id(cur.AcceptedBy), id(cur.HomeHub), string(stream))
		out = cur
		return q.mapErr(err)
	})
	return out, err
}

func (st streams) Delete(ctx context.Context, stream model.StreamID) error {
	return st.changed(st.wr.ExecContext(ctx, `DELETE FROM streams WHERE id = ?`, string(stream)))
}

func (st streams) Detach(ctx context.Context, stream model.StreamID) error {
	return st.atomic(ctx, func(q *queries) error {
		for _, table := range []string{"outbox", "stream_seen", "stream_log"} {
			if _, err := q.wr.ExecContext(ctx, `DELETE FROM `+table+` WHERE stream_id = ?`, string(stream)); err != nil {
				return err
			}
		}
		return nil
	})
}

func (st streams) PutChannel(ctx context.Context, c store.ChannelRecord) error {
	if len(c.Makers) == 0 {
		c.Makers = []byte("{}")
	}
	mods, err := json.Marshal(append([]model.AccountID{}, c.Moderators...))
	if err != nil {
		return err
	}
	_, err = st.wr.ExecContext(ctx, `
		INSERT INTO stream_channels (stream_id, version, home_hub, record, sig, makers, moderators) VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (stream_id) DO UPDATE SET version = excluded.version, home_hub = excluded.home_hub, record = excluded.record,
			sig = excluded.sig, makers = excluded.makers, moderators = excluded.moderators`,
		string(c.StreamID), c.Version, c.HomeHub, c.Record, c.Sig, string(c.Makers), string(mods))
	return st.mapErr(err)
}

func (st streams) Channel(ctx context.Context, stream model.StreamID) (store.ChannelRecord, error) {
	var c store.ChannelRecord
	var makers, mods string
	err := st.rd.QueryRowContext(ctx, `SELECT stream_id, version, home_hub, record, sig, makers, moderators FROM stream_channels WHERE stream_id = ?`,
		string(stream)).Scan(str(&c.StreamID), &c.Version, &c.HomeHub, blob(&c.Record), blob(&c.Sig), &makers, &mods)
	if err != nil {
		return c, st.mapErr(err)
	}
	c.Makers = []byte(makers)
	if err := json.Unmarshal([]byte(mods), &c.Moderators); err != nil {
		return c, err
	}
	return c, nil
}

func (st streams) DeleteChannel(ctx context.Context, stream model.StreamID) error {
	_, err := st.wr.ExecContext(ctx, `DELETE FROM stream_channels WHERE stream_id = ?`, string(stream))
	return st.mapErr(err)
}

const moveCols = `id, stream_id, from_peer_id, hub_peer_id, hub_name, hub_fingerprint, state, reason, commit_seq, commit_event_id,
	accept, accept_sig, commit_block, commit_sig, proposed_by, answered_by, created_at, expires_at, updated_at`

// nilBytes passes an empty byte slice as NULL.
func nilBytes(b []byte) any {
	if len(b) == 0 {
		return nil
	}
	return b
}

func scanMove(sc scanner) (store.StreamMove, error) {
	var m store.StreamMove
	err := sc.Scan(&m.ID, str(&m.StreamID), str(&m.FromPeerID), str(&m.HubPeerID), &m.HubName, &m.HubFingerprint, &m.State,
		str(&m.Reason), num(&m.CommitSeq), str(&m.CommitEventID), blob(&m.Accept), blob(&m.AcceptSig), blob(&m.Commit), blob(&m.CommitSig),
		str(&m.ProposedBy), str(&m.AnsweredBy), tm(&m.CreatedAt), tm(&m.ExpiresAt), tm(&m.UpdatedAt))
	return m, err
}

func (st streams) PutMove(ctx context.Context, m store.StreamMove) error {
	if m.CreatedAt.IsZero() {
		m.CreatedAt = model.Now()
	}
	if m.UpdatedAt.IsZero() {
		m.UpdatedAt = m.CreatedAt
	}
	_, err := st.wr.ExecContext(ctx, `
		INSERT INTO stream_moves (`+moveCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET hub_peer_id = excluded.hub_peer_id, state = excluded.state, reason = excluded.reason,
			commit_seq = excluded.commit_seq, commit_event_id = excluded.commit_event_id, accept = excluded.accept,
			accept_sig = excluded.accept_sig, commit_block = excluded.commit_block, commit_sig = excluded.commit_sig,
			answered_by = excluded.answered_by, expires_at = excluded.expires_at, updated_at = excluded.updated_at`,
		m.ID, string(m.StreamID), string(m.FromPeerID), id(m.HubPeerID), m.HubName, m.HubFingerprint, m.State, id(m.Reason),
		zeroNull(m.CommitSeq), id(m.CommitEventID), nilBytes(m.Accept), nilBytes(m.AcceptSig), nilBytes(m.Commit), nilBytes(m.CommitSig),
		id(m.ProposedBy), id(m.AnsweredBy), int64(m.CreatedAt), int64(m.ExpiresAt), int64(m.UpdatedAt))
	return st.mapErr(err)
}

func (st streams) Move(ctx context.Context, move string) (store.StreamMove, error) {
	m, err := scanMove(st.rd.QueryRowContext(ctx, `SELECT `+moveCols+` FROM stream_moves WHERE id = ?`, move))
	return m, st.mapErr(err)
}

func (st streams) Moves(ctx context.Context, stream model.StreamID) ([]store.StreamMove, error) {
	where, args := "1 = 1", []any{}
	if stream != "" {
		where, args = "stream_id = ?", []any{string(stream)}
	}
	rows, err := st.rd.QueryContext(ctx, `SELECT `+moveCols+` FROM stream_moves WHERE `+where+` ORDER BY created_at DESC, id DESC`, args...)
	return collect(rows, err, scanMove)
}

func (st streams) PutServer(ctx context.Context, ss store.StreamServer) error {
	_, err := st.wr.ExecContext(ctx, `
		INSERT INTO stream_servers (stream_id, server_id, state, last_n, joined_at) VALUES (?, ?, ?, 0, ?)
		ON CONFLICT (stream_id, server_id) DO UPDATE SET state = excluded.state, joined_at = excluded.joined_at`,
		string(ss.StreamID), string(ss.ServerID), ss.State, ms(ss.JoinedAt))
	return st.mapErr(err)
}

func (st streams) Servers(ctx context.Context, stream model.StreamID) ([]store.StreamServer, error) {
	rows, err := st.rd.QueryContext(ctx, `
		SELECT ss.stream_id, ss.server_id, sv.name, ss.state, ss.last_n, ss.last_at, ss.logged_n, ss.joined_at
		FROM stream_servers ss JOIN servers sv ON sv.id = ss.server_id WHERE ss.stream_id = ? ORDER BY sv.name`, string(stream))
	return collect(rows, err, func(sc scanner) (store.StreamServer, error) {
		var ss store.StreamServer
		err := sc.Scan(str(&ss.StreamID), str(&ss.ServerID), &ss.Name, &ss.State, &ss.LastN, &ss.LastAt, &ss.LoggedN, tm(&ss.JoinedAt))
		return ss, err
	})
}

func (st streams) Stamp(ctx context.Context, stream model.StreamID, server model.ServerID, now int64) (int64, int64, error) {
	var at, n int64
	err := st.atomic(ctx, func(q *queries) error {
		// The stream's row first, then the server's: every writer of the
		// two takes them in this order.
		err := q.wr.QueryRowContext(ctx, `
			UPDATE streams SET max_at = CASE WHEN max_at >= ? THEN max_at + 1 ELSE ? END WHERE id = ? RETURNING max_at`,
			now, now, string(stream)).Scan(&at)
		if err != nil {
			return q.mapErr(err)
		}
		err = q.wr.QueryRowContext(ctx, `
			UPDATE stream_servers SET last_n = last_n + 1, last_at = ? WHERE stream_id = ? AND server_id = ? RETURNING last_n`,
			at, string(stream), string(server)).Scan(&n)
		return q.mapErr(err)
	})
	return at, n, err
}

func (st streams) Applied(ctx context.Context, stream model.StreamID, server model.ServerID, n, at int64) error {
	return st.atomic(ctx, func(q *queries) error {
		if _, err := q.wr.ExecContext(ctx, `UPDATE streams SET max_at = ? WHERE id = ? AND max_at < ?`, at, string(stream), at); err != nil {
			return q.mapErr(err)
		}
		_, err := q.wr.ExecContext(ctx, `
			UPDATE stream_servers SET last_n = ?, last_at = CASE WHEN last_at < ? THEN ? ELSE last_at END
			WHERE stream_id = ? AND server_id = ? AND last_n < ?`, n, at, at, string(stream), string(server), n)
		return q.mapErr(err)
	})
}

func (st streams) Acked(ctx context.Context, stream model.StreamID, at int64) error {
	_, err := st.wr.ExecContext(ctx, `UPDATE streams SET acked_at = ? WHERE id = ? AND acked_at < ?`, at, string(stream), at)
	return st.mapErr(err)
}

func (st streams) Rewind(ctx context.Context, stream model.StreamID, self model.ServerID) error {
	return st.atomic(ctx, func(q *queries) error {
		var acked, others int64
		if err := q.wr.QueryRowContext(ctx, `SELECT acked_at FROM streams WHERE id = ?`+q.forUpdate(), string(stream)).Scan(&acked); err != nil {
			return q.mapErr(err)
		}
		if err := q.wr.QueryRowContext(ctx, `SELECT COALESCE(max(last_at), 0) FROM stream_servers WHERE stream_id = ? AND server_id <> ?`,
			string(stream), string(self)).Scan(&others); err != nil {
			return q.mapErr(err)
		}
		_, err := q.wr.ExecContext(ctx, `UPDATE streams SET max_at = ? WHERE id = ?`, max(acked, others), string(stream))
		return q.mapErr(err)
	})
}

func (st streams) RaiseTop(ctx context.Context, stream model.StreamID, ord string) error {
	_, err := st.wr.ExecContext(ctx, `UPDATE streams SET top_ord = ? WHERE id = ? AND top_ord < ?`, ord, string(stream), ord)
	return st.mapErr(err)
}

func (st streams) SetOrder(ctx context.Context, msg model.MessageID, ord string) error {
	return st.atomic(ctx, func(q *queries) error {
		var parent model.MessageID
		err := q.wr.QueryRowContext(ctx, `UPDATE messages SET ord = ? WHERE id = ? RETURNING parent_id`, ord, string(msg)).Scan(str(&parent))
		if err != nil {
			return q.mapErr(err)
		}
		// A reply's order is in its parent's thread summary.
		return q.refreshThread(ctx, parent)
	})
}

func (st streams) SetEditOrder(ctx context.Context, msg model.MessageID, ord string) error {
	_, err := st.wr.ExecContext(ctx, `UPDATE messages SET edit_ord = ? WHERE id = ?`, ord, string(msg))
	return st.mapErr(err)
}

func (st streams) SetOrderFloor(ctx context.Context, conv model.ConversationID, floor int64) error {
	_, err := st.wr.ExecContext(ctx, `UPDATE conversations SET order_floor = ? WHERE id = ?`, floor, string(conv))
	return st.mapErr(err)
}

func (st streams) LockConversation(ctx context.Context, conv model.ConversationID) error {
	var id string
	err := st.wr.QueryRowContext(ctx, `SELECT id FROM conversations WHERE id = ?`+st.forUpdate(), string(conv)).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	return st.mapErr(err)
}

func (st streams) TakeSeq(ctx context.Context, conv model.ConversationID) (int64, model.StreamID, error) {
	var seq int64
	var stream model.StreamID
	var taken takenSeq
	err := st.wr.QueryRowContext(ctx,
		`UPDATE conversations SET next_seq = next_seq + 1 WHERE id = ? RETURNING next_seq - 1, stream_id, order_floor, kind`, string(conv)).
		Scan(&seq, str(&stream), &taken.floor, &taken.kind)
	if err == nil {
		st.noteSeq(conv, taken)
	}
	return seq, stream, st.mapErr(err)
}

func (st streams) TakeMessageSeq(ctx context.Context, conv model.ConversationID, at model.Time) (int64, model.StreamID, error) {
	var seq int64
	var stream model.StreamID
	taken := takenSeq{lastAt: at}
	err := st.wr.QueryRowContext(ctx, `
		UPDATE conversations SET next_seq = next_seq + 1, last_message_at = ? WHERE id = ?
		RETURNING next_seq - 1, stream_id, order_floor, kind`, int64(at), string(conv)).
		Scan(&seq, str(&stream), &taken.floor, &taken.kind)
	if err == nil {
		st.noteSeq(conv, taken)
	}
	return seq, stream, st.mapErr(err)
}

func (st streams) OutboxIDs(ctx context.Context, stream model.StreamID) ([]string, error) {
	rows, err := st.rd.QueryContext(ctx, `SELECT id FROM outbox WHERE stream_id = ?`, string(stream))
	return collectIDs[string](rows, err)
}

func (st streams) SetLastN(ctx context.Context, stream model.StreamID, server model.ServerID, n int64) error {
	_, err := st.wr.ExecContext(ctx, `UPDATE stream_servers SET last_n = ? WHERE stream_id = ? AND server_id = ?`,
		n, string(stream), string(server))
	return st.mapErr(err)
}

func (st streams) NextN(ctx context.Context, stream model.StreamID, server model.ServerID) (int64, error) {
	var n int64
	err := st.wr.QueryRowContext(ctx, `
		UPDATE stream_servers SET last_n = last_n + 1 WHERE stream_id = ? AND server_id = ? RETURNING last_n`,
		string(stream), string(server)).Scan(&n)
	return n, st.mapErr(err)
}

func (st streams) PutKey(ctx context.Context, stream model.StreamID, k store.StreamKey) error {
	if k.CreatedAt.IsZero() {
		k.CreatedAt = model.Now()
	}
	_, err := st.wr.ExecContext(ctx, `
		INSERT INTO stream_keys (stream_id, epoch, key, created_at) VALUES (?, ?, ?, ?)
		ON CONFLICT (stream_id, epoch) DO UPDATE SET key = excluded.key`,
		string(stream), k.Epoch, k.Sealed, int64(k.CreatedAt))
	return st.mapErr(err)
}

func scanKey(sc scanner) (store.StreamKey, error) {
	var k store.StreamKey
	err := sc.Scan(&k.Epoch, blob(&k.Sealed), tm(&k.CreatedAt))
	return k, err
}

func (st streams) Key(ctx context.Context, stream model.StreamID, epoch int64) (store.StreamKey, error) {
	k, err := scanKey(st.rd.QueryRowContext(ctx,
		`SELECT epoch, key, created_at FROM stream_keys WHERE stream_id = ? AND epoch = ?`, string(stream), epoch))
	return k, st.mapErr(err)
}

func (st streams) Keys(ctx context.Context, stream model.StreamID) ([]store.StreamKey, error) {
	rows, err := st.rd.QueryContext(ctx,
		`SELECT epoch, key, created_at FROM stream_keys WHERE stream_id = ? ORDER BY epoch`, string(stream))
	return collect(rows, err, scanKey)
}

const outboxCols = `id, stream_id, envelope, created_at, attempts, next_try_at`

func scanOutbox(sc scanner) (store.OutboxRow, error) {
	var r store.OutboxRow
	err := sc.Scan(str(&r.ID), str(&r.StreamID), blob(&r.Envelope), tm(&r.CreatedAt), &r.Attempts, tm(&r.NextTryAt))
	return r, err
}

func (st streams) PutOutbox(ctx context.Context, row store.OutboxRow) error {
	if row.CreatedAt.IsZero() {
		row.CreatedAt = model.Now()
	}
	if row.NextTryAt.IsZero() {
		row.NextTryAt = row.CreatedAt
	}
	_, err := st.wr.ExecContext(ctx, `
		INSERT INTO outbox (id, stream_id, envelope, created_at, attempts, next_try_at) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET envelope = excluded.envelope, attempts = 0, next_try_at = excluded.next_try_at`,
		row.ID, string(row.StreamID), row.Envelope, int64(row.CreatedAt), row.Attempts, int64(row.NextTryAt))
	return st.mapErr(err)
}

func (st streams) GetOutbox(ctx context.Context, event string) (store.OutboxRow, error) {
	r, err := scanOutbox(st.rd.QueryRowContext(ctx, `SELECT `+outboxCols+` FROM outbox WHERE id = ?`, event))
	return r, st.mapErr(err)
}

func (st streams) Outbox(ctx context.Context, stream model.StreamID) ([]store.OutboxRow, error) {
	rows, err := st.rd.QueryContext(ctx, `SELECT `+outboxCols+` FROM outbox WHERE stream_id = ? ORDER BY created_at, id`, string(stream))
	return collect(rows, err, scanOutbox)
}

func (st streams) OutboxDue(ctx context.Context, now model.Time, limit int) ([]store.OutboxRow, error) {
	rows, err := st.rd.QueryContext(ctx,
		`SELECT `+outboxCols+` FROM outbox WHERE next_try_at <= ? ORDER BY created_at, id LIMIT ?`, int64(now), limit)
	return collect(rows, err, scanOutbox)
}

func (st streams) OutboxTried(ctx context.Context, event string, attempts int, next model.Time) error {
	_, err := st.wr.ExecContext(ctx, `UPDATE outbox SET attempts = ?, next_try_at = ? WHERE id = ?`, attempts, int64(next), event)
	return st.mapErr(err)
}

func (st streams) DeleteOutbox(ctx context.Context, event string) (bool, error) {
	n, err := st.affected(st.wr.ExecContext(ctx, `DELETE FROM outbox WHERE id = ?`, event))
	return n > 0, err
}

func (st streams) OutboxCount(ctx context.Context) (int64, error) {
	var n int64
	err := st.rd.QueryRowContext(ctx, `SELECT count(*) FROM outbox`).Scan(&n)
	return n, err
}

func (st streams) MarkSeen(ctx context.Context, stream model.StreamID, event string, seq int64, at model.Time) (bool, error) {
	n, err := st.affected(st.wr.ExecContext(ctx, `
		INSERT INTO stream_seen (stream_id, origin_event_id, seq, at) VALUES (?, ?, ?, ?)
		ON CONFLICT (stream_id, origin_event_id) DO NOTHING`, string(stream), event, seq, int64(at)))
	return n == 1, err
}

func (st streams) Seen(ctx context.Context, stream model.StreamID, event string) (int64, bool, error) {
	var seq int64
	// Asked inside the transaction that will then write, so it reads
	// through the writer.
	err := st.wr.QueryRowContext(ctx, `SELECT seq FROM stream_seen WHERE stream_id = ? AND origin_event_id = ?`,
		string(stream), event).Scan(&seq)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	return seq, err == nil, err
}

func (st streams) PruneSeen(ctx context.Context, before model.Time) (int64, error) {
	return st.affected(st.wr.ExecContext(ctx, `DELETE FROM stream_seen WHERE at < ?`, int64(before)))
}

const logCols = `seq, origin, n, prev_n, origin_event_id, envelope, at`

func scanLog(sc scanner) (store.LogRow, error) {
	var r store.LogRow
	err := sc.Scan(&r.Seq, &r.Origin, &r.N, &r.Prev, str(&r.OriginEventID), blob(&r.Envelope), tm(&r.At))
	return r, err
}

func (st streams) AppendLog(ctx context.Context, stream model.StreamID, row store.LogRow) (store.LogRow, error) {
	if row.At.IsZero() {
		row.At = model.Now()
	}
	err := st.atomic(ctx, func(q *queries) error {
		// Taking the position locks the stream's row, so two writers get
		// two positions and the second sees what the first wrote.
		if err := q.wr.QueryRowContext(ctx, `UPDATE streams SET last_seq = last_seq + 1 WHERE id = ? RETURNING last_seq`,
			string(stream)).Scan(&row.Seq); err != nil {
			return q.mapErr(err)
		}
		// The origin's row before this one, whether or not the log still
		// holds it.
		var server model.ServerID
		err := q.wr.QueryRowContext(ctx, `
			SELECT ss.server_id, ss.logged_n FROM stream_servers ss JOIN servers sv ON sv.id = ss.server_id
			WHERE ss.stream_id = ? AND sv.name = ?`, string(stream), row.Origin).Scan(str(&server), &row.Prev)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if server != "" {
			if _, err := q.wr.ExecContext(ctx, `UPDATE stream_servers SET logged_n = ? WHERE stream_id = ? AND server_id = ?`,
				row.N, string(stream), string(server)); err != nil {
				return q.mapErr(err)
			}
		}
		_, err = q.wr.ExecContext(ctx, `
			INSERT INTO stream_log (stream_id, seq, origin, n, prev_n, origin_event_id, envelope, at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			string(stream), row.Seq, row.Origin, row.N, row.Prev, id(row.OriginEventID), row.Envelope, int64(row.At))
		return q.mapErr(err)
	})
	return row, err
}

func (st streams) Log(ctx context.Context, stream model.StreamID, after int64, limit int) ([]store.LogRow, error) {
	rows, err := st.rd.QueryContext(ctx,
		`SELECT `+logCols+` FROM stream_log WHERE stream_id = ? AND seq > ? ORDER BY seq LIMIT ?`, string(stream), max(after, 0), limit)
	return collect(rows, err, scanLog)
}

func (st streams) LogSince(ctx context.Context, stream model.StreamID, origin string, n int64, limit int) ([]store.LogRow, error) {
	rows, err := st.rd.QueryContext(ctx,
		`SELECT `+logCols+` FROM stream_log WHERE stream_id = ? AND origin = ? AND n > ? ORDER BY seq LIMIT ?`,
		string(stream), origin, n, limit)
	return collect(rows, err, scanLog)
}

func (st streams) LogHas(ctx context.Context, stream model.StreamID, event string) (bool, error) {
	var one int
	// Asked inside the transaction that will then write, so it reads
	// through the writer.
	err := st.wr.QueryRowContext(ctx, `SELECT 1 FROM stream_log WHERE stream_id = ? AND origin_event_id = ? LIMIT 1`,
		string(stream), event).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func (st streams) LogFloor(ctx context.Context, stream model.StreamID) (map[string]int64, error) {
	rows, err := st.rd.QueryContext(ctx, `
		SELECT l.origin, l.prev_n FROM stream_log l
		WHERE l.stream_id = ? AND l.seq = (SELECT min(o.seq) FROM stream_log o WHERE o.stream_id = l.stream_id AND o.origin = l.origin)`,
		string(stream))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var origin string
		var prev int64
		if err := rows.Scan(&origin, &prev); err != nil {
			return nil, err
		}
		out[origin] = prev
	}
	return out, rows.Err()
}

func (st streams) SetFormTally(ctx context.Context, form model.FormID, answerCount int, tally map[string]map[string]int) error {
	return st.atomic(ctx, func(q *queries) error {
		if _, err := q.wr.ExecContext(ctx, `UPDATE forms SET answer_count = ? WHERE id = ?`, answerCount, string(form)); err != nil {
			return q.mapErr(err)
		}
		if _, err := q.wr.ExecContext(ctx, `DELETE FROM form_tallies WHERE form_id = ?`, string(form)); err != nil {
			return q.mapErr(err)
		}
		for field, options := range tally {
			for option, count := range options {
				if count <= 0 {
					continue
				}
				if _, err := q.wr.ExecContext(ctx, `INSERT INTO form_tallies (form_id, field_id, option_id, count) VALUES (?, ?, ?, ?)`,
					string(form), field, option, count); err != nil {
					return q.mapErr(err)
				}
			}
		}
		return nil
	})
}

func (st streams) PruneLog(ctx context.Context, before model.Time) (int64, error) {
	return st.affected(st.wr.ExecContext(ctx, `DELETE FROM stream_log WHERE at < ?`, int64(before)))
}

func (st streams) PutShadow(ctx context.Context, acc *model.Account) (bool, error) {
	if acc.ServerID == "" || acc.ID == "" {
		return false, errors.New("sqlstore: a shadow account needs its id and its server")
	}
	acc.Role = model.RoleRemote
	changed := false
	err := st.atomic(ctx, func(q *queries) error {
		var server model.ServerID
		var version int64
		// Locked for a change that touches no key: other transactions go on
		// referring to the account (as the creator of a conversation, say)
		// while its profile is brought up to date.
		lock := ""
		if q.pg() {
			lock = " FOR NO KEY UPDATE"
		}
		err := q.wr.QueryRowContext(ctx, `SELECT server_id, profile_version FROM accounts WHERE id = ?`+lock, string(acc.ID)).
			Scan(str(&server), &version)
		if errors.Is(err, sql.ErrNoRows) {
			changed = true
			return accounts{q}.insert(ctx, q, acc, true)
		}
		if err != nil {
			return err
		}
		if server != acc.ServerID {
			return model.ErrConflict.With("That account identifier belongs to another server.")
		}
		if acc.ProfileVersion <= version {
			return nil
		}
		changed = true
		_, err = q.wr.ExecContext(ctx, `
			UPDATE accounts SET username = ?, display_name = ?, avatar_file_id = ?, title = ?, pronouns = ?, time_zone = ?,
				profile_version = ?, removed_at = ? WHERE id = ?`,
			acc.Username, acc.DisplayName, id(acc.AvatarFileID), id(acc.Title), id(acc.Pronouns), id(acc.TimeZone),
			acc.ProfileVersion, ms(acc.RemovedAt), string(acc.ID))
		q.touchAccount(acc.ID)
		return q.taken(err)
	})
	return changed, err
}

func (st streams) Shadow(ctx context.Context, server model.ServerID, username string) (model.Account, error) {
	acc, err := scanAccount(st.rd.QueryRowContext(ctx,
		`SELECT `+accountCols+accountFrom+`WHERE a.server_id = ? AND a.username = ?`, string(server), username))
	return acc, st.mapErr(err)
}

func (st streams) FileArrived(ctx context.Context, f model.File) error {
	return st.changed(st.wr.ExecContext(ctx, `
		UPDATE files SET state = 'ready', blob_key = ?, thumb_key = ?, media_type = ?, kind = ?, width = ?, height = ?,
			duration_seconds = ?, preview_text = ? WHERE id = ?`,
		id(f.BlobKey), id(f.ThumbKey), f.MediaType, f.Kind, zeroNull(int64(f.Width)), zeroNull(int64(f.Height)),
		zeroNull(int64(f.DurationSeconds)), id(f.PreviewText), string(f.ID)))
}

func (st streams) SetFileState(ctx context.Context, file model.FileID, state string) error {
	return st.changed(st.wr.ExecContext(ctx, `UPDATE files SET state = ? WHERE id = ?`, state, string(file)))
}

func (st streams) ArrivingFiles(ctx context.Context, limit int) ([]store.ArrivingFile, error) {
	rows, err := st.rd.QueryContext(ctx, `SELECT `+fileCols+` FROM files WHERE state = 'arriving' ORDER BY created_at, id LIMIT ?`, limit)
	files, err := collect(rows, err, scanFile)
	if err != nil {
		return nil, err
	}
	out := make([]store.ArrivingFile, 0, len(files))
	for _, f := range files {
		a := store.ArrivingFile{File: f}
		// Whose it is: the home server of whoever posted it.
		if err := st.rd.QueryRowContext(ctx, `SELECT server_id FROM accounts WHERE id = ?`, string(f.UploaderID)).
			Scan(str(&a.ServerID)); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		if err := st.rd.QueryRowContext(ctx, `SELECT id FROM streams WHERE conversation_id = ? AND state = 'active'`,
			string(f.ConversationID)).Scan(str(&a.StreamID)); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		out = append(out, a)
	}
	return out, nil
}

func (st streams) FileInStream(ctx context.Context, file model.FileID, conv model.ConversationID) (model.File, bool, error) {
	f, err := scanFile(st.rd.QueryRowContext(ctx, `SELECT `+fileCols+` FROM files WHERE id = ?`, string(file)))
	if errors.Is(err, sql.ErrNoRows) {
		return f, false, nil
	}
	if err != nil {
		return f, false, err
	}
	// An attachment or a card's image belongs to the conversation it was
	// posted in. Emoji and avatars live in the library conversation and
	// are named by any event that draws them.
	ok := f.ConversationID == conv || f.Purpose == "emoji" || f.Purpose == "avatar"
	return f, ok && f.BlobKey != "", nil
}

func (st streams) PutRemoteEmoji(ctx context.Context, e model.Emoji, server model.ServerID) error {
	if e.CreatedAt.IsZero() {
		e.CreatedAt = model.Now()
	}
	_, err := st.wr.ExecContext(ctx, `
		INSERT INTO emoji (id, name, file_id, created_by, created_at, server_id) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT DO NOTHING`,
		string(e.ID), e.Name, string(e.FileID), string(e.CreatedByID), int64(e.CreatedAt), string(server))
	return st.mapErr(err)
}

func (st streams) RemoteEmoji(ctx context.Context, server model.ServerID, name string) (model.Emoji, error) {
	out, err := scanEmoji(st.rd.QueryRowContext(ctx, emojiSelect+` WHERE e.server_id = ? AND e.name = ?`, string(server), name))
	return out, st.mapErr(err)
}

func (st streams) SharedWith(ctx context.Context, a, b model.AccountID) ([]store.Stream, error) {
	rows, err := st.rd.QueryContext(ctx, `
		SELECT `+streamCols+` FROM streams s
		JOIN members ma ON ma.conversation_id = s.conversation_id AND ma.account_id = ?
		JOIN members mb ON mb.conversation_id = s.conversation_id AND mb.account_id = ?
		WHERE s.state = 'active' AND s.kind = 'channel' ORDER BY s.created_at, s.id`, string(a), string(b))
	return collect(rows, err, scanStream)
}

func (st streams) AccountStreams(ctx context.Context, account model.AccountID) ([]store.Stream, error) {
	rows, err := st.rd.QueryContext(ctx, `
		SELECT `+streamCols+` FROM streams s
		JOIN members m ON m.conversation_id = s.conversation_id AND m.account_id = ?
		WHERE s.state = 'active' ORDER BY s.created_at, s.id`, string(account))
	return collect(rows, err, scanStream)
}
