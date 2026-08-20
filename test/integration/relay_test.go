//go:build integration

package integration

import (
	"context"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/Talif787/prism/internal/platform/kafka"
	"github.com/Talif787/prism/internal/platform/postgres"
	"github.com/Talif787/prism/internal/relay"
	"github.com/Talif787/prism/migrations"
)

// captureProducer records produced messages instead of talking to a broker.
type captureProducer struct {
	mu   sync.Mutex
	msgs []kafka.Message
	fail bool
}

func (c *captureProducer) Produce(_ context.Context, m kafka.Message) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.fail {
		return context.DeadlineExceeded
	}
	c.msgs = append(c.msgs, m)
	return nil
}

func newPGForRelay(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()
	container, err := tcpostgres.Run(ctx, "postgres:16-alpine",
		tcpostgres.WithDatabase("prism"),
		tcpostgres.WithUsername("prism"),
		tcpostgres.WithPassword("prism"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(60*time.Second),
		),
	)
	if err != nil {
		t.Fatalf("start postgres: %v", err)
	}
	t.Cleanup(func() { _ = container.Terminate(ctx) })

	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("connection string: %v", err)
	}
	pool, err := postgres.NewPool(ctx, postgres.PoolConfig{URL: dsn, MaxConns: 5, MinConns: 1, ConnMaxLifetime: time.Minute})
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	t.Cleanup(pool.Close)
	if _, err := postgres.NewMigrator(pool, migrations.FS).Up(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return pool
}

func insertOutbox(t *testing.T, pool *pgxpool.Pool, tenantID string) string {
	t.Helper()
	id := uuid.NewString()
	payload := `{"name":"tenant.created","data":{"TenantID":"` + tenantID + `"}}`
	_, err := pool.Exec(context.Background(), `
		INSERT INTO outbox (id, event_name, payload, occurred_at, created_at)
		VALUES ($1, $2, $3::jsonb, now(), now())`, id, "tenant.created", payload)
	if err != nil {
		t.Fatalf("insert outbox: %v", err)
	}
	return id
}

func TestRelay_PublishesAndMarks(t *testing.T) {
	pool := newPGForRelay(t)
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	cap := &captureProducer{}
	r := relay.New(pool, cap, relay.Config{
		Topic: "tenancy.events", BatchSize: 10, MaxAttempts: 5, PollInterval: time.Second,
	}, logger)

	id := insertOutbox(t, pool, "11111111-1111-1111-1111-111111111111")

	n, err := r.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("run once: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 row processed, got %d", n)
	}
	if len(cap.msgs) != 1 {
		t.Fatalf("expected 1 produced message, got %d", len(cap.msgs))
	}
	if got := string(cap.msgs[0].Key); got != "11111111-1111-1111-1111-111111111111" {
		t.Fatalf("message should be keyed by tenant id, got %q", got)
	}

	// The row must be marked published so it is not re-sent.
	var published *time.Time
	if err := pool.QueryRow(context.Background(),
		`SELECT published_at FROM outbox WHERE id = $1`, id).Scan(&published); err != nil {
		t.Fatalf("query published_at: %v", err)
	}
	if published == nil {
		t.Fatal("published_at should be set after a successful publish")
	}
}

func TestRelay_RecordsFailure(t *testing.T) {
	pool := newPGForRelay(t)
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	cap := &captureProducer{fail: true}
	r := relay.New(pool, cap, relay.Config{
		Topic: "tenancy.events", BatchSize: 10, MaxAttempts: 5, PollInterval: time.Second,
	}, logger)

	id := insertOutbox(t, pool, "22222222-2222-2222-2222-222222222222")

	if _, err := r.RunOnce(context.Background()); err != nil {
		t.Fatalf("run once: %v", err)
	}

	var attempts int
	var lastErr *string
	if err := pool.QueryRow(context.Background(),
		`SELECT attempts, last_error FROM outbox WHERE id = $1`, id).Scan(&attempts, &lastErr); err != nil {
		t.Fatalf("query: %v", err)
	}
	if attempts != 1 {
		t.Fatalf("expected attempts=1 after a failed publish, got %d", attempts)
	}
	if lastErr == nil || *lastErr == "" {
		t.Fatal("last_error should record the failure")
	}
}
