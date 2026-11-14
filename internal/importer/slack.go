package importer

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"sort"
	"strconv"
	"strings"
)

// The shapes of a Slack export, as far as the importer reads them. An
// export is a zip archive: users.json, channels.json, and where the plan
// allowed it groups.json (private channels), mpims.json (group direct
// messages) and dms.json (direct messages); then one folder per
// conversation with a file of messages for each day.

type slackUser struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	RealName          string `json:"real_name"`
	Deleted           bool   `json:"deleted"`
	IsBot             bool   `json:"is_bot"`
	IsRestricted      bool   `json:"is_restricted"`
	IsUltraRestricted bool   `json:"is_ultra_restricted"`
	TZ                string `json:"tz"`
	Updated           int64  `json:"updated"`
	Profile           struct {
		DisplayName   string `json:"display_name"`
		RealName      string `json:"real_name"`
		Title         string `json:"title"`
		Email         string `json:"email"`
		ImageOriginal string `json:"image_original"`
		Image512      string `json:"image_512"`
		Image192      string `json:"image_192"`
	} `json:"profile"`
}

// avatar is the address of the largest profile picture the export names.
func (u slackUser) avatar() string {
	for _, a := range []string{u.Profile.ImageOriginal, u.Profile.Image512, u.Profile.Image192} {
		if a != "" {
			return a
		}
	}
	return ""
}

type slackText struct {
	Value string `json:"value"`
}

type slackPin struct {
	ID      string `json:"id"` // the pinned message's ts
	Type    string `json:"type"`
	Created int64  `json:"created"`
	User    string `json:"user"`
}

type slackChannel struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Created    int64      `json:"created"`
	Creator    string     `json:"creator"`
	IsArchived bool       `json:"is_archived"`
	IsGeneral  bool       `json:"is_general"`
	Members    []string   `json:"members"`
	Topic      slackText  `json:"topic"`
	Purpose    slackText  `json:"purpose"`
	Pins       []slackPin `json:"pins"`

	// kind is where the export listed it, and folder where its messages
	// are.
	kind   string // public | private | group_dm | dm
	folder string
}

type slackFile struct {
	ID                 string `json:"id"`
	Name               string `json:"name"`
	Title              string `json:"title"`
	Mode               string `json:"mode"`
	URLPrivate         string `json:"url_private"`
	URLPrivateDownload string `json:"url_private_download"`
}

func (f slackFile) address() string {
	if f.URLPrivateDownload != "" {
		return f.URLPrivateDownload
	}
	return f.URLPrivate
}

func (f slackFile) name() string {
	for _, n := range []string{f.Name, f.Title} {
		if strings.TrimSpace(n) != "" {
			return n
		}
	}
	return "file"
}

type slackField struct {
	Title string `json:"title"`
	Value string `json:"value"`
	Short bool   `json:"short"`
}

type slackAttachment struct {
	Color      string       `json:"color"`
	AuthorName string       `json:"author_name"`
	AuthorLink string       `json:"author_link"`
	Title      string       `json:"title"`
	TitleLink  string       `json:"title_link"`
	Pretext    string       `json:"pretext"`
	Text       string       `json:"text"`
	Fallback   string       `json:"fallback"`
	Fields     []slackField `json:"fields"`
	ImageURL   string       `json:"image_url"`
	ThumbURL   string       `json:"thumb_url"`
	Footer     string       `json:"footer"`
	TS         json.Number  `json:"ts"`
}

type slackReaction struct {
	Name  string   `json:"name"`
	Users []string `json:"users"`
}

type slackMessage struct {
	Type     string `json:"type"`
	Subtype  string `json:"subtype"`
	User     string `json:"user"`
	BotID    string `json:"bot_id"`
	Username string `json:"username"`
	Text     string `json:"text"`
	TS       string `json:"ts"`
	ThreadTS string `json:"thread_ts"`
	Edited   *struct {
		TS string `json:"ts"`
	} `json:"edited"`
	Reactions   []slackReaction   `json:"reactions"`
	Files       []slackFile       `json:"files"`
	File        *slackFile        `json:"file"` // the older, single form
	Attachments []slackAttachment `json:"attachments"`
}

// imported are the kinds of message that are somebody saying something.
// Every other subtype is a notice (joined, left, set the topic, renamed,
// pinned, archived, a reminder, a huddle) and is not imported.
var imported = map[string]bool{"": true, "bot_message": true, "file_share": true, "me_message": true, "thread_broadcast": true}

// millis reads a Slack timestamp ("1700000000.000200": seconds, then a
// counter in microseconds that makes it unique in its channel) as
// milliseconds. ok is false for anything else.
func millis(ts string) (ms int64, ok bool) {
	sec, frac, _ := strings.Cut(ts, ".")
	s, err := strconv.ParseInt(sec, 10, 64)
	if err != nil || s <= 0 {
		return 0, false
	}
	var micro int64
	if frac != "" {
		for len(frac) < 6 {
			frac += "0"
		}
		if micro, err = strconv.ParseInt(frac[:6], 10, 64); err != nil {
			return 0, false
		}
	}
	return s*1000 + micro/1000, true
}

// micros orders timestamps exactly.
func micros(ts string) int64 {
	sec, frac, _ := strings.Cut(ts, ".")
	s, _ := strconv.ParseInt(sec, 10, 64)
	for len(frac) < 6 {
		frac += "0"
	}
	m, _ := strconv.ParseInt(frac[:6], 10, 64)
	return s*1_000_000 + m
}

// archive is a Slack export, opened.
type archive struct {
	zip *zip.ReadCloser
	// files are the archive's entries by path, with any single folder the
	// whole export was wrapped in removed.
	files    map[string]*zip.File
	users    []slackUser
	channels []slackChannel
	// emoji is name to address, or to "alias:other".
	emoji map[string]string
}

// Limits on what is read from an export, so that a file that is not one, or
// one made to hurt, cannot exhaust the machine.
const (
	maxListBytes = 256 << 20 // users.json, channels.json and the like
	maxDayBytes  = 256 << 20 // one day of one channel
)

// open opens an export and reads its lists of people and conversations.
func open(zipPath string) (*archive, error) {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, fmt.Errorf("%s cannot be read as a zip archive: %w", zipPath, err)
	}
	a := &archive{zip: zr, files: map[string]*zip.File{}}
	// Some tools zip the folder, not its contents.
	prefix := ""
	for _, f := range zr.File {
		if base := path.Base(f.Name); base == "users.json" && !f.FileInfo().IsDir() {
			if dir := path.Dir(f.Name); dir != "." && (prefix == "" || len(dir) < len(prefix)) {
				prefix = dir + "/"
			} else if dir == "." {
				prefix = ""
				break
			}
		}
	}
	for _, f := range zr.File {
		if f.FileInfo().IsDir() || !strings.HasPrefix(f.Name, prefix) {
			continue
		}
		a.files[strings.TrimPrefix(f.Name, prefix)] = f
	}
	if _, ok := a.files["users.json"]; !ok {
		zr.Close()
		return nil, errors.New("this is not a Slack export: it has no users.json")
	}
	if err := a.read("users.json", maxListBytes, &a.users); err != nil {
		zr.Close()
		return nil, err
	}
	for _, list := range []struct{ file, kind string }{
		{"channels.json", "public"}, {"groups.json", "private"}, {"mpims.json", "group_dm"}, {"dms.json", "dm"},
	} {
		if _, ok := a.files[list.file]; !ok {
			continue
		}
		var chans []slackChannel
		if err := a.read(list.file, maxListBytes, &chans); err != nil {
			zr.Close()
			return nil, err
		}
		for _, c := range chans {
			c.kind, c.folder = list.kind, c.Name
			if list.kind == "dm" || c.folder == "" {
				c.folder = c.ID
			}
			if c.ID != "" {
				a.channels = append(a.channels, c)
			}
		}
	}
	if _, ok := a.files["emoji.json"]; ok {
		if err := a.read("emoji.json", maxListBytes, &a.emoji); err != nil {
			zr.Close()
			return nil, err
		}
	}
	return a, nil
}

func (a *archive) close() { a.zip.Close() }

// read decodes one JSON file of the archive.
func (a *archive) read(name string, limit int64, into any) error {
	f := a.files[name]
	if f == nil {
		return fmt.Errorf("the export has no %s", name)
	}
	if int64(f.UncompressedSize64) > limit {
		return fmt.Errorf("%s in the export is larger than this importer will read", name)
	}
	rc, err := f.Open()
	if err != nil {
		return fmt.Errorf("%s in the export: %w", name, err)
	}
	defer rc.Close()
	dec := json.NewDecoder(io.LimitReader(rc, limit))
	dec.UseNumber()
	if err := dec.Decode(into); err != nil {
		return fmt.Errorf("%s in the export is not what a Slack export holds: %w", name, err)
	}
	return nil
}

// messages reads every message of a conversation, in the order they were
// sent. Messages with no usable timestamp are dropped; two with the same
// timestamp (the export repeats a thread's parent in some versions) are
// kept once.
func (a *archive) messages(c slackChannel) ([]slackMessage, error) {
	var days []string
	for name := range a.files {
		if path.Dir(name) == c.folder && strings.HasSuffix(name, ".json") {
			days = append(days, name)
		}
	}
	sort.Strings(days)
	var out []slackMessage
	seen := map[string]bool{}
	for _, day := range days {
		var list []slackMessage
		if err := a.read(day, maxDayBytes, &list); err != nil {
			return nil, err
		}
		for _, m := range list {
			if _, ok := millis(m.TS); !ok || seen[m.TS] {
				continue
			}
			seen[m.TS] = true
			if m.File != nil && len(m.Files) == 0 {
				m.Files = []slackFile{*m.File}
			}
			out = append(out, m)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return micros(out[i].TS) < micros(out[j].TS) })
	return out, nil
}
