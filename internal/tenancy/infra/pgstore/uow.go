// Package pgstore implements the tenancy application ports on PostgreSQL using
// pgx. Repositories are transaction-scoped: the unit of work opens a transaction,
// constructs repositories bound to it, runs the use case, then commits or rolls
// back. This keeps multi-step use cases atomic without leaking pgx into the app.
package pgstore

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Talif787/prism/internal/tenancy/app"
)

// UnitOfWork implements app.UnitOfWork over a pgx pool.
type UnitOfWork struct {
	pool   *pgxpool.Pool
	logger *slog.Logger
}

func NewUnitOfWork(pool *pgxpool.Pool, logger *slog.Logger) *UnitOfWork {
	return &UnitOfWork{pool: pool, logger: logger}
}

// Do runs fn inside a serializable-safe read-committed transaction. pgx.BeginTxFunc
// commits on nil error and rolls back on error or panic.
func (u *UnitOfWork) Do(ctx context.Context, fn func(app.Repositories) error) error {
	return pgx.BeginTxFunc(ctx, u.pool, pgx.TxOptions{IsoLevel: pgx.ReadCommitted}, func(tx pgx.Tx) error {
		repos := app.Repositories{
			Tenants: &TenantRepository{q: tx},
			Users:   &UserRepository{q: tx},
			Keys:    &APIKeyRepository{q: tx},
			Events:  &OutboxPublisher{q: tx},
		}
		return fn(repos)
	})
}

// querier is the subset of pgx used by repositories, satisfied by both a pool
// and a transaction, so repositories work inside or outside a unit of work.
type querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconnCommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}
