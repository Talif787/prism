// Package app is the consumer application layer: a generic batching consume loop
// and the Writer port it flushes to. It is storage-agnostic (the ClickHouse
// adapter implements Writer) and transport-agnostic beyond the Kafka reader.
package app

import (
	"context"

	"github.com/Talif787/prism/internal/consumer/domain"
)

// Writer persists batches of rows to the columnar store.
type Writer interface {
	WriteMetrics(ctx context.Context, rows []domain.MetricRow) error
	WriteLogs(ctx context.Context, rows []domain.LogRow) error
	WriteSpans(ctx context.Context, rows []domain.SpanRow) error
}
