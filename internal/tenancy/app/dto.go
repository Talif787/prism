package app

import (
	"time"

	"github.com/Talif787/prism/internal/tenancy/domain"
)

type CreateTenantInput struct {
	Name         string
	Slug         string
	Plan         domain.Plan
	OwnerEmail   string
	OwnerName    string
	OwnerSubject string
}

type CreateTenantOutput struct {
	Tenant *domain.Tenant
	Owner  *domain.User
}

type AddMemberInput struct {
	TenantID    domain.TenantID
	Email       string
	DisplayName string
	Subject     string
	Role        domain.Role
}

type IssueKeyInput struct {
	TenantID  domain.TenantID
	Name      string
	Scopes    []domain.Scope
	ExpiresAt *time.Time
}

// IssueKeyOutput carries the one-time plaintext. Callers must surface it once and
// never persist it.
type IssueKeyOutput struct {
	Key       *domain.APIKey
	Plaintext string
}

// AuthenticatedKey is the result of verifying an ingest or query credential.
type AuthenticatedKey struct {
	TenantID domain.TenantID
	KeyID    domain.APIKeyID
	Scopes   []domain.Scope
}
