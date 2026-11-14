// Package blob keeps file contents: uploads, thumbnails, avatars and emoji
// images. It stores bytes under random names and knows nothing about who
// may read them, what they are called or which message they belong to; all
// of that is in the database, beside the key this package hands back.
//
// It also looks at content: what kind of file some bytes are (Sniff), how
// large an image is (Probe), and how to make a small, clean copy of one
// (Thumbnail). Types always come from the bytes, never from a file name or
// from what an uploader claimed (docs/MESSAGE-FORMAT.md section 5).
package blob

import (
	"context"
	"errors"
	"io"

	"github.com/tikaydaes/churr/internal/model"
)

// Blobs is somewhere file contents are kept.
//
// A key says where its blob lives: "disk:<path>" for a file in the data
// directory, "s3:<key>" for an object in an object store. That is what
// files.blob_key holds, and it is how a server that has switched between
// the two still finds every file (docs/OPERATIONS.md section 10).
type Blobs interface {
	// Put stores everything r yields under a new random name. It returns
	// the key to find it by, how many bytes there were, and their SHA-256
	// hash. If r fails, or ctx ends, nothing is stored.
	Put(ctx context.Context, r io.Reader) (key string, size int64, sha256 []byte, err error)
	// Get opens a blob for reading. The caller closes it. A blob that does
	// not exist gives an error that is model.ErrNotFound.
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	// Delete removes a blob. Deleting one that is already gone is not an
	// error, so a delete that was interrupted can simply be done again.
	Delete(ctx context.Context, key string) error
}

// The two places a blob can live, as Mixed is told them, and as the start
// of every key before the colon.
const (
	OnDisk = "disk"
	OnS3   = "s3"

	diskPrefix = OnDisk + ":"
	s3Prefix   = OnS3 + ":"
)

// ErrBadKey is returned for a key the store cannot use: one that belongs to
// another kind of store, or one that is not the shape this package makes.
// Keys come from the database, so this means a damaged row or a mistake in
// the program, never something a person typed.
var ErrBadKey = errors.New("blob: not a usable key")

// LimitReader returns a reader that yields at most max bytes of r and then
// fails with model.ErrTooLarge if r has more. Wrapped around an upload it
// makes Put fail, and store nothing, when the content is over the limit:
//
//	key, size, sum, err := blobs.Put(ctx, blob.LimitReader(body, limit))
//	if errors.Is(err, model.ErrTooLarge) { ... }
//
// Content of exactly max bytes passes.
func LimitReader(r io.Reader, max int64) io.Reader {
	return &limitReader{r: r, left: max}
}

type limitReader struct {
	r    io.Reader
	left int64 // bytes still allowed; below zero once the limit was passed
}

func (l *limitReader) Read(p []byte) (int, error) {
	if l.left < 0 {
		return 0, model.ErrTooLarge
	}
	if len(p) == 0 {
		return 0, nil
	}
	// Ask for one byte more than is allowed. Getting it is the only way to
	// tell content that ends exactly at the limit from content that goes on.
	if int64(len(p)) > l.left+1 {
		p = p[:l.left+1]
	}
	n, err := l.r.Read(p)
	l.left -= int64(n)
	if l.left < 0 {
		return n - 1, model.ErrTooLarge
	}
	return n, err
}

// ctxReader stops a long copy when its context ends. Without it an upload
// whose client went away would be read until the connection timed out.
type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (c ctxReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}
