// Package importer reads a Slack export into a Churr server
// (docs/FEATURES.md section 25). It is run by the operator, on the server's
// machine, with the server stopped, and writes straight to the database
// through the store: no events, no notifications, no word filter.
//
// People become accounts nobody has signed in to yet, each with a claim
// link. Channels, messages, threads, reactions, pins, custom emoji and files
// keep their original times and authors, and every imported message says it
// came "via Slack".
//
// Running it again on the same export makes nothing twice. Everything it
// makes has an identifier worked out from its Slack identifier, so what is
// already there is found and left alone; and a record of Slack identifier
// to Churr identifier is kept in the data directory, in import-slack.json.
//
// With files, the importer makes an outside connection that no running
// server makes: it fetches each file from the address the export gives,
// through a client that refuses private addresses. With NoFiles it contacts
// nothing.
package importer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/tikaydaes/churr/internal/blob"
	"github.com/tikaydaes/churr/internal/markdown"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/secret"
	"github.com/tikaydaes/churr/internal/store"
)

// Options say where to import into and how.
type Options struct {
	Store store.Store
	// Blobs is where downloaded files are kept. It may be nil with
	// NoFiles.
	Blobs blob.Blobs
	// EmailHash makes the keyed hash of an email address that company
	// sign-in later matches to claim an account. The address itself is
	// never stored. Nil stores none.
	EmailHash func(email string) []byte
	// Origin is the start of the claim links, such as
	// https://chat.example.com. Empty gives links without a host.
	Origin string
	// DataDir is the data directory: the record file is kept in it, and
	// downloads pass through its tmp folder.
	DataDir string
	// NoFiles downloads nothing: no files, no profile pictures, no emoji
	// images. Messages then say which files they had.
	NoFiles bool
	// DryRun reads the export, reports what it holds, and changes nothing.
	DryRun bool
	// HTTP fetches files. Nil means a client that refuses private
	// addresses (Guarded).
	HTTP *http.Client
	// Out is where what was found, and progress, is printed. Nil prints
	// nothing.
	Out io.Writer
	// Confirm is asked before importing messages that the server's
	// retention period would delete at the next janitor run. Nil goes
	// ahead.
	Confirm func(question string) bool
	Log     *slog.Logger
	// Now replaces the clock, for tests.
	Now func() model.Time
}

// Claim is the claim link of one imported person.
type Claim struct {
	AccountID   model.AccountID
	Username    string
	DisplayName string
	URL         string
	ExpiresAt   model.Time
}

// Found is what an export holds, before anything is made of it.
type Found struct {
	Channels, People, Messages, Files int
}

// Report says what an import did.
type Report struct {
	Found  Found
	DryRun bool
	// PastRetention is how many of the export's messages are already
	// older than the server's retention period.
	PastRetention int
	// What was made in this run. A second run over the same export
	// reports zeroes.
	People, Bots, Channels, Messages, Reactions, Pins, Files, Emoji int
	// FilesMissing counts files that could not be fetched, or were not
	// asked for; their messages say "File not available".
	FilesMissing int
	// Notices counts the join, leave, topic and other notice messages
	// that were left out.
	Notices int
	// Claims has one link for every imported person nobody has claimed.
	Claims []Claim
}

// claimLife is how long a claim link made by the importer lasts.
const claimLife = 30 * 24 * time.Hour

// recordName is the record of what was made, in the data directory.
const recordName = "import-slack.json"

// record maps Slack identifiers to Churr identifiers.
type record struct {
	Users    map[string]model.AccountID      `json:"users"`
	Channels map[string]model.ConversationID `json:"channels"`
	Emoji    map[string]model.EmojiID        `json:"emoji"`
	// Note explains what is not listed.
	Note string `json:"note"`
}

const recordNote = "Messages and files are not listed: each has an identifier worked out from its Slack channel and timestamp, so a second run finds it without a list."

// run is one import in progress.
type run struct {
	Options
	ctx context.Context
	a   *archive
	rec record
	rep Report
	now model.Time

	// users are the accounts of the Slack users met so far, bots that
	// only appear as authors included (keyed "bot:<id or name>").
	users map[string]model.Account
	// byName is every local username taken, for resolving mentions.
	byName   map[string]model.AccountID
	channels map[string]model.Conversation
	owner    model.AccountID
	library  model.ConversationID
}

// Slack imports the export at zipPath.
func Slack(ctx context.Context, zipPath string, opts Options) (Report, error) {
	if opts.Store == nil {
		return Report{}, errors.New("importer: no store given")
	}
	if opts.Log == nil {
		opts.Log = slog.Default()
	}
	if opts.Out == nil {
		opts.Out = io.Discard
	}
	if opts.Now == nil {
		opts.Now = model.Now
	}
	if opts.Blobs == nil {
		opts.NoFiles = true
	}
	if opts.HTTP == nil {
		opts.HTTP = Guarded()
	}
	a, err := open(zipPath)
	if err != nil {
		return Report{}, err
	}
	defer a.close()
	r := &run{Options: opts, ctx: ctx, a: a, now: opts.Now(), users: map[string]model.Account{},
		byName: map[string]model.AccountID{}, channels: map[string]model.Conversation{}}
	r.rep.DryRun = opts.DryRun
	if err := r.survey(); err != nil {
		return r.rep, err
	}
	if opts.DryRun {
		fmt.Fprintln(r.Out, "Dry run: nothing was changed.")
		return r.rep, nil
	}
	if err := r.loadRecord(); err != nil {
		return r.rep, err
	}
	steps := []func() error{r.people, r.emoji, r.conversations, r.messages, r.claims, r.finish}
	for _, step := range steps {
		if err := ctx.Err(); err != nil {
			return r.rep, err
		}
		if err := step(); err != nil {
			// What was made is recorded even when the run stops early,
			// so the next run continues from here.
			r.saveRecord()
			return r.rep, err
		}
	}
	return r.rep, r.saveRecord()
}

// survey counts what the export holds and prints it, and asks before
// importing messages the retention period would delete at once.
func (r *run) survey() error {
	retention, set, err := store.SettingInt(r.ctx, r.Store, store.SettingRetention)
	if err != nil {
		return err
	}
	cutoff := int64(0)
	if set && retention > 0 {
		cutoff = int64(r.now) - retention*1000
	}
	for _, u := range r.a.users {
		if !u.IsBot && !u.Deleted {
			r.rep.Found.People++
		}
	}
	for _, c := range r.a.channels {
		msgs, err := r.a.messages(c)
		if err != nil {
			return err
		}
		r.rep.Found.Channels++
		for _, m := range msgs {
			if !imported[m.Subtype] {
				continue
			}
			r.rep.Found.Messages++
			r.rep.Found.Files += len(m.Files)
			if ms, _ := millis(m.TS); cutoff > 0 && ms < cutoff {
				r.rep.PastRetention++
			}
		}
	}
	f := r.rep.Found
	fmt.Fprintf(r.Out, "The export holds %d conversations, %d people, %d messages and %d files.\n", f.Channels, f.People, f.Messages, f.Files)
	if r.rep.PastRetention > 0 {
		q := fmt.Sprintf("This server keeps messages for %s. %d of the export's messages are already older than that and would be deleted by the next clean-up, within the hour of the server starting. Import anyway?",
			days(retention), r.rep.PastRetention)
		if r.DryRun {
			fmt.Fprintln(r.Out, strings.TrimSuffix(q, " Import anyway?"))
		} else if r.Confirm != nil && !r.Confirm(q) {
			return errors.New("nothing was imported. Lengthen or switch off the retention period in the admin console first, then run the import again")
		}
	}
	return nil
}

func days(seconds int64) string {
	if d := seconds / 86400; d >= 1 {
		return fmt.Sprintf("%d days", d)
	}
	return fmt.Sprintf("%d seconds", seconds)
}

func (r *run) recordPath() string { return filepath.Join(r.DataDir, recordName) }

func (r *run) loadRecord() error {
	r.rec = record{Users: map[string]model.AccountID{}, Channels: map[string]model.ConversationID{}, Emoji: map[string]model.EmojiID{}, Note: recordNote}
	if r.DataDir == "" {
		return nil
	}
	b, err := os.ReadFile(r.recordPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var had record
	if err := json.Unmarshal(b, &had); err != nil {
		return fmt.Errorf("the record of an earlier import, %s, cannot be read: %w", r.recordPath(), err)
	}
	for k, v := range had.Users {
		r.rec.Users[k] = v
	}
	for k, v := range had.Channels {
		r.rec.Channels[k] = v
	}
	for k, v := range had.Emoji {
		r.rec.Emoji[k] = v
	}
	return nil
}

// saveRecord writes the record beside itself and moves it into place, so
// the file is always a whole one.
func (r *run) saveRecord() error {
	if r.DataDir == "" || r.DryRun {
		return nil
	}
	b, err := json.MarshalIndent(r.rec, "", "  ")
	if err != nil {
		return err
	}
	tmp := r.recordPath() + ".new"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, r.recordPath())
}

// detID makes the identifier of an imported thing from its Slack
// identifier: the same thing gets the same identifier on every run, which
// is what makes a second run find it. It has the form of every other
// identifier (a version 7 UUID): the moment the thing was made in Slack,
// then bytes of a hash where a fresh one has random bytes.
func detID[T ~string](kind, slackID string, ms int64) T {
	sum := sha256.Sum256([]byte("churr-slack-import\x00" + kind + "\x00" + slackID))
	var b [16]byte
	copy(b[:], sum[:16])
	if ms < 0 {
		ms = 0
	}
	u := uint64(ms)
	b[0], b[1], b[2], b[3], b[4], b[5] = byte(u>>40), byte(u>>32), byte(u>>24), byte(u>>16), byte(u>>8), byte(u)
	b[6] = b[6]&0x0f | 0x70
	b[8] = b[8]&0x3f | 0x80
	h := hex.EncodeToString(b[:])
	return T(h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:])
}

// cut shortens s to at most n characters.
func cut(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

// ---------------------------------------------------------------- people

// usernameFrom makes a Churr username out of a Slack handle: lower case,
// only the characters the username rule allows, starting with a letter or
// digit, 2 to 32 long.
func usernameFrom(handle string) string {
	var b strings.Builder
	for _, c := range strings.ToLower(handle) {
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
			b.WriteRune(c)
		case c == '.' || c == '-' || c == '_':
			if b.Len() > 0 {
				b.WriteRune(c)
			}
		case b.Len() > 0:
			b.WriteByte('-')
		}
	}
	name := cut(strings.TrimRight(b.String(), "-"), 32)
	if len(name) < 2 {
		name = "user" + name
	}
	return name
}

// freeUsername returns name, or name with a number on the end if someone or
// some group already has it.
func (r *run) freeUsername(name string) (string, error) {
	try := name
	for n := 2; ; n++ {
		taken := model.ReservedName(try) || !model.ValidUsername(try)
		if !taken {
			if _, err := r.Store.Accounts().ByUsername(r.ctx, try); err == nil {
				taken = true
			} else if !errors.Is(err, model.ErrNotFound) {
				return "", err
			}
		}
		if !taken {
			if _, err := r.Store.Groups().ByHandle(r.ctx, try); err == nil {
				taken = true
			} else if !errors.Is(err, model.ErrNotFound) {
				return "", err
			}
		}
		if !taken {
			return try, nil
		}
		suffix := fmt.Sprint(n)
		try = cut(name, 32-len(suffix)) + suffix
	}
}

// known finds an account made by an earlier run: by the record, or by the
// identifier a run would have given it.
func (r *run) known(slackID string, id model.AccountID) (model.Account, bool, error) {
	for _, try := range []model.AccountID{r.rec.Users[slackID], id} {
		if try == "" {
			continue
		}
		acc, err := r.Store.Accounts().Get(r.ctx, try)
		if err == nil {
			return acc, true, nil
		}
		if !errors.Is(err, model.ErrNotFound) {
			return acc, false, err
		}
	}
	return model.Account{}, false, nil
}

func (r *run) remember(slackID string, acc model.Account) {
	r.users[slackID] = acc
	r.rec.Users[slackID] = acc.ID
	if !acc.Removed() {
		r.byName[acc.Username] = acc.ID
	}
}

// people makes an account for every user of the export.
func (r *run) people() error {
	if owner, found, err := r.Store.Accounts().Owner(r.ctx); err != nil {
		return err
	} else if found {
		r.owner = owner.ID
	}
	lib, err := r.Store.Conversations().EnsureLibrary(r.ctx)
	if err != nil {
		return err
	}
	r.library = lib.ID
	for _, u := range r.a.users {
		if u.ID == "" {
			continue
		}
		if err := r.person(u); err != nil {
			return fmt.Errorf("importing the Slack user %s: %w", u.ID, err)
		}
	}
	return nil
}

func (r *run) person(u slackUser) error {
	created := u.Updated * 1000
	if created <= 0 {
		created = int64(r.now)
	}
	id := detID[model.AccountID]("user", u.ID, created)
	if acc, found, err := r.known(u.ID, id); err != nil || found {
		if found {
			r.remember(u.ID, acc)
		}
		return err
	}
	role := model.RoleMember
	switch {
	case u.IsBot || u.ID == "USLACKBOT":
		role = model.RoleBot
	case u.IsRestricted || u.IsUltraRestricted:
		role = model.RoleGuest
	}
	handle := u.Name
	if handle == "" {
		handle = u.ID
	}
	display := u.Profile.DisplayName
	for _, alt := range []string{u.Profile.RealName, u.RealName, handle} {
		if strings.TrimSpace(display) == "" {
			display = alt
		}
	}
	acc, err := r.makeAccount(id, handle, display, role, model.Time(created), u.Deleted, func(a *model.Account) {
		a.Title, a.TimeZone = cut(strings.TrimSpace(u.Profile.Title), 100), u.TZ
	})
	if err != nil {
		return err
	}
	r.remember(u.ID, acc)
	if role == model.RoleBot {
		r.rep.Bots++
		return nil
	}
	if u.Deleted {
		return nil
	}
	r.rep.People++
	// The address is hashed under the server's key and then forgotten.
	if r.EmailHash != nil && strings.TrimSpace(u.Profile.Email) != "" {
		if err := r.Store.Identities().AddImportClaim(r.ctx, acc.ID, r.EmailHash(u.Profile.Email)); err != nil {
			return err
		}
	}
	if addr := u.avatar(); addr != "" && !r.NoFiles {
		r.avatar(acc, addr)
	}
	return nil
}

// makeAccount stores one imported account: a person or guest nobody has
// claimed, a bot with no token, or a removed account for someone deleted in
// Slack, so that their messages have an author.
func (r *run) makeAccount(id model.AccountID, handle, display string, role model.Role, created model.Time, removed bool, more func(*model.Account)) (model.Account, error) {
	username, err := r.freeUsername(usernameFrom(handle))
	if err != nil {
		return model.Account{}, err
	}
	clean, ok := model.CleanDisplayName(cut(strings.TrimSpace(display), 64))
	if !ok {
		clean = username
	}
	acc := model.Account{ID: id, Username: username, DisplayName: clean, Role: role, CreatedAt: created}
	if more != nil {
		more(&acc)
	}
	err = r.Store.Tx(r.ctx, func(tx store.Tx) error {
		if role == model.RoleBot {
			// A bot has nobody to claim it. It has no token, so it can
			// do nothing until an admin gives it one.
			if _, err := tx.Accounts().Create(r.ctx, &acc); err != nil {
				return err
			}
			if r.owner != "" {
				if err := tx.Bots().Create(r.ctx, store.BotInfo{AccountID: acc.ID, Description: "Imported from Slack", CreatedBy: r.owner}); err != nil {
					return err
				}
			}
		} else if err := tx.Accounts().CreateUnclaimed(r.ctx, &acc); err != nil {
			return err
		}
		if removed {
			if _, err := tx.Accounts().Remove(r.ctx, acc.ID, "", r.now); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return acc, err
	}
	return r.Store.Accounts().Get(r.ctx, acc.ID)
}

// avatar fetches a profile picture and makes it the account's avatar. A
// picture that cannot be had is not an error: the account has none.
func (r *run) avatar(acc model.Account, addr string) {
	got, err := r.fetch(addr, maxAvatarBytes)
	if err != nil {
		r.Log.Info("a profile picture could not be fetched", "user", acc.Username, "err", err)
		return
	}
	defer got.discard()
	if got.kind != model.FileImage {
		return
	}
	f := model.File{ID: detID[model.FileID]("avatar", string(acc.ID), int64(acc.CreatedAt)), ConversationID: r.library, UploaderID: acc.ID,
		Purpose: "avatar", Name: "avatar", MediaType: got.mediaType, Kind: got.kind, Width: got.width, Height: got.height, CreatedAt: r.now}
	if err := got.store(r.ctx, r.Blobs, &f); err != nil {
		r.Log.Info("a profile picture could not be stored", "err", err)
		return
	}
	err = r.Store.Tx(r.ctx, func(tx store.Tx) error {
		if err := tx.Files().Insert(r.ctx, &f); err != nil {
			return err
		}
		_, err := tx.Accounts().Update(r.ctx, acc.ID, func(a *model.Account) error {
			a.AvatarFileID = f.ID
			return nil
		})
		return err
	})
	if err != nil {
		dropBlobs(r.ctx, r.Blobs, f)
		r.Log.Info("a profile picture could not be stored", "err", err)
	}
}

// author returns the account a message is by, making one for a bot that
// only appears as an author, or for a user the export does not list.
func (r *run) author(m slackMessage) (model.Account, error) {
	if m.User != "" {
		if acc, ok := r.users[m.User]; ok {
			return acc, nil
		}
		// Not in users.json: someone from another workspace, or long
		// gone. Their messages still need an author.
		key := m.User
		id := detID[model.AccountID]("user", key, 0)
		if acc, found, err := r.known(key, id); err != nil || found {
			if found {
				r.remember(key, acc)
			}
			return acc, err
		}
		acc, err := r.makeAccount(id, "slack-"+m.User, "Unknown ("+m.User+")", model.RoleMember, r.now, true, nil)
		if err == nil {
			r.remember(key, acc)
		}
		return acc, err
	}
	name := m.Username
	if name == "" {
		name = m.BotID
	}
	if name == "" {
		name = "slack-bot"
	}
	key := "bot:" + m.BotID
	if m.BotID == "" {
		key = "bot:" + name
	}
	if acc, ok := r.users[key]; ok {
		return acc, nil
	}
	id := detID[model.AccountID]("user", key, 0)
	if acc, found, err := r.known(key, id); err != nil || found {
		if found {
			r.remember(key, acc)
		}
		return acc, err
	}
	acc, err := r.makeAccount(id, name, name, model.RoleBot, r.now, false, nil)
	if err == nil {
		r.remember(key, acc)
		r.rep.Bots++
	}
	return acc, err
}

// ----------------------------------------------------------------- emoji

// emoji imports the custom emoji an export lists. Aliases are skipped, and
// so is an image that is not within the limits of an emoji here.
func (r *run) emoji() error {
	if r.NoFiles || len(r.a.emoji) == 0 {
		return nil
	}
	by := r.owner
	names := make([]string, 0, len(r.a.emoji))
	for name := range r.a.emoji {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		addr := r.a.emoji[name]
		if strings.HasPrefix(addr, "alias:") || !model.ValidEmojiName(name) {
			continue
		}
		if _, err := r.Store.Emoji().ByName(r.ctx, name); err == nil {
			continue
		} else if !errors.Is(err, model.ErrNotFound) {
			return err
		}
		if by == "" {
			r.Log.Info("custom emoji need an owner to belong to; set the server up first", "emoji", name)
			return nil
		}
		got, err := r.fetch(addr, model.MaxEmojiBytes)
		if err != nil {
			r.Log.Info("an emoji image could not be fetched", "emoji", name, "err", err)
			continue
		}
		if got.kind != model.FileImage || got.width == 0 || got.width > model.MaxEmojiPixels || got.height > model.MaxEmojiPixels {
			got.discard()
			continue
		}
		ext := map[string]string{blob.TypePNG: ".png", blob.TypeJPEG: ".jpg", blob.TypeGIF: ".gif", blob.TypeWebP: ".webp"}[got.mediaType]
		f := model.File{ID: detID[model.FileID]("emoji", name, int64(r.now)), ConversationID: r.library, UploaderID: by, Purpose: model.PurposeEmoji,
			Name: name + ext, MediaType: got.mediaType, Kind: got.kind, Width: got.width, Height: got.height, CreatedAt: r.now}
		err = got.store(r.ctx, r.Blobs, &f)
		got.discard()
		if err != nil {
			return err
		}
		e := model.Emoji{Name: name, CreatedByID: by, CreatedAt: r.now}
		err = r.Store.Tx(r.ctx, func(tx store.Tx) error {
			if err := tx.Files().Insert(r.ctx, &f); err != nil {
				return err
			}
			e.FileID = f.ID
			return tx.Emoji().Create(r.ctx, &e)
		})
		if err != nil {
			dropBlobs(r.ctx, r.Blobs, f)
			return fmt.Errorf("the emoji %s: %w", name, err)
		}
		r.rec.Emoji[name] = e.ID
		r.rep.Emoji++
	}
	return nil
}

// --------------------------------------------------------- conversations

// present returns the accounts of the Slack users given that can be in a
// conversation: known, and not removed.
func (r *run) present(slackIDs []string) []model.AccountID {
	var out []model.AccountID
	seen := map[model.AccountID]bool{}
	for _, id := range slackIDs {
		if acc, ok := r.users[id]; ok && !acc.Removed() && !seen[acc.ID] {
			seen[acc.ID] = true
			out = append(out, acc.ID)
		}
	}
	return out
}

func (r *run) conversations() error {
	for _, c := range r.a.channels {
		if err := r.conversation(c); err != nil {
			return fmt.Errorf("importing the Slack conversation %s: %w", c.ID, err)
		}
	}
	return nil
}

func (r *run) conversation(c slackChannel) error {
	created := model.Time(c.Created * 1000)
	if created <= 0 {
		created = r.now
	}
	id := detID[model.ConversationID]("channel", c.ID, int64(created))
	for _, try := range []model.ConversationID{r.rec.Channels[c.ID], id} {
		if try == "" {
			continue
		}
		conv, err := r.Store.Conversations().Get(r.ctx, try)
		if err == nil {
			r.channels[c.ID] = conv
			r.rec.Channels[c.ID] = conv.ID
			return nil
		}
		if !errors.Is(err, model.ErrNotFound) {
			return err
		}
	}
	// Without the record, a channel that went into one already there (the
	// empty #general, an ad-hoc channel of the same people) is found by
	// where its messages are.
	if had, found, err := r.byMessages(c); err != nil {
		return err
	} else if found {
		r.channels[c.ID], r.rec.Channels[c.ID] = had, had.ID
		return nil
	}
	members := r.present(c.Members)
	conv := model.Conversation{ID: id, Kind: model.KindChannel, CreatedAt: created}
	if creator, ok := r.users[c.Creator]; ok {
		conv.CreatedByID = creator.ID
	}
	named := c.kind == "public" || c.kind == "private"
	if named {
		name, ok := model.CleanChannelName(c.Name)
		if !ok {
			name = "imported-" + strings.ToLower(c.ID)
		}
		conv.Name, conv.Private = name, c.kind == "private"
		conv.Topic = cut(strings.TrimSpace(convert(c.Topic.Value, r)), model.MaxTopic)
		if conv.Topic == "" {
			conv.Topic = cut(strings.TrimSpace(convert(c.Purpose.Value, r)), model.MaxTopic)
		}
		// A fresh server has an empty #general. Slack's general channel
		// goes into it instead of beside it.
		if c.IsGeneral || c.Name == "general" {
			if had, ok, err := r.emptyGeneral(); err != nil {
				return err
			} else if ok {
				return r.fill(c, had, members, false)
			}
		}
	} else {
		// A direct message, or a group one: an ad-hoc channel with those
		// members. One with exactly the same people may already be there.
		if len(members) == 0 {
			return nil
		}
		conv.Private, conv.MemberKey = true, store.AdhocMemberKey(members)
		if had, err := r.Store.Conversations().ByMemberKey(r.ctx, conv.MemberKey); err == nil {
			r.channels[c.ID], r.rec.Channels[c.ID] = had, had.ID
			return nil
		} else if !errors.Is(err, model.ErrNotFound) {
			return err
		}
	}
	return r.fill(c, conv, members, true)
}

// byMessages finds the conversation an earlier run put a Slack
// conversation's messages in, by looking for its first message.
func (r *run) byMessages(c slackChannel) (model.Conversation, bool, error) {
	msgs, err := r.a.messages(c)
	if err != nil {
		return model.Conversation{}, false, err
	}
	tried := 0
	for _, m := range msgs {
		if !imported[m.Subtype] {
			continue
		}
		// A handful is enough to tell; a first run finds none.
		if tried++; tried > 20 {
			break
		}
		ms, _ := millis(m.TS)
		had, err := r.Store.Messages().Get(r.ctx, detID[model.MessageID]("message", c.ID+"|"+m.TS, ms))
		if errors.Is(err, model.ErrNotFound) {
			// The first few may have been empty and never stored.
			continue
		}
		if err != nil {
			return model.Conversation{}, false, err
		}
		conv, err := r.Store.Conversations().Get(r.ctx, had.ConversationID)
		return conv, err == nil, err
	}
	return model.Conversation{}, false, nil
}

// emptyGeneral finds a channel called general that has no messages.
func (r *run) emptyGeneral() (model.Conversation, bool, error) {
	list, err := r.Store.Conversations().ByNameAll(r.ctx, "general")
	if err != nil {
		return model.Conversation{}, false, err
	}
	for _, c := range list {
		msgs, _, err := r.Store.Messages().List(r.ctx, c.ID, store.MessageList{SeqPage: store.SeqPage{Limit: 1}})
		if err != nil {
			return c, false, err
		}
		if len(msgs) == 0 && !c.IsShared() {
			return c, true, nil
		}
	}
	return model.Conversation{}, false, nil
}

// fill makes a conversation (unless it is there already) and puts its
// members in: the creator as a manager, everyone else as a member.
func (r *run) fill(c slackChannel, conv model.Conversation, members []model.AccountID, create bool) error {
	err := r.Store.Tx(r.ctx, func(tx store.Tx) error {
		if create {
			if err := tx.Conversations().Create(r.ctx, &conv); err != nil {
				return err
			}
		}
		// The creator goes in first: a channel is never without a
		// manager, so whoever is first in would be made one.
		sort.SliceStable(members, func(i, j int) bool { return members[i] == conv.CreatedByID && members[j] != conv.CreatedByID })
		for i, m := range members {
			level := model.LevelMember
			// The creator manages a named channel; in an ad-hoc one the
			// first member does, as when one is started here.
			if m == conv.CreatedByID || (conv.Name == "" && conv.CreatedByID == "" && i == 0) {
				level = model.LevelManager
			}
			if _, found, err := tx.Members().Get(r.ctx, conv.ID, m); err != nil {
				return err
			} else if found {
				continue
			}
			if _, err := tx.Members().AddDirect(r.ctx, conv.ID, m, level, conv.CreatedAt); err != nil {
				return err
			}
		}
		if c.IsArchived && create {
			if _, err := tx.Conversations().Update(r.ctx, conv.ID, func(c *model.Conversation) error {
				c.ArchivedAt = r.now
				return nil
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	if conv, err = r.Store.Conversations().Get(r.ctx, conv.ID); err != nil {
		return err
	}
	r.channels[c.ID], r.rec.Channels[c.ID] = conv, conv.ID
	if create {
		r.rep.Channels++
	}
	return nil
}

// -------------------------------------------------------------- messages

// username and channel are what the text converter asks (names).
func (r *run) username(slackID string) (string, bool) {
	acc, ok := r.users[slackID]
	if !ok || acc.Removed() {
		return "", false
	}
	return acc.Username, true
}

func (r *run) channel(slackID string) (string, bool) {
	c, ok := r.channels[slackID]
	if !ok || !c.Named() {
		return "", false
	}
	return string(c.ID), true
}

// resolver works out the entities of an imported text as for any message,
// so mentions and channel links draw as what they are. It has no author to
// ask permission for: the text was written elsewhere, and nobody is
// notified by an import.
type resolver struct{ r *run }

func (v resolver) Account(username string) (model.AccountID, bool) {
	id, ok := v.r.byName[username]
	return id, ok
}
func (v resolver) RemoteAccount(string, string) (model.AccountID, bool) { return "", false }
func (v resolver) Group(handle string) (model.GroupID, bool) {
	g, err := v.r.Store.Groups().ByHandle(v.r.ctx, handle)
	return g.ID, err == nil
}
func (v resolver) Broadcast(string) bool { return true }
func (v resolver) Channel(id model.ConversationID) (string, bool) {
	c, err := v.r.Store.Conversations().Get(v.r.ctx, id)
	if err != nil || !c.Named() {
		return "", false
	}
	return c.Name, true
}
func (v resolver) Emoji(name, server string) (model.EmojiID, bool) {
	if server != "" {
		return "", false
	}
	e, err := v.r.Store.Emoji().ByName(v.r.ctx, name)
	return e.ID, err == nil
}

func (r *run) entities(text string) []model.Entity {
	var out []model.Entity
	for _, e := range markdown.Resolve(markdown.Parse(text), resolver{r}) {
		// A checklist needs its item rows, which only a posted message
		// gets. An imported "- [ ]" stays as written.
		if e.Type != model.EntityTask {
			out = append(out, e)
		}
	}
	return out
}

func (r *run) messages() error {
	for _, c := range r.a.channels {
		conv, ok := r.channels[c.ID]
		if !ok {
			continue
		}
		if err := r.ctx.Err(); err != nil {
			return err
		}
		n, err := r.channelMessages(c, conv)
		if err != nil {
			return fmt.Errorf("importing the messages of %s: %w", c.folder, err)
		}
		if n > 0 {
			name := conv.Name
			if name == "" {
				name = "a direct conversation"
			}
			fmt.Fprintf(r.Out, "  %s: %d messages\n", name, n)
		}
		if err := r.saveRecord(); err != nil {
			return err
		}
	}
	return nil
}

// channelMessages imports one conversation's messages in the order they
// were sent, then its pins, then sets everyone's read position to the end,
// so nobody starts with years of unread messages.
func (r *run) channelMessages(c slackChannel, conv model.Conversation) (int, error) {
	msgs, err := r.a.messages(c)
	if err != nil {
		return 0, err
	}
	// ids are the imported messages by Slack timestamp, for finding a
	// reply's parent and a pin's message; top says which are top-level.
	ids := map[string]model.MessageID{}
	authors := map[string]model.AccountID{}
	top := map[string]bool{}
	made := 0
	for _, m := range msgs {
		if !imported[m.Subtype] {
			r.rep.Notices++
			continue
		}
		ms, _ := millis(m.TS)
		id := detID[model.MessageID]("message", c.ID+"|"+m.TS, ms)
		isReply := m.ThreadTS != "" && m.ThreadTS != m.TS && top[m.ThreadTS]
		if had, err := r.Store.Messages().Get(r.ctx, id); err == nil {
			ids[m.TS], authors[m.TS], top[m.TS] = id, had.AuthorID, had.ParentID == ""
			continue
		} else if !errors.Is(err, model.ErrNotFound) {
			return made, err
		}
		author, err := r.author(m)
		if err != nil {
			return made, err
		}
		msg := model.Message{ID: id, ConversationID: conv.ID, AuthorID: author.ID, CreatedAt: model.Time(ms),
			Via: &model.Via{Kind: "import", Name: "Slack"}}
		if isReply {
			msg.ParentID = ids[m.ThreadTS]
		}
		text := convert(m.Text, r)
		if m.Subtype == "me_message" && strings.TrimSpace(text) != "" {
			text = "*" + strings.TrimSpace(text) + "*"
		}
		msg.Cards = r.cards(m.Attachments)

		// Files are fetched before the transaction: it never spans a
		// network call.
		var files []fetched
		for i, sf := range m.Files {
			got, err := r.file(sf)
			if err != nil {
				if !r.NoFiles {
					r.Log.Info("a file could not be fetched", "file", sf.name(), "err", err)
				}
				r.rep.FilesMissing++
				if text != "" {
					text += "\n"
				}
				text += "File not available: " + strings.ReplaceAll(sf.name(), "\n", " ")
				continue
			}
			got.row = model.File{ID: detID[model.FileID]("file", fmt.Sprintf("%s|%s|%d|%s", c.ID, m.TS, i, sf.ID), ms),
				ConversationID: conv.ID, MessageID: id, UploaderID: author.ID, Purpose: "attachment", Position: len(files),
				Name: cleanName(sf.name()), MediaType: got.mediaType, Kind: got.kind, Width: got.width, Height: got.height,
				DurationSeconds: got.duration, PreviewText: got.previewText, CreatedAt: model.Time(ms)}
			files = append(files, got)
		}
		discard := func() {
			for _, f := range files {
				f.discard()
			}
		}
		msg.Text = cut(text, model.MaxMessageText)
		if strings.TrimSpace(msg.Text) == "" && len(files) == 0 && len(msg.Cards) == 0 {
			discard()
			continue
		}
		msg.Entities = r.entities(msg.Text)
		for i := range files {
			if err := files[i].store(r.ctx, r.Blobs, &files[i].row); err != nil {
				discard()
				return made, err
			}
		}
		reactions := r.reactions(m)
		err = r.Store.Tx(r.ctx, func(tx store.Tx) error {
			seq, err := tx.Conversations().NextSeq(r.ctx, conv.ID)
			if err != nil {
				return err
			}
			msg.Seq = seq
			// No mention rows: nobody is notified by an import.
			if err := tx.Messages().Insert(r.ctx, &msg, nil); err != nil {
				return err
			}
			if m.Edited != nil {
				at, ok := millis(m.Edited.TS)
				if !ok {
					at = ms
				}
				w, ok := tx.(store.ImportWriter)
				if !ok {
					return errors.New("this database cannot mark an imported message as edited")
				}
				if err := w.SetEditedAt(r.ctx, id, model.Time(at)); err != nil {
					return err
				}
			}
			for i := range files {
				if err := tx.Files().Insert(r.ctx, &files[i].row); err != nil {
					return err
				}
			}
			for _, re := range reactions {
				if _, err := tx.Reactions().Add(r.ctx, id, re.account, re.emoji, model.Time(ms)); err != nil {
					return err
				}
			}
			return nil
		})
		discard()
		if err != nil {
			for _, f := range files {
				dropBlobs(r.ctx, r.Blobs, f.row)
			}
			return made, fmt.Errorf("the message of %s: %w", m.TS, err)
		}
		ids[m.TS], authors[m.TS], top[m.TS] = id, author.ID, msg.ParentID == ""
		made++
		r.rep.Messages++
		r.rep.Files += len(files)
		r.rep.Reactions += len(reactions)
	}

	for _, p := range c.Pins {
		msg, ok := ids[p.ID]
		if !ok {
			continue
		}
		by := authors[p.ID]
		if acc, ok := r.users[p.User]; ok {
			by = acc.ID
		}
		at := model.Time(p.Created * 1000)
		if at <= 0 {
			at = r.now
		}
		had, err := r.Store.Pins().List(r.ctx, conv.ID)
		if err != nil {
			return made, err
		}
		pinned := false
		for _, h := range had {
			pinned = pinned || h.MessageID == msg
		}
		if pinned {
			continue
		}
		if _, err := r.Store.Pins().Add(r.ctx, conv.ID, msg, by, at); err != nil {
			// A full pin list is not a reason to stop an import.
			r.Log.Info("a pin could not be imported", "err", err)
			continue
		}
		r.rep.Pins++
	}

	if made > 0 {
		fresh, err := r.Store.Conversations().Get(r.ctx, conv.ID)
		if err != nil {
			return made, err
		}
		members, err := r.Store.Members().IDs(r.ctx, conv.ID)
		if err != nil {
			return made, err
		}
		for _, m := range members {
			if _, err := r.Store.Members().SetRead(r.ctx, conv.ID, m, fresh.LastSeq, true); err != nil {
				return made, err
			}
		}
	}
	return made, nil
}

type reaction struct {
	account model.AccountID
	emoji   string
}

// reactions lists a message's reactions as rows to store. One by a user the
// export does not know is dropped, and so is anything past the number of
// different emoji a message may carry.
func (r *run) reactions(m slackMessage) []reaction {
	var out []reaction
	kinds := map[string]bool{}
	seen := map[reaction]bool{}
	for _, re := range m.Reactions {
		emoji := reactionEmoji(re.Name)
		if !kinds[emoji] && len(kinds) >= model.MaxEmojiPerMessage {
			continue
		}
		for _, u := range re.Users {
			acc, ok := r.users[u]
			if !ok {
				continue
			}
			row := reaction{acc.ID, emoji}
			if seen[row] {
				continue
			}
			seen[row], kinds[emoji] = true, true
			out = append(out, row)
		}
	}
	return out
}

// The colours Slack names, and a colour to stand for each of Churr's, for
// finding the nearest to one given as a hex value.
var (
	slackColors = map[string]string{"good": "green", "warning": "yellow", "danger": "red"}
	cardColors  = map[string][3]int{
		"lavender": {0xb4, 0xa7, 0xff}, "cyan": {0x4d, 0xd0, 0xe1}, "pink": {0xff, 0x7e, 0xb6}, "yellow": {0xf5, 0xc5, 0x42},
		"green": {0x4c, 0xaf, 0x50}, "red": {0xe5, 0x39, 0x35}, "grey": {0x9e, 0x9e, 0x9e},
	}
)

// cardColor maps an attachment's colour to the nearest a card may have.
func cardColor(c string) string {
	c = strings.ToLower(strings.TrimSpace(c))
	if named, ok := slackColors[c]; ok {
		return named
	}
	c = strings.TrimPrefix(c, "#")
	if len(c) == 3 {
		c = string([]byte{c[0], c[0], c[1], c[1], c[2], c[2]})
	}
	b, err := hex.DecodeString(c)
	if err != nil || len(b) != 3 {
		return "grey"
	}
	best, bestDist := "grey", -1
	for _, name := range model.CardColors {
		rgb := cardColors[name]
		d := 0
		for i := range rgb {
			d += (rgb[i] - int(b[i])) * (rgb[i] - int(b[i]))
		}
		if bestDist < 0 || d < bestDist {
			best, bestDist = name, d
		}
	}
	return best
}

func webAddress(s string) bool {
	return strings.HasPrefix(s, "https://") || strings.HasPrefix(s, "http://")
}

// cards turns attachments, Slack's older rich format, into cards. An image
// at another site is not fetched: its address goes in the footer.
func (r *run) cards(atts []slackAttachment) []model.Card {
	var out []model.Card
	for _, a := range atts {
		if len(out) == model.MaxCardsPerMessage {
			break
		}
		card := model.Card{Color: cardColor(a.Color), Title: cut(strings.TrimSpace(convert(a.Title, r)), 200)}
		if webAddress(a.TitleLink) {
			card.URL = a.TitleLink
		}
		text := strings.TrimSpace(convert(a.Text, r))
		if pre := strings.TrimSpace(convert(a.Pretext, r)); pre != "" {
			text = strings.TrimSpace(pre + "\n" + text)
		}
		if text == "" && card.Title == "" {
			text = strings.TrimSpace(convert(a.Fallback, r))
		}
		card.Text = cut(text, 4000)
		if a.AuthorName != "" {
			card.Author = &model.CardAuthor{Name: cut(a.AuthorName, 100)}
		}
		for _, f := range a.Fields {
			if len(card.Fields) == 10 {
				break
			}
			card.Fields = append(card.Fields, model.CardField{Label: cut(f.Title, 100), Value: cut(convert(f.Value, r), 1000), Short: f.Short})
		}
		footer := strings.TrimSpace(a.Footer)
		image := a.ImageURL
		if image == "" {
			image = a.ThumbURL
		}
		if webAddress(image) {
			if footer != "" {
				footer += " · "
			}
			footer += image
		}
		card.Footer = cut(footer, 300)
		if sec, err := a.TS.Int64(); err == nil && sec > 0 {
			card.At = model.Time(sec * 1000)
		}
		if card.Title == "" && card.Text == "" {
			if card.Footer == "" {
				continue
			}
			card.Text = card.Footer
			card.Footer = ""
		}
		out = append(out, card)
	}
	return out
}

// ---------------------------------------------------------------- claims

// claims makes sure every imported person nobody has claimed has a claim
// link, and lists them. A link an earlier run made, or an admin since, is
// kept and listed again: making a fresh one would cancel one already handed
// out.
func (r *run) claims() error {
	usable := map[model.AccountID]model.Invite{}
	page := store.Page{Limit: model.MaxLimit}
	for {
		list, next, err := r.Store.Invites().List(r.ctx, "", page)
		if err != nil {
			return err
		}
		for _, inv := range list {
			if inv.ClaimsAccountID != "" && inv.Usable(r.now) {
				if _, ok := usable[inv.ClaimsAccountID]; !ok {
					usable[inv.ClaimsAccountID] = inv
				}
			}
		}
		if next == "" {
			break
		}
		page.Cursor = next
	}
	var keys []string
	for k := range r.users {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		acc, err := r.Store.Accounts().Get(r.ctx, r.users[k].ID)
		if err != nil {
			return err
		}
		if acc.Removed() || !acc.ClaimedAt.IsZero() || (acc.Role != model.RoleMember && acc.Role != model.RoleGuest) {
			continue
		}
		inv, ok := usable[acc.ID]
		if !ok {
			inv = model.Invite{Code: secret.NewInviteCode(), Role: acc.Role, ClaimsAccountID: acc.ID, MaxUses: 1,
				ExpiresAt: r.now.Add(claimLife), CreatedAt: r.now}
			if err := r.Store.Accounts().ClaimLinkFor(r.ctx, &inv); err != nil {
				return fmt.Errorf("the claim link of %s: %w", acc.Username, err)
			}
		}
		r.rep.Claims = append(r.rep.Claims, Claim{AccountID: acc.ID, Username: acc.Username, DisplayName: acc.DisplayName,
			URL: strings.TrimSuffix(r.Origin, "/") + "/join/" + inv.Code, ExpiresAt: inv.ExpiresAt})
	}
	sort.Slice(r.rep.Claims, func(i, j int) bool { return r.rep.Claims[i].Username < r.rep.Claims[j].Username })
	return nil
}

// finish writes the one line of the audit log an import leaves.
func (r *run) finish() error {
	return r.Store.Audit().Append(r.ctx, model.AuditEntry{At: r.Now(), ActorKind: "cli", Action: "import.slack",
		Detail: map[string]any{"people": r.rep.People, "bots": r.rep.Bots, "channels": r.rep.Channels, "messages": r.rep.Messages,
			"files": r.rep.Files, "files_missing": r.rep.FilesMissing, "reactions": r.rep.Reactions, "pins": r.rep.Pins, "emoji": r.rep.Emoji}})
}
