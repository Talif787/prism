package domain

import (
	"encoding/hex"
	"strconv"
	"strings"

	"google.golang.org/protobuf/proto"

	collogspb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	colmetricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	coltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
)

// DecodeMetrics unmarshals a Kafka value into OTLP resource metrics.
func DecodeMetrics(value []byte) ([]*metricspb.ResourceMetrics, error) {
	req := &colmetricspb.ExportMetricsServiceRequest{}
	if err := proto.Unmarshal(value, req); err != nil {
		return nil, err
	}
	return req.GetResourceMetrics(), nil
}

// DecodeLogs unmarshals a Kafka value into OTLP resource logs.
func DecodeLogs(value []byte) ([]*logspb.ResourceLogs, error) {
	req := &collogspb.ExportLogsServiceRequest{}
	if err := proto.Unmarshal(value, req); err != nil {
		return nil, err
	}
	return req.GetResourceLogs(), nil
}

// DecodeTraces unmarshals a Kafka value into OTLP resource spans.
func DecodeTraces(value []byte) ([]*tracepb.ResourceSpans, error) {
	req := &coltracepb.ExportTraceServiceRequest{}
	if err := proto.Unmarshal(value, req); err != nil {
		return nil, err
	}
	return req.GetResourceSpans(), nil
}

// TransformMetrics flattens gauge and sum data points into rows. Histogram,
// exponential histogram, and summary types are skipped for now (they land in a
// later phase with their own bucket-aware tables).
func TransformMetrics(tenantID string, rms []*metricspb.ResourceMetrics) []MetricRow {
	var rows []MetricRow
	for _, rm := range rms {
		resourceAttrs := rm.GetResource().GetAttributes()
		resourceMap := attrsToMap(resourceAttrs)
		resourceKey := canonicalAttrs(resourceAttrs)
		for _, sm := range rm.GetScopeMetrics() {
			scope := sm.GetScope().GetName()
			for _, m := range sm.GetMetrics() {
				name := m.GetName()
				switch {
				case m.GetGauge() != nil:
					for _, dp := range m.GetGauge().GetDataPoints() {
						rows = append(rows, numberRow(tenantID, name, "gauge", scope, resourceMap, resourceKey, dp))
					}
				case m.GetSum() != nil:
					for _, dp := range m.GetSum().GetDataPoints() {
						rows = append(rows, numberRow(tenantID, name, "sum", scope, resourceMap, resourceKey, dp))
					}
				}
			}
		}
	}
	return rows
}

func numberRow(tenantID, name, metricType, scope string, resourceMap map[string]string, resourceKey string, dp *metricspb.NumberDataPoint) MetricRow {
	var value float64
	switch v := dp.GetValue().(type) {
	case *metricspb.NumberDataPoint_AsDouble:
		value = v.AsDouble
	case *metricspb.NumberDataPoint_AsInt:
		value = float64(v.AsInt)
	}
	attrs := dp.GetAttributes()
	return MetricRow{
		TenantID:           tenantID,
		MetricName:         name,
		MetricType:         metricType,
		Timestamp:          tsFromNano(dp.GetTimeUnixNano()),
		Value:              value,
		Attributes:         attrsToMap(attrs),
		ResourceAttributes: resourceMap,
		ScopeName:          scope,
		SeriesFingerprint:  fnv64(resourceKey, name, canonicalAttrs(attrs)),
	}
}

// TransformLogs flattens log records into rows.
func TransformLogs(tenantID string, rls []*logspb.ResourceLogs) []LogRow {
	var rows []LogRow
	for _, rl := range rls {
		resourceMap := attrsToMap(rl.GetResource().GetAttributes())
		for _, sl := range rl.GetScopeLogs() {
			scope := sl.GetScope().GetName()
			for _, lr := range sl.GetLogRecords() {
				traceID := hex.EncodeToString(lr.GetTraceId())
				spanID := hex.EncodeToString(lr.GetSpanId())
				body := anyValueString(lr.GetBody())
				attrs := lr.GetAttributes()
				ts := lr.GetTimeUnixNano()
				rows = append(rows, LogRow{
					TenantID:           tenantID,
					Timestamp:          tsFromNano(ts),
					ObservedTimestamp:  tsFromNano(lr.GetObservedTimeUnixNano()),
					SeverityNumber:     int32(lr.GetSeverityNumber()),
					SeverityText:       lr.GetSeverityText(),
					Body:               body,
					TraceID:            traceID,
					SpanID:             spanID,
					Attributes:         attrsToMap(attrs),
					ResourceAttributes: resourceMap,
					ScopeName:          scope,
					LogID:              fnv64(strconv.FormatUint(ts, 10), body, traceID, spanID, canonicalAttrs(attrs)),
				})
			}
		}
	}
	return rows
}

// TransformTraces flattens spans into rows.
func TransformTraces(tenantID string, rss []*tracepb.ResourceSpans) []SpanRow {
	var rows []SpanRow
	for _, rs := range rss {
		resourceMap := attrsToMap(rs.GetResource().GetAttributes())
		for _, ss := range rs.GetScopeSpans() {
			scope := ss.GetScope().GetName()
			for _, sp := range ss.GetSpans() {
				start := sp.GetStartTimeUnixNano()
				end := sp.GetEndTimeUnixNano()
				var dur uint64
				if end > start {
					dur = end - start
				}
				rows = append(rows, SpanRow{
					TenantID:           tenantID,
					TraceID:            hex.EncodeToString(sp.GetTraceId()),
					SpanID:             hex.EncodeToString(sp.GetSpanId()),
					ParentSpanID:       hex.EncodeToString(sp.GetParentSpanId()),
					Name:               sp.GetName(),
					Kind:               strings.TrimPrefix(sp.GetKind().String(), "SPAN_KIND_"),
					StartTime:          tsFromNano(start),
					EndTime:            tsFromNano(end),
					DurationNs:         dur,
					StatusCode:         strings.TrimPrefix(sp.GetStatus().GetCode().String(), "STATUS_CODE_"),
					StatusMessage:      sp.GetStatus().GetMessage(),
					Attributes:         attrsToMap(sp.GetAttributes()),
					ResourceAttributes: resourceMap,
					ScopeName:          scope,
				})
			}
		}
	}
	return rows
}
