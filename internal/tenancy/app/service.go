package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/Talif787/prism/internal/tenancy/domain"
)

// Service implements the tenancy use cases. It is transport-agnostic and depends
// only on ports, so it is fully unit-testable with fakes.
type Service struct {
	uow    UnitOfWork
	keys   APIKeyRepository
	logger *slog.Logger
}

// NewService wires the service. keys is provided directly (outside a transaction)
// for the hot-path authentication use case, which is a single indexed read.
func NewService(uow UnitOfWork, keys APIKeyRepository, logger *slog.Logger) *Service {
	return &Service{uow: uow, keys: keys, logger: logger}
}

// CreateTenant provisions a tenant and its first owner atomically. Creating a
// tenant without an owner would violate the "at least one owner" invariant, so
// both are created in one transaction.
func (s *Service) CreateTenant(ctx context.Context, in CreateTenantInput) (*CreateTenantOutput, error) {
	tenant, err := domain.NewTenant(in.Name, in.Slug, in.Plan)
	if err != nil {
		return nil, err
	}
	owner, err := domain.NewUser(in.OwnerEmail, in.OwnerName, in.OwnerSubject)
	if err != nil {
		return nil, err
	}

	var out CreateTenantOutput
	err = s.uow.Do(ctx, func(r Repositories) error {
		if existing, err := r.Tenants.GetBySlug(ctx, tenant.Slug); err == nil && existing != nil {
			return fmt.Errorf("slug %q: %w", tenant.Slug, domain.ErrAlreadyExists)
		} else if err != nil && !errors.Is(err, domain.ErrNotFound) {
			return err
		}

		if err := r.Tenants.Create(ctx, tenant); err != nil {
			return err
		}
		persistedOwner, err := r.Users.Upsert(ctx, owner)
		if err != nil {
			return err
		}
		membership := domain.NewMembership(tenant.ID, persistedOwner.ID, domain.RoleOwner)
		if err := r.Users.AddMembership(ctx, membership); err != nil {
			return err
		}
		out.Tenant = tenant
		out.Owner = persistedOwner
		return r.Events.Publish(ctx,
			domain.NewTenantCreated(tenant.ID, tenant.Slug, tenant.Plan),
			domain.NewMemberAdded(tenant.ID, persistedOwner.ID, domain.RoleOwner),
		)
	})
	if err != nil {
		return nil, err
	}
	s.logger.InfoContext(ctx, "tenant created",
		slog.String("tenant_id", out.Tenant.ID.String()),
		slog.String("slug", out.Tenant.Slug),
	)
	return &out, nil
}

// GetTenant reads a tenant by id.
func (s *Service) GetTenant(ctx context.Context, id domain.TenantID) (*domain.Tenant, error) {
	var t *domain.Tenant
	err := s.uow.Do(ctx, func(r Repositories) error {
		got, err := r.Tenants.GetByID(ctx, id)
		if err != nil {
			return err
		}
		t = got
		return nil
	})
	return t, err
}

// AddMember adds or re-roles a user in a tenant, upserting the user record.
func (s *Service) AddMember(ctx context.Context, in AddMemberInput) (*domain.User, error) {
	user, err := domain.NewUser(in.Email, in.DisplayName, in.Subject)
	if err != nil {
		return nil, err
	}
	if _, err := domain.ParseRole(string(in.Role)); err != nil {
		return nil, err
	}

	var persisted *domain.User
	err = s.uow.Do(ctx, func(r Repositories) error {
		tenant, err := r.Tenants.GetByID(ctx, in.TenantID)
		if err != nil {
			return err
		}
		if !tenant.IsActive() {
			return domain.ErrTenantSuspended
		}
		persisted, err = r.Users.Upsert(ctx, user)
		if err != nil {
			return err
		}
		if err := r.Users.AddMembership(ctx, domain.NewMembership(in.TenantID, persisted.ID, in.Role)); err != nil {
			return err
		}
		return r.Events.Publish(ctx, domain.NewMemberAdded(in.TenantID, persisted.ID, in.Role))
	})
	return persisted, err
}

// RemoveMember enforces the last-owner invariant before removing a membership.
func (s *Service) RemoveMember(ctx context.Context, tenantID domain.TenantID, userID domain.UserID) error {
	return s.uow.Do(ctx, func(r Repositories) error {
		role, err := r.Users.GetRole(ctx, tenantID, userID)
		if err != nil {
			return err
		}
		if role == domain.RoleOwner {
			owners, err := r.Users.CountOwners(ctx, tenantID)
			if err != nil {
				return err
			}
			if owners <= 1 {
				return domain.ErrLastOwner
			}
		}
		if err := r.Users.RemoveMembership(ctx, tenantID, userID); err != nil {
			return err
		}
		return r.Events.Publish(ctx, domain.NewMemberRemoved(tenantID, userID))
	})
}

// ListMembers returns the membership projection for a tenant.
func (s *Service) ListMembers(ctx context.Context, tenantID domain.TenantID) ([]MembershipView, error) {
	var views []MembershipView
	err := s.uow.Do(ctx, func(r Repositories) error {
		got, err := r.Users.ListMemberships(ctx, tenantID)
		if err != nil {
			return err
		}
		views = got
		return nil
	})
	return views, err
}

// IssueKey mints an API key and returns the one-time plaintext.
func (s *Service) IssueKey(ctx context.Context, in IssueKeyInput) (*IssueKeyOutput, error) {
	generated, err := domain.GenerateAPIKey(in.TenantID, in.Name, in.Scopes, in.ExpiresAt)
	if err != nil {
		return nil, err
	}
	err = s.uow.Do(ctx, func(r Repositories) error {
		tenant, err := r.Tenants.GetByID(ctx, in.TenantID)
		if err != nil {
			return err
		}
		if !tenant.IsActive() {
			return domain.ErrTenantSuspended
		}
		if err := r.Keys.Create(ctx, generated.Key); err != nil {
			return err
		}
		return r.Events.Publish(ctx, domain.NewAPIKeyIssued(in.TenantID, generated.Key.ID, generated.Key.Scopes))
	})
	if err != nil {
		return nil, err
	}
	return &IssueKeyOutput{Key: generated.Key, Plaintext: generated.Plaintext}, nil
}

// ListKeys returns key metadata for a tenant. Secrets are never returned.
func (s *Service) ListKeys(ctx context.Context, tenantID domain.TenantID) ([]*domain.APIKey, error) {
	var keys []*domain.APIKey
	err := s.uow.Do(ctx, func(r Repositories) error {
		got, err := r.Keys.ListByTenant(ctx, tenantID)
		if err != nil {
			return err
		}
		keys = got
		return nil
	})
	return keys, err
}

// RevokeKey revokes a key immediately.
func (s *Service) RevokeKey(ctx context.Context, tenantID domain.TenantID, keyID domain.APIKeyID) error {
	return s.uow.Do(ctx, func(r Repositories) error {
		key, err := r.Keys.GetByID(ctx, tenantID, keyID)
		if err != nil {
			return err
		}
		key.Revoke()
		if err := r.Keys.UpdateStatus(ctx, key.ID, key.Status); err != nil {
			return err
		}
		return r.Events.Publish(ctx, domain.NewAPIKeyRevoked(tenantID, keyID))
	})
}

// RotateKey issues a replacement key and revokes the old one, returning the new
// plaintext. The old key remains valid until its own expiry window if set,
// letting callers roll over without downtime; here we revoke immediately and rely
// on the caller to deploy the new key, which is the safer default.
func (s *Service) RotateKey(ctx context.Context, tenantID domain.TenantID, keyID domain.APIKeyID) (*IssueKeyOutput, error) {
	var out *IssueKeyOutput
	err := s.uow.Do(ctx, func(r Repositories) error {
		old, err := r.Keys.GetByID(ctx, tenantID, keyID)
		if err != nil {
			return err
		}
		generated, err := domain.GenerateAPIKey(tenantID, old.Name, old.Scopes, old.ExpiresAt)
		if err != nil {
			return err
		}
		if err := r.Keys.Create(ctx, generated.Key); err != nil {
			return err
		}
		old.Revoke()
		if err := r.Keys.UpdateStatus(ctx, old.ID, old.Status); err != nil {
			return err
		}
		out = &IssueKeyOutput{Key: generated.Key, Plaintext: generated.Plaintext}
		return r.Events.Publish(ctx,
			domain.NewAPIKeyRevoked(tenantID, old.ID),
			domain.NewAPIKeyIssued(tenantID, generated.Key.ID, generated.Key.Scopes),
		)
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// AuthenticateKey verifies a plaintext credential for a required scope. This is
// the hot-path call used by the ingest and query gateways: one indexed lookup by
// prefix, a constant-time hash comparison, then status, expiry, and scope checks.
func (s *Service) AuthenticateKey(ctx context.Context, plaintext string, required domain.Scope) (*AuthenticatedKey, error) {
	prefix, ok := domain.ExtractPrefix(plaintext)
	if !ok {
		return nil, domain.ErrNotFound
	}
	key, err := s.keys.GetByPrefix(ctx, prefix)
	if err != nil {
		return nil, err
	}
	if !key.Matches(plaintext) {
		return nil, domain.ErrNotFound
	}
	if err := key.Usable(time.Now().UTC()); err != nil {
		return nil, err
	}
	if !key.HasScope(required) {
		return nil, domain.ErrScopeNotGranted
	}
	// Best-effort usage timestamp; failure here must not fail authentication.
	if err := s.keys.TouchLastUsed(ctx, key.ID); err != nil {
		s.logger.WarnContext(ctx, "touch last_used failed", slog.String("key_id", key.ID.String()), slog.Any("error", err))
	}
	return &AuthenticatedKey{TenantID: key.TenantID, KeyID: key.ID, Scopes: key.Scopes}, nil
}
