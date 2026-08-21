// Package pgstore is the Postgres adapter for metering: usage rollups, the
// per-signal watermark, tenant plan lookup, and invoice persistence. It mirrors the
// pgx patterns used elsewhere; id comparisons use ::text to tolerate malformed ids.
package pgstore

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Talif787/prism/internal/metering/domain"
)

type Store struct{ pool *pgxpool.Pool }

func New(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

func (s *Store) UpsertRollup(ctx context.Context, r domain.UsageRecord) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO usage_rollups (tenant_id, signal, window_start, point_count)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (tenant_id, signal, window_start) DO UPDATE SET
		  point_count = EXCLUDED.point_count, updated_at = now()`,
		r.TenantID, string(r.Signal), r.WindowStart, r.Count)
	return err
}

func (s *Store) GetWatermark(ctx context.Context, signal domain.Signal) (*time.Time, error) {
	var wm time.Time
	err := s.pool.QueryRow(ctx, `SELECT watermark FROM metering_state WHERE signal = $1`, string(signal)).Scan(&wm)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &wm, nil
}

func (s *Store) SetWatermark(ctx context.Context, signal domain.Signal, at time.Time) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO metering_state (signal, watermark) VALUES ($1, $2)
		ON CONFLICT (signal) DO UPDATE SET watermark = EXCLUDED.watermark, updated_at = now()`,
		string(signal), at)
	return err
}

func (s *Store) UsageBySignal(ctx context.Context, tenantID string, from, to time.Time) (map[domain.Signal]int64, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT signal, COALESCE(SUM(point_count), 0) FROM usage_rollups
		WHERE tenant_id::text = $1 AND window_start >= $2 AND window_start < $3
		GROUP BY signal`, tenantID, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[domain.Signal]int64{}
	for rows.Next() {
		var sig string
		var sum int64
		if err := rows.Scan(&sig, &sum); err != nil {
			return nil, err
		}
		out[domain.Signal(sig)] = sum
	}
	return out, rows.Err()
}

func (s *Store) TenantPlan(ctx context.Context, tenantID string) (string, error) {
	var plan string
	err := s.pool.QueryRow(ctx, `SELECT plan FROM tenants WHERE id::text = $1`, tenantID).Scan(&plan)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", domain.ErrNotFound
	}
	if err != nil {
		return "", err
	}
	return plan, nil
}

func (s *Store) CreateInvoice(ctx context.Context, inv *domain.Invoice) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if err := tx.QueryRow(ctx, `
		INSERT INTO invoices (tenant_id, period_start, period_end, status, currency, total)
		VALUES ($1, $2, $3, $4, $5, $6) RETURNING id, created_at`,
		inv.TenantID, inv.PeriodStart, inv.PeriodEnd, inv.Status, inv.Currency, inv.Total,
	).Scan(&inv.ID, &inv.CreatedAt); err != nil {
		return err
	}
	for _, li := range inv.LineItems {
		if _, err := tx.Exec(ctx, `
			INSERT INTO invoice_line_items (invoice_id, signal, quantity, unit_price_per_million, amount)
			VALUES ($1, $2, $3, $4, $5)`,
			inv.ID, string(li.Signal), li.Quantity, li.UnitPricePerMillion, li.Amount); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s *Store) GetInvoice(ctx context.Context, tenantID, id string) (*domain.Invoice, error) {
	var inv domain.Invoice
	err := s.pool.QueryRow(ctx, `
		SELECT id, tenant_id, period_start, period_end, status, currency, total, created_at
		FROM invoices WHERE id::text = $1 AND tenant_id::text = $2`, id, tenantID,
	).Scan(&inv.ID, &inv.TenantID, &inv.PeriodStart, &inv.PeriodEnd, &inv.Status, &inv.Currency, &inv.Total, &inv.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	items, err := s.lineItems(ctx, inv.ID)
	if err != nil {
		return nil, err
	}
	inv.LineItems = items
	return &inv, nil
}

func (s *Store) ListInvoices(ctx context.Context, tenantID string) ([]domain.Invoice, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, tenant_id, period_start, period_end, status, currency, total, created_at
		FROM invoices WHERE tenant_id::text = $1 ORDER BY created_at DESC`, tenantID)
	if err != nil {
		return nil, err
	}
	out := make([]domain.Invoice, 0)
	for rows.Next() {
		var inv domain.Invoice
		if err := rows.Scan(&inv.ID, &inv.TenantID, &inv.PeriodStart, &inv.PeriodEnd, &inv.Status, &inv.Currency, &inv.Total, &inv.CreatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, inv)
	}
	rerr := rows.Err()
	rows.Close()
	if rerr != nil {
		return nil, rerr
	}
	// Attach line items after the invoice rows are drained and the connection freed.
	for i := range out {
		items, err := s.lineItems(ctx, out[i].ID)
		if err != nil {
			return nil, err
		}
		out[i].LineItems = items
	}
	return out, nil
}

func (s *Store) lineItems(ctx context.Context, invoiceID string) ([]domain.LineItem, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT signal, quantity, unit_price_per_million, amount
		FROM invoice_line_items WHERE invoice_id::text = $1`, invoiceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.LineItem, 0)
	for rows.Next() {
		var li domain.LineItem
		var sig string
		if err := rows.Scan(&sig, &li.Quantity, &li.UnitPricePerMillion, &li.Amount); err != nil {
			return nil, err
		}
		li.Signal = domain.Signal(sig)
		out = append(out, li)
	}
	return out, rows.Err()
}
