package server_test

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tikaydaes/churr/internal/config"
	"github.com/tikaydaes/churr/internal/server"
	"github.com/tikaydaes/churr/internal/store"
	"github.com/tikaydaes/churr/internal/store/storetest"
	"github.com/tikaydaes/churr/internal/testkit"
)

// A backup taken while the server runs is a complete data directory: a
// second server started on it has the community as it was, people, messages,
// files and keys (docs/OPERATIONS.md section 11).
func TestBackupAndRestore(t *testing.T) {
	ctx := context.Background()
	ts := testkit.NewServer(t)
	populate(t, ts)
	data := ts.Server.Config.Data
	dest := filepath.Join(t.TempDir(), "backup")
	off := server.Offline{Data: data, DB: ts.Server.Config.DB}

	rep, err := server.Backup(ctx, server.BackupOptions{Offline: off, Dest: dest})
	if err != nil {
		t.Fatal(err)
	}
	// The server kept running and still takes posts.
	late := ts.Member("late")
	late.Post(late.Channel("after-the-backup", false), "posted after the backup")

	same := func(rel string) {
		t.Helper()
		want, err := os.ReadFile(filepath.Join(data, rel))
		if err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(filepath.Join(dest, rel))
		if err != nil || string(got) != string(want) {
			t.Fatalf("%s in the backup: %v", rel, err)
		}
	}
	for _, key := range []string{"keys/seal.key", "keys/sign.key", "keys/box.key"} {
		same(key)
	}
	if st, err := os.Stat(filepath.Join(dest, "keys")); err != nil || st.Mode().Perm() != 0o700 {
		t.Fatalf("the keys folder of a backup must stay closed to others: %v %v", st, err)
	}
	if rep.Files < 1 || rep.FileBytes < 1 {
		t.Fatalf("no uploaded file was copied: %+v", rep)
	}
	if _, err := os.Stat(filepath.Join(dest, "tmp")); err == nil {
		t.Fatal("uploads in progress are not part of a backup")
	}

	if storetest.UsePostgres() {
		// The database is pg_dump's; only the directories are copied.
		if rep.Database {
			t.Fatal("a PostgreSQL database was said to be snapshotted")
		}
		if _, err := os.Stat(filepath.Join(dest, "churr.db")); err == nil {
			t.Fatal("a backup with PostgreSQL has a SQLite file")
		}
	} else {
		if !rep.Database || rep.DatabaseBytes < 4096 {
			t.Fatalf("the database snapshot: %+v", rep)
		}
		// The snapshot is one file, with no write-ahead log beside it.
		if _, err := os.Stat(filepath.Join(dest, "churr.db-wal")); err == nil {
			t.Fatal("the snapshot has a write-ahead log")
		}
		// Restore: a server started on the backup, with the same host
		// name, has the community.
		cfg, err := config.ParseServe([]string{"-dev", "-data", dest, "-http", "127.0.0.1:0", "-media", "off"}, func(string) string { return "" })
		if err != nil {
			t.Fatal(err)
		}
		restored, err := server.NewWith(cfg, server.Options{Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
		if err != nil {
			t.Fatalf("starting on the backup: %v", err)
		}
		defer restored.Close()
		if restored.SetupLink != "" || restored.SetupLinkKept {
			t.Fatal("the restored server thinks it has no owner")
		}
		ann, err := restored.Store.Accounts().ByUsername(ctx, "ann")
		if err != nil {
			t.Fatalf("a member of the community after the restore: %v", err)
		}
		town, err := restored.Store.Conversations().ByName(ctx, "town")
		if err != nil {
			t.Fatal(err)
		}
		msgs, _, err := restored.Store.Messages().List(ctx, town.ID, store.MessageList{})
		if err != nil || len(msgs) < 3 || !strings.Contains(msgs[0].Text, "walrus") || msgs[0].AuthorID != ann.ID {
			t.Fatalf("the messages after the restore: %d %v", len(msgs), err)
		}
		// What was posted after the backup is not in it.
		if _, err := restored.Store.Accounts().ByUsername(ctx, "late"); err == nil {
			t.Fatal("the backup holds something that happened after it")
		}
		// The uploaded file opens with the restored keys and store.
		if err := restored.Store.Messages().Hydrate(ctx, msgs, ""); err != nil {
			t.Fatal(err)
		}
		opened := false
		for _, m := range msgs {
			for _, f := range m.Files {
				stored, err := restored.Store.Files().Get(ctx, f.ID)
				if err != nil {
					t.Fatal(err)
				}
				r, err := restored.Blobs.Get(ctx, stored.BlobKey)
				if err != nil {
					t.Fatalf("an uploaded file after the restore: %v", err)
				}
				b, _ := io.ReadAll(r)
				r.Close()
				opened = string(b) == "some notes\n"
			}
		}
		if !opened {
			t.Fatal("the uploaded file's content is not in the restored server")
		}
	}

	// A destination that holds something is refused, and so is a data
	// directory that is not one.
	if _, err := server.Backup(ctx, server.BackupOptions{Offline: off, Dest: dest}); err == nil || !strings.Contains(err.Error(), "not empty") {
		t.Fatalf("backing up over a backup: %v", err)
	}
	if _, err := server.Backup(ctx, server.BackupOptions{Offline: server.Offline{Data: filepath.Join(t.TempDir(), "nothing")}, Dest: filepath.Join(t.TempDir(), "b")}); err == nil {
		t.Fatal("a data directory that does not exist was backed up")
	}
	// Without files: the database and the keys only.
	bare := filepath.Join(t.TempDir(), "bare")
	rep, err = server.Backup(ctx, server.BackupOptions{Offline: off, Dest: bare, NoFiles: true})
	if err != nil || rep.Files != 0 {
		t.Fatalf("-no-files: %+v %v", rep, err)
	}
	if _, err := os.Stat(filepath.Join(bare, "files")); err == nil {
		t.Fatal("-no-files copied files")
	}
	if _, err := os.Stat(filepath.Join(bare, "keys", "seal.key")); err != nil {
		t.Fatal("-no-files left the keys out")
	}
}
