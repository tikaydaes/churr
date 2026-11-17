package people

import (
	"context"
	"errors"
	"io"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/tikaydaes/churr/internal/blob"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/perm"
	"github.com/tikaydaes/churr/internal/store"
)

// Limits on the profile fields the documents give no number for.
const (
	maxTitle    = 100
	maxPronouns = 40
	// MaxAvatarBytes is the largest avatar accepted.
	MaxAvatarBytes = 1 << 20
)

// ProfileChange is what UpdateMe takes. Nil fields stay as they are.
type ProfileChange struct {
	Username    *string
	DisplayName *string
	Title       *string
	Pronouns    *string
	TimeZone    *string
}

// cleanLine checks a short line of free text: no control characters and not
// too long.
func cleanLine(field, v string, max int) (string, error) {
	v = strings.TrimSpace(v)
	if utf8.RuneCountInString(v) > max || strings.IndexFunc(v, unicode.IsControl) >= 0 {
		return "", model.Invalid(field, "That is too long, or has characters that cannot be shown.")
	}
	return v, nil
}

// own asks perm whether the actor may change its own profile and status.
func (s *Service) own(ctx context.Context, actor model.Account, a perm.ServerAction) error {
	set, err := s.perm.Settings(ctx)
	if err != nil {
		return err
	}
	return perm.CheckServer(actor, a, set)
}

// UpdateMe changes the caller's profile and tells everyone who can see the
// account.
func (s *Service) UpdateMe(ctx context.Context, actor model.Account, in ProfileChange) (model.Me, error) {
	if err := s.own(ctx, actor, perm.OwnProfile); err != nil {
		return model.Me{}, err
	}
	var username, display, title, pronouns, zone *string
	if in.Username != nil {
		u := strings.ToLower(strings.TrimSpace(*in.Username))
		if !model.ValidUsername(u) {
			return model.Me{}, model.Invalid("username", "A username is 2 to 32 characters: letters, digits, dot, - and _, starting with a letter or digit.")
		}
		username = &u
	}
	if in.DisplayName != nil {
		d, ok := model.CleanDisplayName(*in.DisplayName)
		if !ok {
			return model.Me{}, model.Invalid("display_name", "A display name is 1 to 64 characters with no control characters.")
		}
		display = &d
	}
	if in.Title != nil {
		t, err := cleanLine("title", *in.Title, maxTitle)
		if err != nil {
			return model.Me{}, err
		}
		title = &t
	}
	if in.Pronouns != nil {
		p, err := cleanLine("pronouns", *in.Pronouns, maxPronouns)
		if err != nil {
			return model.Me{}, err
		}
		pronouns = &p
	}
	if in.TimeZone != nil {
		z := strings.TrimSpace(*in.TimeZone)
		if z != "" {
			if _, err := time.LoadLocation(z); err != nil || z == "Local" {
				return model.Me{}, model.Invalid("time_zone", "That is not an IANA time zone name, such as Europe/Berlin.")
			}
		}
		zone = &z
	}
	before := s.viewOf(actor, s.now())
	a, err := s.store.Accounts().Update(ctx, actor.ID, func(a *model.Account) error {
		if username != nil {
			a.Username = *username
		}
		if display != nil {
			a.DisplayName = *display
		}
		if title != nil {
			a.Title = *title
		}
		if pronouns != nil {
			a.Pronouns = *pronouns
		}
		if zone != nil {
			a.TimeZone = *zone
		}
		return nil
	})
	if err != nil {
		return model.Me{}, err
	}
	if a.Username != actor.Username {
		// A rename is written down: the old name is held for this
		// account for model.UsernameHold, and an admin looking for who
		// was called what finds it here.
		if err := s.audit(ctx, s.store, actor, "account.renamed", "account", string(actor.ID),
			map[string]any{"from": actor.Username, "to": a.Username}); err != nil {
			s.log.Warn("could not write a rename to the audit log", "err", err)
		}
	}
	s.tellProfile(ctx, a)
	// Quiet hours run in the account's time zone, so moving zone can
	// start or end a quiet period.
	s.announce(ctx, a, &before)
	return s.Me(ctx, a)
}

// SetAvatar stores an image as the caller's avatar, replacing the one it
// had. The file lives in the library conversation, which any signed-in
// account may fetch from (docs/PERMISSIONS.md decision P5).
func (s *Service) SetAvatar(ctx context.Context, actor model.Account, content io.Reader) (model.Me, error) {
	if err := s.own(ctx, actor, perm.OwnProfile); err != nil {
		return model.Me{}, err
	}
	if s.blobs == nil {
		return model.Me{}, model.ErrUnavailable
	}
	// The first bytes say what the content is, whatever the upload called
	// itself.
	head := make([]byte, 512)
	n, err := io.ReadFull(content, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return model.Me{}, tooLargeOr(err, model.BadRequest("The upload could not be read."))
	}
	head = head[:n]
	mediaType, kind := blob.Sniff(head)
	if kind != "image" {
		return model.Me{}, model.Invalid("file", "An avatar must be an image.")
	}
	key, size, sum, err := s.blobs.Put(ctx, blob.LimitReader(io.MultiReader(strings.NewReader(string(head)), content), MaxAvatarBytes))
	if err != nil {
		return model.Me{}, tooLargeOr(err, err)
	}
	lib, err := s.store.Conversations().EnsureLibrary(ctx)
	if err != nil {
		s.blobs.Delete(ctx, key)
		return model.Me{}, err
	}
	f := model.File{ConversationID: lib.ID, UploaderID: actor.ID, Purpose: "avatar", Name: "avatar", Size: size,
		MediaType: mediaType, Kind: kind, SHA256: sum, BlobKey: key, CreatedAt: s.now()}
	var a model.Account
	var old []string
	err = s.store.Tx(ctx, func(tx store.Tx) error {
		if err := tx.Files().Create(ctx, &f); err != nil {
			return err
		}
		var had model.FileID
		var err error
		a, err = tx.Accounts().Update(ctx, actor.ID, func(a *model.Account) error {
			had, a.AvatarFileID = a.AvatarFileID, f.ID
			return nil
		})
		if err != nil || had == "" {
			return err
		}
		gone, err := tx.Files().Delete(ctx, had)
		if err != nil && !errors.Is(err, model.ErrNotFound) {
			return err
		}
		old = store.BlobKeys(gone)
		return nil
	})
	if err != nil {
		s.blobs.Delete(ctx, key)
		return model.Me{}, err
	}
	s.removeBlobs(ctx, old)
	s.tellProfile(ctx, a)
	return s.Me(ctx, a)
}

// tooLargeOr maps the body cap being hit to too_large, and anything else to
// the fallback.
func tooLargeOr(err, fallback error) error {
	if errors.Is(err, model.ErrTooLarge) || strings.Contains(err.Error(), "request body too large") {
		return model.ErrTooLarge.With("An avatar is at most 1 MB.")
	}
	return fallback
}

func (s *Service) removeBlobs(ctx context.Context, keys []string) {
	for _, k := range keys {
		if s.blobs == nil {
			return
		}
		if err := s.blobs.Delete(ctx, k); err != nil {
			// The row is gone; the janitor's sweep finds a blob with no row.
			s.log.Warn("a blob could not be removed", "err", err)
		}
	}
}

// RemoveAvatar removes the caller's avatar. Having none is not an error.
func (s *Service) RemoveAvatar(ctx context.Context, actor model.Account) error {
	if err := s.own(ctx, actor, perm.OwnProfile); err != nil {
		return err
	}
	var a model.Account
	var old []string
	changed := false
	err := s.store.Tx(ctx, func(tx store.Tx) error {
		var had model.FileID
		var err error
		a, err = tx.Accounts().Update(ctx, actor.ID, func(a *model.Account) error {
			had, a.AvatarFileID = a.AvatarFileID, ""
			return nil
		})
		if err != nil || had == "" {
			return err
		}
		changed = true
		gone, err := tx.Files().Delete(ctx, had)
		if err != nil && !errors.Is(err, model.ErrNotFound) {
			return err
		}
		old = store.BlobKeys(gone)
		return nil
	})
	if err != nil || !changed {
		return err
	}
	s.removeBlobs(ctx, old)
	s.tellProfile(ctx, a)
	return nil
}

// Prefs returns the caller's preferences.
func (s *Service) Prefs(actor model.Account) model.Prefs { return prefsOf(actor) }

// PrefsChange is what UpdatePrefs takes. Nil fields stay as they are.
type PrefsChange struct {
	ThreadView        *string
	MeetingPauses     *bool
	AllowNotifyAnyway *bool
	HideLastSeen      *bool
	AutoApproveDirect *bool
	AutoApproveSealed *bool
}

// UpdatePrefs changes the caller's preferences. Nobody else is told what
// they are; a preference that turns the do not disturb mark on or off (a
// meeting that now pauses) shows only as the mark.
func (s *Service) UpdatePrefs(ctx context.Context, actor model.Account, in PrefsChange) (model.Prefs, error) {
	if err := s.own(ctx, actor, perm.OwnProfile); err != nil {
		return model.Prefs{}, err
	}
	if in.ThreadView != nil && *in.ThreadView != "threaded" && *in.ThreadView != "flat" {
		return model.Prefs{}, model.Invalid("thread_view", "thread_view is threaded or flat.")
	}
	before := s.viewOf(actor, s.now())
	a, err := s.store.Accounts().Update(ctx, actor.ID, func(a *model.Account) error {
		if in.ThreadView != nil {
			a.PrefsJSON = setPref(a.PrefsJSON, "thread_view", *in.ThreadView)
		}
		if in.MeetingPauses != nil {
			a.PrefsJSON = setPref(a.PrefsJSON, "meeting_pauses", *in.MeetingPauses)
		}
		if in.AllowNotifyAnyway != nil {
			a.AllowNotifyAnyway = *in.AllowNotifyAnyway
		}
		if in.HideLastSeen != nil {
			a.PrefsJSON = setPref(a.PrefsJSON, "hide_last_seen", *in.HideLastSeen)
		}
		if in.AutoApproveDirect != nil {
			a.PrefsJSON = setPref(a.PrefsJSON, "auto_approve_direct", *in.AutoApproveDirect)
		}
		if in.AutoApproveSealed != nil {
			a.PrefsJSON = setPref(a.PrefsJSON, "auto_approve_sealed", *in.AutoApproveSealed)
		}
		return nil
	})
	if err != nil {
		return model.Prefs{}, err
	}
	// The default thread view decides what counts as unread.
	s.forget(a.ID)
	s.tellMe(ctx, a)
	s.announce(ctx, a, &before)
	return prefsOf(a), nil
}
