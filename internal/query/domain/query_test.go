package domain

import (
	"errors"
	"testing"
	"time"
)

func TestRangeQuery_Validate(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	t.Run("defaults aggregation to avg", func(t *testing.T) {
		q := RangeQuery{Metric: "m", From: base, To: base.Add(time.Hour), Step: time.Minute}
		if err := q.Validate(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if q.Agg != "avg" {
			t.Fatalf("expected default agg avg, got %q", q.Agg)
		}
	})

	t.Run("rejects missing metric", func(t *testing.T) {
		q := RangeQuery{From: base, To: base.Add(time.Hour), Step: time.Minute}
		if err := q.Validate(); !errors.Is(err, ErrInvalidQuery) {
			t.Fatalf("expected ErrInvalidQuery, got %v", err)
		}
	})

	t.Run("rejects reversed range", func(t *testing.T) {
		q := RangeQuery{Metric: "m", From: base.Add(time.Hour), To: base, Step: time.Minute}
		if err := q.Validate(); !errors.Is(err, ErrInvalidQuery) {
			t.Fatalf("expected ErrInvalidQuery, got %v", err)
		}
	})

	t.Run("rejects range over the maximum", func(t *testing.T) {
		q := RangeQuery{Metric: "m", From: base, To: base.Add(MaxRange + time.Hour), Step: time.Hour}
		if err := q.Validate(); !errors.Is(err, ErrInvalidQuery) {
			t.Fatalf("expected ErrInvalidQuery, got %v", err)
		}
	})

	t.Run("rejects too many buckets", func(t *testing.T) {
		// One second steps across two hours is 7200 buckets (ok); across four is not.
		q := RangeQuery{Metric: "m", From: base, To: base.Add(4 * time.Hour), Step: time.Second}
		if err := q.Validate(); !errors.Is(err, ErrInvalidQuery) {
			t.Fatalf("expected ErrInvalidQuery for bucket blowup, got %v", err)
		}
	})

	t.Run("rejects unknown aggregation", func(t *testing.T) {
		q := RangeQuery{Metric: "m", From: base, To: base.Add(time.Hour), Step: time.Minute, Agg: "median"}
		if err := q.Validate(); !errors.Is(err, ErrInvalidQuery) {
			t.Fatalf("expected ErrInvalidQuery, got %v", err)
		}
	})
}

func TestLogQuery_ClampsLimit(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	q := LogQuery{From: base, To: base.Add(time.Hour), Limit: 0}
	if err := q.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if q.Limit != DefaultLimit {
		t.Fatalf("expected default limit %d, got %d", DefaultLimit, q.Limit)
	}
	q2 := LogQuery{From: base, To: base.Add(time.Hour), Limit: 99999}
	_ = q2.Validate()
	if q2.Limit != MaxLimit {
		t.Fatalf("expected clamp to %d, got %d", MaxLimit, q2.Limit)
	}
}
