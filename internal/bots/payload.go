package bots

import (
	"math"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/tikaydaes/churr/internal/model"
)

// Payload is what a webhook asks to post: the body of the native address
// (docs/BOTS.md section 10, schema WebhookMessage), and what the Slack,
// Discord and GitHub converters turn their input into. It differs from a
// posted message in one way: an image is still an address or a part of the
// request, not yet a file.
type Payload struct {
	Text     string           `json:"text,omitempty"`
	Cards    []Card           `json:"cards,omitempty"`
	Form     *model.FormInput `json:"form,omitempty"`
	Files    []FileRef        `json:"files,omitempty"`
	ParentID string           `json:"parent_id,omitempty"`
	ClientID string           `json:"client_id,omitempty"`
	Via      *Via             `json:"via,omitempty"`
}

// Card is a card as a webhook sends it (schema WebhookCard): a card whose
// image and author icon are given by address or by naming a part.
type Card struct {
	Color     string            `json:"color,omitempty"`
	Author    *CardAuthor       `json:"author,omitempty"`
	Title     string            `json:"title,omitempty"`
	URL       string            `json:"url,omitempty"`
	Text      string            `json:"text,omitempty"`
	Fields    []model.CardField `json:"fields,omitempty"`
	ImageURL  string            `json:"image_url,omitempty"`
	ImagePart string            `json:"image_part,omitempty"`
	Footer    string            `json:"footer,omitempty"`
	At        model.Time        `json:"at,omitempty"`
}

// CardAuthor is the small line above a card's title.
type CardAuthor struct {
	Name     string `json:"name"`
	IconURL  string `json:"icon_url,omitempty"`
	IconPart string `json:"icon_part,omitempty"`
}

// FileRef names a part of a multipart request to attach as a file.
type FileRef struct {
	Part    string `json:"part"`
	Name    string `json:"name,omitempty"`
	Caption string `json:"caption,omitempty"`
}

// Via overrides how a webhook's message is labelled.
type Via struct {
	Name     string `json:"name,omitempty"`
	IconURL  string `json:"icon_url,omitempty"`
	IconPart string `json:"icon_part,omitempty"`
}

// empty reports whether a card has none of the three things one of which it
// must have.
func (c Card) empty() bool {
	return c.Title == "" && c.Text == "" && c.ImageURL == "" && c.ImagePart == ""
}

// Limits on converted text (docs/BOTS.md sections 12 to 14).
const (
	maxCardTitle = 200
	maxCardText  = 4000
	maxViaName   = 64
	maxExcerpt   = 500
)

// truncate cuts s to at most max characters. A cut text ends with "…",
// which is counted in the max.
func truncate(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	r := []rune(s)
	return strings.TrimRight(string(r[:max-1]), " \n") + "…"
}

// truncateAtWord is truncate that cuts at the last space before the limit
// when there is one.
func truncateAtWord(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	cut := string([]rune(s)[:max-1])
	if i := strings.LastIndexAny(cut, " \n"); i > 0 {
		cut = cut[:i]
	}
	return strings.TrimRight(cut, " \n") + "…"
}

// colorFromRGB maps a colour to the nearest of Churr's seven by hue
// (docs/BOTS.md section 12.1).
func colorFromRGB(r, g, b uint8) string {
	rf, gf, bf := float64(r)/255, float64(g)/255, float64(b)/255
	max, min := math.Max(rf, math.Max(gf, bf)), math.Min(rf, math.Min(gf, bf))
	l := (max + min) / 2
	var h, s float64
	if d := max - min; d > 0 {
		if l > 0.5 {
			s = d / (2 - max - min)
		} else {
			s = d / (max + min)
		}
		switch max {
		case rf:
			h = math.Mod((gf-bf)/d, 6)
		case gf:
			h = (bf-rf)/d + 2
		default:
			h = (rf-gf)/d + 4
		}
		h *= 60
		if h < 0 {
			h += 360
		}
	}
	switch {
	case s < 0.15 || l < 0.10 || l > 0.95:
		return "grey"
	case h >= 345 || h < 20:
		return "red"
	case h < 70:
		return "yellow"
	case h < 160:
		return "green"
	case h < 215:
		return "cyan"
	case h < 290:
		return "lavender"
	}
	return "pink"
}

// colorFromInt maps a colour held as 0xRRGGBB, the way Discord sends one.
func colorFromInt(n int64) string {
	if n < 0 || n > 0xFFFFFF {
		return "grey"
	}
	return colorFromRGB(uint8(n>>16), uint8(n>>8), uint8(n))
}

// colorFromName maps what Slack accepts as an attachment colour: one of its
// three words, a hex value, or (so that a payload written for Churr reads
// the same here) one of Churr's own names. Anything else is grey.
func colorFromName(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "good":
		return "green"
	case "warning":
		return "yellow"
	case "danger":
		return "red"
	}
	for _, c := range model.CardColors {
		if s == c {
			return c
		}
	}
	hex := strings.TrimPrefix(s, "#")
	if len(hex) == 3 {
		hex = string([]byte{hex[0], hex[0], hex[1], hex[1], hex[2], hex[2]})
	}
	if len(hex) != 6 {
		return "grey"
	}
	n, err := strconv.ParseUint(hex, 16, 32)
	if err != nil {
		return "grey"
	}
	return colorFromInt(int64(n))
}

// linkScheme reports whether an address is one a Churr link may point to:
// http, https, mailto or tel.
func linkScheme(u string) bool {
	l := strings.ToLower(u)
	for _, p := range []string{"http://", "https://", "mailto:", "tel:"} {
		if strings.HasPrefix(l, p) && len(l) > len(p) {
			return true
		}
	}
	return false
}

// webAddress returns u if it is an http or https address with a host, and
// "" otherwise. Card links and image addresses must be one.
func webAddress(u string) string {
	u = strings.TrimSpace(u)
	p, err := url.Parse(u)
	if err != nil || (p.Scheme != "http" && p.Scheme != "https") || p.Host == "" {
		return ""
	}
	return u
}

// linkTo writes a Markdown link. An address that cannot be linked to is
// written as its text.
func linkTo(text, address string) string {
	if !linkScheme(address) {
		return text
	}
	if text == "" {
		return address
	}
	// The address ends at the first closing bracket or space, so those
	// are written in their encoded form.
	address = strings.NewReplacer("(", "%28", ")", "%29", " ", "%20").Replace(address)
	return "[" + text + "](" + address + ")"
}

// escapePlain backslash-escapes every character that means something in
// Churr Markdown, so text another service shows plainly stays plain here.
func escapePlain(s string) string {
	var b strings.Builder
	for _, line := range strings.SplitAfter(s, "\n") {
		b.WriteString(escapeLineStart(escapeInline(line)))
	}
	return b.String()
}

// escapeInline escapes the inline marks of Churr Markdown in one stretch of
// text.
func escapeInline(s string) string {
	if !strings.ContainsAny(s, "\\*_~`[]|") {
		return s
	}
	var b strings.Builder
	for _, r := range s {
		if strings.ContainsRune("\\*_~`[]|", r) {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

var (
	numberedStart = regexp.MustCompile(`^(\s*\d+)\. `)
	ruleLine      = regexp.MustCompile(`^\s*-{3,}\s*$`)
)

// escapeLineStart escapes what would make a line a heading, a quote, a list
// item or a rule.
func escapeLineStart(line string) string {
	trimmed := strings.TrimLeft(line, " ")
	indent := line[:len(line)-len(trimmed)]
	switch {
	case strings.HasPrefix(trimmed, "#"), strings.HasPrefix(trimmed, ">"), strings.HasPrefix(trimmed, "- "), ruleLine.MatchString(line):
		return indent + `\` + trimmed
	case numberedStart.MatchString(line):
		return numberedStart.ReplaceAllString(line, `$1\. `)
	}
	return line
}

var (
	headingLine = regexp.MustCompile(`^\s{0,3}#{1,6}\s+(.*?)\s*#*\s*$`)
	quoteMarks  = regexp.MustCompile(`^\s{0,3}(?:>\s?)+`)
	fenceLine   = regexp.MustCompile("^\\s{0,3}```")
)

// cardText applies the card text rule (docs/BOTS.md section 12.2): a card's
// text holds inline constructs and lists only, so a heading becomes a bold
// line, a quote loses its marks and a code block becomes one inline-code
// span per line. A table and a rule are left as typed. The result is at most
// 4,000 characters.
func cardText(md string) string {
	var out []string
	inFence := false
	for _, line := range strings.Split(strings.ReplaceAll(md, "\r\n", "\n"), "\n") {
		if fenceLine.MatchString(line) {
			inFence = !inFence
			continue
		}
		if inFence {
			if strings.TrimSpace(line) == "" {
				continue
			}
			out = append(out, codeSpan(line))
			continue
		}
		line = quoteMarks.ReplaceAllString(line, "")
		if m := headingLine.FindStringSubmatch(line); m != nil {
			line = ""
			if m[1] != "" {
				line = "**" + m[1] + "**"
			}
		}
		out = append(out, line)
	}
	return truncate(strings.TrimSpace(strings.Join(out, "\n")), maxCardText)
}

// codeSpan writes text as inline code, with a longer fence when the text
// itself holds a backtick.
func codeSpan(s string) string {
	if strings.Contains(s, "`") {
		return "`` " + s + " ``"
	}
	return "`" + s + "`"
}

var lineBreaks = regexp.MustCompile(`\s*\n\s*`)

// fieldValue reduces converted text to what a card field holds: the card
// text rule, then everything on one line.
func fieldValue(md string) string {
	return lineBreaks.ReplaceAllString(cardText(md), " ")
}

// fieldLines writes card fields as lines of text, for fields beyond the
// tenth and for a card that would otherwise have nothing but fields.
func fieldLines(fields []model.CardField) string {
	lines := make([]string, 0, len(fields))
	for _, f := range fields {
		lines = append(lines, "**"+f.Label+":** "+f.Value)
	}
	return strings.Join(lines, "\n")
}

// joinParas joins the parts of a text that are not empty with blank lines.
func joinParas(parts ...string) string {
	kept := parts[:0:0]
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			kept = append(kept, p)
		}
	}
	return strings.Join(kept, "\n\n")
}

// finishCard applies what the Slack and Discord tables share once a card's
// parts are known: fields beyond the tenth move into the text, a card with
// fields and nothing else gets them as text, and the text is cut to size.
// ok is false for a card with nothing in it, which is dropped.
func finishCard(c Card) (Card, bool) {
	if len(c.Fields) > model.MaxFieldsPerCard {
		c.Text = joinParas(c.Text, fieldLines(c.Fields[model.MaxFieldsPerCard:]))
		c.Fields = c.Fields[:model.MaxFieldsPerCard]
	}
	if c.empty() && len(c.Fields) > 0 {
		c.Text, c.Fields = fieldLines(c.Fields), nil
	}
	c.Text = truncate(c.Text, maxCardText)
	if c.empty() {
		return c, false
	}
	return c, true
}

// viaName cleans a name a payload gives for the via label.
func viaName(s string) string {
	clean, ok := model.CleanDisplayName(truncate(strings.TrimSpace(s), maxViaName))
	if !ok {
		return ""
	}
	return clean
}
