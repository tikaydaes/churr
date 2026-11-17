package main

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/url"
	"strings"
)

// m is shorthand for a JSON object.
type m = map[string]any

// must turns a failed request into a panic that seed recovers, which keeps
// the long list of steps below readable.
func must(out map[string]any, err error) map[string]any {
	if err != nil {
		panic(err)
	}
	return out
}

func sub(out map[string]any, key string) map[string]any { return out[key].(map[string]any) }

// picture draws a flat image in the house colours: bands of solid colour
// with square blocks, no gradients.
func picture(w, h int, bg, fg color.RGBA) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := bg
			if (x/(w/8)+y/(h/4))%3 == 0 {
				c = fg
			}
			img.SetRGBA(x, y, c)
		}
	}
	var buf bytes.Buffer
	png.Encode(&buf, img)
	return buf.Bytes()
}

var (
	navy     = color.RGBA{0x0d, 0x10, 0x26, 0xff}
	lavender = color.RGBA{0xb9, 0xa0, 0xff, 0xff}
	cyan     = color.RGBA{0x5d, 0xf0, 0xe4, 0xff}
	pink     = color.RGBA{0xff, 0x5c, 0xcf, 0xff}
	yellow   = color.RGBA{0xff, 0xd9, 0x5e, 0xff}
)

func (c *client) channel(name string, body m, members ...*client) string {
	ids := []string{}
	for _, p := range members {
		ids = append(ids, p.id)
	}
	body["name"], body["account_ids"] = name, ids
	if _, ok := body["private"]; !ok {
		// createChannel requires the caller to say which.
		body["private"] = false
	}
	id := sub(must(c.do("POST", "/api/v1/channels", body, 201)), "conversation")["id"].(string)
	// The people a channel is made with are invited (ruling R103): the
	// seeded ones accept.
	for _, p := range members {
		p.acceptInvitation(id)
	}
	return id
}

// acceptInvitation accepts this person's invitation to a conversation, if
// one waits.
func (c *client) acceptInvitation(conversation string) {
	out := must(c.do("GET", "/api/v1/me/chat-requests", nil, 200))
	list, _ := out["requests"].([]any)
	for _, r := range list {
		req, _ := r.(map[string]any)
		if req["conversation_id"] == conversation {
			must(c.do("POST", "/api/v1/me/chat-requests/"+req["id"].(string)+"/accept", nil, 200))
		}
	}
}

func (c *client) chat(with ...*client) string {
	ids := []string{}
	for _, p := range with {
		ids = append(ids, p.id)
	}
	return sub(must(c.do("POST", "/api/v1/chats", m{"account_ids": ids}, 200, 201)), "conversation")["id"].(string)
}

// post posts a message and returns it.
func (c *client) post(conversation string, body m) map[string]any {
	return sub(must(c.do("POST", "/api/v1/conversations/"+conversation+"/messages", body, 201)), "message")
}

func (c *client) say(conversation, text string) map[string]any {
	return c.post(conversation, m{"text": text})
}

func (c *client) react(message map[string]any, emoji string) {
	must(c.do("PUT", "/api/v1/messages/"+message["id"].(string)+"/reactions/"+url.PathEscape(emoji), nil, 200))
}

func (c *client) file(conversation, name string, content []byte, caption string) string {
	fields := map[string]string{}
	if caption != "" {
		fields["caption"] = caption
	}
	return sub(must(c.upload("/api/v1/conversations/"+conversation+"/files", "file", name, content, fields)), "file")["id"].(string)
}

func (c *client) membership(conversation string, body m) {
	must(c.do("PATCH", "/api/v1/conversations/"+conversation+"/membership", body, 200))
}

// showcase is one message holding every construct of Churr Markdown
// (docs/MESSAGE-FORMAT.md section 2). %s are: a channel id, a channel id
// nobody can see.
const showcase = "# Churr Markdown\n" +
	"## Every construct, in one message\n" +
	"### Inline\n" +
	"Some **bold**, *italic*, _also italic_, ~~struck~~ and ||a spoiler||, with `inline code`, " +
	"a [named link](https://example.org/docs), a bare link https://example.org/a_(b), " +
	"a mention of @kai and of @everyone, a channel link <#%s>, one to nowhere <#%s>, " +
	"a custom emoji :party:, a standard short code :smile:, an escaped \\*star\\* and an image ![diagram](https://example.org/d.png).\n" +
	"A second line of the same paragraph.\n\n" +
	"> A quote.\n> > A quote inside it, with **bold**.\n\n" +
	"- A bullet\n  - nested once\n    - nested twice\n- [ ] A checklist item\n- [x] One ticked as typed\n\n" +
	"3. A numbered list that starts at three\n4. and goes on\n\n" +
	"```go\n// Code, highlighted.\nfunc main() {\n\tfmt.Println(\"churr\", 42)\n}\n```\n\n" +
	"```\nA block with no language is plain monospace.\n```\n\n" +
	"| Service | Status | Owner |\n|---------|:------:|------:|\n| api     | up     | @sam  |\n| search  | **down** | @lee  |\n\n" +
	"---\n" +
	"Not Markdown, so shown as typed: <b>html</b>, #general typed by hand, and 10:30:45."

// seed makes the community and returns what devseed prints.
func (w *world) seed(tokenFor []string, history int) (res output, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%v", r)
		}
	}()

	kai := must2(w.setup("kai", "Kai Tanaka"))
	invite := func() string {
		return sub(must(kai.do("POST", "/api/v1/invites", m{}, 201)), "invite")["code"].(string)
	}
	sam := must2(w.register("sam", "Sam Okafor", invite()))
	lee := must2(w.register("lee", "Lee Park", invite()))
	mia := must2(w.register("mia", "Mia Rossi", invite()))
	ravi := must2(w.register("ravi", "Ravi Shah", invite()))
	w.people = map[string]*client{"kai": kai, "sam": sam, "lee": lee, "mia": mia, "ravi": ravi}
	// New chats are requests by default (ruling R78). The seeded people
	// accept every chat at once, so the seeded chats open with history.
	for _, p := range []*client{kai, sam, lee, mia, ravi} {
		must(p.do("PATCH", "/api/v1/me/prefs", m{"auto_approve_direct": true, "auto_approve_sealed": true}, 200))
	}

	must(sam.do("PATCH", "/api/v1/me", m{"title": "Design lead", "pronouns": "they/them"}, 200))
	must(lee.do("PUT", "/api/v1/me/status", m{"emoji": "🌴", "text": "Back on Monday", "appear_away": true}, 200))

	// The bot posts the cards: a person typing a message cannot.
	botOut := must(kai.do("POST", "/api/v1/admin/bots", m{"username": "ci", "display_name": "CI"}, 201))
	bot := &client{w: w, http: kai.http, bearer: botOut["secret"].(string), username: "ci",
		id: sub(sub(botOut, "bot"), "account")["id"].(string)}

	must(kai.upload("/api/v1/emoji", "file", "party.png", picture(64, 64, pink, yellow), map[string]string{"name": "party"}))

	section := func(name string, pos int) string {
		return sub(must(kai.do("POST", "/api/v1/admin/sections", m{"name": name, "position": pos}, 201)), "section")["id"].(string)
	}
	projects, social := section("Projects", 1), section("Social", 2)

	cv := map[string]string{}
	cv["general"] = kai.channel("General", m{"topic": "Everything that fits nowhere else"}, sam, lee, mia, ravi)
	cv["design"] = kai.channel("Design reviews", m{"section_id": projects, "topic": "Fridays at 10"}, sam, lee, mia)
	cv["icons"] = kai.channel("Icons", m{"parent_id": cv["design"]}, sam)
	cv["typography"] = kai.channel("Typography", m{"parent_id": cv["design"]}, lee)
	cv["ops"] = kai.channel("Ops", m{"section_id": projects}, sam, ravi)
	cv["deploys"] = kai.channel("Deploys", m{"parent_id": cv["ops"]}, sam)
	// A second channel with the same name: names are not unique (R50).
	cv["design_social"] = kai.channel("Design reviews", m{"section_id": social, "topic": "The film club's, not the team's"}, mia)
	cv["japanese"] = kai.channel("デザイン", m{"section_id": social}, mia, lee)
	cv["leadership"] = kai.channel("Leadership", m{"private": true}, lee)
	cv["random"] = kai.channel("Random", m{"section_id": social}, sam, lee, mia, ravi)
	cv["old_plans"] = kai.channel("Old plans", m{}, sam)
	cv["history"] = kai.channel("Long history", m{"topic": "For trying paging"}, sam)
	// A public parent kai is not in, with a sub-channel kai is in: the
	// sidebar draws it as `Tickets › tickets-1` (CLIENTS.md 9.25).
	cv["tickets"] = sam.channel("Tickets", m{"section_id": projects}, lee)
	cv["ticket_1"] = sam.channel("tickets-1", m{"parent_id": cv["tickets"]}, kai)
	// A channel kai is not in at all, for Browse channels.
	cv["sams_corner"] = sam.channel("Sam's corner", m{}, lee)
	cv["chat_sam"] = kai.chat(sam)
	cv["chat_group"] = kai.chat(sam, lee)
	cv["chat_mia"] = mia.chat(kai)

	must(kai.do("POST", "/api/v1/conversations/"+cv["deploys"]+"/members", m{"account_ids": []string{bot.id}}, 200))

	// General: the showcase and one of everything a message can carry.
	g := cv["general"]
	kai.say(g, "Welcome to the seeded community. Everything here was made by `tools/devseed`.")
	show := sam.say(g, fmt.Sprintf(showcase, cv["design"], "0193a000-0000-7000-8000-00000000dead"))
	kai.react(show, "👍")
	lee.react(show, "👍")
	mia.react(show, ":party:")
	// Lee can see Leadership, so the link gets an entity; readers who are
	// not in it see "private channel".
	lee.say(g, "The budget thread is in <#"+cv["leadership"]+">, for those who can see it.")

	plan := kai.say(g, "**Deploy plan for Friday**\n\n- [ ] Tag the release\n- [ ] Tell support\n- [x] Book the room\n\n@sam can you check <#"+cv["ops"]+">?")
	for _, t := range plan["tasks"].([]any) {
		// Tick the first item as sam, so a tick by someone else shows.
		must(sam.do("PUT", "/api/v1/messages/"+plan["id"].(string)+"/checklist/"+t.(map[string]any)["id"].(string), m{"checked": true}, 200))
		break
	}
	for i, p := range []*client{sam, lee, sam} {
		p.post(g, m{"parent_id": plan["id"], "text": []string{"On it. The tag is `v2.4.0`.", "Support knows.", "Done, *shipping at five*."}[i]})
	}
	ravi.react(plan, "🚀")

	poll := lee.post(g, m{"form": m{"preset": "poll", "title": "Lunch on Friday?",
		"fields": []m{{"id": "choice", "type": "choice", "label": "Where", "required": true, "options": []m{{"id": "a", "label": "Noodles"}, {"id": "b", "label": "Tacos"}, {"id": "c", "label": "Whatever Sam is having"}}}},
		"action": m{"type": "tally"}, "anonymous": false, "results": "live"}})
	for p, choice := range map[*client]string{kai: "a", sam: "b", mia: "a"} {
		must(p.do("PUT", "/api/v1/messages/"+poll["id"].(string)+"/form/answer", m{"answers": m{"choice": []string{choice}}}, 200))
	}

	mia.post(g, m{"text": "The floor plan, and the colours we settled on.", "file_ids": []string{
		mia.file(g, "floorplan.png", picture(960, 540, navy, lavender), "Second floor"),
		mia.file(g, "colours.png", picture(640, 640, cyan, pink), ""),
	}})
	ravi.post(g, m{"text": "Notes from the retro.", "file_ids": []string{
		ravi.file(g, "retro-notes.txt", []byte("What went well\n- the launch\n- the docs\n\nWhat to change\n- fewer meetings\n- more tests\n"), ""),
	}})
	ravi.post(g, m{"file_ids": []string{ravi.file(g, "export.bin", bytes.Repeat([]byte{0, 1, 2, 3, 254, 255}, 4096), "")}})

	// Deploys: cards.
	d := cv["deploys"]
	bot.post(d, m{"cards": []m{{"color": "pink", "author": m{"name": "CI"}, "title": "Build #482 failed", "url": "https://ci.example/builds/482",
		"text":   "`main` · 3 tests failing\n- `TestResume`\n- `TestReload`",
		"fields": []m{{"label": "Branch", "value": "main", "short": true}, {"label": "Duration", "value": "4m 12s", "short": true}},
		"footer": "ci.example"}}})
	bot.post(d, m{"text": "Fixed in the next run.", "cards": []m{{"color": "green", "title": "Build #483 passed", "url": "https://ci.example/builds/483",
		"text": "All **212** tests green.", "image_file_id": bot.file(d, "coverage.png", picture(480, 160, navy, cyan), ""), "footer": "ci.example"},
		{"color": "yellow", "title": "Deploy waiting for approval", "fields": []m{{"label": "Environment", "value": "production"}}}}})
	sam.say(d, "Approving. @kai fyi")

	// The long channel, for paging. Authors alternate in runs so the
	// grouping of consecutive messages shows.
	for i := 1; i <= history; i++ {
		p := kai
		if (i/4)%2 == 1 {
			p = sam
		}
		p.say(cv["history"], fmt.Sprintf("Message %d of %d", i, history))
	}

	// A few lines everywhere else, so no conversation opens empty.
	sam.say(cv["design"], "Agenda for Friday: the new sidebar, then icons.")
	mia.say(cv["design"], "I'll bring the three layouts side by side.")
	sam.say(cv["icons"], "Hash, lock and padlock are in. Sub-channel next.")
	lee.say(cv["typography"], "Archivo for text, Martian Mono for code, times and counts.")
	ravi.say(cv["ops"], "Disk on `db-2` is at 81%.")
	mia.say(cv["design_social"], "Same name as the other one, on purpose.")
	mia.say(cv["japanese"], "こんにちは。デザインの話をしましょう。")
	lee.say(cv["leadership"], "Budget review moved to Tuesday.")
	sam.say(cv["random"], "Anyone for coffee?")
	sam.say(cv["old_plans"], "Archiving this soon.")
	sam.say(cv["ticket_1"], "Printer on the second floor is jammed again.")
	sam.say(cv["sams_corner"], "A channel kai has not joined.")
	kai.say(cv["chat_sam"], "Did the build go out?")
	sam.say(cv["chat_sam"], "Yes, ten minutes ago.")
	lee.say(cv["chat_group"], "Lunch at one?")

	// Kai has read everything up to here, except what is posted below.
	list := must(kai.do("GET", "/api/v1/conversations?limit=200", nil, 200))
	for _, c := range list["conversations"].([]any) {
		conv := c.(map[string]any)
		must(kai.do("PUT", "/api/v1/conversations/"+conv["id"].(string)+"/read", m{"seq": conv["last_seq"]}, 200))
	}

	// Kai's settings: one of each case the sidebar has to draw (R46).
	kai.membership(cv["design"], m{"starred": true})
	kai.membership(cv["random"], m{"mode": "nothing"})
	kai.membership(cv["old_plans"], m{"show": "hidden"})
	kai.membership(cv["japanese"], m{"show": "activity"})
	kai.membership(cv["leadership"], m{"show": "notification"})
	kai.membership(cv["history"], m{"mode": "any"})

	// Unread for kai: a mention (a badge under `mentions`), plain unread
	// in a `mentions` channel (bold, no badge), a chat message (a badge
	// under `any`), and unread in the dimmed channel.
	ravi.say(cv["ops"], "@kai the disk is at 90% now, can you look?")
	mia.say(cv["design"], "Slides are up.")
	mia.say(cv["chat_mia"], "Got a minute?")
	lee.say(cv["random"], "I'm in.")
	sam.say(cv["icons"], "Two more to draw.")

	for _, name := range tokenFor {
		name = strings.TrimSpace(name)
		p := w.people[name]
		if p == nil {
			return res, fmt.Errorf("no seeded account is called %q", name)
		}
		a, err := p.appTokens("devseed (" + name + ")")
		if err != nil {
			return res, err
		}
		res.Accounts = append(res.Accounts, a)
	}
	res.Origin, res.Conversations, res.InviteCode = w.origin, cv, invite()
	return res, nil
}

func must2(c *client, err error) *client {
	if err != nil {
		panic(err)
	}
	return c
}
