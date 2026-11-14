package sqlstore

import (
	"context"
	"database/sql"
	"errors"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

type bots struct{ *queries }

// Bots returns the bots area: the `bots`, `webhooks` and `commands` tables.
func (q *queries) Bots() store.Bots { return bots{q} }

func (b bots) Create(ctx context.Context, info store.BotInfo) error {
	return b.atomic(ctx, func(q *queries) error {
		// A bots row exists only for an account whose role is bot
		// (docs/SCHEMA.md section 3), so the role is checked here, in
		// the transaction that makes the row.
		var role model.Role
		err := q.wr.QueryRowContext(ctx, `SELECT role FROM accounts WHERE id = ?`, string(info.AccountID)).Scan(str(&role))
		if err != nil {
			return q.mapErr(err)
		}
		if role != model.RoleBot {
			return model.ErrConflict.With("Only an account whose role is bot has a bot record.")
		}
		n, err := q.affected(q.wr.ExecContext(ctx, `
			INSERT INTO bots (account_id, description, created_by, webhook_only) VALUES (?, ?, ?, ?)
			ON CONFLICT (account_id) DO NOTHING`,
			string(info.AccountID), info.Description, id(info.CreatedBy), info.WebhookOnly))
		if err != nil {
			return err
		}
		if n == 0 {
			return model.ErrAlreadyExists
		}
		return nil
	})
}

func (b bots) Get(ctx context.Context, account model.AccountID) (store.BotInfo, bool, error) {
	var info store.BotInfo
	err := b.rd.QueryRowContext(ctx, `SELECT account_id, description, created_by, webhook_only FROM bots WHERE account_id = ?`,
		string(account)).Scan(str(&info.AccountID), &info.Description, str(&info.CreatedBy), flag(&info.WebhookOnly))
	if errors.Is(err, sql.ErrNoRows) {
		return store.BotInfo{}, false, nil
	}
	return info, err == nil, err
}

func (b bots) SetDescription(ctx context.Context, account model.AccountID, description string) error {
	return b.changed(b.wr.ExecContext(ctx, `UPDATE bots SET description = ? WHERE account_id = ?`, description, string(account)))
}

// botCursor is the sort key of the last bot of a page. Identifiers are
// UUIDv7, so their order is the order the bots were made in.
type botCursor struct {
	ID string `json:"i"`
}

func (b bots) List(ctx context.Context, webhooks bool, p store.Page) ([]store.BotInfo, string, error) {
	where := `a.removed_at IS NULL`
	var args []any
	if !webhooks {
		where += ` AND b.webhook_only = ?`
		args = append(args, false)
	}
	if p.Cursor != "" {
		var c botCursor
		if err := store.DecodeCursor(p.Cursor, &c); err != nil {
			return nil, "", err
		}
		if !model.ValidID(c.ID) {
			return nil, "", model.Invalid("cursor", "That cursor is not one this list gave out.")
		}
		where += ` AND b.account_id > ?`
		args = append(args, c.ID)
	}
	limit := p.Clamp()
	args = append(args, limit+1)
	rows, err := b.rd.QueryContext(ctx, `
		SELECT b.account_id, b.description, b.created_by, b.webhook_only
		FROM bots b JOIN accounts a ON a.id = b.account_id
		WHERE `+where+` ORDER BY b.account_id LIMIT ?`, args...)
	out, err := collect(rows, err, func(s scanner) (store.BotInfo, error) {
		var info store.BotInfo
		err := s.Scan(str(&info.AccountID), &info.Description, str(&info.CreatedBy), flag(&info.WebhookOnly))
		return info, err
	})
	if err != nil {
		return nil, "", err
	}
	next := ""
	if len(out) > limit {
		out = out[:limit]
		next = store.EncodeCursor(botCursor{ID: string(out[limit-1].AccountID)})
	}
	return out, next, nil
}

func (b bots) ChannelIDs(ctx context.Context, account model.AccountID) ([]model.ConversationID, error) {
	rows, err := b.rd.QueryContext(ctx, `
		SELECT c.id FROM members m JOIN conversations c ON c.id = m.conversation_id
		WHERE m.account_id = ? AND c.kind = ? AND c.name IS NOT NULL
		ORDER BY c.name`, string(account), string(model.KindChannel))
	return collectIDs[model.ConversationID](rows, err)
}

func (b bots) SetCommands(ctx context.Context, bot model.AccountID, commands []model.Command) error {
	return b.atomic(ctx, func(q *queries) error {
		// A duplicate would abort a PostgreSQL transaction, so another
		// bot's hold on a name is looked for first. The bot's own row is
		// locked for the length of the change, which makes two
		// replacements of one bot's list take turns.
		var locked string
		if err := q.wr.QueryRowContext(ctx, `SELECT account_id FROM bots WHERE account_id = ?`+q.forUpdate(), string(bot)).Scan(&locked); err != nil {
			return q.mapErr(err)
		}
		if len(commands) > 0 {
			args := []any{string(bot)}
			for _, c := range commands {
				args = append(args, c.Name)
			}
			var taken string
			err := q.wr.QueryRowContext(ctx, `SELECT name FROM commands WHERE bot_id <> ? AND name IN (`+marks(len(commands))+`) ORDER BY name LIMIT 1`,
				args...).Scan(&taken)
			if err == nil {
				return model.ErrNameTaken.With("The command /" + taken + " belongs to another bot.")
			}
			if !errors.Is(err, sql.ErrNoRows) {
				return err
			}
		}
		if _, err := q.wr.ExecContext(ctx, `DELETE FROM commands WHERE bot_id = ?`, string(bot)); err != nil {
			return err
		}
		for _, c := range commands {
			// Two bots registering the same new name at the same
			// moment can both pass the check above; the UNIQUE column
			// then stops the second, and its whole change is undone.
			if _, err := q.wr.ExecContext(ctx, `INSERT INTO commands (id, bot_id, name, description, hint) VALUES (?, ?, ?, ?, ?)`,
				model.NewID[string](), string(bot), c.Name, c.Description, c.Hint); err != nil {
				return q.taken(err)
			}
		}
		return nil
	})
}

const commandCols = `bot_id, name, description, hint`

func scanCommand(s scanner) (model.Command, error) {
	c := model.Command{Kind: model.CommandBot}
	err := s.Scan(str(&c.BotID), &c.Name, &c.Description, &c.Hint)
	return c, err
}

func (b bots) Commands(ctx context.Context, bot model.AccountID) ([]model.Command, error) {
	rows, err := b.rd.QueryContext(ctx, `SELECT `+commandCols+` FROM commands WHERE bot_id = ? ORDER BY name`, string(bot))
	return collect(rows, err, scanCommand)
}

func (b bots) CommandByName(ctx context.Context, name string) (model.Command, bool, error) {
	c, err := scanCommand(b.rd.QueryRowContext(ctx, `SELECT `+commandCols+` FROM commands WHERE name = ?`, name))
	if errors.Is(err, sql.ErrNoRows) {
		return model.Command{}, false, nil
	}
	return c, err == nil, err
}

func (b bots) CommandsIn(ctx context.Context, conv model.ConversationID) ([]model.Command, error) {
	rows, err := b.rd.QueryContext(ctx, `
		SELECT c.bot_id, c.name, c.description, c.hint
		FROM commands c JOIN members m ON m.account_id = c.bot_id
		WHERE m.conversation_id = ? ORDER BY c.name`, string(conv))
	return collect(rows, err, scanCommand)
}

func (b bots) CommunityReplyNamed(ctx context.Context, name string) (bool, error) {
	var one int
	err := b.rd.QueryRowContext(ctx, `SELECT 1 FROM saved_replies WHERE owner_id IS NULL AND name = ?`, name).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

const webhookCols = `id, bot_id, conversation_id, name, purpose, state, requested_by, requested_at, decided_by, decided_at,
	last_used_at, token_hash, signing_secret`

func scanWebhook(s scanner) (store.WebhookSecret, error) {
	var w store.WebhookSecret
	err := s.Scan(str(&w.ID), str(&w.BotID), str(&w.ConversationID), &w.Name, &w.Purpose, &w.State, str(&w.RequestedByID),
		tm(&w.RequestedAt), str(&w.DecidedByID), tm(&w.DecidedAt), tm(&w.LastUsedAt), blob(&w.TokenHash), blob(&w.SigningSecret))
	w.HasSecret = len(w.TokenHash) > 0
	return w, err
}

func plainWebhooks(in []store.WebhookSecret) []model.Webhook {
	out := make([]model.Webhook, len(in))
	for i, w := range in {
		out[i] = w.Webhook
	}
	return out
}

func (b bots) CreateWebhook(ctx context.Context, w *model.Webhook) error {
	if w.ID == "" {
		w.ID = model.NewID[model.WebhookID]()
	}
	if w.RequestedAt.IsZero() {
		w.RequestedAt = model.Now()
	}
	w.State = model.WebhookWaiting
	_, err := b.wr.ExecContext(ctx, `
		INSERT INTO webhooks (id, bot_id, conversation_id, name, purpose, requested_by, requested_at, state)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		string(w.ID), string(w.BotID), string(w.ConversationID), w.Name, w.Purpose, string(w.RequestedByID), int64(w.RequestedAt), w.State)
	return b.mapErr(err)
}

func (b bots) webhook(ctx context.Context, h execer, where string, args ...any) (store.WebhookSecret, error) {
	w, err := scanWebhook(h.QueryRowContext(ctx, `SELECT `+webhookCols+` FROM webhooks WHERE `+where, args...))
	return w, b.mapErr(err)
}

func (b bots) Webhook(ctx context.Context, id model.WebhookID) (model.Webhook, error) {
	w, err := b.webhook(ctx, b.rd, `id = ?`, string(id))
	return w.Webhook, err
}

func (b bots) WebhookByBot(ctx context.Context, bot model.AccountID) (model.Webhook, bool, error) {
	w, err := b.webhook(ctx, b.rd, `bot_id = ?`, string(bot))
	if errors.Is(err, model.ErrNotFound) {
		return model.Webhook{}, false, nil
	}
	return w.Webhook, err == nil, err
}

func (b bots) WebhookByTokenHash(ctx context.Context, tokenHash []byte) (store.WebhookSecret, error) {
	if len(tokenHash) == 0 {
		return store.WebhookSecret{}, model.ErrNotFound
	}
	return b.webhook(ctx, b.rd, `token_hash = ? AND state = ?`, tokenHash, model.WebhookApproved)
}

func (b bots) WebhooksOf(ctx context.Context, conv model.ConversationID) ([]model.Webhook, error) {
	rows, err := b.rd.QueryContext(ctx, `SELECT `+webhookCols+` FROM webhooks WHERE conversation_id = ? AND state IN (?, ?)
		ORDER BY requested_at, id`, string(conv), model.WebhookWaiting, model.WebhookApproved)
	out, err := collect(rows, err, scanWebhook)
	return plainWebhooks(out), err
}

// webhookCursor is the sort key of the last webhook of a page: newest
// first, with the id to break ties.
type webhookCursor struct {
	At int64  `json:"a"`
	ID string `json:"i"`
}

func (b bots) ListWebhooks(ctx context.Context, state string, p store.Page) ([]model.Webhook, string, error) {
	where := `1 = 1`
	var args []any
	if state != "" {
		where += ` AND state = ?`
		args = append(args, state)
	}
	if p.Cursor != "" {
		var c webhookCursor
		if err := store.DecodeCursor(p.Cursor, &c); err != nil {
			return nil, "", err
		}
		if !model.ValidID(c.ID) {
			return nil, "", model.Invalid("cursor", "That cursor is not one this list gave out.")
		}
		where += ` AND (requested_at < ? OR (requested_at = ? AND id < ?))`
		args = append(args, c.At, c.At, c.ID)
	}
	limit := p.Clamp()
	args = append(args, limit+1)
	rows, err := b.rd.QueryContext(ctx, `SELECT `+webhookCols+` FROM webhooks WHERE `+where+`
		ORDER BY requested_at DESC, id DESC LIMIT ?`, args...)
	got, err := collect(rows, err, scanWebhook)
	if err != nil {
		return nil, "", err
	}
	out := plainWebhooks(got)
	next := ""
	if len(out) > limit {
		out = out[:limit]
		last := out[limit-1]
		next = store.EncodeCursor(webhookCursor{At: int64(last.RequestedAt), ID: string(last.ID)})
	}
	return out, next, nil
}

func (b bots) CountWaiting(ctx context.Context, requester model.AccountID) (int, error) {
	var n int
	err := b.rd.QueryRowContext(ctx, `SELECT COUNT(*) FROM webhooks WHERE requested_by = ? AND state = ?`,
		string(requester), model.WebhookWaiting).Scan(&n)
	return n, err
}

func (b bots) DecideWebhook(ctx context.Context, wid model.WebhookID, state string, by model.AccountID, at model.Time) (model.Webhook, error) {
	var out model.Webhook
	err := b.atomic(ctx, func(q *queries) error {
		n, err := q.affected(q.wr.ExecContext(ctx, `UPDATE webhooks SET state = ?, decided_by = ?, decided_at = ? WHERE id = ? AND state = ?`,
			state, id(by), ms(at), string(wid), model.WebhookWaiting))
		if err != nil {
			return err
		}
		w, err := bots{q}.webhook(ctx, q.wr, `id = ?`, string(wid))
		if err != nil {
			return err
		}
		if n == 0 {
			return model.ErrConflict.With("That request has already been decided.")
		}
		out = w.Webhook
		return nil
	})
	return out, err
}

func (b bots) MarkDeclined(ctx context.Context, wid model.WebhookID) error {
	return b.changed(b.wr.ExecContext(ctx, `UPDATE webhooks SET state = ? WHERE id = ? AND decided_at IS NOT NULL AND state = ?`,
		model.WebhookDeclined, string(wid), model.WebhookRevoked))
}

func (b bots) SetWebhookSecret(ctx context.Context, wid model.WebhookID, tokenHash, sealedSigningSecret []byte, first bool) error {
	return b.atomic(ctx, func(q *queries) error {
		w, err := bots{q}.webhook(ctx, q.wr, `id = ?`+q.forUpdate(), string(wid))
		if err != nil {
			return err
		}
		if w.State != model.WebhookApproved {
			return model.ErrConflict.With("Only an approved webhook has an address.")
		}
		if first && w.HasSecret {
			return model.ErrConflict.With("The address has already been collected. Reset it to get a new one.")
		}
		_, err = q.wr.ExecContext(ctx, `UPDATE webhooks SET token_hash = ?, signing_secret = ? WHERE id = ?`,
			bin(tokenHash), bin(sealedSigningSecret), string(wid))
		return q.mapErr(err)
	})
}

func (b bots) RevokeWebhook(ctx context.Context, wid model.WebhookID) (model.Webhook, error) {
	var out model.Webhook
	err := b.atomic(ctx, func(q *queries) error {
		if _, err := q.wr.ExecContext(ctx, `UPDATE webhooks SET state = ?, token_hash = NULL, signing_secret = NULL
			WHERE id = ? AND state IN (?, ?)`, model.WebhookRevoked, string(wid), model.WebhookWaiting, model.WebhookApproved); err != nil {
			return err
		}
		w, err := bots{q}.webhook(ctx, q.wr, `id = ?`, string(wid))
		out = w.Webhook
		return err
	})
	return out, err
}

func (b bots) TouchWebhook(ctx context.Context, wid model.WebhookID, at model.Time) error {
	_, err := b.wr.ExecContext(ctx, `UPDATE webhooks SET last_used_at = ? WHERE id = ?`, int64(at), string(wid))
	return err
}

func (b bots) DeleteDeclinedWebhooks(ctx context.Context, before model.Time) (int64, error) {
	return b.affected(b.wr.ExecContext(ctx, `DELETE FROM webhooks WHERE state = ? AND decided_at < ?`, model.WebhookDeclined, int64(before)))
}

func (b bots) SetMessageImages(ctx context.Context, msg model.MessageID, cards []model.Card, viaIcon model.FileID) error {
	if viaIcon == "" {
		return b.changed(b.wr.ExecContext(ctx, `UPDATE messages SET cards = ? WHERE id = ?`, jsonList(cards), string(msg)))
	}
	return b.changed(b.wr.ExecContext(ctx, `UPDATE messages SET cards = ?, via_icon_file_id = ? WHERE id = ?`,
		jsonList(cards), string(viaIcon), string(msg)))
}
