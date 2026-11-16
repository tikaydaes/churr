package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/tikaydaes/churr/internal/config"
	"github.com/tikaydaes/churr/internal/export"
	"github.com/tikaydaes/churr/internal/secret"
	"github.com/tikaydaes/churr/internal/store"
	"github.com/tikaydaes/churr/internal/store/migrations"
	"github.com/tikaydaes/churr/internal/store/sqlite"
)

// This file is the commands that work on a data directory without running a
// server (docs/OPERATIONS.md section 2): `churr backup` and `churr export`.
// `churr migrate-db` is in migratedb.go, `churr import slack` in
// importslack.go and `churr recovery` in recovery.go. cmd/churr reads the
// flags and prints; everything else is here.

// wireOps gives the API the owner's export.
func (s *Server) wireOps() {
	s.API.Export = export.NewRunner(export.Options{Store: s.Store, Blobs: s.Blobs, Log: s.Log}, s.Config.TmpDir())
}

// closeOps stops an export in progress before the database closes under it.
func (s *Server) closeOps() {
	if s.API != nil && s.API.Export != nil {
		s.API.Export.Close()
	}
}

// Offline says which community a command works on: the data directory, and
// the database if it is not the SQLite file inside it.
type Offline struct {
	// Data is the data directory.
	Data string
	// DB is empty for SQLite in the data directory, or a postgres://
	// address.
	DB string
	// Log receives progress. Nil means the default logger.
	Log *slog.Logger
}

func (o Offline) config() config.Serve {
	return config.Serve{Common: config.Common{Data: o.Data}, DB: o.DB}
}

func (o Offline) log() *slog.Logger {
	if o.Log != nil {
		return o.Log
	}
	return slog.Default()
}

// exists refuses a data directory that is not there. A command that works
// on a community must not make an empty one because of a mistyped path.
func (o Offline) exists() error {
	if o.Data == "" {
		return errors.New("no data directory was given (-data)")
	}
	st, err := os.Stat(o.Data)
	if err != nil || !st.IsDir() {
		return fmt.Errorf("there is no data directory at %s (-data)", o.Data)
	}
	if o.DB == "" {
		if _, err := os.Stat(filepath.Join(o.Data, sqlite.FileName)); err != nil {
			return fmt.Errorf("there is no database in %s. If the community is in PostgreSQL, give its address with -db", o.Data)
		}
	}
	return nil
}

// open opens the community's database, keys and file store without starting
// any service. The server may be running: SQLite lets a second program
// read, and write between the server's own transactions.
func (o Offline) open(ctx context.Context) (*Server, error) {
	if err := o.exists(); err != nil {
		return nil, err
	}
	cfg := o.config()
	s := &Server{Config: cfg, Log: o.log()}
	var err error
	if s.Keys, err = secret.Open(cfg.KeysDir()); err != nil {
		return nil, err
	}
	if s.Store, err = store.Open(migrations.WithLog(ctx, s.Log), cfg.Data, cfg.DB); err != nil {
		return nil, fmt.Errorf("opening the database: %w", err)
	}
	if s.Blobs, err = s.openBlobs(); err != nil {
		s.Store.Close()
		return nil, err
	}
	return s, nil
}

// ExportOptions are what `churr export` is given.
type ExportOptions struct {
	Offline
	// Out is the zip file to write.
	Out string
	// NoFiles leaves uploaded files out.
	NoFiles bool
}

// Export is `churr export`: it writes the community's data to one zip file,
// the same archive the owner can start from the admin console
// (docs/SCHEMA.md section 18). Private and ad-hoc channels are in it; sealed
// chats are not. It is written to the audit log with the command line as
// the actor. The server may be running.
func Export(ctx context.Context, o ExportOptions) (export.Counts, int64, error) {
	if o.Out == "" {
		return export.Counts{}, 0, errors.New("say where to write the export with -o <file.zip>")
	}
	s, err := o.open(ctx)
	if err != nil {
		return export.Counts{}, 0, err
	}
	defer s.Store.Close()
	// Written beside the destination and moved into place, so a file
	// with the final name is always a whole archive.
	tmp := o.Out + ".part"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return export.Counts{}, 0, err
	}
	fail := func(err error) (export.Counts, int64, error) {
		f.Close()
		os.Remove(tmp)
		return export.Counts{}, 0, err
	}
	if err := export.Audit(ctx, s.Store, "cli", "", map[string]any{"files": !o.NoFiles}); err != nil {
		return fail(err)
	}
	counts, err := export.WriteCounted(ctx, f, export.Options{Store: s.Store, Blobs: s.Blobs, NoFiles: o.NoFiles, Log: s.Log})
	if err != nil {
		return fail(err)
	}
	st, err := f.Stat()
	if err != nil {
		return fail(err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return export.Counts{}, 0, err
	}
	if err := os.Rename(tmp, o.Out); err != nil {
		os.Remove(tmp)
		return export.Counts{}, 0, err
	}
	return counts, st.Size(), nil
}

// BackupOptions are what `churr backup` is given.
type BackupOptions struct {
	Offline
	// Dest is the directory to write. It must not exist, or be empty.
	Dest string
	// NoFiles leaves `files/` out, for when uploads are backed up some
	// other way.
	NoFiles bool
}

// BackupReport says what a backup wrote.
type BackupReport struct {
	// Database is true when the SQLite database was snapshotted. It is
	// false with PostgreSQL, which is backed up with pg_dump.
	Database      bool
	DatabaseBytes int64
	// Files and FileBytes count what was copied from `files/`.
	Files     int
	FileBytes int64
}

// Backup is `churr backup`: it writes a complete, consistent data directory
// to the destination while the server keeps running (docs/OPERATIONS.md
// section 11). The database is snapshotted first, in one step; the keys and
// certificates are copied; and the files are copied last, so every file the
// snapshot mentions is there. Files are never changed once written, so
// copying them while uploads continue is safe.
//
// With PostgreSQL the database is left to pg_dump and only the directories
// are copied.
func Backup(ctx context.Context, o BackupOptions) (BackupReport, error) {
	var rep BackupReport
	if o.Dest == "" {
		return rep, errors.New("say where to write the backup: churr backup <destination directory>")
	}
	if err := o.exists(); err != nil {
		return rep, err
	}
	if entries, err := os.ReadDir(o.Dest); err == nil && len(entries) > 0 {
		return rep, fmt.Errorf("the destination %s is not empty. A backup is a whole data directory; give it a new folder", o.Dest)
	} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return rep, err
	}
	// A backup is as sensitive as the server: only its owner reads it.
	if err := os.MkdirAll(o.Dest, 0o700); err != nil {
		return rep, err
	}
	cfg := o.config()
	if !cfg.Postgres() {
		st, err := store.Open(ctx, cfg.Data, "")
		if err != nil {
			return rep, fmt.Errorf("opening the database: %w", err)
		}
		target := filepath.Join(o.Dest, sqlite.FileName)
		err = store.Snapshot(ctx, st, target)
		st.Close()
		if err != nil {
			return rep, fmt.Errorf("taking the database snapshot: %w", err)
		}
		os.Chmod(target, 0o600)
		if info, err := os.Stat(target); err == nil {
			rep.Database, rep.DatabaseBytes = true, info.Size()
		}
	}
	for _, dir := range []string{"keys", "certs"} {
		if _, _, err := copyTree(ctx, filepath.Join(o.Data, dir), filepath.Join(o.Dest, dir)); err != nil {
			return rep, fmt.Errorf("copying %s: %w", dir, err)
		}
	}
	if _, err := os.Stat(filepath.Join(o.Dest, "keys", "seal.key")); err != nil {
		return rep, fmt.Errorf("the data directory has no keys/seal.key; this is not a Churr data directory, or its keys are missing")
	}
	if !o.NoFiles {
		n, size, err := copyTree(ctx, cfg.FilesDir(), filepath.Join(o.Dest, "files"))
		if err != nil {
			return rep, fmt.Errorf("copying files: %w", err)
		}
		rep.Files, rep.FileBytes = n, size
	}
	return rep, nil
}

// copyTree copies a directory and everything in it, keeping each entry's
// permissions. A source that does not exist copies nothing. A file that
// disappears while the copy runs (the server deleted it) is skipped.
func copyTree(ctx context.Context, from, to string) (files int, size int64, err error) {
	if _, err := os.Stat(from); errors.Is(err, fs.ErrNotExist) {
		return 0, 0, nil
	}
	err = filepath.WalkDir(from, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		rel, err := filepath.Rel(from, path)
		if err != nil {
			return err
		}
		target := filepath.Join(to, rel)
		info, err := d.Info()
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		switch {
		case d.IsDir():
			return os.MkdirAll(target, info.Mode().Perm()|0o700)
		case !info.Mode().IsRegular():
			return nil // nothing in a data directory is a link or a device
		}
		n, err := copyFile(path, target, info.Mode().Perm())
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		files++
		size += n
		return nil
	})
	return files, size, err
}

func copyFile(from, to string, mode fs.FileMode) (int64, error) {
	in, err := os.Open(from)
	if err != nil {
		return 0, err
	}
	defer in.Close()
	out, err := os.OpenFile(to, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return 0, err
	}
	n, err := io.Copy(out, in)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	return n, err
}
