package sqlstore

import (
	"context"
	"database/sql"
	"errors"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

// Chat requests and ghost members (rulings R77, R78).

type chatRequests struct{ *queries }

// ChatRequests returns the chat requests area.
func (q *queries) ChatRequests() store.ChatRequests { return chatRequests{q} }

const chatRequestCols = `id, requester_id, recipient_id, kind, conversation_id, state, shown_open, created_at, answered_at, closed_at`

func scanChatRequest(s scanner) (model.ChatRequest, error) {
	var r model.ChatRequest
	err := s.Scan(str(&r.ID), str(&r.RequesterID), str(&r.RecipientID), &r.Kind, str(&r.ConversationID), &r.State,
		flag(&r.ShownOpen), tm(&r.CreatedAt), tm(&r.AnsweredAt), tm(&r.ClosedAt))
	return r, err
}

func (c chatRequests) one(row *sql.Row) (model.ChatRequest, bool, error) {
	r, err := scanChatRequest(row)
	if errors.Is(err, sql.ErrNoRows) {
		return r, false, nil
	}
	return r, err == nil, c.mapErr(err)
}

func (c chatRequests) Create(ctx context.Context, r *model.ChatRequest) error {
	if r.ID == "" {
		r.ID = model.NewID[string]()
	}
	if r.CreatedAt.IsZero() {
		r.CreatedAt = model.Now()
	}
	// The partial unique index lets one open request per pair and kind in;
	// a second is refused without failing the transaction.
	n, err := c.affected(c.wr.ExecContext(ctx, `
		INSERT INTO chat_requests (`+chatRequestCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT DO NOTHING`,
		r.ID, string(r.RequesterID), string(r.RecipientID), r.Kind, id(string(r.ConversationID)), r.State, r.ShownOpen,
		int64(r.CreatedAt), ms(r.AnsweredAt), ms(r.ClosedAt)))
	if err != nil {
		return err
	}
	if n == 0 {
		return model.ErrAlreadyExists
	}
	return nil
}

func (c chatRequests) Get(ctx context.Context, rid string) (model.ChatRequest, error) {
	if !model.ValidID(rid) {
		return model.ChatRequest{}, model.ErrNotFound
	}
	r, found, err := c.one(c.rd.QueryRowContext(ctx, `SELECT `+chatRequestCols+` FROM chat_requests WHERE id = ?`, rid))
	if err == nil && !found {
		err = model.ErrNotFound
	}
	return r, err
}

func (c chatRequests) Open(ctx context.Context, requester, recipient model.AccountID, kind string) (model.ChatRequest, bool, error) {
	return c.one(c.wr.QueryRowContext(ctx, `SELECT `+chatRequestCols+` FROM chat_requests
		WHERE requester_id = ? AND recipient_id = ? AND kind = ? AND state IN ('pending', 'dismissed')`,
		string(requester), string(recipient), kind))
}

func (c chatRequests) ByConversation(ctx context.Context, conv model.ConversationID) (model.ChatRequest, bool, error) {
	return c.one(c.wr.QueryRowContext(ctx, `SELECT `+chatRequestCols+` FROM chat_requests
		WHERE conversation_id = ? ORDER BY created_at DESC, id DESC LIMIT 1`, string(conv)))
}

func (c chatRequests) Answer(ctx context.Context, rid string, from []string, to string, at model.Time) error {
	if len(from) == 0 {
		return model.ErrConflict
	}
	args := []any{to, int64(at), rid}
	for _, f := range from {
		args = append(args, f)
	}
	n, err := c.affected(c.wr.ExecContext(ctx, `UPDATE chat_requests SET state = ?, answered_at = ?
		WHERE id = ? AND state IN (`+marks(len(from))+`)`, args...))
	if err != nil {
		return err
	}
	if n == 0 {
		return model.ErrConflict.With("That request has been answered already.")
	}
	return nil
}

func (c chatRequests) SetConversation(ctx context.Context, rid string, conv model.ConversationID) error {
	return c.changed(c.wr.ExecContext(ctx, `UPDATE chat_requests SET conversation_id = ? WHERE id = ?`, string(conv), rid))
}

func (c chatRequests) Closed(ctx context.Context, conv model.ConversationID, at model.Time) error {
	_, err := c.wr.ExecContext(ctx, `UPDATE chat_requests SET closed_at = ?
		WHERE id = (SELECT id FROM chat_requests WHERE conversation_id = ? AND closed_at IS NULL ORDER BY created_at DESC, id DESC LIMIT 1)`,
		int64(at), string(conv))
	return c.mapErr(err)
}

func (c chatRequests) Incoming(ctx context.Context, recipient model.AccountID) ([]model.ChatRequest, error) {
	rows, err := c.rd.QueryContext(ctx, `SELECT `+chatRequestCols+` FROM chat_requests
		WHERE recipient_id = ? AND state = 'pending' ORDER BY created_at, id`, string(recipient))
	return collect(rows, err, scanChatRequest)
}

func (c chatRequests) LastRefused(ctx context.Context, requester, recipient model.AccountID) (model.Time, error) {
	var at sql.NullInt64
	err := c.wr.QueryRowContext(ctx, `SELECT MAX(answered_at) FROM chat_requests
		WHERE requester_id = ? AND recipient_id = ? AND state IN ('declined', 'withdrawn')`,
		string(requester), string(recipient)).Scan(&at)
	if err != nil {
		return 0, c.mapErr(err)
	}
	return model.Time(at.Int64), nil
}

func (c chatRequests) Counts(ctx context.Context, requester model.AccountID, since model.Time, quick int64) (model.RequestCounts, error) {
	var out model.RequestCounts
	// Kept: accepted, or shown open to the requester, and not closed
	// within `quick` of opening. Opening is the answer, or the asking for
	// one shown open at once.
	err := c.wr.QueryRowContext(ctx, `
		SELECT
			COALESCE(SUM(CASE WHEN kept = 1 THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN kept = 1 THEN 0 ELSE 1 END), 0)
		FROM (SELECT CASE
				WHEN (state = 'accepted' OR shown_open = ?)
				 AND (closed_at IS NULL OR closed_at - COALESCE(answered_at, created_at) >= ?) THEN 1
				ELSE 0 END AS kept
			FROM chat_requests WHERE requester_id = ? AND created_at >= ?) r`,
		true, quick, string(requester), int64(since)).Scan(&out.Kept, &out.Unapproved)
	return out, c.mapErr(err)
}

func (c chatRequests) AddGhost(ctx context.Context, g model.Ghost) error {
	if g.CreatedAt.IsZero() {
		g.CreatedAt = model.Now()
	}
	return c.atomic(ctx, func(q *queries) error {
		if _, err := q.wr.ExecContext(ctx, `INSERT INTO ghost_members (conversation_id, account_id, shown_to, created_at)
			VALUES (?, ?, ?, ?) ON CONFLICT DO NOTHING`,
			string(g.ConversationID), string(g.AccountID), string(g.ShownTo), int64(g.CreatedAt)); err != nil {
			return q.mapErr(err)
		}
		return q.refreshKeyOf(ctx, g.ConversationID)
	})
}

func (c chatRequests) RemoveGhosts(ctx context.Context, conv model.ConversationID, account model.AccountID) error {
	_, err := c.wr.ExecContext(ctx, `DELETE FROM ghost_members WHERE conversation_id = ? AND account_id = ?`, string(conv), string(account))
	return c.mapErr(err)
}

func scanGhost(s scanner) (model.Ghost, error) {
	var g model.Ghost
	err := s.Scan(str(&g.ConversationID), str(&g.AccountID), str(&g.ShownTo), tm(&g.CreatedAt))
	return g, err
}

func (c chatRequests) Ghosts(ctx context.Context, conv model.ConversationID) ([]model.Ghost, error) {
	rows, err := c.rd.QueryContext(ctx, `SELECT conversation_id, account_id, shown_to, created_at FROM ghost_members
		WHERE conversation_id = ? ORDER BY created_at, account_id`, string(conv))
	return collect(rows, err, scanGhost)
}

func (c chatRequests) GhostsShownTo(ctx context.Context, viewer model.AccountID, convs []model.ConversationID) (map[model.ConversationID][]model.Ghost, error) {
	out := map[model.ConversationID][]model.Ghost{}
	if len(convs) == 0 {
		return out, nil
	}
	args := append([]any{string(viewer)}, anys(convs)...)
	rows, err := c.rd.QueryContext(ctx, `SELECT conversation_id, account_id, shown_to, created_at FROM ghost_members
		WHERE shown_to = ? AND conversation_id IN (`+marks(len(convs))+`) ORDER BY created_at, account_id`, args...)
	list, err := collect(rows, err, scanGhost)
	if err != nil {
		return nil, err
	}
	for _, g := range list {
		out[g.ConversationID] = append(out[g.ConversationID], g)
	}
	return out, nil
}
