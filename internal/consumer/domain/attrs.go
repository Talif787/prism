package domain

import (
	"hash/fnv"
	"sort"
	"strconv"
	"time"

	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
)

// attrsToMap flattens OTLP attributes into a string map for a ClickHouse Map column.
func attrsToMap(attrs []*commonpb.KeyValue) map[string]string {
	if len(attrs) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(attrs))
	for _, kv := range attrs {
		out[kv.GetKey()] = anyValueString(kv.GetValue())
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

// canonicalAttrs renders attributes as a stable, sorted string for fingerprinting.
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

// tsFromNano converts an OTLP unix-nano timestamp to time.Time (UTC).
func tsFromNano(ns uint64) time.Time {
	return time.Unix(0, int64(ns)).UTC()
}

// fnv64 hashes the given parts into a 64-bit value, separated so distinct field
// boundaries cannot collide.
func fnv64(parts ...string) uint64 {
	h := fnv.New64a()
	for _, p := range parts {
		_, _ = h.Write([]byte(p))
		_, _ = h.Write([]byte{0})
	}
	return h.Sum64()
}
