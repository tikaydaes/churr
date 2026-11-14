package postgres_test

import (
	"testing"

	"github.com/tikaydaes/churr/internal/store"
	"github.com/tikaydaes/churr/internal/store/storetest"
)

// TestStore runs the shared storage suite against PostgreSQL. It is skipped
// unless CHURR_TEST_POSTGRES names a server (tools/pgtest.sh starts one).
func TestStore(t *testing.T) {
	storetest.Run(t, func(t *testing.T) store.Store { return storetest.OpenPostgres(t) })
}
