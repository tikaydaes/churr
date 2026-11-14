package model

import "encoding/json"

// Role is an account's role on the server.
type Role string

// The roles. RoleRemote is the local shadow of an account on another server
// and carries no powers here.
const (
	RoleOwner  Role = "owner"
	RoleAdmin  Role = "admin"
	RoleMember Role = "member"
	RoleGuest  Role = "guest"
	RoleBot    Role = "bot"
	RoleRemote Role = "remote"
)

// Valid reports whether r is one of the six roles.
func (r Role) Valid() bool {
	switch r {
	case RoleOwner, RoleAdmin, RoleMember, RoleGuest, RoleBot, RoleRemote:
		return true
	}
	return false
}

// IsAdmin reports whether r is owner or admin. "Admin" in the documents
// includes the owner unless they say otherwise.
func (r Role) IsAdmin() bool { return r == RoleOwner || r == RoleAdmin }

// IsFullMember reports whether r is owner, admin or member: the roles that
// are in @everyone, browse public channels and see every local account.
func (r Role) IsFullMember() bool { return r == RoleOwner || r == RoleAdmin || r == RoleMember }

// Presence is what others see of whether a person is around.
type Presence string

// The three presences.
const (
	PresenceOnline  Presence = "online"
	PresenceAway    Presence = "away"
	PresenceOffline Presence = "offline"
)

// Status is the status a person or bot sets.
type Status struct {
	Emoji              string `json:"emoji,omitempty"`
	Text               string `json:"text,omitempty"`
	Until              Time   `json:"until,omitempty"`
	PauseNotifications bool   `json:"pause_notifications"`
	AppearAway         bool   `json:"appear_away"`
}

// Availability is "in a meeting" or "out of office".
type Availability struct {
	Value string `json:"value"` // meeting | out_of_office
	Until Time   `json:"until,omitempty"`
	SetBy string `json:"set_by,omitempty"` // self | integration
}

// Account is a person, a bot, or the local shadow of someone on another
// server. The fields with JSON names are the shape other accounts see
// (schema Account). The rest are the stored columns behind them, which a
// service reads to fill the public ones and never sends as they are.
type Account struct {
	ID           AccountID `json:"id"`
	Username     string    `json:"username"`
	ServerName   string    `json:"server_name,omitempty"`
	Role         Role      `json:"role"`
	DisplayName  string    `json:"display_name"`
	AvatarFileID FileID    `json:"avatar_file_id,omitempty"`
	AvatarURL    string    `json:"avatar_url,omitempty"`
	Title        string    `json:"title,omitempty"`
	Pronouns     string    `json:"pronouns,omitempty"`
	TimeZone     string    `json:"time_zone,omitempty"`

	// Worked out when the account is shown; not stored.
	Presence     Presence      `json:"presence"`
	InCall       bool          `json:"in_call,omitempty"`
	Status       *Status       `json:"status,omitempty"`
	Availability *Availability `json:"availability,omitempty"`
	DND          bool          `json:"dnd,omitempty"`
	GroupIDs     []GroupID     `json:"group_ids,omitempty"`
	// Claimed is sent only as false, for an imported account nobody has claimed.
	Claimed *bool `json:"claimed,omitempty"`

	CreatedAt Time `json:"created_at"`
	RemovedAt Time `json:"removed_at,omitempty"`

	// Stored only.
	ServerID        ServerID  `json:"-"` // empty for a local account
	WebAuthnHandle  []byte    `json:"-"`
	ClaimedAt       Time      `json:"-"`
	RemovedBy       AccountID `json:"-"`
	ProfileVersion  int64     `json:"-"`
	StatusEmoji     string    `json:"-"`
	StatusText      string    `json:"-"`
	StatusUntil     Time      `json:"-"`
	StatusDND       bool      `json:"-"`
	StatusAway      bool      `json:"-"`
	AvailabilityIs  string    `json:"-"` // meeting | out_of_office | ""
	AvailabilityEnd Time      `json:"-"`
	AvailabilityBy  string    `json:"-"` // self | integration:<id>
	// AwayUntil and DNDUntil: nil is off, a pointer to 0 is "until turned
	// off", anything else is the moment it ends (docs/SCHEMA.md).
	AwayUntil         *Time           `json:"-"`
	DNDUntil          *Time           `json:"-"`
	QuietStart        string          `json:"-"` // HH:MM
	QuietEnd          string          `json:"-"`
	QuietDays         string          `json:"-"` // seven characters, Monday first
	QuietSkipUntil    Time            `json:"-"`
	AllowNotifyAnyway bool            `json:"-"`
	PrefsJSON         json.RawMessage `json:"-"` // the prefs column as stored
}

// Local reports whether the account lives on this server.
func (a Account) Local() bool { return a.ServerID == "" }

// Active reports whether the account can act: local, claimed and not removed
// (docs/PERMISSIONS.md section 2.2).
func (a Account) Active() bool { return a.Local() && !a.ClaimedAt.IsZero() && a.RemovedAt.IsZero() }

// Removed reports whether the account has been removed.
func (a Account) Removed() bool { return !a.RemovedAt.IsZero() }

// Prefs are the personal preferences the API returns (schema Prefs).
type Prefs struct {
	ThreadView        string `json:"thread_view"` // threaded | flat
	MeetingPauses     bool   `json:"meeting_pauses"`
	AllowNotifyAnyway bool   `json:"allow_notify_anyway"`
}

// Dnd is the caller's own do not disturb in detail (schema Dnd).
type Dnd struct {
	On          bool     `json:"on"`
	Sources     []string `json:"sources"`
	ManualUntil Time     `json:"manual_until,omitempty"`
	Until       Time     `json:"until,omitempty"`
}

// QuietHours is the daily schedule that puts the account in do not disturb.
type QuietHours struct {
	Start     string `json:"start"`
	End       string `json:"end"`
	Days      string `json:"days"`
	TimeZone  string `json:"time_zone"`
	SkipUntil Time   `json:"skip_until,omitempty"`
}

// AppearAway is the manual appear-away switch on Me.
type AppearAway struct {
	On    bool `json:"on"`
	Until Time `json:"until,omitempty"`
}

// Me is the caller's own account: the public shape plus what only they see.
type Me struct {
	Account
	Prefs         Prefs       `json:"prefs"`
	DndState      Dnd         `json:"dnd_state"`
	AppearAway    *AppearAway `json:"appear_away,omitempty"`
	QuietHours    *QuietHours `json:"quiet_hours,omitempty"`
	PasskeyCount  int64       `json:"passkey_count"`
	HasTotp       bool        `json:"has_totp"`
	IdentityCount int64       `json:"identity_count"`
	BlockedIDs    []AccountID `json:"blocked_ids,omitempty"`
}

// Group is a named set of local accounts with a handle (schema Group).
type Group struct {
	ID            GroupID `json:"id"`
	Handle        string  `json:"handle"`
	Name          string  `json:"name"`
	Description   string  `json:"description"`
	Color         string  `json:"color,omitempty"`
	Open          bool    `json:"open"`
	Builtin       string  `json:"builtin,omitempty"` // everyone | admins
	MemberCount   int64   `json:"member_count"`
	IsMember      bool    `json:"is_member"`
	ProviderID    string  `json:"provider_id,omitempty"`
	ProviderGroup string  `json:"provider_group,omitempty"`
	CreatedAt     Time    `json:"created_at"`
}

// The handles of the two built-in groups.
const (
	GroupEveryone = "everyone"
	GroupAdmins   = "admins"
)

// Section is a heading the admins define to group channels in the list.
type Section struct {
	ID       SectionID `json:"id"`
	Name     string    `json:"name"`
	Position int       `json:"position"`
}

// Session is one signed-in client, token or bot credential (schema Session).
type Session struct {
	ID                  SessionID `json:"id"`
	Kind                string    `json:"kind"` // web | app | personal | bot
	Label               string    `json:"label"`
	SignedInBy          string    `json:"signed_in_by,omitempty"`
	SecondFactor        string    `json:"second_factor,omitempty"`
	PendingSecondFactor bool      `json:"pending_second_factor,omitempty"`
	Recovery            bool      `json:"recovery,omitempty"`
	Current             bool      `json:"current,omitempty"`
	CreatedAt           Time      `json:"created_at"`
	LastSeenAt          Time      `json:"last_seen_at,omitempty"`
	ExpiresAt           Time      `json:"expires_at,omitempty"`

	// Stored only.
	AccountID       AccountID `json:"-"`
	AccessExpiresAt Time      `json:"-"`
	MaxExpiresAt    Time      `json:"-"`
	FreshAt         Time      `json:"-"`
	RotatedAt       Time      `json:"-"`
	RevokedAt       Time      `json:"-"`
}

// The kinds of session.
const (
	SessionWeb      = "web"
	SessionApp      = "app"
	SessionPersonal = "personal"
	SessionBot      = "bot"
)

// Passkey is a passkey on an account (schema Passkey), with the WebAuthn
// record the server keeps for it.
type Passkey struct {
	ID           PasskeyID `json:"id"`
	Name         string    `json:"name"`
	HasPRF       bool      `json:"has_prf"`
	UnlockPublic []byte    `json:"unlock_public,omitempty"`
	CanUnlock    bool      `json:"can_unlock"`
	CreatedAt    Time      `json:"created_at"`
	LastUsedAt   Time      `json:"last_used_at,omitempty"`

	// Stored only.
	AccountID    AccountID       `json:"-"`
	CredentialID []byte          `json:"-"`
	Credential   json.RawMessage `json:"-"` // the WebAuthn library's record: public key, counter, flags
}

// Invite is an invite link (schema Invite).
type Invite struct {
	ID              InviteID         `json:"id"`
	Code            string           `json:"code"`
	URL             string           `json:"url"`
	Role            Role             `json:"role"`
	GroupIDs        []GroupID        `json:"group_ids"`
	ConversationIDs []ConversationID `json:"conversation_ids"`
	ClaimsAccountID AccountID        `json:"claims_account_id,omitempty"`
	MaxUses         int              `json:"max_uses"`
	Uses            int              `json:"uses"`
	ExpiresAt       Time             `json:"expires_at,omitempty"`
	CreatedByID     AccountID        `json:"created_by_id,omitempty"`
	CreatedAt       Time             `json:"created_at"`
	RevokedAt       Time             `json:"revoked_at,omitempty"`
}

// Usable reports whether the invite can still be used at the given moment.
func (i Invite) Usable(now Time) bool {
	return i.RevokedAt.IsZero() &&
		(i.ExpiresAt.IsZero() || i.ExpiresAt.After(now)) &&
		(i.MaxUses == 0 || i.Uses < i.MaxUses)
}

// AuditEntry is one line of the audit log (schema AuditEntry).
type AuditEntry struct {
	ID         string         `json:"id"`
	At         Time           `json:"at"`
	ActorID    string         `json:"actor_id,omitempty"`
	ActorKind  string         `json:"actor_kind"` // account | integration | server | cli
	Action     string         `json:"action"`
	TargetKind string         `json:"target_kind,omitempty"`
	TargetID   string         `json:"target_id,omitempty"`
	Detail     map[string]any `json:"detail"`
}

// Device is an app install or browser profile signed in to one account
// (schema Device).
type Device struct {
	ID          DeviceID  `json:"id"`
	SessionID   SessionID `json:"session_id,omitempty"`
	Name        string    `json:"name"`
	Platform    string    `json:"platform"`
	PushKind    string    `json:"push_kind,omitempty"`
	PushAddress string    `json:"push_address,omitempty"`
	WakeRoute   string    `json:"wake_route,omitempty"`
	SignKey     []byte    `json:"sign_key,omitempty"`
	BoxKey      []byte    `json:"box_key,omitempty"`
	RetiredAt   Time      `json:"retired_at,omitempty"`
	Current     bool      `json:"current,omitempty"`
	CreatedAt   Time      `json:"created_at"`
	LastSeenAt  Time      `json:"last_seen_at,omitempty"`

	AccountID AccountID `json:"-"`
	// PushFailedAt marks the first failure in the current run of failed
	// wake-ups; PushPublicKey and PushAuth are what Web Push encryption
	// needs, for a UnifiedPush distributor that wants it (docs/PUSH.md).
	PushFailedAt  Time   `json:"-"`
	PushPublicKey []byte `json:"-"`
	PushAuth      []byte `json:"-"`
}
