// Package app is the query application layer: the service that validates requests,
// applies a small discovery cache, and delegates to the storage port. It is
// storage-agnostic (the ClickHouse adapter implements Store) and auth-agnostic
// (the control-plane adapter implements Authenticator).
package app

import (
	"context"
	"errors"
	"time"

	"github.com/Talif787/prism/internal/query/domain"
)

// ErrUnauthorized is returned for a missing or invalid credential.
var ErrUnauthorized = errors.New("unauthorized")

// ErrForbidden is returned for a valid credential that lacks the query scope.
var ErrForbidden = errors.New("forbidden")

// Principal is the authenticated caller, always bound to a single tenant.
type Principal struct {
	TenantID string
	Scopes   []string
}

// HasScope reports whether the principal holds the given scope.
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

// Store is the read port over the columnar telemetry tables. Every method is
// tenant-scoped by its first string argument.
type Store interface {
	MetricNames(ctx context.Context, tenantID string, from, to time.Time) ([]string, error)
	QueryRange(ctx context.Context, tenantID string, q domain.RangeQuery) ([]domain.Series, error)
	SearchLogs(ctx context.Context, tenantID string, q domain.LogQuery) ([]domain.LogEntry, error)
	ListTraces(ctx context.Context, tenantID string, q domain.TraceQuery) ([]domain.SpanEntry, error)
	GetTrace(ctx context.Context, tenantID, traceID string) ([]domain.SpanEntry, error)
}
