package auth

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/tikaydaes/churr/internal/blob"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/secret"
	"github.com/tikaydaes/churr/internal/store"
)

// Setting keys this package reads that the foundation did not name.
const (
	SettingSecondFactorAccepted = "auth.second_factor_accepted"
	SettingSsoSeconds           = "sessions.sso_seconds"
	SettingS3Secret             = "files.s3_secret"
	settingS3SecretSet          = "files.s3_secret_set"
	settingIcon                 = "server.icon_file_id"
)

// setting describes one key of docs/OPERATIONS.md section 5: its default,
// how a new value is checked, and whether changing it needs a fresh check.
type setting struct {
	key   string
	def   any
	check func(raw json.RawMessage) (any, bool)
	hint  string
	fresh bool
}

func isNull(raw json.RawMessage) bool { return bytes.Equal(bytes.TrimSpace(raw), []byte("null")) }

func oneOf(values ...string) func(json.RawMessage) (any, bool) {
	return func(raw json.RawMessage) (any, bool) {
		var v string
		if json.Unmarshal(raw, &v) != nil || !contains(values, v) {
			return nil, false
		}
		return v, true
	}
}

func boolean(raw json.RawMessage) (any, bool) {
	var v *bool
	if json.Unmarshal(raw, &v) != nil || v == nil {
		return nil, false
	}
	return *v, true
}

func text(min, max int) func(json.RawMessage) (any, bool) {
	return func(raw json.RawMessage) (any, bool) {
		var v *string
		if json.Unmarshal(raw, &v) != nil || v == nil {
			return nil, false
		}
		s := strings.TrimSpace(*v)
		if n := len([]rune(s)); n < min || n > max || strings.ContainsRune(s, 0) {
			return nil, false
		}
		return s, true
	}
}

func whole(min, max int64) func(json.RawMessage) (any, bool) {
	return func(raw json.RawMessage) (any, bool) {
		var v *int64
		if json.Unmarshal(raw, &v) != nil || v == nil || *v < min || *v > max {
			return nil, false
		}
		return *v, true
	}
}

// orNull accepts null as well, meaning "none" or "no limit".
func orNull(check func(json.RawMessage) (any, bool)) func(json.RawMessage) (any, bool) {
	return func(raw json.RawMessage) (any, bool) {
		if isNull(raw) {
			return nil, true
		}
		return check(raw)
	}
}

func timeZone(raw json.RawMessage) (any, bool) {
	var v string
	if json.Unmarshal(raw, &v) != nil || v == "" || v == "Local" {
		return nil, false
	}
	if _, err := time.LoadLocation(v); err != nil {
		return nil, false
	}
	return v, true
}

func identifier(raw json.RawMessage) (any, bool) {
	var v string
	if json.Unmarshal(raw, &v) != nil || !model.ValidID(v) {
		return nil, false
	}
	return v, true
}

func proofs(raw json.RawMessage) (any, bool) {
	var list []string
	if isNull(raw) || json.Unmarshal(raw, &list) != nil {
		return nil, false
	}
	out := []string{}
	for _, p := range allProofs {
		if contains(list, p) {
			out = append(out, p)
		}
	}
	for _, v := range list {
		if !contains(allProofs, v) {
			return nil, false
		}
	}
	return out, true
}

func httpsAddress(raw json.RawMessage) (any, bool) {
	var v string
	if json.Unmarshal(raw, &v) != nil {
		return nil, false
	}
	if v == "" {
		return v, true
	}
	u, err := url.Parse(v)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return nil, false
	}
	return v, true
}

// s3Settings is the `files.s3` value.
type s3Settings struct {
	Endpoint    string `json:"endpoint"`
	Region      string `json:"region"`
	Bucket      string `json:"bucket"`
	Prefix      string `json:"prefix"`
	PathStyle   bool   `json:"path_style"`
	AccessKeyID string `json:"access_key_id"`
}

func s3Object(raw json.RawMessage) (any, bool) {
	var v s3Settings
	if strictJSON(raw, &v) != nil || v.Endpoint == "" || v.Bucket == "" {
		return nil, false
	}
	return v, true
}

const year = 366 * 24 * 3600

// settings are the keys an admin may read and change, in the order of
// docs/OPERATIONS.md section 5.
func (s *Service) settings() []setting {
	return []setting{
		{key: store.SettingServerName, def: s.cfg.Hostname, check: text(1, 64), hint: "The community's name is 1 to 64 characters."},
		{key: settingIcon, def: nil, check: orNull(identifier), hint: "The icon is a file identifier, or null."},
		{key: "server.time_zone", def: "UTC", check: timeZone, hint: "That is not an IANA time zone name."},
		{key: "welcome.text", def: "", check: text(0, model.MaxMessageText), hint: "The welcome text is too long."},

		{key: store.SettingRegistrationMode, def: "invite", check: oneOf("open", "invite", "closed"), hint: "registration.mode is open, invite or closed.", fresh: true},
		{key: store.SettingRegistrationApproval, def: false, check: boolean, fresh: true},
		{key: store.SettingMembersMayInvite, def: false, check: boolean, fresh: true},
		{key: store.SettingMemberSignIn, def: "passkey", check: oneOf("passkey", "either", "sso"), hint: "auth.member_sign_in is passkey, either or sso.", fresh: true},
		{key: store.SettingSecondFactor, def: "off", check: oneOf("off", "required"), hint: "auth.second_factor is off or required.", fresh: true},
		{key: SettingSecondFactorAccepted, def: allProofs, check: proofs, hint: "auth.second_factor_accepted is a list from passkey, totp and provider.", fresh: true},
		{key: SettingAllowAccessKeys, def: true, check: boolean, fresh: true},
		{key: store.SettingPasskeySeconds, def: int64(passkeySeconds), check: whole(3600, year), hint: "A passkey session lasts between an hour and a year.", fresh: true},
		{key: SettingSsoSeconds, def: int64(ssoSeconds), check: whole(300, 30*24*3600), hint: "A company sign-in session lasts between five minutes and 30 days.", fresh: true},

		{key: store.SettingQuickCorrection, def: int64(model.DefaultQuickCorrection), check: whole(0, 3600), hint: "The quick-correction window is 0 to 3600 seconds."},
		{key: store.SettingEditWindow, def: nil, check: orNull(whole(0, 10*year)), hint: "The edit window is a number of seconds, or null for no limit."},
		{key: store.SettingRetention, def: nil, check: orNull(whole(3600, 100*year)), hint: "Retention is at least an hour, or null to keep messages."},
		{key: store.SettingBroadcastManagers, def: false, check: boolean},
		{key: store.SettingBroadMentionLimit, def: int64(model.DefaultBroadMentionLimit), check: whole(0, 1000000), hint: "The broad mention limit is a number of members from 0 to 1,000,000; 0 is no limit."},
		{key: store.SettingLargeChannelLimit, def: int64(model.DefaultLargeChannelLimit), check: whole(0, 1000000), hint: "The large channel limit is a number of members from 0 to 1,000,000; 0 delivers every channel in full to everyone."},
		{key: store.SettingLargeFeatureLimit, def: int64(model.DefaultLargeFeatureLimit), check: whole(0, 1000000), hint: "The large feature limit is a number of members from 0 to 1,000,000; 0 is no limit."},
		{key: store.SettingMembersMayAddEmoji, def: false, check: boolean},
		{key: store.SettingBlockingEnabled, def: true, check: boolean},
		{key: store.SettingWebhooksManagersMay, def: false, check: boolean},
		{key: store.SettingChannelApproval, def: false, check: boolean},
		{key: store.SettingNewOwnerDelay, def: int64(0), check: whole(0, 30*24), hint: "The wait before a new owner becomes one is 0 to 720 hours.", fresh: true},

		{key: store.SettingFilesMaxBytes, def: int64(model.DefaultFileSize), check: whole(1, 1<<40), hint: "The largest upload is between 1 byte and 1 TB."},
		{key: "files.store", def: "disk", check: oneOf("disk", "s3"), hint: "files.store is disk or s3."},
		{key: "files.s3", def: nil, check: orNull(s3Object), hint: "files.s3 has endpoint, region, bucket, prefix, path_style and access_key_id."},

		{key: store.SettingPushRelayEnabled, def: false, check: boolean},
		{key: "push.relay_url", def: "", check: httpsAddress, hint: "The relay is an https address."},
		{key: store.SettingUnifiedPushEnabled, def: false, check: boolean},
		{key: "push.send_badge", def: true, check: boolean},
		{key: store.SettingPreviewsEnabled, def: false, check: boolean},
		{key: store.SettingSealedEnabled, def: true, check: boolean},
		{key: store.SettingCallsEnabled, def: true, check: boolean},
		{key: store.SettingCallsMaxPeople, def: int64(50), check: whole(2, 500), hint: "A call holds 2 to 500 people."},
		{key: store.SettingCallsMaxCameras, def: int64(12), check: whole(0, 100), hint: "0 to 100 cameras can be on at once."},
		{key: "links.send_presence", def: true, check: boolean},
		{key: "links.default_max_file_bytes", def: int64(26214400), check: whole(0, 1<<40), hint: "The limit is between 0 bytes and 1 TB."},
	}
}

// currentSettings reads every key, with its default where it is not set.
// Secrets are never returned: only whether one is stored.
func (s *Service) currentSettings(ctx context.Context, a store.Areas) (map[string]any, error) {
	stored, err := a.Settings().All(ctx)
	if err != nil {
		return nil, err
	}
	out := map[string]any{}
	for _, st := range s.settings() {
		out[st.key] = st.def
		if raw, found := stored[st.key]; found {
			out[st.key] = raw
		}
	}
	out[settingS3SecretSet] = len(stored[SettingS3Secret]) > 0 && !isNull(stored[SettingS3Secret])
	return out, nil
}

// Settings returns the server settings, for admins.
func (s *Service) Settings(ctx context.Context, who Who) (map[string]any, error) {
	if err := s.adminOnly(ctx, who); err != nil {
		return nil, err
	}
	return s.currentSettings(ctx, s.store)
}

func sameJSON(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	var u, v any
	json.Unmarshal(x, &u)
	json.Unmarshal(y, &v)
	x, _ = json.Marshal(u)
	y, _ = json.Marshal(v)
	return bytes.Equal(x, y)
}

// S3Secret returns the object store's secret access key, which is kept
// sealed under the key file. found is false when none is stored.
func S3Secret(ctx context.Context, a store.Areas, keys *secret.Keys) (key string, found bool, err error) {
	v, err := store.SettingString(ctx, a, SettingS3Secret, "")
	if err != nil || v == "" {
		return "", false, err
	}
	sealed, err := base64.StdEncoding.DecodeString(v)
	if err != nil {
		return "", false, err
	}
	plain, err := keys.Unseal(sealed, "settings", "value", SettingS3Secret)
	return string(plain), err == nil, err
}

// UpdateSettings changes the settings named in patch. Each change takes
// effect at once and is written to the audit log with the old and new
// value. It returns the settings afterwards and whether anything changed.
func (s *Service) UpdateSettings(ctx context.Context, who Who, patch map[string]json.RawMessage) (map[string]any, bool, error) {
	if err := s.adminOnly(ctx, who); err != nil {
		return nil, false, err
	}
	known := map[string]setting{}
	for _, st := range s.settings() {
		known[st.key] = st
	}
	keys := make([]string, 0, len(patch))
	for k := range patch {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	values := map[string]any{}
	needFresh := false
	var newSecret *string
	for _, k := range keys {
		raw := patch[k]
		if k == SettingS3Secret {
			var v string
			if json.Unmarshal(raw, &v) != nil {
				return nil, false, model.Invalid(k, "The secret access key is text.")
			}
			newSecret = &v
			continue
		}
		st, ok := known[k]
		if !ok {
			if k == settingS3SecretSet {
				return nil, false, model.BadRequest(k + " is read-only.")
			}
			return nil, false, model.BadRequest("There is no setting called " + k + ".")
		}
		v, ok := st.check(raw)
		if !ok {
			hint := st.hint
			if hint == "" {
				hint = k + " is true or false."
			}
			return nil, false, model.Invalid(k, hint)
		}
		values[k] = v
		needFresh = needFresh || st.fresh
	}
	if needFresh {
		if err := s.fresh(who); err != nil {
			return nil, false, err
		}
	}
	before, err := s.currentSettings(ctx, s.store)
	if err != nil {
		return nil, false, err
	}
	// What the settings would be afterwards, for the rules that look at
	// more than one key.
	final := func(key string) any {
		if v, changed := values[key]; changed {
			return v
		}
		return before[key]
	}
	str := func(key string) string {
		b, _ := json.Marshal(final(key))
		var v string
		json.Unmarshal(b, &v)
		return v
	}
	if _, touched := values[store.SettingMemberSignIn]; touched && str(store.SettingMemberSignIn) == "sso" && !sameJSON(before[store.SettingMemberSignIn], "sso") {
		list, err := s.store.Identities().ListProviders(ctx)
		if err != nil {
			return nil, false, err
		}
		enabled := false
		for _, p := range list {
			enabled = enabled || p.Enabled
		}
		if !enabled {
			return nil, false, model.ErrConflict.With("Switch on a company sign-in provider before requiring it.")
		}
		// The admin making the change must have a passkey or an
		// access key, which is what gets them back in if the provider
		// breaks.
		n, err := s.store.Passkeys().Count(ctx, who.Account.ID)
		if err != nil {
			return nil, false, err
		}
		_, hasKey, err := s.store.Passkeys().AccessKey(ctx, who.Account.ID)
		if err != nil {
			return nil, false, err
		}
		if n == 0 && !hasKey {
			return nil, false, model.ErrConflict.With("Add a passkey to your own account first, so a broken provider cannot lock you out.")
		}
	}
	if str(store.SettingSecondFactor) == "required" {
		b, _ := json.Marshal(final(SettingSecondFactorAccepted))
		var accepted []string
		json.Unmarshal(b, &accepted)
		if len(accepted) == 0 {
			return nil, false, model.ErrConflict.With("Tick at least one second step before requiring one.")
		}
	}
	_, storeTouched := values["files.store"]
	_, s3Touched := values["files.s3"]
	if storeTouched || s3Touched || newSecret != nil {
		if str("files.store") == "s3" {
			if final("files.s3") == nil || sameJSON(final("files.s3"), nil) {
				return nil, false, model.ErrConflict.With("Object storage needs its details in files.s3.")
			}
			if s.testStorage != nil {
				probe := map[string]json.RawMessage{}
				probe["files.store"], _ = json.Marshal("s3")
				probe["files.s3"], _ = json.Marshal(final("files.s3"))
				key := ""
				if newSecret != nil {
					key = *newSecret
				} else if key, _, err = S3Secret(ctx, s.store, s.keys); err != nil {
					return nil, false, err
				}
				probe[SettingS3Secret], _ = json.Marshal(key)
				// Object storage is tried before the change is made,
				// and the change refused if the try fails.
				if err := s.testStorage(ctx, probe); err != nil {
					return nil, false, model.ErrUnavailable.With("Object storage could not be used: " + err.Error())
				}
			}
		}
	}
	changed := false
	err = s.store.Tx(ctx, func(tx store.Tx) error {
		for _, k := range keys {
			v, ok := values[k]
			if !ok || sameJSON(before[k], v) {
				continue
			}
			if err := tx.Settings().Set(ctx, k, v, who.Account.ID); err != nil {
				return err
			}
			if err := s.audit(ctx, tx, who.Account.ID, "setting.changed", "setting", k, map[string]any{"old": before[k], "new": v}); err != nil {
				return err
			}
			changed = true
		}
		if newSecret != nil {
			var stored any
			if *newSecret != "" {
				sealed, err := s.keys.Seal([]byte(*newSecret), "settings", "value", SettingS3Secret)
				if err != nil {
					return err
				}
				stored = base64.StdEncoding.EncodeToString(sealed)
			}
			if err := tx.Settings().Set(ctx, SettingS3Secret, stored, who.Account.ID); err != nil {
				return err
			}
			// The log says that the secret changed, never what it is.
			if err := s.audit(ctx, tx, who.Account.ID, "setting.changed", "setting", SettingS3Secret, map[string]any{"changed": true}); err != nil {
				return err
			}
			changed = true
		}
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	after, err := s.currentSettings(ctx, s.store)
	return after, changed, err
}

// MaxIconBytes is the largest community icon accepted.
const MaxIconBytes = 1 << 20

// SetServerIcon stores an image as the community's icon, replacing the one
// before it. The file lives in the library conversation, like avatars.
func (s *Service) SetServerIcon(ctx context.Context, who Who, content io.Reader) (map[string]any, error) {
	if err := s.adminOnly(ctx, who); err != nil {
		return nil, err
	}
	if s.blobs == nil {
		return nil, model.ErrUnavailable
	}
	tooLarge := model.ErrTooLarge.With("An icon is at most 1 MB.")
	// The first bytes say what the content is, whatever the upload
	// called itself.
	head := make([]byte, 512)
	n, err := io.ReadFull(content, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return nil, model.BadRequest("The upload could not be read.")
	}
	head = head[:n]
	mediaType, kind := blob.Sniff(head)
	if kind != "image" {
		return nil, model.Invalid("file", "The icon must be an image.")
	}
	key, size, sum, err := s.blobs.Put(ctx, blob.LimitReader(io.MultiReader(bytes.NewReader(head), content), MaxIconBytes))
	if err != nil {
		if errors.Is(err, model.ErrTooLarge) || strings.Contains(err.Error(), "too large") {
			return nil, tooLarge
		}
		return nil, err
	}
	lib, err := s.store.Conversations().EnsureLibrary(ctx)
	if err != nil {
		s.blobs.Delete(ctx, key)
		return nil, err
	}
	f := model.File{ConversationID: lib.ID, UploaderID: who.Account.ID, Purpose: "avatar", Name: "icon", Size: size,
		MediaType: mediaType, Kind: kind, SHA256: sum, BlobKey: key, CreatedAt: s.now()}
	var old []string
	err = s.store.Tx(ctx, func(tx store.Tx) error {
		if err := tx.Files().Create(ctx, &f); err != nil {
			return err
		}
		had, err := store.SettingString(ctx, tx, settingIcon, "")
		if err != nil {
			return err
		}
		if err := tx.Settings().Set(ctx, settingIcon, string(f.ID), who.Account.ID); err != nil {
			return err
		}
		if err := s.audit(ctx, tx, who.Account.ID, "setting.changed", "setting", settingIcon, map[string]any{"old": had, "new": f.ID}); err != nil {
			return err
		}
		if !model.ValidID(had) {
			return nil
		}
		gone, err := tx.Files().Delete(ctx, model.FileID(had))
		if err != nil && !errors.Is(err, model.ErrNotFound) {
			return err
		}
		old = store.BlobKeys(gone)
		return nil
	})
	if err != nil {
		s.blobs.Delete(ctx, key)
		return nil, err
	}
	for _, k := range old {
		if err := s.blobs.Delete(ctx, k); err != nil {
			s.log.Warn("a blob could not be removed", "err", err)
		}
	}
	return s.currentSettings(ctx, s.store)
}

// MayChangeSettings reports whether the caller is an admin with a complete
// session, for handlers that must check before reading a large body.
func (s *Service) MayChangeSettings(ctx context.Context, who Who) error { return s.adminOnly(ctx, who) }
