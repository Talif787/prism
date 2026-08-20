// Package ratelimit implements a per-tenant token bucket backed by Redis. The
// refill and take operation runs as a single Lua script so it is atomic across
// gateway replicas: every replica shares the same bucket, so the limit is a true
// per-tenant limit rather than per-instance.
package ratelimit

import (
	"context"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/Talif787/prism/internal/ingest/app"
)

// tokenBucket refills at `rate` tokens per second up to `burst`, then tries to
// take `cost` tokens. It returns {allowed, remaining, retry_after_ms}. State is a
// hash holding the token count and the last refill time in milliseconds.
var tokenBucket = goredis.NewScript(`
local key    = KEYS[1]
local rate   = tonumber(ARGV[1])
local burst  = tonumber(ARGV[2])
local now_ms = tonumber(ARGV[3])
local cost   = tonumber(ARGV[4])
local ttl_ms = tonumber(ARGV[5])

local data = redis.call('HMGET', key, 'tokens', 'ts')
local tokens = tonumber(data[1])
local ts = tonumber(data[2])
if tokens == nil then
  tokens = burst
  ts = now_ms
end

local elapsed = math.max(0, now_ms - ts)
local refill = (elapsed / 1000.0) * rate
tokens = math.min(burst, tokens + refill)

local allowed = 0
local retry_after_ms = 0
if tokens >= cost then
  tokens = tokens - cost
  allowed = 1
else
  local deficit = cost - tokens
  retry_after_ms = math.ceil((deficit / rate) * 1000)
end

redis.call('HMSET', key, 'tokens', tokens, 'ts', now_ms)
redis.call('PEXPIRE', key, ttl_ms)
return {allowed, math.floor(tokens), retry_after_ms}
`)

type Config struct {
	RatePerSecond int
	Burst         int
}

// Limiter implements app.RateLimiter.
type Limiter struct {
	redis *goredis.Client
	rate  int
	burst int
}

func New(rdb *goredis.Client, cfg Config) *Limiter {
	return &Limiter{redis: rdb, rate: cfg.RatePerSecond, burst: cfg.Burst}
}

func (l *Limiter) Allow(ctx context.Context, tenantID string, cost int) (app.Decision, error) {
	if cost <= 0 {
		return app.Decision{Allowed: true}, nil
	}
	nowMs := time.Now().UnixMilli()
	// TTL: enough time to fully refill an empty bucket, so idle buckets expire.
	ttlMs := int64((float64(l.burst)/float64(l.rate))*1000) + 1000

	raw, err := tokenBucket.Run(ctx, l.redis,
		[]string{l.key(tenantID)},
		l.rate, l.burst, nowMs, cost, ttlMs,
	).Slice()
	if err != nil {
		return app.Decision{}, err
	}
	if len(raw) != 3 {
		return app.Decision{Allowed: true}, nil
	}
	// Redis returns Lua numbers as int64.
	allowed, _ := raw[0].(int64)
	remaining, _ := raw[1].(int64)
	retryMs, _ := raw[2].(int64)
	return app.Decision{
		Allowed:    allowed == 1,
		Remaining:  remaining,
		RetryAfter: float64(retryMs) / 1000.0,
	}, nil
}

func (l *Limiter) key(tenantID string) string {
	return "rl:ingest:" + tenantID
}
