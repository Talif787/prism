// Package domain holds the ingest bounded context: the signal taxonomy, the
// Kafka envelope, and the OTLP walking helpers used to count data points and
// fingerprint metric series. It depends only on the OTLP proto types.
package domain

// Signal identifies the telemetry kind. Each maps to a dedicated Kafka topic so
// consumers can scale per signal.
type Signal string

const (
	SignalMetrics Signal = "metrics"
	SignalLogs    Signal = "logs"
	SignalTraces  Signal = "traces"
)

func (s Signal) Valid() bool {
	switch s {
	case SignalMetrics, SignalLogs, SignalTraces:
		return true
	default:
		return false
	}
}

// SchemaVersion is stamped on every Kafka message so the consumer can evolve the
// envelope format without ambiguity.
const SchemaVersion = "1"

// Header keys used on Kafka messages. Metadata travels in headers; the value is
// the raw OTLP protobuf payload.
const (
	HeaderTenantID    = "tenant_id"
	HeaderSignal      = "signal"
	HeaderReceivedAt  = "received_at_unix_nano"
	HeaderContentType = "content_type"
	HeaderSchema      = "schema_version"
	HeaderPointCount  = "point_count"
)

// ContentTypeProtobuf is the canonical wire format placed on Kafka regardless of
// whether the client sent protobuf or JSON, so consumers only decode one format.
const ContentTypeProtobuf = "application/x-protobuf"
