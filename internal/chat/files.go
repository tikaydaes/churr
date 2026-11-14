package chat

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/tikaydaes/churr/internal/blob"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/perm"
	"github.com/tikaydaes/churr/internal/store"
)

// SetTmpDir says where uploads are held while they are looked at: the tmp
// folder of the data directory, which is on the same disk as the files.
func (s *Service) SetTmpDir(dir string) { s.tmpDir = dir }

// Limits on the parts of an upload that are not the file itself.
const (
	maxCaption    = 1000
	maxFileName   = 255
	maxPosterSize = 5 << 20
	// maxTextFile is the size under which UTF-8 text is kind `text`
	// (docs/MESSAGE-FORMAT.md section 5).
	maxTextFile = 1 << 20
)

// thumbnails lets only a few images be decoded at once: each takes up to
// eight bytes a pixel while it is.
var thumbnails = make(chan struct{}, 2)

// Upload is a file on its way in.
type Upload struct {
	// Name is the file name the client gave. Only its last path element is
	// kept, and nothing about the file is decided from it.
	Name    string
	Caption string
	// Content is the file. It is read once, to its end.
	Content io.Reader
	// Poster is one image a client may send with a video, to show before
	// it plays; the server makes none (ruling R17).
	Poster io.Reader
	// Rest, if set, is called once Content has been read to its end, for
	// a caption and a poster that follow the file in the request. What it
	// returns replaces Caption and Poster where it is not empty.
	Rest func() (caption string, poster io.Reader, err error)
}

// cleanFileName keeps the last path element of a name, without control
// characters, at a sensible length.
func cleanFileName(name string) string {
	name = path.Base(strings.ReplaceAll(name, `\`, "/"))
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == '/' {
			return -1
		}
		return r
	}, name)
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." {
		return "file"
	}
	for utf8.RuneCountInString(name) > maxFileName {
		_, size := utf8.DecodeLastRuneInString(name)
		name = name[:len(name)-size]
	}
	return name
}

// UploadFile stores an upload to a conversation, to be attached later by
// naming it in `file_ids`. Kind and media type come from the content, never
// from the name or from what the uploader claimed.
func (s *Service) UploadFile(ctx context.Context, actor model.Account, conv model.ConversationID, up Upload) (model.File, error) {
	st, err := s.perm.Can(ctx, actor, perm.Upload, conv)
	if err != nil {
		return model.File{}, hide(err)
	}
	blobs := s.fileStore()
	if blobs == nil {
		return model.File{}, model.ErrUnavailable
	}
	limit, err := store.SettingIntOr(ctx, s.store, store.SettingFilesMaxBytes, model.DefaultFileSize)
	if err != nil {
		return model.File{}, err
	}
	// The upload is held in a temporary file while it is looked at: what
	// it is and how large can only be told from the whole of it, and it
	// may be far more than fits in memory.
	tmp, err := os.CreateTemp(s.tmpDir, "upload-*")
	if err != nil {
		return model.File{}, err
	}
	defer func() {
		tmp.Close()
		os.Remove(tmp.Name())
	}()
	size, err := io.Copy(tmp, blob.LimitReader(up.Content, limit))
	if err != nil {
		if errors.Is(err, model.ErrTooLarge) {
			return model.File{}, model.ErrTooLarge.With("That file is larger than this server accepts.")
		}
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			return model.File{}, model.ErrTooLarge.With("That file is larger than this server accepts.")
		}
		return model.File{}, model.BadRequest("The upload could not be read.")
	}
	if up.Rest != nil {
		caption, poster, err := up.Rest()
		if err != nil {
			return model.File{}, err
		}
		if caption != "" {
			up.Caption = caption
		}
		if poster != nil {
			up.Poster = poster
		}
	}
	if utf8.RuneCountInString(up.Caption) > maxCaption {
		return model.File{}, model.Invalid("caption", "A caption is at most 1,000 characters.")
	}
	if size == 0 {
		return model.File{}, model.Invalid("file", "The file is empty.")
	}
	f := model.File{ConversationID: conv, UploaderID: actor.ID, Name: cleanFileName(up.Name), Caption: up.Caption, Size: size,
		MediaType: blob.TypeUnknown, Kind: model.FileOther, CreatedAt: s.now()}
	var thumb, poster *blob.Thumb
	if st.Conversation.Sealed() {
		// An encrypted blob: the real name, type and kind are inside the
		// ciphertext, and the server makes nothing of it.
		f.Name, f.Caption = "", ""
	} else {
		head := make([]byte, blob.SniffLen)
		n, _ := tmp.ReadAt(head, 0)
		f.MediaType, f.Kind = blob.Sniff(head[:n])
		switch f.Kind {
		case model.FileText:
			// Text only when it is small and the whole of it is text.
			f.MediaType, f.Kind = blob.TypeUnknown, model.FileOther
			if size < maxTextFile {
				if _, err := tmp.Seek(0, io.SeekStart); err != nil {
					return model.File{}, err
				}
				if all, err := io.ReadAll(tmp); err == nil && blob.IsText(all) {
					if text, err := blob.PreviewText(bytes.NewReader(all)); err == nil {
						f.MediaType, f.Kind, f.PreviewText = blob.TypeText, model.FileText, text
					}
				}
			}
		case model.FileImage, model.FileVideo, model.FileAudio:
			f.Width, f.Height, f.DurationSeconds = blob.Probe(tmp)
		}
		if f.Kind == model.FileImage {
			if _, err := tmp.Seek(0, io.SeekStart); err != nil {
				return model.File{}, err
			}
			thumbnails <- struct{}{}
			thumb, err = blob.MakeThumbnail(tmp)
			<-thumbnails
			if err != nil {
				// An image that cannot be decoded, or is too large to
				// decode safely, is stored and served without one.
				thumb = nil
			}
		}
		if f.Kind == model.FileVideo && up.Poster != nil {
			data, err := io.ReadAll(io.LimitReader(up.Poster, maxPosterSize+1))
			if err != nil || len(data) > maxPosterSize {
				return model.File{}, model.Invalid("poster", "A poster is one PNG, JPEG or WebP image of at most 5 MB.")
			}
			if len(data) > 0 {
				if t, _ := blob.Sniff(data[:min(len(data), blob.SniffLen)]); t != blob.TypePNG && t != blob.TypeJPEG && t != blob.TypeWebP {
					return model.File{}, model.Invalid("poster", "A poster is one PNG, JPEG or WebP image of at most 5 MB.")
				}
				// Re-encoded like a thumbnail, so it carries no location
				// or camera details either.
				thumbnails <- struct{}{}
				poster, err = blob.MakeThumbnail(bytes.NewReader(data))
				<-thumbnails
				if err != nil {
					return model.File{}, model.Invalid("poster", "That poster image could not be read.")
				}
			}
		}
	}

	var keys []string
	put := func(r io.Reader) (string, []byte, error) {
		key, _, sum, err := blobs.Put(ctx, r)
		if err == nil {
			keys = append(keys, key)
		}
		return key, sum, err
	}
	fail := func(err error) (model.File, error) {
		s.dropBlobs(ctx, keys)
		if errors.Is(err, model.ErrUnavailable) {
			return model.File{}, model.ErrUnavailable.With("The file store cannot be reached. Try again shortly.")
		}
		return model.File{}, err
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return model.File{}, err
	}
	if f.BlobKey, f.SHA256, err = put(tmp); err != nil {
		return fail(err)
	}
	if thumb != nil {
		if f.ThumbKey, _, err = put(bytes.NewReader(thumb.Data)); err != nil {
			return fail(err)
		}
	}
	if poster != nil {
		if f.PosterKey, _, err = put(bytes.NewReader(poster.Data)); err != nil {
			return fail(err)
		}
	}
	if err := s.store.Files().Create(ctx, &f); err != nil {
		return fail(err)
	}
	return f, nil
}

// The parts of a file that can be downloaded.
const (
	FileContent = "file"
	FileThumb   = "thumb"
	FilePoster  = "poster"
)

// Download is a file somebody was found able to fetch.
type Download struct {
	File model.File
	// Body is the content; the caller closes it. It can also seek when the
	// blob is on disk, which is what serving ranges needs.
	Body io.ReadCloser
	// MediaType is what to send as the content type: the server's own
	// choice from the content, never the uploader's.
	MediaType string
	// Inline is true for the kinds a browser may draw: image, video and
	// audio. Everything else is sent as an attachment.
	Inline bool
	// Size is the length of the content, or -1 when it is not known.
	Size int64
}

// OpenFile checks, on every download, that the actor may fetch a file, and
// opens the part asked for. A file the actor may not fetch, one that has
// not arrived, and a part the file does not have are all not_found.
func (s *Service) OpenFile(ctx context.Context, actor model.Account, id model.FileID, part string) (Download, error) {
	f, err := s.store.Files().Get(ctx, id)
	if err != nil {
		return Download{}, hide(err)
	}
	st, err := s.perm.Load(ctx, actor, f.ConversationID)
	if err != nil {
		return Download{}, hide(err)
	}
	var msg *model.Message
	if f.MessageID != "" {
		if m, err := s.store.Messages().Get(ctx, f.MessageID); err == nil {
			msg = &m
		}
	}
	if err := perm.CheckFile(st, f, msg); err != nil {
		return Download{}, err
	}
	blobs := s.fileStore()
	if blobs == nil {
		return Download{}, model.ErrUnavailable
	}
	d := Download{File: f, Size: -1}
	key := ""
	switch part {
	case FileContent:
		key, d.MediaType, d.Size = f.BlobKey, f.MediaType, f.Size
		d.Inline = f.Kind == model.FileImage || f.Kind == model.FileVideo || f.Kind == model.FileAudio
	case FileThumb:
		key, d.Inline = f.ThumbKey, true
	case FilePoster:
		key, d.Inline = f.PosterKey, true
	}
	if key == "" || (f.State != "" && f.State != "ready") {
		return Download{}, model.ErrNotFound
	}
	body, err := blobs.Get(ctx, key)
	if err != nil {
		if errors.Is(err, model.ErrUnavailable) {
			return Download{}, model.ErrUnavailable.With("The file store cannot be reached. Try again shortly.")
		}
		return Download{}, hide(err)
	}
	d.Body = body
	if part != FileContent {
		// Thumbnails and posters are images the server made itself: a
		// JPEG, or a PNG when there is transparency. The type is read
		// from the bytes all the same.
		head := make([]byte, blob.SniffLen)
		n, _ := io.ReadFull(body, head)
		mediaType, kind := blob.Sniff(head[:n])
		if kind != model.FileImage {
			body.Close()
			return Download{}, model.ErrNotFound
		}
		d.MediaType = mediaType
		if seeker, ok := body.(io.Seeker); ok {
			if _, err := seeker.Seek(0, io.SeekStart); err != nil {
				body.Close()
				return Download{}, err
			}
		} else {
			d.Body = struct {
				io.Reader
				io.Closer
			}{io.MultiReader(bytes.NewReader(head[:n]), body), body}
		}
	}
	return d, nil
}
