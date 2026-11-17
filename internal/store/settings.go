package store

import (
	"context"
	"encoding/json"

	"github.com/tikaydaes/churr/internal/model"
)

// Setting keys used by the foundation. docs/SCHEMA.md section 2 lists them
// all with their defaults.
const (
	SettingServerName        = "server.name"
	SettingSetupTokenHash    = "setup.token_hash"
	SettingSetupDoneAt       = "setup.done_at"
	SettingRegistrationMode  = "registration.mode"
	SettingMembersMayInvite  = "invites.members_may_invite"
	SettingMemberSignIn      = "auth.member_sign_in"
	SettingSecondFactor      = "auth.second_factor"
	SettingPasskeySeconds    = "sessions.passkey_seconds"
	SettingQuickCorrection   = "messages.quick_correction_seconds"
	SettingEditWindow        = "messages.edit_window_seconds"
	SettingRetention         = "retention.seconds"
	SettingBroadcastManagers = "mentions.broadcast_managers_only"
	SettingBroadMentionLimit = "mentions.broad_mention_limit"
	// SettingNewOwnerDelay is how many hours a new Owner waits before
	// becoming one, during which any Owner may withdraw it; 0 is at once.
	SettingNewOwnerDelay       = "roles.new_owner_delay_hours"
	SettingLargeChannelLimit   = "channels.large_channel_limit"
	SettingLargeFeatureLimit   = "channels.large_feature_limit"
	SettingMembersMayAddEmoji  = "emoji.members_may_add"
	SettingBlockingEnabled     = "blocking.enabled"
	SettingFilesMaxBytes       = "files.max_bytes"
	SettingPreviewsEnabled     = "previews.enabled"
	SettingSealedEnabled       = "sealed.enabled"
	SettingCallsEnabled        = "calls.enabled"
	SettingCallsMaxPeople      = "calls.max_participants"
	SettingCallsMaxCameras     = "calls.max_cameras"
	SettingPushRelayEnabled    = "push.relay_enabled"
	SettingUnifiedPushEnabled  = "push.unifiedpush_enabled"
	SettingWebhooksManagersMay = "webhooks.managers_may_approve"
	// The approvals queue (docs/FEATURES.md section 31): new accounts
	// that register without an invite, and new channels, wait for one who
	// may approve them.
	SettingRegistrationApproval = "registration.approval"
	SettingChannelApproval      = "channels.creation_approval"
)

// Settings is the `settings` table: one JSON value per dotted key. A key
// with no row has its default, which the reader supplies.
type Settings interface {
	// Get returns the stored JSON value. found is false when the key has
	// no row.
	Get(ctx context.Context, key string) (value json.RawMessage, found bool, err error)
	// All returns every stored key.
	All(ctx context.Context) (map[string]json.RawMessage, error)
	// Set stores value (anything json.Marshal accepts) under key. by is
	// the admin who changed it, or "" for the server and the command line.
	Set(ctx context.Context, key string, value any, by model.AccountID) error
	// Delete removes a key, so it has its default again. Only the setup
	// token is ever deleted; other settings return to their default by
	// writing it.
	Delete(ctx context.Context, key string) error
}

// SettingString reads a string setting, or def if it is not set.
func SettingString(ctx context.Context, s Areas, key, def string) (string, error) {
	raw, found, err := s.Settings().Get(ctx, key)
	if err != nil || !found {
		return def, err
	}
	var v string
	if json.Unmarshal(raw, &v) != nil {
		return def, nil
	}
	return v, nil
}

// SettingBool reads a true-or-false setting, or def if it is not set.
func SettingBool(ctx context.Context, s Areas, key string, def bool) (bool, error) {
	raw, found, err := s.Settings().Get(ctx, key)
	if err != nil || !found {
		return def, err
	}
	var v bool
	if json.Unmarshal(raw, &v) != nil {
		return def, nil
	}
	return v, nil
}

// SettingInt reads a whole-number setting. set is false when the key has no
// row or holds null, which for several settings means "no limit".
func SettingInt(ctx context.Context, s Areas, key string) (value int64, set bool, err error) {
	raw, found, err := s.Settings().Get(ctx, key)
	if err != nil || !found {
		return 0, false, err
	}
	var v *int64
	if json.Unmarshal(raw, &v) != nil || v == nil {
		return 0, false, nil
	}
	return *v, true, nil
}

// SettingIntOr reads a whole-number setting, or def if it is not set.
func SettingIntOr(ctx context.Context, s Areas, key string, def int64) (int64, error) {
	v, set, err := SettingInt(ctx, s, key)
	if err != nil || !set {
		return def, err
	}
	return v, nil
}
