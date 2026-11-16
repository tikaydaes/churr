package server

import (
	"context"
	"fmt"
	"time"

	"github.com/tikaydaes/churr/internal/auth"
	"github.com/tikaydaes/churr/internal/config"
	"github.com/tikaydaes/churr/internal/store"
)

// RecoveryLink is `churr recovery <username>`: it opens the database named
// by the configuration and makes a one-hour recovery link for a local
// person, the owner included (docs/AUTH.md section 6.2). It needs the data
// directory and nothing else, so it works whether or not the server is
// running. The making is written to the audit log with the command line as
// the actor.
func RecoveryLink(cfg config.Serve, username string) (link string, expires time.Time, err error) {
	ctx := context.Background()
	st, err := store.Open(ctx, cfg.Data, cfg.DB)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("opening the database: %w", err)
	}
	defer st.Close()
	out, err := auth.CLIRecoveryLink(ctx, st, cfg.Origin(), username)
	if err != nil {
		return "", time.Time{}, err
	}
	return out.URL, out.ExpiresAt.Time(), nil
}

// NewSetupLink is `churr setup-link`: for a server that has no owner yet, it
// opens the database named by the configuration and makes a new setup link,
// and the one made before stops working. Like RecoveryLink it works whether
// or not the server is running.
func NewSetupLink(cfg config.Serve) (string, error) {
	ctx := context.Background()
	st, err := store.Open(ctx, cfg.Data, cfg.DB)
	if err != nil {
		return "", fmt.Errorf("opening the database: %w", err)
	}
	defer st.Close()
	return auth.NewSetupLink(ctx, st, cfg.Origin())
}
