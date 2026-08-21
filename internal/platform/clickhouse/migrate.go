package clickhouse

import (
	"context"
	"fmt"
	"io/fs"
	"strings"
)

// Migrate applies a ClickHouse DDL file. Statements are idempotent
// (CREATE ... IF NOT EXISTS), so no migration-tracking table is needed: applying
// the file repeatedly is safe. Statements are split on the semicolon terminator
// and executed one at a time, since the native protocol executes a single
// statement per Exec.
func (c *Conn) Migrate(ctx context.Context, fsys fs.FS, name string) error {
	raw, err := fs.ReadFile(fsys, name)
	if err != nil {
		return fmt.Errorf("read migration %s: %w", name, err)
	}
	for _, stmt := range splitStatements(string(raw)) {
		if err := c.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("apply migration %s: %w", name, err)
		}
	}
	return nil
}

// splitStatements strips full-line comments, then splits on ';' and drops empty
// fragments. The DDL contains no semicolons inside statements (Map(...) uses
// commas), so a plain split is safe here.
func splitStatements(sql string) []string {
	var b strings.Builder
	for _, line := range strings.Split(sql, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "--") {
			continue
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	var out []string
	for _, part := range strings.Split(b.String(), ";") {
		if s := strings.TrimSpace(part); s != "" {
			out = append(out, s)
		}
	}
	return out
}
