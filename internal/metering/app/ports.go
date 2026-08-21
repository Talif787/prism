// Package app is the metering application layer: a background rollup engine that
// meters accepted telemetry, and a usage and billing service over the rolled-up
// data. It is storage- and source-agnostic through the ports below.
package app

import (
	"context"
	"errors"
	"time"

	"github.com/Talif787/prism/internal/metering/domain"
)

var (
	ErrUnauthorized = errors.New("unauthorized")
	ErrForbidden    = errors.New("forbidden")
)

// Principal is the authenticated caller, bound to one tenant.
type Principal struct {
	TenantID string
	Scopes   []string
}

func (p *Principal) HasScope(scope string) bool {
	for _, s := range p.Scopes {
		if s == scope {
			return true
		}
	}
	return false
}

// Authenticator resolves an API key to a principal.
type Authenticator interface {
	Authenticate(ctx context.Context, apiKey string) (*Principal, error)
}

// UsageSource counts accepted telemetry from the store of record (ClickHouse),
// grouped by tenant and by window of the given interval, over [from, to).
type UsageSource interface {
	CountUsage(ctx context.Context, signal domain.Signal, from, to time.Time, intervalSeconds int) ([]domain.UsageRecord, error)
}

// UsageStore persists rollups and the metering watermark, and reads usage for
// reporting and billing.
type UsageStore interface {
	UpsertRollup(ctx context.Context, r domain.UsageRecord) error
	GetWatermark(ctx context.Context, signal domain.Signal) (*time.Time, error)
	SetWatermark(ctx context.Context, signal domain.Signal, at time.Time) error

	UsageBySignal(ctx context.Context, tenantID string, from, to time.Time) (map[domain.Signal]int64, error)
	TenantPlan(ctx context.Context, tenantID string) (string, error)

	CreateInvoice(ctx context.Context, inv *domain.Invoice) error
	GetInvoice(ctx context.Context, tenantID, id string) (*domain.Invoice, error)
	ListInvoices(ctx context.Context, tenantID string) ([]domain.Invoice, error)
}
