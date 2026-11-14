package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/tikaydaes/churr/internal/server"
)

// backup writes a complete, consistent data directory to a destination
// while the server keeps running.
func backup(args []string) error {
	f := newFlags("churr backup", "churr backup [-data DIR] [-db URL] [-no-files] <destination directory>")
	data, db := f.community()
	noFiles := f.toggle("no-files", "Leave out files/, for when uploads are backed up some other way.")
	rest, err := f.parse(args)
	if err != nil {
		return err
	}
	if len(rest) != 1 {
		f.Usage()
		return errors.New("give one destination directory")
	}
	rep, err := server.Backup(context.Background(), server.BackupOptions{
		Offline: server.Offline{Data: *data, DB: *db}, Dest: rest[0], NoFiles: *noFiles})
	if err != nil {
		return err
	}
	fmt.Printf("Backup written to %s\n", rest[0])
	if rep.Database {
		fmt.Printf("  churr.db   a consistent snapshot, %d bytes\n", rep.DatabaseBytes)
	} else {
		fmt.Println("  The database is in PostgreSQL and is not in this backup. Dump it with pg_dump, as close in time to this as you can:")
		fmt.Println("    pg_dump --format=custom --file=churr.dump '<the -db address>'")
	}
	fmt.Println("  keys/      the server's keys. Without them the backup cannot be used")
	if *noFiles {
		fmt.Println("  files/     left out, as asked. Copy the data directory's files/ yourself, after this")
	} else {
		fmt.Printf("  files/     %d files, %d bytes\n", rep.Files, rep.FileBytes)
	}
	fmt.Println("A backup is as sensitive as the server: it holds everything the server can read, and its keys.")
	return nil
}
