package domain

import (
	"testing"

	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
)

func strKV(k, v string) *commonpb.KeyValue {
	return &commonpb.KeyValue{Key: k, Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: v}}}
}

func TestTransformMetrics_GaugeAndSum(t *testing.T) {
	rms := []*metricspb.ResourceMetrics{{
		Resource: &resourcepb.Resource{Attributes: []*commonpb.KeyValue{strKV("service.name", "api")}},
		ScopeMetrics: []*metricspb.ScopeMetrics{{
			Scope: &commonpb.InstrumentationScope{Name: "lib"},
			Metrics: []*metricspb.Metric{
				{Name: "temp", Data: &metricspb.Metric_Gauge{Gauge: &metricspb.Gauge{DataPoints: []*metricspb.NumberDataPoint{
					{TimeUnixNano: 1700000000000000000, Attributes: []*commonpb.KeyValue{strKV("room", "a")},
						Value: &metricspb.NumberDataPoint_AsDouble{AsDouble: 21.5}},
					{TimeUnixNano: 1700000000000000000, Attributes: []*commonpb.KeyValue{strKV("room", "b")},
						Value: &metricspb.NumberDataPoint_AsDouble{AsDouble: 19.0}},
				}}}},
				{Name: "requests", Data: &metricspb.Metric_Sum{Sum: &metricspb.Sum{DataPoints: []*metricspb.NumberDataPoint{
					{TimeUnixNano: 1700000000000000000, Value: &metricspb.NumberDataPoint_AsInt{AsInt: 42}},
				}}}},
			},
		}},
	}}

	rows := TransformMetrics("t1", rms)
	if len(rows) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(rows))
	}
	// Row from the sum metric.
	var sumRow *MetricRow
	for i := range rows {
		if rows[i].MetricName == "requests" {
			sumRow = &rows[i]
		}
	}
	if sumRow == nil || sumRow.MetricType != "sum" || sumRow.Value != 42 {
		t.Fatalf("unexpected sum row: %+v", sumRow)
	}
	if sumRow.ResourceAttributes["service.name"] != "api" {
		t.Fatal("resource attributes should propagate to rows")
	}
	// The two gauge points are distinct series.
	if rows[0].SeriesFingerprint == rows[1].SeriesFingerprint {
		t.Fatal("distinct gauge series should have distinct fingerprints")
	}
}

func TestTransformLogs(t *testing.T) {
	rls := []*logspb.ResourceLogs{{
		Resource: &resourcepb.Resource{Attributes: []*commonpb.KeyValue{strKV("service.name", "api")}},
		ScopeLogs: []*logspb.ScopeLogs{{
			Scope: &commonpb.InstrumentationScope{Name: "lib"},
			LogRecords: []*logspb.LogRecord{{
				TimeUnixNano:   1700000000000000000,
				SeverityNumber: logspb.SeverityNumber_SEVERITY_NUMBER_ERROR,
				SeverityText:   "ERROR",
				Body:           &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "boom"}},
				TraceId:        []byte{0x01, 0x02},
				SpanId:         []byte{0x0a},
				Attributes:     []*commonpb.KeyValue{strKV("k", "v")},
			}},
		}},
	}}
	rows := TransformLogs("t1", rls)
	if len(rows) != 1 {
		t.Fatalf("expected 1 log row, got %d", len(rows))
	}
	r := rows[0]
	if r.Body != "boom" || r.SeverityText != "ERROR" || r.TraceID != "0102" || r.SpanID != "0a" {
		t.Fatalf("unexpected log row: %+v", r)
	}
	if r.Attributes["k"] != "v" || r.ResourceAttributes["service.name"] != "api" {
		t.Fatal("log attributes should be flattened")
	}
	if r.LogID == 0 {
		t.Fatal("log_id should be a nonzero fingerprint")
	}
}

func TestTransformTraces(t *testing.T) {
	rss := []*tracepb.ResourceSpans{{
		Resource: &resourcepb.Resource{Attributes: []*commonpb.KeyValue{strKV("service.name", "api")}},
		ScopeSpans: []*tracepb.ScopeSpans{{
			Scope: &commonpb.InstrumentationScope{Name: "lib"},
			Spans: []*tracepb.Span{{
				TraceId:           []byte{0xaa, 0xbb},
				SpanId:            []byte{0xcc},
				ParentSpanId:      []byte{0xdd},
				Name:              "GET /x",
				Kind:              tracepb.Span_SPAN_KIND_SERVER,
				StartTimeUnixNano: 1700000000000000000,
				EndTimeUnixNano:   1700000000000000500,
				Status:            &tracepb.Status{Code: tracepb.Status_STATUS_CODE_OK, Message: "ok"},
				Attributes:        []*commonpb.KeyValue{strKV("http.method", "GET")},
			}},
		}},
	}}
	rows := TransformTraces("t1", rss)
	if len(rows) != 1 {
		t.Fatalf("expected 1 span row, got %d", len(rows))
	}
	r := rows[0]
	if r.TraceID != "aabb" || r.SpanID != "cc" || r.ParentSpanID != "dd" {
		t.Fatalf("unexpected ids: %+v", r)
	}
	if r.Kind != "SERVER" || r.StatusCode != "OK" {
		t.Fatalf("enum trimming failed: kind=%q status=%q", r.Kind, r.StatusCode)
	}
	if r.DurationNs != 500 {
		t.Fatalf("expected duration 500ns, got %d", r.DurationNs)
	}
}

func TestDecodeMetrics_RoundTrip(t *testing.T) {
	// Ensure the collector-level wrapper decodes to resource metrics.
	rms := []*metricspb.ResourceMetrics{{
		ScopeMetrics: []*metricspb.ScopeMetrics{{
			Metrics: []*metricspb.Metric{
				{Name: "m", Data: &metricspb.Metric_Gauge{Gauge: &metricspb.Gauge{DataPoints: []*metricspb.NumberDataPoint{
					{Value: &metricspb.NumberDataPoint_AsInt{AsInt: 1}},
				}}}},
			},
		}},
	}}
	if got := TransformMetrics("t1", rms); len(got) != 1 || got[0].Value != 1 {
		t.Fatalf("transform of decoded metrics failed: %+v", got)
	}
}
