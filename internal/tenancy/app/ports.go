// Package app is the application layer for the tenancy context. It orchestrates
// domain logic through repository ports and emits domain events. It depends only
// on the domain and on interfaces it defines here, never on infrastructure.
package app

import (
	"context"

	"github.com/Talif787/prism/internal/tenancy/domain"
)

// TenantRepository persists and retrieves tenant aggregates.
type TenantRepository interface {
	Create(ctx context.Context, t *domain.Tenant) error
	GetByID(ctx context.Context, id domain.TenantID) (*domain.Tenant, error)
	GetBySlug(ctx context.Context, slug string) (*domain.Tenant, error)
}

// UserRepository persists users and their memberships.
type UserRepository interface {
	Upsert(ctx context.Context, u *domain.User) (*domain.User, error)
	GetByEmail(ctx context.Context, email string) (*domain.User, error)
	AddMembership(ctx context.Context, m domain.Membership) error
	RemoveMembership(ctx context.Context, tenantID domain.TenantID, userID domain.UserID) error
	ListMemberships(ctx context.Context, tenantID domain.TenantID) ([]MembershipView, error)
	CountOwners(ctx context.Context, tenantID domain.TenantID) (int, error)
	GetRole(ctx context.Context, tenantID domain.TenantID, userID domain.UserID) (domain.Role, error)
}

// APIKeyRepository persists API keys. Lookups for authentication are by prefix,
// which is indexed; the caller then verifies the hash in constant time.
type APIKeyRepository interface {
	Create(ctx context.Context, k *domain.APIKey) error
	GetByPrefix(ctx context.Context, prefix string) (*domain.APIKey, error)
	GetByID(ctx context.Context, tenantID domain.TenantID, id domain.APIKeyID) (*domain.APIKey, error)
	ListByTenant(ctx context.Context, tenantID domain.TenantID) ([]*domain.APIKey, error)
	UpdateStatus(ctx context.Context, id domain.APIKeyID, status domain.APIKeyStatus) error
	TouchLastUsed(ctx context.Context, id domain.APIKeyID) error
}

// EventPublisher records domain events for reliable downstream delivery. In
// Phase 1 this writes to a transactional outbox; a relay ships them to Kafka.
type EventPublisher interface {
	Publish(ctx context.Context, events ...domain.Event) error
}

// UnitOfWork runs a function within a single database transaction, giving each
// repository a transaction-scoped view so multi-step use cases stay atomic.
type UnitOfWork interface {
	Do(ctx context.Context, fn func(Repositories) error) error
}

// Repositories bundles the transaction-scoped ports handed to a use case.
type Repositories struct {
	Tenants TenantRepository
	Users   UserRepository
	Keys    APIKeyRepository
	Events  EventPublisher
}

// MembershipView is a read-optimized projection joining users and roles.
type MembershipView struct {
	UserID      domain.UserID
	Email       string
	DisplayName string
	Role        domain.Role
}
