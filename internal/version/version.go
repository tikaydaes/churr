// Package version says which build this is.
//
// The two variables are set by the linker when a release is built:
//
//	go build -ldflags "-X github.com/tikaydaes/churr/internal/version.Version=1.4.0 \
//	                   -X github.com/tikaydaes/churr/internal/version.Commit=3fa1c0de"
//
// A plain `go build` leaves them alone, and the commit is then taken from
// what the Go tool recorded about the checkout, when it recorded anything.
package version

import "runtime/debug"

// Version is the release, such as "1.4.0". It stays "dev" in a build nobody
// stamped.
var Version = "dev"

// Commit is the source commit the binary was built from, or empty when it is
// not known.
var Commit = ""

// shortCommit is how many characters of a commit hash String shows. A full
// hash is noise on a status page; twelve characters still name one commit.
const shortCommit = 12

// Revision returns Commit, or the commit the Go tool recorded at build time
// when the linker flag was not given. It is empty when neither is known, as
// in a build from a directory that is not a checkout.
func Revision() string {
	if Commit != "" {
		return Commit
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	for _, s := range info.Settings {
		if s.Key == "vcs.revision" {
			return s.Value
		}
	}
	return ""
}

// String is the version as shown to people: "1.4.0 (3fa1c0de)", or just
// "1.4.0" when the commit is not known.
func String() string {
	rev := Revision()
	if rev == "" {
		return Version
	}
	if len(rev) > shortCommit {
		rev = rev[:shortCommit]
	}
	return Version + " (" + rev + ")"
}
