//go:build integration

// Redis-backed rate limiter integration tests. Run with:
//
//	go test -tags=integration ./test/integration/...
//
// A Docker daemon must be available.
package integration

import (
	"context"
	"testing"

	goredis "github.com/redis/go-redis/v9"
	tcredis "github.com/testcontainers/testcontainers-go/modules/redis"

	"github.com/Talif787/prism/internal/ingest/app"
	"github.com/Talif787/prism/internal/ingest/infra/ratelimit"
)

func redisClient(t *testing.T) *goredis.Client {
	t.Helper()
	ctx := context.Background()
	container, err := tcredis.Run(ctx, "redis:7-alpine")
	if err != nil {
		t.Fatalf("start redis: %v", err)
	}
	t.Cleanup(func() { _ = container.Terminate(ctx) })

	connStr, err := container.ConnectionString(ctx)
	if err != nil {
		t.Fatalf("connection string: %v", err)
	}
	opts, err := goredis.ParseURL(connStr)
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}
	rdb := goredis.NewClient(opts)
	t.Cleanup(func() { _ = rdb.Close() })
	if err := rdb.Ping(ctx).Err(); err != nil {
		t.Fatalf("ping: %v", err)
	}
	return rdb
}

func TestRateLimiter_AllowsWithinBurstThenBlocks(t *testing.T) {
	rdb := redisClient(t)
	// Small bucket so we can exhaust it deterministically within the test.
	lim := ratelimit.New(rdb, ratelimit.Config{RatePerSecond: 10, Burst: 100})
	ctx := context.Background()

	dec, err := lim.Allow(ctx, "tenant-a", 100)
	if err != nil {
		t.Fatalf("allow: %v", err)
	}
	if !dec.Allowed {
		t.Fatal("first request within burst should be allowed")
	}

	// Bucket now near-empty; a large request must be refused with a retry hint.
	dec, err = lim.Allow(ctx, "tenant-a", 100)
	if err != nil {
		t.Fatalf("allow: %v", err)
	}
	if dec.Allowed {
		t.Fatal("second large request should be rate limited")
	}
	if dec.RetryAfter <= 0 {
		t.Fatalf("expected a positive retry-after, got %v", dec.RetryAfter)
	}
}

func TestRateLimiter_IsolatesTenants(t *testing.T) {
	rdb := redisClient(t)
	lim := ratelimit.New(rdb, ratelimit.Config{RatePerSecond: 10, Burst: 100})
	ctx := context.Background()

	if dec, _ := lim.Allow(ctx, "tenant-a", 100); !dec.Allowed {
		t.Fatal("tenant-a first request should be allowed")
	}
	// A different tenant has its own full bucket.
	dec, err := lim.Allow(ctx, "tenant-b", 100)
	if err != nil {
		t.Fatalf("allow: %v", err)
	}
	if !dec.Allowed {
		t.Fatal("tenant-b must not be affected by tenant-a's usage")
	}
}

var _ app.RateLimiter = (*ratelimit.Limiter)(nil)
