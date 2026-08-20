package domain

import (
	"regexp"
	"strings"
	"time"
)

type TenantStatus string

const (
	TenantActive    TenantStatus = "active"
	TenantSuspended TenantStatus = "suspended"
)

type Plan string

const (
	PlanFree       Plan = "free"
	PlanTeam       Plan = "team"
	PlanEnterprise Plan = "enterprise"
)

var slugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,38}[a-z0-9]$`)

// Tenant is the aggregate root for an isolated customer workspace.
type Tenant struct {
	ID        TenantID
	Name      string
	Slug      string
	Plan      Plan
	Status    TenantStatus
	CreatedAt time.Time
	UpdatedAt time.Time
}

// NewTenant creates a valid, active tenant or returns a validation error.
func NewTenant(name, slug string, plan Plan) (*Tenant, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 120 {
		return nil, ErrInvalidName
	}
	slug = strings.TrimSpace(slug)
	if !slugPattern.MatchString(slug) {
		return nil, ErrInvalidSlug
	}
	if plan == "" {
		plan = PlanFree
	}
	now := time.Now().UTC()
	return &Tenant{
		ID:        NewTenantID(),
		Name:      name,
		Slug:      slug,
		Plan:      plan,
		Status:    TenantActive,
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

func (t *Tenant) IsActive() bool { return t.Status == TenantActive }

func (t *Tenant) Suspend() {
	t.Status = TenantSuspended
	t.UpdatedAt = time.Now().UTC()
}
