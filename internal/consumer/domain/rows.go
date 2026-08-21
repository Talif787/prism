// Package domain is the consumer bounded context: the ClickHouse row model and
// the pure functions that decode OTLP payloads into rows. It depends only on the
// OTLP proto types and the standard library, so transforms are unit-testable
// without Kafka or ClickHouse.
package domain

import "time"

// MetricRow is one numeric metric data point (gauge or sum).
type MetricRow struct {
	TenantID           string
	MetricName         string
	MetricType         string
	Timestamp          time.Time
	Value              float64
	Attributes         map[string]string
	ResourceAttributes map[string]string
	ScopeName          string
	SeriesFingerprint  uint64
}

// LogRow is one log record.
type LogRow struct {
	TenantID           string
	Timestamp          time.Time
	ObservedTimestamp  time.Time
	SeverityNumber     int32
	SeverityText       string
	Body               string
	TraceID            string
	SpanID             string
	Attributes         map[string]string
	ResourceAttributes map[string]string
	ScopeName          string
	LogID              uint64
}

// SpanRow is one span.
type SpanRow struct {
	TenantID           string
	TraceID            string
	SpanID             string
	ParentSpanID       string
	Name               string
	Kind               string
	StartTime          time.Time
	EndTime            time.Time
	DurationNs         uint64
	StatusCode         string
	StatusMessage      string
	Attributes         map[string]string
	ResourceAttributes map[string]string
	ScopeName          string
}
