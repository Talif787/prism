package app

import (
	"context"

	"github.com/Talif787/prism/internal/alerting/domain"
)

// RuleService orchestrates tenant-scoped rule CRUD and alert listing.
type RuleService struct {
	store RuleStore
}

func NewRuleService(store RuleStore) *RuleService { return &RuleService{store: store} }

func (s *RuleService) CreateRule(ctx context.Context, tenantID string, r *domain.Rule) error {
	r.TenantID = tenantID
	if err := r.Validate(); err != nil {
		return err
	}
	return s.store.CreateRule(ctx, r)
}

func (s *RuleService) GetRule(ctx context.Context, tenantID, id string) (*domain.Rule, error) {
	return s.store.GetRule(ctx, tenantID, id)
}

func (s *RuleService) ListRules(ctx context.Context, tenantID string) ([]domain.Rule, error) {
	return s.store.ListRules(ctx, tenantID)
}

func (s *RuleService) UpdateRule(ctx context.Context, tenantID, id string, r *domain.Rule) error {
	r.TenantID = tenantID
	r.ID = id
	if err := r.Validate(); err != nil {
		return err
	}
	return s.store.UpdateRule(ctx, r)
}

func (s *RuleService) DeleteRule(ctx context.Context, tenantID, id string) error {
	return s.store.DeleteRule(ctx, tenantID, id)
}

func (s *RuleService) ListAlerts(ctx context.Context, tenantID string) ([]domain.Instance, error) {
	return s.store.ListTenantInstances(ctx, tenantID)
}
