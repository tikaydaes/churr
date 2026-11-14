// Command churr-relay is the push relay (docs/PUSH.md sections 5 and 7):
// it turns content-free wake-ups from Churr servers into requests to
// Apple's and Google's push services. It has no database and no accounts.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/tikaydaes/churr/internal/config"
	"github.com/tikaydaes/churr/internal/relay"
	"github.com/tikaydaes/churr/internal/version"
)

const usage = `churr-relay passes phone wake-ups from Churr servers to Apple and Google. It cannot read them.

  churr-relay serve     run the relay
  churr-relay version   print the version

Run "churr-relay serve -h" for its flags. Started without the Apple flags it answers
iPhone wake-ups with "platform off", and the same for Android without the Google file.
With -dry-run it accepts and counts wake-ups and sends nothing, for development.
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
	case "-h", "-help", "--help", "help":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "churr-relay: there is no command %q\n\n%s", os.Args[1], usage)
		os.Exit(2)
	}
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			os.Exit(0)
		}
		fmt.Fprintln(os.Stderr, "churr-relay:", err)
		os.Exit(1)
	}
}

// serve runs the relay until it is interrupted.
func serve(args []string) error {
	cfg, err := config.ParseRelay(args, os.Getenv)
	if err != nil {
		return err
	}
	r, err := relay.New(cfg)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return r.Run(ctx)
}
