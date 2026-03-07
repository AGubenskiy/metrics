package migrations

import "embed"

// FS contains SQL migration files for the metrics schema.
//
//go:embed *.sql
var FS embed.FS
