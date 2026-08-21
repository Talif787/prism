// Package chsource meters accepted telemetry by counting rows in the ClickHouse
// tables of record, grouped by tenant and by window. Each signal maps to its table
// and time column; metrics and logs key on timestamp, traces on start_time.
package chsource

import (
	"context"
	"fmt"
	"time"

	"github.com/Talif787/prism/internal/metering/domain"
	"github.com/Talif787/prism/internal/platform/clickhouse"
)

type Source struct{ conn *clickhouse.Conn }

func New(conn *clickhouse.Conn) *Source { return &Source{conn: conn} }

func signalTable(sig domain.Signal) (table, timeCol string, ok bool) {
	switch sig {
	case domain.SignalMetrics:
		return "metrics_numeric", "timestamp", true
	case domain.SignalLogs:
		return "logs", "timestamp", true
	case domain.SignalTraces:
		return "spans", "start_time", true
	default:
		return "", "", false
	}
}

func (s *Source) CountUsage(ctx context.Context, sig domain.Signal, from, to time.Time, intervalSeconds int) ([]domain.UsageRecord, error) {
	table, timeCol, ok := signalTable(sig)
	if !ok {
		return nil, fmt.Errorf("unknown signal %q", sig)
	}
	if intervalSeconds < 1 {
		intervalSeconds = 1
	}
	// intervalSeconds, table, and timeCol are server-side constants, not user input,
	// so they are inlined safely; from and to are bound parameters.
	sql := fmt.Sprintf(
		"SELECT tenant_id, toStartOfInterval(%s, toIntervalSecond(%d)) AS w, toInt64(count()) AS c "+
			"FROM %s WHERE %s >= ? AND %s < ? GROUP BY tenant_id, w ORDER BY tenant_id, w",
		timeCol, intervalSeconds, table, timeCol, timeCol)

	rows, err := s.conn.Query(ctx, sql, from, to)
	if err != nil {
		return nil, fmt.Errorf("count usage: %w", err)
	}
	defer rows.Close()

	var out []domain.UsageRecord
	for rows.Next() {
		var tenantID string
		var w time.Time
		var c int64
		if err := rows.Scan(&tenantID, &w, &c); err != nil {
			return nil, err
		}
		out = append(out, domain.UsageRecord{TenantID: tenantID, Signal: sig, WindowStart: w.UTC(), Count: c})
	}
	return out, rows.Err()
}
