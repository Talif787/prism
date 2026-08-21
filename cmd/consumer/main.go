// Command consumer reads telemetry from Kafka, transforms OTLP payloads into
// rows, and batch-writes them to ClickHouse. It runs one consume loop per signal
// (metrics, logs, traces) under a shared consumer group, and exits on the first
// unrecoverable write error so the orchestrator restarts it (at-least-once).
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/Talif787/prism/internal/consumer/app"
	"github.com/Talif787/prism/internal/consumer/domain"
	"github.com/Talif787/prism/internal/consumer/infra/chwriter"
	"github.com/Talif787/prism/internal/platform/clickhouse"
	"github.com/Talif787/prism/internal/platform/config"
	"github.com/Talif787/prism/internal/platform/httpx"
	"github.com/Talif787/prism/internal/platform/kafka"
	"github.com/Talif787/prism/internal/platform/logging"
	"github.com/Talif787/prism/internal/platform/observability"
	chmigrations "github.com/Talif787/prism/migrations/clickhouse"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.LoadConsumer()
	if err != nil {
		return err
	}

	logger := logging.New(cfg.Log.Level, cfg.Log.Format)
	slog.SetDefault(logger)

	signalCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithCancel(signalCtx)
	defer cancel()

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

	conn, err := clickhouse.New(ctx, clickhouse.Config{
		Addr: cfg.ClickHouse.Addr, Database: cfg.ClickHouse.Database,
		Username: cfg.ClickHouse.Username, Password: cfg.ClickHouse.Password,
		DialTimeout: cfg.ClickHouse.DialTimeout, MaxOpenConns: cfg.ClickHouse.MaxOpenConns,
	})
	if err != nil {
		return fmt.Errorf("connect clickhouse: %w", err)
	}
	defer func() { _ = conn.Close() }()

	if err := conn.Migrate(ctx, chmigrations.FS, "0001_init.sql"); err != nil {
		return fmt.Errorf("apply clickhouse migrations: %w", err)
	}
	logger.Info("clickhouse schema ready")

	writer := chwriter.New(conn)

	newReader := func(topic string) *kafka.Reader {
		return kafka.NewReader(kafka.ReaderConfig{
			Brokers: cfg.Kafka.Brokers, GroupID: cfg.GroupID, Topic: topic,
		})
	}
	metricReader := newReader(cfg.Kafka.TopicMetrics)
	logReader := newReader(cfg.Kafka.TopicLogs)
	traceReader := newReader(cfg.Kafka.TopicTraces)

	// Health server: liveness always ok; readiness checks ClickHouse and Kafka.
	health := observability.NewHealth(5 * time.Second)
	health.Register(observability.CheckerFunc{
		CheckerName: "clickhouse",
		Fn:          func(ctx context.Context) error { return conn.Ping(ctx) },
	})
	health.Register(observability.CheckerFunc{
		CheckerName: "kafka",
		Fn:          func(ctx context.Context) error { return kafka.Ping(ctx, cfg.Kafka.Brokers) },
	})
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", health.LiveHandler())
	mux.HandleFunc("GET /readyz", health.ReadyHandler())
	healthSrv := httpx.NewServer(httpx.ServerConfig{
		Addr: cfg.HTTP.Addr(), ReadTimeout: cfg.HTTP.ReadTimeout,
		WriteTimeout: cfg.HTTP.WriteTimeout, ShutdownTimeout: cfg.HTTP.ShutdownTimeout,
	}, mux, logger)
	go func() {
		if err := healthSrv.Run(ctx); err != nil {
			logger.Error("health server stopped", slog.Any("error", err))
		}
	}()

	batchCfg := app.BatchConfig{MaxRows: cfg.BatchMaxRows, MaxWait: cfg.BatchMaxWait}

	var wg sync.WaitGroup
	errCh := make(chan error, 3)
	launch := func(name string, fn func() error) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := fn(); err != nil {
				errCh <- fmt.Errorf("%s consumer: %w", name, err)
			}
		}()
	}

	launch("metrics", func() error {
		return app.Consume[domain.MetricRow](ctx, metricReader, "metrics",
			func(m kafka.FetchedMessage) ([]domain.MetricRow, error) {
				rms, err := domain.DecodeMetrics(m.Value)
				if err != nil {
					return nil, err
				}
				return domain.TransformMetrics(string(m.Key), rms), nil
			}, writer.WriteMetrics, batchCfg, logger)
	})
	launch("logs", func() error {
		return app.Consume[domain.LogRow](ctx, logReader, "logs",
			func(m kafka.FetchedMessage) ([]domain.LogRow, error) {
				rls, err := domain.DecodeLogs(m.Value)
				if err != nil {
					return nil, err
				}
				return domain.TransformLogs(string(m.Key), rls), nil
			}, writer.WriteLogs, batchCfg, logger)
	})
	launch("traces", func() error {
		return app.Consume[domain.SpanRow](ctx, traceReader, "traces",
			func(m kafka.FetchedMessage) ([]domain.SpanRow, error) {
				rss, err := domain.DecodeTraces(m.Value)
				if err != nil {
					return nil, err
				}
				return domain.TransformTraces(string(m.Key), rss), nil
			}, writer.WriteSpans, batchCfg, logger)
	})

	logger.Info("consumer started",
		slog.String("env", string(cfg.Env)),
		slog.String("group", cfg.GroupID),
		slog.Int("batch_max_rows", cfg.BatchMaxRows),
		slog.Duration("batch_max_wait", cfg.BatchMaxWait),
	)

	var runErr error
	select {
	case err := <-errCh:
		logger.Error("consumer loop failed; shutting down", slog.Any("error", err))
		runErr = err
	case <-ctx.Done():
		logger.Info("shutdown signal received")
	}

	cancel()  // tell all loops to drain and stop
	wg.Wait() // wait for final flushes and offset commits

	_ = metricReader.Close()
	_ = logReader.Close()
	_ = traceReader.Close()

	logger.Info("consumer stopped")
	return runErr
}
