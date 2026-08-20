// Package redis provides a configured go-redis client and a health checker. Redis
// backs the gateway's auth cache, rate limiter, and cardinality guard.
package redis

import (
	"context"
	"fmt"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

type Config struct {
	Addr         string
	Password     string
	DB           int
	DialTimeout  time.Duration
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	PoolSize     int
}

// Client wraps the go-redis client so callers depend on this package, not the
// driver directly, keeping the driver swappable.
type Client struct {
	*goredis.Client
}

// New builds and verifies a Redis client, failing fast if unreachable.
func New(ctx context.Context, cfg Config) (*Client, error) {
	rdb := goredis.NewClient(&goredis.Options{
		Addr:         cfg.Addr,
		Password:     cfg.Password,
		DB:           cfg.DB,
		DialTimeout:  orDefault(cfg.DialTimeout, 5*time.Second),
		ReadTimeout:  orDefault(cfg.ReadTimeout, 3*time.Second),
		WriteTimeout: orDefault(cfg.WriteTimeout, 3*time.Second),
		PoolSize:     cfg.PoolSize,
	})

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := rdb.Ping(pingCtx).Err(); err != nil {
		_ = rdb.Close()
		return nil, fmt.Errorf("ping redis: %w", err)
	}
	return &Client{Client: rdb}, nil
}

func orDefault(d, def time.Duration) time.Duration {
	if d <= 0 {
		return def
	}
	return d
}
