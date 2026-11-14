package bots

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/tikaydaes/churr/internal/model"
)

// The GitHub converter (docs/BOTS.md section 14): one event in, one message
// with one card out. It is a pure function of its input and the time the
// request arrived.

// gh is a JSON object from a GitHub payload, read without a struct for
// every event: only a handful of members are used from each.
type gh map[string]any

func (o gh) obj(key string) gh {
	m, _ := o[key].(map[string]any)
	return m
}

func (o gh) has(key string) bool { return o[key] != nil }

func (o gh) str(key string) string {
	s, _ := o[key].(string)
	return strings.TrimSpace(s)
}

func (o gh) flag(key string) bool {
	b, _ := o[key].(bool)
	return b
}

func (o gh) num(key string) int64 {
	f, _ := o[key].(float64)
	return int64(f)
}

func (o gh) list(key string) []gh {
	raw, _ := o[key].([]any)
	out := make([]gh, 0, len(raw))
	for _, v := range raw {
		if m, ok := v.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

var htmlComment = regexp.MustCompile(`(?s)<!--.*?-->`)

// excerpt is the "body excerpt" of section 14: HTML comments removed, the
// card text rule applied, and cut to 500 characters at a word.
func excerpt(body string) string {
	return truncateAtWord(cardText(htmlComment.ReplaceAllString(body, "")), maxExcerpt)
}

// firstLine is the first line of a commit message, cut to 72 characters and
// escaped, since a commit message is plain text.
func firstLine(message string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(message), "\n")
	return escapeInline(truncate(strings.TrimSpace(line), 72))
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

// ConvertGitHub turns one GitHub event into a Payload. event is the value
// of X-GitHub-Event and now is when the request arrived. post is false for
// a listed event whose action posts nothing. A body that is not a JSON
// object is refused with invalid_request.
func ConvertGitHub(event string, raw []byte, now model.Time) (p Payload, post bool, err error) {
	var in gh
	if err := json.Unmarshal(raw, &in); err != nil || in == nil {
		return Payload{}, false, model.BadRequest("The body is not a GitHub event.")
	}
	repo, sender := in.obj("repository"), in.obj("sender")
	// What every card has.
	c := Card{Footer: repo.str("full_name"), At: now}
	if login := sender.str("login"); login != "" {
		c.Author = &CardAuthor{Name: login, IconURL: webAddress(sender.str("avatar_url"))}
	}
	action := in.str("action")

	switch event {
	case "ping":
		c.Title, c.Color = "GitHub is connected", "cyan"
		if c.Text = escapeInline(repo.str("full_name")); c.Text == "" {
			c.Text = escapeInline(in.obj("organization").str("login"))
		}

	case "push":
		ref, kind := in.str("ref"), "Branch"
		if t, ok := strings.CutPrefix(ref, "refs/tags/"); ok {
			ref, kind = t, "Tag"
		} else {
			ref = strings.TrimPrefix(ref, "refs/heads/")
		}
		commits := in.list("commits")
		n := len(commits)
		switch {
		case in.flag("deleted"):
			c.Title, c.Color = kind+" "+ref+" deleted", "grey"
		case in.flag("created") && kind == "Tag":
			c.Title, c.Color = "Tag "+ref+" created", "cyan"
		case in.flag("created") && n == 0:
			c.Title, c.Color = "Branch "+ref+" created", "cyan"
		case n == 0:
			// Nothing was pushed, made or deleted.
			return Payload{}, false, nil
		case in.flag("forced"):
			c.Title, c.Color = plural(n, "commit", "commits")+" force-pushed to "+ref, "yellow"
		default:
			c.Title, c.Color = plural(n, "commit", "commits")+" pushed to "+ref, "lavender"
		}
		c.URL = webAddress(in.str("compare"))
		var lines []string
		for i, cm := range commits {
			if i == 5 {
				break
			}
			id := cm.str("id")
			if len(id) > 7 {
				id = id[:7]
			}
			who := cm.obj("author").str("username")
			if who == "" {
				who = cm.obj("author").str("name")
			}
			line := "- " + linkTo(codeSpan(id), cm.str("url")) + " " + firstLine(cm.str("message"))
			if who != "" {
				line += " — " + escapeInline(who)
			}
			lines = append(lines, line)
		}
		c.Text = strings.Join(lines, "\n")
		if n > 5 {
			c.Text += fmt.Sprintf("\n\nand %d more", n-5)
		}

	case "pull_request":
		pr := in.obj("pull_request")
		what := "Pull request"
		var verb string
		switch action {
		case "opened":
			verb, c.Color = "opened", "green"
			if pr.flag("draft") {
				what = "Draft pull request"
			}
		case "ready_for_review":
			verb, c.Color = "ready for review", "green"
		case "reopened":
			verb, c.Color = "reopened", "green"
		case "closed":
			verb, c.Color = "closed", "red"
			if pr.flag("merged") {
				verb, c.Color = "merged", "lavender"
			}
		default:
			return Payload{}, false, nil
		}
		c.Title = fmt.Sprintf("%s #%d %s: %s", what, number(in, pr), verb, pr.str("title"))
		c.URL = webAddress(pr.str("html_url"))
		if head, base := pr.obj("head").str("ref"), pr.obj("base").str("ref"); head != "" || base != "" {
			c.Fields = append(c.Fields, model.CardField{Label: "Branch", Value: escapeInline(head + " → " + base), Short: true})
		}
		if action == "opened" {
			files := int(pr.num("changed_files"))
			c.Fields = append(c.Fields, model.CardField{Label: "Changes", Short: true,
				Value: fmt.Sprintf("+%d −%d in %s", pr.num("additions"), pr.num("deletions"), plural(files, "file", "files"))})
			c.Text = excerpt(pr.str("body"))
		}

	case "issues":
		issue := in.obj("issue")
		var verb string
		switch action {
		case "opened":
			verb, c.Color = "opened", "green"
		case "closed":
			verb, c.Color = "closed", "lavender"
			if issue.str("state_reason") == "not_planned" {
				c.Color = "grey"
			}
		case "reopened":
			verb, c.Color = "reopened", "green"
		default:
			return Payload{}, false, nil
		}
		c.Title = fmt.Sprintf("Issue #%d %s: %s", issue.num("number"), verb, issue.str("title"))
		c.URL = webAddress(issue.str("html_url"))
		if action == "opened" {
			c.Text = excerpt(issue.str("body"))
			var labels []string
			for _, l := range issue.list("labels") {
				if name := l.str("name"); name != "" {
					labels = append(labels, escapeInline(name))
				}
			}
			if len(labels) > 0 {
				c.Fields = append(c.Fields, model.CardField{Label: "Labels", Value: strings.Join(labels, ", ")})
			}
		}

	case "issue_comment":
		if action != "created" {
			return Payload{}, false, nil
		}
		issue, comment := in.obj("issue"), in.obj("comment")
		what := "issue"
		if issue.has("pull_request") {
			what = "pull request"
		}
		c.Title = fmt.Sprintf("Comment on %s #%d: %s", what, issue.num("number"), issue.str("title"))
		c.URL, c.Text, c.Color = webAddress(comment.str("html_url")), excerpt(comment.str("body")), "grey"

	case "release":
		if action != "published" {
			return Payload{}, false, nil
		}
		rel := in.obj("release")
		name, what := rel.str("name"), "Release"
		if name == "" {
			name = rel.str("tag_name")
		}
		if rel.flag("prerelease") {
			what = "Pre-release"
		}
		c.Title = what + " " + name + " published"
		c.URL, c.Text, c.Color = webAddress(rel.str("html_url")), excerpt(rel.str("body")), "cyan"
		if tag := rel.str("tag_name"); tag != "" {
			c.Fields = append(c.Fields, model.CardField{Label: "Tag", Value: escapeInline(tag)})
		}

	case "workflow_run":
		if action != "completed" {
			return Payload{}, false, nil
		}
		run := in.obj("workflow_run")
		conclusion := run.str("conclusion")
		c.Title = fmt.Sprintf("%s: %s on %s", run.str("name"), strings.ReplaceAll(conclusion, "_", " "), run.str("head_branch"))
		c.URL = webAddress(run.str("html_url"))
		if sha := run.str("head_sha"); sha != "" {
			if len(sha) > 7 {
				sha = sha[:7]
			}
			c.Fields = append(c.Fields, model.CardField{Label: "Commit", Value: codeSpan(sha), Short: true})
		}
		runValue := fmt.Sprintf("#%d", run.num("run_number"))
		if a := run.num("run_attempt"); a > 1 {
			runValue += fmt.Sprintf(" attempt %d", a)
		}
		c.Fields = append(c.Fields, model.CardField{Label: "Run", Value: runValue, Short: true})
		if ev := run.str("event"); ev != "" {
			c.Fields = append(c.Fields, model.CardField{Label: "Trigger", Value: escapeInline(ev), Short: true})
		}
		if t, err := time.Parse(time.RFC3339, run.str("updated_at")); err == nil {
			c.At = model.Time(t.UnixMilli())
		}
		switch conclusion {
		case "success":
			c.Color = "green"
		case "failure", "timed_out", "startup_failure":
			c.Color = "red"
		case "action_required":
			c.Color = "yellow"
		default:
			c.Color = "grey"
		}

	default:
		// Nothing GitHub was asked to send vanishes: any other event is
		// one plain card.
		c.Title = strings.ReplaceAll(event, "_", " ")
		if action != "" {
			c.Title += " " + strings.ReplaceAll(action, "_", " ")
		}
		if c.URL = webAddress(in.obj(event).str("html_url")); c.URL == "" {
			c.URL = webAddress(repo.str("html_url"))
		}
		c.Color = "grey"
	}
	c.Title = truncate(strings.Join(strings.Fields(c.Title), " "), maxCardTitle)
	return Payload{Cards: []Card{c}}, true, nil
}

// number is a pull request's number, which GitHub gives both beside and
// inside the pull request.
func number(in, pr gh) int64 {
	if n := in.num("number"); n != 0 {
		return n
	}
	return pr.num("number")
}
