package postgres

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/tikaydaes/churr/internal/store/migrations"
)

// SchemaVersion checks the database at url against this build's
// migrations (migrations.Inspect) without changing it: no migration is
// run. Its Version is 0 for a database with no Churr tables. `churr migrate-db` asks before it opens a source, which Open
// would migrate.
func SchemaVersion(ctx context.Context, url string) (migrations.Status, error) {
	db, err := sql.Open("pgx", url)
	if err != nil {
		return migrations.Status{}, err
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		return migrations.Status{}, fmt.Errorf("connecting to PostgreSQL: %w", err)
	}
	return migrations.Inspect(ctx, db, migrations.Postgres)
}
