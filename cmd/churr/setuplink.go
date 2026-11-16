package main

import (
	"fmt"
	"os"

	"github.com/tikaydaes/churr/internal/config"
	"github.com/tikaydaes/churr/internal/server"
)

// setupLink prints a new setup link for a server that has no owner yet, for
// an operator who lost the one printed at the first start. It takes the same
// flags as `churr serve`, so it finds the same data directory, database and
// host name.
func setupLink(args []string) error {
	cfg, err := config.ParseServe(args, os.Getenv)
	if err != nil {
		return err
	}
	link, err := server.NewSetupLink(cfg)
	if err != nil {
		return err
	}
	fmt.Printf("A new setup link:\n  %s\nIt works once. The link printed before no longer works.\n", link)
	return nil
}
