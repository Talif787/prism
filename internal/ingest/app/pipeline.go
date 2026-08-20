package app

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"time"

	"github.com/Talif787/prism/internal/ingest/domain"
)

// Errors surfaced by the pipeline, mapped to HTTP status codes by the transport.
var (
	ErrUnauthorized = errors.New("unauthorized")
	ErrRateLimited  = errors.New("rate limited")
	ErrCardinality  = errors.New("cardinality budget exceeded")
	ErrForbidden    = errors.New("forbidden")
)

const ingestScope = "ingest"

// Topics maps a signal to its Kafka topic.
type Topics struct {
	Metrics string
	Logs    string
	Traces  string
}

func (t Topics) For(s domain.Signal) string {
	switch s {
	case domain.SignalMetrics:
		return t.Metrics
	case domain.SignalLogs:
		return t.Logs
	case domain.SignalTraces:
		return t.Traces
	default:
		return ""
	}
}

// Pipeline orchestrates a single ingest request: authenticate, rate limit, guard
// cardinality, then produce to the durable buffer.
type Pipeline struct {
	auth     Authenticator
	limiter  RateLimiter
	guard    CardinalityGuard
	producer Producer
	topics   Topics
	logger   *slog.Logger
}

func NewPipeline(a Authenticator, l RateLimiter, g CardinalityGuard, p Producer, topics Topics, logger *slog.Logger) *Pipeline {
	return &Pipeline{auth: a, limiter: l, guard: g, producer: p, topics: topics, logger: logger}
}

// Input is a decoded, validated ingest request handed to the pipeline. The
// transport layer parses OTLP and computes PointCount and Fingerprints so the
// pipeline stays free of protobuf details.
type Input struct {
	APIKey       string
	Signal       domain.Signal
	Payload      []byte
	PointCount   int
	Fingerprints [][]byte
	ReceivedAt   time.Time
}

// Result summarizes an accepted request.
type Result struct {
	TenantID    string
	Accepted    int
	Remaining   int64
	Cardinality CardinalityDecision
}

// Ingest runs the pipeline. It returns a sentinel error (wrapped) that the
// transport maps to the correct status code.
func (p *Pipeline) Ingest(ctx context.Context, in Input) (*Result, error) {
	principal, err := p.auth.Authenticate(ctx, in.APIKey)
	if err != nil {
		return nil, ErrUnauthorized
	}
	if !hasScope(principal.Scopes, ingestScope) {
		return nil, ErrForbidden
	}

	if in.PointCount > 0 {
		decision, err := p.limiter.Allow(ctx, principal.TenantID, in.PointCount)
		if err != nil {
			return nil, err
		}
		if !decision.Allowed {
			p.logger.WarnContext(ctx, "rate limited",
				slog.String("tenant_id", principal.TenantID),
				slog.Int("cost", in.PointCount),
				slog.Float64("retry_after_s", decision.RetryAfter),
			)
			return nil, ErrRateLimited
		}
	}

	var card CardinalityDecision
	if len(in.Fingerprints) > 0 {
		card, err = p.guard.Admit(ctx, principal.TenantID, in.Fingerprints)
		if err != nil {
			return nil, err
		}
		if card.Enforced && !card.Admitted {
			p.logger.WarnContext(ctx, "cardinality budget exceeded",
				slog.String("tenant_id", principal.TenantID),
				slog.Int64("estimate", card.Estimate),
				slog.Int64("budget", card.Budget),
			)
			return nil, ErrCardinality
		}
	}

	msg := Message{
		Topic: p.topics.For(in.Signal),
		Key:   []byte(principal.TenantID),
		Value: in.Payload,
		Headers: []Header{
			{Key: domain.HeaderTenantID, Value: []byte(principal.TenantID)},
			{Key: domain.HeaderSignal, Value: []byte(in.Signal)},
			{Key: domain.HeaderReceivedAt, Value: []byte(strconv.FormatInt(in.ReceivedAt.UnixNano(), 10))},
			{Key: domain.HeaderContentType, Value: []byte(domain.ContentTypeProtobuf)},
			{Key: domain.HeaderSchema, Value: []byte(domain.SchemaVersion)},
			{Key: domain.HeaderPointCount, Value: []byte(strconv.Itoa(in.PointCount))},
		},
	}
	if err := p.producer.Produce(ctx, msg); err != nil {
		return nil, err
	}

	return &Result{
		TenantID:    principal.TenantID,
		Accepted:    in.PointCount,
		Cardinality: card,
	}, nil
}

func hasScope(scopes []string, want string) bool {
	for _, s := range scopes {
		if s == want {
			return true
		}
	}
	return false
}
