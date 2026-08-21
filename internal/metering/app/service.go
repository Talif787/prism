package app

import (
	"context"
	"time"

	"github.com/Talif787/prism/internal/metering/domain"
)

// MeteringService answers usage, quota, cost, and invoice questions for a tenant
// over the rolled-up data. Pricing and quota math live in the domain; this service
// supplies the rate card and per-plan allowances from configuration.
type MeteringService struct {
	store      UsageStore
	rateCard   domain.RateCard
	planQuotas map[string]int64
}

func NewMeteringService(store UsageStore, rateCard domain.RateCard, planQuotas map[string]int64) *MeteringService {
	return &MeteringService{store: store, rateCard: rateCard, planQuotas: planQuotas}
}

// Usage returns per-signal counts over [from, to).
func (s *MeteringService) Usage(ctx context.Context, tenantID string, from, to time.Time) (map[domain.Signal]int64, error) {
	return s.store.UsageBySignal(ctx, tenantID, from, to)
}

// Summary is the current billing period to date: usage, quota, and estimated cost.
type Summary struct {
	PeriodStart time.Time
	PeriodEnd   time.Time
	Usage       map[domain.Signal]int64
	TotalPoints int64
	Quota       domain.Quota
	LineItems   []domain.LineItem
	Cost        float64
	Currency    string
}

// Summary reports the current calendar-month usage to date, the plan quota, and
// the estimated cost at the configured rate card.
func (s *MeteringService) Summary(ctx context.Context, tenantID string, now time.Time) (*Summary, error) {
	start := monthStart(now)
	usage, err := s.store.UsageBySignal(ctx, tenantID, start, now)
	if err != nil {
		return nil, err
	}
	plan, err := s.store.TenantPlan(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	total := domain.TotalPoints(usage)
	quota := domain.EvaluateQuota(plan, s.includedFor(plan), total)
	items, cost := domain.PriceUsage(usage, s.rateCard)
	return &Summary{
		PeriodStart: start, PeriodEnd: now, Usage: usage, TotalPoints: total,
		Quota: quota, LineItems: items, Cost: cost, Currency: s.rateCard.Currency,
	}, nil
}

// CloseInvoice prices usage over [from, to) and persists it as a closed invoice.
func (s *MeteringService) CloseInvoice(ctx context.Context, tenantID string, from, to time.Time) (*domain.Invoice, error) {
	if !from.Before(to) {
		return nil, domain.ErrInvalidPeriod
	}
	usage, err := s.store.UsageBySignal(ctx, tenantID, from, to)
	if err != nil {
		return nil, err
	}
	items, total := domain.PriceUsage(usage, s.rateCard)
	inv := &domain.Invoice{
		TenantID: tenantID, PeriodStart: from, PeriodEnd: to, Status: "closed",
		Currency: s.rateCard.Currency, Total: total, LineItems: items,
	}
	if err := s.store.CreateInvoice(ctx, inv); err != nil {
		return nil, err
	}
	return inv, nil
}

func (s *MeteringService) ListInvoices(ctx context.Context, tenantID string) ([]domain.Invoice, error) {
	return s.store.ListInvoices(ctx, tenantID)
}

func (s *MeteringService) GetInvoice(ctx context.Context, tenantID, id string) (*domain.Invoice, error) {
	return s.store.GetInvoice(ctx, tenantID, id)
}

// includedFor returns the monthly point allowance for a plan. An unknown plan is
// treated as uncapped so metering never reports a false over-quota.
func (s *MeteringService) includedFor(plan string) int64 {
	if v, ok := s.planQuotas[plan]; ok {
		return v
	}
	return domain.Unlimited
}

func monthStart(t time.Time) time.Time {
	u := t.UTC()
	return time.Date(u.Year(), u.Month(), 1, 0, 0, 0, 0, time.UTC)
}
