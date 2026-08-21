//go:build integration

// ClickHouse writer integration tests. Run with:
//
//	go test -tags=integration ./test/integration/...
//
// A Docker daemon must be available.
package integration

import (
	"context"
	"testing"
	"time"

	tcclickhouse "github.com/testcontainers/testcontainers-go/modules/clickhouse"

	"github.com/Talif787/prism/internal/consumer/domain"
	"github.com/Talif787/prism/internal/consumer/infra/chwriter"
	platformch "github.com/Talif787/prism/internal/platform/clickhouse"
	chmigrations "github.com/Talif787/prism/migrations/clickhouse"
)

func clickhouseConn(t *testing.T) *platformch.Conn {
	t.Helper()
	ctx := context.Background()
	container, err := tcclickhouse.Run(ctx, "clickhouse/clickhouse-server:24.8-alpine",
		tcclickhouse.WithUsername("test"),
		tcclickhouse.WithPassword("test"),
		tcclickhouse.WithDatabase("testdb"),
	)
	if err != nil {
		t.Fatalf("start clickhouse: %v", err)
	}
	t.Cleanup(func() { _ = container.Terminate(ctx) })

	host, err := container.ConnectionHost(ctx)
	if err != nil {
		t.Fatalf("connection host: %v", err)
	}

	conn, err := platformch.New(ctx, platformch.Config{
		Addr: host, Database: "testdb", Username: "test", Password: "test",
		DialTimeout: 10 * time.Second, MaxOpenConns: 4,
	})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	if err := conn.Migrate(ctx, chmigrations.FS, "0001_init.sql"); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return conn
}

func count(t *testing.T, conn *platformch.Conn, query string) uint64 {
	t.Helper()
	var n uint64
	if err := conn.QueryRow(context.Background(), query).Scan(&n); err != nil {
		t.Fatalf("count query %q: %v", query, err)
	}
	return n
}

func TestClickHouseWriter_RoundTrips(t *testing.T) {
	conn := clickhouseConn(t)
	w := chwriter.New(conn)
	ctx := context.Background()
	now := time.Now().UTC()

	metrics := []domain.MetricRow{
		{TenantID: "t1", MetricName: "cpu", MetricType: "gauge", Timestamp: now, Value: 0.5,
			Attributes: map[string]string{"host": "a"}, ResourceAttributes: map[string]string{"service.name": "api"},
			ScopeName: "lib", SeriesFingerprint: 111},
		{TenantID: "t1", MetricName: "cpu", MetricType: "gauge", Timestamp: now, Value: 0.7,
			Attributes: map[string]string{"host": "b"}, ResourceAttributes: map[string]string{"service.name": "api"},
			ScopeName: "lib", SeriesFingerprint: 222},
	}
	if err := w.WriteMetrics(ctx, metrics); err != nil {
		t.Fatalf("write metrics: %v", err)
	}

	logs := []domain.LogRow{
		{TenantID: "t1", Timestamp: now, ObservedTimestamp: now, SeverityNumber: 17, SeverityText: "ERROR",
			Body: "boom", TraceID: "aabb", SpanID: "cc", Attributes: map[string]string{"k": "v"},
			ResourceAttributes: map[string]string{"service.name": "api"}, ScopeName: "lib", LogID: 999},
	}
	if err := w.WriteLogs(ctx, logs); err != nil {
		t.Fatalf("write logs: %v", err)
	}

	spans := []domain.SpanRow{
		{TenantID: "t1", TraceID: "aabb", SpanID: "cc", ParentSpanID: "dd", Name: "GET /x", Kind: "SERVER",
			StartTime: now, EndTime: now.Add(500 * time.Nanosecond), DurationNs: 500, StatusCode: "OK",
			StatusMessage: "ok", Attributes: map[string]string{"http.method": "GET"},
			ResourceAttributes: map[string]string{"service.name": "api"}, ScopeName: "lib"},
	}
	if err := w.WriteSpans(ctx, spans); err != nil {
		t.Fatalf("write spans: %v", err)
	}

	if got := count(t, conn, "SELECT count() FROM metrics_numeric WHERE tenant_id = 't1'"); got != 2 {
		t.Fatalf("metrics count = %d, want 2", got)
	}
	if got := count(t, conn, "SELECT count() FROM logs WHERE tenant_id = 't1'"); got != 1 {
		t.Fatalf("logs count = %d, want 1", got)
	}
	if got := count(t, conn, "SELECT count() FROM spans WHERE tenant_id = 't1'"); got != 1 {
		t.Fatalf("spans count = %d, want 1", got)
	}

	// A representative aggregate query over the metric rows.
	var total float64
	if err := conn.QueryRow(ctx,
		"SELECT sum(value) FROM metrics_numeric WHERE tenant_id = 't1' AND metric_name = 'cpu'").
		Scan(&total); err != nil {
		t.Fatalf("aggregate query: %v", err)
	}
	if total < 1.19 || total > 1.21 {
		t.Fatalf("sum(value) = %v, want ~1.2", total)
	}
}

// Re-writing an identical span (redelivery) must not create a second logical row
// after a merge, thanks to ReplacingMergeTree on the span's natural key.
func TestClickHouseWriter_DedupOnRedelivery(t *testing.T) {
	conn := clickhouseConn(t)
	w := chwriter.New(conn)
	ctx := context.Background()
	now := time.Now().UTC()

	span := domain.SpanRow{
		TenantID: "t1", TraceID: "ff", SpanID: "01", Name: "op", Kind: "INTERNAL",
		StartTime: now, EndTime: now, StatusCode: "OK",
		Attributes: map[string]string{}, ResourceAttributes: map[string]string{}, ScopeName: "lib",
	}
	if err := w.WriteSpans(ctx, []domain.SpanRow{span}); err != nil {
		t.Fatalf("write 1: %v", err)
	}
	if err := w.WriteSpans(ctx, []domain.SpanRow{span}); err != nil {
		t.Fatalf("write 2: %v", err)
	}
	// FINAL forces the ReplacingMergeTree collapse at query time.
	if got := count(t, conn, "SELECT count() FROM spans FINAL WHERE tenant_id = 't1'"); got != 1 {
		t.Fatalf("deduped span count = %d, want 1", got)
	}
}
