// Command alerter is the alerting service: it serves the tenant-scoped rule and
// alert API, and runs a background loop that evaluates due rules against the
// metrics store, advances the alert state machine, and fires webhook notifications.
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

	"github.com/Talif787/prism/internal/alerting/api/rest"
	"github.com/Talif787/prism/internal/alerting/app"
	"github.com/Talif787/prism/internal/alerting/infra/authcache"
	"github.com/Talif787/prism/internal/alerting/infra/chreader"
	"github.com/Talif787/prism/internal/alerting/infra/pgstore"
	"github.com/Talif787/prism/internal/alerting/infra/webhook"
	"github.com/Talif787/prism/internal/platform/clickhouse"
	"github.com/Talif787/prism/internal/platform/config"
	"github.com/Talif787/prism/internal/platform/httpx"
	"github.com/Talif787/prism/internal/platform/logging"
	"github.com/Talif787/prism/internal/platform/observability"
	"github.com/Talif787/prism/internal/platform/postgres"
	"github.com/Talif787/prism/internal/platform/redis"
	"github.com/Talif787/prism/internal/query/infra/chstore"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.LoadAlerter()
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
	reader := chreader.New(chstore.New(conn))
	notifier := webhook.New(cfg.WebhookTimeout)
	auth := authcache.New(rdb.Client, authcache.Config{
		VerifyURL: cfg.ControlPlane.VerifyURL, InternalToken: cfg.ControlPlane.InternalToken,
		Timeout: cfg.ControlPlane.Timeout, CacheTTL: cfg.ControlPlane.CacheTTL,
		NegativeCacheTTL: cfg.ControlPlane.NegativeCacheTTL,
	})

	rules := app.NewRuleService(store)
	evaluator := app.NewEvaluator(store, reader, notifier, logger)
	handler := rest.NewHandler(rules, auth, logger)

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

	// Evaluation loop: on each tick, evaluate every rule whose interval elapsed.
	go func() {
		ticker := time.NewTicker(cfg.EvalInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := evaluator.EvaluateDue(ctx, time.Now().UTC()); err != nil {
					logger.ErrorContext(ctx, "evaluation cycle failed", slog.Any("error", err))
				}
			}
		}
	}()

	logger.Info("alerter starting",
		slog.String("env", string(cfg.Env)),
		slog.String("addr", cfg.HTTP.Addr()),
		slog.Duration("eval_interval", cfg.EvalInterval),
	)
	return srv.Run(ctx)
}
