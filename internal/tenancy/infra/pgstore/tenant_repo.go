package pgstore

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/Talif787/prism/internal/tenancy/domain"
)

type TenantRepository struct{ q querier }

func NewTenantRepository(q querier) *TenantRepository { return &TenantRepository{q: q} }

func (r *TenantRepository) Create(ctx context.Context, t *domain.Tenant) error {
	_, err := r.q.Exec(ctx, `
		INSERT INTO tenants (id, name, slug, plan, status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		t.ID.String(), t.Name, t.Slug, string(t.Plan), string(t.Status), t.CreatedAt, t.UpdatedAt,
	)
	return err
}

func (r *TenantRepository) GetByID(ctx context.Context, id domain.TenantID) (*domain.Tenant, error) {
	return r.scanOne(ctx, `
		SELECT id, name, slug, plan, status, created_at, updated_at
		FROM tenants WHERE id = $1`, id.String())
}

func (r *TenantRepository) GetBySlug(ctx context.Context, slug string) (*domain.Tenant, error) {
	return r.scanOne(ctx, `
		SELECT id, name, slug, plan, status, created_at, updated_at
		FROM tenants WHERE slug = $1`, slug)
}

func (r *TenantRepository) scanOne(ctx context.Context, sql string, arg any) (*domain.Tenant, error) {
	var (
		t            domain.Tenant
		idStr        string
		plan, status string
	)
	err := r.q.QueryRow(ctx, sql, arg).Scan(
		&idStr, &t.Name, &t.Slug, &plan, &status, &t.CreatedAt, &t.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	id, err := domain.ParseTenantID(idStr)
	if err != nil {
		return nil, err
	}
	t.ID = id
	t.Plan = domain.Plan(plan)
	t.Status = domain.TenantStatus(status)
	return &t, nil
}
