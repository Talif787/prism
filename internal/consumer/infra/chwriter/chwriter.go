// Package chwriter implements the consumer Writer port against ClickHouse using
// batched inserts. Each flush prepares one batch, appends the rows, and sends it,
// which is the efficient path for ClickHouse's columnar engine.
package chwriter

import (
	"context"

	"github.com/Talif787/prism/internal/consumer/domain"
	"github.com/Talif787/prism/internal/platform/clickhouse"
)

type Writer struct {
	conn *clickhouse.Conn
}

func New(conn *clickhouse.Conn) *Writer { return &Writer{conn: conn} }

func (w *Writer) WriteMetrics(ctx context.Context, rows []domain.MetricRow) error {
	if len(rows) == 0 {
		return nil
	}
	batch, err := w.conn.PrepareBatch(ctx, `INSERT INTO metrics_numeric
		(tenant_id, metric_name, metric_type, timestamp, value, attributes, resource_attributes, scope_name, series_fingerprint)`)
	if err != nil {
		return err
	}
	for _, r := range rows {
		if err := batch.Append(
			r.TenantID, r.MetricName, r.MetricType, r.Timestamp, r.Value,
			r.Attributes, r.ResourceAttributes, r.ScopeName, r.SeriesFingerprint,
		); err != nil {
			return err
		}
	}
	return batch.Send()
}

func (w *Writer) WriteLogs(ctx context.Context, rows []domain.LogRow) error {
	if len(rows) == 0 {
		return nil
	}
	batch, err := w.conn.PrepareBatch(ctx, `INSERT INTO logs
		(tenant_id, timestamp, observed_timestamp, severity_number, severity_text, body, trace_id, span_id, attributes, resource_attributes, scope_name, log_id)`)
	if err != nil {
		return err
	}
	for _, r := range rows {
		if err := batch.Append(
			r.TenantID, r.Timestamp, r.ObservedTimestamp, r.SeverityNumber, r.SeverityText,
			r.Body, r.TraceID, r.SpanID, r.Attributes, r.ResourceAttributes, r.ScopeName, r.LogID,
		); err != nil {
			return err
		}
	}
	return batch.Send()
}

func (w *Writer) WriteSpans(ctx context.Context, rows []domain.SpanRow) error {
	if len(rows) == 0 {
		return nil
	}
	batch, err := w.conn.PrepareBatch(ctx, `INSERT INTO spans
		(tenant_id, trace_id, span_id, parent_span_id, name, kind, start_time, end_time, duration_ns, status_code, status_message, attributes, resource_attributes, scope_name)`)
	if err != nil {
		return err
	}
	for _, r := range rows {
		if err := batch.Append(
			r.TenantID, r.TraceID, r.SpanID, r.ParentSpanID, r.Name, r.Kind,
			r.StartTime, r.EndTime, r.DurationNs, r.StatusCode, r.StatusMessage,
			r.Attributes, r.ResourceAttributes, r.ScopeName,
		); err != nil {
			return err
		}
	}
	return batch.Send()
}
