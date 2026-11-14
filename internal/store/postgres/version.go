package postgres

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/tikaydaes/churr/internal/store/migrations"
)

// SchemaVersion returns the schema version of the database at url without
// changing it: no migration is run. It is 0 for a database with no Churr
// tables. `churr migrate-db` asks before it opens a source, which Open
// would migrate.
func SchemaVersion(ctx context.Context, url string) (int, error) {
	db, err := sql.Open("pgx", url)
	if err != nil {
		return 0, err
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		return 0, fmt.Errorf("connecting to PostgreSQL: %w", err)
	}
	return migrations.Version(ctx, db, migrations.Postgres)
}
