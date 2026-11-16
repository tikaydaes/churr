package server_test

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/tikaydaes/churr/internal/config"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/server"
	"github.com/tikaydaes/churr/internal/testkit"
)

// An unused setup link survives a restart: the second start keeps it and
// prints no new one. churr setup-link replaces it, and only the new one
// works.
func TestSetupLinkSurvivesRestart(t *testing.T) {
	ctx := context.Background()
	cfg, err := config.ParseServe([]string{"-dev", "-data", t.TempDir(), "-http", "127.0.0.1:0", "-media", "off"}, func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	start := func() *server.Server {
		t.Helper()
		srv, err := server.NewWith(cfg, server.Options{Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
		if err != nil {
			t.Fatal(err)
		}
		return srv
	}
	token := func(link string) string { return link[strings.LastIndex(link, "/")+1:] }
	works := func(srv *server.Server, tok string) bool {
		t.Helper()
		_, err := srv.Auth.BeginSetup(ctx, "127.0.0.1", tok, "ann", "Ann")
		if err != nil && model.CodeOf(err) != model.CodeNotFound {
			t.Fatalf("beginning setup: %v", err)
		}
		return err == nil
	}

	first := start()
	if first.SetupLink == "" || first.SetupLinkKept {
		t.Fatalf("a new server: link %q, kept %v", first.SetupLink, first.SetupLinkKept)
	}
	old := token(first.SetupLink)
	first.Close()

	again := start()
	if again.SetupLink != "" || !again.SetupLinkKept {
		t.Fatalf("after a restart: link %q, kept %v", again.SetupLink, again.SetupLinkKept)
	}
	if !works(again, old) {
		t.Fatal("the first setup link stopped working at the restart")
	}
	again.Close()

	link, err := server.NewSetupLink(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(link, cfg.Origin()+"/setup/") || token(link) == old {
		t.Fatalf("churr setup-link printed %q", link)
	}
	third := start()
	defer third.Close()
	if works(third, old) {
		t.Error("the replaced setup link still works")
	}
	if !works(third, token(link)) {
		t.Error("the new setup link does not work")
	}
}

// Once there is an owner (testkit makes one) there is no setup link to make.
func TestSetupLinkRefusedWithOwner(t *testing.T) {
	ts := testkit.NewServer(t)
	if _, err := server.NewSetupLink(ts.Server.Config); model.CodeOf(err) != model.CodeConflict {
		t.Fatalf("churr setup-link with an owner: %v", err)
	}
}
