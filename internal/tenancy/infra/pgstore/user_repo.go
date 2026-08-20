package pgstore

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/Talif787/prism/internal/tenancy/app"
	"github.com/Talif787/prism/internal/tenancy/domain"
)

type UserRepository struct{ q querier }

func NewUserRepository(q querier) *UserRepository { return &UserRepository{q: q} }

// Upsert inserts a user or returns the existing row keyed by email, so inviting
// an existing user to a second tenant reuses their identity.
func (r *UserRepository) Upsert(ctx context.Context, u *domain.User) (*domain.User, error) {
	var idStr string
	err := r.q.QueryRow(ctx, `
		INSERT INTO users (id, email, display_name, external_subject, created_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (email) DO UPDATE SET display_name = EXCLUDED.display_name
		RETURNING id`,
		u.ID.String(), u.Email, u.DisplayName, u.ExternalSubject, u.CreatedAt,
	).Scan(&idStr)
	if err != nil {
		return nil, err
	}
	id, err := domain.ParseUserID(idStr)
	if err != nil {
		return nil, err
	}
	u.ID = id
	return u, nil
}

func (r *UserRepository) GetByEmail(ctx context.Context, email string) (*domain.User, error) {
	var (
		u     domain.User
		idStr string
	)
	err := r.q.QueryRow(ctx, `
		SELECT id, email, display_name, external_subject, created_at
		FROM users WHERE email = $1`, email,
	).Scan(&idStr, &u.Email, &u.DisplayName, &u.ExternalSubject, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	id, err := domain.ParseUserID(idStr)
	if err != nil {
		return nil, err
	}
	u.ID = id
	return &u, nil
}

func (r *UserRepository) AddMembership(ctx context.Context, m domain.Membership) error {
	_, err := r.q.Exec(ctx, `
		INSERT INTO memberships (tenant_id, user_id, role, created_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (tenant_id, user_id) DO UPDATE SET role = EXCLUDED.role`,
		m.TenantID.String(), m.UserID.String(), string(m.Role), m.CreatedAt,
	)
	return err
}

func (r *UserRepository) RemoveMembership(ctx context.Context, tenantID domain.TenantID, userID domain.UserID) error {
	tag, err := r.q.Exec(ctx, `DELETE FROM memberships WHERE tenant_id = $1 AND user_id = $2`,
		tenantID.String(), userID.String())
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *UserRepository) ListMemberships(ctx context.Context, tenantID domain.TenantID) ([]app.MembershipView, error) {
	rows, err := r.q.Query(ctx, `
		SELECT u.id, u.email, u.display_name, m.role
		FROM memberships m
		JOIN users u ON u.id = m.user_id
		WHERE m.tenant_id = $1
		ORDER BY m.created_at ASC`, tenantID.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []app.MembershipView
	for rows.Next() {
		var (
			idStr string
			v     app.MembershipView
			role  string
		)
		if err := rows.Scan(&idStr, &v.Email, &v.DisplayName, &role); err != nil {
			return nil, err
		}
		uid, err := domain.ParseUserID(idStr)
		if err != nil {
			return nil, err
		}
		v.UserID = uid
		v.Role = domain.Role(role)
		out = append(out, v)
	}
	return out, rows.Err()
}

func (r *UserRepository) CountOwners(ctx context.Context, tenantID domain.TenantID) (int, error) {
	var n int
	err := r.q.QueryRow(ctx, `
		SELECT count(*) FROM memberships WHERE tenant_id = $1 AND role = $2`,
		tenantID.String(), string(domain.RoleOwner),
	).Scan(&n)
	return n, err
}

func (r *UserRepository) GetRole(ctx context.Context, tenantID domain.TenantID, userID domain.UserID) (domain.Role, error) {
	var role string
	err := r.q.QueryRow(ctx, `
		SELECT role FROM memberships WHERE tenant_id = $1 AND user_id = $2`,
		tenantID.String(), userID.String(),
	).Scan(&role)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", domain.ErrNotFound
	}
	if err != nil {
		return "", err
	}
	return domain.Role(role), nil
}
