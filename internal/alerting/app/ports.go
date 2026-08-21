// Package app is the alerting application layer: rule CRUD and the evaluation
// engine. It is storage-, metrics-, and notifier-agnostic through the ports below.
package app

import (
	"context"
	"errors"
	"time"

	"github.com/Talif787/prism/internal/alerting/domain"
)

var (
	ErrUnauthorized = errors.New("unauthorized")
	ErrForbidden    = errors.New("forbidden")
)

// Principal is the authenticated caller, bound to one tenant.
type Principal struct {
	TenantID string
	Scopes   []string
}

func (p *Principal) HasScope(scope string) bool {
	for _, s := range p.Scopes {
		if s == scope {
			return true
		}
	}
	return false
}

// Authenticator resolves an API key to a principal.
type Authenticator interface {
	Authenticate(ctx context.Context, apiKey string) (*Principal, error)
}

// RuleStore persists rules and the current alert instances.
type RuleStore interface {
	CreateRule(ctx context.Context, r *domain.Rule) error
	GetRule(ctx context.Context, tenantID, id string) (*domain.Rule, error)
	ListRules(ctx context.Context, tenantID string) ([]domain.Rule, error)
	UpdateRule(ctx context.Context, r *domain.Rule) error
	DeleteRule(ctx context.Context, tenantID, id string) error

	LoadDueRules(ctx context.Context, now time.Time) ([]domain.Rule, error)
	MarkEvaluated(ctx context.Context, ruleID string, at time.Time) error

	ListInstances(ctx context.Context, ruleID string) ([]domain.Instance, error)
	ListTenantInstances(ctx context.Context, tenantID string) ([]domain.Instance, error)
	UpsertInstance(ctx context.Context, i *domain.Instance) error
	DeleteInstance(ctx context.Context, ruleID, fingerprint string) error
}

// Condition is the metric query a rule evaluates: one aggregated value per series
// over the trailing window.
type Condition struct {
	Metric  string
	Agg     string
	GroupBy []string
	Filters map[string]string
	Window  time.Duration
}

// SeriesValue is one aggregated series value produced by a condition.
type SeriesValue struct {
	Labels map[string]string
	Value  float64
}

// MetricsReader evaluates a condition against the metrics store.
type MetricsReader interface {
	Read(ctx context.Context, tenantID string, cond Condition) ([]SeriesValue, error)
}

// Notification describes a state transition delivered to a webhook.
type Notification struct {
	Event    string // "firing" or "resolved"
	Rule     domain.Rule
	Instance domain.Instance
}

// Notifier delivers a notification to a destination (a webhook URL).
type Notifier interface {
	Notify(ctx context.Context, webhook string, n Notification) error
}
