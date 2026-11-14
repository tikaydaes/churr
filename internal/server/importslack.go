package server

import (
	"context"
	"errors"
	"io"
	"strings"

	"github.com/tikaydaes/churr/internal/auth"
	"github.com/tikaydaes/churr/internal/importer"
)

// ImportOptions are what `churr import slack` is given.
type ImportOptions struct {
	Offline
	// Zip is the Slack export.
	Zip string
	// Hostname is the server's public host name, for the claim links.
	// Without it the links are printed without a host.
	Hostname string
	// NoFiles downloads nothing; DryRun changes nothing.
	NoFiles, DryRun bool
	// Out is where what was found and progress are printed.
	Out io.Writer
	// Confirm is asked before importing messages the retention period
	// would delete.
	Confirm func(question string) bool
}

// ImportSlack is `churr import slack`: it reads a Slack export into the
// community (docs/FEATURES.md section 25). The server should be stopped: it
// would not tell anyone connected about what appears, and on SQLite the two
// would wait on each other.
func ImportSlack(ctx context.Context, o ImportOptions) (importer.Report, error) {
	if o.Zip == "" {
		return importer.Report{}, errors.New("say which export to read: churr import slack <export.zip>")
	}
	s, err := o.open(ctx)
	if err != nil {
		return importer.Report{}, err
	}
	defer s.Store.Close()
	origin := ""
	if host := strings.TrimSpace(o.Hostname); host != "" {
		origin = "https://" + strings.ToLower(host)
	}
	return importer.Slack(ctx, o.Zip, importer.Options{
		Store: s.Store, Blobs: s.Blobs, DataDir: o.Data, Origin: origin, NoFiles: o.NoFiles, DryRun: o.DryRun,
		// The same keyed hash company sign-in will look for.
		EmailHash: func(email string) []byte { return auth.ImportEmailHashWith(s.Keys, email) },
		Out:       o.Out, Confirm: o.Confirm, Log: s.Log,
	})
}
