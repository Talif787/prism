// Command gateway is the ingest gateway: it accepts OTLP/HTTP telemetry,
// authenticates the API key (cached), enforces per-tenant rate and cardinality
// limits, and produces validated telemetry to Kafka. It is the composition root
// for the ingest path.
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

	"github.com/Talif787/prism/internal/ingest/api/otlphttp"
	"github.com/Talif787/prism/internal/ingest/app"
	"github.com/Talif787/prism/internal/ingest/infra/authcache"
	"github.com/Talif787/prism/internal/ingest/infra/cardinality"
	"github.com/Talif787/prism/internal/ingest/infra/kafkaprod"
	"github.com/Talif787/prism/internal/ingest/infra/ratelimit"
	"github.com/Talif787/prism/internal/platform/config"
	"github.com/Talif787/prism/internal/platform/httpx"
	"github.com/Talif787/prism/internal/platform/kafka"
	"github.com/Talif787/prism/internal/platform/logging"
	"github.com/Talif787/prism/internal/platform/observability"
	"github.com/Talif787/prism/internal/platform/redis"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.LoadGateway()
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
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = shutdownOTel(shutdownCtx)
	}()

	rdb, err := redis.New(ctx, redis.Config{
		Addr: cfg.Redis.Addr, Password: cfg.Redis.Password,
		DB: cfg.Redis.DB, PoolSize: cfg.Redis.PoolSize,
	})
	if err != nil {
		return fmt.Errorf("connect redis: %w", err)
	}
	defer rdb.Close()

	producer := kafka.NewProducer(kafka.Config{
		Brokers:                cfg.Kafka.Brokers,
		RequiredAcks:           cfg.Kafka.RequiredAcks,
		AllowAutoTopicCreation: cfg.Kafka.AllowAutoTopicCreation,
	})
	defer producer.Close()

	authenticator := authcache.New(rdb.Client, authcache.Config{
		VerifyURL: cfg.ControlPlane.VerifyURL, InternalToken: cfg.ControlPlane.InternalToken,
		Timeout: cfg.ControlPlane.Timeout, CacheTTL: cfg.ControlPlane.CacheTTL,
		NegativeCacheTTL: cfg.ControlPlane.NegativeCacheTTL,
	})
	limiter := ratelimit.New(rdb.Client, ratelimit.Config{
		RatePerSecond: cfg.Limits.RatePerSecond, Burst: cfg.Limits.Burst,
	})
	guard := cardinality.New(rdb.Client, cardinality.Config{
		Budget: cfg.Limits.CardinalityBudget, Window: cfg.Limits.CardinalityWindow,
		Enforce: cfg.Limits.EnforceCardinality,
	})
	producerAdapter := kafkaprod.New(producer)

	pipeline := app.NewPipeline(authenticator, limiter, guard, producerAdapter, app.Topics{
		Metrics: cfg.Kafka.TopicMetrics, Logs: cfg.Kafka.TopicLogs, Traces: cfg.Kafka.TopicTraces,
	}, logger)

	handler := otlphttp.NewHandler(pipeline, cfg.Limits.MaxBodyBytes, logger)

	health := observability.NewHealth(3 * time.Second)
	health.Register(observability.CheckerFunc{
		CheckerName: "redis",
		Fn:          func(ctx context.Context) error { return rdb.Ping(ctx).Err() },
	})
	health.Register(observability.CheckerFunc{
		CheckerName: "kafka",
		Fn:          func(ctx context.Context) error { return kafka.Ping(ctx, cfg.Kafka.Brokers) },
	})

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", health.LiveHandler())
	mux.HandleFunc("GET /readyz", health.ReadyHandler())
	handler.Register(mux)

	root := httpx.Chain(mux,
		httpx.RequestID(),
		httpx.Trace(cfg.OTel.ServiceName),
		httpx.Recover(logger),
		httpx.AccessLog(logger),
	)

	server := httpx.NewServer(httpx.ServerConfig{
		Addr: cfg.HTTP.Addr(), ReadTimeout: cfg.HTTP.ReadTimeout,
		WriteTimeout: cfg.HTTP.WriteTimeout, ShutdownTimeout: cfg.HTTP.ShutdownTimeout,
	}, root, logger)

	logger.Info("gateway starting",
		slog.String("env", string(cfg.Env)),
		slog.String("addr", cfg.HTTP.Addr()),
		slog.Bool("cardinality_enforced", cfg.Limits.EnforceCardinality),
	)
	return server.Run(ctx)
}
