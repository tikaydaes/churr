package blob

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// Lister is a store that can say what it holds. Disk, S3 and Mixed all can.
// The janitor uses it to find blobs that no row owns any more: the content
// of a file whose row was deleted in a transaction that then could not
// reach the store.
type Lister interface {
	// List calls fn for every blob with its key and when it was written.
	// It stops at the first error fn returns and returns it.
	List(ctx context.Context, fn func(key string, modified time.Time) error) error
}

// List calls fn for every blob under the directory. Uploads in progress,
// which sit at the top under temporary names, are not blobs and are skipped.
func (d *disk) List(ctx context.Context, fn func(key string, modified time.Time) error) error {
	firsts, err := os.ReadDir(d.dir)
	if err != nil {
		return err
	}
	for _, a := range firsts {
		if !a.IsDir() || len(a.Name()) != 2 || !isHex(a.Name()) {
			continue
		}
		seconds, err := os.ReadDir(filepath.Join(d.dir, a.Name()))
		if err != nil {
			return err
		}
		for _, b := range seconds {
			if !b.IsDir() || len(b.Name()) != 2 || !isHex(b.Name()) {
				continue
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			names, err := os.ReadDir(filepath.Join(d.dir, a.Name(), b.Name()))
			if err != nil {
				return err
			}
			for _, n := range names {
				key := diskPrefix + a.Name() + "/" + b.Name() + "/" + n.Name()
				if _, err := d.path(key); err != nil {
					continue // not a name Put makes
				}
				info, err := n.Info()
				if err != nil {
					if os.IsNotExist(err) || err == fs.ErrNotExist {
						continue // deleted while we were looking
					}
					return err
				}
				if err := fn(key, info.ModTime()); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// List lists both stores, whichever of them are set up and can list.
func (m *mixed) List(ctx context.Context, fn func(key string, modified time.Time) error) error {
	for _, b := range []Blobs{m.disk, m.s3} {
		if l, ok := b.(Lister); ok && b != nil {
			if err := l.List(ctx, fn); err != nil {
				return err
			}
		}
	}
	return nil
}

// WritesTo says where Mixed puts new blobs: OnDisk or OnS3. The admin
// console shows it.
func (m *mixed) WritesTo() string { return m.writeTo }
