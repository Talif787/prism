// Package otlphttp implements the OTLP/HTTP receiver for metrics, logs, and
// traces. It accepts protobuf (the default wire format) and JSON, decodes the
// export request, computes point counts and metric series fingerprints, and hands
// the request to the ingest pipeline. The value placed on Kafka is always
// protobuf, so the consumer decodes a single format.
package otlphttp

import (
	"fmt"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	collogspb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	colmetricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	coltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"

	"github.com/Talif787/prism/internal/ingest/domain"
)

const (
	contentTypeProtobuf = "application/x-protobuf"
	contentTypeJSON     = "application/json"
)

// decoded carries the results of parsing an export request: the canonical
// protobuf bytes to buffer, the data point count, and metric fingerprints.
type decoded struct {
	payload      []byte
	pointCount   int
	fingerprints [][]byte
}

func decodeMetrics(body []byte, contentType string) (decoded, error) {
	req := &colmetricspb.ExportMetricsServiceRequest{}
	if err := unmarshal(body, contentType, req); err != nil {
		return decoded{}, err
	}
	payload, err := canonicalBytes(body, contentType, req)
	if err != nil {
		return decoded{}, err
	}
	rms := req.GetResourceMetrics()
	return decoded{
		payload:      payload,
		pointCount:   domain.CountMetricPoints(rms),
		fingerprints: domain.MetricFingerprints(rms),
	}, nil
}

func decodeTraces(body []byte, contentType string) (decoded, error) {
	req := &coltracepb.ExportTraceServiceRequest{}
	if err := unmarshal(body, contentType, req); err != nil {
		return decoded{}, err
	}
	payload, err := canonicalBytes(body, contentType, req)
	if err != nil {
		return decoded{}, err
	}
	return decoded{payload: payload, pointCount: domain.CountSpans(req.GetResourceSpans())}, nil
}

func decodeLogs(body []byte, contentType string) (decoded, error) {
	req := &collogspb.ExportLogsServiceRequest{}
	if err := unmarshal(body, contentType, req); err != nil {
		return decoded{}, err
	}
	payload, err := canonicalBytes(body, contentType, req)
	if err != nil {
		return decoded{}, err
	}
	return decoded{payload: payload, pointCount: domain.CountLogs(req.GetResourceLogs())}, nil
}

func unmarshal(body []byte, contentType string, msg proto.Message) error {
	if len(body) == 0 {
		return domain.ErrEmptyPayload
	}
	switch normalizeContentType(contentType) {
	case contentTypeJSON:
		if err := protojson.Unmarshal(body, msg); err != nil {
			return fmt.Errorf("%w: %v", domain.ErrMalformedPayload, err)
		}
	default: // protobuf is the default per the OTLP/HTTP spec
		if err := proto.Unmarshal(body, msg); err != nil {
			return fmt.Errorf("%w: %v", domain.ErrMalformedPayload, err)
		}
	}
	return nil
}

// canonicalBytes returns protobuf bytes for the buffer. When the client sent
// protobuf we reuse the original body; when it sent JSON we re-marshal to protobuf.
func canonicalBytes(body []byte, contentType string, msg proto.Message) ([]byte, error) {
	if normalizeContentType(contentType) == contentTypeJSON {
		return proto.Marshal(msg)
	}
	return body, nil
}

func normalizeContentType(ct string) string {
	if len(ct) >= len(contentTypeJSON) && ct[:len(contentTypeJSON)] == contentTypeJSON {
		return contentTypeJSON
	}
	return contentTypeProtobuf
}
