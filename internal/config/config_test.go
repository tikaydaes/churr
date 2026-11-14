package config

import (
	"bytes"
	"errors"
	"flag"
	"log/slog"
	"net/netip"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// env makes an environment out of pairs: env("CHURR_DATA", "/d").
func env(pairs ...string) func(string) string {
	m := map[string]string{}
	for i := 0; i+1 < len(pairs); i += 2 {
		m[pairs[i]] = pairs[i+1]
	}
	return func(k string) string { return m[k] }
}

func args(a ...string) []string { return a }

func serve(t *testing.T, a []string, e func(string) string) Serve {
	t.Helper()
	cfg, err := ParseServe(a, e)
	if err != nil {
		t.Fatalf("ParseServe(%q): %v", a, err)
	}
	return cfg
}

// wantError checks that err is a plain sentence mentioning each of parts.
func wantError(t *testing.T, what string, err error, parts ...string) {
	t.Helper()
	if err == nil {
		t.Errorf("%s: no error", what)
		return
	}
	if errors.Is(err, flag.ErrHelp) {
		t.Errorf("%s: got the help error, want a refusal", what)
		return
	}
	msg := err.Error()
	for _, p := range parts {
		if !strings.Contains(msg, p) {
			t.Errorf("%s: error %q does not mention %q", what, msg, p)
		}
	}
	if !strings.HasSuffix(msg, ".") || strings.Contains(msg, "\n") {
		t.Errorf("%s: error is not a plain sentence: %q", what, msg)
	}
}

func prefixes(t *testing.T, list ...string) []netip.Prefix {
	t.Helper()
	var out []netip.Prefix
	for _, s := range list {
		out = append(out, netip.MustParsePrefix(s))
	}
	return out
}

func TestServeDefaults(t *testing.T) {
	cfg := serve(t, args("-hostname", "chat.example.com"), nil)
	want := Serve{
		Common: Common{
			Data:          "./data",
			Hostname:      "chat.example.com",
			HTTP:          ":80",
			HTTPS:         ":443",
			ACMEDirectory: "https://acme-v02.api.letsencrypt.org/directory",
			LogLevel:      "info",
			LogFormat:     "text",
		},
		Media: ":24877",
	}
	if !reflect.DeepEqual(cfg, want) {
		t.Errorf("defaults:\n got %+v\nwant %+v", cfg, want)
	}
}

func TestServeDefaultsBehindProxy(t *testing.T) {
	cfg := serve(t, args("-behind-proxy", "-hostname", "chat.example.com"), nil)
	if cfg.HTTP != "127.0.0.1:8472" {
		t.Errorf("HTTP = %q, want 127.0.0.1:8472", cfg.HTTP)
	}
	if cfg.HTTPS != "" {
		t.Errorf("HTTPS = %q, want none behind a proxy", cfg.HTTPS)
	}
	if want := prefixes(t, "127.0.0.0/8", "::1/128"); !reflect.DeepEqual(cfg.TrustedProxies, want) {
		t.Errorf("TrustedProxies = %v, want %v", cfg.TrustedProxies, want)
	}
	if !cfg.BehindProxy {
		t.Error("BehindProxy is false")
	}
	// -https is "not used behind a proxy": accepted and left unused.
	cfg = serve(t, args("-behind-proxy", "-hostname", "chat.example.com", "-https", ":8443"), nil)
	if cfg.HTTPS != "" {
		t.Errorf("HTTPS = %q with -https given behind a proxy, want none", cfg.HTTPS)
	}
}

func TestServeEveryFlag(t *testing.T) {
	cfg := serve(t, args(
		"-data", "/var/lib/churr",
		"-hostname", "Chat.Example.COM",
		"-db", "postgres://churr:pw@db:5432/churr",
		"-behind-proxy",
		"-http", ":8080",
		"-https", ":8443",
		"-trusted-proxies", "172.16.0.0/12, 10.0.0.5,fd00::/8",
		"-media", ":30000",
		"-media-address", "203.0.113.20, 2001:db8::1,media.example.com",
		"-tls-cert", "/etc/c.pem", "-tls-key", "/etc/k.pem",
		"-acme-directory", "https://acme.example/dir",
		"-unifiedpush-allow-private",
		"-log-level", "debug",
		"-log-format", "json",
	), nil)
	want := Serve{
		Common: Common{
			Data:           "/var/lib/churr",
			Hostname:       "chat.example.com",
			BehindProxy:    true,
			HTTP:           ":8080",
			HTTPS:          "",
			TrustedProxies: prefixes(t, "172.16.0.0/12", "10.0.0.5/32", "fd00::/8"),
			TLSCert:        "/etc/c.pem",
			TLSKey:         "/etc/k.pem",
			ACMEDirectory:  "https://acme.example/dir",
			LogLevel:       "debug",
			LogFormat:      "json",
		},
		DB:                      "postgres://churr:pw@db:5432/churr",
		Media:                   ":30000",
		MediaAddress:            []string{"203.0.113.20", "2001:db8::1", "media.example.com"},
		UnifiedPushAllowPrivate: true,
	}
	if !reflect.DeepEqual(cfg, want) {
		t.Errorf("every flag:\n got %+v\nwant %+v", cfg, want)
	}
}

func TestServeEveryEnvironmentVariable(t *testing.T) {
	// The same configuration as TestServeEveryFlag, with nothing on the
	// command line. Every name is the flag in capitals after CHURR_.
	cfg := serve(t, nil, env(
		"CHURR_DATA", "/var/lib/churr",
		"CHURR_HOSTNAME", "chat.example.com",
		"CHURR_DB", "postgres://churr:pw@db:5432/churr",
		"CHURR_BEHIND_PROXY", "true",
		"CHURR_HTTP", ":8080",
		"CHURR_HTTPS", ":8443",
		"CHURR_TRUSTED_PROXIES", "172.16.0.0/12",
		"CHURR_MEDIA", ":30000",
		"CHURR_MEDIA_ADDRESS", "203.0.113.20",
		"CHURR_TLS_CERT", "/etc/c.pem",
		"CHURR_TLS_KEY", "/etc/k.pem",
		"CHURR_ACME_DIRECTORY", "https://acme.example/dir",
		"CHURR_UNIFIEDPUSH_ALLOW_PRIVATE", "true",
		"CHURR_LOG_LEVEL", "warn",
		"CHURR_LOG_FORMAT", "json",
		"CHURR_DEV", "false",
	))
	want := Serve{
		Common: Common{
			Data:           "/var/lib/churr",
			Hostname:       "chat.example.com",
			BehindProxy:    true,
			HTTP:           ":8080",
			TrustedProxies: prefixes(t, "172.16.0.0/12"),
			TLSCert:        "/etc/c.pem",
			TLSKey:         "/etc/k.pem",
			ACMEDirectory:  "https://acme.example/dir",
			LogLevel:       "warn",
			LogFormat:      "json",
		},
		DB:                      "postgres://churr:pw@db:5432/churr",
		Media:                   ":30000",
		MediaAddress:            []string{"203.0.113.20"},
		UnifiedPushAllowPrivate: true,
	}
	if !reflect.DeepEqual(cfg, want) {
		t.Errorf("every variable:\n got %+v\nwant %+v", cfg, want)
	}
}

func TestPrecedence(t *testing.T) {
	e := env("CHURR_HOSTNAME", "env.example.com", "CHURR_DATA", "/from-env", "CHURR_LOG_LEVEL", "warn")

	// The environment beats the default.
	cfg := serve(t, nil, e)
	if cfg.Hostname != "env.example.com" || cfg.Data != "/from-env" || cfg.LogLevel != "warn" {
		t.Errorf("environment over default: %+v", cfg.Common)
	}
	// A flag beats the environment, and only for the flag that was given.
	cfg = serve(t, args("-hostname", "flag.example.com", "-log-level", "error"), e)
	if cfg.Hostname != "flag.example.com" || cfg.LogLevel != "error" {
		t.Errorf("flag over environment: hostname %q, level %q", cfg.Hostname, cfg.LogLevel)
	}
	if cfg.Data != "/from-env" {
		t.Errorf("Data = %q; a flag for something else must not undo the environment", cfg.Data)
	}
	// A flag set to its default value still beats the environment.
	cfg = serve(t, args("-data", "./data", "-log-level", "info"), e)
	if cfg.Data != "./data" || cfg.LogLevel != "info" {
		t.Errorf("a flag equal to the default lost to the environment: %q, %q", cfg.Data, cfg.LogLevel)
	}

	// Switches: a flag turns off what the environment turned on.
	on := env("CHURR_HOSTNAME", "chat.example.com", "CHURR_BEHIND_PROXY", "true")
	if cfg := serve(t, nil, on); !cfg.BehindProxy {
		t.Error("CHURR_BEHIND_PROXY=true was not read")
	}
	if cfg := serve(t, args("-behind-proxy=false"), on); cfg.BehindProxy {
		t.Error("-behind-proxy=false lost to CHURR_BEHIND_PROXY=true")
	}
	offEnv := env("CHURR_HOSTNAME", "chat.example.com", "CHURR_BEHIND_PROXY", "false")
	if cfg := serve(t, args("-behind-proxy"), offEnv); !cfg.BehindProxy {
		t.Error("-behind-proxy lost to CHURR_BEHIND_PROXY=false")
	}
}

func TestEmptyEnvironmentVariableIsUnset(t *testing.T) {
	// compose.yaml passes CHURR_DB= and CHURR_MEDIA_ADDRESS= when left blank.
	cfg := serve(t, nil, env("CHURR_HOSTNAME", "chat.example.com", "CHURR_DB", "", "CHURR_MEDIA_ADDRESS", "", "CHURR_DATA", ""))
	if cfg.DB != "" || cfg.MediaAddress != nil || cfg.Data != "./data" {
		t.Errorf("empty variables were not treated as unset: %+v", cfg)
	}
}

func TestOtherProgramsVariablesAreNotRead(t *testing.T) {
	e := env("CHURR_HUB_HOSTNAME", "hub.example.com", "CHURR_RELAY_HOSTNAME", "relay.example.com")
	if _, err := ParseServe(nil, e); err == nil {
		t.Error("churr serve took its host name from another program's variable")
	}
	e = env("CHURR_HOSTNAME", "chat.example.com")
	if _, err := ParseHub(nil, e); err == nil {
		t.Error("churr-hub took its host name from CHURR_HOSTNAME")
	}
	if _, err := ParseRelay(nil, e); err == nil {
		t.Error("churr-relay took its host name from CHURR_HOSTNAME")
	}
}

func TestSwitchesInTheEnvironmentTakeTrueOrFalseOnly(t *testing.T) {
	for _, v := range []string{"1", "yes", "TRUE", "True", "on", "0", "t"} {
		_, err := ParseServe(nil, env("CHURR_HOSTNAME", "chat.example.com", "CHURR_BEHIND_PROXY", v))
		wantError(t, "CHURR_BEHIND_PROXY="+v, err, "CHURR_BEHIND_PROXY", "true or false", v)
	}
}

func TestDev(t *testing.T) {
	cfg := serve(t, args("-dev"), nil)
	want := Serve{
		Common: Common{
			Data:          "./data",
			Hostname:      "localhost",
			HTTP:          "localhost:8472",
			HTTPS:         "",
			ACMEDirectory: DefaultACMEDirectory,
			LogLevel:      "info",
			LogFormat:     "text",
		},
		// Calls media is left as documented.
		Media: ":24877",
		Dev:   true,
	}
	if !reflect.DeepEqual(cfg, want) {
		t.Errorf("-dev:\n got %+v\nwant %+v", cfg, want)
	}
	if got := cfg.Origin(); got != "http://localhost:8472" {
		t.Errorf("Origin = %q", got)
	}
	if cfg.SecureCookies() {
		t.Error("SecureCookies is true for a local trial at http://localhost")
	}

	// From the environment too.
	cfg = serve(t, nil, env("CHURR_DEV", "true"))
	if !cfg.Dev || cfg.Hostname != "localhost" || cfg.HTTP != "localhost:8472" {
		t.Errorf("CHURR_DEV=true: %+v", cfg.Common)
	}
}

func TestDevKeepsWhatTheOperatorGave(t *testing.T) {
	cfg := serve(t, args("-dev", "-data", "/tmp/trial", "-http", "localhost:9000", "-media", "off", "-log-level", "debug",
		"-db", "postgres://localhost/churr"), nil)
	if cfg.Data != "/tmp/trial" || cfg.HTTP != "localhost:9000" || cfg.Media != "" || cfg.LogLevel != "debug" || !cfg.Postgres() {
		t.Errorf("-dev overrode explicit flags: %+v", cfg)
	}
	if got := cfg.Origin(); got != "http://localhost:9000" {
		t.Errorf("Origin = %q, want the port that was given", got)
	}

	// And what the environment gave.
	cfg = serve(t, args("-dev"), env("CHURR_DATA", "/env-data", "CHURR_HTTP", "127.0.0.1:9100"))
	if cfg.Data != "/env-data" || cfg.HTTP != "127.0.0.1:9100" {
		t.Errorf("-dev overrode the environment: %+v", cfg.Common)
	}

	// A name under localhost is still local, so two trial servers can be
	// told apart when trying out links between servers.
	cfg = serve(t, args("-dev", "-hostname", "a.localhost", "-http", "localhost:9001"), nil)
	if cfg.Hostname != "a.localhost" || cfg.Origin() != "http://a.localhost:9001" {
		t.Errorf("-dev -hostname a.localhost: hostname %q, origin %q", cfg.Hostname, cfg.Origin())
	}
}

func TestDevRefusesWhatCannotBeALocalTrial(t *testing.T) {
	_, err := ParseServe(args("-dev", "-hostname", "chat.example.com"), nil)
	wantError(t, "-dev with a real host name", err, "-dev", "localhost", "chat.example.com")

	_, err = ParseServe(args("-dev", "-behind-proxy"), nil)
	wantError(t, "-dev -behind-proxy", err, "-dev", "-behind-proxy")

	_, err = ParseServe(args("-dev", "-tls-cert", "c.pem", "-tls-key", "k.pem"), nil)
	wantError(t, "-dev with a certificate", err, "-dev", "-tls-cert")

	// The names in the sentence are the ones the operator used.
	_, err = ParseServe(nil, env("CHURR_DEV", "true", "CHURR_BEHIND_PROXY", "true"))
	wantError(t, "CHURR_DEV with CHURR_BEHIND_PROXY", err, "CHURR_DEV", "CHURR_BEHIND_PROXY")
}

func TestServeValidation(t *testing.T) {
	host := []string{"-hostname", "chat.example.com"}
	with := func(more ...string) []string { return append(append([]string{}, host...), more...) }

	cases := []struct {
		what  string
		args  []string
		env   func(string) string
		parts []string
	}{
		{"missing -hostname", nil, nil, []string{"-hostname", "required", "CHURR_HOSTNAME"}},
		{"host name with scheme", args("-hostname", "https://chat.example.com"), nil, []string{"-hostname", "https://"}},
		{"host name with path", args("-hostname", "chat.example.com/chat"), nil, []string{"-hostname", "path"}},
		{"host name with port", args("-hostname", "chat.example.com:443"), nil, []string{"-hostname", "port"}},
		{"IPv4 host name", args("-hostname", "203.0.113.9"), nil, []string{"-hostname", "IP address"}},
		{"IPv6 host name", args("-hostname", "2001:db8::1"), nil, []string{"-hostname", "IP address"}},
		{"host name with space", args("-hostname", "chat example.com"), nil, []string{"-hostname", "not a host name"}},
		{"host name with underscore", args("-hostname", "my_chat.example.com"), nil, []string{"-hostname", "not a host name"}},
		{"host name with trailing dot", args("-hostname", "chat.example.com."), nil, []string{"-hostname", "not a host name"}},
		{"host name with empty label", args("-hostname", "chat..example.com"), nil, []string{"-hostname", "not a host name"}},
		{"host name starting with hyphen", args("-hostname=-chat.example.com"), nil, []string{"-hostname", "not a host name"}},
		{"host name in another script", args("-hostname", "ch\u00e4t.example.com"), nil, []string{"-hostname", "xn--"}},
		{"host name with a long label", args("-hostname", strings.Repeat("a", 64)+".example.com"), nil, []string{"-hostname", "not a host name"}},
		{"bad host name from the environment", nil, env("CHURR_HOSTNAME", "https://x"), []string{"CHURR_HOSTNAME"}},

		{"bad -log-level", with("-log-level", "loud"), nil, []string{"-log-level", "debug, info, warn or error", "loud"}},
		{"upper-case -log-level", with("-log-level", "INFO"), nil, []string{"-log-level", "INFO"}},
		{"bad log level from the environment", host, env("CHURR_LOG_LEVEL", "verbose"), []string{"CHURR_LOG_LEVEL", "verbose"}},
		{"bad -log-format", with("-log-format", "xml"), nil, []string{"-log-format", "text or json", "xml"}},

		{"-trusted-proxies without -behind-proxy", with("-trusted-proxies", "10.0.0.1"), nil, []string{"-trusted-proxies", "-behind-proxy"}},
		{"CHURR_TRUSTED_PROXIES without -behind-proxy", host, env("CHURR_TRUSTED_PROXIES", "172.16.0.0/12"), []string{"CHURR_TRUSTED_PROXIES", "-behind-proxy"}},
		{"the default proxies given explicitly without -behind-proxy", with("-trusted-proxies", DefaultTrustedProxies), nil, []string{"-trusted-proxies", "-behind-proxy"}},
		{"bad -trusted-proxies", with("-behind-proxy", "-trusted-proxies", "10.0.0.1,proxy.internal"), nil, []string{"-trusted-proxies", "proxy.internal"}},
		{"bad range in -trusted-proxies", with("-behind-proxy", "-trusted-proxies", "10.0.0.0/40"), nil, []string{"-trusted-proxies", "10.0.0.0/40"}},
		{"empty list in -trusted-proxies", with("-behind-proxy", "-trusted-proxies", " , "), nil, []string{"-trusted-proxies", "no address"}},

		{"-tls-cert without -tls-key", with("-tls-cert", "c.pem"), nil, []string{"-tls-cert", "-tls-key"}},
		{"-tls-key without -tls-cert", with("-tls-key", "k.pem"), nil, []string{"-tls-cert", "-tls-key"}},

		{"-http without a port", with("-http", "localhost"), nil, []string{"-http", "localhost"}},
		{"-http with a named port", with("-http", ":http"), nil, []string{"-http"}},
		{"-http with a port too large", with("-http", ":70000"), nil, []string{"-http", ":70000"}},
		{"-http that is an address in a browser", with("-http", "http://localhost:80"), nil, []string{"-http"}},
		{"bad -https", with("-https", "443"), nil, []string{"-https", "443"}},
		{"bad -media", with("-media", "24877"), nil, []string{"-media", "24877", "off"}},
		{"bad -media-address", with("-media-address", "203.0.113.20,not a host"), nil, []string{"-media-address", "not a host"}},

		{"-db that is a file", with("-db", "/var/lib/churr/other.db"), nil, []string{"-db", "postgres://"}},
		{"-db for another database", with("-db", "mysql://db/churr"), nil, []string{"-db", "postgres://"}},
		{"empty -data", with("-data", ""), nil, []string{"-data", "data directory"}},
		{"bad -acme-directory", with("-acme-directory", "letsencrypt"), nil, []string{"-acme-directory", "letsencrypt"}},

		{"unknown flag", with("-colour", "pink"), nil, []string{"-colour", "-h"}},
		{"flag without its value", args("-hostname"), nil, []string{"-hostname", "needs a value"}},
		{"switch with a bad value", with("-dev=maybe"), nil, []string{"-dev", "switch", "maybe"}},
		{"a stray argument", with("extra"), nil, []string{"extra"}},
		{"a switch written with a separate value", with("-behind-proxy", "false"), nil, []string{"false", "-name=false"}},
	}
	for _, c := range cases {
		_, err := ParseServe(c.args, c.env)
		wantError(t, c.what, err, c.parts...)
	}
}

func TestDBErrorDoesNotRepeatThePassword(t *testing.T) {
	_, err := ParseServe(args("-hostname", "chat.example.com", "-db", "mysql://churr:hunter2@db/churr"), nil)
	if err == nil || strings.Contains(err.Error(), "hunter2") {
		t.Errorf("the error repeats the database address: %v", err)
	}
}

func TestServeAcceptsWhatItShould(t *testing.T) {
	good := [][]string{
		{"-hostname", "localhost"},
		{"-hostname", "churr"},                   // a single label, as on an intranet
		{"-hostname", "xn--cht-qla.example.com"}, // a name in another script, in its ASCII form
		{"-hostname", "a-b.c-d.example"},
		{"-hostname", "chat.example.com", "-media", "off"},
		{"-hostname", "chat.example.com", "-http", "[::1]:8080"},
		{"-hostname", "chat.example.com", "-http", "0.0.0.0:0"},
		{"-hostname", "chat.example.com", "-db", "postgresql://db/churr"},
		{"--hostname", "chat.example.com", "--log-level=warn"},
		{"-hostname", "chat.example.com", "-behind-proxy", "-tls-cert", "c.pem", "-tls-key", "k.pem"},
	}
	for _, a := range good {
		if _, err := ParseServe(a, nil); err != nil {
			t.Errorf("ParseServe(%q): %v", a, err)
		}
	}
	if cfg := serve(t, args("-hostname", "chat.example.com", "-media", "off"), nil); cfg.Media != "" {
		t.Errorf("Media = %q with -media off, want empty", cfg.Media)
	}
}

func TestHelp(t *testing.T) {
	type parse func(Options) error
	programs := map[string]struct {
		parse  parse
		prefix string
		flags  []string
	}{
		"churr serve": {
			func(o Options) error { _, err := o.ParseServe(args("-h"), nil); return err },
			"CHURR_",
			[]string{"data", "hostname", "db", "behind-proxy", "http", "https", "trusted-proxies", "media", "media-address",
				"tls-cert", "tls-key", "acme-directory", "unifiedpush-allow-private", "clock-synced", "log-level", "log-format", "dev"},
		},
		"churr-hub serve": {
			func(o Options) error { _, err := o.ParseHub(args("-h"), nil); return err },
			"CHURR_HUB_",
			[]string{"data", "hostname", "behind-proxy", "http", "https", "trusted-proxies", "status", "keep-days",
				"server-rate", "max-envelope", "clock-synced", "tls-cert", "tls-key", "acme-directory", "log-level", "log-format"},
		},
		"churr-relay serve": {
			func(o Options) error { _, err := o.ParseRelay(args("-h"), nil); return err },
			"CHURR_RELAY_",
			[]string{"hostname", "behind-proxy", "http", "https", "trusted-proxies", "data", "apns-key", "apns-key-id",
				"apns-team-id", "apns-topic", "apns-sandbox", "fcm-credentials", "device-rate", "device-daily",
				"address-rate", "max-inflight", "dry-run", "tls-cert", "tls-key", "acme-directory", "log-level", "log-format"},
		},
	}
	for name, p := range programs {
		var out bytes.Buffer
		err := p.parse(Options{Usage: &out})
		if !errors.Is(err, flag.ErrHelp) {
			t.Errorf("%s -h: err = %v, want flag.ErrHelp", name, err)
		}
		text := out.String()
		if !strings.Contains(text, "Usage: "+name) {
			t.Errorf("%s -h does not begin with its usage line:\n%s", name, text)
		}
		// Every flag of the document's table, with its variable, and no others.
		for _, f := range p.flags {
			if !strings.Contains(text, "\n  -"+f+"\n") && !strings.Contains(text, "\n  -"+f+" ") {
				t.Errorf("%s -h does not list -%s", name, f)
			}
			variable := p.prefix + strings.ToUpper(strings.ReplaceAll(f, "-", "_"))
			if !strings.Contains(text, "Environment: "+variable+".") {
				t.Errorf("%s -h does not give %s for -%s", name, variable, f)
			}
		}
		if got := strings.Count(text, "\n  -"); got != len(p.flags) {
			t.Errorf("%s -h lists %d flags, want %d", name, got, len(p.flags))
		}
	}
}

func TestHelpIsAskedForInSeveralWays(t *testing.T) {
	for _, h := range []string{"-h", "-help", "--help"} {
		var out bytes.Buffer
		// Help wins even when the rest would not pass.
		_, err := Options{Usage: &out}.ParseServe(args("-log-level", "loud", h), nil)
		if !errors.Is(err, flag.ErrHelp) || out.Len() == 0 {
			t.Errorf("%s: err = %v, %d bytes of help", h, err, out.Len())
		}
	}
}

func TestNothingIsWrittenOnAnError(t *testing.T) {
	var out bytes.Buffer
	if _, err := (Options{Usage: &out}).ParseServe(args("-colour", "pink"), nil); err == nil {
		t.Fatal("an unknown flag was accepted")
	}
	if out.Len() != 0 {
		t.Errorf("a mistake printed the help text: %q", out.String())
	}
}

func TestOrigin(t *testing.T) {
	cases := []struct {
		args []string
		want string
	}{
		{args("-hostname", "chat.example.com"), "https://chat.example.com"},
		{args("-hostname", "chat.example.com", "-https", ":443"), "https://chat.example.com"},
		{args("-hostname", "chat.example.com", "-https", "0.0.0.0:443"), "https://chat.example.com"},
		{args("-hostname", "chat.example.com", "-https", ":8443"), "https://chat.example.com:8443"},
		{args("-hostname", "chat.example.com", "-https", "[::]:8443"), "https://chat.example.com:8443"},
		{args("-hostname", "CHAT.example.com"), "https://chat.example.com"},
		// Behind a proxy the public side is the proxy's, on 443, whatever
		// the server listens on.
		{args("-hostname", "chat.example.com", "-behind-proxy"), "https://chat.example.com"},
		{args("-hostname", "chat.example.com", "-behind-proxy", "-http", ":8080"), "https://chat.example.com"},
		{args("-hostname", "chat.example.com", "-behind-proxy", "-https", ":8443"), "https://chat.example.com"},
		{args("-dev"), "http://localhost:8472"},
		{args("-dev", "-http", "127.0.0.1:3000"), "http://localhost:3000"},
		{args("-dev", "-http", ":80"), "http://localhost"},
		{args("-dev", "-https", ":8443"), "http://localhost:8472"},
	}
	for _, c := range cases {
		if got := serve(t, c.args, nil).Origin(); got != c.want {
			t.Errorf("Origin with %q = %q, want %q", c.args, got, c.want)
		}
	}
}

func TestSecureCookies(t *testing.T) {
	for _, a := range [][]string{
		{"-hostname", "chat.example.com"},
		{"-hostname", "chat.example.com", "-behind-proxy"},
		{"-hostname", "localhost"}, // localhost with a certificate is not the local trial
	} {
		if !serve(t, a, nil).SecureCookies() {
			t.Errorf("SecureCookies is false with %q", a)
		}
	}
	if serve(t, args("-dev"), nil).SecureCookies() {
		t.Error("SecureCookies is true with -dev")
	}
}

func TestServePaths(t *testing.T) {
	cfg := serve(t, args("-hostname", "chat.example.com", "-data", "/var/lib/churr"), nil)
	paths := map[string][2]string{
		"DBPath":   {cfg.DBPath(), "/var/lib/churr/churr.db"},
		"KeysDir":  {cfg.KeysDir(), "/var/lib/churr/keys"},
		"FilesDir": {cfg.FilesDir(), "/var/lib/churr/files"},
		"TmpDir":   {cfg.TmpDir(), "/var/lib/churr/tmp"},
		"CertsDir": {cfg.CertsDir(), "/var/lib/churr/certs"},
	}
	for name, p := range paths {
		if p[0] != filepath.FromSlash(p[1]) {
			t.Errorf("%s = %q, want %q", name, p[0], p[1])
		}
	}
	if cfg.Postgres() {
		t.Error("Postgres is true with no -db")
	}

	// The default data directory.
	cfg = serve(t, args("-hostname", "chat.example.com"), nil)
	if cfg.DBPath() != filepath.Join("data", "churr.db") {
		t.Errorf("DBPath = %q with the default data directory", cfg.DBPath())
	}

	// With PostgreSQL there is no SQLite file, and the rest stays put.
	cfg = serve(t, args("-hostname", "chat.example.com", "-data", "/d", "-db", "postgres://db/churr"), nil)
	if !cfg.Postgres() || cfg.DBPath() != "" {
		t.Errorf("with PostgreSQL: Postgres = %v, DBPath = %q", cfg.Postgres(), cfg.DBPath())
	}
	if cfg.KeysDir() != filepath.FromSlash("/d/keys") || cfg.FilesDir() != filepath.FromSlash("/d/files") {
		t.Errorf("with PostgreSQL the folders moved: %q, %q", cfg.KeysDir(), cfg.FilesDir())
	}
}

func TestSlogLevel(t *testing.T) {
	for level, want := range map[string]slog.Level{
		"debug": slog.LevelDebug, "info": slog.LevelInfo, "warn": slog.LevelWarn, "error": slog.LevelError,
	} {
		cfg := serve(t, args("-hostname", "chat.example.com", "-log-level", level), nil)
		if got := cfg.SlogLevel(); got != want {
			t.Errorf("SlogLevel for %q = %v, want %v", level, got, want)
		}
	}
}

func TestHubDefaults(t *testing.T) {
	cfg, err := ParseHub(args("-hostname", "hub.example.com"), nil)
	if err != nil {
		t.Fatal(err)
	}
	want := Hub{
		Common: Common{
			Data:          "./hub-data",
			Hostname:      "hub.example.com",
			HTTP:          ":80",
			HTTPS:         ":443",
			ACMEDirectory: DefaultACMEDirectory,
			LogLevel:      "info",
			LogFormat:     "text",
		},
		Status:      "127.0.0.1:8475",
		KeepDays:    30,
		ServerRate:  50,
		MaxEnvelope: 1048576,
	}
	if !reflect.DeepEqual(cfg, want) {
		t.Errorf("hub defaults:\n got %+v\nwant %+v", cfg, want)
	}
	if cfg.DBPath() != filepath.Join("hub-data", "hub.db") || cfg.KeysDir() != filepath.Join("hub-data", "keys") ||
		cfg.CertsDir() != filepath.Join("hub-data", "certs") {
		t.Errorf("hub paths: %q, %q, %q", cfg.DBPath(), cfg.KeysDir(), cfg.CertsDir())
	}

	cfg, err = ParseHub(args("-hostname", "hub.example.com", "-behind-proxy"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTP != "127.0.0.1:8473" || cfg.HTTPS != "" || len(cfg.TrustedProxies) != 2 {
		t.Errorf("hub behind a proxy: %+v", cfg.Common)
	}
}

func TestHubFlagsAndEnvironment(t *testing.T) {
	// The command line of the Dockerfile, with the rest from the environment.
	cfg, err := ParseHub(args("-data", "/data", "-behind-proxy", "-http", ":8080"), env(
		"CHURR_HUB_HOSTNAME", "hub.example.com",
		"CHURR_HUB_DATA", "/ignored",
		"CHURR_HUB_TRUSTED_PROXIES", "172.16.0.0/12",
		"CHURR_HUB_STATUS", "off",
		"CHURR_HUB_KEEP_DAYS", "7",
		"CHURR_HUB_SERVER_RATE", "200",
		"CHURR_HUB_MAX_ENVELOPE", "262144",
		"CHURR_HUB_TLS_CERT", "c.pem",
		"CHURR_HUB_TLS_KEY", "k.pem",
		"CHURR_HUB_ACME_DIRECTORY", "https://acme.example/dir",
		"CHURR_HUB_LOG_LEVEL", "debug",
		"CHURR_HUB_LOG_FORMAT", "json",
		"CHURR_HUB_HTTPS", ":8443",
	))
	if err != nil {
		t.Fatal(err)
	}
	want := Hub{
		Common: Common{
			Data:           "/data",
			Hostname:       "hub.example.com",
			BehindProxy:    true,
			HTTP:           ":8080",
			TrustedProxies: prefixes(t, "172.16.0.0/12"),
			TLSCert:        "c.pem",
			TLSKey:         "k.pem",
			ACMEDirectory:  "https://acme.example/dir",
			LogLevel:       "debug",
			LogFormat:      "json",
		},
		Status:      "",
		KeepDays:    7,
		ServerRate:  200,
		MaxEnvelope: 262144,
	}
	if !reflect.DeepEqual(cfg, want) {
		t.Errorf("hub:\n got %+v\nwant %+v", cfg, want)
	}
}

func TestHubValidation(t *testing.T) {
	host := []string{"-hostname", "hub.example.com"}
	with := func(more ...string) []string { return append(append([]string{}, host...), more...) }
	cases := []struct {
		what  string
		args  []string
		env   func(string) string
		parts []string
	}{
		{"missing -hostname", nil, nil, []string{"-hostname", "CHURR_HUB_HOSTNAME"}},
		{"missing -hostname behind a proxy", args("-behind-proxy"), nil, []string{"-hostname"}},
		{"-keep-days not a number", with("-keep-days", "a month"), nil, []string{"-keep-days", "whole number", "a month"}},
		{"-keep-days zero", with("-keep-days", "0"), nil, []string{"-keep-days", "at least 1"}},
		{"-keep-days negative", with("-keep-days=-3"), nil, []string{"-keep-days"}},
		{"-keep-days with a fraction", with("-keep-days", "1.5"), nil, []string{"-keep-days", "1.5"}},
		{"-server-rate zero", with("-server-rate", "0"), nil, []string{"-server-rate"}},
		{"-max-envelope with a unit", with("-max-envelope", "1MB"), nil, []string{"-max-envelope", "1MB"}},
		{"keep days from the environment", host, env("CHURR_HUB_KEEP_DAYS", "many"), []string{"CHURR_HUB_KEEP_DAYS", "many"}},
		{"bad -status", with("-status", "on"), nil, []string{"-status", "on", "off"}},
		{"-trusted-proxies without -behind-proxy", with("-trusted-proxies", "10.0.0.1"), nil, []string{"-trusted-proxies", "-behind-proxy"}},
		{"bad -log-level", with("-log-level", "trace"), nil, []string{"-log-level", "trace"}},
		{"a server's flag", with("-media", "off"), nil, []string{"-media"}},
		{"-dev", with("-dev"), nil, []string{"-dev"}},
	}
	for _, c := range cases {
		_, err := ParseHub(c.args, c.env)
		wantError(t, "hub: "+c.what, err, c.parts...)
	}
}

func TestRelayDefaults(t *testing.T) {
	cfg, err := ParseRelay(args("-hostname", "relay.example.com"), nil)
	if err != nil {
		t.Fatal(err)
	}
	want := Relay{
		Common: Common{
			Data:          "./relay-data",
			Hostname:      "relay.example.com",
			HTTP:          ":80",
			HTTPS:         ":443",
			ACMEDirectory: DefaultACMEDirectory,
			LogLevel:      "info",
			LogFormat:     "text",
		},
		APNsTopic:   DefaultAPNsTopic,
		DeviceRate:  60,
		DeviceDaily: 2000,
		AddressRate: 600,
		MaxInflight: 2000,
	}
	if !reflect.DeepEqual(cfg, want) {
		t.Errorf("relay defaults:\n got %+v\nwant %+v", cfg, want)
	}
	if cfg.CertsDir() != filepath.Join("relay-data", "certs") {
		t.Errorf("CertsDir = %q", cfg.CertsDir())
	}
}

func TestRelayNeedsNoHostnameBehindAProxy(t *testing.T) {
	// The command line of the Dockerfile.
	cfg, err := ParseRelay(args("-behind-proxy", "-http", ":8080"), nil)
	if err != nil {
		t.Fatalf("a relay behind a proxy was refused without a host name: %v", err)
	}
	if cfg.Hostname != "" || cfg.HTTP != ":8080" || cfg.HTTPS != "" {
		t.Errorf("relay behind a proxy: %+v", cfg.Common)
	}
	cfg, err = ParseRelay(args("-behind-proxy"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTP != "127.0.0.1:8474" {
		t.Errorf("HTTP = %q, want 127.0.0.1:8474", cfg.HTTP)
	}
	// Alone it fetches a certificate for its name, so it needs one.
	_, err = ParseRelay(nil, nil)
	wantError(t, "relay alone without -hostname", err, "-hostname", "CHURR_RELAY_HOSTNAME")
	// And a host name that is given is still checked.
	_, err = ParseRelay(args("-behind-proxy", "-hostname", "https://relay.example.com"), nil)
	wantError(t, "relay with a bad host name", err, "-hostname")
}

func TestRelayFlagsAndEnvironment(t *testing.T) {
	cfg, err := ParseRelay(args("-apns-sandbox", "-device-rate", "30"), env(
		"CHURR_RELAY_HOSTNAME", "relay.example.com",
		"CHURR_RELAY_DATA", "/relay",
		"CHURR_RELAY_APNS_KEY", "/keys/AuthKey.p8",
		"CHURR_RELAY_APNS_KEY_ID", "ABC123DEFG",
		"CHURR_RELAY_APNS_TEAM_ID", "TEAM123456",
		"CHURR_RELAY_APNS_TOPIC", "org.example.churr",
		"CHURR_RELAY_APNS_SANDBOX", "false",
		"CHURR_RELAY_FCM_CREDENTIALS", "/keys/fcm.json",
		"CHURR_RELAY_DEVICE_RATE", "90",
		"CHURR_RELAY_DEVICE_DAILY", "500",
		"CHURR_RELAY_ADDRESS_RATE", "100",
		"CHURR_RELAY_MAX_INFLIGHT", "50",
	))
	if err != nil {
		t.Fatal(err)
	}
	want := Relay{
		Common: Common{
			Data:          "/relay",
			Hostname:      "relay.example.com",
			HTTP:          ":80",
			HTTPS:         ":443",
			ACMEDirectory: DefaultACMEDirectory,
			LogLevel:      "info",
			LogFormat:     "text",
		},
		APNsKey:        "/keys/AuthKey.p8",
		APNsKeyID:      "ABC123DEFG",
		APNsTeamID:     "TEAM123456",
		APNsTopic:      "org.example.churr",
		APNsSandbox:    true, // the flag, over the variable
		FCMCredentials: "/keys/fcm.json",
		DeviceRate:     30, // the flag, over the variable
		DeviceDaily:    500,
		AddressRate:    100,
		MaxInflight:    50,
	}
	if !reflect.DeepEqual(cfg, want) {
		t.Errorf("relay:\n got %+v\nwant %+v", cfg, want)
	}
}

func TestRelayValidation(t *testing.T) {
	host := []string{"-hostname", "relay.example.com"}
	with := func(more ...string) []string { return append(append([]string{}, host...), more...) }
	cases := []struct {
		what  string
		args  []string
		parts []string
	}{
		{"an Apple key without its identifiers", with("-apns-key", "k.p8"), []string{"-apns-key", "-apns-key-id", "-apns-team-id"}},
		{"Apple identifiers without the key", with("-apns-key-id", "A", "-apns-team-id", "T"), []string{"-apns-key"}},
		{"-device-rate zero", with("-device-rate", "0"), []string{"-device-rate"}},
		{"-device-daily not a number", with("-device-daily", "lots"), []string{"-device-daily", "lots"}},
		{"-address-rate negative", with("-address-rate=-1"), []string{"-address-rate"}},
		{"-max-inflight empty", with("-max-inflight", ""), []string{"-max-inflight"}},
		{"-trusted-proxies without -behind-proxy", with("-trusted-proxies", "10.0.0.1"), []string{"-trusted-proxies", "-behind-proxy"}},
		{"a hub's flag", with("-keep-days", "3"), []string{"-keep-days"}},
	}
	for _, c := range cases {
		_, err := ParseRelay(c.args, nil)
		wantError(t, "relay: "+c.what, err, c.parts...)
	}
}

func TestRelayAPNsTopic(t *testing.T) {
	apple := []string{"-hostname", "relay.example.com", "-apns-key", "k.p8", "-apns-key-id", "A", "-apns-team-id", "T"}
	if DefaultAPNsTopic == "" {
		// Until the app identifier is chosen there is nothing to default to.
		_, err := ParseRelay(apple, nil)
		wantError(t, "an Apple key with no topic", err, "-apns-topic")
	}
	cfg, err := ParseRelay(append(apple, "-apns-topic", "org.example.churr"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.APNsTopic != "org.example.churr" {
		t.Errorf("APNsTopic = %q", cfg.APNsTopic)
	}
}
