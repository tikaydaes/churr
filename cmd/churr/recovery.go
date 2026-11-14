package main

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/tikaydaes/churr/internal/config"
	"github.com/tikaydaes/churr/internal/server"
)

// recovery prints a one-time recovery link for an account. It takes the
// username first and then the same flags as `churr serve`, so it finds the
// same data directory, database and host name.
func recovery(args []string) error {
	if len(args) == 0 || args[0] == "" || args[0][0] == '-' {
		return errors.New("usage: churr recovery <username> [the flags churr serve is run with]")
	}
	cfg, err := config.ParseServe(args[1:], os.Getenv)
	if err != nil {
		return err
	}
	link, expires, err := server.RecoveryLink(cfg, args[0])
	if err != nil {
		return err
	}
	fmt.Printf("A recovery link for %s:\n  %s\nIt works once, until %s. Whoever opens it becomes that account.\n",
		args[0], link, expires.Local().Format(time.RFC1123))
	return nil
}
