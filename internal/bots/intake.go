package bots

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/tikaydaes/churr/internal/blob"
	"github.com/tikaydaes/churr/internal/chat"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/perm"
	"github.com/tikaydaes/churr/internal/ratelimit"
	"github.com/tikaydaes/churr/internal/secret"
	"github.com/tikaydaes/churr/internal/store"
)

// Posting through a webhook's address (docs/BOTS.md sections 9 to 11). The
// four addresses differ only in the shape they accept; each ends in Post
// with a Payload.

// MaxWebhookBody is the largest request a webhook address accepts, files
// included.
const MaxWebhookBody = 1 << 20

// Part is one file part of a multipart request to a webhook address.
type Part struct {
	Filename string
	Data     []byte
}

// Hook is a webhook found by the token in its address, ready to post
// through.
type Hook struct {
	w store.WebhookSecret
}

// ID is the webhook's identifier.
func (h Hook) ID() model.WebhookID { return h.w.ID }

// Find returns the webhook whose address holds this token. An unknown,
// revoked or uncollected token is not found, and so is anything that is not
// a webhook token at all: no other credential works at these addresses.
func (s *Service) Find(ctx context.Context, token string) (Hook, error) {
	if secret.TokenPrefix(token) != secret.PrefixHook {
		return Hook{}, model.ErrNotFound
	}
	hash := secret.HashToken(token)
	w, err := s.store.Bots().WebhookByTokenHash(ctx, hash)
	if err != nil {
		return Hook{}, notFound(err)
	}
	// The row was found by its hash; comparing the two again in constant
	// time means the answer does not depend on how the database compares
	// byte strings.
	if subtle.ConstantTimeCompare(hash, w.TokenHash) != 1 || w.State != model.WebhookApproved {
		return Hook{}, model.ErrNotFound
	}
	return Hook{w: w}, nil
}

// TakePost counts one post against a webhook's limit of 60 a minute.
func (s *Service) TakePost(h Hook) ratelimit.Result {
	return s.limits.webhookPosts.Take(string(h.w.ID))
}

// VerifySignature checks GitHub's X-Hub-Signature-256 header against the
// webhook's own signing secret: HMAC-SHA-256 over the raw body, exactly as
// received, keyed with the bytes of the secret's base64url string, which is
// what was typed into GitHub. A missing header, a header in another form or
// a value that does not match is unauthenticated.
func (s *Service) VerifySignature(h Hook, header string, body []byte) error {
	refused := model.ErrUnauthenticated.With("The signature is missing or does not match this webhook's signing secret.")
	sent, ok := strings.CutPrefix(strings.TrimSpace(header), "sha256=")
	if !ok {
		return refused
	}
	want, err := hex.DecodeString(sent)
	if err != nil || len(want) != sha256.Size || len(h.w.SigningSecret) == 0 {
		return refused
	}
	key, err := s.keys.Unseal(h.w.SigningSecret, "webhooks", "signing_secret", string(h.w.ID))
	if err != nil {
		return err
	}
	mac := hmac.New(sha256.New, key)
	mac.Write(body)
	if !hmac.Equal(mac.Sum(nil), want) {
		return refused
	}
	return nil
}

// Posted names the message a webhook posted, and nothing else about the
// channel: a webhook reads nothing.
type Posted struct {
	ID        model.MessageID `json:"id"`
	Seq       int64           `json:"seq"`
	CreatedAt model.Time      `json:"created_at"`
}

// imageTypes are what a card image or an icon may be, by content.
var imageTypes = map[string]bool{blob.TypePNG: true, blob.TypeJPEG: true, blob.TypeGIF: true, blob.TypeWebP: true}

// isImage reports whether bytes are a PNG, JPEG, GIF or WebP image.
func isImage(data []byte) bool {
	mediaType, _ := blob.Sniff(data[:min(len(data), 512)])
	return imageTypes[mediaType]
}

// slot is one place in a message an image goes: a card's image, a card
// author's icon, or the icon of the via label.
type slot struct {
	card   int  // index into the cards; -1 for the via icon
	author bool // the card author's icon rather than the card's image
	part   string
	url    string
}

// Post posts a payload through a webhook. parts are the file parts that came
// with it, by name; one the payload does not name is refused.
//
// The message's author is the webhook's own account and it is marked `via`
// a webhook, under the name the payload gives or else the webhook's own.
// Text is resolved and mentions notify as for any member; mentions inside
// cards never do.
func (s *Service) Post(ctx context.Context, h Hook, p Payload, parts map[string]Part) (Posted, error) {
	w := h.w
	acc, err := s.store.Accounts().Get(ctx, w.BotID)
	if err != nil || !acc.Active() {
		return Posted{}, model.ErrNotFound
	}
	st, err := s.perm.Load(ctx, acc, w.ConversationID)
	if err != nil {
		return Posted{}, notFound(err)
	}
	if !st.IsMember() {
		// Removing the webhook's account from the channel is the same as
		// revoking the webhook.
		err := s.chat.Tx(ctx, func(tx store.Tx, rec *chat.Recorder) error {
			return s.revokeTx(ctx, tx, rec, acc, w.Webhook, "its account is no longer in the channel")
		})
		if err != nil {
			return Posted{}, err
		}
		return Posted{}, model.ErrNotFound
	}
	if err := perm.Check(st, perm.Post); err != nil {
		return Posted{}, err
	}

	// Everything that can be refused is checked before anything is
	// stored.
	if utf8.RuneCountInString(p.Text) > model.MaxMessageText {
		return Posted{}, model.Invalid("text", "A message is at most 16,000 characters.")
	}
	if len(p.Cards) > model.MaxCardsPerMessage {
		return Posted{}, model.Invalid("cards", "A message carries at most five cards.")
	}
	if len(p.Files) > model.MaxFilesPerMessage {
		return Posted{}, model.Invalid("files", "A message carries at most ten files.")
	}
	in := chat.NewMessage{ConversationID: w.ConversationID, Text: p.Text, ClientID: p.ClientID}
	if p.ClientID != "" && !model.ValidID(p.ClientID) {
		return Posted{}, model.Invalid("client_id", "client_id must be a UUID.")
	}
	if p.ParentID != "" {
		if !model.ValidID(p.ParentID) {
			return Posted{}, model.Invalid("parent_id", "parent_id is not an identifier.")
		}
		in.ParentID = model.MessageID(p.ParentID)
	}
	if p.Form != nil {
		if err := checkWebhookForm(*p.Form); err != nil {
			return Posted{}, err
		}
		in.Form = p.Form
	}
	via := model.Via{Kind: "webhook", Name: w.Name}
	if p.Via != nil && p.Via.Name != "" {
		name, ok := model.CleanDisplayName(p.Via.Name)
		if !ok {
			return Posted{}, model.Invalid("via.name", "A via name is 1 to 64 characters with no control characters.")
		}
		via.Name = name
	}

	// Which parts are used, and where the images go.
	used := map[string]bool{}
	var slots []slot
	place := func(field string, sl slot) error {
		switch {
		case sl.part != "" && sl.url != "":
			return model.Invalid(field, "Give an image as an address or as a part, not both.")
		case sl.part != "":
			part, ok := parts[sl.part]
			if !ok {
				return model.Invalid(field, "The request has no part named "+sl.part+".")
			}
			if !isImage(part.Data) {
				return model.Invalid(field, "A card image or icon must be a PNG, JPEG, GIF or WebP image.")
			}
			used[sl.part] = true
		case sl.url != "":
			if webAddress(sl.url) == "" {
				return model.Invalid(field, "An image address must be an http or https address.")
			}
		default:
			return nil
		}
		slots = append(slots, sl)
		return nil
	}
	cards := make([]model.Card, len(p.Cards))
	for i, c := range p.Cards {
		cards[i] = model.Card{Color: c.Color, Title: c.Title, URL: c.URL, Text: c.Text, Fields: c.Fields, Footer: c.Footer, At: c.At}
		if c.Author != nil {
			cards[i].Author = &model.CardAuthor{Name: c.Author.Name}
			if err := place("cards", slot{card: i, author: true, part: c.Author.IconPart, url: c.Author.IconURL}); err != nil {
				return Posted{}, err
			}
		}
		if err := place("cards", slot{card: i, part: c.ImagePart, url: c.ImageURL}); err != nil {
			return Posted{}, err
		}
		// The image is not there yet, so the rule that a card needs a
		// title, text or image is applied to what the payload said.
		if err := checkCardShape(cards[i], c.ImagePart != "" || c.ImageURL != ""); err != nil {
			return Posted{}, err
		}
	}
	if p.Via != nil {
		if err := place("via", slot{card: -1, part: p.Via.IconPart, url: p.Via.IconURL}); err != nil {
			return Posted{}, err
		}
	}
	for _, f := range p.Files {
		part, ok := parts[f.Part]
		switch {
		case !ok:
			return Posted{}, model.Invalid("files", "The request has no part named "+f.Part+".")
		case used[f.Part]:
			return Posted{}, model.Invalid("files", "The part "+f.Part+" is used twice.")
		case len(part.Data) == 0:
			return Posted{}, model.Invalid("files", "The part "+f.Part+" is empty.")
		}
		used[f.Part] = true
	}
	for name := range parts {
		if !used[name] {
			return Posted{}, model.BadRequest("The part " + name + " is not used by the payload.")
		}
	}

	// Images given by address are fetched after the message is posted,
	// under the link-preview rules. With link previews off nothing can be
	// fetched, and that is known now.
	fetcher := s.imageFetcher()
	fetching := fetcher != nil && fetcher.Enabled(ctx)
	var later []slot
	if !fetching {
		kept := slots[:0]
		for _, sl := range slots {
			if sl.url == "" {
				kept = append(kept, sl)
				continue
			}
			dropImage(cards, sl)
		}
		slots = kept
	}
	// A card that lost its image and has no title or text goes; one that
	// is still waiting for an image stays.
	waiting := map[int]bool{}
	for _, sl := range slots {
		if sl.card >= 0 && !sl.author {
			waiting[sl.card] = true
		}
	}
	cards, slots = dropEmptyCards(cards, slots, waiting)
	if p.Text == "" && len(cards) == 0 && p.Form == nil && len(p.Files) == 0 {
		return Posted{}, model.BadRequest("A message needs text, a card, a form or a file.")
	}

	// Posting the same client_id again returns the first message and
	// posts nothing, so nothing is stored for it either.
	if p.ClientID != "" {
		old, found, err := s.store.Messages().ByClientID(ctx, w.ConversationID, acc.ID, p.ClientID)
		if err != nil {
			return Posted{}, err
		}
		if found && s.now().Sub(old.CreatedAt) <= 24*time.Hour {
			return Posted{ID: old.ID, Seq: old.Seq, CreatedAt: old.CreatedAt}, nil
		}
	}

	// Parts become files: card images and icons now, attachments with
	// the message.
	var stored []model.File
	undo := func() {
		for _, f := range stored {
			s.discard(ctx, f)
		}
	}
	for _, sl := range slots {
		if sl.part == "" {
			later = append(later, sl)
			continue
		}
		f, err := s.save(ctx, newFile{conv: w.ConversationID, uploader: acc.ID, name: parts[sl.part].Filename, purpose: "card", data: parts[sl.part].Data})
		if err != nil {
			undo()
			return Posted{}, err
		}
		stored = append(stored, f)
		setImage(cards, &via, sl, f.ID)
	}

	// The message is posted at once, without the images still to be
	// fetched. A card that is nothing but such an image cannot be posted
	// yet and joins the message when its image arrives. If the message
	// would then hold nothing at all, the images are fetched first, so
	// that a message left with nothing is not posted.
	now := make([]model.Card, 0, len(cards))
	for _, c := range cards {
		if c.Title != "" || c.Text != "" || c.ImageFileID != "" {
			now = append(now, c)
		}
	}
	if len(later) > 0 && p.Text == "" && len(now) == 0 && p.Form == nil && len(p.Files) == 0 {
		stored = append(stored, s.fetchInto(ctx, fetcher, w.Webhook, acc, "", cards, &via, later)...)
		cards, _ = dropEmptyCards(cards, nil, nil)
		now, later = cards, nil
		if len(now) == 0 {
			undo()
			return Posted{}, model.BadRequest("None of the images could be fetched, and the message has nothing else.")
		}
	}
	for _, ref := range p.Files {
		part := parts[ref.Part]
		name := ref.Name
		if name == "" {
			name = part.Filename
		}
		f, err := s.save(ctx, newFile{conv: w.ConversationID, uploader: acc.ID, name: name, caption: ref.Caption, purpose: "attachment", data: part.Data})
		if err != nil {
			undo()
			return Posted{}, err
		}
		stored = append(stored, f)
		in.FileIDs = append(in.FileIDs, f.ID)
	}
	// What is posted now and what the fetch works on afterwards must not
	// share anything that changes.
	full := copyCards(cards)
	posting := via
	in.Cards, in.Via = now, &posting
	posted, err := s.chat.Post(ctx, acc, in)
	if err != nil {
		undo()
		return Posted{}, err
	}
	m := posted.Message
	if !posted.Created {
		// The same request arrived twice at once and the other one won.
		undo()
	} else {
		// The images of the cards were stored before the message had an
		// identifier. They are its files from here on.
		s.adopt(ctx, m.ID, stored)
	}
	if err := s.store.Bots().TouchWebhook(ctx, w.ID, s.now()); err != nil {
		s.log.Warn("a webhook's last use could not be recorded", "err", err)
	}
	if posted.Created && len(later) > 0 {
		s.background.Add(1)
		go func() {
			defer s.background.Done()
			ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Minute)
			defer cancel()
			s.fetchLater(ctx, fetcher, w.Webhook, acc, m.ID, full, via, later)
		}()
	}
	return Posted{ID: m.ID, Seq: m.Seq, CreatedAt: m.CreatedAt}, nil
}

// copyCards copies cards deeply enough that changing an author's icon in
// the copy leaves the original alone.
func copyCards(cards []model.Card) []model.Card {
	out := make([]model.Card, len(cards))
	for i, c := range cards {
		if c.Author != nil {
			a := *c.Author
			c.Author = &a
		}
		out[i] = c
	}
	return out
}

// checkWebhookForm applies what a webhook's form may be: the actions tally,
// collect and thread. A webhook has no connection to receive answers on, so
// `bot` is refused; it is never a manager or admin, so `channel`, `group`
// and answerer_group_id are refused. That only `tally` is allowed in a
// channel shared with other servers is perm's rule, applied when the form
// is posted.
func checkWebhookForm(f model.FormInput) error {
	switch f.Action.Type {
	case model.FormTally, model.FormCollect, model.FormThread:
	default:
		return model.Invalid("form.action.type", "A webhook's form may use the actions tally, collect and thread.")
	}
	if f.AnswererGroupID != "" {
		return model.Invalid("form.answerer_group_id", "A webhook cannot limit who answers its form.")
	}
	return nil
}

// setImage puts a stored image where its slot says.
func setImage(cards []model.Card, via *model.Via, sl slot, id model.FileID) {
	switch {
	case sl.card < 0:
		via.IconFileID = id
	case sl.author:
		if cards[sl.card].Author != nil {
			cards[sl.card].Author.IconFileID = id
		}
	default:
		cards[sl.card].ImageFileID = id
	}
}

// dropImage is what happens to an image that cannot be had: a card that
// lost its image gets the address appended to its footer, and a dropped icon
// leaves nothing behind.
func dropImage(cards []model.Card, sl slot) {
	if sl.card < 0 || sl.author || sl.url == "" {
		return
	}
	c := &cards[sl.card]
	if c.Footer == "" {
		c.Footer = sl.url
	} else {
		c.Footer += " · " + sl.url
	}
}

// dropEmptyCards removes the cards left with no title, text or image, and
// renumbers the slots that point at the cards after them. waiting names the
// cards whose image is still to come.
func dropEmptyCards(cards []model.Card, slots []slot, waiting map[int]bool) ([]model.Card, []slot) {
	moved := map[int]int{}
	kept := make([]model.Card, 0, len(cards))
	for i, c := range cards {
		if c.Title == "" && c.Text == "" && c.ImageFileID == "" && !waiting[i] {
			continue
		}
		moved[i] = len(kept)
		kept = append(kept, c)
	}
	out := slots[:0]
	for _, sl := range slots {
		if sl.card >= 0 {
			to, ok := moved[sl.card]
			if !ok {
				continue
			}
			sl.card = to
		}
		out = append(out, sl)
	}
	return kept, out
}

// fetchedImage is an image remembered after it was fetched.
type fetchedImage struct {
	data []byte
	at   time.Time
}

const (
	fetchedFor = 24 * time.Hour
	// Only small images are remembered, and not many of them: the point
	// is the icon a webhook sends with every post.
	fetchedMaxBytes = 256 << 10
	fetchedMaxCount = 256
)

// fetch returns the image at an address, fetched once a day at most. Each
// real fetch is counted against the webhook.
func (s *Service) fetch(ctx context.Context, fetcher Images, hook model.WebhookID, address string) ([]byte, error) {
	key := sha256.Sum256([]byte(address))
	now := time.Now()
	s.fetchedMu.Lock()
	got, ok := s.fetched[key]
	s.fetchedMu.Unlock()
	if ok && now.Sub(got.at) < fetchedFor {
		return got.data, nil
	}
	if !s.limits.imageFetchs.Take(string(hook)).OK {
		return nil, errors.New("this webhook has fetched too many images in the last minute")
	}
	data, err := fetcher.FetchImage(ctx, address)
	if err != nil {
		return nil, err
	}
	if !isImage(data) {
		return nil, errors.New("the address did not give a PNG, JPEG, GIF or WebP image")
	}
	if len(data) <= fetchedMaxBytes {
		s.fetchedMu.Lock()
		if len(s.fetched) >= fetchedMaxCount {
			for k, v := range s.fetched {
				if now.Sub(v.at) >= fetchedFor || len(s.fetched) >= fetchedMaxCount {
					delete(s.fetched, k)
				}
			}
		}
		s.fetched[key] = fetchedImage{data: data, at: now}
		s.fetchedMu.Unlock()
	}
	return data, nil
}

// fetchInto fetches the image of each slot, stores it as a file with
// purpose `card` and puts it in its place. An image that cannot be had is
// dropped as dropImage says. It returns the files it stored. msg is the
// message the images belong to, or "" when it has not been posted yet.
func (s *Service) fetchInto(ctx context.Context, fetcher Images, w model.Webhook, acc model.Account, msg model.MessageID, cards []model.Card, via *model.Via, slots []slot) []model.File {
	var stored []model.File
	for _, sl := range slots {
		data, err := s.fetch(ctx, fetcher, w.ID, sl.url)
		var f model.File
		if err == nil {
			f, err = s.save(ctx, newFile{conv: w.ConversationID, msg: msg, uploader: acc.ID, name: "image", purpose: "card", data: data})
		}
		if err != nil {
			s.log.Info("an image a webhook named could not be fetched", "webhook", w.ID, "err", err)
			dropImage(cards, sl)
			continue
		}
		stored = append(stored, f)
		setImage(cards, via, sl, f.ID)
	}
	return stored
}

// fetchLater runs after a webhook's message was posted without the images
// it named by address: each is fetched once and added to the message with
// the event that adds a link preview. That is not an edit and leaves no
// history. cards are the message's cards as the payload meant them, those
// held back for want of an image included.
func (s *Service) fetchLater(ctx context.Context, fetcher Images, w model.Webhook, acc model.Account, msg model.MessageID, cards []model.Card, via model.Via, slots []slot) {
	stored := s.fetchInto(ctx, fetcher, w, acc, msg, cards, &via, slots)
	// A card left with no title, text or image is removed.
	cards, _ = dropEmptyCards(cards, nil, nil)
	err := s.chat.Tx(ctx, func(tx store.Tx, rec *chat.Recorder) error {
		m, err := tx.Messages().Get(ctx, msg)
		if err != nil {
			return err
		}
		if m.Deleted() {
			return model.ErrNotFound
		}
		// Link previews the server added in the meantime are kept.
		for _, c := range m.Cards {
			if c.PreviewURL != "" {
				cards = append(cards, c)
			}
		}
		if err := tx.Bots().SetMessageImages(ctx, msg, cards, via.IconFileID); err != nil {
			return err
		}
		_, err = s.chat.EmitMessageChange(ctx, tx, rec, eventPreviewAdded, msg)
		return err
	})
	if err != nil {
		for _, f := range stored {
			s.discard(ctx, f)
		}
		if model.CodeOf(err) != model.CodeNotFound {
			s.log.Warn("fetched images could not be added to a webhook's message", "err", err)
		}
	}
}

// newFile is bytes about to become a file of a conversation.
type newFile struct {
	conv model.ConversationID
	// msg is the message a card's image belongs to, when it exists
	// already. The row is then stored as that message's file, so deleting,
	// purging or expiring the message removes the image with it.
	msg      model.MessageID
	uploader model.AccountID
	name     string
	caption  string
	purpose  string // attachment | card
	data     []byte
}

const maxFileName = 255

// cleanFileName makes a part's file name safe to show and to put in an
// address: no path, no control characters, not empty.
func cleanFileName(name string) string {
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		name = name[i+1:]
	}
	name = strings.TrimSpace(strings.Map(func(r rune) rune {
		if r < ' ' || r == 0x7f {
			return -1
		}
		return r
	}, name))
	if name == "" || name == "." || name == ".." {
		return "file"
	}
	return truncate(name, maxFileName)
}

// save stores bytes a webhook sent as a file: the content in the file
// store, and the row beside it. Kind and media type are decided from the
// content, never from the name, and an image gets its size and a thumbnail.
func (s *Service) save(ctx context.Context, in newFile) (model.File, error) {
	if s.blobs == nil {
		return model.File{}, model.ErrUnavailable.With("This server cannot store files.")
	}
	if utf8.RuneCountInString(in.caption) > 1000 {
		return model.File{}, model.Invalid("files", "A caption is at most 1,000 characters.")
	}
	mediaType, kind := blob.Sniff(in.data[:min(len(in.data), 512)])
	f := model.File{
		ConversationID: in.conv, UploaderID: in.uploader, Purpose: in.purpose, Name: cleanFileName(in.name), Caption: in.caption,
		MediaType: mediaType, Kind: kind, CreatedAt: s.now(),
	}
	f.Width, f.Height, f.DurationSeconds = blob.Probe(bytes.NewReader(in.data))
	if kind == model.FileText {
		if text, err := blob.PreviewText(bytes.NewReader(in.data)); err == nil {
			f.PreviewText = text
		}
	}
	var err error
	if f.BlobKey, f.Size, f.SHA256, err = s.blobs.Put(ctx, bytes.NewReader(in.data)); err != nil {
		return model.File{}, err
	}
	if kind == model.FileImage {
		// An image that cannot be decoded simply has no thumbnail.
		if thumb, err := blob.MakeThumbnail(bytes.NewReader(in.data)); err == nil {
			if key, _, _, err := s.blobs.Put(ctx, bytes.NewReader(thumb.Data)); err == nil {
				f.ThumbKey = key
			}
		}
	}
	if in.msg != "" && in.purpose == "card" {
		f.MessageID = in.msg
		err = s.store.Files().Insert(ctx, &f)
	} else {
		err = s.store.Files().Create(ctx, &f)
	}
	if err != nil {
		s.discard(ctx, f)
		return model.File{}, err
	}
	return f, nil
}

// adopt makes the card images stored before a message was posted that
// message's files. A row of purpose `card` with no message is reached by
// nothing that deletes a message, so its image would outlive the message
// for good; with the message's identifier, deletion, purge and retention
// remove it as they remove any file of the message. The row is put back
// whole, through the store's own Insert, in one transaction.
func (s *Service) adopt(ctx context.Context, msg model.MessageID, files []model.File) {
	var cards []model.File
	for _, f := range files {
		if f.Purpose == "card" {
			cards = append(cards, f)
		}
	}
	if len(cards) == 0 {
		return
	}
	err := s.store.Tx(ctx, func(tx store.Tx) error {
		for _, f := range cards {
			row, err := tx.Files().Delete(ctx, f.ID)
			if err != nil {
				return err
			}
			row.MessageID = msg
			if err := tx.Files().Insert(ctx, &row); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		s.log.Warn("the images of a webhook's cards could not be tied to its message", "message", msg, "err", err)
	}
}

// discard removes a file that was stored for a message that was then not
// posted.
func (s *Service) discard(ctx context.Context, f model.File) {
	if f.ID != "" {
		if _, err := s.store.Files().Delete(ctx, f.ID); err != nil && model.CodeOf(err) != model.CodeNotFound {
			s.log.Warn("a file could not be removed", "err", err)
		}
	}
	for _, key := range []string{f.BlobKey, f.ThumbKey} {
		if key != "" {
			if err := s.blobs.Delete(ctx, key); err != nil {
				s.log.Warn("a blob could not be removed", "err", err)
			}
		}
	}
}
