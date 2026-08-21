// Package otlpgrpc implements the OTLP/gRPC receiver for metrics, logs, and
// traces. It is the gRPC counterpart to the OTLP/HTTP receiver: most OpenTelemetry
// SDKs and Collectors default to gRPC on port 4317. Both transports share the same
// ingest pipeline (authenticate, rate limit, cardinality guard, produce to Kafka)
// and the same domain point-counting and fingerprinting, so behavior is identical
// regardless of transport. The value placed on Kafka is always protobuf.
package otlpgrpc

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	_ "google.golang.org/grpc/encoding/gzip" // register gzip so compressed requests decode

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	collogspb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	colmetricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	coltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"

	"github.com/Talif787/prism/internal/ingest/app"
	"github.com/Talif787/prism/internal/ingest/domain"
)

// Register wires the three OTLP services onto the gRPC server, all sharing the
// ingest pipeline.
func Register(s *grpc.Server, pipeline *app.Pipeline, logger *slog.Logger) {
	base := &server{pipeline: pipeline, logger: logger}
	colmetricspb.RegisterMetricsServiceServer(s, &metricsServer{server: base})
	coltracepb.RegisterTraceServiceServer(s, &traceServer{server: base})
	collogspb.RegisterLogsServiceServer(s, &logsServer{server: base})
}

type server struct {
	pipeline *app.Pipeline
	logger   *slog.Logger
}

type metricsServer struct {
	*server
	colmetricspb.UnimplementedMetricsServiceServer
}

type traceServer struct {
	*server
	coltracepb.UnimplementedTraceServiceServer
}

type logsServer struct {
	*server
	collogspb.UnimplementedLogsServiceServer
}

func (s *metricsServer) Export(ctx context.Context, req *colmetricspb.ExportMetricsServiceRequest) (*colmetricspb.ExportMetricsServiceResponse, error) {
	rms := req.GetResourceMetrics()
	if err := s.ingest(ctx, domain.SignalMetrics, req, domain.CountMetricPoints(rms), domain.MetricFingerprints(rms)); err != nil {
		return nil, err
	}
	return &colmetricspb.ExportMetricsServiceResponse{}, nil
}

func (s *traceServer) Export(ctx context.Context, req *coltracepb.ExportTraceServiceRequest) (*coltracepb.ExportTraceServiceResponse, error) {
	if err := s.ingest(ctx, domain.SignalTraces, req, domain.CountSpans(req.GetResourceSpans()), nil); err != nil {
		return nil, err
	}
	return &coltracepb.ExportTraceServiceResponse{}, nil
}

func (s *logsServer) Export(ctx context.Context, req *collogspb.ExportLogsServiceRequest) (*collogspb.ExportLogsServiceResponse, error) {
	if err := s.ingest(ctx, domain.SignalLogs, req, domain.CountLogs(req.GetResourceLogs()), nil); err != nil {
		return nil, err
	}
	return &collogspb.ExportLogsServiceResponse{}, nil
}

// ingest marshals the request to protobuf, then runs the shared pipeline. The
// key is read from gRPC metadata; the pipeline enforces scope, limits, and guard.
func (s *server) ingest(ctx context.Context, signal domain.Signal, req proto.Message, pointCount int, fingerprints [][]byte) error {
	apiKey, ok := apiKeyFromContext(ctx)
	if !ok {
		return status.Error(codes.Unauthenticated, "missing API key")
	}
	payload, err := proto.Marshal(req)
	if err != nil {
		return status.Error(codes.InvalidArgument, "could not marshal OTLP payload")
	}
	if _, err := s.pipeline.Ingest(ctx, app.Input{
		APIKey:       apiKey,
		Signal:       signal,
		Payload:      payload,
		PointCount:   pointCount,
		Fingerprints: fingerprints,
		ReceivedAt:   time.Now().UTC(),
	}); err != nil {
		return mapError(err)
	}
	return nil
}

// apiKeyFromContext reads the key from gRPC metadata, accepting either
// "authorization: Bearer <key>" or "x-prism-key: <key>". Both are settable through
// OTEL_EXPORTER_OTLP_HEADERS. Metadata keys are matched case-insensitively.
func apiKeyFromContext(ctx context.Context) (string, bool) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return "", false
	}
	if vals := md.Get("authorization"); len(vals) > 0 {
		const prefix = "Bearer "
		if strings.HasPrefix(vals[0], prefix) {
			if k := strings.TrimSpace(vals[0][len(prefix):]); k != "" {
				return k, true
			}
		}
	}
	if vals := md.Get("x-prism-key"); len(vals) > 0 {
		if k := strings.TrimSpace(vals[0]); k != "" {
			return k, true
		}
	}
	return "", false
}

// mapError translates pipeline sentinels to gRPC status codes, mirroring the HTTP
// receiver's status mapping.
func mapError(err error) error {
	switch {
	case errors.Is(err, app.ErrUnauthorized):
		return status.Error(codes.Unauthenticated, "invalid API key")
	case errors.Is(err, app.ErrForbidden):
		return status.Error(codes.PermissionDenied, "key lacks ingest scope")
	case errors.Is(err, app.ErrRateLimited):
		return status.Error(codes.ResourceExhausted, "ingestion rate limit exceeded")
	case errors.Is(err, app.ErrCardinality):
		return status.Error(codes.ResourceExhausted, "series cardinality budget exceeded")
	default:
		return status.Error(codes.Unavailable, "unable to accept telemetry")
	}
}
