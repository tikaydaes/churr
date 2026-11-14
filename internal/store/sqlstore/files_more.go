package sqlstore

import (
	"context"

	"github.com/tikaydaes/churr/internal/model"
)

func (f files) Insert(ctx context.Context, file *model.File) error {
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
	sum := file.SHA256
	if sum == nil {
		sum = []byte{}
	}
	_, err := f.wr.ExecContext(ctx, `
		INSERT INTO files (id, conversation_id, message_id, uploader_id, purpose, position, name, caption, size, media_type, kind,
			width, height, duration_seconds, preview_text, sha256, state, blob_key, poster_key, thumb_key, detached_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		string(file.ID), string(file.ConversationID), id(file.MessageID), string(file.UploaderID), file.Purpose, file.Position,
		file.Name, file.Caption, file.Size, file.MediaType, file.Kind, zeroNull(int64(file.Width)), zeroNull(int64(file.Height)),
		zeroNull(int64(file.DurationSeconds)), id(file.PreviewText), sum, state, id(file.BlobKey), id(file.PosterKey),
		id(file.ThumbKey), ms(file.DetachedAt), int64(file.CreatedAt))
	if err != nil {
		return f.mapErr(err)
	}
	file.State = state
	fileOut(file)
	return nil
}

func (f files) CardImages(ctx context.Context, msg model.MessageID) ([]model.File, error) {
	rows, err := f.rd.QueryContext(ctx,
		`SELECT `+fileCols+` FROM files WHERE message_id = ? AND purpose = 'card' ORDER BY created_at, id`, string(msg))
	return collect(rows, err, scanFile)
}

func (f files) KeyKnown(ctx context.Context, key string) (bool, error) {
	var n int
	err := f.rd.QueryRowContext(ctx,
		`SELECT count(*) FROM files WHERE blob_key = ? OR thumb_key = ? OR poster_key = ?`, key, key, key).Scan(&n)
	return n > 0, err
}

func (f files) Totals(ctx context.Context) (int64, int64, error) {
	var count, bytes int64
	err := f.rd.QueryRowContext(ctx, `SELECT count(*), COALESCE(sum(size), 0) FROM files`).Scan(&count, &bytes)
	return count, bytes, err
}

// cardFileIDs lists the files a message's cards name: each card's image and
// author icon, and the icon of its via label.
func cardFileIDs(m model.Message, more ...[]model.Card) []model.FileID {
	var ids []model.FileID
	add := func(cards []model.Card) {
		for _, c := range cards {
			if c.ImageFileID != "" {
				ids = append(ids, c.ImageFileID)
			}
			if c.Author != nil && c.Author.IconFileID != "" {
				ids = append(ids, c.Author.IconFileID)
			}
		}
	}
	add(m.Cards)
	for _, cards := range more {
		add(cards)
	}
	if m.Via != nil && m.Via.IconFileID != "" {
		ids = append(ids, m.Via.IconFileID)
	}
	return ids
}

// deleteLooseCardFiles deletes the card images of a message that are on no
// message row: a webhook's card images and icons are stored that way, and
// nothing but the cards that name them ties them to the message. It looks
// at the cards the message has now and the ones its earlier versions kept,
// so it must run before those versions are deleted. Files of purpose `card`
// that do name a message go with the rest of that message's files.
func (q *queries) deleteLooseCardFiles(ctx context.Context, m model.Message) ([]string, error) {
	rows, err := q.wr.QueryContext(ctx, `SELECT cards FROM message_versions WHERE message_id = ?`, string(m.ID))
	versions, err := collect(rows, err, func(s scanner) ([]model.Card, error) {
		var cards []model.Card
		err := s.Scan(js(&cards))
		return cards, err
	})
	if err != nil {
		return nil, err
	}
	ids := cardFileIDs(m, versions...)
	if len(ids) == 0 {
		return nil, nil
	}
	args := append(anys(ids), string(m.ConversationID))
	return q.deleteFiles(ctx, `id IN (`+marks(len(ids))+`) AND purpose = 'card' AND message_id IS NULL AND conversation_id = ?`, args...)
}
