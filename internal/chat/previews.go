package chat

import (
	"context"
	"net/url"
	"strings"
	"time"

	"github.com/tikaydaes/churr/internal/blob"
	"github.com/tikaydaes/churr/internal/markdown"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/perm"
	"github.com/tikaydaes/churr/internal/preview"
	"github.com/tikaydaes/churr/internal/store"
)

// Previewer is what chat needs of the preview package: whether previews are
// on, and the card for an address. preview.Service implements it.
type Previewer interface {
	Enabled(ctx context.Context) bool
	Lookup(ctx context.Context, address string, by model.AccountID) (preview.Preview, error)
}

// SetPreviewer turns link previews on in the post path. Without one, no
// preview is ever looked for.
func (s *Service) SetPreviewer(p Previewer) { s.previews = p }

// fileStore is the whole file store, for the places chat reads and writes
// content: uploads, downloads, copies. The server always hands chat one;
// a test with a smaller fake gets nil, and those operations are unavailable.
func (s *Service) fileStore() blob.Blobs {
	b, _ := s.blobs.(blob.Blobs)
	return b
}

// messageLinks returns the links of a text in order, without repeats: bare
// addresses and Markdown links, http and https only (docs/FEATURES.md
// section 23.1).
func messageLinks(text string) []string {
	if !strings.Contains(text, "http") {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	var inlines func(list []markdown.Inline)
	inlines = func(list []markdown.Inline) {
		for _, in := range list {
			if in.Kind == markdown.Link || in.Kind == markdown.BareLink {
				u, err := url.Parse(in.URL)
				if err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" && !seen[in.URL] {
					seen[in.URL] = true
					out = append(out, in.URL)
				}
			}
			inlines(in.Children)
		}
	}
	var blocks func(list []markdown.Block)
	blocks = func(list []markdown.Block) {
		for _, b := range list {
			inlines(b.Inlines)
			blocks(b.Blocks)
			for _, it := range b.Items {
				inlines(it.Inlines)
				blocks(it.Lists)
			}
			for _, c := range b.Head {
				inlines(c.Inlines)
			}
			for _, row := range b.Rows {
				for _, c := range row {
					inlines(c.Inlines)
				}
			}
		}
	}
	blocks(markdown.Parse(text).Blocks)
	return out
}

// previewTimeout bounds the whole of one message's preview work: three
// pages and three images at five seconds each, and room to queue.
const previewTimeout = 90 * time.Second

// previewLinks looks for previews of a message's links after it was posted
// or edited, outside the request, so posting never waits for a fetch. On an
// edit only links that were not in the earlier text are looked up, so a
// preview someone removed does not come back.
func (s *Service) previewLinks(conv model.Conversation, m model.Message, earlier string) {
	if (s.previews == nil && s.themeLinks == nil) || conv.Sealed() || conv.IsLibrary() || m.Deleted() || m.IsSystem() {
		return
	}
	links := messageLinks(m.Text)
	if len(links) == 0 {
		return
	}
	had := map[string]bool{}
	for _, l := range messageLinks(earlier) {
		had[l] = true
	}
	for _, c := range m.Cards {
		if c.PreviewURL != "" {
			had[c.PreviewURL] = true
		}
	}
	// The first three links of the message are the ones that may have a
	// preview.
	if len(links) > model.MaxPreviewsPerPost {
		links = links[:model.MaxPreviewsPerPost]
	}
	var want []string
	for _, l := range links {
		if !had[l] && !s.ownMessageLink(l) {
			want = append(want, l)
		}
	}
	if len(want) == 0 {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), previewTimeout)
		defer cancel()
		// A link to one of this server's own themes becomes a theme card
		// with no fetch, so it does not wait for link previews to be on
		// (themecards.go). Every other link is looked up only if they are.
		fetch := s.previews != nil && s.previews.Enabled(ctx)
		type found struct {
			link string
			p    preview.Preview
		}
		var got []found
		for _, l := range want {
			if card, ok := s.themeCard(ctx, l); ok {
				got = append(got, found{l, preview.Preview{Card: &card}})
				continue
			}
			if !fetch {
				continue
			}
			p, err := s.previews.Lookup(ctx, l, m.AuthorID)
			if err != nil {
				s.log.Info("a link preview could not be looked up", "reason", err.Error())
				continue
			}
			if p.Card != nil {
				got = append(got, found{l, p})
			}
		}
		if len(got) == 0 {
			return
		}
		links := make([]string, len(got))
		previews := make([]preview.Preview, len(got))
		for i, g := range got {
			links[i], previews[i] = g.link, g.p
		}
		if err := s.addPreviews(ctx, m.ID, links, previews); err != nil {
			s.log.Warn("link previews could not be added to a message", "err", err)
		}
	}()
}

// copyFile makes a copy of a stored file's content, thumbnail and poster and
// returns a new row for it, not yet inserted. Each row owns its blobs
// (docs/SCHEMA.md decision S9), so a copy of a row is a copy of the bytes.
func (s *Service) copyFile(ctx context.Context, f model.File) (model.File, error) {
	blobs := s.fileStore()
	if blobs == nil {
		return model.File{}, model.ErrUnavailable
	}
	out := f
	out.ID, out.MessageID, out.DetachedAt = model.NewID[model.FileID](), "", 0
	out.BlobKey, out.ThumbKey, out.PosterKey = "", "", ""
	dup := func(key string) (string, error) {
		if key == "" {
			return "", nil
		}
		r, err := blobs.Get(ctx, key)
		if err != nil {
			return "", err
		}
		defer r.Close()
		copied, _, _, err := blobs.Put(ctx, r)
		return copied, err
	}
	var err error
	if out.BlobKey, err = dup(f.BlobKey); err == nil {
		if out.ThumbKey, err = dup(f.ThumbKey); err == nil {
			out.PosterKey, err = dup(f.PosterKey)
		}
	}
	if err != nil {
		s.dropBlobs(ctx, store.BlobKeys(out))
		return model.File{}, err
	}
	out.FillURLs()
	return out, nil
}

// dropBlobs removes blobs that ended up with no row.
func (s *Service) dropBlobs(ctx context.Context, keys []string) {
	for _, k := range keys {
		if err := s.blobs.Delete(context.WithoutCancel(ctx), k); err != nil {
			s.log.Warn("a blob could not be removed", "err", err)
		}
	}
}

// addPreviews appends preview cards to a message (section 23.5): each with
// preview_url set to the address it previews, and its image copied to a
// file of purpose `card` on the message. It stops at three previews and at
// five cards in all. This is not an edit: version, edited_at and history
// are untouched.
func (s *Service) addPreviews(ctx context.Context, id model.MessageID, links []string, previews []preview.Preview) error {
	m, err := s.store.Messages().Get(ctx, id)
	if err != nil {
		return hide(err)
	}
	// Images are copied before the transaction, which must not span the
	// file store.
	images := make([]*model.File, len(previews))
	var copied []string
	for i, p := range previews {
		if p.Image == nil {
			continue
		}
		f, err := s.copyFile(ctx, *p.Image)
		if err != nil {
			continue // the card goes without its image
		}
		f.ConversationID, f.MessageID, f.UploaderID, f.Purpose, f.CreatedAt = m.ConversationID, m.ID, m.AuthorID, model.PurposeCard, s.now()
		images[i] = &f
		copied = append(copied, store.BlobKeys(f)...)
	}
	used := map[string]bool{}
	err = s.Tx(ctx, func(tx store.Tx, rec *Recorder) error {
		// Read again: the message may have been edited or deleted while
		// the pages were fetched.
		cur, err := tx.Messages().Get(ctx, id)
		if err != nil {
			return err
		}
		if cur.Deleted() {
			return nil
		}
		still := map[string]bool{}
		for _, l := range messageLinks(cur.Text) {
			still[l] = true
		}
		have := 0
		for _, c := range cur.Cards {
			if c.PreviewURL != "" {
				have++
				still[c.PreviewURL] = false // already previewed
			}
		}
		cards := cur.Cards
		for i, p := range previews {
			if !still[links[i]] || have >= model.MaxPreviewsPerPost || len(cards) >= model.MaxCardsPerMessage {
				continue
			}
			card := *p.Card
			card.PreviewURL, card.ImageFileID = links[i], ""
			if img := images[i]; img != nil {
				if err := tx.Files().Insert(ctx, img); err != nil {
					return err
				}
				card.ImageFileID = img.ID
				for _, k := range store.BlobKeys(*img) {
					used[k] = true
				}
			}
			cards = append(cards, card)
			still[links[i]] = false
			have++
		}
		if len(cards) == len(cur.Cards) {
			return nil
		}
		updated, err := tx.Messages().SetCards(ctx, id, cards)
		if err != nil {
			return err
		}
		conv, err := tx.Conversations().Get(ctx, cur.ConversationID)
		if err != nil {
			return err
		}
		out, err := s.neutral(ctx, tx, conv, updated)
		if err != nil {
			return err
		}
		_, err = rec.Emit(ctx, model.Event{Type: model.EventPreviewAdded, ConversationID: cur.ConversationID, MessageID: id,
			Payload: map[string]any{"message": out}})
		return err
	})
	if err != nil {
		used = map[string]bool{}
	}
	var unused []string
	for _, k := range copied {
		if !used[k] {
			unused = append(unused, k)
		}
	}
	s.dropBlobs(ctx, unused)
	return err
}

// dropCardImage deletes the image file of a card that is being removed.
func (s *Service) dropCardImage(ctx context.Context, tx store.Tx, rec *Recorder, c model.Card) error {
	if c.ImageFileID == "" {
		return nil
	}
	f, err := tx.Files().Delete(ctx, c.ImageFileID)
	if err != nil {
		return hide404(err)
	}
	rec.RemoveBlobs(store.BlobKeys(f)...)
	return nil
}

// hide404 forgives a row that was already gone.
func hide404(err error) error {
	if model.CodeOf(err) == model.CodeNotFound {
		return nil
	}
	return err
}

// dropStalePreviews takes off, inside an edit's transaction, the preview
// cards whose link is no longer in the text (section 4.1, step 5).
func (s *Service) dropStalePreviews(ctx context.Context, tx store.Tx, rec *Recorder, m model.Message) (model.Message, error) {
	has := false
	for _, c := range m.Cards {
		has = has || c.PreviewURL != ""
	}
	if !has {
		return m, nil
	}
	still := map[string]bool{}
	for _, l := range messageLinks(m.Text) {
		still[l] = true
	}
	var keep []model.Card
	for _, c := range m.Cards {
		if c.PreviewURL == "" || still[c.PreviewURL] {
			keep = append(keep, c)
			continue
		}
		if err := s.dropCardImage(ctx, tx, rec, c); err != nil {
			return m, err
		}
	}
	if len(keep) == len(m.Cards) {
		return m, nil
	}
	return tx.Messages().SetCards(ctx, m.ID, keep)
}

// RemovePreview removes one link-preview card from a message, named by its
// position in `cards`; its image file is deleted. The message's author or
// an account acting as manager may.
func (s *Service) RemovePreview(ctx context.Context, actor model.Account, id model.MessageID, index int) (model.Message, error) {
	st, m, err := s.load(ctx, actor, id, perm.RemoveOwnPreview)
	if err != nil {
		return m, err
	}
	// Only a preview: a card a bot or webhook posted has no preview_url
	// and is not removed this way.
	if index < 0 || index >= len(m.Cards) || m.Cards[index].PreviewURL == "" {
		return m, model.ErrNotFound.With("That message has no link preview at that position.")
	}
	err = s.Tx(ctx, func(tx store.Tx, rec *Recorder) error {
		cur, err := tx.Messages().Get(ctx, id)
		if err != nil {
			return err
		}
		if index >= len(cur.Cards) || cur.Cards[index].PreviewURL != m.Cards[index].PreviewURL {
			return model.ErrConflict.With("The message changed. Look at it again.")
		}
		if err := s.dropCardImage(ctx, tx, rec, cur.Cards[index]); err != nil {
			return err
		}
		cards := append(append([]model.Card{}, cur.Cards[:index]...), cur.Cards[index+1:]...)
		updated, err := tx.Messages().SetCards(ctx, id, cards)
		if err != nil {
			return err
		}
		out, err := s.neutral(ctx, tx, st.Conversation, updated)
		if err != nil {
			return err
		}
		_, err = rec.Emit(ctx, model.Event{Type: model.EventPreviewRemoved, ConversationID: m.ConversationID, MessageID: id,
			Payload: map[string]any{"message": out}})
		return err
	})
	if err != nil {
		return m, err
	}
	return s.Message(ctx, actor, id)
}

// serverCardOnly refuses, in cards a client or bot posts, the fields only
// the server sets: preview_url (a link preview) and theme (a theme card).
// Otherwise anyone could post a card that removes, and draws, as if the
// server had made it.
func serverCardOnly(c model.Card) error {
	if c.PreviewURL != "" || c.Theme != nil {
		return model.Invalid("cards", "Only the server sets preview_url and theme on a card.")
	}
	// A shield names outcomes of the draw; whether they check out is each
	// reader's to work out (docs/DESIGN-FAIR.md section 13).
	if c.FairMark != nil && !c.FairMark.Valid() {
		return model.Invalid("cards", "A card's fair_mark names at most 20 objects of the message's draw, each 0 to 99.")
	}
	return nil
}

// keepServerCards checks the cards an edit gives. A link preview or theme
// card may be kept by sending it back, and is then kept exactly as the
// server made it; one the message does not already carry is refused.
func keepServerCards(given, had []model.Card) ([]model.Card, error) {
	out := make([]model.Card, len(given))
	for i, c := range given {
		out[i] = c
		if serverCardOnly(c) == nil {
			continue
		}
		kept := false
		for _, h := range had {
			if h.PreviewURL != "" && h.PreviewURL == c.PreviewURL {
				out[i], kept = h, true
				break
			}
		}
		if !kept {
			return nil, serverCardOnly(c)
		}
	}
	return out, nil
}
