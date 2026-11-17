// Command churr-hub relays shared channels between Churr servers
// (docs/LINK-PROTOCOL.md section 23). It has no admin console and no
// accounts: whoever can run these commands on its machine is its operator.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/tikaydaes/churr/internal/config"
	"github.com/tikaydaes/churr/internal/hub"
	"github.com/tikaydaes/churr/internal/version"
)

const usage = `churr-hub relays shared channels between Churr servers. It cannot read them.

  churr-hub serve                      run the hub
  churr-hub fingerprint                print its name and fingerprint
  churr-hub invite [--for NAME] [--expires DURATION]
                                       print an invitation for a server's owner
  churr-hub invites                    list invitations not yet used
  churr-hub revoke-invite ID           cancel one
  churr-hub servers                    list the servers on the hub, each with its measured clock
  churr-hub remove NAME                remove a server
  churr-hub streams                    list the streams it carries, each with its home hub
  churr-hub channels                   list the channels it owns: name, description, listing, servers, managers, moderators
  churr-hub appoint STREAM ACCOUNT-ID USERNAME@SERVER   make an account a manager of a channel
  churr-hub dismiss STREAM ACCOUNT-ID  take an account off a channel's managers
  churr-hub expel STREAM SERVER        remove a server from one channel
  churr-hub limits [NAME] [--submits N] [--blob-bytes N] [--streams N]
                                       show or set limits, for one server or as the default
  churr-hub status                     print what the status page shows

Hubs joined into a cluster by trunks (docs/LINK-PROTOCOL.md section 23b):

  churr-hub trunk invite [--for NAME] [--expires DURATION]
                                       print an invitation for another hub to join this hub's cluster
  churr-hub trunk join INVITATION      join the cluster of the hub that made the invitation
  churr-hub trunk cancel               give up a join that has not happened yet
  churr-hub cluster                    list the cluster's hubs, this hub's clock against each, and its trunks
  churr-hub cluster leave              take this hub out of its cluster
  churr-hub cluster remove HUB         take out a hub that joined through this one
  churr-hub rehome STREAM HUB          move a channel's home from this hub to another; both must be up.
                                       Nothing else ever moves a home: a channel whose home hub is down
                                       is read-and-post only until that hub returns
  churr-hub version                    print the version

Every command takes -data and -hostname as "churr-hub serve" does, or reads
CHURR_HUB_DATA and CHURR_HUB_HOSTNAME. Run "churr-hub serve -h" for its flags.
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	err := run(os.Args[1], os.Args[2:])
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			os.Exit(0)
		}
		fmt.Fprintln(os.Stderr, "churr-hub:", err)
		os.Exit(1)
	}
}

// take pulls a command's own options out of its arguments, so the rest can
// go to the parser of the flags every command shares. A value may follow as
// the next argument or after "=".
func take(args []string, names ...string) (values map[string]string, rest []string) {
	values = map[string]string{}
	for i := 0; i < len(args); i++ {
		arg := strings.TrimLeft(args[i], "-")
		key, value, inline := strings.Cut(arg, "=")
		matched := false
		if strings.HasPrefix(args[i], "-") {
			for _, n := range names {
				if key != n {
					continue
				}
				matched = true
				if !inline && i+1 < len(args) {
					i++
					value = args[i]
				}
				values[n] = value
			}
		}
		if !matched {
			rest = append(rest, args[i])
		}
	}
	return values, rest
}

// open opens the hub's data for an operator command. positional is how many
// leading arguments are the command's own and not flags.
func open(args []string) (*hub.Hub, []string, error) {
	var positional []string
	for len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		positional = append(positional, args[0])
		args = args[1:]
	}
	cfg, err := config.ParseHub(args, os.Getenv)
	if err != nil {
		return nil, nil, err
	}
	if !hub.DataDirExists(cfg.Data) {
		return nil, nil, fmt.Errorf("there is no hub in %s; run \"churr-hub serve\" there first, or give -data", cfg.Data)
	}
	h, err := hub.New(cfg)
	return h, positional, err
}

func number(s, what string) (int64, error) {
	if s == "" {
		return 0, nil
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("%s must be a whole number above 0", what)
	}
	return n, nil
}

func run(command string, args []string) error {
	switch command {
	case "version":
		fmt.Println(version.String())
		return nil
	case "-h", "-help", "--help", "help":
		fmt.Print(usage)
		return nil
	case "serve":
		cfg, err := config.ParseHub(args, os.Getenv)
		if err != nil {
			return err
		}
		h, err := hub.New(cfg)
		if err != nil {
			return err
		}
		defer h.Close()
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		return h.Run(ctx)
	}
	var opts map[string]string
	switch command {
	case "invite":
		opts, args = take(args, "for", "expires")
	case "limits":
		opts, args = take(args, "submits", "blob-bytes", "streams")
	case "trunk":
		opts, args = take(args, "for", "expires")
	case "cluster", "rehome":
	case "fingerprint", "invites", "revoke-invite", "servers", "remove", "streams", "status", "channels", "appoint", "dismiss", "expel":
	default:
		fmt.Fprintf(os.Stderr, "churr-hub: there is no command %q\n\n%s", command, usage)
		os.Exit(2)
	}
	h, positional, err := open(args)
	if err != nil {
		return err
	}
	defer h.Close()
	switch command {
	case "fingerprint":
		fmt.Printf("%s\n%s\n", h.Name(), h.Fingerprint())
	case "invite":
		expires := time.Duration(0)
		if opts["expires"] != "" {
			if expires, err = time.ParseDuration(opts["expires"]); err != nil || expires <= 0 {
				return errors.New("--expires takes a duration such as 24h, at most seven days (168h)")
			}
		}
		text, err := h.Invite(opts["for"], expires)
		if err != nil {
			return err
		}
		fmt.Println(text)
	case "invites":
		list, err := h.Invites()
		if err != nil {
			return err
		}
		for _, in := range list {
			who := "any server"
			if in.For != "" {
				who = in.For
			}
			fmt.Printf("%s  for %s  expires %s\n", in.ID, who, in.ExpiresAt)
		}
	case "revoke-invite":
		if len(positional) != 1 {
			return errors.New("say which invitation: churr-hub revoke-invite ID")
		}
		return h.RevokeInvite(positional[0])
	case "servers":
		list, err := h.Servers()
		if err != nil {
			return err
		}
		for _, sv := range list {
			// Whether a server is connected is known to the serving
			// process, not to this one.
			fmt.Printf("%s  %s  streams %d  last day: %d envelopes, %d bytes\n    %s\n", sv.Name, sv.Fingerprint, sv.Streams, sv.Envelopes, sv.Bytes,
				hub.ClockText(sv.Clock))
		}
	case "remove":
		if len(positional) != 1 {
			return errors.New("say which server: churr-hub remove NAME")
		}
		return h.Remove(positional[0])
	case "channels":
		list, err := h.Channels()
		if err != nil {
			return err
		}
		for _, c := range list {
			// A channel whose home is another hub of the cluster is listed
			// too, with its home: this hub carries it or lists it.
			listing := c.Listing
			if c.Private {
				listing += " (private)"
			}
			fmt.Printf("%s  %q  %s  home %s  servers %s  managers %s  moderators %s  invited %s  asking %s  key-maker %s  carried %d\n    %s\n", c.ID, c.Name, listing, c.Home,
				strings.Join(c.Servers, ","), hub.Accounts(c.Managers), hub.Accounts(c.Moderators), strings.Join(c.Invited, ","), strings.Join(c.Requests, ","), c.KeyMaker, c.Cursor, c.Description)
		}
	case "appoint":
		if len(positional) != 3 {
			return errors.New("usage: churr-hub appoint STREAM ACCOUNT-ID USERNAME@SERVER")
		}
		return h.Appoint(positional[0], positional[1], positional[2])
	case "dismiss":
		if len(positional) != 2 {
			return errors.New("usage: churr-hub dismiss STREAM ACCOUNT-ID")
		}
		return h.Dismiss(positional[0], positional[1])
	case "expel":
		if len(positional) != 2 {
			return errors.New("usage: churr-hub expel STREAM SERVER")
		}
		return h.Expel(positional[0], positional[1])
	case "streams":
		list, err := h.Streams()
		if err != nil {
			return err
		}
		for _, st := range list {
			fmt.Printf("%s  owner %s  home %s  servers %s  log position %d  log %d bytes\n", st.ID, st.Owner, st.HomeHub, strings.Join(st.Servers, ","), st.Cursor, st.LogBytes)
		}
	case "limits":
		name := ""
		if len(positional) > 0 {
			name = positional[0]
		}
		submits, err := number(opts["submits"], "--submits")
		if err != nil {
			return err
		}
		blob, err := number(opts["blob-bytes"], "--blob-bytes")
		if err != nil {
			return err
		}
		streams, err := number(opts["streams"], "--streams")
		if err != nil {
			return err
		}
		if submits+blob+streams > 0 {
			if err := h.SetLimits(name, int(submits), blob, streams); err != nil {
				return err
			}
		}
		l, err := h.Limits(name)
		if err != nil {
			return err
		}
		fmt.Printf("envelopes a minute: %d\nfile bytes an hour: %d\nstreams owned: %d\n", l.Submits, l.BlobBytes, l.Streams)
	case "status":
		fmt.Print(h.Status().Text())
	case "trunk":
		if len(positional) == 0 {
			return errors.New("usage: churr-hub trunk invite | join INVITATION | cancel")
		}
		switch positional[0] {
		case "invite":
			expires := time.Duration(0)
			if opts["expires"] != "" {
				if expires, err = time.ParseDuration(opts["expires"]); err != nil || expires <= 0 {
					return errors.New("--expires takes a duration such as 24h, at most seven days (168h)")
				}
			}
			text, err := h.TrunkInvite(opts["for"], expires)
			if err != nil {
				return err
			}
			fmt.Println(text)
		case "join":
			if len(positional) != 2 {
				return errors.New("usage: churr-hub trunk join INVITATION")
			}
			if err := h.TrunkJoin(positional[1]); err != nil {
				return err
			}
			fmt.Println("The running hub dials within a second. \"churr-hub cluster\" shows when it has been let in.")
		case "cancel":
			return h.TrunkCancel()
		default:
			return errors.New("usage: churr-hub trunk invite | join INVITATION | cancel")
		}
	case "cluster":
		switch {
		case len(positional) == 1 && positional[0] == "leave":
			if err := h.ClusterLeave(); err != nil {
				return err
			}
			// The serving hub does the telling. If it is not running, or its
			// trunk is down, the hub it joined through still lists it until
			// this hub next reaches it; nothing more need be run.
			fmt.Println("This hub is out of its cluster. The hub it joined through is told at once if \"churr-hub serve\" is running and can reach it, and otherwise as soon as it can; until then that hub still lists this one.")
			return nil
		case len(positional) == 2 && positional[0] == "remove":
			return h.ClusterRemove(positional[1])
		case len(positional) != 0:
			return errors.New("usage: churr-hub cluster [leave | remove HUB]")
		}
		list, err := h.Cluster()
		if err != nil {
			return err
		}
		for i, hb := range list {
			line := hb.Name + "  " + hb.Fingerprint
			switch {
			case i == 0:
				line += "  this hub"
			case hb.Fingerprint == "":
				line += "  not a member yet"
			}
			if hb.Via != "" {
				line += "  joined through " + hb.Via
			}
			if hb.Trunk != "" {
				line += "  trunk " + hb.Trunk
			}
			if i > 0 && hb.Known {
				line += fmt.Sprintf("  clock %+d ms ± %d against it", hb.Offset, hb.Uncertainty)
			}
			if i > 0 {
				line += fmt.Sprintf("  servers %d", hb.Servers)
			}
			fmt.Println(line)
		}
	case "rehome":
		if len(positional) != 2 {
			return errors.New("usage: churr-hub rehome STREAM HUB")
		}
		if err := h.Rehome(positional[0], positional[1]); err != nil {
			return err
		}
		// The running hub does it; this process waits to say how it went.
		for i := 0; i < 50; i++ {
			time.Sleep(200 * time.Millisecond)
			switch got := h.RehomeResult(positional[0]); {
			case got == "done":
				fmt.Printf("The channel's home is now %s.\n", positional[1])
				return nil
			case strings.HasPrefix(got, "failed"):
				return errors.New("the home was not moved: " + strings.TrimPrefix(got, "failed: "))
			}
		}
		return errors.New("the running hub has not answered; is \"churr-hub serve\" running? The move happens when it next looks")
	}
	return nil
}
