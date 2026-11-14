package config

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/netip"
	"net/url"
	"os"
	"strconv"
	"strings"
)

// Options changes how the Parse functions behave. The zero value is what a
// program wants; tests set Usage to capture the help text.
type Options struct {
	// Usage is where the list of flags is written when -h is given.
	// Nil means standard error.
	Usage io.Writer
}

// parser is one program's flags together with their environment variables.
// It wraps flag.FlagSet, which knows nothing about the environment, and
// remembers where each value came from so that an error can name the thing
// the operator actually wrote.
type parser struct {
	fs      *flag.FlagSet
	command string // "churr serve", for the help text
	prefix  string // "CHURR_", "CHURR_HUB_" or "CHURR_RELAY_"
	defs    []*def
	// from says where a flag's value came from: "-name" when it was on the
	// command line, the variable's name when it came from the environment.
	// A flag left at its default is not in the map.
	from map[string]string
}

// def is one flag, kept for the help text and the environment pass.
type def struct {
	name    string
	value   string // what the help text shows after the flag, such as DIR
	shown   string // the default as the help text puts it
	meaning string
	boolean bool
}

func newParser(command, prefix string) *parser {
	fs := flag.NewFlagSet(command, flag.ContinueOnError)
	// The flag package prints the error and the usage by itself on any
	// mistake. We want to decide both, so it prints nowhere.
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}
	return &parser{fs: fs, command: command, prefix: prefix, from: map[string]string{}}
}

// text adds a flag that takes a value. Numbers are taken as text too and
// read afterwards, so a bad one gets a sentence of ours and not the flag
// package's.
func (p *parser) text(name, value, def0, shown, meaning string) *string {
	if shown == "" {
		shown = def0
	}
	p.defs = append(p.defs, &def{name: name, value: value, shown: shown, meaning: meaning})
	return p.fs.String(name, def0, "")
}

// toggle adds a switch, false unless given.
func (p *parser) toggle(name, meaning string) *bool {
	p.defs = append(p.defs, &def{name: name, shown: "false", meaning: meaning, boolean: true})
	return p.fs.Bool(name, false, "")
}

// envName is the environment variable for a flag: the program's prefix,
// then the flag's name in capitals with dashes turned into underscores
// (docs/OPERATIONS.md section 1).
func (p *parser) envName(flagName string) string {
	return p.prefix + strings.ToUpper(strings.ReplaceAll(flagName, "-", "_"))
}

// given reports whether the operator set the flag, on the command line or in
// the environment, as opposed to it being left at its default.
func (p *parser) given(name string) bool {
	_, ok := p.from[name]
	return ok
}

// label is what to call a flag in an error: the environment variable if
// that is where its value came from, the flag otherwise.
func (p *parser) label(name string) string {
	if from, ok := p.from[name]; ok {
		return from
	}
	return "-" + name
}

// parse reads the command line, then fills every flag that was not on it
// from the environment. So a flag beats its variable, and the variable
// beats the default.
func (p *parser) parse(args []string, env func(string) string, opts Options) error {
	if env == nil {
		env = func(string) string { return "" }
	}
	if err := p.fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			w := opts.Usage
			if w == nil {
				w = os.Stderr
			}
			p.usage(w)
			return flag.ErrHelp
		}
		return plain(err)
	}
	if p.fs.NArg() > 0 {
		return fmt.Errorf("%s takes flags only, and %q is not one. A switch is written -name or -name=false, never -name false.",
			p.command, p.fs.Arg(0))
	}
	p.fs.Visit(func(f *flag.Flag) { p.from[f.Name] = "-" + f.Name })

	for _, d := range p.defs {
		if p.given(d.name) {
			continue
		}
		name := p.envName(d.name)
		v := env(name)
		// An empty variable is the same as none. A compose file passes
		// CHURR_DB= when the operator left it blank.
		if v == "" {
			continue
		}
		if d.boolean && v != "true" && v != "false" {
			return fmt.Errorf("%s must be true or false, not %q.", name, v)
		}
		if err := p.fs.Set(d.name, v); err != nil {
			return fmt.Errorf("%s has a value that cannot be used: %q.", name, v)
		}
		p.from[d.name] = name
	}
	return nil
}

// usage writes the list of flags: each with its environment variable, its
// default and what it is for.
func (p *parser) usage(w io.Writer) {
	fmt.Fprintf(w, "Usage: %s [flags]\n\n", p.command)
	fmt.Fprintf(w, "Each flag can also be set with the environment variable shown beside it.\n")
	fmt.Fprintf(w, "A flag on the command line wins. Switches take true or false in the environment.\n\n")
	for _, d := range p.defs {
		head := "-" + d.name
		if d.value != "" {
			head += " " + d.value
		}
		fmt.Fprintf(w, "  %s\n", head)
		fmt.Fprintf(w, "        %s\n", d.meaning)
		def0 := d.shown
		if def0 == "" {
			def0 = "none"
		}
		fmt.Fprintf(w, "        Environment: %s. Default: %s.\n", p.envName(d.name), def0)
	}
}

// plain turns the flag package's own errors into sentences in the voice of
// the rest. Anything it does not recognise is passed on as it is.
func plain(err error) error {
	msg := err.Error()
	if name, ok := strings.CutPrefix(msg, "flag provided but not defined: "); ok {
		return fmt.Errorf("There is no flag %s. Run with -h to see the flags.", name)
	}
	if name, ok := strings.CutPrefix(msg, "flag needs an argument: "); ok {
		return fmt.Errorf("%s needs a value after it.", name)
	}
	if rest, ok := strings.CutPrefix(msg, "invalid boolean value "); ok {
		// `"maybe" for -dev: parse error`
		if value, tail, ok := strings.Cut(rest, " for "); ok {
			name, _, _ := strings.Cut(tail, ":")
			return fmt.Errorf("%s is a switch. Write %s or %s=false, not %s=%s.", name, name, name, name, value)
		}
	}
	return err
}

// number reads a flag that must be a whole number of at least 1.
func (p *parser) number(name, value string) (int, error) {
	n, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || n < 1 {
		return 0, fmt.Errorf("%s must be a whole number of at least 1, not %q.", p.label(name), value)
	}
	return n, nil
}

// oneOf checks a flag that takes one word from a short list.
func (p *parser) oneOf(name, value string, allowed ...string) error {
	for _, a := range allowed {
		if value == a {
			return nil
		}
	}
	list := strings.Join(allowed[:len(allowed)-1], ", ") + " or " + allowed[len(allowed)-1]
	return fmt.Errorf("%s must be %s, not %q.", p.label(name), list, value)
}

// listen checks an address to listen on, such as ":443" or
// "127.0.0.1:8472". Only its shape is checked; whether the port can be
// bound is found out when the server tries.
func (p *parser) listen(name, value string) error {
	bad := fmt.Errorf("%s must be an address to listen on, such as :8472 or 127.0.0.1:8472, not %q.", p.label(name), value)
	host, port, err := net.SplitHostPort(value)
	if err != nil {
		return bad
	}
	if n, err := strconv.Atoi(port); err != nil || n < 0 || n > 65535 {
		return bad
	}
	if strings.ContainsAny(host, " /") {
		return bad
	}
	return nil
}

// proxies reads a comma-separated list of addresses and ranges.
func (p *parser) proxies(name, value string) ([]netip.Prefix, error) {
	var out []netip.Prefix
	for _, part := range strings.Split(value, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if prefix, err := netip.ParsePrefix(part); err == nil {
			// 127.0.0.1/8 is how the documented default is written; what is
			// meant is the range it lies in.
			out = append(out, prefix.Masked())
			continue
		}
		if addr, err := netip.ParseAddr(part); err == nil {
			out = append(out, netip.PrefixFrom(addr, addr.BitLen()))
			continue
		}
		return nil, fmt.Errorf("%s must be a list of addresses or ranges, such as 10.0.0.5 or 172.16.0.0/12, separated by commas. %q is neither.",
			p.label(name), part)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%s names no address.", p.label(name))
	}
	return out, nil
}

// hostname checks a public host name and returns it in lower case. It
// explains the mistakes people actually make: pasting an address with
// https:// in front, adding the port, or giving an IP address.
func (p *parser) hostname(name, value string) (string, error) {
	label := p.label(name)
	host := strings.ToLower(strings.TrimSpace(value))
	switch {
	case strings.Contains(host, "://") || strings.Contains(host, "/"):
		return "", fmt.Errorf("%s must be a bare host name such as chat.example.com, without https:// or a path. It was %q.", label, value)
	case strings.Contains(host, ":"):
		// A port, or an IPv6 address; neither is a host name.
		return "", fmt.Errorf("%s must be a bare host name such as chat.example.com, without a port, and not an IP address. It was %q.", label, value)
	}
	if _, err := netip.ParseAddr(host); err == nil {
		return "", fmt.Errorf("%s must be a host name, not an IP address: passkeys and certificates are tied to a name. It was %q.", label, value)
	}
	if !validHostname(host) {
		return "", fmt.Errorf("%s is not a host name: %q. Use letters, digits, hyphens and dots, as in chat.example.com. "+
			"A name in another script must be given in its xn-- form.", label, value)
	}
	return host, nil
}

// validHostname reports whether host is a DNS name in its ASCII form:
// labels of letters, digits and hyphens, joined by dots.
func validHostname(host string) bool {
	if host == "" || len(host) > 253 {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for i := 0; i < len(label); i++ {
			c := label[i]
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
				return false
			}
		}
	}
	return true
}

// webAddress checks an http or https address, such as an ACME directory.
func (p *parser) webAddress(name, value string) error {
	u, err := url.Parse(value)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return fmt.Errorf("%s must be an https:// address, not %q.", p.label(name), value)
	}
	return nil
}

// portSuffix is ":port" for the port of a listening address, or nothing
// when that port is the scheme's own and so is left out of an origin.
func portSuffix(listen, usual string) string {
	_, port, err := net.SplitHostPort(listen)
	if err != nil || port == usual {
		return ""
	}
	return ":" + port
}
