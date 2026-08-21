// Command metering is the metering and billing service: it serves the tenant-scoped
// usage, quota, cost, and invoice API, and runs a background loop that rolls up
// accepted telemetry from ClickHouse into per-tenant usage records in Postgres.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Talif787/prism/internal/metering/api/rest"
	"github.com/Talif787/prism/internal/metering/app"
	"github.com/Talif787/prism/internal/metering/domain"
	"github.com/Talif787/prism/internal/metering/infra/authcache"
	"github.com/Talif787/prism/internal/metering/infra/chsource"
	"github.com/Talif787/prism/internal/metering/infra/pgstore"
	"github.com/Talif787/prism/internal/platform/clickhouse"
	"github.com/Talif787/prism/internal/platform/config"
	"github.com/Talif787/prism/internal/platform/httpx"
	"github.com/Talif787/prism/internal/platform/logging"
	"github.com/Talif787/prism/internal/platform/observability"
	"github.com/Talif787/prism/internal/platform/postgres"
	"github.com/Talif787/prism/internal/platform/redis"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.LoadMetering()
	if err != nil {
		return err
	}

	logger := logging.New(cfg.Log.Level, cfg.Log.Format)
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	shutdownOTel, err := observability.Setup(ctx, observability.Config{
		Enabled: cfg.OTel.Enabled, ServiceName: cfg.OTel.ServiceName,
		Environment: string(cfg.Env), OTLPEndpoint: cfg.OTel.OTLPEndpoint,
		SamplerRatio: cfg.OTel.SamplerRatio,
	})
	if err != nil {
		return fmt.Errorf("setup observability: %w", err)
	}
	defer func() {
		sctx, c := context.WithTimeout(context.Background(), 5*time.Second)
		defer c()
		_ = shutdownOTel(sctx)
	}()

	pool, err := postgres.NewPool(ctx, postgres.PoolConfig{
		URL: cfg.DB.URL, MaxConns: cfg.DB.MaxConns, MinConns: cfg.DB.MinConns,
		ConnMaxLifetime: cfg.DB.ConnMaxLifetime,
	})
	if err != nil {
		return fmt.Errorf("connect postgres: %w", err)
	}
	defer pool.Close()

	conn, err := clickhouse.New(ctx, clickhouse.Config{
		Addr: cfg.ClickHouse.Addr, Database: cfg.ClickHouse.Database,
		Username: cfg.ClickHouse.Username, Password: cfg.ClickHouse.Password,
		DialTimeout: cfg.ClickHouse.DialTimeout, MaxOpenConns: cfg.ClickHouse.MaxOpenConns,
		Settings: map[string]any{"max_execution_time": cfg.MaxExecutionSeconds},
	})
	if err != nil {
		return fmt.Errorf("connect clickhouse: %w", err)
	}
	defer func() { _ = conn.Close() }()

	rdb, err := redis.New(ctx, redis.Config{
		Addr: cfg.Redis.Addr, Password: cfg.Redis.Password, DB: cfg.Redis.DB, PoolSize: cfg.Redis.PoolSize,
	})
	if err != nil {
		return fmt.Errorf("connect redis: %w", err)
	}
	defer func() { _ = rdb.Close() }()

	store := pgstore.New(pool)
	source := chsource.New(conn)
	rollup := app.NewRollupService(source, store, cfg.RollupInterval, cfg.Backfill, logger)

	rateCard := domain.RateCard{
		PerMillion: map[domain.Signal]float64{
			domain.SignalMetrics: cfg.PriceMetricsPerM,
			domain.SignalLogs:    cfg.PriceLogsPerM,
			domain.SignalTraces:  cfg.PriceTracesPerM,
		},
		Currency: cfg.Currency,
	}
	planQuotas := map[string]int64{
		"free":       cfg.QuotaFree,
		"team":       cfg.QuotaTeam,
		"enterprise": cfg.QuotaEnterprise,
	}

	meteringSvc := app.NewMeteringService(store, rateCard, planQuotas)
	auth := authcache.New(rdb.Client, authcache.Config{
		VerifyURL: cfg.ControlPlane.VerifyURL, InternalToken: cfg.ControlPlane.InternalToken,
		Timeout: cfg.ControlPlane.Timeout, CacheTTL: cfg.ControlPlane.CacheTTL,
		NegativeCacheTTL: cfg.ControlPlane.NegativeCacheTTL,
	})
	handler := rest.NewHandler(meteringSvc, auth, logger)

	health := observability.NewHealth(5 * time.Second)
	health.Register(observability.CheckerFunc{CheckerName: "postgres", Fn: func(ctx context.Context) error { return pool.Ping(ctx) }})
	health.Register(observability.CheckerFunc{CheckerName: "clickhouse", Fn: func(ctx context.Context) error { return conn.Ping(ctx) }})
	health.Register(observability.CheckerFunc{CheckerName: "redis", Fn: func(ctx context.Context) error { return rdb.Ping(ctx).Err() }})

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", health.LiveHandler())
	mux.HandleFunc("GET /readyz", health.ReadyHandler())
	mux.Handle("/", handler.Routes())

	root := httpx.Chain(mux, httpx.RequestID(), httpx.Recover(logger))
	srv := httpx.NewServer(httpx.ServerConfig{
		Addr: cfg.HTTP.Addr(), ReadTimeout: cfg.HTTP.ReadTimeout,
		WriteTimeout: cfg.HTTP.WriteTimeout, ShutdownTimeout: cfg.HTTP.ShutdownTimeout,
	}, root, logger)

	// Rollup loop: meter closed windows on startup and then on each tick.
	go func() {
		if err := rollup.RunOnce(ctx, time.Now().UTC()); err != nil {
			logger.ErrorContext(ctx, "initial rollup failed", slog.Any("error", err))
		}
		ticker := time.NewTicker(cfg.RollupTick)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := rollup.RunOnce(ctx, time.Now().UTC()); err != nil {
					logger.ErrorContext(ctx, "rollup cycle failed", slog.Any("error", err))
				}
			}
		}
	}()

	logger.Info("metering starting",
		slog.String("env", string(cfg.Env)),
		slog.String("addr", cfg.HTTP.Addr()),
		slog.Duration("rollup_interval", cfg.RollupInterval),
		slog.Duration("rollup_tick", cfg.RollupTick),
	)
	return srv.Run(ctx)
}
