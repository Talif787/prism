// Package chreader implements the alerting MetricsReader by reusing the query
// service's ClickHouse store. A condition becomes a single-bucket range query over
// the trailing window, and each series reduces to its latest aggregated value.
package chreader

import (
	"context"
	"time"

	"github.com/Talif787/prism/internal/alerting/app"
	qdomain "github.com/Talif787/prism/internal/query/domain"
)

// queryStore is the subset of the query ClickHouse store the reader needs.
type queryStore interface {
	QueryRange(ctx context.Context, tenantID string, q qdomain.RangeQuery) ([]qdomain.Series, error)
}

type Reader struct{ store queryStore }

func New(store queryStore) *Reader { return &Reader{store: store} }

func (r *Reader) Read(ctx context.Context, tenantID string, cond app.Condition) ([]app.SeriesValue, error) {
	now := time.Now().UTC()
	q := qdomain.RangeQuery{
		Metric:  cond.Metric,
		From:    now.Add(-cond.Window),
		To:      now,
		Step:    cond.Window, // one bucket over the window
		Agg:     cond.Agg,
		GroupBy: cond.GroupBy,
		Filters: cond.Filters,
	}
	series, err := r.store.QueryRange(ctx, tenantID, q)
	if err != nil {
		return nil, err
	}
	out := make([]app.SeriesValue, 0, len(series))
	for _, s := range series {
		if len(s.Points) == 0 {
			continue
		}
		out = append(out, app.SeriesValue{Labels: s.Labels, Value: s.Points[len(s.Points)-1].V})
	}
	return out, nil
}
