// Command relay drains the control-plane outbox to Kafka, completing the
// transactional outbox pattern with at-least-once delivery.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Talif787/prism/internal/platform/config"
	"github.com/Talif787/prism/internal/platform/kafka"
	"github.com/Talif787/prism/internal/platform/logging"
	"github.com/Talif787/prism/internal/platform/observability"
	"github.com/Talif787/prism/internal/platform/postgres"
	"github.com/Talif787/prism/internal/relay"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.LoadRelay()
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

	pool, err := postgres.NewPool(ctx, postgres.PoolConfig{
		URL: cfg.DB.URL, MaxConns: cfg.DB.MaxConns,
		MinConns: cfg.DB.MinConns, ConnMaxLifetime: cfg.DB.ConnMaxLifetime,
	})
	if err != nil {
		return fmt.Errorf("connect database: %w", err)
	}
	defer pool.Close()

	producer := kafka.NewProducer(kafka.Config{
		Brokers:                cfg.Kafka.Brokers,
		RequiredAcks:           cfg.Kafka.RequiredAcks,
		AllowAutoTopicCreation: cfg.Kafka.AllowAutoTopicCreation,
	})
	defer producer.Close()

	r := relay.New(pool, producer, relay.Config{
		Topic:        cfg.Kafka.TopicEvents,
		BatchSize:    cfg.BatchSize,
		MaxAttempts:  cfg.MaxAttempts,
		PollInterval: cfg.PollInterval,
	}, logger)

	logger.Info("relay starting", slog.String("env", string(cfg.Env)))
	return r.Run(ctx)
}
