package app

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/Talif787/prism/internal/ingest/domain"
)

type fakeAuth struct {
	principal *Principal
	err       error
}

func (f fakeAuth) Authenticate(context.Context, string) (*Principal, error) {
	return f.principal, f.err
}

type fakeLimiter struct{ allowed bool }

func (f fakeLimiter) Allow(context.Context, string, int) (Decision, error) {
	return Decision{Allowed: f.allowed, RetryAfter: 1}, nil
}

type fakeGuard struct{ admitted, enforced bool }

func (f fakeGuard) Admit(context.Context, string, [][]byte) (CardinalityDecision, error) {
	return CardinalityDecision{Admitted: f.admitted, Enforced: f.enforced}, nil
}

type fakeProducer struct{ msgs []Message }

func (f *fakeProducer) Produce(_ context.Context, m Message) error {
	f.msgs = append(f.msgs, m)
	return nil
}

func newPipeline(auth Authenticator, lim RateLimiter, guard CardinalityGuard, prod Producer) *Pipeline {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewPipeline(auth, lim, guard, prod, Topics{Metrics: "telemetry.metrics"}, logger)
}

func baseInput() Input {
	return Input{
		APIKey: "pk_x", Signal: domain.SignalMetrics, Payload: []byte("otlp-bytes"),
		PointCount: 5, Fingerprints: [][]byte{{1}, {2}}, ReceivedAt: time.Now(),
	}
}

func TestPipeline_HappyPath(t *testing.T) {
	prod := &fakeProducer{}
	p := newPipeline(
		fakeAuth{principal: &Principal{TenantID: "t1", Scopes: []string{"ingest"}}},
		fakeLimiter{allowed: true},
		fakeGuard{admitted: true, enforced: true},
		prod,
	)
	res, err := p.Ingest(context.Background(), baseInput())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.TenantID != "t1" || res.Accepted != 5 {
		t.Fatalf("unexpected result: %+v", res)
	}
	if len(prod.msgs) != 1 || prod.msgs[0].Topic != "telemetry.metrics" {
		t.Fatalf("expected one message to metrics topic, got %+v", prod.msgs)
	}
	if string(prod.msgs[0].Key) != "t1" {
		t.Fatalf("message must be keyed by tenant, got %q", prod.msgs[0].Key)
	}
}

func TestPipeline_Unauthorized(t *testing.T) {
	p := newPipeline(fakeAuth{err: errors.New("bad")}, fakeLimiter{allowed: true}, fakeGuard{admitted: true}, &fakeProducer{})
	if _, err := p.Ingest(context.Background(), baseInput()); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("want ErrUnauthorized, got %v", err)
	}
}

func TestPipeline_MissingScope(t *testing.T) {
	p := newPipeline(
		fakeAuth{principal: &Principal{TenantID: "t1", Scopes: []string{"query"}}},
		fakeLimiter{allowed: true}, fakeGuard{admitted: true}, &fakeProducer{},
	)
	if _, err := p.Ingest(context.Background(), baseInput()); !errors.Is(err, ErrForbidden) {
		t.Fatalf("want ErrForbidden, got %v", err)
	}
}

func TestPipeline_RateLimited(t *testing.T) {
	prod := &fakeProducer{}
	p := newPipeline(
		fakeAuth{principal: &Principal{TenantID: "t1", Scopes: []string{"ingest"}}},
		fakeLimiter{allowed: false}, fakeGuard{admitted: true}, prod,
	)
	if _, err := p.Ingest(context.Background(), baseInput()); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("want ErrRateLimited, got %v", err)
	}
	if len(prod.msgs) != 0 {
		t.Fatal("rate-limited request must not produce")
	}
}

func TestPipeline_CardinalityEnforced(t *testing.T) {
	p := newPipeline(
		fakeAuth{principal: &Principal{TenantID: "t1", Scopes: []string{"ingest"}}},
		fakeLimiter{allowed: true}, fakeGuard{admitted: false, enforced: true}, &fakeProducer{},
	)
	if _, err := p.Ingest(context.Background(), baseInput()); !errors.Is(err, ErrCardinality) {
		t.Fatalf("want ErrCardinality, got %v", err)
	}
}

func TestPipeline_CardinalityObserveOnly(t *testing.T) {
	// When enforcement is off, over-budget series are still admitted.
	prod := &fakeProducer{}
	p := newPipeline(
		fakeAuth{principal: &Principal{TenantID: "t1", Scopes: []string{"ingest"}}},
		fakeLimiter{allowed: true}, fakeGuard{admitted: false, enforced: false}, prod,
	)
	if _, err := p.Ingest(context.Background(), baseInput()); err != nil {
		t.Fatalf("observe-only cardinality must not block: %v", err)
	}
	if len(prod.msgs) != 1 {
		t.Fatal("expected the request to be produced")
	}
}
