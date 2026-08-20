// Package migrations embeds the SQL migration files so the control plane can
// apply schema changes on startup without shipping loose files.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
