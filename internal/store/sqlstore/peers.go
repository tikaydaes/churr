package sqlstore

import (
	"context"
	"database/sql"
	"errors"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

type peers struct{ *queries }

// Peers returns the Peers area.
func (q *queries) Peers() store.Peers { return peers{q} }

const serverCols = `id, name, sign_key, box_key, first_seen_at, blocked_at`

func scanServer(s scanner) (store.Server, error) {
	var out store.Server
	err := s.Scan(str(&out.ID), &out.Name, blob(&out.SignKey), blob(&out.BoxKey), tm(&out.FirstSeenAt), tm(&out.BlockedAt))
	return out, err
}

func (p peers) PinServer(ctx context.Context, s store.Server) (store.Server, bool, error) {
	if s.ID == "" {
		s.ID = model.NewID[model.ServerID]()
	}
	if s.FirstSeenAt.IsZero() {
		s.FirstSeenAt = model.Now()
	}
	// A hub has no box key; the column is NOT NULL, so it holds no bytes.
	box := s.BoxKey
	if box == nil {
		box = []byte{}
	}
	var out store.Server
	created := false
	err := p.atomic(ctx, func(q *queries) error {
		n, err := q.affected(q.wr.ExecContext(ctx, `
			INSERT INTO servers (id, name, sign_key, box_key, first_seen_at) VALUES (?, ?, ?, ?, ?)
			ON CONFLICT (name) DO NOTHING`, string(s.ID), s.Name, s.SignKey, box, int64(s.FirstSeenAt)))
		if err != nil {
			return err
		}
		created = n == 1
		out, err = scanServer(q.wr.QueryRowContext(ctx, `SELECT `+serverCols+` FROM servers WHERE name = ?`, s.Name))
		return q.mapErr(err)
	})
	return out, created, err
}

func (p peers) Server(ctx context.Context, server model.ServerID) (store.Server, error) {
	out, err := scanServer(p.rd.QueryRowContext(ctx, `SELECT `+serverCols+` FROM servers WHERE id = ?`, string(server)))
	return out, p.mapErr(err)
}

func (p peers) ServerByName(ctx context.Context, name string) (store.Server, error) {
	out, err := scanServer(p.rd.QueryRowContext(ctx, `SELECT `+serverCols+` FROM servers WHERE name = ?`, name))
	return out, p.mapErr(err)
}

func (p peers) Servers(ctx context.Context) ([]store.Server, error) {
	rows, err := p.rd.QueryContext(ctx, `SELECT `+serverCols+` FROM servers ORDER BY name`)
	return collect(rows, err, scanServer)
}

func (p peers) SetServerBlocked(ctx context.Context, server model.ServerID, at model.Time) error {
	return p.changed(p.wr.ExecContext(ctx, `UPDATE servers SET blocked_at = ? WHERE id = ?`, ms(at), string(server)))
}

const peerCols = `id, kind, server_id, hub_name, hub_sign_key, url, dials, state, invite_hash, invite_expires_at,
	created_by, created_at, connected_at, last_error, clock_told, clock_measured, away`

func scanPeer(s scanner) (store.Peer, error) {
	var out store.Peer
	err := s.Scan(str(&out.ID), &out.Kind, str(&out.ServerID), str(&out.HubName), blob(&out.HubSignKey), str(&out.URL),
		flag(&out.Dials), &out.State, blob(&out.InviteHash), tm(&out.InviteExpiresAt), str(&out.CreatedBy), tm(&out.CreatedAt),
		tm(&out.ConnectedAt), str(&out.LastError), js(&out.ClockTold), js(&out.ClockMeasured), js(&out.Away))
	return out, err
}

func (p peers) AddAway(ctx context.Context, peer model.PeerID, from, to int64, keep int) error {
	return p.atomic(ctx, func(q *queries) error {
		var away [][2]int64
		if err := q.wr.QueryRowContext(ctx, `SELECT away FROM peers WHERE id = ?`+q.forUpdate(), string(peer)).Scan(js(&away)); err != nil {
			return q.mapErr(err)
		}
		away = append(away, [2]int64{from, to})
		if len(away) > keep {
			away = away[len(away)-keep:]
		}
		_, err := q.wr.ExecContext(ctx, `UPDATE peers SET away = ? WHERE id = ?`, jsonArg(away), string(peer))
		return q.mapErr(err)
	})
}

func (p peers) SetClockTold(ctx context.Context, peer model.PeerID, c model.PeerClock) error {
	return p.changed(p.wr.ExecContext(ctx, `UPDATE peers SET clock_told = ? WHERE id = ?`, jsonArg(c), string(peer)))
}

func (p peers) SetClockMeasured(ctx context.Context, peer model.PeerID, c model.PeerClock) error {
	return p.changed(p.wr.ExecContext(ctx, `UPDATE peers SET clock_measured = ? WHERE id = ?`, jsonArg(c), string(peer)))
}

func (p peers) Create(ctx context.Context, peer *store.Peer) error {
	if peer.ID == "" {
		peer.ID = model.NewID[model.PeerID]()
	}
	if peer.CreatedAt.IsZero() {
		peer.CreatedAt = model.Now()
	}
	_, err := p.wr.ExecContext(ctx, `
		INSERT INTO peers (id, kind, server_id, hub_name, hub_sign_key, url, dials, state, invite_hash, invite_expires_at,
			created_by, created_at, connected_at, last_error)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		string(peer.ID), peer.Kind, id(peer.ServerID), id(peer.HubName), bin(peer.HubSignKey), id(peer.URL), peer.Dials, peer.State,
		bin(peer.InviteHash), ms(peer.InviteExpiresAt), string(peer.CreatedBy), int64(peer.CreatedAt), ms(peer.ConnectedAt),
		id(peer.LastError))
	return p.mapErr(err)
}

func (p peers) Get(ctx context.Context, peer model.PeerID) (store.Peer, error) {
	out, err := scanPeer(p.rd.QueryRowContext(ctx, `SELECT `+peerCols+` FROM peers WHERE id = ?`, string(peer)))
	return out, p.mapErr(err)
}

func (p peers) List(ctx context.Context) ([]store.Peer, error) {
	rows, err := p.rd.QueryContext(ctx, `SELECT `+peerCols+` FROM peers ORDER BY created_at, id`)
	return collect(rows, err, scanPeer)
}

func (p peers) Update(ctx context.Context, peer model.PeerID, fn func(*store.Peer) error) (store.Peer, error) {
	var out store.Peer
	err := p.atomic(ctx, func(q *queries) error {
		cur, err := scanPeer(q.wr.QueryRowContext(ctx, `SELECT `+peerCols+` FROM peers WHERE id = ?`+q.forUpdate(), string(peer)))
		if err != nil {
			return q.mapErr(err)
		}
		if err := fn(&cur); err != nil {
			return err
		}
		_, err = q.wr.ExecContext(ctx, `
			UPDATE peers SET kind = ?, server_id = ?, hub_name = ?, hub_sign_key = ?, url = ?, dials = ?, state = ?,
				invite_hash = ?, invite_expires_at = ?, connected_at = ?, last_error = ?
			WHERE id = ?`,
			cur.Kind, id(cur.ServerID), id(cur.HubName), bin(cur.HubSignKey), id(cur.URL), cur.Dials, cur.State,
			bin(cur.InviteHash), ms(cur.InviteExpiresAt), ms(cur.ConnectedAt), id(cur.LastError), string(peer))
		out = cur
		return q.mapErr(err)
	})
	return out, err
}

func (p peers) Delete(ctx context.Context, peer model.PeerID) error {
	return p.atomic(ctx, func(q *queries) error {
		var one int
		err := q.wr.QueryRowContext(ctx, `SELECT 1 FROM streams WHERE peer_id = ? LIMIT 1`, string(peer)).Scan(&one)
		if err == nil {
			return model.ErrConflict.With("This link still has shared channels.")
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		return q.changed(q.wr.ExecContext(ctx, `DELETE FROM peers WHERE id = ?`, string(peer)))
	})
}

func (p peers) SetReachable(ctx context.Context, peer model.PeerID, servers []model.ServerID) error {
	return p.atomic(ctx, func(q *queries) error {
		if _, err := q.wr.ExecContext(ctx, `DELETE FROM peer_servers WHERE peer_id = ?`, string(peer)); err != nil {
			return err
		}
		for _, s := range servers {
			if _, err := q.wr.ExecContext(ctx, `INSERT INTO peer_servers (peer_id, server_id) VALUES (?, ?) ON CONFLICT DO NOTHING`,
				string(peer), string(s)); err != nil {
				return q.mapErr(err)
			}
		}
		return nil
	})
}

func (p peers) Reachable(ctx context.Context, peer model.PeerID) ([]store.Server, error) {
	rows, err := p.rd.QueryContext(ctx, `
		SELECT s.id, s.name, s.sign_key, s.box_key, s.first_seen_at, s.blocked_at
		FROM peer_servers ps JOIN servers s ON s.id = ps.server_id WHERE ps.peer_id = ? ORDER BY s.name`, string(peer))
	return collect(rows, err, scanServer)
}
