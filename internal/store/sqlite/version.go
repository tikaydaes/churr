package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	"github.com/tikaydaes/churr/internal/store/migrations"
)

// SchemaVersion checks the database file at path against this build's
// migrations (migrations.Inspect) without changing it: the file is opened
// read-only and no migration is run. `churr migrate-db` asks before it opens a source, which Open would
// migrate. A file that does not exist is an error; it is not created.
func SchemaVersion(ctx context.Context, path string) (migrations.Status, error) {
	if _, err := os.Stat(path); err != nil {
		return migrations.Status{}, fmt.Errorf("there is no database file at %s", path)
	}
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?"+url.Values{"mode": {"ro"}}.Encode())
	if err != nil {
		return migrations.Status{}, err
	}
	defer db.Close()
	return migrations.Inspect(ctx, db, migrations.SQLite)
}
