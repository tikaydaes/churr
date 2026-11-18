package sqlstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

type modules struct{ *queries }

// Modules returns what the built-in modules keep.
func (q *queries) Modules() store.Modules { return modules{q} }

// moduleWhere is the condition that finds one key, by its scope: the owner
// column of the scope is compared only where the scope has one, so each
// scope's unique index is the one used.
func moduleWhere(k model.ModuleKey) (string, []any, error) {
	where, args := `module = ? AND scope = ? AND key = ?`, []any{k.Module, string(k.Scope), k.Key}
	switch k.Scope {
	case model.ModuleScopeInstance:
	case model.ModuleScopeConversation:
		where, args = where+` AND conversation_id = ?`, append(args, string(k.Conversation))
	case model.ModuleScopeAccount:
		where, args = where+` AND account_id = ?`, append(args, string(k.Account))
	default:
		return "", nil, fmt.Errorf("sqlstore: a module's value of scope %q", k.Scope)
	}
	return where, args, nil
}

func (m modules) Value(ctx context.Context, k model.ModuleKey) ([]byte, bool, error) {
	where, args, err := moduleWhere(k)
	if err != nil {
		return nil, false, err
	}
	var v []byte
	err = m.rd.QueryRowContext(ctx, `SELECT value FROM module_values WHERE `+where, args...).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, m.mapErr(err)
	}
	return v, true, nil
}

func (m modules) PutValue(ctx context.Context, k model.ModuleKey, value []byte, perConversation, perModule int64) error {
	where, args, err := moduleWhere(k)
	if err != nil {
		return err
	}
	return m.atomic(ctx, func(q *queries) error {
		// What the module holds without this key, read through the
		// writer so that two puts at once cannot both fit under a limit.
		var old int64
		if err := q.wr.QueryRowContext(ctx, `SELECT COALESCE(SUM(length(value)), 0) FROM module_values WHERE `+where, args...).Scan(&old); err != nil {
			return q.mapErr(err)
		}
		size := int64(len(value))
		if perModule > 0 {
			var all int64
			if err := q.wr.QueryRowContext(ctx, `SELECT COALESCE(SUM(length(value)), 0) FROM module_values WHERE module = ?`, k.Module).Scan(&all); err != nil {
				return q.mapErr(err)
			}
			if all-old+size > perModule {
				return model.ErrTooLarge.With("The " + k.Module + " module has no room left to keep this.")
			}
		}
		if perConversation > 0 && k.Scope == model.ModuleScopeConversation {
			var here int64
			if err := q.wr.QueryRowContext(ctx, `SELECT COALESCE(SUM(length(value)), 0) FROM module_values WHERE module = ? AND conversation_id = ?`,
				k.Module, string(k.Conversation)).Scan(&here); err != nil {
				return q.mapErr(err)
			}
			if here-old+size > perConversation {
				return model.ErrTooLarge.With("The " + k.Module + " module has no room left to keep this here.")
			}
		}
		res, err := q.wr.ExecContext(ctx, `UPDATE module_values SET value = ? WHERE `+where, append([]any{value}, args...)...)
		if err != nil {
			return q.mapErr(err)
		}
		if n, err := res.RowsAffected(); err != nil || n > 0 {
			return err
		}
		_, err = q.wr.ExecContext(ctx, `INSERT INTO module_values (module, scope, conversation_id, account_id, key, value) VALUES (?, ?, ?, ?, ?, ?)`,
			k.Module, string(k.Scope), id(k.Conversation), id(k.Account), k.Key, value)
		return q.mapErr(err)
	})
}

func (m modules) DeleteValue(ctx context.Context, k model.ModuleKey) error {
	where, args, err := moduleWhere(k)
	if err != nil {
		return err
	}
	_, err = m.wr.ExecContext(ctx, `DELETE FROM module_values WHERE `+where, args...)
	return m.mapErr(err)
}
