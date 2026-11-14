package model

// The objects of sealed chats that the API returns (docs/SEALED.md). The
// server stores each as bytes it cannot read; the few fields beside the
// bytes are what it needs to file them.

// The events of sealed chats (docs/EVENTS.md section 12).
const (
	EventSealedEpochStarted    = "sealed.epoch_started"
	EventDeviceListChanged     = "device_list.changed"
	EventUnlockRequestCreated  = "unlock_request.created"
	EventUnlockRequestAnswered = "unlock_request.answered"
	EventBackupChanged         = "backup.changed"
)

// The kinds of locked copy: the way of unlocking each is sealed to.
const (
	CopyPasskey = "passkey"
	CopyPhrase  = "phrase"
	CopyDevice  = "device"
)

// The push kinds of docs/PUSH.md section 3.
const (
	PushAPNs        = "apns"
	PushFCM         = "fcm"
	PushUnifiedPush = "unifiedpush"
	PushNone        = "none"
)

// DeviceListVersion is one version of an account's signed device list
// (schema DeviceListVersion). List, Sig and PrevSig are the three parts of
// what is stored in device_lists.signed_list.
type DeviceListVersion struct {
	AccountID AccountID `json:"account_id"`
	Version   int64     `json:"version"`
	List      Bytes     `json:"list"`
	Sig       Bytes     `json:"sig"`
	PrevSig   Bytes     `json:"prev_sig,omitempty"`
	CreatedAt Time      `json:"created_at"`
}

// EpochRecord is one key epoch of a sealed chat (schema EpochRecord).
// AccountID and DeviceID are the maker's, read from the record.
type EpochRecord struct {
	ConversationID ConversationID `json:"conversation_id"`
	KeyEpoch       int64          `json:"key_epoch"`
	Seq            int64          `json:"seq"`
	AccountID      AccountID      `json:"account_id"`
	DeviceID       DeviceID       `json:"device_id"`
	Record         Bytes          `json:"record"`
	CreatedAt      Time           `json:"created_at"`
	// Ord is, in a chat shared with another server, the order string of
	// the stamp of the event that carried the record. Two records made
	// for one key epoch are settled by it: the lower stands.
	Ord string `json:"-"`
}

// LockedCopy is the backup key sealed to one way of unlocking (schema
// LockedCopy).
type LockedCopy struct {
	ID         string    `json:"id"`
	Kind       string    `json:"kind"`
	PasskeyID  PasskeyID `json:"passkey_id,omitempty"`
	DeviceID   DeviceID  `json:"device_id,omitempty"`
	KeyVersion int64     `json:"key_version"`
	LockedKey  Bytes     `json:"locked_key"`
	CreatedAt  Time      `json:"created_at"`

	AccountID AccountID `json:"-"`
}

// AccountRecord is the account key and the list of ways to unlock,
// encrypted under the backup key (schema AccountRecord).
type AccountRecord struct {
	KeyVersion int64 `json:"key_version"`
	Revision   int64 `json:"revision"`
	Record     Bytes `json:"record"`
	UpdatedAt  Time  `json:"updated_at,omitempty"`

	AccountID      AccountID `json:"-"`
	ReplacementDue bool      `json:"-"`
}

// Backup is what a complete session of an account is given to unlock sealed
// chats (schema Backup).
type Backup struct {
	KeyVersion     int64         `json:"key_version"`
	AccountRecord  AccountRecord `json:"account_record"`
	LockedCopies   []LockedCopy  `json:"locked_copies"`
	ReplacementDue bool          `json:"replacement_due"`
}

// BackupRecord is a history secret for one sealed chat and one key epoch,
// encrypted under the backup key (schema BackupRecord).
type BackupRecord struct {
	ConversationID ConversationID `json:"conversation_id"`
	KeyEpoch       int64          `json:"key_epoch"`
	KeyVersion     int64          `json:"key_version"`
	Record         Bytes          `json:"record"`
	CreatedAt      Time           `json:"created_at,omitempty"`

	AccountID AccountID `json:"-"`
}

// UnlockRequest is a new, locked device asking an unlocked device of the
// same account for the backup key (schema UnlockRequest).
type UnlockRequest struct {
	ID         string   `json:"id"`
	DeviceID   DeviceID `json:"device_id"`
	Device     *Device  `json:"device,omitempty"`
	Request    Bytes    `json:"request"`
	Answer     Bytes    `json:"answer,omitempty"`
	CreatedAt  Time     `json:"created_at"`
	ExpiresAt  Time     `json:"expires_at"`
	AnsweredAt Time     `json:"answered_at,omitempty"`

	AccountID AccountID `json:"-"`
}
