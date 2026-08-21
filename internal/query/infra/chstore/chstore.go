// Package chstore implements the query Store port against ClickHouse. Every query
// is tenant-scoped by a bound tenant_id parameter, and all user-supplied values
// (metric names, attribute keys and values, search text) are passed as positional
// parameters rather than interpolated, so the read path is injection-safe.
package chstore

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Talif787/prism/internal/platform/clickhouse"
	"github.com/Talif787/prism/internal/query/domain"
)

type Store struct {
	conn *clickhouse.Conn
}

func New(conn *clickhouse.Conn) *Store { return &Store{conn: conn} }

func (s *Store) MetricNames(ctx context.Context, tenantID string, from, to time.Time) ([]string, error) {
	rows, err := s.conn.Query(ctx,
		`SELECT DISTINCT metric_name FROM metrics_numeric
		 WHERE tenant_id = ? AND timestamp >= ? AND timestamp < ?
		 ORDER BY metric_name`,
		tenantID, from, to)
	if err != nil {
		return nil, fmt.Errorf("metric names: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out = append(out, name)
	}
	return out, rows.Err()
}

func (s *Store) QueryRange(ctx context.Context, tenantID string, q domain.RangeQuery) ([]domain.Series, error) {
	stepSeconds := int64(q.Step / time.Second)
	if stepSeconds < 1 {
		stepSeconds = 1
	}

	var sel strings.Builder
	// stepSeconds is a server-computed, validated integer, so it is inlined
	// safely; every user-supplied value below is a bound parameter.
	fmt.Fprintf(&sel, "toStartOfInterval(timestamp, toIntervalSecond(%d)) AS bucket", stepSeconds)
	args := make([]any, 0, len(q.GroupBy)+4+2*len(q.Filters))
	for i, key := range q.GroupBy {
		fmt.Fprintf(&sel, ", attributes[?] AS l%d", i)
		args = append(args, key)
	}
	sel.WriteString(", toFloat64(" + domain.Aggregations[q.Agg] + ") AS v")

	where := "WHERE tenant_id = ? AND metric_name = ? AND timestamp >= ? AND timestamp < ?"
	args = append(args, tenantID, q.Metric, q.From, q.To)
	for key, val := range q.Filters {
		where += " AND attributes[?] = ?"
		args = append(args, key, val)
	}

	groupBy := "GROUP BY bucket"
	for i := range q.GroupBy {
		groupBy += fmt.Sprintf(", l%d", i)
	}

	sql := fmt.Sprintf("SELECT %s FROM metrics_numeric %s %s ORDER BY bucket", sel.String(), where, groupBy)

	rows, err := s.conn.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("range query: %w", err)
	}
	defer rows.Close()

	type acc struct {
		labels map[string]string
		points []domain.Point
	}
	order := make([]string, 0)
	byKey := make(map[string]*acc)

	for rows.Next() {
		var bucket time.Time
		var value float64
		labelVals := make([]string, len(q.GroupBy))
		dest := make([]any, 0, len(q.GroupBy)+2)
		dest = append(dest, &bucket)
		for i := range labelVals {
			dest = append(dest, &labelVals[i])
		}
		dest = append(dest, &value)
		if err := rows.Scan(dest...); err != nil {
			return nil, err
		}

		key := strings.Join(labelVals, "\x00")
		a, ok := byKey[key]
		if !ok {
			labels := make(map[string]string, len(q.GroupBy))
			for i, gk := range q.GroupBy {
				labels[gk] = labelVals[i]
			}
			a = &acc{labels: labels}
			byKey[key] = a
			order = append(order, key)
		}
		a.points = append(a.points, domain.Point{T: bucket.UTC(), V: value})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	series := make([]domain.Series, 0, len(order))
	for _, key := range order {
		a := byKey[key]
		series = append(series, domain.Series{Labels: a.labels, Points: a.points})
	}
	return series, nil
}

func (s *Store) SearchLogs(ctx context.Context, tenantID string, q domain.LogQuery) ([]domain.LogEntry, error) {
	sql := `SELECT timestamp, severity_number, severity_text, body, trace_id, span_id, attributes
	        FROM logs WHERE tenant_id = ? AND timestamp >= ? AND timestamp < ?`
	args := []any{tenantID, q.From, q.To}
	if q.MinSeverity > 0 {
		sql += " AND severity_number >= ?"
		args = append(args, q.MinSeverity)
	}
	if q.Contains != "" {
		sql += " AND positionCaseInsensitive(body, ?) > 0"
		args = append(args, q.Contains)
	}
	sql += fmt.Sprintf(" ORDER BY timestamp DESC LIMIT %d", q.Limit)

	rows, err := s.conn.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("search logs: %w", err)
	}
	defer rows.Close()

	out := make([]domain.LogEntry, 0)
	for rows.Next() {
		var e domain.LogEntry
		if err := rows.Scan(&e.Timestamp, &e.SeverityNumber, &e.SeverityText, &e.Body,
			&e.TraceID, &e.SpanID, &e.Attributes); err != nil {
			return nil, err
		}
		e.Timestamp = e.Timestamp.UTC()
		out = append(out, e)
	}
	return out, rows.Err()
}

const spanSelect = `SELECT trace_id, span_id, parent_span_id, name, kind, start_time, duration_ns, status_code, resource_attributes['service.name'] AS service`

func (s *Store) ListTraces(ctx context.Context, tenantID string, q domain.TraceQuery) ([]domain.SpanEntry, error) {
	sql := spanSelect + ` FROM spans
	        WHERE tenant_id = ? AND start_time >= ? AND start_time < ? AND parent_span_id = ''`
	args := []any{tenantID, q.From, q.To}
	if q.Service != "" {
		sql += " AND resource_attributes['service.name'] = ?"
		args = append(args, q.Service)
	}
	sql += fmt.Sprintf(" ORDER BY start_time DESC LIMIT %d", q.Limit)
	return s.scanSpans(ctx, sql, args...)
}

func (s *Store) GetTrace(ctx context.Context, tenantID, traceID string) ([]domain.SpanEntry, error) {
	sql := spanSelect + ` FROM spans WHERE tenant_id = ? AND trace_id = ? ORDER BY start_time ASC`
	return s.scanSpans(ctx, sql, tenantID, traceID)
}

func (s *Store) scanSpans(ctx context.Context, sql string, args ...any) ([]domain.SpanEntry, error) {
	rows, err := s.conn.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("spans query: %w", err)
	}
	defer rows.Close()

	out := make([]domain.SpanEntry, 0)
	for rows.Next() {
		var e domain.SpanEntry
		if err := rows.Scan(&e.TraceID, &e.SpanID, &e.ParentSpanID, &e.Name, &e.Kind,
			&e.StartTime, &e.DurationNs, &e.StatusCode, &e.Service); err != nil {
			return nil, err
		}
		e.StartTime = e.StartTime.UTC()
		out = append(out, e)
	}
	return out, rows.Err()
}
