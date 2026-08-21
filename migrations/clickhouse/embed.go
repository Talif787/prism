// Package chmigrations embeds the ClickHouse DDL so the consumer can create the
// telemetry tables on startup without shipping loose files.
package chmigrations

import "embed"

//go:embed *.sql
var FS embed.FS
