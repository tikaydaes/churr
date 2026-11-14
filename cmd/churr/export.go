package main

import (
	"context"
	"fmt"

	"github.com/tikaydaes/churr/internal/server"
)

// export writes the community's data to one zip file.
func export(args []string) error {
	f := newFlags("churr export", "churr export [-data DIR] [-db URL] [-no-files] -o <file.zip>")
	data, db := f.community()
	out := f.text("o", "", "The file to write. Required.")
	noFiles := f.toggle("no-files", "Leave out uploaded files.")
	if _, err := f.parse(args); err != nil {
		return err
	}
	counts, size, err := server.Export(context.Background(), server.ExportOptions{
		Offline: server.Offline{Data: *data, DB: *db}, Out: *out, NoFiles: *noFiles})
	if err != nil {
		return err
	}
	fmt.Printf("Export written to %s (%d bytes): %d accounts, %d channels, %d messages, %d files.\n",
		*out, size, counts.Accounts, counts.Conversations, counts.Messages, counts.Files)
	if counts.FilesMissing > 0 {
		fmt.Printf("%d files could not be read and are not in it.\n", counts.FilesMissing)
	}
	fmt.Println("It holds every channel, private and ad-hoc ones included. Sealed chats are not in it. Keep it as carefully as the server.")
	return nil
}
