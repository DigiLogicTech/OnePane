package sqlite

import (
	"errors"

	modernsqlite "modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// IsBusy reports whether err wraps a SQLite busy result, including extended
// results such as SQLITE_BUSY_SNAPSHOT. Driver messages vary across platforms.
func IsBusy(err error) bool {
	var sqliteErr *modernsqlite.Error
	return errors.As(err, &sqliteErr) && sqliteErr.Code()&0xff == sqlite3.SQLITE_BUSY
}
