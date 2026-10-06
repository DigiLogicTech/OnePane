package migrations

import "embed"

// FS contains schema migrations in lexical order.
//
//go:embed *.sql
var FS embed.FS
