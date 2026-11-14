package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/tikaydaes/churr/internal/chat"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

// Files and custom emoji: uploads, downloads, and the emoji list.

// Storage is what the API needs to know about where files are kept; the
// server implements it.
type Storage interface {
	// TestStorage writes, reads and deletes one small object with the
	// object storage settings given, or the stored ones where a part is
	// not given.
	TestStorage(ctx context.Context, settings json.RawMessage, secret *string) error
	// FilesStore says where new files go: "disk" or "s3".
	FilesStore(ctx context.Context) string
}

// largeBody names the operations whose body is a file and not JSON, so the
// 1 MB cap on bodies does not apply; each applies its own limit.
var largeBody = map[string]bool{"uploadFile": true, "createEmoji": true}

func init() {
	register(func(a *API) {
		a.handle("uploadFile", a.uploadFile)
		a.handle("downloadFile", a.downloadFile)
		a.handle("downloadThumbnail", a.downloadThumbnail)
		a.handle("downloadPoster", a.downloadPoster)
		a.handle("listEmoji", a.listEmoji)
		a.handle("createEmoji", a.createEmoji)
		a.handle("deleteEmoji", a.deleteEmoji)
	})
}

// The room a multipart upload is given beyond the file itself: the
// boundaries, a caption and a poster image.
const uploadSlack = 8 << 20

// formParts opens a multipart body.
func formParts(r *Request) (*multipart.Reader, error) {
	mr, err := r.R.MultipartReader()
	if err != nil {
		return nil, model.BadRequest("This request is a multipart form with the file in it.")
	}
	return mr, nil
}

// smallField reads a text field of a form.
func smallField(p *multipart.Part, field string, max int64) (string, error) {
	b, err := io.ReadAll(io.LimitReader(p, max+1))
	if err != nil {
		return "", model.BadRequest("The upload could not be read.")
	}
	if int64(len(b)) > max {
		return "", model.Invalid(field, "That is too long.")
	}
	return string(b), nil
}

func (a *API) uploadFile(r *Request) error {
	id, err := PathID[model.ConversationID](r, "id")
	if err != nil {
		return err
	}
	limit, err := store.SettingIntOr(r.Ctx(), a.Store, store.SettingFilesMaxBytes, model.DefaultFileSize)
	if err != nil {
		return err
	}
	r.R.Body = http.MaxBytesReader(r.W, r.R.Body, limit+uploadSlack)
	mr, err := formParts(r)
	if err != nil {
		return err
	}
	// The parts are read in the order they arrive and the file is never
	// held in memory: what comes before it is kept, the file is streamed
	// to the service, and what comes after is read once the file has been.
	var caption string
	var poster []byte
	keep := func(p *multipart.Part) error {
		switch p.FormName() {
		case "caption":
			caption, err = smallField(p, "caption", 8<<10)
			return err
		case "poster":
			b, err := io.ReadAll(io.LimitReader(p, 5<<20+1))
			if err != nil {
				return model.BadRequest("The upload could not be read.")
			}
			poster = b
			return nil
		case "file":
			return model.BadRequest("A request carries one file.")
		}
		return model.BadRequest("This form has the fields file, caption and poster.")
	}
	var file *multipart.Part
	for file == nil {
		p, err := mr.NextPart()
		if err == io.EOF {
			return model.Invalid("file", "The form has no file in it.")
		}
		if err != nil {
			return model.BadRequest("The upload could not be read.")
		}
		if p.FormName() == "file" {
			file = p
			break
		}
		if err := keep(p); err != nil {
			return err
		}
	}
	up := chat.Upload{Name: file.FileName(), Content: file}
	up.Rest = func() (string, io.Reader, error) {
		for {
			p, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				return "", nil, tooLargeOr(err, model.BadRequest("The upload could not be read."))
			}
			if err := keep(p); err != nil {
				return "", nil, err
			}
		}
		if poster == nil {
			return caption, nil, nil
		}
		return caption, strings.NewReader(string(poster)), nil
	}
	f, err := a.Chat.UploadFile(r.Ctx(), r.Actor(), id, up)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"file": f})
}

// tooLargeOr turns the body cap being hit into too_large.
func tooLargeOr(err error, otherwise error) error {
	if strings.Contains(err.Error(), "request body too large") {
		return model.ErrTooLarge.With("That file is larger than this server accepts.")
	}
	return otherwise
}

// sandbox is sent with every download: whatever the file is, a browser
// that is shown it may load nothing and run nothing.
const sandbox = "default-src 'none'; sandbox"

// serveFile checks permission, as on every download, and sends one part of
// a file with a content type the server chose from the content.
func (a *API) serveFile(r *Request, part string) error {
	id, err := PathID[model.FileID](r, "id")
	if err != nil {
		return err
	}
	d, err := a.Chat.OpenFile(r.Ctx(), r.Actor(), id, part)
	if err != nil {
		return err
	}
	defer d.Body.Close()
	h := r.W.Header()
	h.Set("Content-Type", d.MediaType)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", sandbox)
	// Checked again each time: nothing between here and the reader may
	// keep a copy to hand to someone else.
	h.Set("Cache-Control", "private, no-cache")
	name := d.File.Name
	if part != chat.FileContent || name == "" {
		name = part
	}
	// Only image, video and audio are shown in place; anything else is
	// saved, never opened by the browser.
	disposition := "attachment"
	if d.Inline {
		disposition = "inline"
	}
	h.Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": name}))
	if h.Get("Content-Disposition") == "" {
		h.Set("Content-Disposition", disposition+"; filename*=UTF-8''"+url.PathEscape(name))
	}
	if seeker, ok := d.Body.(io.ReadSeeker); ok {
		// Ranges, for audio and video players that seek.
		http.ServeContent(r.W, r.R, "", d.File.CreatedAt.Time(), seeker)
		return nil
	}
	return serveStream(r, d)
}

// serveStream sends a file whose content cannot seek (it is in object
// storage): whole, or one range of it by reading past what comes before.
func serveStream(r *Request, d chat.Download) error {
	h := r.W.Header()
	start, length, ranged := int64(0), d.Size, false
	if spec := r.R.Header.Get("Range"); spec != "" && d.Size > 0 {
		if s, e, ok := oneRange(spec, d.Size); ok {
			start, length, ranged = s, e-s+1, true
		} else {
			h.Set("Content-Range", fmt.Sprintf("bytes */%d", d.Size))
			h.Del("Content-Disposition")
			r.W.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			return nil
		}
	}
	if d.Size >= 0 {
		h.Set("Accept-Ranges", "bytes")
		h.Set("Content-Length", strconv.FormatInt(length, 10))
	}
	if ranged {
		if _, err := io.CopyN(io.Discard, d.Body, start); err != nil {
			return err
		}
		h.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, start+length-1, d.Size))
		r.W.WriteHeader(http.StatusPartialContent)
		io.CopyN(r.W, d.Body, length)
		return nil
	}
	r.W.WriteHeader(http.StatusOK)
	io.Copy(r.W, d.Body)
	return nil
}

// oneRange reads a Range header that asks for one range of bytes: `a-b`,
// `a-` or `-n`. Several ranges at once are not served; the whole file is.
func oneRange(spec string, size int64) (start, end int64, ok bool) {
	spec, found := strings.CutPrefix(strings.TrimSpace(spec), "bytes=")
	if !found || strings.Contains(spec, ",") {
		return 0, 0, false
	}
	from, to, found := strings.Cut(spec, "-")
	if !found {
		return 0, 0, false
	}
	if from == "" {
		n, err := strconv.ParseInt(to, 10, 64)
		if err != nil || n <= 0 {
			return 0, 0, false
		}
		if n > size {
			n = size
		}
		return size - n, size - 1, true
	}
	start, err := strconv.ParseInt(from, 10, 64)
	if err != nil || start < 0 || start >= size {
		return 0, 0, false
	}
	end = size - 1
	if to != "" {
		if end, err = strconv.ParseInt(to, 10, 64); err != nil || end < start {
			return 0, 0, false
		}
		if end >= size {
			end = size - 1
		}
	}
	return start, end, true
}

func (a *API) downloadFile(r *Request) error      { return a.serveFile(r, chat.FileContent) }
func (a *API) downloadThumbnail(r *Request) error { return a.serveFile(r, chat.FileThumb) }
func (a *API) downloadPoster(r *Request) error    { return a.serveFile(r, chat.FilePoster) }

func (a *API) listEmoji(r *Request) error {
	emoji, err := a.Chat.Emoji(r.Ctx(), r.Actor())
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"emoji": orEmpty(emoji)})
}

func (a *API) createEmoji(r *Request) error {
	r.R.Body = http.MaxBytesReader(r.W, r.R.Body, model.MaxEmojiBytes+uploadSlack)
	mr, err := formParts(r)
	if err != nil {
		return err
	}
	var name string
	var image []byte
	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return tooLargeOr(err, model.BadRequest("The upload could not be read."))
		}
		switch p.FormName() {
		case "name":
			if name, err = smallField(p, "name", 256); err != nil {
				return err
			}
		case "file":
			if image, err = io.ReadAll(io.LimitReader(p, model.MaxEmojiBytes+1)); err != nil {
				return tooLargeOr(err, model.BadRequest("The upload could not be read."))
			}
		default:
			return model.BadRequest("This form has the fields name and file.")
		}
	}
	if image == nil {
		return model.Invalid("file", "The form has no file in it.")
	}
	e, err := a.Chat.CreateEmoji(r.Ctx(), r.Actor(), name, strings.NewReader(string(image)))
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"emoji": e})
}

func (a *API) deleteEmoji(r *Request) error {
	id, err := PathID[model.EmojiID](r, "id")
	if err != nil {
		return err
	}
	if err := a.Chat.DeleteEmoji(r.Ctx(), r.Actor(), id); err != nil {
		return err
	}
	return r.NoContent()
}
