package bots

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/tikaydaes/churr/internal/model"
)

// The Discord converter (docs/BOTS.md section 13): a Discord webhook payload
// in, a Payload out. It is a pure function of its input.

// DiscordPart describes one `files[n]` part of a multipart request: the
// part's name in the form and the file name it carries. The converter needs
// them to match `attachment://name` addresses and the `attachments` list;
// it never sees the bytes.
type DiscordPart struct {
	Part     string
	Filename string
}

type discordImage struct {
	URL string `json:"url"`
}

type discordEmbed struct {
	Title       flexString `json:"title"`
	Description flexString `json:"description"`
	URL         string     `json:"url"`
	Color       flexString `json:"color"`
	Author      *struct {
		Name    flexString `json:"name"`
		IconURL string     `json:"icon_url"`
	} `json:"author"`
	Fields []struct {
		Name   flexString `json:"name"`
		Value  flexString `json:"value"`
		Inline flexBool   `json:"inline"`
	} `json:"fields"`
	Image     *discordImage `json:"image"`
	Thumbnail *discordImage `json:"thumbnail"`
	Footer    *struct {
		Text flexString `json:"text"`
	} `json:"footer"`
	Timestamp string `json:"timestamp"`
}

type discordPayload struct {
	Content     flexString     `json:"content"`
	Username    flexString     `json:"username"`
	AvatarURL   string         `json:"avatar_url"`
	Embeds      []discordEmbed `json:"embeds"`
	Attachments []struct {
		ID          flexString `json:"id"`
		Description flexString `json:"description"`
		Filename    flexString `json:"filename"`
	} `json:"attachments"`
	AllowedMentions *struct {
		Parse []string `json:"parse"`
	} `json:"allowed_mentions"`
}

// ConvertDiscord turns a Discord webhook payload into a Payload. parts are
// the file parts that came with it, in order. Unknown fields are ignored. A
// payload that is not JSON, or that leaves nothing to post, is refused with
// invalid_request.
func ConvertDiscord(raw []byte, parts []DiscordPart) (Payload, error) {
	var in discordPayload
	if err := json.Unmarshal(raw, &in); err != nil {
		return Payload{}, model.BadRequest("The body is not a Discord payload.")
	}
	// Without "everyone" in allowed_mentions.parse, Discord notifies
	// nobody for @everyone and @here, so neither may notify here.
	everyone := true
	if in.AllowedMentions != nil {
		everyone = false
		for _, p := range in.AllowedMentions.Parse {
			if p == "everyone" {
				everyone = true
			}
		}
	}
	used := map[string]bool{}
	// image resolves an image address: a part of this request named by
	// attachment://, or an address to fetch.
	image := func(address string) (url, part string) {
		if name, ok := strings.CutPrefix(address, "attachment://"); ok {
			for _, p := range parts {
				if p.Filename == name {
					used[p.Part] = true
					return "", p.Part
				}
			}
			return "", ""
		}
		return webAddress(address), ""
	}
	paras := []string{discordText(string(in.Content), everyone)}
	var cards []Card
	for _, e := range in.Embeds {
		if len(cards) >= model.MaxCardsPerMessage {
			title := escapeInline(strings.TrimSpace(string(e.Title)))
			if u := webAddress(e.URL); u != "" && title != "" {
				title = linkTo(title, u)
			}
			paras = append(paras, joinParas(title, discordText(string(e.Description), everyone)))
			continue
		}
		c := Card{
			Color: "grey",
			Title: truncate(strings.Join(strings.Fields(string(e.Title)), " "), maxCardTitle),
			URL:   webAddress(e.URL),
			Text:  cardText(discordText(string(e.Description), everyone)),
		}
		if n, err := strconv.ParseInt(strings.TrimSpace(string(e.Color)), 10, 64); err == nil && e.Color != "" {
			c.Color = colorFromInt(n)
		}
		if e.Author != nil && strings.TrimSpace(string(e.Author.Name)) != "" {
			c.Author = &CardAuthor{Name: strings.TrimSpace(string(e.Author.Name))}
			c.Author.IconURL, c.Author.IconPart = image(e.Author.IconURL)
		}
		for _, f := range e.Fields {
			label, value := strings.TrimSpace(string(f.Name)), fieldValue(discordText(string(f.Value), everyone))
			if label == "" && value == "" {
				continue
			}
			c.Fields = append(c.Fields, model.CardField{Label: label, Value: value, Short: bool(f.Inline)})
		}
		if e.Image != nil {
			c.ImageURL, c.ImagePart = image(e.Image.URL)
		}
		if c.ImageURL == "" && c.ImagePart == "" && e.Thumbnail != nil {
			c.ImageURL, c.ImagePart = image(e.Thumbnail.URL)
		}
		if e.Footer != nil {
			c.Footer = strings.TrimSpace(string(e.Footer.Text))
		}
		if t, err := time.Parse(time.RFC3339, e.Timestamp); err == nil {
			c.At = model.Time(t.UnixMilli())
		}
		if c, ok := finishCard(c); ok {
			cards = append(cards, c)
		}
	}
	out := Payload{Text: truncate(joinParas(paras...), model.MaxMessageText), Cards: cards}
	// A part no embed used is a file on the message. `attachments` may
	// give it a name and a caption, by its index.
	for i, p := range parts {
		if used[p.Part] || len(out.Files) >= model.MaxFilesPerMessage {
			continue
		}
		ref := FileRef{Part: p.Part}
		for _, a := range in.Attachments {
			if string(a.ID) == strconv.Itoa(i) {
				ref.Name, ref.Caption = strings.TrimSpace(string(a.Filename)), strings.TrimSpace(string(a.Description))
			}
		}
		out.Files = append(out.Files, ref)
	}
	if out.Text == "" && len(out.Cards) == 0 && len(out.Files) == 0 {
		return Payload{}, errNothingToPost
	}
	name := viaName(string(in.Username))
	icon, iconPart := image(in.AvatarURL)
	if name != "" || icon != "" || iconPart != "" {
		out.Via = &Via{Name: name, IconURL: icon, IconPart: iconPart}
	}
	return out, nil
}

var (
	discordFence     = regexp.MustCompile("(?s)```.*?```")
	discordCode      = regexp.MustCompile("`[^`\n]+`")
	discordQuoteRest = regexp.MustCompile(`(?m)^>>> ?`)
	discordSmall     = regexp.MustCompile(`(?m)^-# `)
	discordTask      = regexp.MustCompile(`(?m)^(\s*[-*] )\[([ xX])\] `)
	discordURL       = regexp.MustCompile(`<(https?://[^<>\s]+)>`)
	discordUser      = regexp.MustCompile(`<@!?(\d+)>`)
	discordRole      = regexp.MustCompile(`<@&(\d+)>`)
	discordChannel   = regexp.MustCompile(`<#(\d+)>`)
	discordEmoji     = regexp.MustCompile(`<a?:(\w+):\d+>`)
	discordTime      = regexp.MustCompile(`<t:(-?\d+)(?::[a-zA-Z])?>`)
	discordUnderline = regexp.MustCompile(`__(\S(?:.*?\S)?)__`)
	discordBroadcast = regexp.MustCompile(`@(everyone|here)\b`)
)

// discordText converts Discord's Markdown to Churr Markdown: most of it
// passes through, and the table in docs/BOTS.md section 13 lists what does
// not. everyone says whether @everyone and @here may notify.
func discordText(s string, everyone bool) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.NewReplacer(string(heldOpen), "", string(heldClose), "").Replace(s)
	h := &holder{}
	// Code passes through untouched, so it is set aside first.
	s = discordFence.ReplaceAllStringFunc(s, h.hold)
	s = discordCode.ReplaceAllStringFunc(s, h.hold)

	// `>>> ` quotes everything after it; the marks are added last, once
	// code blocks are back, so every line of them is quoted too.
	var quoted string
	hasQuote := false
	if loc := discordQuoteRest.FindStringIndex(s); loc != nil {
		s, quoted, hasQuote = s[:loc[0]], s[loc[1]:], true
	}
	convert := func(s string) string {
		s = discordSmall.ReplaceAllString(s, "")
		// A webhook does not create a checklist.
		s = discordTask.ReplaceAllString(s, `$1\[$2] `)
		s = discordURL.ReplaceAllString(s, "$1")
		code := func(re *regexp.Regexp, prefix string) {
			s = re.ReplaceAllStringFunc(s, func(m string) string {
				return h.hold(codeSpan(prefix + re.FindStringSubmatch(m)[1]))
			})
		}
		// A Discord identifier names nobody here.
		code(discordRole, "@&")
		code(discordUser, "@")
		// A channel of Discord's has no name in the payload and is no
		// channel here, so nothing is written for it (ruling R52).
		s = trailingSpace.ReplaceAllString(discordChannel.ReplaceAllString(s, ""), "\n")
		s = discordEmoji.ReplaceAllString(s, ":$1:")
		s = discordTime.ReplaceAllStringFunc(s, func(m string) string {
			n, err := strconv.ParseInt(discordTime.FindStringSubmatch(m)[1], 10, 64)
			if err != nil || n < -1<<40 || n > 1<<40 {
				return m
			}
			return time.Unix(n, 0).UTC().Format("2006-01-02 15:04 UTC")
		})
		s = discordUnderline.ReplaceAllString(s, "$1")
		s = discordBroadcast.ReplaceAllStringFunc(s, func(m string) string {
			switch {
			case !everyone:
				return h.hold(codeSpan(m))
			case m == "@everyone":
				return "@channel"
			}
			return m
		})
		return s
	}
	out := h.restore(convert(s))
	if hasQuote {
		lines := strings.Split(strings.Trim(h.restore(convert(quoted)), "\n"), "\n")
		for i := range lines {
			lines[i] = strings.TrimRight("> "+lines[i], " ")
		}
		out += strings.Join(lines, "\n")
	}
	return strings.TrimSpace(out)
}
