// Package relay implements the outbox relay: it drains committed domain events
// from the control plane's outbox table and publishes them to Kafka, completing
// the transactional outbox pattern. Delivery is at-least-once; downstream
// consumers must be idempotent. Rows are claimed with FOR UPDATE SKIP LOCKED so
// multiple relay instances can run without processing the same row twice.
package relay

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Talif787/prism/internal/platform/kafka"
)

type Config struct {
	Topic        string
	BatchSize    int
	MaxAttempts  int
	PollInterval time.Duration
}

// Producer is the subset of the platform Kafka producer the relay needs, defined
// as an interface so the relay can be tested with a fake. *kafka.Producer satisfies it.
type Producer interface {
	Produce(ctx context.Context, msg kafka.Message) error
}

type Relay struct {
	pool     *pgxpool.Pool
	producer Producer
	cfg      Config
	logger   *slog.Logger
}

func New(pool *pgxpool.Pool, producer Producer, cfg Config, logger *slog.Logger) *Relay {
	return &Relay{pool: pool, producer: producer, cfg: cfg, logger: logger}
}

// RunOnce processes a single batch and returns the number of rows claimed. It
// exists so integration tests can drive the relay deterministically.
func (r *Relay) RunOnce(ctx context.Context) (int, error) { return r.processBatch(ctx) }

// Run polls until the context is cancelled. Each tick drains up to BatchSize rows.
func (r *Relay) Run(ctx context.Context) error {
	ticker := time.NewTicker(r.cfg.PollInterval)
	defer ticker.Stop()

	r.logger.Info("relay started",
		slog.String("topic", r.cfg.Topic),
		slog.Int("batch_size", r.cfg.BatchSize),
		slog.Duration("poll_interval", r.cfg.PollInterval),
	)

	for {
		select {
		case <-ctx.Done():
			r.logger.Info("relay stopping")
			return nil
		case <-ticker.C:
			// Drain fully when there is a backlog, so bursts clear quickly.
			for {
				n, err := r.processBatch(ctx)
				if err != nil {
					r.logger.Error("relay batch failed", slog.Any("error", err))
					break
				}
				if n < r.cfg.BatchSize {
					break
				}
			}
		}
	}
}

type outboxRow struct {
	id        string
	eventName string
	payload   []byte
}

func (r *Relay) processBatch(ctx context.Context) (int, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	rows, err := tx.Query(ctx, `
		SELECT id, event_name, payload
		FROM outbox
		WHERE published_at IS NULL AND attempts < $1
		ORDER BY created_at
		LIMIT $2
		FOR UPDATE SKIP LOCKED`, r.cfg.MaxAttempts, r.cfg.BatchSize)
	if err != nil {
		return 0, err
	}

	var batch []outboxRow
	for rows.Next() {
		var row outboxRow
		if err := rows.Scan(&row.id, &row.eventName, &row.payload); err != nil {
			rows.Close()
			return 0, err
		}
		batch = append(batch, row)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}

	published := 0
	for _, row := range batch {
		msg := kafka.Message{
			Topic: r.cfg.Topic,
			Key:   []byte(tenantKey(row.payload, row.id)),
			Value: row.payload,
			Headers: []kafka.Header{
				{Key: "event_name", Value: []byte(row.eventName)},
				{Key: "event_id", Value: []byte(row.id)},
			},
		}
		if err := r.producer.Produce(ctx, msg); err != nil {
			if _, uErr := tx.Exec(ctx,
				`UPDATE outbox SET attempts = attempts + 1, last_error = $2 WHERE id = $1`,
				row.id, err.Error(),
			); uErr != nil {
				return published, uErr
			}
			continue
		}
		if _, err := tx.Exec(ctx, `UPDATE outbox SET published_at = now() WHERE id = $1`, row.id); err != nil {
			return published, err
		}
		published++
	}

	if err := tx.Commit(ctx); err != nil {
		return published, err
	}
	if published > 0 {
		r.logger.Debug("relay published batch", slog.Int("count", published))
	}
	return len(batch), nil
}

// tenantKey extracts the tenant id from the event payload so per-tenant events
// share a partition and preserve order; it falls back to the event id.
func tenantKey(payload []byte, fallback string) string {
	var env struct {
		Data struct {
			TenantID string `json:"TenantID"`
		} `json:"data"`
	}
	if json.Unmarshal(payload, &env) == nil && env.Data.TenantID != "" {
		return env.Data.TenantID
	}
	return fallback
}
