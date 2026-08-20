//go:build integration

// Package integration exercises the Postgres repositories against a real database
// spun up with testcontainers. Run with: go test -tags=integration ./test/integration/...
// A Docker daemon must be available. These tests validate the SQL, constraints,
// and mapping code that unit tests with fakes cannot cover.
package integration

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"log/slog"
	"os"

	"github.com/Talif787/prism/internal/platform/postgres"
	"github.com/Talif787/prism/internal/tenancy/app"
	"github.com/Talif787/prism/internal/tenancy/domain"
	"github.com/Talif787/prism/internal/tenancy/infra/pgstore"
	"github.com/Talif787/prism/migrations"
)

func setup(t *testing.T) (*app.Service, *pgxpool.Pool) {
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

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	svc := app.NewService(pgstore.NewUnitOfWork(pool, logger), pgstore.NewAPIKeyRepository(pool), logger)
	return svc, pool
}

func TestTenantLifecycle(t *testing.T) {
	svc, _ := setup(t)
	ctx := context.Background()

	created, err := svc.CreateTenant(ctx, app.CreateTenantInput{
		Name: "Acme", Slug: "acme", Plan: domain.PlanTeam, OwnerEmail: "owner@acme.io", OwnerName: "Owner",
	})
	if err != nil {
		t.Fatalf("create tenant: %v", err)
	}

	got, err := svc.GetTenant(ctx, created.Tenant.ID)
	if err != nil {
		t.Fatalf("get tenant: %v", err)
	}
	if got.Slug != "acme" {
		t.Fatalf("slug mismatch: %s", got.Slug)
	}

	members, err := svc.ListMembers(ctx, created.Tenant.ID)
	if err != nil {
		t.Fatalf("list members: %v", err)
	}
	if len(members) != 1 || members[0].Role != domain.RoleOwner {
		t.Fatalf("expected single owner, got %+v", members)
	}
}

func TestKeyIssueVerifyRevoke(t *testing.T) {
	svc, _ := setup(t)
	ctx := context.Background()

	tenant, err := svc.CreateTenant(ctx, app.CreateTenantInput{
		Name: "Acme", Slug: "acme", OwnerEmail: "owner@acme.io",
	})
	if err != nil {
		t.Fatalf("create tenant: %v", err)
	}

	issued, err := svc.IssueKey(ctx, app.IssueKeyInput{
		TenantID: tenant.Tenant.ID, Name: "ingest", Scopes: []domain.Scope{domain.ScopeIngest},
	})
	if err != nil {
		t.Fatalf("issue key: %v", err)
	}

	authed, err := svc.AuthenticateKey(ctx, issued.Plaintext, domain.ScopeIngest)
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	if authed.TenantID != tenant.Tenant.ID {
		t.Fatal("tenant mismatch")
	}

	if err := svc.RevokeKey(ctx, tenant.Tenant.ID, issued.Key.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, err := svc.AuthenticateKey(ctx, issued.Plaintext, domain.ScopeIngest); err == nil {
		t.Fatal("expected authentication to fail after revoke")
	}
}

func TestLastOwnerInvariantEnforcedInDB(t *testing.T) {
	svc, _ := setup(t)
	ctx := context.Background()

	tenant, _ := svc.CreateTenant(ctx, app.CreateTenantInput{
		Name: "Acme", Slug: "acme", OwnerEmail: "owner@acme.io",
	})
	err := svc.RemoveMember(ctx, tenant.Tenant.ID, tenant.Owner.ID)
	if err != domain.ErrLastOwner {
		t.Fatalf("expected ErrLastOwner, got %v", err)
	}
}
