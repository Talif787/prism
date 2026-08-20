// Package cardinality estimates the number of distinct metric series a tenant has
// produced within a rolling window using a Redis HyperLogLog. HLL gives a compact
// (about 12KB per tenant), approximate distinct count, which is the right tool:
// exact tracking of millions of series would be prohibitively expensive, and an
// estimate is sufficient to enforce a budget.
package cardinality

import (
	"context"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/Talif787/prism/internal/ingest/app"
)

type Config struct {
	Budget  int64
	Window  time.Duration
	Enforce bool
}

// Guard implements app.CardinalityGuard.
type Guard struct {
	redis   *goredis.Client
	budget  int64
	window  time.Duration
	enforce bool
}

func New(rdb *goredis.Client, cfg Config) *Guard {
	return &Guard{redis: rdb, budget: cfg.Budget, window: cfg.Window, enforce: cfg.Enforce}
}

// Admit records the series fingerprints and returns the current estimate. When
// enforcement is on and the estimate exceeds the budget, new series are rejected.
// Fingerprints already counted are effectively free (HLL is idempotent), so
// established series keep flowing; only growth past the budget is blocked.
func (g *Guard) Admit(ctx context.Context, tenantID string, fingerprints [][]byte) (app.CardinalityDecision, error) {
	if len(fingerprints) == 0 {
		return app.CardinalityDecision{Admitted: true, Budget: g.budget, Enforced: g.enforce}, nil
	}
	key := g.key(tenantID)

	members := make([]any, len(fingerprints))
	for i, fp := range fingerprints {
		members[i] = string(fp)
	}
	if err := g.redis.PFAdd(ctx, key, members...).Err(); err != nil {
		return app.CardinalityDecision{}, err
	}
	// Refresh the window TTL on activity so an inactive tenant's estimate resets.
	g.redis.Expire(ctx, key, g.window)

	estimate, err := g.redis.PFCount(ctx, key).Result()
	if err != nil {
		return app.CardinalityDecision{}, err
	}

	admitted := true
	if g.enforce && estimate > g.budget {
		admitted = false
	}
	return app.CardinalityDecision{
		Admitted: admitted,
		Estimate: estimate,
		Budget:   g.budget,
		Enforced: g.enforce,
	}, nil
}

func (g *Guard) key(tenantID string) string {
	return "card:series:" + tenantID
}
