package importer

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tikaydaes/churr/internal/blob"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/preview"
	"github.com/tikaydaes/churr/internal/store"
)

// An export does not hold files. It holds, for each, an address at Slack
// that works for a while after the export was made. The importer fetches
// each one while it still works.
//
// The addresses come from a file someone handed the operator, so they are
// treated as link previews treat an address a member posted: only http and
// https, only ports 80 and 443, never a private, loopback or link-local
// address, a few redirects at most, each checked again, and a limit on size
// and time.

const (
	// fetchTimeout is how long one file may take.
	fetchTimeout = 5 * time.Minute
	// maxRedirects is how many redirects a fetch follows.
	maxRedirects = 5
	// maxAvatarBytes is the largest profile picture kept.
	maxAvatarBytes = 1 << 20
)

// Guarded returns the HTTP client the importer fetches files with: one that
// connects only to public addresses (the guarded dialer of the preview
// package), uses no proxy from the environment, keeps no cookies and
// follows few redirects.
func Guarded() *http.Client {
	dialer := preview.SafeDialer()
	return &http.Client{
		Timeout: fetchTimeout,
		Transport: &http.Transport{
			// A proxy would do the connecting, and the guard would
			// check the proxy.
			Proxy:                  nil,
			DialContext:            dialer.DialContext,
			TLSHandshakeTimeout:    10 * time.Second,
			ResponseHeaderTimeout:  30 * time.Second,
			MaxResponseHeaderBytes: 64 << 10,
			MaxIdleConnsPerHost:    2,
			IdleConnTimeout:        30 * time.Second,
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > maxRedirects {
				return fmt.Errorf("more than %d redirects", maxRedirects)
			}
			if err := checkAddress(req.URL); err != nil {
				return err
			}
			req.Header.Del("Referer")
			return nil
		},
	}
}

// checkAddress refuses anything that is not a plain web address.
func checkAddress(u *url.URL) error {
	if u.Scheme != "https" && u.Scheme != "http" {
		return fmt.Errorf("not a web address: %s", u.Scheme)
	}
	if u.Hostname() == "" || u.User != nil {
		return errors.New("not a plain web address")
	}
	return nil
}

// fetched is a downloaded file, held in a temporary file until it is put in
// the file store.
type fetched struct {
	path      string
	size      int64
	mediaType string
	kind      string
	width     int
	height    int
	duration  int
	// webPage is true when the content starts as an HTML page does.
	webPage bool
	// previewText is the start of a text file.
	previewText string
	// row is the file row this becomes, filled in by the caller.
	row model.File
}

// discard removes the temporary file.
func (f fetched) discard() {
	if f.path != "" {
		os.Remove(f.path)
	}
}

// store puts the content in the file store, with a thumbnail if it is an
// image, and writes the keys, size and hash into row.
func (f fetched) store(ctx context.Context, blobs blob.Blobs, row *model.File) error {
	src, err := os.Open(f.path)
	if err != nil {
		return err
	}
	defer src.Close()
	if row.BlobKey, row.Size, row.SHA256, err = blobs.Put(ctx, src); err != nil {
		return err
	}
	if f.kind == model.FileImage {
		// An image that cannot be decoded simply has no thumbnail.
		if _, err := src.Seek(0, io.SeekStart); err == nil {
			if thumb, err := blob.MakeThumbnail(src); err == nil {
				if key, _, _, err := blobs.Put(ctx, bytes.NewReader(thumb.Data)); err == nil {
					row.ThumbKey = key
				}
			}
		}
	}
	return nil
}

// dropBlobs removes the blobs of a file row that was not stored after all.
func dropBlobs(ctx context.Context, blobs blob.Blobs, f model.File) {
	if blobs == nil {
		return
	}
	for _, key := range store.BlobKeys(f) {
		blobs.Delete(ctx, key)
	}
}

// file fetches one file of a message. It is an error when files were not
// asked for, when the export has no address for it (Slack hid it behind a
// plan's limit, or it was deleted), and when the address no longer works.
func (r *run) file(sf slackFile) (fetched, error) {
	if r.NoFiles {
		return fetched{}, errors.New("files were not asked for")
	}
	if sf.Mode == "hidden_by_limit" || sf.Mode == "tombstone" || sf.address() == "" {
		return fetched{}, errors.New("the export has no address for it")
	}
	limit, err := store.SettingIntOr(r.ctx, r.Store, store.SettingFilesMaxBytes, model.DefaultFileSize)
	if err != nil {
		return fetched{}, err
	}
	got, err := r.fetch(sf.address(), limit)
	if err != nil {
		return got, err
	}
	// A link that has stopped working does not fail: it answers with
	// Slack's sign-in page. A web page where the export promised something
	// else is that page, not the file.
	if got.webPage && !strings.HasSuffix(strings.ToLower(sf.name()), ".html") && !strings.HasSuffix(strings.ToLower(sf.name()), ".htm") {
		got.discard()
		return fetched{}, errors.New("the address answered with a web page, not the file: the link has stopped working")
	}
	return got, nil
}

// fetch downloads an address into a temporary file of at most limit bytes
// and looks at what it is. Kind and media type come from the content, never
// from what the export or the other end called it.
func (r *run) fetch(address string, limit int64) (fetched, error) {
	u, err := url.Parse(address)
	if err != nil {
		return fetched{}, errors.New("the address cannot be read")
	}
	if err := checkAddress(u); err != nil {
		return fetched{}, err
	}
	ctx, cancel := context.WithTimeout(r.ctx, fetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return fetched{}, err
	}
	req.Header.Set("User-Agent", "churr-import")
	resp, err := r.HTTP.Do(req)
	if err != nil {
		// The address carries a token; the error must not repeat it.
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return fetched{}, fmt.Errorf("%s: %w", u.Hostname(), err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fetched{}, fmt.Errorf("%s answered %d", u.Hostname(), resp.StatusCode)
	}
	dir := os.TempDir()
	if r.DataDir != "" {
		dir = filepath.Join(r.DataDir, "tmp")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fetched{}, err
		}
	}
	tmp, err := os.CreateTemp(dir, "import-*")
	if err != nil {
		return fetched{}, err
	}
	out := fetched{path: tmp.Name()}
	fail := func(err error) (fetched, error) {
		tmp.Close()
		out.discard()
		return fetched{}, err
	}
	// One byte more than allowed is read, to tell "exactly the limit" from
	// "more than the limit".
	n, err := io.Copy(tmp, io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return fail(fmt.Errorf("%s: the download stopped: %w", u.Hostname(), err))
	}
	if n > limit {
		return fail(fmt.Errorf("it is larger than this server's limit of %d bytes", limit))
	}
	if n == 0 {
		return fail(errors.New("it is empty"))
	}
	out.size = n
	head := make([]byte, blob.SniffLen)
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return fail(err)
	}
	hn, _ := io.ReadFull(tmp, head)
	out.mediaType, out.kind = blob.Sniff(head[:hn])
	start := strings.ToLower(strings.TrimLeft(string(head[:hn]), "\ufeff \t\r\n"))
	out.webPage = strings.HasPrefix(start, "<!doctype html") || strings.HasPrefix(start, "<html")
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return fail(err)
	}
	out.width, out.height, out.duration = blob.Probe(tmp)
	if out.kind == model.FileText {
		if _, err := tmp.Seek(0, io.SeekStart); err == nil {
			if text, err := blob.PreviewText(tmp); err == nil {
				out.previewText = text
			}
		}
	}
	return out, tmp.Close()
}

// cleanName makes a Slack file name safe to show and to put in an address:
// no path, no control characters, not empty, not too long.
func cleanName(name string) string {
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		name = name[i+1:]
	}
	name = strings.TrimSpace(strings.Map(func(r rune) rune {
		if r < ' ' || r == 0x7f {
			return -1
		}
		return r
	}, name))
	if name == "" || name == "." || name == ".." {
		return "file"
	}
	return cut(name, 255)
}
