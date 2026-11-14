package blob

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tikaydaes/churr/internal/model"
)

// nameBytes is the randomness in a blob's name: 128 bits, 32 hex
// characters. Names are never guessed at, only looked up, so this is for
// uniqueness, and two of them will not collide.
const nameBytes = 16

// tmpPattern names the files that uploads in progress are written to. They
// sit at the top of the directory, where no blob ever is, so they are on
// the same file system as their final place and can be renamed into it.
const tmpPattern = ".put-*"

// staleAfter is how old a temporary file must be before a starting server
// clears it away. No upload takes a day, and nothing younger is touched in
// case another process, such as an import, is writing beside this one.
const staleAfter = 24 * time.Hour

type disk struct {
	dir string
}

// Disk keeps blobs as files under dir, which it makes if missing. This is
// the files/ folder of the data directory (docs/OPERATIONS.md section 4):
//
//	files/3f/a1/3fa1c0…   random names, two levels of folders
//
// Keys are "disk:3f/a1/3fa1c0…", the path under dir with forward slashes.
// Files and folders are made readable by the server's own user only.
func Disk(dir string) (Blobs, error) {
	if dir == "" {
		return nil, errors.New("blob: no directory given for files")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("blob: making the files directory: %w", err)
	}
	// Whatever a crash left half-written. Failing to tidy is not a reason
	// to refuse to start.
	if stale, err := filepath.Glob(filepath.Join(dir, tmpPattern)); err == nil {
		for _, path := range stale {
			if info, err := os.Lstat(path); err == nil && time.Since(info.ModTime()) > staleAfter {
				os.Remove(path)
			}
		}
	}
	return &disk{dir: dir}, nil
}

func (d *disk) Put(ctx context.Context, r io.Reader) (key string, size int64, sum []byte, err error) {
	if err := ctx.Err(); err != nil {
		return "", 0, nil, err
	}
	// Written under a temporary name and renamed when complete, so a file
	// with a blob's name is always a whole blob.
	tmp, err := os.CreateTemp(d.dir, tmpPattern)
	if err != nil {
		return "", 0, nil, fmt.Errorf("blob: starting a file: %w", err)
	}
	defer func() {
		if err != nil {
			tmp.Close()
			os.Remove(tmp.Name())
		}
	}()

	hash := sha256.New()
	size, err = io.Copy(io.MultiWriter(tmp, hash), ctxReader{ctx, r})
	if err != nil {
		// Passed on as it is inside the wrapping, so that a caller can tell
		// model.ErrTooLarge from LimitReader, or its own reader's error.
		return "", 0, nil, fmt.Errorf("blob: writing a file: %w", err)
	}
	// The database is about to say this file exists; make that true first.
	if err = tmp.Sync(); err != nil {
		return "", 0, nil, fmt.Errorf("blob: writing a file: %w", err)
	}
	if err = tmp.Close(); err != nil {
		return "", 0, nil, fmt.Errorf("blob: writing a file: %w", err)
	}

	raw := make([]byte, nameBytes)
	rand.Read(raw)
	name := hex.EncodeToString(raw)
	rel := name[0:2] + "/" + name[2:4] + "/" + name
	final := filepath.Join(d.dir, filepath.FromSlash(rel))
	if err = os.MkdirAll(filepath.Dir(final), 0o700); err != nil {
		return "", 0, nil, fmt.Errorf("blob: making a folder: %w", err)
	}
	if err = os.Rename(tmp.Name(), final); err != nil {
		return "", 0, nil, fmt.Errorf("blob: putting a file in place: %w", err)
	}
	return diskPrefix + rel, size, hash.Sum(nil), nil
}

func (d *disk) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	path, err := d.path(key)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("blob: %s is gone: %w", key, model.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("blob: opening %s: %w", key, err)
	}
	return f, nil
}

func (d *disk) Delete(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	path, err := d.path(key)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("blob: deleting %s: %w", key, err)
	}
	// The two folders above are left, even when empty. There are at most
	// 65,536 of them, and removing one could race with a Put about to use
	// it.
	return nil
}

// path turns a key into the file it names, or refuses it. Only the exact
// shape Put makes is accepted, "disk:3f/a1/3fa1c0…", which rules out by
// construction every way of pointing a key outside the directory: there is
// no room in it for "..", an absolute path, a backslash or a temporary
// file's name.
func (d *disk) path(key string) (string, error) {
	rel, ok := strings.CutPrefix(key, diskPrefix)
	if !ok {
		return "", fmt.Errorf("%w: %q is not a disk key", ErrBadKey, key)
	}
	const nameLen = 2 * nameBytes
	if len(rel) != 2+1+2+1+nameLen || rel[2] != '/' || rel[5] != '/' {
		return "", fmt.Errorf("%w: %q", ErrBadKey, key)
	}
	a, b, name := rel[0:2], rel[3:5], rel[6:]
	if !isHex(name) || name[0:2] != a || name[2:4] != b {
		return "", fmt.Errorf("%w: %q", ErrBadKey, key)
	}
	return filepath.Join(d.dir, a, b, name), nil
}

// isHex reports whether s is lower-case hexadecimal and nothing else.
func isHex(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
