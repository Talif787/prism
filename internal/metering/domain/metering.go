// Package domain is the metering and billing bounded context: signals, usage
// records, the rate card and pricing math, per-plan quotas, and invoices. Pricing
// and quota logic are pure functions over inputs supplied by configuration, so
// they are fully unit-testable and depend only on the standard library.
package domain

import (
	"errors"
	"math"
	"time"
)

var (
	ErrNotFound      = errors.New("not found")
	ErrInvalidPeriod = errors.New("invalid period")
)

// Signal is a billable telemetry kind.
type Signal string

const (
	SignalMetrics Signal = "metrics"
	SignalLogs    Signal = "logs"
	SignalTraces  Signal = "traces"
)

// AllSignals is the stable iteration and reporting order.
var AllSignals = []Signal{SignalMetrics, SignalLogs, SignalTraces}

func ValidSignal(s Signal) bool {
	switch s {
	case SignalMetrics, SignalLogs, SignalTraces:
		return true
	default:
		return false
	}
}

// UsageRecord is one rolled-up count for a tenant, signal, and window.
type UsageRecord struct {
	TenantID    string
	Signal      Signal
	WindowStart time.Time
	Count       int64
}

// RateCard prices usage per signal, expressed as price per one million points.
type RateCard struct {
	PerMillion map[Signal]float64
	Currency   string
}

// LineItem is a priced quantity of one signal.
type LineItem struct {
	Signal              Signal
	Quantity            int64
	UnitPricePerMillion float64
	Amount              float64
}

// PriceUsage turns per-signal counts into line items and a total, in AllSignals
// order so output is deterministic. Amounts are rounded to six decimal places.
func PriceUsage(counts map[Signal]int64, rc RateCard) ([]LineItem, float64) {
	items := make([]LineItem, 0, len(AllSignals))
	var total float64
	for _, sig := range AllSignals {
		qty := counts[sig]
		unit := rc.PerMillion[sig]
		amount := round6(float64(qty) / 1_000_000 * unit)
		items = append(items, LineItem{Signal: sig, Quantity: qty, UnitPricePerMillion: unit, Amount: amount})
		total += amount
	}
	return items, round6(total)
}

// Unlimited is the included quota for a plan with no cap.
const Unlimited int64 = -1

// Quota is a tenant's monthly allowance and consumption.
type Quota struct {
	Plan      string
	Included  int64
	Used      int64
	Remaining int64
	Over      bool
}

// EvaluateQuota computes remaining allowance and over-quota status. An Included of
// Unlimited (negative) means no cap: Remaining stays Unlimited and Over is false.
func EvaluateQuota(plan string, included, used int64) Quota {
	q := Quota{Plan: plan, Included: included, Used: used}
	if included < 0 {
		q.Remaining = Unlimited
		q.Over = false
		return q
	}
	q.Remaining = included - used
	if q.Remaining < 0 {
		q.Remaining = 0
	}
	q.Over = used > included
	return q
}

// Invoice is a closed billing statement for a period.
type Invoice struct {
	ID          string
	TenantID    string
	PeriodStart time.Time
	PeriodEnd   time.Time
	Status      string
	Currency    string
	Total       float64
	LineItems   []LineItem
	CreatedAt   time.Time
}

// TotalPoints sums counts across signals.
func TotalPoints(counts map[Signal]int64) int64 {
	var t int64
	for _, sig := range AllSignals {
		t += counts[sig]
	}
	return t
}

func round6(x float64) float64 { return math.Round(x*1_000_000) / 1_000_000 }
