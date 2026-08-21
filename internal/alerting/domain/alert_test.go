package domain

import (
	"errors"
	"testing"
	"time"
)

func TestRuleBreached(t *testing.T) {
	cases := []struct {
		op        Operator
		threshold float64
		value     float64
		want      bool
	}{
		{OpGreaterThan, 10, 11, true},
		{OpGreaterThan, 10, 10, false},
		{OpGreaterOrEqual, 10, 10, true},
		{OpLessThan, 10, 9, true},
		{OpLessThan, 10, 10, false},
		{OpLessOrEqual, 10, 10, true},
	}
	for _, c := range cases {
		r := &Rule{Operator: c.op, Threshold: c.threshold}
		if got := r.Breached(c.value); got != c.want {
			t.Errorf("%s %v vs %v: got %v want %v", c.op, c.value, c.threshold, got, c.want)
		}
	}
}

func TestRuleValidate(t *testing.T) {
	base := func() *Rule {
		return &Rule{Name: "r", Metric: "m", Operator: OpGreaterThan, Threshold: 1, Window: time.Minute, Interval: 30 * time.Second}
	}
	t.Run("defaults agg and severity", func(t *testing.T) {
		r := base()
		if err := r.Validate(); err != nil {
			t.Fatalf("unexpected: %v", err)
		}
		if r.Agg != "avg" || r.Severity != "warning" {
			t.Fatalf("expected defaults, got agg=%q severity=%q", r.Agg, r.Severity)
		}
	})
	t.Run("rejects bad operator", func(t *testing.T) {
		r := base()
		r.Operator = "between"
		if err := r.Validate(); !errors.Is(err, ErrInvalidRule) {
			t.Fatalf("expected ErrInvalidRule, got %v", err)
		}
	})
	t.Run("rejects window over max", func(t *testing.T) {
		r := base()
		r.Window = MaxWindow + time.Hour
		if err := r.Validate(); !errors.Is(err, ErrInvalidRule) {
			t.Fatalf("expected ErrInvalidRule, got %v", err)
		}
	})
}

func TestFingerprintStable(t *testing.T) {
	a := Fingerprint(map[string]string{"route": "/a", "host": "web1"})
	b := Fingerprint(map[string]string{"host": "web1", "route": "/a"})
	if a != b {
		t.Fatalf("fingerprint should be order-independent: %s vs %s", a, b)
	}
	if a == Fingerprint(map[string]string{"route": "/b", "host": "web1"}) {
		t.Fatal("different labels should fingerprint differently")
	}
}
