package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
)

// flags is the flag set of one of the commands that work on a data
// directory. Every flag has an environment variable made from its name, as
// for `churr serve` (docs/OPERATIONS.md section 1): CHURR_ and the name in
// capitals, dashes as underscores. A flag on the command line wins.
type flags struct {
	*flag.FlagSet
	names []string
}

func newFlags(command, synopsis string) *flags {
	f := &flags{FlagSet: flag.NewFlagSet(command, flag.ContinueOnError)}
	f.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: %s\n\n", synopsis)
		f.PrintDefaults()
		fmt.Fprintln(os.Stderr, "\nEvery flag can also be set in the environment: -no-files is CHURR_NO_FILES.")
	}
	return f
}

func envName(name string) string {
	return "CHURR_" + strings.ToUpper(strings.ReplaceAll(name, "-", "_"))
}

func (f *flags) text(name, def, meaning string) *string {
	f.names = append(f.names, name)
	return f.String(name, def, meaning)
}

func (f *flags) toggle(name, meaning string) *bool {
	f.names = append(f.names, name)
	return f.Bool(name, false, meaning)
}

// community adds the two flags that say which community: -data and -db.
func (f *flags) community() (data, db *string) {
	return f.text("data", "./data", "The data directory."),
		f.text("db", "", "Empty means SQLite at churr.db in the data directory. A postgres:// address means PostgreSQL.")
}

// parse reads the arguments, then fills in from the environment whatever
// the command line did not give. It returns what is left: the arguments
// that are not flags. Flags may come before or after them.
func (f *flags) parse(args []string) ([]string, error) {
	var rest []string
	for {
		if err := f.Parse(args); err != nil {
			return nil, err
		}
		// The standard parser stops at the first argument that is not a
		// flag; carry on after it, so `backup /dest -no-files` works.
		if f.NArg() == 0 {
			break
		}
		rest = append(rest, f.Arg(0))
		args = f.Args()[1:]
	}
	given := map[string]bool{}
	f.Visit(func(fl *flag.Flag) { given[fl.Name] = true })
	for _, name := range f.names {
		if given[name] {
			continue
		}
		if v := os.Getenv(envName(name)); v != "" {
			if err := f.Set(name, v); err != nil {
				return nil, fmt.Errorf("%s=%q: %v", envName(name), v, err)
			}
		}
	}
	return rest, nil
}
