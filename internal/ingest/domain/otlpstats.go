package domain

import (
	"hash/fnv"
	"sort"
	"strconv"

	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
)

// CountMetricPoints returns the total number of metric data points across all
// resources. This is the unit metered and rate limited for metrics.
func CountMetricPoints(rms []*metricspb.ResourceMetrics) int {
	total := 0
	for _, rm := range rms {
		for _, sm := range rm.GetScopeMetrics() {
			for _, m := range sm.GetMetrics() {
				total += metricPointCount(m)
			}
		}
	}
	return total
}

func metricPointCount(m *metricspb.Metric) int {
	switch {
	case m.GetGauge() != nil:
		return len(m.GetGauge().GetDataPoints())
	case m.GetSum() != nil:
		return len(m.GetSum().GetDataPoints())
	case m.GetHistogram() != nil:
		return len(m.GetHistogram().GetDataPoints())
	case m.GetExponentialHistogram() != nil:
		return len(m.GetExponentialHistogram().GetDataPoints())
	case m.GetSummary() != nil:
		return len(m.GetSummary().GetDataPoints())
	default:
		return 0
	}
}

// CountSpans returns the total number of spans across all resources.
func CountSpans(rss []*tracepb.ResourceSpans) int {
	total := 0
	for _, rs := range rss {
		for _, ss := range rs.GetScopeSpans() {
			total += len(ss.GetSpans())
		}
	}
	return total
}

// CountLogs returns the total number of log records across all resources.
func CountLogs(rls []*logspb.ResourceLogs) int {
	total := 0
	for _, rl := range rls {
		for _, sl := range rl.GetScopeLogs() {
			total += len(sl.GetLogRecords())
		}
	}
	return total
}

// MetricFingerprints returns one 64-bit fingerprint per metric series, where a
// series is identified by (resource attributes, metric name, data point
// attributes). Fingerprints feed the cardinality guard's HyperLogLog estimator.
func MetricFingerprints(rms []*metricspb.ResourceMetrics) [][]byte {
	var out [][]byte
	for _, rm := range rms {
		resourceKey := canonicalAttrs(rm.GetResource().GetAttributes())
		for _, sm := range rm.GetScopeMetrics() {
			for _, m := range sm.GetMetrics() {
				name := m.GetName()
				for _, dpAttrs := range dataPointAttrs(m) {
					out = append(out, fingerprint(resourceKey, name, dpAttrs))
				}
			}
		}
	}
	return out
}

func dataPointAttrs(m *metricspb.Metric) [][]*commonpb.KeyValue {
	var out [][]*commonpb.KeyValue
	switch {
	case m.GetGauge() != nil:
		for _, dp := range m.GetGauge().GetDataPoints() {
			out = append(out, dp.GetAttributes())
		}
	case m.GetSum() != nil:
		for _, dp := range m.GetSum().GetDataPoints() {
			out = append(out, dp.GetAttributes())
		}
	case m.GetHistogram() != nil:
		for _, dp := range m.GetHistogram().GetDataPoints() {
			out = append(out, dp.GetAttributes())
		}
	case m.GetExponentialHistogram() != nil:
		for _, dp := range m.GetExponentialHistogram().GetDataPoints() {
			out = append(out, dp.GetAttributes())
		}
	case m.GetSummary() != nil:
		for _, dp := range m.GetSummary().GetDataPoints() {
			out = append(out, dp.GetAttributes())
		}
	}
	return out
}

func fingerprint(resourceKey, name string, dpAttrs []*commonpb.KeyValue) []byte {
	h := fnv.New64a()
	_, _ = h.Write([]byte(resourceKey))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(name))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(canonicalAttrs(dpAttrs)))
	sum := h.Sum64()
	b := make([]byte, 8)
	for i := 0; i < 8; i++ {
		b[i] = byte(sum >> (8 * i))
	}
	return b
}

// canonicalAttrs produces a stable string for a set of attributes by sorting on
// key, so two identical attribute sets always yield the same fingerprint.
func canonicalAttrs(attrs []*commonpb.KeyValue) string {
	if len(attrs) == 0 {
		return ""
	}
	pairs := make([]string, 0, len(attrs))
	for _, kv := range attrs {
		pairs = append(pairs, kv.GetKey()+"="+anyValueString(kv.GetValue()))
	}
	sort.Strings(pairs)
	out := ""
	for i, p := range pairs {
		if i > 0 {
			out += ";"
		}
		out += p
	}
	return out
}

func anyValueString(v *commonpb.AnyValue) string {
	if v == nil {
		return ""
	}
	switch val := v.GetValue().(type) {
	case *commonpb.AnyValue_StringValue:
		return val.StringValue
	case *commonpb.AnyValue_BoolValue:
		return strconv.FormatBool(val.BoolValue)
	case *commonpb.AnyValue_IntValue:
		return strconv.FormatInt(val.IntValue, 10)
	case *commonpb.AnyValue_DoubleValue:
		return strconv.FormatFloat(val.DoubleValue, 'g', -1, 64)
	default:
		return ""
	}
}
