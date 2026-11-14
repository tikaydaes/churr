package blob

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/tikaydaes/churr/internal/model"
)

type mixed struct {
	disk, s3 Blobs
	writeTo  string
}

// Mixed joins a disk store and an object store into one. Each blob is read
// and deleted wherever its key says it lives, and new blobs go to the one
// named by writeTo, OnDisk or OnS3. This is what lets an admin switch
// object storage on or off without moving anything: old files stay where
// they are and keep working (docs/OPERATIONS.md section 10).
//
// Either store may be nil when it is not set up. Using a blob that lives in
// a store that is not there gives model.ErrUnavailable, as does a Put when
// writeTo names it.
//
// The two stores can be any Blobs. Mixed only looks at the start of a key.
func Mixed(disk, s3 Blobs, writeTo string) Blobs {
	return &mixed{disk: disk, s3: s3, writeTo: writeTo}
}

func (m *mixed) Put(ctx context.Context, r io.Reader) (string, int64, []byte, error) {
	var to Blobs
	switch m.writeTo {
	case OnDisk:
		to = m.disk
	case OnS3:
		to = m.s3
	default:
		return "", 0, nil, fmt.Errorf("blob: files can be written to %q or %q, not %q", OnDisk, OnS3, m.writeTo)
	}
	if to == nil {
		return "", 0, nil, fmt.Errorf("blob: the %s store is not set up: %w", m.writeTo, model.ErrUnavailable)
	}
	return to.Put(ctx, r)
}

func (m *mixed) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	from, err := m.home(key)
	if err != nil {
		return nil, err
	}
	return from.Get(ctx, key)
}

func (m *mixed) Delete(ctx context.Context, key string) error {
	from, err := m.home(key)
	if err != nil {
		return err
	}
	return from.Delete(ctx, key)
}

// home is the store a key's blob lives in.
func (m *mixed) home(key string) (Blobs, error) {
	var b Blobs
	var where string
	switch {
	case strings.HasPrefix(key, diskPrefix):
		b, where = m.disk, OnDisk
	case strings.HasPrefix(key, s3Prefix):
		b, where = m.s3, OnS3
	default:
		return nil, fmt.Errorf("%w: %q says neither disk nor s3", ErrBadKey, key)
	}
	if b == nil {
		return nil, fmt.Errorf("blob: %s lives in the %s store, which is not set up: %w", key, where, model.ErrUnavailable)
	}
	return b, nil
}
