package chat

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/tikaydaes/churr/internal/blob"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/perm"
	"github.com/tikaydaes/churr/internal/store"
)

// Custom emoji (docs/FEATURES.md section 22) and sections (section 21.3):
// the two small things every client keeps a whole list of.

// everyone returns every owner, admin and member.
func everyone(ctx context.Context, a store.Areas) ([]model.AccountID, error) {
	g, err := a.Groups().Builtin(ctx, model.GroupEveryone)
	if err != nil {
		return nil, err
	}
	return a.Groups().MemberIDs(ctx, g.ID)
}

// locals returns every local account that can be connected: people, guests
// and bots.
func (s *Service) locals(ctx context.Context) ([]model.AccountID, error) {
	var out []model.AccountID
	p := store.Page{Limit: model.MaxLimit}
	for {
		list, next, err := s.store.Accounts().List(ctx, store.AccountFilter{State: "active"}, p)
		if err != nil {
			return nil, err
		}
		for _, a := range list {
			if a.Local() {
				out = append(out, a.ID)
			}
		}
		if next == "" {
			return out, nil
		}
		p.Cursor = next
	}
}

// tellLocals sends an event to every connection of a local account.
func (s *Service) tellLocals(ctx context.Context, eventType string, payload map[string]any) {
	to, err := s.locals(ctx)
	if err != nil {
		s.log.Warn("could not list accounts to tell them of a change", "err", err)
		return
	}
	s.tell(to, eventType, payload)
}

// Emoji returns the whole list of custom emoji, for any signed-in account.
func (s *Service) Emoji(ctx context.Context, actor model.Account) ([]model.Emoji, error) {
	if !actor.Active() {
		return nil, model.ErrUnauthenticated
	}
	var out []model.Emoji
	p := store.Page{Limit: model.MaxLimit}
	for {
		list, next, err := s.store.Emoji().List(ctx, p)
		if err != nil {
			return nil, err
		}
		out = append(out, list...)
		if next == "" {
			return out, nil
		}
		p.Cursor = next
	}
}

// AdminEmoji is the admin console's paged list of custom emoji.
func (s *Service) AdminEmoji(ctx context.Context, actor model.Account, p store.Page) ([]model.Emoji, string, error) {
	if err := s.perm.CanAdminister(ctx, actor, perm.UseAdminConsole); err != nil {
		return nil, "", err
	}
	return s.store.Emoji().List(ctx, p)
}

func emojiName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if !model.ValidEmojiName(name) {
		return "", model.Invalid("name", "An emoji name is 2 to 32 characters: lower-case letters, digits and _.")
	}
	return name, nil
}

// CreateEmoji adds a custom emoji: an image of at most 256 KB and 256
// pixels on each side, PNG, JPEG, GIF or WebP by content, under a name no
// other custom emoji has.
func (s *Service) CreateEmoji(ctx context.Context, actor model.Account, name string, image io.Reader) (model.Emoji, error) {
	if err := s.perm.CanAdminister(ctx, actor, perm.AddEmoji); err != nil {
		return model.Emoji{}, err
	}
	name, err := emojiName(name)
	if err != nil {
		return model.Emoji{}, err
	}
	blobs := s.fileStore()
	if blobs == nil {
		return model.Emoji{}, model.ErrUnavailable
	}
	if _, err := s.store.Emoji().ByName(ctx, name); err == nil {
		return model.Emoji{}, model.ErrNameTaken
	} else if !errors.Is(err, model.ErrNotFound) {
		return model.Emoji{}, err
	}
	data, err := io.ReadAll(io.LimitReader(image, model.MaxEmojiBytes+1))
	if err != nil {
		return model.Emoji{}, model.BadRequest("The upload could not be read.")
	}
	if len(data) > model.MaxEmojiBytes {
		return model.Emoji{}, model.ErrTooLarge.With("An emoji image is at most 256 KB.")
	}
	mediaType, kind := blob.Sniff(data[:min(len(data), blob.SniffLen)])
	if kind != model.FileImage {
		return model.Emoji{}, model.Invalid("file", "An emoji is a PNG, JPEG, GIF or WebP image.")
	}
	w, h, _ := blob.Probe(bytes.NewReader(data))
	if w == 0 || h == 0 {
		return model.Emoji{}, model.Invalid("file", "That image could not be read.")
	}
	if w > model.MaxEmojiPixels || h > model.MaxEmojiPixels {
		return model.Emoji{}, model.ErrTooLarge.With("An emoji image is at most 256 pixels on each side.")
	}
	lib, err := s.store.Conversations().EnsureLibrary(ctx)
	if err != nil {
		return model.Emoji{}, err
	}
	key, size, sum, err := blobs.Put(ctx, bytes.NewReader(data))
	if err != nil {
		return model.Emoji{}, err
	}
	ext := map[string]string{blob.TypePNG: ".png", blob.TypeJPEG: ".jpg", blob.TypeGIF: ".gif", blob.TypeWebP: ".webp"}[mediaType]
	f := model.File{ConversationID: lib.ID, UploaderID: actor.ID, Purpose: model.PurposeEmoji, Name: name + ext, Size: size,
		MediaType: mediaType, Kind: model.FileImage, Width: w, Height: h, SHA256: sum, BlobKey: key, CreatedAt: s.now()}
	e := model.Emoji{Name: name, CreatedByID: actor.ID, CreatedAt: f.CreatedAt}
	err = s.store.Tx(ctx, func(tx store.Tx) error {
		if err := tx.Files().Insert(ctx, &f); err != nil {
			return err
		}
		e.FileID = f.ID
		if err := tx.Emoji().Create(ctx, &e); err != nil {
			return err
		}
		return tx.Audit().Append(ctx, model.AuditEntry{At: e.CreatedAt, ActorID: string(actor.ID), ActorKind: "account",
			Action: "emoji.added", TargetKind: "emoji", TargetID: string(e.ID), Detail: map[string]any{"name": name}})
	})
	if err != nil {
		s.dropBlobs(ctx, []string{key})
		return model.Emoji{}, err
	}
	s.tellLocals(ctx, model.EventEmojiAdded, map[string]any{"emoji": e})
	return e, nil
}

// removeEmoji deletes an emoji and its file. Messages keep their text and
// draw `:name:` as written.
func (s *Service) removeEmoji(ctx context.Context, actor model.Account, e model.Emoji) error {
	var keys []string
	err := s.store.Tx(ctx, func(tx store.Tx) error {
		if _, err := tx.Emoji().Delete(ctx, e.ID); err != nil {
			return err
		}
		f, err := tx.Files().Delete(ctx, e.FileID)
		if err != nil && !errors.Is(err, model.ErrNotFound) {
			return err
		}
		keys = store.BlobKeys(f)
		return tx.Audit().Append(ctx, model.AuditEntry{ActorID: string(actor.ID), ActorKind: "account",
			Action: "emoji.removed", TargetKind: "emoji", TargetID: string(e.ID), Detail: map[string]any{"name": e.Name}})
	})
	if err != nil {
		return hide(err)
	}
	s.dropBlobs(ctx, keys)
	s.tellLocals(ctx, model.EventEmojiRemoved, map[string]any{"emoji": e})
	return nil
}

// DeleteEmoji removes a custom emoji: an admin, or the member who added it
// while members may add emoji.
func (s *Service) DeleteEmoji(ctx context.Context, actor model.Account, id model.EmojiID) error {
	e, err := s.store.Emoji().Get(ctx, id)
	if err != nil {
		return hide(err)
	}
	set, err := s.perm.Settings(ctx)
	if err != nil {
		return err
	}
	if err := perm.CheckRemoveEmoji(actor, e, set); err != nil {
		return err
	}
	return s.removeEmoji(ctx, actor, e)
}

// AdminDeleteEmoji removes a custom emoji from the admin console.
func (s *Service) AdminDeleteEmoji(ctx context.Context, actor model.Account, id model.EmojiID) error {
	if err := s.perm.CanAdminister(ctx, actor, perm.RemoveAnyEmoji); err != nil {
		return err
	}
	e, err := s.store.Emoji().Get(ctx, id)
	if err != nil {
		return hide(err)
	}
	return s.removeEmoji(ctx, actor, e)
}

// RenameEmoji gives a custom emoji another name. Text that used the old
// name keeps it as written.
func (s *Service) RenameEmoji(ctx context.Context, actor model.Account, id model.EmojiID, name string) (model.Emoji, error) {
	if err := s.perm.CanAdminister(ctx, actor, perm.RemoveAnyEmoji); err != nil {
		return model.Emoji{}, err
	}
	name, err := emojiName(name)
	if err != nil {
		return model.Emoji{}, err
	}
	var e model.Emoji
	err = s.store.Tx(ctx, func(tx store.Tx) error {
		var err error
		if e, err = tx.Emoji().Rename(ctx, id, name); err != nil {
			return err
		}
		return tx.Audit().Append(ctx, model.AuditEntry{ActorID: string(actor.ID), ActorKind: "account",
			Action: "emoji.renamed", TargetKind: "emoji", TargetID: string(id), Detail: map[string]any{"name": name}})
	})
	if err != nil {
		return e, hide(err)
	}
	s.tellLocals(ctx, model.EventEmojiUpdated, map[string]any{"emoji": e})
	return e, nil
}

// Sections lists the headings named channels are grouped under.
func (s *Service) Sections(ctx context.Context, actor model.Account) ([]model.Section, error) {
	if !actor.Active() {
		return nil, model.ErrUnauthenticated
	}
	return s.store.Sections().List(ctx)
}

// sectionsChanged tells every owner, admin and member the new list.
func (s *Service) sectionsChanged(ctx context.Context) {
	list, err := s.store.Sections().List(ctx)
	if err != nil {
		s.log.Warn("could not read the sections to send them", "err", err)
		return
	}
	to, err := everyone(ctx, s.store)
	if err != nil {
		s.log.Warn("could not list accounts to send the sections to", "err", err)
		return
	}
	if list == nil {
		list = []model.Section{}
	}
	s.tell(to, model.EventSectionsChanged, map[string]any{"sections": list})
}

func sectionName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > 64 || strings.ContainsFunc(name, func(r rune) bool { return r < 0x20 || r == 0x7F }) {
		return "", model.Invalid("name", "A section name is 1 to 64 characters.")
	}
	return name, nil
}

// CreateSection adds a section. With no position it goes last.
func (s *Service) CreateSection(ctx context.Context, actor model.Account, name string, position *int) (model.Section, error) {
	if err := s.perm.CanAdminister(ctx, actor, perm.DefineSections); err != nil {
		return model.Section{}, err
	}
	name, err := sectionName(name)
	if err != nil {
		return model.Section{}, err
	}
	sec := model.Section{Name: name}
	if position != nil {
		sec.Position = *position
	} else {
		all, err := s.store.Sections().List(ctx)
		if err != nil {
			return sec, err
		}
		for _, other := range all {
			if other.Position >= sec.Position {
				sec.Position = other.Position + 1
			}
		}
	}
	err = s.store.Tx(ctx, func(tx store.Tx) error {
		if err := tx.Sections().Create(ctx, &sec); err != nil {
			return err
		}
		return tx.Audit().Append(ctx, model.AuditEntry{ActorID: string(actor.ID), ActorKind: "account",
			Action: "section.created", TargetKind: "section", TargetID: string(sec.ID), Detail: map[string]any{"name": name}})
	})
	if err != nil {
		return sec, err
	}
	s.sectionsChanged(ctx)
	return sec, nil
}

// UpdateSection renames or moves a section.
func (s *Service) UpdateSection(ctx context.Context, actor model.Account, id model.SectionID, name *string, position *int) (model.Section, error) {
	if err := s.perm.CanAdminister(ctx, actor, perm.DefineSections); err != nil {
		return model.Section{}, err
	}
	sec, err := s.store.Sections().Get(ctx, id)
	if err != nil {
		return sec, hide(err)
	}
	if name != nil {
		if sec.Name, err = sectionName(*name); err != nil {
			return sec, err
		}
	}
	if position != nil {
		sec.Position = *position
	}
	err = s.store.Tx(ctx, func(tx store.Tx) error {
		if err := tx.Sections().Update(ctx, sec); err != nil {
			return err
		}
		return tx.Audit().Append(ctx, model.AuditEntry{ActorID: string(actor.ID), ActorKind: "account",
			Action: "section.updated", TargetKind: "section", TargetID: string(id), Detail: map[string]any{"name": sec.Name, "position": sec.Position}})
	})
	if err != nil {
		return sec, hide(err)
	}
	s.sectionsChanged(ctx)
	return sec, nil
}

// DeleteSection removes a section. Its channels stay, under no section.
func (s *Service) DeleteSection(ctx context.Context, actor model.Account, id model.SectionID) error {
	if err := s.perm.CanAdminister(ctx, actor, perm.DefineSections); err != nil {
		return err
	}
	err := s.store.Tx(ctx, func(tx store.Tx) error {
		if err := tx.Sections().Delete(ctx, id); err != nil {
			return err
		}
		return tx.Audit().Append(ctx, model.AuditEntry{ActorID: string(actor.ID), ActorKind: "account",
			Action: "section.deleted", TargetKind: "section", TargetID: string(id)})
	})
	if err != nil {
		return hide(err)
	}
	s.sectionsChanged(ctx)
	return nil
}
