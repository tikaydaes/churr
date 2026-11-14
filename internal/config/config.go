// Package config reads what each of the three programs needs before it can
// open its database: where to listen, its host name, its data directory.
//
// Every flag has an environment variable made from its name
// (docs/OPERATIONS.md section 1): CHURR_ for the server, CHURR_HUB_ for
// the hub, CHURR_RELAY_ for the relay, then the flag in capitals with
// dashes as underscores. A flag on the command line beats its variable,
// which beats the default. There is no configuration file; everything else
// about a server is a setting in its database.
//
// The Parse functions take the arguments after the command word ("serve")
// and a function to read the environment with, normally os.Getenv. They
// return an error that is a plain sentence fit to print as it is, or
// flag.ErrHelp after writing the list of flags when -h was given.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"path/filepath"
	"strings"
)

// Defaults that are named in more than one place.
const (
	// DefaultACMEDirectory is Let's Encrypt's production directory.
	DefaultACMEDirectory = "https://acme-v02.api.letsencrypt.org/directory"

	// DefaultTrustedProxies is loopback, written as the documents write it.
	DefaultTrustedProxies = "127.0.0.1/8,::1/128"

	// The ports of docs/OPERATIONS.md section 18.
	defaultHTTP       = ":80"
	defaultHTTPS      = ":443"
	defaultServeProxy = "127.0.0.1:8472"
	defaultHubProxy   = "127.0.0.1:8473"
	defaultRelayProxy = "127.0.0.1:8474"
	defaultHubStatus  = "127.0.0.1:8475"
	defaultMedia      = ":24877"
	defaultDev        = "localhost:8472"

	// off is the word that switches a listener off.
	off = "off"

	// DefaultAPNsTopic is the iPhone app's bundle identifier.
	//
	// TODO(config-apns-topic): the apps' identifier has not been chosen
	// (<APP-ID-NOT-CHOSEN> in docs/OPERATIONS.md). Until it is, there is no
	// default, and a relay given an Apple key must be given -apns-topic too.
	DefaultAPNsTopic = ""
)

// Common is what all three programs share: where they listen, who they are
// to the outside, how they get a certificate and how they log.
type Common struct {
	// Data is the data directory.
	Data string
	// Hostname is the public host name, in lower case.
	Hostname string
	// BehindProxy means a reverse proxy provides HTTPS: the program serves
	// plain HTTP, fetches no certificate, and believes the forwarding
	// headers of TrustedProxies.
	BehindProxy bool
	// HTTP is where to listen for plain HTTP. Alone, that port serves
	// certificate challenges and redirects; behind a proxy it is the whole
	// service.
	HTTP string
	// HTTPS is where to listen for HTTPS. It is empty when the program
	// serves no HTTPS itself: behind a proxy, and in a local trial.
	HTTPS string
	// TrustedProxies are the addresses whose X-Forwarded-For and
	// X-Forwarded-Proto are believed. It is empty unless BehindProxy.
	TrustedProxies []netip.Prefix
	// TLSCert and TLSKey name a certificate and key to use instead of
	// fetching one. Both are set or both are empty.
	TLSCert, TLSKey string
	// ACMEDirectory is the certificate authority's ACME address.
	ACMEDirectory string
	// LogLevel is debug, info, warn or error.
	LogLevel string
	// LogFormat is text or json.
	LogFormat string
}

// CertsDir is where a fetched certificate and the ACME account are kept.
func (c Common) CertsDir() string { return filepath.Join(c.Data, "certs") }

// SlogLevel is LogLevel as log/slog wants it.
func (c Common) SlogLevel() slog.Level {
	switch c.LogLevel {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	}
	return slog.LevelInfo
}

// Serve is the configuration of `churr serve`.
type Serve struct {
	Common
	// DB is empty for SQLite in the data directory, or a postgres://
	// address.
	DB string
	// Media is where to listen for call media, on UDP and TCP both. It is
	// empty when calls are off (-media off).
	Media string
	// MediaAddress lists the public IP addresses, or a host name, that
	// clients send call media to. Empty means whatever Hostname resolves
	// to, which is looked up by the calls code and not here.
	MediaAddress []string
	// ClockSynced is the owner's declaration that this server's clock and
	// those of the servers linked to it directly are synchronised
	// (docs/LINK-PROTOCOL.md section 5). It is never worked out by the
	// program.
	ClockSynced bool
	// UnifiedPushAllowPrivate allows UnifiedPush endpoints on private
	// addresses.
	UnifiedPushAllowPrivate bool
	// Dev is a local trial: plain HTTP on localhost, no certificate.
	Dev bool
}

// Hub is the configuration of `churr-hub serve`.
type Hub struct {
	Common
	// Status is where the status page listens. It is empty when the page
	// is off (-status off).
	Status string
	// KeepDays is how long envelopes are kept for servers that were
	// offline.
	KeepDays int
	// ServerRate is how many envelopes a second are accepted from one
	// server.
	ServerRate int
	// MaxEnvelope is the largest envelope accepted, in bytes.
	MaxEnvelope int
	// ClockSynced is the operator's declaration that the hub's clock and
	// its servers' clocks are synchronised (docs/LINK-PROTOCOL.md section
	// 5). It is never worked out by the program.
	ClockSynced bool
}

// Relay is the configuration of `churr-relay serve`.
type Relay struct {
	Common
	// APNsKey is the path to Apple's .p8 signing key, APNsKeyID that key's
	// identifier and APNsTeamID the developer team. All three are set, or
	// none is and the relay answers iPhone wake-ups with "platform off".
	APNsKey, APNsKeyID, APNsTeamID string
	// APNsTopic is the iPhone app's bundle identifier.
	APNsTopic string
	// APNsSandbox selects Apple's development push service.
	APNsSandbox bool
	// FCMCredentials is the path to the Google service account file, or
	// empty, in which case Android wake-ups are answered "platform off".
	FCMCredentials string
	// DeviceRate is wake-ups a minute per push address, and DeviceDaily
	// wake-ups a day.
	DeviceRate, DeviceDaily int
	// AddressRate is requests a minute per calling address.
	AddressRate int
	// MaxInflight is how many requests may be in progress at once.
	MaxInflight int
	// DryRun is for development: the relay accepts and counts every
	// well-formed wake-up and sends nothing to Apple or Google, whatever
	// keys it has. Without it, a platform with no key answers "platform
	// off" (docs/PUSH.md section 5).
	DryRun bool
}

// ParseServe reads the flags of `churr serve`, writing help to standard
// error if it is asked for.
func ParseServe(args []string, env func(string) string) (Serve, error) {
	return Options{}.ParseServe(args, env)
}

// ParseHub reads the flags of `churr-hub serve`, writing help to standard
// error if it is asked for.
func ParseHub(args []string, env func(string) string) (Hub, error) {
	return Options{}.ParseHub(args, env)
}

// ParseRelay reads the flags of `churr-relay serve`, writing help to
// standard error if it is asked for.
func ParseRelay(args []string, env func(string) string) (Relay, error) {
	return Options{}.ParseRelay(args, env)
}

// common holds the flags every program has, between defining them and
// reading them.
type common struct {
	data, hostname, http, https, trusted *string
	tlsCert, tlsKey, acme, level, format *string
	behindProxy                          *bool
}

// The flags are added in the order the tables of docs/OPERATIONS.md list
// them, which is the order -h shows them in. So the shared ones come in
// three groups, with each program's own flags in between.

func (p *parser) addData(def0, meaning string) *string {
	return p.text("data", "DIR", def0, "", meaning)
}

func (p *parser) addHostname(shown, meaning string) *string {
	return p.text("hostname", "NAME", "", shown, meaning)
}

func (p *parser) addListeners(c *common, proxyHTTP string) {
	c.behindProxy = p.toggle("behind-proxy",
		"A reverse proxy provides HTTPS. Serve plain HTTP, fetch no certificate, and believe the proxy's forwarding headers.")
	c.http = p.text("http", "ADDRESS", "", defaultHTTP+", or "+proxyHTTP+" behind a proxy",
		"Where to listen for HTTP. Alone, this serves certificate challenges and redirects to HTTPS. Behind a proxy, this is the whole service.")
	c.https = p.text("https", "ADDRESS", defaultHTTPS, "",
		"Where to listen for HTTPS. Not used behind a proxy.")
	c.trusted = p.text("trusted-proxies", "LIST", DefaultTrustedProxies, "",
		"Addresses or ranges whose X-Forwarded-For and X-Forwarded-Proto are believed, separated by commas. Only with -behind-proxy.")
}

func (p *parser) addCertificates(c *common) {
	c.tlsCert = p.text("tls-cert", "FILE", "", "",
		"Use this certificate instead of fetching one. Goes with -tls-key. Read again when the file changes.")
	c.tlsKey = p.text("tls-key", "FILE", "", "",
		"The private key for -tls-cert.")
	c.acme = p.text("acme-directory", "URL", DefaultACMEDirectory, "Let's Encrypt",
		"The certificate authority's ACME address.")
}

func (p *parser) addLogging(c *common) {
	c.level = p.text("log-level", "LEVEL", "info", "", "debug, info, warn or error.")
	c.format = p.text("log-format", "FORMAT", "text", "", "text or json.")
}

// finish checks the shared flags and works out the defaults that depend on
// other flags. proxyHTTP is where the program listens behind a proxy when
// -http does not say. localTrial is true for `churr serve -dev`, which
// listens on localhost and, like a proxied server, has no HTTPS listener.
func (p *parser) finish(c *common, proxyHTTP string, hostnameRequired, localTrial bool) (Common, error) {
	out := Common{
		Data:          *c.data,
		BehindProxy:   *c.behindProxy,
		HTTP:          *c.http,
		HTTPS:         *c.https,
		TLSCert:       *c.tlsCert,
		TLSKey:        *c.tlsKey,
		ACMEDirectory: *c.acme,
		LogLevel:      *c.level,
		LogFormat:     *c.format,
	}
	if out.Data == "" {
		return out, fmt.Errorf("%s cannot be empty: it names the data directory.", p.label("data"))
	}

	if *c.hostname == "" {
		if hostnameRequired {
			return out, fmt.Errorf("-hostname is required: the public host name, such as chat.example.com. "+
				"It can also be set as %s.", p.envName("hostname"))
		}
	} else {
		host, err := p.hostname("hostname", *c.hostname)
		if err != nil {
			return out, err
		}
		out.Hostname = host
	}

	// Where plain HTTP is served depends on who is in front.
	if !p.given("http") {
		switch {
		case localTrial:
			out.HTTP = defaultDev
		case out.BehindProxy:
			out.HTTP = proxyHTTP
		default:
			out.HTTP = defaultHTTP
		}
	}
	if err := p.listen("http", out.HTTP); err != nil {
		return out, err
	}
	if out.BehindProxy || localTrial {
		// Nothing listens for HTTPS, so the field says so, whatever -https
		// was given: the documents call it "not used" here, not wrong.
		out.HTTPS = ""
	} else if err := p.listen("https", out.HTTPS); err != nil {
		return out, err
	}

	if out.BehindProxy {
		proxies, err := p.proxies("trusted-proxies", *c.trusted)
		if err != nil {
			return out, err
		}
		out.TrustedProxies = proxies
	} else if p.given("trusted-proxies") {
		// Believing forwarding headers while facing the network directly
		// would let anyone choose the address the server sees them at.
		return out, fmt.Errorf("%s only has a meaning with -behind-proxy. Without a proxy in front, nobody's forwarding headers are believed.",
			p.label("trusted-proxies"))
	}

	if (out.TLSCert == "") != (out.TLSKey == "") {
		return out, errors.New("-tls-cert and -tls-key go together: give both, or neither to have a certificate fetched.")
	}
	if err := p.webAddress("acme-directory", out.ACMEDirectory); err != nil {
		return out, err
	}
	if err := p.oneOf("log-level", out.LogLevel, "debug", "info", "warn", "error"); err != nil {
		return out, err
	}
	if err := p.oneOf("log-format", out.LogFormat, "text", "json"); err != nil {
		return out, err
	}
	return out, nil
}

// ParseServe reads the flags of `churr serve`.
func (o Options) ParseServe(args []string, env func(string) string) (Serve, error) {
	p := newParser("churr serve", "CHURR_")
	var c common
	c.data = p.addData("./data", "The data directory. Made if missing.")
	c.hostname = p.addHostname("none; required",
		"The server's public host name, such as chat.example.com. It is the server's name to other servers and what passkeys are tied to. Do not change it later.")
	db := p.text("db", "URL", "", "empty",
		"Empty means SQLite at churr.db in the data directory. A postgres:// address means PostgreSQL.")
	p.addListeners(&c, defaultServeProxy)
	media := p.text("media", "ADDRESS", defaultMedia, "",
		"Where to listen for call media, on UDP and TCP both. \"off\" runs without calls.")
	mediaAddress := p.text("media-address", "LIST", "", "the addresses -hostname resolves to",
		"The public IP addresses, or a host name, that clients send call media to. Comma-separated.")
	p.addCertificates(&c)
	unifiedPush := p.toggle("unifiedpush-allow-private",
		"Allow UnifiedPush endpoints on private addresses.")
	clockSynced := p.toggle("clock-synced",
		"Declare that this server's clock and the clocks of the servers linked to it directly are synchronised (by NTP, say). A measured clock offset beyond 10 ms is then put down to the network path and reported, not applied. Never assumed: leave it out unless you know.")
	p.addLogging(&c)
	dev := p.toggle("dev",
		"For trying it out: plain HTTP on localhost:8472, host name localhost, no certificate, data in ./data. Never use it on a network.")

	if err := p.parse(args, env, o); err != nil {
		return Serve{}, err
	}

	if *dev {
		// A local trial changes defaults only. Whatever the operator wrote
		// stands, unless it cannot be true of a local trial at all, and
		// then it is refused rather than quietly dropped.
		if *c.behindProxy {
			return Serve{}, fmt.Errorf("%s and %s cannot be used together: a local trial is reached directly at http://localhost.",
				p.label("dev"), p.label("behind-proxy"))
		}
		if *c.tlsCert != "" || *c.tlsKey != "" {
			return Serve{}, fmt.Errorf("%s serves plain HTTP and uses no certificate, so -tls-cert and -tls-key cannot be used with it.",
				p.label("dev"))
		}
		if !p.given("hostname") {
			*c.hostname = "localhost"
		}
	}

	shared, err := p.finish(&c, defaultServeProxy, true, *dev)
	if err != nil {
		return Serve{}, err
	}
	cfg := Serve{
		Common:                  shared,
		DB:                      *db,
		UnifiedPushAllowPrivate: *unifiedPush,
		ClockSynced:             *clockSynced,
		Dev:                     *dev,
	}

	if cfg.Dev && cfg.Hostname != "localhost" && !strings.HasSuffix(cfg.Hostname, ".localhost") {
		// Browsers allow passkeys over plain HTTP only at localhost and
		// names under it. Anywhere else a trial server could not be signed
		// in to, and its cookies would cross a network in the clear.
		return Serve{}, fmt.Errorf("%s is a local trial and works only at localhost or a name ending in .localhost, not %q. "+
			"For a real host name, leave -dev out.", p.label("dev"), cfg.Hostname)
	}

	if cfg.DB != "" && !strings.HasPrefix(cfg.DB, "postgres://") && !strings.HasPrefix(cfg.DB, "postgresql://") {
		// The value may hold a password, so it is not repeated.
		return Serve{}, fmt.Errorf("%s must be empty, for SQLite in the data directory, or a postgres:// address.", p.label("db"))
	}

	if *media != off {
		if err := p.listen("media", *media); err != nil {
			return Serve{}, fmt.Errorf("%s Or \"off\" to run without calls.", err)
		}
		cfg.Media = *media
	}
	for _, part := range strings.Split(*mediaAddress, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if _, err := netip.ParseAddr(part); err != nil && !validHostname(strings.ToLower(part)) {
			return Serve{}, fmt.Errorf("%s must be IP addresses or a host name, separated by commas. %q is neither.",
				p.label("media-address"), part)
		}
		cfg.MediaAddress = append(cfg.MediaAddress, part)
	}
	return cfg, nil
}

// Origin is the server's own origin, the one passkeys are checked against
// (docs/AUTH.md section 5.1) and the start of every link the server prints:
// https://<host name>, with the port if it is not 443. Behind a proxy the
// public side is the proxy's, so it is https://<host name> whatever port
// the server itself listens on. In a local trial it is
// http://localhost:<port>.
//
// A listener on port 0, which a test may use to get any free port, has no
// origin that can be known from here.
func (s Serve) Origin() string {
	switch {
	case s.Dev:
		return "http://" + s.Hostname + portSuffix(s.HTTP, "80")
	case s.BehindProxy:
		return "https://" + s.Hostname
	default:
		return "https://" + s.Hostname + portSuffix(s.HTTPS, "443")
	}
}

// Postgres reports whether the database is PostgreSQL and not SQLite.
func (s Serve) Postgres() bool { return s.DB != "" }

// DBPath is the SQLite file: churr.db in the data directory. It is empty
// with PostgreSQL, when there is no such file.
func (s Serve) DBPath() string {
	if s.Postgres() {
		return ""
	}
	return filepath.Join(s.Data, "churr.db")
}

// KeysDir is where the key files are (see package secret).
func (s Serve) KeysDir() string { return filepath.Join(s.Data, "keys") }

// FilesDir is where uploaded files are kept when they are kept on disk.
func (s Serve) FilesDir() string { return filepath.Join(s.Data, "files") }

// TmpDir is where uploads in progress are written. It is emptied at start.
func (s Serve) TmpDir() string { return filepath.Join(s.Data, "tmp") }

// SecureCookies reports whether cookies carry the Secure attribute. They
// always do, except in a local trial at http://localhost, where a browser
// would otherwise never send them back (docs/AUTH.md section 10.2).
func (s Serve) SecureCookies() bool { return !s.Dev }

// ParseHub reads the flags of `churr-hub serve`.
func (o Options) ParseHub(args []string, env func(string) string) (Hub, error) {
	p := newParser("churr-hub serve", "CHURR_HUB_")
	var c common
	c.data = p.addData("./hub-data", "Data directory: hub.db, keys/, certs/.")
	c.hostname = p.addHostname("none; required", "The hub's public host name.")
	p.addListeners(&c, defaultHubProxy)
	status := p.text("status", "ADDRESS", defaultHubStatus, "",
		"Where to serve the status page. It shows totals only (servers joined and connected, streams open, channels by listing mode, the cluster, envelopes carried, the clocks measured) and names no server, stream or channel. It is plain HTTP and listens on localhost unless told otherwise. \"off\" disables it.")
	keepDays := p.text("keep-days", "DAYS", "30", "",
		"How long envelopes are kept for servers that were offline.")
	serverRate := p.text("server-rate", "N", "50", "",
		"Envelopes a second accepted from one server.")
	maxEnvelope := p.text("max-envelope", "BYTES", "1048576", "",
		"Largest envelope, in bytes.")
	clockSynced := p.toggle("clock-synced",
		"Declare that this hub's clock and the clocks of its servers are synchronised (by NTP, say). A measured clock offset beyond 10 ms is then put down to the network path and reported, not applied. Never assumed: leave it out unless you know.")
	p.addCertificates(&c)
	p.addLogging(&c)

	if err := p.parse(args, env, o); err != nil {
		return Hub{}, err
	}
	shared, err := p.finish(&c, defaultHubProxy, true, false)
	if err != nil {
		return Hub{}, err
	}
	cfg := Hub{Common: shared}

	if *status != off {
		if err := p.listen("status", *status); err != nil {
			return Hub{}, fmt.Errorf("%s Or \"off\" for no status page.", err)
		}
		cfg.Status = *status
	}
	if cfg.KeepDays, err = p.number("keep-days", *keepDays); err != nil {
		return Hub{}, err
	}
	if cfg.ServerRate, err = p.number("server-rate", *serverRate); err != nil {
		return Hub{}, err
	}
	if cfg.MaxEnvelope, err = p.number("max-envelope", *maxEnvelope); err != nil {
		return Hub{}, err
	}
	cfg.ClockSynced = *clockSynced
	return cfg, nil
}

// DBPath is the hub's SQLite file: hub.db in the data directory.
func (h Hub) DBPath() string { return filepath.Join(h.Data, "hub.db") }

// KeysDir is where the hub's key files are.
func (h Hub) KeysDir() string { return filepath.Join(h.Data, "keys") }

// ParseRelay reads the flags of `churr-relay serve`.
func (o Options) ParseRelay(args []string, env func(string) string) (Relay, error) {
	p := newParser("churr-relay serve", "CHURR_RELAY_")
	var c common
	c.hostname = p.addHostname("none; required unless behind a proxy", "The relay's public host name.")
	p.addListeners(&c, defaultRelayProxy)
	c.data = p.addData("./relay-data",
		"Holds certs/ only, and only when the relay fetches its own certificate.")
	apnsKey := p.text("apns-key", "FILE", "", "", "Path to Apple's .p8 signing key.")
	apnsKeyID := p.text("apns-key-id", "ID", "", "", "That key's identifier.")
	apnsTeamID := p.text("apns-team-id", "ID", "", "", "The Apple developer team identifier.")
	apnsTopic := p.text("apns-topic", "ID", DefaultAPNsTopic, "none yet; the app identifier is not chosen",
		"The iPhone app's bundle identifier.")
	apnsSandbox := p.toggle("apns-sandbox", "Use Apple's development push service.")
	fcm := p.text("fcm-credentials", "FILE", "", "", "Path to the Google service account file.")
	deviceRate := p.text("device-rate", "N", "60", "", "Wake-ups a minute per push address.")
	deviceDaily := p.text("device-daily", "N", "2000", "", "Wake-ups a day per push address.")
	addressRate := p.text("address-rate", "N", "600", "", "Requests a minute per calling address.")
	maxInflight := p.text("max-inflight", "N", "2000", "", "Requests in progress at once.")
	dryRun := p.toggle("dry-run", "For development: accept and count wake-ups, and send nothing to Apple or Google.")
	p.addCertificates(&c)
	p.addLogging(&c)

	if err := p.parse(args, env, o); err != nil {
		return Relay{}, err
	}
	// Behind a proxy the relay never needs its own name: the proxy holds
	// the certificate, and the relay has no identity to other servers.
	shared, err := p.finish(&c, defaultRelayProxy, !*c.behindProxy, false)
	if err != nil {
		return Relay{}, err
	}
	cfg := Relay{
		Common:         shared,
		APNsKey:        *apnsKey,
		APNsKeyID:      *apnsKeyID,
		APNsTeamID:     *apnsTeamID,
		APNsTopic:      *apnsTopic,
		APNsSandbox:    *apnsSandbox,
		FCMCredentials: *fcm,
		DryRun:         *dryRun,
	}

	apple := 0
	for _, v := range []string{cfg.APNsKey, cfg.APNsKeyID, cfg.APNsTeamID} {
		if v != "" {
			apple++
		}
	}
	switch {
	case apple != 0 && apple != 3:
		return Relay{}, errors.New("-apns-key, -apns-key-id and -apns-team-id go together: give all three, or none to run without iPhone wake-ups.")
	case apple == 3 && cfg.APNsTopic == "":
		return Relay{}, errors.New("-apns-topic is required with -apns-key: the iPhone app's bundle identifier.")
	}

	if cfg.DeviceRate, err = p.number("device-rate", *deviceRate); err != nil {
		return Relay{}, err
	}
	if cfg.DeviceDaily, err = p.number("device-daily", *deviceDaily); err != nil {
		return Relay{}, err
	}
	if cfg.AddressRate, err = p.number("address-rate", *addressRate); err != nil {
		return Relay{}, err
	}
	if cfg.MaxInflight, err = p.number("max-inflight", *maxInflight); err != nil {
		return Relay{}, err
	}
	return cfg, nil
}
