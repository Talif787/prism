// Command controlplane runs the Prism control-plane service: tenants, users,
// API keys, and RBAC. It is the composition root, the one place where concrete
// implementations are wired to interfaces, keeping the rest of the code testable.
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

	"github.com/Talif787/prism/internal/platform/auth"
	"github.com/Talif787/prism/internal/platform/config"
	"github.com/Talif787/prism/internal/platform/httpx"
	"github.com/Talif787/prism/internal/platform/logging"
	"github.com/Talif787/prism/internal/platform/observability"
	"github.com/Talif787/prism/internal/platform/postgres"
	"github.com/Talif787/prism/internal/tenancy/api/rest"
	"github.com/Talif787/prism/internal/tenancy/app"
	"github.com/Talif787/prism/internal/tenancy/infra/pgstore"
	"github.com/Talif787/prism/migrations"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
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

	applied, err := postgres.NewMigrator(pool, migrations.FS).Up(ctx)
	if err != nil {
		return fmt.Errorf("run migrations: %w", err)
	}
	if len(applied) > 0 {
		logger.Info("migrations applied", slog.Any("versions", applied))
	}

	verifier, err := buildVerifier(ctx, cfg.Auth)
	if err != nil {
		return fmt.Errorf("build token verifier: %w", err)
	}

	// Wiring: the unit of work provides transaction-scoped repositories; the
	// hot-path key repository is bound to the pool for a single indexed read.
	uow := pgstore.NewUnitOfWork(pool, logger)
	keyRepo := pgstore.NewAPIKeyRepository(pool)
	svc := app.NewService(uow, keyRepo, logger)
	handler := rest.NewHandler(svc, logger)

	health := observability.NewHealth(2 * time.Second)
	health.Register(observability.CheckerFunc{
		CheckerName: "postgres",
		Fn:          func(ctx context.Context) error { return pool.Ping(ctx) },
	})

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", health.LiveHandler())
	mux.HandleFunc("GET /readyz", health.ReadyHandler())
	handler.Register(mux, rest.RouterConfig{Verifier: verifier, InternalToken: cfg.Internal.APIToken})

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

	logger.Info("control plane starting",
		slog.String("env", string(cfg.Env)),
		slog.String("addr", cfg.HTTP.Addr()),
		slog.String("auth_mode", string(cfg.Auth.Mode)),
	)
	return server.Run(ctx)
}

func buildVerifier(ctx context.Context, cfg config.AuthConfig) (auth.TokenVerifier, error) {
	switch cfg.Mode {
	case config.AuthModeOIDC:
		return auth.NewOIDCVerifier(ctx, cfg.OIDCIssuer, cfg.OIDCAudience)
	default:
		return auth.NewDevVerifier(cfg.DevHS256Secret, cfg.OIDCAudience), nil
	}
}
