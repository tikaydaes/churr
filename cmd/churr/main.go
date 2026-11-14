// Command churr is the Churr server.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/tikaydaes/churr/internal/version"
)

const usage = `churr is a self-hosted team chat server.

  churr serve       run the server
  churr backup      write a consistent copy of the data directory
  churr recovery    print a one-time recovery link for an account
  churr migrate-db  copy a community between SQLite and PostgreSQL
  churr import slack   read a Slack export into the community
  churr export      write the community's data to a file
  churr version     print the version

Run "churr serve -h" for its flags, or "churr serve -dev" to try it on this computer.
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "serve":
		err = serve(os.Args[2:])
	case "version":
		fmt.Println(version.String())
	case "recovery":
		err = recovery(os.Args[2:])
	case "backup":
		err = backup(os.Args[2:])
	case "migrate-db":
		err = migrateDB(os.Args[2:])
	case "import":
		err = importCmd(os.Args[2:])
	case "export":
		err = export(os.Args[2:])
	case "-h", "-help", "--help", "help":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "churr: there is no command %q\n\n%s", os.Args[1], usage)
		os.Exit(2)
	}
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			os.Exit(0)
		}
		fmt.Fprintln(os.Stderr, "churr:", err)
		os.Exit(1)
	}
}
