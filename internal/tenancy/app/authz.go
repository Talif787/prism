package app

import (
	"context"

	"github.com/Talif787/prism/internal/tenancy/domain"
)

// AccessGrant is the outcome of a console authorization check: the caller's
// resolved identity and role within the tenant.
type AccessGrant struct {
	UserID domain.UserID
	Role   domain.Role
}

// Authorize resolves a console principal (by email) to a membership in the given
// tenant and checks the required permission. It is the single choke point for
// console authorization, so RBAC rules are enforced consistently.
func (s *Service) Authorize(ctx context.Context, email string, tenantID domain.TenantID, perm domain.Permission) (*AccessGrant, error) {
	var grant AccessGrant
	err := s.uow.Do(ctx, func(r Repositories) error {
		user, err := r.Users.GetByEmail(ctx, email)
		if err != nil {
			return err
		}
		role, err := r.Users.GetRole(ctx, tenantID, user.ID)
		if err != nil {
			return err
		}
		if !role.Allows(perm) {
			return domain.ErrScopeNotGranted
		}
		grant = AccessGrant{UserID: user.ID, Role: role}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &grant, nil
}
