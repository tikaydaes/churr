package markdown

import (
	"strconv"

	"github.com/tikaydaes/churr/internal/instant"
	"github.com/tikaydaes/churr/internal/model"
)

// Resolver looks names up for Resolve. Each method reports false when the
// name does not resolve; the text then stays plain text and no entity is
// made. Names arrive exactly as typed, which is always lowercase, because
// the parser does not take anything else for a name.
type Resolver interface {
	// Account looks up `@username`, a local account.
	Account(username string) (model.AccountID, bool)
	// RemoteAccount looks up `@username@server.name`. It resolves only in
	// a conversation shared with that server.
	RemoteAccount(username, server string) (model.AccountID, bool)
	// Group looks up `@handle`, including the built-in `everyone` and
	// `admins`. Where broadcast mentions are limited to managers and the
	// author is not one, it must report false for `everyone`.
	Group(handle string) (model.GroupID, bool)
	// Broadcast reports whether the author may use `@channel` or `@here`
	// in this conversation. scope is "channel" or "here".
	Broadcast(scope string) bool
	// Channel looks up a channel link, `<#conversation-id>`. It reports
	// the channel's name as it is now, for the plain text, and true only
	// for a channel the author can see, so that a message cannot be used
	// to find out which private channels exist. A link that does not
	// resolve stays as written (ruling R52).
	Channel(id model.ConversationID) (name string, ok bool)
	// Emoji looks up a custom emoji: `:name:` with server "", or
	// `:name@server.name:`. Standard emoji are never asked about.
	Emoji(name, server string) (model.EmojiID, bool)
}

// ZoneResolver may also be implemented by a Resolver. AuthorZone is the
// author's IANA zone, sent by the posting client, in which an instant typed
// without a zone is read; "" leaves such an instant as plain text. Instants
// typed with a zone resolve without a Resolver at all.
type ZoneResolver interface {
	AuthorZone() string
}

// The two broadcast mentions and the two built-in groups.
const (
	scopeChannel  = "channel"
	scopeHere     = "here"
	groupEveryone = "everyone"
	groupAdmins   = "admins"
)

// Resolve returns the entities of a parsed text: account, group, broadcast,
// conversation, emoji, link, instant, task and the objects of objects.go, in order of `at`, never overlapping,
// with positions in UTF-16 code units.
//
// Nothing inside inline code or a code block becomes an entity. Neither does
// anything inside the text of a checklist item: the task entity covers that
// whole text, and entities may not overlap (testdata/markdown/README.md,
// decision "Nothing inside a checklist item is an entity").
//
// Task entities get item ids "t1", "t2", … in order. On an edit the chat
// service maps them back to the ids the items had before.
//
// A nil Resolver resolves no names; links and tasks are still returned.
func Resolve(doc Doc, r Resolver) []model.Entity {
	res := resolver{r: r, src: doc.src}
	res.blocks(doc.Blocks)
	return res.out
}

type resolver struct {
	r     Resolver
	out   []model.Entity
	tasks int
	// src is the text, and obj what is known about where it is written,
	// for the objects found inside runs of plain text (objects.go).
	src []uint16
	obj objectScan
}

func (res *resolver) blocks(blocks []Block) {
	for _, b := range blocks {
		switch b.Kind {
		case Paragraph, Heading:
			res.inlines(b.Inlines)
		case Quote:
			res.blocks(b.Blocks)
		case List:
			for _, it := range b.Items {
				if it.Task {
					at, n := itemText(it)
					res.tasks++
					res.out = append(res.out, model.Entity{
						Type: model.EntityTask, At: at, Len: n,
						ItemID: "t" + strconv.Itoa(res.tasks),
					})
				} else {
					res.inlines(it.Inlines)
				}
				res.blocks(it.Lists)
			}
		case Table:
			for _, c := range b.Head {
				res.inlines(c.Inlines)
			}
			for _, row := range b.Rows {
				for _, c := range row {
					res.inlines(c.Inlines)
				}
			}
		}
	}
}

func (res *resolver) inlines(spans []Inline) {
	for _, s := range spans {
		e := model.Entity{At: s.At, Len: s.Len}
		switch s.Kind {
		case Text:
			res.objects(s.At, s.At+s.Len)
		case Link, NoLink:
			// A link's text belongs to the link: formatting inside it
			// is walked, but no object is found there.
			held := res.src
			res.src = nil
			res.inlines(s.Children)
			res.src = held
		case BareLink:
			e.Type, e.URL = model.EntityLink, s.URL
			res.bareMessageLink(&e)
		case Mention:
			if res.r != nil {
				res.mention(s, &e)
			}
		case Channel:
			if res.r != nil {
				if _, ok := res.r.Channel(model.ConversationID(s.ID)); ok {
					e.Type, e.ConversationID = model.EntityConversation, model.ConversationID(s.ID)
				}
			}
		case Instant:
			// The author may keep a typed date and time as plain text
			// (an object hint of kind `plain`, objects.go).
			if res.plainOver(s.At, s.At+s.Len) {
				break
			}
			zone := ""
			if zr, ok := res.r.(ZoneResolver); ok {
				zone = zr.AuthorZone()
			}
			if in, ok := instant.Resolve(s.Local, s.Zone, zone); ok {
				e.Type, e.Time, e.Precision, e.Zone = model.EntityInstant, instant.Format(in.Time), in.Precision, in.Zone
			}
		case Emoji:
			// Only a name a custom emoji could have is looked up. `:+1:`
			// and `:a:` are standard short codes or nothing.
			if res.r != nil && model.ValidEmojiName(s.Name) {
				if id, ok := res.r.Emoji(s.Name, s.Server); ok {
					e.Type, e.EmojiID = model.EntityEmoji, id
				}
			}
		default:
			// Code has no children, so nothing inside it is reached.
			res.inlines(s.Children)
		}
		if e.Type != "" {
			res.out = append(res.out, e)
		}
	}
}

// mention fills e if the mention resolves. Usernames and group handles share
// one namespace, so the order of the two lookups does not matter; the
// reserved names are handled first because nobody can hold them.
func (res *resolver) mention(s Inline, e *model.Entity) {
	switch {
	case s.Server != "":
		if id, ok := res.r.RemoteAccount(s.Name, s.Server); ok {
			e.Type, e.AccountID = model.EntityAccount, id
		}
	case s.Name == scopeChannel || s.Name == scopeHere:
		if res.r.Broadcast(s.Name) {
			e.Type, e.Scope = model.EntityBroadcast, s.Name
		}
	case s.Name == groupEveryone || s.Name == groupAdmins:
		if id, ok := res.r.Group(s.Name); ok {
			e.Type, e.GroupID = model.EntityGroup, id
		}
	case model.ReservedName(s.Name):
		// `@me`, `@system`, `@churr`: nobody has these names.
	default:
		if id, ok := res.r.Account(s.Name); ok {
			e.Type, e.AccountID = model.EntityAccount, id
		} else if id, ok := res.r.Group(s.Name); ok {
			e.Type, e.GroupID = model.EntityGroup, id
		}
	}
}

// Task is one checklist item of a text.
type Task struct {
	At      int    // where the item's text starts, after the box, in UTF-16 code units
	Len     int    // how long that text is: the same range the task entity covers
	Text    string // that text, as typed
	Checked bool   // whether the author typed `[x]`
}

// Tasks returns the checklist items in order.
func Tasks(doc Doc) []Task {
	var out []Task
	var walk func(blocks []Block)
	walk = func(blocks []Block) {
		for _, b := range blocks {
			switch b.Kind {
			case Quote:
				walk(b.Blocks)
			case List:
				for _, it := range b.Items {
					if it.Task {
						at, n := itemText(it)
						t := Task{At: at, Len: n, Checked: it.Checked}
						// A Doc from Parse always holds its text. One put
						// together by hand does not, and has no text to give.
						if at >= 0 && n >= 0 && at+n <= len(doc.src) {
							t.Text = unitsString(doc.src[at : at+n])
						}
						out = append(out, t)
					}
					walk(it.Lists)
				}
			}
		}
	}
	walk(doc.Blocks)
	return out
}

// itemText is the range of an item's text. A parsed item always has text,
// and its spans cover that text from end to end.
func itemText(it Item) (at, n int) {
	if len(it.Inlines) == 0 {
		return it.At + it.Len, 0
	}
	first, last := it.Inlines[0], it.Inlines[len(it.Inlines)-1]
	return first.At, last.At + last.Len - first.At
}
