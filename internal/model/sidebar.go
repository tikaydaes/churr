package model

import (
	"strconv"
	"strings"
)

// A person's own sidebar (ruling R83, docs/FEATURES.md section 21.4): how
// they group and order the conversations of this server for themselves.
// Nobody arranges anyone else's. Without a stored layout everyone sees the
// server's default: the admin's sections in the admin's order, each sorted
// by name, then Chats and Sealed sorted by last activity.
//
// Headings are named by keys:
//
//	p:<n>     one of the person's own sections, n its number
//	s:<id>    one of the admin's sections
//	s:        channels in no section ("Channels")
//	chats     ad-hoc channels
//	sealed    sealed chats

// Sidebar is the stored layout (schema Sidebar).
type Sidebar struct {
	// Version goes up by one with every change; a change names the
	// version it was made against.
	Version int64 `json:"version"`
	// Sections are the person's own, oldest first.
	Sections []SidebarSection `json:"sections"`
	// Headings is the person's order of the channel headings (own
	// sections, the admin's, and "s:"). Empty means the default order;
	// a heading missing from it goes after those in it, in the default
	// order. Chats and Sealed are always last.
	Headings []string `json:"headings"`
	// Groups holds each heading's sort and custom order where it is not
	// the default.
	Groups []SidebarGroup `json:"groups"`
	// Placements puts conversations in sections of the person's choosing:
	// their own, or the admin's, or Channels. A conversation not here sits
	// where the server puts it.
	Placements []SidebarPlacement `json:"placements"`
	// Stars is the order of starred conversations: each sits at the top
	// of its heading, in this order. Whether a conversation is starred
	// is its membership's `starred`.
	Stars []ConversationID `json:"stars"`
}

// SidebarSection is one of a person's own sections.
type SidebarSection struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// SidebarGroup is how one heading sorts.
type SidebarGroup struct {
	Key  string `json:"key"`
	Sort string `json:"sort"`
	// Order is the custom order, used while Sort is custom. Conversations
	// not in it follow, by name.
	Order []ConversationID `json:"order"`
}

// SidebarPlacement puts a conversation under a section heading: Key is
// p:<n>, s:<id> or s: (never chats or sealed).
type SidebarPlacement struct {
	ConversationID ConversationID `json:"conversation_id"`
	Key            string         `json:"key"`
}

// IsSectionKey says whether a heading key names a section a conversation
// may be put in: one of the person's own, an admin's, or Channels.
func IsSectionKey(key string) bool {
	return strings.HasPrefix(key, "p:") || strings.HasPrefix(key, "s:")
}

// The sorts a heading can have.
const (
	SortAlphabetical = "alphabetical"
	SortActivity     = "activity"
	SortCustom       = "custom"
)

// Fixed heading keys.
const (
	HeadingChannels = "s:"
	HeadingChats    = "chats"
	HeadingSealed   = "sealed"
)

// Limits of a sidebar.
const (
	MaxSidebarSections = 50
	MaxSidebarOrder    = 2000
	MaxSidebarOps      = 50
)

// SectionKey is the heading key of a person's own section.
func SectionKey(id int) string { return "p:" + strconv.Itoa(id) }

// OwnSection returns the number of a person's own section from its key.
func OwnSection(key string) (int, bool) {
	rest, ok := strings.CutPrefix(key, "p:")
	if !ok {
		return 0, false
	}
	n, err := strconv.Atoi(rest)
	return n, err == nil && n > 0
}

// DefaultSort is the sort a heading has until the person changes it.
func DefaultSort(key string) string {
	if key == HeadingChats || key == HeadingSealed {
		return SortActivity
	}
	return SortAlphabetical
}

// EventSidebarChanged goes to the person's own connections when their
// sidebar changes.
const EventSidebarChanged = "sidebar.changed"
