package model

import (
	"fmt"

	"github.com/google/uuid"
)

// Identifiers are UUIDv7 strings, lowercase with hyphens (docs/CONVENTIONS.md).
// Each kind of thing has its own type so that a message identifier cannot be
// passed where an account identifier is wanted. The empty string means "none".
type (
	// AccountID names an account.
	AccountID string
	// ConversationID names a channel or a sealed chat.
	ConversationID string
	// MessageID names a message.
	MessageID string
	// FileID names an uploaded file.
	FileID string
	// GroupID names a group.
	GroupID string
	// SectionID names a section of the channel list.
	SectionID string
	// SessionID names a session or token.
	SessionID string
	// PasskeyID names a passkey.
	PasskeyID string
	// InviteID names an invite.
	InviteID string
	// FormID names a form.
	FormID string
	// DeviceID names a device.
	DeviceID string
	// ServerID names another Churr server.
	ServerID string
	// CallID names a call.
	CallID string
	// PeerID names a link to another server or a hub.
	PeerID string
	// StreamID names a conversation shared over a peer.
	StreamID string
	// EmojiID names a custom emoji.
	EmojiID string
)

// NewID makes a fresh UUIDv7 of the wanted kind: NewID[AccountID]().
func NewID[T ~string]() T {
	id, err := uuid.NewV7()
	if err != nil {
		// NewV7 fails only if the system's random source does, and then
		// nothing else the server does is safe either.
		panic("model: no randomness for identifiers: " + err.Error())
	}
	return T(id.String())
}

// ParseID checks that s is a UUID in the canonical form and returns it as the
// wanted kind. It accepts any UUID version, because identifiers that came from
// another server or from a client (client_id) need not be version 7.
func ParseID[T ~string](s string) (T, error) {
	if !ValidID(s) {
		return "", fmt.Errorf("not an identifier: %q", s)
	}
	return T(s), nil
}

// ValidID reports whether s is a UUID written lowercase with hyphens.
func ValidID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return false
			}
		default:
			if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
				return false
			}
		}
	}
	return true
}
