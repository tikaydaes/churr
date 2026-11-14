package bots

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/tikaydaes/churr/internal/model"
)

// The Slack converter (docs/BOTS.md section 12): a Slack incoming-webhook
// payload in, a Payload out. It is a pure function of its input.

// flexString reads a JSON string, number or boolean as text. Tools that
// send Slack payloads are loose about types in field values and timestamps.
type flexString string

func (f *flexString) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) > 0 && b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		*f = flexString(s)
		return nil
	}
	if string(b) == "null" || (len(b) > 0 && (b[0] == '{' || b[0] == '[')) {
		*f = ""
		return nil
	}
	*f = flexString(b)
	return nil
}

// flexBool reads true, "true" or 1 as true.
type flexBool bool

func (f *flexBool) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(bytes.TrimSpace(b)), `"`)
	*f = s == "true" || s == "1"
	return nil
}

type slackPayload struct {
	Text        flexString        `json:"text"`
	Blocks      []json.RawMessage `json:"blocks"`
	Attachments []slackAttachment `json:"attachments"`
	Username    flexString        `json:"username"`
	IconURL     flexString        `json:"icon_url"`
	Mrkdwn      *flexBool         `json:"mrkdwn"`
}

type slackAttachment struct {
	Color      flexString `json:"color"`
	Pretext    flexString `json:"pretext"`
	AuthorName flexString `json:"author_name"`
	AuthorIcon flexString `json:"author_icon"`
	Title      flexString `json:"title"`
	TitleLink  flexString `json:"title_link"`
	Text       flexString `json:"text"`
	Fields     []struct {
		Title flexString `json:"title"`
		Value flexString `json:"value"`
		Short flexBool   `json:"short"`
	} `json:"fields"`
	ImageURL flexString        `json:"image_url"`
	ThumbURL flexString        `json:"thumb_url"`
	Footer   flexString        `json:"footer"`
	Fallback flexString        `json:"fallback"`
	TS       flexString        `json:"ts"`
	Blocks   []json.RawMessage `json:"blocks"`
	Actions  []struct {
		Text flexString `json:"text"`
		URL  flexString `json:"url"`
	} `json:"actions"`
}

// errNothingToPost is the refusal for a payload that converts to nothing.
var errNothingToPost = model.BadRequest("Nothing in that payload can be posted.")

// ConvertSlack turns a Slack incoming-webhook payload into a Payload.
// Unknown fields are ignored. A payload that is not JSON, or that leaves
// nothing to post, is refused with invalid_request.
func ConvertSlack(raw []byte) (Payload, error) {
	var in slackPayload
	if err := json.Unmarshal(raw, &in); err != nil {
		return Payload{}, model.BadRequest("The body is not a Slack payload.")
	}
	mrkdwn := in.Mrkdwn == nil || bool(*in.Mrkdwn)
	var (
		paras []string
		cards []Card
	)
	if len(in.Blocks) > 0 {
		// With blocks, Slack uses `text` only for notifications.
		paras = slackBlocks(in.Blocks, func(address, title, alt string) string {
			if len(cards) >= model.MaxCardsPerMessage {
				return linkTo(escapeInline(alt), address)
			}
			c := Card{Color: "grey", ImageURL: address, Title: truncate(title, maxCardTitle)}
			if title == "" {
				c.Text = escapePlain(alt)
			}
			cards = append(cards, c)
			return ""
		})
	} else if in.Text != "" {
		paras = append(paras, slackText(string(in.Text), mrkdwn))
	}
	for _, a := range in.Attachments {
		if a.Pretext != "" {
			paras = append(paras, slackText(string(a.Pretext), true))
		}
		if len(cards) < model.MaxCardsPerMessage {
			if c, ok := slackCard(a); ok {
				cards = append(cards, c)
			}
			continue
		}
		paras = append(paras, slackOverflow(a))
	}
	out := Payload{Text: truncate(joinParas(paras...), model.MaxMessageText), Cards: cards}
	if out.Text == "" && len(out.Cards) == 0 {
		return Payload{}, errNothingToPost
	}
	if name, icon := viaName(string(in.Username)), webAddress(string(in.IconURL)); name != "" || icon != "" {
		out.Via = &Via{Name: name, IconURL: icon}
	}
	return out, nil
}

// slackCard turns one attachment into a card. ok is false for an attachment
// with nothing in it.
func slackCard(a slackAttachment) (Card, bool) {
	c := Card{
		Color:  "grey",
		Title:  truncate(slackPlain(string(a.Title)), maxCardTitle),
		URL:    webAddress(string(a.TitleLink)),
		Footer: slackPlain(string(a.Footer)),
	}
	if a.Color != "" {
		c.Color = colorFromName(string(a.Color))
	}
	if name := slackPlain(string(a.AuthorName)); name != "" {
		c.Author = &CardAuthor{Name: name, IconURL: webAddress(string(a.AuthorIcon))}
	}
	for _, f := range a.Fields {
		label, value := slackPlain(string(f.Title)), fieldValue(slackText(string(f.Value), true))
		if label == "" && value == "" {
			continue
		}
		c.Fields = append(c.Fields, model.CardField{Label: label, Value: value, Short: bool(f.Short)})
	}
	// A card has one image: the thumbnail is used only without one.
	if c.ImageURL = webAddress(string(a.ImageURL)); c.ImageURL == "" {
		c.ImageURL = webAddress(string(a.ThumbURL))
	}
	if ts, _, _ := strings.Cut(strings.TrimSpace(string(a.TS)), "."); ts != "" {
		if n, err := strconv.ParseInt(ts, 10, 64); err == nil && n > 0 && n < 1<<40 {
			c.At = model.Time(n * 1000)
		}
	}
	text := cardText(slackText(string(a.Text), true))
	var extra []string
	if len(a.Blocks) > 0 {
		paras := slackBlocks(a.Blocks, func(address, title, alt string) string {
			if c.ImageURL == "" {
				c.ImageURL = address
				return ""
			}
			if alt == "" {
				alt = title
			}
			return linkTo(escapeInline(alt), address)
		})
		extra = append(extra, cardText(joinParas(paras...)))
	}
	var links []string
	for _, act := range a.Actions {
		if u := webAddress(string(act.URL)); u != "" && act.Text != "" {
			links = append(links, linkTo(escapeInline(string(act.Text)), u))
		}
	}
	extra = append(extra, strings.Join(links, " · "))
	c.Text = joinParas(append([]string{text}, extra...)...)
	return finishCard(c)
}

// slackOverflow writes an attachment after the fifth as a paragraph of the
// message text: its fallback, or its title and text.
func slackOverflow(a slackAttachment) string {
	if a.Fallback != "" {
		return slackText(string(a.Fallback), true)
	}
	title := escapeInline(slackPlain(string(a.Title)))
	if u := webAddress(string(a.TitleLink)); u != "" && title != "" {
		title = linkTo(title, u)
	}
	return joinParas(title, slackText(string(a.Text), true))
}

// ---------------------------------------------------------------- mrkdwn

// Parts of a text that are already converted (code, links, mentions) are
// set aside and stood in for by a marker, so the formatting pass cannot
// change them. A marker is heldOpen, the part's number, heldClose. Both are
// private-use characters, removed from the input first.
const (
	heldOpen  = ''
	heldClose = ''
)

type holder struct{ parts []string }

func (h *holder) hold(s string) string {
	h.parts = append(h.parts, s)
	return string(heldOpen) + strconv.Itoa(len(h.parts)-1) + string(heldClose)
}

var heldMarker = regexp.MustCompile(string(heldOpen) + `(\d+)` + string(heldClose))

func (h *holder) restore(s string) string {
	return heldMarker.ReplaceAllStringFunc(s, func(m string) string {
		n, _ := strconv.Atoi(strings.Trim(m, string(heldOpen)+string(heldClose)))
		if n < len(h.parts) {
			return h.parts[n]
		}
		return ""
	})
}

var trailingSpace = regexp.MustCompile(`[ \t]+\n`)

var entities = strings.NewReplacer("&amp;", "&", "&lt;", "<", "&gt;", ">")

// slackText converts Slack's mrkdwn to Churr Markdown (the table in
// docs/BOTS.md section 12). With mrkdwn false the text is plain: everything
// is escaped except the angle-bracket forms.
func slackText(s string, mrkdwn bool) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.NewReplacer(string(heldOpen), "", string(heldClose), "").Replace(s)
	h := &holder{}
	var w []byte
	for i := 0; i < len(s); {
		if mrkdwn && strings.HasPrefix(s[i:], "```") {
			if end := strings.Index(s[i+3:], "```"); end >= 0 {
				// The fences go onto lines of their own, with no
				// language after the first.
				code := strings.Trim(entities.Replace(s[i+3:i+3+end]), "\n")
				w = bytes.TrimRight(w, " ")
				if len(w) > 0 && w[len(w)-1] != '\n' {
					w = append(w, '\n')
				}
				w = append(w, h.hold("```\n"+code+"\n```")...)
				for i += end + 6; i < len(s) && s[i] == ' '; i++ {
				}
				if i < len(s) && s[i] != '\n' {
					w = append(w, '\n')
				}
				continue
			}
		}
		if mrkdwn && s[i] == '`' {
			if end := strings.IndexAny(s[i+1:], "`\n"); end > 0 && s[i+1+end] == '`' {
				w = append(w, h.hold(codeSpan(entities.Replace(s[i+1:i+1+end])))...)
				i += end + 2
				continue
			}
		}
		if s[i] == '<' {
			if end := strings.IndexAny(s[i+1:], "<>\n"); end > 0 && s[i+1+end] == '>' {
				w = append(w, h.hold(slackAngle(s[i+1:i+1+end]))...)
				i += end + 2
				continue
			}
		}
		w = append(w, s[i])
		i++
	}
	lines := strings.Split(string(w), "\n")
	for i, line := range lines {
		lines[i] = slackLine(line, mrkdwn)
	}
	// Something dropped at the end of a line leaves no space behind.
	return strings.TrimSpace(trailingSpace.ReplaceAllString(h.restore(strings.Join(lines, "\n")), "\n"))
}

// slackLine converts one line outside code.
func slackLine(line string, mrkdwn bool) string {
	quote := ""
	if mrkdwn && strings.HasPrefix(line, ">") {
		quote = "> "
		line = strings.TrimLeft(strings.TrimLeft(line, ">"), " ")
	}
	out := slackInline([]rune(line), mrkdwn)
	// What Slack shows plainly at the start of a line but Churr would
	// read as a block.
	switch {
	case strings.HasPrefix(out, "#"), strings.HasPrefix(out, ">"), ruleLine.MatchString(out):
		out = `\` + out
	case numberedStart.MatchString(out):
		out = numberedStart.ReplaceAllString(out, `$1\. `)
	}
	return quote + out
}

func wordRune(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }

// slackClose finds the mark that closes the one at i, by Slack's rule: the
// opening mark starts a word, the closing one ends a word, and neither has
// a space on its inside.
func slackClose(r []rune, i int) int {
	mark := r[i]
	if (i > 0 && wordRune(r[i-1])) || i+1 >= len(r) || unicode.IsSpace(r[i+1]) || r[i+1] == mark {
		return -1
	}
	for j := i + 2; j < len(r); j++ {
		if r[j] == mark && !unicode.IsSpace(r[j-1]) && (j+1 == len(r) || !wordRune(r[j+1])) {
			return j
		}
	}
	return -1
}

// slackInline converts the inline marks of one stretch of text.
func slackInline(r []rune, mrkdwn bool) string {
	var b strings.Builder
	for i := 0; i < len(r); i++ {
		ch := r[i]
		switch {
		case ch == heldOpen:
			for ; i < len(r) && r[i] != heldClose; i++ {
				b.WriteRune(r[i])
			}
			if i < len(r) {
				b.WriteRune(r[i])
			}
		case ch == '&':
			rest := string(r[i:min(i+5, len(r))])
			switch {
			case strings.HasPrefix(rest, "&amp;"):
				b.WriteByte('&')
				i += 4
			case strings.HasPrefix(rest, "&lt;"):
				// `<#…>` is how Churr text links a channel; text that
				// only looks like one stays text.
				if i+4 < len(r) && r[i+4] == '#' {
					b.WriteByte('\\')
				}
				b.WriteByte('<')
				i += 3
			case strings.HasPrefix(rest, "&gt;"):
				b.WriteByte('>')
				i += 3
			default:
				b.WriteByte('&')
			}
		case mrkdwn && (ch == '*' || ch == '~'):
			if j := slackClose(r, i); j > 0 {
				mark := "**"
				if ch == '~' {
					mark = "~~"
				}
				b.WriteString(mark + slackInline(r[i+1:j], true) + mark)
				i = j
				continue
			}
			// A mark that closes nothing is shown plainly by Slack. A
			// lone ~ means nothing here either; * and ~~ would.
			if ch == '*' || (i+1 < len(r) && r[i+1] == '~') || (i > 0 && r[i-1] == '~') {
				b.WriteByte('\\')
			}
			b.WriteRune(ch)
		case !mrkdwn && strings.ContainsRune("*_~`", ch):
			b.WriteByte('\\')
			b.WriteRune(ch)
		case ch == '[' || ch == ']' || ch == '|':
			b.WriteByte('\\')
			b.WriteRune(ch)
		case ch == '\\' && i+1 < len(r) && r[i+1] < 128 && unicode.IsPunct(r[i+1]):
			// Slack has no escapes, so a backslash is a backslash.
			b.WriteString(`\\`)
		default:
			b.WriteRune(ch)
		}
	}
	return b.String()
}

// slackAngle converts the inside of one `<…>` form.
func slackAngle(inner string) string {
	target, label, _ := strings.Cut(inner, "|")
	label = entities.Replace(label)
	switch {
	case strings.HasPrefix(target, "#"):
		// A channel of Slack's is no channel here, and a Churr channel is
		// linked only by its identifier (ruling R52): the name is plain
		// text, and without a name there is nothing to write.
		if label != "" {
			return "#" + escapeInline(strings.TrimLeft(label, "#"))
		}
		return ""
	case strings.HasPrefix(target, "@"):
		// A Slack identifier names nobody here; a label might.
		if label != "" {
			return "@" + strings.TrimLeft(label, "@")
		}
		return codeSpan(target)
	case strings.HasPrefix(target, "!"):
		word := target[1:]
		switch {
		case word == "channel" || word == "everyone":
			return "@channel"
		case word == "here":
			return "@here"
		case strings.HasPrefix(word, "subteam^"):
			if label != "" {
				return "@" + strings.TrimLeft(label, "@")
			}
			return codeSpan("@" + strings.TrimPrefix(word, "subteam^"))
		}
		if label != "" {
			return escapeInline(label)
		}
		name, _, _ := strings.Cut(word, "^")
		return escapeInline(name)
	}
	target = entities.Replace(target)
	if label != "" {
		return linkTo(escapeInline(label), target)
	}
	if linkScheme(target) {
		return target
	}
	return escapeInline(target)
}

var (
	slackAngleForm  = regexp.MustCompile(`<([^<>|\n]+)(?:\|([^<>\n]*))?>`)
	slackUnderscore = regexp.MustCompile(`(^|[^\pL\pN_])_(\S(?:[^_]*\S)?)_([^\pL\pN_]|$)`)
)

// slackPlain reduces mrkdwn to plain text, for a card's title, footer and
// labels: links become their text and the marks go.
func slackPlain(s string) string {
	s = slackAngleForm.ReplaceAllStringFunc(s, func(m string) string {
		p := slackAngleForm.FindStringSubmatch(m)
		if p[2] != "" {
			return p[2]
		}
		return strings.TrimLeft(p[1], "!")
	})
	s = entities.Replace(s)
	s = strings.NewReplacer("*", "", "~", "", "`", "").Replace(s)
	s = slackUnderscore.ReplaceAllString(s, "$1$2$3")
	return strings.Join(strings.Fields(s), " ")
}

// ---------------------------------------------------------------- blocks

type slackTextObj struct {
	Type string     `json:"type"`
	Text flexString `json:"text"`
}

// md is the object's text as Churr Markdown: converted if it is mrkdwn,
// escaped if it is plain text.
func (t *slackTextObj) md() string {
	if t == nil {
		return ""
	}
	if t.Type == "mrkdwn" {
		return slackText(string(t.Text), true)
	}
	return escapePlain(string(t.Text))
}

// plain is the object's text with nothing escaped, for a place that holds
// plain text.
func (t *slackTextObj) plain() string {
	if t == nil {
		return ""
	}
	return strings.TrimSpace(string(t.Text))
}

type slackElement struct {
	Type     string        `json:"type"`
	Text     *slackTextObj `json:"text"`
	URL      flexString    `json:"url"`
	ImageURL flexString    `json:"image_url"`
	AltText  flexString    `json:"alt_text"`
}

type slackBlock struct {
	Type      string            `json:"type"`
	Text      json.RawMessage   `json:"text"`
	Fields    []slackTextObj    `json:"fields"`
	Accessory *slackElement     `json:"accessory"`
	Elements  []json.RawMessage `json:"elements"`
	ImageURL  flexString        `json:"image_url"`
	AltText   flexString        `json:"alt_text"`
	Title     *slackTextObj     `json:"title"`
	VideoURL  flexString        `json:"video_url"`
}

// textObj reads a block's `text`, which is a text object in most blocks and
// a bare string in a markdown block.
func (b slackBlock) textObj() *slackTextObj {
	if len(b.Text) == 0 {
		return nil
	}
	var t slackTextObj
	if json.Unmarshal(b.Text, &t) != nil || t.Text == "" {
		return nil
	}
	return &t
}

// buttonLinks writes the buttons that have an address as links on one line.
// A button without one would have to call Slack, so it is dropped, and so
// is every other kind of element.
func buttonLinks(elements []json.RawMessage) string {
	var links []string
	for _, raw := range elements {
		var e slackElement
		if json.Unmarshal(raw, &e) != nil || e.Type != "button" {
			continue
		}
		if u := webAddress(string(e.URL)); u != "" && e.Text.plain() != "" {
			links = append(links, linkTo(escapeInline(e.Text.plain()), u))
		}
	}
	return strings.Join(links, " · ")
}

// slackBlocks converts layout blocks, in order, to paragraphs of text.
// image is called for each image block with its address, title and
// alternative text; it returns a paragraph to put in the text instead, or
// "" when it has kept the image.
func slackBlocks(blocks []json.RawMessage, image func(address, title, alt string) string) []string {
	var paras []string
	add := func(p string) {
		if p = strings.TrimSpace(p); p != "" {
			paras = append(paras, p)
		}
	}
	for _, raw := range blocks {
		var b slackBlock
		if json.Unmarshal(raw, &b) != nil {
			continue
		}
		switch b.Type {
		case "header":
			if t := b.textObj(); t != nil {
				add("## " + escapeInline(strings.Join(strings.Fields(string(t.Text)), " ")))
			}
		case "section":
			add(b.textObj().md())
			for i := range b.Fields {
				add(b.Fields[i].md())
			}
			if a := b.Accessory; a != nil {
				switch a.Type {
				case "image":
					if u := webAddress(string(a.ImageURL)); u != "" {
						add(image(u, "", strings.TrimSpace(string(a.AltText))))
					}
				case "button":
					if u := webAddress(string(a.URL)); u != "" && a.Text.plain() != "" {
						add(linkTo(escapeInline(a.Text.plain()), u))
					}
				}
			}
		case "divider":
			add("---")
		case "context":
			var parts []string
			for _, el := range b.Elements {
				var t slackTextObj
				if json.Unmarshal(el, &t) == nil && (t.Type == "mrkdwn" || t.Type == "plain_text") {
					if s := t.md(); s != "" {
						parts = append(parts, s)
					}
				}
			}
			add(strings.Join(parts, " · "))
		case "image":
			// One that names a file at Slack has nothing to fetch.
			if u := webAddress(string(b.ImageURL)); u != "" {
				add(image(u, b.Title.plain(), strings.TrimSpace(string(b.AltText))))
			}
		case "actions":
			add(buttonLinks(b.Elements))
		case "rich_text":
			var els []richElement
			for _, raw := range b.Elements {
				var e richElement
				if json.Unmarshal(raw, &e) == nil {
					els = append(els, e)
				}
			}
			for _, p := range richBlocks(els) {
				add(p)
			}
		case "markdown":
			var s string
			if json.Unmarshal(b.Text, &s) == nil {
				add(s)
			}
		case "video":
			if u := webAddress(string(b.VideoURL)); u != "" {
				title := b.Title.plain()
				if title == "" {
					title = "Video"
				}
				add(linkTo(escapeInline(title), u))
			}
		case "input", "file", "call":
		default:
			for _, p := range flattenTexts(raw) {
				add(p)
			}
		}
	}
	return paras
}

// flattenTexts returns the text of every text object inside a block the
// converter does not know, in the order they appear in the document.
func flattenTexts(raw json.RawMessage) []string {
	dec := json.NewDecoder(bytes.NewReader(raw))
	var out []string
	var walk func() bool
	walk = func() bool {
		tok, err := dec.Token()
		if err != nil {
			return false
		}
		switch tok {
		case json.Delim('['):
			for dec.More() {
				if !walk() {
					return false
				}
			}
			_, err := dec.Token()
			return err == nil
		case json.Delim('{'):
			// Where this object's own text goes is fixed when the
			// object opens, so texts nested inside it come after.
			at := len(out)
			out = append(out, "")
			var typ, text string
			var hasText bool
			for dec.More() {
				key, err := dec.Token()
				if err != nil {
					return false
				}
				switch key {
				case "type", "text":
					var v json.RawMessage
					if dec.Decode(&v) != nil {
						return false
					}
					var s string
					if json.Unmarshal(v, &s) == nil {
						if key == "type" {
							typ = s
						} else {
							text, hasText = s, true
						}
						continue
					}
					out = append(out, flattenTexts(v)...)
				default:
					if !walk() {
						return false
					}
				}
			}
			if hasText && (typ == "mrkdwn" || typ == "plain_text") {
				out[at] = (&slackTextObj{Type: typ, Text: flexString(text)}).md()
			}
			_, err := dec.Token()
			return err == nil
		}
		return true
	}
	walk()
	kept := out[:0]
	for _, s := range out {
		if strings.TrimSpace(s) != "" {
			kept = append(kept, s)
		}
	}
	return kept
}

// ------------------------------------------------------------- rich text

type richElement struct {
	Type        string          `json:"type"`
	Elements    []richElement   `json:"elements"`
	Style       json.RawMessage `json:"style"`
	Indent      int             `json:"indent"`
	Text        string          `json:"text"`
	URL         string          `json:"url"`
	Name        string          `json:"name"`
	UserID      string          `json:"user_id"`
	ChannelID   string          `json:"channel_id"`
	UsergroupID string          `json:"usergroup_id"`
	Range       string          `json:"range"`
}

// marks wraps text in the marks its style asks for, keeping any space at
// its ends outside them, where Markdown needs it.
func (e richElement) marks(text string, escape bool) string {
	var st struct {
		Bold   bool `json:"bold"`
		Italic bool `json:"italic"`
		Strike bool `json:"strike"`
		Code   bool `json:"code"`
	}
	if len(e.Style) > 0 {
		json.Unmarshal(e.Style, &st)
	}
	core := strings.TrimSpace(text)
	if core == "" {
		return text
	}
	lead := text[:strings.Index(text, core)]
	trail := text[len(lead)+len(core):]
	switch {
	case st.Code:
		core = codeSpan(core)
	case escape:
		core = escapeInline(core)
	}
	if st.Strike {
		core = "~~" + core + "~~"
	}
	if st.Italic {
		core = "_" + core + "_"
	}
	if st.Bold {
		core = "**" + core + "**"
	}
	return lead + core + trail
}

// richInline flattens the inline elements of a rich text section.
func richInline(els []richElement) string {
	var b strings.Builder
	for _, e := range els {
		switch e.Type {
		case "text":
			b.WriteString(e.marks(e.Text, true))
		case "link":
			label := e.Text
			if label == "" {
				b.WriteString(linkTo("", e.URL))
				continue
			}
			b.WriteString(e.marks(linkTo(escapeInline(label), e.URL), false))
		case "emoji":
			b.WriteString(":" + e.Name + ":")
		case "user":
			b.WriteString(codeSpan("@" + e.UserID))
		case "channel":
			// Slack's channel, known only by its identifier there: it
			// names nothing here (ruling R52).
		case "usergroup":
			b.WriteString(codeSpan("@" + e.UsergroupID))
		case "broadcast":
			if e.Range == "here" {
				b.WriteString("@here")
			} else {
				b.WriteString("@channel")
			}
		}
	}
	return b.String()
}

// richRaw is the text of elements with no formatting, for a code block.
func richRaw(els []richElement) string {
	var b strings.Builder
	for _, e := range els {
		switch {
		case e.Text != "":
			b.WriteString(e.Text)
		case e.URL != "":
			b.WriteString(e.URL)
		}
	}
	return b.String()
}

// richBlocks flattens the elements of a rich_text block to paragraphs.
func richBlocks(els []richElement) []string {
	var paras []string
	inList, topStyle := false, ""
	counters := map[int]int{}
	for _, e := range els {
		switch e.Type {
		case "rich_text_list":
			var style string
			json.Unmarshal(e.Style, &style)
			indent := min(max(e.Indent, 0), 2)
			// A top-level list of the other kind is a new list.
			if indent == 0 && style != topStyle {
				inList = false
			}
			if !inList {
				counters = map[int]int{}
			}
			if indent == 0 {
				topStyle = style
			}
			for k := range counters {
				if k > indent {
					delete(counters, k)
				}
			}
			var lines []string
			for _, item := range e.Elements {
				marker := "- "
				if style == "ordered" {
					counters[indent]++
					marker = strconv.Itoa(counters[indent]) + ". "
				}
				text := lineBreaks.ReplaceAllString(strings.TrimSpace(richInline(item.Elements)), " ")
				lines = append(lines, strings.Repeat("  ", indent)+marker+text)
			}
			if len(lines) == 0 {
				continue
			}
			// Slack sends each change of depth as a list of its own;
			// together they are one nested list.
			if inList {
				paras[len(paras)-1] += "\n" + strings.Join(lines, "\n")
			} else {
				paras = append(paras, strings.Join(lines, "\n"))
			}
			inList = true
			continue
		case "rich_text_preformatted":
			paras = append(paras, "```\n"+strings.Trim(richRaw(e.Elements), "\n")+"\n```")
		case "rich_text_quote":
			lines := strings.Split(strings.TrimSpace(richInline(e.Elements)), "\n")
			for i := range lines {
				lines[i] = "> " + lines[i]
			}
			paras = append(paras, strings.Join(lines, "\n"))
		case "rich_text_section":
			lines := strings.Split(strings.TrimSpace(richInline(e.Elements)), "\n")
			for i := range lines {
				lines[i] = escapeLineStart(lines[i])
			}
			paras = append(paras, strings.Join(lines, "\n"))
		}
		inList = false
	}
	return paras
}
