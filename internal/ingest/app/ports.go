// Package app is the ingest application layer. It defines the ingest pipeline use
// case and the ports it depends on (authentication, rate limiting, cardinality,
// production). It is transport-agnostic and unit-testable with fakes.
package app

import "context"

// Principal is the authenticated tenant context for an ingest request.
type Principal struct {
	TenantID string
	Scopes   []string
}

// Authenticator verifies an API key and returns its tenant and scopes. The
// gateway only accepts keys with the ingest scope; enforcement happens at the
// authenticator boundary (it verifies against the ingest scope).
type Authenticator interface {
	Authenticate(ctx context.Context, apiKey string) (*Principal, error)
}

// Decision reports a rate-limit outcome.
type Decision struct {
	Allowed    bool
	Remaining  int64
	RetryAfter float64 // seconds until capacity is available, when not allowed
}

// RateLimiter enforces a per-tenant token bucket. cost is the number of tokens
// (data points) the request consumes.
type RateLimiter interface {
	Allow(ctx context.Context, tenantID string, cost int) (Decision, error)
}

// CardinalityDecision reports whether new series are admitted and the current
// estimate against the budget.
type CardinalityDecision struct {
	Admitted bool
	Estimate int64
	Budget   int64
	Enforced bool
}

// CardinalityGuard tracks distinct metric series per tenant and admits or rejects
// based on the tenant's budget.
type CardinalityGuard interface {
	Admit(ctx context.Context, tenantID string, fingerprints [][]byte) (CardinalityDecision, error)
}

// Header and Message mirror the platform kafka types but keep the app layer free
// of the broker dependency.
type Header struct {
	Key   string
	Value []byte
}

type Message struct {
	Topic   string
	Key     []byte
	Value   []byte
	Headers []Header
}

// Producer publishes a message to the durable buffer.
type Producer interface {
	Produce(ctx context.Context, msg Message) error
}
