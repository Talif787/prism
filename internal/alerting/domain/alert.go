// Package domain is the alerting bounded context: alert rules, alert instances,
// and the pure evaluation helpers (threshold comparison and series fingerprinting)
// with rule validation. It depends only on the standard library.
package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"time"
)

var (
	// ErrNotFound is returned when a rule does not exist for the tenant.
	ErrNotFound = errors.New("not found")
	// ErrInvalidRule is returned when a rule fails validation.
	ErrInvalidRule    = errors.New("invalid rule")
	ErrRuleNameExists = errors.New("a rule with this name already exists")
)

// State is the lifecycle state of an alert instance. Resolved instances are not
// stored; they are deleted, so only pending and firing are persisted.
type State string

const (
	StatePending State = "pending"
	StateFiring  State = "firing"
)

// Operator compares an observed value against a rule threshold.
type Operator string

const (
	OpGreaterThan    Operator = "gt"
	OpGreaterOrEqual Operator = "gte"
	OpLessThan       Operator = "lt"
	OpLessOrEqual    Operator = "lte"
)

// aggregations accepted for a rule, matching the query service.
var validAggs = map[string]bool{"avg": true, "sum": true, "min": true, "max": true, "count": true, "last": true}

// Guard bounds on rule fields.
const (
	MinWindow   = time.Second
	MaxWindow   = 24 * time.Hour
	MinInterval = 5 * time.Second
	MaxInterval = time.Hour
	MaxFor      = 24 * time.Hour
	MaxGroupBy  = 8
)

// Rule is a tenant's threshold alert definition over one metric.
type Rule struct {
	ID          string
	TenantID    string
	Name        string
	Metric      string
	Agg         string
	GroupBy     []string
	Filters     map[string]string
	Window      time.Duration
	Operator    Operator
	Threshold   float64
	For         time.Duration
	Interval    time.Duration
	Severity    string
	Labels      map[string]string
	Annotations map[string]string
	Webhook     string
	Enabled     bool
	LastEval    *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Validate applies field and guard checks, normalizing defaults.
func (r *Rule) Validate() error {
	if r.Name == "" {
		return fmt.Errorf("%w: name is required", ErrInvalidRule)
	}
	if r.Metric == "" {
		return fmt.Errorf("%w: metric is required", ErrInvalidRule)
	}
	if r.Agg == "" {
		r.Agg = "avg"
	}
	if !validAggs[r.Agg] {
		return fmt.Errorf("%w: unknown aggregation %q", ErrInvalidRule, r.Agg)
	}
	if !r.Operator.valid() {
		return fmt.Errorf("%w: operator must be one of gt, gte, lt, lte", ErrInvalidRule)
	}
	if len(r.GroupBy) > MaxGroupBy {
		return fmt.Errorf("%w: at most %d group-by attributes", ErrInvalidRule, MaxGroupBy)
	}
	if r.Window < MinWindow || r.Window > MaxWindow {
		return fmt.Errorf("%w: window must be between %s and %s", ErrInvalidRule, MinWindow, MaxWindow)
	}
	if r.Interval == 0 {
		r.Interval = 30 * time.Second
	}
	if r.Interval < MinInterval || r.Interval > MaxInterval {
		return fmt.Errorf("%w: interval must be between %s and %s", ErrInvalidRule, MinInterval, MaxInterval)
	}
	if r.For < 0 || r.For > MaxFor {
		return fmt.Errorf("%w: for must be between 0 and %s", ErrInvalidRule, MaxFor)
	}
	if r.Severity == "" {
		r.Severity = "warning"
	}
	return nil
}

func (o Operator) valid() bool {
	switch o {
	case OpGreaterThan, OpGreaterOrEqual, OpLessThan, OpLessOrEqual:
		return true
	default:
		return false
	}
}

// Breached reports whether an observed value breaches the rule threshold.
func (r *Rule) Breached(value float64) bool {
	switch r.Operator {
	case OpGreaterThan:
		return value > r.Threshold
	case OpGreaterOrEqual:
		return value >= r.Threshold
	case OpLessThan:
		return value < r.Threshold
	case OpLessOrEqual:
		return value <= r.Threshold
	default:
		return false
	}
}

// Instance is the current state of one series for a rule.
type Instance struct {
	ID          string
	RuleID      string
	TenantID    string
	Fingerprint string
	Labels      map[string]string
	State       State
	Value       float64
	ActiveSince time.Time
	FiredAt     *time.Time
	UpdatedAt   time.Time
}

// Fingerprint is a stable identity for a series within a rule, derived from its
// label set, so the same series maps to the same instance across evaluations.
func Fingerprint(labels map[string]string) string {
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	h := sha256.New()
	for _, k := range keys {
		h.Write([]byte(k))
		h.Write([]byte{0})
		h.Write([]byte(labels[k]))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))[:32]
}
