package domain

import (
	"bytes"
	"testing"

	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
)

func strKV(k, v string) *commonpb.KeyValue {
	return &commonpb.KeyValue{Key: k, Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: v}}}
}

func numDP(attrs ...*commonpb.KeyValue) *metricspb.NumberDataPoint {
	return &metricspb.NumberDataPoint{Attributes: attrs}
}

func gauge(name string, dps ...*metricspb.NumberDataPoint) *metricspb.Metric {
	return &metricspb.Metric{Name: name, Data: &metricspb.Metric_Gauge{Gauge: &metricspb.Gauge{DataPoints: dps}}}
}

func request(resourceAttrs []*commonpb.KeyValue, metrics ...*metricspb.Metric) []*metricspb.ResourceMetrics {
	return []*metricspb.ResourceMetrics{{
		Resource:     &resourcepb.Resource{Attributes: resourceAttrs},
		ScopeMetrics: []*metricspb.ScopeMetrics{{Metrics: metrics}},
	}}
}

func TestCountMetricPoints(t *testing.T) {
	rms := request(
		[]*commonpb.KeyValue{strKV("service.name", "api")},
		gauge("http.requests", numDP(strKV("path", "/a")), numDP(strKV("path", "/b"))),
		gauge("cpu.usage", numDP()),
	)
	if got := CountMetricPoints(rms); got != 3 {
		t.Fatalf("CountMetricPoints = %d, want 3", got)
	}
}

func TestMetricFingerprints_CountAndStability(t *testing.T) {
	rms := request(
		[]*commonpb.KeyValue{strKV("service.name", "api")},
		gauge("http.requests", numDP(strKV("path", "/a")), numDP(strKV("path", "/b"))),
	)
	fps := MetricFingerprints(rms)
	if len(fps) != 2 {
		t.Fatalf("fingerprint count = %d, want 2", len(fps))
	}
	if bytes.Equal(fps[0], fps[1]) {
		t.Fatal("distinct series should have distinct fingerprints")
	}

	// Identical series must fingerprint identically (attribute order independent).
	a := MetricFingerprints(request(
		[]*commonpb.KeyValue{strKV("service.name", "api")},
		gauge("m", numDP(strKV("x", "1"), strKV("y", "2"))),
	))
	b := MetricFingerprints(request(
		[]*commonpb.KeyValue{strKV("service.name", "api")},
		gauge("m", numDP(strKV("y", "2"), strKV("x", "1"))),
	))
	if len(a) != 1 || len(b) != 1 || !bytes.Equal(a[0], b[0]) {
		t.Fatal("identical series with reordered attributes must share a fingerprint")
	}
}
