package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/tikaydaes/churr/internal/server"
)

// importCmd is `churr import <what>`. Slack is the one thing it reads.
func importCmd(args []string) error {
	if len(args) == 0 || args[0] != "slack" {
		return errors.New("usage: churr import slack [flags] <export.zip>")
	}
	f := newFlags("churr import slack", "churr import slack [-data DIR] [-db URL] [-hostname NAME] [-no-files] [-dry-run] [-claims FILE] <export.zip>")
	data, db := f.community()
	hostname := f.text("hostname", "", "The server's public host name, for the claim links. Without it they are printed without a host.")
	noFiles := f.toggle("no-files", "Do not download the files the export links to. Without this, the command contacts Slack's file addresses.")
	dryRun := f.toggle("dry-run", "Read the export and report what would be created, and change nothing.")
	claims := f.text("claims", "", "Write the claim links to this file, one line per person, instead of printing them.")
	rest, err := f.parse(args[1:])
	if err != nil {
		return err
	}
	if len(rest) != 1 {
		f.Usage()
		return errors.New("give one Slack export")
	}
	if !*dryRun {
		fmt.Println("Stop the server before importing. Nothing is sent to anyone: people find the history when they next open the app.")
	}
	rep, err := server.ImportSlack(context.Background(), server.ImportOptions{
		Offline: server.Offline{Data: *data, DB: *db}, Zip: rest[0], Hostname: *hostname, NoFiles: *noFiles, DryRun: *dryRun,
		Out: os.Stdout, Confirm: confirm,
	})
	if err != nil {
		return err
	}
	if rep.DryRun {
		return nil
	}
	fmt.Printf("Imported %d people, %d bots, %d conversations, %d messages, %d reactions, %d pins, %d files and %d custom emoji. %d notices (joins, topic changes) were left out.\n",
		rep.People, rep.Bots, rep.Channels, rep.Messages, rep.Reactions, rep.Pins, rep.Files, rep.Emoji, rep.Notices)
	if rep.FilesMissing > 0 {
		fmt.Printf("%d files could not be fetched; their messages say \"File not available\".\n", rep.FilesMissing)
	}
	if len(rep.Claims) == 0 {
		return nil
	}
	var b strings.Builder
	for _, c := range rep.Claims {
		fmt.Fprintf(&b, "%s\t%s\t%s\n", c.Username, c.DisplayName, c.URL)
	}
	until := rep.Claims[0].ExpiresAt.Time().Local().Format("2 January 2006")
	if *claims != "" {
		if err := os.WriteFile(*claims, []byte(b.String()), 0o600); err != nil {
			return err
		}
		fmt.Printf("%d claim links were written to %s.\n", len(rep.Claims), *claims)
	} else {
		fmt.Printf("\nClaim links, one per person (username, name, link):\n%s\n", b.String())
	}
	fmt.Printf("Each link works once, until about %s. Whoever opens one takes that account over, with its history, so give each to the right person. An admin can make a fresh link at any time.\n", until)
	if *hostname == "" {
		fmt.Println("The links have no host, because -hostname was not given: put https://<your host name> in front of each.")
	}
	return nil
}

// confirm asks a yes-or-no question on the terminal. Anything but yes is no.
func confirm(question string) bool {
	fmt.Printf("%s [y/N] ", question)
	answer := make(chan string, 1)
	go func() {
		line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		answer <- strings.ToLower(strings.TrimSpace(line))
	}()
	select {
	case a := <-answer:
		return a == "y" || a == "yes"
	case <-time.After(10 * time.Minute):
		fmt.Println()
		return false
	}
}
