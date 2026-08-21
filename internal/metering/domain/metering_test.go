package domain

import (
	"testing"
)

func TestPriceUsage(t *testing.T) {
	rc := RateCard{
		PerMillion: map[Signal]float64{SignalMetrics: 0.10, SignalLogs: 0.50, SignalTraces: 0.20},
		Currency:   "USD",
	}
	counts := map[Signal]int64{SignalMetrics: 2_000_000, SignalLogs: 1_000_000, SignalTraces: 500_000}
	items, total := PriceUsage(counts, rc)

	if len(items) != 3 {
		t.Fatalf("expected 3 line items, got %d", len(items))
	}
	// metrics: 2M * 0.10 = 0.20; logs: 1M * 0.50 = 0.50; traces: 0.5M * 0.20 = 0.10
	if items[0].Amount != 0.20 || items[1].Amount != 0.50 || items[2].Amount != 0.10 {
		t.Fatalf("unexpected amounts: %v", items)
	}
	if total != 0.80 {
		t.Fatalf("expected total 0.80, got %v", total)
	}
	// ordering must follow AllSignals
	if items[0].Signal != SignalMetrics || items[2].Signal != SignalTraces {
		t.Fatalf("line items not in AllSignals order: %v", items)
	}
}

func TestEvaluateQuota(t *testing.T) {
	t.Run("under", func(t *testing.T) {
		q := EvaluateQuota("free", 10_000_000, 4_000_000)
		if q.Over || q.Remaining != 6_000_000 {
			t.Fatalf("unexpected: %+v", q)
		}
	})
	t.Run("over clamps remaining to zero", func(t *testing.T) {
		q := EvaluateQuota("free", 10_000_000, 12_000_000)
		if !q.Over || q.Remaining != 0 {
			t.Fatalf("unexpected: %+v", q)
		}
	})
	t.Run("unlimited", func(t *testing.T) {
		q := EvaluateQuota("enterprise", Unlimited, 999_999_999)
		if q.Over || q.Remaining != Unlimited {
			t.Fatalf("unexpected: %+v", q)
		}
	})
}

func TestTotalPoints(t *testing.T) {
	got := TotalPoints(map[Signal]int64{SignalMetrics: 3, SignalLogs: 4, SignalTraces: 5})
	if got != 12 {
		t.Fatalf("expected 12, got %d", got)
	}
}
