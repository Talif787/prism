//go:build integration

// Query store integration tests. Seed ClickHouse through the Phase 3 writer, then
// read back through the query store. Run with:
//
//	go test -tags=integration ./test/integration/...
package integration

import (
	"context"
	"testing"
	"time"

	cdomain "github.com/Talif787/prism/internal/consumer/domain"
	"github.com/Talif787/prism/internal/consumer/infra/chwriter"
	qdomain "github.com/Talif787/prism/internal/query/domain"
	"github.com/Talif787/prism/internal/query/infra/chstore"
)

func TestQueryStore_ReadsSeededData(t *testing.T) {
	conn := clickhouseConn(t) // helper from clickhouse_test.go
	ctx := context.Background()
	w := chwriter.New(conn)
	store := chstore.New(conn)

	base := time.Now().UTC().Truncate(time.Second)
	from := base.Add(-time.Hour)
	to := base.Add(time.Hour)

	// Two series for one metric. The /a series has two distinct timestamps so both
	// samples survive (same timestamp would dedup under ReplacingMergeTree).
	metrics := []cdomain.MetricRow{
		{TenantID: "t1", MetricName: "cpu", MetricType: "gauge", Timestamp: base, Value: 10,
			Attributes: map[string]string{"route": "/a"}, ResourceAttributes: map[string]string{}, ScopeName: "lib", SeriesFingerprint: 1},
		{TenantID: "t1", MetricName: "cpu", MetricType: "gauge", Timestamp: base.Add(time.Second), Value: 20,
			Attributes: map[string]string{"route": "/a"}, ResourceAttributes: map[string]string{}, ScopeName: "lib", SeriesFingerprint: 1},
		{TenantID: "t1", MetricName: "cpu", MetricType: "gauge", Timestamp: base, Value: 5,
			Attributes: map[string]string{"route": "/b"}, ResourceAttributes: map[string]string{}, ScopeName: "lib", SeriesFingerprint: 2},
	}
	if err := w.WriteMetrics(ctx, metrics); err != nil {
		t.Fatalf("seed metrics: %v", err)
	}

	logs := []cdomain.LogRow{
		{TenantID: "t1", Timestamp: base, ObservedTimestamp: base, SeverityNumber: 17, SeverityText: "ERROR",
			Body: "payment gateway timeout", Attributes: map[string]string{}, ResourceAttributes: map[string]string{}, ScopeName: "lib", LogID: 1},
		{TenantID: "t1", Timestamp: base, ObservedTimestamp: base, SeverityNumber: 9, SeverityText: "INFO",
			Body: "order created", Attributes: map[string]string{}, ResourceAttributes: map[string]string{}, ScopeName: "lib", LogID: 2},
	}
	if err := w.WriteLogs(ctx, logs); err != nil {
		t.Fatalf("seed logs: %v", err)
	}

	spans := []cdomain.SpanRow{
		{TenantID: "t1", TraceID: "aa", SpanID: "01", ParentSpanID: "", Name: "GET /checkout", Kind: "SERVER",
			StartTime: base, EndTime: base.Add(100), DurationNs: 100, StatusCode: "OK",
			Attributes: map[string]string{}, ResourceAttributes: map[string]string{"service.name": "checkout"}, ScopeName: "lib"},
		{TenantID: "t1", TraceID: "aa", SpanID: "02", ParentSpanID: "01", Name: "db.query", Kind: "CLIENT",
			StartTime: base, EndTime: base.Add(50), DurationNs: 50, StatusCode: "OK",
			Attributes: map[string]string{}, ResourceAttributes: map[string]string{"service.name": "checkout"}, ScopeName: "lib"},
	}
	if err := w.WriteSpans(ctx, spans); err != nil {
		t.Fatalf("seed spans: %v", err)
	}

	// Metric names.
	names, err := store.MetricNames(ctx, "t1", from, to)
	if err != nil {
		t.Fatalf("metric names: %v", err)
	}
	if len(names) != 1 || names[0] != "cpu" {
		t.Fatalf("expected [cpu], got %v", names)
	}

	// Range query grouped by route, averaged into one bucket.
	series, err := store.QueryRange(ctx, "t1", qdomain.RangeQuery{
		Metric: "cpu", From: from, To: to, Step: time.Hour, Agg: "avg", GroupBy: []string{"route"},
	})
	if err != nil {
		t.Fatalf("range query: %v", err)
	}
	if len(series) != 2 {
		t.Fatalf("expected 2 series, got %d", len(series))
	}
	got := map[string]float64{}
	for _, s := range series {
		if len(s.Points) != 1 {
			t.Fatalf("expected 1 point per series, got %d for %v", len(s.Points), s.Labels)
		}
		got[s.Labels["route"]] = s.Points[0].V
	}
	if got["/a"] != 15 {
		t.Fatalf("route /a avg = %v, want 15", got["/a"])
	}
	if got["/b"] != 5 {
		t.Fatalf("route /b avg = %v, want 5", got["/b"])
	}

	// Log search with severity floor and substring.
	found, err := store.SearchLogs(ctx, "t1", qdomain.LogQuery{From: from, To: to, MinSeverity: 17, Contains: "timeout", Limit: 100})
	if err != nil {
		t.Fatalf("search logs: %v", err)
	}
	if len(found) != 1 || found[0].Body != "payment gateway timeout" {
		t.Fatalf("expected the one error log, got %+v", found)
	}

	// Trace listing returns only the root span.
	roots, err := store.ListTraces(ctx, "t1", qdomain.TraceQuery{From: from, To: to, Limit: 100})
	if err != nil {
		t.Fatalf("list traces: %v", err)
	}
	if len(roots) != 1 || roots[0].SpanID != "01" || roots[0].Service != "checkout" {
		t.Fatalf("expected one root span for checkout, got %+v", roots)
	}

	// Trace by id returns both spans.
	all, err := store.GetTrace(ctx, "t1", "aa")
	if err != nil {
		t.Fatalf("get trace: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 spans in trace aa, got %d", len(all))
	}
}
