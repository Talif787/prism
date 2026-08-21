package otlpgrpc

import (
	"context"
	"log/slog"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	colmetricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"

	"github.com/Talif787/prism/internal/ingest/app"
)

type fakeAuth struct {
	scopes []string
	err    error
}

func (f fakeAuth) Authenticate(_ context.Context, _ string) (*app.Principal, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &app.Principal{TenantID: "t1", Scopes: f.scopes}, nil
}

type fakeLimiter struct{}

func (fakeLimiter) Allow(_ context.Context, _ string, _ int) (app.Decision, error) {
	return app.Decision{Allowed: true}, nil
}

type fakeGuard struct{}

func (fakeGuard) Admit(_ context.Context, _ string, _ [][]byte) (app.CardinalityDecision, error) {
	return app.CardinalityDecision{Admitted: true}, nil
}

type fakeProducer struct{ msgs []app.Message }

func (f *fakeProducer) Produce(_ context.Context, m app.Message) error {
	f.msgs = append(f.msgs, m)
	return nil
}

func newMetricsServer(auth app.Authenticator, prod app.Producer) *metricsServer {
	pipe := app.NewPipeline(auth, fakeLimiter{}, fakeGuard{}, prod,
		app.Topics{Metrics: "telemetry.metrics", Logs: "telemetry.logs", Traces: "telemetry.traces"},
		slog.Default())
	return &metricsServer{server: &server{pipeline: pipe, logger: slog.Default()}}
}

func ctxWithKey(key string) context.Context {
	return metadata.NewIncomingContext(context.Background(), metadata.Pairs("x-prism-key", key))
}

func TestMetricsExport_Success(t *testing.T) {
	prod := &fakeProducer{}
	srv := newMetricsServer(fakeAuth{scopes: []string{"ingest"}}, prod)

	resp, err := srv.Export(ctxWithKey("abc"), &colmetricspb.ExportMetricsServiceRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected a non-nil response")
	}
	if len(prod.msgs) != 1 {
		t.Fatalf("expected 1 produced message, got %d", len(prod.msgs))
	}
	if prod.msgs[0].Topic != "telemetry.metrics" {
		t.Fatalf("expected metrics topic, got %q", prod.msgs[0].Topic)
	}
	if string(prod.msgs[0].Key) != "t1" {
		t.Fatalf("expected tenant key t1, got %q", string(prod.msgs[0].Key))
	}
}

func TestMetricsExport_MissingKey(t *testing.T) {
	srv := newMetricsServer(fakeAuth{scopes: []string{"ingest"}}, &fakeProducer{})
	_, err := srv.Export(context.Background(), &colmetricspb.ExportMetricsServiceRequest{})
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("expected Unauthenticated, got %v", status.Code(err))
	}
}

func TestMetricsExport_WrongScope(t *testing.T) {
	srv := newMetricsServer(fakeAuth{scopes: []string{"query"}}, &fakeProducer{})
	_, err := srv.Export(ctxWithKey("abc"), &colmetricspb.ExportMetricsServiceRequest{})
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("expected PermissionDenied, got %v", status.Code(err))
	}
}
