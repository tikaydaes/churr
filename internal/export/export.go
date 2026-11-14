// Package export writes a community's data to one zip archive, for taking
// it elsewhere and for answering "what does this server hold"
// (docs/SCHEMA.md section 18). It is not a backup and cannot be loaded
// back: the backup is the data directory.
//
// The archive holds every channel, public, private and ad-hoc, whoever is
// in it. That is the catch the README prints: whoever runs the server holds
// the database. It holds nothing of a sealed chat: not its messages, not
// its files, not who is in it, not that it exists. Anything else that would
// name a sealed chat (a report, a time-out, a line of the audit log) is left
// out with it.
//
// Also left out, because they are credentials, secrets, one person's own
// state or derived data: passkeys, authenticator secrets, recovery codes,
// sessions and tokens, devices and push addresses, keys, read positions,
// modes, bookmarks, blocks, scheduled items, who was notified anyway, the
// event log, the search index, and peers.
package export

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/tikaydaes/churr/internal/blob"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
	"github.com/tikaydaes/churr/internal/store/migrations"
	"github.com/tikaydaes/churr/internal/version"
)

// Options say what to export from.
type Options struct {
	Store store.Store
	// Blobs is where file contents are read from. It may be nil when
	// NoFiles is set.
	Blobs blob.Blobs
	// NoFiles leaves file contents out. The messages still list their
	// files by name.
	NoFiles bool
	Log     *slog.Logger
}

// Counts is how much of each thing went into an archive. It is written into
// export.json.
type Counts struct {
	Accounts      int `json:"accounts"`
	Groups        int `json:"groups"`
	Sections      int `json:"sections"`
	Emoji         int `json:"emoji"`
	Conversations int `json:"conversations"`
	Messages      int `json:"messages"`
	Versions      int `json:"message_versions"`
	Files         int `json:"files"`
	FilesMissing  int `json:"files_missing"`
	AuditEntries  int `json:"audit_entries"`
}

// head is export.json.
type head struct {
	ServerName    string     `json:"server_name"`
	ExportedAt    model.Time `json:"exported_at"`
	SchemaVersion int        `json:"schema_version"`
	Churr         string     `json:"churr_version"`
	// Files is false when the archive was made without file contents.
	Files  bool   `json:"files_included"`
	Counts Counts `json:"counts"`
	// Note says what is never in an export, for whoever opens it later.
	Note string `json:"note"`
}

const note = "Sealed chats are not in this export: the server holds nothing readable of them. " +
	"Sign-in credentials, tokens, keys and each person's private state are not in it either."

// writer is one export in progress.
type writer struct {
	Options
	zip    *zip.Writer
	counts Counts
	// sealed are the conversations that must not be named anywhere.
	sealed map[string]bool
	// files are the file ids already written, so one file is stored once.
	files map[model.FileID]bool
}

// Write writes the archive to w.
func Write(ctx context.Context, w io.Writer, opts Options) error {
	_, err := WriteCounted(ctx, w, opts)
	return err
}

// WriteCounted is Write that also says how much it wrote.
func WriteCounted(ctx context.Context, w io.Writer, opts Options) (Counts, error) {
	if opts.Store == nil {
		return Counts{}, errors.New("export: no store given")
	}
	if opts.Log == nil {
		opts.Log = slog.Default()
	}
	if opts.Blobs == nil {
		opts.NoFiles = true
	}
	x := &writer{Options: opts, zip: zip.NewWriter(w), sealed: map[string]bool{}, files: map[model.FileID]bool{}}
	if err := x.run(ctx); err != nil {
		return x.counts, err
	}
	return x.counts, x.zip.Close()
}

func (x *writer) run(ctx context.Context) error {
	open, sealed, err := store.AllConversationIDs(ctx, x.Store)
	if err != nil {
		return err
	}
	for _, id := range sealed {
		x.sealed[string(id)] = true
	}
	steps := []func(context.Context) error{
		x.accounts, x.groups, x.sections, x.emoji,
		func(ctx context.Context) error { return x.conversations(ctx, open) },
		x.moderation, x.settings, x.audit, x.head,
	}
	for _, step := range steps {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := step(ctx); err != nil {
			return err
		}
	}
	return nil
}

// json writes one JSON document as a file of the archive.
func (x *writer) json(name string, v any) error {
	f, err := x.zip.Create(name)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(f)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// lines opens a file of the archive that holds one JSON value per line.
func (x *writer) lines(name string) (func(v any) error, error) {
	f, err := x.zip.Create(name)
	if err != nil {
		return nil, err
	}
	enc := json.NewEncoder(f)
	enc.SetEscapeHTML(false)
	return enc.Encode, nil
}

// account is one line of accounts.json: the profile and the role, and
// nothing a person signs in with.
type account struct {
	ID          model.AccountID `json:"id"`
	Username    string          `json:"username"`
	DisplayName string          `json:"display_name"`
	Role        model.Role      `json:"role"`
	// ServerName is the home server of someone from a linked server; it
	// is absent for an account of this one.
	ServerName   string       `json:"server_name,omitempty"`
	Title        string       `json:"title,omitempty"`
	Pronouns     string       `json:"pronouns,omitempty"`
	TimeZone     string       `json:"time_zone,omitempty"`
	AvatarFileID model.FileID `json:"avatar_file_id,omitempty"`
	CreatedAt    model.Time   `json:"created_at"`
	// ClaimedAt is absent for an imported account nobody has claimed.
	ClaimedAt model.Time `json:"claimed_at,omitempty"`
	RemovedAt model.Time `json:"removed_at,omitempty"`
}

func (x *writer) accounts(ctx context.Context) error {
	var out []account
	page := store.Page{Limit: model.MaxLimit}
	for {
		list, next, err := x.Store.Accounts().List(ctx, store.AccountFilter{IncludeRemoved: true}, page)
		if err != nil {
			return err
		}
		for _, a := range list {
			out = append(out, account{ID: a.ID, Username: a.Username, DisplayName: a.DisplayName, Role: a.Role, ServerName: a.ServerName,
				Title: a.Title, Pronouns: a.Pronouns, TimeZone: a.TimeZone, AvatarFileID: a.AvatarFileID,
				CreatedAt: a.CreatedAt, ClaimedAt: a.ClaimedAt, RemovedAt: a.RemovedAt})
			if a.AvatarFileID != "" {
				if err := x.fileByID(ctx, a.AvatarFileID); err != nil {
					return err
				}
			}
		}
		if next == "" {
			break
		}
		page.Cursor = next
	}
	x.counts.Accounts = len(out)
	return x.json("accounts.json", orEmpty(out))
}

func (x *writer) groups(ctx context.Context) error {
	type group struct {
		model.Group
		MemberIDs []model.AccountID `json:"member_ids"`
	}
	list, err := x.Store.Groups().List(ctx, "")
	if err != nil {
		return err
	}
	out := make([]group, 0, len(list))
	for _, g := range list {
		ids, err := x.Store.Groups().MemberIDs(ctx, g.ID)
		if err != nil {
			return err
		}
		out = append(out, group{Group: g, MemberIDs: orEmpty(ids)})
	}
	x.counts.Groups = len(out)
	return x.json("groups.json", out)
}

func (x *writer) sections(ctx context.Context) error {
	list, err := x.Store.Sections().List(ctx)
	if err != nil {
		return err
	}
	x.counts.Sections = len(list)
	return x.json("sections.json", orEmpty(list))
}

func (x *writer) emoji(ctx context.Context) error {
	var out []model.Emoji
	page := store.Page{Limit: model.MaxLimit}
	for {
		list, next, err := x.Store.Emoji().List(ctx, page)
		if err != nil {
			return err
		}
		for _, e := range list {
			// The address is this server's; the file is in the archive.
			e.URL = ""
			out = append(out, e)
			if e.FileID != "" {
				if err := x.fileByID(ctx, e.FileID); err != nil {
					return err
				}
			}
		}
		if next == "" {
			break
		}
		page.Cursor = next
	}
	x.counts.Emoji = len(out)
	return x.json("emoji.json", orEmpty(out))
}

// conversation is one entry of conversations.json.
type conversation struct {
	model.Conversation
	// Members are who is in it and at what level; Groups are the groups
	// added to it. Read positions and modes are each person's own and are
	// not exported.
	Members []member             `json:"members"`
	Groups  []model.ChannelGroup `json:"groups"`
	Pins    []pin                `json:"pins"`
}

type member struct {
	AccountID   model.AccountID `json:"account_id"`
	Level       model.Level     `json:"level"`
	DirectLevel model.Level     `json:"direct_level,omitempty"`
	ViaGroup    bool            `json:"via_group"`
	JoinedAt    model.Time      `json:"joined_at"`
}

type pin struct {
	MessageID  model.MessageID `json:"message_id"`
	PinnedByID model.AccountID `json:"pinned_by_id"`
	PinnedAt   model.Time      `json:"pinned_at"`
}

func (x *writer) conversations(ctx context.Context, ids []model.ConversationID) error {
	out := make([]conversation, 0, len(ids))
	for _, id := range ids {
		c, err := x.Store.Conversations().Get(ctx, id)
		if errors.Is(err, model.ErrNotFound) {
			continue // deleted since the list was read
		}
		if err != nil {
			return err
		}
		// The library holds avatars and emoji images and no messages.
		// Those files are exported with what they belong to.
		if c.Kind != model.KindChannel || c.IsLibrary() {
			continue
		}
		entry := conversation{Conversation: c, Members: []member{}, Pins: []pin{}}
		page := store.Page{Limit: model.MaxLimit}
		for {
			list, next, err := x.Store.Members().List(ctx, id, "", page)
			if err != nil {
				return err
			}
			for _, m := range list {
				entry.Members = append(entry.Members, member{AccountID: m.AccountID, Level: m.Level, DirectLevel: m.DirectLevel, ViaGroup: m.ViaGroup, JoinedAt: m.JoinedAt})
			}
			if next == "" {
				break
			}
			page.Cursor = next
		}
		groups, err := x.Store.Members().Groups(ctx, id)
		if err != nil {
			return err
		}
		entry.Groups = orEmpty(groups)
		pins, err := x.Store.Pins().List(ctx, id)
		if err != nil {
			return err
		}
		for _, p := range pins {
			entry.Pins = append(entry.Pins, pin{MessageID: p.MessageID, PinnedByID: p.PinnedByID, PinnedAt: p.PinnedAt})
		}
		if err := x.messages(ctx, c); err != nil {
			return fmt.Errorf("conversation %s: %w", id, err)
		}
		out = append(out, entry)
	}
	x.counts.Conversations = len(out)
	return x.json("conversations.json", out)
}

// messages writes one conversation's messages/<id>.jsonl: each message on a
// line, in seq order, in the shape the API gives it to nobody in particular,
// followed by one line for each earlier version it has. A version's line
// has `version_of` with the message's identifier.
func (x *writer) messages(ctx context.Context, c model.Conversation) error {
	line, err := x.lines("messages/" + string(c.ID) + ".jsonl")
	if err != nil {
		return err
	}
	after := int64(0)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		msgs, more, err := x.Store.Messages().List(ctx, c.ID, store.MessageList{SeqPage: store.SeqPage{After: after, Limit: model.MaxLimit}})
		if err != nil {
			return err
		}
		if len(msgs) == 0 {
			return nil
		}
		if err := x.Store.Messages().Hydrate(ctx, msgs, ""); err != nil {
			return err
		}
		ids := make([]model.MessageID, len(msgs))
		for i, m := range msgs {
			ids[i] = m.ID
		}
		forms, err := x.Store.Forms().ForMessages(ctx, ids)
		if err != nil {
			return err
		}
		for _, m := range msgs {
			after = max(after, m.Seq)
			if m.Sealed != nil {
				// Cannot happen in a channel; never write ciphertext.
				continue
			}
			if err := x.message(ctx, line, m, forms); err != nil {
				return err
			}
		}
		if !more {
			return nil
		}
	}
}

// exportedMessage is a message's line: the message, the answers to its
// form, and the reactions in full.
type exportedMessage struct {
	model.Message
	// AllReactions lists every account behind each reaction; the
	// message's own `reactions` stops at ten.
	AllReactions []model.Reaction `json:"all_reactions,omitempty"`
	// FormAnswers are the answers to the message's form, with who gave
	// each. An anonymous form has none here: its counts are in the form's
	// tally and who answered what is not exported.
	FormAnswers []model.FormAnswer `json:"form_answers,omitempty"`
}

type exportedVersion struct {
	VersionOf model.MessageID `json:"version_of"`
	model.MessageVersion
}

func (x *writer) message(ctx context.Context, line func(any) error, m model.Message, forms map[model.MessageID]store.Form) error {
	out := exportedMessage{Message: m}
	// The author's own tag for matching a pending message; nobody else is
	// ever given it.
	out.ClientID = ""
	for i := range out.Files {
		// Addresses on this server mean nothing in an archive.
		out.Files[i].URL, out.Files[i].ThumbURL, out.Files[i].PosterURL = "", "", ""
	}
	if len(m.Reactions) > 0 {
		all, err := x.Store.Reactions().List(ctx, m.ID, "")
		if err != nil {
			return err
		}
		out.AllReactions = all
	}
	if f, ok := forms[m.ID]; ok {
		form := f.Form
		answers, err := x.Store.Forms().AllAnswers(ctx, f.ID)
		if err != nil {
			return err
		}
		tallies, err := x.Store.Forms().Tallies(ctx, []model.FormID{f.ID})
		if err != nil {
			return err
		}
		form.AnswerCount, form.Tally, form.Voters, form.MyAnswer = len(answers), tallies[f.ID], nil, nil
		out.Form = &form
		if !f.Anonymous {
			out.FormAnswers = answers
		}
	}
	if err := line(out); err != nil {
		return err
	}
	x.counts.Messages++

	// The files on the message now, the images of its cards, and the
	// files earlier versions carried.
	attached, err := x.Store.Files().ForMessage(ctx, m.ID, true)
	if err != nil {
		return err
	}
	cards, err := x.Store.Files().CardImages(ctx, m.ID)
	if err != nil {
		return err
	}
	byID := map[model.FileID]model.File{}
	for _, f := range append(attached, cards...) {
		byID[f.ID] = f
		if err := x.file(ctx, f); err != nil {
			return err
		}
	}
	if !m.HasHistory {
		return nil
	}
	versions, err := x.Store.Messages().Versions(ctx, m.ID)
	if err != nil {
		return err
	}
	for _, v := range versions {
		v.Files = []model.File{}
		for _, id := range v.FileIDs {
			if f, ok := byID[id]; ok {
				f.URL, f.ThumbURL, f.PosterURL = "", "", ""
				v.Files = append(v.Files, f)
			}
		}
		if err := line(exportedVersion{VersionOf: m.ID, MessageVersion: v}); err != nil {
			return err
		}
		x.counts.Versions++
	}
	return nil
}

// fileByID exports a file known only by its identifier: an avatar, an
// emoji's image.
func (x *writer) fileByID(ctx context.Context, id model.FileID) error {
	if x.NoFiles || x.files[id] {
		return nil
	}
	f, err := x.Store.Files().Get(ctx, id)
	if errors.Is(err, model.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	return x.file(ctx, f)
}

// file writes a file's content at files/<file id>/<name>. A file whose
// content is not here (it never arrived from another server, or its blob is
// missing) is counted and skipped: one lost file must not lose the export.
func (x *writer) file(ctx context.Context, f model.File) error {
	if x.NoFiles || x.files[f.ID] || f.BlobKey == "" {
		return nil
	}
	if x.sealed[string(f.ConversationID)] {
		return nil
	}
	x.files[f.ID] = true
	r, err := x.Blobs.Get(ctx, f.BlobKey)
	if err != nil {
		x.counts.FilesMissing++
		x.Log.Warn("a file's content could not be read for the export", "file", f.ID, "err", err)
		return nil
	}
	defer r.Close()
	// Stored as it is: most uploads are compressed already.
	w, err := x.zip.CreateHeader(&zip.FileHeader{Name: "files/" + string(f.ID) + "/" + safeName(f.Name), Method: zip.Store, Modified: f.CreatedAt.Time()})
	if err != nil {
		return err
	}
	if _, err := io.Copy(w, r); err != nil {
		return fmt.Errorf("file %s: %w", f.ID, err)
	}
	x.counts.Files++
	return nil
}

// safeName makes a file's name fit to be the last part of a path inside an
// archive: no separators, no control characters, not a dot name, not empty.
func safeName(name string) string {
	name = strings.Map(func(r rune) rune {
		switch {
		case r == '/' || r == '\\' || r == ':':
			return '_'
		case r < ' ' || r == 0x7f:
			return -1
		}
		return r
	}, name)
	name = strings.TrimLeft(strings.TrimSpace(name), ".")
	if name == "" {
		return "file"
	}
	return name
}

// moderation writes moderation.json. Anything about a message in a sealed
// chat is left out: a sealed chat is not named anywhere in an export.
func (x *writer) moderation(ctx context.Context) error {
	mod := x.Store.Moderation()
	out := struct {
		Reports       []model.Report       `json:"reports"`
		PurgeRequests []model.PurgeRequest `json:"purge_requests"`
		Bans          []model.Ban          `json:"bans"`
		// Timeouts are the time-outs in force; one that has ended or
		// was lifted is in the audit log.
		Timeouts    []model.Timeout    `json:"timeouts"`
		WordFilters []model.WordFilter `json:"word_filters"`
	}{}
	if err := pages(func(p store.Page) (string, error) {
		list, next, err := mod.Reports(ctx, store.ReportFilter{}, p)
		for _, r := range list {
			if !x.sealed[string(r.ConversationID)] {
				r.Message = nil
				out.Reports = append(out.Reports, r)
			}
		}
		return next, err
	}); err != nil {
		return err
	}
	if err := pages(func(p store.Page) (string, error) {
		list, next, err := mod.PurgeRequests(ctx, false, p)
		for _, r := range list {
			if len(x.sealed) > 0 {
				m, err := x.Store.Messages().Get(ctx, r.MessageID)
				if err == nil && x.sealed[string(m.ConversationID)] {
					continue
				}
			}
			r.Message = nil
			out.PurgeRequests = append(out.PurgeRequests, r)
		}
		return next, err
	}); err != nil {
		return err
	}
	if err := pages(func(p store.Page) (string, error) {
		list, next, err := x.Store.Accounts().ListBans(ctx, p)
		out.Bans = append(out.Bans, list...)
		return next, err
	}); err != nil {
		return err
	}
	if err := pages(func(p store.Page) (string, error) {
		list, next, err := mod.Timeouts(ctx, store.TimeoutFilter{}, model.Now(), p)
		for _, t := range list {
			if !x.sealed[string(t.ConversationID)] {
				out.Timeouts = append(out.Timeouts, t)
			}
		}
		return next, err
	}); err != nil {
		return err
	}
	filters, err := mod.WordFilters(ctx)
	if err != nil {
		return err
	}
	out.WordFilters = filters
	out.Reports, out.PurgeRequests, out.Bans = orEmpty(out.Reports), orEmpty(out.PurgeRequests), orEmpty(out.Bans)
	out.Timeouts, out.WordFilters = orEmpty(out.Timeouts), orEmpty(out.WordFilters)
	return x.json("moderation.json", out)
}

// pages calls fn for each page of a cursor-paged list until there are no
// more.
func pages(fn func(p store.Page) (next string, err error)) error {
	p := store.Page{Limit: model.MaxLimit}
	for {
		next, err := fn(p)
		if err != nil || next == "" {
			return err
		}
		p.Cursor = next
	}
}

// secretSetting reports whether a settings key holds something that must
// not leave the server: a sealed secret, a token's hash, or a row the server
// keeps for itself.
func secretSetting(key string) bool {
	if key == store.SettingSetupTokenHash || strings.HasPrefix(key, "setup.") {
		return true
	}
	for _, word := range []string{"secret", "token", "password", "private", "_key", ".key", "hash"} {
		if strings.Contains(key, word) {
			return true
		}
	}
	return false
}

func (x *writer) settings(ctx context.Context) error {
	all, err := x.Store.Settings().All(ctx)
	if err != nil {
		return err
	}
	out := map[string]json.RawMessage{}
	for k, v := range all {
		if !secretSetting(k) {
			out[k] = v
		}
	}
	return x.json("settings.json", out)
}

// audit writes audit.jsonl, one entry per line. A line that names a sealed
// chat, as its target or anywhere in its detail, is left out.
func (x *writer) audit(ctx context.Context) error {
	line, err := x.lines("audit.jsonl")
	if err != nil {
		return err
	}
	return pages(func(p store.Page) (string, error) {
		list, next, err := x.Store.Audit().List(ctx, store.AuditFilter{}, p)
		if err != nil {
			return "", err
		}
		for _, e := range list {
			if x.namesSealed(e) {
				continue
			}
			if err := line(e); err != nil {
				return "", err
			}
			x.counts.AuditEntries++
		}
		return next, nil
	})
}

func (x *writer) namesSealed(e model.AuditEntry) bool {
	if len(x.sealed) == 0 {
		return false
	}
	if x.sealed[e.TargetID] {
		return true
	}
	b, err := json.Marshal(e.Detail)
	if err != nil {
		return true
	}
	for id := range x.sealed {
		if strings.Contains(string(b), id) {
			return true
		}
	}
	return false
}

func (x *writer) head(ctx context.Context) error {
	name, err := store.SettingString(ctx, x.Store, store.SettingServerName, "")
	if err != nil {
		return err
	}
	schema, err := migrations.Latest(migrations.Dialect(x.Store.Dialect()))
	if err != nil {
		return err
	}
	return x.json("export.json", head{ServerName: name, ExportedAt: model.Now(), SchemaVersion: schema, Churr: version.Version,
		Files: !x.NoFiles, Counts: x.counts, Note: note})
}

func orEmpty[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}
