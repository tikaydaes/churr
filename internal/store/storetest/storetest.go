// Package storetest is the one suite both storage backends must pass. Every
// storage behaviour is tested here once and run against SQLite and
// PostgreSQL (docs/STRUCTURE.md section 5).
//
// Each area of store.Areas has a file here named like its interface file,
// holding one function that tests it and an init that registers it. Run
// gives each area a fresh, empty database. ../README.md says how to add one.
package storetest

import (
	"context"
	"sort"
	"testing"

	"github.com/tikaydaes/churr/internal/store"
)

// areaTest tests one area against a fresh store.
type areaTest func(t *testing.T, s store.Store)

var areas = map[string]areaTest{}

// register adds an area's tests to the suite. Each area file calls it from
// init, so adding an area never means editing this file.
func register(name string, fn areaTest) {
	if _, dup := areas[name]; dup {
		panic("storetest: area registered twice: " + name)
	}
	areas[name] = fn
}

// Run runs every area's tests, each against a store of its own made by open.
// A backend's test file calls it once.
func Run(t *testing.T, open func(t *testing.T) store.Store) {
	names := make([]string, 0, len(areas))
	for n := range areas {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		fn := areas[name]
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fn(t, open(t))
		})
	}
}

// ctx is the context every test uses.
var ctx = context.Background()
