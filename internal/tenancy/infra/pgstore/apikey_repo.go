package pgstore

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Talif787/prism/internal/tenancy/domain"
)

type APIKeyRepository struct{ q querier }

func NewAPIKeyRepository(q querier) *APIKeyRepository { return &APIKeyRepository{q: q} }

func (r *APIKeyRepository) Create(ctx context.Context, k *domain.APIKey) error {
	_, err := r.q.Exec(ctx, `
		INSERT INTO api_keys (id, tenant_id, name, prefix, hash, scopes, status, created_at, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		k.ID.String(), k.TenantID.String(), k.Name, k.Prefix, k.Hash,
		scopesToStrings(k.Scopes), string(k.Status), k.CreatedAt, k.ExpiresAt,
	)
	return err
}

func (r *APIKeyRepository) GetByPrefix(ctx context.Context, prefix string) (*domain.APIKey, error) {
	return r.scanOne(ctx, `
		SELECT id, tenant_id, name, prefix, hash, scopes, status, created_at, expires_at, last_used_at
		FROM api_keys WHERE prefix = $1`, prefix)
}

func (r *APIKeyRepository) GetByID(ctx context.Context, tenantID domain.TenantID, id domain.APIKeyID) (*domain.APIKey, error) {
	return r.scanOne(ctx, `
		SELECT id, tenant_id, name, prefix, hash, scopes, status, created_at, expires_at, last_used_at
		FROM api_keys WHERE tenant_id = $1 AND id = $2`, tenantID.String(), id.String())
}

func (r *APIKeyRepository) ListByTenant(ctx context.Context, tenantID domain.TenantID) ([]*domain.APIKey, error) {
	rows, err := r.q.Query(ctx, `
		SELECT id, tenant_id, name, prefix, hash, scopes, status, created_at, expires_at, last_used_at
		FROM api_keys WHERE tenant_id = $1 ORDER BY created_at DESC`, tenantID.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*domain.APIKey
	for rows.Next() {
		k, err := scanKey(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

func (r *APIKeyRepository) UpdateStatus(ctx context.Context, id domain.APIKeyID, status domain.APIKeyStatus) error {
	tag, err := r.q.Exec(ctx, `UPDATE api_keys SET status = $2 WHERE id = $1`, id.String(), string(status))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *APIKeyRepository) TouchLastUsed(ctx context.Context, id domain.APIKeyID) error {
	_, err := r.q.Exec(ctx, `UPDATE api_keys SET last_used_at = $2 WHERE id = $1`, id.String(), time.Now().UTC())
	return err
}

func (r *APIKeyRepository) scanOne(ctx context.Context, sql string, args ...any) (*domain.APIKey, error) {
	row := r.q.QueryRow(ctx, sql, args...)
	k, err := scanKey(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	return k, err
}

// rowScanner unifies pgx.Row and pgx.Rows for a single scan implementation.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanKey(row rowScanner) (*domain.APIKey, error) {
	var (
		k           domain.APIKey
		idStr       string
		tenantStr   string
		scopeStrs   []string
		status      string
		expiresAt   *time.Time
		lastUsedAt  *time.Time
	)
	if err := row.Scan(
		&idStr, &tenantStr, &k.Name, &k.Prefix, &k.Hash,
		&scopeStrs, &status, &k.CreatedAt, &expiresAt, &lastUsedAt,
	); err != nil {
		return nil, err
	}
	id, err := domain.ParseAPIKeyID(idStr)
	if err != nil {
		return nil, err
	}
	tenantID, err := domain.ParseTenantID(tenantStr)
	if err != nil {
		return nil, err
	}
	k.ID = id
	k.TenantID = tenantID
	k.Scopes = stringsToScopes(scopeStrs)
	k.Status = domain.APIKeyStatus(status)
	k.ExpiresAt = expiresAt
	k.LastUsedAt = lastUsedAt
	return &k, nil
}

func scopesToStrings(scopes []domain.Scope) []string {
	out := make([]string, len(scopes))
	for i, s := range scopes {
		out[i] = string(s)
	}
	return out
}

func stringsToScopes(in []string) []domain.Scope {
	out := make([]domain.Scope, len(in))
	for i, s := range in {
		out[i] = domain.Scope(s)
	}
	return out
}
