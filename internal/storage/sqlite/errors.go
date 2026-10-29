package sqlite

import (
	"database/sql"
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

// IsStoreFailure reports a durable SQLite or database/sql failure. Optional
// component errors are ordinary Go errors and must not be classified as one.
// IsReadOnly reports a SQLite SQLITE_READONLY result. BEGIN IMMEDIATE fails
// this way on a query_only connection or a read-only file; a deferred
// transaction can still read.
func IsReadOnly(err error) bool {
	var sqliteErr *modernsqlite.Error
	return errors.As(err, &sqliteErr) && sqliteErr.Code()&0xff == sqlite3.SQLITE_READONLY
}

func IsStoreFailure(err error) bool {
	if err == nil {
		return false
	}
	if IsBusy(err) || errors.Is(err, sql.ErrConnDone) || errors.Is(err, sql.ErrTxDone) {
		return true
	}
	var sqliteErr *modernsqlite.Error
	return errors.As(err, &sqliteErr)
}
