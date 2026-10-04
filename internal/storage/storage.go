package storage

import (
	"context"
	"database/sql"
)

// Tx is the narrow transactional surface exposed to domain repositories.
// Domain services should depend on repositories/services rather than Tx directly.
type Tx interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type Transactor interface {
	Within(context.Context, func(context.Context, Tx) error) error
}
