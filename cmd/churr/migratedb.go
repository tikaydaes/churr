package main

import (
	"context"
	"fmt"

	"github.com/tikaydaes/churr/internal/server"
)

// migrateDB copies a community from one database to the other.
func migrateDB(args []string) error {
	f := newFlags("churr migrate-db", "churr migrate-db -from <path or URL> -to <path or URL>")
	from := f.text("from", "", "The database to copy from: the path of a SQLite file, or a postgres:// address. It is only read.")
	to := f.text("to", "", "The database to copy into: a path or a postgres:// address. It must be empty.")
	if _, err := f.parse(args); err != nil {
		return err
	}
	fmt.Println("Copying. The server must be stopped while this runs.")
	counts, err := server.MigrateDB(context.Background(), *from, *to)
	if err != nil {
		return err
	}
	var rows int64
	for _, c := range counts {
		if c.Rows > 0 {
			fmt.Printf("  %-22s %d\n", c.Table, c.Rows)
		}
		rows += c.Rows
	}
	fmt.Printf("Copied %d rows in %d tables. The row counts of every table match, and the search index was built again.\n", rows, len(counts))
	fmt.Println("The source was not changed. Start the server with the new database (-db), and keep the old one until you have checked the new.")
	return nil
}
