package model

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// Limits from docs/CONVENTIONS.md. Those an admin can change are settings;
// these are the fixed ones and the defaults.
const (
	MaxMessageText         = 16000
	MaxFilesPerMessage     = 10
	MaxCardsPerMessage     = 5
	MaxFieldsPerCard       = 10
	MaxFieldsPerForm       = 20
	MaxOptionsPerField     = 50
	MaxButtonsPerForm      = 5
	MaxChecklistItems      = 100
	MaxEmojiPerMessage     = 20
	MaxPinsPerConversation = 100
	MaxAdhocMembers        = 50
	MaxTopic               = 250
	MaxStatusText          = 100
	DefaultLimit           = 50
	MaxLimit               = 200
	DefaultFileSize        = 100 << 20
	DefaultQuickCorrection = 30 // seconds
	EventsKeptDays         = 30
)

// reserved names cannot be usernames or group handles.
var reserved = map[string]bool{
	"everyone": true, "admins": true, "channel": true, "here": true,
	"churr": true, "system": true, "me": true,
}

// ReservedName reports whether name is one nobody may take as a username or
// group handle. The two built-in groups hold `everyone` and `admins`.
func ReservedName(name string) bool { return reserved[name] }

func lowerAlnum(c byte) bool { return c >= 'a' && c <= 'z' || c >= '0' && c <= '9' }

// validName checks length, a first character from [a-z0-9] and the rest from
// [a-z0-9] plus the extra characters given.
func validName(s string, min, max int, extra string) bool {
	if len(s) < min || len(s) > max {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if lowerAlnum(c) {
			continue
		}
		if i == 0 || strings.IndexByte(extra, c) < 0 {
			return false
		}
	}
	return true
}

// ValidUsername reports whether s follows the username rule: 2 to 32
// characters, [a-z0-9] first, then [a-z0-9._-]. Group handles follow the same
// rule. It does not check the reserved names; a reserved name is refused as
// taken, not as malformed (ruling R3).
func ValidUsername(s string) bool { return validName(s, 2, 32, "._-") }

// MaxChannelName is the longest channel name, in characters.
const MaxChannelName = 64

// CleanChannelName puts a channel name in the form it is stored in: space
// at the ends removed and every run of white space made one space. ok is
// false if what is left does not follow the rule of ruling R51: 1 to 64
// characters of any script, spaces and capitals allowed, no control
// characters. Names are not unique and are matched without regard to case.
func CleanChannelName(s string) (clean string, ok bool) {
	if !utf8.ValidString(s) {
		return "", false
	}
	for _, r := range s {
		// A tab or a line break is white space to collapse; any other
		// control or format-control character is refused, not dropped.
		if unicode.IsControl(r) && !unicode.IsSpace(r) {
			return "", false
		}
	}
	// Composed, so that the same name typed on two keyboards is the same.
	clean = strings.Join(strings.Fields(norm.NFC.String(s)), " ")
	n := utf8.RuneCountInString(clean)
	return clean, n >= 1 && n <= MaxChannelName
}

// ValidChannelName reports whether s is a channel name exactly as it would
// be stored: CleanChannelName accepts it and changes nothing.
func ValidChannelName(s string) bool {
	clean, ok := CleanChannelName(s)
	return ok && clean == s
}

// ValidEmojiName reports whether s follows the custom emoji rule: 2 to 32
// characters from [a-z0-9_].
func ValidEmojiName(s string) bool {
	if len(s) < 2 || len(s) > 32 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if !lowerAlnum(s[i]) && s[i] != '_' {
			return false
		}
	}
	return true
}

// ValidCommandName reports whether s follows the command rule: 1 to 32
// characters from [a-z0-9_-].
func ValidCommandName(s string) bool { return ValidShortID(s) }

// ValidShortID reports whether s can identify a form field, option, button
// or checklist item: 1 to 32 characters from [a-z0-9_-].
func ValidShortID(s string) bool {
	if len(s) < 1 || len(s) > 32 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if !lowerAlnum(s[i]) && s[i] != '_' && s[i] != '-' {
			return false
		}
	}
	return true
}

// CleanDisplayName puts a display name in the form it is stored in: Unicode
// NFC with the spaces at its ends removed. ok is false if the result is not
// 1 to 64 characters or holds a control character.
func CleanDisplayName(s string) (clean string, ok bool) {
	s = strings.TrimSpace(norm.NFC.String(s))
	n := utf8.RuneCountInString(s)
	if n < 1 || n > 64 || !utf8.ValidString(s) {
		return "", false
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return "", false
		}
	}
	return s, true
}

// ValidDisplayName reports whether s is already a clean display name.
func ValidDisplayName(s string) bool {
	clean, ok := CleanDisplayName(s)
	return ok && clean == s
}
