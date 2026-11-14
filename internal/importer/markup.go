package importer

import (
	"strings"
	"unicode"
)

// Slack's message markup rewritten as Churr Markdown (docs/FEATURES.md
// section 25.3).
//
// Slack writes references between angle brackets (`<@U123>`, `<#C123|name>`,
// `<!here>`, `<https://x|label>`), emphasis with single marks (`*bold*`,
// `_italic_`, `~strike~`), and the three characters `&`, `<` and `>` as
// entities. Code, quotes and `:emoji:` are the same in both and are left
// alone; nothing inside code is touched at all.

// names is what the references in a text are looked up in.
type names interface {
	// username returns the Churr username of a Slack user.
	username(slackID string) (string, bool)
	// channel returns the Churr identifier of an imported Slack channel.
	channel(slackID string) (string, bool)
}

// convert rewrites one message's text.
func convert(text string, n names) string {
	var b strings.Builder
	for len(text) > 0 {
		// Code is copied as it is, entities aside.
		if strings.HasPrefix(text, "```") {
			if end := strings.Index(text[3:], "```"); end >= 0 {
				b.WriteString(unescape(text[:end+6]))
				text = text[end+6:]
				continue
			}
		}
		if text[0] == '`' {
			if end := strings.IndexAny(text[1:], "`\n"); end >= 0 && text[1+end] == '`' {
				b.WriteString(unescape(text[:end+2]))
				text = text[end+2:]
				continue
			}
		}
		if text[0] == '<' {
			if end := strings.IndexByte(text, '>'); end > 0 && !strings.ContainsAny(text[1:end], "<\n") {
				b.WriteString(reference(text[1:end], n))
				text = text[end+1:]
				continue
			}
		}
		// Plain text up to the next thing that is not.
		next := strings.IndexAny(text[1:], "`<")
		if next < 0 {
			next = len(text) - 1
		}
		b.WriteString(unescape(emphasis(text[:next+1])))
		text = text[next+1:]
	}
	return b.String()
}

// unescape turns Slack's three entities back into characters.
func unescape(s string) string {
	if !strings.Contains(s, "&") {
		return s
	}
	return strings.NewReplacer("&lt;", "<", "&gt;", ">", "&amp;", "&").Replace(s)
}

// reference rewrites what stood between angle brackets.
func reference(inner string, n names) string {
	target, label, labelled := strings.Cut(inner, "|")
	label = unescape(label)
	switch {
	case strings.HasPrefix(target, "@"):
		if name, ok := n.username(target[1:]); ok {
			return "@" + name
		}
		if labelled && label != "" {
			return "@" + strings.TrimPrefix(label, "@")
		}
		return "@unknown"
	case strings.HasPrefix(target, "#"):
		if id, ok := n.channel(target[1:]); ok {
			return "<#" + id + ">"
		}
		if labelled && label != "" {
			return "#" + label
		}
		return "#unknown-channel"
	case strings.HasPrefix(target, "!"):
		word := target[1:]
		switch {
		case word == "channel" || word == "everyone" || word == "group":
			return "@channel"
		case word == "here":
			return "@here"
		case labelled:
			// A user group or a formatted date: Slack's own fallback.
			return label
		}
		return "@" + word
	}
	target = unescape(target)
	if strings.HasPrefix(target, "mailto:") {
		if labelled && label != "" {
			return label
		}
		return strings.TrimPrefix(target, "mailto:")
	}
	if labelled && label != "" && label != target {
		return "[" + strings.NewReplacer("[", "\\[", "]", "\\]").Replace(label) + "](" + target + ")"
	}
	return target
}

// emphasis rewrites Slack's single marks in a stretch of plain text: bold
// first, so that the stars italics become are not taken for it.
func emphasis(s string) string {
	s = pairs(s, '*', "**")
	s = pairs(s, '~', "~~")
	return pairs(s, '_', "*")
}

func wordChar(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }

// pairs replaces mark…mark with with…with where Slack would have drawn it:
// the opening mark does not follow a letter or digit and is followed by
// something that is not a space, the closing mark follows something that is
// not a space and is not followed by a letter or digit, and both are on one
// line. Anything else (a lone star, snake_case_words, 2*3*4) is left as it
// is.
func pairs(s string, mark rune, with string) string {
	if !strings.ContainsRune(s, mark) {
		return s
	}
	r := []rune(s)
	var b strings.Builder
	for i := 0; i < len(r); i++ {
		if r[i] != mark || (i > 0 && (wordChar(r[i-1]) || r[i-1] == mark)) || i+1 >= len(r) || unicode.IsSpace(r[i+1]) || r[i+1] == mark {
			b.WriteRune(r[i])
			continue
		}
		end := -1
		for j := i + 2; j < len(r) && r[j] != '\n'; j++ {
			if r[j] == mark && !unicode.IsSpace(r[j-1]) && (j+1 == len(r) || !wordChar(r[j+1])) {
				end = j
				break
			}
		}
		if end < 0 {
			b.WriteRune(r[i])
			continue
		}
		b.WriteString(with)
		b.WriteString(string(r[i+1 : end]))
		b.WriteString(with)
		i = end
	}
	return b.String()
}
