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

// SchemaVersion returns the schema version of the database file at path
// without changing it: the file is opened read-only and no migration is
// run. `churr migrate-db` asks before it opens a source, which Open would
// migrate. A file that does not exist is an error; it is not created.
func SchemaVersion(ctx context.Context, path string) (int, error) {
	if _, err := os.Stat(path); err != nil {
		return 0, fmt.Errorf("there is no database file at %s", path)
	}
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?"+url.Values{"mode": {"ro"}}.Encode())
	if err != nil {
		return 0, err
	}
	defer db.Close()
	return migrations.Version(ctx, db, migrations.SQLite)
}
