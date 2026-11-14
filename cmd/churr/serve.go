package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/tikaydaes/churr/internal/config"
	"github.com/tikaydaes/churr/internal/server"
)

// serve runs the server until it is interrupted.
func serve(args []string) error {
	cfg, err := config.ParseServe(args, os.Getenv)
	if err != nil {
		return err
	}
	srv, err := server.New(cfg)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return srv.Run(ctx)
}
