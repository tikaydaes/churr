package api_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	imgpng "image/png"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/preview"
	"github.com/tikaydaes/churr/internal/store"
	"github.com/tikaydaes/churr/internal/testkit"
)

func pngBytes(w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := 0; x < w; x++ {
		for y := 0; y < h; y++ {
			img.Set(x, y, color.RGBA{uint8(x), uint8(y), 99, 255})
		}
	}
	var b bytes.Buffer
	imgpng.Encode(&b, img)
	return b.Bytes()
}

// tinyMP4 is the smallest thing that is an MP4 by content: a 640x360 movie
// of 12 seconds, with no pictures in it.
func tinyMP4() []byte {
	box := func(kind string, body ...[]byte) []byte {
		b := bytes.Join(body, nil)
		out := binary.BigEndian.AppendUint32(nil, uint32(8+len(b)))
		return append(append(out, kind...), b...)
	}
	u32 := func(v uint32) []byte { return binary.BigEndian.AppendUint32(nil, v) }
	matrix := bytes.Join([][]byte{u32(0x10000), u32(0), u32(0), u32(0), u32(0x10000), u32(0), u32(0), u32(0), u32(0x40000000)}, nil)
	mvhd := box("mvhd", u32(0), u32(0), u32(0), u32(1000), u32(12000), make([]byte, 80))
	tkhd := box("tkhd", u32(0), u32(0), u32(0), u32(1), u32(0), u32(12000), make([]byte, 16), matrix, u32(640<<16), u32(360<<16))
	return bytes.Join([][]byte{box("ftyp", []byte("isom"), u32(0), []byte("isomiso2mp41")), box("moov", mvhd, box("trak", tkhd)), box("mdat", make([]byte, 4000))}, nil)
}

func TestFiles(t *testing.T) {
	ts := testkit.NewServer(t)
	ann, bob, out := ts.Member("ann"), ts.Member("bob"), ts.Member("out")
	priv := ann.Channel("priv", true, bob)
	pub := ann.Channel("pub", false)
	files := conv + priv + "/files"

	// Kind and media type come from the content, never from the name.
	text := ann.Upload(priv, "../../etc/notes.exe", []byte("line one\nline two\n"), testkit.Part{Field: "caption", Content: []byte("Two lines")})
	if text["kind"] != "text" || text["name"] != "notes.exe" || text["media_type"] != "text/plain; charset=utf-8" || text["caption"] != "Two lines" ||
		text["preview_text"] != "line one\nline two" && text["preview_text"] != "line one\nline two\n" || text["size"] != float64(18) {
		t.Fatalf("a text file: %v", text)
	}
	img := ann.Upload(priv, "photo.txt", pngBytes(1000, 500))
	if img["kind"] != "image" || img["media_type"] != "image/png" || img["width"] != float64(1000) || img["height"] != float64(500) || img["thumb_url"] == nil {
		t.Fatalf("an image: %v", img)
	}
	// SVG is always a plain file.
	svg := ann.Upload(priv, "logo.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`))
	if svg["kind"] != "file" || svg["media_type"] != "application/octet-stream" || svg["thumb_url"] != nil {
		t.Fatalf("an SVG: %v", svg)
	}
	html := ann.Upload(priv, "page.html", []byte("<html><script>alert(1)</script></html>"))
	if html["kind"] != "text" || !strings.HasPrefix(html["media_type"].(string), "text/plain") {
		t.Fatalf("HTML is text and never a type a browser would run: %v", html)
	}
	// A video with the one poster image a client may send; its size and
	// length are read from the container.
	video := ann.Upload(priv, "clip.mp4", tinyMP4(), testkit.Part{Field: "poster", FileName: "poster.png", Content: pngBytes(64, 36)})
	if video["kind"] != "video" || video["width"] != float64(640) || video["height"] != float64(360) || video["duration_seconds"] != float64(12) ||
		video["poster_url"] == nil || video["thumb_url"] != nil {
		t.Fatalf("a video: %v", video)
	}
	if s, body := ann.Form("POST", files, testkit.Part{Field: "file", FileName: "v.mp4", Content: tinyMP4()},
		testkit.Part{Field: "poster", FileName: "p.txt", Content: []byte("not an image")}); s != 400 || obj(body, "error")["field"] != "poster" {
		t.Fatalf("a poster that is not an image: %d %v", s, body)
	}

	// Refusals.
	ann.Must(200, "PATCH", conv+priv+"/members/"+string(bob.ID), map[string]any{"level": "reader"})
	if s, body := bob.Form("POST", files, testkit.Part{Field: "file", FileName: "a.txt", Content: []byte("x")}); s != 403 || obj(body, "error")["code"] != "forbidden" {
		t.Fatalf("a reader uploads: %d %v", s, body)
	}
	if s, body := out.Form("POST", files, testkit.Part{Field: "file", FileName: "a.txt", Content: []byte("x")}); s != 404 || obj(body, "error")["code"] != "not_found" {
		t.Fatalf("an outsider uploads: %d %v", s, body)
	}
	if s, body := ann.Form("POST", files, testkit.Part{Field: "caption", Content: []byte("no file")}); s != 400 || obj(body, "error")["field"] != "file" {
		t.Fatalf("a form with no file: %d %v", s, body)
	}
	if s, _ := ann.Form("POST", files, testkit.Part{Field: "file", FileName: "empty", Content: nil}); s != 400 {
		t.Fatalf("an empty file: %d", s)
	}
	ann.Refused(400, model.CodeInvalidRequest, "POST", files, map[string]any{"file": "not a form"})
	setting(t, ts, store.SettingFilesMaxBytes, 1000)
	if s, body := ann.Form("POST", files, testkit.Part{Field: "file", FileName: "big.bin", Content: make([]byte, 1001)}); s != 413 || obj(body, "error")["code"] != "too_large" {
		t.Fatalf("over the limit: %d %v", s, body)
	}
	ann.Upload(priv, "fits.bin", make([]byte, 1000))
	setting(t, ts, store.SettingFilesMaxBytes, model.DefaultFileSize)
	ann.Must(200, "POST", conv+priv+"/archive", nil)
	if s, body := ann.Form("POST", files, testkit.Part{Field: "file", FileName: "a.txt", Content: []byte("x")}); s != 403 || obj(body, "error")["code"] != "archived" {
		t.Fatalf("upload to an archived channel: %d %v", s, body)
	}
	ann.Must(200, "DELETE", conv+priv+"/archive", nil)

	// An upload not yet attached can be fetched only by its uploader.
	textURL := text["url"].(string)
	if s, _, _ := ann.Get(textURL, nil); s != 200 {
		t.Fatalf("the uploader fetches an unattached upload: %d", s)
	}
	if s, _, _ := bob.Get(textURL, nil); s != 404 {
		t.Fatalf("another member fetches an unattached upload: %d", s)
	}
	// Attached on post, in the order given.
	posted := obj(ann.Must(201, "POST", conv+priv+"/messages", map[string]any{"file_ids": []any{img["id"], text["id"], svg["id"], video["id"], html["id"]}}), "message")
	if fs := arr(posted, "files"); len(fs) != 5 || fs[0].(map[string]any)["id"] != img["id"] {
		t.Fatalf("attached: %v", posted)
	}
	ann.Refused(404, model.CodeNotFound, "POST", conv+priv+"/messages", map[string]any{"text": "again", "file_ids": []any{img["id"]}})

	// Downloads: the server's own content type, nosniff, the sandbox
	// policy; attachment for anything but image, video and audio.
	check := func(url, wantType, wantDisposition string) []byte {
		t.Helper()
		s, h, body := bob.Get(url, nil)
		if s != 200 || h.Get("Content-Type") != wantType || h.Get("X-Content-Type-Options") != "nosniff" ||
			h.Get("Content-Security-Policy") != "default-src 'none'; sandbox" || !strings.HasPrefix(h.Get("Content-Disposition"), wantDisposition) {
			t.Fatalf("download of %s: %d %v", url, s, h)
		}
		return body
	}
	if body := check(img["url"].(string), "image/png", "inline"); !bytes.Equal(body, pngBytes(1000, 500)) {
		t.Fatal("the original is stored and served unchanged")
	}
	check(textURL, "text/plain; charset=utf-8", "attachment")
	check(svg["url"].(string), "application/octet-stream", "attachment")
	check(html["url"].(string), "text/plain; charset=utf-8", "attachment")
	check(video["url"].(string), "video/mp4", "inline")
	thumb := check(img["thumb_url"].(string), "image/jpeg", "inline")
	if cfg, _, err := image.DecodeConfig(bytes.NewReader(thumb)); err != nil || cfg.Width != 720 || cfg.Height != 360 {
		t.Fatalf("thumbnail: %v %v", cfg, err)
	}
	check(video["poster_url"].(string), "image/jpeg", "inline")
	// The name in the address is only a name.
	check("/api/v1/files/"+img["id"].(string)+"/anything-else.png", "image/png", "inline")
	// Range requests, for players that seek.
	s, h, part := bob.Get(video["url"].(string), map[string]string{"Range": "bytes=4-11"})
	if s != 206 || string(part) != "ftypisom" || h.Get("Content-Range") != fmt.Sprintf("bytes 4-11/%d", len(tinyMP4())) {
		t.Fatalf("a range: %d %q %v", s, part, h.Get("Content-Range"))
	}
	if s, _, _ := bob.Get(video["url"].(string), map[string]string{"Range": "bytes=99999999-"}); s != 416 {
		t.Fatalf("a range past the end: %d", s)
	}
	// Permission is checked on every download.
	for _, u := range []string{img["url"].(string), img["thumb_url"].(string), video["poster_url"].(string)} {
		if s, _, body := out.Get(u, nil); s != 404 || !strings.Contains(string(body), "not_found") {
			t.Fatalf("an outsider downloads %s: %d", u, s)
		}
		if s, _, _ := ts.Owner.Get(u, nil); s != 404 {
			t.Fatalf("the owner, outside a private channel, downloads %s: %d", u, s)
		}
		if s, _, _ := ts.Browser().Get(u, nil); s != 401 {
			t.Fatalf("nobody downloads %s: %d", u, s)
		}
	}
	if s, _, _ := bob.Get(textURL[:strings.LastIndex(textURL, "/")]+"/thumb", nil); s != 404 {
		t.Fatalf("a thumbnail of a file that has none: %d", s)
	}
	if s, _, _ := bob.Get("/api/v1/files/"+string(model.NewID[model.FileID]())+"/x", nil); s != 404 {
		t.Fatalf("a file that does not exist: %d", s)
	}
	ann.Must(204, "DELETE", conv+priv+"/members/"+string(bob.ID), nil)
	if s, _, _ := bob.Get(img["url"].(string), nil); s != 404 {
		t.Fatalf("after being removed: %d", s)
	}

	// A file an edit took off follows the rule for history; a purge
	// erases it from the file store (118).
	setting(t, ts, store.SettingQuickCorrection, 0)
	f := ann.Upload(pub, "plan.txt", []byte("the plan\n"))
	m := obj(ann.Must(201, "POST", conv+pub+"/messages", map[string]any{"text": "see file", "file_ids": []any{f["id"]}}), "message")["id"].(string)
	time.Sleep(3 * time.Millisecond)
	ann.Must(200, "PATCH", msgs+m, map[string]any{"text": "file removed", "file_ids": []any{}})
	if s, _, _ := out.Get(f["url"].(string), nil); s != 200 {
		t.Fatalf("a detached file, while history is open: %d", s)
	}
	ann.Must(200, "POST", msgs+m+"/purge", nil) // public: history hidden at once
	if s, _, _ := out.Get(f["url"].(string), nil); s != 404 {
		t.Fatalf("a detached file, while history is hidden: %d", s)
	}
	if s, _, _ := ts.Owner.Get(f["url"].(string), nil); s != 200 {
		t.Fatalf("an admin can still open hidden history's file: %d", s)
	}
	row, err := ts.Server.Store.Files().Get(context.Background(), model.FileID(f["id"].(string)))
	if err != nil {
		t.Fatal(err)
	}
	req := arr(ts.Owner.Must(200, "GET", "/api/v1/admin/purge-requests?waiting=true", nil), "purge_requests")[0].(map[string]any)["id"].(string)
	ts.Owner.Must(200, "POST", "/api/v1/admin/purge-requests/"+req+"/decide", map[string]any{"outcome": "erased"})
	if s, _, _ := ts.Owner.Get(f["url"].(string), nil); s != 404 {
		t.Fatalf("118: a purged file, for the owner: %d", s)
	}
	if _, err := ts.Server.Blobs.Get(context.Background(), row.BlobKey); err == nil {
		t.Fatal("118: the blob of a purged file is still in the file store")
	}
}

func TestEmojiAndSections(t *testing.T) {
	ts := testkit.NewServer(t)
	ann, bob := ts.Member("ann"), ts.Member("bob")
	admin := ts.Admin("adm")
	ch := ann.Channel("room", false, bob)
	stream, _ := bob.Events(nil)
	create := func(c *testkit.Client, name string, image []byte) (int, map[string]any) {
		return c.Form("POST", "/api/v1/emoji", testkit.Part{Field: "name", Content: []byte(name)}, testkit.Part{Field: "file", FileName: "e.png", Content: image})
	}
	// Admins; members only if the setting allows.
	if s, body := create(ann, "party", pngBytes(64, 64)); s != 403 {
		t.Fatalf("a member adds an emoji: %d %v", s, body)
	}
	s, body := create(admin, "party", pngBytes(64, 64))
	e := obj(body, "emoji")
	if s != 201 || e["name"] != "party" || e["created_by_id"] != string(admin.ID) || e["url"] == nil || e["file_id"] == nil {
		t.Fatalf("created: %d %v", s, body)
	}
	if ev := stream.Expect("emoji.added"); obj(ev, "emoji")["name"] != "party" {
		t.Fatalf("emoji.added: %v", ev)
	}
	for name, want := range map[string]int{"party": 409, "Bad-Name": 400, "x": 400} {
		if s, body := create(admin, name, pngBytes(8, 8)); s != want {
			t.Fatalf("create %q: %d %v", name, s, body)
		}
	}
	if s, body := create(admin, "taken", pngBytes(8, 8)); s != 201 {
		t.Fatalf("%d %v", s, body)
	}
	if s, body := create(admin, "party", pngBytes(8, 8)); s != 409 || obj(body, "error")["code"] != "name_taken" {
		t.Fatalf("a taken name: %d %v", s, body)
	}
	if s, body := create(admin, "huge", pngBytes(300, 20)); s != 413 || obj(body, "error")["code"] != "too_large" {
		t.Fatalf("over 256 pixels: %d %v", s, body)
	}
	if s, body := create(admin, "nope", []byte("<svg></svg>")); s != 400 || obj(body, "error")["field"] != "file" {
		t.Fatalf("not an image: %d %v", s, body)
	}
	// Served like any file, to any signed-in account.
	if s, h, _ := bob.Get(e["url"].(string), nil); s != 200 || h.Get("Content-Type") != "image/png" {
		t.Fatalf("download: %d", s)
	}
	if list := arr(bob.Must(200, "GET", "/api/v1/emoji", nil), "emoji"); len(list) != 2 {
		t.Fatalf("list: %v", list)
	}
	// In text it becomes an entity; as a reaction it is stored as written.
	m := obj(bob.Must(201, "POST", conv+ch+"/messages", map[string]any{"text": "well done :party: :nosuch:"}), "message")
	ents := arr(m, "entities")
	if len(ents) != 1 || ents[0].(map[string]any)["type"] != "emoji" || ents[0].(map[string]any)["emoji_id"] != e["id"] {
		t.Fatalf("entities: %v", ents)
	}
	bob.Must(200, "PUT", msgs+m["id"].(string)+"/reactions/:party:", nil)
	bob.Refused(404, model.CodeNotFound, "PUT", msgs+m["id"].(string)+"/reactions/:nosuch:", nil)

	// The admin console: list, rename, delete.
	bob.Refused(403, model.CodeForbidden, "GET", "/api/v1/admin/emoji", nil)
	if list := arr(admin.Must(200, "GET", "/api/v1/admin/emoji?limit=1", nil), "emoji"); len(list) != 1 {
		t.Fatalf("admin list: %v", list)
	}
	bob.Refused(403, model.CodeForbidden, "PATCH", "/api/v1/admin/emoji/"+e["id"].(string), map[string]any{"name": "fiesta"})
	admin.Refused(409, model.CodeNameTaken, "PATCH", "/api/v1/admin/emoji/"+e["id"].(string), map[string]any{"name": "taken"})
	admin.Refused(400, model.CodeInvalidValue, "PATCH", "/api/v1/admin/emoji/"+e["id"].(string), map[string]any{"name": "No"})
	if r := obj(admin.Must(200, "PATCH", "/api/v1/admin/emoji/"+e["id"].(string), map[string]any{"name": "fiesta"}), "emoji"); r["name"] != "fiesta" {
		t.Fatalf("renamed: %v", r)
	}
	stream.Expect("emoji.updated")
	// A member who may add may remove their own, and only their own.
	setting(t, ts, store.SettingMembersMayAddEmoji, true)
	_, body = create(ann, "mine", pngBytes(8, 8))
	mine := obj(body, "emoji")["id"].(string)
	stream.Expect("emoji.added")
	bob.Refused(403, model.CodeForbidden, "DELETE", "/api/v1/emoji/"+mine, nil)
	ann.Refused(403, model.CodeForbidden, "DELETE", "/api/v1/emoji/"+e["id"].(string), nil)
	ann.Must(204, "DELETE", "/api/v1/emoji/"+mine, nil)
	stream.Expect("emoji.removed")
	ann.Refused(404, model.CodeNotFound, "DELETE", "/api/v1/emoji/"+mine, nil)
	bob.Refused(403, model.CodeForbidden, "DELETE", "/api/v1/admin/emoji/"+e["id"].(string), nil)
	admin.Must(204, "DELETE", "/api/v1/admin/emoji/"+e["id"].(string), nil)
	admin.Refused(404, model.CodeNotFound, "DELETE", "/api/v1/admin/emoji/"+e["id"].(string), nil)
	// The file went with it; the message keeps its text.
	if s, _, _ := bob.Get(e["url"].(string), nil); s != 404 {
		t.Fatalf("the image of a deleted emoji: %d", s)
	}

	// Sections.
	sec := "/api/v1/admin/sections"
	bob.Refused(403, model.CodeForbidden, "POST", sec, map[string]any{"name": "Work"})
	admin.Refused(400, model.CodeInvalidValue, "POST", sec, map[string]any{"name": " "})
	work := obj(admin.Must(201, "POST", sec, map[string]any{"name": "Work"}), "section")
	play := obj(admin.Must(201, "POST", sec, map[string]any{"name": "Play"}), "section")
	if ev := stream.Expect("sections.changed"); len(arr(ev, "sections")) != 1 {
		t.Fatalf("sections.changed: %v", ev)
	}
	if work["position"] == play["position"] {
		t.Fatalf("a new section goes last: %v %v", work, play)
	}
	moved := obj(admin.Must(200, "PATCH", sec+"/"+play["id"].(string), map[string]any{"name": "Fun", "position": -1}), "section")
	if moved["name"] != "Fun" {
		t.Fatalf("updated: %v", moved)
	}
	list := arr(bob.Must(200, "GET", "/api/v1/sections", nil), "sections")
	if len(list) != 2 || list[0].(map[string]any)["name"] != "Fun" {
		t.Fatalf("sections in order: %v", list)
	}
	bob.Refused(403, model.CodeForbidden, "PATCH", sec+"/"+work["id"].(string), map[string]any{"name": "x"})
	admin.Refused(404, model.CodeNotFound, "PATCH", sec+"/"+string(model.NewID[model.SectionID]()), map[string]any{"name": "x"})
	// A channel's manager files it under a section; deleting the section
	// leaves the channel under none.
	ann.Must(200, "PATCH", conv+ch, map[string]any{"section_id": work["id"]})
	if got := arr(bob.Must(200, "GET", "/api/v1/channels?section_id="+work["id"].(string), nil), "conversations"); len(got) != 1 {
		t.Fatalf("browse by section: %v", got)
	}
	bob.Refused(403, model.CodeForbidden, "DELETE", sec+"/"+work["id"].(string), nil)
	admin.Must(204, "DELETE", sec+"/"+work["id"].(string), nil)
	admin.Refused(404, model.CodeNotFound, "DELETE", sec+"/"+work["id"].(string), nil)
	if c := convOf(bob.Must(200, "GET", conv+ch, nil)); c["section_id"] != nil {
		t.Fatalf("after its section was deleted: %v", c)
	}
}

// The admin view of channels: PERMISSIONS section 4.1 note 1.
func TestAdminChannels(t *testing.T) {
	ts := testkit.NewServer(t)
	ann, bob := ts.Member("ann"), ts.Member("bob")
	admin := ts.Admin("adm")
	pub := ann.Channel("pub", false)
	priv := ann.Channel("priv", true, bob)
	ann.Must(200, "PATCH", conv+priv, map[string]any{"topic": "a secret topic", "welcome": "secret welcome"})
	ann.Must(200, "PATCH", conv+pub, map[string]any{"topic": "open"})
	ann.Post(priv, "secret words")
	chat := mkchat(ann, bob)
	ac := "/api/v1/admin/channels"

	bob.Refused(403, model.CodeForbidden, "GET", ac, nil)
	byID := map[string]map[string]any{}
	for _, c := range arr(admin.Must(200, "GET", ac, nil), "channels") {
		byID[c.(map[string]any)["id"].(string)] = c.(map[string]any)
	}
	// Ad-hoc channels never appear.
	if byID[chat] != nil || byID[pub] == nil || byID[priv] == nil {
		t.Fatalf("the list: %v", byID)
	}
	// A private channel: its name, member count and managers, and
	// nothing else. Not the topic.
	p := byID[priv]
	if p["name"] != "priv" || p["private"] != true || p["member_count"] != float64(2) || len(arr(p, "manager_ids")) != 1 || p["topic"] != nil {
		t.Fatalf("a private channel in the admin view: %v", p)
	}
	for _, leak := range []string{"welcome", "last_seq", "last_message_at"} {
		if _, has := p[leak]; has {
			t.Fatalf("the admin view carries %s", leak)
		}
	}
	if byID[pub]["topic"] != "open" {
		t.Fatalf("a public channel's topic: %v", byID[pub])
	}
	if got := arr(admin.Must(200, "GET", ac+"?private=true&q=PRI", nil), "channels"); len(got) != 1 {
		t.Fatalf("filters: %v", got)
	}
	got := admin.Must(200, "GET", ac+"/"+priv, nil)
	if obj(got, "channel")["name"] != "priv" || len(arr(got, "members")) != 2 {
		t.Fatalf("get: %v", got)
	}
	admin.Refused(404, model.CodeNotFound, "GET", ac+"/"+chat, nil)
	bob.Refused(403, model.CodeForbidden, "GET", ac+"/"+priv, nil)
	// Every ordinary operation still answers not_found (decision P22).
	for _, path := range []string{conv + priv, conv + priv + "/messages", conv + priv + "/members", conv + priv + "/pins"} {
		admin.Refused(404, model.CodeNotFound, "GET", path, nil)
	}
	admin.Refused(404, model.CodeNotFound, "POST", conv+priv+"/members", map[string]any{"account_ids": []string{string(admin.ID)}})

	// Appoint a manager: an owner's, never an admin's (ruling R99).
	stream, _ := ann.Events(nil)
	admin.Refused(403, model.CodeForbidden, "PUT", ac+"/"+priv+"/managers/"+string(bob.ID), nil)
	bob.Refused(403, model.CodeForbidden, "PUT", ac+"/"+priv+"/managers/"+string(bob.ID), nil)
	if m := obj(ts.Owner.Must(200, "PUT", ac+"/"+priv+"/managers/"+string(bob.ID), nil), "member"); m["level"] != "manager" {
		t.Fatalf("appointed: %v", m)
	}
	// The channel can see it.
	if e := stream.Expect("member.level_changed"); e["by_id"] != string(ts.Owner.ID) {
		t.Fatalf("member.level_changed: %v", e)
	}
	if !auditedFor(t, ts, "channel.manager_appointed", string(bob.ID)) {
		t.Error("appointing a manager is written to the audit log")
	}
	// Retention and section.
	upd := obj(admin.Must(200, "PATCH", ac+"/"+priv, map[string]any{"retention_seconds": 30 * 86400}), "channel")
	if upd["retention_seconds"] != float64(30*86400) {
		t.Fatalf("retention: %v", upd)
	}
	if upd = obj(admin.Must(200, "PATCH", ac+"/"+priv, map[string]any{"retention_seconds": 0}), "channel"); upd["retention_seconds"] != nil {
		t.Fatalf("cleared: %v", upd)
	}
	bob.Refused(403, model.CodeForbidden, "PATCH", ac+"/"+priv, map[string]any{"retention_seconds": 60})
	admin.Refused(404, model.CodeNotFound, "PATCH", ac+"/"+chat, map[string]any{"retention_seconds": 60})
	// The console has no archive and no lock: those are the channel's
	// managers' (ruling R99).
	admin.Refused(404, model.CodeNotFound, "POST", ac+"/"+priv+"/archive", nil)
	ann.Must(200, "POST", conv+priv+"/archive", nil)
	stream.Expect("conversation.updated")
	stream.Expect("conversation.updated")
	stream.Expect("conversation.archived")
	ann.Must(200, "DELETE", conv+priv+"/archive", nil)

	// The audit log and the overview.
	bob.Refused(403, model.CodeForbidden, "GET", "/api/v1/admin/audit", nil)
	entries := arr(admin.Must(200, "GET", "/api/v1/admin/audit?action=channel.&actor_id="+string(admin.ID), nil), "entries")
	if len(entries) < 2 {
		t.Fatalf("audit by prefix and actor: %v", entries)
	}
	for _, e := range entries {
		if !strings.HasPrefix(e.(map[string]any)["action"].(string), "channel.") {
			t.Fatalf("an entry that does not match: %v", e)
		}
	}
	if one := arr(admin.Must(200, "GET", "/api/v1/admin/audit?action=channel.archived&target_id="+priv, nil), "entries"); len(one) != 1 {
		t.Fatalf("audit by action and target: %v", one)
	}
	future := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	if none := arr(admin.Must(200, "GET", "/api/v1/admin/audit?since="+future, nil), "entries"); len(none) != 0 {
		t.Fatalf("audit since the future: %v", none)
	}
	page := admin.Must(200, "GET", "/api/v1/admin/audit?limit=2", nil)
	if len(arr(page, "entries")) != 2 || page["next_cursor"] == nil {
		t.Fatalf("audit paging: %v", page)
	}
	bob.Refused(403, model.CodeForbidden, "GET", "/api/v1/admin/overview", nil)
	ov := obj(admin.Must(200, "GET", "/api/v1/admin/overview", nil), "overview")
	if ov["version"] == nil || obj(ov, "files")["store"] != "disk" || obj(ov, "accounts")["people"] != float64(4) ||
		obj(ov, "waiting")["reports"] != float64(0) || obj(ov, "database")["kind"] == nil || ov["uptime_seconds"] == nil {
		t.Fatalf("overview: %v", ov)
	}
	// Testing object storage: an answer either way, never a failure.
	bob.Refused(403, model.CodeForbidden, "POST", "/api/v1/admin/settings/test-storage", nil)
	res := admin.Must(200, "POST", "/api/v1/admin/settings/test-storage", map[string]any{
		"files.s3":        map[string]any{"endpoint": "http://127.0.0.1:1", "region": "x", "bucket": "b", "path_style": true, "access_key_id": "k"},
		"files.s3_secret": "s"})
	if res["ok"] != false || res["error"] == nil {
		t.Fatalf("a store that cannot be reached: %v", res)
	}
	if res := admin.Must(200, "POST", "/api/v1/admin/settings/test-storage", nil); res["ok"] != false {
		t.Fatalf("with nothing stored and nothing given: %v", res)
	}
}

// previewWorld makes link previews reach a local listener as if it were a
// site on the public internet, with the guard itself left real.
func previewWorld(t *testing.T, ts *testkit.TestServer, h http.Handler) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	dialer := &preview.Dialer{
		Resolve: func(ctx context.Context, host string) ([]netip.Addr, error) {
			if host == "site.example" {
				return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
			}
			if host == "inside.example" {
				return []netip.Addr{netip.MustParseAddr("10.0.0.7")}, nil
			}
			return nil, fmt.Errorf("no such host")
		},
		Connect: func(ctx context.Context, network, address string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, srv.Listener.Addr().String())
		},
	}
	ts.Server.Chat.SetPreviewer(preview.New(preview.Deps{Store: ts.Server.Store, Blobs: ts.Server.Blobs, Dialer: dialer, Own: "chat.example"}))
}

func TestLinkPreviews(t *testing.T) {
	ts := testkit.NewServer(t)
	ann, bob := ts.Member("ann"), ts.Member("bob")
	ch := ann.Channel("room", true, bob)
	hits := 0
	previewWorld(t, ts, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		switch r.URL.Path {
		case "/img.png":
			w.Write(pngBytes(100, 50))
		default:
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprintf(w, `<title>Page %s</title><meta property="og:description" content="About it"><meta property="og:image" content="/img.png">`, r.URL.Path)
		}
	}))
	stream, _ := bob.Events(nil)

	// Off until an admin turns previews on.
	ann.Post(ch, "see http://site.example/off")
	stream.Expect("message.created")
	stream.ExpectNone(300 * time.Millisecond)
	if hits != 0 {
		t.Fatal("a page was fetched while previews are off")
	}
	setting(t, ts, store.SettingPreviewsEnabled, true)
	stream, _ = bob.Events(nil) // waiting for nothing used the first one up

	// Previews arrive after the message, as preview.added: cards with
	// preview_url, each with its own copy of the image.
	m := ann.Post(ch, "see http://site.example/a and [this](http://site.example/b), http://inside.example/x, http://site.example/c http://site.example/d")["id"].(string)
	stream.Expect("message.created")
	e := stream.Expect("preview.added")
	msg := obj(e, "message")
	cards := arr(msg, "cards")
	// The first three links are looked up; the one that resolves to a
	// private address gives no card.
	if len(cards) != 2 || msg["edited_at"] != nil || msg["id"] != m {
		t.Fatalf("preview.added: %v", e)
	}
	first := cards[0].(map[string]any)
	if first["preview_url"] != "http://site.example/a" || first["title"] != "Page /a" || first["text"] != "About it" || first["color"] != "grey" ||
		first["footer"] != "site.example" || first["image_file_id"] == nil || cards[1].(map[string]any)["preview_url"] != "http://site.example/b" {
		t.Fatalf("cards: %v", cards)
	}
	if len(arr(msg, "files")) != 0 {
		t.Fatalf("a card's image is not one of the message's files: %v", msg["files"])
	}
	// The image is a file on the message, served like any other, to
	// those who can read the conversation.
	imgURL := "/api/v1/files/" + first["image_file_id"].(string) + "/preview"
	if s, h, _ := bob.Get(imgURL, nil); s != 200 || !strings.HasPrefix(h.Get("Content-Type"), "image/") {
		t.Fatalf("the card image: %d", s)
	}
	if s, _, _ := ts.Member("out").Get(imgURL, nil); s != 404 {
		t.Fatalf("the card image, for an outsider: %d", s)
	}
	if h := arr(ann.Must(200, "GET", msgs+m+"/history", nil), "versions"); len(h) != 0 {
		t.Fatalf("adding previews is not an edit: %v", h)
	}

	// Removing one: the author or a manager, by position.
	bob.Refused(403, model.CodeForbidden, "DELETE", msgs+m+"/previews/0", nil)
	ann.Refused(404, model.CodeNotFound, "DELETE", msgs+m+"/previews/7", nil)
	ann.Refused(404, model.CodeNotFound, "DELETE", msgs+m+"/previews/x", nil)
	left := arr(obj(ann.Must(200, "DELETE", msgs+m+"/previews/0", nil), "message"), "cards")
	if len(left) != 1 || left[0].(map[string]any)["preview_url"] != "http://site.example/b" {
		t.Fatalf("after removing one: %v", left)
	}
	if ev := stream.Expect("preview.removed"); len(arr(obj(ev, "message"), "cards")) != 1 {
		t.Fatalf("preview.removed: %v", ev)
	}
	if s, _, _ := bob.Get(imgURL, nil); s != 404 {
		t.Fatalf("the image of a removed preview: %d", s)
	}

	// On an edit only links that were not in the earlier text are looked
	// up, so the removed preview does not come back; a card whose link
	// has gone is dropped.
	setting(t, ts, store.SettingQuickCorrection, 0)
	time.Sleep(3 * time.Millisecond)
	edited := obj(ann.Must(200, "PATCH", msgs+m, map[string]any{"text": "now http://site.example/a and http://site.example/new"}), "message")
	if c := arr(edited, "cards"); len(c) != 0 {
		t.Fatalf("the card of a link that has gone: %v", c)
	}
	stream.Expect("message.edited")
	added := arr(obj(stream.Expect("preview.added"), "message"), "cards")
	if len(added) != 1 || added[0].(map[string]any)["preview_url"] != "http://site.example/new" {
		t.Fatalf("after the edit: %v", added)
	}
}

// A channel is linked by its identifier (ruling R52): the name can be
// searched for, and the identifier cannot.
func TestSearchLinkedChannel(t *testing.T) {
	ts := testkit.NewServer(t)
	ann, bob := ts.Member("ann"), ts.Member("bob")
	ch := ann.Channel("room", false, bob)
	target := ann.Channel("Zeppelin Hangar", false)
	hidden := ann.Channel("Submarine Dock", true)
	long := strings.Repeat("padding words before the point ", 12)
	m := obj(bob.Must(201, "POST", conv+ch+"/messages", map[string]any{
		"text": long + "meet in <#" + target + "> or <#" + hidden + "> or #room, not <#" + string(model.NewID[model.ConversationID]()) + ">"}), "message")
	// Only the link to a channel the author can see becomes an entity,
	// covering the whole <#…>. `#room` typed as characters is plain text.
	ents := arr(m, "entities")
	if len(ents) != 1 || ents[0].(map[string]any)["type"] != "conversation" || ents[0].(map[string]any)["conversation_id"] != target ||
		ents[0].(map[string]any)["len"] != float64(39) {
		t.Fatalf("entities: %v", ents)
	}
	if !strings.Contains(m["text"].(string), "<#"+hidden+">") {
		t.Fatalf("a link that does not resolve stays as written: %v", m["text"])
	}
	find := func(q string) []any {
		return arr(bob.Must(200, "GET", "/api/v1/search?q="+q, nil), "results")
	}
	got := find("zeppelin")
	if len(got) != 1 || obj(got[0].(map[string]any), "message")["id"] != m["id"] {
		t.Fatalf("searching for the linked channel's name: %v", got)
	}
	// The snippet is cut from what the index holds, around the match.
	snippet, _ := got[0].(map[string]any)["snippet"].(string)
	if !strings.Contains(snippet, "meet in Zeppelin Hangar") || strings.Contains(snippet, target) || !strings.HasPrefix(snippet, "…") || len([]rune(snippet)) > 170 {
		t.Fatalf("snippet: %q", snippet)
	}
	if got := find(target); len(got) != 0 {
		t.Fatalf("searching for the identifier found the message: %v", got)
	}
	if got := find(target[len(target)-12:]); len(got) != 0 {
		t.Fatalf("searching for a piece of the identifier found the message: %v", got)
	}
	// The name of a channel the author could not see is not in the index.
	if got := find("submarine"); len(got) != 0 {
		t.Fatalf("the name of a private channel was indexed: %v", got)
	}
	if got := find("unknown"); len(got) != 1 {
		t.Fatalf("a link that did not resolve is indexed as unknown channel: %v", got)
	}
}
