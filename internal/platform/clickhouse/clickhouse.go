// Package clickhouse provides a configured ClickHouse connection (native
// protocol) and a migration runner. ClickHouse is the columnar store for
// telemetry: metrics, logs, and spans written by the Phase 3 consumer and read
// by the Phase 4 query service.
package clickhouse

import (
	"context"
	"fmt"
	"time"

	ch "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

type Config struct {
	Addr         string
	Database     string
	Username     string
	Password     string
	DialTimeout  time.Duration
	MaxOpenConns int
}

// Conn wraps the driver connection so callers depend on this package rather than
// the driver directly.
type Conn struct {
	driver.Conn
}

// New opens and verifies a ClickHouse connection, failing fast if unreachable.
func New(ctx context.Context, cfg Config) (*Conn, error) {
	conn, err := ch.Open(&ch.Options{
		Addr: []string{cfg.Addr},
		Auth: ch.Auth{
			Database: cfg.Database,
			Username: cfg.Username,
			Password: cfg.Password,
		},
		DialTimeout:  orDefault(cfg.DialTimeout, 10*time.Second),
		MaxOpenConns: orInt(cfg.MaxOpenConns, 10),
	})
	if err != nil {
		return nil, fmt.Errorf("open clickhouse: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := conn.Ping(pingCtx); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("ping clickhouse: %w", err)
	}
	return &Conn{Conn: conn}, nil
}

func orDefault(d, def time.Duration) time.Duration {
	if d <= 0 {
		return def
	}
	return d
}

func orInt(v, def int) int {
	if v <= 0 {
		return def
	}
	return v
}
