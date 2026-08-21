// Package domain is the query bounded context: the read model returned to callers
// and the request types with their cost-guard validation. It depends only on the
// standard library, so validation is unit-testable in isolation.
package domain

import (
	"errors"
	"fmt"
	"time"
)

// ErrInvalidQuery is returned when a request violates a cost guard or is malformed.
var ErrInvalidQuery = errors.New("invalid query")

// Cost guards. These bound the work any single query can ask ClickHouse to do,
// protecting a shared cluster from accidental or hostile expensive queries.
const (
	MaxRange     = 31 * 24 * time.Hour // widest time window a query may span
	MaxBuckets   = 11000               // most time buckets a range query may return
	MinStep      = time.Second         // finest bucket width allowed
	MaxGroupBy   = 8                   // most attributes a range query may group by
	MaxLimit     = 1000                // hard cap on rows for logs and traces
	DefaultLimit = 200
)

// Aggregations maps the public aggregation name to a ClickHouse expression over
// the value column. "last" is the latest sample in the bucket by timestamp.
var Aggregations = map[string]string{
	"avg":   "avg(value)",
	"sum":   "sum(value)",
	"min":   "min(value)",
	"max":   "max(value)",
	"count": "count()",
	"last":  "argMax(value, timestamp)",
}

// RangeQuery is a time-bucketed aggregation over one metric.
type RangeQuery struct {
	Metric  string
	From    time.Time
	To      time.Time
	Step    time.Duration
	Agg     string
	GroupBy []string
	Filters map[string]string
}

// Validate applies the cost guards and normalizes defaults.
func (q *RangeQuery) Validate() error {
	if q.Metric == "" {
		return fmt.Errorf("%w: metric name is required", ErrInvalidQuery)
	}
	if !q.To.After(q.From) {
		return fmt.Errorf("%w: 'to' must be after 'from'", ErrInvalidQuery)
	}
	if q.To.Sub(q.From) > MaxRange {
		return fmt.Errorf("%w: time range exceeds the %s maximum", ErrInvalidQuery, MaxRange)
	}
	if q.Step < MinStep {
		return fmt.Errorf("%w: step must be at least %s", ErrInvalidQuery, MinStep)
	}
	if buckets := q.To.Sub(q.From) / q.Step; buckets > MaxBuckets {
		return fmt.Errorf("%w: %d buckets exceeds the %d maximum; widen the step", ErrInvalidQuery, buckets, MaxBuckets)
	}
	if len(q.GroupBy) > MaxGroupBy {
		return fmt.Errorf("%w: at most %d group-by attributes are allowed", ErrInvalidQuery, MaxGroupBy)
	}
	if q.Agg == "" {
		q.Agg = "avg"
	}
	if _, ok := Aggregations[q.Agg]; !ok {
		return fmt.Errorf("%w: unknown aggregation %q", ErrInvalidQuery, q.Agg)
	}
	return nil
}

// Point is one sample in a series.
type Point struct {
	T time.Time `json:"t"`
	V float64   `json:"v"`
}

// Series is a labeled sequence of samples.
type Series struct {
	Labels map[string]string `json:"labels"`
	Points []Point           `json:"points"`
}

// LogQuery searches log records in a window.
type LogQuery struct {
	From        time.Time
	To          time.Time
	MinSeverity int32
	Contains    string
	Limit       int
}

func (q *LogQuery) Validate() error {
	if !q.To.After(q.From) {
		return fmt.Errorf("%w: 'to' must be after 'from'", ErrInvalidQuery)
	}
	if q.To.Sub(q.From) > MaxRange {
		return fmt.Errorf("%w: time range exceeds the %s maximum", ErrInvalidQuery, MaxRange)
	}
	q.Limit = clampLimit(q.Limit)
	return nil
}

// LogEntry is one returned log record.
type LogEntry struct {
	Timestamp      time.Time         `json:"timestamp"`
	SeverityNumber int32             `json:"severity_number"`
	SeverityText   string            `json:"severity_text"`
	Body           string            `json:"body"`
	TraceID        string            `json:"trace_id,omitempty"`
	SpanID         string            `json:"span_id,omitempty"`
	Attributes     map[string]string `json:"attributes"`
}

// TraceQuery lists root spans in a window.
type TraceQuery struct {
	From    time.Time
	To      time.Time
	Service string
	Limit   int
}

func (q *TraceQuery) Validate() error {
	if !q.To.After(q.From) {
		return fmt.Errorf("%w: 'to' must be after 'from'", ErrInvalidQuery)
	}
	if q.To.Sub(q.From) > MaxRange {
		return fmt.Errorf("%w: time range exceeds the %s maximum", ErrInvalidQuery, MaxRange)
	}
	q.Limit = clampLimit(q.Limit)
	return nil
}

// SpanEntry is one returned span.
type SpanEntry struct {
	TraceID      string    `json:"trace_id"`
	SpanID       string    `json:"span_id"`
	ParentSpanID string    `json:"parent_span_id,omitempty"`
	Name         string    `json:"name"`
	Kind         string    `json:"kind"`
	StartTime    time.Time `json:"start_time"`
	DurationNs   uint64    `json:"duration_ns"`
	StatusCode   string    `json:"status_code"`
	Service      string    `json:"service,omitempty"`
}

func clampLimit(n int) int {
	if n <= 0 {
		return DefaultLimit
	}
	if n > MaxLimit {
		return MaxLimit
	}
	return n
}
