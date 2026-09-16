// Package migrations embeds the schema history. Each file is NNNN_name.sql,
// applied in order by cmd/migrate and by pgtest (§8.10: every change is a
// migration file that runs on a database with data; expand → migrate →
// contract).
//
// Every table has exactly one owning module (§6.9.5) and the file header
// names it. A migration that touches two modules' tables is a design smell
// before it is a review comment.
package migrations

import "embed"

// FS holds the migration files at its root.
//
//go:embed *.sql
var FS embed.FS
