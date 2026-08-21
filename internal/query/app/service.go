package app

import (
	"context"
	"sync"
	"time"

	"github.com/Talif787/prism/internal/query/domain"
)

// Service orchestrates query validation, a short-lived metric-name cache, and
// delegation to the store.
type Service struct {
	store Store
	names *nameCache
}

func NewService(store Store, nameCacheTTL time.Duration) *Service {
	return &Service{store: store, names: newNameCache(nameCacheTTL)}
}

// MetricNames returns the distinct metric names for a tenant, served from a
// brief cache. Names change slowly, so the cache is keyed by tenant rather than
// by exact window; the window still bounds the underlying query on a miss.
func (s *Service) MetricNames(ctx context.Context, tenantID string, from, to time.Time) ([]string, error) {
	if v, ok := s.names.get(tenantID); ok {
		return v, nil
	}
	names, err := s.store.MetricNames(ctx, tenantID, from, to)
	if err != nil {
		return nil, err
	}
	s.names.set(tenantID, names)
	return names, nil
}

// QueryRange validates the request against the cost guards, then executes it.
func (s *Service) QueryRange(ctx context.Context, tenantID string, q domain.RangeQuery) ([]domain.Series, error) {
	if err := q.Validate(); err != nil {
		return nil, err
	}
	return s.store.QueryRange(ctx, tenantID, q)
}

func (s *Service) SearchLogs(ctx context.Context, tenantID string, q domain.LogQuery) ([]domain.LogEntry, error) {
	if err := q.Validate(); err != nil {
		return nil, err
	}
	return s.store.SearchLogs(ctx, tenantID, q)
}

func (s *Service) ListTraces(ctx context.Context, tenantID string, q domain.TraceQuery) ([]domain.SpanEntry, error) {
	if err := q.Validate(); err != nil {
		return nil, err
	}
	return s.store.ListTraces(ctx, tenantID, q)
}

func (s *Service) GetTrace(ctx context.Context, tenantID, traceID string) ([]domain.SpanEntry, error) {
	return s.store.GetTrace(ctx, tenantID, traceID)
}

// nameCache is a tiny per-tenant TTL cache for metric-name discovery.
type nameCache struct {
	ttl time.Duration
	mu  sync.Mutex
	m   map[string]nameEntry
}

type nameEntry struct {
	names   []string
	expires time.Time
}

func newNameCache(ttl time.Duration) *nameCache {
	return &nameCache{ttl: ttl, m: make(map[string]nameEntry)}
}

func (c *nameCache) get(tenantID string) ([]string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.m[tenantID]
	if !ok || time.Now().After(e.expires) {
		return nil, false
	}
	return e.names, true
}

func (c *nameCache) set(tenantID string, names []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m[tenantID] = nameEntry{names: names, expires: time.Now().Add(c.ttl)}
}
