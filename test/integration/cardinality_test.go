//go:build integration

package integration

import (
	"context"
	"testing"
	"time"

	"github.com/Talif787/prism/internal/ingest/app"
	"github.com/Talif787/prism/internal/ingest/infra/cardinality"
)

func fp(b byte) []byte { return []byte{b, 0, 0, 0, 0, 0, 0, 0} }

func TestCardinality_EnforcesBudget(t *testing.T) {
	rdb := redisClient(t)
	guard := cardinality.New(rdb, cardinality.Config{Budget: 3, Window: time.Hour, Enforce: true})
	ctx := context.Background()

	// Add three distinct series: within budget.
	dec, err := guard.Admit(ctx, "t1", [][]byte{fp(1), fp(2), fp(3)})
	if err != nil {
		t.Fatalf("admit: %v", err)
	}
	if !dec.Admitted {
		t.Fatalf("three series should be within budget of 3, estimate=%d", dec.Estimate)
	}

	// Re-adding the same series is free (HLL idempotent): still admitted.
	if dec, _ = guard.Admit(ctx, "t1", [][]byte{fp(1), fp(2)}); !dec.Admitted {
		t.Fatal("established series must remain admitted")
	}

	// New series push the estimate over budget: rejected under enforcement.
	dec, err = guard.Admit(ctx, "t1", [][]byte{fp(4), fp(5)})
	if err != nil {
		t.Fatalf("admit: %v", err)
	}
	if dec.Admitted {
		t.Fatalf("growth past budget must be rejected, estimate=%d budget=%d", dec.Estimate, dec.Budget)
	}
}

func TestCardinality_ObserveOnly(t *testing.T) {
	rdb := redisClient(t)
	guard := cardinality.New(rdb, cardinality.Config{Budget: 1, Window: time.Hour, Enforce: false})
	ctx := context.Background()

	dec, err := guard.Admit(ctx, "t2", [][]byte{fp(1), fp(2), fp(3)})
	if err != nil {
		t.Fatalf("admit: %v", err)
	}
	if !dec.Admitted {
		t.Fatal("observe-only mode must always admit")
	}
	if dec.Estimate < 3 {
		t.Fatalf("estimate should reflect three series, got %d", dec.Estimate)
	}
}

var _ app.CardinalityGuard = (*cardinality.Guard)(nil)
