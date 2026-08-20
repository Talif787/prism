package postgres

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Migrator applies forward SQL migrations embedded at build time. It records
// applied versions in a schema_migrations table and runs each new migration in a
// transaction, so a failed migration leaves the schema unchanged.
type Migrator struct {
	pool *pgxpool.Pool
	fsys fs.FS
}

func NewMigrator(pool *pgxpool.Pool, fsys embed.FS) *Migrator {
	return &Migrator{pool: pool, fsys: fsys}
}

type migration struct {
	version string
	sql     string
}

// Up applies all pending .up.sql migrations in lexical version order.
func (m *Migrator) Up(ctx context.Context) (applied []string, err error) {
	if err := m.ensureTable(ctx); err != nil {
		return nil, err
	}
	done, err := m.appliedVersions(ctx)
	if err != nil {
		return nil, err
	}
	migrations, err := m.load()
	if err != nil {
		return nil, err
	}
	for _, mig := range migrations {
		if done[mig.version] {
			continue
		}
		if err := m.apply(ctx, mig); err != nil {
			return applied, fmt.Errorf("apply migration %s: %w", mig.version, err)
		}
		applied = append(applied, mig.version)
	}
	return applied, nil
}

func (m *Migrator) ensureTable(ctx context.Context) error {
	_, err := m.pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version    TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`)
	return err
}

func (m *Migrator) appliedVersions(ctx context.Context) (map[string]bool, error) {
	rows, err := m.pool.Query(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out[v] = true
	}
	return out, rows.Err()
}

func (m *Migrator) load() ([]migration, error) {
	entries, err := fs.ReadDir(m.fsys, ".")
	if err != nil {
		return nil, err
	}
	var migs []migration
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".up.sql") {
			continue
		}
		b, err := fs.ReadFile(m.fsys, name)
		if err != nil {
			return nil, err
		}
		migs = append(migs, migration{
			version: strings.TrimSuffix(name, ".up.sql"),
			sql:     string(b),
		})
	}
	sort.Slice(migs, func(i, j int) bool { return migs[i].version < migs[j].version })
	return migs, nil
}

func (m *Migrator) apply(ctx context.Context, mig migration) error {
	return pgx.BeginTxFunc(ctx, m.pool, pgx.TxOptions{}, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, mig.sql); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO schema_migrations (version) VALUES ($1)`, mig.version)
		return err
	})
}

// ErrNoMigrations indicates the embedded filesystem contained no migrations.
var ErrNoMigrations = errors.New("no migrations found")
