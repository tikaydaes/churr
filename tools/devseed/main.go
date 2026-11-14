// Command devseed fills a fresh data directory with a small community, so
// the apps can be tried by hand and by their integration tests without a
// browser sign-in.
//
//	go run ./tools/devseed -data /tmp/churr-dev
//	go run ./cmd/churr serve -dev -data /tmp/churr-dev
//
// It starts the server in this process on the address the trial server will
// use, makes everything through the real API (so entities, events and
// counts are exactly what a server produces), signs each chosen account in
// as an app through the real OAuth endpoints, prints the tokens as JSON and
// stops. The server started afterwards on the same data directory accepts
// them.
//
// What it makes: an owner and four members, a bot, two sections, channels
// with sub-channels (two of them sharing a name), a private channel, ad-hoc
// chats, a message holding every Churr Markdown construct, a long history
// for paging, threads, reactions, a poll, a checklist with ticks, files
// (an image, a text file, a binary file), cards from the bot, and
// memberships with different `mode` and `show` settings.
//
// It is a development tool. It imports the server's packages but no test
// package, and changes nothing outside the data directory it is given.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tikaydaes/churr/internal/config"
	"github.com/tikaydaes/churr/internal/server"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "devseed:", err)
		os.Exit(1)
	}
}

// output is what devseed prints: everything a trial or a test needs to
// find its way around the seeded community.
type output struct {
	// Origin is the address the trial server must be started on for the
	// tokens' server to be the one the app is told about.
	Origin string `json:"origin"`
	// Accounts holds app tokens for the accounts asked for with -tokens.
	Accounts []account `json:"accounts"`
	// Conversations maps a short key (see seed.go) to a conversation id.
	Conversations map[string]string `json:"conversations"`
	// InviteCode lets a person register an account of their own in a
	// browser, to try the real sign-in.
	InviteCode string `json:"invite_code"`
}

type account struct {
	Username     string `json:"username"`
	ID           string `json:"id"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
}

func run() error {
	data := flag.String("data", "", "The data directory to fill. It must not hold a database yet.")
	addr := flag.String("http", "localhost:8472", "The address the trial server will listen on (the default of `churr serve -dev`).")
	tokens := flag.String("tokens", "kai,sam", "Usernames to print app tokens for: any of kai, sam, lee, mia, ravi.")
	history := flag.Int("history", 130, "How many messages the long channel gets, for trying paging.")
	out := flag.String("o", "", "Write the JSON here instead of standard output.")
	flag.Parse()
	if *data == "" {
		return fmt.Errorf("-data is required")
	}
	if _, err := os.Stat(filepath.Join(*data, "churr.db")); err == nil {
		return fmt.Errorf("%s already holds a database; devseed only fills a fresh directory", *data)
	}
	if err := os.MkdirAll(*data, 0o700); err != nil {
		return err
	}

	cfg, err := config.ParseServe([]string{"-dev", "-data", *data, "-http", *addr, "-log-level", "error",
		// Calls are not seeded; media goes on a spare loopback port so
		// devseed never collides with a server that is running.
		"-media", "127.0.0.1:0", "-media-address", "127.0.0.1"}, func(string) string { return "" })
	if err != nil {
		return err
	}
	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		return fmt.Errorf("%v (is a server already running there? stop it first)", err)
	}
	srv, err := server.NewWith(cfg, server.Options{
		Listener: ln,
		Log:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		// Seeding posts far faster than a person does.
		RatePerSecond: 1000, RateBurst: 100000,
	})
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		srv.Run(ctx)
		close(done)
	}()
	stop := func() {
		cancel()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
		}
	}

	w := &world{url: "http://" + ln.Addr().String(), origin: cfg.Origin(), hostname: cfg.Hostname, setupLink: srv.SetupLink}
	res, err := w.seed(strings.Split(*tokens, ","), *history)
	stop()
	if err != nil {
		return err
	}
	raw, _ := json.MarshalIndent(res, "", "  ")
	raw = append(raw, '\n')
	if *out != "" {
		return os.WriteFile(*out, raw, 0o600)
	}
	_, err = os.Stdout.Write(raw)
	return err
}
