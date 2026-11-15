package markdown

import (
	"strings"

	"github.com/tikaydaes/churr/internal/model"
)

// Plain turns a text into plain text for search, fallbacks and notification
// bodies.
//
// Formatting marks are removed and their content kept. A link is shown as
// its text, a bare link and an image as the address. Heading marks, quote
// marks, list markers and tick-boxes are dropped. Inline code and code
// blocks are kept as their content. A rule gives nothing.
//
// Each paragraph, heading, list item and table row is one line, with runs
// of white space made one space; a line break inside a paragraph becomes a
// space. The cells of a table row are joined with " | ". A code block keeps
// its own lines exactly, because the line structure is the content.
//
// Mentions and emoji codes are written as typed (`@sam`, `:shipit:`), which
// is already the readable form: the text holds names, not identifiers. entities is what the server stored for the text.
// Each account, group, broadcast, emoji and link entity marks
// a range that is copied as typed and never read as formatting, so the
// result agrees with the stored entities even if they were worked out by
// another device or an earlier version of the rules. With nil entities the
// parser's own reading is used; for entities made by Resolve from the same
// text the two give the same result.
//
// A channel link, `<#conversation-id>`, is not readable as typed. Plain
// writes "unknown channel" in its place; PlainNames writes the channel's
// name.
func Plain(text string, entities []model.Entity) string {
	return PlainNames(text, entities, nil)
}

// UnknownChannel is what the plain text says in place of a channel link
// that names no channel the author could see.
const UnknownChannel = "unknown channel"

// PlainNames is Plain with the names of channels: each channel link is
// written as the channel's name at this moment, as name gives it (ruling
// R52). With entities given, only a link that has a conversation entity is
// named, since the entity is the record that the author could see that
// channel; any other link, and one name does not know, is written
// "unknown channel". name may be nil.
func PlainNames(text string, entities []model.Entity, name func(model.ConversationID) (string, bool)) string {
	src := toUnits(text)
	doc := parseUnits(src, guards(src, entities))
	var lines []string
	plainBlocks(doc.Blocks, &lines)
	out := strings.Join(lines, "\n")
	if !strings.Contains(out, channelMark) {
		return out
	}
	linked := map[string]bool{}
	for _, e := range entities {
		if e.Type == model.EntityConversation {
			linked[string(e.ConversationID)] = true
		}
	}
	// plainInlines left each link as a mark around its identifier.
	var sb strings.Builder
	for {
		i := strings.Index(out, channelMark)
		if i < 0 || len(out) < i+len(channelMark)+36 {
			break
		}
		id := out[i+len(channelMark) : i+len(channelMark)+36]
		sb.WriteString(out[:i])
		written := UnknownChannel
		if name != nil && (entities == nil || linked[id]) {
			if n, ok := name(model.ConversationID(id)); ok && n != "" {
				written = n
			}
		}
		sb.WriteString(written)
		out = out[i+len(channelMark)+36:]
	}
	sb.WriteString(out)
	return sb.String()
}

// channelMark stands before a channel link's identifier while the plain
// text is put together. It cannot come from a message: the parser never
// copies these control characters next to each other into a Channel span,
// and a text that held them would only ever get "unknown channel" for
// whatever followed.
const channelMark = "\x00\x1a#\x00"

// guards turns entities into the ranges the inline parser must leave alone.
// Entities are untrusted here (in a sealed chat they come from a device), so
// anything out of range, out of order or cutting a character in half is
// ignored rather than believed.
func guards(src []uint16, entities []model.Entity) []int {
	var g []int
	end := 0
	for _, e := range entities {
		switch e.Type {
		case model.EntityAccount, model.EntityGroup, model.EntityBroadcast, model.EntityEmoji, model.EntityLink:
		// A conversation entity is not listed: its range is a channel
		// link, which the parser reads the same way with or without it,
		// and which must be written as a name, not copied as typed.
		default:
			continue
		}
		// The first two tests also keep At+Len from overflowing.
		if e.At < end || e.Len <= 0 || e.Len > len(src) || e.At > len(src)-e.Len {
			continue
		}
		stop := e.At + e.Len
		if isLow(src[e.At]) || stop < len(src) && isLow(src[stop]) {
			continue
		}
		if g == nil {
			g = make([]int, len(src)+1)
		}
		g[e.At] = stop
		end = stop
	}
	return g
}

func plainBlocks(blocks []Block, lines *[]string) {
	add := func(s string) {
		if s = squash(s); s != "" {
			*lines = append(*lines, s)
		}
	}
	for _, b := range blocks {
		switch b.Kind {
		case Paragraph, Heading:
			add(plainInlines(b.Inlines))
		case Quote:
			plainBlocks(b.Blocks, lines)
		case List:
			for _, it := range b.Items {
				add(plainInlines(it.Inlines))
				plainBlocks(it.Lists, lines)
			}
		case CodeBlock:
			if b.Code != "" {
				*lines = append(*lines, b.Code)
			}
		case Table:
			row := func(cells []Cell) {
				parts := make([]string, len(cells))
				for i, c := range cells {
					parts[i] = squash(plainInlines(c.Inlines))
				}
				add(strings.Join(parts, " | "))
			}
			row(b.Head)
			for _, r := range b.Rows {
				row(r)
			}
		}
	}
}

func plainInlines(spans []Inline) string {
	var sb strings.Builder
	var walk func(spans []Inline)
	walk = func(spans []Inline) {
		for _, s := range spans {
			switch s.Kind {
			case Text, Code, Escape, Instant:
				sb.WriteString(s.Text)
			case Image, BareLink:
				sb.WriteString(s.URL)
			case Mention:
				sb.WriteString("@" + s.Name)
				if s.Server != "" {
					sb.WriteString("@" + s.Server)
				}
			case Channel:
				sb.WriteString(channelMark + s.ID)
			case Emoji:
				sb.WriteString(":" + s.Name)
				if s.Server != "" {
					sb.WriteString("@" + s.Server)
				}
				sb.WriteString(":")
			case Break:
				sb.WriteString(" ")
			default:
				walk(s.Children)
			}
		}
	}
	walk(spans)
	return sb.String()
}

// squash makes every run of white space one space and removes it from the
// ends.
func squash(s string) string {
	var sb strings.Builder
	space := false
	for _, r := range s {
		if isWhite(r) {
			space = true
			continue
		}
		if space && sb.Len() > 0 {
			sb.WriteByte(' ')
		}
		space = false
		sb.WriteRune(r)
	}
	return sb.String()
}
