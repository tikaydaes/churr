package sqlite_test

import (
	"testing"

	"github.com/tikaydaes/churr/internal/store"
	"github.com/tikaydaes/churr/internal/store/storetest"
)

// TestStore runs the shared storage suite against SQLite.
func TestStore(t *testing.T) {
	storetest.Run(t, func(t *testing.T) store.Store { return storetest.OpenSQLite(t) })
}
