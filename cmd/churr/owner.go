package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/tikaydaes/churr/internal/config"
	"github.com/tikaydaes/churr/internal/server"
)

// owner is `churr owner grant <username>` and `churr owner revoke
// <username>`, then the same flags as `churr serve`, so it finds the same
// data directory and database. It is for the extreme case the instance
// cannot settle itself: its owners are gone, or one must be stopped.
func owner(args []string) error {
	const use = "usage: churr owner grant|revoke <username> [the flags churr serve is run with]"
	if len(args) < 2 || (args[0] != "grant" && args[0] != "revoke") || args[1] == "" || args[1][0] == '-' {
		return errors.New(use)
	}
	cfg, err := config.ParseServe(args[2:], os.Getenv)
	if err != nil {
		return err
	}
	grant := args[0] == "grant"
	fmt.Println("The server must be stopped while this runs: it keeps roles in memory.")
	acc, err := server.SetOwner(cfg, args[1], grant)
	if err != nil {
		return err
	}
	if grant {
		fmt.Printf("%s is an owner of this instance. Only they, or this command, can undo it. It is in the audit log.\n", acc.Username)
	} else {
		fmt.Printf("%s is an admin of this instance and no longer an owner. It is in the audit log.\n", acc.Username)
	}
	return nil
}
