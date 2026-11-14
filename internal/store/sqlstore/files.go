package sqlstore

import (
	"context"
	"sort"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

type files struct{ *queries }

// Files returns the files area.
func (q *queries) Files() store.Files { return files{q} }

// fileCols lists the columns of a files row, without an alias so the list
// also serves a RETURNING clause.
const fileCols = `id, conversation_id, message_id, uploader_id, purpose, position, name, caption, size, media_type, kind,
	width, height, duration_seconds, preview_text, sha256, state, blob_key, poster_key, thumb_key, detached_at, created_at`

// fileReady is the state of every file whose content is here.
const fileReady = "ready"

// filePurposeAttachment is the purpose of an upload meant for a message.
const filePurposeAttachment = "attachment"

func scanFile(s scanner) (model.File, error) {
	var f model.File
	err := s.Scan(str(&f.ID), str(&f.ConversationID), str(&f.MessageID), str(&f.UploaderID), &f.Purpose, num(&f.Position),
		&f.Name, &f.Caption, &f.Size, &f.MediaType, &f.Kind, num(&f.Width), num(&f.Height), num(&f.DurationSeconds),
		str(&f.PreviewText), blob(&f.SHA256), &f.State, str(&f.BlobKey), str(&f.PosterKey), str(&f.ThumbKey),
		tm(&f.DetachedAt), tm(&f.CreatedAt))
	if err != nil {
		return f, err
	}
	fileOut(&f)
	return f, nil
}

// fileOut puts a file in the shape it leaves the store in: addresses filled
// in, and no state when it is the ordinary one, because the API sends a state
// only when it is not `ready`.
func fileOut(f *model.File) {
	if f.State == fileReady {
		f.State = ""
	}
	f.FillURLs()
}

// sortFiles puts files in the order a message shows them.
func sortFiles(list []model.File) {
	sort.Slice(list, func(i, j int) bool {
		if list[i].Position != list[j].Position {
			return list[i].Position < list[j].Position
		}
		return list[i].ID < list[j].ID
	})
}

func (f files) Create(ctx context.Context, file *model.File) error {
	if file.ID == "" {
		file.ID = model.NewID[model.FileID]()
	}
	if file.CreatedAt.IsZero() {
		file.CreatedAt = model.Now()
	}
	if file.Purpose == "" {
		file.Purpose = filePurposeAttachment
	}
	state := file.State
	if state == "" {
		state = fileReady
	}
	// An upload is on no message until Attach says so.
	file.MessageID, file.DetachedAt = "", 0
	sum := file.SHA256
	if sum == nil {
		// The column is NOT NULL, and a nil slice would be sent as NULL.
		sum = []byte{}
	}
	_, err := f.wr.ExecContext(ctx, `
		INSERT INTO files (id, conversation_id, uploader_id, purpose, position, name, caption, size, media_type, kind,
			width, height, duration_seconds, preview_text, sha256, state, blob_key, poster_key, thumb_key, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		string(file.ID), string(file.ConversationID), string(file.UploaderID), file.Purpose, file.Position, file.Name,
		file.Caption, file.Size, file.MediaType, file.Kind, zeroNull(int64(file.Width)), zeroNull(int64(file.Height)),
		zeroNull(int64(file.DurationSeconds)), id(file.PreviewText), sum, state, id(file.BlobKey), id(file.PosterKey),
		id(file.ThumbKey), int64(file.CreatedAt))
	if err != nil {
		return f.mapErr(err)
	}
	file.State = state
	fileOut(file)
	return nil
}

func (f files) Get(ctx context.Context, file model.FileID) (model.File, error) {
	out, err := scanFile(f.rd.QueryRowContext(ctx, `SELECT `+fileCols+` FROM files WHERE id = ?`, string(file)))
	return out, f.mapErr(err)
}

func (f files) GetMany(ctx context.Context, ids []model.FileID) ([]model.File, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := f.rd.QueryContext(ctx,
		`SELECT `+fileCols+` FROM files WHERE id IN (`+marks(len(ids))+`) ORDER BY created_at, id`, anys(ids)...)
	return collect(rows, err, scanFile)
}

func (f files) Attach(ctx context.Context, msg model.MessageID, conv model.ConversationID, uploader model.AccountID, ids []model.FileID) error {
	if len(ids) == 0 {
		return nil
	}
	seen := make(map[model.FileID]bool, len(ids))
	for _, file := range ids {
		if seen[file] {
			// Named twice, it would already be on a message the
			// second time.
			return model.ErrNotFound
		}
		seen[file] = true
	}
	return f.atomic(ctx, func(q *queries) error {
		var in model.ConversationID
		err := q.wr.QueryRowContext(ctx, `SELECT conversation_id FROM messages WHERE id = ?`, string(msg)).Scan(str(&in))
		if err != nil {
			return q.mapErr(err)
		}
		if in != conv {
			return model.ErrNotFound
		}
		// Every file is checked before any is changed, so that a refusal
		// leaves nothing attached even when the caller's transaction
		// goes on.
		args := append(anys(ids), string(conv), string(uploader))
		ok, err := collectIDs[model.FileID](q.wr.QueryContext(ctx, `
			SELECT id FROM files WHERE id IN (`+marks(len(ids))+`) AND conversation_id = ? AND uploader_id = ?
				AND message_id IS NULL`+q.forUpdate(), args...))
		if err != nil {
			return err
		}
		if len(ok) != len(ids) {
			return model.ErrNotFound
		}
		// Files an edit adds go after the ones the message has, the
		// detached ones included, so history keeps its order too.
		var next int
		err = q.wr.QueryRowContext(ctx,
			`SELECT COALESCE(MAX(position) + 1, 0) FROM files WHERE message_id = ?`, string(msg)).Scan(&next)
		if err != nil {
			return err
		}
		for i, file := range ids {
			_, err := q.wr.ExecContext(ctx, `UPDATE files SET message_id = ?, position = ? WHERE id = ?`,
				string(msg), next+i, string(file))
			if err != nil {
				return q.mapErr(err)
			}
		}
		return nil
	})
}

func (f files) ForMessage(ctx context.Context, msg model.MessageID, includeDetached bool) ([]model.File, error) {
	// Only what was attached. A card's image also names the message, and
	// is the card's business (CardImages).
	where := "message_id = ? AND purpose = 'attachment'"
	if !includeDetached {
		where += " AND detached_at IS NULL"
	}
	rows, err := f.rd.QueryContext(ctx, `SELECT `+fileCols+` FROM files WHERE `+where+` ORDER BY position, id`, string(msg))
	return collect(rows, err, scanFile)
}

func (f files) Detach(ctx context.Context, msg model.MessageID, keep []model.FileID, at model.Time) ([]model.File, error) {
	if at.IsZero() {
		at = model.Now()
	}
	where := "message_id = ? AND detached_at IS NULL AND purpose = 'attachment'"
	args := []any{int64(at), string(msg)}
	if len(keep) > 0 {
		where += " AND id NOT IN (" + marks(len(keep)) + ")"
		args = append(args, anys(keep)...)
	}
	rows, err := f.wr.QueryContext(ctx, `UPDATE files SET detached_at = ? WHERE `+where+` RETURNING `+fileCols, args...)
	out, err := collect(rows, err, scanFile)
	if err != nil {
		return nil, f.mapErr(err)
	}
	sortFiles(out)
	return out, nil
}

func (f files) Delete(ctx context.Context, file model.FileID) (model.File, error) {
	out, err := scanFile(f.wr.QueryRowContext(ctx, `DELETE FROM files WHERE id = ? RETURNING `+fileCols, string(file)))
	return out, f.mapErr(err)
}

func (f files) Unattached(ctx context.Context, before model.Time, limit int) ([]model.File, error) {
	// Only uploads meant for a message. Avatars, emoji and the images of
	// cached link previews are on no message either, for good, and each
	// has an owner that deletes it; the janitor must never be handed one.
	rows, err := f.rd.QueryContext(ctx, `
		SELECT `+fileCols+` FROM files WHERE message_id IS NULL AND purpose = ? AND created_at < ?
		ORDER BY created_at, id LIMIT ?`, filePurposeAttachment, int64(before), store.Page{Limit: limit}.Clamp())
	return collect(rows, err, scanFile)
}

// deleteFiles deletes the file rows that match a condition and returns the
// keys of the blobs they owned, for the caller to remove from the file store.
func (q *queries) deleteFiles(ctx context.Context, where string, args ...any) ([]string, error) {
	rows, err := q.wr.QueryContext(ctx, `DELETE FROM files WHERE `+where+` RETURNING blob_key, thumb_key, poster_key`, args...)
	keys, err := collect(rows, err, func(s scanner) ([]string, error) {
		var f model.File
		err := s.Scan(str(&f.BlobKey), str(&f.ThumbKey), str(&f.PosterKey))
		return store.BlobKeys(f), err
	})
	if err != nil {
		return nil, q.mapErr(err)
	}
	var out []string
	for _, k := range keys {
		out = append(out, k...)
	}
	return out, nil
}
