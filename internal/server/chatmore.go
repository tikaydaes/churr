package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/tikaydaes/churr/internal/blob"
	"github.com/tikaydaes/churr/internal/janitor"
	"github.com/tikaydaes/churr/internal/mod"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/preview"
	"github.com/tikaydaes/churr/internal/store"
)

// wireModeration builds what the rest of conversations and messages needs
// and connects it to chat: moderation (slow mode, the word filter,
// time-outs, reports), link previews, and the janitor.
func (s *Server) wireModeration() {
	s.Mod = mod.New(mod.Deps{Store: s.Store, Perm: s.Perm, Hub: s.Live, Log: s.Log})
	// mod sits below chat. chat asks it on the post path; it asks chat to
	// delete a message when a handler chooses that outcome for a report.
	s.Mod.SetDeleter(s.Chat)
	s.Chat.SetModerator(s.Mod)

	s.Previews = preview.New(preview.Deps{Store: s.Store, Blobs: s.Blobs, Own: s.Config.Hostname, Log: s.Log})
	s.Chat.SetPreviewer(s.Previews)
	// A card image a webhook names by address is fetched the same way.
	if s.Bots != nil {
		s.Bots.SetImages(s.Previews)
	}
	s.Chat.SetTmpDir(s.Config.TmpDir())

	s.Janitor = janitor.New(janitor.Deps{Store: s.Store, Blobs: s.Blobs, Previews: s.Previews, Log: s.Log})
}

// S3Settings is the `files.s3` setting as an admin enters it
// (docs/OPERATIONS.md section 5).
type S3Settings struct {
	Endpoint    string `json:"endpoint"`
	Region      string `json:"region"`
	Bucket      string `json:"bucket"`
	Prefix      string `json:"prefix"`
	PathStyle   bool   `json:"path_style"`
	AccessKeyID string `json:"access_key_id"`
}

// The settings that say where files are kept.
const (
	settingFilesStore = "files.store"
	settingS3         = "files.s3"
	settingS3Secret   = "files.s3_secret"
)

// S3Config reads the stored object storage settings and opens the secret,
// which is sealed under the server's own key. found is false when none are
// stored.
func (s *Server) S3Config(ctx context.Context) (cfg blob.S3Config, found bool, err error) {
	raw, ok, err := s.Store.Settings().Get(ctx, settingS3)
	if err != nil || !ok || string(raw) == "null" {
		return cfg, false, err
	}
	var set S3Settings
	if err := json.Unmarshal(raw, &set); err != nil {
		return cfg, false, errors.New("the stored object storage settings cannot be read")
	}
	cfg = blob.S3Config{Endpoint: set.Endpoint, Region: set.Region, Bucket: set.Bucket, Prefix: set.Prefix, PathStyle: set.PathStyle,
		AccessKey: set.AccessKeyID, TmpDir: s.Config.TmpDir()}
	sealedRaw, ok, err := s.Store.Settings().Get(ctx, settingS3Secret)
	if err != nil {
		return cfg, false, err
	}
	if ok && string(sealedRaw) != "null" {
		// Stored as JSON: the sealed bytes in base64, as encoding/json
		// writes a byte slice.
		var sealed []byte
		if err := json.Unmarshal(sealedRaw, &sealed); err != nil {
			return cfg, false, errors.New("the stored object storage secret cannot be read")
		}
		plain, err := s.Keys.Unseal(sealed, "settings", "value", settingS3Secret)
		if err != nil {
			return cfg, false, errors.New("the stored object storage secret cannot be opened with this server's key")
		}
		cfg.SecretKey = string(plain)
	}
	return cfg, true, nil
}

// liveBlobs is the file store the server runs on: files on disk, objects in
// an S3-compatible store, or both. Each blob is read and deleted wherever
// its key says it lives, and new ones go where the `files.store` setting
// says now, so an admin can switch object storage on or off without a
// restart and without moving anything (docs/OPERATIONS.md section 10). The
// settings are read again at most every few seconds.
type liveBlobs struct {
	s    *Server
	disk blob.Blobs

	mu      sync.Mutex
	current blob.Blobs
	shape   string
	checked time.Time
}

// reread is how long a look at the settings is trusted.
const reread = 5 * time.Second

// store returns the Mixed store for the settings as they stand.
func (l *liveBlobs) store(ctx context.Context) blob.Blobs {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.current != nil && time.Since(l.checked) < reread {
		return l.current
	}
	l.checked = time.Now()
	writeTo, err := store.SettingString(ctx, l.s.Store, settingFilesStore, blob.OnDisk)
	if err != nil {
		if l.current != nil {
			return l.current
		}
		writeTo = blob.OnDisk
	}
	cfg, found, err := l.s.S3Config(ctx)
	if err != nil {
		l.s.Log.Warn("object storage settings could not be read; files in object storage are unavailable", "err", err)
		found = false
	}
	shape, _ := json.Marshal([]any{writeTo, found, cfg.Endpoint, cfg.Region, cfg.Bucket, cfg.Prefix, cfg.PathStyle, cfg.AccessKey, len(cfg.SecretKey)})
	if l.current != nil && string(shape) == l.shape {
		return l.current
	}
	var s3 blob.Blobs
	if found {
		if s3, err = blob.S3(cfg); err != nil {
			l.s.Log.Warn("object storage is not usable as configured", "err", err)
			s3 = nil
		}
	}
	// With no object store, Mixed answers `unavailable` for uploads when
	// the setting says s3, and for blobs that live there.
	l.current, l.shape = blob.Mixed(l.disk, s3, writeTo), string(shape)
	return l.current
}

func (l *liveBlobs) Put(ctx context.Context, r io.Reader) (string, int64, []byte, error) {
	return l.store(ctx).Put(ctx, r)
}

func (l *liveBlobs) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	return l.store(ctx).Get(ctx, key)
}

func (l *liveBlobs) Delete(ctx context.Context, key string) error {
	return l.store(ctx).Delete(ctx, key)
}

// List lists every blob in both stores, for the janitor's sweep.
func (l *liveBlobs) List(ctx context.Context, fn func(key string, modified time.Time) error) error {
	if lister, ok := l.store(ctx).(blob.Lister); ok {
		return lister.List(ctx, fn)
	}
	return nil
}

// FilesStore says where new files go now: "disk" or "s3".
func (s *Server) FilesStore(ctx context.Context) string {
	v, err := store.SettingString(ctx, s.Store, settingFilesStore, blob.OnDisk)
	if err != nil || (v != blob.OnDisk && v != blob.OnS3) {
		return blob.OnDisk
	}
	return v
}

// openBlobs opens the file store: the files folder of the data directory,
// joined with object storage as the settings say.
func (s *Server) openBlobs() (blob.Blobs, error) {
	disk, err := blob.Disk(s.Config.FilesDir())
	if err != nil {
		return nil, err
	}
	return &liveBlobs{s: s, disk: disk}, nil
}

// TestStorage is the admin console's "Test": it writes, reads and deletes
// one small object with the settings given (the `files.s3` object and the
// secret), or with the stored ones where a part is not given.
func (s *Server) TestStorage(ctx context.Context, settings json.RawMessage, secret *string) error {
	cfg, found, err := s.S3Config(ctx)
	if err != nil {
		return err
	}
	if len(settings) > 0 && string(settings) != "null" {
		var set S3Settings
		if err := json.Unmarshal(settings, &set); err != nil {
			return model.Invalid("files.s3", "files.s3 has endpoint, region, bucket, prefix, path_style and access_key_id.")
		}
		cfg = blob.S3Config{Endpoint: set.Endpoint, Region: set.Region, Bucket: set.Bucket, Prefix: set.Prefix,
			PathStyle: set.PathStyle, AccessKey: set.AccessKeyID, SecretKey: cfg.SecretKey, TmpDir: s.Config.TmpDir()}
	} else if !found {
		return errors.New("no object storage settings are stored, and none were given to test")
	}
	if secret != nil {
		cfg.SecretKey = *secret
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	return blob.CheckS3(ctx, cfg)
}
